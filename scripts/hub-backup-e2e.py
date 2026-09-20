#!/usr/bin/env python3
"""Run an isolated, encrypted restore drill against the real hub binary."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


spec = importlib.util.spec_from_file_location("hub_backup", Path(__file__).with_name("hub-backup.py"))
backup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(backup)


def start(binary, root):
    log = (root / "server.log").open("w+")
    process = subprocess.Popen([binary, "serve", "--config", str(root / "config.json"),
                                "--data", str(root / "data"), "--audit-dir", str(root / "audit"),
                                "--listen", "127.0.0.1:0"], stdout=log, stderr=log)
    try:
        for _ in range(100):
            if process.poll() is not None:
                raise RuntimeError("hub exited during drill startup")
            log.seek(0)
            match = re.search(r"listening on (127\.0\.0\.1:\d+)", log.read())
            if match:
                return process, log, "http://" + match.group(1)
            time.sleep(0.05)
        raise RuntimeError("hub startup timed out")
    except BaseException:
        stop(process, log)
        raise


def stop(process, log):
    process.terminate()
    try:
        process.wait(timeout=12)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait()
    log.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", default="bin/worldbisect-hub")
    parser.add_argument("--age", default="age")
    parser.add_argument("--age-keygen", default="age-keygen")
    args = parser.parse_args()
    binary = str(Path(args.binary).resolve())
    os.umask(0o077)
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def request(origin, token, method="GET", payload=None):
        data = None if payload is None else json.dumps(payload).encode()
        req = urllib.request.Request(origin + "/api/v1/reports", method=method, data=data,
                                     headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
        with opener.open(req, timeout=5) as response:
            return json.load(response)

    with tempfile.TemporaryDirectory(prefix="worldbisect-hub-restore-drill-") as temporary:
        root = Path(temporary)
        original = root / "original"
        original.mkdir(mode=0o700)
        initialized = subprocess.run([binary, "init", "--config", str(original / "config.json")],
                                     check=True, capture_output=True, text=True, timeout=10)
        reader = re.search(r"Read token: (\S+)", initialized.stdout).group(1)
        writer = re.search(r"Write token: (\S+)", initialized.stdout).group(1)
        config_path = original / "config.json"
        config = json.loads(config_path.read_text())
        config["subjects"].append({"id": "ci-drill", "kind": "service"})
        config["memberships"].append({"subject_id": "ci-drill", "workspace": "default", "role": "publisher"})
        config["ci_publishers"] = [{"subject_id": "ci-drill", "workspace": "default", "audience": "https://hub.example/restore-drill",
                                    "repository": "example/backup-drill", "repository_id": "123", "repository_owner_id": "456",
                                    "workflow_ref": "example/backup-drill/.github/workflows/ci.yml@refs/heads/main", "ref": "refs/heads/main",
                                    "subject": "repo:example@456/backup-drill@123:ref:refs/heads/main"}]
        config_path.write_text(json.dumps(config))
        identity = root / "identity.txt"
        subprocess.run([args.age_keygen, "-o", str(identity)], check=True, capture_output=True, timeout=10)
        recipient = subprocess.run([args.age_keygen, "-y", str(identity)], check=True, capture_output=True, text=True, timeout=10).stdout.strip()
        identity.chmod(0o600)
        encrypted = root / "snapshot.age"
        process, log, origin = start(binary, original)
        try:
            for number in range(10):
                request(origin, writer, "POST", {"repository": "example/backup-drill", "check_name": f"Synthetic check {number}",
                        "status": "UNPROVEN", "finding": "Synthetic restore-drill summary; not a diagnosis.",
                        "tested": "Backup persistence only; no causal experiment.", "next_step": "Verify recovery integrity.", "experiments": 0})
            expected = request(origin, reader)["reports"]
            try:
                backup.backup(original / "config.json", original / "data", original / "audit", encrypted, recipient, age=args.age)
            except ValueError as error:
                assert "hub is running" in str(error), str(error)
            else:
                raise AssertionError("live server backup must fail")
        finally:
            stop(process, log)

        verified = subprocess.run([binary, "audit-verify", "--audit-dir", str(original / "audit")],
                                  check=True, capture_output=True, text=True, timeout=10)
        audit = json.loads(verified.stdout)
        creation = backup.backup(original / "config.json", original / "data", original / "audit", encrypted, recipient, age=args.age)
        restored = root / "restored"
        recovery = backup.restore(encrypted, identity, restored, age=args.age)
        subprocess.run([binary, "audit-verify", "--audit-dir", str(restored / "audit"), "--expected-head", audit["head_hash"]],
                       check=True, capture_output=True, text=True, timeout=10)
        process, log, origin = start(binary, restored)
        try:
            actual = request(origin, reader)["reports"]
            assert actual == expected, "restored report IDs, timestamps and summaries differ"
            try:
                request(origin, reader, "POST", {})
            except urllib.error.HTTPError as error:
                assert error.code == 403, error.code
            else:
                raise AssertionError("restored reader must not write")
            req = urllib.request.Request(origin + "/api/v1/ci/reports", data=b"{}", method="POST",
                                         headers={"Authorization": "Bearer synthetic-not-a-real-jwt", "Content-Type": "application/json"})
            try:
                opener.open(req, timeout=5)
            except urllib.error.HTTPError as error:
                assert error.code == 503, "restored CI publishing must be quarantined before token processing"
                assert int(error.headers.get("Retry-After", "0")) >= 650
                error.close()
            else:
                raise AssertionError("restore quarantine was not enforced")
        finally:
            stop(process, log)
        print(json.dumps({"result": "PASS", "scope": "local synthetic single-host restore drill; not an HA/SLO claim",
                          "reports_restored": len(expected), "audit_head_preserved": True,
                          "live_backup_rejected": True, "read_only_preserved": True,
                          "ci_restore_quarantine_enforced": True,
                          "backup": creation, "restore": recovery}, sort_keys=True))


if __name__ == "__main__":
    main()
