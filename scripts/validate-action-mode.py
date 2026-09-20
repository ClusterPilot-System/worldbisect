#!/usr/bin/env python3
"""Validate the public Action mode before either implementation can run."""
import os
import sys


def validate(env):
    mode = env.get("INPUT_MODE", "compare")
    if mode not in ("compare", "ci"):
        raise ValueError("mode must be compare or ci")
    if not env.get("INPUT_COMMAND", "").strip():
        raise ValueError("command is required")
    files = env.get("INPUT_FILES", "").strip()
    good = env.get("INPUT_GOOD_WORKSPACE", "").strip()
    bad = env.get("INPUT_BAD_WORKSPACE", "").strip()
    if mode == "ci":
        if not files:
            raise ValueError("mode=ci requires an explicit list of safe files")
        if good or bad:
            raise ValueError("mode=ci cannot also select good-workspace or bad-workspace")
        if env.get("INPUT_REPOSITORY", "ClusterPilot-System/worldbisect") != "ClusterPilot-System/worldbisect":
            raise ValueError("mode=ci uses the official WorldBisect release repository")
        if env.get("INPUT_FAIL_ON", "never") != "never":
            raise ValueError("fail-on applies to compare mode; CI mode always preserves the check failure")
    else:
        if files:
            raise ValueError("files requires mode=ci; compare mode uses two explicit workspaces")
        if not good or not bad:
            raise ValueError("compare mode requires good-workspace and bad-workspace; use mode=ci with files for automatic baselines")
    return mode


if __name__ == "__main__":
    try:
        validate(os.environ)
    except ValueError as error:
        print(f"WorldBisect Action: {error}", file=sys.stderr)
        sys.exit(1)
