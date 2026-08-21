# WorldBisect

**Git bisect for runtime reality.**

WorldBisect is a local-first Linux diagnostic system that finds the smallest supported set of runtime conditions that causes a command to fail. It captures good and bad executions, compares the runtime worlds they consulted, performs isolated counterfactual experiments, and emits a machine-verifiable causal result.

```bash
worldbisect capture --output good.wcap -- ./application
worldbisect capture --output bad.wcap -- ./application
worldbisect compare --good good.wcap --bad bad.wcap -- ./application
```

For supported factors, a `PROVEN` result means the minimized factor set repairs the bad world and reproduces the failure when applied in the opposite direction. Unsupported or uncontrolled factors are reported as evidence boundaries rather than guessed causes.

## Why WorldBisect

Linux debugging tools answer important partial questions:

- logs show what an application reported;
- tracing shows operations that were attempted;
- record/replay repeats an execution;
- Git bisect finds a behavior-changing commit.

WorldBisect asks a different question:

> Which smallest supported set of runtime conditions was necessary for this observed failure?

The 1.0 causal contract is intentionally bounded. It can intervene on non-secret environment variables and regular workspace objects, including file content, file mode, presence, directories, and symbolic links. Host files, shared libraries, mounts, resources, network observations, secret values, kernel scheduling, hardware, and distributed systems can be captured as evidence but are not automatically promoted to `PROVEN` causes in 1.0.

## Status

WorldBisect 1.0 is released as a stable, maintenance-oriented package for Linux. It is not an automatic code repair agent and does not claim universal causal completeness.

Supported release platforms:

- Linux AMD64: native syscall capture with a bounded `ptrace` tracer;
- Linux ARM64: portable basic capture fallback;
- other platforms: source compatibility is not part of the 1.0 release contract.

Read [`docs/limitations.md`](docs/limitations.md) before relying on a causal result.

## Install

### Release tarball

Download the tarball and `SHA256SUMS` from the GitHub release, verify the checksum, then install:

```bash
sha256sum -c SHA256SUMS
sudo ./scripts/install.sh
```

### Debian package

```bash
sudo dpkg -i worldbisect_1.1.0_linux_amd64.deb
```

### From source

```bash
git clone https://github.com/ClusterPilot-System/worldbisect.git
cd worldbisect
make check
make build
sudo make install
```

## First causal analysis

Create two isolated workspaces with a machine-checkable oracle. The examples directory contains complete fixtures.

```bash
cp -R examples/file-cause /tmp/worldbisect-good
cp -R examples/file-cause /tmp/worldbisect-bad
cp /tmp/worldbisect-good/config.good.txt /tmp/worldbisect-good/config.txt
cp /tmp/worldbisect-bad/config.bad.txt /tmp/worldbisect-bad/config.txt

worldbisect capture \
  --store /tmp/worldbisect-store \
  --workspace /tmp/worldbisect-good \
  --oracle exit=0 \
  --output /tmp/good.wcap \
  -- ./check.sh

worldbisect capture \
  --store /tmp/worldbisect-store \
  --workspace /tmp/worldbisect-bad \
  --oracle exit=0 \
  --output /tmp/bad.wcap \
  -- ./check.sh || true

worldbisect compare \
  --store /tmp/worldbisect-store \
  --good /tmp/good.wcap \
  --bad /tmp/bad.wcap \
  -- ./check.sh
```

Expected conclusion for a normal user:

```markdown
# WorldBisect diagnosis

**Status:** `PROVEN`

The failing run was repaired by changing the detected factor, and the failure returned when that change was reversed. The factor was also minimal within the tested model.

## Confirmed or suspected cause

- `workspace:config.txt` — workspace file "config.txt" differs between the successful and failing run

## Next steps

1. Make a backup of "config.txt" before editing or replacing it.
2. Open "config.txt" in the failing workspace and compare it with the known-good copy; check the content, file presence, permissions, and link target if applicable.
3. Restore or correct "config.txt" so it matches the known-good configuration. Do not change unrelated files.
4. Run the original command again and confirm that the oracle passes.
5. If the command still fails, create fresh good and bad captures and attach this analysis ID when contacting support.
```

The text report is written for operators who need an actionable answer. It
also includes a proof explanation, evidence boundaries, and technical IDs for
support. Use `--format json` when a machine needs the stable structured
contract instead of the human-readable report.

## Try the GitHub Action in 5 minutes

The fastest way to see the causal proof is the public
[`worldbisect-demo`](https://github.com/ClusterPilot-System/worldbisect-demo)
repository. Open its **Actions** tab, run **WorldBisect demo**, and inspect the
workflow summary and `worldbisect-diagnostic` artifact. The intentionally bad
workspace differs only in `config.txt`; a successful run reports `PROVEN` and
identifies that file as the smallest tested cause.

To use the Action in your own Linux workflow, keep the two workspaces explicit
and pin the Action to a reviewed commit or immutable release:

```yaml
- name: Diagnose workspace difference
  id: worldbisect
  uses: ClusterPilot-System/worldbisect@v1
  with:
    command: ./check.sh
    good-workspace: demo/good
    bad-workspace: demo/bad
    version: 1.1.0
    fail-on: proven
```

For the official `ClusterPilot-System/worldbisect` `v1.1.0` release, the
Action selects and verifies the correct built-in digest for Linux AMD64 or
ARM64. You can still provide an explicit digest when using a custom release or
repository:

```yaml
    # Optional explicit Linux AMD64 digest for v1.1.0:
    # sha256: 74602fb5a1894eaf63ef12178fa5d9ff53b6369a9277f17021c3733f18f7d757
```

### Action trust pins

`v1` is the maintained compatibility tag for the latest compatible 1.x Action
release. It is intentionally movable and initially points to the reviewed
`v1.1.1` release commit. Use it when receiving compatible updates matters more
than pinning a single revision:

```yaml
uses: ClusterPilot-System/worldbisect@v1
```

For security-critical workflows, pin the Action to the full reviewed commit
SHA and explicitly verify the release archive that it downloads. This example
pins the `v1.1.1` Action commit and its Linux AMD64 archive:

```yaml
- uses: ClusterPilot-System/worldbisect@65e217a1e759bd35a0039d5dfcb17f8aebec01d2 # v1.1.1
  with:
    command: ./check.sh
    good-workspace: demo/good
    bad-workspace: demo/bad
    version: 1.1.1
    sha256: 5725bd04acdd9bedefddf899fd1bae19f914dd2d8db3d60eae4156d0324202c6
```

Use the matching architecture-specific SHA-256 from the release's
`SHA256SUMS`; do not copy the AMD64 digest to an ARM64 runner. After download,
verify GitHub provenance as well:

```bash
gh attestation verify worldbisect_1.1.1_linux_amd64.tar.gz \
  --repo ClusterPilot-System/worldbisect
```

GitHub Immutable Releases are enabled for future published releases. GitHub
does not retroactively lock earlier published releases, including `v1.0.0`,
`v1.1.0`, and `v1.1.1`; preserve those historical tags and use their reviewed
commit SHAs when an immutable reference is required.

`good-workspace` is the relative path to the workspace where the command is
known to pass. `bad-workspace` is the relative path to the workspace where the
same command is known to fail. `oracle` describes how WorldBisect decides
whether the command passed; the default is `exit=0`. Both workspaces must stay
inside `GITHUB_WORKSPACE`.

The Action writes the diagnosis to `$GITHUB_STEP_SUMMARY`, exposes the status
and analysis ID as outputs, and links directly to the Markdown report and
diagnostic artifact. It uploads the Markdown, JSON, JUnit, SARIF, certificate,
and redacted handoff before applying the `fail-on` policy. It does not repair
the workspace or upload raw workspace contents automatically.

For a reproducible local terminal demonstration and a recording checklist, see
[`docs/quickstart-demo.md`](docs/quickstart-demo.md).

Large or long-running analyses are bounded by explicit workspace file/byte,
output, timeout, factor, and experiment limits. Use `--progress` to receive
experiment progress on stderr without corrupting JSON stdout. Press Ctrl+C to
cancel safely; completed work remains persisted and reusable through the
experiment cache, while an interrupted run remains visibly incomplete.

The JSON output is the versioned `worldbisect.analysis-report.v1` contract.
Markdown output is also stable and can be pasted into GitHub pull requests or
support tickets. Both formats are derived from the same report model and omit
captured command output and factor values.

Create a redacted diagnostic handoff for another engineer or support:

```bash
worldbisect handoff --store /tmp/worldbisect-store \
  --analysis <analysis-id> --preview
worldbisect handoff --store /tmp/worldbisect-store \
  --analysis <analysis-id> --output diagnosis.wdiag --confirm
worldbisect import --store /tmp/receiving-store \
  --certificate-output imported.wbc diagnosis.wdiag
worldbisect explain --store /tmp/receiving-store <analysis-id>
worldbisect verify imported.wbc
```

Diagnostic bundles are deterministic and contain the analysis, both redacted
captures, signed certificate, Markdown report, JSON report, and checksummed
manifest. Raw command output, workspace content blobs, and secret-looking
values are intentionally excluded.

The handoff preview is read-only and must be reviewed before repeating the
command with `--confirm`. It reports a deterministic incident ID, redacted
fields, artifact names, and retention guidance. Keep handoff files in the
approved support channel only, and delete them after the support case closes.

`verify` performs an offline signature check and returns a machine-readable
`valid`, `trust`, and `next_action` result. Use `--public-key` when the signing
key must also match an independently retained trust root. Current v2
certificates carry hashes for the captures, experiments, and factor set rather
than copying raw evidence; changing any signed claim or digest fails closed.

For CI integrations, `compare` and `explain` also support `--format junit` and
`--format sarif`. JUnit marks `PROVEN` and `SUPPORTED` as failures and the
other statuses as explicit skips. SARIF emits an `error` for `PROVEN`, a
`warning` for `SUPPORTED` or `CORRELATED`, and a `note` for `UNPROVEN`.
Formatting alone exits with code `0`; use `--fail-on proven`, `supported`,
`correlated`, or `any` to make selected statuses exit with code `1`.

## Commands

```text
worldbisect capture     Capture a command and its bounded runtime world
worldbisect compare     Compare good and bad sessions and run interventions
worldbisect explain     Render a stored analysis
worldbisect export      Create a deterministic portable bundle
worldbisect handoff     Preview and explicitly confirm a support handoff
worldbisect import      Import a validated capture or diagnostic bundle
worldbisect verify      Verify a causal certificate
worldbisect audit       Verify the local audit chain
worldbisect doctor      Validate host capabilities and configuration
worldbisect serve       Run the authenticated API and dashboard
worldbisect version     Print version and build information
worldbisectd            Run the persistent API and job worker daemon
```

Use `worldbisect help <command>` or the manpages for details.

## Safety model

WorldBisect runs user-selected commands. Treat captures, command output, workspace files, and imported bundles as untrusted data.

- The local CLI runs commands only when explicitly requested.
- Daemon remote execution is disabled by default.
- Remote execution requires scoped bearer authentication, canonical absolute command paths, permitted working directories, and bounded quotas.
- Executables and working directories are bound to opened file descriptors to prevent path replacement between authorization and execution.
- Secret-looking environment variables are redacted and never become automatic causal factors.
- Imported archives reject absolute paths, traversal, duplicate paths, links, devices, oversized entries, and malformed manifests.
- A result is not `PROVEN` unless bidirectional intervention succeeds within the declared model.

Read [`SECURITY.md`](SECURITY.md), [`docs/security.md`](docs/security.md), and [`docs/threat-model.md`](docs/threat-model.md).

## Architecture

WorldBisect is composed of:

1. capture and observation;
2. typed runtime-world comparison;
3. bounded intervention planning;
4. isolated experiment execution;
5. deterministic minimization;
6. causal proof verification;
7. artifact, store, API, audit, and observability services.

The architecture and trust boundaries are documented in [`docs/architecture.md`](docs/architecture.md) and the ADRs under [`docs/adr`](docs/adr).

## API and dashboard

Initialize a secure local daemon configuration:

```bash
sudo worldbisectd init \
  --config /etc/worldbisect/config.json \
  --data-dir /var/lib/worldbisect
```

The raw bearer token is displayed once. Only its SHA-256 hash is stored. Start the service with systemd or:

```bash
worldbisectd run --config /etc/worldbisect/config.json
```

The REST contract is defined in [`api/openapi.yaml`](api/openapi.yaml). The dashboard is embedded in the Go binary and requires an authenticated API session.

## Development

```bash
make check
make test-race
make e2e
make coverage
```

The project intentionally has no external Go module dependencies.

## Releases and verification

Official release artifacts include:

- source archive;
- static Linux AMD64 and ARM64 tarballs;
- Debian packages;
- SPDX SBOM;
- `SHA256SUMS`;
- GitHub artifact attestation.

Full semantic release tags are published as immutable GitHub Releases. The
moving `v1` compatibility tag is not a GitHub Release and is updated only to a
reviewed compatible release commit.

Verify checksums:

```bash
sha256sum -c SHA256SUMS
```

Verify the GitHub attestation:

```bash
gh attestation verify <artifact> --repo ClusterPilot-System/worldbisect
```

## Open-source governance

WorldBisect is licensed under Apache License 2.0. Contributions are welcome for defects, security, compatibility, documentation, and bounded maintenance of the 1.x contract.

- [`CONTRIBUTING.md`](CONTRIBUTING.md)
- [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md)
- [`GOVERNANCE.md`](GOVERNANCE.md)
- [`SECURITY.md`](SECURITY.md)
- [`PROVENANCE.md`](PROVENANCE.md)

Security vulnerabilities must be reported privately through GitHub private vulnerability reporting.
