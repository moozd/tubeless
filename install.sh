#!/usr/bin/env bash
# Detects the OS/distro and installs the matching tubeless package:
# .deb on Debian/Ubuntu-family, .rpm on Fedora/RHEL/openSUSE-family, an
# Arch package on Arch/Manjaro-family, a generic tarball into ~/.local/bin
# on any other Linux, and the .app bundle (symlinked onto PATH) on macOS.
#
# Usage:
#   ./install.sh              Download and install the latest GitHub release
#   ./install.sh --version v1.2.3   Install a specific release
#   ./install.sh --local      Install from this repo's dist/ (after running
#                              `make package-linux` / `make package-darwin`)
set -euo pipefail

REPO="moozd/tubeless"
VERSION="latest"
LOCAL=0
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DIST_DIR="$SCRIPT_DIR/dist"

usage() {
	sed -n '2,12p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

while [ $# -gt 0 ]; do
	case "$1" in
	--local)
		LOCAL=1
		shift
		;;
	--version)
		VERSION="$2"
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "unknown argument: $1" >&2
		usage
		exit 1
		;;
	esac
done

os=$(uname -s)
arch=$(uname -m)
case "$arch" in
x86_64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*)
	echo "unsupported architecture: $arch" >&2
	exit 1
	;;
esac

# fetch_release_asset finds and downloads the release asset whose name
# ends in $1 (e.g. "linux-amd64.deb"), echoing the local temp path it was
# saved to. Uses the GitHub API directly (no `gh` CLI / jq dependency) so
# it works on a bare install.
fetch_release_asset() {
	local suffix=$1
	local api_url
	if [ "$VERSION" = "latest" ]; then
		api_url="https://api.github.com/repos/$REPO/releases/latest"
	else
		api_url="https://api.github.com/repos/$REPO/releases/tags/$VERSION"
	fi
	local asset_url
	asset_url=$(curl -fsSL "$api_url" |
		grep -o "\"browser_download_url\": *\"[^\"]*$suffix\"" |
		head -1 |
		sed -e 's/^"browser_download_url": *"//' -e 's/"$//')
	if [ -z "$asset_url" ]; then
		echo "no release asset matching *$suffix found for $VERSION" >&2
		exit 1
	fi
	local tmp
	tmp=$(mktemp -d)
	local dest="$tmp/$(basename "$asset_url")"
	curl -fsSL -o "$dest" "$asset_url"
	echo "$dest"
}

# find_local_asset finds a previously-built dist/ file ending in $1 —
# the --local counterpart to fetch_release_asset.
find_local_asset() {
	local suffix=$1
	local match
	match=$(find "$DIST_DIR" -maxdepth 1 -name "*$suffix" 2>/dev/null | head -1)
	if [ -z "$match" ]; then
		echo "no local dist/ file matching *$suffix — run the matching 'make package-*' target first" >&2
		exit 1
	fi
	echo "$match"
}

get_asset() {
	if [ "$LOCAL" = 1 ]; then
		find_local_asset "$1"
	else
		fetch_release_asset "$1"
	fi
}

install_darwin() {
	local zip
	zip=$(get_asset "darwin-$arch.zip")
	local app_dir="$HOME/Applications"
	mkdir -p "$app_dir"
	rm -rf "$app_dir/Tubeless.app"
	unzip -q "$zip" -d "$app_dir"
	# The zip download sets com.apple.quarantine, and the app is only
	# ad-hoc signed (no paid Apple Developer ID / notarization) — without
	# clearing this, Gatekeeper reports "Tubeless is damaged and can't be
	# opened" instead of actually launching it.
	xattr -cr "$app_dir/Tubeless.app"
	local bundle="$app_dir/Tubeless.app/Contents/MacOS"
	local bin_dir="/usr/local/bin"
	mkdir -p "$bin_dir"
	ln -sf "$bundle/tubeless" "$bin_dir/tubeless"
	ln -sf "$bundle/tubeless-config" "$bin_dir/tubeless-config"
	echo "Installed $app_dir/Tubeless.app, symlinked into $bin_dir"
}

install_linux() {
	local id="" id_like=""
	if [ -r /etc/os-release ]; then
		. /etc/os-release
		id="${ID:-}"
		id_like="${ID_LIKE:-}"
	fi

	if command -v apt >/dev/null 2>&1 && { [ "$id" = "debian" ] || [ "$id" = "ubuntu" ] || echo "$id_like" | grep -q debian; }; then
		local pkg
		pkg=$(get_asset "linux-$arch.deb")
		sudo apt install -y "$pkg"
	elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1 || echo "$id $id_like" | grep -Eq "fedora|rhel|suse"; then
		local pkg
		pkg=$(get_asset "linux-$arch.rpm")
		local mgr=dnf
		command -v dnf >/dev/null 2>&1 || mgr=yum
		sudo "$mgr" install -y "$pkg"
	elif command -v pacman >/dev/null 2>&1 || [ "$id" = "arch" ] || echo "$id_like" | grep -q arch; then
		local pkg
		pkg=$(get_asset "linux-$arch.pkg.tar.zst")
		sudo pacman -U --noconfirm "$pkg"
	else
		echo "No supported package manager detected — falling back to a generic install into ~/.local/bin"
		local tarball
		tarball=$(get_asset "linux-$arch.tar.gz")
		local dest="$HOME/.local"
		mkdir -p "$dest"
		tar -C "$dest" -xzf "$tarball"
		echo "Installed into $dest/bin — make sure that's on your PATH."
	fi
}

case "$os" in
Darwin) install_darwin ;;
Linux) install_linux ;;
*)
	echo "unsupported OS: $os" >&2
	exit 1
	;;
esac
