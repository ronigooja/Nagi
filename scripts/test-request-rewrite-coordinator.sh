#!/usr/bin/env bash
# Source fixture only: no OS TUN, subscriptions, keychain or installed engine.
set -euo pipefail
project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
expected_engine=84369733b97b897a04a70e507183eda22e38e0c6
if [[ -z "${MIHOMO_DIR:-}" ]]; then
    echo 'Set MIHOMO_DIR to the committed request-rewrite engine checkout' >&2
    exit 2
fi
engine_dir="$(cd "$MIHOMO_DIR" && pwd)"
if [[ "$(git -C "$engine_dir" rev-parse HEAD)" != "$expected_engine" ]] || [[ -n "$(git -C "$engine_dir" status --porcelain --untracked-files=no)" ]]; then
    echo 'Fixture requires the reviewed engine commit and a clean tracked tree' >&2
    exit 1
fi
go_bin="${GO:-$(command -v go || true)}"
if [[ -z "$go_bin" ]]; then
    for candidate in "$HOME"/environment/go*/bin/go /opt/homebrew/bin/go /usr/local/go/bin/go; do
        if [[ -x "$candidate" ]]; then go_bin="$candidate"; break; fi
    done
fi
if [[ -z "$go_bin" ]]; then echo 'Set GO=/path/to/go' >&2; exit 1; fi
fixture_dir="$(mktemp -d "${TMPDIR:-/tmp}/nagi-rewrite-coordinator.XXXXXX")"
trap 'rm -rf -- "$fixture_dir"' EXIT
# Ordinary-UID fixture children must be able to traverse the artifact directory.
chmod 0755 "$fixture_dir"
cp "$engine_dir/go.mod" "$engine_dir/go.sum" "$fixture_dir/"
cp "$project_dir/internal/command/testdata/rewrite-engine/main.go" "$fixture_dir/main.go"
export GOWORK=off
"$go_bin" -C "$fixture_dir" mod edit -module=nagi-coordinator-engine-fixture -go=1.24.0 \
    -require=github.com/metacubex/mihomo@v0.0.0 "-replace=github.com/metacubex/mihomo=$engine_dir"
"$go_bin" -C "$fixture_dir" build -mod=mod -tags with_gvisor -o "$fixture_dir/engine" .
"$go_bin" -C "$project_dir" build -o "$fixture_dir/nagi" ./cmd/nagi
python3 "$project_dir/scripts/test-request-rewrite-coordinator.py" "$fixture_dir/nagi" "$fixture_dir/engine"
