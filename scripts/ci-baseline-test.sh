#!/usr/bin/env bash
set -Eeuo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
go build -o "$tmp/worldbisect" ./cmd/worldbisect
PYTHONDONTWRITEBYTECODE=1 WORLDBISECT_TEST_BINARY="$tmp/worldbisect" \
  python3 -m unittest discover -s actions/ci -p 'test_*.py' -v
node --test actions/ci/select-baseline.test.cjs
