# Governance

WorldBisect is maintained by the maintainers listed in `MAINTAINERS.md`.

## Decision principles

1. Proof soundness is more important than convenience.
2. Unknown or uncontrolled factors produce `UNPROVEN`, never a fabricated causal claim.
3. Security boundaries fail closed.
4. The 1.x product contract remains bounded and maintenance-oriented.
5. Public contracts change only with explicit compatibility analysis and documentation.
6. Repository history, issue discussions, and architecture decisions are the public record.

## Maintainer responsibilities

Maintainers:

- triage defects and security reports;
- review changes to the proof kernel and execution boundary;
- protect release integrity;
- enforce licensing and provenance requirements;
- publish deterministic release artifacts and security advisories;
- avoid conflicts of interest and disclose them when relevant.

## Changes to governance

Governance changes require a public pull request and approval from an active maintainer. Changes affecting ownership, licensing, release authority, or security response require explicit rationale and a transition plan.

## Project continuity

If no listed maintainer is active for 90 days, contributors may open a governance issue proposing new maintainers. Transfer of release authority must be recorded publicly and must not alter existing Apache-2.0 rights.
