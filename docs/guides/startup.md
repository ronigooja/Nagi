# Configure startup and recovery

This guide covers login startup, current-session controls, and recovery on macOS and Linux.

Enable login startup (installing the service automatically if needed) with:

```sh
nagi startup enable
nagi startup status
```

In `nagi menu`, open Settings & Maintenance → Start at Login. Select Enable start at login or Disable start at login; only the action matching the current state is shown. View details includes service installation and activity as well as engine state. Advanced options → Remove login startup service removes only the service, keeping Nagi and configuration.

Use `nagi startup disable` to turn off future login startup without deleting the service definition or interrupting the current monitor, subscription scheduler, or mihomo connections. Use `nagi startup enable` to turn it back on; an existing definition is retained and an active monitor is not reloaded. To stop the current engine separately, use `nagi stop`. `nagi service status` shows the definition path and manager state. `nagi service uninstall` stops, disables, and removes the definition; this can interrupt mihomo. Complete command semantics are in the [CLI reference](../reference/cli.md#startup-and-background-operation).

After upgrading from a release whose service ran a one-shot `nagi start`, run `nagi service uninstall` and then `nagi service install` to replace the loaded definition. The uninstall stops mihomo briefly.

The installed login service checks the engine and control API every 30 seconds. On macOS, it also checks managed system proxy settings. If mihomo exits unexpectedly, the service removes stale PID/socket markers and tries to start it again. Failed attempts back off for up to five minutes. After network changes and sleep/wake, the macOS service reasserts OS proxy settings only when they exactly match the saved pre-Nagi settings. Different settings are reported for inspection rather than overwritten. Run `nagi startup check` to see `system_proxy.status`, conflicts, and any new or missing macOS services. A live mihomo process whose control API is unreachable is left running for manual inspection with `nagi logs`.

For a manual recovery without the service, run `nagi startup check` to report state and clear stale markers, then `nagi start`. `nagi recover` performs only stale-state cleanup. Use `nagi stop` to keep the engine stopped while leaving the login service installed. Use `nagi start` or `nagi restart` to resume it. `nagi quit` stops the current login monitor and engine, restores Nagi-managed system proxy settings, and leaves next-login startup enabled. A later `nagi start` resumes an enabled monitor. If quit was interrupted and `start` reports `lifecycle_busy`, rerun `nagi quit` to finish cleanup, then start again.

Concurrent lifecycle commands are serialized. A live process produces `already_running`; stale state is reported as `unexpected_exit` and is safe to recover. Inspect `nagi logs` before restarting if the process repeatedly exits.

## Scheduled subscriptions

The installed login monitor also hosts the subscription scheduler. Run `nagi subscription list` to inspect intervals, next due times, and redacted failures. To host it in a foreground terminal instead, run `nagi subscription watch` and interrupt it to stop. Only one scheduler runs for the user. Automatic application requires an associated selected profile established by `nagi subscription apply NAME`; other subscriptions refresh their caches only. If a source profile changes outside Nagi or cannot be read, the schedule pauses for that subscription. Review the profile and run `nagi subscription apply NAME` to resume.
