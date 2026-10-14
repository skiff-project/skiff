#!/usr/bin/env bash
# Wraps a macOS binary into Skiff.app (run on macOS: needs sips + iconutil).
set -euo pipefail
BIN="$1"; OUT="${2:-Skiff.app}"; VERSION="${3:-0.1.0}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
rm -rf "$OUT"
mkdir -p "$OUT/Contents/MacOS" "$OUT/Contents/Resources"
cp "$BIN" "$OUT/Contents/MacOS/skiff"
ICONSET="$(mktemp -d)/skiff.iconset"; mkdir -p "$ICONSET"
for s in 16 32 64 128 256 512; do
  sips -z $s $s "$ROOT/build/appicon.png" --out "$ICONSET/icon_${s}x${s}.png" >/dev/null
  d=$((s*2)); sips -z $d $d "$ROOT/build/appicon.png" --out "$ICONSET/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$OUT/Contents/Resources/skiff.icns"
cat > "$OUT/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key><string>Skiff</string>
  <key>CFBundleDisplayName</key><string>Skiff</string>
  <key>CFBundleIdentifier</key><string>app.skiff.desktop</string>
  <key>CFBundleExecutable</key><string>skiff</string>
  <key>CFBundleIconFile</key><string>skiff</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>${VERSION}</string>
  <key>CFBundleVersion</key><string>${VERSION}</string>
  <key>LSMinimumSystemVersion</key><string>11.0</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>NSAppTransportSecurity</key><dict><key>NSAllowsLocalNetworking</key><true/></dict>
</dict></plist>
PLIST
echo "created $OUT"
