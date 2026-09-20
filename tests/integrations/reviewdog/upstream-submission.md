### Documentation improvement

While adding an optional reviewdog integration to WorldBisect, I found a useful preflight step that is not shown in the README's SARIF section: inspect the parsed diagnostic paths with the local `rdjson` reporter before enabling a remote reporter.

This is a documentation suggestion, not a claim of a new parser bug. In particular, an analysis URI such as `analysis://run/123` is not a repository file location. Disabling diff filtering does not make it one.

### Reproduction and tested scope

Tested locally on Linux x86_64 with the published reviewdog v0.21.2 binary. The downloaded archive matched SHA-256 `30413aa3c7443e9c3c157fe5766cad40e3bb39a32e210ee69b710a8d5c4b8e51` from the release checksums.

Save this as `report.sarif`:

```json
{
  "version": "2.1.0",
  "runs": [{
    "tool": {"driver": {"name": "example"}},
    "results": [{
      "ruleId": "example/check",
      "level": "error",
      "message": {"text": "Example diagnosis"},
      "locations": [{
        "physicalLocation": {
          "artifactLocation": {"uri": "analysis://run/123"},
          "region": {"startLine": 1}
        }
      }]
    }]
  }]
}
```

Run:

```sh
reviewdog -f=sarif -reporter=rdjson -filter-mode=nofilter < report.sarif
```

The output makes the parsed path visible: it does not refer to a source file named `config.txt`. In the integration retest, replacing the synthetic analysis location with the deliberately selected workspace file produced the expected `config.txt` path, and `-filter-mode=file` retained it against a diff changing that file. The original analysis URI produced no retained diagnostics with that file diff.

The WorldBisect fixture exercised an actual successful/failing command pair and nine bounded experiments before reporting a single file cause. No remote GitHub reporter or PR comment was exercised in this test.

### Suggested README addition after the SARIF example

> Before enabling a remote reporter, inspect the parsed paths and messages locally:
>
> `reviewdog -f=sarif -reporter=rdjson -filter-mode=nofilter < report.sarif`
>
> For file-based reporting, check that each `physicalLocation.artifactLocation.uri` resolves to the intended file from the directory where reviewdog runs. Analysis identifiers are not source-file locations; keep report links in the message or rule `helpUri` instead.
>
> The default `-filter-mode=added` keeps diagnostics on added or modified lines. For a diagnostic applying to a whole changed file, `-filter-mode=file` can be more appropriate. `nofilter` helps inspect parsed diagnostics, but does not repair an invalid source-file location.

Disclosure: I maintain WorldBisect; this investigation and wording were AI-assisted. The proposed README text is generic and contains no project promotion. I checked the contribution guide and searched existing SARIF path/documentation issues before submitting.

