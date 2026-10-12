# Build and test

This document describes development builds and verification for Nagi and the pinned mihomo source.

Use Go 1.20 or newer. `engine.lock` records the mihomo repository, ref, and exact commit. `make build cli` creates `bin/nagi` without rebuilding mihomo and prints `Nagi CLI build succeeded: PATH` on success or `Nagi CLI build failed` on a compilation error. The CLI build uses Go from `PATH`, then checks `$HOME/environment/go*/bin/go`, `/opt/homebrew/bin/go`, and `/usr/local/go/bin/go`. This also works in macOS SSH sessions whose non-interactive `PATH` omits Homebrew. Set `GO=/path/to/go` to choose another executable. On macOS, `make build app` packages `bin/Nagi.app` through `macos/NagiApp/package-app.sh` and prints `Nagi App build succeeded: PATH` or `Nagi App build failed`. Run `./scripts/build.sh` for a full pinned mihomo and CLI build; it reports the result of each compilation. The mihomo build includes `with_gvisor`, which is required when a profile selects the gVisor TUN stack. Set `MIHOMO_DIR` to a checkout at the exact locked commit for local builds. Otherwise the script clones or updates `.build/mihomo` and checks out that commit. A mismatch fails the build.

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

The current lock points at a commit available on the configured fork's `main` branch and includes the `--check-json`, restricted configuration policy, activation ID, age decryption, and `dns.resolve-proxy-destinations` interfaces used by Nagi. Publish engine changes to the declared repository and ref before updating `engine.lock`, then verify the pinned commit is reachable from a fresh fetch of that ref. A successful build with `MIHOMO_DIR` alone does not establish remote availability. A dedicated `nagi/meta` branch and upstream synchronization policy in the original plan have not yet been established in the mihomo repository.

## Tailscale acceptance

Use private configuration, state, and runtime directories for Tailscale checks. Unit fixtures use placeholder keys and must not authenticate to a real tailnet. Run the relevant profile, command, menu, helper policy, helper runtime, and runtime path tests; run race checks for configuration activation and engine Tailscale Start/Close. Engine tests require `-tags with_gvisor` to exercise the real adapter.

With the matching new engine, import an isolated profile containing a TCP proxy (for example SS), a proxy that connects to its server over UDP (for example Hysteria2), and an inline provider. Add a Tailscale exit, bind both nodes and the provider, and validate while stopped. Confirm stopped state is retained, then start, edit the exit, reload, refresh the provider, unbind, remove, and stop. Check key/path redaction, retained provider overrides, activation IDs, and persistent identity directories. These configuration checks do not verify traffic through the exit.

For authenticated transit acceptance, supply a dedicated test tailnet, approved exit, and TCP and UDP-transport proxy servers. Request a known destination through each selected proxy and verify the proxy server observes the expected exit path. Recheck after provider refresh, reload, and engine restart. Exercise rejected login and exit preference initialization without allowing fallback forwarding. Protect test keys and do not commit captured identities or credentials.

Native macOS acceptance must run the helper with its root-private UID state, start and stop TUN, verify network-service DNS restoration, and repeat restart/reload and backend switching. Darwin cross compilation confirms build compatibility only. Record authenticated transit and native acceptance as unverified when those hosts or credentials are unavailable; do not infer network success from configuration activation.
