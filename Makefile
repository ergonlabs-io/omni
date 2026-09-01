# omni

BIN     := omni
PKG     := ./cmd/omni
PREFIX  ?= /usr/local

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%d)

# A version string without a commit is useless for bug reports.
# See internal-docs/09-cli-design.md §7.
LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)

.PHONY: all build test vet fmt fmt-check lint check clean install dist docs-dev docs-build run-help

all: check build

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BIN) $(PKG)

test:
	go test ./...

test-race:
	go test -race ./...

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then echo "unformatted files:"; echo "$$out"; exit 1; fi

# Full pre-commit gate.
check: fmt-check vet test

# Doc 09 §3: `omni <agent> 2>/dev/null` must be byte-clean on stdout, and
# omni must never print a banner. Guard it in the build, not just in review.
smoke: build
	@test "$$(./bin/$(BIN) 2>/dev/null | wc -c | tr -d ' ')" = "0" \
		|| { echo "FAIL: omni wrote to stdout with no args"; exit 1; }
	@./bin/$(BIN) --version >/dev/null || { echo "FAIL: --version"; exit 1; }
	@./bin/$(BIN) --help    >/dev/null || { echo "FAIL: --help"; exit 1; }
	@echo "smoke ok"

install: build
	install -d $(PREFIX)/bin
	install -m 0755 bin/$(BIN) $(PREFIX)/bin/$(BIN)

# Release artifacts. install.sh downloads exactly these names, so the two
# must change together: omni_<version>_<os>_<arch>.tar.gz containing a bare
# `omni`, plus a checksums.txt the installer verifies against.
DIST_VERSION := $(patsubst v%,%,$(VERSION))
PLATFORMS    := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64

dist:
	rm -rf dist
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "  $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -ldflags "$(LDFLAGS)" -o dist/$(BIN) $(PKG) || exit 1; \
		tar -czf dist/$(BIN)_$(DIST_VERSION)_$${os}_$${arch}.tar.gz -C dist $(BIN) || exit 1; \
		rm -f dist/$(BIN); \
	done
	@cd dist && { sha256sum *.tar.gz 2>/dev/null || shasum -a 256 *.tar.gz; } > checksums.txt
	@echo "dist ok -> dist/ (version $(DIST_VERSION))"

# --------------------------------------------------------------- docs site --
# The Astro + Starlight site at docs/site/. `npm install` is not run by these
# targets -- run it once yourself (or let `npm run` fail loudly) so a docs
# edit doesn't pay a dependency-install tax on every invocation.

docs-dev:
	cd docs/site && npm run dev

docs-build:
	cd docs/site && npm run build

clean:
	rm -rf bin dist coverage.out
