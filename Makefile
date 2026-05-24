.PHONY: install-tools lint test test-verbose format build install help

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

install-tools: ## Install linting/formatting tools
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install mvdan.cc/gofumpt@latest
	go install github.com/segmentio/golines@latest

lint: ## Run golangci linter
	golangci-lint run ./...

format: ## Format code
	gofumpt -l -w -extra .
	golines . -w

test: ## Run tests
	go test -race -count=1 -shuffle on -cover ./...

test-verbose: ## Run tests with verbose output
	go test -race -count=1 -shuffle on -v -cover ./...

build: ## Build the gocu binary into ./bin/
	mkdir -p bin
	go build -o bin/gocu ./cmd/gocu

install: ## Install gocu into $GOBIN
	go install ./cmd/gocu
