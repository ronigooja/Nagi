#!/usr/bin/env bash
set -euo pipefail

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
for target in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64; do
    TARGET_GOOS="${target%-*}" TARGET_GOARCH="${target#*-}" "$project_dir/scripts/build.sh"
done

# Produce the stable asset names consumed by scripts/install.sh. A macOS App
# is included when it was built beside the CLI and matches the host architecture;
# unsigned Apps remain an explicit user choice at installation time.
command -v tar >/dev/null || { echo 'tar is required to package releases' >&2; exit 1; }
command -v shasum >/dev/null || { echo 'shasum is required to package releases' >&2; exit 1; }
stage_root="$project_dir/dist/.release-stage"
rm -rf -- "$stage_root"
mkdir -p "$stage_root"
cleanup_stage() { rm -rf -- "$stage_root"; }
trap cleanup_stage EXIT
package_target() {
    local target=$1 source=$2 app=0 stage archive
    [[ -x "$source/nagi" && -x "$source/mihomo" ]] || {
        echo "missing release binaries for $target" >&2
        exit 1
    }
    stage="$stage_root/$target"
    archive="$project_dir/dist/nagi-$target.tar.gz"
    mkdir -p "$stage"
    cp "$source/nagi" "$source/mihomo" "$project_dir/scripts/installer.sh" "$stage/"
    chmod 0755 "$stage/installer.sh"
    if [[ "$target" == macos-* && -d "$project_dir/bin/Nagi.app" ]]; then
        case "$(uname -m)" in
            x86_64|amd64) [[ "$target" == macos-amd64 ]] && app=1 ;;
            arm64|aarch64) [[ "$target" == macos-arm64 ]] && app=1 ;;
        esac
    fi
    if (( app )); then cp -R "$project_dir/bin/Nagi.app" "$stage/Nagi.app"; fi
    rm -f -- "$archive"
    if (( app )); then
        tar -C "$stage" -czf "$archive" nagi mihomo installer.sh Nagi.app
    else
        tar -C "$stage" -czf "$archive" nagi mihomo installer.sh
    fi
}
package_target linux-amd64 "$project_dir/dist/linux-amd64"
package_target linux-arm64 "$project_dir/dist/linux-arm64"
package_target macos-amd64 "$project_dir/dist/darwin-amd64"
package_target macos-arm64 "$project_dir/dist/darwin-arm64"
(cd "$project_dir/dist" && shasum -a 256 nagi-*.tar.gz > SHA256SUMS)

if [[ "$(uname -s)" == Darwin ]]; then
    mkdir -p "$project_dir/dist/darwin-universal"
    lipo -create "$project_dir/dist/darwin-amd64/nagi" "$project_dir/dist/darwin-arm64/nagi" -output "$project_dir/dist/darwin-universal/nagi"
    lipo -create "$project_dir/dist/darwin-amd64/mihomo" "$project_dir/dist/darwin-arm64/mihomo" -output "$project_dir/dist/darwin-universal/mihomo"
    if [[ -n "${APPLE_SIGN_IDENTITY:-}" ]]; then
        codesign --force --options runtime --sign "$APPLE_SIGN_IDENTITY" "$project_dir/dist/darwin-universal/nagi" "$project_dir/dist/darwin-universal/mihomo"
    fi
    if [[ -n "${APPLE_NOTARY_PROFILE:-}" ]]; then
        if [[ -z "${APPLE_SIGN_IDENTITY:-}" ]]; then
            echo 'APPLE_NOTARY_PROFILE requires APPLE_SIGN_IDENTITY' >&2
            exit 1
        fi
        (cd "$project_dir/dist/darwin-universal" && zip -q nagi-macos-universal.zip nagi mihomo)
        xcrun notarytool submit "$project_dir/dist/darwin-universal/nagi-macos-universal.zip" --keychain-profile "$APPLE_NOTARY_PROFILE" --wait
    fi
fi
