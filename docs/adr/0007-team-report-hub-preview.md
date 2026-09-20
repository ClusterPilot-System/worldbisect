# ADR 0007: Separate team report hub preview

- Status: Accepted for an opt-in preview; managed SaaS remains unshipped
- Date: 2026-09-20
- Tracking: [#60](https://github.com/ClusterPilot-System/worldbisect/issues/60)

## Context

Teams need a shared place to review diagnoses across CI projects. The existing
daemon can execute explicitly authorized local commands and is not a shared
tenant boundary. Publishing it as a multi-tenant SaaS would change its trust
model. ADR 0005 keeps the 1.x causal contract maintenance oriented and requires
an explicit proposal for a cloud control plane.

## Decision

Add a separate, source-built `worldbisect-hub` preview. The CLI, existing daemon,
CI baseline selection and proof kernel retain their contracts. The hub accepts
bounded client-reported diagnosis summaries and never executes a workload,
downloads a baseline, imports a capture or independently verifies a proof.

An operator provisions workspace-bound opaque credentials. Configuration stores
hashes; the authenticated key determines the workspace, never a request field.
Read credentials cannot mutate data. Identifiers from another workspace return
the same not-found response as missing records. Browser credentials are held in
memory and are cleared on disconnect.

One process owns a private local store with atomic report writes, a writer lock,
retention, body limits and per-workspace quotas. This design is deliberately a
single-instance preview, not an HA database or a hostile-tenant isolation claim.
The operator must control the storage directory and terminate TLS in front of
the service. Workspace owners remain responsible for content they submit.

The explicit publisher forwards an allowlist of summary fields. It omits source
paths, free-text source summaries, values, environment, command output and raw
files. Repository/check/run identifiers remain potentially sensitive metadata.
The hub API also permits human-readable summaries, so the API itself is not a
secret scanner. CI publication is opt-in, and its credential must never be
exposed to code from untrusted pull requests.

## Consequences and release gates

- Share client-reported results with the four useful answers and a CI evidence
  link, while making the distinction from hub-verified evidence explicit.
- Retain a zero-dependency Go runtime and leave stable release packaging intact.
- Source-build the preview separately; do not advertise a managed hosted service.
- Before public SaaS: independent tenant/security review; user identity and
  membership/revocation; verified CI identity/provenance; rate limits and abuse
  response; encrypted backup/restore drills; operational monitoring and SLOs;
  durable migration/HA decisions; privacy/deletion policy and billing if needed.
- A future major product proposal must cover remote execution separately if
  ever offered. This preview does not authorize arbitrary workloads in a hub.

See [the operator guide](../team-hub.md) for the exact setup and boundaries.
