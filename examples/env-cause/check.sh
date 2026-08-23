#!/usr/bin/env bash
set -euo pipefail

# This oracle makes the supported proof boundary explicit: only the selected
# environment value is intended to vary between the captured worlds.
[[ "${WORLDBISECT_EXAMPLE_MODE:-bad}" == "good" ]]
