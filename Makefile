# Conductor — build & dev targets.
#
# Targets are POSIX-make compatible (also works under GNU make on Windows / WSL).
# See docs/phases.md for what each phase contributes to the buildable surface.

GO              ?= go
BUN             ?= bun
GOLANGCI_LINT   ?= golangci-lint
DOCKER          ?= docker

BIN_NAME        := conductor
BIN_DIR         := bin
BIN             := $(BIN_DIR)/$(BIN_NAME)

WEB_DIR         := web
WEB_BUILD_DIR   := $(WEB_DIR)/build

LDFLAGS         ?= -s -w
BUILD_FLAGS     ?= -trimpath
PKGS            := ./...

.PHONY: all build dev test lint embed-web docker clean web-deps web-build help

all: build

help:
	@echo "Conductor build targets:"
	@echo "  make build       Build single binary with embedded web assets"
	@echo "  make dev         Run Go backend + bun dev concurrently"
	@echo "  make test        Run go test ./..."
	@echo "  make lint        Run golangci-lint over the module"
	@echo "  make embed-web   Build the SvelteKit dashboard and embed it"
	@echo "  make docker      Build the Conductor Docker image"
	@echo "  make clean       Remove build artifacts"

# ----------------------------------------------------------------------------
# Backend build (depends on embedded web assets).
# ----------------------------------------------------------------------------

build: embed-web
	@mkdir -p $(BIN_DIR)
	$(GO) build $(BUILD_FLAGS) -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/conductor
	@echo "Built $(BIN)"

# ----------------------------------------------------------------------------
# Dev mode: backend + frontend in parallel.
# ----------------------------------------------------------------------------

dev:
	@echo "Starting Go backend and bun dev concurrently. Ctrl+C to stop."
	@$(MAKE) -j2 dev-backend dev-frontend

dev-backend:
	$(GO) run ./cmd/conductor start --log-level debug

dev-frontend:
	cd $(WEB_DIR) && $(BUN) run dev

# ----------------------------------------------------------------------------
# Tests.
# ----------------------------------------------------------------------------

test:
	$(GO) test -count=1 -race -timeout 120s $(PKGS)

# ----------------------------------------------------------------------------
# Lint.
# ----------------------------------------------------------------------------

lint:
	$(GOLANGCI_LINT) run --timeout=5m $(PKGS)

# ----------------------------------------------------------------------------
# Frontend.
# ----------------------------------------------------------------------------

web-deps:
	cd $(WEB_DIR) && $(BUN) install

web-build: web-deps
	cd $(WEB_DIR) && $(BUN) run build

# Embed-web builds the SvelteKit assets into web/build/. The Go embed
# directive in internal/api consumes that directory at compile time.
embed-web: web-build
	@test -d $(WEB_BUILD_DIR) || (echo "web build output missing"; exit 1)
	@echo "Web assets ready at $(WEB_BUILD_DIR)"

# ----------------------------------------------------------------------------
# Docker.
# ----------------------------------------------------------------------------

docker:
	$(DOCKER) build -t conductor:latest .

# ----------------------------------------------------------------------------
# Cleanup.
# ----------------------------------------------------------------------------

clean:
	rm -rf $(BIN_DIR) $(WEB_BUILD_DIR) $(WEB_DIR)/.svelte-kit
