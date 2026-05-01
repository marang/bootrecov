APP := bootrecov
BIN_DIR := bin
OUT := $(BIN_DIR)/$(APP)
GO_CACHE_DIR ?= /tmp/bootrecov-go-cache
GO_MOD_CACHE_DIR ?= /tmp/bootrecov-go-mod-cache
GO_ENV := GOCACHE=$(GO_CACHE_DIR) GOMODCACHE=$(GO_MOD_CACHE_DIR)

.PHONY: help build run fmt test clean test-bootvm-requirements test-bootvm-prepare test-bootvm test-bootvm-watch

help:
	@echo "Targets:"
	@echo "  make build                Build binary to $(OUT)"
	@echo "  make run                  Run the TUI"
	@echo "  make fmt                  Format Go files"
	@echo "  make test                 Run full test suite (vet + unit + race + coverage)"
	@echo "  make clean                Remove build artifacts"
	@echo "  make test-bootvm-requirements Check host requirements for rootless VM test"
	@echo "  make test-bootvm-prepare  Pre-cache VM assets (optional; test-bootvm auto-runs this when needed)"
	@echo "  make test-bootvm          Run rootless QEMU boot test (auto-prepare + guest smoke test)"
	@echo "  make test-bootvm-watch    Run tests, then open tmux dashboard for QEMU boot test"

build:
	@mkdir -p $(BIN_DIR)
	$(GO_ENV) go build -o $(OUT) ./cmd/bootrecov

run:
	$(GO_ENV) go run ./cmd/bootrecov

fmt:
	gofmt -w cmd/bootrecov/main.go internal/tui/*.go test/bootvm/guest_smoke.go

test:
	$(GO_ENV) go vet ./...
	$(GO_ENV) go test ./...
	$(GO_ENV) go test -race ./...
	$(GO_ENV) go test -cover ./...

clean:
	rm -rf $(BIN_DIR)

test-bootvm-requirements:
	bash test/bootvm/run_rootless_vm_test.sh --check

test-bootvm-prepare: test-bootvm-requirements
	bash test/bootvm/run_rootless_vm_test.sh --prepare

test-bootvm: test-bootvm-requirements
	$(GO_ENV) bash test/bootvm/run_rootless_vm_test.sh

test-bootvm-watch: test
	$(GO_ENV) bash test/bootvm/watch_tmux.sh
