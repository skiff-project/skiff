#!/usr/bin/env bash
# Makes the disk image of Skiff (on macOS: needs hdiutil): Skiff.app and a
# link to Applications, to install it by dragging.
#   build/darwin/dmg.sh Skiff.app dist/Skiff-0.1.0-macos.dmg
set -euo pipefail
APP="$1"; OUT="$2"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
cp -R "$APP" "$STAGE/Skiff.app"
ln -s /Applications "$STAGE/Applications"
mkdir -p "$(dirname "$OUT")"
rm -f "$OUT"
hdiutil create -volname Skiff -srcfolder "$STAGE" -fs HFS+ -format UDZO -ov "$OUT" >/dev/null
echo "$OUT"
