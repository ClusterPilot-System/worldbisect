# Data model and migrations

## Schema version

The store schema is versioned in `schema.json`. WorldBisect 1.0 opens older supported schemas and applies ordered, idempotent migrations to the current version.

Current schema version: `3`.

## Entities

### Capture

A capture contains:

- immutable ID and timestamps;
- label and command specification;
- process result and oracle result;
- sanitized environment;
- workspace manifest;
- host, mount, resource, network, and syscall evidence;
- evidence boundaries and capture warnings;
- content-blob references.

### Analysis

An analysis contains:

- good and bad capture IDs;
- typed differences;
- supported causal candidates;
- experiment records;
- minimized causal factor IDs;
- bidirectional verification;
- confidence status and limitations;
- certificate metadata.

### Job

A job contains:

- type and payload digest;
- state: queued, running, succeeded, failed;
- lease owner, lease expiry, heartbeat, attempt count;
- idempotency identity;
- result reference or structured error.

### Idempotency record

An idempotency record binds:

```text
principal + route + key -> canonical request digest + job ID
```

Reuse with a different digest is a conflict.

### Blob

A blob is addressed by SHA-256 of uncompressed content. The stored representation is gzip-compressed JSON metadata plus content. Existing identical blobs are reused.

### Audit event

An audit event contains sequence, timestamp, actor fingerprint, action, entity, request ID, detail digest, previous hash, and current hash.

## Migrations

Migrations run while holding the store transaction lock and write a new schema marker only after all steps complete.

- v1 -> v2: adds explicit proof status and evidence boundaries.
- v2 -> v3: adds job leases, idempotency records, trace metadata, and audit versioning.

Migration functions are safe to rerun. Unsupported future schema versions fail closed.

## Retention

WorldBisect does not silently delete captures or analyses. Operators can remove entities according to local policy after exporting needed evidence. Content blobs are deduplicated; garbage collection is intentionally not automatic in 1.0 because incorrect reachability collection could destroy evidence.
