# FAQ

## Is WorldBisect an AI debugger?

No. The proof result is produced by executed counterfactual experiments, a declared oracle, deterministic minimization, and bidirectional verification. External AI tooling may propose experiments through future stable interfaces, but cannot set `PROVEN` directly.

## Does it replace logs or tracing?

No. It consumes runtime evidence and complements logs, tracing, record/replay, tests, and Git bisect.

## Can it prove any Linux cause?

No. Version 1.0 proves only causes within its bounded intervention model. Other factors are reported as boundaries.

## Does it modify my original workspace?

Capture reads the workspace. Experiments use copied temporary workspaces. Import and export use separate artifact paths.

## Is remote execution enabled by default?

No. It must be explicitly enabled and constrained by absolute command and working-directory allowlists, authentication scopes, quotas, and file-descriptor binding.

## Does WorldBisect upload telemetry?

No. Logs, metrics, traces, captures, and audit records remain local unless the operator independently exports them.

## Can I send `.wcap` files publicly?

Treat them as sensitive. They may contain command output, paths, metadata, and sanitized but still private evidence. Review them before sharing.

## Why can an analysis be `UNPROVEN`?

Common reasons include unstable baselines, an unsupported factor, no effective intervention, experiment-budget exhaustion, capture loss, or failure outside the controlled model.
