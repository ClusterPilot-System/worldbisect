# Project status and next steps

This is a maintained, evidence-linked snapshot of the public project as of
2026-11-18. A passing PR check is not a merged change, and the source-built
team hub preview is not a managed SaaS launch.

| Area | Available or verified | Next evidence needed |
| --- | --- | --- |
| Product and causal contract | [Proof boundary](proof-boundary.md) defines what `PROVEN` means and what remains an observation. | Collect a sanitized, repeatable check from a real user and record whether the diagnosis reproduces. |
| Engine, CLI and API | [Architecture](architecture.md), [API](api.md) and [first diagnosis](first-diagnosis.md) describe the released Linux 1.2.1 workflow. | Validate first-use friction with someone outside the demo; turn a reproducible obstacle into a scoped issue. |
| CI integration | [CI baseline guide](ci-baselines.md) covers selected inputs, retention and failure-to-reproduce outcomes. [PR #89](https://github.com/ClusterPilot-System/worldbisect/pull/89) proposes a stricter temporal baseline boundary; it is not merged. | Obtain independent review, then verify the merged behavior on a completed default-branch run. |
| Team hub and data | The [self-hosted preview](team-hub.md) has a separate trust boundary. [PR #83](https://github.com/ClusterPilot-System/worldbisect/pull/83) documents data lifecycle and offboarding for [issue #80](https://github.com/ClusterPilot-System/worldbisect/issues/80); it is not merged. | Review the data boundaries and gather operational evidence for the [SaaS launch gates](https://github.com/ClusterPilot-System/worldbisect/issues/63). Do not call the preview a hosted product. |
| Tests and integrations | [Integration validation](integration-validation.md) documents real-workload checks; [CI](https://github.com/ClusterPilot-System/worldbisect/actions/workflows/ci.yml) and CodeQL run on PRs. | Keep testing on supported AMD64 and native ARM64 runners, including failure and non-reproduction cases. |
| Packaging and release | The [release checklist](release-checklist.md) requires reproducible artifacts and immutable tags. [Release PR #78](https://github.com/ClusterPilot-System/worldbisect/pull/78) proposes 1.2.2 but remains unmerged. | Independent approval and all required gates before merging; then inspect published assets and update the Action installer digest in a follow-up PR. |
| Documentation and community | [First diagnosis](first-diagnosis.md), [feedback channels](../README.md#try-it-improve-it-share-a-useful-failure) and the [reviewdog integration](integrations/reviewdog.md) are public. | Ask for a concrete, sanitized CI reproduction; answer actual questions without repeating promotion in unrelated threads. |

## Working rule

Treat `planned`, `proposed in a PR`, `merged`, `released`, and `observed in a
user workflow` as different states. The linked issues and PRs are the live
source of truth for reviews and CI; this page tracks priorities, not a claim
of completed SaaS operations or user adoption.
