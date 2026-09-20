#!/usr/bin/env python3
"""Exercise the real hub binary and publisher using an engine-generated report."""
import argparse
import importlib.util
import json
from pathlib import Path
import re
import subprocess
import tempfile
import time
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", default="bin/worldbisect-hub", type=Path)
    parser.add_argument("--report", required=True, type=Path)
    args = parser.parse_args()
    binary = str(args.binary.resolve())
    report = args.report.resolve()
    spec = importlib.util.spec_from_file_location("publisher", Path(__file__).with_name("publish-hub-report.py"))
    publisher = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(publisher)
    with tempfile.TemporaryDirectory(prefix="worldbisect-hub-e2e-") as tmp:
        root = Path(tmp)
        init = subprocess.run([binary, "init", "--config", str(root / "config.json")],
                              capture_output=True, text=True, check=True, timeout=10)
        reader = re.search(r"Read token: (\S+)", init.stdout).group(1)
        writer = re.search(r"Write token: (\S+)", init.stdout).group(1)
        with (root / "server.log").open("w+") as log:
            process = subprocess.Popen([binary, "serve", "--config", str(root / "config.json"),
                                        "--data", str(root / "data"), "--listen", "127.0.0.1:0"],
                                       stdout=log, stderr=log)
            try:
                origin = None
                for _ in range(100):
                    if process.poll() is not None:
                        raise RuntimeError("hub exited during startup")
                    log.seek(0)
                    match = re.search(r"listening on (127\.0\.0\.1:\d+)", log.read())
                    if match:
                        origin = "http://" + match.group(1)
                        break
                    time.sleep(0.05)
                if origin is None:
                    raise RuntimeError("hub did not start within five seconds")
                payload = publisher.project(json.loads(report.read_text()), "example/ci", "engine-e2e")
                identifier = publisher.publish(origin, payload, writer)
                opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
                request = urllib.request.Request(origin + "/api/v1/reports", headers={"Authorization": "Bearer " + reader})
                with opener.open(request, timeout=5) as response:
                    reports = json.load(response)["reports"]
                assert len(reports) == 1 and reports[0]["id"] == identifier
                assert reports[0]["status"] == payload["status"]
                assert reports[0]["experiments"] == payload["experiments"]
                request = urllib.request.Request(origin + "/api/v1/reports/" + identifier,
                                                 headers={"Authorization": "Bearer " + writer}, method="DELETE")
                with opener.open(request, timeout=5) as response:
                    assert response.status == 204
                print("hub E2E: engine report -> bounded publisher -> authenticated store/list/delete: PASS")
            finally:
                process.terminate()
                try:
                    process.wait(timeout=12)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()


if __name__ == "__main__":
    main()
