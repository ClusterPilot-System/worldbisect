#!/usr/bin/env sh
set -eu
# The script deliberately reads its configuration relative to the selected root.
grep -qx 'feature=enabled' examples/ci-baseline/config.txt
