SHELL := /bin/bash

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

BIN_DIR := $(CURDIR)/bin
GOLANGCI_LINT_VERSION := v2.12.2

# Демо-проект для проверки шаблонов и либы «как есть» — без тега и пуша:
# в его go.mod прописывается replace на этот каталог, поэтому правки в pkg/
# видны сразу, достаточно пересобрать сам демо-проект.
DEMO_DIR    ?= $(CURDIR)/.demo
DEMO_MODULE ?= github.com/acme/demo

# Каталоги с Go-кодом самого scratchy: gofmt, в отличие от go build,
# заходит и во вложенные модули, а в DEMO_DIR лежит чужой проект.
GO_DIRS := cmd internal pkg

.PHONY: build
build: ## Собрать бинарник scratch в ./bin
	go build -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/scratch ./cmd/scratch

.PHONY: install
install: ## Установить scratch в GOBIN
	go install -ldflags '$(LDFLAGS)' ./cmd/scratch

.PHONY: demo
demo: build ## Сгенерировать проект из текущего кода в ./.demo (DEMO_DIR, DEMO_MODULE, FORCE=1)
	@test -n "$(DEMO_DIR)" || { echo "DEMO_DIR пуст"; exit 1; }
	@if [ -d "$(DEMO_DIR)" ] && [ -n "$$(ls -A '$(DEMO_DIR)')" ]; then \
		if [ ! -f "$(DEMO_DIR)/.scratch.yaml" ]; then \
			echo "$(DEMO_DIR) не пуст и не похож на проект scratch (нет .scratch.yaml)."; \
			echo "Задай другой каталог: make demo DEMO_DIR=/путь"; \
			exit 1; \
		fi; \
		if [ -z "$(FORCE)" ]; then \
			echo "В $(DEMO_DIR) уже есть проект scratch — не перезаписываю, чтобы не потерять правки."; \
			echo "Поверх существующего:  make demo FORCE=1"; \
			echo "С чистого листа:       rm -rf $(DEMO_DIR) && make demo"; \
			exit 1; \
		fi; \
	fi
	# Именно --force, без rm -rf: каталог не пересоздаётся, поэтому не ломается
	# cwd у открытого шелла и иде, а файлы вне шаблонов остаются на месте.
	$(BIN_DIR)/scratch new $(DEMO_MODULE) --dir "$(DEMO_DIR)" --lib-replace "$(CURDIR)" --force

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
	@out="$$(gofmt -l $(GO_DIRS))"; \
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
