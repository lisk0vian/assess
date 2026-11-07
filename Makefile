# Makefile for Assess (ax)
# Targets use Go and pnpm; run from repo root.

GO ?= go
PNPM ?= pnpm

.PHONY: build test vet lint web-build web-install clean

build:
	$(GO) build ./cmd/ax

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

lint:
	golangci-lint run

web-install:
	cd web && $(PNPM) install --frozen-lockfile

web-build:
	cd web && $(PNPM) build

web-typecheck:
	cd web && $(PNPM) typecheck

clean:
	rm -f ax ax.exe