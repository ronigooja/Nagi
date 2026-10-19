# Content rewrite verification

This record separates v2 software evidence from native platform acceptance and
release availability. The [design](../design/request-rewrite.md) defines behavior;
the [CLI reference](../reference/cli.md#tun-request-rewrite) defines public syntax.
All fixtures use temporary private state, fictitious names and local origins.
No real OS TUN, system CA trust or installed user service was changed.

## Source and environment

The 2026-10-18 verification host is Linux ARM64 with Go 1.26.4, Python 3 and Bash.
The implementing mihomo commit is
`84369733b97b897a04a70e507183eda22e38e0c6`, published on
`https://github.com/ronigooja/mihomo.git`, ref `main`, and recorded
in `engine.lock`. The v2 commit was fast-forwarded into `main`; a subsequent
fresh fetch returned the identical locked commit and verified source availability.
Nagi release binaries have not been published for this change.

The Nagi v2 configuration, immutable request/response/message pipeline, fixed
QuickJS/WASM runner, CLI and terminal menu have software tests. The macOS source
uses the CLI interface; Swift/framework tooling is unavailable on this host.
Native Swift compilation/tests, packaging, keychain trust, UI and TUN/backend
acceptance remain **unverified**. Cross-compilation is only Go build evidence.

## Verification ledger

| Check | Evidence |
| --- | --- |
| Shared transformations | Unit tests cover rule-derived intercept/target sets, original matching, stage order and rollback, final target permission for built-in/script operations, exact JSON numbers, pointer traversal and response body restrictions. |
| Body/Range utilities | Unit/race checks cover identity/gzip/zlib/Brotli/Zstandard and three layers, charset losslessness, RE2 captures, single/multipart equal-length fragments, cancellation, output limits and EOF trailers. |
| Script isolation | Real guest tests cover an 8 MiB binary base64 roundtrip under the one-second production deadline, timeout, cancellation, allocation exhaustion, invalid output, deterministic fresh instances, absent host capabilities and two-instance admission. The throughput case is explicitly skipped under race instrumentation; other isolation cases run under race. |
| Guest reproducibility | Pinned QuickJS and WASI SDK input checksums, runner checksum, WASM checksum and licenses are tracked. A second temporary-directory build reproduced the artifact. See [guest reproduction](rewrite-script-guest.md). |
| CLI/menu/output | Unit/race/vet checks cover scripts CRUD/reference protection/source sensitivity, rules, current invalid input, five staged preview kinds, complete JSON drafts, completion and output. |
| Engine | `go test ./...` passed, including the existing long-running inbound suite; rewrite-focused race checks and Linux/Darwin AMD64/ARM64 builds passed. |
| Publication | Fast-forward push to engine `main` and subsequent fresh fetch returned the identical locked commit. |

## Combined-source verification (2026-10-18)

The integrated source passed the following checks on Linux ARM64. The complete
race suite passed before final review fixes; all five affected packages passed
race again after those fixes. Native platform gates remain separate below.

| Command or fixture | Result |
| --- | --- |
| `make test GO=/path/to/go` | Passed, including all Go packages and installer integration. |
| `go test -race ./...` | Passed. |
| `go test -race ./internal/requestrewrite ./internal/requestrewriteservice ./internal/command ./internal/menu ./internal/rewritescript` | Passed on the final combined code, including final Range length, trailer invalidation, UTF-8 close, handshake operations, field limits, fallback counters and failure-policy ownership. |
| `go vet ./...`, `go mod verify` | Passed; direct dependencies and checksums normalized with `go mod tidy`. |
| `go test ./internal/requestrewrite -run '^$' -fuzz '^FuzzDocument$' -fuzztime=10s` | Passed: 378,594 executions. |
| `go test ./internal/requestrewrite -run '^$' -fuzz '^FuzzBodyEncodings$' -fuzztime=10s` | Passed: 35,354 executions. |
| `scripts/test-request-rewrite-engine.sh` with the clean locked checkout | Passed HTTP and HTTPS in Rule/Global/Direct modes, including request targets/gzip/exact numbers, response original matching, actual isolated scripts, gzip response bodies and EOF trailers. |
| `scripts/test-request-rewrite-coordinator.sh` with the clean locked checkout | Passed ordinary-UID child/supervisor, CAS rollback/cutover, crash blocking/recovery, trust refresh, certificate renewal, engine-exit/reload, idle, TUN teardown and quit fixtures. |
| `NAGI_REWRITE_TEST_ENGINE=/path/to/locked/mihomo go test ./internal/privileged/server -run Rewrite -count=1 -v` | Passed the real restricted engine owner/reload fixture and strict helper schemas. |
| `scripts/build.sh` without `MIHOMO_DIR` | Passed fresh remote ref fetch, locked engine build and CLI build. |
| CLI `scripts/build.sh cli` for Linux/Darwin AMD64/ARM64 | Passed all four targets. |
| Offline `scripts/build-rewrite-script-guest.sh` plus `go test ./internal/rewritescript -count=1` | Passed reproduction of the final read-only original-metadata guest and real 8 MiB throughput test. |
| Changed Markdown relative links/anchors, shell syntax and `git diff --check` | Passed. |

One combined test run performed concurrently with race checks failed the existing
`TestCancelledAuthorizationKillsDescendantsAndAllowsRetry` maintenance-lock
assertion. Its isolated five-run repeat and the subsequent serial complete
`make test` passed. The cause of that single failure was not established; no
unrelated installer behavior was changed.

Service fixtures cover HTTP/1.1 and HTTP/2 response bodies/trailers, compressed
WebSocket negotiation in both directions, preserved first-frame bytes, oversized
edited messages without partial forwarding, streaming messages above 8 MiB,
invalid UTF-8 close 1007, and executed request-handshake operations. These are
local software fixtures, not native TUN or public-network acceptance.

## Native and release gates

Run on native macOS: `swift test --package-path macos/NagiApp`, `make build app`,
and the [App acceptance flow](../../macos/NagiApp/README.md#request-rewrite).
Exercise rule/script forms, all stage previews, keyboard cancellation, retained
drafts after failures, sensitive file/stdin flows, window close, quit, sleep/wake,
trust installation/removal and helper authorization. Record actual OS/version,
backend and retained state after each failure.

On supported native hosts, verify actual TUN capture, source/process identity,
fake-IP/address-family behavior, Rule/Global/Direct, unrelated DNS/UDP, visible
QUIC fallback and system lifecycle recovery. Synthetic TUN metadata and private
mTLS fixtures prove software boundaries, not OS route installation. Certificate
pinning/client certificates and unidentifiable/ECH traffic remain coverage limits.

Measure sustained host heap/RSS, CPU, descriptors and goroutines separately from
the 64 MiB logical conversion workspace and 128 MiB per-instance WASM linear
memory. Those budgets are not a whole-process memory ceiling.

Before advertising installation/update availability, build and inspect matching
Nagi release artifacts using the [release gate](release.md#request-rewrite-release-gate).
No Nagi tag, release archive or GitHub Release is created by this implementation.
