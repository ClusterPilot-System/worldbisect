# ADR 0003: Native ptrace on Linux AMD64

- Status: Accepted
- Date: 2026-08-17

## Context

Capture must observe files consulted by arbitrary binaries without requiring language-specific instrumentation or external runtime dependencies.

## Decision

Linux AMD64 uses a native bounded `ptrace` tracer for selected filesystem-related system calls. Unsupported platforms use the portable basic capture path.

The tracer is observational. It does not grant automatic causal status to host paths or libraries.

## Consequences

- Linux AMD64 receives richer evidence.
- ARM64 packages remain usable but do not claim equivalent native syscall coverage in 1.0.
- Kernel, seccomp, container, or security policies may prevent tracing; the capture records that boundary.
