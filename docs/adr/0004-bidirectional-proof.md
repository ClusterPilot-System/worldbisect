# ADR 0004: Bidirectional causal proof

- Status: Accepted
- Date: 2026-08-17

## Context

A factor that repairs a bad execution may be correlated with the failure rather than necessary for it. One-way experiments are insufficient for a strong causal claim.

## Decision

A `PROVEN` factor set must pass both directions:

1. bad world plus the good values of the set satisfies the oracle;
2. good world plus the bad values of the set violates the oracle.

The set must also be minimal within the tested factor model.

## Consequences

- `PROVEN` is intentionally difficult to obtain.
- Analysis uses more experiments than one-way delta debugging.
- Unsupported, unstable, or nondeterministic cases degrade to lower confidence states.
