#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
PREFIX=${PREFIX:-/usr/local}
DESTDIR=${DESTDIR:-}

install -Dm0755 "$ROOT/bin/worldbisect" "$DESTDIR$PREFIX/bin/worldbisect"
install -Dm0755 "$ROOT/bin/worldbisectd" "$DESTDIR$PREFIX/sbin/worldbisectd"
install -Dm0644 "$ROOT/docs/man/worldbisect.1" "$DESTDIR$PREFIX/share/man/man1/worldbisect.1"
install -Dm0644 "$ROOT/docs/man/worldbisectd.8" "$DESTDIR$PREFIX/share/man/man8/worldbisectd.8"
install -Dm0644 "$ROOT/docs/man/worldbisect.conf.5" "$DESTDIR$PREFIX/share/man/man5/worldbisect.conf.5"
install -Dm0644 "$ROOT/LICENSE" "$DESTDIR$PREFIX/share/doc/worldbisect/LICENSE"
install -Dm0644 "$ROOT/NOTICE" "$DESTDIR$PREFIX/share/doc/worldbisect/NOTICE"

printf 'Installed WorldBisect under %s%s\n' "$DESTDIR" "$PREFIX"
