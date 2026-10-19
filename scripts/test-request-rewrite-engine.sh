#!/usr/bin/env bash
# Isolated opt-in source fixture; never starts an installed engine or OS TUN.
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
if [[ -z "$go_bin" ]]; then echo 'Set GO=/path/to/go (Go 1.24.0 or newer)' >&2; exit 1; fi
fixture_dir="$(mktemp -d "${TMPDIR:-/tmp}/nagi-rewrite-engine.XXXXXX")"
trap 'rm -rf -- "$fixture_dir"' EXIT
chmod 0755 "$fixture_dir"
cp "$engine_dir/go.mod" "$engine_dir/go.sum" "$fixture_dir/"
cp "$project_dir/internal/requestrewriteservice/testdata/engine-fixture/main.go" "$fixture_dir/main.go"
export GOWORK=off
"$go_bin" -C "$fixture_dir" mod edit \
    -module=github.com/ronigooja/Nagi/internal/requestrewriteservice/enginefixture \
    -go=1.24.0 \
    -require=github.com/ronigooja/Nagi@v0.0.0 \
    -require=github.com/metacubex/mihomo@v0.0.0 \
    "-replace=github.com/ronigooja/Nagi=$project_dir" \
    "-replace=github.com/metacubex/mihomo=$engine_dir"
"$go_bin" -C "$fixture_dir" build -mod=mod -tags with_gvisor -o "$fixture_dir/fixture" .
python3 - "$fixture_dir/fixture" <<'PYTHON'
import os, sys
if os.geteuid() == 0:
    os.setgroups([])
    os.setgid(65534)
    os.setuid(65534)
os.execv(sys.argv[1], [sys.argv[1]])
PYTHON
