# Install the public WorldBisect Action

The Action is published as [WorldBisect CI Diagnosis on GitHub Marketplace](https://github.com/marketplace/actions/worldbisect-ci-diagnosis?version=action-v1.0.1).
The published immutable [action-v1.0.1 release](https://github.com/ClusterPilot-System/worldbisect/releases/tag/action-v1.0.1)
points to `db6b33f891779cf8e636393cf0b6afb242f6a282`. It downloads and verifies
the separate [1.2.1 engine release](https://github.com/ClusterPilot-System/worldbisect/releases/tag/v1.2.1).
The [local demo](first-diagnosis.md) and [real cross-run diagnosis](integration-validation.md#verify-actual-github-artifact-reuse)
are verified.

Use the version-specific Marketplace link above: its button says
**Use action-v1.0.1** and copies `@action-v1.0.1`. The generic listing's
**Use latest version** currently follows the repository's latest engine release,
`v1.2.1`, rather than the separate Action release. The examples here use the
full Action commit SHA so that selection is explicit.

The repository-root Action provides two explicit modes:

| Mode | Input contract | Result |
| --- | --- | --- |
| `ci` | JSON command array and an explicit list of safe files | Save a successful default-branch baseline; diagnose a later failure |
| `compare` (default) | Existing command string and good/bad workspace paths | Compare two caller-prepared workspaces with the existing failure policy |

Existing root-Action consumers retain `compare` behavior. The companion path
`actions/ci` continues to work. Selecting `files` without `mode: ci`, or mixing
files with workspace paths, fails before running a command. CI mode always
preserves the original failed check. The `fail-on` option belongs to compare mode.

The root adapter calls both implementations using full immutable commit pins.
It does not rely on `uses: ./actions/ci`, which would resolve inside a consumer's
checkout rather than the downloaded Action. Maintainers update both pins only
after reviewing and testing the implementation revision. Existing release tags
must never move. The Action revision and downloaded engine version are separate:
`action-v1.0.1` selects the checksum-verified **1.2.1** archives.
An engine release alone does not update an existing Action's download pins.

The earlier implementation revisions are retained on the archival branch
`action-implementations/root-ci-v1`, because this repository requires squash
merges. The 1.2.1 implementation is retained as
[`1ef4595f36e5153a1f4c072b6d115e4104d00dba`](https://github.com/ClusterPilot-System/worldbisect/commit/1ef4595f36e5153a1f4c072b6d115e4104d00dba)
on `action-implementations/engine-1.2.1`. Keep both histories: the root adapter
uses full commit SHAs from them. The consumer examples also pin the full commit
SHA of the published root Action release.

## Complete CI example

Pin the published root Action commit and select CI mode:

```yaml
- uses: ClusterPilot-System/worldbisect@db6b33f891779cf8e636393cf0b6afb242f6a282 # action-v1.0.1
  with:
    mode: ci
    command: '["./examples/ci-baseline/check.sh"]'
    files: |
      examples/ci-baseline/check.sh
      examples/ci-baseline/config.txt
```

Use the pinned workflow in [the root Action example](examples/root-ci-workflow.yml).
Its two explicitly selected inputs are the existing
[`check.sh`](../examples/ci-baseline/check.sh) and
[`config.txt`](../examples/ci-baseline/config.txt) fixture. Copy those to the same
paths in a test repository. A successful push on `main` creates the baseline;
changing `feature=enabled` to `feature=disabled` produces a controlled failure.
The original check remains failed even if diagnosis succeeds.

Use a separate baseline key for each command/matrix variant. The retained input
files are raw contents, not redacted; never select credentials or private inputs
that must not be stored as workflow artifacts. See [the CI guide](ci-baselines.md)
for compatibility, retention, fork and reproduction boundaries.

## Publication record and future releases

The initial Marketplace listing and [action-v1.0.0 release](https://github.com/ClusterPilot-System/worldbisect/releases/tag/action-v1.0.0)
were verified on **2026-09-20**. That immutable release remains at
`8b054544cf3a8f9febf0b316214981a17817cce8` and defaults to engine 1.2.0.
Its [announcement #68](https://github.com/ClusterPilot-System/worldbisect/discussions/68)
is historical distribution context. Existing tags and assets remain unchanged.
The 1.2.1 binary archives still package only the CLI and diagnostic daemon;
the experimental team hub requires a source build.

Engine 1.2.1 was published on **2026-09-20** after the
[release workflow passed](https://github.com/ClusterPilot-System/worldbisect/actions/runs/35531490889).
Independent downloads matched all six published asset digests; the source
archive and its 239-file SBOM manifest matched. The downloaded AMD64 engine
passed the [first-diagnosis demo](first-diagnosis.md).

Action `action-v1.0.1` was published as an immutable release on **2026-09-20**
at `db6b33f891779cf8e636393cf0b6afb242f6a282`. The version-specific Marketplace
button and copied Action tag were checked; the repository's latest engine
release remains `v1.2.1`. At that exact Action revision,
[baseline run 35532408932](https://github.com/ClusterPilot-System/worldbisect/actions/runs/35532408932)
was consumed by [controlled regression 35532434501](https://github.com/ClusterPilot-System/worldbisect/actions/runs/35532434501).
The engine download was verified, the prior baseline was found, and diagnosis
returned `PROVEN` while preserving the original step failure and exit code 1.
See [integration validation](integration-validation.md) for the complete evidence
and the [published release update](https://github.com/ClusterPilot-System/worldbisect/discussions/68#discussioncomment-18530792)
in the existing announcement thread.

For a future Action release:

1. Select a new reviewed Action revision/version; keep existing immutable tags
   and engine assets unchanged.
2. Verify both interface modes and a successful baseline followed by a later
   failed check against the exact distributed revision.
3. Publish the Action-specific release with Marketplace publication enabled.
   Review any changed GitHub agreement, authentication or metadata requirements.
4. Verify the listing's selected version, release target and pinned examples;
   update the publication record with the actual new release URL.

GitHub automatically lists the root metadata only, not nested Actions. Release
publication alone does not prove Marketplace enrollment. Requirements:
[GitHub's Marketplace publication guide](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/publish-in-github-marketplace).
