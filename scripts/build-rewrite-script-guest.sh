#!/usr/bin/env bash
# Reproduce the checked-in guest on Linux ARM64. Uses only pinned inputs.
set -euo pipefail
repo_dir=$(cd "$(dirname "$0")/.." && pwd)
build_dir=$(mktemp -d)
trap 'rm -rf "$build_dir"' EXIT
source_url=https://bellard.org/quickjs/quickjs-2025-09-13-2.tar.xz
sdk_url=https://github.com/WebAssembly/wasi-sdk/releases/download/wasi-sdk-27/wasi-sdk-27.0-arm64-linux.tar.gz
source_sha=996c6b5018fc955ad4d06426d0e9cb713685a00c825aa5c0418bd53f7df8b0b4
sdk_sha=4cf4c553c4640e63e780442146f87d83fdff5737f988c06a6e3b2f0228e37665
runner_sha=afea689dc3d877a4cb5a994152be5658d62da32515fb021f60bbf8f0decce882
artifact_sha=9a3d9c3eacae5405085730c1fe00fa2cf94bea0d29d740c844bfcfb4703fae27
if [[ $(uname -s) != Linux || $(uname -m) != aarch64 ]]; then
  echo 'This pinned reproduction toolchain requires Linux ARM64.' >&2
  exit 1
fi
if [[ -n ${NAGI_QUICKJS_ARCHIVE:-} ]]; then
  cp "$NAGI_QUICKJS_ARCHIVE" "$build_dir/quickjs.tar.xz"
else
  curl -fsSL "$source_url" -o "$build_dir/quickjs.tar.xz"
fi
if [[ -n ${NAGI_WASI_SDK_ARCHIVE:-} ]]; then
  cp "$NAGI_WASI_SDK_ARCHIVE" "$build_dir/sdk.tar.gz"
else
  curl -fsSL "$sdk_url" -o "$build_dir/sdk.tar.gz"
fi
printf '%s  %s\n' "$source_sha" "$build_dir/quickjs.tar.xz" "$sdk_sha" "$build_dir/sdk.tar.gz" "$runner_sha" "$repo_dir/internal/rewritescript/guest/runner.c" | sha256sum -c -
tar -xf "$build_dir/quickjs.tar.xz" -C "$build_dir"
tar -xf "$build_dir/sdk.tar.gz" -C "$build_dir"
source_dir="$build_dir/quickjs-2025-09-13"
# dtoa does not use setjmp/longjmp; remove its unused header, which otherwise
# requires WASM exception handling unsupported by wazero. No semantic patch.
sed -i '/#include <setjmp.h>/d' "$source_dir/dtoa.c"
# WASI exposes malloc_usable_size in malloc.h; QuickJS includes that header only
# on selected native targets. Force-include it without changing source behavior.
"$build_dir/wasi-sdk-27.0-arm64-linux/bin/clang" \
  -O2 -DNDEBUG -D_GNU_SOURCE '-DCONFIG_VERSION="2025-09-13"' \
  -include malloc.h -I"$source_dir" -mexec-model=reactor \
  -Wl,--strip-all -Wl,-z,stack-size=1048576 -Wl,--max-memory=134217728 \
  -Wl,--export=malloc -Wl,--export=transform_json -Wl,--export=result_size \
  "$repo_dir/internal/rewritescript/guest/runner.c" \
  "$source_dir/quickjs.c" "$source_dir/dtoa.c" "$source_dir/libregexp.c" \
  "$source_dir/libunicode.c" "$source_dir/cutils.c" -lm -o "$build_dir/quickjs.wasm"
printf '%s  %s\n' "$artifact_sha" "$build_dir/quickjs.wasm" | sha256sum -c -
install -m 0644 "$build_dir/quickjs.wasm" "$repo_dir/internal/rewritescript/guest/quickjs.wasm"
