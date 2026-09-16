#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
OUTPUT_DIR=${WORLD_BISECT_OUTPUT_DIR:-"${RUNNER_TEMP:-/tmp}/worldbisect-action"}
ARTIFACT_DIR="$OUTPUT_DIR/artifacts"
WORK_DIR="$OUTPUT_DIR/work"
STORE="$OUTPUT_DIR/store"
mkdir -p "$ARTIFACT_DIR" "$WORK_DIR" "$STORE"

fail() {
  echo "worldbisect action: $*" >&2
  exit 1
}

require_input() {
  local name=$1 value=${2-}
  [[ -n "$value" ]] || fail "$name is required"
}

require_input INPUT_COMMAND "${INPUT_COMMAND-}"
require_input INPUT_GOOD_WORKSPACE "${INPUT_GOOD_WORKSPACE-}"
require_input INPUT_BAD_WORKSPACE "${INPUT_BAD_WORKSPACE-}"
read -r -a command_args <<< "$INPUT_COMMAND"
(( ${#command_args[@]} > 0 )) || fail 'command must contain an executable'

case "${INPUT_FAIL_ON:-never}" in
  never|proven|supported|correlated|any) ;;
  *) fail "unsupported fail-on policy: ${INPUT_FAIL_ON}" ;;
esac
[[ "${INPUT_REPETITIONS:-3}" =~ ^[1-9][0-9]*$ ]] || fail 'repetitions must be a positive integer'
[[ "${INPUT_MAX_EXPERIMENTS:-128}" =~ ^[1-9][0-9]*$ ]] || fail 'max-experiments must be a positive integer'

workspace_root=$(cd "${GITHUB_WORKSPACE:-$ROOT}" && pwd -P)
resolve_workspace() {
  local relative=$1 resolved
  case "$relative" in
    /*|..|../*|*/../*|*/.. ) fail 'workspace paths must stay relative to GITHUB_WORKSPACE' ;;
  esac
  resolved=$(cd "$workspace_root/$relative" 2>/dev/null && pwd -P) || fail "workspace does not exist: $relative"
  case "$resolved" in
    "$workspace_root"|"$workspace_root"/*) printf '%s\n' "$resolved" ;;
    *) fail 'workspace path escapes GITHUB_WORKSPACE' ;;
  esac
}

good_workspace=$(resolve_workspace "$INPUT_GOOD_WORKSPACE")
bad_workspace=$(resolve_workspace "$INPUT_BAD_WORKSPACE")

source "$ROOT/scripts/github-install.sh"

good_capture="$WORK_DIR/good.wcap"
bad_capture="$WORK_DIR/bad.wcap"
"$binary" capture --store "$STORE" --workspace "$good_workspace" --oracle "${INPUT_ORACLE:-exit=0}" --output "$good_capture" -- "${command_args[@]}" >"$WORK_DIR/good.out"
if "$binary" capture --store "$STORE" --workspace "$bad_workspace" --oracle "${INPUT_ORACLE:-exit=0}" --output "$bad_capture" -- "${command_args[@]}" >"$WORK_DIR/bad.out" 2>"$WORK_DIR/bad.err"; then
  fail 'bad-workspace command unexpectedly satisfied the oracle'
fi

certificate="$ARTIFACT_DIR/result.wbc"
json_report="$ARTIFACT_DIR/report.json"
"$binary" compare --store "$STORE" --good "$good_capture" --bad "$bad_capture" --repetitions "${INPUT_REPETITIONS:-3}" --max-experiments "${INPUT_MAX_EXPERIMENTS:-128}" --format json --certificate "$certificate" --report-url "${INPUT_REPORT_URL:-}" --bundle-url "${INPUT_BUNDLE_URL:-}" -- "${command_args[@]}" >"$json_report"
analysis_id=$(sed -n 's/.*"analysis_id": "\([^"]*\)".*/\1/p' "$json_report" | head -n 1)
[[ -n "$analysis_id" ]] || fail 'WorldBisect did not return an analysis ID'

"$binary" explain --store "$STORE" --format markdown --report-url "${INPUT_REPORT_URL:-}" --bundle-url "${INPUT_BUNDLE_URL:-}" "$analysis_id" >"$ARTIFACT_DIR/report.md"
"$binary" explain --store "$STORE" --format junit --report-url "${INPUT_REPORT_URL:-}" --bundle-url "${INPUT_BUNDLE_URL:-}" "$analysis_id" >"$ARTIFACT_DIR/report.junit.xml"
"$binary" explain --store "$STORE" --format sarif --report-url "${INPUT_REPORT_URL:-}" --bundle-url "${INPUT_BUNDLE_URL:-}" "$analysis_id" >"$ARTIFACT_DIR/report.sarif"
"$binary" handoff --store "$STORE" --analysis "$analysis_id" --preview >"$ARTIFACT_DIR/handoff-preview.json"
"$binary" handoff --store "$STORE" --analysis "$analysis_id" --output "$ARTIFACT_DIR/diagnosis.wdiag" --confirm >/dev/null

status=$(sed -n 's/.*"status": "\([^"]*\)".*/\1/p' "$json_report" | head -n 1)
[[ -n "$status" ]] || fail 'WorldBisect did not return an analysis status'
policy_exit=0
if ! "$binary" explain --store "$STORE" --format json --fail-on "${INPUT_FAIL_ON:-never}" "$analysis_id" > /dev/null; then
  policy_exit=1
fi
printf '%s\n' "$policy_exit" > "$OUTPUT_DIR/exit-code"
printf 'status=%s\n' "$status" >> "${GITHUB_OUTPUT:-/dev/null}"
printf 'analysis-id=%s\n' "$analysis_id" >> "${GITHUB_OUTPUT:-/dev/null}"
printf 'report-path=%s\n' "$ARTIFACT_DIR/report.md" >> "${GITHUB_OUTPUT:-/dev/null}"
printf 'bundle-path=%s\n' "$ARTIFACT_DIR/diagnosis.wdiag" >> "${GITHUB_OUTPUT:-/dev/null}"
printf 'junit-path=%s\n' "$ARTIFACT_DIR/report.junit.xml" >> "${GITHUB_OUTPUT:-/dev/null}"
printf 'sarif-path=%s\n' "$ARTIFACT_DIR/report.sarif" >> "${GITHUB_OUTPUT:-/dev/null}"
echo "worldbisect action: analysis complete (status=$status)"
