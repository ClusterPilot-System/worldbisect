# Testing strategy

## Test layers

### Unit tests

Cover configuration, authentication, redaction, oracle evaluation, workspace handling, runner behavior, store migrations, audit verification, comparison, minimization, certificates, bundles, API validation, and job transitions.

### Security regression tests

Cover:

- command basename bypass;
- relative and `PATH` command rejection;
- symlink command rejection;
- hardlink substitution;
- executable replacement and in-place modification;
- working-directory symlink replacement;
- archive traversal and special entries;
- token hashing and constant-time verification behavior;
- audit-chain tampering;
- idempotency conflicts;
- parallel job claims.

### Integration tests

Exercise real process execution, output limits, process-group timeout, native Linux AMD64 tracing when available, persistent store behavior, API authentication, and daemon initialization.

### End-to-end tests

`scripts/e2e.sh` builds the binaries and runs:

1. good workspace capture;
2. bad workspace capture;
3. portable export and import;
4. typed comparison;
5. bidirectional causal minimization;
6. `PROVEN` result assertion;
7. certificate creation and verification;
8. daemon initialization;
9. authenticated API read;
10. audit-chain verification.

### Race detector

```bash
go test -race ./... -count=1
```

This is required because the store, API, and job manager are concurrent.

### Determinism

Bundle export is run twice and compared byte-for-byte. Release packaging is run twice with the same `SOURCE_DATE_EPOCH` and artifact digests are compared.

## Local commands

```bash
make check
make test-race
make e2e
make coverage
```

## Architecture coverage

The required GitHub Actions `arm64-runtime` job cross-builds and executes the
ARM64 binaries, tests, E2E flow, and packaged artifacts under QEMU emulation.
This verifies ARM64 binary compatibility in CI, but is not a substitute for
native ARM64 hardware validation. A release must not claim native hardware
validation unless that hardware-specific check is separately green.
