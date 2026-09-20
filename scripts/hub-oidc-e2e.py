#!/usr/bin/env python3
"""Trusted push CI only: exercise the real GitHub issuer and a temporary local hub.

The temporary test rule deliberately uses this job's subject. This is a test
harness, never a production trust-provisioning or auto-enrollment mechanism.
"""
import argparse
import base64
import importlib.util
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


spec = importlib.util.spec_from_file_location("publisher_oidc", Path(__file__).with_name("publish-hub-oidc.py"))
publisher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publisher)


def start(binary, root):
    log = (root / "server.log").open("w+")
    process = subprocess.Popen([binary, "serve", "--config", str(root / "config.json"),
                                "--data", str(root / "data"), "--listen", "127.0.0.1:0"],
                               stdout=log, stderr=log)
    try:
        for _ in range(100):
            if process.poll() is not None:
                raise RuntimeError("temporary OIDC hub exited during startup")
            log.seek(0)
            match = re.search(r"listening on (127\.0\.0\.1:\d+)", log.read())
            if match:
                return process, log, "http://" + match.group(1)
            time.sleep(0.05)
        raise RuntimeError("temporary OIDC hub startup timed out")
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


def expect_replay(origin, payload, token):
    request = urllib.request.Request(origin + "/api/v1/ci/reports", data=json.dumps(payload).encode(),
                                     headers={"Authorization": "Bearer " + token,
                                              "Content-Type": "application/json"}, method="POST")
    try:
        with publisher.opener().open(request, timeout=15):
            raise RuntimeError("OIDC replay was accepted")
    except urllib.error.HTTPError as exc:
        if exc.code != 409:
            raise RuntimeError("OIDC replay did not produce the expected rejection") from None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, default=Path("bin/worldbisect-hub"))
    parser.add_argument("--report", type=Path, required=True)
    args = parser.parse_args()
    repo, sha, run_url = publisher.workflow_context(os.environ)
    repository_id = os.environ["GITHUB_REPOSITORY_ID"]
    owner_id = os.environ["GITHUB_REPOSITORY_OWNER_ID"]
    workflow_ref = os.environ["GITHUB_WORKFLOW_REF"]
    audience = "https://worldbisect.invalid/ci-smoke/" + os.environ["GITHUB_RUN_ID"] + "/" + secrets.token_hex(16)
    print("live OIDC E2E: requesting short-lived GitHub identity", flush=True)
    token = publisher.request_identity(audience, os.environ)
    # The hub, not this decode, verifies origin. Only the subject is extracted to
    # accommodate GitHub's legacy/custom/immutable subject configurations.
    encoded = token.split(".")[1]
    claims = json.loads(base64.urlsafe_b64decode(encoded + "=" * (-len(encoded) % 4)))
    payload = publisher.summary.project(json.loads(args.report.read_text()), repo, "live OIDC smoke", sha, run_url)
    binary = str(args.binary.resolve())
    with tempfile.TemporaryDirectory(prefix="worldbisect-live-oidc-") as tmp:
        root = Path(tmp)
        config_path = root / "config.json"
        initialized = subprocess.run([binary, "init", "--config", str(config_path)],
                                     capture_output=True, text=True, check=True, timeout=10)
        reader = re.search(r"Read token: (\S+)", initialized.stdout).group(1)
        config = json.loads(config_path.read_text())
        config["subjects"].append({"id": "live-ci", "kind": "service"})
        config["memberships"].append({"subject_id": "live-ci", "workspace": "default", "role": "publisher"})
        config["ci_publishers"] = [{
            "subject_id": "live-ci", "workspace": "default", "audience": audience,
            "repository": repo, "repository_id": repository_id, "repository_owner_id": owner_id,
            "workflow_ref": workflow_ref, "ref": os.environ["GITHUB_REF"], "subject": claims["sub"]}]
        config_path.write_text(json.dumps(config))
        process, log, origin = start(binary, root)
        try:
            print("live OIDC E2E: verifying issuer signature and configured origin", flush=True)
            identifier = publisher.publish(origin, payload, token)
            request = urllib.request.Request(origin + "/api/v1/reports/" + identifier,
                                             headers={"Authorization": "Bearer " + reader})
            with publisher.opener().open(request, timeout=5) as response:
                report = json.load(response)
            assert report["publisher"]["provider"] == "github-actions-oidc"
            assert report["publisher"]["repository_id"] == repository_id
            assert report["publisher"]["repository_owner_id"] == owner_id
            assert report["publisher"]["workflow_ref"] == workflow_ref
            assert report["publisher"]["ref"] == os.environ["GITHUB_REF"]
            assert report["publisher"]["run_attempt"] == os.environ["GITHUB_RUN_ATTEMPT"]
            assert report["status"] == payload["status"]
            assert report["commit_sha"] == sha and report["run_url"] == run_url
            expect_replay(origin, payload, token)
        finally:
            stop(process, log)
        process, log, origin = start(binary, root)
        try:
            print("live OIDC E2E: verifying replay rejection after process restart", flush=True)
            expect_replay(origin, payload, token)
        finally:
            stop(process, log)
    print("live OIDC E2E: GitHub identity -> verified origin -> persisted summary -> restart replay rejection: PASS")


if __name__ == "__main__":
    try:
        main()
    except Exception:
        # Do not include provider responses, token claims or subprocess output.
        raise SystemExit("live OIDC E2E failed; inspect job permissions and the fixed trust boundary") from None
