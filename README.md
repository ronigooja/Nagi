# Nagi

Nagi is a macOS and Linux CLI for managing a locally installed mihomo process. It starts mihomo with a private Unix Socket control API, manages profiles, local overrides, subscription caches, and encrypted DNS policy, and provides proxy search, saved selections, latency checks, connection controls, runtime routing modes, system proxy and TUN controls, and listener/LAN status. Profile export, backup, restore, and comparison support local configuration work. Read-only diagnostics, Bash, Zsh, and Fish completion, and an interactive terminal dashboard support terminal use. A macOS menu bar app calls the CLI for common operations.

## Quick start

Requirements: Go 1.20 or newer, Git, and a local mihomo checkout at the commit in [`engine.lock`](engine.lock), or network access to obtain that commit.

```sh
MIHOMO_DIR=/path/to/mihomo make build
./bin/nagi --json version
./bin/nagi start
./bin/nagi status
```

Without `MIHOMO_DIR`, the build script fetches the repository and exact commit in `engine.lock`. The first `start` creates a minimal `default` profile. Set `NAGI_MIHOMO_BIN` to use a different mihomo executable at runtime. macOS and Linux are supported; the menu bar app requires macOS 13 or newer.

## Documentation

- [Architecture](ARCHITECTURE.md): system boundaries and module relationships.
- [macOS menu app](macos/NagiApp/README.md): menu controls, traffic display, installation, and packaging.
- [Getting started](docs/guides/getting-started.md): initial configuration and operation.
- [Rules](docs/guides/rules.md): active rules, custom rules, and rule sets.
- [Traffic access](docs/guides/traffic-access.md): system proxy, TUN, listener ports, and LAN access.
- [Startup and recovery](docs/guides/startup.md): login service, stale state, and rechecks.
- [Diagnostics](docs/guides/diagnostics.md): redacted health and security checks.
- [CLI reference](docs/reference/cli.md): commands, output, and exit codes.
- [Terminal dashboard](docs/guides/terminal-dashboard.md): interactive keys and workflows.
- [Runtime design](docs/design/runtime.md): process, socket, configuration, and recovery behavior.
- [Development](docs/development/build-and-test.md): locked builds and tests.
- [Feature roadmap](docs/development/roadmap.md): planned user-facing capabilities.
- [CLI usability rules](docs/development/cli-usability.md): command help, human output, actionable errors, and compatibility checks.
- [Release process](docs/development/release.md): version rules, release checks, and release note requirements.
- [Architecture decision](docs/decisions/0001-pin-mihomo-commit.md): mihomo source pinning.
