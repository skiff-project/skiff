#!/usr/bin/env bash
# Skiff build script (works without make).
#
#   ./build.sh            build the UI and the binary for this machine (bin/skiff)
#   ./build.sh portable   build without the native webview (fully static)
#   ./build.sh release    cross-compile every target into dist/
#   ./build.sh packages   the installers this machine can make, into dist/:
#                         on Linux .deb, AppImage and the Windows installer,
#                         on macOS the .dmg (one at a time: deb, appimage,
#                         setup-windows, dmg)
#   ./build.sh dev        rebuild the UI on change and serve on 127.0.0.1:8484
#   ./build.sh test       go vet + unit tests + TypeScript typecheck + translations
#   ./build.sh itest      end-to-end tests against the Docker test servers:
#                         every API method, web mode, command line and scripts
#   ./build.sh uitest     end-to-end tests of the interface in a browser
#   ./build.sh ui         only build the web UI
# Several commands run one after the other: ./build.sh deb appimage
set -euo pipefail
cd "$(dirname "$0")"

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo 0.1.0-dev)}"
LDFLAGS="-s -w -X main.version=${VERSION}"
GO="${GO:-go}"
PNPM="${PNPM:-pnpm}"

native_tags() {
  case "$(uname -s)" in
    Linux)
      if command -v pkg-config >/dev/null && pkg-config --exists webkit2gtk-4.1 2>/dev/null; then
        echo "desktop,production,webkit2_41"
      fi ;;
    Darwin|MINGW*|MSYS*|CYGWIN*) echo "desktop,production" ;;
  esac
}

UI_BUILT=""
ui() {
  [ -n "$UI_BUILT" ] && return
  if [ ! -d frontend/node_modules ]; then
    (cd frontend && ($PNPM install --frozen-lockfile || $PNPM install))
  fi
  (cd frontend && $PNPM run build)
  UI_BUILT=1
}

build() {
  ui
  mkdir -p bin
  local tags; tags="$(native_tags)"
  local ext=""; case "$(uname -s)" in MINGW*|MSYS*|CYGWIN*) ext=".exe" ;; esac
  if [ -n "$tags" ]; then
    CGO_ENABLED=1 $GO build -trimpath -tags "$tags" -ldflags "$LDFLAGS" -o "bin/skiff$ext" .
    echo "built bin/skiff$ext (native window) — $VERSION"
  else
    CGO_ENABLED=0 $GO build -trimpath -ldflags "$LDFLAGS" -o "bin/skiff$ext" .
    echo "built bin/skiff$ext (browser window; install WebKitGTK headers for the native window) — $VERSION"
  fi
}

portable() {
  ui
  mkdir -p bin
  CGO_ENABLED=0 $GO build -trimpath -ldflags "$LDFLAGS" -o bin/skiff .
  echo "built bin/skiff (portable) — $VERSION"
}

release() {
  ui
  rm -rf dist && mkdir -p dist
  local t
  for t in windows/amd64 windows/arm64; do
    GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 $GO build -trimpath -tags desktop,production -ldflags "$LDFLAGS" -o "dist/skiff-${t%/*}-${t#*/}.exe" .
  done
  for t in linux/amd64 linux/arm64 linux/arm darwin/arm64 darwin/amd64 freebsd/amd64; do
    local name="skiff-${t%/*}-${t#*/}"
    [ "${t%/*}" = darwin ] && name="skiff-macos-${t#*/}"
    [ "$t" = linux/arm ] && name="skiff-linux-armv7"
    GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 $GO build -trimpath -ldflags "$LDFLAGS" -o "dist/$name" .
  done
  if [ "$(uname -s)" = Linux ] && [ -n "$(native_tags)" ]; then
    CGO_ENABLED=1 $GO build -trimpath -tags "$(native_tags)" -ldflags "$LDFLAGS" -o "dist/skiff-linux-$($GO env GOARCH)-desktop" .
  fi
  cp LICENSE NOTICE THIRD_PARTY_NOTICES.txt dist/
  (cd dist && sha256sum ./* > SHA256SUMS)
  ls -lh dist
}

# The packages want a version that starts with a digit:
# v1.2.0 → 1.2.0, v1.2.0-5-gabc → 1.2.0+5.gabc, abc1234-dirty → 0.1.0~git.abc1234.dirty
pkg_version() {
  local v="${VERSION#v}"
  if [[ "$v" =~ ^[0-9]+\.[0-9]+ ]]; then
    v="${v/-/+}"
    echo "${v//-/.}"
  else
    echo "0.1.0~git.${v//-/.}" # no tag yet: a commit
  fi
}

# X.Y.Z only (macOS bundle, Windows file version)
num_version() {
  local v; v="$(pkg_version)"; v="${v%%[~+]*}"
  case "$v" in
    *.*.*) echo "$v" ;;
    *.*) echo "$v.0" ;;
    *) echo "$v.0.0" ;;
  esac
}

# a folder for the binaries put in the packages, removed at the end
PKG_WORK=""
pkg_work() {
  if [ -z "$PKG_WORK" ]; then
    PKG_WORK="$(mktemp -d)"
    trap 'rm -rf "$PKG_WORK"' EXIT
  fi
}

# the native binary (skiff) and the portable one (skiff-browser) of Linux
LINUX_BUILT=""
linux_binaries() {
  [ -n "$LINUX_BUILT" ] && return
  if [ "$(uname -s)" != Linux ] || [ -z "$(native_tags)" ]; then
    echo "the Linux packages hold the native window: build them on Linux with libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config" >&2
    exit 1
  fi
  ui
  pkg_work
  CGO_ENABLED=1 $GO build -trimpath -tags "$(native_tags)" -ldflags "$LDFLAGS" -o "$PKG_WORK/skiff" .
  CGO_ENABLED=0 $GO build -trimpath -ldflags "$LDFLAGS" -o "$PKG_WORK/skiff-browser" .
  LINUX_BUILT=1
}

deb() {
  linux_binaries
  local arch; arch="$($GO env GOARCH)"; [ "$arch" = arm ] && arch=armhf
  build/linux/deb.sh "$PKG_WORK/skiff" "$arch" "$(pkg_version)" dist
}

# The AppImage runtime of an architecture: $APPIMAGE_RUNTIME, or the one of
# the AppImage project, downloaded once into build/cache and checked.
APPIMAGE_RUNTIME_RELEASE=20251108
appimage_runtime() {
  if [ -n "${APPIMAGE_RUNTIME:-}" ]; then
    echo "$APPIMAGE_RUNTIME"
    return
  fi
  local sum
  case "$1" in
    x86_64) sum=2fca8b443c92510f1483a883f60061ad09b46b978b2631c807cd873a47ec260d ;;
    aarch64) sum=00cbdfcf917cc6c0ff6d3347d59e0ca1f7f45a6df1a428a0d6d8a78664d87444 ;;
  esac
  local f="build/cache/runtime-$APPIMAGE_RUNTIME_RELEASE-$1"
  if [ ! -s "$f" ]; then
    mkdir -p build/cache
    echo "downloading the AppImage runtime ($1) from github.com/AppImage/type2-runtime" >&2
    curl -fsSL -o "$f.part" "https://github.com/AppImage/type2-runtime/releases/download/$APPIMAGE_RUNTIME_RELEASE/runtime-$1"
    if ! echo "$sum  $f.part" | sha256sum -c --status -; then
      rm -f "$f.part"
      echo "the AppImage runtime does not match its checksum" >&2
      exit 1
    fi
    mv "$f.part" "$f"
  fi
  echo "$f"
}

appimage() {
  linux_binaries
  local arch
  case "$($GO env GOARCH)" in
    amd64) arch=x86_64 ;;
    arm64) arch=aarch64 ;;
    *) echo "no AppImage for $($GO env GOARCH)" >&2; exit 1 ;;
  esac
  local runtime; runtime="$(appimage_runtime "$arch")"
  build/linux/appimage.sh "$PKG_WORK/skiff" "$PKG_WORK/skiff-browser" "$arch" "$(pkg_version)" "$runtime" dist
}

# The Windows installers (x64 and ARM64), made with NSIS from any system.
setup_windows() {
  if ! command -v makensis >/dev/null; then
    echo "the Windows installer needs NSIS: apt install nsis, brew install makensis or choco install nsis" >&2
    exit 1
  fi
  ui
  pkg_work
  mkdir -p dist
  local arch v; v="$(pkg_version)"
  for arch in amd64 arm64; do
    GOOS=windows GOARCH=$arch CGO_ENABLED=0 $GO build -trimpath -tags desktop,production -ldflags "$LDFLAGS" -o "$PKG_WORK/skiff-$arch.exe" .
    makensis -V2 -DVERSION="$v" -DNUMVERSION="$(num_version).0" -DARCH="$arch" \
      -DEXE="$PKG_WORK/skiff-$arch.exe" -DOUTFILE="$PWD/dist/skiff-setup-$v-windows-$arch.exe" build/windows/installer.nsi
    echo "dist/skiff-setup-$v-windows-$arch.exe"
  done
}

# The macOS disk image: a universal Skiff.app with the native window (on
# macOS, which builds it with cgo).
dmg() {
  if [ "$(uname -s)" != Darwin ]; then
    echo "the disk image is made on macOS (cgo for the native window, hdiutil); the CI does it" >&2
    exit 1
  fi
  ui
  pkg_work
  local arch
  for arch in arm64 amd64; do
    CGO_ENABLED=1 GOARCH=$arch $GO build -trimpath -tags desktop,production -ldflags "$LDFLAGS" -o "$PKG_WORK/skiff-$arch" .
  done
  lipo -create -output "$PKG_WORK/skiff" "$PKG_WORK/skiff-arm64" "$PKG_WORK/skiff-amd64"
  build/darwin/bundle.sh "$PKG_WORK/skiff" "$PKG_WORK/Skiff.app" "$(num_version)"
  build/darwin/dmg.sh "$PKG_WORK/Skiff.app" "dist/Skiff-$(pkg_version)-macos.dmg"
}

packages() {
  case "$(uname -s)" in
    Linux)
      deb
      appimage
      if command -v makensis >/dev/null; then
        setup_windows
      else
        echo "skipped: the Windows installer (install NSIS to make it)" >&2
      fi ;;
    Darwin) dmg ;;
    *) setup_windows ;;
  esac
}

dev() {
  [ -d frontend/node_modules ] || (cd frontend && $PNPM install)
  (cd frontend && $PNPM run watch) &
  trap 'kill %1 2>/dev/null' EXIT
  echo "Skiff UI: http://127.0.0.1:8484/?token=dev"
  SKIFF_NO_WINDOW=1 SKIFF_PORT=8484 SKIFF_DEV_TOKEN=dev SKIFF_UI_DIR=frontend/dist $GO run .
}

test_all() {
  $GO vet ./...
  $GO test ./...
  (cd frontend && $PNPM run typecheck && node scripts/check-i18n.mjs)
  # the notices of the included software follow the dependencies
  python3 build/notices.py --check
  # the links of the documentation, in both languages
  python3 docs/check-links.py
}

testenv() {
  docker compose -f testenv/docker-compose.yml up -d
  sleep 5
}

itest() {
  testenv
  $GO test -tags integration -count=1 -timeout 40m ./internal/core/ ./internal/server/ .
}

uitest() {
  build
  testenv
  (cd test/ui && $PNPM install && $PNPM exec playwright install chromium && SKIFF_BIN=../../bin/skiff $PNPM test)
}

for cmd in "${@:-build}"; do
  case "$cmd" in
    build) build ;;
    portable) portable ;;
    release) release ;;
    packages) packages ;;
    deb) deb ;;
    appimage) appimage ;;
    setup-windows) setup_windows ;;
    dmg) dmg ;;
    dev) dev ;;
    test) test_all ;;
    itest) itest ;;
    uitest) uitest ;;
    ui) ui ;;
    *) echo "usage: $0 [build|portable|release|packages|deb|appimage|setup-windows|dmg|dev|test|itest|uitest|ui]"; exit 2 ;;
  esac
done
