# WorldBisect distribution plan

Research checked on **2026-09-20**. This document separates verified activity
from proposed follow-up work. GitHub reported **3 stars** on that date; no
customer adoption or future star count is inferred from it.

## Positioning and near-term outcome

**Your CI passed yesterday. Today it fails. Test what changed.**

The released product is a Linux CLI and GitHub Actions integration. The separate
[team report hub](../team-hub.md) is available from source on `main`, following
[PR #62](https://github.com/ClusterPilot-System/worldbisect/pull/62). It remains an
experimental self-hosted preview. A hosted SaaS service, paid plans and production
service guarantees are not currently available.

The [access and CI identity extension](https://github.com/ClusterPilot-System/worldbisect/pull/67)
adds scoped subjects, audit/recovery tooling and an optional OIDC verifier.
Configured CI-origin verification is distinct from proof: reported diagnoses
remain client-reported, and ordinary token uploads do not verify a GitHub origin.
The [live GitHub verification run](https://github.com/ClusterPilot-System/worldbisect/actions/runs/35529451078)
passed the real identity upload, stored-report check and restart replay rejection
in an isolated test hub. This is evidence for the narrow configured identity
flow, not a public deployment or independent proof verification.
Use the [reviewed About settings and development announcement](launch-kit.md#reviewed-repository-about-recommendation)
for the next own-repository discovery update.

A useful next adoption target is **five voluntary pilot users** who have a repeatable Linux
check and can deliberately select non-secret input files. A pilot should try a
successful baseline followed by a failure and report whether the diagnosis helps.
Five is a target, not an existing customer count. Stars are a secondary discovery
signal; do not promise 100 stars or trade contributions for them.

## Three relevant community channels

| Channel | Useful contribution | Rules and next action |
| --- | --- | --- |
| [nektos/act](https://github.com/nektos/act) | Small, executed regression cases for CI failure handling. Our existing step-level `continue-on-error` retest is a relevant starting point. | The [contribution guide](https://github.com/nektos/act/blob/master/CONTRIBUTING.md) asks contributors to search existing issues and supply reproduction details. Usage questions belong in Discussions; code PRs target `master`, user-guide changes go to `nektos/act-docs`. Follow up on #900 only when there is a maintainer question or new evidence. Do not file a second report for the same behavior. |
| [reviewdog](https://github.com/reviewdog/reviewdog) | The [executed integration](../integrations/reviewdog.md) maps one proven file cause to a useful reviewdog diagnostic. Raw analysis URIs are not source paths; the bounded adapter preserves the proof limits and refuses inconclusive results. | Verified locally with reviewdog 0.21.2: nine-experiment proof, one file diagnostic with `-filter-mode=file`, and error exit policy. Remote PR delivery is not verified. The [contribution guide](https://github.com/reviewdog/reviewdog/blob/master/.github/CONTRIBUTING.md) was checked. [Documentation issue #2821](https://github.com/reviewdog/reviewdog/issues/2821) is published; the [generic README patch](../integrations/reviewdog.md#upstream-documentation-contribution) awaits maintainer feedback. No upstream PR has been published or accepted. Respond to substantive feedback; do not portray this documentation finding as a vulnerability. |
| [Show HN](https://news.ycombinator.com/shownew) | Jo can personally present an executable example and discuss the trade-offs of intervention-based diagnosis. | [Show HN rules](https://news.ycombinator.com/showhn.html) require something people can try, preferably without signup, and the maker's availability for discussion. [HN guidelines](https://news.ycombinator.com/newsguidelines.html) prohibit generated or AI-edited text and soliciting votes. Jo must write the submission and discussion himself; marketing agents must not publish generated copy there. A future SaaS signup page alone is unsuitable. |

The act and reviewdog repositories were not archived when checked. Reviewdog had
repository activity on September 19; act had recent issue activity. Show HN had
new submissions on the research date. Recheck discussion state and contribution
rules before any later submission.

## Existing external contributions: verified

- [actions/runner #2418, comment 5702898846](https://github.com/actions/runner/issues/2418#issuecomment-5702898846): exists under `JossefMo1`; includes public CI failure evidence and a tested dispatch-input workaround. Do not duplicate it or describe a runner patch as verified.
- [nektos/act #900, comment 5703159971](https://github.com/nektos/act/issues/900#issuecomment-5703159971): exists under `JossefMo1`; reports the v0.2.89 Linux host-execution retest and its limits. This does not validate the original macOS/Docker/Terraform setup.
- [reviewdog #2821](https://github.com/reviewdog/reviewdog/issues/2821): opened by `JossefMo1` on September 20 with measured SARIF path/filter behavior, a generic README patch and project/AI-assistance disclosure. The issue is open; the patch awaits maintainer feedback. No upstream PR or endorsement is claimed.
- [WorldBisect preview update in #58](https://github.com/ClusterPilot-System/worldbisect/issues/58#issuecomment-5750580641): published by `JossefMo1` on September 20, with source setup, implementation and validation links. It asks which two repositories/checks a pilot would review together. This is an announcement, not evidence of customer adoption. The launch issue is now closed; the merged [feedback guide](feedback.md) and [getting-started form](https://github.com/ClusterPilot-System/worldbisect/issues/new?template=getting-started.yml) are the ongoing intake routes.
- [Action release announcement #68](https://github.com/ClusterPilot-System/worldbisect/discussions/68): the repository's published announcement for `action-v1.0.0`. Check this thread before posting another release announcement.

GitHub's repository API reports `has_discussions: true`. The [Discussions root](https://github.com/ClusterPilot-System/worldbisect/discussions)
is the existing route for open-ended questions; no unverified category URL or
new discussion is needed. New setup attempts have their own short issue form.

Earlier external writes returned `403 Resource not accessible by integration`.
The comments' current existence does not prove that this connection now has
general external write access. A single new attempt to create the useful reviewdog
documentation issue on September 20 returned the same HTTP 403. The user then
authenticated through GitHub's web UI, which offered the issue form, and
[issue #2821](https://github.com/reviewdog/reviewdog/issues/2821) was published
through that authorized session. The connector rejection remains recorded in
the [publication status](../../tests/integrations/reviewdog/upstream-status.json).
Only the issue is published; the prepared patch has not been submitted as an
upstream PR or accepted.

## Root Action distribution and Marketplace

The compatible root interface is available on `main` after merged [PR #65](https://github.com/ClusterPilot-System/worldbisect/pull/65)
([merge revision](https://github.com/ClusterPilot-System/worldbisect/commit/8b054544cf3a8f9febf0b316214981a17817cce8)):
`mode: ci` exposes automatic baselines while the default `compare` mode preserves
existing explicit-workspace consumers. The companion remains available in
`actions/ci`. Use a reviewed revision containing the adapter and preserve its
pinned implementation references; older release tags do not acquire the new
interface automatically.

The [WorldBisect CI Diagnosis listing](https://github.com/marketplace/actions/worldbisect-ci-diagnosis)
is published and was verified on September 20. Its immutable
[action-v1.0.0 release](https://github.com/ClusterPilot-System/worldbisect/releases/tag/action-v1.0.0)
points to `8b054544cf3a8f9febf0b316214981a17817cce8`; the latest diagnosis engine
remains version 1.2.0. [Installation and versioning](../marketplace.md) distinguish
the Action and engine releases and retain the reviewed full-commit pin.

The distribution work is recorded in completed [issue #61](https://github.com/ClusterPilot-System/worldbisect/issues/61).
For future Action releases, verify both modes and the exact successful-baseline /
later-failure pair, preserve implementation references, and check the Marketplace
version after publication. An existing listing does not by itself validate a
future release or establish customer adoption.

## Proposed follow-up milestones

Maintainers can schedule these milestones as people volunteer. This document
does not start background outreach or a recurring campaign.

| Milestone | Deliverable | Evidence of progress |
| --- | --- | --- |
| First useful result | Offer the [verified-release demo](../first-diagnosis.md), the published Marketplace Action and clear unsupported cases. Invite interested people through our own project/profile channels to try one real check. | Exact demo revision, successful public run, honest installation instructions and a public feedback route. Count only people who explicitly agree to try it. |
| First two pilots | Help volunteers select safe files, capture a baseline and interpret one failure or explicit non-reproduction result. Offer the locally verified reviewdog example when file diagnostics fit the pilot's workflow. | Two completed setup attempts, documented friction and test results. Do not count a signup as a successful diagnosis. |
| First evidenced improvement | Address the most common onboarding problem. Publish a case study only with permission and sanitized evidence. Offer the verified reviewdog example if it adds value; Jo may personally submit to Show HN. | Shipped onboarding improvement, reproducible before/after example, and replies to relevant community questions. |
| Five pilot attempts | Ask which result changed the developer's next action and whether they would use the tool again. Rank product work from observed needs. | Pilot attempts, successful baselines, useful diagnoses, explicit inconclusive cases, repeat use and volunteered feedback. Report the actual counts even when below target. |

For each pilot, track the supported use case, setup outcome, time to first useful
result and the next action the diagnosis enabled. Keep credentials, private source
files and unsanitized CI logs out of public marketing records. Publish quantitative
time-saving claims only when they were measured and the context can be stated.

## Channels intentionally deferred

- `sdras/awesome-actions` last showed a repository push in September 2024; do not
  prioritize it based on its star count alone.
- `rhysd/actionlint` had July 2026 repository activity and recent discussions about
  maintainer inactivity. Reassess responsiveness before investing in an upstream PR.
- DEV's [AI content guidelines](https://dev.to/guidelines-for-ai-assisted-articles-on-dev)
  disallow AI-assisted promotional articles and AI-generated comments. Do not use
  it for an autonomous marketing campaign.

Useful contributions must stand on their own: reproduce the problem, explain the
tested scope, disclose the project relationship when relevant and respond to
maintainer feedback. No fabricated bugs, CVEs, testimonials, paid stars or mass
promotional comments.
