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

`scripts/release-e2e.sh` runs the same contract against an extracted release
package instead of a source-built binary. It verifies the selected archive
against `SHA256SUMS`, checks both packaged binaries, exercises the bundled
`scripts/install.sh` into an isolated `DESTDIR`, and then executes the full
capture/import/compare/handoff/report/certificate/audit flow. The release check
invokes this locally for the package produced by `make release`; the scheduled
CI consumer job downloads a published GitHub release and runs it unchanged.

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
./scripts/release-e2e.sh --version 1.2.1
make coverage
```

The published-release check requires the GitHub CLI (`gh`) and network access.
For a local package, pass `--archive` and `--checksum-file` instead.

## Architecture coverage

The required GitHub Actions `arm64-runtime` job runs the test suite, race detector,
E2E flow, and packaged ARM64 binaries on a native `ubuntu-22.04-arm` runner.
Local Windows development still does not provide native ARM64 validation; the
repository must not claim ARM64 validation when that CI job is not green.
