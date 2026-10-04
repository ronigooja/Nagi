#!/usr/bin/env bash
set -euo pipefail

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
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

go_bin="${GO:-go}"
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
(cd "$engine_dir" && "$go_bin" build -o "$output_dir/mihomo" .)
(cd "$project_dir" && "$go_bin" build -ldflags "-X main.version=$nagi_version -X main.engineCommit=$commit" -o "$output_dir/nagi" ./cmd/nagi)
