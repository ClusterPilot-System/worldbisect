#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

usage() {
  cat >&2 <<'EOF'
Usage: scripts/release-e2e.sh [--archive PATH --checksum-file PATH] [--version VERSION]

Without --archive, download the selected official GitHub release. A checksum
file is mandatory in both modes; the archive is never executed before its
SHA-256 is verified.
EOF
}

archive=""
checksum_file=""
version="${WORLDBISECT_RELEASE_VERSION:-}"
repository="${WORLDBISECT_RELEASE_REPOSITORY:-ClusterPilot-System/worldbisect}"
while (($# > 0)); do
  case "$1" in
    --archive) (($# >= 2)) || { usage; exit 2; }; archive=$2; shift 2 ;;
    --checksum-file) (($# >= 2)) || { usage; exit 2; }; checksum_file=$2; shift 2 ;;
    --version) (($# >= 2)) || { usage; exit 2; }; version=${2#v}; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) usage; exit 2 ;;
  esac
done

if [[ -z "$archive" ]]; then
  [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'a semantic --version is required when downloading' >&2; exit 2; }
  command -v gh >/dev/null 2>&1 || { echo 'gh is required to download a release' >&2; exit 1; }
  case "$(uname -m)" in
    x86_64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) echo "unsupported release architecture: $(uname -m)" >&2; exit 1 ;;
  esac
  work=$(mktemp -d)
  trap 'rm -rf "$work"' EXIT
  archive_name="worldbisect_${version}_linux_${arch}.tar.gz"
  gh release download "v${version}" --repo "$repository" --pattern "$archive_name" --pattern SHA256SUMS --dir "$work" --clobber
  archive="$work/$archive_name"
  checksum_file="$work/SHA256SUMS"
else
  [[ -f "$archive" ]] || { echo "release archive not found: $archive" >&2; exit 1; }
  [[ -n "$checksum_file" && -f "$checksum_file" ]] || { echo '--checksum-file is required with --archive' >&2; exit 2; }
  work=$(mktemp -d)
  trap 'rm -rf "$work"' EXIT
  archive_name=$(basename "$archive")
fi

expected=$(awk -v name="$archive_name" '$2 == name || $2 == "*" name { print $1; exit }' "$checksum_file")
[[ "$expected" =~ ^[[:xdigit:]]{64}$ ]] || { echo "no SHA-256 entry for $archive_name" >&2; exit 1; }
printf '%s  %s\n' "$expected" "$archive" | sha256sum --check --status - || { echo 'release archive SHA-256 verification failed' >&2; exit 1; }

archive_listing="$work/archive-listing.txt"
tar -tzf "$archive" > "$archive_listing"
if grep -Eq '(^|/)\.\.(/|$)|^/' "$archive_listing"; then
  echo 'unsafe path in release archive' >&2
  exit 1
fi
tar -xzf "$archive" -C "$work"
root_name=$(awk -F/ 'NF > 1 { print $1; exit }' "$archive_listing")
[[ -n "$root_name" ]] || { echo 'release archive has no root directory' >&2; exit 1; }
root="$work/$root_name"
binary="$root/bin/worldbisect"
daemon_binary="$root/bin/worldbisectd"
[[ -x "$binary" && -x "$daemon_binary" ]] || { echo 'release archive is missing executable binaries' >&2; exit 1; }
if [[ -n "$version" ]]; then
  version_output=$("$binary" version)
  grep -Fq " $version (" <<<"$version_output" || { echo "unexpected binary version: $version_output" >&2; exit 1; }
fi

e2e_environment=("WORLDBISECT_E2E_BINARY=$binary" "WORLDBISECT_E2E_DAEMON_BINARY=$daemon_binary")
if [[ -n "${WORLDBISECT_E2E_TRACE:-}" ]]; then
  e2e_environment+=("WORLDBISECT_E2E_TRACE=$WORLDBISECT_E2E_TRACE")
fi
timeout --foreground "${WORLDBISECT_RELEASE_E2E_TIMEOUT:-180}s" env "${e2e_environment[@]}" "$ROOT/scripts/e2e.sh"
printf 'release e2e: PASS (%s)\n' "$archive_name"
