# Security architecture

## Security goals

- Prevent unauthenticated API access.
- Keep remote execution disabled unless explicitly configured.
- Ensure authorization is tied to the object actually executed.
- Bound resource use and persistent growth.
- Protect signing keys and bearer-token material.
- Treat captures, workspaces, outputs, and bundles as untrusted.
- Prevent unsupported evidence from becoming a false causal proof.

## Authentication

Bearer tokens are random opaque values. Configuration stores only SHA-256 hashes. Authentication compares hashes in constant time and assigns explicit scopes.

Tokens should be stored in an external secret manager. Rotate them by replacing configuration hashes and restarting the daemon.

## Command authorization

Allowed commands must be canonical absolute paths. Authorization does not compare basenames and does not trust `PATH`.

For each remote execution:

1. open the allowed executable without following a final symlink;
2. verify it is a regular executable file;
3. record device, inode, size, mode, ownership, modification time, change time, and SHA-256 digest;
4. open the authorized working directory;
5. resolve both through `/proc/self/fd`;
6. execute through the opened executable descriptor and enter the opened directory descriptor;
7. revalidate executable identity immediately before child execution;
8. reject path, symlink, hardlink, inode, metadata, or digest changes.

This closes the basename bypass and materially reduces path time-of-check/time-of-use attacks.

## Process isolation

The runner uses a new process group and kills the group on timeout or cancellation. Experiments operate on copied temporary workspaces and do not intentionally modify the original workspace.

This is not a full malicious-code sandbox. Use an independently hardened VM or container when executing untrusted code.

## Archive safety

Bundle import rejects:

- absolute paths;
- `..` traversal;
- duplicate entries;
- symbolic links and hard links;
- devices and special files;
- excessive entry counts and sizes;
- digest or manifest mismatches;
- data outside the declared entity root.

Extraction occurs into a temporary directory and is committed only after validation.

## Secret handling

Environment names matching credential, token, key, password, cookie, authorization, and secret patterns are redacted with an HMAC fingerprint. Raw values are never stored or used as automatic causal factors.

Command output and workspace files may still contain secrets. Operators must choose safe workspaces and commands and review artifacts before sharing.

## Signing keys

The causal Ed25519 private key is generated locally with restrictive permissions and remains in the data directory. Compromise of the key requires rotation and invalidation of trust in certificates signed after the suspected compromise time.

## Audit

Mutating operations append a canonical event to a hash chain. Verification detects modification, deletion within the retained sequence, or broken linkage. An attacker able to rewrite the entire data directory and replace trust anchors remains outside this local integrity model.

## Supply chain

- no third-party Go module dependencies;
- GitHub Actions pinned to immutable SHAs;
- deterministic packages;
- SHA-256 checksum manifest;
- SPDX SBOM;
- GitHub artifact attestation;
- license and provenance records in source and binary packages.
