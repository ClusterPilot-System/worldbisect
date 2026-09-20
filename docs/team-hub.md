# Team report hub (experimental preview)

Review CI diagnoses from several repositories in one workspace. The hub stores
client-reported summaries; checks and proof experiments still run in your CI.
This is a source-built, self-hosted preview, not a managed SaaS or a production
tenant-isolation guarantee. The stable CLI and execution daemon are unchanged.

The access extension adds [named identities, scopes, expiry, reload and audit](hub-access.md),
[verified GitHub Actions publisher identity](hub-ci-identity.md), and
[encrypted backup, recovery and operational runbooks](hub-operations.md).
The guide below remains the basic opaque-token path. Verified publisher identity
does not independently verify a submitted diagnosis.

## Start locally

Use Linux and the Go version in `go.mod`. From a reviewed checkout:

```bash
make build-hub
umask 077
bin/worldbisect-hub init --config /tmp/worldbisect-hub-config.json
bin/worldbisect-hub serve --config /tmp/worldbisect-hub-config.json \
  --data /tmp/worldbisect-hub-data --listen 127.0.0.1:8090
```

Initialization prints one read credential and one write credential for the
default workspace once. Keep them in a secret manager; configuration contains
only their SHA-256 hashes. Open `http://127.0.0.1:8090` and connect with the read
credential. The dashboard holds it only in memory; refresh requires reconnecting.
Do not put credentials into URLs, screenshots, shell arguments or repository files.

The UI shows repository/check filters, the latest diagnoses and four answers:
finding, experiments, reported evidence level and next step. **Reported** means
the submitting CI or person asserted it. The hub does not independently verify
the original experiments or a signed diagnosis certificate. Basic opaque-token
uploads carry no verified CI origin. The optional
[GitHub Actions identity flow](hub-ci-identity.md) verifies the configured publisher
and binds repository, commit and run metadata; it does not verify the experiments.

## Publish a diagnosis deliberately

Export the existing engine's versioned JSON report, or download `report.json`
from its diagnosis artifact. For example:

```bash
worldbisect explain --store /path/to/local-store --format json ANALYSIS_ID > report.json
python3 scripts/publish-hub-report.py --report report.json \
  --repository your-org/your-repo --check build --dry-run
```

Review the printed projection. It contains a report ID from the engine, status,
proof-check booleans expressed in text, factor/experiment counts, and the
repository/check/run metadata you supply. It deliberately omits file paths,
factor values, source summary text, commands, environment and command output.
Keep the detailed artifact in your existing CI access boundary.

Set `WORLDBISECT_HUB_TOKEN` through your secret manager or a CI secret and
`WORLDBISECT_HUB_URL` to the final trusted origin, then omit `--dry-run`:

```bash
python3 scripts/publish-hub-report.py --report report.json \
  --repository your-org/your-repo --check build
```

HTTPS is mandatory except for a literal loopback IP during local development.
The publisher refuses redirects and ambient proxy settings, limits the source
report to 4 MiB, and never retries a write automatically. A network failure may
occur after storage succeeds: inspect the dashboard before retrying, since
this preview does not yet deduplicate submissions.

For GitHub Actions, put the following step **after report generation** in a
trusted default-branch push job with a reviewed publisher script. The CI
companion removes its temporary report after uploading the artifact, so this
example expects an explicitly exported/downloaded `report.json` in the job's
working directory. Skip it when diagnosis did not produce an analysis report.

```yaml
- name: Publish reviewed diagnosis summary
  if: >-
    always() && github.event_name == 'push' &&
    github.ref_name == github.event.repository.default_branch &&
    hashFiles('report.json') != ''
  continue-on-error: true
  env:
    WORLDBISECT_HUB_TOKEN: ${{ secrets.WORLDBISECT_HUB_TOKEN }}
    WORLDBISECT_HUB_URL: ${{ vars.WORLDBISECT_HUB_URL }}
    REPORT_REPOSITORY: ${{ github.repository }}
    REPORT_SHA: ${{ github.sha }}
    REPORT_RUN_ID: ${{ github.run_id }}
  run: >-
    python3 scripts/publish-hub-report.py --report report.json
    --repository "$REPORT_REPOSITORY" --check build --commit "$REPORT_SHA"
    --run-url "https://github.com/$REPORT_REPOSITORY/actions/runs/$REPORT_RUN_ID"
```

A hub outage must not erase or replace the original check result. Never pass a
hub credential into an untrusted PR, a PR-supplied script or a workload under
test. Do not use `pull_request_target` to execute contributor code with secrets.

## Workspace access

The configuration has this shape; replace placeholders with hashes of separate
random high-entropy credentials. Keep a private file with mode `0600`:

```json
{
  "version": 1,
  "retention_days": 7,
  "max_reports_per_workspace": 500,
  "keys": [
    {"workspace": "default", "permission": "read", "token_sha256": "REPLACE_WITH_64_HEX_CHARACTERS"},
    {"workspace": "default", "permission": "write", "token_sha256": "REPLACE_WITH_ANOTHER_64_HEX_HASH"}
  ]
}
```

Add distinct keys mapped to another workspace to provision a separate team.
This version 1 configuration remains supported. `write` includes reading and
deleting reports in that workspace; read credentials cannot mutate reports.
New initialization creates version 2 with named subjects and explicit scopes.
Rotate/revoke by replacing/removing a hash and reloading with SIGHUP, or restarting.
See the [access guide](hub-access.md) for memberships and expiry. There is no
self-service signup, invitation UI or SSO yet.

## HTTP contract

All `/api/v1/*` requests require `Authorization: Bearer <token>`.

| Route | Access | Result |
| --- | --- | --- |
| `GET /healthz` | Public | Process health, no workspace data |
| `GET /api/v1/session` | Read | Current workspace and permission |
| `GET /api/v1/reports` | Read | `{"reports": [...]}`, newest 100 retained summaries |
| `POST /api/v1/reports` | Write | `201` with stored report |
| `GET /api/v1/reports/{id}` | Read | Report or `404` |
| `DELETE /api/v1/reports/{id}` | Write | Remove the report |

POST fields are `repository`, `check_name`, `commit_sha`, `run_url`, `status`,
`finding`, `tested`, `next_step`, `experiments`, `analysis_id`. `id` and
`created_at` are server assigned. Unknown fields, including caller-selected
workspace IDs, are rejected. The request limit is 16 KiB. An optional run URL
must refer to a GitHub Actions run in the supplied repository. Errors return
`{"error":"message"}`. Cross-workspace IDs are indistinguishable from missing IDs.

## Operation and boundaries

- One process and one private local data directory. Do not share the directory
  with other users or applications. A writer lock rejects a second server.
- Default retention is seven days (1–30 configurable), enforced on access and
  by a periodic sweep. Backups and exported copies need their own retention.
- Default quota is 500 summaries per workspace. The newest 100 are listed;
  this preview has no full-history pagination. Delete reports or let them expire
  to reclaim capacity. Quota exhaustion rejects new submissions.
- Stop the process before copying the config/data together for backup. Protect
  and encrypt backups externally. Test restoring into a separate private directory
  with the same config before depending on a backup.
- Bind to loopback and use an authenticated deployment perimeter, TLS reverse
  proxy, request/concurrency/rate limits and firewall rules for team access.
  `/healthz` is a liveness check, not a storage-readiness or dependency check.
  Non-loopback binding additionally requires `--allow-remote-listen`. The API
  bounds active requests to 16 globally and two per workspace, and each
  credential to a burst of 30 requests,
  replenishing one request every two seconds. These limits do not replace
  perimeter protection against unauthenticated traffic.
- Direct API summaries and repository/check identifiers can contain confidential
  information. This is not a complete secret scanner or encrypted database.
  The [audit chain](hub-access.md) needs external checkpoints to detect a hostile
  operator rewriting all local state. [CI identity verification](hub-ci-identity.md)
  authenticates the publisher, not the experiments or selected source checkout.
- Browser disconnect forgets the credential locally; token revocation is an
  operator action. A stolen valid key can perform its workspace permission.

See [ADR 0007](adr/0007-team-report-hub-preview.md) for the separate architecture
and the remaining requirements before offering a managed SaaS.
