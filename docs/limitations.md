# Limitations and evidence boundaries

WorldBisect 1.0 is intentionally conservative.

## Eligible for `PROVEN`

- non-secret environment-variable value or presence differences;
- regular files under the captured workspace: content, mode, presence;
- directories under the captured workspace: mode, presence;
- symbolic links under the captured workspace: target, presence;
- combinations of the above;
- explicit machine-checkable oracles supported by the engine.

## Captured or compared but not automatically proven

- host files outside the workspace;
- shared libraries and package versions;
- mount topology and options;
- kernel parameters and security modules;
- cgroup and resource values;
- network and DNS observations;
- secret environment values;
- wall-clock and random-device values;
- process and thread schedules;
- CPU features;
- container-runtime behavior.

These can be `CORRELATED`, `SUPPORTED` by operator evidence, or `UNPROVEN`.

## Not supported in 1.0

- kernel-internal causal intervention;
- GPU, firmware, or hardware-debugging claims;
- distributed multi-host causal minimization;
- transparent replay of arbitrary encrypted network protocols;
- automatic source-code modification;
- causal claims without a stable executable oracle;
- Windows or macOS release support.

## Native tracer

Native selected-syscall tracing is implemented for Linux AMD64. Other architectures use basic capture. ARM64 release artifacts are cross-compiled and structurally validated; runtime equivalence with AMD64 native tracing is not claimed.

## Security boundaries

The tool cannot make an untrusted command safe. Run unknown commands inside an independently hardened sandbox. WorldBisect isolation is designed for experiment reproducibility and original-workspace protection, not as a complete malicious-code containment boundary.
