# Install and update

This document defines the end-user GitHub Releases workflow and the local artifact workflow used by development builds. The online wrapper downloads and verifies a release archive, then delegates installation to `scripts/installer.sh`. Neither workflow builds, signs, or notarizes files.

## Online install and update

Published releases use fixed GitHub asset names. The online wrapper selects the
matching operating system and architecture, downloads the archive and
`SHA256SUMS` from the latest release, verifies the archive, and invokes the
local installer. GitHub is the primary distribution service; if a download
fails, the wrapper retries the archive and checksum file through the third-party
`https://gh-proxy.com/` proxy. No Nagi download server is required.

The online wrapper supports the system Bash 3.2 on macOS and newer Bash
versions on Linux. It compares SHA256 digests case-insensitively without
requiring a newer Bash installation.

Install the CLI with the repository's raw script:

```sh
curl -fsSL https://raw.githubusercontent.com/ronigooja/Nagi/main/scripts/install.sh | bash
```

If the script itself is unreachable, use the proxy entry point:

```sh
curl -fsSL https://gh-proxy.com/https://raw.githubusercontent.com/ronigooja/Nagi/main/scripts/install.sh | bash
```

The wrapper's `NAGI_RELEASE_BASE` environment variable overrides the complete
release download directory (containing the archive and `SHA256SUMS`). An explicit
override is used exclusively, without automatic fallback to the public proxy.

Each source is attempted once. The shell wrapper uses a 10-second connection
timeout and aborts a transfer that stays below 100 KiB/s (102,400 bytes per second) for 10 seconds;
there is no total download time limit. It prints download, fallback, checksum,
extraction, and installation stages. CLI and menu update timing and live stage
output are defined in the [CLI reference](../reference/cli.md#installation-maintenance).

After installing the CLI on macOS, add the optional unsigned menu bar App:

```sh
nagi app install
```

Run the same command again to update an existing installation, or make the
operation explicit:

```sh
nagi update
```

The online wrapper accepts `--version vX.Y.Z`, `--repo OWNER/REPOSITORY`, and
`--prefix DIR`. A fresh macOS installation is CLI-only unless `--app` is
provided. An update keeps an App that was already installed; `--no-app` keeps
the same behavior explicitly. Updating and uninstalling still use the local
installer's preflight checks: stop mihomo, quit Nagi.app, and remove the login
service before replacing managed executables.

The macOS App is unsigned and unnotarized. macOS may require the user to
right-click the App and choose **Open** on its first launch. This does not
change the CLI or mihomo installation path.

## Installed CLI maintenance

Use the release-managed CLI to install or remove the menu bar App and update or
remove the whole installation. The [CLI reference](../reference/cli.md#installation-maintenance)
defines syntax, release selection, JSON results, and errors. Local development
binaries without an installer record must use the local artifact workflow below.

On macOS, `nagi app install` downloads the release matching the CLI version and
installs only the App. It leaves mihomo running and preserves the CLI, engine,
login service, and profiles. The CLI must be installed at `/usr/local/bin/nagi`,
the path compiled into the App. Open the installed App from Applications.
The terminal menu offers whole-installation update and removal under
**Maintenance** on macOS and Linux. On macOS,
App-only installation and removal are also listed there. The menu groups the
uninstall actions together and offers both data-preserving removal and removal
with data purge.

`nagi update` downloads the latest matching platform release and verifies its
checksum, installation record, paths, and candidate executables before stopping
anything. It updates the CLI, mihomo, and an already installed App together.
It refreshes the `default` profile, preserves other user data, temporarily
removes an installed engine login service, and restores its installed/enabled
preferences afterward. A previously running engine is restarted after success.
OS settings managed by Nagi are restored when the engine stops; runtime TUN,
system proxy, and other temporary settings must be enabled again as needed.

CLI release downloads, including App installation and background updates, try
GitHub first and retry the entire archive/checksum pair through `gh-proxy.com`
after a network, HTTP-status, or response-read failure. Cancellation, checksum
mismatches, invalid archives, and local filesystem failures do not trigger a
source switch. Both sources retain SHA256 verification against the downloaded
checksum file; this is an integrity check, not independent publisher authentication.
Text CLI and terminal menu archive downloads refresh size and speed on one line
every second on capable terminals, with total size and percentage when supplied
by the server. Redirected output remains line-oriented. Throughout a transfer,
a rolling 10-second average below 256 KiB/s triggers fallback when another source
is available. The final source continues downloading without a minimum-speed
cutoff; network errors and cancellation still apply.
The proxy's availability and cache freshness are outside Nagi's control.

An already-open terminal menu retains access to the installed mihomo after a
successful update. Exit and reopen the menu to run the new CLI version. See the
[maintenance design](../design/installation.md#transactions-and-recovery) for
replacement and rollback behavior.

`nagi app update` invokes the same whole-installation update and requires an
installed App.

Quit the App before a shell update. Alternatively, choose **About Nagi → Update**
in the App and acknowledge the short informational prompt. The CLI downloads the release,
verifies its checksum and installation preflight, and obtains native macOS
administrator authorization when needed while the App remains open. Only after
preparation succeeds does the App complete its normal quit cleanup. A detached
updater installs the prepared package and reopens Nagi. The App does not show
download or installation progress. macOS handles credentials; Nagi does not
collect passwords. Downloads use the running engine's mixed or HTTP listener
when available, so proxy nodes remain usable during preparation. If that listener
requires authentication and rejects the download, the App stays open with an
error; correct listener access and retry.

A download, verification, authorization, or updater-launch failure keeps the App
open and displays the error at the top of its menu. A quit cleanup failure also
keeps the App available; the updater cancels if it has not quit within 60 seconds.
If installation fails after exit, the updater records a sanitized error through
the CLI and attempts to reopen the recorded App. The reopened App displays the
error at the top of its menu. If the App cannot be reopened, use the per-user
update log for recovery. See the [App update design](../design/installation.md#app-update-handoff)
for the handoff and failure handling.

Use `nagi app uninstall` to remove only the App, or `nagi uninstall` to stop Nagi
and remove its managed CLI, mihomo, App, login service, and privileged helper.
Both preserve user profiles, subscriptions, settings, and logs by default. Add
Choose **Uninstall Nagi and clear user data** in the terminal menu, or add
`--purge-data` to `nagi uninstall`, to also remove Nagi's per-user configuration,
data, state, runtime, and cache directories. On macOS this removes
`~/Library/Application Support/Nagi` and `~/Library/Caches/Nagi`. On Linux it
removes the Nagi directories under `XDG_CONFIG_HOME`, `XDG_DATA_HOME`,
`XDG_STATE_HOME`, `XDG_RUNTIME_DIR` (when set), and `XDG_CACHE_HOME`, using the
standard home-directory defaults when a variable is unset or is not a safe
absolute directory.
Review those directories before purging; removal cannot be undone. Before
removing the App, disable **Open menu bar app at login** and quit it. Removal
does not download a release.

Run maintenance as the intended user, not with `sudo`; the installer requests
administrator authorization for protected files and the macOS helper. Terminal
access is required when authorization is needed. Whole updates replace modified
recorded files; removal deletes them. A failed download, cancellation, or the
2-minute update deadline leaves
the running engine and services untouched. A failed installation reports
installer rollback details and leaves the engine stopped; login service
preferences are restored when possible.
After resolving the reported failure, inspect `nagi status` and run `nagi start`.
If files were updated but service restoration or restart failed, the error says
which step needs recovery. See the [maintenance design](../design/installation.md).

## Prepare artifacts

Place matching executable `nagi` and `mihomo` files in one directory. On macOS, an optional `Nagi.app` bundle in the same directory is installed when present. `--app-dest` changes its destination. `make build cli` creates `bin/nagi`; on macOS, `make build app` creates `bin/Nagi.app`. Both leave an existing `bin/mihomo` untouched. Use `./scripts/build.sh` when the pinned mihomo also needs to be built. Check the files and their architecture before installation. Release archives created by `make release` include this version of `scripts/installer.sh` beside the binaries. A download workflow can verify the archive, then invoke the included installer on its artifact directory; the installer does not fetch releases or verify their provenance.

## Install, update, and remove

The `default` profile is Nagi's built-in configuration template and is refreshed with each installed version. Both `install` and `update` replace `config/profiles/default.yaml` in the invoking user's Nagi configuration directory with that version's built-in profile, so the template receives current default settings. Use a different profile or subscription name, such as `work`, for configuration you want to retain across updates. The CLI accepts the name `default`, but the installer will overwrite any local edits, imported configuration, or subscription content saved there. If you already keep personal configuration in `default`, export it with `nagi profile export default FILE` before installation or update, replacing `FILE` with a new private destination path.

Run from the repository root. Stop Nagi before replacing binaries. On macOS, quit the menu app as well. Remove an installed login service before updating or uninstalling binaries; disabling it alone does not satisfy the installer preflight. With the default `/usr/local` prefix on macOS, the installer installs and selects the privileged TUN helper on a new installation. It retries the helper's authenticated connection briefly after launchd starts the service; if the helper remains unavailable, installation fails and the installer rolls back its changes. On update, it reinstalls an existing helper and preserves its backend selection. On uninstall, it removes the helper and waits for launchd to release the job, including when its definition has already disappeared. If no installer record or managed binaries remain, `uninstall` also removes a leftover helper using the built `bin/nagi`. A new `install` removes a leftover helper before installing the new one. Both operations select the user backend first and require administrator authorization when they find a leftover helper. See [Startup and recovery](../guides/startup.md) for login service operations.

```sh
make build cli
# On macOS, also run: make build app
./scripts/installer.sh install
./scripts/installer.sh update
./scripts/installer.sh uninstall
./scripts/installer.sh uninstall --purge-data
```

For a local artifact update with an installed login service, use the installed
CLI to stop and remove that service before updating (default prefix shown):

```sh
/usr/local/bin/nagi service uninstall
./scripts/installer.sh update
```

This interrupts the current monitor and mihomo. `startup disable` only changes
next-login startup: `Enabled: no` with `Active: yes` still blocks the update.
Even an inactive service definition must be removed. After a successful update,
run `/usr/local/bin/nagi startup enable` if login startup is wanted, then
`/usr/local/bin/nagi start` to resume the engine. Unlike `nagi update`, the
artifact installer does not restore service preferences or restart the engine.
Use the same service-removal step before an artifact uninstall.

The installer uses existing artifacts and never compiles anything. Build the CLI and macOS App explicitly before installing or updating. As a development example, `./scripts/sync_to_mac.sh` copies source files to a configured remote checkout and leaves remote `.git`, `bin/`, `dist/`, and build caches intact. Set the destination in your local shell, or save it in the ignored `.nagi-sync.local` file at the repository root, then run the script:

```sh
NAGI_MAC_REMOTE='user@mac-host' \
NAGI_MAC_REMOTE_DIR='/path/to/Nagi' \
./scripts/sync_to_mac.sh
```

It replaces remote source files, including local edits, and removes remote source files absent locally. `install` and `update` use the repository `bin/` directory by default and require both executables; `uninstall` does not require build artifacts. `update` replaces the installed executables using the same artifact layout as `install`. After installing or updating, check the installed version with `/usr/local/bin/nagi --json version` and confirm the companion `mihomo` is the intended pinned build.

| Option | Meaning |
| --- | --- |
| `--artifact-dir DIR` | Directory containing executable `nagi` and `mihomo`, plus optional `Nagi.app`. Defaults to the repository `bin/` directory. |
| `--prefix DIR` | Directory prefix for installed CLI binaries; defaults to `/usr/local`, giving `/usr/local/bin/nagi` and `/usr/local/bin/mihomo`. Use the same prefix for later updates and removal. |
| `--app-dest PATH` | Override the macOS App destination (default `/Applications/Nagi.app`). The installer installs an App whenever the artifact directory includes `Nagi.app` and remembers its destination for updates and removal. |
| `--purge-data` | With `uninstall` only, remove the invoking user's Nagi configuration, data, state, runtime, and cache directories after removing the managed installation. |

The macOS app invokes `/usr/local/bin/nagi` directly. Keep the default prefix when installing the app; an alternate prefix does not change the path compiled into the app. Install the app in Applications before enabling “Open menu bar app at login.” `--app-dest` cannot enable App installation without a `Nagi.app` in the artifact directory. On update, an installation that includes the App requires an updated App in the artifact directory. A CLI-only installation gains the App when the update artifact contains it and the destination is free.

An interrupted file transaction is recovered automatically by the next mutating
installer invocation for the same prefix, before reading the installer record.
Until recovery completes, some installed paths may reflect the interrupted
operation. If the installed CLI cannot run, use a verified release's installer
and runnable artifacts for the same platform, with the original `--prefix`.
For example, from a development checkout with matching binaries in `bin/`:

```sh
./scripts/installer.sh recover --prefix /usr/local --artifact-dir ./bin
```

A release package's installer accepts the same `recover` action and artifact
directory layout. Recovery restores the previous files for an uncommitted
transaction and keeps committed files; it does not automatically restore the
login service or restart the engine after a hard crash. Inspect `nagi status`
and `nagi service status`, then restore the intended service preference and run
`nagi start` as needed. This installer action differs from `nagi recover`, which
only cleans stale runtime markers. Do not manually reconstruct `install-state`.
Ordinary engine activation refuses a pending journal with
`installation_recovery_required`. Read-only `--verify`
does not perform recovery. A corrupt journal, unsafe path, failed synchronization,
or incomplete rollback stops maintenance and reports retained recovery data.
Preserve `PREFIX/.nagi-transaction` and its backups until the reported problem
is resolved; deleting them can destroy the only recoverable previous version. If
commit succeeded but cleanup failed, the new files remain installed and the
error reports pending cleanup; do not assume the previous version was restored.
A cleanup interruption may leave a retired garbage directory beside the journal;
it is not an active transaction and cannot authorize rollback. Recovery removes
validated private retired directories once no active journal remains. If the
error instead reports an uncertain commit outcome, preserve all recovery data
and run explicit recovery before starting Nagi or repairing the privileged
helper; the installer cannot promise that either version was restored.
See the [transaction design](../design/installation.md#transactions-and-recovery)
for the commit and recovery rules.

The installer record at `<prefix>/share/nagi/install-state` identifies managed files. New records contain only a version marker and the App path. Existing v3 records are accepted for update or removal; a successful update writes the smaller v4 record. Content changes at recorded paths do not block update, removal, or App-only operations. An absent or invalid installation record and symbolic links at managed paths are refused. The installer durably stages replacements and backups before changing managed files. A failed operation attempts recovery of the previous files and default profile; a recovery error retains its journal and backups. After commit, recovery keeps the new files and completes journal cleanup. Other user profiles, subscriptions, settings, and logs are preserved unless whole uninstall uses `--purge-data`; this option removes the user's Nagi configuration, data, state, runtime, and cache directories after the managed installation transaction commits. A purge failure is reported as partial completion after the managed installation has been removed; some user data may remain, and the CLI does not attempt to restore the removed login service. Uninstall does not remove user login service definitions. Updating and uninstalling require removing any installed login service first. On macOS with `/usr/local`, the installer manages the privileged helper using `sudo`; run it as the intended user, not as root. Stop mihomo before update or uninstall. Uninstall leaves the user backend selected. If the helper was present before an update, its backend selection is restored; if it was absent, the update installs and selects it. TUN itself remains a runtime setting and must be enabled again after mihomo restarts. On macOS, disable the App login item before uninstalling the App. A privileged helper requires a root-owned, non-symlink `/usr/local/bin/nagi` and root-controlled parent directories. The installer does not change parent-directory ownership or permissions; check them before installation, especially on machines where `/usr/local` is user-writable.

The online wrapper requires Bash, `curl`, `tar`, and `shasum`. It expects each GitHub
Release to contain `nagi-linux-amd64.tar.gz`, `nagi-linux-arm64.tar.gz`,
`nagi-macos-amd64.tar.gz`, `nagi-macos-arm64.tar.gz`, and `SHA256SUMS` for the
assets that were built. Each archive also contains the matching `installer.sh`.
macOS archives may also contain `Nagi.app`; the release script includes it when
an App was built for the release host architecture.
Local Linux testing covers the CLI artifact flow. The macOS helper lifecycle
and unsigned App launch must be checked on a macOS host.

## Durability verification

Run the installer checks with Go and Bash available on `PATH`:

```sh
go test ./internal/installtx ./scripts ./internal/installation
bash scripts/test-install.sh
```

The shell fixtures forward filesystem transactions to a freshly built production
CLI; they replace only unrelated lifecycle/status responses. The transaction
package tests inject process termination and synchronization failures. Separate
process checks cover prepare/activate/commit recovery, repeated recovery, whole
installation/update/removal, and App directory installation/removal. The shell
checks additionally recover a transaction with a missing installation record and
ensure `--verify` leaves its journal untouched. Process termination does not
emulate loss of the kernel's or device's volatile write cache.

Linux verification on 2026-10-18 passed the full Go suite, transaction and
installation race checks, Go vet, and the installer checks with Bash and Bash
3.2. An isolated installation using the real CLI and pinned engine passed
installation, version inspection, update, and CLI removal while retaining user
data; it did not start the engine. Darwin AMD64 and ARM64 CLI cross-builds also
passed. Native macOS helper/service acceptance and VM power-cut acceptance were
not run.

VM power-cut acceptance is a separate, unrun requirement in the Linux development
environment. On disposable Linux and macOS virtual machines, retain a snapshot
with known old files and checksums, use the filesystem and storage-cache settings
intended for deployment, and run each installation/update/removal scenario.
Interrupt the VM's power externally at preparation, destination mutation, commit,
rollback, and cleanup boundaries; do not shut down the guest or merely kill the
installer. After reboot, run the same verified installer against the original
prefix and recover repeatedly, including a second power cut during rollback.
Confirm that uncommitted transactions restore every prior file and profile,
committed transactions retain the complete new result, and recovery data remains
available when restoration cannot finish. Include App bundles and cross-volume
App/profile destinations. Record guest OS/filesystem, virtual disk cache policy,
cut point, before/after checksums, recovery output, and any native helper/service
state. Passing process-crash tests alone must not be reported as passing this
power-cut acceptance.
