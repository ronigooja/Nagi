# Nagi

Nagi is a macOS and Linux CLI for managing a locally installed mihomo process. It starts mihomo with a private Unix Socket control API, manages profiles, local overrides, subscription caches, and encrypted DNS policy, and provides proxy search, saved selections, latency checks, connection controls, runtime routing modes, TUN controls, listener/LAN status, and macOS system proxy controls. Profile export, backup, restore, and comparison support local configuration work. An interactive, line-oriented terminal menu, read-only diagnostics, and Bash, Zsh, and Fish completion support terminal use. A macOS menu bar app calls the CLI for common operations.

## Quick start

Requirements: Go 1.20 or newer, Git, and a matching executable at `bin/mihomo`. To build mihomo from the pinned source for the first time, run `./scripts/build.sh` with a checkout at the commit in [`engine.lock`](engine.lock), or let that script fetch it.

```sh
make build cli
./bin/nagi --json version
./bin/nagi start
./bin/nagi status
./bin/nagi menu
```

Build the CLI with `make build cli` on Linux or macOS; build the macOS App separately with `make build app`. Install, update, or uninstall existing artifacts with `./scripts/installer.sh install`, `./scripts/installer.sh update`, or `./scripts/installer.sh uninstall`. `./scripts/sync_to_mac.sh` copies the source tree to the macair checkout while preserving its local build artifacts. The CLI build does not rebuild mihomo; prepare a matching `bin/mihomo` separately. For a full locked CLI and mihomo build, run `./scripts/build.sh` with `MIHOMO_DIR` pointing to the pinned checkout, or let that script fetch the pinned source. The first `start` creates a `default` profile with DoH resolvers and TUN DNS interception settings, but TUN is disabled until you run `nagi tun enable` (which requires OS permissions). Set `NAGI_MIHOMO_BIN` to use a different mihomo executable at runtime. macOS and Linux are supported; the menu bar app requires macOS 13 or newer.

`nagi menu` opens a step-by-step menu when both input and output are terminals. Choose numbered resources and letter actions; use `/word` to filter names, `b` to go back, and `q` to leave the menu while mihomo keeps running. Actions execute without a second confirmation; their effects appear beside the entry. Use ordinary commands with `--json` for scripts and applications. See the [CLI reference](docs/reference/cli.md#terminal-menu) for the full interaction contract.

## Documentation

- [Architecture](ARCHITECTURE.md): system boundaries and module relationships.
- [macOS menu app](macos/NagiApp/README.md): menu controls, traffic display, installation, and packaging.
- [Getting started](docs/guides/getting-started.md): initial configuration and operation.
- [Rules](docs/guides/rules.md): active rules, custom rules, and rule sets.
- [Traffic access](docs/guides/traffic-access.md): system proxy, TUN, listener ports, and LAN access.
- [Startup and recovery](docs/guides/startup.md): login service, stale state, and rechecks.
- [Diagnostics](docs/guides/diagnostics.md): redacted health and security checks.
- [CLI reference](docs/reference/cli.md): commands, output, and exit codes.
- [Terminal menu design](docs/design/terminal-menu.md): current menu responsibilities, navigation, command dispatch, and failure behavior.
- [Runtime design](docs/design/runtime.md): process, socket, configuration, and recovery behavior.
- [Development](docs/development/build-and-test.md): locked builds and tests.
- [Local installation](docs/development/install.md): installing, updating, and removing built artifacts for development and future release workflows.
- [Linux privileged helper scaffold](docs/development/privileged-linux.md): system service boundary and current availability.
- [macOS privileged TUN helper](docs/reference/cli.md#traffic-access): installation and backend selection for TUN access.
- [Feature roadmap](docs/development/roadmap.md): planned user-facing capabilities.
- [CLI usability rules](docs/development/cli-usability.md): command help, human output, actionable errors, and compatibility checks.
- [Release process](docs/development/release.md): version rules, release checks, and release note requirements.
- [Architecture decision](docs/decisions/0001-pin-mihomo-commit.md): mihomo source pinning.
