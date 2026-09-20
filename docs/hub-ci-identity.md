# Verified CI publisher identity

The experimental team hub can accept a short-lived GitHub Actions identity token
instead of a stored hub write key. This authenticates **who submitted a summary**.
It does not prove that the workflow executed the claimed experiments, that its
source checkout matches the token's commit, or that its diagnosis is correct.
`PROVEN`, `SUPPORTED`, `CORRELATED`, and `UNPROVEN` remain caller-reported outcomes.

This is an opt-in extension of [ADR 0007](adr/0007-team-report-hub-preview.md),
separate from the diagnostic engine. Existing opaque-token uploads remain
available and never gain verified publisher metadata.

## Supported boundary

Only github.com, GitHub-hosted runners, direct workflow jobs and `push` events on
one configured branch are supported. Pull request events, `pull_request_target`,
`workflow_run`, jobs delegated to a different reusable workflow, self-hosted
runners, tags and GitHub Enterprise Server are rejected. GitHub may include
`job_workflow_ref` for a direct job; when present, it must equal the exact trusted
`workflow_ref`. The narrow scope is intentional for this preview.

Every trust rule pins an audience, exact subject, repository name, immutable
repository and owner IDs, workflow file and branch. The hub verifies RS256 using
Go's standard cryptographic library and keys fetched only from GitHub's fixed
HTTPS JWKS endpoint. It checks issuer, signature, token times and job context.
The submitted repository, commit SHA and Actions run URL must equal signed
claims. Retained report metadata records the service subject, repository IDs,
workflow, branch, run and attempt. Tokens and raw claims are never stored.

## Provision a publisher

Add a service subject, a `publisher` membership and one `ci_publishers` rule to
your private version 2 hub configuration. This minimal example provisions CI
only; retain your existing separately provisioned viewer credentials for access
to the dashboard.

```json
{
  "version": 2,
  "retention_days": 7,
  "max_reports_per_workspace": 500,
  "audit_retention_days": 7,
  "keys": [],
  "subjects": [{"id": "project-ci", "kind": "service"}],
  "memberships": [
    {"subject_id": "project-ci", "workspace": "platform", "role": "publisher"}
  ],
  "ci_publishers": [{
    "subject_id": "project-ci",
    "workspace": "platform",
    "audience": "https://hub.example.com/workspaces/platform",
    "repository": "example/project",
    "repository_id": "123",
    "repository_owner_id": "456",
    "workflow_ref": "example/project/.github/workflows/ci.yml@refs/heads/main",
    "ref": "refs/heads/main",
    "subject": "repo:example@456/project@123:ref:refs/heads/main"
  }]
}
```

Replace every example value with the exact value for your repository. Keep the
audience unique to this hub and workspace. Use immutable numeric IDs from GitHub,
not names converted to arbitrary identifiers. The `subject` must match the actual
GitHub OIDC subject configuration. Both legacy and immutable subject formats are
supported through exact matching; customized subjects must also match exactly.

Protect the configured branch and review workflow changes. The trust policy
authorizes code running in that workflow; it cannot protect against a maintainer
deliberately changing that code to publish false results. Use a dedicated upload
job with the smallest needed permissions, and never run untrusted checkout code
in a job allowed to obtain this identity. Giving a job `id-token: write` enables
all of its steps to request an identity token, including third-party actions.

## Publish from a trusted workflow

Make a reviewed checkout of WorldBisect's scripts available to the job. Keep it
pinned to a reviewed commit. After producing or retrieving an explicitly selected
analysis report, add a step like this to the exact configured workflow:

```yaml
permissions:
  contents: read
  id-token: write

# In the configured branch-push job, after producing analysis.json:
steps:
  - name: Publish diagnosis origin and summary
    if: ${{ always() && github.event_name == 'push' && github.ref == 'refs/heads/main' }}
    env:
      WORLDBISECT_HUB_URL: https://hub.example.com
    run: |
      test -f analysis.json
      python3 ./worldbisect/scripts/publish-hub-oidc.py \
        --report analysis.json \
        --check "unit tests" \
        --audience https://hub.example.com/workspaces/platform
```

The publisher derives repository, SHA and run metadata from the Actions context,
requests a short-lived identity from GitHub and posts one allowlisted summary to
`POST /api/v1/ci/reports`. It does not need `WORLDBISECT_HUB_TOKEN`. Neither the
job's credential nor its identity token is printed, written to disk, or passed
through command-line arguments. `--dry-run` prints the projected summary without
requesting an identity or uploading it. See the [team hub guide](team-hub.md) for
what the summary includes and omits.

HTTPS is required for remote hub destinations. HTTP is permitted only for a
literal loopback IP during local validation. Both credential requests and uploads
refuse redirects and ambient proxy configuration. GitHub token request URLs must
be HTTPS Actions endpoints under `actions.githubusercontent.com`; the audience
is URL-encoded rather than appended as raw text.

The token request path is supplied by GitHub rather than assumed to end with a
particular filename. This follows GitHub's [official OIDC client](https://github.com/actions/toolkit/blob/main/packages/core/src/oidc-utils.ts),
while retaining the hub publisher's HTTPS, provider-host, bounded URL, no-redirect
and no-proxy restrictions. Failures print only an allowlisted diagnostic category
such as `OIDC_URL_HOST` or `OIDC_RESPONSE_TOKEN`; URLs, credentials, response bodies
and arbitrary exception text remain excluded from logs.

## Limits, revocation and recovery

There are at most 50 trust rules. Token verification has two concurrent slots;
they are released before the request body is read. Authenticated CI traffic has a
separate burst of 30 requests per workspace, refilling one per two seconds.
CI body handling shares the ordinary API's two-per-workspace and 16-global
concurrency limits. Unauthenticated requests cannot consume workspace rate budgets.
Put a trusted reverse proxy with request limits
in front of an internet-facing deployment; these bounds do not provide DDoS
protection or tenant availability guarantees.

JWKS responses are limited to 64 KiB and 10 RSA keys, cached for ten minutes.
Refreshes occur at most once per minute, with a five-second network timeout.
Unknown keys, expired cache during an outage and malformed key sets fail closed.
Key rotations may temporarily require a later request. No stale key cache is
accepted beyond its lifetime.

Tokens must have a lifetime of at most ten minutes; future issue/not-before times
allow up to 30 seconds of clock skew. Keep the host clock synchronized. Each token
ID is reserved once in a private, durable ledger **before** the report is stored.
The ledger holds at most 1,000 active IDs, including at most 32 per service subject,
and is capped at 256 KiB. Expired entries are removed on the next reservation.
No token value is persisted. A restart retains replay protection.

A failed write can consume a token without storing the report. A lost response can
also hide a successful write. There is no automatic POST retry. Inspect the
workspace before asking for another token and retrying; a fresh token is a new
submission and may create a duplicate report. A replay or full ledger returns
409, with no internal ledger details in the response.

Disable the service subject and reload configuration to revoke its access. The
hub rechecks trust and membership immediately before storage, including requests
that began reading their body before revocation. Invalid reloads preserve the
previous configuration. Rotating the audience also invalidates previously issued
tokens when that change is loaded.

The backup restore command sets `ci_quarantine_until` to at least eleven minutes
after the restore. During that interval the CI endpoint returns 503 with
`Retry-After`, while viewer access remains available. This prevents restored replay
state from accepting a token used after the snapshot. Preserve this field across
restart and do not shorten it. For a manual filesystem restore, set the same
quarantine before starting the hub, or rotate every CI audience. Configuration and
filesystem administrators are trusted operators, not isolated tenants.

## Validation and reference

Local tests use temporary RSA keys to exercise signatures, algorithm confusion,
duplicate claims, issuer/time/context checks, exact trust matching, metadata
spoofing, mismatched commits/runs, durable concurrent replay and revocation while
a request body is pending. Publisher tests check destination restrictions,
credential separation, bounded token responses and redirect rejection. These are
local verification tests. The separate
[Hub identity verification workflow](../.github/workflows/hub-identity-verification.yml)
exercises an actual GitHub-issued token on trusted main-branch pushes. It starts a
temporary loopback hub with a random audience, uploads a real engine report,
checks persisted identity and rejects replay before and after restart. Inspect
that workflow's result for live-provider compatibility. It publishes no token or
test artifact and is never triggered by a pull request. Its temporary trust setup
is not a production auto-enrollment mechanism. Neither test suite constitutes a
public deployment or an independent security audit.

The [September 20, 2026 live compatibility run](https://github.com/ClusterPilot-System/worldbisect/actions/runs/35529451078)
passed with an actual GitHub token, persisted origin metadata and replay rejection
after restarting the hub. That temporary, explicitly authorized test-branch run
also identified two provider compatibility details: the request route is opaque,
and direct jobs can carry `job_workflow_ref` equal to `workflow_ref`. The permanent
workflow remains restricted to main-branch pushes.

GitHub documents the token fields, audience customization, immutable subjects and
job permissions in its [OIDC reference](https://docs.github.com/en/actions/reference/security/oidc).
The fixed issuer and JWKS address are published in GitHub's
[discovery document](https://token.actions.githubusercontent.com/.well-known/openid-configuration).
