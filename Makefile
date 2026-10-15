.PHONY: all build test dist publish clean help

VERSION ?= $(shell cat VERSION 2>/dev/null | tr -d ' \r\n')
# Support 'make publish 0.2.0' where target argument is passed as next goal
ifeq (publish,$(firstword $(MAKECMDGOALS)))
  VERSION_ARG := $(wordlist 2,$(words $(MAKECMDGOALS)),$(MAKECMDGOALS))
  TARGET_VERSION := $(if $(strip $(VERSION_ARG)),$(strip $(VERSION_ARG)),$(VERSION))
  # Treat extra goal as no-op so make doesn't attempt to build it
  %:
	@:
else
  TARGET_VERSION := $(VERSION)
endif

CLEAN_VERSION := $(patsubst v%,%,$(TARGET_VERSION))
LDFLAGS := -s -w -X main.version=$(CLEAN_VERSION)

all: build

help:
	@echo "pagebox make targets:"
	@echo "  make build             - build pagebox binary for current platform"
	@echo "  make test              - run go test suite"
	@echo "  make dist              - compile cross-platform release binaries into dist/"
	@echo "  make publish <version> - bump VERSION, build dist binaries, commit and tag"
	@echo "  make clean             - remove build artifacts"

build:
	@echo "Building pagebox for local system..."
	go build -trimpath -ldflags="$(LDFLAGS)" -o pagebox ./cmd/pagebox
	@echo "Built ./pagebox ($$(du -h pagebox | cut -f1))"

test:
	go test -v ./...

dist:
	@./build.sh

publish:
	@if [ -z "$(CLEAN_VERSION)" ]; then \
		echo "Error: Version cannot be empty. Usage: make publish <version> (e.g. make publish 0.2.0)"; \
		exit 1; \
	fi
	@echo "==> Preparing release v$(CLEAN_VERSION) (previous: $$(cat VERSION 2>/dev/null || echo 'none'))"
	@echo "$(CLEAN_VERSION)" > VERSION
	@echo "$(CLEAN_VERSION)" > cmd/pagebox/VERSION
	@echo "==> Building dist binaries and checksums..."
	@./build.sh
	@echo "==> Updating git repository..."
	@git add VERSION cmd/pagebox/VERSION
	@git commit -m "Release v$(CLEAN_VERSION)" || true
	@git tag -fa "v$(CLEAN_VERSION)" -m "Release v$(CLEAN_VERSION)"
	@echo ""
	@echo "✓ Successfully prepared release v$(CLEAN_VERSION) and updated dist/!"
	@echo "Tag v$(CLEAN_VERSION) has been created."
	@echo ""
	@echo "To publish to GitHub and trigger release, run:"
	@echo "  git push origin master --tags"

clean:
	rm -f pagebox coverage.out profile.out
	rm -rf dist/
