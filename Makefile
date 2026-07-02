.PHONY: build test lint run clean install modernise version stamp-version

build:
	go build -ldflags="-s -w" -o bin/ccu ./cmd/ccu

test:
	go test -v -race -cover ./...

lint:
	golangci-lint run

run:
	go run ./cmd/ccu

clean:
	rm -rf bin/

install: build
	mkdir -p "$${GOPATH:-$$HOME/go}/bin"
	cp bin/ccu "$${GOPATH:-$$HOME/go}/bin/ccu"

modernise:
	go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest -fix -test ./...

# Freeze CHANGELOG [Unreleased] under a version heading. Git tags stay the source
# of truth (this project auto-bumps tags in CI), so only the changelog is stamped.
# Usage: make version V=0.2.5
version:
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
stamp-version:
	@TAG=$$(git describe --tags --abbrev=0 2>/dev/null); \
	V=$${TAG#v}; \
	if [ -z "$$V" ]; then echo "ERROR: no git tag found"; exit 1; fi; \
	if command -v uv >/dev/null 2>&1; then \
		uv run scripts/version.py stamp --version "$$V" --changelog-only; \
	else \
		python3 scripts/version.py stamp --version "$$V" --changelog-only; \
	fi
