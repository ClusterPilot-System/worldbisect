#!/usr/bin/env bash
set -Eeuo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
store="$work/store"
good="$work/good"
bad="$work/bad"
imported="$work/imported"
mkdir -p "$good" "$bad" "$imported"

build_flags=()
if [[ "${WORLDBISECT_E2E_RACE:-0}" == "1" ]]; then
  build_flags=(-race)
fi

cat > "$good/check.sh" <<'SH'
#!/bin/sh
set -eu
grep -qx 'mode=good' config.txt
SH
chmod +x "$good/check.sh"
cp "$good/check.sh" "$bad/check.sh"
printf 'mode=good\n' > "$good/config.txt"
printf 'mode=bad\n' > "$bad/config.txt"

mkdir -p "$work/bin"
go build "${build_flags[@]}" -o "$work/bin/worldbisect" ./cmd/worldbisect
go build "${build_flags[@]}" -o "$work/bin/worldbisectd" ./cmd/worldbisectd

"$work/bin/worldbisect" capture --store "$store" --workspace "$good" --oracle exit=0 --output "$work/good.wcap" -- ./check.sh > "$work/good.out"
if "$work/bin/worldbisect" capture --store "$store" --workspace "$bad" --oracle exit=0 --output "$work/bad.wcap" -- ./check.sh > "$work/bad.out" 2> "$work/bad.err"; then
  echo "bad capture unexpectedly succeeded" >&2
  exit 1
fi

"$work/bin/worldbisect" import --store "$imported" "$work/good.wcap" >/dev/null
analysis_json=$("$work/bin/worldbisect" compare --store "$store" --good "$work/good.wcap" --bad "$work/bad.wcap" --format json --certificate "$work/result.wbc" -- ./check.sh)
printf '%s\n' "$analysis_json" > "$work/analysis.json"
python3 - "$work/analysis.json" <<'PY'
import json, sys
value=json.load(open(sys.argv[1]))
assert value["status"] == "PROVEN", value
assert value["schema_version"] == 1, value
assert value["format"] == "worldbisect.analysis-report.v1", value
assert value["proof"] == {"forward_verified": True, "reverse_verified": True, "minimal_in_model": True}, value
assert len(value["cause"]) == 1, value
assert value["cause"][0]["key"] == "config.txt", value
assert "boundaries" in value and "limitations" in value, value
PY
analysis_id=$(python3 - "$work/analysis.json" <<'PY'
import json, sys
print(json.load(open(sys.argv[1]))["analysis_id"])
PY
)
diagnostic_store="$work/diagnostic-store"
"$work/bin/worldbisect" export --store "$store" --analysis "$analysis_id" --output "$work/diagnosis.wdiag" >/dev/null
"$work/bin/worldbisect" import --store "$diagnostic_store" --certificate-output "$work/imported.wbc" "$work/diagnosis.wdiag" >/dev/null
"$work/bin/worldbisect" explain --store "$diagnostic_store" "$analysis_id" >/dev/null
"$work/bin/worldbisect" verify "$work/imported.wbc" > "$work/imported-verify.json"
python3 - "$work/imported-verify.json" <<'PY'
import json, sys
value=json.load(open(sys.argv[1]))
assert value["valid"] is True, value
PY
"$work/bin/worldbisect" explain --store "$store" --format junit --report-url https://ci.example/report --bundle-url https://ci.example/bundle "$analysis_id" > "$work/analysis.junit.xml"
"$work/bin/worldbisect" explain --store "$store" --format sarif --report-url https://ci.example/report --bundle-url https://ci.example/bundle "$analysis_id" > "$work/analysis.sarif"
if "$work/bin/worldbisect" explain --store "$store" --format sarif --fail-on proven "$analysis_id" > "$work/fail-on.sarif" 2> "$work/fail-on.err"; then
  echo "--fail-on proven did not fail for PROVEN analysis" >&2
  exit 1
fi
python3 - "$work/analysis.junit.xml" "$work/analysis.sarif" <<'PY'
import json, sys, xml.etree.ElementTree as ET
junit=ET.parse(sys.argv[1]).getroot()
assert junit.attrib["failures"] == "1", junit.attrib
sarif=json.load(open(sys.argv[2]))
assert sarif["version"] == "2.1.0", sarif
assert sarif["runs"][0]["results"][0]["ruleId"] == "worldbisect/PROVEN", sarif
assert sarif["runs"][0]["results"][0]["properties"]["report_url"] == "https://ci.example/report", sarif
PY
"$work/bin/worldbisect" verify "$work/result.wbc" > "$work/verify.json"
python3 - "$work/verify.json" <<'PY'
import json, sys
value=json.load(open(sys.argv[1]))
assert value["valid"] is True, value
PY
"$work/bin/worldbisect" audit --store "$store" > "$work/audit.json"
python3 - "$work/audit.json" <<'PY'
import json, sys
value=json.load(open(sys.argv[1]))
assert value["valid"] is True, value
PY

if [[ -n "${WORLDBISECT_E2E_ARTIFACT_DIR:-}" ]]; then
  mkdir -p "$WORLDBISECT_E2E_ARTIFACT_DIR"
  cp "$work/analysis.json" "$WORLDBISECT_E2E_ARTIFACT_DIR/analysis.json"
  cp "$work/analysis.junit.xml" "$WORLDBISECT_E2E_ARTIFACT_DIR/analysis.junit.xml"
  cp "$work/analysis.sarif" "$WORLDBISECT_E2E_ARTIFACT_DIR/analysis.sarif"
  cp "$work/diagnosis.wdiag" "$WORLDBISECT_E2E_ARTIFACT_DIR/diagnosis.wdiag"
fi

config="$work/config.json"
init_output=$("$work/bin/worldbisectd" init --config "$config" --data-dir "$work/daemon-data" --listen 127.0.0.1:0)
token=$(printf '%s\n' "$init_output" | sed -n 's/^Initial bearer token (shown once): //p')
test -n "$token"
if grep -q "$token" "$config"; then
  echo "raw token persisted" >&2
  exit 1
fi

printf 'e2e: PASS\n'
