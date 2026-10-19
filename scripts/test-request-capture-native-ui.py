#!/usr/bin/env python3
"""Opt-in macOS model -> actual CLI -> real engine traffic, synthetic TUN only.

Uses a private HOME and a freshly generated CA. The caller explicitly authorizes
temporary trust in the current user's login keychain; cleanup removes that CA.
Never uses an installed Nagi process, profile, helper, subscription or proxy.
"""
import hashlib
import http.client
import json
import os
import pathlib
import shutil
import shlex
import socket
import ssl
import subprocess
import sys
import tempfile
import time

cli, engine, package = map(os.path.abspath, sys.argv[1:4])
offline = sys.argv[4:] == ["--offline"]
assert sys.platform == "darwin", "Native macOS only"
root = pathlib.Path(os.environ["NAGI_CAPTURE_ACCEPTANCE_HOME"]) if os.environ.get("NAGI_CAPTURE_ACCEPTANCE_HOME") else pathlib.Path(tempfile.mkdtemp(prefix="ncap-ui-", dir="/private/tmp"))
keychain = pathlib.Path.home() / "Library/Keychains/login.keychain-db"
env = dict(os.environ, HOME=str(root), NAGI_MIHOMO_BIN=engine)
runtime = root / "Library/Application Support/Nagi/runtime"
public = root / "public.pem"
fingerprint = None
trusted = False
completed = False

def run(*args, data=None, raw=False):
    result = subprocess.run([cli] + ([] if raw else ["--json"]) + list(args),
        input=None if data is None else json.dumps(data).encode(), env=env,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=40)
    assert result.returncode == 0, (args, result.stderr.decode())
    return result.stdout if raw else json.loads(result.stdout)["data"]

def traffic():
    conn = http.client.HTTPConnection("engine.fixture", timeout=15)
    conn.sock = socket.socket(socket.AF_UNIX)
    conn.sock.connect(str(runtime / "mihomo.sock"))
    conn.request("GET", "/fixture/traffic")
    response = conn.getresponse()
    body = response.read()
    conn.close()
    assert response.status == 200, body
    return json.loads(body)

try:
    config = root / "fixture.yaml"
    config.write_text("mixed-port: 0\nlog-level: silent\nfind-process-mode: off\n"
        "tun: {enable: false}\ndns: {enable: false}\nrules: ['MATCH,DIRECT']\n")
    config.chmod(0o600)
    profiles = run("profile", "list")
    if "work" not in json.dumps(profiles):
        run("profile", "import", "work", str(config))
    run("profile", "use", "work")
    if offline:
        # Deliberately imported fictitious content: this path proves native
        # model/CLI persistence and preview, with no live-network claim.
        scope = dict(domains=[], process_paths=[], app_paths=[])
        status = dict(session_id="native-ui-offline", revision=1, state="stopped",
            profile="work", backend="user", owner="cli", scope=scope, body_limit=1048576,
            records=1, bytes=0, dropped=0, truncated=0, read_only=True)
        request = dict(scheme="http", host="api.fixture.example", port=80, method="POST",
            path="/original", query="credential=fictitious-token", headers={
            "Authorization": ["Bearer fictitious-token"]}, body_state="complete", body_size=13, captured=13)
        response = dict(status=200, headers={}, body_state="complete", body_size=0, captured=0)
        record = dict(id="native-ui-record", session_id=status["session_id"], kind="http",
            generation="offline-generation", rule_version="offline-version", started="2026-10-18T00:00:00Z",
            duration_ms=1, rule_ids=[], outcome="complete", identity=dict(session_id=status["session_id"],
            revision=1, uid=os.getuid(), process="fictitious-client", process_path="/fictitious/client",
            source="127.0.0.1:12345", destination="127.0.0.1:80"), snapshots=dict(
            request_original=request, request_actual=request, response_original=response, response_actual=response))
        lines = [dict(type="session", version=1, status=status), dict(type="record", record=record)]
        for snapshot in ["request_original", "request_actual"]:
            lines.append(dict(type="body", record_id=record["id"], snapshot=snapshot, data="b3JpZ2luYWwtYm9keQ=="))
        lines.append(dict(type="end", records=1))
        archive = root / "offline.ndjson"
        archive.write_text("".join(json.dumps(line)+"\n" for line in lines))
        archive.chmod(0o600)
        run("rewrite", "capture", "import", str(archive))
        rows = [record]
    else:
        if os.environ.get("NAGI_CAPTURE_ACCEPTANCE_TRUST_PREPARED") != "1":
            run("rewrite", "ca", "generate")
            run("rewrite", "ca", "export", str(public), raw=True)
        der = ssl.PEM_cert_to_DER_cert(public.read_text())
        fingerprint = hashlib.sha1(der).hexdigest().upper()
        # This is an explicit native trust acceptance mutation, scoped to this CA.
        if os.environ.get("NAGI_CAPTURE_ACCEPTANCE_TRUST_PREPARED") != "1":
            subprocess.run(["/usr/bin/security", "add-trusted-cert", "-r", "trustRoot",
                "-k", str(keychain), str(public)], check=True, timeout=30)
        trusted = True
        assert run("rewrite", "ca", "status")["trust"] == "trusted"
        run("start")
        try:
            run("rewrite", "capture", "start")
        except Exception:
            print("Activation evidence:", json.dumps(run("rewrite", "status")), flush=True)
            raise
        assert traffic()["rewrite"] == ""
        rows = []
        for _ in range(60):
            rows = run("rewrite", "capture", "list").get("records", [])
            if rows and rows[0].get("outcome"):
                break
            time.sleep(0.1)
    assert rows
    # XCTest may reset HOME; pin only this fixture's subprocess environment.
    launcher = root / "nagi-fixture"
    launcher.write_text("#!/bin/sh\nexport HOME=" + shlex.quote(str(root)) +
        "\nexport NAGI_MIHOMO_BIN=" + shlex.quote(engine) +
        "\nexec " + shlex.quote(cli) + ' "$@"\n')
    launcher.chmod(0o700)
    test_env = dict(env, NAGI_CAPTURE_ACCEPTANCE_CLI=str(launcher),
        NAGI_CAPTURE_ACCEPTANCE_RECORD=rows[0]["id"], NAGI_CAPTURE_ACCEPTANCE_ACTIVATE="0" if offline else "1")
    subprocess.run(["swift", "test", "--package-path", package, "--filter",
        "CaptureNativeCLITests"], env=test_env, check=True, timeout=180)
    if not offline:
        assert traffic()["rewrite"] == "native-swift-draft"
    assert any(r["id"] == rows[0]["id"] for r in run("rewrite", "capture", "list")["records"])
    print("Native Swift raw field -> editable draft -> offline preview -> atomic save passed" +
        (" (imported offline archive; no network activation)." if offline else
         " -> actual CLI activation -> changed real HTTP request (synthetic TUN)."))
    completed = True
finally:
    try:
        run("quit")
    except Exception:
        pass
    retain = not completed and bool(os.environ.get("NAGI_CAPTURE_ACCEPTANCE_HOME"))
    if fingerprint and not retain:
        # Remove trust even if add-trusted-cert failed after a partial mutation.
        subprocess.run(["/usr/bin/security", "remove-trusted-cert", str(public)],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15)
        subprocess.run(["/usr/bin/security", "delete-certificate", "-Z", fingerprint, str(keychain)],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15)
        check = subprocess.run(["/usr/bin/security", "find-certificate", "-a", "-Z", str(keychain)],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15)
        assert fingerprint.encode() not in check.stdout, "Fixture CA remained in login keychain"
    if retain:
        print("Retained owner-only fixture for explicit retry:", root, flush=True)
    else:
        shutil.rmtree(root)
