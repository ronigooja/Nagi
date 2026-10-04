# Configure startup and recovery

Install login startup with:

```sh
nagi service install
nagi startup status
```

Use `nagi startup enable` or `nagi startup disable` to change login activation without deleting the service definition. `nagi service status` shows the definition path and manager state. `nagi service uninstall` stops, disables, and removes the definition.

If mihomo exits unexpectedly, run `nagi startup check`. It reports process and control API state and removes stale PID/socket markers. `nagi recover` performs only that stale-state cleanup. Start mihomo again explicitly with `nagi start`; Nagi does not silently restart a process after a crash. Run `startup check` after network changes or sleep/wake to verify the control socket before using proxy operations.

Concurrent lifecycle commands are serialized. A live process produces `already_running`; stale state is reported as `unexpected_exit` and is safe to recover. Inspect `nagi logs` before restarting if the process repeatedly exits.
