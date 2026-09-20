# Changelog

All notable changes to this project are documented in this file.

The format is based on Keep a Changelog and the project follows Semantic Versioning for its public contracts.

## [Unreleased]

## [1.2.1]

### Fixed

- Bind optional daemon file opens and workspace capture to authorized directory
  descriptors, and reject special files without blocking. Bound `file_digest`
  rules read captured regular files; links and missing entries return an explicit
  failed result.
- Accept GitHub's provider-selected OIDC request route and direct-job
  `job_workflow_ref` when it exactly matches the configured workflow. Preserve
  signature, issuer, audience, repository, branch and replay checks; emit only
  fixed diagnostic categories on publication failures.
- Exclude generated build state and Python caches from source/SBOM artifacts,
  populate deterministic binary build timestamps, and include existing license,
  notice and copyright files in Debian packages.
- Compute the SPDX 2.3 package verification code from sorted file SHA-1
  checksums while retaining SHA-256 file and release checksums.

### Added

- Experimental, source-built team hub with isolated workspaces, named identities,
  scoped credentials, atomic revocation/reload, bounded audit records, verified
  GitHub Actions publisher origin and encrypted backup/recovery. Diagnosis
  confidence remains client-reported; this preview is not a hosted SaaS.
- Live GitHub identity checks, restart replay protection tests and encrypted
  recovery checks, including a restore quarantine for previously issued tokens.
- Root Action `mode: ci`, first-diagnosis onboarding and a tested reviewdog SARIF
  integration. Marketplace Action `action-v1.0.0` is versioned separately from
  engine archives; existing verified engine download pins remain unchanged.

### Changed

- Update pinned CI tools and run real age encryption checks in the release gate.
- Release binary archives continue to contain the CLI and diagnostic daemon;
  the team hub remains available through the source distribution.

## [1.2.0]

### Added

- Opt-in CI companion Action retains explicitly selected successful inputs,
  finds compatible trusted baselines, reproduces both outcomes, and preserves
  the original check failure while testing candidate causes.
- Short workflow summaries and optional updatable PR comments explain the
  finding, executed tests, confidence, and next step.
- Bounded retention and input sizes, credential rejection, clean environments,
  fork isolation, and explicit non-reproducible outcomes.
- Real-tool integration checks cover Node.js, GCC and the pinned ItsDangerous
  upstream suite (297 tests), plus automatic cross-run artifact verification.
- Reviewed VERSION changes on main run the gated, attested release pipeline.

### Fixed

- Native Linux AMD64 tracing keeps ptrace operations on their owning OS
  thread, scopes child waits to the isolated process group, drains command
  output, and bounds cancellation of nested processes. Commands that change
  process groups use documented portable fallback. This fixes a compiler-check
  hang and concurrent command exit-status theft.
- Release archives include the standalone installer.

### Community

- Added contributor onboarding with two concrete `good first issue` tickets,
  public Discussions guidance, a bounded 1.x roadmap, and monthly maintenance
  notes.
- Added a consented/anonymized user-report intake form and publication gate;
  no synthetic demo is presented as real user evidence.

### Security

- Future GitHub releases are published through the repository's Immutable
  Releases setting using a draft-first asset upload flow.
- Documented the moving `v1` Action compatibility tag separately from full
  commit-SHA pins and archive/attestation verification for security-critical
  workflows.

## [1.1.1]

### Changed

- The v1.1.1 Action release supplies the verified v1.1.1 Linux archive digest
  by default; custom releases and repositories still require an explicit
  `sha256`.
- GitHub Action runs publish the diagnosis, report link, and diagnostic artifact
  link in `GITHUB_STEP_SUMMARY` and expose artifact URLs as outputs.

### Fixed

- Action failure handling now reports a clear operational failure when a
  diagnosis output is unavailable instead of masking it with a secondary file
  error.

## [1.1.0]

### Added

- Production-ready GitHub Action for deterministic workspace diagnosis.
- Markdown, JSON, JUnit, and SARIF Action outputs with redacted diagnostic handoffs.
- Explicit Action failure policies and stable analysis outputs.
- Public five-minute demo workflow and reproducible `PROVEN` fixture.
- Hardened workspace capture against hardlinks, symlinks, and in-place races.

### Security

- Action downloads verify the selected immutable release archive before execution.
- GitHub workflows use explicit permissions and immutable action references.

## [1.0.0]

### Added

- Typed capture of Linux command executions, environments, workspace state, process identity, mounts, resources, and host evidence.
- Good/bad session comparison with a bounded intervention model.
- Bidirectional causal minimization for environment and workspace factors.
- `PROVEN`, `SUPPORTED`, `CORRELATED`, and `UNPROVEN` result semantics.
- Signed Ed25519 causal certificates and deterministic portable bundles.
- File-backed schema with migrations, content-addressed blobs, audit hash chain, API, daemon, dashboard, metrics, and trace output.
- Command authorization, atomic job leases, execution timeouts, quotas, and conservative secret redaction.
- Linux AMD64 and ARM64 release packages, Debian packaging, systemd integration, SPDX SBOM, deterministic release scripts, and GitHub artifact attestations.
- Unit, integration, race, determinism, security, and end-to-end tests.

### Security

- Remote command execution is disabled by default.
- Remote execution requires canonical absolute executable paths and permitted working directories.
- Authorization binds execution to opened file descriptors to resist path, symlink, hardlink, and in-place replacement attacks.
- Secret-looking environment variables are redacted and are never eligible for automatic causal claims.
