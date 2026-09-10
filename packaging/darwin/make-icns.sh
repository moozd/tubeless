#!/bin/sh
# Rasterizes packaging/icon.svg into packaging/darwin/tubeless.icns.
# macOS-only: uses qlmanage (Quick Look's thumbnail renderer, ships with
# every macOS release, unlike sips which can't rasterize SVG on older
# macOS versions) to render the SVG once at high resolution, then sips
# (preinstalled) to downscale into every size a .icns needs, and iconutil
# (preinstalled) to pack the resulting .iconset.
set -eu

cd "$(dirname "$0")/../.."

SRC=packaging/icon.svg
OUT=packaging/darwin/tubeless.icns
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

ICONSET="$WORK/tubeless.iconset"
mkdir -p "$ICONSET"

# qlmanage names its output <basename>.png inside -o's directory and
# doesn't guarantee exact pixel dimensions, so render well above the
# largest size needed (1024) and let sips downscale precisely from there.
qlmanage -t -s 1024 -o "$WORK" "$SRC" >/dev/null
MASTER="$WORK/$(basename "$SRC").png"

for size in 16 32 128 256 512; do
	sips -z "$size" "$size" "$MASTER" --out "$ICONSET/icon_${size}x${size}.png" >/dev/null
	double=$((size * 2))
	sips -z "$double" "$double" "$MASTER" --out "$ICONSET/icon_${size}x${size}@2x.png" >/dev/null
done

iconutil -c icns "$ICONSET" -o "$OUT"
echo "Built $OUT"
