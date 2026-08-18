# ADR 0002: File-backed transactional store

- Status: Accepted
- Date: 2026-08-17

## Context

WorldBisect requires durable local storage, schema migration, portable inspection, content deduplication, atomic entity updates, and a small dependency surface.

## Decision

Use a file-backed store with:

- versioned JSON entities;
- atomic temporary-file plus rename writes;
- content-addressed compressed blobs;
- a process-wide transactional lock for compound transitions;
- an append-only audit log with a hash chain;
- explicit migrations to the current schema.

## Consequences

- The repository has no external database dependency.
- Operators can inspect and back up the data directory.
- Multi-process deployments require one authoritative daemon per data directory.
- High-scale distributed operation is outside the 1.0 contract.
