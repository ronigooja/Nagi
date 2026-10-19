# Nagi — Proxy Client for macOS and Linux

[简体中文](README.zh-CN.md)

Nagi is a proxy client for macOS and Linux with a native macOS menu bar app, an interactive terminal menu, and a command-line interface (CLI). Switch proxy nodes, manage subscriptions and profiles, configure routing rules and DNS, and control TUN access from one tool.

Use the macOS menu bar app for daily controls, live traffic rates, and active connections. Use the CLI for advanced configuration, automation, diagnostics, and recovery on either platform.

[Install Nagi](#installation) · [Quick start](#quick-start) · [User guides](#user-guides) · [CLI reference](docs/reference/cli.md)

## Features

- **macOS menu bar app:** engine status, proxy selection, system proxy and TUN controls, profiles, subscriptions, live upload/download rates, and a native Connections window.
- **Terminal menu and CLI:** step-by-step operations, JSON output for scripts and applications, and Bash, Zsh, and Fish completion.
- **Tailscale exits:** manage top-level Tailscale nodes and bind ordinary proxy nodes, entire providers, or a subscription persistently through an exit node, with optional HTTPS Headscale control. Use the [matching pinned engine](docs/guides/tailscale.md#engine-compatibility). See the [Tailscale guide](docs/guides/tailscale.md).
- **Proxy selection:** search nodes, compare latency, save group selections, and add or refresh proxy providers.
- **Subscription management:** cached updates, previews of node changes, scheduled refresh, and optional age-encrypted feeds.
- **Configuration profiles:** import, switch, export, back up, restore, and compare profiles; apply local overrides separately from source configuration.
- **DNS leak prevention:** encrypted DoH, TUN DNS interception, and mihomo resolution of proxied destinations in the default settings; apply those DNS safeguards over a subscription profile while retaining its nodes, groups, and routing rules.
- **Low-latency proxying:** in a macOS-to-Linux LAN VLESS TCP+TLS benchmark, Nagi measured 5.42 ms median for new connections and 0.32 ms for reused requests with the server's default TCP ACK settings. A reference Shadowrocket run measured 53.36 ms and 0.55 ms; the runs took place on different dates and network conditions, so these figures are indicative only. See the [benchmark notes](docs/development/benchmarks.md).
- **Request rewrite:** CLI and terminal forms for private per-profile authorized domains, ordered HTTP/HTTPS rules, no-network previews, explicit CA/trust management and supervised TUN-only activation. A native macOS management window has a source implementation, with native build and acceptance unverified. Requires the [reviewed local development engine](docs/development/build-and-test.md#request-rewrite-coordinator); the release pin lacks the capability. See the [verification matrix](docs/development/request-rewrite-acceptance.md).
- **Routing and DNS:** custom rules, imported rule sets, domain/IP rule tests, rule/global/direct modes, DNS-over-HTTPS (DoH) policy, and domain-specific DNS exceptions.
- **Traffic access:** macOS system proxy settings, TUN controls on macOS and Linux, and proxy listener and LAN access settings.
- **Monitoring and recovery:** connection inspection and closure, live logs, read-only diagnostics, and login startup services.

The CLI supports macOS and Linux on AMD64 and ARM64. The optional menu bar app requires macOS 13 or newer. See the [traffic access guide](docs/guides/traffic-access.md) for platform permissions and routing behavior.

## Installation

Install the CLI from [GitHub Releases](https://github.com/ronigooja/Nagi/releases):

```sh
curl -fsSL https://raw.githubusercontent.com/ronigooja/Nagi/main/scripts/install.sh | bash
```

On macOS, install the optional menu bar app through the CLI:

```sh
nagi app install
```

Use `nagi update` to update the CLI, mihomo, and any installed App, `nagi app uninstall` to remove only the App, or `nagi uninstall` to remove Nagi while preserving user data. Add `--purge-data` to `nagi uninstall` to remove that data too.

Release downloads are verified against their archive checksum. The macOS app is unsigned and unnotarized; its first launch may require choosing **Open** from the Finder context menu. See the [installation and update instructions](docs/development/install.md#installed-cli-maintenance) for permissions and update behavior.

## Quick start

After installation, start the engine, check its status, and open the terminal menu:

```sh
nagi start
nagi status
nagi menu
```

The initial `default` profile has no proxy nodes and routes traffic directly. [Import your own configuration or apply a subscription](docs/guides/getting-started.md) to use proxy nodes. Starting the engine leaves OS system proxy settings unchanged; enable macOS system proxy or TUN access using the [traffic access guide](docs/guides/traffic-access.md). TUN requires OS permissions.

Use a name such as `work` for personal profiles and subscriptions; installation and updates refresh the `default` profile template. See the [default profile behavior](docs/development/install.md#install-update-and-remove).

`nagi menu` requires terminal input and output. Use `nagi --help` to explore commands or `nagi --json status` for structured output. See the [terminal menu reference](docs/reference/cli.md#terminal-menu) for interaction details.

## Build from source

Requirements: Go 1.24.0 or newer and Git. Build the CLI and its pinned proxy engine from the repository root:

```sh
./scripts/build.sh
./bin/nagi --json version
```

If a matching `bin/mihomo` is already available, `make build cli` builds only the CLI. On macOS, `make build app` builds the menu bar app separately. See [Build and test](docs/development/build-and-test.md) for pinned checkout selection and [Installation](docs/development/install.md#install-update-and-remove) for local artifact installation.

Nagi uses [mihomo](https://github.com/MetaCubeX/mihomo) for proxy protocols, DNS resolution, routing, and traffic forwarding. Nagi manages configuration and process lifecycle through a private Unix Socket control API. The matching source commit is recorded in [`engine.lock`](engine.lock); `NAGI_MIHOMO_BIN` overrides the runtime executable.

## Documentation

### User guides

- [Getting started](docs/guides/getting-started.md): initial configuration and operation.
- [macOS menu bar app](macos/NagiApp/README.md): menu controls, traffic display, installation, and packaging.
- [Traffic access](docs/guides/traffic-access.md): system proxy, TUN, listener ports, and LAN access.
- [Tailscale exits](docs/guides/tailscale.md): keys, node/provider bindings, persistent subscription bindings, activation, and identity persistence.
- [Proxy providers](docs/guides/proxy-providers.md): add a node source, attach it to a group, and refresh its nodes.
- [Rules](docs/guides/rules.md): active rules, custom rules, and rule sets.
- [Startup and recovery](docs/guides/startup.md): login service, stale state, and rechecks.
- [Diagnostics](docs/guides/diagnostics.md): redacted health and security checks.

### Reference and design

- [CLI reference](docs/reference/cli.md): commands, output, and exit codes.
- [DNS configuration](docs/reference/cli.md#dns-policy-and-checks): DoH policy, domain exceptions, and configuration checks.
- [macOS privileged TUN helper](docs/reference/cli.md#traffic-access): installation and backend selection for TUN access.
- [Architecture](ARCHITECTURE.md): system boundaries and module relationships.
- [macOS connections window](docs/design/connections-window.md): snapshot scheduling, cancellation, and operation coordination.
- [Terminal menu design](docs/design/terminal-menu.md): current menu responsibilities, navigation, command dispatch, and failure behavior.
- [Runtime design](docs/design/runtime.md): process, socket, configuration, and recovery behavior.
- [Request rewrite CLI](docs/reference/cli.md#tun-request-rewrite), [design](docs/design/request-rewrite.md) and [decision](docs/decisions/0010-tun-request-rewrite.md): implemented CLI/coordinator and terminal forms, plus source-reviewed macOS controls, against a local development engine; [native acceptance and release integration remain pending](docs/development/request-rewrite-acceptance.md).
- [Architecture decision](docs/decisions/0001-pin-mihomo-commit.md): mihomo source pinning.
- [Restricted root configuration decision](docs/decisions/0003-restricted-root-configuration.md): privileged helper and mihomo validation boundary.
- [Per-profile rules decision](docs/decisions/0008-per-profile-rules.md): isolated rule ownership and subscription validation.
- [Persistent subscription Tailscale decision](docs/decisions/0007-persistent-subscription-tailscale.md): feed updates, durable binding intent, and native engine composition.
- [Privileged Tailscale state decision](docs/decisions/0006-privileged-tailscale-state.md): persistent identity isolation and adapter handoff.
- [macOS TUN DNS decision](docs/decisions/0004-macos-tun-dns.md): system DNS ownership, restoration, and proxy destination resolution.

### Development and releases

- [Build and test](docs/development/build-and-test.md): locked builds and tests.
- [Request rewrite verification](docs/development/request-rewrite-acceptance.md): local software evidence, platform gaps and release gates.
- [Installation and updates](docs/development/install.md): GitHub Release installation, local artifacts, updating, and removal.
- [CLI usability rules](docs/development/cli-usability.md): command help, human output, actionable errors, and compatibility checks.
- [Release process](docs/development/release.md): version rules, release checks, and release note requirements.
- [Linux privileged helper scaffold](docs/development/privileged-linux.md): system service boundary and current availability.
- [Feature roadmap](docs/development/roadmap.md): planned user-facing capabilities.
