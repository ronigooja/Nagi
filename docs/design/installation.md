# Installed CLI maintenance

This document describes current release download, App management, and whole
installation maintenance. The [CLI reference](../reference/cli.md#installation-maintenance)
owns command syntax and JSON contracts; the [installation workflow](../development/install.md)
owns user steps and artifact installer options.

## Responsibilities and interfaces

`internal/installation` locates the installer record beside the running
`PREFIX/bin/nagi`, selects a GitHub Release archive, verifies its checksum,
extracts bounded regular files, and orchestrates existing CLI lifecycle and
service operations. It does not configure mihomo protocols, DNS, or forwarding.
The terminal menu calls ordinary CLI commands. The macOS App calls only the CLI
JSON interface and retains its existing normal launch/quit responsibilities.

`scripts/installer.sh` owns file integrity validation, staged replacement,
privileged-helper changes, and rollback. The CLI embeds this same source through
the `scripts` Go package for local verification, App-only operations, and
uninstall. Whole updates run the installer contained in the verified release so
its file transaction agrees with the candidate version. The existing shell
online installer remains a separate first-install and recovery entry point.

The installer record remains `nagi-installer-v3`: App path, CLI SHA-256, mihomo
SHA-256, and App bundle digest. App-only operations preserve the binary hashes
and replace only the App fields. The App digest covers content, directories,
and link layout using the same implementation as ordinary artifact installation.

## Download and preparation

CLI maintenance operations acquire a nonblocking per-user `maintenance.lock`
under the OS user cache directory before downloading or changing the
installation. They reread the installation record after acquiring the lock.
The lock serializes CLI maintenance for that user; direct shell installation
and other users are outside this lock.

A private temporary directory holds `SHA256SUMS`, the platform archive, and
extracted artifacts. Checksum verification precedes extraction and execution.
Extraction bounds total bytes and entry count, refuses escape paths, duplicate
files, links, and special files, and requires regular executable CLI, engine,
and installer files. The package App must exist when requested or already
recorded; an optional packaged App is discarded for a CLI-only update.

The embedded installer performs read-only `--verify` validation of current
managed hashes, App layout, and candidate executables before the CLI prepares
runtime state. Verification does not require the engine to be stopped or its
login service to be removed. Downloads and verification failures leave runtime
and service state untouched.

## Transactions and recovery

App installation/removal uses `app-install`/`app-uninstall` in the embedded
installer. It refuses a running App, stages changes beside the destination and
installation record, updates both, and restores prior state on failure. It
does not change CLI/engine files, profiles, services, or running engine state.
An incomplete rollback reports and retains backup directories for repair.

Whole update/removal refuses a running recorded App and snapshots engine
running state and login service installed/enabled state through the installed
CLI. It invokes `quit`, then `stop` to record a durable stopped intent, then
removes an installed login service. The explicit stop prevents a restored login
monitor from starting mihomo during maintenance. The installer then performs
the existing file/helper transaction.

After a whole update succeeds, the CLI restores login service installed/enabled
preferences and starts an engine that was running before preparation. A whole
uninstall succeeds without restoring services. User data is retained; updates
refresh the default profile as in the artifact workflow. OS proxy/DNS restore
journals are processed by existing stop behavior. Temporary runtime settings
are not replayed automatically.

If installation fails after preparation, the installer reports its attempted
rollback and retains backups when rollback is incomplete. The CLI attempts to
restore login service preferences, leaves engine intent stopped, and returns
both installation and restoration errors. A service-removal failure before the
transaction leaves Nagi stopped and reports that step. A successful file update
with a failed service restore or engine restart reports partial completion and
the relevant CLI recovery command. The CLI never describes a failed transaction
as successfully rolled back merely because it attempted restoration.

## App update handoff

The App's About Nagi panel shows its bundle version and an Update Nagi button.
That button submits `update --terminal` and requests its normal
quit only after the CLI confirms Terminal launch. The CLI passes a quoted shell
command as an AppleScript argument to Terminal; neither resource paths nor
downloaded content are interpolated as AppleScript source. Terminal waits up
to 60 seconds for App exit, runs the installed CLI's whole update, and opens the
recorded App path only after success. Administrator authorization and update
failures appear in Terminal. The CLI's launch response describes scheduling,
not installation completion.

A Terminal launch failure keeps the App open with an actionable error. A quit
cleanup failure retains the existing App menu and is displayed there; the
waiting terminal command eventually cancels. A failed update keeps the App
closed and leaves the terminal report available for recovery. A successful
update opens the App through its ordinary launch behavior, which starts Nagi.

## Verification

Isolated Go integration tests exercise verified whole updates, preflight and
checksum failures before runtime changes, stopped intent, service preference
restoration, removal without downloading, archive validation, and App-only
rollback. Command/menu tests check help, argument validation, JSON errors,
completion discovery, and dispatch. Swift tests cover CLI scheduling before
quit and launcher failure without quitting. Platform simulation in installer
tests verifies file transactions; administrator authorization, launchd helper
replacement, Terminal automation permission, and visible menu interaction
still require native interactive macOS acceptance.
