# Hub identities, credential lifecycle and audit

The team hub now supports **operator-provisioned named identities**. It remains
an invited, single-instance service: these are not federated user accounts,
self-service invitations, email verification or a completed managed SaaS.

The stable diagnostic engine and its proof contract remain unchanged. A named
publisher is accountable for submitting a summary; identity does not establish
that the submitted diagnosis is correct.

## Start and provision

Build the source preview and initialize private configuration:

```bash
make build-hub
./bin/worldbisect-hub init --config hub.json
./bin/worldbisect-hub credential --config hub.json \
  --subject alice --kind user --workspace platform --role viewer --expires-in 720h
./bin/worldbisect-hub credential --config hub.json \
  --subject platform-ci --kind service --workspace platform --role publisher --expires-in 168h
./bin/worldbisect-hub serve --config hub.json --data hub-data --audit-dir hub-audit
```

Each command prints a newly generated raw token once. Put it in the intended
user's password manager or a trusted CI secret store; configuration contains
only SHA-256 hashes. Avoid terminal recording or shared shell output when
provisioning. New credentials do not become active in a running hub until reload.
The CLI expiry default is 30 days; `--expires-in 0` deliberately creates a key
without expiry. Durations above one year are rejected.

`init` creates configuration version 2 and two initial named service identities
for compatibility with the existing read/write setup. Provision individual
identities and replace shared bootstrap credentials before inviting users.

## Identity, membership and scopes

A subject has an operator-chosen identifier, a `user` or `service` kind and an
optional `disabled` flag. A membership grants that subject one role in one
workspace. Every opaque credential is bound to one subject and workspace.

| Membership role | Effective report scopes |
| --- | --- |
| `viewer` | `reports:read` |
| `publisher` | `reports:write` (create only) |
| `editor` | `reports:read`, `reports:write`, `reports:delete` |

A credential may narrow its membership with an explicit `scopes` list. It may
never exceed that role. `permission` remains `read` or `write` for old clients;
when `scopes` is present it is authoritative. Session responses include subject,
kind, role, credential ID and effective scopes. The browser uses scopes and
rejects publishing-only credentials because those cannot read team reports.

A version 2 example fragment:

```json
{
  "version": 2,
  "retention_days": 7,
  "max_reports_per_workspace": 500,
  "audit_retention_days": 7,
  "subjects": [{"id": "alice", "kind": "user"}],
  "memberships": [{"subject_id": "alice", "workspace": "platform", "role": "viewer"}],
  "keys": [{
    "id": "alice-laptop",
    "subject_id": "alice",
    "workspace": "platform",
    "permission": "read",
    "scopes": ["reports:read"],
    "token_sha256": "REPLACE_WITH_THE_HASH_OF_A_GENERATED_RANDOM_TOKEN",
    "expires_at": "2026-12-01T00:00:00Z"
  }]
}
```

The placeholder is intentionally invalid. Use `credential` to generate actual
secret material rather than inventing a token or hashing a human password.
The configuration supports at most 100 opaque credentials, 100 subjects and
200 memberships. Storage remains limited to 100 workspace directories.

## Reload, revoke and rotate

Write a complete validated configuration with mode `0600`, then send SIGHUP to
the serving process. For the systemd unit:

```bash
sudo systemctl kill --kill-whom=main --signal=HUP worldbisect-hub
```

For a manually started process, use `kill -HUP <pid>`. Inspect the operator log
for `configuration reloaded`; an invalid file is rejected without replacing the
active configuration. There is no remote administrative mutation API.

To revoke a credential, remove its key entry and reload. To disable an identity
including all its keys, set its subject's `disabled` flag and reload. To remove
membership, remove both that membership and credentials which depend on it;
otherwise validation rejects the inconsistent file. To narrow a role, narrow
any associated explicit scopes at the same time. Keep at least one configured
opaque credential or CI publisher; disabling every subject is supported.

For rotation, provision a new credential for the same subject and role, reload,
update its consumer, remove the old key, and reload again. Expiry is checked for
every request and again immediately before storage access. Requests which have
already entered an authorized storage operation complete before reload takes
effect; a request still uploading its body is rechecked and cannot commit after
its key is revoked. The server does not hold its access lock while receiving a
potentially slow request body.

Version 1 configuration still works, including optional `expires_at` per key.
Its keys are represented as `legacy-service` identities with stable local
credential fingerprints. They do **not** identify individual people. To migrate,
change to version 2, add subjects/memberships and assign each existing key a
unique `id` and `subject_id`; keep its token hash unchanged. Validate and reload
before replacing any shared keys.

## Local audit chain

Serving enables audit storage by default at `<data-path>.audit`; `--audit-dir`
selects another dedicated private directory. Configuration reloads, successful
authenticated access and report read/create/delete operations record actor,
identity kind, workspace, credential ID, action, outcome and relevant report ID.
The local administration actor is `operator`, not a verified personal identity
of whoever sent the Unix signal. Anonymous authentication failures are recorded
with a burst of 30 and a refill of one event per two seconds; excess failures
are denied but not individually recorded. Rejected request bodies and rate-limit
responses do not constitute completed report operations.

Audit records omit bearer tokens, token hashes, request bodies, repository names,
run URLs, query strings, IP addresses and report text. Workspace/subject IDs are
still identifying metadata: choose non-sensitive identifiers and restrict the
operator's access.

Every operation durably records an intent before accessing report storage and a
completion before returning report data. An unavailable audit writer denies new
operations. A failure after the mutation but before its completion checkpoint
returns HTTP 503; consult the recorded intent and current report state before
retrying. Audit and report files are **not** one database transaction.

Each audit entry contains its predecessor's SHA-256 digest. A private committed
head checkpoint detects modified, missing, reordered or fully truncated retained
entries. Segments are at most 256 KiB; at most 16 are retained (about 4 MiB plus
metadata). Size rotation may shorten the requested retention period. The default
age limit is seven days, configurable from one through thirty days. Whole
segments expire based on their creation time, so some later entries in an
expiring segment may be removed earlier; retention is a maximum, not a minimum
history guarantee. Sweeps run every fifteen minutes and appends enforce limits.
Expired segment removal preserves its final digest and sequence as the retained
chain anchor, without retaining its event content.

The audit directory must use mode `0700`, regular files use `0600`, and a separate
writer lock prevents concurrent servers or verification while serving. Stop the
hub before verification or a consistent backup:

```bash
./bin/worldbisect-hub audit-verify --audit-dir hub-audit
./bin/worldbisect-hub audit-verify --audit-dir hub-audit --expected-head <saved-head-hash>
```

The first command prints sequence/head/retained-anchor metadata. Save checkpoints
outside the hub if independent integrity evidence is required. `--expected-head`
compares the exact snapshot head, useful for a restore drill. It is not automatic
remote anchoring or proof of the identity of an external auditor.

An administrator able to rewrite **both the entire audit history and its local
checkpoint** can forge a new internally consistent history. The chain is not
an immutable external audit service. Abrupt interruption between log append and
checkpoint commit can leave an uncommitted tail; startup refuses that state
rather than silently accepting or discarding evidence. Preserve it for review
and restore a verified backup or establish a deliberately documented new audit
history. Never silently delete the audit directory to recover availability.

See [hub operations](hub-operations.md) for encrypted snapshots and recovery,
[CI publisher provenance](hub-ci-identity.md) for the separate GitHub OIDC
publication contract, and [the SaaS launch gates](https://github.com/ClusterPilot-System/worldbisect/issues/63)
for the remaining managed-service work.
