# Release checklist

## Source

- [ ] `VERSION` matches the semantic release tag.
- [ ] Changelog entry exists.
- [ ] Working tree contains no generated binaries, captures, tokens, or private data.
- [ ] Go module path matches the canonical repository.
- [ ] License, NOTICE, copyright, provenance, governance, and security documents are current.

## Validation

- [ ] `gofmt` reports no changes.
- [ ] `go mod verify` succeeds.
- [ ] `go vet ./...` succeeds.
- [ ] `go test ./... -count=1` succeeds.
- [ ] `go test -race ./... -count=1` succeeds.
- [ ] `make e2e` succeeds.
- [ ] Security regression tests pass.
- [ ] Clean-source build succeeds.
- [ ] Release build is byte-reproducible.

## Packaging

- [ ] AMD64 and ARM64 binaries are correct ELF architectures.
- [ ] Debian packages contain `LICENSE`, `NOTICE`, and Debian copyright metadata.
- [ ] Installed files have safe permissions.
- [ ] Source archives contain no absolute or traversal paths.
- [ ] `SHA256SUMS` validates all artifacts.
- [ ] SPDX SBOM is valid JSON and names the exact release.

## GitHub

- [ ] Actions are pinned to immutable commit SHAs.
- [ ] GitHub Immutable Releases are enabled for the repository or organization.
- [ ] Full release assets are uploaded to a draft before publishing the immutable release.
- [ ] The `v1` compatibility tag points to the intended reviewed 1.x release commit.
- [ ] Release workflow has minimum explicit permissions.
- [ ] CodeQL is enabled.
- [ ] Tag is immutable and points to the reviewed commit.
- [ ] Artifact attestation is published.
- [ ] Release assets exactly match `SHA256SUMS`.
