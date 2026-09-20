# WorldBisect reports with reviewdog

This tested example connects a **single proven workspace-file cause** to
reviewdog's file diagnostics. It runs locally without tokens or network access
after the two binaries are installed. It does not claim that a remote PR comment
was published or that WorldBisect identifies a faulty line inside the file.

## Why the example needs a small adapter

WorldBisect's standard SARIF export uses
`worldbisect://analysis/<analysis-id>` as a stable analysis location. That is not a
repository file. With reviewdog 0.21.2, the SARIF parser accepts it, but the parsed
path points outside the working directory. A diff limited to `config.txt` then
filters the finding out.

The example [prepare_sarif.py](../../tests/integrations/reviewdog/prepare_sarif.py)
uses the matching WorldBisect JSON report to map that analysis location to its
one proven file cause. It requires matching analysis IDs, explanations and proof
fields. It refuses non-`PROVEN` outcomes, multiple causes, missing files,
symlinks, path traversal and incomplete proof metadata.

The mapped location uses **line 1 only as a display anchor**. The diagnostic says
this explicitly and retains the proof boundaries in its visible message. Use
`-filter-mode=file` for this file-level result: the default `added` filter may
drop an annotation anchored on an unchanged line. SARIF severity `error` is
preserved; it means the selected CI cause was proven within the tested model,
not that a security vulnerability was found.

This is an example bridge for reports produced by your own trusted job, not a
certificate verifier or a general converter for arbitrary SARIF documents.
Retain the original JSON/SARIF reports. Unsupported results should remain visible
in the normal WorldBisect workflow summary rather than being promoted to an
inline annotation. This example does not alter the CLI's standard export format.

## Run the reproducible check

Requirements: Linux x86_64, Python 3.9+, Go compatible with `go.mod`, `curl`,
`tar`, `sha256sum`, `git`, and ordinary `/bin/sh` and `grep` utilities. Commands
below run from the WorldBisect repository root. The reviewdog binary is pinned
to [v0.21.2](https://github.com/reviewdog/reviewdog/releases/tag/v0.21.2); the digest
was checked against its release `checksums.txt`.

```sh
demo_dir=$(mktemp -d)
go build -o "$demo_dir/worldbisect" ./cmd/worldbisect
curl --fail --location --retry 2 --max-time 60 \
  --output "$demo_dir/reviewdog.tar.gz" \
  https://github.com/reviewdog/reviewdog/releases/download/v0.21.2/reviewdog_0.21.2_Linux_x86_64.tar.gz
printf '%s  %s\n' \
  30413aa3c7443e9c3c157fe5766cad40e3bb39a32e210ee69b710a8d5c4b8e51 \
  "$demo_dir/reviewdog.tar.gz" | sha256sum --check -
tar --extract --gzip --file "$demo_dir/reviewdog.tar.gz" \
  --directory "$demo_dir" reviewdog
python3 tests/integrations/reviewdog/run.py \
  --worldbisect "$demo_dir/worldbisect" \
  --reviewdog "$demo_dir/reviewdog" \
  --output "$demo_dir/evidence"
cat "$demo_dir/evidence/evidence.json"
```

Only the download needs network access. The integration script runs the binaries
with a small explicit environment and temporary home directory; it does not pass
GitHub tokens or other ambient credentials to either executable. Capture uses the
portable path (`--trace off`). Each command is timed out, output capture is
bounded, and the successful proof has an experiment budget of 16.

The fixture changes the second line of `config.txt` from `mode=good` to
`mode=bad`. The command passes for the first version and fails for the second.
WorldBisect must prove that file cause before the adapter is exercised.

## Verified results

The [recorded evidence](../../tests/integrations/reviewdog/validated-evidence.json)
was generated on 2026-09-20 from the source revision and binary recorded there.
It is a controlled test, not a customer incident or a hosted SaaS demonstration.

| Check | Observed result |
| --- | --- |
| Successful and failing command captures | Exit 0 and exit 1 respectively |
| WorldBisect comparison | `PROVEN`, `config.txt`, 9 experiments |
| Original SARIF plus reviewdog file filter | No retained diagnostic: the analysis URI is not the changed file |
| Adapted SARIF plus default added-line filter | No retained diagnostic: the display anchor is on unchanged line 1 |
| Adapted SARIF plus file filter | Exactly one `config.txt` diagnostic, `ERROR`, `worldbisect/PROVEN` |
| Diagnostic message | Preserves the explanation, analysis ID, bounded confidence and file-level anchor notice |
| reviewdog `-fail-level=error` | Exit 1 for the proven cause |
| Real comparison with a six-experiment budget | `UNPROVEN` after six experiments; no inline annotation produced |
| Ten unsupported/mismatched inputs | All refused; CLI refusal exits 2 without emitting partial SARIF |

The script writes the original report, original SARIF, adapted SARIF, parsed
reviewdog output, an actual `UNPROVEN` report and an evidence summary. Raw captures
and their temporary store are removed after the test.

## Use with an existing diagnosis

Generate both formats from the same stored analysis, then run the bridge from the
corresponding failing checkout:

```sh
worldbisect explain --store "$store_dir" --format json "$analysis_id" > analysis.json
worldbisect explain --store "$store_dir" --format sarif "$analysis_id" > analysis.sarif
python3 /path/to/worldbisect/tests/integrations/reviewdog/prepare_sarif.py \
  --analysis analysis.json --sarif analysis.sarif --workspace "$PWD" \
  > reviewdog.sarif
reviewdog -f=sarif -reporter=rdjson -filter-mode=file \
  -diff='git diff HEAD^' < reviewdog.sarif
```

Run these as separate successful steps, or enable shell error handling before
copying them into a script. If the bridge exits 2, keep the original diagnosis and
skip reviewdog; an empty output is not a successful annotation. Choose a diff that
actually represents the change being reviewed; `HEAD^` is only an example for a
single local commit. Do not treat `nofilter` as a repair for invalid file paths.

For remote reporting, follow [reviewdog's reporter documentation](https://github.com/reviewdog/reviewdog#reporters).
That requires the appropriate CI context and credentials. This integration test
validates parsing, file filtering and exit policy locally; remote API delivery,
fork permissions and GitHub review placement remain outside its verified scope.

## Upstream documentation contribution

The upstream [contribution guide](https://github.com/reviewdog/reviewdog/blob/master/.github/CONTRIBUTING.md)
and [SARIF documentation](https://github.com/reviewdog/reviewdog#sarif-format) were
checked before preparing a small generic README addition about location inspection
and diff filters. The proposal does not advertise WorldBisect.

- [Exact README patch](../../tests/integrations/reviewdog/upstream.patch), checked against README blob `9e786b03e63c170730ddb0f63269461bd064d576`.
- [Complete submission text](../../tests/integrations/reviewdog/upstream-submission.md), including tested scope and project/AI-assistance disclosure.
- [Published issue #2821](https://github.com/reviewdog/reviewdog/issues/2821): opened by `JossefMo1` on September 20 through the authenticated GitHub web UI. The README patch is prepared and awaits maintainer feedback; no upstream PR has been published or accepted.
- [Publication status](../../tests/integrations/reviewdog/upstream-status.json) preserves the earlier GitHub connector rejection: HTTP **403**, `Resource not accessible by integration`. The later web publication does not establish external write access for that connector.

Follow the existing issue for maintainer feedback before preparing an upstream PR.
To submit the patch from an appropriately authorized fork, update that fork
from upstream, create a branch, run `git apply --check /path/to/upstream.patch`
and then `git apply /path/to/upstream.patch`. Review the README diff before opening
a PR against `reviewdog/reviewdog:master`; recheck the current contribution rules
and search for a duplicate first. Publication of the issue is not upstream
acceptance or endorsement of WorldBisect.
