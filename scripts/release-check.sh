#!/usr/bin/env bash
set -Eeuo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

printf '[1/11] formatting\n'
if [[ -n $(gofmt -l .) ]]; then
  gofmt -l .
  exit 1
fi
printf '[2/11] vet\n'
go vet ./...
printf '[3/11] tests\n'
go test ./... -count=1
printf '[4/11] race tests\n'
timeout --foreground 180s go test -race ./... -count=1
printf '[5/11] end-to-end\n'
timeout --foreground 120s ./scripts/e2e.sh
printf '[6/11] static contracts and web assets\n'
if command -v node >/dev/null 2>&1; then node --check web/app.js; fi
python3 - <<'PY'
import json, pathlib
for path in [pathlib.Path('configs/worldbisect.example.json')]:
    json.loads(path.read_text())
PY
printf '[7/11] shell syntax\n'
for script in scripts/*.sh examples/*/*.sh; do bash -n "$script"; done
printf '[8/11] package\n'
timeout --foreground 180s ./scripts/package.sh
printf '[9/11] checksums and archive safety\n'
(cd dist && sha256sum -c SHA256SUMS)
for archive in dist/*.tar.gz; do
  if tar -tzf "$archive" | grep -Eq '(^|/)\.\.(/|$)|^/'; then
    echo "unsafe archive path: $archive" >&2
    exit 1
  fi
done
printf '[10/11] binary smoke tests\n'
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
tar -xzf "dist/worldbisect_$(cat VERSION)_linux_amd64.tar.gz" -C "$tmp"
"$tmp/worldbisect_$(cat VERSION)_linux_amd64/bin/worldbisect" version >/dev/null
"$tmp/worldbisect_$(cat VERSION)_linux_amd64/bin/worldbisectd" version >/dev/null
printf '[11/11] GitHub Action\n'
timeout --foreground 120s ./scripts/github-action-test.sh
printf 'release check: PASS\n'
