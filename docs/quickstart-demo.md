# WorldBisect five-minute demo

This guide produces a real `PROVEN` result from the checked-in file-cause
fixture. It is suitable for a terminal recording because every input is local,
deterministic, and safe to repeat.

## Run locally

From the repository root on Linux or WSL:

```bash
make build
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
cp -R examples/file-cause "$tmp/good"
cp -R examples/file-cause "$tmp/bad"
cp "$tmp/good/config.good.txt" "$tmp/good/config.txt"
cp "$tmp/bad/config.bad.txt" "$tmp/bad/config.txt"

./bin/worldbisect capture \
  --store "$tmp/store" \
  --workspace "$tmp/good" \
  --oracle exit=0 \
  --output "$tmp/good.wcap" \
  -- ./check.sh

./bin/worldbisect capture \
  --store "$tmp/store" \
  --workspace "$tmp/bad" \
  --oracle exit=0 \
  --output "$tmp/bad.wcap" \
  -- ./check.sh || true

./bin/worldbisect compare \
  --store "$tmp/store" \
  --good "$tmp/good.wcap" \
  --bad "$tmp/bad.wcap" \
  --format markdown \
  -- ./check.sh
```

The important output is:

```text
Status: PROVEN
workspace file "config.txt"
```

The result means that WorldBisect repaired the failing world by changing the
tested factor and reproduced the failure in the opposite direction. It is a
proof within this bounded test model, not a claim that every possible cause
outside the captured workspaces was eliminated.

## Run the public Action demo

Open the [worldbisect-demo repository](https://github.com/ClusterPilot-System/worldbisect-demo),
select **Actions**, choose **WorldBisect demo**, and click **Run workflow**.
The workflow is intentionally manual so a public repository does not consume a
runner on every push. The run summary displays the status and analysis ID; the
uploaded `worldbisect-diagnostic` artifact contains the full redacted evidence.

## Recording checklist

For a trustworthy GIF or short video, record the commands above from a clean
checkout and show these three moments in order:

1. the good and bad `config.txt` values;
2. the `compare` command and its `PROVEN` result;
3. the actionable next steps naming `config.txt`.

Do not type a fabricated result into a terminal recording. If a run returns
`SUPPORTED`, `CORRELATED`, or `UNPROVEN`, keep that output and investigate the
fixture or environment before publishing the recording.
