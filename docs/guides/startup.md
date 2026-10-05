# Configure startup and recovery

Install login startup with:

```sh
nagi service install
nagi startup status
```

Use `nagi startup enable` or `nagi startup disable` to change login activation without deleting the service definition. `nagi service status` shows the definition path and manager state. `nagi service uninstall` stops, disables, and removes the definition.

After upgrading from a release whose service ran a one-shot `nagi start`, run `nagi service uninstall` and then `nagi service install` to replace the loaded definition. The uninstall stops mihomo briefly.

The installed login service checks the engine, control API, and managed system proxy every 30 seconds. If mihomo exits unexpectedly, the service removes stale PID/socket markers and tries to start it again. Failed attempts back off for up to five minutes. After network changes and sleep/wake, it reasserts OS proxy settings only when they exactly match the saved pre-Nagi settings. Different settings are reported for inspection rather than overwritten. Run `nagi startup check` to see `system_proxy.status`, conflicts, and any new or missing macOS services. Inspect `nagi dns check` when traffic still fails, because a reachable control API and restored system proxy do not prove OS routes or DNS capture. A live mihomo process whose control API is unreachable is left running for manual inspection with `nagi logs`.

For a manual recovery without the service, run `nagi startup check` to report state and clear stale markers, then `nagi start`. `nagi recover` performs only stale-state cleanup. Disable the service before using `nagi stop` to keep the engine stopped; otherwise the monitor starts it again at the next check.

Concurrent lifecycle commands are serialized. A live process produces `already_running`; stale state is reported as `unexpected_exit` and is safe to recover. Inspect `nagi logs` before restarting if the process repeatedly exits.
