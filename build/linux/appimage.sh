#!/usr/bin/env bash
# Makes the AppImage of Skiff: one file that runs on most Linux systems
# without installation. It holds the native binary (WebKitGTK) and the
# portable one, used when the system lacks WebKitGTK 4.1 (see AppRun).
#   build/linux/appimage.sh <native binary> <portable binary> <x86_64|aarch64> <version> <runtime> [output folder]
# <runtime> is the AppImage type 2 runtime of the architecture
# (https://github.com/AppImage/type2-runtime/releases).
set -euo pipefail
umask 022
NATIVE="$1"; PORTABLE="$2"; ARCH="$3"; VERSION="$4"; RUNTIME="$5"; OUT="${6:-dist}"
HERE="$(cd "$(dirname "$0")" && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
APPDIR="$WORK/Skiff.AppDir"

install -Dm755 "$HERE/AppRun" "$APPDIR/AppRun"
install -Dm755 "$NATIVE" "$APPDIR/usr/bin/skiff"
install -Dm755 "$PORTABLE" "$APPDIR/usr/bin/skiff-browser"
install -Dm644 "$HERE/skiff.desktop" "$APPDIR/usr/share/applications/skiff.desktop"
for icon in "$HERE"/icons/hicolor/*/apps/skiff.png; do
  install -Dm644 "$icon" "$APPDIR/usr/share/icons/${icon#"$HERE/icons/"}"
done
for f in LICENSE NOTICE THIRD_PARTY_NOTICES.txt; do
  install -Dm644 "$HERE/../../$f" "$APPDIR/usr/share/doc/skiff/$f"
done
# the AppImage itself: entry and icon at the top
sed "s/^StartupNotify=true$/&\nX-AppImage-Version=$VERSION/" "$HERE/skiff.desktop" > "$APPDIR/skiff.desktop"
cp "$HERE/icons/hicolor/256x256/apps/skiff.png" "$APPDIR/skiff.png"
ln -s skiff.png "$APPDIR/.DirIcon"

mksquashfs "$APPDIR" "$WORK/skiff.squashfs" -root-owned -noappend -comp zstd -quiet -no-progress >/dev/null
mkdir -p "$OUT"
img="$OUT/Skiff-$VERSION-$ARCH.AppImage"
cat "$RUNTIME" "$WORK/skiff.squashfs" > "$img"
chmod 755 "$img"
echo "$img"
