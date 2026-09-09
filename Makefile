.PHONY: build build-x11 tubeless tubeless-config tektest test clean \
        install uninstall install-darwin uninstall-darwin help \
        run-green run-amber run-config

BINARY_NAME=tubeless
BIN_DIR=bin
CMD_PATH=./cmd/$(BINARY_NAME)

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
	@echo "                        (default $(APP_DIR))"
	@echo "  make uninstall-darwin - Remove the installed Tubeless.app bundle."
	@echo "  make clean          - Remove build artifacts"

build: tubeless tubeless-config tektest

build-x11:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_PATH)
	@echo "Built $(BIN_DIR)/$(BINARY_NAME) (X11)"

tubeless:
	@mkdir -p $(BIN_DIR)
	go build -tags wayland -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_PATH)
	@echo "Built $(BIN_DIR)/$(BINARY_NAME)"

# tubeless-config is the in-terminal settings UI; `tubeless config` runs it
# directly in the current terminal (see cmd/tubeless/main.go), no window.
tubeless-config:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/tubeless-config ./cmd/tubeless-config
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

# install-darwin builds a minimal Tubeless.app bundle. Must be run on an
# actual Mac: GLFW's macOS backend needs the real Cocoa/OpenGL frameworks,
# which can't be cross-compiled from this Makefile's usual Linux host —
# see packaging/darwin/Info.plist's own notes on why there's no .icns yet.
install-darwin: tubeless tubeless-config
	@mkdir -p $(APP_BUNDLE)/Contents/MacOS $(APP_BUNDLE)/Contents/Resources
	cp $(BIN_DIR)/$(BINARY_NAME) $(APP_BUNDLE)/Contents/MacOS/
	cp $(BIN_DIR)/tubeless-config $(APP_BUNDLE)/Contents/MacOS/
	cp packaging/darwin/Info.plist $(APP_BUNDLE)/Contents/
	@echo "Installed $(APP_BUNDLE)"
	@echo "Your config at ~/Library/Application Support/tubeless/config.toml is untouched."

uninstall-darwin:
	rm -rf $(APP_BUNDLE)
	@echo "Removed $(APP_BUNDLE)."
	@echo "Your config at ~/Library/Application Support/tubeless/config.toml was left in place."

clean:
	rm -rf $(BIN_DIR)
	go clean
	@echo "Cleaned build artifacts"
