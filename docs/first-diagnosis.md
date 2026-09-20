# Get your first tested diagnosis

See WorldBisect identify one configuration change using its **published 1.2.0
binary**. No account, Go installation, root access or changes to your CI are
needed. This is a deliberately introduced regression, not a customer incident.

## Copy, run, inspect

Use Linux AMD64/ARM64 or WSL, with Bash, Git, curl, tar and GNU coreutils.
The download requires access to GitHub releases. From a terminal:

```bash
git clone --depth 1 https://github.com/ClusterPilot-System/worldbisect.git
cd worldbisect
bash docs/marketing/first-diagnosis.sh
```

Already have a checkout? Run the final command from the repository root.
You can [read the complete script](marketing/first-diagnosis.sh) before running
it. It downloads the release archive, verifies a pinned SHA-256 **before
execution**, copies the existing [file-cause fixture](../examples/file-cause/),
and performs real comparisons. Nothing is installed system-wide or uploaded.
Temporary captures and the downloaded binary are removed when the script exits.
Download and execution time depend on your connection and machine; the script
prints the measured time for your run.

The fixture has one difference:

```text
Working input: config.txt contains mode=good
Failing input: config.txt contains mode=bad
Check:        grep -qx 'mode=good' config.txt
```

The expected result includes:

```text
Status: PROVEN
workspace file "config.txt"
```

The full report explains the finding, executed checks, bounded confidence and
next step. In this controlled example, the next step is to correct `config.txt`
and rerun the original check. The deliberately failing capture is expected;
the script succeeds only when the comparison reports the expected proof state
and file. A different result is a finding to investigate, not something to hide.

## What you just tested

The command passes with the working input and fails with the changed one.
WorldBisect tests restoring the file in the failing workspace and applying the
opposite change to the working workspace, with repeated proof checks.
`PROVEN` applies to this command, selected factor and tested model.

This demo explicitly uses portable capture (`--trace off`) on both architectures
so it can run without native tracing permissions. It demonstrates a selected
file cause; it does not test native syscall capture or reconstruct an old host,
dependency installation, network service or secret. Read the
[proof boundary](proof-boundary.md) for the complete interpretation.

## Try one useful check next

Pick a repeatable Linux command whose safe input files you can list explicitly,
such as a configuration validation or a small regression test. Follow the
[CI baseline setup](ci-baselines.md) to let successful default-branch runs retain
those inputs automatically. CI mode requires no manual good/bad folders.

Only select files that contain no secrets or customer data: CI baselines store
their raw contents, with seven-day retention by default. Both old and current
inputs must reproduce on the current runner.

For a broader view, inspect the [controlled Node.js, GCC and upstream Python
checks](integration-validation.md). Teams can separately try the source-built
[report hub preview](team-hub.md), which displays client-reported summaries while
execution remains in CI.

**Which check are you trying to diagnose, and where did you get stuck?** Use the
[getting-started form](https://github.com/ClusterPilot-System/worldbisect/issues/new?template=getting-started.yml)
or the [feedback guide](marketing/feedback.md). A short, sanitized answer is enough.
