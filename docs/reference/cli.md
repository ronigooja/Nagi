# CLI reference

This document defines the implemented Nagi commands and their JSON interface. `--json` may appear anywhere in the argument list. Without it, Nagi prints readable text or indented data.

| Command | Result or effect |
| --- | --- |
| `start`, `stop`, `restart` | Manage the mihomo process. `start` creates the default profile if absent. |
| `status` | Report running state, selected profile, PID, paths, and available mihomo version and mixed port. |
| `logs [lines]` | Return the last 100 lines by default, or 1 through 10000 lines. |
| `config validate` | Check the selected profile with `mihomo -t`. |
| `config show` | Return the selected profile YAML. |
| `profile list` | List profile names and the current selection. |
| `profile use NAME` | Validate and activate an existing profile, reloading a running mihomo instance. |
| `profile import NAME FILE` | Validate and atomically write a profile from a local YAML file (maximum 8 MiB). |
| `subscription list` | List names and refresh times without URLs. |
| `subscription add NAME URL` | Save an HTTP or HTTPS subscription URL. |
| `subscription update NAME` | Fetch up to 8 MiB into Nagi's cache. |
| `subscription apply NAME` | Validate cached YAML as a mihomo profile, import it as profile `NAME`, and activate it. |
| `subscription remove NAME` | Remove a saved subscription and its cache. |
| `proxy groups`, `proxy show GROUP` | Read mihomo proxy groups through its Unix Socket API. |
| `proxy select GROUP NODE` | Select a node in a group. |
| `connections list` | Return a snapshot of active connections. |
| `service install`, `service uninstall` | Manage a per-user launchd Agent or systemd service. |
| `version` | Report Nagi version, mihomo version and pinned commit, OS, and architecture. |

Profile and subscription names consist of ASCII letters, digits, `_`, or `-` and have a maximum length of 128 characters. A profile file is named `profiles/NAME.yaml`. `subscription update` accepts HTTP 200 responses only. Nagi saves the response in its own cache. For subscriptions that contain a complete mihomo YAML configuration, `subscription apply` validates and activates it. Other subscription formats require conversion outside Nagi before importing a profile.

JSON success output has the shape `{"ok":true,"data":{...}}`. JSON failure output is written to stderr as `{"ok":false,"error":{"code":"...","message":"..."}}`. Successful commands exit with status `0`; usage errors exit with `2`; all other errors exit with `1`. Error codes include `usage`, `already_running`, `not_running`, `not_found`, `invalid_config`, `profile_error`, `subscription_error`, `proxy_selection_error`, `mihomo_api_error`, and `internal_error`. These codes and field names are the CLI integration contract for the macOS app.

`NAGI_MIHOMO_BIN` overrides the default mihomo executable path, which is a file named `mihomo` beside the Nagi executable. Nagi uses XDG directories on Linux and `~/Library/Application Support/Nagi` on macOS; see the [runtime design](../design/runtime.md).
