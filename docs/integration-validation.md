# Integration validation

These tests exercise real language runtimes and a pinned upstream project's test
suite. Each regression is deliberately introduced so its expected cause is known.
They are **controlled integration checks, not external customer incidents or
claims of measured developer time savings**.

## Executed cases

| Workload | Working state | Controlled regression | Required result |
| --- | --- | --- | --- |
| Node.js built-in test runner | Two cart-total tests in an ES module package | `package.json` changes `type` from `module` to `commonjs` | `PROVEN`, only `package.json` identified |
| GCC compilation and execution | A C11 program compiles and passes its exit-code check | `compiler.flags` changes the language standard to C89 with pedantic errors | `PROVEN`, only `compiler.flags` identified |
| Pallets ItsDangerous 2.2.0 upstream suite | **297 upstream tests pass** using the local `src` package | `pytest.ini` points `pythonpath` at a missing directory | `PROVEN`, only `pytest.ini` identified |

All three cases passed locally and on native GitHub-hosted Linux AMD64 on
2026-09-16. Each completed
comparison reported nine experiments and preserved the failed check outcome.
These timings are not a performance benchmark. The
[Real workload integrations workflow](https://github.com/ClusterPilot-System/worldbisect/actions/workflows/real-workload-integrations.yml)
runs the same checks on PRs and main and uploads `integration-results.json` with
its measured duration, status, factor, experiment count and upstream revision.
A [successful source run](https://github.com/ClusterPilot-System/worldbisect/actions/runs/35132064995)
also executes each of the four native process-tracing regressions 20 times.
Restricted local containers may lack ptrace; the native GitHub job requires it
and cannot silently fall back.

The regular CI separately validates Go unit/race tests, packaging, the companion
Action contracts and native AMD64/ARM64 behavior. The earlier published-engine
checks exercised official 1.2.0 binaries on both architectures, and the
real-workload job repeated all three diagnoses with the checksum-pinned 1.2.0
AMD64 release. Those results remain historical 1.2.0 evidence.

Engine 1.2.1 consumption is verified at the Action release revision
`db6b33f891779cf8e636393cf0b6afb242f6a282`. The
[baseline workflow](https://github.com/ClusterPilot-System/worldbisect/actions/runs/35532408932)
passed published-engine contracts on native AMD64 and ARM64. The
[real-workload run](https://github.com/ClusterPilot-System/worldbisect/actions/runs/35532408980)
passed the three diagnoses with the published engine and the reviewdog
integration; its workload artifact is `released-integration-results.json`.
[Full CI](https://github.com/ClusterPilot-System/worldbisect/actions/runs/35532408948)
also passed at that revision.

## Reproduce

Requirements: Linux, Go from `go.mod`, Git, Node.js with its built-in test runner,
GCC, Python 3.12 with venv/pip, and network access for initial dependency setup.
The diagnostic runs themselves do not use network services.

```bash
git clone https://github.com/pallets/itsdangerous.git /tmp/itsdangerous
git -C /tmp/itsdangerous checkout 096c8d42545d3b68ea21a4f890fb2b2d8979c0bd
python3 -m venv /tmp/worldbisect-integration-env
/tmp/worldbisect-integration-env/bin/pip install --require-hashes \
  -r tests/integrations/requirements.txt
go build -o /tmp/worldbisect ./cmd/worldbisect
python3 tests/integrations/real_workloads.py \
  --binary /tmp/worldbisect \
  --upstream /tmp/itsdangerous \
  --python /tmp/worldbisect-integration-env/bin/python \
  --report /tmp/worldbisect-integration-results.json
```

The harness verifies the upstream commit, copies only the selected source/tests
and license into temporary input trees, disables bytecode/cache writes, records
successful inputs, introduces one change, and invokes the production CI
orchestrator and proof engine. It asserts the exact diagnosed file, proof status
and preserved failure. No upstream source is vendored or modified in this repo.
Test dependencies are pinned with wheel hashes and are not runtime dependencies.

Upstream provenance: [Pallets ItsDangerous commit 096c8d4](https://github.com/pallets/itsdangerous/tree/096c8d42545d3b68ea21a4f890fb2b2d8979c0bd),
released as 2.2.0 under its [BSD license](https://github.com/pallets/itsdangerous/blob/096c8d42545d3b68ea21a4f890fb2b2d8979c0bd/LICENSE.txt).
Use of its tests does not imply affiliation or endorsement.

## Verify actual GitHub artifact reuse

The [CI baseline demo](https://github.com/ClusterPilot-System/worldbisect/actions/workflows/ci-baseline-demo.yml)
first runs successfully on a main push and retains its selected inputs. After the
entire workflow succeeds, **Verify completed CI baselines** dispatches a separate
regression run. That run downloads the prior workflow's artifact, changes the
synthetic configuration and requires both `PROVEN` and a failed original check.
It also requires a nonempty `baseline-run-id`. This verifies the GitHub API,
permissions, artifact format, provenance checks and download path end to end.

Historical 1.2.0 example: [successful baseline run 35132417741](https://github.com/ClusterPilot-System/worldbisect/actions/runs/35132417741)
was consumed by [regression run 35132454247](https://github.com/ClusterPilot-System/worldbisect/actions/runs/35132454247),
which asserted `status=PROVEN`, `outcome=failure`, and that exact baseline run ID.
The verification job passes precisely because it confirms the deliberate failure
and the diagnosis; the Action itself still returns the original check failure.

The current `action-v1.0.1` revision
`db6b33f891779cf8e636393cf0b6afb242f6a282` passed the same cross-run check with
checksum-verified engine 1.2.1.
[Baseline run 35532408932](https://github.com/ClusterPilot-System/worldbisect/actions/runs/35532408932)
completed successfully; [dispatcher 35532430174](https://github.com/ClusterPilot-System/worldbisect/actions/runs/35532430174)
started [controlled regression 35532434501](https://github.com/ClusterPilot-System/worldbisect/actions/runs/35532434501).
The regression found the prior baseline and reported `PROVEN`, while preserving
`outcome=failure` and the original check's exit code 1. Its verification job
succeeded because those assertions matched the deliberate failure.

The same revision passed [live GitHub OIDC verification](https://github.com/ClusterPilot-System/worldbisect/actions/runs/35532408886)
and [CodeQL](https://github.com/ClusterPilot-System/worldbisect/actions/runs/35532409024).
The two previously tracked CodeQL alerts were closed as fixed;
[issue #70](https://github.com/ClusterPilot-System/worldbisect/issues/70) records
the resolved path-binding work. These checks do not establish a hosted SaaS or
verify client-reported causal evidence in the team hub.

## What remains outside this evidence

We have not measured incident-resolution savings for external teams, replayed
historical host/package/service state, or established support for arbitrary build
pipelines. Report a [sanitized real experience](../.github/ISSUE_TEMPLATE/user-report.yml)
if you can share it. Never turn one of the deliberate regressions above into a
purported customer case study.
