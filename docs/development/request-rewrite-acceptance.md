# Content rewrite verification

This record separates v2 software evidence from native platform acceptance and
release availability. The [design](../design/request-rewrite.md) defines behavior;
the [CLI reference](../reference/cli.md#tun-request-rewrite) defines public syntax.
All fixtures use temporary private state, fictitious names and local origins.
No real OS TUN, system CA trust or installed user service was changed. Capture
evidence and native source verification added later are recorded separately below.

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
uses the CLI interface. Swift/framework tooling is unavailable on this Linux host;
later native compilation and automated-test evidence is recorded in the capture
section below. Interactive UI, packaging for this capture change, keychain trust
and TUN/backend acceptance remain **unverified**. Cross-compilation is only Go
build evidence.

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


## Capture verification

This section records capture-specific evidence separately from the preceding v2
rewrite ledger. The current [capture design](../design/request-capture.md) and
[user guide](../guides/request-capture.md) distinguish implemented behavior from
platform acceptance. The capture engine source used for these checks is
`da88dc0d710f78a659bc36251b2ab18310aa4ebd`; installation/update availability still
requires a matching Nagi and engine release. This commit is published on the
declared fork's `nagi/request-capture` branch. A fresh depth-one fetch confirmed
the exact commit before advancing `engine.lock`; the default locked build without
`MIHOMO_DIR` passed and reported this commit as `mihomo_commit`.

| Layer | Evidence and limits |
| --- | --- |
| Terminal menu | `go test ./internal/menu` and `go test -race ./internal/menu` pass. Capture journeys cover private stdin scope, failed-form retention and EOF/cancel discard, observed/uncertain status and stop recovery, raw Authorization/Cookie/query/body display with minute timestamps and Enter pause, cursor batches and stale-read clearing, fixture/body access, archive allowlisting and versioned quick-rule save conflicts without automatic retry. |
| CLI/runtime | Recorder, service, capture draft and command tests exercise bounded original records and body states, streaming archives, strict JSON/scope, reconciliation and uncertain-session recovery. Runtime integration evidence is supplied by the ordinary-UID coordinator fixture described below. |
| Native App source | An isolated ARM64 macOS 14 checkout with Apple Swift 6.0.3 passed `swift test` (182 tests, zero failures) and `swift build -c release`. After the final RFC 6901 invalid-index correction, the focused 12-test capture-draft suite and release build passed again. Tests cover transport/stdin/raw values, window-owned session lifetime, cancellation, operation ordering, insertion/enable, exact matching, profile/version binding, incomplete originals, preview and save conflicts. These are automated source/model checks. |
| Outstanding native acceptance | macOS actual TUN/helper and app-bundle attribution, certificate trust and application stores, interactive App behavior, sleep/wake and sustained resource measurement still require native acceptance. Linux actual TUN/helper evidence is recorded below. |

The native source checks used an isolated development checkout and touched no
ordinary user services or traffic settings. The full test and final focused/release
logs were retained on the native development host as development artifacts;
those local paths are not runtime requirements.

For a reproducible capture coordinator check, use the capture engine revision
above with the repository harness:

```sh
MIHOMO_DIR=/path/to/clean/capture-engine GO=/path/to/go \
  bash scripts/test-request-capture-coordinator.sh
```

The harness runs the real CLI, child, recorder, controller, mTLS handoff and
reinjection as an isolated ordinary UID. It injects synthetic TUN connections
rather than creating an OS TUN device. Verified cases include zero-rule HTTP and
both WebSocket directions, original UID/process path and raw credentials/bodies,
pause/resume without a generation swap, suppression of globally disabled stored
rules, rule-save revision preparation and certificate-renewal history retention,
raw stdout export, rejection of engines without capture capability, TUN disable,
engine exit/restart without automatic resume and profile/quit teardown. This proves software coordination;
it does not prove OS route installation, root-helper policy or application trust.
Run the [native and release gates](#native-and-release-gates), adding App and
terminal capture start/scope/apps/list/show/body/clear/import/export/fixture and
quick-rule preview/save journeys, before claiming platform delivery complete.

### Integrated checks

The integrated capture/menu/App source, including fixed-length body completion,
bounded body allocation, live WebSocket parent retention and uncertain-state
recovery controls, passed `go test ./...`, installer integration, and focused
race checks for capture, requestrewriteservice, command and menu packages. The
actual-engine coordinator fixture above passed again on this combined source.

On ARM64 macOS 14.8.9 with Apple Swift 6.0.3, the integrated source passed all
185 Swift tests (one explicit opt-in CLI test skipped), release compilation and
`make build app`. An opt-in rerun of that skipped test against a real isolated
CLI and imported fictitious archive passed: raw Authorization field selection,
editable draft preparation, offline preview and atomic rule save through actual
subprocess stdin. This is native model/CLI evidence; it does not establish
interactive accessibility or live-network activation. Reproduce this offline
flow on macOS with matching CLI and source fixture binaries:

```sh
python3 scripts/test-request-capture-native-ui.py \
  /path/to/nagi /path/to/capture-engine-fixture macos/NagiApp --offline
```

The same harness's live variant explicitly adds a freshly generated isolated CA
to the current user's login keychain, then removes its trust/certificate and
verifies the fingerprint is absent during cleanup. Its initial SSH run was
blocked by `SecTrustSettingsSetTrustSettings`: no interactive authorization was
available. That run removed the temporary certificate and verified cleanup;
trust acceptance remains pending an authorized local session. System Events
reported UI automation disabled, so interactive menu-bar acceptance remains
pending. No accessibility permission was changed by these checks.

### Actual Linux TUN and root helper

The integrated `scripts/test-request-capture-native-linux.sh` passed both
capture-only and rewrite-coexistence variants in disposable network namespaces.
It refuses the initial network namespace, creates an actual OS TUN, and runs a
restricted root mihomo behind an isolated helper Unix socket. The helper checks
the ordinary child's kernel peer UID, the trusted binary and source allowlist;
the original source PID/path/UID is obtained from actual sockets. Only fictitious
local origins and documentation-address routes are used. The recorder and rewrite
service run as ordinary UID 65534. HTTPS trusts an isolated generated CA through
the child process's certificate roots, without changing the host trust store.

Verified cases: HTTP/HTTPS original credentials and bodies, both WebSocket
directions with retained HTTP parent records, exact process inclusion/exclusion,
unknown macOS bundle exclusion on Linux, revision updates, and paused capture.
The coexistence variant additionally proves that existing rewrite rules still
execute when capture is paused or its process scope excludes the request.
These checks do not exercise the complete CLI supervisor or installed daemon;
the separate coordinator fixture covers CLI lifecycle using synthetic TUN input.

```sh
NAGI_CAPTURE_NATIVE_ENGINE=/path/to/reviewed-mihomo GO=/path/to/go \
  bash scripts/test-request-capture-native-linux.sh
```

The portable helper fixture was also attempted on native macOS. Its temporary
socket must use root-controlled `/private/var`; `/private/var/run` is group
writable on this host and correctly fails the helper's ancestor check. With the
safe socket path, helper start reached the real TUN DNS prerequisite and rejected
the fixture's disabled DNS with `TUN DNS requires running mihomo DNS enabled`.
The macOS helper automatically manages network-service DNS during TUN startup;
this test did not enable DNS takeover of ordinary services. The fixture uses a
distinct `198.19.255.1/24` fake-IP range and only `198.51.100.1/32` routing. Its
processes and temporary state were removed after failure; the test destination
returned to the host's original default route. This is a recorded blocked
acceptance attempt, not a macOS TUN/helper pass.

Both older rewrite engine and coordinator scripts passed again against the
published capture engine source. A preceding rerun failed because the build
volume had no space available to its ordinary UID; the serial rerun after removal
of old regenerable build-cache entries passed without source changes.
