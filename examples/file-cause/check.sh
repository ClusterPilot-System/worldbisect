#!/usr/bin/env bash
set -euo pipefail

# This oracle makes the supported proof boundary explicit: config.txt is the
# workspace factor that the comparison is expected to test.
grep -qx 'mode=good' config.txt
