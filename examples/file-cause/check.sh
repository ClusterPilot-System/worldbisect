#!/usr/bin/env bash
set -euo pipefail

grep -qx 'mode=good' config.txt
