The README’s SARIF section could include a quick local check before users configure a remote reporter:

```sh
reviewdog -f=sarif -reporter=rdjson -filter-mode=nofilter < report.sarif
```

This exposes parsed paths and messages without posting comments.

I tested reviewdog **v0.21.2 on Linux x86_64** while integrating WorldBisect. A synthetic location such as `analysis://run/123` parses successfully but is not a repository file. Against a diff changing `config.txt`, the original analysis URI yielded no retained diagnostic; mapping the diagnosis to its actual file and using `-filter-mode=file` retained one.

The default `added` filter also dropped our file-level diagnostic because its display anchor was on unchanged line 1 while the change was on line 2. This is a documentation suggestion, not a new parser-bug report.

Suggested additions:

- Check that `physicalLocation.artifactLocation.uri` resolves to the intended workspace file.
- Keep analysis/report links in the message or rule `helpUri`, rather than using them as source-file locations.
- Explain when `file` filtering suits a file-level finding; `nofilter` does not repair an invalid path.

I prepared a [small, generic README patch](https://github.com/ClusterPilot-System/worldbisect/blob/main/tests/integrations/reviewdog/upstream.patch) and [reproducible local integration evidence](https://github.com/ClusterPilot-System/worldbisect/blob/main/docs/integrations/reviewdog.md). Remote GitHub reporting was not tested.

Disclosure: I maintain WorldBisect. The investigation and wording were AI-assisted; the proposed README addition contains no product promotion.
