# WorldBisect launch kit

Draft copy for maintainer review; publication status must be checked before use.
The release claims below are grounded in the documented 1.2.0 engine and CI
integration. The separate, source-built team report hub preview is available on
`main` following [PR #62](https://github.com/ClusterPilot-System/worldbisect/pull/62).
It is not part of the published 1.2.0 binary or a managed SaaS offering.

## Positioning and audience

**Your pipeline passed yesterday. Today it fails. Test what changed.**

WorldBisect keeps explicitly selected working inputs, reruns a failing check,
and tests which supported differences explain the failure. It reports what was
tested, the evidence level and what to inspect next.

Start with developers and platform/QA engineers maintaining repeatable Linux
checks in GitHub Actions. The best first use case is a small configuration,
build or test check whose required safe files can be listed explicitly.

The useful distinction is a tested explanation: restoring a selected input
must repair the failing check, and reversing that change must reproduce the
failure before the configured proof checks can earn `PROVEN`. Existing logs,
Git history and debugging tools remain useful alongside it.

## LinkedIn post draft

Your pipeline passed yesterday. Today it's red.
Same confidence. Very different morning. ☕

I’m building WorldBisect to help answer one practical question:
which change can we actually test as the cause?

For a repeatable Linux check, its GitHub Action can save the safe input files
you select after a successful run. When a later run fails, it tries those old
inputs again and tests the differences in isolated copies.

The result tells you what was found, what was tested, how strong the evidence
is and what to check next. If it cannot reproduce the pass/fail pair, it says so.

It doesn’t recreate yesterday’s entire machine. Start with a small check and
selected files that contain no secrets.

Try one controlled diagnosis, then tell me which repeatable CI check you would
want to test next:

https://github.com/ClusterPilot-System/worldbisect/blob/main/docs/first-diagnosis.md

#GitHubActions #DevOps #SoftwareTesting

## GitHub announcement draft

**Title: Yesterday green. Today red. Get a tested explanation.**

WorldBisect helps investigate a repeatable Linux check that used to pass and
now fails. The opt-in CI Action retains explicitly selected inputs from a
successful default-branch run, then compares them with a later failing run.

The workflow summary answers four questions:

- **What changed?** The supported input difference found by the analysis.
- **What was tested?** The reruns and interventions actually executed.
- **How strong is the evidence?** A bounded proof status, not a confidence guess.
- **What next?** A concrete input to inspect or an explanation of what prevented diagnosis.

A missing baseline or a non-reproducible failure produces an explicit result.
The original failed check remains failed.

[Controlled integration checks](https://github.com/ClusterPilot-System/worldbisect/blob/main/docs/integration-validation.md)
cover Node.js, GCC and the pinned ItsDangerous test suite. These are deliberately
introduced regressions, not customer incidents or measured time savings.

Start with [one controlled diagnosis](https://github.com/ClusterPilot-System/worldbisect/blob/main/docs/first-diagnosis.md),
then follow the [CI setup guide](https://github.com/ClusterPilot-System/worldbisect/blob/main/docs/ci-baselines.md).
List only safe files: baseline artifacts contain their raw contents and default
to seven days of retention. Historical packages, services and host state are
not restored.

Tell us [which check you tried and where you got stuck](https://github.com/ClusterPilot-System/worldbisect/issues/new?template=getting-started.yml).
A sanitized reproduction or a clear report of where setup failed is especially
useful. If the project helps you, a star helps other developers discover it.

## Current claims and limits

| Safe claim | Evidence and qualification |
| --- | --- |
| Open-source Linux diagnosis engine | [README](../../README.md) and [Apache 2.0 license](../../LICENSE); AMD64 native capture and ARM64 portable fallback have different capture coverage. |
| CI can reuse working inputs automatically | [CI guide](../ci-baselines.md); opt-in file selection, a compatible prior successful workflow, and reproducibility on the current runner are required. |
| Tested causes have an explicit evidence level | [Proof boundary](../proof-boundary.md); `PROVEN` applies to the selected factors and tested model, not every possible historical cause. |
| An authenticated API and dashboard exist | [API guide](../api.md); the existing daemon is self-hosted, and remote execution is disabled by default. This does not establish a hosted SaaS offering. |
| Real runtimes and an upstream test suite are exercised | [Integration evidence](../integration-validation.md); controlled regressions, with no claim of upstream endorsement or external customer adoption. |
| A team report hub preview is available from source | [Setup and boundaries](../team-hub.md), merged in [PR #62](https://github.com/ClusterPilot-System/worldbisect/pull/62). Workspace-scoped credentials and client-reported summaries; no hosted service, billing, SSO, production-readiness or service-level agreement claim. |

## Preview messaging and a useful pilot

The source-built preview message is: “Review CI diagnosis summaries from several
repositories in one workspace while checks and proof experiments run in your
CI environment.” The dashboard displays client-reported evidence and does not
independently verify the experiments or CI identity. Label it as an experimental
self-hosted preview, not a generally available SaaS launch.

For an opt-in pilot, measure time to first successful setup, baseline
availability, reproducible pass/fail pairs, useful diagnoses and diagnostic
duration. Ask whether the report changed the developer's next action. Measure
incident-resolution time before making any savings claim; do not upload private
captures or customer data to collect these metrics.

Before publishing, verify release/setup links and any referenced workflow run,
then check for an existing announcement. Link the ongoing [feedback guide](feedback.md)
or getting-started form for new reports. The [launch thread #58](https://github.com/ClusterPilot-System/worldbisect/issues/58)
is historical context and can be closed once the evergreen intake is merged.
External issues deserve a concrete
reproduction or fix relevant to that project; a product link belongs only where
it explains the evidence or helps reproduce the issue.
