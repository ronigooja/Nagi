#!/usr/bin/env bash
# Actual OS TUN acceptance in a disposable network namespace. Never installs
# a helper or CA, changes host routes, or opens ordinary Nagi state.
set -euo pipefail
project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ "$(uname -s)" != Linux || "$(id -u)" != 0 ]]; then
    echo 'Requires Linux root with a usable /dev/net/tun and network namespace privileges' >&2
    exit 2
fi
if [[ -z "${NAGI_CAPTURE_NATIVE_ENGINE:-}" || ! -x "$NAGI_CAPTURE_NATIVE_ENGINE" ]]; then
    echo 'Set NAGI_CAPTURE_NATIVE_ENGINE to the reviewed engine executable' >&2
    exit 2
fi
engine="$(cd "$(dirname "$NAGI_CAPTURE_NATIVE_ENGINE")" && pwd)/$(basename "$NAGI_CAPTURE_NATIVE_ENGINE")"
[[ -c /dev/net/tun ]] || { echo '/dev/net/tun is unavailable' >&2; exit 2; }
command -v unshare >/dev/null
command -v ip >/dev/null
# Refuse before building anything when namespace creation is unavailable.
unshare --net true
go_bin="${GO:-$(command -v go || true)}"
if [[ -z "$go_bin" ]]; then
    for candidate in "$HOME"/environment/go*/bin/go /usr/local/go/bin/go; do
        if [[ -x "$candidate" ]]; then go_bin="$candidate"; break; fi
    done
fi
[[ -n "$go_bin" ]] || { echo 'Set GO=/path/to/go' >&2; exit 2; }
fixture_dir="$(mktemp -d "${TMPDIR:-/tmp}/nagi-native-run.XXXXXX")"
trap 'rm -rf -- "$fixture_dir"' EXIT
"$go_bin" -C "$project_dir" test -c -o "$fixture_dir/native.test" ./internal/privileged/server
for rewrite in 0 1; do
    unshare --net bash -c '
        set -euo pipefail
        ip link set lo up
        export NAGI_CAPTURE_NATIVE_ENGINE="$1" NAGI_CAPTURE_NATIVE_REWRITE="$2"
        "$3" -test.run="^TestCaptureNativeRestrictedTUN$" -test.v -test.timeout=120s
    ' fixture "$engine" "$rewrite" "$fixture_dir/native.test"
done
