#!/usr/bin/env python3
"""Isolated ordinary-UID coordinator acceptance. No real TUN or OS trust writes."""
import json, os, pathlib, shutil, signal, subprocess, sys, tempfile, time
import ctypes
import http.client, socket, base64

if sys.platform.startswith("linux"):
    assert ctypes.CDLL(None).prctl(36, 1, 0, 0, 0) == 0

cli, engine = map(os.path.abspath, sys.argv[1:3])
root = pathlib.Path(tempfile.mkdtemp(prefix="nagi-rw-coordinator-"))
uid = 65534 if os.geteuid() == 0 else os.geteuid()
gid = 65534 if os.geteuid() == 0 else os.getegid()
if os.geteuid() == 0:
    os.chown(root, uid, gid)

def credentials():
    if os.geteuid() == 0:
        os.setgroups([])
        os.setgid(gid)
        os.setuid(uid)

env = dict(os.environ, HOME=str(root), XDG_CONFIG_HOME=str(root / "config"),
           XDG_DATA_HOME=str(root / "data"), XDG_STATE_HOME=str(root / "state"),
           XDG_RUNTIME_DIR=str(root / "run"), NAGI_MIHOMO_BIN=engine,
           SSL_CERT_FILE=str(root / "public-root.pem"), SSL_CERT_DIR=str(root / "empty-roots"))
(root / "empty-roots").mkdir(mode=0o700)
if os.geteuid() == 0:
    os.chown(root / "empty-roots", uid, gid)

def run(*args, data=None, raw=False):
    argv = [cli] + ([] if raw else ["--json"]) + list(args)
    result = subprocess.run(argv, input=None if data is None else json.dumps(data).encode(),
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env,
                            preexec_fn=credentials, timeout=35)
    if result.returncode:
        raise AssertionError(f"{' '.join(args)}: {result.stderr.decode()}")
    return result.stdout if raw else json.loads(result.stdout)["data"]

def write(name, data):
    path = root / name
    path.write_text(data)
    path.chmod(0o600)
    if os.geteuid() == 0:
        os.chown(path, uid, gid)
    return str(path)

journal = root / "run/nagi/request-rewrite-run.json"
def wait_for(predicate, seconds=8):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        value = predicate()
        if value:
            return value
        time.sleep(0.1)
    raise AssertionError("fixture timed out")

def traffic(kind="http"):
    conn = http.client.HTTPConnection("engine.fixture", timeout=15)
    conn.sock = socket.socket(socket.AF_UNIX)
    conn.sock.settimeout(15)
    conn.sock.connect(str(root / "run/nagi/mihomo.sock"))
    conn.request("GET", "/fixture/traffic?kind=" + kind)
    response = conn.getresponse()
    data = response.read()
    conn.close()
    assert response.status == 200, data
    return json.loads(data)

def records():
    return run("rewrite", "capture", "list").get("records", [])

try:
    profile = write("fixture.yaml", "mixed-port: 0\nport: 0\nsocks-port: 0\nlog-level: silent\nfind-process-mode: off\ntun: {enable: false}\ndns: {enable: false}\nrules: ['MATCH,DIRECT']\n")
    run("profile", "import", "work", profile)
    run("profile", "use", "work")
    run("rewrite", "ca", "generate")
    run("rewrite", "ca", "export", str(root / "public-root.pem"), raw=True)
    # Failed prerequisites may not start recorder, TUN, or an engine.
    rejected = subprocess.run([cli, "--json", "rewrite", "capture", "start"], stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env, preexec_fn=credentials, timeout=20)
    assert rejected.returncode == 1 and not journal.exists()
    assert not (root / "run/nagi/capture.sock").exists()
    run("start")
    # An engine that lacks capture capability fails without changing runtime.
    write("old-capabilities", "fixture")
    rejected = subprocess.run([cli, "--json", "rewrite", "capture", "start"], stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env, preexec_fn=credentials, timeout=20)
    assert rejected.returncode == 1 and b"capture_capability_missing" in rejected.stderr
    assert not journal.exists() and not (root / "run/nagi/capture.sock").exists()
    (root / "old-capabilities").unlink()
    started = run("rewrite", "capture", "start")["status"]
    session = started["session_id"]
    first = json.loads(journal.read_text())
    assert first["capture_session"] == session and first["owner_uid"] == uid
    assert run("rewrite", "capture", "status")["status"]["state"] == "recording"
    assert traffic() == {"body":"original-response:original-body", "rewrite":"", "status":200}
    wait_for(lambda: len(records()) == 1)
    record = records()[0]
    assert record["identity"]["uid"] == uid, record
    assert record["identity"]["process_path"] == "/Applications/Fixture.app/Contents/MacOS/Fixture", record
    assert record["snapshots"]["request_original"]["headers"]["Authorization"] == ["Bearer fictitious-token"], record
    body = run("rewrite", "capture", "body", record["id"], "response_original", "0")["body"]
    assert base64.b64decode(body["data"]) == b"original-response:original-body", body
    assert traffic("websocket")["body"] == "original-message"
    wait_for(lambda: len(records()) >= 4)
    messages = [r for r in records() if r["kind"] == "websocket"]
    assert len(messages) == 2 and {r["stage"] for r in messages} == {"websocket_client", "websocket_server"}, messages
    count = len(records())
    run("rewrite", "capture", "pause", session)
    assert traffic()["body"] == "original-response:original-body"
    assert len(records()) == count
    run("rewrite", "capture", "resume", session)
    assert json.loads(journal.read_text())["generation"] == first["generation"]
    assert traffic()["body"] == "original-response:original-body"
    wait_for(lambda: len(records()) == count+1)
    # A saved enabled rule under disabled global intent must never execute.
    rule = {"id":"disabled-intent", "enabled":True, "stage":"request", "match":{"domain":"api.fixture.example"}, "on_error":"block", "redirect_consent":[], "operations":[{"op":"header_set", "name":"X-Rewritten", "value":"must-not-run"}]}
    run("rewrite", "rules", "add", "-", data=rule)
    second = json.loads(journal.read_text())
    assert second["generation"] != first["generation"]
    assert run("rewrite", "capture", "status")["status"]["revision"] == 3
    assert traffic()["rewrite"] == ""
    wait_for(lambda: len(records()) == count+2)
    assert any(r["id"] == record["id"] for r in records())
    assert run("rewrite", "status")["enabled_intent"] is False
    # Move only the owned journal's certificate renewal deadline into the past.
    second["renew_at"] = "2000-01-01T00:00:00Z"
    journal.write_text(json.dumps(second))
    wait_for(lambda: json.loads(journal.read_text())["generation"] != second["generation"])
    assert run("rewrite", "capture", "status")["status"]["state"] == "recording"
    assert any(r["id"] == record["id"] for r in records())
    exported = run("rewrite", "capture", "export", "-", raw=True)
    assert b'"type":"end"' in exported and b"fictitious-token" in exported
    run("rewrite", "capture", "stop", session)
    assert run("rewrite", "capture", "status")["status"]["state"] == "stopped"
    assert any(r["id"] == record["id"] for r in records())
    # TUN disable stops collection while retaining this session's history.
    run("rewrite", "capture", "start")
    traffic()
    retained = wait_for(lambda: records())[0]["id"]
    run("tun", "disable")
    assert run("rewrite", "capture", "status")["status"]["state"] == "stopped"
    assert records()[0]["id"] == retained and not journal.exists()
    if sys.platform.startswith("linux"):
        run("rewrite", "capture", "start")
        traffic()
        retained = wait_for(lambda: records())[0]["id"]
        exited = json.loads(journal.read_text())
        os.kill(exited["engine_pid"], signal.SIGKILL)
        os.waitpid(exited["engine_pid"], 0)
        wait_for(lambda: not journal.exists())
        assert run("rewrite", "capture", "status")["status"]["state"] == "stopped"
        assert records()[0]["id"] == retained
        run("start")
        assert run("rewrite", "capture", "status")["status"]["state"] == "stopped"
    # A new session is stopped by a profile transition, without resuming later.
    run("rewrite", "capture", "start")
    run("profile", "import", "idle", profile)
    run("profile", "use", "idle")
    assert run("rewrite", "capture", "status")["status"]["state"] == "stopped"
    run("profile", "use", "work")
    assert run("rewrite", "capture", "status")["status"]["state"] == "stopped"
    run("quit")
    assert not journal.exists()
    print("ordinary-UID real-engine capture: zero-rule HTTP + bidirectional WebSocket, original identity/content, pause/resume generation retention, disabled rules, rule/certificate generation history, export, missing-capability rejection, TUN/engine-exit/profile stop and quit passed (synthetic TUN only)")
finally:
    try:
        run("quit")
    except Exception:
        pass
    shutil.rmtree(root)
    if sys.platform.startswith("linux"):
        while True:
            try:
                if os.waitpid(-1, os.WNOHANG)[0] == 0:
                    break
            except ChildProcessError:
                break
