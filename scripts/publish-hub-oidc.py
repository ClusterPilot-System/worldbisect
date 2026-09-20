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


SAFE_ERROR_CODES = frozenset({
    "OIDC_URL_MISSING", "OIDC_URL_SIZE", "OIDC_URL_FORMAT", "OIDC_URL_ORIGIN",
    "OIDC_URL_HOST", "OIDC_URL_PATH", "OIDC_URL_QUERY", "OIDC_AUDIENCE",
    "OIDC_CREDENTIAL_MISSING", "OIDC_CREDENTIAL_SIZE", "OIDC_CREDENTIAL_FORMAT",
    "OIDC_HTTP_AUTH", "OIDC_HTTP_STATUS", "OIDC_TRANSPORT", "OIDC_REDIRECT",
    "OIDC_RESPONSE_SIZE", "OIDC_RESPONSE_JSON", "OIDC_RESPONSE_TOKEN",
    "HUB_HTTP_IDENTITY", "HUB_HTTP_TRUST", "HUB_HTTP_STATUS", "HUB_TRANSPORT", "HUB_RESPONSE",
    "PUBLISHER_ERROR",
})


class PublisherError(ValueError):
    """A fixed diagnostic category, never provider-controlled text or a URL."""
    def __init__(self, code):
        self.code = code if code in SAFE_ERROR_CODES else "PUBLISHER_ERROR"
        super().__init__(self.code)


def safe_error_code(error):
    if isinstance(error, PublisherError) and error.code in SAFE_ERROR_CODES:
        return error.code
    return "PUBLISHER_ERROR"


def identity_url(raw, audience):
    if not raw:
        raise PublisherError("OIDC_URL_MISSING")
    if len(raw) > 4096:
        raise PublisherError("OIDC_URL_SIZE")
    try:
        url = urllib.parse.urlsplit(raw)
        port = url.port
    except ValueError:
        raise PublisherError("OIDC_URL_FORMAT") from None
    host = url.hostname or ""
    if (url.scheme != "https" or url.username or url.password or url.fragment or
            port not in (None, 443)):
        raise PublisherError("OIDC_URL_ORIGIN")
    if not re.fullmatch(r"[a-z0-9.-]+\.actions\.githubusercontent\.com", host):
        raise PublisherError("OIDC_URL_HOST")
    # GitHub's runner supplies GenerateIdTokenUrl and its official toolkit treats
    # the route as opaque. A fixed /idtoken suffix is not a provider contract.
    # Keep transport and origin restrictions; never follow a provider redirect.
    if (not url.path.startswith("/") or "\\" in url.path or
            any(ord(c) < 33 or ord(c) == 127 for c in raw)):
        raise PublisherError("OIDC_URL_PATH")
    try:
        aud = urllib.parse.urlsplit(audience)
    except ValueError:
        raise PublisherError("OIDC_AUDIENCE") from None
    if (aud.scheme != "https" or not aud.hostname or aud.username or aud.password or
            aud.query or aud.fragment or len(audience) > 256):
        raise PublisherError("OIDC_AUDIENCE")
    try:
        query = urllib.parse.parse_qsl(url.query, keep_blank_values=True, strict_parsing=True)
    except ValueError:
        raise PublisherError("OIDC_URL_QUERY") from None
    if not query or any(k != "api-version" for k, _ in query) or len(query) != 1:
        raise PublisherError("OIDC_URL_QUERY")
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


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise PublisherError("OIDC_REDIRECT")


def opener():
    return urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())


def request_identity(audience, env):
    url = identity_url(env.get("ACTIONS_ID_TOKEN_REQUEST_URL", ""), audience)
    credential = env.get("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "")
    if not credential:
        raise PublisherError("OIDC_CREDENTIAL_MISSING")
    if len(credential) > 8192:
        raise PublisherError("OIDC_CREDENTIAL_SIZE")
    if any(c.isspace() for c in credential):
        raise PublisherError("OIDC_CREDENTIAL_FORMAT")
    req = urllib.request.Request(url, headers={"Authorization": "Bearer " + credential})
    try:
        with opener().open(req, timeout=10) as response:
            raw = response.read(16385)
            if response.status != 200:
                raise PublisherError("OIDC_HTTP_STATUS")
            if len(raw) > 16384:
                raise PublisherError("OIDC_RESPONSE_SIZE")
            try:
                document = json.loads(raw)
            except (ValueError, UnicodeError):
                raise PublisherError("OIDC_RESPONSE_JSON") from None
            token = document.get("value") if isinstance(document, dict) else None
            if (not isinstance(token, str) or len(token) > 12288 or
                    not re.fullmatch(r"[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+", token)):
                raise PublisherError("OIDC_RESPONSE_TOKEN")
            return token
    except urllib.error.HTTPError as exc:
        code = "OIDC_HTTP_AUTH" if exc.code in (401, 403) else "OIDC_HTTP_STATUS"
        raise PublisherError(code) from None
    except (urllib.error.URLError, TimeoutError):
        raise PublisherError("OIDC_TRANSPORT") from None


def publish(hub, payload, token):
    endpoint = summary.endpoint(hub).removesuffix("/reports") + "/ci/reports"
    request = urllib.request.Request(endpoint, data=json.dumps(payload).encode(), method="POST",
                                     headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
    try:
        with opener().open(request, timeout=15) as response:
            raw = response.read(16385)
            if response.status != 201 or len(raw) > 16384:
                raise PublisherError("HUB_RESPONSE")
            try:
                document = json.loads(raw)
            except (ValueError, UnicodeError):
                raise PublisherError("HUB_RESPONSE") from None
            identifier = document.get("id") if isinstance(document, dict) else None
            if not isinstance(identifier, str) or not re.fullmatch(r"[a-f0-9]{32}", identifier):
                raise PublisherError("HUB_RESPONSE")
            return identifier
    except urllib.error.HTTPError as exc:
        code = {401: "HUB_HTTP_IDENTITY", 403: "HUB_HTTP_TRUST"}.get(exc.code, "HUB_HTTP_STATUS")
        raise PublisherError(code) from None
    except (urllib.error.URLError, TimeoutError):
        raise PublisherError("HUB_TRANSPORT") from None


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
    except (ValueError, OSError, TypeError) as error:
        # Never echo request URLs, tokens, response bodies or arbitrary exceptions.
        print("WorldBisect CI publish failed [" + safe_error_code(error) + "]; verify the job permissions, configured trust and hub availability. "
              "Check existing reports before retrying.", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
