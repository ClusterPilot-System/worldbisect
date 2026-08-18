# Determinism

WorldBisect separates deterministic representation from nondeterministic executions.

## Deterministic artifacts

Portable bundles are deterministic for the same stored entity:

- entries are sorted;
- timestamps use `SOURCE_DATE_EPOCH` or zero;
- ownership is normalized;
- gzip headers are normalized;
- JSON is encoded canonically;
- generated archives are compared in tests.

Release packages use fixed timestamps, sorted file order, static binaries, `-trimpath`, and normalized archive metadata.

## Experiment repeatability

A causal proof requires stable baseline and intervention outcomes across configured repetitions. If a command changes outcome without an intervention, analysis is `UNPROVEN`.

The 1.0 engine does not claim deterministic control over:

- kernel scheduling;
- hardware timing;
- network services;
- wall-clock time inside arbitrary programs;
- random-device output;
- distributed state.

These appear as limitations or correlated evidence.

## IDs and timestamps

Runtime entities use collision-resistant IDs and actual timestamps. These values are excluded from semantic proof comparison and normalized in portable artifacts where determinism is required.
