# Examples and the proof boundary

Every example uses an executable oracle and makes the causal claim explicit:
WorldBisect can prove only the supported factor that it can change and verify
inside the captured workspaces. A successful `PROVEN` result is not a claim
about every possible host, network, kernel, or source-history cause.

| Example | Controlled factor | What a `PROVEN` result means |
| --- | --- | --- |
| [`env-cause`](env-cause/README.md) | `WORLDBISECT_EXAMPLE_MODE` | This environment value is necessary and sufficient for this oracle in the captured model. |
| [`file-cause`](file-cause/README.md) | `config.txt` content | This workspace file difference is necessary and sufficient for this oracle in the captured model. |
| [`github-actions`](github-actions/README.md) | One workspace per matrix entry | Each job gets an independent report; only its declared workspace difference can be proven. |

The examples complement logs, tracing, Git bisect, and AI assistance. Those
tools can provide observations or hypotheses, but they do not replace the
bidirectional intervention required for `PROVEN`. See the full
[`proof-boundary.md`](../docs/proof-boundary.md) contract.
