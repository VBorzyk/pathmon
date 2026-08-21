# Имя итогового бинарника и путь к пакету с функцией main.
BINARY := pathmon
PKG    := ./cmd/pathmon

# Версия берётся из git-тега. Если тегов ещё нет — будет "dev".
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)

# Цель по умолчанию: сработает, если набрать просто "make".
.DEFAULT_GOAL := build

# .PHONY говорит make, что build — это не имя файла, а имя действия.
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
