# WorldBisect distribution plan

Research checked on **2026-09-20**. This is an execution plan, not a record of
external publications or a promise of adoption.

## Positioning and near-term outcome

**Your CI passed yesterday. Today it fails. Test what changed.**

The released product is a Linux CLI and GitHub Actions integration. Describe any
new team report hub as a preview until its deployment and operating guarantees
are verified. A hosted SaaS service, paid plans and production service guarantees
are not currently available.

The next 30 days target **five voluntary pilot users** who have a repeatable Linux
check and can deliberately select non-secret input files. A pilot should try a
successful baseline followed by a failure and report whether the diagnosis helps.
Five is a target, not an existing customer count. Stars are a secondary discovery
signal; do not promise 100 stars or trade contributions for them.

## Three relevant community channels

| Channel | Useful contribution | Rules and next action |
| --- | --- | --- |
| [nektos/act](https://github.com/nektos/act) | Small, executed regression cases for CI failure handling. Our existing step-level `continue-on-error` retest is a relevant starting point. | The [contribution guide](https://github.com/nektos/act/blob/master/CONTRIBUTING.md) asks contributors to search existing issues and supply reproduction details. Usage questions belong in Discussions; code PRs target `master`, user-guide changes go to `nektos/act-docs`. Follow up on #900 only when there is a maintainer question or new evidence. Do not file a second report for the same behavior. |
| [reviewdog](https://github.com/reviewdog/reviewdog) | Test whether WorldBisect's SARIF output can produce useful PR diagnostics through reviewdog, including missing locations and inconclusive results. | Its [README](https://github.com/reviewdog/reviewdog#sarif-format) documents SARIF 2.1.0 input. Its [contribution guide](https://github.com/reviewdog/reviewdog/blob/master/.github/CONTRIBUTING.md) leaves general contribution rules unspecified. Build and verify the example in WorldBisect first, then offer a narrowly useful documentation contribution. Compatibility is not yet verified. Do not force causal diagnostics into security-vulnerability claims. |
| [Show HN](https://news.ycombinator.com/shownew) | Jo can personally present an executable example and discuss the trade-offs of intervention-based diagnosis. | [Show HN rules](https://news.ycombinator.com/showhn.html) require something people can try, preferably without signup, and the maker's availability for discussion. [HN guidelines](https://news.ycombinator.com/newsguidelines.html) prohibit generated or AI-edited text and soliciting votes. Jo must write the submission and discussion himself; marketing agents must not publish generated copy there. A future SaaS signup page alone is unsuitable. |

The act and reviewdog repositories were not archived when checked. Reviewdog had
repository activity on September 19; act had recent issue activity. Show HN had
new submissions on the research date. Recheck discussion state and contribution
rules before any later submission.

## Existing external contributions: verified

- [actions/runner #2418, comment 5702898846](https://github.com/actions/runner/issues/2418#issuecomment-5702898846): exists under `JossefMo1`; includes public CI failure evidence and a tested dispatch-input workaround. Do not duplicate it or describe a runner patch as verified.
- [nektos/act #900, comment 5703159971](https://github.com/nektos/act/issues/900#issuecomment-5703159971): exists under `JossefMo1`; reports the v0.2.89 Linux host-execution retest and its limits. This does not validate the original macOS/Docker/Terraform setup.
- [WorldBisect feedback issue #58](https://github.com/ClusterPilot-System/worldbisect/issues/58): no comments when checked. It is an intake channel, not evidence of customer adoption.

Earlier external writes returned `403 Resource not accessible by integration`.
The comments' current existence does not prove that this connection now has
general external write access. No external write was attempted for this plan.

## Marketplace discovery gap

The automatic-baseline Action is in `actions/ci`; the root `action.yml` still
requires explicit good and bad workspaces. GitHub automatically lists only the
root Action metadata file, so a Marketplace listing does not automatically expose
the companion Action. See [GitHub's publication requirements](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/publish-in-github-marketplace).

Track the bounded work in [issue #61](https://github.com/ClusterPilot-System/worldbisect/issues/61):

1. Choose a separately released companion Action repository or a deliberately
   compatible root interface; preserve existing consumers.
2. Verify a successful baseline and a later diagnosis using the exact distributed
   version, with the current trust, retention and secret-handling boundaries.
3. Prepare accurate metadata and a pinned full-workflow example. The repository
   owner must complete any outstanding Marketplace Developer Agreement and
   two-factor-authentication requirements before publication.
4. Record an actual published listing URL only after publication succeeds.

Marketplace discovery is a better direct promotion opportunity than mentioning
WorldBisect in unrelated bug reports. This plan does not assert that a new
Marketplace listing has been published.

## 30-day execution plan

| Period | Deliverable | Evidence of progress |
| --- | --- | --- |
| Days 1–7 | Prepare one pinned, runnable CI example, a short demonstration and a clear explanation of unsupported cases. Resolve the Marketplace distribution design. Invite interested people through our own project/profile channels to try one real check. | Exact demo revision, successful public run, honest installation instructions and a public feedback route. Count only people who explicitly agree to try it. |
| Days 8–14 | Help the first two pilots select safe files, capture a baseline and interpret one failure or explicit non-reproduction result. Investigate a reviewdog integration locally. | Two completed setup attempts, documented friction and test results. Do not count a signup as a successful diagnosis. |
| Days 15–21 | Address the most common onboarding problem. Publish a case study only with permission and sanitized evidence. Offer the verified reviewdog example if it adds value; Jo may personally submit to Show HN. | Shipped onboarding improvement, reproducible before/after example, and replies to relevant community questions. |
| Days 22–30 | Aim to complete five pilot attempts. Ask which result changed the developer's next action and whether they would use the tool again. Rank product work from observed needs. | Pilot attempts, successful baselines, useful diagnoses, explicit inconclusive cases, repeat use and volunteered feedback. Report the actual counts even when below target. |

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
