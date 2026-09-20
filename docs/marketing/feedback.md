# Share a useful WorldBisect result

Start with the [first diagnosis](../first-diagnosis.md), then try one repeatable
Linux check that matters to you. The most useful first feedback is simple:
**Which check did you try, and did the report change what you checked next?**

| What you need | Where to go |
| --- | --- |
| Help choosing a first check, setup feedback, or an unexpected first result | [Getting-started form](https://github.com/ClusterPilot-System/worldbisect/issues/new?template=getting-started.yml) |
| A reproducible defect | [Bug report](https://github.com/ClusterPilot-System/worldbisect/issues/new?template=bug.yml) |
| An open-ended usage or design question | [GitHub Discussions](https://github.com/ClusterPilot-System/worldbisect/discussions) |
| A real result you consent to having reviewed for an anonymized case study | [Sanitized user report](https://github.com/ClusterPilot-System/worldbisect/issues/new?template=user-report.yml) |
| A vulnerability or sensitive proof-integrity issue | [Private security report](https://github.com/ClusterPilot-System/worldbisect/security/advisories/new) |

For setup feedback, share the release version, Linux distribution/architecture,
the broad kind of check, where setup stopped, and the result status if available.
You do not need a complete diagnosis or a success story to participate.

Public issues and Discussions are public. Keep credentials, customer data,
private paths, raw captures and private CI logs out of them. Use a minimal
synthetic example when one helps explain the problem.

## Team report hub pilot

The [hub](../team-hub.md) is a source-built, experimental self-hosted preview.
Tell us which two repositories/checks you would want to review together and
whether its finding, tests, reported evidence level and next step help you act.
Names and repository URLs are optional; describe private projects generically.

Execution stays in CI, and the hub displays client-reported evidence. Review
the publisher's dry run before sending summary metadata. There is no managed
service, billing or SSO in this preview.

## How feedback becomes work

Maintainers turn actionable setup failures and reproducible defects into scoped
issues. A demo result remains demo evidence. Real case studies require consent
and verification; measured time savings require actual measurement.

This guide is the ongoing intake route. The original [launch thread #58](https://github.com/ClusterPilot-System/worldbisect/issues/58)
is historical context; new questions and reports belong in the channels above.
