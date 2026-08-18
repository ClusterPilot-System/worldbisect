# ADR 0001: Bounded causal contract

- Status: Accepted
- Date: 2026-08-17

## Context

A general-purpose system cannot control every Linux, kernel, hardware, network, or distributed-system factor. Claiming universal causality would be unsound.

## Decision

WorldBisect 1.0 defines a bounded intervention model. Only factors that can be captured, materialized, independently changed, rerun against an explicit oracle, and verified bidirectionally may receive `PROVEN` status.

The built-in proven factor set is limited to non-secret environment variables and regular workspace objects.

Uncontrolled evidence remains visible but cannot become a causal proof.

## Consequences

- Users receive conservative results.
- The proof kernel remains small and testable.
- New intervention domains require a future explicit contract rather than an implicit feature expansion.
