#!/usr/bin/env bash
# Actual macOS TUN acceptance. Temporarily manages network-service DNS through
# the normal isolated helper; never installs a daemon or changes system trust.
set -euo pipefail
project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ "$(uname -s)" != Darwin || "$(id -u)" != 0 ]]; then
    echo 'Requires macOS root and explicit authorization for temporary network-service DNS and a documentation /32 route' >&2
    exit 2
fi
if [[ "${NAGI_CAPTURE_NATIVE_MACOS:-}" != 1 ]]; then
    echo 'Set NAGI_CAPTURE_NATIVE_MACOS=1 after authorizing temporary DNS management and scoped TUN routing' >&2
    exit 2
fi
if [[ -z "${NAGI_CAPTURE_NATIVE_ENGINE:-}" || ! -x "$NAGI_CAPTURE_NATIVE_ENGINE" ]]; then
    echo 'Set NAGI_CAPTURE_NATIVE_ENGINE to the reviewed engine executable' >&2
    exit 2
fi
if pgrep -x mihomo >/dev/null; then
    echo 'Another mihomo is running; refusing to change its network environment' >&2
    exit 2
fi
engine="$(cd "$(dirname "$NAGI_CAPTURE_NATIVE_ENGINE")" && pwd)/$(basename "$NAGI_CAPTURE_NATIVE_ENGINE")"
go_bin="${GO:-$(command -v go || true)}"
[[ -n "$go_bin" ]] || { echo 'Set GO=/path/to/go' >&2; exit 2; }
fixture_dir="$(mktemp -d /private/var/nagi-native-run.XXXXXX)"
trap 'rm -rf -- "$fixture_dir"' EXIT
"$go_bin" -C "$project_dir" test -c -o "$fixture_dir/native.test" ./internal/privileged/server
for rewrite in 0 1; do
    NAGI_CAPTURE_NATIVE_ENGINE="$engine" NAGI_CAPTURE_NATIVE_REWRITE="$rewrite" \
        "$fixture_dir/native.test" -test.run='^TestCaptureNativeRestrictedTUN$' \
        -test.v -test.timeout=120s
done
