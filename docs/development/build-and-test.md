# Build and test

This document describes development builds and verification for Nagi and the pinned mihomo source.

Use Go 1.20 or newer. `engine.lock` records the mihomo repository, ref, and exact commit. `make build` invokes `scripts/build.sh` and creates `bin/mihomo` and `bin/nagi`. Set `MIHOMO_DIR` to a checkout at the exact locked commit for local builds. Otherwise the script clones or updates `.build/mihomo` and checks out that commit. A mismatch fails the build.

```sh
GO=/path/to/go MIHOMO_DIR=/path/to/mihomo make build
GO=/path/to/go make test
```

`make test` runs the Go unit tests. For runtime integration, start with `bin/nagi start`, inspect `bin/nagi --json status`, read `bin/nagi proxy groups`, then stop. Use an isolated XDG environment when running these commands in automated tests. The macOS app is under `macos/NagiApp` and is built with Swift Package Manager on macOS. Signing and notarization require an Apple developer identity and are not part of the local build script.

The CI workflow runs tests and a locked build on Linux and macOS. It does not sign or publish binaries.

`make release` cross compiles Nagi and mihomo into `dist/linux-amd64`, `dist/linux-arm64`, `dist/darwin-amd64`, and `dist/darwin-arm64`. On macOS it also uses `lipo` to make `dist/darwin-universal`. Set `APPLE_SIGN_IDENTITY` to sign both universal executables. With a configured notarytool keychain profile, set `APPLE_NOTARY_PROFILE` to zip and notarize them. The macOS SwiftUI app itself is not packaged by this script. Versioning, tag checks, artifact review, and release notes are defined in [Release process](release.md).

The current lock points at the available fork `main` commit. A dedicated `nagi/meta` branch and upstream synchronization policy in the original plan have not yet been established in the mihomo repository.
