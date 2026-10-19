#!/usr/bin/env python3
"""Isolated ordinary-UID coordinator acceptance. No real TUN or OS trust writes."""
import json, os, pathlib, shutil, signal, subprocess, sys, tempfile, time
import ctypes

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

try:
    profile = write("fixture.yaml", "mixed-port: 0\nport: 0\nsocks-port: 0\nlog-level: silent\nfind-process-mode: off\ntun: {enable: false}\ndns: {enable: false}\nrules: ['MATCH,DIRECT']\n")
    run("profile", "import", "work", profile)
    run("profile", "use", "work")
    run("request-rewrite", "ca", "generate")
    run("request-rewrite", "ca", "export", str(root / "public-root.pem"), raw=True)
    def rule(id, domain):
        return {"id":id,"enabled":True,"stage":"request","match":{"domain":domain},
                "on_error":"block","redirect_consent":[],"operations":[{"op":"header_set","name":"X-Demo","value":"example"}]}
    run("request-rewrite", "rules", "add", "-", data=rule("api", "api.service.example"))
    run("start")
    enabled = run("request-rewrite", "enable")
    assert enabled["actual"] == "active" and enabled["service_running"] is True, enabled
    first = json.loads(journal.read_text())
    assert run("request-rewrite", "enable")["generation"] == first["generation"]
    # The old supervisor remains live while a rejected candidate is rolled
    # back. Its owner socket makes supervisor start idempotent under cfg lock.
    write("fail-next-probe", "fixture")
    failed = subprocess.run([cli, "--json", "request-rewrite", "rules", "add", "-"],
                            input=json.dumps(rule("rejected", "rejected.service.example")).encode(),
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env,
                            preexec_fn=credentials, timeout=35)
    assert failed.returncode == 1 and not failed.stdout, failed
    restored = run("request-rewrite", "status")
    assert restored["actual"] == "active" and restored["generation"] == first["generation"], restored
    assert [d["id"] for d in run("request-rewrite", "rules", "list")["rules"]] == ["api"]

    assert first["owner_uid"] == uid and first["start_identity"] and first["engine_start_identity"]
    assert (root / "run/nagi" / ("rs-" + first["generation"] + ".sock")).exists()
    # A normal transient CLI exit leaves both child and supervisor alive.
    time.sleep(1.2)
    assert run("request-rewrite", "status")["actual"] == "active"
    run("request-rewrite", "rules", "add", "-", data=rule("mirror", "mirror.service.example"))
    second = json.loads(journal.read_text())
    assert second["generation"] != first["generation"]
    wait_for(lambda: not (root / "run/nagi" / ("rewrite-" + first["generation"] + ".sock")).exists())
    # Kill only the process owned by this isolated fixture. Engine readiness
    # loss must block recognized authorized targets; it must not remove branch.
    os.kill(second["pid"], signal.SIGKILL)
    blocked = wait_for(lambda: (lambda x: x if x["actual"] == "blocked" else None)(run("request-rewrite", "status")))
    assert blocked["enabled_intent"] is True
    recovered = run("request-rewrite", "enable")
    assert recovered["actual"] == "active" and recovered["generation"] != second["generation"]
    # The persistent supervisor rereads the isolated Linux root store. Removing
    # the fixture's public root must block instead of retaining a cached trust.
    os.rename(root / "public-root.pem", root / "public-root.backup")
    wait_for(lambda: run("request-rewrite", "status")["actual"] == "blocked")
    os.rename(root / "public-root.backup", root / "public-root.pem")
    recovered = run("request-rewrite", "enable")
    assert recovered["actual"] == "active"
    # Move only the isolated recovery record's renewal deadline into the past;
    # real time, CA validity and the system trust store remain unchanged.
    rotation = json.loads(journal.read_text())
    rotation["renew_at"] = "2000-01-01T00:00:00Z"
    journal.write_text(json.dumps(rotation))
    recovered = wait_for(lambda: (lambda x: x if x["actual"] == "active" and x["generation"] != rotation["generation"] else None)(run("request-rewrite", "status")))
    # Ordinary native reload retains rewrite generation/epoch.
    generation, epoch = recovered["generation"], recovered["engine_epoch"]
    run("config", "reload")
    after = run("request-rewrite", "status")
    assert (after["generation"], after["engine_epoch"], after["actual"]) == (generation, epoch, "active")
    if sys.platform.startswith("linux"):
        exited = json.loads(journal.read_text())
        os.kill(exited["engine_pid"], signal.SIGKILL)
        os.waitpid(exited["engine_pid"], 0)
        wait_for(lambda: not journal.exists())
        wait_for(lambda: not list((root / "run/nagi").glob("rewrite-*.sock")))
        # Explicit start reads retained intent and stages a fresh generation.
        run("start")
        assert run("request-rewrite", "status")["actual"] == "active"
    # Profile selection switches rules and intent with the same coordinator.
    run("profile", "import", "idle", profile)
    run("profile", "use", "idle")
    idle = run("request-rewrite", "status")
    assert idle["profile"] == "idle" and idle["actual"] == "disabled" and not idle["enabled_intent"]
    run("profile", "use", "work")
    assert run("request-rewrite", "status")["actual"] == "active"
    run("tun", "disable")
    assert not journal.exists()
    assert run("request-rewrite", "status")["enabled_intent"] is True
    # Disable intent after TUN teardown, then reactivate against only the
    # internal synthetic fixture TUN (never a real OS interface).
    run("request-rewrite", "disable")
    run("request-rewrite", "enable")
    # Zero enabled rules preserve enabled intent while removing the branch.
    run("request-rewrite", "rules", "disable", "api")
    run("request-rewrite", "rules", "disable", "mirror")
    assert run("request-rewrite", "status")["actual"] == "idle"
    assert not journal.exists()
    run("request-rewrite", "rules", "enable", "api")
    assert run("request-rewrite", "status")["actual"] == "active"
    run("quit")
    assert not journal.exists()
    assert not list((root / "run/nagi").glob("rewrite-*.sock"))
    wait_for(lambda: not list((root / "run/nagi").glob("rs-*.sock")))
    print("ordinary-UID CLI/child/supervisor, CAS rollback/cutover, crash block/recovery, trust refresh, certificate renewal, engine-exit cleanup, reload, TUN teardown and quit passed")
finally:
    try:
        run("request-rewrite", "disable")
    except Exception:
        pass
    try:
        run("stop")
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
