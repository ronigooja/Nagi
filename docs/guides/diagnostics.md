# Inspect diagnostics and security

Run the redacted report before troubleshooting a proxy path:

```sh
nagi diagnostics
nagi --json diagnostics export /absolute/path/nagi-diagnostics.json
```

The report checks the executable, selected profile, process marker, private Unix Socket API, configured and reachable listener ports, DNS configuration, listener/controller exposure, and owner-only permissions. It distinguishes a running process from a reachable API and a configured listener from a working proxy path. Run `nagi proxy delay NODE` to test a real request through a selected node.

The export excludes profile YAML, subscription URLs, credentials, and mihomo command output. Keep the file private because paths and state can still reveal local environment details.

`nagi kill-switch status` reports whether a firewall backend is available. `kill-switch enable` refuses to change firewall state when Nagi cannot prove a safe managed transaction. Configure and verify an administrator-managed nftables/iptables or pf policy if direct-traffic blocking is required; do not treat backend detection alone as protection.
