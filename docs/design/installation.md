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

`scripts/installer.sh` owns managed path validation, operation sequencing,
privileged-helper changes, and rollback. The CLI's private
`__install-transaction` entry point delegates durable filesystem changes to
`internal/installtx` before ordinary runtime configuration is loaded. The CLI embeds this same source through
the `scripts` Go package for local verification, App-only operations, and
uninstall. Whole updates run the installer contained in the verified release so
its file transaction agrees with the candidate version. The existing shell
online installer remains a separate first-install and recovery entry point.

New installations use `nagi-installer-v4`, which records only the managed App
path. The installer accepts an existing v3 record once so update or App-only
maintenance can replace it with v4; removal deletes either version. No file
digest is used to authorize maintenance.

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

The embedded installer performs read-only `--verify` validation of the
installation record, managed path types, and candidate executables before the
CLI prepares runtime state. Verification does not require the engine to be
stopped or its login service to be removed. Downloads and verification failures
leave runtime and service state untouched.

Whole updates and removal accept changed contents at recorded paths. An update
replaces those files; removal deletes them. App-only operations also accept
changed recorded contents. Missing or invalid records and symbolic links
remain errors.

## Transactions and recovery

The independent journal at `PREFIX/.nagi-transaction` records the complete file
operation list and keeps durable copies of the previous and candidate contents.
It does not depend on `share/nagi/install-state`, which can itself be absent
partway through replacement or removal. A per-prefix filesystem lock serializes
mutating installer invocations, including direct shell invocations. The CLI's
per-user maintenance lock additionally covers downloads and runtime preparation.

Before publishing a prepared journal, the helper synchronizes staged files,
backup files, their directories, and the journal. Each destination mutation is
followed by synchronization of its parent directory. A separately synchronized
commit marker decides recovery: a transaction without that marker restores the
previous files; a committed transaction keeps the installed result and finishes
cleanup. Recovery copies backups instead of consuming them so another
interruption during rollback can be recovered again. Journal cleanup first
retires the complete journal by rename and synchronizes its parent; removing
retired files cannot turn a committed transaction back into an uncommitted one.
A crash during deletion can leave a retired garbage directory; it is no longer
an active recovery journal and cannot authorize rollback. A cleanup failure
after durable commit reports that the files were committed and cleanup remains;
it must not trigger file rollback or privileged-helper compensation.
macOS uses `F_FULLFSYNC` for regular file durability; Linux uses `fsync`.

The durable journal choice and its trade-offs are recorded in
[decision 0009](../decisions/0009-durable-installation-transactions.md).

The next mutating installer invocation recovers the journal before interpreting
the installer record or beginning another transaction. Installed CLI maintenance
also detects and recovers a pending journal before reading its record. Ordinary
engine start/restart, configuration reload, and startup activation refuse a
pending journal with `installation_recovery_required`; they do not start an
incomplete installation. Read-only App status reports pending recovery instead
of treating a partial record as an authoritative installation. The artifact
installer's explicit `recover` operation can run using candidate artifacts even
when the installed executable is absent. It restores files without automatically
restoring runtime services or starting the engine. `--verify` remains
read-only: it neither recovers an interrupted transaction nor deletes its
journal. Recovery refuses unsafe paths and invalid or damaged recovery data,
returns an error, and preserves the journal for inspection. Operators must not
delete backups to bypass that error. Managed paths, journal paths, and their
existing ancestors must not be symbolic links; staged App trees also refuse
symbolic links and special files.

App installation/removal uses `app-install`/`app-uninstall` in the embedded
installer. It refuses a running App and includes both the App directory and
installer record in one transaction. It does not change CLI/engine files,
profiles, services, or running engine state. The same file transaction machinery
covers whole installation, update, and removal; updates also include the default
profile. Recovery offers an old-or-committed result after it completes, rather
than atomic visibility of every destination to concurrent readers.

Whole update/removal refuses a running recorded App and snapshots engine
running state and login service installed/enabled state through the installed
CLI. It invokes `quit`, then `stop` to record a durable stopped intent, then
removes an installed login service. The explicit stop prevents a restored login
monitor from starting mihomo during maintenance. The installer then performs
the existing file/helper transaction.

For updates, the helper copies the old CLI without moving the installed
executable, then renames the staged replacement over `PREFIX/bin/nagi`. On Linux,
a still-running CLI therefore retains its installed executable path even after
the old inode is unlinked. Companion mihomo lookup and later CLI invocations
continue to use `PREFIX/bin`. Backups preserve permissions and ownership for
restoration. Filesystem transaction recovery does not replay service-manager,
privileged-helper activation, or engine lifecycle calls; those remain the
orchestrator's responsibility and failures require the reported recovery steps.

After a whole update succeeds, the CLI restores login service installed/enabled
preferences and starts an engine that was running before preparation. A whole
uninstall succeeds without restoring services. User data is retained by
default; `uninstall --purge-data` asks the installer to remove the invoking
user's configuration, data, state, runtime, and cache directories after the
managed installation transaction commits. A purge failure is reported after
the installation files have already been removed, with an explicit message that
some user data may remain; the CLI does not attempt to reinstall the removed
login service. Updates refresh the default profile as in the artifact
workflow. OS proxy/DNS restore journals are processed by existing stop
behavior. Temporary runtime settings are not replayed automatically.

If installation fails after preparation, the installer reports its attempted
rollback and retains backups when rollback is incomplete. The CLI attempts to
restore login service preferences, leaves engine intent stopped, and returns
both installation and restoration errors. A service-removal failure before the
transaction leaves Nagi stopped and reports that step. A successful file update
with a failed service restore or engine restart reports partial completion and
the relevant CLI recovery command. The CLI never describes a failed transaction
as successfully rolled back merely because it attempted restoration.

## App update handoff

The App's About Nagi panel shows its bundle version and an Update button.
The button shows a short informational prompt, then submits `update --app`.
There is no download or installation progress display. The CLI acquires the
per-user maintenance lock, snapshots engine running state, downloads and verifies
the release, and performs read-only installer preflight while the App is still
running. Release downloads use the current mihomo mixed or HTTP listener
reported by `ports status` when available; otherwise they use the ordinary HTTP
environment or TUN route. An authenticated listener that refuses the local
request causes preparation to fail before quit. It launches the detached updater
and waits for any required native administrator authorization before reporting
a scheduled result. That result confirms preparation and handoff, not installation
completion. Only then does the App request its normal quit cleanup.

The CLI bounds the entire preparation request to 14 minutes and handles `SIGINT`
and `SIGTERM` as cancellation before handoff. The App allows 15 minutes for the
CLI call. Cancellation stops the pending updater process group, cleans up the
private preparation artifacts and authorization adapter, and releases the
maintenance lock while keeping the App open and preserving engine and service
state. Authorization child
processes do not retain the maintenance-lock descriptor. After successful
handoff, the detached updater owns the operation independently of the
preparation context.

The handoff transfers the private prepared artifacts and maintenance-lock file
descriptor to the detached updater. It waits up to 60 seconds for App exit,
then invokes the installed CLI's whole-update operation using the prepared
package rather than downloading again. The saved engine running state allows
ordinary successful-update restoration after App quit has stopped the engine.
The updater retains the lock throughout the wait and transaction and cleans
up its temporary artifacts and authorization adapter on exit.

The detached updater creates a private temporary `sudo` adapter and prepends
its directory to its own `PATH`. Before handoff completes, it starts one
update-scoped worker through AppleScript `do shell script ... with administrator
privileges` when protected installation paths or the privileged helper require
authorization. Installer operations are forwarded to that worker, so the update
normally needs one administrator authorization. macOS handles credentials;
Nagi does not collect passwords. Downloads, verification, lifecycle management,
and user configuration remain unprivileged. The adapter passes the invoking
user's `SUDO_UID`, `SUDO_GID`, and `SUDO_USER` to privileged commands so helper
installation retains the correct session owner, and sets a system-only
privileged `PATH`. Shell and AppleScript arguments are quoted separately.

Download, preflight, authorization, and background-launch failures keep the App
open with an error at the top of its menu. A quit cleanup failure also retains
the App and its error; the waiting updater eventually cancels. After installation
succeeds, the updater opens the recorded App through its ordinary launch
behavior, which starts Nagi. After installation fails, the CLI persists a
sanitized error and the updater attempts to reopen the recorded App for
recovery, including the restored bundle when rollback succeeds. If reopening
fails, the per-user update log remains available for manual recovery.

The CLI owns the pending failure at `~/Library/Caches/Nagi/update-error` and
returns and clears it through `app update-error`, without requiring an
installation record or running engine. Its JSON result contains `error`, an
empty string when no failure is pending. The App reads this once on startup and
retains the message across normal startup and refresh results; the next explicit
Update attempt clears its displayed update error. The App does not read the
pending file or update log directly. The detached updater appends operation and
reopen output to `~/Library/Caches/Nagi/update.log`.

## Verification

Isolated Go integration tests exercise verified whole updates, preflight and
checksum failures before runtime changes, stopped intent, service preference
restoration, removal without downloading, archive validation, and App-only
rollback. Command/menu tests check help, argument validation, JSON errors,
completion discovery, and dispatch. Swift tests cover CLI preparation before
quit, preparation failure without quitting, and persisted failure retention
across startup refreshes. Platform simulation in installer tests verifies file
transactions. Transaction tests inject abrupt process death at preparation,
destination replacement, commit, rollback, and cleanup boundaries, and inject
synchronization failures. Integration checks invoke the production helper in
separate processes and verify that shell recovery works when the installer
record is missing; read-only verification leaves the interrupted journal intact.
These tests validate process-crash recovery and synchronization ordering. They
do not simulate loss of volatile storage caches or prove physical power-loss
durability. The [installation verification workflow](../development/install.md#durability-verification)
describes the separate VM power-cut acceptance, which has not been run in this
Linux development environment. Administrator authorization, launchd helper
replacement, Terminal automation permission, and visible menu interaction also
require native interactive macOS acceptance.
