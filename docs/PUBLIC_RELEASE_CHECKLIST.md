# Public release checklist

This checklist covers repository settings that cannot be guaranteed by source code alone.

## Repository

- [ ] Repository is public.
- [ ] Default branch is `main`.
- [ ] Description and topics are set.
- [ ] Issues are enabled.
- [ ] Wiki is disabled unless actively maintained.
- [ ] Discussions are enabled only when moderation capacity exists.
- [ ] `LICENSE`, `NOTICE`, `COPYRIGHT`, governance, contribution, provenance, and security files are visible.

## Branch protection

Protect `main` with a GitHub ruleset or branch-protection rule:

- [ ] Pull requests required.
- [ ] At least one approval required.
- [ ] Stale approvals dismissed when new commits are pushed.
- [ ] Conversation resolution required.
- [ ] Required status checks include CI and CodeQL.
- [ ] Branch must be up to date before merge.
- [ ] Force pushes and deletion blocked.
- [ ] Administrators are included or bypass is tightly restricted.

## Security

- [ ] Private vulnerability reporting enabled.
- [ ] Dependabot alerts enabled.
- [ ] Dependabot security updates enabled.
- [ ] Secret scanning enabled.
- [ ] Push protection enabled.
- [ ] Code scanning enabled through `.github/workflows/codeql.yml`.
- [ ] Actions permissions default to read-only.
- [ ] Workflow approval policy reviewed for first-time contributors.

## Release

- [ ] Tag is annotated or cryptographically signed.
- [ ] Release workflow completed successfully.
- [ ] Release is neither draft nor prerelease.
- [ ] `SHA256SUMS` verifies every published binary artifact.
- [ ] SPDX SBOM is attached.
- [ ] GitHub artifact attestation is available.
- [ ] AMD64 package executed on an AMD64 Linux system.
- [ ] ARM64 package executed on a real ARM64 Linux system before claiming runtime validation.

## Maintainer operations

- [ ] Private security-report notifications reach an active maintainer.
- [ ] Organization recovery methods and release credentials are current.
- [ ] No long-lived release token is stored in the repository.
- [ ] Maintainer and CODEOWNERS entries are correct.
