# Name of the resulting binary and the path to the package holding func main.
BINARY := pathmon
PKG    := ./cmd/pathmon

# The version comes from the git tag. With no tags yet it falls back to "dev".
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)

# Default goal: what plain "make" runs.
.DEFAULT_GOAL := build

# .PHONY tells make that these are actions, not file names.
.PHONY: build
build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/$(BINARY) $(PKG)

.PHONY: run
run: build
	./bin/$(BINARY) help

.PHONY: test
test:
	go test ./...

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: clean
clean:
	rm -rf bin
