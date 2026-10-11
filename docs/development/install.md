# Install and update

This document defines the end-user GitHub Releases workflow and the local artifact workflow used by development builds. The online wrapper downloads and verifies a release archive, then delegates installation to `scripts/installer.sh`. Neither workflow builds, signs, or notarizes files.

## Online install and update

Published releases use fixed GitHub asset names. The online wrapper selects the
matching operating system and architecture, downloads the archive and
`SHA256SUMS` from the latest release, verifies the archive, and invokes the
local installer. GitHub is the distribution service; no Nagi download server is
required.

Install the CLI with the repository's raw script:

```sh
curl -fsSL https://raw.githubusercontent.com/ronigooja/Nagi/main/scripts/install.sh | bash
```

On macOS, request the optional unsigned menu bar App:

```sh
curl -fsSL https://raw.githubusercontent.com/ronigooja/Nagi/main/scripts/install.sh | bash -s -- --app
```

Run the same command again to update an existing installation, or make the
operation explicit:

```sh
curl -fsSL https://raw.githubusercontent.com/ronigooja/Nagi/main/scripts/install.sh | bash -s -- update
```

The wrapper accepts `--version vX.Y.Z`, `--repo OWNER/REPOSITORY`, and
`--prefix DIR`. A fresh macOS installation is CLI-only unless `--app` is
provided. An update keeps an App that was already installed; `--no-app` keeps
the same behavior explicitly. Updating and uninstalling still use the local
installer's preflight checks: stop mihomo, quit Nagi.app, and remove the login
service before replacing managed executables.

The macOS App is unsigned and unnotarized. macOS may require the user to
right-click the App and choose **Open** on its first launch. This does not
change the CLI or mihomo installation path.

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
```

The installer uses existing artifacts and never compiles anything. Build the CLI and macOS App explicitly before installing or updating. `make build cli` does not rebuild mihomo; use an existing matching `bin/mihomo` or run `./scripts/build.sh` for a full pinned build. `./scripts/sync_to_mac.sh` copies source files to `zhuwei@macair:/Users/zhuwei/terminal/Nagi` and leaves remote `.git`, `bin/`, `dist/`, and build caches intact. It replaces remote source files, including local edits, and removes remote source files absent locally. `install` and `update` use the repository `bin/` directory by default and require both executables; `uninstall` does not require build artifacts. `update` replaces the installed executables using the same artifact layout as `install`. After installing or updating, check the installed version with `/usr/local/bin/nagi --json version` and confirm the companion `mihomo` is the intended pinned build.

| Option | Meaning |
| --- | --- |
| `--artifact-dir DIR` | Directory containing executable `nagi` and `mihomo`, plus optional `Nagi.app`. Defaults to the repository `bin/` directory. |
| `--prefix DIR` | Directory prefix for installed CLI binaries; defaults to `/usr/local`, giving `/usr/local/bin/nagi` and `/usr/local/bin/mihomo`. Use the same prefix for later updates and removal. |
| `--app-dest PATH` | Override the macOS App destination (default `/Applications/Nagi.app`). The installer installs an App whenever the artifact directory includes `Nagi.app` and remembers its destination for updates and removal. |

The macOS app invokes `/usr/local/bin/nagi` directly. Keep the default prefix when installing the app; an alternate prefix does not change the path compiled into the app. Install the app in Applications before enabling “Open menu bar app at login.” `--app-dest` cannot enable App installation without a `Nagi.app` in the artifact directory. On update, an installation that includes the App requires an updated App in the artifact directory. A CLI-only installation gains the App when the update artifact contains it and the destination is free.

The installer stores hashes for the two binaries and, if installed, the entire App bundle content and layout in `<prefix>/share/nagi/install-state`. It refuses to change files whose recorded hashes no longer match; resolve a manually changed installation before retrying. It stages replacements and attempts to restore the previous files if an operation fails. A failed operation restores the previous default profile; after a successful operation, the temporary rollback copy is deleted. Other user profiles, subscriptions, settings, and logs are preserved. Uninstall preserves the installed default profile along with other user data; it does not remove user login service definitions. Updating and uninstalling require removing any installed login service first. On macOS with `/usr/local`, the installer manages the privileged helper using `sudo`; run it as the intended user, not as root. Stop mihomo before update or uninstall. Uninstall leaves the user backend selected. If the helper was present before an update, its backend selection is restored; if it was absent, the update installs and selects it. TUN itself remains a runtime setting and must be enabled again after mihomo restarts. On macOS, disable the App login item before uninstalling the App. A privileged helper requires a root-owned, non-symlink `/usr/local/bin/nagi` and root-controlled parent directories. The installer does not change parent-directory ownership or permissions; check them before installation, especially on machines where `/usr/local` is user-writable.

The online wrapper requires Bash, `curl`, `tar`, and `shasum`. It expects each GitHub
Release to contain `nagi-linux-amd64.tar.gz`, `nagi-linux-arm64.tar.gz`,
`nagi-macos-amd64.tar.gz`, `nagi-macos-arm64.tar.gz`, and `SHA256SUMS` for the
assets that were built. Each archive also contains the matching `installer.sh`.
macOS archives may also contain `Nagi.app`; the release script includes it when
an App was built for the release host architecture.
Local Linux testing covers the CLI artifact flow. The macOS helper lifecycle
and unsigned App launch must be checked on a macOS host.
