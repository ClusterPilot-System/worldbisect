# Contributing to WorldBisect

WorldBisect 1.x is maintained as a stable, bounded Linux diagnostic package. Contributions should fix defects, improve compatibility, strengthen security, or clarify documentation without silently broadening the causal contract.

## Before contributing

1. Read `docs/architecture.md`, `docs/security.md`, `docs/threat-model.md`, and `docs/limitations.md`.
2. Search existing issues and pull requests.
3. Use a public issue for normal defects and GitHub private vulnerability reporting for security-sensitive findings.
4. Never upload real credentials, private captures, customer data, or proprietary binaries.

## Development

Required tools:

- Linux
- Go version declared by `go.mod`
- Bash
- Python 3
- standard packaging tools for release packaging

Run the complete local validation:

```bash
make check
make test-race
make e2e
```

A release build is produced with:

```bash
SOURCE_DATE_EPOCH=0 make release
```

## Pull requests

Every pull request must:

- have a narrow, explicit scope;
- include regression coverage for changed behavior;
- preserve fail-closed proof semantics;
- document public contract or file-format changes;
- pass formatting, vet, unit, race, E2E, and release checks;
- describe compatibility and security impact;
- avoid adding cloud services, telemetry, or runtime dependencies without a formal architecture decision.

Security-sensitive changes require review from a maintainer familiar with the affected trust boundary.

## Commit policy

Use clear imperative commit messages. Keep generated binaries, captures, local data stores, secrets, and test residue out of Git.

## Licensing and provenance

By submitting a contribution, you agree that it is licensed under Apache License 2.0 and that you have the right to submit it.

Do not paste code from sources with unknown or incompatible licenses. When using AI-assisted tooling, you remain responsible for correctness, licensing, originality review, and disclosure of material third-party provenance. See `PROVENANCE.md`.
