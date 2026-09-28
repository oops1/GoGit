#!/bin/sh
set -eu

VERSION="${1:-0.0.0-dev}"
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
BIN="${GOGIT_BINARY:-$ROOT/dist/linux-amd64/gogit}"
APPDIR="$ROOT/dist/appimage/Go.Git.AppDir"
APPIMAGETOOL="${APPIMAGETOOL:-appimagetool}"

if [ ! -f "$BIN" ]; then
	echo "appimage.sh: binary not found at $BIN (run 'make build-linux' first)" >&2
	exit 1
fi

rm -rf "$APPDIR"
mkdir -p "$APPDIR/usr/bin" "$APPDIR/usr/share/applications" "$APPDIR/usr/share/icons/hicolor/scalable/apps"

cp "$BIN" "$APPDIR/usr/bin/gogit"
chmod 0755 "$APPDIR/usr/bin/gogit"

cp "$ROOT/packaging/linux/gogit.desktop" "$APPDIR/usr/share/applications/gogit.desktop"
cp "$ROOT/packaging/linux/gogit.desktop" "$APPDIR/gogit.desktop"

cp "$ROOT/internal/assets/icons/app.svg" "$APPDIR/usr/share/icons/hicolor/scalable/apps/gogit.svg"
cp "$ROOT/internal/assets/icons/app.svg" "$APPDIR/gogit.svg"

cp "$ROOT/packaging/linux/AppImage/AppRun" "$APPDIR/AppRun"
chmod 0755 "$APPDIR/AppRun"

if command -v rsvg-convert >/dev/null 2>&1; then
	rsvg-convert -w 256 -h 256 -o "$APPDIR/gogit.png" "$ROOT/internal/assets/icons/app.svg"
elif command -v magick >/dev/null 2>&1; then
	magick -background none "$ROOT/internal/assets/icons/app.svg" -resize 256x256 "$APPDIR/gogit.png"
fi

mkdir -p "$ROOT/dist"
ARCH=x86_64 "$APPIMAGETOOL" "$APPDIR" "$ROOT/dist/gogit-$VERSION-x86_64.AppImage"
