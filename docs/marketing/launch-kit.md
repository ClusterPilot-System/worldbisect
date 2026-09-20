# WorldBisect launch kit

Draft copy for maintainer review; publication status must be checked before use.
Engine [1.2.1 is published](https://github.com/ClusterPilot-System/worldbisect/releases/tag/v1.2.1).
The [updated demo passed](../first-diagnosis.md) with the downloaded 1.2.1
AMD64 binary: `PROVEN`, `config.txt`, nine experiments. The published Action
`action-v1.0.1` also passed a [real baseline-to-failure run](../integration-validation.md#verify-actual-github-artifact-reuse).
The [publication record](../marketplace.md) contains the exact revision and
version-specific Marketplace link.
The separate, source-built team report hub preview is available on
`main` following [PR #62](https://github.com/ClusterPilot-System/worldbisect/pull/62),
with access, audit, CI identity and recovery work in [PR #67](https://github.com/ClusterPilot-System/worldbisect/pull/67).
The 1.2.1 binary packages contain the CLI and diagnostic daemon. The hub still
requires a source build and is not a managed SaaS offering.
The root Action is separately published as [action-v1.0.1](https://github.com/ClusterPilot-System/worldbisect/releases/tag/action-v1.0.1)
in [GitHub Marketplace](https://github.com/marketplace/actions/worldbisect-ci-diagnosis?version=action-v1.0.1).
The original [Action announcement #68](https://github.com/ClusterPilot-System/worldbisect/discussions/68)
now has a published [1.0.1 release update](https://github.com/ClusterPilot-System/worldbisect/discussions/68#discussioncomment-18530792).
Check that existing update before posting more announcement copy.

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

The 1.2.1 demo and immutable Action `action-v1.0.1` are published and verified.
This remains draft announcement text; check the existing thread before posting.
Existing 1.2.0 and Action 1.0.0 releases remain unchanged.

**Title: Yesterday green. Today red. Try a failure you can actually investigate.**

WorldBisect now has a three-command demo using its checksum-verified 1.2.1
release. It runs real proof checks against a deliberately changed configuration
file. No account, Go installation or root access needed.

[Try your first diagnosis](https://github.com/ClusterPilot-System/worldbisect/blob/main/docs/first-diagnosis.md).
For CI, the root Action is now on [GitHub Marketplace](https://github.com/marketplace/actions/worldbisect-ci-diagnosis?version=action-v1.0.1),
with automatic baselines available through `mode: ci`. Action `action-v1.0.1`
selects engine 1.2.1; the Action and engine have separate release versions.

Using reviewdog? Our [tested integration](https://github.com/ClusterPilot-System/worldbisect/blob/main/docs/integrations/reviewdog.md)
turns a single proven file cause into a file-level diagnostic. It preserves the
evidence boundary; its line anchor does not claim the faulty line.

Teams can also try the separate, source-built [report hub preview](https://github.com/ClusterPilot-System/worldbisect/blob/main/docs/team-hub.md).
Execution stays in CI, and diagnosis confidence remains client-reported.

Which repeatable check would you try? [Share where you got stuck](https://github.com/ClusterPilot-System/worldbisect/issues/new?template=getting-started.yml).
If it helps, a star helps others discover it.

## Reviewed repository About recommendation

These settings were saved and verified through GitHub's repository API on
September 20, 2026. The homepage points at the executable first result, with
topics covering the tested CI and reviewdog integrations:

```json
{
  "description": "Your CI passed yesterday. Today it fails. Test selected input changes, reproduce the failure, and get a useful next step.",
  "homepage": "https://github.com/ClusterPilot-System/worldbisect/blob/main/docs/first-diagnosis.md",
  "topics": [
    "github-actions", "debugging", "linux",
    "go", "diagnostics", "causal-analysis", "causal-inference", "sarif", "reviewdog"
  ]
}
```

## Current claims and limits

| Safe claim | Evidence and qualification |
| --- | --- |
| Open-source Linux diagnosis engine | [README](../../README.md) and [Apache 2.0 license](../../LICENSE); AMD64 native capture and ARM64 portable fallback have different capture coverage. |
| CI can reuse working inputs automatically | [CI guide](../ci-baselines.md); opt-in file selection, a compatible prior successful workflow, and reproducibility on the current runner are required. |
| The root Action is published in GitHub Marketplace | [Verified version-specific listing](https://github.com/marketplace/actions/worldbisect-ci-diagnosis?version=action-v1.0.1), immutable [action-v1.0.1](https://github.com/ClusterPilot-System/worldbisect/releases/tag/action-v1.0.1) at `db6b33f891779cf8e636393cf0b6afb242f6a282`, downloading engine 1.2.1. Explicit `mode: ci` preserves default `compare` compatibility; [cross-run proof](../integration-validation.md#verify-actual-github-artifact-reuse) preserves the failed check. The generic Marketplace latest button selects the engine release; share the version-specific link. |
| Tested causes have an explicit evidence level | [Proof boundary](../proof-boundary.md); `PROVEN` applies to the selected factors and tested model, not every possible historical cause. |
| An authenticated API and dashboard exist | [API guide](../api.md); the existing daemon is self-hosted, and remote execution is disabled by default. This does not establish a hosted SaaS offering. |
| Real runtimes and an upstream test suite are exercised | [Integration evidence](../integration-validation.md); controlled regressions, with no claim of upstream endorsement or external customer adoption. |
| A team report hub preview is available from source | [Setup and boundaries](../team-hub.md), [access](../hub-access.md) and [recovery](../hub-operations.md). Scoped identities, audit and recovery tooling; no hosted service, billing, SSO, production-readiness or service-level agreement claim. |
| Optional CI identity verification has a separate trust boundary | [OIDC contract](../hub-ci-identity.md) and successful [live GitHub verification run at the Action release revision](https://github.com/ClusterPilot-System/worldbisect/actions/runs/35532408886). The isolated test verified the real publisher identity, stored report and replay rejection after restart. Ordinary opaque-token uploads do not receive verified CI identity; proof remains client-reported. |

## Preview messaging and a useful pilot

The source-built preview message is: “Review CI diagnosis summaries from several
repositories in one workspace while checks and proof experiments run in your
CI environment.” The dashboard displays client-reported evidence and does not
independently verify the experiments or causal proof. Opt-in OIDC verification
authenticates a configured CI publisher's origin; it does not make the submitted
diagnosis true. Ordinary opaque-token uploads do not verify CI origin. The
[live GitHub verification](https://github.com/ClusterPilot-System/worldbisect/actions/runs/35529451078)
passed in an isolated test hub; it does not establish a public deployment. Label the hub
as an experimental self-hosted preview, not a generally available SaaS launch.

For an opt-in pilot, measure time to first successful setup, baseline
availability, reproducible pass/fail pairs, useful diagnoses and diagnostic
duration. Ask whether the report changed the developer's next action. Measure
incident-resolution time before making any savings claim; do not upload private
captures or customer data to collect these metrics.

Before publishing, verify release/setup links and any referenced workflow run,
then check for an existing announcement. Link the ongoing [feedback guide](feedback.md)
or getting-started form for new reports. The [launch thread #58](https://github.com/ClusterPilot-System/worldbisect/issues/58)
is closed historical context; the evergreen intake is already merged.
External issues deserve a concrete
reproduction or fix relevant to that project; a product link belongs only where
it explains the evidence or helps reproduce the issue.
