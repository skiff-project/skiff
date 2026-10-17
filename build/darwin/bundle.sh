#!/usr/bin/env bash
# Wraps a macOS binary into Skiff.app (the icon is skiff.icns, made by
# build/icons.py). On macOS, the bundle is then signed ad hoc (codesign), as
# Apple Silicon requires.
set -euo pipefail
BIN="$1"; OUT="${2:-Skiff.app}"; VERSION="${3:-0.1.0}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
rm -rf "$OUT"
mkdir -p "$OUT/Contents/MacOS" "$OUT/Contents/Resources"
cp "$BIN" "$OUT/Contents/MacOS/skiff"
chmod 755 "$OUT/Contents/MacOS/skiff"
cp "$ROOT/build/darwin/skiff.icns" "$OUT/Contents/Resources/skiff.icns"
cp "$ROOT/LICENSE" "$ROOT/NOTICE" "$ROOT/THIRD_PARTY_NOTICES.txt" "$OUT/Contents/Resources/"
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
  <key>NSHumanReadableCopyright</key><string>© 2026 Laterna — Apache-2.0</string>
  <key>NSAppTransportSecurity</key><dict><key>NSAllowsLocalNetworking</key><true/></dict>
  <key>CFBundleURLTypes</key><array><dict>
    <key>CFBundleURLName</key><string>Skiff session</string>
    <key>CFBundleURLSchemes</key><array>
      <string>sftp</string><string>scp</string><string>ftp</string><string>ftps</string><string>ftpes</string><string>dav</string><string>davs</string>
    </array>
  </dict></array>
</dict></plist>
PLIST
if command -v codesign >/dev/null; then
  codesign --force --sign - "$OUT"
fi
echo "created $OUT"
