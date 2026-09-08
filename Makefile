.PHONY: build build-x11 tektest test clean install help run-green run-amber

BINARY_NAME=tubeless
BIN_DIR=bin
INSTALL_DIR=$(HOME)/bin
CMD_PATH=./cmd/$(BINARY_NAME)

help:
	@echo "Available targets:"
	@echo "  make build      - Build the tubeless binary (native Wayland on"
	@echo "                    Linux; the 'wayland' build tag is a no-op on"
	@echo "                    macOS/Windows, which always use their own"
	@echo "                    native Cocoa/Win32 backend regardless)"
	@echo "  make build-x11  - Build for X11 instead, for Linux desktops"
	@echo "                    without a Wayland compositor"
	@echo "  make tektest    - Build the TDS-420 demo TUI binary"
	@echo "  make run-green  - Build both and run tubeless (green theme) with tektest"
	@echo "  make run-amber  - Build both and run tubeless (amber theme) with tektest"
	@echo "  make test       - Run tests"
	@echo "  make install    - Build and install to $(INSTALL_DIR)"
	@echo "  make clean      - Remove build artifacts"

build:
	@mkdir -p $(BIN_DIR)
	go build -tags wayland -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_PATH)
	@echo "Built $(BIN_DIR)/$(BINARY_NAME)"

build-x11:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_PATH)
	@echo "Built $(BIN_DIR)/$(BINARY_NAME) (X11)"

tektest:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/tektest ./cmd/tektest
	@echo "Built $(BIN_DIR)/tektest"

run-green: build tektest
	$(BIN_DIR)/$(BINARY_NAME) --theme=green --shell=$(BIN_DIR)/tektest

run-amber: build tektest
	$(BIN_DIR)/$(BINARY_NAME) --theme=amber --shell=$(BIN_DIR)/tektest

test:
	go test -v ./...

install: build
	@mkdir -p $(INSTALL_DIR)
	cp $(BIN_DIR)/$(BINARY_NAME) $(INSTALL_DIR)/
	@echo "Installed $(BINARY_NAME) to $(INSTALL_DIR)"

clean:
	rm -rf $(BIN_DIR)
	go clean
	@echo "Cleaned build artifacts"
