#!/usr/bin/env bash
set -euo pipefail

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
for target in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64; do
    TARGET_GOOS="${target%-*}" TARGET_GOARCH="${target#*-}" "$project_dir/scripts/build.sh"
done

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
