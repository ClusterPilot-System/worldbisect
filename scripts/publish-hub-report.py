#!/usr/bin/env python3
"""Explicitly publish a bounded, allowlisted report summary to the team hub."""
import argparse
import ipaddress
import json
import os
from pathlib import Path
import re
import sys
import urllib.error
import urllib.parse
import urllib.request


MAX_REPORT = 4 * 1024 * 1024
STATUSES = {"PROVEN", "SUPPORTED", "CORRELATED", "UNPROVEN"}


def endpoint(base):
    url = urllib.parse.urlsplit(base)
    if url.username or url.password or url.query or url.fragment or url.path not in ("", "/"):
        raise ValueError("hub URL must be an origin without credentials, path, query or fragment")
    if not url.hostname or url.scheme not in ("http", "https"):
        raise ValueError("hub URL must use HTTPS (HTTP allowed only on a loopback IP)")
    if url.scheme == "http":
        try:
            loopback = ipaddress.ip_address(url.hostname).is_loopback
        except ValueError:
            loopback = False
        if not loopback:
            raise ValueError("HTTP is allowed only on a literal loopback IP")
    _ = url.port
    return base.rstrip("/") + "/api/v1/reports"


def project(report, repository, check_name, commit_sha="", run_url=""):
    if not isinstance(report, dict) or type(report.get("schema_version")) is not int or report.get("schema_version") != 1 or report.get("format") != "worldbisect.analysis-report.v1":
        raise ValueError("expected a WorldBisect analysis-report.v1 JSON report")
    status = report.get("status")
    if status not in STATUSES:
        raise ValueError("unsupported diagnosis status")
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
        raise ValueError("repository must be owner/name")
    if not 1 <= len(check_name) <= 120 or any(ord(c) < 32 for c in check_name):
        raise ValueError("check name must contain 1-120 printable characters")
    if commit_sha and not re.fullmatch(r"[0-9a-fA-F]{40}|[0-9a-fA-F]{64}", commit_sha):
        raise ValueError("commit SHA must have 40 or 64 hexadecimal characters")
    if run_url and not re.fullmatch(r"https://github\.com/" + re.escape(repository) + r"/actions/runs/[0-9]+", run_url):
        raise ValueError("run URL must be a GitHub Actions run for the supplied repository")
    proof = report.get("proof")
    evidence = report.get("evidence")
    causes = report.get("cause")
    if not isinstance(proof, dict) or not isinstance(evidence, dict) or not isinstance(causes, list):
        raise ValueError("report must contain proof, evidence and cause fields")
    for field in ("forward_verified", "reverse_verified", "minimal_in_model"):
        if type(proof.get(field)) is not bool:
            raise ValueError("proof checks must be booleans")
    experiments = evidence.get("experiment_count")
    if type(experiments) is not int or not 0 <= experiments <= 1000000:
        raise ValueError("invalid experiment count")
    analysis_id = report.get("analysis_id", "")
    if not isinstance(analysis_id, str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,128}", analysis_id):
        raise ValueError("invalid analysis identifier")
    if status == "PROVEN" and not all(proof[k] for k in ("forward_verified", "reverse_verified", "minimal_in_model")):
        raise ValueError("PROVEN report is missing completed proof checks")
    # Never forward paths, factor values, descriptions, commands, environment,
    # output, free-text summaries or arbitrary extensions from the source report.
    finding = (f"{len(causes)} selected factor(s) reported; consult the CI artifact for details."
               if causes else "No confirmed factor was reported; consult the CI artifact.")
    tested = (f"{experiments} experiments; repair verified: {str(proof['forward_verified']).lower()}; "
              f"reverse reproduction verified: {str(proof['reverse_verified']).lower()}; "
              f"minimal in the tested model: {str(proof['minimal_in_model']).lower()}.")
    next_step = ("Review the identified inputs in the CI artifact and rerun the check after correcting them."
                 if status in ("PROVEN", "SUPPORTED") else
                 "Check reproducibility and evidence boundaries in the CI artifact before attributing a cause.")
    return dict(repository=repository, check_name=check_name, commit_sha=commit_sha,
                run_url=run_url, status=status, finding=finding, tested=tested,
                next_step=next_step, experiments=experiments, analysis_id=analysis_id)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise ValueError("hub redirects are refused; use the final trusted HTTPS origin")


def publish(url, payload, token):
    if not token or any(c.isspace() for c in token):
        raise ValueError("WORLDBISECT_HUB_TOKEN is missing or invalid")
    request = urllib.request.Request(endpoint(url), data=json.dumps(payload).encode(),
                                     headers={"Authorization": "Bearer " + token,
                                              "Content-Type": "application/json"}, method="POST")
    # No ambient proxy: avoid accidentally sending a token through an unreviewed
    # environment-provided proxy. No redirects or automatic retry after a write.
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    try:
        with opener.open(request, timeout=15) as response:
            raw = response.read(16385)
            if response.status != 201 or len(raw) > 16384:
                raise ValueError("hub returned an unexpected response")
            value = json.loads(raw)
            identifier = value.get("id") if isinstance(value, dict) else None
            if not isinstance(identifier, str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,128}", identifier):
                raise ValueError("hub response is missing a valid report ID")
            return identifier
    except urllib.error.HTTPError as exc:
        raise ValueError(f"hub rejected upload (HTTP {exc.code}); check credentials, quota and payload") from None
    except urllib.error.URLError:
        raise ValueError("hub connection failed; verify its address and TLS certificate") from None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--report", required=True, type=Path)
    parser.add_argument("--hub", default=os.environ.get("WORLDBISECT_HUB_URL", ""))
    parser.add_argument("--repository", required=True)
    parser.add_argument("--check", required=True)
    parser.add_argument("--commit", default="")
    parser.add_argument("--run-url", default="")
    parser.add_argument("--dry-run", action="store_true", help="print the exact allowlisted payload without uploading")
    args = parser.parse_args()
    try:
        with args.report.open("rb") as stream:
            raw = stream.read(MAX_REPORT + 1)
        if len(raw) > MAX_REPORT:
            raise ValueError("source report exceeds 4 MiB")
        payload = project(json.loads(raw), args.repository, args.check, args.commit, args.run_url)
        if args.dry_run:
            print(json.dumps(payload, indent=2))
        else:
            identifier = publish(args.hub, payload, os.environ.get("WORLDBISECT_HUB_TOKEN", ""))
            print("Published diagnosis " + identifier)
        return 0
    except (ValueError, OSError, TypeError) as exc:
        # Do not print transport response bodies or request objects containing auth.
        print("worldbisect hub publisher: " + str(exc), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
