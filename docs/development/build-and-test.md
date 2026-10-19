# Build and test

This document describes development builds and verification for Nagi and the pinned mihomo source.

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

The current lock points at a commit available on the configured fork's `request-rewrite-v2` branch and includes the `--check-json`, restricted configuration policy, activation ID, age decryption, `dns.resolve-proxy-destinations`, destination rule testing, and restricted Tailscale state/lifecycle and request-rewrite v2 interfaces used by Nagi. Publish engine changes to the declared repository and ref before updating `engine.lock`, then verify the pinned commit is reachable from a fresh fetch of that ref. A successful build with `MIHOMO_DIR` alone does not establish remote availability. A dedicated `nagi/meta` branch and upstream synchronization policy in the original plan have not yet been established in the mihomo repository.

## Rule test acceptance

Run `go test ./...` in Nagi and `go test -race -tags with_gvisor ./tunnel ./hub/route -run 'TestRule(API|TestEndpoint)'` in the matching engine checkout. Command tests verify runtime-independent help and invalid input, text/JSON output, default metadata, old-engine errors, and exclusion from configuration transactions. Menu tests cover numeric navigation, input validation, retained values after failure, explicit retry, and EOF without submission. Helper tests check the strict request allowlist.

For runtime checks, build the matching pair and use private home, configuration, data, state, and runtime directories. Import a profile without TUN or external listeners and a complete native-rule list, start the engine, and compare domain, IPv4/IPv6, supplied-IP, port, and process results. Compare rule counters before and after tests, verify global/direct warnings, and check disable/enable changes after activation. A local HTTP fixture can exercise remote snapshot import without downloading third-party rules. In a real PTY, open Settings & Maintenance → Routing Rules → Test Rule, submit an address, return, and exit while the engine remains running.

The October 2026 Linux check passed remote snapshot import of the two Personal IP rules, domain and supplied-IP matches, unchanged rule counters, global-mode warnings, disable/enable activation, and the real PTY menu flow. Native macOS interactive acceptance requires a macOS host; cross-compilation verifies compilation only.

## Rule file import acceptance

The simplified `rules import NAME SOURCE` interface accepts only complete native rules; it has no format selector, common target, or format detection. Previously imported provider sets remain readable and manageable.

The unified [rule import command](../reference/cli.md#rule-file-imports) is covered by storage, command, menu, and output tests. They check per-rule targets and order, persistence and whole-object management, bounded local/HTTP snapshots, validation rollback, subscription target checks, help and argument errors, text/JSON output, removal of the old commands, shell completion, and retained menu forms with explicit retry.

The October 2026 Linux check passed `go test ./...` and a CLI build with the simplified `rules import NAME SOURCE` interface. An isolated run against the exact engine commit in `engine.lock` passed full-rule import while stopped, different rule targets and preserved order, invalid-target rollback while stopped and running, runtime reload, disable/enable/remove, HTTP snapshot import, management of an existing provider set, and rejection of removed commands and format arguments. A real PTY menu run entered the name and source directly after Import rules, imported the rules, and removed the whole named import through Manage Rules and Sets; exiting the menu left mihomo running. The checks used private XDG directories with TUN, DNS, and proxy listeners disabled. Changed documentation links and command examples were reviewed. Native macOS interactive acceptance was not run on the Linux host.

## Per-profile rule verification (2026-10-13)

`go test ./...` passed after custom-rule storage changed to `config/rules/PROFILE.yaml`. Regression tests cover independent same-name rules, profile switching, target-profile subscription checks across explicit and scheduled application, failed rule changes, profile removal and recreation, and ignoring the former global file without migration.

An isolated Linux CLI run against the commit in `engine.lock` passed running-engine switches between a subscription profile and a node-less `default`, independent rule activation, invalid-target rollback, subscription application retaining a rule's manual-node target, and deletion of only the removed profile's rules. The fixture disabled TUN, DNS, and proxy listeners and used a local HTTP subscription server. Changed documentation links, command help, and examples were reviewed. Native macOS interactive acceptance was not run on the Linux host.

## Tailscale acceptance

Use private configuration, state, and runtime directories for Tailscale checks. Unit fixtures use placeholder keys and must not authenticate to a real tailnet. Run the relevant profile, command, menu, helper policy, helper runtime, and runtime path tests; run race checks for configuration activation and engine Tailscale Start/Close. Engine tests require `-tags with_gvisor` to exercise the real adapter.

With the matching new engine, import an isolated profile containing a TCP proxy (for example SS), a proxy that connects to its server over UDP (for example Hysteria2), and an inline provider. Add a Tailscale exit, bind both nodes and the provider, and validate while stopped. Confirm stopped state is retained, then start, edit the exit, reload, refresh the provider, unbind, remove, and stop. Check key/path redaction, retained provider overrides, activation IDs, and persistent identity directories. These configuration checks do not verify traffic through the exit. To opt into the real root helper/engine lifecycle harness without TUN or tailnet login, run `NAGI_TS_TEST_ENGINE=/absolute/path/to/mihomo go test ./internal/privileged/server -run TestTailscaleRealRestrictedEngineLifecycle` as root in an isolated checkout. The test creates private temporary state under the test account's home and verifies reload, stop cleanup, and restart retain it; ordinary tests skip the harness.

For authenticated transit acceptance, supply a dedicated test tailnet, approved exit, and TCP and UDP-transport proxy servers. Request a known destination through each selected proxy and verify the proxy server observes the expected exit path. Recheck after provider refresh, reload, and engine restart. Exercise rejected login and exit preference initialization without allowing fallback forwarding. Protect test keys and do not commit captured identities or credentials.

Native macOS acceptance must run the helper with its root-private UID state, start and stop TUN, verify network-service DNS restoration, and repeat restart/reload and backend switching. Darwin cross compilation confirms build compatibility only. Record authenticated transit and native acceptance as unverified when those hosts or credentials are unavailable; do not infer network success from configuration activation.

### Implementation verification (2026-10-12)

The Tailscale implementation was checked against engine commit `e8555db99bb891112f880c9e06f2c6edc4bd5cc3`. This commit was published to the declared repository's `main` ref and verified from a fresh fetch before advancing `engine.lock`.

| Check | Result |
| --- | --- |
| `make test`, including installer integration | Passed |
| Tailscale profile, CLI, menu, state-path, policy, helper and activation race checks | Passed |
| Engine policy, provider, configuration, DNS, outbound and executor tests with `with_gvisor`; lifecycle race checks | Passed |
| Isolated real-engine CLI validation and activation with SS, Hysteria2, inline and HTTP providers | Passed; refreshed HTTP provider nodes inherited the binding and unbind cleared it; no authenticated transit |
| Linux root helper harness with real restricted engine, reload, restart and state retention | Passed; TUN disabled, no tailnet login |
| CLI and engine Darwin AMD64/ARM64 builds | Passed |
| Visible Auth Key paste in Linux CLI/menu PTY, blank edit preservation, Ctrl-C/EOF cancellation, and stdin result redaction | Passed against the real engine in isolated directories; terminal echo and settings unchanged |
| Bash and Bash 3.2 completion execution | Passed |
| Zsh/Fish generated completion content | Checked; executable shell checks unavailable because those shells are not installed |
| Documentation relative paths and changed command/field examples | Reviewed |
| Authenticated TCP and UDP-transport exit/proxy transit | Unverified; dedicated tailnet, approved exit and test proxy credentials unavailable |
| Native macOS helper, TUN and system DNS restoration | Unverified; no macOS runtime host used |

## Latency management verification

The October 2026 Linux verification used the exact engine commit in `engine.lock`, isolated XDG directories, and a local HTTP subscription/test server with DNS, TUN, and proxy listeners disabled. It covered saved-but-unapplied subscription intervals, initial application, Provider-local precedence, inheritance and reset, unchanged source bytes, new feed groups, and scheduled refresh retaining local policy. A separate inactive-profile check confirmed that editing or removing its subscription policy leaves the current profile and source files unchanged.

Native requests verified same-name top-level and Provider nodes remain distinct, ambiguous ownership fails before probing, inherited group test URLs and configured timeouts/expected HTTP status are used, unexpected status fails, and successful zero milliseconds remains successful. Nested group availability followed the selected leaf; built-ins retained plain names without delay observations. Single-node tests retained fixed choices, while native automatic-group batch tests released them. A real terminal PTY verified passive menu opening, nested availability, plain DIRECT/REJECT entries, and absence of per-group interval settings.

Go unit/installer tests, targeted race tests, Go vet, Bash and Bash 3.2 contextual completion, Darwin AMD64/ARM64 CLI cross-builds, and modified Markdown links passed. App regression tests cover the same presentation and dispatch contracts, but native Swift compilation and macOS interactive acceptance were not run on the Linux host. On macOS, run `swift test --package-path macos/NagiApp` and `make build app`, then inspect nested-group selection changes, built-in rows, contextual single tests, and subscription/Provider interval dialogs against the matching CLI.

## Shared latency test verification (2026-10-17)

The matching engine shares identical node measurements across group schedulers and manual tests; the [CLI reference](../reference/cli.md#automatic-latency-check-intervals) defines the five-second reuse behavior. Engine race checks cover concurrent request sharing, expiry, distinct node instances and test conditions, caller cancellation, expected-status failures, and successful zero-millisecond API responses. Nagi regression tests accept successful reused node/group responses without requiring new history.

`make test` passed, including installer integration. Engine checks passed with `go test -race -tags with_gvisor ./adapter ./adapter/provider ./adapter/outboundgroup ./hub/route`. An isolated Linux runtime fixture used two automatic groups referencing one local direct node, a local HTTP test server, private XDG directories, and disabled TUN, DNS, and proxy listeners. Both initial automatic checks produced one HTTP request; subsequent manual node and group tests reused it and retained its original history timestamp. After five seconds, concurrent group tests produced one new request. Expected-status failures were retried without caching and invalidated the earlier success. No installed engine was restarted. Native macOS interactive acceptance was not run.

## Private request rewrite service

The private service requires Go 1.24.0 or newer, goproxy v1.9.2, coder/websocket v1.8.14, Brotli v1.1.1, klauspost/compress v1.17.9 and wazero v1.10.1. The [fixed QuickJS/WASM artifact](rewrite-script-guest.md) builds reproducibly with its pinned source/toolchain and licenses; ordinary Go builds embed it without downloading a compiler. The shared `scripts/build.sh` check applies to CLI and release builds; `go.mod`/`go.sum` lock transitive versions. `go mod verify` checks the downloaded module contents. The public CLI, lifecycle coordinator and platform trust interfaces are implemented. Terminal menu forms are implemented and verified on Linux. Native macOS controls have a source implementation, with Swift build/tests, packaging and native trust/TUN/UI acceptance unverified. The [verification matrix](request-rewrite-acceptance.md) records the combined checks and remaining release gates.

Run focused race checks with an isolated test environment:

```sh
go test -race ./internal/requestrewrite ./internal/requestrewriteservice
go mod verify
```

The child fixture exercises a real subprocess under an ordinary UID. On Linux when tests run as root, it drops the fixture to UID/GID 65534; the production root-child guard remains enabled. All CA material and sockets are throwaway and no OS trust is installed.

For the opt-in real engine integration, supply a clean tracked checkout at reviewed commit `84369733b97b897a04a70e507183eda22e38e0c6`:

```sh
GO=/path/to/go MIHOMO_DIR=/path/to/committed-engine ./scripts/test-request-rewrite-engine.sh
```

The script checks that exact commit and tracked cleanliness, creates a temporary module with local source replacements, and removes it on exit. It leaves Nagi's module files and `engine.lock` unchanged. Engine dependencies are confined to the temporary module. The fixture uses synthetic TUN metadata through the real engine tunnel, authenticated channels, configured engine hosts and native DIRECT outbound to local HTTP/TLS servers. It verifies Rule/Global/Direct, separate interception/target scopes, Host/SNI changes, path/query edits, gzip JSON and exact integers, plus original-request response
matching, the real isolated guest, compressed response edits and EOF trailers. Additional service tests exercise response, HTTP/2, trailers and bidirectional WebSocket transformations. Expected final output is `actual-engine HTTP/TLS Rule/Global/Direct roundtrips passed`. It neither opens an OS TUN nor starts an installed engine, uses subscriptions or changes user routing. Invalid environment proxy addresses may be supplied to confirm the adapter ignores them. This engine commit is now published and remotely verified as the source pin; the fixture alone does not establish real OS TUN or platform acceptance.

Native macOS peer-credential runtime behavior, real TUN/process capture, platform CA trust and final lifecycle/UI acceptance require their respective host/integration gates. Cross-compilation does not establish those results. The conversion 64 MiB workspace reservation bounds admission, not process heap/RSS; pressure acceptance must measure the complete runtime.


## Request rewrite coordinator

The CLI/coordinator uses published mihomo commit `84369733b97b897a04a70e507183eda22e38e0c6`, including restricted immutable owner binding. `engine.lock` records this commit on `https://github.com/ronigooja/mihomo.git`, ref `request-rewrite-v2`. A fresh remote checkout and the matching locked build were verified on 2026-10-18. This publishes engine source, not Nagi release binaries: ordinary installation and `nagi update` require a matching Nagi release. Older engines return an actionable capability error for rewrite enable.

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

The helper fixture covers a real restricted root engine, authenticated fixed-UID bootstrap, mTLS synthetic probe, reload retention, mismatched/unbound owner rejection and redacted status. Unit/race fixtures cover incomplete capability limits, actionable CLI prerequisite reasons, uncertain publish/disable, retained two-generation ownership, blocked-generation retirement, shutdown failure, private journal permissions and FIFO-safe root-store reads. These checks passed on Linux; CLI cross-builds cover Linux/macOS AMD64/ARM64. Actual OS TUN capture/routes, sleep/wake, native macOS keychain trust and macOS interaction remain unverified. Raw request fixtures, explicit rule exports and explicitly viewed engine configurations/logs may contain sensitive content and must not be attached to ordinary diagnostics.
