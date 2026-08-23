# Community

WorldBisect is an open-source, maintenance-oriented project. Community spaces
are for sharing reproducible, sanitized engineering experience; they are not a
place to publish secrets, private captures, customer data, or unsupported
promises.

## GitHub Discussions

[Discussions](https://github.com/ClusterPilot-System/worldbisect/discussions)
are enabled for design questions, usage questions, release feedback, and
community ideas that are not yet actionable issues. Use the categories as
follows:

- **Q&A** — supported usage questions and troubleshooting that contains no
  sensitive evidence;
- **Ideas** — bounded proposals that may become a public issue after scope and
  proof/security impact are understood;
- **Announcements** — release and monthly maintenance notes;
- **Show and tell** — consented, anonymized real-world reports and integrations.

Open a public issue for a reproducible defect, compatibility problem, or
documentation correction. Use [private vulnerability reporting](https://github.com/ClusterPilot-System/worldbisect/security/advisories/new)
for security issues or sensitive proof-integrity failures. Maintainers may
move, close, or redact a thread when it contains private data or duplicates an
existing contract.

## Monthly release and maintenance notes

Maintainers publish one note per month under
[`docs/maintenance/`](maintenance/). Each note records released versions,
security or compatibility maintenance, CI/release health, documentation and
good-first-issue focus, and the next month's bounded priorities. A note is a
status record, not a promise of new features; the 1.x boundary in
[`ROADMAP.md`](../ROADMAP.md) remains authoritative.

## Anonymized user reports

Real user reports are welcome when the reporter explicitly consents to public
publication. Use the
[`User report` issue form](../.github/ISSUE_TEMPLATE/user-report.yml) to start
the review; sensitive incidents must use a private channel first. A maintainer
publishes only a derived case study after verifying the source privately and
removing or generalizing:

- organization, person, host, repository, cloud, and ticket identifiers;
- credentials, tokens, private paths, command arguments, workspace contents,
  raw logs, and timestamps that could identify a customer;
- unsupported claims, speculative causes, and details outside the supported
  Linux contract.

Published reports retain only the useful technical shape: release version,
Linux distribution and architecture, sanitized symptom, evidence level,
bounded factor class, and the safe next action. They are marked as
**consented** and **source-verified** without publishing the source evidence.
The project never turns the public demo fixture into a purported user case.

No real-world case study has been published yet; the intake and review path is
deliberately in place so the first report can be handled without fabricating
customer evidence.
