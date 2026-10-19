# Request rewrite verification

This document records combined request rewrite software verification and the
remaining platform and release gates. It is an engineering acceptance record,
not a claim that the feature is available in a published release. Commands and
fields remain authoritative in the [CLI reference](../reference/cli.md#tun-request-rewrite);
contracts and limits remain in the [design](../design/request-rewrite.md).

## Verified source and environment

Verification on 2026-10-18 used Nagi source
`35ba854b0e8ef6f7620735038302a7a4431e6483`, including the terminal forms and macOS
UI source, and clean mihomo source
`d7c0a70776e4e698bee43f2044e7fa91d2d67d7b`. The host was Linux ARM64 with Go
1.26.4, Python 3 and Bash. Swift and macOS frameworks were unavailable; no tools
were downloaded for native acceptance.

The domain library, service, coordinator, helper interfaces, CLI and terminal
forms are implemented and Linux-verified. The macOS menu/window source was
independently reviewed; its 27 Swift test fixtures were written but **not run**.
Swift compilation, tests, packaging and native interactions remain unverified.
T8 local software verification passed; complete platform and release acceptance
has not passed.

`engine.lock` remains at `9a449c5797f7c855067f15be69fa659d13f6ce88`, which lacks
request rewrite. The reviewed development engine has not been published or
verified obtainable from the declared remote. Ordinary installation and
`nagi update` therefore do not supply this capability. Local development needs
a separately built matching engine selected with `NAGI_MIHOMO_BIN`, following
[the development-pair instructions](build-and-test.md#request-rewrite-coordinator).
No push, publication, release archive or engine pin change was performed.

## Automated and interactive checks

The following checks passed against the combined source. Builds went to
throwaway locations; tests used isolated state rather than an installed Nagi
instance.

| Check | Evidence and scope |
| --- | --- |
| `make test` | Complete Go suite and `scripts/test-install.sh`; installer regression fixtures passed. This does not establish native macOS installation or power-loss recovery. |
| `go test -race ./internal/menu ./internal/command ./internal/requestrewrite ./internal/requestrewriteservice ./internal/privileged/...` | All affected packages passed, including CLI transactions, menu dispatch, child/control channels and helper policy. |
| `go vet ./...`; `go mod verify` | Passed; all module contents verified. |
| CLI cross-builds with `CGO_ENABLED=0` | Linux AMD64/ARM64 and Darwin AMD64/ARM64 passed. Darwin compilation is not Swift compilation or native runtime acceptance. |
| Engine `go test -race -tags with_gvisor ./tunnel ./hub/route ./component/configpolicy -run Rewrite -count=1` | Passed against the same clean engine commit during independent baseline verification; the subsequent Nagi UI changes did not modify that engine. |
| `scripts/test-request-rewrite-engine.sh` | Exact-source HTTP/TLS local origin roundtrips in Rule, Global and Direct passed. |
| `scripts/test-request-rewrite-coordinator.sh` | Ordinary-UID CLI/child/supervisor lifecycle and failure/recovery integration passed. |
| `NAGI_REWRITE_TEST_ENGINE=... go test -race ./internal/privileged/server -run '^TestRewriteRealRestrictedOwnerAndReload$' -count=1 -v` | Passed with a freshly built reviewed engine: restricted root startup, bound/unbound owner rejection, mTLS probe and reload. TUN remained disabled. |
| Real Linux PTY against the combined CLI | Domain CRUD and failed dependent-consent removal, ordered operation editing, rule add/edit/toggle/reorder/remove, destination consent and error policy, stdin/file preview, import/export/validate, public CA export and failure retention, repeated Ctrl-C and EOF before Submit passed. Exiting the menu left the fixture engine running; enable without actual TUN was rejected. |
| Documentation | Changed relative destinations/anchors, command paths, version identifiers, examples and `git diff --check` were reviewed. |

For reproducible automated integration commands and fixture prerequisites, use
[Build and test](build-and-test.md#private-request-rewrite-service). The PTY run
used the independent terminal review harness with fictitious forms and local
files; it was an interactive acceptance exercise rather than a new committed
test framework.

## Coverage matrix

“Software fixture” means the tested boundary below, not a real OS network
capture. The engine source fixture calls the real tunnel with synthetic TUN
metadata over `net.Pipe` and configured local hosts. The coordinator fixture
uses real engine APIs and authenticated channels with synthetic TUN state.
Neither creates an OS TUN, installs OS trust, changes routes or uses real
subscriptions.

| Requirement | Verified evidence | Remaining acceptance |
| --- | --- | --- |
| HTTP/HTTPS and changed destinations | Real engine/service local HTTP and TLS origins on nonstandard ports; Host, SNI, path/query, gzip JSON and exact large integers; all three routing modes. | Real TUN capture of HTTP/TLS on standard and nonstandard ports, IPv4/IPv6 and fake-IP across supported backends. |
| Identity, routing, DNS and ordinary UDP | Engine race fixtures preserve supplied process/UID/source/inbound metadata, select changed-target native rules and mode outbounds, exercise fake-IP with sniffer disabled, cancellation of native rule DNS and internal bypass/UDP classification. | Real application identity discovery and transit; real DNS, unrelated UDP and node/internal traffic remaining ordinary through OS capture. Synthetic metadata does not prove process lookup. |
| Transformations and persistence | Domain tests cover original matching, ordered cumulative operations, destination consent, JSON Pointer/number precision, gzip/8 MiB validation, rollback, private files and profile isolation; CLI/coordinator tests cover transactional live changes and failures. | Sustained whole-system traffic during edits and subscription/profile/backend transitions on native hosts. |
| TLS and application protocols | Service fixtures reject untrusted/wrong-host upstream TLS without forwarding and redact errors; HTTP/2 concurrency/authority/independent cancellation, HTTP/1 framing, streaming, truncated responses and buffered WebSocket first frames are tested. | Real certificate-pinned and client-certificate applications, production HTTP/2/WebSocket/stream compatibility and application-specific trust stores. Pinning/mTLS failures must remain visible; no success is inferred from ordinary TLS tests. |
| QUIC and hidden domains | Engine fixtures block recognized authorized QUIC, preserve unrelated/unknown UDP and skip internal traffic. | Real QUIC identification/fallback and hidden-domain coverage. Unknown UDP, ECH and unidentifiable traffic are not proof of interception; no HTTP/3 proxy is implemented. |
| Authentication and resource cleanup | Engine/service tests cover role/pin/validity checks, bad secrets, replay, epochs/generations, expiry, bounds, heartbeat loss, original abort, half-close, backpressure, pending DNS/dial deadlines and reservation release. | Sustained full-runtime CPU/RSS/file-descriptor/goroutine measurements across native capture, backend failures and suspend/resume. |
| Lifecycle and failure recovery | Coordinator integration covers persistent child/supervisor, repeated enable, rejected probe rollback, CAS cutover, child SIGKILL blocking and explicit recovery, isolated trust-root removal/restore, certificate renewal, engine death cleanup, reload, profile switch, TUN teardown and quit. Unit tests cover uncertain publication/disable and retained ownership. | Real TUN/backend/helper failures, native suspend/wake and OS lifecycle races. A synthetic teardown is not an OS interface teardown test. |
| Three entry points | CLI and terminal Go/race/PTY checks passed. App source/API/concurrency review and Swift fixtures cover CLI-only dispatch, serialized mutations, stale reads, drafts and quit coordination. | Run Swift tests/package and native menu/window/forms/keyboard/file-picker/minimize/hide/sleep/quit/error-recovery checks. Source review does not establish executable App behavior. |
| CA and sensitive data | Throwaway CA generation/public-only exports, private storage, sanitized ordinary output and temporary root-file assessment passed. Rule/request input uses stdin or private files; raw exports/previews are explicit sensitive operations. | Native login-keychain trust install/remove, application trust, authorization dialogs and separate Linux distribution/application trust instructions. No OS trust operation was performed in these checks. |
| Build, installation and release | Four CLI targets compile; Go installer regressions pass. | Native App/helper/service installation and matching published-engine locked build/release artifacts. Development overrides do not prove remote availability. |

Resource reservations are logical admission limits. In particular, the 64 MiB
workspace budget is **not** a whole-process heap/RSS ceiling. Earlier isolated
near-8-MiB JSON pressure evidence measured about 84 MB Go heap and 100 MiB RSS;
this is not a sustained full-runtime benchmark. Original TCP FIN can represent
a valid half-close, and EOF behind backpressure is not always immediately
observable. Pending engine DNS/dial work is bounded to five seconds; idle and
absolute context limits provide further cleanup, as specified in the design.

## Remaining host and release gates

On disposable, explicitly approved native hosts, repeat the matrix with a real
TUN and both user and privileged backends where supported. Record OS/architecture,
engine and Nagi commits, stack/address family/fake-IP settings, local origin
observations, actual process/routing identity, failure outcomes and retained
state after recovery. Include unrelated DNS/UDP traffic, domain-visible QUIC,
service/engine death, occupied ports/prepare failure, backend/profile changes,
suspend/wake and bounded resource measurements. Use only throwaway CA material,
private state and local fixture origins.

On macOS, run `swift test --package-path macos/NagiApp` and `make build app` first,
then exercise the [App request rewrite flow](../../macos/NagiApp/README.md#request-rewrite)
and native trust/helper/TUN lifecycle. Check all daily forms, failure-retained
drafts, sensitive stdin/export boundaries, window reuse and close preserving the
service, and App Quit cleanup plus failure recovery. Record an unavailable gate
as **unverified**, never as passed by cross-compilation or source inspection.

Before changing `engine.lock`, publish the reviewed engine commit to the declared
repository/ref and verify it from a fresh fetch. Then run the locked build and
ordinary startup/capability checks, inspect matching release artifacts and follow
[the release gate](release.md#request-rewrite-release-gate). Publication is outside
the current authorized local work. These open host and release gates prevent a
claim of complete end-to-end acceptance.
