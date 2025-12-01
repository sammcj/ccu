.PHONY: build test lint run clean install modernise

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
	mkdir -p ~/go/bin
	cp bin/ccu ~/go/bin/ccu

modernise:
	go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest -fix -test ./...
