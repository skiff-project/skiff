#!/bin/sh
# Installs Skiff on macOS, from the latest release of
# https://github.com/skiff-project/skiff:
#
#   curl -fsSL https://raw.githubusercontent.com/skiff-project/skiff/main/install.sh | sh
#
# It downloads the disk image, checks it against the SHA256SUMS of the
# release, and copies Skiff.app into /Applications (~/Applications when
# /Applications cannot be written). A file downloaded by curl carries no
# quarantine: macOS opens Skiff without the "unidentified developer"
# warning, which a disk image downloaded by a browser would show (Skiff is
# not notarized by Apple). Run it again to update Skiff.
#
# SKIFF_DMG=<disk image> installs that file instead, without downloading
# anything (the CI tests the image it has just made this way).
set -eu

REPO=skiff-project/skiff

fail() {
  echo "skiff: $*" >&2
  exit 1
}

[ "$(uname -s)" = Darwin ] || fail "this script installs Skiff on macOS; for Linux and Windows, see https://github.com/$REPO/releases/latest"

tmp=$(mktemp -d)
mnt="$tmp/mnt"
cleanup() {
  [ -d "$mnt" ] && hdiutil detach -quiet "$mnt" 2>/dev/null || true
  rm -rf "$tmp"
}
trap cleanup EXIT INT TERM

if [ -n "${SKIFF_DMG:-}" ]; then
  name=${SKIFF_DMG##*/}
  cp "$SKIFF_DMG" "$tmp/$name"
else
  # the files of the latest release (the API answers in JSON, one field a line)
  release=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest") || fail "no release found on github.com/$REPO"
  asset() {
    printf '%s\n' "$release" | sed -n 's/.*"browser_download_url": *"\([^"]*\)".*/\1/p' | grep -E "$1" | head -1
  }
  dmg_url=$(asset '/Skiff-[^/]*-macos\.dmg$')
  sums_url=$(asset '/SHA256SUMS$')
  [ -n "$dmg_url" ] || fail "the latest release has no macOS disk image"
  [ -n "$sums_url" ] || fail "the latest release has no SHA256SUMS"
  name=${dmg_url##*/}

  echo "Downloading $name…"
  curl -fL --progress-bar -o "$tmp/$name" "$dmg_url"
  curl -fsSL -o "$tmp/SHA256SUMS" "$sums_url"
  want=$(awk -v n="$name" '$2 == n || $2 == "*" n { print $1 }' "$tmp/SHA256SUMS")
  have=$(shasum -a 256 "$tmp/$name" | awk '{ print $1 }')
  [ -n "$want" ] || fail "$name is not in SHA256SUMS"
  [ "$want" = "$have" ] || fail "$name does not match its SHA-256 sum: download it again"
fi

mkdir -p "$mnt"
hdiutil attach -quiet -nobrowse -readonly -mountpoint "$mnt" "$tmp/$name"
[ -d "$mnt/Skiff.app" ] || fail "no Skiff.app in $name"

dest=/Applications
[ -w "$dest" ] || dest="$HOME/Applications"
mkdir -p "$dest"
if pgrep -x skiff >/dev/null 2>&1; then
  echo "Skiff is open: the new version starts at its next start."
fi
rm -rf "$dest/Skiff.app.new"
ditto "$mnt/Skiff.app" "$dest/Skiff.app.new"
rm -rf "$dest/Skiff.app"
mv "$dest/Skiff.app.new" "$dest/Skiff.app"
xattr -dr com.apple.quarantine "$dest/Skiff.app" 2>/dev/null || true

version=${name#Skiff-}
echo "Skiff ${version%-macos.dmg} is installed in $dest/Skiff.app: open it from the Launchpad, or run: open \"$dest/Skiff.app\""
