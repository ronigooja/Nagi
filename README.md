# Nagi

Nagi is a macOS and Linux CLI for managing a locally installed mihomo process. It starts mihomo with a private Unix Socket control API, manages profiles, local overrides, subscription caches, and encrypted DNS policy, and provides proxy selection and latency checks, connection controls, and runtime routing modes. Profile export, backup, restore, and comparison support local configuration work. Read-only diagnostics and Bash, Zsh, and Fish completion support terminal use. A macOS SwiftUI client calls the CLI for common operations.

## Quick start

Requirements: Go 1.20 or newer, Git, and a local mihomo checkout at the commit in [`engine.lock`](engine.lock), or network access to obtain that commit.

```sh
MIHOMO_DIR=/path/to/mihomo make build
./bin/nagi --json version
./bin/nagi start
./bin/nagi status
```

Without `MIHOMO_DIR`, the build script fetches the repository and exact commit in `engine.lock`. The first `start` creates a minimal `default` profile. Set `NAGI_MIHOMO_BIN` to use a different mihomo executable at runtime. macOS and Linux are supported; the SwiftUI app requires macOS 13 or newer.

## Documentation

- [Architecture](ARCHITECTURE.md): system boundaries and module relationships.
- [Getting started](docs/guides/getting-started.md): initial configuration and operation.
- [Rules](docs/guides/rules.md): active rules, custom rules, and rule sets.
- [CLI reference](docs/reference/cli.md): commands, output, and exit codes.
- [Runtime design](docs/design/runtime.md): process, socket, configuration, and recovery behavior.
- [Development](docs/development/build-and-test.md): locked builds and tests.
- [Feature roadmap](docs/development/roadmap.md): planned user-facing capabilities.
- [CLI usability rules](docs/development/cli-usability.md): command help, human output, actionable errors, and compatibility checks.
- [Release process](docs/development/release.md): version rules, release checks, and release note requirements.
- [Architecture decision](docs/decisions/0001-pin-mihomo-commit.md): mihomo source pinning.
