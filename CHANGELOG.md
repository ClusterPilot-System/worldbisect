# Changelog

All notable changes to this project are documented in this file.

The format is based on Keep a Changelog and the project follows Semantic Versioning for its public contracts.

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
