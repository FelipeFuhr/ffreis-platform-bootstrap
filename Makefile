BINARY     := platform-bootstrap
BUILD_DIR  := ./bin
MODULE     := github.com/ffreis/platform-bootstrap

GOFMT         ?= gofmt
GOLANGCI_LINT ?= golangci-lint
GITLEAKS      ?= gitleaks
GOVULNCHECK   ?= govulncheck
COVERAGE_MIN  ?= 80

LEFTHOOK_VERSION ?= 1.7.10
LEFTHOOK_DIR     ?= $(CURDIR)/.bin
LEFTHOOK_BIN     ?= $(LEFTHOOK_DIR)/lefthook

# Build flags: embed version info from git at compile time.
GIT_COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_TAG     := $(shell git describe --tags --exact-match 2>/dev/null || echo "dev")
BUILD_TIME  := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS     := -ldflags "-X $(MODULE)/cmd.version=$(GIT_TAG) \
                          -X $(MODULE)/cmd.commit=$(GIT_COMMIT) \
                          -X $(MODULE)/cmd.buildTime=$(BUILD_TIME)"

.PHONY: all build clean test test-verbose test-race fmt fmt-check lint tidy \
        coverage-gate smoke-check secrets-scan-staged quality-gates hook-generated-drift \
        lefthook-bootstrap lefthook-install lefthook-run lefthook \
        run-init run-init-dry

all: tidy build

## build: compile the binary into ./bin/
build:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) .
	@echo "built $(BUILD_DIR)/$(BINARY)"

## clean: remove build artefacts
clean:
	rm -rf $(BUILD_DIR)

## test: run all tests
test:
	go test ./...

## test-verbose: run all tests with verbose output
test-verbose:
	go test -v ./...

## fmt: format all Go source files
fmt:
	$(GOFMT) -w .

## fmt-check: fail if Go files are not gofmt-formatted
fmt-check:
	@./scripts/hooks/check_required_tools.sh $(GOFMT)
	@out="$$(find . -type f -name '*.go' -not -path './vendor/*' -not -path './.git/*' -print0 | xargs -0 -r $(GOFMT) -l)"; \
	if [ -n "$$out" ]; then \
		echo "Unformatted Go files:"; \
		echo "$$out"; \
		echo "Run: make fmt"; \
		exit 1; \
	fi

## lint: run golangci-lint
lint:
	@command -v $(GOLANGCI_LINT) >/dev/null 2>&1 || (echo "Missing tool: $(GOLANGCI_LINT). Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest" && exit 1)
	$(GOLANGCI_LINT) run ./...

## tidy: resolve and pin all dependencies, update go.sum
tidy:
	go mod tidy

## test-race: run tests with race detector
test-race:
	go test -race ./...

## coverage-gate: run tests with coverage; fail if below COVERAGE_MIN
coverage-gate:
	@COVERAGE_MIN="$(COVERAGE_MIN)" ./scripts/hooks/check_coverage_gate.sh

## smoke-check: build binary and verify --help exits cleanly
smoke-check:
	@set -euo pipefail; \
	tmp_bin="$$(mktemp)"; \
	trap 'rm -f "$$tmp_bin"' EXIT; \
	go build $(LDFLAGS) -o "$$tmp_bin" . && "$$tmp_bin" --help >/dev/null

## secrets-scan-staged: scan staged diff for secrets
secrets-scan-staged:
	@command -v $(GITLEAKS) >/dev/null 2>&1 || (echo "Missing tool: $(GITLEAKS). Install: https://github.com/gitleaks/gitleaks#installing" && exit 1)
	$(GITLEAKS) protect --staged --redact

## quality-gates: strict pre-push checks (tests + race + coverage + vulncheck + smoke)
quality-gates:
	@command -v $(GOVULNCHECK) >/dev/null 2>&1 || (echo "Missing tool: $(GOVULNCHECK). Install with: go install golang.org/x/vuln/cmd/govulncheck@latest" && exit 1)
	$(MAKE) test
	$(MAKE) test-race
	$(MAKE) coverage-gate
	$(GOVULNCHECK) ./...
	$(MAKE) smoke-check

## hook-generated-drift: run generate target if present and fail on uncommitted changes
hook-generated-drift:
	@set -euo pipefail; \
	if $(MAKE) -n generate >/dev/null 2>&1; then \
		$(MAKE) generate; \
		if ! git diff --quiet -- .; then \
			echo "Generated files are out of date. Run 'make generate' and commit updates."; \
			git status --short; \
			exit 1; \
		fi; \
	else \
		echo "No 'generate' target found; skipping generated drift check."; \
	fi

# ---------------------------------------------------------------------------
# Local run targets — require ORG, PROFILE, and ROOT_EMAIL to be set.
# Example:
#   make run-init ORG=acme PROFILE=bootstrap ROOT_EMAIL=root@acme.example.com
# ---------------------------------------------------------------------------

## run-init: execute `init` against real AWS
run-init:
ifndef ORG
	$(error ORG is required, e.g. make run-init ORG=acme PROFILE=bootstrap ROOT_EMAIL=root@acme.example.com)
endif
ifndef PROFILE
	$(error PROFILE is required)
endif
ifndef ROOT_EMAIL
	$(error ROOT_EMAIL is required)
endif
	go run . init \
		--org=$(ORG) \
		--profile=$(PROFILE) \
		--root-email=$(ROOT_EMAIL)

## run-init-dry: dry-run `init` — no AWS calls made
run-init-dry:
ifndef ORG
	$(error ORG is required)
endif
ifndef PROFILE
	$(error PROFILE is required)
endif
ifndef ROOT_EMAIL
	$(error ROOT_EMAIL is required)
endif
	go run . init \
		--org=$(ORG) \
		--profile=$(PROFILE) \
		--root-email=$(ROOT_EMAIL) \
		--dry-run

## run-audit: run `audit` against real AWS
run-audit:
ifndef ORG
	$(error ORG is required, e.g. make run-audit ORG=ffreis PROFILE=bootstrap)
endif
ifndef PROFILE
	$(error PROFILE is required)
endif
	go run . audit \
		--org=$(ORG) \
		--profile=$(PROFILE)

## run-audit-json: run `audit` and output JSON
run-audit-json:
ifndef ORG
	$(error ORG is required)
endif
ifndef PROFILE
	$(error PROFILE is required)
endif
	go run . audit \
		--org=$(ORG) \
		--profile=$(PROFILE) \
		--json

## lefthook-bootstrap: download lefthook binary into ./.bin
lefthook-bootstrap:
	LEFTHOOK_VERSION="$(LEFTHOOK_VERSION)" BIN_DIR="$(LEFTHOOK_DIR)" bash ./scripts/bootstrap_lefthook.sh

## lefthook-install: install git hooks (runs bootstrap first)
lefthook-install: lefthook-bootstrap
	@if [ -x "$(LEFTHOOK_BIN)" ] && [ -x ".git/hooks/pre-commit" ] && [ -x ".git/hooks/pre-push" ] && [ -x ".git/hooks/commit-msg" ]; then \
		echo "lefthook hooks already installed"; \
		exit 0; \
	fi
	LEFTHOOK="$(LEFTHOOK_BIN)" "$(LEFTHOOK_BIN)" install

## lefthook-run: run all hooks locally
lefthook-run: lefthook-bootstrap
	LEFTHOOK="$(LEFTHOOK_BIN)" "$(LEFTHOOK_BIN)" run pre-commit
	@tmp_msg="$$(mktemp)"; \
	echo "chore(hooks): validate commit-msg hook" > "$$tmp_msg"; \
	LEFTHOOK="$(LEFTHOOK_BIN)" "$(LEFTHOOK_BIN)" run commit-msg -- "$$tmp_msg"; \
	rm -f "$$tmp_msg"
	LEFTHOOK="$(LEFTHOOK_BIN)" "$(LEFTHOOK_BIN)" run pre-push

## lefthook: install hooks and run them
lefthook: lefthook-bootstrap lefthook-install lefthook-run

## help: list documented targets
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
