#!/usr/bin/env bash
set -Eeuo pipefail

if [[ $# -ne 1 || "$1" != */* ]]; then
  echo "usage: $0 OWNER/REPOSITORY" >&2
  exit 2
fi
new=$1
old=$(sed -n 's/^module //p' go.mod)
if [[ -z "$old" ]]; then
  echo "go.mod module not found" >&2
  exit 1
fi

python3 - "$old" "github.com/$new" <<'PY'
import pathlib, sys
old, new = sys.argv[1:]
for path in pathlib.Path('.').rglob('*'):
    if not path.is_file() or '.git' in path.parts or 'dist' in path.parts:
        continue
    try:
        text = path.read_text()
    except UnicodeDecodeError:
        continue
    updated = text.replace(old, new)
    if updated != text:
        path.write_text(updated)
PY

printf 'Repository identity changed from %s to github.com/%s\n' "$old" "$new"
printf 'Run make check before committing.\n'
