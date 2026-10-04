# Inspect diagnostics and security

Run the redacted report before troubleshooting a proxy path:

```sh
nagi diagnostics
nagi --json diagnostics export /absolute/path/nagi-diagnostics.json
```

The report checks the executable, selected profile, process marker, private Unix Socket API, configured and reachable listener ports, DNS configuration, listener/controller exposure, and owner-only permissions. It distinguishes a running process from a reachable API and a configured listener from a working proxy path. Run `nagi proxy delay NODE` to test a real request through a selected node.

The export excludes profile YAML, subscription URLs, credentials, and mihomo command output. Keep the file private because paths and state can still reveal local environment details.

On Linux with nftables, enable direct-traffic blocking after starting a TUN interface. Supply numeric upstream addresses and ports for proxy and DNS traffic:

```sh
sudo nagi kill-switch enable tun0 203.0.113.10:443 203.0.113.53:853
sudo nagi kill-switch status
sudo nagi kill-switch disable
```

Replace `tun0` and the example addresses with your real interface and upstream endpoints. `sudo` should preserve `SUDO_UID` so Nagi scopes the rules to your user. The policy allows loopback, the named TUN interface, and the listed upstream endpoints; it drops every other outbound packet for that UID. Add every required proxy and DNS endpoint before enabling, or mihomo may lose connectivity. The nftables table persists if Nagi or mihomo exits; use `sudo nagi kill-switch disable` to remove it. On macOS or Linux without nftables, the CLI reports that the feature is unsupported and leaves firewall rules unchanged.
