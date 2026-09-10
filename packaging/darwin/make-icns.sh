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

# macOS's Big Sur+ icon convention expects a margin around the artwork
# so it sits at a consistent size next to other apps' icons in the Dock
# — but icon.svg already draws its own rounded square (rx=40) inset a
# few px from its 256x256 canvas, i.e. it's already "self-masked" rather
# than a raw edge-to-edge square expecting the OS to add that margin for
# it. Shrinking it by the full ~82% Apple guidance assumes for that raw
# case double-applies the inset on top of the artwork's own, landing the
# icon noticeably smaller in the Dock than sibling apps. 94% instead
# accounts for the source's own margin (~94% fill) without stacking
# another one on top — a light touch-up, not the full safe-zone shrink.
for size in 16 32 128 256 512; do
	for variant in "$size" "$((size * 2))"; do
		content=$((variant * 94 / 100))
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
