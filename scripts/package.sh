#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"
VERSION=$(tr -d '\n' < VERSION)
DIST="$ROOT/dist"
SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-0}
export SOURCE_DATE_EPOCH
BUILD_DATE=$(date -u -d "@$SOURCE_DATE_EPOCH" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -r "$SOURCE_DATE_EPOCH" +%Y-%m-%dT%H:%M:%SZ)
COMMIT=$(git rev-parse --short=12 HEAD 2>/dev/null || printf source)
LDFLAGS="-s -w -X github.com/ClusterPilot-System/worldbisect/internal/version.Version=$VERSION -X github.com/ClusterPilot-System/worldbisect/internal/version.Commit=$COMMIT -X github.com/ClusterPilot-System/worldbisect/internal/version.Date=$BUILD_DATE"

rm -rf "$DIST"
mkdir -p "$DIST"

archive_tree() {
  local source=$1 output=$2
  tar --sort=name --mtime="@$SOURCE_DATE_EPOCH" --owner=0 --group=0 --numeric-owner \
      --pax-option=delete=atime,delete=ctime -C "$(dirname "$source")" -cf - "$(basename "$source")" \
    | gzip -n -9 > "$output"
}

for arch in amd64 arm64; do
  name="worldbisect_${VERSION}_linux_${arch}"
  stage="$DIST/$name"
  mkdir -p "$stage/bin" "$stage/docs/man" "$stage/configs" "$stage/packaging"
  GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "$stage/bin/worldbisect" ./cmd/worldbisect
  GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "$stage/bin/worldbisectd" ./cmd/worldbisectd
  cp LICENSE README.md CHANGELOG.md SECURITY.md "$stage/"
  cp docs/man/* "$stage/docs/man/"
  cp configs/worldbisect.example.json "$stage/configs/"
  cp packaging/systemd/worldbisectd.service "$stage/packaging/"
  cp packaging/tmpfiles.d/worldbisect.conf "$stage/packaging/worldbisect.tmpfiles.conf"
  cp packaging/sysusers.d/worldbisect.conf "$stage/packaging/worldbisect.sysusers.conf"
  archive_tree "$stage" "$DIST/$name.tar.gz"

  if command -v dpkg-deb >/dev/null 2>&1; then
    debroot="$DIST/.deb-$arch"
    mkdir -p "$debroot/DEBIAN" "$debroot/usr/bin" "$debroot/usr/sbin" \
      "$debroot/usr/share/man/man1" "$debroot/usr/share/man/man5" "$debroot/usr/share/man/man8" \
      "$debroot/usr/share/doc/worldbisect" "$debroot/lib/systemd/system" \
      "$debroot/usr/lib/tmpfiles.d" "$debroot/usr/lib/sysusers.d" "$debroot/etc/worldbisect"
    chmod 0755 "$debroot/DEBIAN"
    sed -e "s/@VERSION@/$VERSION/g" -e "s/@ARCH@/$arch/g" packaging/debian/control > "$debroot/DEBIAN/control"
    cp packaging/debian/postinst "$debroot/DEBIAN/postinst"
    chmod 0755 "$debroot/DEBIAN/postinst"
    cp "$stage/bin/worldbisect" "$debroot/usr/bin/worldbisect"
    cp "$stage/bin/worldbisectd" "$debroot/usr/sbin/worldbisectd"
    chmod 0755 "$debroot/usr/bin/worldbisect" "$debroot/usr/sbin/worldbisectd"
    gzip -n -9 -c docs/man/worldbisect.1 > "$debroot/usr/share/man/man1/worldbisect.1.gz"
    gzip -n -9 -c docs/man/worldbisect.conf.5 > "$debroot/usr/share/man/man5/worldbisect.conf.5.gz"
    gzip -n -9 -c docs/man/worldbisectd.8 > "$debroot/usr/share/man/man8/worldbisectd.8.gz"
    cp README.md CHANGELOG.md "$debroot/usr/share/doc/worldbisect/"
    cp packaging/systemd/worldbisectd.service "$debroot/lib/systemd/system/"
    cp packaging/tmpfiles.d/worldbisect.conf "$debroot/usr/lib/tmpfiles.d/"
    cp packaging/sysusers.d/worldbisect.conf "$debroot/usr/lib/sysusers.d/"
    printf '%s\n' 'Run worldbisectd init to create config.json. No default API token is shipped.' > "$debroot/etc/worldbisect/README"
    find "$debroot" -exec touch -h -d "@$SOURCE_DATE_EPOCH" {} +
    find "$debroot" -type d -exec chmod 0755 {} +
    find "$debroot" -type d -exec chmod g-s {} +
    chmod 0755 "$debroot/DEBIAN/postinst" "$debroot/usr/bin/worldbisect" "$debroot/usr/sbin/worldbisectd"
    chmod 0644 "$debroot/DEBIAN/control"
    dpkg-deb --root-owner-group --build "$debroot" "$DIST/worldbisect_${VERSION}_linux_${arch}.deb" >/dev/null
    rm -rf "$debroot"
  fi
  rm -rf "$stage"
done

source_tmp=$(mktemp -d)
source_stage="$source_tmp/worldbisect-$VERSION"
mkdir -p "$source_stage"
tar --exclude-vcs --exclude='./dist' --exclude='dist' --exclude='./bin' --exclude='bin' \
    --exclude='./.coverage' --exclude='.coverage' --exclude='./worldbisect' --exclude='./worldbisectd' \
    -C "$ROOT" -cf - . | tar -C "$source_stage" -xf -
archive_tree "$source_stage" "$DIST/worldbisect-${VERSION}-source.tar.gz"
rm -rf "$source_tmp"

python3 scripts/generate-sbom.py --output "$DIST/worldbisect-${VERSION}.spdx.json" --version "$VERSION"
(
  cd "$DIST"
  find . -maxdepth 1 -type f ! -name SHA256SUMS -printf '%f\n' | LC_ALL=C sort | xargs -r sha256sum > SHA256SUMS
)

printf 'Release artifacts:\n'
find "$DIST" -maxdepth 1 -type f -printf '  %f\n' | LC_ALL=C sort
