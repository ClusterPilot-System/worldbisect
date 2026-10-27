# Hub data inventory and deletion boundaries

This guide describes the data created by the experimental, source-built team
report hub, where it lives, how long the implementation retains it, and what an
operator must remove separately. It applies to the single-instance preview in
[ADR 0007](adr/0007-team-report-hub-preview.md); it is not a managed-service
privacy policy, legal retention schedule, secure-erasure guarantee, or evidence
that the remaining [SaaS launch gates](https://github.com/ClusterPilot-System/worldbisect/issues/63)
are complete.

Choose and record an approved retention policy before accepting real team data.
Use non-sensitive workspace, subject, repository and check identifiers. The hub
does not execute workloads, but submitted summaries and operational metadata can
still be confidential.

## Data inventory

| Data | Contents and location | Access boundary | Retention and deletion |
| --- | --- | --- | --- |
| Hub configuration | The operator-selected `--config` file contains retention and quota settings, subjects, memberships, credential IDs and SHA-256 token hashes, CI publisher trust rules and an optional restore quarantine deadline. It does not contain raw opaque tokens. | Owner-only regular file (`0600`); read by the hub process. There is no remote administration API. | No automatic expiry for the file or old configuration copies. Remove keys, memberships, subjects and CI publisher rules in a complete validated configuration, then reload or restart. A restored old configuration can reactivate revoked access until it is reconciled. |
| Raw opaque credentials | Printed once during initialization or provisioning, then held by the user's password manager or the CI secret store. A connected dashboard keeps its token only in JavaScript memory. | Outside the hub store; whoever holds a valid token has its configured workspace scopes. | The hub cannot delete external secret-manager copies. Revoke the hash in configuration and reload first, then delete every external copy. Dashboard disconnect, refresh or tab closure clears its in-memory copy; this is not server-side revocation. |
| Report summaries | One private JSON file per report below `--data/<sha256(workspace)>/`. A report contains its server ID/time, repository, check, optional commit/run link, reported status, finding, tested text, next step, experiment count and optional verified publisher metadata. Raw captures, selected CI files, commands, environment and command output are not accepted. | Workspace scopes control create, read and delete. Direct submissions are client-reported; verified CI identity authenticates the publisher, not the diagnosis. Files and directories are owner-only. | `retention_days` is 1–30 days, default 7. Expired files are removed on startup, report-store access or the 15-minute sweep. `DELETE /api/v1/reports/{id}` removes one file immediately and syncs its directory. The newest 100 retained reports are listed; repeat list/delete to remove a larger workspace. An empty workspace directory is removed by a later sweep. |
| CI replay ledger | `--data/.ci-replays.json` stores a SHA-256 digest of each used GitHub OIDC token ID, the configured service subject and token expiry. Token values and raw claims are not stored. | Private hub data file, bounded to 256 KiB, 1,000 active IDs and 32 active IDs per publisher. | Entries stop protecting a live token after its signed expiry and are removed from the file on the next CI reservation. Accepted tokens live at most 10 minutes, with up to 30 seconds of clock skew. Report deletion does not change this ledger. Restore sets at least an 11-minute CI quarantine because replay state may be older than activity after the snapshot. |
| Audit metadata | The `--audit-dir` (default `<data>.audit`) contains `state.json` and hash-chained JSONL segments. Events record actor/kind, workspace, credential ID, action, outcome and sometimes report ID. They omit tokens, hashes, report text, repositories, run URLs, query strings and IP addresses. | Owner-only directory and files; one writer lock. Verification requires the server to be stopped. The local checkpoint is not an independent external trust anchor. | `audit_retention_days` defaults to 7 and accepts 1–30. Segments are limited to 256 KiB and at most 16; the size cap can shorten history. Whole segments expire on append or the 15-minute sweep, preserving only their final sequence/hash as the chain anchor. Deleting a report deliberately does **not** erase its audit events. |
| Process, proxy and monitor logs | The hub writes process/reload/retention errors to its configured stderr logger; systemd, a reverse proxy and monitoring may retain those messages or add request metadata. The synthetic probe emits bounded JSON without credentials, destination, workspace or report IDs. | Controlled by the host journal, proxy and monitoring systems, not by report scopes. Authorization headers must never be logged. | WorldBisect does not configure or delete external logs. Set explicit journal, proxy, shipping and alert retention; review them separately during offboarding. |
| Encrypted backups and exports | `hub-backup.py` encrypts one stopped snapshot of configuration, report data and audit data to an operator-selected `.age` object. Separately exported reports, CI artifacts and copied files are outside the live hub. | Backup identity and object storage are separate operator trust boundaries. `age` protects confidentiality/integrity but does not authenticate who created the backup. | Hub retention and report deletion never delete backup objects, exported reports or CI artifacts. Apply a separate approved retention schedule and delete every replica when authorized. Plaintext staging cleanup is not secure erasure on SSD or copy-on-write storage. |

Lowering either configured age limit does not rewrite external backups or logs.
Report retention is evaluated from the server-assigned `created_at`; audit
retention is segment-based. Both are age limits, not promises that data remains
available for the full configured period.

## What report deletion does not do

Deleting a report removes only its live JSON record. It does not:

- remove the deletion intent/completion or older activity from the audit chain;
- revoke an opaque credential, membership, subject or GitHub CI trust rule;
- erase a used-token digest from the CI replay ledger;
- clear a token already held by another browser, CI system or secret manager;
- delete reverse-proxy, systemd, monitoring or external audit records;
- delete encrypted backups, restored copies, exported reports or GitHub Actions
  artifacts; or
- provide physical secure erasure from the underlying filesystem or storage
  snapshots.

These separations preserve auditability and prevent a report API call from
silently changing access control or recovery evidence.

## Bounded workspace offboarding

Perform offboarding from an approved operator host and record the workspace,
approver, start time, backup disposition and final audit checkpoint. Do not put a
token in a URL, command-line argument, repository file or transcript.

1. Stop new publication. Disable the CI job or publisher secret first and wait
   for in-flight requests to finish. Decide whether an incident/legal hold
   requires a final encrypted snapshot; otherwise do not create another copy.
2. Use a temporary editor credential scoped to the exact workspace. Confirm
   `/api/v1/session` reports the expected workspace and includes
   `reports:read` and `reports:delete` before removing anything.
3. Delete live reports in batches. The API lists at most 100, so continue until
   a final list is empty. The following standard-library helper rejects
   redirects and ambient proxies and requires an HTTPS origin:

   ```bash
   export WORLDBISECT_HUB_URL=https://hub.example.com
   export WORLDBISECT_HUB_WORKSPACE=platform
   read -r -s -p 'Temporary editor token: ' WORLDBISECT_HUB_TOKEN; echo
   export WORLDBISECT_HUB_TOKEN
   python3 - <<'PY'
   import json
   import os
   import urllib.error
   import urllib.parse
   import urllib.request

   origin = os.environ["WORLDBISECT_HUB_URL"].rstrip("/")
   expected = os.environ["WORLDBISECT_HUB_WORKSPACE"]
   token = os.environ["WORLDBISECT_HUB_TOKEN"]
   parsed = urllib.parse.urlsplit(origin)
   if parsed.scheme != "https" or parsed.username or parsed.password or parsed.path or parsed.query or parsed.fragment:
       raise SystemExit("WORLDBISECT_HUB_URL must be an HTTPS origin without credentials or a path")

   class NoRedirect(urllib.request.HTTPRedirectHandler):
       def redirect_request(self, req, fp, code, msg, headers, newurl):
           return None

   opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
   headers = {"Authorization": "Bearer " + token, "Accept": "application/json"}

   def call(path, method="GET"):
       request = urllib.request.Request(origin + path, headers=headers, method=method)
       try:
           with opener.open(request, timeout=10) as response:
               body = response.read()
               return response.status, json.loads(body) if body else None
       except urllib.error.HTTPError as error:
           raise SystemExit(f"{method} {path} failed with HTTP {error.code}") from None

   _, session = call("/api/v1/session")
   if session.get("workspace") != expected:
       raise SystemExit("credential workspace does not match the approved workspace")
   scopes = set(session.get("scopes", []))
   if not {"reports:read", "reports:delete"}.issubset(scopes):
       raise SystemExit("credential lacks reports:read or reports:delete")

   deleted = 0
   while True:
       _, payload = call("/api/v1/reports")
       reports = payload.get("reports", [])
       if not reports:
           break
       for report in reports:
           report_id = report.get("id", "")
           if len(report_id) != 32 or any(c not in "0123456789abcdef" for c in report_id):
               raise SystemExit("server returned an invalid report ID")
           status, _ = call("/api/v1/reports/" + report_id, "DELETE")
           if status != 204:
               raise SystemExit(f"unexpected delete status {status}")
           deleted += 1

   _, final = call("/api/v1/reports")
   if final.get("reports"):
       raise SystemExit("workspace still contains reports")
   print(f"deleted {deleted} live report(s); final list is empty")
   PY
   unset WORLDBISECT_HUB_TOKEN
   ```

4. Replace the complete private configuration atomically: remove every opaque
   key, membership and CI publisher rule for the workspace; remove a subject
   only when no remaining membership/rule references it. Keep at least one
   opaque credential or CI publisher elsewhere in the configuration. Reload
   with SIGHUP and confirm `configuration reloaded`; an invalid file leaves the
   old configuration active.
5. Verify a removed credential now receives `401`, while `/healthz` remains
   available. Do not retain the token merely for this test; delete it from each
   password manager, CI secret store and operator handoff after verification.
6. Wait for the next normal 15-minute retention sweep (or a normal restart,
   which also runs one) and confirm that no report is returned. An empty hashed
   directory may remain until that sweep; it contains no report body. Do not
   delete active data or audit files manually while the service is running.
7. Stop the hub and verify the retained audit chain, optionally against a trusted
   checkpoint:

   ```bash
   bin/worldbisect-hub audit-verify --audit-dir /var/lib/worldbisect-hub/audit
   bin/worldbisect-hub audit-verify \
     --audit-dir /var/lib/worldbisect-hub/audit --expected-head SAVED_HEAD_HASH
   ```

   Restart only after verification. The chain continues to retain bounded
   offboarding metadata; do not erase it merely to make the workspace disappear.
8. Apply the separately approved disposition to encrypted backups, restored
   directories, exports, Actions artifacts, host/proxy logs and external audit
   checkpoints. Record what was deleted, retained, unavailable or subject to a
   hold. A live-hub empty result alone does not verify these systems.

## Verification boundary

Repository tests exercise retention, deletion, cross-workspace isolation,
credential revocation, audit integrity, CI replay rejection and encrypted
restore. They do not inspect an operator's proxy, journal, secret manager,
off-host backup storage or legal policy. Before any managed launch, test this
checklist against the selected deployment and keep evidence for each external
system. This guide documents the current preview; it does not close issue #63.
