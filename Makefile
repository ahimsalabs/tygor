# Root Makefile for tygor
# Tip: run with -j for parallel execution (e.g., make -j precommit)

.PHONY: all test test-quiet lint lint-quiet precommit fmt fmt-check ci-local typecheck-docs typecheck-vite-plugin release help gen

# Default target
all: test lint

# Run tests
test:
	GOWORK=off go test ./...

# Run tests quietly (output only on failure)
test-quiet:
	@output=$$(GOWORK=off go test ./... 2>&1) || (echo "$$output"; exit 1)

# Run linters
lint:
	GOWORK=off go vet ./...
	GOWORK=off go run honnef.co/go/tools/cmd/staticcheck@v0.6.1 ./...

# Run linters quietly (output only on failure)
lint-quiet:
	@output=$$(GOWORK=off go vet ./... 2>&1) || (echo "$$output"; exit 1)
	@output=$$(GOWORK=off go run honnef.co/go/tools/cmd/staticcheck@v0.6.1 ./... 2>&1) || (echo "$$output"; exit 1)

# Format code
fmt:
	gofmt -w .

# Check formatting
fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "Files need formatting:"; gofmt -l .; exit 1)

# Generate client error types from Go source
gen:
	go run ./cmd/tygor gen packages/client/generated -t ErrorCode -t Error -p tygor.dev/tygor

# Type-check documentation examples
typecheck-docs:
	@bun x typescript --noEmit --project doc/examples/tsconfig.json

# Type-check vite plugin
typecheck-vite-plugin:
	@cd packages/vite-plugin && bun run --silent typecheck

# Precommit sub-targets (for parallel execution, all depend on fmt-check)
.PHONY: precommit-test precommit-lint precommit-typecheck precommit-vite-plugin precommit-devserver precommit-client-bundle precommit-version-sync precommit-client-gen
precommit-test: fmt-check ; @$(MAKE) --no-print-directory test-quiet
precommit-lint: fmt-check ; @$(MAKE) --no-print-directory lint-quiet
precommit-typecheck: fmt-check ; @$(MAKE) --no-print-directory typecheck-docs
precommit-vite-plugin: fmt-check ; @$(MAKE) --no-print-directory typecheck-vite-plugin
precommit-devserver: fmt-check
	@go run ./cmd/tygor gen --check -p ./cmd/tygor/internal/dev ./packages/vite-plugin/src/devserver
precommit-client-gen: fmt-check
	@go run ./cmd/tygor gen --check packages/client/generated -t ErrorCode -t Error -p tygor.dev/tygor
precommit-version-sync: fmt-check
	@if ! diff -q VERSION cmd/tygor/VERSION > /dev/null 2>&1; then \
		echo "ERROR: VERSION files are out of sync"; \
		echo "  VERSION:          $$(cat VERSION)"; \
		echo "  cmd/tygor/VERSION: $$(cat cmd/tygor/VERSION)"; \
		echo "Run: cp VERSION cmd/tygor/VERSION"; \
		exit 1; \
	fi
precommit-client-bundle: fmt-check
	@cd packages/vite-plugin && bun run --silent build:client
	@# Check for changes using jj (preferred) or git as fallback
	@if [ -d .jj ]; then \
		changes=$$(jj diff --name-only packages/vite-plugin/src/generated/client-bundle.ts 2>/dev/null | grep -v '^$$' || true); \
	else \
		changes=$$(git diff --name-only packages/vite-plugin/src/generated/client-bundle.ts 2>/dev/null || true); \
	fi; \
	if [ -n "$$changes" ]; then \
		echo ""; \
		echo "ERROR: Client bundle out of sync with packages/vite-plugin/src/client/ sources."; \
		echo "Run 'cd packages/vite-plugin && bun run build' to update."; \
		echo ""; \
		exit 1; \
	fi

# Run all precommit checks in parallel (fmt-check runs first)
precommit: precommit-test precommit-lint precommit-typecheck precommit-vite-plugin precommit-devserver precommit-client-bundle precommit-version-sync precommit-client-gen
	@echo "All precommit checks passed."

# Run CI locally using act (https://github.com/nektos/act)
ci-local:
	go run github.com/nektos/act@latest --container-architecture linux/amd64

# Release packages (usage: make release TYPE=patch|minor|major)
release:
ifndef TYPE
	$(error TYPE is required. Usage: make release TYPE=patch)
endif
	./release.bash $(TYPE)

# Help
help:
	@echo "Available targets:"
	@echo "  make all            - Run tests and lint (default)"
	@echo "  make test           - Run tests"
	@echo "  make lint           - Run go vet and staticcheck"
	@echo "  make fmt            - Format Go code"
	@echo "  make gen            - Generate client error types from Go source"
	@echo "  make typecheck-docs - Type-check documentation examples"
	@echo "  make precommit      - Run all checks (test, lint, typecheck, gen)"
	@echo "  make ci-local       - Run GitHub Actions workflow locally via Docker (requires act)"
	@echo "  make release TYPE=  - Release packages (TYPE: patch|minor|major)"
