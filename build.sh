#!/usr/bin/env bash
# Skiff build script (works without make).
#
#   ./build.sh            build the UI and the binary for this machine (bin/skiff)
#   ./build.sh portable   build without the native webview (fully static)
#   ./build.sh release    cross-compile every target into dist/
#   ./build.sh dev        rebuild the UI on change and serve on 127.0.0.1:8484
#   ./build.sh test       go vet + unit tests + TypeScript typecheck
#   ./build.sh itest      integration tests against the Docker test servers
#   ./build.sh ui         only build the web UI
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

ui() {
  if [ ! -d frontend/node_modules ]; then
    (cd frontend && ($PNPM install --frozen-lockfile || $PNPM install))
  fi
  (cd frontend && $PNPM run build)
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
  (cd dist && sha256sum ./* > SHA256SUMS)
  ls -lh dist
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
  (cd frontend && $PNPM run typecheck)
}

itest() {
  docker compose -f testenv/docker-compose.yml up -d
  sleep 5
  $GO test -tags integration -count=1 ./internal/core/ -run TestProtocols -v
}

case "${1:-build}" in
  build) build ;;
  portable) portable ;;
  release) release ;;
  dev) dev ;;
  test) test_all ;;
  itest) itest ;;
  ui) ui ;;
  *) echo "usage: $0 [build|portable|release|dev|test|itest|ui]"; exit 2 ;;
esac
