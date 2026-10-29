.DEFAULT_GOAL := help
.PHONY: help deps test lint fix scan update all clean docs docs-check sbom

UV ?= uv
LINT_UV := $(UV) run --extra dev
LINT_RUFF_PATHS := .
LINT_TY_PATHS := avrea_cli/ tests/
VERSION := $(shell sed -n 's/^version = "\(.*\)"/\1/p' pyproject.toml | head -n 1)

all: help

clean:
	@echo "No clean step defined for avr."

help: ## Show available targets
	@echo "avr - Available targets:"
	@echo "  help             - Show this help message"
	@echo "  go-check         - Test, lint, compare and scan the Go preview"
	@echo "  go-build         - Build the Go preview in build/go/avr"
	@echo "  test             - Run unit tests (fast, no Docker)"
	@echo "  lint             - Check for linting issues"
	@echo "  fix              - Auto-fix linting issues"
	@echo "  scan             - Scan for vulnerabilities"
	@echo "  update           - Update dependencies"
	@echo "  docs             - Regenerate docs/REFERENCE.md and docs/reference.json from source"
	@echo "  docs-check       - Fail if committed docs are stale (CI gate)"
	@echo "  sbom             - Regenerate the committed OSS SBOM under .oss-report/"

deps: ## Install/sync dependencies (including dev extras)
	@echo "📦 Installing avr dependencies..."
	@$(UV) sync --extra dev
	@git config core.hooksPath .githooks
	@echo "✅ avr dependencies installed"

test: ## Run unit tests (fast, no Docker)
	@echo "Running avr unit tests..."
	@$(UV) run --extra dev python -m pytest tests -v
	@echo "avr unit tests completed"

lint: ## Check for linting issues and formatting problems
	@$(LINT_UV) ruff check --output-format=concise $(LINT_RUFF_PATHS)
	@$(LINT_UV) ruff format --check $(LINT_RUFF_PATHS)
	@bash -c 'set -o pipefail && $(LINT_UV) ty check --output-format=concise $(LINT_TY_PATHS) 2>&1 | sed "/WARN ty is pre-release software/d"'
	@echo "✅ avr lint passed"

fix: ## Auto-fix linting issues and format code
	@$(LINT_UV) ruff format $(LINT_RUFF_PATHS)
	@$(LINT_UV) ruff check --fix $(LINT_RUFF_PATHS)
	@bash -c 'set -o pipefail && $(LINT_UV) ty check --output-format=concise $(LINT_TY_PATHS) 2>&1 | sed "/WARN ty is pre-release software/d"'
	@echo "✅ avr code fixed and formatted"

scan: ## Scan for vulnerabilities
	@echo "🔍 Scanning avr for vulnerabilities..."
	@syft scan . --source-name avr-cli --source-version $(VERSION) -o syft-json | grype --fail-on medium -o table
	@echo "✅ avr vulnerability scan complete"

docs: ## Regenerate reference docs from the live Click tree
	@echo "📝 Regenerating avr docs..."
	@$(UV) run --extra dev avr internal docs --format all --out .
	@echo "✅ avr docs regenerated"

docs-check: ## Fail if committed docs differ from a fresh regeneration
	@echo "🔍 Checking that avr docs are up to date..."
	@$(UV) run --extra dev avr internal docs --format all --out . >/dev/null
	@if ! git diff --quiet -- docs/ man/; then \
		echo "❌ avr docs are stale — run 'make docs' and commit the result." >&2; \
		git --no-pager diff -- docs/ man/ >&2; \
		exit 1; \
	fi
	@echo "✅ avr docs are up to date"

# TODO: move SBOM generation into CI at release time so contributors don't need
# the internal oss-report tool installed locally.
sbom: ## Regenerate the committed OSS SBOM under .oss-report/
	@command -v oss-report >/dev/null 2>&1 || { echo "❌ oss-report not on PATH; uv tool install it from the Avrea internal tooling repo" >&2; exit 1; }
	@echo "📦 Regenerating avr SBOM (keep oss-report.yaml version in sync with pyproject)..."
	@oss-report generate --sbom-format both --output-dir .oss-report
	@echo "✅ avr SBOM regenerated"

update: ## Update dependencies
	@echo "📦 Updating avr dependencies..."
	@$(UV) lock --upgrade
	@$(UV) sync
	@echo "✅ avr dependencies updated"

# Go preview. The existing Python build and distribution remain active.
GO ?= go

.PHONY: go-build go-test go-lint go-parity go-scan go-check

go-build: ## Build the native Go preview and bundled license notices
	@$(GO) build -trimpath -ldflags "-X main.commit=$(shell git rev-parse --short HEAD)" -o build/go/avr ./cmd/avr
	@$(UV) run --python 3.14 --frozen --extra dev python scripts/go_notices.py --go "$(GO)"

go-test:
	@$(GO) test -race ./...

go-lint:
	@$(GO) vet ./...
	@$(GO) tool -modfile=tools/go.mod golangci-lint run ./...

go-parity: go-build
	@$(UV) run --python 3.14 --frozen --extra dev python scripts/setup_parity_jq.py
	@$(UV) run --python 3.14 --frozen --extra dev python scripts/compare_auth.py --go build/go/avr
	@$(UV) run --python 3.14 --frozen --extra dev python -m scripts.compare_inspection --go build/go/avr
	@$(UV) run --python 3.14 --frozen --extra dev python -m scripts.compare_jobs --go build/go/avr

go-scan:
	@$(GO) tool -modfile=tools/go.mod govulncheck ./...

go-check: go-test go-lint go-parity go-generation-check go-scan

.PHONY: go-generate go-generation-check go-schema-gate
go-generate:
	@$(UV) run --python 3.14 --frozen --extra dev python scripts/generate_go_api.py --go "$(GO)"

go-generation-check:
	@$(UV) run --python 3.14 --frozen --extra dev python scripts/generate_go_api.py --go "$(GO)" --check

go-schema-gate:
	@test -n "$(AVREA_CORE)" || { echo "Set AVREA_CORE to the avrea-core checkout" >&2; exit 1; }
	@$(UV) run --python 3.14 --frozen --extra dev python scripts/generate_go_api.py --go "$(GO)" --check --core "$(AVREA_CORE)"
