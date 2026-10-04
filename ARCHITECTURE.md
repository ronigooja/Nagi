# Nagi Architecture Implementation Plan

## 1. Goals and Scope

Nagi is a mihomo management tool centered on a CLI:

- The CLI supports macOS and Linux and provides the complete feature set.
- SwiftUI supports macOS only and handles common operations and status display.
- mihomo uses the `Meta` branch of https://github.com/MetaCubeX/mihomo.git as its upstream.
- Custom mihomo changes are maintained in `/root/mihomo` and are not copied into the Nagi repository.
- SwiftUI does not access the mihomo API, configuration files, or Unix Socket directly; it always invokes the Nagi CLI.

System relationship:

    SwiftUI macOS
          │ Process + JSON
          ▼
    Nagi CLI (macOS / Linux)
          │
          ├── mihomo process lifecycle management
          ├── mihomo REST API client
          ├── configuration, Profile, subscription, and status management
          └── launchd / systemd integration
                │ Unix Domain Socket
                ▼
          mihomo Meta (custom branch)

Nagi does not reimplement proxy-core capabilities. mihomo continues to provide proxy protocols, DNS, rules, and connection handling; Nagi manages startup, configuration, subscriptions, node selection, and the user interface.

## 2. Repository Boundaries

### `/root/mihomo`

The mihomo fork is responsible for:

- Tracking the official `Meta` branch;
- Keeping mihomo core customizations;
- Building the mihomo executable;
- Recording customization behavior and mihomo-specific configuration.

Suggested branches:

    upstream/Meta       Official upstream branch
    nagi/meta            Custom branch used by Nagi
    fork                 Personal remote repository

CLI, SwiftUI, launchd, and systemd logic does not belong in mihomo. Only changes that must alter mihomo core behavior enter that repository.

### `/root/Nagi`

The Nagi product repository is responsible for:

- The Nagi CLI;
- mihomo process management;
- mihomo REST API calls;
- Profile, subscription, and node management;
- The macOS SwiftUI application;
- launchd, systemd, and distribution packages;
- Product-level testing and release workflows.

Nagi pins the mihomo repository, branch, and commit through `engine.lock`.

## 3. Recommended Directory Layout

    Nagi/
    ├── cmd/nagi/main.go             # Program entry point
    ├── internal/
    │   ├── app/                     # Dependency assembly, startup, and shutdown
    │   ├── command/                 # CLI commands, argument parsing, and output
    │   ├── control/                 # REST API / Unix Socket client
    │   ├── engine/                  # Process, PID, logs, and exit status
    │   ├── profile/                 # Profiles and YAML configuration
    │   ├── subscription/            # Subscription CRUD, refresh, and cache
    │   ├── proxy/                   # Proxy groups, nodes, connections, and selection state
    │   ├── runtime/                 # Socket, PID, lock, and runtime directory
    │   ├── output/                  # Human-readable and JSON output
    │   ├── schema/                  # CLI JSON schema
    │   └── platform/                # macOS / Linux differences
    ├── macos/NagiApp/               # SwiftUI project
    ├── packaging/launchd/
    ├── packaging/systemd/
    ├── scripts/
    ├── docs/
    ├── engine.lock
    ├── Makefile
    └── go.mod

`command` handles only arguments, business calls, and output; business logic belongs in feature packages such as `profile`, `subscription`, `proxy`, and `engine`. `control` and `engine` must remain separate: the former calls a running mihomo instance, while the latter starts, stops, and monitors the process.

## 4. mihomo Upstream and Customization Synchronization

Add an upstream remote in `/root/mihomo`:

    cd /root/mihomo
    git remote rename origin fork
    git remote add upstream https://github.com/MetaCubeX/mihomo.git
    git fetch upstream Meta

Base the customization branch on upstream `Meta`:

    git switch -c nagi/meta upstream/Meta

If the current branch already contains customization commits, preserve them and organize them on `nagi/meta`. Keep each customization in an independent commit, for example:

    fix: preserve proxy destination hostname
    feat: add preserve-proxy-hostname option

Synchronize upstream:

    git fetch upstream Meta
    git switch nagi/meta
    git rebase upstream/Meta

Classify customizations into three categories:

1. Fixes that generally improve mihomo should be submitted upstream when possible;
2. mihomo behavior required by Nagi but unsuitable for upstream stays on `nagi/meta`;
3. CLI, SwiftUI, subscription menus, service management, and release logic belong in Nagi.

Whenever mihomo customization behavior changes, update `docs/customizations.md` in the mihomo repository. Nagi does not duplicate mihomo customization documentation.

## 5. Version Pinning and Builds

The root-level `engine.lock` records the exact dependency:

    repository: https://github.com/your-org/mihomo.git
    ref: nagi/meta
    commit: 0000000000000000000000000000000000000000

A local checkout may be used during development:

    MIHOMO_DIR=/root/mihomo make build

CI and release builds locate mihomo in this order:

1. `MIHOMO_DIR`;
2. Download and check out the exact commit from `engine.lock`;
3. Fail if the exact commit cannot be obtained.

`nagi version` should display the Nagi version, mihomo version, mihomo commit, operating system, and architecture.

## 6. CLI Design

The CLI is the complete feature entry point:

    nagi start
    nagi stop
    nagi restart
    nagi status
    nagi logs

    nagi config validate
    nagi config show

    nagi profile list
    nagi profile use <name>

    nagi subscription list
    nagi subscription add <name> <url>
    nagi subscription update <name>
    nagi subscription remove <name>

    nagi proxy groups
    nagi proxy show <group>
    nagi proxy select <group> <node>
    nagi connections list

    nagi service install
    nagi service uninstall
    nagi version

Every command supports human-readable and machine-readable output:

    nagi status
    nagi --json status
    nagi --json proxy groups

JSON output is the stable interface for SwiftUI. Example success response:

    {
      "ok": true,
      "data": {
        "running": true,
        "version": "mihomo-meta",
        "mixed_port": 17890
      }
    }

Errors use a non-zero exit code and emit stable `code` and `message` fields. SwiftUI depends only on JSON; it does not parse tables, log text, or YAML.

## 7. mihomo Process and Control Interface

Nagi prefers a Unix Domain Socket when starting mihomo:

    macOS: ~/Library/Application Support/Nagi/runtime/mihomo.sock
    Linux: $XDG_RUNTIME_DIR/nagi/mihomo.sock

A TCP control port is not exposed by default. Configure `external-controller` only when the user explicitly needs remote management.

`engine` creates the runtime directory, validates configuration, starts the child process, saves the PID, collects logs, checks liveness, stops and restarts the process, and prevents duplicate starts. `control` handles `version`, `configs`, `proxies`, `connections`, subscription refresh, configuration reload, timeouts, and error conversion.

The first version does not implement unlimited automatic restarts. After an unexpected exit, it displays a clear status and recent logs to avoid restart loops caused by invalid configuration.

## 8. Configuration, Profiles, and Runtime Data

Configuration, cache, and runtime state are separated:

    Application Support/Nagi/
    ├── config/
    │   ├── profiles/default.yaml
    │   ├── subscriptions.yaml
    │   └── settings.yaml
    ├── runtime/
    │   ├── mihomo.sock
    │   ├── mihomo.pid
    │   └── logs/
    ├── cache/subscriptions/
    └── state/selected-nodes.json

Linux uses the `nagi` directory under `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, and `XDG_RUNTIME_DIR`. macOS uses `~/Library/Application Support/Nagi`.

Every configuration write:

1. Reads and validates the old configuration;
2. Writes a temporary file;
3. Atomically replaces the target file;
4. Keeps the most recent backup;
5. Asks mihomo to validate or reload the configuration.

SwiftUI does not write configuration files directly.

## 9. SwiftUI Scope

SwiftUI provides only:

- Current runtime status;
- Start, stop, and restart;
- Current Profile;
- Proxy-group and node switching;
- Subscription refresh;
- A simple connection list;
- Recent logs;
- Basic settings.

Full YAML editing, rule-set conversion, advanced DNS, batch subscriptions, debugging and diagnostics, and scripting remain in the CLI.

SwiftUI invokes:

    nagi --json status
    nagi --json proxy groups
    nagi --json subscription update <name>

The first version polls status with short-lived commands. For real-time updates, add `nagi events --json`, with the CLI emitting events through stdout.

## 10. Background Services

macOS uses a per-user launchd Agent; Linux uses a `systemd --user` Service. The CLI generates service files:

    nagi service install
    nagi service uninstall

SwiftUI does not operate launchd or systemd directly; it only calls `nagi start`, `nagi stop`, and `nagi status`.

## 11. Security Requirements

- Use only a Unix Socket by default; do not listen on public addresses;
- Restrict Socket file permissions to the current user;
- Do not write control-interface secrets to logs;
- Do not output subscription URLs to ordinary logs;
- Use temporary files and atomic replacement for configuration writes;
- Validate Profiles, paths, and external command arguments;
- SwiftUI executes only a fixed Nagi CLI path and never constructs unvalidated shell commands.

## 12. Testing Boundaries

Nagi unit tests cover path resolution, engine state, atomic Profile writes, subscription merging, JSON schema, error codes, and CLI arguments.

mihomo integration tests cover startup with a minimal configuration, Unix Socket, `version`/`configs`/`proxies`, node switching, configuration reload, and unexpected exits.

SwiftUI validation covers a missing CLI, a stopped mihomo instance, JSON schema compatibility, start and stop, node switching, and subscription refresh.

## 13. Implementation Phases

### Phase 1: CLI Skeleton

Initialize the Go module; implement paths and runtime directories; add `engine.lock`; implement `version`, `status`, `start`, and `stop`; compile and start `/root/mihomo`.

### Phase 2: mihomo Control Layer

Implement a Unix Socket HTTP client; connect `version`, `configs`, `proxies`, and `connections`; standardize error codes and JSON output; add configuration validation and secure writes.

### Phase 3: Complete CLI

Implement Profiles, subscriptions, proxy groups, nodes, connections, logs, diagnostics, and launchd/systemd service installation.

### Phase 4: SwiftUI

Create the macOS app; implement `NagiCLIClient`; implement Dashboard, Proxies, Subscriptions, and Settings; add status polling and error handling.

### Phase 5: Release

Build macOS universal binaries and Linux amd64/arm64 binaries; associate the mihomo version; complete macOS signing and notarization, service-file packaging, and locked CI builds.

## 14. Long-Term Constraints

1. The CLI is the sole complete-feature entry point;
2. SwiftUI may call only the CLI and must not duplicate business logic;
3. Nagi does not modify mihomo source code directly;
4. mihomo core customizations exist only in `/root/mihomo`;
5. Nagi does not depend on an unpinned mihomo commit;
6. Use Unix Socket by default and do not expose a TCP control port by default;
7. The CLI and graphical interface use the same business implementation.
