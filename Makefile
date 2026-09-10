.PHONY: build build-x11 tubeless tubeless-config tektest test clean \
        install uninstall install-darwin uninstall-darwin help \
        run-green run-amber run-config package-linux package-darwin

BINARY_NAME=tubeless
BIN_DIR=bin
CMD_PATH=./cmd/$(BINARY_NAME)

# VERSION is baked into both binaries (see main.go's -version flag) and
# into packaging (Info.plist's CFBundleShortVersionString, nfpm/tarball
# filenames) via LDFLAGS below. Defaults to the nearest git tag; override
# for a build that shouldn't depend on the local git state.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.0.0-dev)
LDFLAGS = -X main.version=$(VERSION)

# PREFIX is the Linux install root — everything lands under
# $(PREFIX)/bin, $(PREFIX)/share/applications, $(PREFIX)/share/icons.
# Defaults to a per-user, no-sudo location; override for a system-wide
# install, e.g. `sudo make install PREFIX=/usr/local`.
PREFIX ?= $(HOME)/.local
BIN_INSTALL_DIR = $(PREFIX)/bin
DESKTOP_DIR = $(PREFIX)/share/applications
ICON_DIR = $(PREFIX)/share/icons/hicolor/scalable/apps

# APP_DIR is where the macOS .app bundle installs — user-writable, no
# sudo, same reasoning as PREFIX's default above. Override for
# /Applications if you want it system-wide: `make install-darwin APP_DIR=/Applications`.
APP_DIR ?= $(HOME)/Applications
APP_BUNDLE = $(APP_DIR)/Tubeless.app

help:
	@echo "Available targets:"
	@echo "  make build          - Build everything: tubeless (native Wayland on"
	@echo "                        Linux; the 'wayland' build tag is a no-op on"
	@echo "                        macOS/Windows, which always use their own"
	@echo "                        native Cocoa/Win32 backend regardless),"
	@echo "                        tubeless-config, and tektest"
	@echo "  make build-x11      - Build tubeless for X11 instead, for Linux"
	@echo "                        desktops without a Wayland compositor"
	@echo "  make run-green      - Build and run tubeless (green theme) with tektest"
	@echo "  make run-amber      - Build and run tubeless (amber theme) with tektest"
	@echo "  make run-config     - Run 'tubeless config' (the in-terminal settings UI)"
	@echo "  make test           - Run tests"
	@echo "  make install        - Linux: build + install tubeless/tubeless-config,"
	@echo "                        a .desktop entry, and an icon to \$$PREFIX"
	@echo "                        (default $(PREFIX); override for a system-wide"
	@echo "                        install, e.g. \`sudo make install PREFIX=/usr/local\`)"
	@echo "  make uninstall      - Remove exactly what 'make install' placed."
	@echo "                        Never touches ~/.config/tubeless/config.toml."
	@echo "  make install-darwin - macOS only, run on an actual Mac (GLFW's Cocoa"
	@echo "                        backend can't be cross-compiled from Linux):"
	@echo "                        builds a Tubeless.app bundle into \$$APP_DIR"
	@echo "                        (default $(APP_DIR)) and symlinks tubeless/"
	@echo "                        tubeless-config into /usr/local/bin so they"
	@echo "                        run from any Terminal shell, not just Finder"
	@echo "  make uninstall-darwin - Remove the installed Tubeless.app bundle"
	@echo "                        and its /usr/local/bin symlinks."
	@echo "  make package-linux  - Build for the host arch (override with"
	@echo "                        GOARCH_TARGET=amd64|arm64) and package as"
	@echo "                        .deb/.rpm/Arch pkg (via nfpm) + .tar.gz into dist/"
	@echo "  make package-darwin - Build a Tubeless.app (with icon) for the host"
	@echo "                        arch and zip it into dist/. Run per-arch on an"
	@echo "                        actual Mac of that architecture."
	@echo "  make clean          - Remove build artifacts"

build: tubeless tubeless-config tektest

build-x11:
	@mkdir -p $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_PATH)
	@echo "Built $(BIN_DIR)/$(BINARY_NAME) (X11)"

tubeless:
	@mkdir -p $(BIN_DIR)
	go build -tags wayland -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_PATH)
	@echo "Built $(BIN_DIR)/$(BINARY_NAME)"

# tubeless-config is the in-terminal settings UI; `tubeless config` runs it
# directly in the current terminal (see cmd/tubeless/main.go), no window.
tubeless-config:
	@mkdir -p $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/tubeless-config ./cmd/tubeless-config
	@echo "Built $(BIN_DIR)/tubeless-config"

tektest:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/tektest ./cmd/tektest
	@echo "Built $(BIN_DIR)/tektest"

run-green: build
	$(BIN_DIR)/$(BINARY_NAME) --theme=green --shell=$(BIN_DIR)/tektest

run-amber: build
	$(BIN_DIR)/$(BINARY_NAME) --theme=amber --shell=$(BIN_DIR)/tektest

run-config: build
	$(BIN_DIR)/$(BINARY_NAME) config

test:
	go test -v ./...

# install is Linux-only (desktop entries and the XDG icon theme directory
# layout are Linux/freedesktop conventions) — see install-darwin for
# macOS. tektest is deliberately excluded: it's a dev-only VT test-pattern
# generator (see run-green/run-amber above), not something a system
# install should ship.
install: tubeless tubeless-config
	@mkdir -p $(BIN_INSTALL_DIR) $(DESKTOP_DIR) $(ICON_DIR)
	cp $(BIN_DIR)/$(BINARY_NAME) $(BIN_INSTALL_DIR)/
	cp $(BIN_DIR)/tubeless-config $(BIN_INSTALL_DIR)/
	cp packaging/icon.svg $(ICON_DIR)/tubeless.svg
	sed -e 's|__EXEC__|$(BIN_INSTALL_DIR)/$(BINARY_NAME)|' \
	    -e 's|__ICON__|tubeless|' \
	    packaging/tubeless.desktop > $(DESKTOP_DIR)/tubeless.desktop
	@command -v update-desktop-database >/dev/null 2>&1 && \
	    update-desktop-database $(DESKTOP_DIR) 2>/dev/null || true
	@command -v gtk-update-icon-cache >/dev/null 2>&1 && \
	    gtk-update-icon-cache -f -t $(PREFIX)/share/icons/hicolor 2>/dev/null || true
	@echo "Installed $(BINARY_NAME) + tubeless-config to $(BIN_INSTALL_DIR)"
	@echo "Installed desktop entry to $(DESKTOP_DIR)/tubeless.desktop"
	@echo "Your config at ~/.config/tubeless/config.toml is untouched."

uninstall:
	rm -f $(BIN_INSTALL_DIR)/$(BINARY_NAME) $(BIN_INSTALL_DIR)/tubeless-config
	rm -f $(DESKTOP_DIR)/tubeless.desktop
	rm -f $(ICON_DIR)/tubeless.svg
	@command -v update-desktop-database >/dev/null 2>&1 && \
	    update-desktop-database $(DESKTOP_DIR) 2>/dev/null || true
	@echo "Uninstalled $(BINARY_NAME) + tubeless-config, desktop entry, and icon."
	@echo "Your config at ~/.config/tubeless/config.toml was left in place."

# CLI_BIN_DIR is where install-darwin symlinks the CLI binaries so they're
# runnable from any Terminal shell — /usr/local/bin is on macOS's default
# PATH (via /etc/paths) and user-writable without sudo on modern macOS,
# unlike dropping something only inside the .app bundle (Contents/MacOS
# isn't on PATH, and Finder-launched apps can't be run as a CLI command).
CLI_BIN_DIR ?= /usr/local/bin
ICNS = packaging/darwin/tubeless.icns

# install-darwin builds a minimal Tubeless.app bundle. Must be run on an
# actual Mac: GLFW's macOS backend needs the real Cocoa/OpenGL frameworks,
# which can't be cross-compiled from this Makefile's usual Linux host.
install-darwin: tubeless tubeless-config $(ICNS)
	@mkdir -p $(APP_BUNDLE)/Contents/MacOS $(APP_BUNDLE)/Contents/Resources
	cp $(BIN_DIR)/$(BINARY_NAME) $(APP_BUNDLE)/Contents/MacOS/
	cp $(BIN_DIR)/tubeless-config $(APP_BUNDLE)/Contents/MacOS/
	sed -e 's|__VERSION__|$(VERSION)|' packaging/darwin/Info.plist > $(APP_BUNDLE)/Contents/Info.plist
	cp $(ICNS) $(APP_BUNDLE)/Contents/Resources/
	ln -sf $(APP_BUNDLE)/Contents/MacOS/$(BINARY_NAME) $(CLI_BIN_DIR)/$(BINARY_NAME)
	ln -sf $(APP_BUNDLE)/Contents/MacOS/tubeless-config $(CLI_BIN_DIR)/tubeless-config
	@echo "Installed $(APP_BUNDLE)"
	@echo "Symlinked $(BINARY_NAME) + tubeless-config into $(CLI_BIN_DIR)"
	@echo "Your config at ~/Library/Application Support/tubeless/config.toml is untouched."

# $(ICNS) is only (re)built when packaging/icon.svg is newer than it —
# make-icns.sh needs macOS-native tooling (sips/qlmanage/iconutil), so this
# rule only ever runs on a real Mac, same constraint as install-darwin itself.
$(ICNS): packaging/icon.svg packaging/darwin/make-icns.sh
	packaging/darwin/make-icns.sh

uninstall-darwin:
	rm -rf $(APP_BUNDLE)
	rm -f $(CLI_BIN_DIR)/$(BINARY_NAME) $(CLI_BIN_DIR)/tubeless-config
	@echo "Removed $(APP_BUNDLE) and its $(CLI_BIN_DIR) symlinks."
	@echo "Your config at ~/Library/Application Support/tubeless/config.toml was left in place."

DIST_DIR = dist
GOARCH_TARGET ?= $(shell go env GOARCH)
NFPM ?= go run github.com/goreleaser/nfpm/v2/cmd/nfpm@latest

# package-linux builds tubeless + tubeless-config for GOARCH_TARGET (the
# host arch by default) and packages them as .deb/.rpm/an Arch pkg (via
# nfpm, see packaging/nfpm.yaml) plus a generic .tar.gz, all under dist/.
# Deliberately doesn't cross-build both amd64 and arm64 in one invocation:
# GLFW's cgo dependency needs a matching cross-toolchain to cross-compile
# reliably, so each arch is packaged on its own native runner in CI
# instead (override GOARCH_TARGET locally if you do have that toolchain).
package-linux:
	@mkdir -p $(DIST_DIR)/linux-$(GOARCH_TARGET)/bin
	GOARCH=$(GOARCH_TARGET) go build -tags wayland -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/linux-$(GOARCH_TARGET)/bin/$(BINARY_NAME) $(CMD_PATH)
	GOARCH=$(GOARCH_TARGET) go build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/linux-$(GOARCH_TARGET)/bin/tubeless-config ./cmd/tubeless-config
	tar -C $(DIST_DIR)/linux-$(GOARCH_TARGET) -czf $(DIST_DIR)/tubeless-$(VERSION)-linux-$(GOARCH_TARGET).tar.gz bin
	sed -e 's|__EXEC__|/usr/bin/$(BINARY_NAME)|' -e 's|__ICON__|tubeless|' \
	    packaging/tubeless.desktop > $(DIST_DIR)/tubeless-$(GOARCH_TARGET).desktop
	sed -e 's|__ARCH__|$(GOARCH_TARGET)|' -e 's|__VERSION__|$(VERSION)|' \
	    -e 's|__BIN_DIR__|$(DIST_DIR)/linux-$(GOARCH_TARGET)/bin|' \
	    -e 's|__DESKTOP__|$(DIST_DIR)/tubeless-$(GOARCH_TARGET).desktop|' \
	    packaging/nfpm.yaml > $(DIST_DIR)/nfpm-$(GOARCH_TARGET).yaml
	@for fmt in deb rpm archlinux; do \
		$(NFPM) package -f $(DIST_DIR)/nfpm-$(GOARCH_TARGET).yaml -p $$fmt -t $(DIST_DIR)/ || exit 1; \
	done
	@echo "Packages written to $(DIST_DIR)/"

# package-darwin builds a Tubeless.app for GOARCH_TARGET (the host arch —
# GLFW's Cocoa backend can't cross-compile, same constraint as
# install-darwin) with its .icns, and zips it into dist/. Run per-arch on
# an actual Mac of that architecture (arm64 and amd64 each need their own
# runner in CI — see .github/workflows/release.yml).
package-darwin: $(ICNS)
	@mkdir -p $(DIST_DIR)
	GOARCH=$(GOARCH_TARGET) go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_PATH)
	GOARCH=$(GOARCH_TARGET) go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/tubeless-config ./cmd/tubeless-config
	@rm -rf $(DIST_DIR)/Tubeless.app
	@mkdir -p $(DIST_DIR)/Tubeless.app/Contents/MacOS $(DIST_DIR)/Tubeless.app/Contents/Resources
	cp $(BIN_DIR)/$(BINARY_NAME) $(DIST_DIR)/Tubeless.app/Contents/MacOS/
	cp $(BIN_DIR)/tubeless-config $(DIST_DIR)/Tubeless.app/Contents/MacOS/
	sed -e 's|__VERSION__|$(VERSION)|' packaging/darwin/Info.plist > $(DIST_DIR)/Tubeless.app/Contents/Info.plist
	cp $(ICNS) $(DIST_DIR)/Tubeless.app/Contents/Resources/
	cd $(DIST_DIR) && zip -qr tubeless-$(VERSION)-darwin-$(GOARCH_TARGET).zip Tubeless.app && rm -rf Tubeless.app
	@echo "Packaged $(DIST_DIR)/tubeless-$(VERSION)-darwin-$(GOARCH_TARGET).zip"

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)
	go clean
	@echo "Cleaned build artifacts"
