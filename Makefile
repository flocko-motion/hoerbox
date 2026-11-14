BIN := bin/hoerbox
INSTALL_DIR := $(HOME)/Music/hoerbox
APP_DIR := $(HOME)/Applications
APP_ID := net.omnitopos.hoerbox

.DEFAULT_GOAL := build

# Fyne and GLFW each pass -lobjc to cgo; Apple's linker (Xcode 15+) warns
# about the duplicate. It's harmless, so silence it on macOS.
ifeq ($(shell uname),Darwin)
export CGO_LDFLAGS += -Wl,-no_warn_duplicate_libraries
endif

.PHONY: build bundle install vet fmt lint check clean help

build: ## Build the hoerbox binary to bin/hoerbox in this repo (default target) — doesn't install anything
	@mkdir -p bin
	@echo "==> building $(BIN)"
	go build -o $(BIN) .
	@echo "==> built $(BIN) — run it as ./$(BIN), or \"make install\" to install it"

bundle: build ## macOS only: package hoerbox.app here — a real double-clickable app, not a bare binary
	@echo "==> packaging hoerbox.app (fyne package)"
	go run fyne.io/tools/cmd/fyne@latest package --os darwin --exe $(BIN) --name hoerbox --app-id $(APP_ID) --icon Icon.png
	# fyne package carries over the plain binary's own ad-hoc signature as-is,
	# which doesn't cover Info.plist/Resources as a bundle signature must —
	# Finder silently refuses to launch that on Apple Silicon. Re-sign the
	# whole bundle properly.
	@echo "==> signing hoerbox.app"
	codesign --force --deep -s - hoerbox.app

# bundle (not build) on macOS: it builds first anyway, and depending on
# both would run the build twice.
ifeq ($(shell uname),Darwin)
install: bundle
else
install: build
endif
install: ## Build, then install the CLI to ~/Music/hoerbox/bin and (macOS) hoerbox.app to ~/Applications
	@echo "==> installing the CLI to $(INSTALL_DIR)/bin/hoerbox"
	@mkdir -p $(INSTALL_DIR)/bin
	cp $(BIN) $(INSTALL_DIR)/bin/hoerbox
ifeq ($(shell uname),Darwin)
	@echo "==> installing hoerbox.app to $(APP_DIR)"
	@mkdir -p $(APP_DIR)
	rm -rf $(APP_DIR)/hoerbox.app
	mv hoerbox.app $(APP_DIR)/
endif
	@echo "Installed."
	@echo
	@echo "$(INSTALL_DIR) is your hoerbox project dir — in/, out/, and data/"
	@echo "live there, alongside the CLI binary in bin/."
	@echo
ifeq ($(shell uname),Darwin)
	@echo "  GUI: double-click $(APP_DIR)/hoerbox.app in Finder"
	@echo "       (deliberately kept out of $(INSTALL_DIR) — ~/Music is one"
	@echo "       of macOS's specially-protected folders, and Finder"
	@echo "       silently refused to launch anything installed inside it;"
	@echo "       the app itself still reads and writes its library at"
	@echo "       $(INSTALL_DIR) no matter where you move the .app to)"
else
	@echo "  GUI: run $(INSTALL_DIR)/bin/hoerbox with no arguments"
endif
	@echo "  CLI: cd $(INSTALL_DIR) && bin/hoerbox stream ..."

vet: ## Run go vet
	go vet ./...

fmt: ## Check gofmt formatting (fails if any file needs formatting)
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)

lint: ## Run brokkr's lint suite
	brokkr lint

check: vet fmt lint ## Run vet, fmt check, and lint together

clean: ## Remove build artifacts
	rm -rf bin hoerbox.app

help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-8s\033[0m %s\n", $$1, $$2}'
