SHELL := /bin/bash

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

BIN_DIR := $(CURDIR)/bin
GOLANGCI_LINT_VERSION := v2.12.2

.PHONY: build
build: ## Собрать бинарник scratch в ./bin
	go build -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/scratch ./cmd/scratch

.PHONY: install
install: ## Установить scratch в GOBIN
	go install -ldflags '$(LDFLAGS)' ./cmd/scratch

.PHONY: test
test: ## Юнит-тесты
	go test -race -count=1 ./...

.PHONY: lint
lint: ## Линтер (требует golangci-lint)
	GOBIN=$(BIN_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	$(BIN_DIR)/golangci-lint run ./...

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "%-12s %s\n", $$1, $$2}'
