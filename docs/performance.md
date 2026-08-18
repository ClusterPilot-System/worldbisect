# Performance and cost model

WorldBisect is local-first and has no mandatory service cost.

## Cost drivers

1. Workspace traversal and hashing.
2. Content-blob compression and storage.
3. Command runtime multiplied by repetitions and experiment count.
4. Copying good and bad workspaces for each intervention.
5. Native tracing overhead on Linux AMD64.

Approximate analysis time:

```text
baseline repetitions
+ minimization experiments
+ bidirectional verification
+ minimality checks
```

multiplied by the command duration.

## Controls

- workspace file and byte quotas;
- output-size limits;
- timeout per command;
- maximum factors;
- maximum experiments;
- experiment repetitions;
- worker concurrency;
- content-addressed deduplication;
- gzip compression;
- exclusion of unsupported host state from workspace copying.

## Recommendations

- Capture the smallest workspace that contains the suspected state.
- Use a fast, deterministic oracle.
- Minimize test setup before invoking WorldBisect.
- Keep the first analysis budget small; increase it only when the candidate space is justified.
- Do not place the store on a high-latency network filesystem.

## Benchmarks

Repository benchmarks cover workspace scanning and candidate minimization. Benchmark numbers are environment-dependent and are not published as universal performance guarantees.
