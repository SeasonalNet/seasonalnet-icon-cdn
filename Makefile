SHELL := /bin/sh
.SHELLFLAGS := -eu -c

GO ?= go
PNPM ?= pnpm
UV ?= uv
GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
GOVULNCHECK ?= $(GO) run golang.org/x/vuln/cmd/govulncheck@v1.8.0
BINARY ?= bin/icon-cdn
COVERAGE_MIN ?= 90
override COVERAGE_BASELINE := 90
IMAGE ?= seasonalnet-icon-cdn:local

.DEFAULT_GOAL := help
.PHONY: help setup sync-icons check-lucide fmt fmt-check lint vet test test-only test-race coverage \
	lint-only vet-only test-race-only coverage-only vuln vuln-only openapi-check quality-checks quality check manual-check ci \
	build build-only manual-build dev package-check check-js anti-quality test-anti-quality container container-check

help: ## List common development and quality targets
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_-]+:.*##/ {printf "%-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

setup: ## Install pinned asset tooling and generate embedded Lucide assets
	$(PNPM) install --frozen-lockfile
	$(PNPM) sync-icons

sync-icons: ## Generate embedded Lucide assets from the pinned package
	$(PNPM) sync-icons

check-lucide: ## Verify embedded assets match the pinned Lucide package
	$(MAKE) setup
	$(PNPM) check-lucide

fmt: ## Format Go source files
	gofmt -w cmd internal

fmt-check: ## Fail if Go source files are not gofmt-clean
	@files="$$(gofmt -l cmd internal)"; \
	if [ -n "$$files" ]; then printf 'gofmt required:\n%s\n' "$$files"; exit 1; fi; \
	printf 'gofmt: clean\n'

lint: ## Run the pinned golangci-lint standard linter set
	$(MAKE) setup
	$(MAKE) lint-only

lint-only:
	$(GOLANGCI_LINT) run ./...

vet: ## Run go vet across all packages
	$(MAKE) setup
	$(MAKE) vet-only

vet-only:
	$(GO) vet ./...

test: ## Prepare assets and run all unit tests
	$(MAKE) setup
	$(MAKE) test-only

test-only: ## Run unit tests with existing generated assets
	$(GO) test ./...

test-race: ## Run all Go tests with the race detector
	$(MAKE) setup
	$(MAKE) test-race-only

test-race-only:
	$(GO) test -race ./...

coverage: ## Enforce the internal/cdn statement coverage baseline
	$(MAKE) setup
	$(MAKE) coverage-only

coverage-only:
	@awk -v minimum="$(COVERAGE_MIN)" -v baseline="$(COVERAGE_BASELINE)" \
		'BEGIN { if (minimum + 0 < baseline + 0) { printf "coverage threshold %.1f%% cannot be below the checked-in %.1f%% baseline\n", minimum, baseline; exit 1 } }'
	@profile="$$(mktemp)"; trap 'rm -f "$$profile"' EXIT HUP INT TERM; \
	$(GO) test -coverprofile="$$profile" ./internal/cdn; \
	actual="$$($(GO) tool cover -func="$$profile" | awk '/^total:/ {gsub(/%/, "", $$3); print $$3}')"; \
	awk -v actual="$$actual" -v minimum="$(COVERAGE_MIN)" \
		'BEGIN { if (actual + 0 < minimum + 0) { printf "coverage %.1f%% is below the %.1f%% baseline\n", actual, minimum; exit 1 } printf "coverage %.1f%% meets the %.1f%% baseline\n", actual, minimum }'

vuln: ## Scan reachable Go dependencies for known vulnerabilities
	$(MAKE) setup
	$(MAKE) vuln-only

vuln-only:
	$(GOVULNCHECK) -show verbose ./...

openapi-check: ## Validate the OpenAPI 3.2 contract against its specification
	$(UV) run --locked --group ci python -m openapi_spec_validator openapi.yaml

quality-checks: ## Run formatting, lint, vet, unit, race, coverage, vulnerability, API, and asset checks
	$(MAKE) fmt-check
	$(MAKE) test-anti-quality
	$(MAKE) anti-quality
	$(MAKE) openapi-check
	$(MAKE) lint-only
	$(MAKE) vet-only
	$(MAKE) vuln-only
	$(MAKE) test-only
	$(MAKE) test-race-only
	$(MAKE) coverage-only
	$(PNPM) check-lucide

quality: ## Prepare the workspace and run the complete quality gate
	$(MAKE) setup
	$(MAKE) quality-checks

check: quality ## Alias for the complete local quality gate

manual-check: check ## Run the complete manual check sequence

build: ## Prepare assets and build the Go executable
	$(MAKE) setup
	$(MAKE) build-only

build-only: ## Build the Go executable with existing generated assets
	mkdir -p "$(dir $(BINARY))"
	$(GO) build -trimpath -o "$(BINARY)" ./cmd/icon-cdn

manual-build: build ## Run the manual asset sync and executable build sequence

dev: ## Prepare assets and run the service from source
	$(MAKE) setup
	$(GO) run ./cmd/icon-cdn

package-check: ## Check pnpm policy and direct dependency state
	$(PNPM) ignored-builds
	$(PNPM) peers check
	$(PNPM) list --depth 0

check-js: ## Parse-check repository JavaScript files
	find . -path './node_modules' -prune -o -path './.git' -prune -o -type f -name '*.js' -print0 | xargs -0 -r -n1 node --check

anti-quality: ## Reject Go and golangci-lint quality-check suppressions
	node tools/check-quality-suppressions.js

test-anti-quality: ## Unit-test the quality suppression finder
	node tools/check-quality-suppressions.test.js

ci: ## Run quality, dependency, OpenAPI, vulnerability, build, and container smoke gates
	$(MAKE) setup
	$(MAKE) package-check
	$(MAKE) quality-checks
	$(MAKE) build-only
	$(MAKE) container-check
	$(MAKE) check-js

container: ## Build the local multi-stage container image
	docker buildx build --load --tag "$(IMAGE)" .

container-check: container ## Build and exercise the runtime image over HTTP
	@set -eu; \
	name="seasonalnet-icon-cdn-smoke-$$$$"; \
	cleanup() { status=$$?; if [ "$$status" -ne 0 ]; then docker logs "$$name" >&2 || true; fi; docker rm --force "$$name" >/dev/null 2>&1 || true; exit "$$status"; }; \
	trap cleanup EXIT HUP INT TERM; \
	docker run --detach --rm --name "$$name" --publish 127.0.0.1::3600 "$(IMAGE)" >/dev/null; \
	port="$$(docker port "$$name" 3600/tcp | sed 's/.*://')"; \
	CDN_SMOKE_BASE_URL="http://127.0.0.1:$$port" node tools/container-smoke.js
