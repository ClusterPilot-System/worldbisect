# ADR 0006: Opt-in CI input baselines

- Status: Accepted
- Date: 2026-09-16
- Tracking: #47, #48, #49

## Context

The root Action requires two manually constructed workspaces. This blocks
routine CI adoption even though the existing engine can diagnose selected file
differences. The product owner authorized an implementation that removes that
setup burden while keeping results reproducible and bounded.

## Decision

Add a separate `actions/ci` companion Action. It retains explicitly selected
regular input files from successful default-branch push workflows in GitHub
Actions artifacts. It restores compatible trusted inputs on later failures,
re-executes both sides on the current runner, then delegates proof to the
unchanged engine. A failed check never becomes successful because diagnosis
succeeds. Existing CLI, bundle formats, root Action and proof semantics remain
compatible with the maintenance contract in ADR 0005.

This is an explicit new **optional artifact trust boundary**, not a new causal
domain. Python and Node are confined to CI orchestration (already used by Action
and development tooling); the Go runtime gains no dependencies, network service,
telemetry or credentials. No code repair or cloud control plane is introduced.

## Security and compatibility

Raw baselines are different from redacted handoff artifacts. Use exact file
allowlists, content/mode digests, bounded snapshots, short retention, no inherited
secret environment, and fail-closed path/provenance validation. Only successful
prior default-branch push runs from the same workflow are eligible. Fork PRs
cannot retrieve raw baselines. Reject pull_request_target. Historical runner
images, packages, services and secrets are not replayed or certified.

## Consequences

The easiest supported entry point is a small deterministic file-based check.
Large builds and checks requiring credentials remain outside this companion
Action's initial scope. Expired/incompatible baselines and unstable executions
produce specific outcomes instead of causal claims. External pilot feedback is
needed before promising measured savings. Rollback consists of removing the
companion Action from workflows and deleting its retained artifacts; the existing
explicit-workspace integration remains available.
