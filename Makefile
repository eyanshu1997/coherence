SHELL := /bin/bash
COHERENCE_HOME := $(shell pwd)
BIN_DIR := $(COHERENCE_HOME)/bin
ASSET_VER := v28

.PHONY: build test test-unit test-e2e clean generate-golden

build:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -ldflags="-s -w -X 'coherence/internal/docgen.AssetVer=$(ASSET_VER)'" -o $(BIN_DIR)/coherence-server   ./cmd/coherence-server
	CGO_ENABLED=0 go build -ldflags="-s -w -X 'coherence/internal/docgen.AssetVer=$(ASSET_VER)'" -o $(BIN_DIR)/coherence-doc      ./cmd/coherence-doc
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BIN_DIR)/coherence-bash-log ./cmd/coherence-bash-log
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BIN_DIR)/coherence-mcp-log  ./cmd/coherence-mcp-log
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BIN_DIR)/coherence-patterns ./cmd/coherence-patterns
	@echo "Built: $$(ls $(BIN_DIR) | tr '\n' ' ')"

test-unit:
	go test ./internal/... ./cmd/...

test-e2e: build
	go test ./tests/e2e/... -timeout 120s -v

test: test-unit test-e2e

generate-golden:
	@echo "Golden file generation requires the Python renderer for comparison."
	@echo "Run: python3 tests/gen_golden.py (needs python-dotenv in a venv)"

clean:
	rm -rf $(BIN_DIR)
