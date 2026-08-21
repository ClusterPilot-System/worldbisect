# User guide

## Mental model

WorldBisect needs:

1. a good execution that satisfies an oracle;
2. a bad execution that violates the same oracle;
3. a bounded workspace and environment;
4. a command that can be rerun safely;
5. enough determinism to distinguish intervention effects from noise.

The tool does not fix source code automatically. It isolates a supported causal factor set.

## Oracles

Supported forms:

```text
exit=0
exit=1
timeout
stdout_regex=<pattern>
stderr_regex=<pattern>
file_digest=<relative-path>:sha256:<hex>
```

Choose the narrowest machine-checkable condition that represents success.

## Capture

```bash
worldbisect capture \
  --store /tmp/wb-store \
  --workspace /path/to/good \
  --oracle exit=0 \
  --label good \
  --output /tmp/good.wcap \
  -- ./test-command
```

A failed command may return a nonzero CLI status while still writing a valid bad capture.

Capture records:

- command and sanitized environment;
- workspace manifest and content blobs;
- process exit, signal, timeout, stdout, and stderr;
- user, group, capabilities, seccomp, cgroup, mount, and host evidence;
- selected consulted paths on Linux AMD64;
- warnings and unsupported evidence boundaries.

## Compare

```bash
worldbisect compare \
  --store /tmp/wb-store \
  --good /tmp/good.wcap \
  --bad /tmp/bad.wcap \
  --repetitions 3 \
  --max-experiments 128 \
  --certificate /tmp/result.wbc \
  -- ./test-command
```

WorldBisect imports bundles when a path is supplied, compares the sessions, reruns the baselines, tests candidate groups, minimizes a supported factor set, reverses the intervention direction, and records all experiments.

## Result states

The default text report is intended for normal operators. It presents the
result first, explains the conclusion in plain language, and gives numbered
next steps. Internal analysis and capture IDs remain at the bottom under
`Technical details (for support)`.

### PROVEN

The factor set passed repeated bad-to-good and good-to-bad intervention and was minimal within the tested model.

For a workspace factor, follow the numbered steps in the report: back up the
named path, compare it with the known-good workspace, restore only that factor,
rerun the original command, and collect fresh captures if the problem remains.
Do not treat the report as an automatic repair; the operator must review and
approve the change.

### SUPPORTED

Executed experiments support a causal interpretation, but a proof condition such as complete minimality or full repetition was not met.

### CORRELATED

The difference tracks the failure but could not be independently controlled.

### UNPROVEN

No sound causal claim can be made. Read the limitations and experiment diagnostics.

## Explain

```bash
worldbisect explain --store /tmp/wb-store <analysis-id>
```

The default output is stable Markdown and can be pasted directly into a GitHub
pull request or support ticket. Use `--format markdown` explicitly when a
script needs to document the chosen presentation. Use `--format json` for the
versioned `worldbisect.analysis-report.v1` automation contract. Its top-level
fields are `schema_version`, `status`, `proof`, `cause`, `boundaries`,
`limitations`, `next_steps`, and `evidence`. The report deliberately omits
captured command output and factor values; use a reviewed diagnostic bundle for
that sensitive evidence.

## Export and import

```bash
worldbisect export --store /tmp/wb-store --output capture.wcap <capture-id>
worldbisect import --store /tmp/other-store capture.wcap
```

Bundles are deterministic and validated on import. Treat them as sensitive until reviewed.

## Verify certificate

```bash
worldbisect verify result.wbc
```

Optionally supply an independently trusted public key.

## Audit

```bash
worldbisect audit --store /tmp/wb-store
```

A valid response confirms retained audit-chain linkage and hashes, not protection against an attacker who replaced the complete store and its trust anchors.

## Doctor

```bash
worldbisect doctor
```

Doctor reports platform, procfs, mount information, writable store location, and native tracer availability.

## Daemon

Initialize:

```bash
sudo worldbisectd init
```

Start:

```bash
sudo systemctl enable --now worldbisectd
```

The default daemon listens on loopback and disables remote execution. See `docs/operations.md` before enabling write scopes or remote access.

## Data sensitivity

Environment values with secret-like names are redacted. Command output and workspace content are not automatically safe. Use synthetic reproductions whenever possible.
