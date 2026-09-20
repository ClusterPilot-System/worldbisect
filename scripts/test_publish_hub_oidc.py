import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import unittest
from unittest.mock import patch
import urllib.parse

spec = importlib.util.spec_from_file_location("publisher_oidc", Path(__file__).with_name("publish-hub-oidc.py"))
publisher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publisher)


class Response(io.BytesIO):
    def __init__(self, document, status=200):
        super().__init__(json.dumps(document).encode())
        self.status = status


class OIDCPublisherTest(unittest.TestCase):
    def test_identity_endpoint_cannot_send_job_token_elsewhere(self):
        base = "https://run-actions-1.actions.githubusercontent.com/a/_apis/jobs/1/idtoken?api-version=2.0"
        audience = "https://hub.example/team"
        result = publisher.identity_url(base, audience)
        self.assertEqual(urllib.parse.parse_qs(urllib.parse.urlsplit(result).query)["audience"], [audience])
        for bad in [base.replace("https:", "http:"), base.replace(".com/", ".com.evil.test/"),
                    base.replace("run-actions-1.actions.githubusercontent.com", "127.0.0.1"),
                    base.replace("https://", "https://user:pass@"), base + "&audience=evil",
                    base + "#fragment", base.replace("/idtoken?", "/bad\\route?"),
                    base.replace(".com/", ".com:444/"), base + "&api-version=1.0"]:
            with self.subTest(url=bad), self.assertRaises(ValueError):
                publisher.identity_url(bad, audience)

    def test_only_trusted_hosted_push_context(self):
        env = dict(GITHUB_ACTIONS="true", GITHUB_SERVER_URL="https://github.com", GITHUB_EVENT_NAME="push",
                   RUNNER_ENVIRONMENT="github-hosted", GITHUB_REF="refs/heads/main",
                   GITHUB_REPOSITORY="example/project", GITHUB_SHA="a"*40, GITHUB_RUN_ID="123")
        self.assertEqual(publisher.workflow_context(env)[2], "https://github.com/example/project/actions/runs/123")
        for field, value in [("GITHUB_EVENT_NAME", "pull_request"), ("GITHUB_EVENT_NAME", "pull_request_target"),
                             ("GITHUB_EVENT_NAME", "workflow_run"), ("RUNNER_ENVIRONMENT", "self-hosted"),
                             ("GITHUB_REF", "refs/tags/latest"), ("GITHUB_RUN_ID", "../1")]:
            with self.subTest(field=field), self.assertRaises(ValueError):
                publisher.workflow_context(dict(env, **{field: value}))

    def test_token_response_is_bounded_and_never_reused_as_job_credential(self):
        env = dict(ACTIONS_ID_TOKEN_REQUEST_URL="https://run-actions-1.actions.githubusercontent.com/a/idtoken?api-version=2.0",
                   ACTIONS_ID_TOKEN_REQUEST_TOKEN="secret-job-credential")
        class FakeOpener:
            def open(self, request, timeout):
                self_request = request
                self.assertEqual(self_request.get_header("Authorization"), "Bearer secret-job-credential")
                return Response({"value": "header.payload.signature"})
        fake = FakeOpener()
        fake.assertEqual = self.assertEqual
        with patch.object(publisher, "opener", return_value=fake):
            self.assertEqual(publisher.request_identity("https://hub.example/team", env), "header.payload.signature")
        with patch.object(fake, "open", return_value=Response({"value": "a"*16385})):
            with patch.object(publisher, "opener", return_value=fake), self.assertRaises(ValueError):
                publisher.request_identity("https://hub.example/team", env)

    def test_publish_uses_ci_endpoint_and_does_not_follow_redirects(self):
        class FakeOpener:
            def open(self, request, timeout):
                self.assertEqual(request.full_url, "https://hub.example/api/v1/ci/reports")
                self.assertEqual(request.get_header("Authorization"), "Bearer identity-token")
                return Response({"id": "a"*32}, 201)
        fake = FakeOpener()
        fake.assertEqual = self.assertEqual
        with patch.object(publisher, "opener", return_value=fake):
            self.assertEqual(publisher.publish("https://hub.example", {"status": "UNPROVEN"}, "identity-token"), "a"*32)
        with self.assertRaises(ValueError):
            publisher.NoRedirect().redirect_request(None, None, 302, "", {}, "https://elsewhere.example")

    def test_safe_diagnostics_identify_validation_boundary_without_values(self):
        base = "https://run-actions-1.actions.githubusercontent.com/a/idtoken?api-version=2.0"
        cases = [
            ("", "OIDC_URL_MISSING"),
            ("https://secret.example/a/idtoken?api-version=2.0", "OIDC_URL_HOST"),
            (base.replace("/idtoken?", "/secret\\path?"), "OIDC_URL_PATH"),
            (base + "&secret-query=secret-value", "OIDC_URL_QUERY"),
            (base.replace(".com/", ".com:secret-port/"), "OIDC_URL_FORMAT"),
        ]
        for url, expected in cases:
            with self.subTest(code=expected), self.assertRaises(publisher.PublisherError) as raised:
                publisher.identity_url(url, "https://hub.example/team")
            self.assertEqual(publisher.safe_error_code(raised.exception), expected)
            self.assertNotIn("secret", str(raised.exception))
        env = dict(ACTIONS_ID_TOKEN_REQUEST_URL=base)
        for credential, expected in [("", "OIDC_CREDENTIAL_MISSING"), ("x"*8193, "OIDC_CREDENTIAL_SIZE"),
                                     ("secret credential", "OIDC_CREDENTIAL_FORMAT")]:
            with self.assertRaises(publisher.PublisherError) as raised:
                publisher.request_identity("https://hub.example/team", dict(env, ACTIONS_ID_TOKEN_REQUEST_TOKEN=credential))
            self.assertEqual(publisher.safe_error_code(raised.exception), expected)
        self.assertEqual(publisher.safe_error_code(ValueError("secret-token-value")), "PUBLISHER_ERROR")
        self.assertEqual(publisher.safe_error_code(publisher.PublisherError("secret-token-value")), "PUBLISHER_ERROR")

    def test_provider_selected_routes_preserve_the_trusted_origin_boundary(self):
        for path in ("/a/idtoken", "/a/_apis/token/identity", "/oidc/token", "/nested/provider/route/"):
            url = "https://run-actions-1.actions.githubusercontent.com" + path + "?api-version=2.0"
            result = publisher.identity_url(url, "https://hub.example/team")
            self.assertEqual(urllib.parse.urlsplit(result).path, path)
            self.assertEqual(urllib.parse.urlsplit(result).hostname, "run-actions-1.actions.githubusercontent.com")
            for unsafe in (url.replace(".com/", ".com.attacker.example/"), url.replace("https:", "http:"),
                           url.replace("https://", "https://user:secret@"), url + "#fragment"):
                with self.subTest(path=path), self.assertRaises(publisher.PublisherError):
                    publisher.identity_url(unsafe, "https://hub.example/team")
        for path in ("", "/bad\\path", "/bad path", "/bad\tpath", "/bad\npath", "/bad\x7fpath"):
            with self.subTest(path=path), self.assertRaises(publisher.PublisherError):
                publisher.identity_url("https://run-actions-1.actions.githubusercontent.com" + path + "?api-version=2.0",
                                       "https://hub.example/team")

    def test_live_helper_prints_only_fixed_diagnostic_code(self):
        env = dict(os.environ, GITHUB_ACTIONS="true", GITHUB_SERVER_URL="https://github.com", GITHUB_EVENT_NAME="push",
                   RUNNER_ENVIRONMENT="github-hosted", GITHUB_REF="refs/heads/main", GITHUB_REPOSITORY="example/project",
                   GITHUB_SHA="a"*40, GITHUB_RUN_ID="123", GITHUB_REPOSITORY_ID="123", GITHUB_REPOSITORY_OWNER_ID="456",
                   GITHUB_WORKFLOW_REF="example/project/.github/workflows/ci.yml@refs/heads/main",
                   ACTIONS_ID_TOKEN_REQUEST_URL="https://secret-host.example/secret-path?credential=secret-value",
                   ACTIONS_ID_TOKEN_REQUEST_TOKEN="secret-job-credential")
        result = subprocess.run([sys.executable, str(Path(__file__).with_name("hub-oidc-e2e.py")), "--report", "not-read.json"],
                                env=env, capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 1)
        self.assertIn("[OIDC_URL_HOST]", result.stderr)
        self.assertNotIn("secret", result.stdout + result.stderr)
        self.assertNotIn("Traceback", result.stderr)


if __name__ == "__main__":
    unittest.main()
