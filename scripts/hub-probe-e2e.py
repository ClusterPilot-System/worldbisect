#!/usr/bin/env python3
"""Exercise the operations probe against an isolated, real source-built hub."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request


WORKSPACE = "synthetic-probe"
PHASES = ("session", "create", "read", "delete", "verify_deleted")
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def command(arguments):
    result = subprocess.run(arguments, capture_output=True, text=True, timeout=15)
    if result.returncode != 0:
        raise AssertionError("hub setup or audit verification command failed")
    return result.stdout


def provision(binary, root, subject, role):
    output = command([binary, "credential", "--config", str(root / "config.json"),
                      "--subject", subject, "--kind", "service", "--workspace", WORKSPACE,
                      "--role", role, "--expires-in", "1h"])
    match = re.search(r"^Token: (\S+)$", output, re.MULTILINE)
    assert match, "credential command did not return a one-time token"
    token = match.group(1)
    path = root / (subject + ".token")
    path.write_text(token + "\n")
    path.chmod(0o600)
    return path, token


def start(binary, root):
    log = (root / "server.log").open("w+")
    process = subprocess.Popen([binary, "serve", "--config", str(root / "config.json"),
                                "--data", str(root / "data"), "--audit-dir", str(root / "audit"),
                                "--listen", "127.0.0.1:0"], stdout=log, stderr=log)
    try:
        for _ in range(100):
            if process.poll() is not None:
                raise AssertionError("hub exited during startup")
            log.seek(0)
            match = re.search(r"listening on (127\.0\.0\.1:\d+)", log.read())
            if match:
                return process, log, "http://" + match.group(1)
            time.sleep(0.05)
        raise AssertionError("hub did not start within five seconds")
    except BaseException:
        stop(process, log)
        raise


def stop(process, log):
    process.terminate()
    try:
        process.wait(timeout=12)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)
    finally:
        log.close()


def request(origin, path, token=None):
    headers = {} if token is None else {"Authorization": "Bearer " + token}
    req = urllib.request.Request(origin + path, headers=headers)
    with OPENER.open(req, timeout=5) as response:
        return response.status, json.load(response)


def run_probe(origin, token_file, tokens, workspace=WORKSPACE):
    script = Path(__file__).with_name("hub-probe.py")
    result = subprocess.run([sys.executable, str(script), "--hub", origin,
                             "--workspace", workspace, "--token-file", str(token_file),
                             "--timeout", "20"], capture_output=True, text=True, timeout=25)
    assert len(result.stdout) < 8192, "probe result exceeded its bounded diagnostic size"
    assert not result.stderr, "probe emitted unexpected stderr"
    assert all(token not in result.stdout for token in tokens), "probe leaked a credential"
    assert str(token_file.parent) not in result.stdout, "probe disclosed a private fixture path"
    value = json.loads(result.stdout)
    assert value["schema_version"] == 1, "unexpected probe schema"
    assert set(value["phases"]) == set(PHASES), "unexpected probe phases"
    assert result.returncode == (0 if value["ok"] else 1), "probe status and exit code disagree"
    assert all(status in ("ok", "failed", "skipped") for status in value["phases"].values())
    return value


def expect_session_failure(value, code, http_status=None):
    assert value["ok"] is False, "invalid session was accepted"
    assert value["error"]["phase"] == "session", "failure happened after the session check"
    assert value["error"]["code"] == code, "unexpected session failure code"
    if http_status is not None:
        assert value["error"]["http_status"] == http_status, "unexpected HTTP failure status"
    assert value["phases"]["session"] == "failed"
    assert all(value["phases"][phase] == "skipped" for phase in PHASES[1:])
    assert value["cleanup"] == {"attempted": False, "outcome": "not_needed"}
    assert value["residue_possible"] is False


def events(audit_dir):
    return [json.loads(line)["event"] for segment in sorted(audit_dir.glob("*.jsonl"))
            for line in segment.read_text().splitlines()]


def creations(audit_dir):
    return sum(event["action"] == "reports.create" for event in events(audit_dir))


def assert_empty(origin, token, data_dir):
    status, value = request(origin, "/api/v1/reports", token)
    assert status == 200 and value["reports"] == [], "synthetic report remained in the workspace"
    assert not list(data_dir.glob("*/*.json")), "synthetic report remained on disk"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", default="bin/worldbisect-hub", type=Path)
    args = parser.parse_args()
    binary = str(args.binary.resolve())
    os.umask(0o077)

    with tempfile.TemporaryDirectory(prefix="worldbisect-hub-probe-e2e-") as temporary:
        root = Path(temporary)
        command([binary, "init", "--config", str(root / "config.json")])
        editor_file, editor = provision(binary, root, "probe-editor", "editor")
        viewer_file, viewer = provision(binary, root, "probe-viewer", "viewer")
        expired_file, expired = provision(binary, root, "probe-expired", "editor")
        tokens = (editor, viewer, expired)
        config_path = root / "config.json"
        config = json.loads(config_path.read_text())
        for key in config["keys"]:
            if key["subject_id"] == "probe-expired":
                # A fixed past expiry avoids a timing-sensitive sleep at the boundary.
                key["expires_at"] = "2000-01-01T00:00:00Z"
        config_path.write_text(json.dumps(config))
        audit_dir = root / "audit"
        data_dir = root / "data"
        held_audit = root / "audit-unavailable"
        process, log, origin = start(binary, root)
        try:
            assert request(origin, "/healthz") == (200, {"status": "ok"})
            assert_empty(origin, editor, data_dir)
            good = run_probe(origin, editor_file, tokens)
            assert good["ok"] and good["error"] is None, "healthy lifecycle probe failed"
            assert all(good["phases"][phase] == "ok" for phase in PHASES)
            assert good["residue_possible"] is False
            assert_empty(origin, editor, data_dir)
            before_rejections = creations(audit_dir)
            expect_session_failure(run_probe(origin, viewer_file, tokens), "scope_missing")
            expect_session_failure(run_probe(origin, editor_file, tokens, "another-workspace"), "workspace_mismatch")
            expect_session_failure(run_probe(origin, expired_file, tokens), "http_error", 401)
            assert creations(audit_dir) == before_rejections, "rejected session attempted a report write"
            assert_empty(origin, editor, data_dir)

            # Controlled local I/O fixture: the synthetic empty workspace cannot
            # be opened as a directory. This works as root and as an ordinary user.
            workspace_dir = data_dir / hashlib.sha256(WORKSPACE.encode()).hexdigest()
            held_workspace = root / "workspace-unavailable"
            workspace_dir.rename(held_workspace)
            try:
                workspace_dir.write_text("deliberate integration storage outage\n")
                assert request(origin, "/healthz") == (200, {"status": "ok"})
                failed_store = run_probe(origin, editor_file, tokens)
                assert not failed_store["ok"] and failed_store["phases"]["session"] == "ok"
                assert failed_store["error"] == {"phase": "create", "code": "http_error", "http_status": 500}
                assert failed_store["phases"]["create"] == "failed"
            finally:
                if workspace_dir.is_file():
                    workspace_dir.unlink()
                held_workspace.rename(workspace_dir)
            assert_empty(origin, editor, data_dir)

            # Remove only this test's audit directory from its configured path.
            # The server must refuse authenticated work even though liveness is OK.
            # Restore after stopping because an audit I/O fault remains latched.
            checkpoint = json.loads((audit_dir / "state.json").read_text())["head_hash"]
            audit_dir.rename(held_audit)
            assert request(origin, "/healthz") == (200, {"status": "ok"})
            expect_session_failure(run_probe(origin, editor_file, tokens), "http_error", 503)
            assert request(origin, "/healthz") == (200, {"status": "ok"})
        finally:
            stop(process, log)
            if held_audit.exists():
                held_audit.rename(audit_dir)

        verification = json.loads(command([binary, "audit-verify", "--audit-dir", str(audit_dir),
                                            "--expected-head", checkpoint]))
        recorded = events(audit_dir)
        assert verification["sequence"] == len(recorded), "unexpected audit event count"
        assert sum(event["action"] == "reports.create" and event["outcome"] == "201" for event in recorded) == 1
        assert sum(event["action"] == "reports.create" and event["outcome"] == "500" for event in recorded) == 1
        assert sum(event["action"] == "reports.delete" and event["outcome"] == "204" for event in recorded) == 1
        assert any(event["action"] == "reports.get" and event["outcome"] == "404" for event in recorded)
        assert any(event["action"] == "auth.failure" and event["outcome"] == "denied" for event in recorded)
        assert not list(data_dir.glob("*/*.json")), "outage fixture left a synthetic report"
        print(json.dumps({"result": "PASS", "scope": "local real-hub synthetic operations check; not a managed deployment or SLO",
                          "lifecycle_phases": list(PHASES), "reports_remaining": 0,
                          "rejected_before_write": ["viewer", "wrong_workspace", "expired"],
                          "storage_failure_detected_with_healthz_200": True,
                          "audit_failure_detected_with_healthz_200": True,
                          "audit_chain_verified": True, "audit_events": len(recorded)}, sort_keys=True))


if __name__ == "__main__":
    try:
        main()
    except AssertionError as error:
        print("hub probe E2E: FAIL: " + str(error), file=sys.stderr)
        sys.exit(1)
    except (OSError, ValueError, KeyError, subprocess.SubprocessError):
        # Do not echo captured commands, credentials, URLs or private fixture paths.
        print("hub probe E2E: FAIL: fixture setup, response or subprocess failed", file=sys.stderr)
        sys.exit(1)
