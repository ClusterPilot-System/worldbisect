# WorldBisect: cause within a proof boundary

WorldBisect does not promise a longer log. It answers a narrower, verifiable
question:

> Which smallest supported set of runtime conditions is necessary and
> sufficient for this observed failure, within the declared intervention model?

That boundary is the product contract. A `PROVEN` result is earned only when
WorldBisect can capture a factor, change it in an isolated workspace, rerun the
declared oracle in both directions, and verify the result and tested
minimality. The result is machine-readable, reportable, and accompanied by the
evidence boundaries that remain outside the model.

## What the proof states mean

| State | Meaning | Safe interpretation |
| --- | --- | --- |
| `PROVEN` | The supported factor set repaired the bad world and reproduced the failure in the reverse direction, with the configured proof checks passing. | A verified cause within this model and these captures. |
| `SUPPORTED` | Executed experiments support the interpretation, but a stronger proof condition such as complete minimality or repetition was not met. | A strong lead, not final proof. |
| `CORRELATED` | Evidence and an observed factor are associated, but the causal intervention did not establish necessity and sufficiency. | A correlation to investigate. |
| `UNPROVEN` | The controlled evidence is insufficient or an experiment failed or was interrupted. | No causal claim. |

`PROVEN` never means that every possible cause was searched, that the host is
safe, or that the result generalizes outside the captured command, workspaces,
oracle, and intervention model. Unsupported observations remain visible as
evidence boundaries instead of being promoted to a guessed cause. See
[`limitations.md`](limitations.md) for the supported factor inventory and
known exclusions.

## How this differs from adjacent tools

These tools remain useful and complementary. They answer different questions:

| Tool | Primary answer | What WorldBisect adds |
| --- | --- | --- |
| `git bisect` | Which commit changed a test outcome? | It compares runtime worlds and tests supported environment/workspace interventions, even when source history is unchanged. |
| `strace` and other tracing | Which operations and observations occurred? | It uses observations to select candidates, then reruns controlled counterfactuals and verifies both causal directions. |
| Logs | What the application reported? | It preserves reports and boundaries but does not treat an assertion in a log as a causal proof. |
| AI debuggers | Which hypotheses an assistant ranks or explains? | WorldBisect does not use a model to assign `PROVEN`; only executed, bounded experiments can do that. AI suggestions remain hypotheses until independently verified. |

The distinction is not “tool versus no tool.” Logs, traces, tests, Git
history, and AI assistance can all contribute useful evidence. WorldBisect's
differentiator is the explicit, reproducible proof boundary around the causal
claim.

## Read a result responsibly

1. Check the proof state and the declared oracle.
2. Read the detected factor and the proof explanation.
3. Read every evidence boundary and limitation before changing production.
4. Re-run the original command after applying the smallest safe repair.
5. If the state is `SUPPORTED`, `CORRELATED`, or `UNPROVEN`, report it as such;
   do not rewrite it as a confirmed cause.

The checked-in examples are intentionally small so their proof boundary is
visible. Start with [`examples/README.md`](../examples/README.md), then inspect
the per-example README beside each fixture.
