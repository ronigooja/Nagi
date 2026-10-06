#!/usr/bin/env bash
set -euo pipefail

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if (( $# > 1 )); then echo 'Usage: scripts/build.sh [cli]' >&2; exit 2; fi
build_mode=${1:-all}
case "$build_mode" in all|cli) ;; *) echo 'Usage: scripts/build.sh [cli]' >&2; exit 2 ;; esac
read_lock() {
    sed -n "s/^$1: //p" "$project_dir/engine.lock"
}
repository="$(read_lock repository)"
ref="$(read_lock ref)"
commit="$(read_lock commit)"
if [[ ! "$commit" =~ ^[0-9a-f]{40}$ || -z "$repository" || -z "$ref" ]]; then
    echo 'Invalid engine.lock' >&2
    exit 1
fi

if [[ "$build_mode" == all ]]; then
    if [[ -n "${MIHOMO_DIR:-}" ]]; then
        engine_dir="$(cd "$MIHOMO_DIR" && pwd)"
        actual="$(git -C "$engine_dir" rev-parse HEAD)"
        if [[ "$actual" != "$commit" ]]; then
            echo "MIHOMO_DIR is at $actual; engine.lock requires $commit" >&2
            exit 1
        fi
    else
        engine_dir="$project_dir/.build/mihomo"
        if [[ ! -d "$engine_dir/.git" ]]; then
            mkdir -p "$(dirname "$engine_dir")"
            git clone --no-checkout "$repository" "$engine_dir"
        fi
        git -C "$engine_dir" fetch origin "$ref"
        git -C "$engine_dir" cat-file -e "$commit^{commit}" || {
            echo "Pinned mihomo commit $commit cannot be obtained" >&2
            exit 1
        }
        if ! git -C "$engine_dir" merge-base --is-ancestor "$commit" FETCH_HEAD; then
            echo "Pinned mihomo commit $commit is not on $ref" >&2
            exit 1
        fi
        git -C "$engine_dir" checkout --detach "$commit"
    fi
fi

go_bin="${GO:-}"
if [[ -z "$go_bin" ]]; then
    go_bin="$(command -v go || true)"
fi
if [[ -z "$go_bin" ]]; then
    for candidate in "$HOME"/environment/go*/bin/go; do
        if [[ -x "$candidate" ]]; then
            go_bin="$candidate"
            break
        fi
    done
fi
if [[ -z "$go_bin" ]] || ! command -v "$go_bin" >/dev/null 2>&1; then
    echo 'Go executable not found; install Go or set GO=/path/to/go' >&2
    exit 1
fi
nagi_version="$(git -C "$project_dir" describe --tags --always --dirty)"
output_dir="$project_dir/bin"
if [[ -n "${TARGET_GOOS:-}" || -n "${TARGET_GOARCH:-}" ]]; then
    if [[ -z "${TARGET_GOOS:-}" || -z "${TARGET_GOARCH:-}" ]]; then
        echo 'Set both TARGET_GOOS and TARGET_GOARCH' >&2
        exit 1
    fi
    case "$TARGET_GOOS/$TARGET_GOARCH" in
        linux/amd64|linux/arm64|darwin/amd64|darwin/arm64) ;;
        *) echo 'Unsupported target' >&2; exit 1 ;;
    esac
    export GOOS="$TARGET_GOOS" GOARCH="$TARGET_GOARCH" CGO_ENABLED=0
    output_dir="$project_dir/dist/$TARGET_GOOS-$TARGET_GOARCH"
fi
mkdir -p "$output_dir"
if [[ "$build_mode" == all ]]; then
    (cd "$engine_dir" && "$go_bin" build -tags with_gvisor -o "$output_dir/mihomo" .)
fi
(cd "$project_dir" && "$go_bin" build -ldflags "-X main.version=$nagi_version -X main.engineCommit=$commit" -o "$output_dir/nagi" ./cmd/nagi)
