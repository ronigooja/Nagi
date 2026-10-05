# Configure traffic access

This guide explains how to route application traffic through Nagi and how to inspect the result. Start mihomo with a profile containing useful proxy nodes first; the default profile routes directly.

1. Run `nagi start` and `nagi ports status`. A mixed port accepts HTTP and SOCKS clients; the HTTP port accepts HTTP and HTTPS CONNECT clients. A port of `0` is disabled.
2. To use OS proxy aware applications, run `nagi system-proxy enable`, then `nagi system-proxy status`. Nagi saves prior OS settings and restores them with `nagi system-proxy disable` or `nagi stop`.
3. For traffic that does not honor OS proxy settings, run `nagi tun enable` and inspect `nagi tun status` and `nagi logs`. TUN requires OS permission to create an adapter and install routes. Run `nagi tun disable` to stop it.
4. To allow other devices to connect to Nagi's proxy listener, run `nagi lan enable ADDRESS`, replacing ADDRESS with a local interface IP. `nagi lan enable` binds all interfaces; `nagi lan disable` returns to loopback. Check `nagi lan status` and configure your firewall before sharing a listener.

System proxy settings route only apps that honor the OS HTTP/HTTPS proxy. TUN captures routed traffic through the adapter, subject to OS routes and permissions. Neither command guarantees that every app or DNS request uses a proxy. LAN access exposes a listener but does not change another device's routing. Use `nagi connections list` to inspect traffic seen by mihomo and `nagi logs` for adapter or listener errors.

On macOS, system proxy changes use `networksetup` for active network services. On Linux, they require an active GNOME desktop session with `gsettings`. Other Linux desktops can configure applications manually using the ports from `nagi ports status`. If restoration fails, run `nagi system-proxy disable` again; Nagi keeps the saved settings until restoration succeeds.
