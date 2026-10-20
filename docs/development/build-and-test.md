# Build and test

This document describes development builds and verification for Nagi and the pinned mihomo source.

The manually dispatched [script throughput diagnostic workflow](../../.github/workflows/script-diagnostics.yml)
compares five isolated maximum-binary-sample runs with parallel and serial full
Go test suites on separate macOS runners. Artifacts retain environment details,
test logs, exit statuses, and an isolated CPU profile and execution trace for
seven days. It preserves test assertions and production deadlines; profiling
can itself affect timing. This diagnostic workflow does not publish releases.

The [October 2026 comparison](https://github.com/ronigooja/Nagi/actions/runs/39353178212)
on three-CPU, 7 GiB macOS runners passed five isolated 8 MiB samples
(852–931 ms) and the full serial suite (854 ms for the sample). The parallel
suite failed only that sample with `script_timeout` after 1.037 seconds.
The CPU profile from the preceding diagnostic run attributed most samples to
QuickJS/WASM execution, with little time in compilation. These observations
show sensitivity to concurrent load; they do not establish host throttling or
guarantee throughput on other machines. Release CI sets `GOFLAGS=-p=1` for
`make test` to reduce package competition while retaining all assertions and
the one-second production deadline.

Use Go 1.24.0 or newer. `engine.lock` records the mihomo repository, ref, and exact commit. `make build cli` creates `bin/nagi` without rebuilding mihomo and prints `Nagi CLI build succeeded: PATH` on success or `Nagi CLI build failed` on a compilation error. The CLI build uses Go from `PATH`, then checks `$HOME/environment/go*/bin/go`, `/opt/homebrew/bin/go`, and `/usr/local/go/bin/go`. This also works in macOS SSH sessions whose non-interactive `PATH` omits Homebrew. Set `GO=/path/to/go` to choose another executable. On macOS, `make build app` packages `bin/Nagi.app` through `macos/NagiApp/package-app.sh` and prints `Nagi App build succeeded: PATH` or `Nagi App build failed`. Run `./scripts/build.sh` for a full pinned mihomo and CLI build; it reports the result of each compilation. The mihomo build includes `with_gvisor`, which is required when a profile selects the gVisor TUN stack. Set `MIHOMO_DIR` to a checkout at the exact locked commit for local builds. Otherwise the script clones or updates `.build/mihomo` and checks out that commit. A mismatch fails the build.

```sh
make build cli
# On macOS:
swift test --package-path macos/NagiApp
make build app
# To rebuild the pinned mihomo as well:
GO=/path/to/go MIHOMO_DIR=/path/to/mihomo ./scripts/build.sh
GO=/path/to/go make test
```

For installation, updates, or removal, use `./scripts/installer.sh install`, `./scripts/installer.sh update`, or `./scripts/installer.sh uninstall` with artifacts already in `bin/`. Use `./scripts/sync_to_mac.sh` to copy the source tree to the macair checkout without copying local build artifacts. The script prints the destination when it starts and reports success or failure when it finishes. The [installation workflow](install.md) defines artifact layout, installer options, app installation, and cleanup.

Source sync also mirrors `.git`, replacing the destination's Git history,
refs, and index with those from the source checkout. Use the destination only
as a development mirror; changes made there can be overwritten by the next
sync. CLI and App builds therefore derive their versions from the same Git
state as the source checkout, including its `dirty` marker. After syncing,
rebuild with `make build cli` and `make build app`, then quit and reopen the
rebuilt App to see the updated About version.

`make test` runs the Go unit tests and the installer integration test with a temporary installation prefix. The CLI test package isolates its home and XDG directories and replaces inherited `NAGI_MIHOMO_BIN` with a nonexistent fixture path. Tests that require mihomo validation or lifecycle behavior must explicitly provide a fixture executable; ordinary CLI unit tests must not launch an installed engine. The installer test isolates its home and installation prefix. For a macOS test run, use a private checkout and set `HOME` to a temporary directory before invoking `make test`; this also isolates other test packages from the interactive user's Nagi files. Keep the checkout and `TMPDIR` paths short enough for Darwin's Unix socket path limit (for example, use a private directory under `/private/tmp` and `TMPDIR=/private/tmp`). These paths are for tests only; macOS runtime configuration remains under `~/Library/Application Support/Nagi`.

To check runtime integration manually with the built mihomo executable, run these commands from the repository root in order:

```sh
./bin/nagi start
./bin/nagi --json status
./bin/nagi config validate
./bin/nagi proxy groups
./bin/nagi connections list
./bin/nagi stop
```

`start` creates the default profile if none exists. TUN is disabled in that profile, so this integration check does not alter routes unless you explicitly run `nagi tun enable`; enabling it requires suitable permissions and, on macOS, may require the privileged helper. Expect `status` to report a running process, `config validate` to report a valid profile, and the proxy and connection commands to succeed; an empty connection list is valid. Inspect `nagi tun status` and `nagi logs` after explicitly enabling TUN. On Linux, set `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, and `XDG_RUNTIME_DIR` to directories in a temporary test location before running the commands if you need to isolate them from a normal Nagi instance. `XDG_RUNTIME_DIR` must be an absolute path. On macOS, Nagi uses `~/Library/Application Support/Nagi` rather than XDG paths. The macOS app is under `macos/NagiApp`; its [packaging script](../../macos/NagiApp/README.md) builds with Swift Package Manager and creates an `LSUIElement` bundle in `bin/`. Signing and notarization require an Apple developer identity and are not part of the local build.

For Connections changes, run the Swift tests and package on macOS. Verify a repeated **Connections…** selection brings the same window forward, table selection follows IDs during sorting and snapshot replacement, details and cumulative columns remain readable at the minimum window size, and close-all confirmation includes connections outside the current search. Check read cancellation on close, minimize, hide, sleep, and quit; a failed quit must restore reading. Use an injected CLI and private temporary checkout for fixtures, preserving the user's installed App and runtime. Run sustained near-limit snapshots while searching, scrolling, and resizing, and inspect CPU and memory. Linux tests and source inspection do not replace native macOS window acceptance. Restricted desktop sessions can decline native minimization and prevent screen capture; exercise the hide fallback and its cancellation, and distinguish cached AppKit view rendering from a full desktop screenshot. The [window design](../design/connections-window.md) defines the state and operation contract.

The tag-triggered [release workflow](../../.github/workflows/release.yml) runs release checks before publishing unsigned release archives.

`make release` cross compiles Nagi and mihomo into `dist/linux-amd64`, `dist/linux-arm64`, `dist/darwin-amd64`, and `dist/darwin-arm64`, then creates the fixed-name archives and `dist/SHA256SUMS` consumed by the online installer. On macOS it also uses `lipo` to make `dist/darwin-universal`. Set `APPLE_SIGN_IDENTITY` to sign both universal executables only when signing is desired; signing is not required for release packaging. With a configured notarytool keychain profile, set `APPLE_NOTARY_PROFILE` to zip and notarize them only when that distribution path is desired. Run `make build app` before `make release` to include the unsigned App in the archive matching the host architecture. Versioning, tag checks, artifact review, and release notes are defined in [Release process](release.md).

The current lock points at a commit available on the configured fork's `main` branch and includes the `--check-json`, restricted configuration policy, activation ID, age decryption, `dns.resolve-proxy-destinations`, destination rule testing, restricted Tailscale state/lifecycle, request-rewrite v2 and capture protocol 1 interfaces used by Nagi. Follow the [engine maintenance workflow](engine-maintenance.md) to integrate changes into the fork's `main`, publish and verify the source, and update the lock. A successful build with `MIHOMO_DIR` alone does not establish remote availability.

## Rule test acceptance

Run `go test ./...` in Nagi and `go test -race -tags with_gvisor ./tunnel ./hub/route -run 'TestRule(API|TestEndpoint)'` in the matching engine checkout. Command tests verify runtime-independent help and invalid input, text/JSON output, default metadata, old-engine errors, and exclusion from configuration transactions. Menu tests cover numeric navigation, input validation, retained values after failure, explicit retry, and EOF without submission. Helper tests check the strict request allowlist.

For runtime checks, build the matching pair and use private home, configuration, data, state, and runtime directories. Import a profile without TUN or external listeners and a complete native-rule list, start the engine, and compare domain, IPv4/IPv6, supplied-IP, port, and process results. Compare rule counters before and after tests, verify global/direct warnings, and check disable/enable changes after activation. A local HTTP fixture can exercise remote snapshot import without downloading third-party rules. In a real PTY, open Routing & DNS → Routing Rules → Test Rule, submit an address, return, and exit while the engine remains running.

The October 2026 Linux check passed remote snapshot import of the two Personal IP rules, domain and supplied-IP matches, unchanged rule counters, global-mode warnings, disable/enable activation, and the real PTY menu flow. Native macOS interactive acceptance requires a macOS host; cross-compilation verifies compilation only.

## Rule file import acceptance

The simplified `rules import NAME SOURCE` interface accepts only complete native rules; it has no format selector, common target, or format detection. Previously imported provider sets remain readable and manageable.

The unified [rule import command](../reference/cli.md#rule-file-imports) is covered by storage, command, menu, and output tests. They check per-rule targets and order, persistence and whole-object management, bounded local/HTTP snapshots, validation rollback, subscription target checks, help and argument errors, text/JSON output, removal of the old commands, shell completion, and retained menu forms with explicit retry.

The October 2026 Linux check passed `go test ./...` and a CLI build with the simplified `rules import NAME SOURCE` interface. An isolated run against the exact engine commit in `engine.lock` passed full-rule import while stopped, different rule targets and preserved order, invalid-target rollback while stopped and running, runtime reload, disable/enable/remove, HTTP snapshot import, management of an existing provider set, and rejection of removed commands and format arguments. A real PTY menu run entered the name and source directly after Import rules, imported the rules, and removed the whole named import through Manage Rules and Sets; exiting the menu left mihomo running. The checks used private XDG directories with TUN, DNS, and proxy listeners disabled. Changed documentation links and command examples were reviewed. Native macOS interactive acceptance was not run on the Linux host.

## Per-profile rule verification (2026-10-13)

`go test ./...` passed after custom-rule storage changed to `config/rules/PROFILE.yaml`. Regression tests cover independent same-name rules, profile switching, target-profile subscription checks across explicit and scheduled application, failed rule changes, profile removal and recreation, and ignoring the former global file without migration.

An isolated Linux CLI run against the commit in `engine.lock` passed running-engine switches between a subscription profile and a node-less `default`, independent rule activation, invalid-target rollback, subscription application retaining a rule's manual-node target, and deletion of only the removed profile's rules. The fixture disabled TUN, DNS, and proxy listeners and used a local HTTP subscription server. Changed documentation links, command help, and examples were reviewed. Native macOS interactive acceptance was not run on the Linux host.

## Node and chain acceptance

Use private home/XDG configuration, data, state, and runtime directories and the exact engine commit in `engine.lock`. Never reuse an installed profile, identity, subscription, or service. Unit fixtures use placeholders and must not authenticate to a real tailnet. The [node and chain guide](../guides/proxy-nodes-and-chains.md) and [CLI reference](../reference/cli.md#proxy-nodes-and-chains) define the implemented workflow.

Run `make test`, focused race tests for profile/command/output/menu/subscription/helper/runtime packages, and CLI cross-builds for Linux and Darwin AMD64/ARM64. Verify stopped JSON queries and previews, invalid arguments and removed commands, full transaction rollback, chain order, first-hop clearing, shared dependencies, provider payload conflicts, subscription explicit/automatic replay, policy switching/clearing, and owner-only sensitive state. Menu tests must use the production `data.plan` wrapper rather than an unwrapped plan fixture and must block conflicting submissions.

For isolated runtime acceptance, run a local HTTP subscription/target service and local SOCKS fixtures. Validate both Tailscale directions and WireGuard combinations while stopped; separately exercise real local multi-hop SOCKS forwarding, running provider changes, nonrecursive clear, downloaded-cache separation, and new subscription nodes inheriting a policy. In a real PTY, enter node details and construct a chain preview through the final CLI binary, verify the reported order and replacements, cancel before Submit, and exit while the engine remains running. Successful configuration validation is not evidence of authenticated Tailscale or WireGuard traffic.

For authenticated transit acceptance, supply a dedicated tailnet, approved exit, and controlled TCP and UDP proxy servers. Check actual observed egress after provider refresh, reload, and restart, including rejected authentication without fallback forwarding. Keep credentials and captured identities private. Native macOS acceptance additionally needs the root helper, TUN/system DNS restoration, restart/reload, and backend switching. The root helper's opt-in non-TUN lifecycle fixture remains `NAGI_TS_TEST_ENGINE=/absolute/path/to/mihomo go test ./internal/privileged/server -run TestTailscaleRealRestrictedEngineLifecycle`; it does not establish authenticated transit.

### Verification (2026-10-20)

The integration used a freshly built `with_gvisor` engine from clean source commit `a5df21a6a339a651bb50dca73b30f7679dbd0436`, matching `engine.lock`. Tests used private directories, local HTTP/SOCKS services, disabled TUN and DNS, and a private loopback listener. No installed engine or host proxy configuration was changed.

| Check | Result |
| --- | --- |
| `make test`, including installer integration | Passed |
| Profile, command, output, menu, subscription, helper and runtime race checks | Passed |
| Independent profile tests for ordering, cycles, provider conflicts, group references, adopted-source switching and exact clear plans | Passed |
| Linux/Darwin AMD64/ARM64 CLI builds | Passed |
| Stopped node/group/chain JSON reads and previews, no file writes, usage envelopes and old command rejection | Passed |
| Tailscale first/later hop and mixed WireGuard configuration validation | Passed while stopped; no authenticated transit |
| Real local SOCKS first → middle → target → HTTP forwarding | Passed |
| Running provider changes, nonrecursive clear, subscription cache separation/new-node replay and private policy permissions | Passed |
| Linux PTY node details and real chain preview with order/replacements, cancellation and exit retaining the engine | Passed |
| Generated localization registry and native descriptor resources | Regenerated with the existing tool and checked for consistency; no native UI change |
| Authenticated Tailscale or WireGuard forwarding, live UDP multi-hop behavior | Unverified; dedicated credentials and endpoints unavailable |
| Native macOS App/helper/TUN and system DNS acceptance | Unverified; Linux host only |

## Latency management verification

The October 2026 Linux verification used the exact engine commit in `engine.lock`, isolated XDG directories, and a local HTTP subscription/test server with DNS, TUN, and proxy listeners disabled. It covered saved-but-unapplied subscription intervals, initial application, Provider-local precedence, inheritance and reset, unchanged source bytes, new feed groups, and scheduled refresh retaining local policy. A separate inactive-profile check confirmed that editing or removing its subscription policy leaves the current profile and source files unchanged.

Native requests verified same-name top-level and Provider nodes remain distinct, ambiguous ownership fails before probing, inherited group test URLs and configured timeouts/expected HTTP status are used, unexpected status fails, and successful zero milliseconds remains successful. Nested group availability followed the selected leaf; built-ins retained plain names without delay observations. Single-node tests retained fixed choices, while native automatic-group batch tests released them. A real terminal PTY verified passive menu opening, nested availability, plain DIRECT/REJECT entries, and absence of per-group interval settings.

Go unit/installer tests, targeted race tests, Go vet, Bash and Bash 3.2 contextual completion, Darwin AMD64/ARM64 CLI cross-builds, and modified Markdown links passed. App regression tests cover the same presentation and dispatch contracts, but native Swift compilation and macOS interactive acceptance were not run on the Linux host. On macOS, run `swift test --package-path macos/NagiApp` and `make build app`, then inspect nested-group selection changes, built-in rows, contextual single tests, and subscription/Provider interval dialogs against the matching CLI.

## Shared latency test verification (2026-10-17)

The matching engine shares identical node measurements across group schedulers and manual tests; the [CLI reference](../reference/cli.md#automatic-latency-check-intervals) defines the five-second reuse behavior. Engine race checks cover concurrent request sharing, expiry, distinct node instances and test conditions, caller cancellation, expected-status failures, and successful zero-millisecond API responses. Nagi regression tests accept successful reused node/group responses without requiring new history.

`make test` passed, including installer integration. Engine checks passed with `go test -race -tags with_gvisor ./adapter ./adapter/provider ./adapter/outboundgroup ./hub/route`. An isolated Linux runtime fixture used two automatic groups referencing one local direct node, a local HTTP test server, private XDG directories, and disabled TUN, DNS, and proxy listeners. Both initial automatic checks produced one HTTP request; subsequent manual node and group tests reused it and retained its original history timestamp. After five seconds, concurrent group tests produced one new request. Expected-status failures were retried without caching and invalidated the earlier success. No installed engine was restarted. Native macOS interactive acceptance was not run.

## Private request rewrite service

The private service requires Go 1.24.0 or newer, goproxy v1.9.2, coder/websocket v1.8.14, Brotli v1.1.1, klauspost/compress v1.17.9 and wazero v1.10.1. The [fixed QuickJS/WASM artifact](rewrite-script-guest.md) builds reproducibly with its pinned source/toolchain and licenses; ordinary Go builds embed it without downloading a compiler. The shared `scripts/build.sh` check applies to CLI and release builds; `go.mod`/`go.sum` lock transitive versions. `go mod verify` checks the downloaded module contents. The public CLI, lifecycle coordinator and platform trust interfaces are implemented. Terminal menu forms are implemented and verified on Linux. Native macOS controls passed Swift tests, release compilation and App packaging; platform trust, TUN and interactive acceptance have separate gates. The [verification matrix](request-rewrite-acceptance.md) records the combined checks and remaining release gates.

Run focused race checks with an isolated test environment:

```sh
go test -race ./internal/requestrewrite ./internal/requestrewriteservice
go mod verify
```

The child fixture exercises a real subprocess under an ordinary UID. On Linux when tests run as root, it drops the fixture to UID/GID 65534; the production root-child guard remains enabled. All CA material and sockets are throwaway and no OS trust is installed.

For the opt-in real engine integration, supply a clean tracked checkout at reviewed commit `a5df21a6a339a651bb50dca73b30f7679dbd0436`:

```sh
GO=/path/to/go MIHOMO_DIR=/path/to/committed-engine ./scripts/test-request-rewrite-engine.sh
```

The script checks that exact commit and tracked cleanliness, creates a temporary module with local source replacements, and removes it on exit. It leaves Nagi's module files and `engine.lock` unchanged. Engine dependencies are confined to the temporary module. The fixture uses synthetic TUN metadata through the real engine tunnel, authenticated channels, configured engine hosts and native DIRECT outbound to local HTTP/TLS servers. It verifies Rule/Global/Direct, separate interception/target scopes, Host/SNI changes, path/query edits, gzip JSON and exact integers, plus original-request response
matching, the real isolated guest, compressed response edits and EOF trailers. Additional service tests exercise response, HTTP/2, trailers and bidirectional WebSocket transformations. Expected final output is `actual-engine HTTP/TLS Rule/Global/Direct roundtrips passed`. It neither opens an OS TUN nor starts an installed engine, uses subscriptions or changes user routing. Invalid environment proxy addresses may be supplied to confirm the adapter ignores them. This engine commit is now published and remotely verified as the source pin; the fixture alone does not establish real OS TUN or platform acceptance.

Native macOS peer credentials, actual TUN/process capture on Linux and macOS, isolated CA trust cleanup and the native model-to-CLI draft activation chain have separate evidence in the [verification record](request-rewrite-acceptance.md#capture-verification). Manual App interaction, sleep/wake and sustained resource checks remain unverified. Cross-compilation does not establish those results. The conversion 64 MiB workspace reservation bounds admission, not process heap/RSS; pressure acceptance must measure the complete runtime.


## Request rewrite coordinator

The CLI/coordinator uses published mihomo commit `a5df21a6a339a651bb50dca73b30f7679dbd0436`, including restricted immutable owner binding and capture protocol 1. `engine.lock` records this commit on `https://github.com/ronigooja/mihomo.git`, ref `main`. The same commit was initially verified on the capture feature branch on 2026-10-18 and integrated into the remote `main` on 2026-10-19. This publishes engine source, not Nagi release binaries: ordinary installation and `nagi update` require a matching Nagi release. Older engines return an actionable capability error for rewrite or capture activation.

Build the matching source pair through the locked path, without a local checkout override:

```sh
./scripts/build.sh
./bin/nagi --json version
```

The version result must report the commit in `engine.lock` as `mihomo_commit`. `NAGI_MIHOMO_BIN` remains available for explicit development overrides. Use isolated Nagi home/XDG directories for runtime work; do not point a test at ordinary subscriptions, existing CA files, installed services or proxy ports.

Run the exact-source coordinator fixture with:

```sh
GO=/path/to/go MIHOMO_DIR=/path/to/reviewed-mihomo ./scripts/test-request-rewrite-coordinator.sh
NAGI_REWRITE_TEST_ENGINE=/path/to/reviewed-mihomo-binary go test -race ./internal/privileged/...
go test -race ./...
```

The coordinator script checks the exact clean engine commit, builds a temporary source fixture and CLI, and runs all commands under an ordinary UID (65534 when the Linux test runner is root). It uses real engine Unix control/mTLS channels and an explicitly synthetic internal TUN identity, never an OS TUN. Isolated CA generation/public export and `SSL_CERT_FILE`/`SSL_CERT_DIR` exercise trust without modifying an OS trust store. The fixture verifies persistent child/supervisor readiness, repeated enable, rejected-probe rule rollback with the original supervisor alive, CAS cutover, child crash blocking and explicit recovery, trust-root removal and fresh assessment, scheduled transport-certificate renewal, engine-exit cleanup, same-epoch config reload, profile switches, zero-rule idle transitions, TUN teardown and quit. Temporary state and artifacts are removed. Empty generation lock files contain no secrets and may remain during a user session.

The helper fixture covers a real restricted root engine, authenticated fixed-UID bootstrap, mTLS synthetic probe, reload retention, mismatched/unbound owner rejection and redacted status. Unit/race fixtures cover incomplete capability limits, actionable CLI prerequisite reasons, uncertain publish/disable, retained two-generation ownership, blocked-generation retirement, shutdown failure, private journal permissions and FIFO-safe root-store reads. These checks passed on Linux; CLI cross-builds cover Linux/macOS AMD64/ARM64. Actual Linux/macOS TUN/helper capture, source identity and route/DNS restoration, plus isolated native macOS trust installation/removal, are recorded in the [capture verification ledger](request-rewrite-acceptance.md#capture-verification). Manual macOS interaction and sleep/wake remain unverified. Raw request fixtures, explicit rule exports and explicitly viewed engine configurations/logs may contain sensitive content and must not be attached to ordinary diagnostics.

## Localization verification

Catalog/schema parity and strict descriptor arguments are checked by Go tests;
`internal/i18n/contract_test.go` and the native `LocalizationTests` also consume
`docs/design/localization-contract.json`. After changing producer catalogs, run
`python3 macos/NagiApp/Tools/update-message-catalog.py` and include the generated
native resources in the same change. This generator is a maintenance step,
not a runtime translation dependency.

Run `make test` for the complete Go suite and installer integration. Exercise
the affected operation boundaries and immutable contexts with:

```sh
go test -race ./internal/i18n ./internal/output ./internal/menu ./internal/command ./internal/subscription ./internal/installation ./internal/privileged/... ./internal/profile
```

The integrated Linux candidate passed the complete Go/installer suite and the
affected race checks. Catalog checks require exact producer signatures, independently invalid optional
metadata, nested source composition, redaction and language-invariant JSON.
Bilingual PTY menu flows and raw-content preservation have separate regression
coverage. Actual macOS Zsh syntax, loading and language-prefix operand completion
passed using isolated fictitious candidates. Fish was unavailable on both test hosts;
its generation/schema checks passed, but executable Fish acceptance remains
unverified.

On an isolated macOS checkout, run `swift test --package-path macos/NagiApp` and
`make build app`, inspect both packaged language directories, signature JSON and
menu PDF, and verify the relocated bundle without the development directory.
Use per-process language preferences for automated English/Chinese runs; leave
the host's preferences and installed user App untouched. Native minimum-size
constraints and mounted accessibility fixtures resolve the earlier five baseline
layout assertions. The native full English suite ran 231 tests with no failures
and one intentional live-capture skip; 26
selected Chinese resource, transport, action and layout tests passed. Earlier
full Chinese runs include legacy English-only test assertions, so these results
do not claim a complete Chinese suite pass. After the final resource update,
18 localization/parity tests passed separately in each language, and App
packaging plus fresh-process relocated resource selection passed with all 8,800
producer signatures. Native automated actions cover bilingual retry forms,
retained edits, keyboard focus and light/dark appearances. The test session has desktop accessibility disabled; in-process fixtures do not
require that permission. Manual checks must still
cover all window families, preferred/per-App language and restart, long text,
focused/disabled controls, editor interaction, scrolling and minimum window size.
The [localization design](../design/localization.md) separates source migration
from this remaining desktop acceptance.
