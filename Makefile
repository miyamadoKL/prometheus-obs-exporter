ROOT_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
BINARY := obs-exporter
CMD_DIR := ./cmd/obs-exporter

VERSION_PKG := github.com/prometheus/common/version

VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo unknown)
REVISION   ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BRANCH     ?= $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)
BUILD_USER ?= $(shell whoami)@$(shell hostname)
BUILD_DATE ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

LDFLAGS := -s -w \
	-X $(VERSION_PKG).Version=$(VERSION) \
	-X $(VERSION_PKG).Revision=$(REVISION) \
	-X $(VERSION_PKG).Branch=$(BRANCH) \
	-X $(VERSION_PKG).BuildUser=$(BUILD_USER) \
	-X $(VERSION_PKG).BuildDate=$(BUILD_DATE)

.PHONY: all build test lint clean

all: clean lint test build

build:
	CGO_ENABLED=0 go build -o bin/$(BINARY) -ldflags "$(LDFLAGS)" $(CMD_DIR)

test:
	go test -race -cover ./...

lint:
	golangci-lint run ./...

clean:
	rm -f bin/$(BINARY)
	rm -rf dist
