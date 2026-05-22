.DEFAULT_GOAL := help
.PHONY: help deps test lint fix scan update all clean docs docs-check

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
	@syft scan . --source-name avr-cli --source-version $(VERSION) -o syft-json | grype --fail-on critical -o table
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

update: ## Update dependencies
	@echo "📦 Updating avr dependencies..."
	@$(UV) lock --upgrade
	@$(UV) sync
	@echo "✅ avr dependencies updated"
