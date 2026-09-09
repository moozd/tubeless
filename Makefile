.PHONY: build build-x11 tubeless tubeless-config tektest test clean install help run-green run-amber run-config

BINARY_NAME=tubeless
BIN_DIR=bin
INSTALL_DIR=$(HOME)/bin
CMD_PATH=./cmd/$(BINARY_NAME)

help:
	@echo "Available targets:"
	@echo "  make build      - Build everything: tubeless (native Wayland on"
	@echo "                    Linux; the 'wayland' build tag is a no-op on"
	@echo "                    macOS/Windows, which always use their own"
	@echo "                    native Cocoa/Win32 backend regardless),"
	@echo "                    tubeless-config, and tektest"
	@echo "  make build-x11  - Build tubeless for X11 instead, for Linux"
	@echo "                    desktops without a Wayland compositor"
	@echo "  make run-green  - Build and run tubeless (green theme) with tektest"
	@echo "  make run-amber  - Build and run tubeless (amber theme) with tektest"
	@echo "  make run-config - Run 'tubeless config' (the in-terminal settings UI)"
	@echo "  make test       - Run tests"
	@echo "  make install    - Build and install to $(INSTALL_DIR)"
	@echo "  make clean      - Remove build artifacts"

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

install: build
	@mkdir -p $(INSTALL_DIR)
	cp $(BIN_DIR)/$(BINARY_NAME) $(INSTALL_DIR)/
	cp $(BIN_DIR)/tubeless-config $(INSTALL_DIR)/
	@echo "Installed $(BINARY_NAME) + tubeless-config to $(INSTALL_DIR)"

clean:
	rm -rf $(BIN_DIR)
	go clean
	@echo "Cleaned build artifacts"
