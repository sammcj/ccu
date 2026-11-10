.PHONY: build test lint run clean install modernise version stamp-version help

build: ## Build bin/ccu
	go build -ldflags="-s -w" -o bin/ccu ./cmd/ccu

test: ## Run Go tests (race + coverage) and script tests
	go test -v -race -cover ./...
	python3 -m unittest discover -s scripts -q

lint: ## Run golangci-lint
	golangci-lint run

run: ## Run ccu from source
	go run ./cmd/ccu

clean: ## Remove bin/
	rm -rf bin/

install: build ## Build and copy ccu to $GOPATH/bin
	mkdir -p "$${GOPATH:-$$HOME/go}/bin"
	# rm first: cp over an existing Mach-O keeps the old vnode and macOS SIGKILLs
	# the next launch on a cached-signature mismatch
	rm -f "$${GOPATH:-$$HOME/go}/bin/ccu"
	cp bin/ccu "$${GOPATH:-$$HOME/go}/bin/ccu"

modernise: ## Apply Go modernize fixes
	go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest -fix -test ./...

# Freeze CHANGELOG [Unreleased] under a version heading. Git tags stay the source
# of truth (this project auto-bumps tags in CI), so only the changelog is stamped.
# Usage: make version V=0.2.5
version: ## Stamp CHANGELOG [Unreleased] as V (make version V=0.2.5)
	@if [ -z "$(V)" ]; then \
		echo "ERROR: pass V=X.Y.Z, e.g. make version V=0.2.5"; exit 1; \
	fi
	@if ! echo "$(V)" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$$'; then \
		echo "ERROR: '$(V)' is not a valid semver string"; exit 1; \
	fi
	@if command -v uv >/dev/null 2>&1; then \
		uv run scripts/version.py stamp --version "$(V)" --changelog-only; \
	else \
		python3 scripts/version.py stamp --version "$(V)" --changelog-only; \
	fi

# Freeze CHANGELOG using the latest git tag (strips the leading v).
stamp-version: ## Stamp entries shipped in the latest tag
	@TAG=$$(git describe --tags --abbrev=0 2>/dev/null); \
	V=$${TAG#v}; \
	if [ -z "$$V" ]; then echo "ERROR: no git tag found"; exit 1; fi; \
	if command -v uv >/dev/null 2>&1; then \
		uv run scripts/version.py stamp --version "$$V" --changelog-only --released-at "$$TAG"; \
	else \
		python3 scripts/version.py stamp --version "$$V" --changelog-only --released-at "$$TAG"; \
	fi

help: ## List targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
