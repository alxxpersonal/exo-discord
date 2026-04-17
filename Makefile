.PHONY: help build vet lint test fmt coverage run-bot run-mcp doctor changelog hooks clean install-bin

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

build: ## Build ./exo-discord
	go build -o exo-discord ./cmd/exo-discord/

install-bin: build ## install exo-discord to /usr/local/bin (codesigns for macOS)
	cp exo-discord /usr/local/bin/exo-discord
	@codesign --force --sign - /usr/local/bin/exo-discord 2>/dev/null || true
	@echo "installed - run 'exo-discord' from anywhere"

vet: ## Run go vet
	go vet ./...

lint: ## Run golangci-lint
	golangci-lint run ./...

test: ## Run tests with race detection
	go test -race -count=1 ./...

fmt: ## Format all Go files
	gofmt -w .

coverage: ## Run tests with coverage report
	go test -race -coverprofile=coverage.out -coverpkg=./internal/... ./...
	go tool cover -func=coverage.out | tail -1
	@rm -f coverage.out

run-bot: ## Run bot mode
	go run ./cmd/exo-discord/ bot-mode

run-mcp: ## Run MCP server
	go run ./cmd/exo-discord/ mcp serve

doctor: ## Run diagnostics
	go run ./cmd/exo-discord/ doctor

changelog: ## Generate changelog from conventional commits
	@command -v git-cliff > /dev/null 2>&1 && git-cliff -o CHANGELOG.md || echo "install git-cliff: cargo install git-cliff"

hooks: ## Install git hooks
	@if [ -d .git ]; then cp scripts/pre-commit .git/hooks/pre-commit && chmod +x .git/hooks/pre-commit && echo "pre-commit hook installed"; else echo "no .git directory found"; fi

clean: ## Clean build artifacts
	rm -f exo-discord coverage.out
