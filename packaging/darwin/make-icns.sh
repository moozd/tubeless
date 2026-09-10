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

# macOS (since Big Sur) applies its own rounded-square/shadow treatment
# to every app icon, on top of whatever artwork is given it — which
# expects the actual glyph to occupy only ~82% of the canvas, centered,
# with a transparent margin for the OS's own treatment to sit in.
# icon.svg deliberately fills its canvas edge-to-edge (right for Linux,
# where no such treatment happens), so each size is shrunk to that 82%
# content box and then padded back out to the full size here — macOS
# only, not the shared source every other packaging target reads from.
for size in 16 32 128 256 512; do
	for variant in "$size" "$((size * 2))"; do
		content=$((variant * 82 / 100))
		tmp="$WORK/content-$variant.png"
		sips -z "$content" "$content" "$MASTER" --out "$tmp" >/dev/null
		if [ "$variant" = "$size" ]; then
			out="$ICONSET/icon_${size}x${size}.png"
		else
			out="$ICONSET/icon_${size}x${size}@2x.png"
		fi
		sips -p "$variant" "$variant" "$tmp" --out "$out" >/dev/null
	done
done

iconutil -c icns "$ICONSET" -o "$OUT"
echo "Built $OUT"
