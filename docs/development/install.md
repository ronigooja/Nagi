# Install and update from local artifacts

This document defines the local artifact layout and installation workflow shared by development builds and future release workflows. The installer uses files already on disk; it does not download, build, sign, or notarize them.

## Prepare artifacts

Place matching executable `nagi` and `mihomo` files in one directory. On macOS, an optional `Nagi.app` bundle in the same directory is installed when present. `--app-dest` changes its destination. Build local CLI artifacts with `make build` (which produces `bin/nagi` and `bin/mihomo`); package the app separately with `macos/NagiApp/package-app.sh` and copy `macos/NagiApp/dist/Nagi.app` into the artifact directory when needed. Check the files and their architecture before installation. A future release workflow should include this version of `scripts/install.sh` in its published archive alongside the binaries. A download workflow can verify the archive, then invoke the included installer on its artifact directory; the installer does not fetch releases or verify their provenance.

## Install, update, and remove

Run from the repository root. Stop Nagi before replacing binaries. On macOS, quit the menu app as well. Remove an installed login service and, on macOS, an installed privileged helper before updating or uninstalling binaries; inspect their status afterward and reinstall them if needed. Disabling the login service alone does not satisfy the installer preflight. See [Startup and recovery](../guides/startup.md) and [Traffic access](../guides/traffic-access.md) for service and helper operations.

```sh
make build
./scripts/install.sh install --artifact-dir bin
./scripts/install.sh update --artifact-dir bin
./scripts/install.sh uninstall
```

`make install` and `make update` build CLI artifacts first; `make uninstall` does not build. All three provide local development entry points. The installer itself takes a prebuilt artifact directory so workflows can call it without invoking the local build. `install` and `update` require both executables; `uninstall` does not require build artifacts. `update` replaces the installed executables using the same artifact layout as `install`. After installing or updating, check the installed version with `/usr/local/bin/nagi --json version` and confirm the companion `mihomo` is the intended pinned build.

| Option | Meaning |
| --- | --- |
| `--artifact-dir DIR` | Directory containing executable `nagi` and `mihomo`, plus optional `Nagi.app`. Required for `install` and `update`. |
| `--prefix DIR` | Directory prefix for installed CLI binaries; defaults to `/usr/local`, giving `/usr/local/bin/nagi` and `/usr/local/bin/mihomo`. Use the same prefix for later updates and removal. |
| `--app-dest PATH` | Override the macOS App destination (default `/Applications/Nagi.app`). The installer installs an App whenever the artifact directory includes `Nagi.app` and remembers its destination for updates and removal. |

The macOS app invokes `/usr/local/bin/nagi` directly. Keep the default prefix when installing the app; an alternate prefix does not change the path compiled into the app. Install the app in Applications before enabling “Open menu bar app at login.” `--app-dest` cannot enable App installation without a `Nagi.app` in the artifact directory. On update, an installation that includes the App requires an updated App in the artifact directory as well.

The installer stores hashes for the two binaries and, if installed, the entire App bundle content and layout in `<prefix>/share/nagi/install-state`. It refuses to change files whose recorded hashes no longer match; resolve a manually changed installation before retrying. It stages replacements and attempts to restore the previous files if an operation fails. Both `install` and `update` write the version's built-in default profile to `config/profiles/default.yaml` in the invoking user's Nagi configuration directory, replacing any existing file at that path. This includes local edits, imported content, and subscription content saved under the name `default`; export or back up that profile before installation if you need it. A failed operation restores the previous default profile. Other user profiles, subscriptions, settings, and logs are preserved. Uninstall preserves the installed default profile along with other user data; it does not remove user login service definitions or the macOS privileged helper automatically. Updating and uninstalling require removing any installed login service first. They also require the macOS privileged helper to be uninstalled; reinstall either component after updating if needed. Manage those through the [CLI service and helper commands](../reference/cli.md#startup-and-background-operation). If the privileged helper is in use, stop privileged mihomo and switch back to the user backend before uninstalling it; the [traffic access guide](../guides/traffic-access.md) describes its installation and selection. The installer refuses to change binaries while the privileged helper remains installed, including when it is inactive. On macOS, disable the App login item before uninstalling the App. A privileged helper requires a root-owned, non-symlink `/usr/local/bin/nagi` and root-controlled parent directories. The installer does not change parent-directory ownership or permissions; check them before reinstalling the helper, especially on machines where `/usr/local` is user-writable.

The installer does not provide an online updater. A publishing workflow will be able to reuse this interface after artifacts, download locations, and integrity checks are defined. The macOS App branch and system-directory permissions still require verification on macOS; local Linux testing covers the CLI artifact flow.
