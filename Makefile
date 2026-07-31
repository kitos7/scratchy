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
	go test -race -count=1 -cover ./...

.PHONY: e2e
e2e: ## Сквозной тест: сгенерированный проект собирается (нужна сеть, минуты)
	go test -tags e2e -count=1 -timeout 30m -v ./internal/generator/

.PHONY: tools
tools: $(BIN_DIR)/golangci-lint ## Инструменты в ./bin

$(BIN_DIR)/golangci-lint:
	GOBIN=$(BIN_DIR) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: lint
lint: $(BIN_DIR)/golangci-lint ## Линтер
	$(BIN_DIR)/golangci-lint run ./...

.PHONY: fmt-check
fmt-check: ## Проверить форматирование
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "gofmt требуется в:"; echo "$$out"; exit 1; fi

.PHONY: check
check: fmt-check vet test lint ## Всё, что должно быть зелёным перед коммитом

.PHONY: vet
vet: ## go vet
	go vet ./...

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "%-12s %s\n", $$1, $$2}'
