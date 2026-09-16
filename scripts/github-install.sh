#!/usr/bin/env bash
# Shared verified release installer. Source after defining fail and WORK_DIR.
binary=${WORLDBISECT_BINARY:-}
if [[ -z "$binary" ]]; then
  version=${INPUT_VERSION#v}
  [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail 'version must be a semantic version such as 1.0.0'
  case "$(uname -m)" in
    x86_64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) fail "unsupported Linux architecture: $(uname -m)" ;;
  esac
  sha256=${INPUT_SHA256:-}
  if [[ -z "$sha256" ]]; then
    if [[ "${INPUT_REPOSITORY:-ClusterPilot-System/worldbisect}" != "ClusterPilot-System/worldbisect" || ( "$version" != "1.1.0" && "$version" != "1.1.1" ) ]]; then
      fail 'sha256 is required for custom repositories and release versions'
    fi
    case "$version/$arch" in
      1.1.0/amd64) sha256=74602fb5a1894eaf63ef12178fa5d9ff53b6369a9277f17021c3733f18f7d757 ;;
      1.1.0/arm64) sha256=180dbbb140fa9026ea12eb335e30d00268f1a0ca2dda4fcc0ebaeae2d8df9b79 ;;
      1.1.1/amd64) sha256=5725bd04acdd9bedefddf899fd1bae19f914dd2d8db3d60eae4156d0324202c6 ;;
      1.1.1/arm64) sha256=64362505a4593b7e69fbb85a1e28d6d87db1fe2e74301ffc146487f1b0c655d7 ;;
    esac
    echo "worldbisect action: using built-in verified SHA-256 for v${version} Linux ${arch}" >&2
  fi
  [[ "$sha256" =~ ^[[:xdigit:]]{64}$ ]] || fail 'sha256 must be a 64-character hexadecimal digest'
  archive="worldbisect_${version}_linux_${arch}.tar.gz"
  archive_path="$WORK_DIR/$archive"
  url="https://github.com/${INPUT_REPOSITORY:-ClusterPilot-System/worldbisect}/releases/download/v${version}/${archive}"
  curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 "$url" --output "$archive_path"
  printf '%s  %s\n' "$sha256" "$archive_path" | sha256sum --check --status - || fail 'release archive SHA-256 verification failed'
  tar --extract --no-same-owner --file "$archive_path" --directory "$WORK_DIR"
  binary="$WORK_DIR/worldbisect_${version}_linux_${arch}/bin/worldbisect"
fi
[[ -x "$binary" ]] || fail "WorldBisect binary is not executable: $binary"
