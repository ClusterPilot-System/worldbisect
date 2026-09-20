import importlib.util
import json
from pathlib import Path
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


spec = importlib.util.spec_from_file_location("publisher", Path(__file__).with_name("publish-hub-report.py"))
publisher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publisher)


def report():
    return {"schema_version": 1, "format": "worldbisect.analysis-report.v1",
            "analysis_id": "analysis_123", "status": "PROVEN",
            "proof": {"forward_verified": True, "reverse_verified": True, "minimal_in_model": True},
            "evidence": {"experiment_count": 9},
            "cause": [{"description": "PRIVATE_VALUE", "key": "PRIVATE_PATH"}],
            "summary": "PRIVATE_SUMMARY", "command": "PRIVATE_COMMAND",
            "next_steps": ["PRIVATE_STEP"], "environment": {"TOKEN": "PRIVATE_TOKEN"}}


class PublisherTests(unittest.TestCase):
    def test_projection_does_not_forward_source_free_text_or_extensions(self):
        payload = publisher.project(report(), "owner/repo", "unit")
        self.assertNotIn("PRIVATE", json.dumps(payload))
        self.assertEqual(payload["experiments"], 9)
        self.assertEqual(payload["status"], "PROVEN")
        self.assertIn("reverse reproduction verified: true", payload["tested"])

    def test_invalid_proof_is_not_published(self):
        value = report()
        value["proof"]["reverse_verified"] = False
        with self.assertRaises(ValueError):
            publisher.project(value, "owner/repo", "unit")
        value["status"] = "UNPROVEN"
        self.assertEqual(publisher.project(value, "owner/repo", "unit")["status"], "UNPROVEN")
        value["proof"]["reverse_verified"] = "false"
        with self.assertRaises(ValueError):
            publisher.project(value, "owner/repo", "unit")

    def test_transport_destination_and_run_links(self):
        for url in ("http://example.com", "https://u:p@example.com", "https://example.com/?x=1",
                    "https://example.com/#token", "file:///tmp/socket", "http://localhost"):
            with self.subTest(url=url), self.assertRaises(ValueError):
                publisher.endpoint(url)
        self.assertEqual(publisher.endpoint("http://127.0.0.1:8090"), "http://127.0.0.1:8090/api/v1/reports")
        with self.assertRaises(ValueError):
            publisher.project(report(), "owner/repo", "unit", run_url="https://github.com/other/repo/actions/runs/1")

    def test_redirect_does_not_forward_bearer(self):
        requests = []

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                requests.append((self.path, self.headers.get("Authorization")))
                self.send_response(307)
                self.send_header("Location", "/stolen")
                self.end_headers()

            def log_message(self, *args):
                pass

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            with self.assertRaisesRegex(ValueError, "redirects are refused"):
                publisher.publish(f"http://127.0.0.1:{server.server_port}", {}, "test-token")
            self.assertEqual(requests, [("/api/v1/reports", "Bearer test-token")])
        finally:
            server.shutdown()
            server.server_close()
            thread.join()


if __name__ == "__main__":
    unittest.main()
