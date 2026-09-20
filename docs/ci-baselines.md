# Diagnose a failed Linux CI check using its last successful inputs

The opt-in **WorldBisect CI** companion Action runs a check in an isolated copy
of explicitly selected files. A successful default-branch push saves those
inputs. When a subsequent check fails, the Action retrieves a compatible
successful run and asks the existing engine to test the differences.

You no longer need to construct `good-workspace` and `bad-workspace` manually.
You do need a reproducible command and a small, complete set of safe input files.
This is best suited to configuration checks, small regression fixtures and
self-contained tests. It is not a drop-in replay of an arbitrary build machine.

## Quick start

The repository-root Action now also exposes this workflow with `mode: ci`.
Use the [complete pinned root example](examples/root-ci-workflow.yml) and see
[distribution and Marketplace status](marketplace.md). Existing root consumers
keep the explicit-workspace `compare` mode by default. The example below uses
the full commit pin for published `action-v1.0.1` and selects CI mode explicitly.

The companion Action is available from the repository revision containing
`actions/ci`. Existing `v1`/`v1.1.1` Action tags do not include this new path.
Action `action-v1.0.1` selects and checksum-verifies engine 1.2.1. Use the
reviewed full commit pin below or the [version-specific Marketplace listing](https://github.com/marketplace/actions/worldbisect-ci-diagnosis?version=action-v1.0.1);
the generic latest button currently selects the separate engine release.

```yaml
name: Configuration check
on:
  push:
    branches: [main]
  pull_request:
permissions:
  contents: read
  actions: read
jobs:
  check:
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      # Install required interpreters/tools here, before WorldBisect.
      - uses: ClusterPilot-System/worldbisect@db6b33f891779cf8e636393cf0b6afb242f6a282 # action-v1.0.1
        with:
          mode: ci
          command: '["./ci/check.sh"]'
          files: |
            ci/check.sh
            config/app.conf
          baseline-key: config-check
          retention-days: '7'
```

`command` is a JSON argument array, not a shell expression. Use a checked-in
script for pipes or multiple commands. The working directory is the root of the
staged input tree, so preserve relative paths in scripts. The command must be
safe to run repeatedly and must signal success with exit code zero.

Only the selected files exist in that tree. List scripts, configuration and
other required fixtures individually. No glob patterns or directories are
accepted. Missing selected files are represented explicitly, allowing file
addition/removal to be diagnosed without changing the selection. Up to 256
files and 16 MiB of total content are supported; modes are retained, links and
special files are rejected. File selection changes create a new baseline key.

The execution environment contains only `PATH`, `LANG` and an empty temporary
`HOME`. GitHub tokens and other workflow environment variables are not inherited.
Install tools in an earlier step; dependencies outside the selected tree remain
current-runner dependencies. This initial CI mode diagnoses file differences,
not historical environment-variable changes. Use the original Action or CLI
when you need explicitly controlled environment comparisons.

## What happens on each run

1. Read selected inputs before the command can modify them.
2. Stage those inputs in a fresh temporary directory and run the command.
3. If it passes on a default-branch push, upload a baseline with short retention.
4. If it fails, look for a prior successful default-branch **workflow run**, not
   just a successful step. Failed, cancelled, PR and current runs are excluded.
5. Download the exact matching artifact and validate its format, file digests,
   repository/workflow/job/key, platform, command, timeout, selection and source run.
6. Re-run the old and current inputs on this runner. If they still pass/fail as
   expected, perform repeated bidirectional experiments with the existing engine.
7. Publish the explanation and redacted reports, remove local raw inputs, and
   preserve the original failed check's outcome.

The lookup examines at most 300 recent successful runs of the same workflow.
Compatibility includes runner image family and architecture, but does not mean
identical installed package versions. Different matrix variants must use unique
`baseline-key` values. Use separate keys for multiple checks within one job.
Current-run artifacts with the same name are never used as a baseline.

A rerun of the same run may encounter an existing baseline artifact name; do
not overwrite it. Start a fresh push run to publish a new baseline.

## Outcomes

| Status | Meaning / next action |
| --- | --- |
| `PASSED` | Check passed; eligible trusted pushes publish the selected inputs. |
| `BASELINE_MISSING` | First run, changed configuration, expired artifact, fork PR or no compatible successful workflow. Run successfully on the default branch first. |
| `BASELINE_UNAVAILABLE` | API/download failed; check `actions: read` and artifact availability. |
| `BASELINE_INVALID` | Integrity/provenance/compatibility checks failed; rebuild the baseline. |
| `BASELINE_NOT_REPRODUCIBLE` | Old inputs no longer pass here. Check external services, dependency versions or nondeterminism. |
| `FAILURE_NOT_REPRODUCIBLE` | Current inputs passed on repetition; investigate flaky behavior. |
| `COMMAND_TIMEOUT` / `COMMAND_ERROR` | Check timed out or could not execute normally. |
| `DIAGNOSTIC_ERROR` | Diagnosis could not complete within its limits; original failure remains. |
| `PROVEN` / `SUPPORTED` / `CORRELATED` / `UNPROVEN` | Existing bounded proof states; read the report's checks and evidence boundaries. |

Outputs: `status`, `analysis-id`, `baseline-run-id`, `artifact-url`.
Every completed run publishes a job summary and a small `outcome.json`.
Completed comparisons additionally publish Markdown, JSON, JUnit, SARIF and a
signed certificate. Reports omit raw input contents and command output.
The workflow summary answers finding, executed checks, confidence and next action
without embedding the long technical report. Full reports remain in artifacts.

To publish the same compact result directly on same-repository PRs, set
`comment-pr: 'true'` and grant `pull-requests: write` to that job. The default is
`false`. One GitHub Actions bot comment is updated per check/matrix key. Closed
PRs, stale heads and fork PRs cannot publish; permission failures remain visible
without replacing the check's original outcome. Do not grant extra permissions
to untrusted code or use `pull_request_target`. Code Scanning publishing remains
available through the original root Action.

`timeout-seconds` defaults to 60 (maximum 600) for each command execution.
`diagnostic-timeout-seconds` defaults to 180 (maximum 900) for the comparison;
there are also initial and good/bad verification captures. Set an enclosing job
timeout appropriate to these limits. Comparison uses three repetitions and a
64-experiment budget. Cancellation or exhausted evidence must not become proof.

## Raw baseline data and trust

**Baseline artifacts contain the selected file contents and command arguments
without redaction.** Anyone who can read the repository's Actions artifacts may
be able to download them. Select synthetic/public configuration and safe scripts;
never list credentials, customer data, secret-bearing configs or proprietary
binaries you are not authorized to retain. Do not put secrets in command arguments.

Common credential paths, private-key material, recognizable GitHub/AWS/Slack
credential formats, symlinks, hardlinks, traversal,
special files, oversized snapshots and digest mismatches are rejected. These
checks are not a general secret scanner: a password inside `app.conf` can still
be uploaded if you select that file. Baseline artifacts and redacted diagnosis
artifacts are separate; inspect the selection before enabling the Action.

Retention defaults to seven days and can be set from 1 to 30, subject to repository
policy. Delete unwanted baseline artifacts in the Actions UI. No remote service
other than the explicitly enabled GitHub Actions artifact store is introduced.

Only default-branch push runs publish baselines, and only fully successful prior
runs are read. Same-repository PRs and manual runs can consume a trusted baseline;
fork PRs cannot consume one. `pull_request_target` is rejected. Use ephemeral
GitHub-hosted runners for untrusted PR code; staging is not a malicious-code sandbox.

Checks run without inherited environment credentials, but a command still has
the runner's operating-system permissions. It can access host files or network
services. Avoid sensitive self-hosted runners and commands with external side effects.
Historical host state, packages, network, secrets and service responses are not
restored. A `PROVEN` result applies only to the selected inputs and current tested
model, not to every cause of the historical pipeline failure.

## Try the executable demo

This repository's **CI baseline demo** workflow runs a synthetic configuration
check on PRs and default-branch pushes. After one successful main run, manually
run it with `simulate_failure=true`. It changes one configuration value, downloads
the previous successful input artifact and expects a `PROVEN` diagnosis. The
check itself remains failed; the demo's final assertion verifies that this expected
failure was correctly diagnosed. It never promotes the intentionally bad inputs.

A separate `workflow_run` verifier starts this manual regression automatically
only after a successful default-branch push demo completes. Its privileged job
checks the source workflow path, never checks out repository code, and can only
dispatch the named demo. PR, fork and manual-run completions cannot trigger it.
This exercises a real download from a previous run, not a same-run cache.

The local equivalent and security/selection/comment tests run with:

```bash
./scripts/ci-baseline-test.sh
```

## Validate usefulness with a pilot

For consenting teams, record setup time, failed checks, baseline availability,
reproducible pairs, useful diagnoses, diagnostic duration and measured time to
resolution. Compare similar incidents with and without the tool. Collect no
private captures or customer data without explicit consent. Synthetic fixtures
prove the integration works; they do not establish production time savings.
