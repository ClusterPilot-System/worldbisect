#!/usr/bin/env python3
"""Publish an allowlisted summary using one short-lived GitHub Actions identity."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import sys
import urllib.error
import urllib.parse
import urllib.request


_spec = importlib.util.spec_from_file_location("hub_summary", Path(__file__).with_name("publish-hub-report.py"))
summary = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(summary)


def identity_url(raw, audience):
    url = urllib.parse.urlsplit(raw)
    host = url.hostname or ""
    if (url.scheme != "https" or url.username or url.password or url.fragment or
            url.port not in (None, 443) or len(raw) > 4096 or
            not re.fullmatch(r"[a-z0-9.-]+\.actions\.githubusercontent\.com", host) or
            not url.path.endswith("/idtoken")):
        raise ValueError("OIDC request URL must be GitHub's HTTPS Actions idtoken endpoint")
    aud = urllib.parse.urlsplit(audience)
    if (aud.scheme != "https" or not aud.hostname or aud.username or aud.password or
            aud.query or aud.fragment or len(audience) > 256):
        raise ValueError("audience must be the exact HTTPS identifier configured by the hub operator")
    query = urllib.parse.parse_qsl(url.query, keep_blank_values=True, strict_parsing=True)
    if not query or any(k != "api-version" for k, _ in query) or len(query) != 1:
        raise ValueError("unexpected parameters in GitHub OIDC request URL")
    return urllib.parse.urlunsplit((url.scheme, url.netloc, url.path,
                                   urllib.parse.urlencode(query + [("audience", audience)]), ""))


def workflow_context(env):
    if (env.get("GITHUB_ACTIONS") != "true" or env.get("GITHUB_SERVER_URL") != "https://github.com" or
            env.get("GITHUB_EVENT_NAME") != "push" or env.get("RUNNER_ENVIRONMENT") != "github-hosted" or
            not re.fullmatch(r"refs/heads/[A-Za-z0-9_./-]{1,200}", env.get("GITHUB_REF", ""))):
        raise ValueError("OIDC publishing supports GitHub-hosted github.com branch push jobs only")
    repository, sha, run = (env.get(k, "") for k in ("GITHUB_REPOSITORY", "GITHUB_SHA", "GITHUB_RUN_ID"))
    if (not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository) or
            not re.fullmatch(r"[a-fA-F0-9]{40}|[a-fA-F0-9]{64}", sha) or
            not re.fullmatch(r"[1-9][0-9]{0,19}", run)):
        raise ValueError("required GitHub repository, commit or run metadata is missing")
    return repository, sha, f"https://github.com/{repository}/actions/runs/{run}"


def opener():
    return urllib.request.build_opener(urllib.request.ProxyHandler({}), summary.NoRedirect())


def request_identity(audience, env):
    url = identity_url(env.get("ACTIONS_ID_TOKEN_REQUEST_URL", ""), audience)
    credential = env.get("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "")
    if not credential or len(credential) > 8192 or any(c.isspace() for c in credential):
        raise ValueError("GitHub OIDC credentials are missing; grant this job id-token: write")
    req = urllib.request.Request(url, headers={"Authorization": "Bearer " + credential})
    try:
        with opener().open(req, timeout=10) as response:
            raw = response.read(16385)
            if response.status != 200 or len(raw) > 16384:
                raise ValueError("unexpected GitHub OIDC response")
            document = json.loads(raw)
            token = document.get("value") if isinstance(document, dict) else None
            if (not isinstance(token, str) or len(token) > 12288 or
                    not re.fullmatch(r"[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+", token)):
                raise ValueError("GitHub did not return a bounded signed identity token")
            return token
    except urllib.error.HTTPError as exc:
        raise ValueError(f"GitHub rejected the identity request (HTTP {exc.code})") from None
    except urllib.error.URLError:
        raise ValueError("GitHub OIDC connection failed") from None


def publish(hub, payload, token):
    endpoint = summary.endpoint(hub).removesuffix("/reports") + "/ci/reports"
    request = urllib.request.Request(endpoint, data=json.dumps(payload).encode(), method="POST",
                                     headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
    try:
        with opener().open(request, timeout=15) as response:
            raw = response.read(16385)
            if response.status != 201 or len(raw) > 16384:
                raise ValueError("hub returned an unexpected CI response")
            document = json.loads(raw)
            identifier = document.get("id") if isinstance(document, dict) else None
            if not isinstance(identifier, str) or not re.fullmatch(r"[a-f0-9]{32}", identifier):
                raise ValueError("hub response is missing the report identifier")
            return identifier
    except urllib.error.HTTPError as exc:
        raise ValueError(f"hub rejected CI upload (HTTP {exc.code}); check trust, replay and capacity limits") from None
    except urllib.error.URLError:
        raise ValueError("hub connection failed; inspect existing reports before retrying") from None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--report", required=True, type=Path)
    parser.add_argument("--check", required=True)
    parser.add_argument("--hub", default=os.environ.get("WORLDBISECT_HUB_URL", ""))
    parser.add_argument("--audience", required=True)
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    try:
        repo, sha, run = workflow_context(os.environ)
        with args.report.open("rb") as stream:
            raw = stream.read(summary.MAX_REPORT + 1)
        if len(raw) > summary.MAX_REPORT:
            raise ValueError("source report exceeds 4 MiB")
        payload = summary.project(json.loads(raw), repo, args.check, sha, run)
        if args.dry_run:
            print(json.dumps(payload, indent=2))
        else:
            summary.endpoint(args.hub)  # Validate destination before requesting credentials.
            token = request_identity(args.audience, os.environ)
            identifier = publish(args.hub, payload, token)
            print("Published diagnosis " + identifier + " with verified GitHub Actions origin")
        return 0
    except (ValueError, OSError, TypeError):
        # Never echo request URLs, tokens, response bodies or arbitrary exceptions.
        print("WorldBisect CI publish failed; verify the job permissions, configured trust and hub availability. "
              "Check existing reports before retrying.", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
