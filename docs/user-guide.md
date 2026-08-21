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
  --max-workspace-files 10000 \
  --max-workspace-bytes 1073741824 \
  --max-output-bytes 8388608 \
  --timeout 2m \
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
  --max-factors 512 \
  --max-output-bytes 8388608 \
  --timeout 2m \
  --progress \
  --certificate /tmp/result.wbc \
  -- ./test-command
```

WorldBisect imports bundles when a path is supplied, compares the sessions, reruns the baselines, tests candidate groups, minimizes a supported factor set, reverses the intervention direction, and records all experiments. Workspace file/byte limits, output limits, timeouts, factor limits, and experiment budgets are explicit. Progress is written to stderr, so JSON stdout remains machine-readable.

Press Ctrl+C to cancel a long capture or analysis. Running child process groups
are terminated safely, and an interrupted analysis is persisted as `UNPROVEN`
with completed experiments retained in the store/cache. The report explains
that the run was interrupted and that repeating the same analysis can reuse
completed experiment results; partial failure is never presented as proof.

Workspace factors are limited to regular file content/presence, permission
bits, directories, and relative symlinks whose targets remain inside the
workspace. Absolute targets, traversal targets, unsupported filesystem objects,
and symlink escapes are reported as evidence boundaries and are never applied.
Experiments run in fresh temporary directories; the original workspaces are
not modified.

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

For an engineer-to-engineer or support handoff, export an analysis bundle:

```bash
worldbisect handoff --store /tmp/wb-store \
  --analysis <analysis-id> --preview
worldbisect handoff --store /tmp/wb-store \
  --analysis <analysis-id> --output diagnosis.wdiag --confirm
worldbisect import --store /tmp/receiving-store \
  --certificate-output imported.wbc diagnosis.wdiag
worldbisect explain --store /tmp/receiving-store <analysis-id>
worldbisect verify imported.wbc
```

The preview is read-only. It shows the deterministic incident ID, files that
will be created, fields that will be redacted, and retention guidance. No
diagnostic bundle is written until the operator reviews the preview and repeats
the command with `--confirm`. The handoff contains the analysis, good and bad
capture metadata, signed certificate, stable reports, and a checksummed
manifest. Export redacts command output, workspace content blobs, command
arguments, sensitive paths, and secret-looking environment values. Import
validates every entry, the incident manifest, certificate, and report before
persistence.

Treat a handoff as sensitive operational evidence. Share it only through the
approved support channel, retain it for the support period, and delete the
bundle and any separately exported certificate when the case is closed. Keep
the local signing key in the store backup; do not send it with the handoff.

## CI output and exit codes

`compare` and `explain` support these machine-readable formats:

```bash
worldbisect explain --store /tmp/wb-store --format junit <analysis-id> > analysis.junit.xml
worldbisect explain --store /tmp/wb-store --format sarif <analysis-id> > analysis.sarif
```

JUnit reports `PROVEN` and `SUPPORTED` as failures; `CORRELATED` and
`UNPROVEN` are explicit skips. SARIF reports `PROVEN` as an error,
`SUPPORTED`/`CORRELATED` as warnings, and `UNPROVEN` as a note. Report
formatting exits `0` regardless of proof status. Add `--fail-on proven`,
`--fail-on supported`, `--fail-on correlated`, or `--fail-on any` when the CI
job must exit `1` for that status threshold. Operational errors always exit
`1`. `--report-url` and `--bundle-url` add stable links to JUnit properties and
SARIF result properties without embedding sensitive data.

## GitHub Action

The repository exposes a composite action for Linux GitHub runners. It downloads
the selected WorldBisect release only after verifying an explicit SHA-256 or the
documented built-in digest for the official default release, captures only the
two explicitly selected workspace paths, and uploads
the Markdown/JSON/JUnit/SARIF reports, certificate, and redacted diagnostic
bundle as one artifact. It never collects kubeconfigs, credentials, or other
files outside those workspaces automatically.

```yaml
- name: Diagnose workspace difference
  uses: ClusterPilot-System/worldbisect@65e217a1e759bd35a0039d5dfcb17f8aebec01d2 # v1.1.1
  with:
    command: ./ci/check.sh
    good-workspace: fixtures/good
    bad-workspace: fixtures/bad
    version: 1.1.1
    sha256: 5725bd04acdd9bedefddf899fd1bae19f914dd2d8db3d60eae4156d0324202c6
    fail-on: proven
```

For security-critical automation, pin the action reference to a reviewed full
commit SHA and copy the SHA-256 matching the Linux runner architecture from the
release `SHA256SUMS`. The `v1` tag is a compatible-update channel and can move;
it is not equivalent to a fixed commit SHA. On pull
requests from forks, the action has no automatic access to repository secrets;
the command and workspace inputs must still be safe for untrusted source code.
The `fail-on` policy is applied only after evidence upload, so a proven result
fails the check without hiding the report.

## Verify certificate

```bash
worldbisect verify result.wbc
# To require a separately retained trust root:
worldbisect verify --public-key causal_ed25519_public.pem result.wbc
```

The command prints a JSON result and exits non-zero when verification fails. A
v2 certificate contains the proof status, model name, capture identifiers, and
SHA-256 digests of the good capture, bad capture, experiment set, and factor
set. It does not contain raw command output, workspace files, factor values, or
experiment details. The `trust` field distinguishes the embedded signing key
from a key matched with `--public-key`; an embedded key proves integrity, but
not that the key belongs to a person or organization. The `next_action` field
states what to review after a valid check. A changed claim, signature, or
evidence digest causes verification to fail. Older v1 certificates remain
readable, but report that evidence digests are unavailable and should be
reviewed against their original evidence bundle.

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
