# Source provenance

WorldBisect is an original open-source project maintained in this repository and released under Apache License 2.0.

## Contribution provenance

Every contributor is responsible for ensuring that submitted material:

- is their original work or is lawfully reusable;
- does not copy code, documentation, or assets from incompatible or unidentified sources;
- preserves required notices for compatible third-party material;
- does not contain secrets, private captures, customer information, or proprietary artifacts;
- is licensed to the project under Apache License 2.0.

The Git history is the authoritative contribution record.

## AI-assisted development

AI-assisted tools may be used for design exploration, implementation, review, testing, and documentation. AI output is not treated as an authority or a provenance guarantee. The submitting human remains responsible for:

- technical correctness;
- security and proof soundness;
- license compatibility;
- originality and similarity review;
- tests and documentation;
- disclosure of any material third-party source.

AI-generated suggestions must not introduce unattributed text or code copied from external projects.

## Dependencies

The Go module intentionally has no third-party module dependencies. Release tooling relies on operating-system utilities and GitHub Actions, whose exact action commits are pinned in the workflows.

## Release provenance

Official releases are built by GitHub Actions. Repository-level GitHub Immutable
Releases are enabled for future publications; the workflow uploads assets to a
draft before publication locks the release tag and assets. Earlier published
releases are retained as historical evidence and are not rewritten because
GitHub does not apply release immutability retroactively. The release workflow:

1. validates the source tree;
2. runs unit, race, integration, and end-to-end tests;
3. builds deterministic source and Linux package artifacts;
4. emits SHA-256 checksums and an SPDX SBOM;
5. publishes a GitHub artifact attestation backed by GitHub OIDC and Sigstore.

Consumers should verify `SHA256SUMS` and the GitHub artifact attestation before installing release binaries.

The `v1` Action tag is a moving compatible-release pointer, not a fixed trust
root. Security-critical consumers must pin a reviewed full commit SHA and
verify the architecture-matching archive digest and GitHub attestation.
