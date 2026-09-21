import contextlib
import importlib.util
import io
import json
import multiprocessing
import os
from pathlib import Path
import tempfile
import threading
import time
import unittest
import warnings
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest import mock


spec = importlib.util.spec_from_file_location("hub_probe", Path(__file__).with_name("hub-probe.py"))
probe = importlib.util.module_from_spec(spec)
spec.loader.exec_module(probe)

REPORT_ID = "a" * 32
TOKEN = "private-token-that-must-never-be-logged"


class FakeTransport:
    def __init__(self, overrides=None):
        self.calls = []
        self.overrides = overrides or {}
        self.report = None
        self.deleted = False

    def request(self, method, path, payload, deadline):
        self.calls.append((method, path, payload, deadline))
        key = len(self.calls)
        if key in self.overrides:
            override = self.overrides[key]
            if isinstance(override, Exception):
                raise override
            if callable(override):
                return override(self, payload, deadline)
            return override
        if path == "/api/v1/session":
            return 200, json.dumps({"workspace": "synthetic", "permission": "write",
                                    "scopes": sorted(probe.REQUIRED_SCOPES)}).encode()
        if method == "POST":
            self.report = dict(payload, id=REPORT_ID, created_at="2026-09-21T00:00:00Z")
            return 201, json.dumps(self.report).encode()
        if method == "DELETE":
            self.deleted = True
            return 204, b""
        return (404, b"") if self.deleted else (200, json.dumps(self.report).encode())


class ProbeTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.token = Path(self.temp.name) / "private-token-file"
        self.token.write_text(TOKEN + "\n")
        self.token.chmod(0o600)

    def run_probe(self, transport=None, **kwargs):
        result = probe.probe(kwargs.pop("hub", "http://127.0.0.1:1234"),
                             kwargs.pop("workspace", "synthetic"), self.token,
                             transport=transport or FakeTransport(), **kwargs)
        encoded = json.dumps(result)
        for private in (TOKEN, str(self.token), REPORT_ID, "127.0.0.1", "synthetic", "PRIVATE"):
            self.assertNotIn(private, encoded)
        self.assertRegex(result["checked_at"], r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$")
        return result

    def test_success_checks_full_lifecycle_and_deletes_only_its_report(self):
        transport = FakeTransport()
        result = self.run_probe(transport)
        self.assertTrue(result["ok"])
        self.assertEqual(result["phases"], dict.fromkeys(probe.PHASES, "ok"))
        self.assertEqual(result["cleanup"], {"attempted": True, "outcome": "deleted"})
        self.assertFalse(result["residue_possible"])
        self.assertEqual([(m, p) for m, p, _, _ in transport.calls], [
            ("GET", "/api/v1/session"), ("POST", "/api/v1/reports"),
            ("GET", "/api/v1/reports/" + REPORT_ID),
            ("DELETE", "/api/v1/reports/" + REPORT_ID),
            ("GET", "/api/v1/reports/" + REPORT_ID)])
        payload = transport.calls[1][2]
        self.assertEqual(payload["status"], "UNPROVEN")
        self.assertEqual(payload["experiments"], 0)
        self.assertEqual(payload["repository"], "worldbisect/synthetic")
        self.assertEqual(payload["check_name"], "operations-probe")
        self.assertIn("not a diagnosis", payload["finding"])
        self.assertNotEqual(payload["analysis_id"], probe.submission()["analysis_id"])

    def test_workspace_mismatch_does_not_write(self):
        transport = FakeTransport()
        result = self.run_probe(transport, workspace="different")
        self.assertEqual(result["error"], {"phase": "session", "code": "workspace_mismatch"})
        self.assertEqual(len(transport.calls), 1)
        self.assertFalse(result["residue_possible"])

    def test_modern_scopes_are_authoritative_even_with_write_permission(self):
        for scopes in (None, [], ["reports:read", "reports:write"],
                       ["reports:read", "reports:delete"], ["reports:write", "reports:delete"],
                       "reports:read reports:write reports:delete", [True]):
            with self.subTest(scopes=scopes):
                session = {"workspace": "synthetic", "permission": "write", "scopes": scopes}
                transport = FakeTransport({1: (200, json.dumps(session).encode())})
                result = self.run_probe(transport)
                self.assertEqual(result["error"]["code"], "scope_missing")
                self.assertEqual(len(transport.calls), 1)

    def test_session_http_failure_and_malformed_json_do_not_write(self):
        for status, body, code in ((401, b"PRIVATE", "http_error"),
                                   (503, b"PRIVATE", "http_error"),
                                   (200, b"PRIVATE", "invalid_response"),
                                   (200, b"[]", "invalid_response"),
                                   (200, b'{"workspace":"synthetic","workspace":"other"}', "invalid_response"),
                                   (200, b"x" * (probe.MAX_RESPONSE + 1), "response_too_large")):
            with self.subTest(status=status, code=code):
                transport = FakeTransport({1: (status, body)})
                result = self.run_probe(transport)
                self.assertEqual(result["error"]["code"], code)
                self.assertEqual(len(transport.calls), 1)
                self.assertFalse(result["residue_possible"])

    def test_unknown_create_never_retries_lists_or_deletes(self):
        for outcome in ((503, b"PRIVATE"), (201, b"PRIVATE"),
                        probe.ProbeError("connection_failed"), probe.ProbeError("timeout")):
            with self.subTest(outcome=type(outcome).__name__):
                transport = FakeTransport({2: outcome})
                result = self.run_probe(transport)
                self.assertFalse(result["ok"])
                self.assertEqual(result["error"]["phase"], "create")
                self.assertEqual(len(transport.calls), 2)
                self.assertTrue(result["residue_possible"])
                self.assertEqual(result["cleanup"], {"attempted": False, "outcome": "not_attempted"})

    def test_unbound_create_response_cannot_name_report_for_cleanup(self):
        for changed in ({"id": "../PRIVATE"}, {"id": "B" * 32},
                        {"analysis_id": "PRIVATE"}, {"repository": "other/project"},
                        {"check_name": "other"}):
            with self.subTest(changed=changed):
                def create(transport, payload, deadline):
                    return 201, json.dumps(dict(payload, id=REPORT_ID) | changed).encode()
                transport = FakeTransport({2: create})
                result = self.run_probe(transport)
                self.assertEqual(result["error"]["code"], "report_mismatch")
                self.assertTrue(result["residue_possible"])
                self.assertEqual(len(transport.calls), 2)

    def test_bound_created_id_is_cleaned_even_when_content_is_malformed(self):
        def create(transport, payload, deadline):
            return 201, json.dumps(dict(payload, id=REPORT_ID, status="PROVEN")).encode()
        transport = FakeTransport({2: create})
        result = self.run_probe(transport)
        self.assertFalse(result["ok"])
        self.assertEqual(result["error"], {"phase": "create", "code": "report_mismatch"})
        self.assertEqual(result["cleanup"]["outcome"], "deleted")
        self.assertFalse(result["residue_possible"])
        self.assertEqual(transport.calls[2][0], "DELETE")

    def test_persisted_read_checks_all_content_and_id_then_cleans_original(self):
        for changed in ({"id": "b" * 32}, {"analysis_id": "different"},
                        {"finding": "different"}, {"tested": "different"},
                        {"next_step": "different"}, {"status": "PROVEN"},
                        {"experiments": False}, {"commit_sha": "a" * 40},
                        {"run_url": "https://PRIVATE.invalid"}):
            with self.subTest(changed=changed):
                def read(transport, payload, deadline):
                    return 200, json.dumps(transport.report | changed).encode()
                transport = FakeTransport({3: read})
                result = self.run_probe(transport)
                self.assertEqual(result["error"], {"phase": "read", "code": "report_mismatch"})
                self.assertEqual(result["cleanup"]["outcome"], "deleted")
                self.assertFalse(result["ok"])
                self.assertEqual(transport.calls[3][1], "/api/v1/reports/" + REPORT_ID)

    def test_read_connection_failure_still_attempts_cleanup(self):
        transport = FakeTransport({3: probe.ProbeError("connection_failed")})
        result = self.run_probe(transport)
        self.assertEqual(result["error"]["phase"], "read")
        self.assertEqual(result["cleanup"]["outcome"], "deleted")
        self.assertFalse(result["residue_possible"])

    def test_primary_timeout_reserves_cleanup_budget(self):
        def timeout(transport, payload, deadline):
            time.sleep(max(0, deadline - time.monotonic()))
            raise probe.ProbeError("timeout")
        transport = FakeTransport({3: timeout})
        result = self.run_probe(transport, timeout=0.2)
        self.assertEqual(result["error"], {"phase": "read", "code": "timeout"})
        self.assertEqual(result["cleanup"]["outcome"], "deleted")
        self.assertLess(transport.calls[2][3], transport.calls[3][3])
        self.assertLess(transport.calls[3][3], transport.calls[4][3])

    def test_cleanup_failure_and_unconfirmed_delete_are_explicit(self):
        for overrides, outcome, phase in (({4: (503, b"PRIVATE")}, "failed", "delete"),
                ({5: (503, b"PRIVATE")}, "unconfirmed", "verify_deleted"),
                ({4: probe.ProbeError("timeout")}, "failed", "delete")):
            with self.subTest(outcome=outcome, phase=phase):
                result = self.run_probe(FakeTransport(overrides))
                self.assertFalse(result["ok"])
                self.assertEqual(result["error"]["phase"], phase)
                self.assertEqual(result["cleanup"]["outcome"], outcome)
                self.assertTrue(result["residue_possible"])

    def test_delete_error_can_still_verify_absence_without_claiming_success(self):
        result = self.run_probe(FakeTransport({4: (503, b""), 5: (404, b"")}))
        self.assertFalse(result["ok"])
        self.assertEqual(result["error"]["phase"], "delete")
        self.assertEqual(result["cleanup"]["outcome"], "deleted")
        self.assertFalse(result["residue_possible"])

    def test_unknown_exceptions_never_disclose_exception_text(self):
        result = self.run_probe(FakeTransport({3: RuntimeError("PRIVATE " + TOKEN)}))
        self.assertEqual(result["error"]["code"], "internal_error")
        self.assertEqual(result["cleanup"]["outcome"], "deleted")

    def test_invalid_urls_are_rejected_before_token_is_read(self):
        urls = ("http://example.com", "http://localhost", "file:///tmp/PRIVATE", "https://u:p@example.com",
                "https://example.com/path", "https://example.com?", "https://example.com#", "https://example.com?x=PRIVATE",
                "https://example.com\\PRIVATE", " https://example.com", "https://exam\nple.com", "https://example.com:0",
                "https://example.com:70000", "http://[::1%25eth0]", "https://example.com/\t", "https://example.com/\x7f")
        with mock.patch.object(probe, "read_token", side_effect=AssertionError("must not read credentials")):
            for url in urls:
                with self.subTest(url=url):
                    result = self.run_probe(hub=url)
                    self.assertEqual(result["error"]["code"], "invalid_url")
        self.assertEqual(probe.origin("https://example.com/"), "https://example.com")
        self.assertEqual(probe.origin("http://[::1]:8090"), "http://[::1]:8090")

    def test_invalid_config_is_sanitized(self):
        for timeout in (0, -1, 61, "PRIVATE", float("nan"), float("inf"), True):
            self.assertEqual(self.run_probe(timeout=timeout)["error"]["code"], "invalid_timeout")
        self.assertEqual(self.run_probe(workspace="invalid/PRIVATE")["error"]["code"], "invalid_workspace")

    def test_private_regular_token_file_and_single_line_are_required(self):
        for mode in (0o644, 0o640, 0o700):
            self.token.chmod(mode)
            self.assertEqual(self.run_probe()["error"]["code"], "invalid_token_file")
        self.token.chmod(0o400)
        self.assertTrue(self.run_probe()["ok"])
        self.token.chmod(0o600)
        for content in (b"", b"x" * (probe.MAX_TOKEN + 1), b"hello world", b"hello\nworld", b"\xff", b"hello\n\n"):
            self.token.write_bytes(content)
            self.assertEqual(self.run_probe()["error"]["code"], "invalid_token_file")
        self.token.unlink()
        self.token.symlink_to(Path(self.temp.name) / "missing")
        self.assertEqual(self.run_probe()["error"]["code"], "invalid_token_file")
        self.token.unlink()
        os.mkfifo(self.token, 0o600)
        started = time.monotonic()
        self.assertEqual(self.run_probe()["error"]["code"], "invalid_token_file")
        self.assertLess(time.monotonic() - started, 0.2)

    def test_cli_returns_one_json_error_without_usage_or_arguments(self):
        stdout, stderr = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            status = probe.main(["--hub", "https://PRIVATE.invalid"])
        self.assertEqual(status, 1)
        self.assertEqual(stderr.getvalue(), "")
        result = json.loads(stdout.getvalue())
        self.assertEqual(result["error"]["code"], "invalid_arguments")
        self.assertNotIn("PRIVATE", stdout.getvalue())
        self.assertEqual(len(stdout.getvalue().splitlines()), 1)


class TransportTests(unittest.TestCase):
    def server(self, mode):
        calls = []
        state = {"report": None}

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def reply(self, status, value=None, slow=False):
                raw = b"" if value is None else json.dumps(value).encode()
                self.send_response(status)
                self.send_header("Content-Length", str(len(raw)))
                self.end_headers()
                try:
                    if slow:
                        for byte in raw:
                            self.wfile.write(bytes([byte]))
                            self.wfile.flush()
                            time.sleep(0.06)
                    else:
                        self.wfile.write(raw)
                except (BrokenPipeError, ConnectionResetError):
                    pass

            def do_GET(self):
                calls.append(("GET", self.path, self.headers.get("Authorization")))
                if mode == "redirect":
                    self.send_response(307)
                    self.send_header("Location", "/PRIVATE-token-destination")
                    self.end_headers()
                elif mode == "large":
                    self.reply(200, {"PRIVATE": "x" * probe.MAX_RESPONSE})
                elif self.path == "/api/v1/session":
                    self.reply(200, {"workspace": "synthetic", "scopes": sorted(probe.REQUIRED_SCOPES)})
                elif state["report"]:
                    self.reply(200, state["report"], slow=mode == "slow-read")
                else:
                    self.reply(404)

            def do_POST(self):
                calls.append(("POST", self.path, self.headers.get("Authorization")))
                raw = self.rfile.read(int(self.headers["Content-Length"]))
                state["report"] = dict(json.loads(raw), id=REPORT_ID)
                self.reply(201, state["report"], slow=mode == "slow-create")

            def do_DELETE(self):
                calls.append(("DELETE", self.path, self.headers.get("Authorization")))
                state["report"] = None
                self.reply(204)

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        server.daemon_threads = True
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(thread.join)
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        return "http://127.0.0.1:" + str(server.server_port), calls, state

    def call_probe(self, hub, timeout=2):
        with tempfile.TemporaryDirectory() as directory:
            token_file = Path(directory) / "token"
            token_file.write_text(TOKEN)
            token_file.chmod(0o600)
            # The production CLI is single-threaded. This fixture runs its local
            # HTTP server in a thread; the child never touches the server state.
            with warnings.catch_warnings():
                warnings.filterwarnings("ignore", message=r".*multi-threaded.*fork.*", category=DeprecationWarning)
                result = probe.probe(hub, "synthetic", token_file, timeout)
        for private in (TOKEN, REPORT_ID, hub, "PRIVATE"):
            self.assertNotIn(private, json.dumps(result))
        return result

    def test_real_http_complete_cycle_ignores_ambient_proxy(self):
        hub, calls, state = self.server("normal")
        with mock.patch.dict(os.environ, {"HTTP_PROXY": "http://127.0.0.1:1", "http_proxy": "http://127.0.0.1:1",
                                         "ALL_PROXY": "http://127.0.0.1:1", "NO_PROXY": "", "no_proxy": ""}):
            result = self.call_probe(hub)
        self.assertTrue(result["ok"], result)
        self.assertEqual(len(calls), 5)
        self.assertIsNone(state["report"])
        self.assertTrue(all(c[2] == "Bearer " + TOKEN for c in calls))
        self.assertEqual(multiprocessing.active_children(), [])

    def test_redirect_is_refused_without_second_request(self):
        hub, calls, _ = self.server("redirect")
        result = self.call_probe(hub)
        self.assertEqual(result["error"], {"phase": "session", "code": "redirect_refused", "http_status": 307})
        self.assertEqual(len(calls), 1)

    def test_stalled_name_resolution_is_killed_within_deadline(self):
        def stalled_lookup(*args, **kwargs):
            time.sleep(3)
            raise OSError("PRIVATE lookup error")
        started = time.monotonic()
        with mock.patch("socket.getaddrinfo", side_effect=stalled_lookup):
            result = self.call_probe("https://unresolved.invalid", timeout=0.4)
        self.assertLess(time.monotonic() - started, 0.55)
        self.assertEqual(result["error"], {"phase": "session", "code": "timeout"})
        self.assertFalse(result["residue_possible"])
        self.assertEqual(multiprocessing.active_children(), [])

    def test_real_transport_bounds_success_body(self):
        hub, calls, _ = self.server("large")
        result = self.call_probe(hub)
        self.assertEqual(result["error"]["code"], "response_too_large")
        self.assertEqual(len(calls), 1)

    def test_slow_read_cannot_extend_budget_and_known_report_is_cleaned(self):
        hub, calls, state = self.server("slow-read")
        started = time.monotonic()
        result = self.call_probe(hub, timeout=1.2)
        self.assertLess(time.monotonic() - started, 1.35)
        self.assertEqual(result["error"], {"phase": "read", "code": "timeout"})
        self.assertEqual(result["cleanup"]["outcome"], "deleted")
        self.assertFalse(result["residue_possible"])
        self.assertIsNone(state["report"])
        self.assertEqual(len(calls), 5)
        self.assertEqual(multiprocessing.active_children(), [])

    def test_slow_create_is_unknown_and_is_not_retried_or_deleted(self):
        hub, calls, state = self.server("slow-create")
        started = time.monotonic()
        result = self.call_probe(hub, timeout=0.6)
        self.assertLess(time.monotonic() - started, 0.75)
        self.assertEqual(result["error"], {"phase": "create", "code": "timeout"})
        self.assertEqual(result["cleanup"]["outcome"], "not_attempted")
        self.assertTrue(result["residue_possible"])
        self.assertIsNotNone(state["report"])
        self.assertEqual(len(calls), 2)
        self.assertEqual(multiprocessing.active_children(), [])


if __name__ == "__main__":
    unittest.main()
