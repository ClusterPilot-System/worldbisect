#!/usr/bin/env bash
set -Eeuo pipefail

if (($# == 0)); then
  echo "usage: check-release-archives.sh ARCHIVE..." >&2
  exit 2
fi

listing=$(mktemp)
trap 'rm -f "$listing"' EXIT

for archive in "$@"; do
  if [[ ! -f "$archive" || -L "$archive" ]]; then
    echo "release archive is not a regular file: $archive" >&2
    exit 1
  fi
  : > "$listing"
  if ! tar -tzf "$archive" > "$listing"; then
    echo "unreadable or truncated release archive: $archive" >&2
    exit 1
  fi
  if grep -Eq '(^|/)\.\.(/|$)|^/' "$listing"; then
    echo "unsafe archive path: $archive" >&2
    exit 1
  fi
done
