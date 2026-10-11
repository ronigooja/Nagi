# Build and test

This document describes development builds and verification for Nagi and the pinned mihomo source.

Use Go 1.20 or newer. `engine.lock` records the mihomo repository, ref, and exact commit. `make build cli` creates `bin/nagi` without rebuilding mihomo and prints `Nagi CLI build succeeded: PATH` on success or `Nagi CLI build failed` on a compilation error. The CLI build uses Go from `PATH`, then checks `$HOME/environment/go*/bin/go`, `/opt/homebrew/bin/go`, and `/usr/local/go/bin/go`. This also works in macOS SSH sessions whose non-interactive `PATH` omits Homebrew. Set `GO=/path/to/go` to choose another executable. On macOS, `make build app` packages `bin/Nagi.app` through `macos/NagiApp/package-app.sh` and prints `Nagi App build succeeded: PATH` or `Nagi App build failed`. Run `./scripts/build.sh` for a full pinned mihomo and CLI build; it reports the result of each compilation. The mihomo build includes `with_gvisor`, which is required when a profile selects the gVisor TUN stack. Set `MIHOMO_DIR` to a checkout at the exact locked commit for local builds. Otherwise the script clones or updates `.build/mihomo` and checks out that commit. A mismatch fails the build.

```sh
make build cli
# On macOS:
make build app
# To rebuild the pinned mihomo as well:
GO=/path/to/go MIHOMO_DIR=/path/to/mihomo ./scripts/build.sh
GO=/path/to/go make test
```

For installation, updates, or removal, use `./scripts/installer.sh install`, `./scripts/installer.sh update`, or `./scripts/installer.sh uninstall` with artifacts already in `bin/`. Use `./scripts/sync_to_mac.sh` to copy the source tree to the macair checkout without copying local build artifacts. The script prints the destination when it starts and reports success or failure when it finishes. The [installation workflow](install.md) defines artifact layout, installer options, app installation, and cleanup.

`make test` runs the Go unit tests and the installer integration test with a temporary installation prefix. To check runtime integration manually with the built mihomo executable, run these commands from the repository root in order:

```sh
./bin/nagi start
./bin/nagi --json status
./bin/nagi config validate
./bin/nagi proxy groups
./bin/nagi connections list
./bin/nagi stop
```

`start` creates the default profile if none exists. TUN is disabled in that profile, so this integration check does not alter routes unless you explicitly run `nagi tun enable`; enabling it requires suitable permissions and, on macOS, may require the privileged helper. Expect `status` to report a running process, `config validate` to report a valid profile, and the proxy and connection commands to succeed; an empty connection list is valid. Inspect `nagi tun status` and `nagi logs` after explicitly enabling TUN. On Linux, set `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, and `XDG_RUNTIME_DIR` to directories in a temporary test location before running the commands if you need to isolate them from a normal Nagi instance. `XDG_RUNTIME_DIR` must be an absolute path. On macOS, Nagi uses `~/Library/Application Support/Nagi` rather than XDG paths. This manual check is not part of CI. The macOS app is under `macos/NagiApp`; its [packaging script](../../macos/NagiApp/README.md) builds with Swift Package Manager and creates an `LSUIElement` bundle in `bin/`. Signing and notarization require an Apple developer identity and are not part of the local build.

The CI workflow runs tests and a full locked CLI and mihomo build on Linux and macOS. It does not sign or publish binaries.

`make release` cross compiles Nagi and mihomo into `dist/linux-amd64`, `dist/linux-arm64`, `dist/darwin-amd64`, and `dist/darwin-arm64`. On macOS it also uses `lipo` to make `dist/darwin-universal`. Set `APPLE_SIGN_IDENTITY` to sign both universal executables. With a configured notarytool keychain profile, set `APPLE_NOTARY_PROFILE` to zip and notarize them. The macOS menu app is packaged separately by its own script. Versioning, tag checks, artifact review, and release notes are defined in [Release process](release.md).

The current lock points at the available fork's `main` commit and includes the `--check-json`, restricted configuration policy, activation ID, and age decryption interfaces used by Nagi. A dedicated `nagi/meta` branch and upstream synchronization policy in the original plan have not yet been established in the mihomo repository.
