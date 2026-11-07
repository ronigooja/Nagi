#!/usr/bin/env python3
"""Exercise restricted root TUN and LAN proxy listeners on disposable CI hosts."""

import http.client
import http.server
import json
import os
from pathlib import Path
import shutil
import socket
import stat
import struct
import subprocess
import sys
import tempfile
import threading
import time


class UnixHTTP(http.client.HTTPConnection):
    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self.host)


def main():
    if os.environ.get("GITHUB_ACTIONS") != "true" or os.geteuid() != 0:
        raise SystemExit("This fixture requires root on a disposable GitHub-hosted runner")
    binary = str(Path(sys.argv[1]).resolve())
    parent = "/private/var/root" if sys.platform == "darwin" else "/root"
    home = Path(tempfile.mkdtemp(prefix="nagi-lan-", dir=parent))
    os.chmod(home, 0o700)
    controller = str(home / "api.sock")
    process = None
    origin = None
    log = None

    def api(method="GET", payload=None):
        conn = UnixHTTP(controller, timeout=5)
        try:
            body = None if payload is None else json.dumps(payload)
            conn.request(method, "/configs", body, {"Content-Type": "application/json"})
            response = conn.getresponse()
            data = response.read()
            assert response.status < 300, (response.status, data)
            return json.loads(data) if data else None
        finally:
            conn.close()

    def await_value(check, description):
        deadline = time.monotonic() + 30
        last = None
        while time.monotonic() < deadline:
            try:
                value = check()
                if value:
                    return value
            except (OSError, AssertionError, http.client.HTTPException) as error:
                last = error
            if process is not None and process.poll() is not None:
                raise AssertionError("engine exited before " + description)
            time.sleep(0.2)
        raise AssertionError(f"timed out waiting for {description}: {last}")

    def free_port():
        with socket.socket() as sock:
            sock.bind(("0.0.0.0", 0))
            return sock.getsockname()[1]

    try:
        # UDP connect selects the primary interface without sending a packet.
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as route:
            route.connect(("192.0.2.1", 9))
            lan_ip = route.getsockname()[0]
        assert not lan_ip.startswith("127."), "non-loopback CI interface required"
        ports = [free_port() for _ in range(3)]
        assert len(set(ports)) == 3
        config = home / "config.yaml"
        config.write_text(
            f"port: {ports[0]}\nsocks-port: {ports[1]}\nmixed-port: {ports[2]}\n"
            "mode: rule\nlog-level: warning\nallow-lan: true\nbind-address: '*'\n"
            f"lan-allowed-ips: ['{lan_ip}/32']\n"
            "rules: ['NETWORK,udp,REJECT', 'IP-CIDR,127.0.0.0/8,DIRECT,no-resolve', 'MATCH,REJECT']\n"
            "dns:\n  enable: false\ntun:\n  enable: true\n  stack: gvisor\n"
            "  auto-route: false\n  auto-detect-interface: true\n"
        )
        os.chmod(config, 0o600)
        log = (home / "engine.log").open("w+")
        process = subprocess.Popen(
            [binary, "-d", str(home), "-f", str(config), "--config-policy", "restricted",
             "-ext-ctl-unix", controller], stdout=log, stderr=log,
        )
        initial = await_value(lambda: api(), "controller")
        assert initial["tun"]["enable"] is True
        device = initial["tun"].get("device")
        assert device, "engine must report its TUN device"
        def adapter_up():
            if sys.platform == "darwin":
                result = subprocess.run(["ifconfig", device], capture_output=True, text=True)
                return result.returncode == 0 and "UP" in result.stdout.splitlines()[0] and "inet " in result.stdout
            result = subprocess.run(["ip", "-j", "address", "show", "dev", device], capture_output=True, text=True)
            if result.returncode != 0:
                return False
            adapters = json.loads(result.stdout)
            return bool(adapters and "UP" in adapters[0]["flags"] and any(a["family"] == "inet" for a in adapters[0]["addr_info"]))

        await_value(adapter_up, "up TUN adapter with IPv4 address")
        assert stat.S_IMODE(os.stat(controller).st_mode) == 0o600

        class Origin(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(200)
                self.end_headers()
                self.wfile.write(b"nagi-lan-fixture")

            def log_message(self, *_args):
                pass

        origin = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Origin)
        threading.Thread(target=origin.serve_forever, daemon=True).start()

        def proxy_request(port):
            conn = http.client.HTTPConnection(lan_ip, port, timeout=3)
            try:
                conn.request("GET", f"http://127.0.0.1:{origin.server_port}/fixture")
                response = conn.getresponse()
                return response.status == 200 and response.read() == b"nagi-lan-fixture"
            finally:
                conn.close()

        def check_shared():
            for port in (ports[0], ports[2]):
                await_value(lambda: proxy_request(port), "LAN HTTP forwarding")
            for port in (ports[1], ports[2]):
                with socket.create_connection((lan_ip, port), timeout=3) as sock:
                    def receive(size):
                        data = b""
                        while len(data) < size:
                            chunk = sock.recv(size - len(data))
                            assert chunk, "unexpected SOCKS EOF"
                            data += chunk
                        return data

                    sock.sendall(b"\x05\x01\x00")
                    assert receive(2) == b"\x05\x00"
                    sock.sendall(b"\x05\x01\x00\x01\x7f\x00\x00\x01" + struct.pack("!H", origin.server_port))
                    header = receive(4)
                    assert header[:2] == b"\x05\x00", header
                    if header[3] == 1:
                        receive(6)
                    elif header[3] == 4:
                        receive(18)
                    else:
                        receive(receive(1)[0] + 2)
                    sock.sendall(b"GET /fixture HTTP/1.0\r\nHost: fixture\r\n\r\n")
                    response = http.client.HTTPResponse(sock)
                    response.begin()
                    assert response.status == 200 and response.read() == b"nagi-lan-fixture"
            state = api()
            assert state["allow-lan"] is True and state["tun"]["enable"] is True
            assert state["tun"]["device"] == device
            await_value(adapter_up, "preserved up TUN adapter")

        check_shared()
        api("PATCH", {"allow-lan": False, "bind-address": "127.0.0.1"})
        for port in ports:
            try:
                socket.create_connection((lan_ip, port), timeout=1).close()
            except OSError:
                pass
            else:
                raise AssertionError("disabled LAN listener remains reachable")
        assert api()["tun"]["enable"] is True
        await_value(adapter_up, "up TUN after disabling LAN")
        api("PATCH", {"allow-lan": True, "bind-address": lan_ip})
        check_shared()
        api("PATCH", {"allow-lan": True, "bind-address": "*"})
        check_shared()
        print("PASS: restricted root TUN + wildcard/specific LAN HTTP/SOCKS/mixed; disable preserves TUN; controller private")
    except Exception:
        if log is not None:
            log.flush()
            print((home / "engine.log").read_text()[-6000:], file=sys.stderr)
        raise
    finally:
        if origin is not None:
            origin.shutdown()
            origin.server_close()
        if process is not None:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
        if log is not None:
            log.close()
        shutil.rmtree(home)


if __name__ == "__main__":
    main()
