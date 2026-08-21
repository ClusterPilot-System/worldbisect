# Architecture

## Product contract

WorldBisect determines whether a smallest supported factor set is necessary and sufficient for an observed command failure within a declared intervention model.

It is not a universal debugger. It does not infer kernel, hardware, external-service, or distributed causality without control over those factors.

## System context

```text
User or CI
    |
    v
CLI / authenticated HTTP API / embedded dashboard
    |
    v
Capture service ---- Job manager ---- Audit chain
    |                     |
    v                     v
Runner + observer     Persistent store
    |                     |
    v                     v
Typed world       Content-addressed blobs
    |
    v
Comparator -> experiment planner -> isolated materializer -> oracle
    |
    v
Bidirectional proof kernel -> report -> signed certificate / bundle
```

## Components

### CLI

The CLI is the primary interface. It captures commands, compares sessions, explains analyses, previews and explicitly confirms support handoffs, exports and imports bundles, verifies certificates, validates the audit chain, runs host diagnostics, and can host the API.

### Daemon

`worldbisectd` initializes a secure configuration, runs the HTTP server, and executes a bounded persistent job queue. One daemon is authoritative for one data directory.

### API and dashboard

The REST API is versioned under `/api/v1`. The embedded dashboard is static HTML, CSS, and JavaScript compiled into the Go binary. It has no Node.js runtime dependency.

### Runner

The runner starts a new process group, applies timeout and output limits, captures process identity and resource evidence, and terminates the entire group on cancellation or timeout.

On Linux AMD64, a bounded `ptrace` tracer records selected filesystem-related syscalls and consulted paths. Other platforms use basic capture.

### Workspace capture

The workspace scanner records regular files, directories, and symbolic links using relative paths. It stores content hashes and file mode, rejects traversal, does not follow symlink targets, and enforces file and byte quotas.

### Comparator

The comparator emits typed factors only for supported differences:

- non-secret environment-variable values and presence;
- regular workspace file content, mode, and presence;
- directory presence and mode;
- symbolic-link target and presence.

Host, mount, network, resource, package, library, and secret differences are retained as evidence boundaries.

### Experiment engine

Experiments run in fresh temporary directories. The selected good or bad workspace is copied without following symlinks, interventions are applied, and the command is rerun against the declared oracle.

The engine validates baseline determinism, minimizes candidates with deterministic delta debugging, verifies every strict subset for minimality when bounded, and performs both causal directions.

### Proof kernel

The proof state machine is:

```text
PROVEN
SUPPORTED
CORRELATED
UNPROVEN
```

`PROVEN` requires:

1. stable good and bad baselines;
2. at least one supported factor;
3. bad-to-good intervention passes repeatedly;
4. good-to-bad intervention fails repeatedly;
5. tested minimality;
6. no experiment-budget or evidence-integrity failure.

### Store

The store contains:

```text
data/
  schema.json
  captures/*.json
  analyses/*.json
  jobs/*.json
  idempotency/*.json
  blobs/sha256/<prefix>/<digest>.json.gz
  audit/events.jsonl
  keys/causal_ed25519_private.pem
  keys/causal_ed25519_public.pem
  traces/spans.jsonl
```

All entity writes use temporary files, sync, and rename. Compound state transitions run under one transaction lock. Blobs are content-addressed and compressed.

### Jobs

A job transition from queued to running is atomic with owner and lease assignment. Only the lease owner may heartbeat or complete it. Expired jobs are requeued up to the configured attempt limit.

### Artifacts

Portable bundles use deterministic tar.gz output. Import validates archive paths, entry types, duplicate names, sizes, digests, and manifest structure before materializing an entity.

Causal certificates are canonical JSON payloads signed with Ed25519.

### Observability

- Logs are structured JSON.
- Metrics use Prometheus text exposition.
- Spans are local JSONL records with W3C trace context.
- Audit entries form a SHA-256 hash chain.
- No telemetry leaves the host unless the operator configures an external collector outside WorldBisect.

## Trust boundaries

```text
Untrusted:
  commands and subprocesses
  workspaces
  stdout and stderr
  imported bundles
  capture labels and API inputs
  dashboard browser

Privileged or trusted:
  daemon configuration
  token hashes
  signing key
  proof-state transition code
  command authorization
  store transaction and audit code
```

## Reliability

- Context cancellation terminates process groups.
- Server timeouts bound headers, reads, writes, and idle connections.
- Job leases recover abandoned work.
- Idempotency prevents duplicate accepted mutations.
- Quotas bound storage and experiment growth.
- Atomic writes prevent partial entities.
- Audit verification detects mutation or truncation within the retained chain.

## Deployment

The default deployment binds to loopback and runs under a dedicated system account. Remote exposure should be placed behind a trusted TLS reverse proxy and network policy. Remote execution should remain disabled unless the operator explicitly needs it.
