# Content rewrite verification

This record separates v2 software evidence from native platform acceptance and
release availability. The [design](../design/request-rewrite.md) defines behavior;
the [CLI reference](../reference/cli.md#tun-request-rewrite) defines public syntax.
All fixtures use temporary private state, fictitious names and local origins.
The original v2 software checks changed no real OS TUN, system CA trust or
installed user service. Later capture checks include explicit native TUN/DNS
transactions and isolated CA trust, with restoration evidence recorded below.

## Source and environment

The 2026-10-18 verification host is Linux ARM64 with Go 1.26.4, Python 3 and Bash.
The original v2 implementing mihomo commit was
`84369733b97b897a04a70e507183eda22e38e0c6`, published on
`https://github.com/ronigooja/mihomo.git`, ref `main`, and recorded
in `engine.lock` at that stage. The v2 commit was fast-forwarded into `main`; a subsequent
fresh fetch returned the identical locked commit and verified source availability.
Nagi release binaries have not been published for this change.

The Nagi v2 configuration, immutable request/response/message pipeline, fixed
QuickJS/WASM runner, CLI and terminal menu have software tests. The macOS source
uses the CLI interface. Swift/framework tooling is unavailable on this Linux host;
later native compilation and automated-test evidence is recorded in the capture
section below together with App packaging, isolated keychain trust and actual
TUN/helper checks. Manual App interaction remains **unverified**. Cross-compilation
is only Go build evidence.

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
platform acceptance. The initial capture engine source used for software checks was
`da88dc0d710f78a659bc36251b2ab18310aa4ebd`; the final macOS TUN checks use
`a5df21a6a339a651bb50dca73b30f7679dbd0436`, which also supplies original Darwin
socket UID attribution. Installation/update availability still
requires a matching Nagi and engine release. This commit is published on the
declared fork's `main` branch after a fast-forward on 2026-10-19. It was initially
published on `nagi/request-capture`. A fresh depth-one fetch of that feature branch confirmed
the exact commit before advancing `engine.lock`; the default locked build without
`MIHOMO_DIR` passed and reported this commit as `mihomo_commit`. On 2026-10-19,
a fresh fetch of remote `main` confirmed the same commit; the default locked
Linux ARM64 build with Go 1.26.4 passed for mihomo (`with_gvisor`) and Nagi, and
`nagi --json version` reported the locked commit. This checks mainline source
availability and builds; it does not add native platform acceptance or publish
release artifacts.

| Layer | Evidence and limits |
| --- | --- |
| Terminal menu | `go test ./internal/menu` and `go test -race ./internal/menu` pass. Capture journeys cover private stdin scope, failed-form retention and EOF/cancel discard, observed/uncertain status and stop recovery, raw Authorization/Cookie/query/body display with minute timestamps and Enter pause, cursor batches and stale-read clearing, fixture/body access, archive allowlisting and versioned quick-rule save conflicts without automatic retry. |
| CLI/runtime | Recorder, service, capture draft and command tests exercise bounded original records and body states, streaming archives, strict JSON/scope, reconciliation and uncertain-session recovery. Runtime integration evidence is supplied by the ordinary-UID coordinator fixture described below. |
| Native App source | An isolated ARM64 macOS 14 checkout with Apple Swift 6.0.3 passed `swift test` (182 tests, zero failures) and `swift build -c release`. After the final RFC 6901 invalid-index correction, the focused 12-test capture-draft suite and release build passed again. Tests cover transport/stdin/raw values, window-owned session lifetime, cancellation, operation ordering, insertion/enable, exact matching, profile/version binding, incomplete originals, preview and save conflicts. These are automated source/model checks. |
| Outstanding native acceptance | Interactive App behavior, sleep/wake, application-specific certificate stores and sustained resource measurement still require native acceptance. Linux/macOS actual TUN/helper and macOS model/CLI/trust evidence are recorded below. |

The native source checks used an isolated development checkout and touched no
ordinary user services or traffic settings. The full test and final focused/release
logs were retained on the native development host as development artifacts;
those local paths are not runtime requirements.

For a reproducible capture coordinator check, use the current engine revision
from `engine.lock` with the repository harness:

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

The same harness's live variant passed after the user authorized its isolated CA
through a local Terminal prompt: native raw-field selection, editable draft,
offline preview, atomic save, CLI activation and an actual local HTTP origin
observing the changed header. The generation transition retained existing capture
history. The engine input remains synthetic TUN; this is not OS TUN evidence.
The harness removed that CA's trust and certificate after the successful run;
fresh checks found its fingerprint absent from both the login keychain and user
trust settings. Its CLI, recorder, rewrite child, supervisor and fixture engine
were stopped, and its private files were removed.

The initial SSH trust operation failed with `SecTrustSettingsSetTrustSettings`
because interactive authorization was unavailable; launching the exact command
in local Terminal and the user's fingerprint authorization succeeded. A first
live attempt used a fixture HOME too long for Darwin's Unix socket limit; it
failed activation and removed that CA. The harness now checks socket length
before activation, uses short canonical private paths and retains an explicitly
supplied fixture after failure for retry with the same CA. System Events reported
UI automation disabled, so interactive menu-bar acceptance remains pending.
No accessibility permission was changed by these checks.

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

### Actual macOS TUN and root helper

On ARM64 macOS 14.8.9 with engine
`a5df21a6a339a651bb50dca73b30f7679dbd0436`, the portable helper fixture passed
capture-only and rewrite-coexistence variants through an actual `utun` interface
(2.24 seconds and 2.01 seconds respectively). It uses the same
restricted root-engine/helper boundary and ordinary UID 65534 recorder, rewrite
service and local HTTP/HTTPS/WebSocket origins as the Linux test. An executable
inside a fictitious `.app/Contents/Helpers` directory verifies exact executable
attribution and outermost bundle inclusion/exclusion. HTTPS uses a private Go
fallback certificate pool in the child process; it neither installs a CA nor
changes system trust.

The normal helper TUN lifecycle temporarily manages macOS network-service DNS.
Before startup, the test snapshots every service's static DNS list or DHCP state,
the default route and the documentation destination route. It preserves the
existing resolver as mihomo's DNS upstream, uses local hosts for fixture traffic,
a distinct `198.19.255.1/24` fake-IP range and only `198.51.100.1/32` routing.
During activation, it asserts that the documentation /32 enters `utun`, the
default route is unchanged and helper DNS has no ownership conflicts. The socket
uses root-controlled `/private/var`; `/private/var/run` is group writable on this
host and correctly rejected by the helper's ancestor check.

HTTP/HTTPS original credentials and bodies, both WebSocket directions, original
socket UID 65534, exact process and bundle matching, capture revisions, pause and
stop are verified. The coexistence variant additionally checks rewriting while capture is excluded,
paused or stopped. Explicit helper stop and deferred close restore DNS through
the production journal, which preserves external changes rather than overwriting
them. After successful and failed attempts, the test checks exact static/DHCP
DNS for all five network services and original/default routes. A failed
restoration retains its private root journal for recovery. Test processes and temporary state are removed.

Reproduce only after authorizing the temporary DNS transaction and scoped route;
the script refuses an already running mihomo rather than interrupting it:

```sh
sudo env NAGI_CAPTURE_NATIVE_MACOS=1 \
  NAGI_CAPTURE_NATIVE_ENGINE=/path/to/reviewed-mihomo GO=/path/to/go \
  bash scripts/test-request-capture-native-macos.sh
```

These checks use an in-process helper server and its actual authenticated Unix
socket, not an installed LaunchDaemon. They do not replace full CLI supervisor
coverage, interactive App acceptance, sleep/wake or sustained resource checks.

Both older rewrite engine and coordinator scripts passed again against the
published capture engine source. A preceding rerun failed because the build
volume had no space available to its ordinary UID; the serial rerun after removal
of old regenerable build-cache entries passed without source changes.

### Final locked-source regression

With `engine.lock` at `a5df21a6a339a651bb50dca73b30f7679dbd0436`, a fresh
normal `./scripts/build.sh` fetch/build without `MIHOMO_DIR` passed and the CLI
reported that exact `mihomo_commit`. The capture coordinator, older rewrite
engine/coordinator harnesses and both actual Linux TUN/helper variants passed
again. Focused race checks for capture, service, drafts, command and menu passed,
as did `go vet ./...`, installer integration, shell syntax, Markdown relative
paths/anchors and `git diff --check`.

The complete final `go test -p 1 ./...` passed. An initial parallel run failed
the existing detached-updater lock timing assertion and the 8 MiB guest sample's
one-second deadline (1.03 seconds). Each case then passed five isolated repeats;
no cause was established and no production timeout was changed. A prior native
fixture development run also saw an HTTP/2 flow `INTERNAL_ERROR`; ten focused
repeats and the subsequent full suite passed without a source change.

### Sustained recorder resources (local service)

The opt-in resource fixtures exercise the actual capture adapter, authenticated
loopback handoff and private Unix recorder service in one Go test process. They
use a local HTTP/WebSocket origin and fictitious content; they do not change host
routes, install software, use TUN or establish native App/sleep acceptance. Heap,
RSS, CPU, descriptors and goroutines therefore include the adapter, recorder,
origin and test harness, rather than measuring a standalone recorder process.

Reproduce the sustained phase separately from concurrent build/race workloads:

```sh
NAGI_CAPTURE_RESOURCE_DURATION=3m go test ./internal/requestrewriteservice \
  -run '^TestCaptureSustainedResources$' -count=1 -timeout=5m -v
NAGI_CAPTURE_RESOURCE_DURATION=3m go test -race \
  ./internal/capture ./internal/requestrewriteservice \
  -run '^TestCaptureResource(Adversaries|DiscardedBytes)$' -count=1 -v
```

The sustained duration must be at least three minutes. Ordinary `go test` skips
these opt-in tests. Two HTTP workers rotate 256 KiB responses alongside one
continuous HTTP response and one bidirectional WebSocket connection, with every
forwarded byte checked against its original content. The first minute has no UI
queries; later, slow original-content searches follow cursors concurrently with
writes and evictions. Fifteen-second samples collect recorder status, heap after
GC, Linux RSS/descriptors, goroutines and cumulative process CPU. Regression
checks compare middle/final peak windows for continued heap, goroutine and
file-descriptor growth; they are not physical-memory ceilings.

Recorded on **2026-10-19 07:15 UTC**, Linux 6.1.0-50 ARM64 with Go 1.26.4:
two isolated three-minute runs passed. The final run used the complete HTTP/1
deadline/closure fix (`cfa5fb1`) and recorded:

| Measurement | Final 180-second run |
| --- | --- |
| Forwarding checks | 4,498 HTTP responses, 899 bidirectional WebSocket round trips, one continuous 14,741,504-byte HTTP response; original bytes unchanged. |
| Concurrent reads | 171 successful search/cursor operations after the no-UI phase. |
| Recorder retention | 168–170 records; 55.76–55.92 MiB logical allocated content/metadata; 6,128 quota evictions. |
| Whole test-process heap after GC | 65.25–67.26 MiB across twelve samples. |
| Whole test-process RSS | 154.13–157.14 MiB; no continuing duration-related growth after saturation. |
| Active sampled goroutines / descriptors | 44–53 / 26–31; final cancellation sample dropped to 32 / 24. |
| Cumulative process CPU | 82.09 CPU seconds over 180 seconds, including all local fixture endpoints. |

The middle/final peak checks passed, as did final capture/service package race
tests and `go vet`. The earlier isolated pass forwarded the same HTTP/long-stream
counts and 899 WebSocket round trips, with 171 successful search/cursor reads.

The finite adversarial phases also passed with the race detector:

- One-byte events retained 65,536 body bytes in five 16 KiB allocations (81,920
  bytes); a maximum-size 262,144-byte event used seventeen blocks (278,528 bytes).
  Fixed-block slack and allocation overhead are counted in the recorder budget.
- Repeated completed 1 MiB records reached 58,600,709 logical bytes and evicted
  45 records, retaining 55, with the ingress reserve intact.
- Exactly 1,000 active WebSocket parents remained bounded while 1,000 incoming
  records were rejected. Closing a parent made it evictable and admitted the next
  record.
- A stalled recorder retained 64 queued events and one delivery worker, with
  705,678 serialized bytes including its in-flight event and 10,064 rejected
  offers. Offering events added four transport goroutines rather than one waiter
  per offer; cancellation returned the shared queue accounting to zero.
- A real capture body read one byte at a time forwarded all 65,536 original bytes
  while retaining only 65 bytes after overload. Its recorded state was `cutoff`,
  preserving the distinction from a complete body.

The first sustained attempt exposed a reproducible HTTP/1 gate defect: waiting
for a subsequent request header could cancel a still-active response after one
minute. The gate now waits for response completion before starting the next
header-idle clock, clears the header deadline before streaming, and releases its
wait after connection closure. Focused tests cover a cancellation-sensitive long
response, pipelining, slow upload, upgraded traffic, incomplete-header timeout,
idle closure, cancellation, TCP half-close and server rejection. The long-response
regression failed against the preceding implementation. A separate retry during
concurrent package/race checks ended with an unlabelled EOF after 43 seconds;
its cause was not established. Subsequent isolated sustained runs are recorded
below rather than attributing that failure to an assumed cause.

The 64 MiB contract bounds logical retained content/event buffers, with the store
reserving space for ingress and transport. It does not cap process heap or RSS;
Go allocator/GC behavior, metadata, TLS, HTTP and the test harness add memory.
These local service checks do not replace sustained measurement on native hosts
with actual TUN, helper and App workloads.
