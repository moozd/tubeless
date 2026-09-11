#!/bin/sh
# Compiles packaging/darwin/tubeless.icon into Assets.car — the icon
# format macOS 26 "Tahoe" actually renders through its Liquid Glass
# pipeline (adaptive light/dark/tinted variants, specular gloss). Without
# it, Tahoe treats the app as a legacy icon: since tubeless.icns draws
# its own rounded-square shape rather than Apple's exact continuity-curve
# squircle, Tahoe's "is this already a proper squircle?" heuristic says
# no and draws a gray squircle behind it instead of just using it as-is.
#
# packaging/darwin/tubeless.icon doesn't exist yet — it has to be
# authored in Apple's Icon Composer (ships with Xcode 26, or as a
# standalone download from developer.apple.com's "Additional Tools for
# Xcode") on an actual Mac, the same way this project's actual glyph
# artwork would be designed, then saved to that path. Until it exists,
# this script is a deliberate no-op (exit 0, not an error) so `make
# install-darwin`/`package-darwin` keep working off tubeless.icns alone,
# same as before this file existed.
#
# Needs actool from Xcode 26 + the macOS 26 SDK to run (same "only works
# on a real, up-to-date Mac" constraint make-icns.sh already has on
# sips/qlmanage/iconutil) — the Assets.car it produces still runs fine
# on older macOS versions per Apple's own guidance, so this only needs
# to be (re)built on one up-to-date machine, not on every install target.
set -eu

cd "$(dirname "$0")/../.."

SRC=packaging/darwin/tubeless.icon
OUT_DIR=packaging/darwin/assets-car-build

if [ ! -e "$SRC" ]; then
	echo "note: $SRC not found — skipping Assets.car (Tahoe's adaptive icon)." >&2
	echo "      Design it in Icon Composer and save it there to fix the macOS 26 icon; falling back to tubeless.icns for now." >&2
	exit 0
fi

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"
xcrun actool "$SRC" \
	--compile "$OUT_DIR" \
	--output-format human-readable-text \
	--notices --warnings --errors \
	--output-partial-info-plist "$OUT_DIR/partial-info.plist" \
	--app-icon tubeless \
	--include-all-app-icons \
	--enable-on-demand-resources NO \
	--development-region en \
	--target-device mac \
	--minimum-deployment-target 26.0 \
	--platform macosx

echo "Built $OUT_DIR/Assets.car"
