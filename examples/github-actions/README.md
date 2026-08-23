# GitHub Actions monorepo example

[`monorepo.yml`](monorepo.yml) runs WorldBisect once per workspace through a
matrix. Each job publishes its own Step Summary, JUnit/SARIF reports, and
artifact; the API workspace updates the single pull-request comment to avoid
matrix races.

The matrix does not broaden the proof claim. A `PROVEN` status belongs only to
the declared good/bad workspace pair and command for that job. Logs, traces,
Git bisect, or AI suggestions may help investigate other workspaces, but they
are not substitutes for WorldBisect's bidirectional intervention proof. See
[`../../docs/proof-boundary.md`](../../docs/proof-boundary.md).
