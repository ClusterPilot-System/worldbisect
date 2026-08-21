#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
binary="$tmp/worldbisect"
# The race build deliberately disables the native ptrace tracer. This keeps the
# local WSL harness deterministic; GitHub-hosted Linux runners exercise native
# tracing through the existing end-to-end and release checks.
go build -race -o "$binary" ./cmd/worldbisect

mkdir -p "$tmp/good" "$tmp/bad"
printf 'mode=good\n' > "$tmp/good/config.txt"
printf 'mode=bad\n' > "$tmp/bad/config.txt"
cat > "$tmp/check.go" <<'EOF'
package main

import (
	"os"
	"strings"
)

func main() {
	content, err := os.ReadFile("config.txt")
	if err != nil || strings.TrimSpace(string(content)) != "mode=good" {
		os.Exit(1)
	}
}
EOF
GO111MODULE=off go build -o "$tmp/check" "$tmp/check.go"

run_action() {
  local fail_policy=$1 output_dir=$2
  mkdir -p "$output_dir"
  GITHUB_WORKSPACE="$tmp" \
  GITHUB_OUTPUT="$output_dir/outputs" \
  WORLD_BISECT_OUTPUT_DIR="$output_dir" \
  WORLDBISECT_BINARY="$binary" \
  INPUT_COMMAND="$tmp/check" \
  INPUT_GOOD_WORKSPACE=good \
  INPUT_BAD_WORKSPACE=bad \
  INPUT_SHA256=0000000000000000000000000000000000000000000000000000000000000000 \
  INPUT_ORACLE=exit=0 \
  INPUT_REPETITIONS=1 \
  INPUT_MAX_EXPERIMENTS=8 \
  INPUT_FAIL_ON="$fail_policy" \
  bash scripts/github-action.sh
}

run_action never "$tmp/pass"
test -s "$tmp/pass/artifacts/report.md"
test -s "$tmp/pass/artifacts/report.json"
test -s "$tmp/pass/artifacts/report.junit.xml"
test -s "$tmp/pass/artifacts/report.sarif"
test -s "$tmp/pass/artifacts/diagnosis.wdiag"
test -s "$tmp/pass/artifacts/result.wbc"
grep -q '^status=PROVEN$' "$tmp/pass/outputs"
grep -qx '0' "$tmp/pass/exit-code"

run_action proven "$tmp/fail"
test -s "$tmp/fail/artifacts/report.md"
grep -qx '1' "$tmp/fail/exit-code"
if grep -R -E 'TOKEN|kubeconfig|mode=bad' "$tmp/fail/artifacts"; then
  echo 'action artifacts contain unsafe fixture material' >&2
  exit 1
fi

printf 'github action test: PASS\n'
