# Roadmap

WorldBisect 1.x is maintenance-oriented.

## Public 1.x roadmap

This roadmap is intentionally bounded. A row means the maintenance direction
is public; it is not a release promise or a commitment to a calendar date.

| Horizon | Focus | Definition of done |
| --- | --- | --- |
| Shipped | deterministic capture, bidirectional proof states, signed certificates, redacted handoff, and Linux release packaging | executable contracts, focused tests, and release evidence remain green |
| Shipped | GitHub Action with Markdown/JSON/JUnit/SARIF outputs, immutable archive verification, Step Summary, and team-oriented PR reporting | Action inputs/outputs are documented, reports remain secret-safe, and fork permissions fail closed |
| Available on main | opt-in CI input baselines and automatic bounded failure diagnosis | trusted baseline selection, real-engine regression tests, clear outcomes and documented raw-artifact boundaries; external pilot validation still pending |
| Current maintenance | contributor onboarding, Discussions, monthly notes, public issue triage, and consented anonymized case-study intake | community changes stay documentation-/governance-scoped and do not broaden product support |
| Next bounded work | correctness, security, Linux compatibility, performance, observability, testing, packaging, and documentation maintenance | additive change, regression coverage, compatibility analysis, and updated operator guidance |

### How priorities change

Maintainers use public issues, Discussions, release evidence, and security
reports to refine the next bounded work. A proposed item must state its scope,
support impact, proof/secret boundary, acceptance checks, and rollback or
reversion posture. Items that need a new trust boundary or a new causal domain
are not silently added to 1.x; they require a public architecture proposal and
may belong in a future major version.

The current newcomer queue is [issue #36](https://github.com/ClusterPilot-System/worldbisect/issues/36)
and [issue #37](https://github.com/ClusterPilot-System/worldbisect/issues/37).
Monthly progress is recorded in [`docs/maintenance/`](docs/maintenance/).

Accepted 1.x work:

- correctness and security fixes;
- compatibility fixes within the documented Linux contract;
- performance improvements that preserve semantics;
- diagnostics and documentation improvements;
- test and release-infrastructure hardening.

Not planned for 1.x:

- cloud control planes;
- automatic source-code modification;
- universal kernel or hardware causal claims;
- distributed multi-host interventions;
- hidden telemetry;
- an unbounded plugin marketplace;
- a continuously expanding feature roadmap.

A future major version requires a public architecture proposal, explicit new trust boundaries, compatibility analysis, and proof-semantics review.
