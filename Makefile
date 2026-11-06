.DEFAULT_GOAL := help
.PHONY: help deps test lint tooling-test tooling-lint fix scan update all clean docs docs-check

UV ?= uv
LINT_UV := $(UV) run --extra dev
LINT_RUFF_PATHS := .
LINT_TY_PATHS := scripts/

all: help

clean:
	@echo "No clean step defined for avr."

help: ## Show available targets
	@echo "avr - Available targets:"
	@echo "  help             - Show this help message"
	@echo "  go-check         - Test, lint and scan the native CLI"
	@echo "  go-build         - Build the native CLI in build/go/avr"
	@echo "  go-package       - Build and validate a native CLI archive"
	@echo "  test             - Run unit tests (fast, no Docker)"
	@echo "  lint             - Check for linting issues"
	@echo "  fix              - Auto-fix linting issues"
	@echo "  scan             - Scan for vulnerabilities"
	@echo "  update           - Update dependencies"
	@echo "  docs             - Regenerate docs/REFERENCE.md and docs/reference.json from source"
	@echo "  docs-check       - Fail if committed docs are stale (CI gate)"

deps: ## Install/sync dependencies (including dev extras)
	@echo "📦 Installing avr dependencies..."
	@$(UV) sync --extra dev
	@$(GO) mod download
	@git config core.hooksPath .githooks
	@echo "✅ avr dependencies installed"

test: go-test tooling-test go-integration ## Run native, tooling and integration tests

tooling-test: ## Test packaging and developer tools
	@$(UV) run --python 3.14 --frozen --extra dev python -m pytest scripts/test_go_package.py scripts/test_go_distribution.py scripts/test_go_publish.py scripts/test_go_release.py scripts/test_go_schema_fetch.py -q

lint: go-lint tooling-lint ## Lint Go and Python tooling

tooling-lint: ## Check for linting issues and formatting problems
	@$(LINT_UV) ruff check --output-format=concise $(LINT_RUFF_PATHS)
	@$(LINT_UV) ruff format --check $(LINT_RUFF_PATHS)
	@bash -c 'set -o pipefail && $(LINT_UV) ty check --output-format=concise $(LINT_TY_PATHS) 2>&1 | sed "/WARN ty is pre-release software/d"'
	@echo "✅ avr lint passed"

fix: ## Auto-fix linting issues and format code
	@$(LINT_UV) ruff format $(LINT_RUFF_PATHS)
	@$(LINT_UV) ruff check --fix $(LINT_RUFF_PATHS)
	@bash -c 'set -o pipefail && $(LINT_UV) ty check --output-format=concise $(LINT_TY_PATHS) 2>&1 | sed "/WARN ty is pre-release software/d"'
	@echo "✅ avr code fixed and formatted"

scan: go-scan ## Scan native dependencies for vulnerabilities

docs: go-docs ## Regenerate reference docs from the native command tree

docs-check: go-docs ## Fail if committed docs differ from native generation
	@changes=$$(git status --porcelain --untracked-files=all -- README.md docs/REFERENCE.md docs/reference.json docs/starlight/ man/) || exit $$?; \
	if [ -n "$$changes" ]; then \
		echo "avr docs are stale: run make docs and commit the result." >&2; \
		printf '%s\n' "$$changes" >&2; \
		exit 1; \
	fi

update: ## Update dependencies
	@echo "📦 Updating avr dependencies..."
	@$(UV) lock --upgrade
	@$(UV) sync
	@echo "✅ avr dependencies updated"

# Native CLI. Python is used only for development and packaging tools.
GO ?= go

.PHONY: go-build go-test go-lint go-integration go-scan go-check

go-build: ## Build the native CLI and bundled license notices
	@CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-X main.commit=$(shell git rev-parse --short HEAD)" -o build/go/avr ./cmd/avr
	@$(UV) run --python 3.14 --frozen --extra dev python scripts/go_notices.py --go "$(GO)"

go-test:
	@$(GO) test -race ./...

go-lint:
	@$(GO) vet ./...
	@$(GO) tool -modfile=tools/go.mod golangci-lint run ./...

go-integration: go-build
	@AVR_GO_BINARY=build/go/avr $(UV) run --python 3.14 --frozen --extra dev python -m pytest scripts/test_go_clone.py scripts/test_go_billing_card.py scripts/test_go_vm_ssh.py scripts/test_go_vm_tunnels.py scripts/test_go_vm_bootstrap.py scripts/test_go_docs.py scripts/test_go_completion.py -q

go-scan:
	@$(GO) tool -modfile=tools/go.mod govulncheck ./...

go-check: go-test go-lint go-integration go-scan

.PHONY: go-fetch-schema
go-fetch-schema: ## Fetch current OpenAPI for API inspection
	@$(UV) run --python 3.14 --frozen python scripts/fetch_go_schema.py $(if $(AVREA_CORE),--core "$(AVREA_CORE)",$(if $(OPENAPI_URL),--url "$(OPENAPI_URL)",))

.PHONY: go-api-check
go-api-check: go-fetch-schema ## Check representative command requests against current OpenAPI
	@AVR_TEST_OPENAPI="$(CURDIR)/build/go/openapi.json" $(GO) test ./commands -run '^TestAPIContract' -count=1 -v

.PHONY: go-docs
go-docs: go-build ## Generate the public reference with the native CLI
	@./build/go/avr internal docs --format all --out . --readme README.md

.PHONY: go-package
go-package: ## Build and validate a native CLI archive
	@$(UV) run --python 3.14 --frozen python scripts/go_package.py --go "$(GO)"
