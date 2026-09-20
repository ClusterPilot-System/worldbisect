# ADR 0008: Operator-managed hub access and bounded local audit

- Status: Accepted for the invited, single-instance hub
- Date: 2026-09-20
- Tracking: [#63](https://github.com/ClusterPilot-System/worldbisect/issues/63)

## Context

The report hub's shared workspace credentials do not identify individuals and
require restart for revocation. A public managed service also needs accountable
access and recoverable operations, without exposing the execution daemon.

## Decision

Extend the separate hub with named operator-provisioned user/service subjects,
explicit workspace memberships and constrained credential scopes. Expiry and
atomic validated SIGHUP reload apply to subsequent operations; slow request
bodies must pass authorization again before committing. Version 1 configurations
continue to work but are identified as legacy service credentials.

The serving CLI enables a bounded private local audit chain. Report operations
record durable intent/completion metadata without token material or report
content. Audit failure denies further operations. Rotation retains a hash anchor
and committed head; independent integrity requires a checkpoint outside the
operator-controlled directory. Report storage and the audit log are separate
transactions, which is explicit in failure and recovery documentation.

GitHub Actions provenance uses a separate OIDC publication endpoint and exact
operator-configured subject/workspace/repository/workflow bindings. Verified CI
identity does not verify the submitted causal finding. Operator-provisioned
browser credentials are not an external identity-provider login.

## Consequences

This is a useful access-management and operations step, not completed SaaS:
self-service invitations, external identity federation, tenant lifecycle policy,
external audit anchoring, production SLO evidence, independent security review
and paid onboarding remain launch gates. No workload executes in the hub.

See [access and audit operations](../hub-access.md) for provisioning, revocation,
limits, compatibility and recovery semantics.
