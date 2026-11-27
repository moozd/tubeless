#!/bin/sh
# add-cef.sh BUNDLE CEF_ROOT HELPER VERSION
#
# Puts the CEF framework and the four helper apps into a Tubeless.app,
# so a tubeless built with the cef tag can run applets. CEF on macOS
# loads only from this layout:
#
#   Contents/Frameworks/Chromium Embedded Framework.framework
#   Contents/Frameworks/tubeless Helper.app
#   Contents/Frameworks/tubeless Helper (GPU).app
#   Contents/Frameworks/tubeless Helper (Renderer).app
#   Contents/Frameworks/tubeless Helper (Plugin).app
#
# Each helper app holds the same HELPER binary under its own name.
# Sign the bundle after this script.
set -eu

bundle=$1
cef_root=$2
helper=$3
version=$4

here=$(dirname "$0")
frameworks="$bundle/Contents/Frameworks"
framework_name="Chromium Embedded Framework.framework"
framework="$cef_root/Release/$framework_name"

if [ ! -d "$framework" ]; then
	echo "add-cef.sh: no $framework_name in $cef_root/Release (set CEF_ROOT)" >&2
	exit 1
fi

mkdir -p "$frameworks"
rm -rf "$frameworks/$framework_name"
cp -R "$framework" "$frameworks/"
xattr -dr com.apple.quarantine "$frameworks/$framework_name"

# make_helper SUFFIX ID_SUFFIX makes one helper app.
make_helper() {
	name="tubeless Helper$1"
	app="$frameworks/$name.app"
	rm -rf "$app"
	mkdir -p "$app/Contents/MacOS"
	cp "$helper" "$app/Contents/MacOS/$name"
	sed -e "s|__NAME__|$name|g" \
	    -e "s|__ID__|com.moozd.tubeless.helper$2|" \
	    -e "s|__VERSION__|$version|" \
	    "$here/helper-Info.plist" > "$app/Contents/Info.plist"
}

make_helper "" ""
make_helper " (GPU)" ".gpu"
make_helper " (Renderer)" ".renderer"
make_helper " (Plugin)" ".plugin"
echo "Added CEF and its helpers to $bundle"
