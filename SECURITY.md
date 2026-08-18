# Security policy

## Supported versions

| Version | Supported |
| --- | --- |
| 1.x | Yes |
| Earlier or unreleased builds | No |

## Reporting a vulnerability

Use GitHub private vulnerability reporting:

https://github.com/ClusterPilot-System/worldbisect/security/advisories/new

Do not open a public issue for vulnerabilities, proof-integrity failures with sensitive evidence, credential exposure, archive parser exploits, command-authorization bypasses, or remote execution flaws.

Include:

- affected version and architecture;
- threat model and required privileges;
- minimal sanitized reproduction;
- impact assessment;
- suggested remediation when available.

Do not include real credentials, private captures, customer data, or proprietary artifacts.

## Response process

Maintainers will acknowledge a valid report, reproduce it in an isolated environment, assess severity, prepare regression coverage and a patch, and coordinate disclosure. Timelines depend on severity and reproducibility; no fixed remediation deadline is promised.

## Security boundaries

WorldBisect executes user-selected commands and processes untrusted captures and bundles. Important boundaries include:

- authenticated daemon access;
- command and working-directory authorization;
- opened-file-descriptor binding for remote execution;
- archive extraction and import validation;
- secret redaction;
- certificate signing keys;
- proof-state transitions;
- audit-chain integrity.

See `docs/security.md` and `docs/threat-model.md`.

## Release integrity

Official releases are produced from immutable tags by GitHub Actions. Release artifacts include SHA-256 checksums, an SPDX SBOM, and a GitHub artifact attestation.

Consumers should verify both checksums and provenance before installation.
