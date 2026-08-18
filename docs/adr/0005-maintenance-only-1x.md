# ADR 0005: Maintenance-oriented 1.x line

- Status: Accepted
- Date: 2026-08-17

## Context

WorldBisect is intended to have a stable, closed product contract rather than a perpetual feature roadmap.

## Decision

The 1.x line accepts security, correctness, compatibility, performance, observability, testing, packaging, and documentation maintenance. New causal domains, distributed execution, cloud control planes, and automatic code modification require a major-version architecture proposal.

## Consequences

- Users can depend on stable semantics.
- Maintainers can reject scope expansion without rejecting valuable defect fixes.
- Unknown future systems fail closed as unsupported evidence.
