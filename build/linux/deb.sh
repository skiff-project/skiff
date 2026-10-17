#!/usr/bin/env bash
# Makes the Debian / Ubuntu package of Skiff from a native binary (built
# with WebKitGTK): the program in /usr/bin, its menu entry and its icons.
#   build/linux/deb.sh <binary> <amd64|arm64> <version> [output folder]
set -euo pipefail
umask 022
BIN="$1"; ARCH="$2"; VERSION="$3"; OUT="${4:-dist}"
HERE="$(cd "$(dirname "$0")" && pwd)"
STAGE="$(mktemp -d)"
chmod 755 "$STAGE"
trap 'rm -rf "$STAGE"' EXIT

install -Dm755 "$BIN" "$STAGE/usr/bin/skiff"
install -Dm644 "$HERE/skiff.desktop" "$STAGE/usr/share/applications/skiff.desktop"
for icon in "$HERE"/icons/hicolor/*/apps/skiff.png; do
  install -Dm644 "$icon" "$STAGE/usr/share/icons/${icon#"$HERE/icons/"}"
done
install -Dm644 "$HERE/copyright" "$STAGE/usr/share/doc/skiff/copyright"
install -Dm644 "$HERE/../../NOTICE" "$STAGE/usr/share/doc/skiff/NOTICE"
install -Dm644 "$HERE/../../THIRD_PARTY_NOTICES.txt" "$STAGE/usr/share/doc/skiff/THIRD_PARTY_NOTICES.txt"

mkdir -p "$STAGE/DEBIAN"
cat > "$STAGE/DEBIAN/control" <<CONTROL
Package: skiff
Version: $VERSION
Architecture: $ARCH
Maintainer: Laterna <337788684+laterna-media@users.noreply.github.com>
Installed-Size: $(du -sk "$STAGE/usr" | cut -f1)
Depends: libc6, libgtk-3-0t64 | libgtk-3-0, libwebkit2gtk-4.1-0
Section: net
Priority: optional
Description: SFTP, SCP, FTP, WebDAV and S3 client, with terminal and editor
 Skiff transfers and synchronizes files over SFTP, SCP, FTP, FTPS, WebDAV
 and S3: two-panel commander, SSH terminal, tunnels, text editor, server
 monitor and scripts compatible with WinSCP.
CONTROL

mkdir -p "$OUT"
deb="$OUT/skiff_${VERSION}_${ARCH}.deb"
# xz: read by every dpkg (zstd only by recent ones)
dpkg-deb --root-owner-group -Zxz --build "$STAGE" "$deb" >/dev/null
echo "$deb"
