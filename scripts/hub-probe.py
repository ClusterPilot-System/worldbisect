#!/usr/bin/env python3
"""Run an opt-in, bounded synthetic create/read/delete check of a team hub.

Linux/POSIX, Python 3.10+, standard library only. A private token file and the
expected workspace are required. Output is one sanitized JSON object. This
checks the report API, not diagnosis correctness or a production availability SLO.
"""

import argparse
from datetime import datetime, timezone
import ipaddress
import json
import math
import multiprocessing
import os
import re
import secrets
import stat
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


MAX_RESPONSE = 16 * 1024
MAX_TOKEN = 4096
PHASES = ("session", "create", "read", "delete", "verify_deleted")
REQUIRED_SCOPES = {"reports:read", "reports:write", "reports:delete"}
ID_PATTERN = re.compile(r"[a-f0-9]{32}\Z")


class ProbeError(Exception):
    """Only fixed, locally defined error codes may reach the output."""

    def __init__(self, code, http_status=None):
        self.code = code
        self.http_status = http_status


def origin(value):
    # urlsplit silently strips certain control characters; reject them first.
    if (not isinstance(value, str) or len(value) > 2048 or
            any(ord(c) <= 32 or ord(c) == 127 for c in value) or
            any(c in value for c in ("\\", "?", "#"))):
        raise ProbeError("invalid_url")
    try:
        url = urllib.parse.urlsplit(value)
        if (url.scheme not in ("https", "http") or not url.hostname or
                url.username is not None or url.password is not None or
                url.path not in ("", "/") or url.query or url.fragment or
                "%" in url.netloc or url.port == 0):
            raise ValueError()
        if url.scheme == "http" and not ipaddress.ip_address(url.hostname).is_loopback:
            raise ValueError()
        _ = url.port
    except (ValueError, TypeError):
        raise ProbeError("invalid_url") from None
    return value.rstrip("/")


def read_token(path):
    """Reject links, special files and permissions beyond owner read/write."""
    descriptor = None
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW)
        info = os.fstat(descriptor)
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or
                stat.S_IMODE(info.st_mode) not in (0o400, 0o600) or
                not 1 <= info.st_size <= MAX_TOKEN):
            raise ProbeError("invalid_token_file")
        raw = os.read(descriptor, MAX_TOKEN + 1)
        if len(raw) > MAX_TOKEN:
            raise ProbeError("invalid_token_file")
        # Provisioning writes one optional newline; never accept embedded space.
        token = raw.decode("ascii").removesuffix("\n").removesuffix("\r")
        if not token or any(ord(c) <= 32 or ord(c) >= 127 for c in token):
            raise ProbeError("invalid_token_file")
        return token
    except (OSError, UnicodeError, TypeError, ValueError):
        raise ProbeError("invalid_token_file") from None
    finally:
        if descriptor is not None:
            os.close(descriptor)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def _http_child(pipe, destination, method, payload, token, timeout):
    """One attempt only. No response text or exceptions are printed."""
    try:
        request = urllib.request.Request(
            destination, data=None if payload is None else json.dumps(payload).encode(),
            headers={"Authorization": "Bearer " + token,
                     "Content-Type": "application/json", "Accept": "application/json"},
            method=method)
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        try:
            response = opener.open(request, timeout=timeout)
        except urllib.error.HTTPError as exc:
            # Error bodies are irrelevant and may be slow, huge or sensitive.
            status = exc.code
            exc.close()
            pipe.send((status, b"", None))
            return
        with response:
            status = response.status
            raw = response.read(MAX_RESPONSE + 1)
        if len(raw) > MAX_RESPONSE:
            pipe.send((status, b"", "response_too_large"))
        else:
            pipe.send((status, raw, None))
    except BaseException:
        # Includes DNS/TLS/socket failures; never serialize unknown exception text.
        try:
            pipe.send((None, b"", "connection_failed"))
        except BaseException:
            pass
    finally:
        pipe.close()


class HTTPTransport:
    """A request worker makes DNS and drip-fed bodies subject to a wall deadline.

    Fork is intentional: the operator script is single-threaded and Linux-only;
    credentials are inherited in memory, never put in argv or the environment.
    The parent kills a running worker and gives reaping the remaining deadline.
    As with any process timeout, this assumes the OS can schedule and kill it;
    an external service-manager timeout is still appropriate for host failures.
    """

    def __init__(self, hub, token):
        self.hub = hub
        self.token = token

    def request(self, method, path, payload, deadline):
        remaining = deadline - time.monotonic()
        if remaining <= 0.025:
            raise ProbeError("timeout")
        context = multiprocessing.get_context("fork")
        receiving, sending = context.Pipe(duplex=False)
        worker = context.Process(target=_http_child, args=(
            sending, self.hub + path, method, payload, self.token, remaining), daemon=True)
        started = False
        try:
            worker.start()
            started = True
            sending.close()
            # Reserve a short slice for closing and reaping, within this deadline.
            wait = max(0, deadline - time.monotonic() - 0.025)
            if not receiving.poll(wait):
                raise ProbeError("timeout")
            try:
                status, raw, error = receiving.recv()
            except (EOFError, OSError):
                raise ProbeError("connection_failed") from None
            if error:
                raise ProbeError(error, status)
            if type(status) is not int or not 100 <= status <= 599:
                raise ProbeError("invalid_response")
            if 300 <= status <= 399:
                raise ProbeError("redirect_refused", status)
            return status, raw
        finally:
            receiving.close()
            sending.close()
            if started:
                if worker.is_alive():
                    worker.kill()
                worker.join(timeout=max(0, deadline - time.monotonic()))
                # No unbounded join here. Normal DNS/socket stalls are killed and
                # reaped; uninterruptible host failures need the outer watchdog.
                if not worker.is_alive():
                    worker.close()


def decode(raw):
    def unique_object(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError()
            result[key] = value
        return result

    try:
        if len(raw) > MAX_RESPONSE:
            raise ProbeError("response_too_large")
        value = json.loads(raw, object_pairs_hook=unique_object)
        if not isinstance(value, dict):
            raise ValueError()
        return value
    except (ValueError, TypeError, UnicodeError, RecursionError):
        raise ProbeError("invalid_response") from None


def submission():
    return {
        "repository": "worldbisect/synthetic", "check_name": "operations-probe",
        "status": "UNPROVEN", "experiments": 0,
        "analysis_id": "synthetic_" + secrets.token_hex(16),
        "finding": "Synthetic operator availability check; this is not a diagnosis.",
        "tested": "Report creation, persisted read and deletion only; no diagnosis experiments.",
        "next_step": "Monitor the operator probe result; do not infer a causal finding.",
    }


def bound_identifier(value, payload):
    identifier = value.get("id")
    if (isinstance(identifier, str) and ID_PATTERN.fullmatch(identifier) and
            all(value.get(key) == payload[key] for key in
                ("analysis_id", "repository", "check_name"))):
        return identifier
    return None


def same_report(value, payload, identifier):
    return (value.get("id") == identifier and
            all(type(value.get(key)) is type(expected) and value.get(key) == expected
                for key, expected in payload.items()) and
            value.get("commit_sha", "") == "" and value.get("run_url", "") == "")


def empty_result():
    return {"schema_version": 1, "ok": False, "checked_at": "", "elapsed_seconds": 0,
            "phases": {phase: "pending" for phase in PHASES}, "error": None,
            "cleanup": {"attempted": False, "outcome": "not_needed"},
            "residue_possible": False}


def fail(result, phase, error):
    if phase in result["phases"]:
        result["phases"][phase] = "failed"
    if result["error"] is None:
        result["error"] = {"phase": phase, "code": error.code}
        if error.http_status is not None:
            result["error"]["http_status"] = error.http_status


def finish(result, started):
    result["elapsed_seconds"] = round(time.monotonic() - started, 3)
    result["checked_at"] = datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")
    result["phases"] = {key: "skipped" if value == "pending" else value
                        for key, value in result["phases"].items()}
    result["ok"] = result["error"] is None and all(
        value == "ok" for value in result["phases"].values())
    return result


def probe(hub, workspace, token_file, timeout=20, *, transport=None):
    """Return only the public sanitized result; transport is injectable for tests."""
    started = time.monotonic()
    result = empty_result()
    phase = "config"
    own_identifier = None
    deadline = started
    try:
        if isinstance(timeout, bool):
            raise ProbeError("invalid_timeout")
        try:
            timeout = float(timeout)
        except (TypeError, ValueError):
            raise ProbeError("invalid_timeout") from None
        if not math.isfinite(timeout) or not 0 < timeout <= 60:
            raise ProbeError("invalid_timeout")
        deadline = started + timeout
        # The final quarter is kept for cleanup after a failed/stalled read.
        normal_deadline = started + timeout * 0.75
        hub = origin(hub)
        if not isinstance(workspace, str) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_-]{0,63}", workspace):
            raise ProbeError("invalid_workspace")
        token = read_token(token_file)
        transport = transport or HTTPTransport(hub, token)
        phase = "session"
        status, raw = transport.request("GET", "/api/v1/session", None, normal_deadline)
        if status != 200:
            raise ProbeError("http_error", status)
        session = decode(raw)
        if session.get("workspace") != workspace:
            raise ProbeError("workspace_mismatch")
        scopes = session.get("scopes")
        if (not isinstance(scopes, list) or any(not isinstance(s, str) for s in scopes) or
                not REQUIRED_SCOPES.issubset(scopes)):
            raise ProbeError("scope_missing")
        result["phases"][phase] = "ok"
        payload = submission()
        phase = "create"
        # Once POST is attempted its outcome can be unknown (e.g. failed audit
        # completion). Never retry it, list reports, or guess an ID to delete.
        result["residue_possible"] = True
        result["cleanup"]["outcome"] = "not_attempted"
        status, raw = transport.request("POST", "/api/v1/reports", payload, normal_deadline)
        if status != 201:
            raise ProbeError("http_error", status)
        created = decode(raw)
        own_identifier = bound_identifier(created, payload)
        if own_identifier is None or not same_report(created, payload, own_identifier):
            raise ProbeError("report_mismatch")
        result["phases"][phase] = "ok"
        phase = "read"
        status, raw = transport.request("GET", "/api/v1/reports/" + own_identifier, None, normal_deadline)
        if status != 200:
            raise ProbeError("http_error", status)
        if not same_report(decode(raw), payload, own_identifier):
            raise ProbeError("report_mismatch")
        result["phases"][phase] = "ok"
    except ProbeError as error:
        fail(result, phase, error)
    except Exception:
        fail(result, phase, ProbeError("internal_error"))
    finally:
        if own_identifier is not None:
            path = "/api/v1/reports/" + own_identifier
            result["cleanup"]["attempted"] = True
            result["cleanup"]["outcome"] = "failed"
            # Share the remaining cleanup budget so a stalled DELETE still
            # leaves time to check whether deletion actually happened.
            delete_deadline = time.monotonic() + max(0, deadline - time.monotonic()) * 0.6
            try:
                status, _ = transport.request("DELETE", path, None, delete_deadline)
                if status != 204:
                    raise ProbeError("http_error", status)
                result["phases"]["delete"] = "ok"
                result["cleanup"]["outcome"] = "unconfirmed"
            except ProbeError as error:
                fail(result, "delete", error)
            except Exception:
                fail(result, "delete", ProbeError("internal_error"))
            try:
                status, _ = transport.request("GET", path, None, deadline)
                if status != 404:
                    raise ProbeError("delete_not_verified", status)
                result["phases"]["verify_deleted"] = "ok"
                result["cleanup"]["outcome"] = "deleted"
                result["residue_possible"] = False
            except ProbeError as error:
                fail(result, "verify_deleted", error)
            except Exception:
                fail(result, "verify_deleted", ProbeError("internal_error"))
    return finish(result, started)


class Parser(argparse.ArgumentParser):
    def error(self, message):
        raise ProbeError("invalid_arguments")


def main(argv=None):
    started = time.monotonic()
    parser = Parser(description=__doc__)
    parser.add_argument("--hub", required=True, help="trusted HTTPS origin; literal loopback HTTP also allowed")
    parser.add_argument("--workspace", required=True, help="expected workspace, verified before any write")
    parser.add_argument("--token-file", required=True, help="owner-only regular file containing a bearer token")
    parser.add_argument("--timeout", default="20", help="total seconds including cleanup (default 20, maximum 60)")
    try:
        args = parser.parse_args(argv)
        result = probe(args.hub, args.workspace, args.token_file, args.timeout)
    except ProbeError as error:
        result = empty_result()
        fail(result, "config", error)
        finish(result, started)
    except Exception:
        result = empty_result()
        fail(result, "config", ProbeError("internal_error"))
        finish(result, started)
    print(json.dumps(result, separators=(",", ":"), sort_keys=True))
    return 0 if result["ok"] else 1


if __name__ == "__main__":
    sys.exit(main())
