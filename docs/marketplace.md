# Install the public WorldBisect Action

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
the current engine remains the checksum-verified **1.2.0** release.

## Complete CI example

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

## Marketplace publication status and owner steps

**The root interface is prepared for publication. This document does not claim
that a new Marketplace listing is live.** A verified listing URL and its release
must be recorded here only after successful publication.

The repository owner completes these GitHub-controlled steps:

1. Open the root `action.yml` and choose **Draft a release**. Use a new reviewed
   release; never replace an existing immutable engine release or its assets.
2. Enable **Publish this Action to the GitHub Marketplace**. If unavailable,
   the organization owner must review and accept the Marketplace Developer
   Agreement. Publishing also requires two-factor authentication.
3. Resolve any name/metadata validation errors. Select **Continuous integration**
   as the primary category and **Testing** as the secondary category if offered.
4. Verify the exact pinned baseline-success and later-failure runs, then publish.
   Record the actual listing URL and release in this document and issue #61.

GitHub automatically lists the root metadata only, not nested Actions. Release
publication alone does not prove Marketplace enrollment. Requirements:
[GitHub's Marketplace publication guide](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/publish-in-github-marketplace).
