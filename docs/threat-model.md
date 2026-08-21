# Threat model

## Assets

- host filesystem and original workspace;
- daemon bearer-token hashes;
- raw tokens held by operators;
- causal signing private key;
- captures, analyses, command output, and imported bundles;
- audit-chain integrity;
- proof-state correctness;
- release artifacts and source provenance.

## Adversaries

- unauthenticated network client;
- authenticated client with insufficient scope;
- malicious command or workspace;
- attacker controlling an imported bundle;
- local user able to race pathnames;
- compromised worker process;
- contributor attempting supply-chain injection;
- maintainer credential compromise.

## Trust assumptions

- the Linux kernel and Go runtime enforce documented primitives;
- the operator protects configuration, raw bearer tokens, and signing keys;
- the daemon is authoritative for one data directory;
- remote TLS and perimeter controls are supplied outside WorldBisect;
- users provide an honest machine-checkable oracle.

## Threats and controls

### Unauthorized API access

Controls: loopback default, bearer authentication, hashed stored tokens, constant-time verification, explicit scopes, request limits.

### Command allowlist bypass

Controls: canonical absolute paths, no basename matching, no `PATH` authorization, final-symlink rejection, opened-file-descriptor binding, inode and digest revalidation.

Workspace capture separately rejects Linux hardlinks and verifies file identity
and metadata before and after reads, preventing pathname replacement and
unstable file ingestion from becoming stored evidence.

### Working-directory replacement

Controls: canonical directory authorization, directory descriptor binding, pre-execution identity validation.

### Duplicate command execution

Controls: idempotency-key binding, atomic queued-to-running claim, lease ownership, heartbeat, bounded retry, terminal state checks.

### Resource exhaustion

Controls: request-size, output, timeout, workspace-file, workspace-byte, session, factor, experiment, worker, and attempt quotas.

### Archive exploitation

Controls: strict path canonicalization, duplicate rejection, file-type allowlist, size bounds, digest validation, temporary extraction, atomic import.

### Secret disclosure

Controls: environment-name redaction, no automatic secret factors, restrictive store permissions, no telemetry. Residual risk: secrets in command output or workspace content.

### False causal proof

Controls: bounded factor types, deterministic baselines, repeated experiments, bidirectional verification, minimality checks, fail-closed status transitions, signed certificates.

### Supply-chain compromise

Controls: no external Go modules, immutable Action SHAs, CodeQL, protected main branch, review policy, deterministic releases, checksums, SPDX SBOM, artifact attestation.

## Out of scope

- malicious kernel or firmware;
- root attacker with persistent control of all local state and trust anchors;
- complete containment of arbitrary hostile code;
- causal proof outside the supported intervention model;
- attacks on external TLS proxies or secret managers.
