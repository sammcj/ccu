# Suggested Commands

## Build and Run
- `make build` - Compile to `bin/ccu` with `-ldflags="-s -w"`
- `make run` - Run via `go run ./cmd/ccu`
- `make install` - Build and install to GOPATH/bin

## Testing
- `make test` - All tests with race detection and coverage
- `go test -v -run TestName ./internal/package` - Single test

## Code Quality
- `make lint` - Run golangci-lint
- `make modernise` - Apply modern Go patterns via gopls modernize

## Cleanup
- `make clean` - Remove bin/ directory

## System Utilities (macOS/Darwin)
- `git`, `ls`, `cd`, `grep`, `find` - Standard unix (BSD variants on macOS)
- `security find-generic-password` - macOS Keychain access (used for OAuth tokens)
