.PHONY: build install clean test lint

VERSION ?= dev
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

build:
	go build $(LDFLAGS) -o invarity ./cmd/invarity

install:
	go install $(LDFLAGS) ./cmd/invarity

clean:
	rm -f invarity
	rm -f invarity-*

test:
	go test -v ./...

lint:
	golangci-lint run

# Cross-compilation targets
build-all: build-linux build-darwin build-windows

build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o invarity-linux-amd64 ./cmd/invarity
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o invarity-linux-arm64 ./cmd/invarity

build-darwin:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o invarity-darwin-amd64 ./cmd/invarity
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o invarity-darwin-arm64 ./cmd/invarity

build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o invarity-windows-amd64.exe ./cmd/invarity

# Development helpers
run:
	go run ./cmd/invarity $(ARGS)

deps:
	go mod download
	go mod tidy
