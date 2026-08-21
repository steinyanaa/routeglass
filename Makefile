SHELL := /bin/bash
.DEFAULT_GOAL := build

VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X github.com/steinyanaa/routeglass/internal/buildinfo.Version=$(VERSION) -X github.com/steinyanaa/routeglass/internal/buildinfo.Commit=$(COMMIT) -X github.com/steinyanaa/routeglass/internal/buildinfo.Date=$(BUILD_DATE)

.PHONY: deps web lint test build build-linux install shell-test clean

deps:
	cd web && npm ci

web:
	cd web && npm run build
	rm -rf internal/server/webdist/*
	cp -R web/dist/. internal/server/webdist/

lint:
	test -z "$$(gofmt -l cmd internal)"
	go vet ./cmd/... ./internal/...
	cd web && npm run lint && npm run typecheck

test:
	go test ./cmd/... ./internal/...
	cd web && npm test

build: web
	go build -trimpath -ldflags "$(LDFLAGS)" -o routeglass ./cmd/routeglass

build-linux: web
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/routeglass-linux-amd64 ./cmd/routeglass
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/routeglass-linux-arm64 ./cmd/routeglass

install: build
	install -Dm0755 routeglass /usr/local/bin/routeglass

shell-test:
	bash install/tests/run.sh

clean:
	rm -rf routeglass dist coverage web/dist web/coverage
