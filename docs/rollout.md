# Rollout guide

WorldBisect 1.0 is a new diagnostic system. Deploy conservatively.

## Phase 1: Local CLI

- Install on an isolated developer or QA Linux host.
- Run `worldbisect doctor`.
- Use only synthetic example workspaces.
- Verify capture, export, import, comparison, and certificate validation.

Exit criterion: deterministic example analyses produce expected results and no private data is captured unexpectedly.

## Phase 2: Controlled engineering use

- Select non-production test commands with explicit oracles.
- Define workspace boundaries narrowly.
- Retain captures according to an approved data policy.
- Compare WorldBisect conclusions with manual debugging.

Exit criterion: engineers understand `PROVEN` versus evidence-boundary states and trust the operational data handling.

## Phase 3: Local daemon

- Initialize a dedicated service account and data directory.
- Keep the listener on loopback.
- Leave remote execution disabled.
- Integrate read-only API access with local tooling.

Exit criterion: authentication, audit, backup, restore, quotas, and monitoring are validated.

## Phase 4: Bounded remote execution

Only when required:

- place the daemon behind TLS and network policy;
- use narrowly scoped tokens;
- allow only canonical absolute commands and directories;
- set strict concurrency, timeout, output, factor, and storage quotas;
- exercise incident-response and token-revocation procedures.

Exit criterion: security review approves the exact execution boundary.

## Rollback

Stop the daemon and remove the package. Existing data remains under the configured data directory. Restore a previous binary only when it supports the existing schema; otherwise restore the pre-upgrade data backup together with the previous package.
