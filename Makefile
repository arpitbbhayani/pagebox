BINARY_NAME ?= pagebox
SRC_DIR ?= ./cmd/pagebox
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS ?= -X main.version=$(VERSION)

.PHONY: all build test clean run help

# Default target
all: build

## build: Build the pagebox binary (default)
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) $(SRC_DIR)

## test: Run tests
test:
	go test -v ./...

## clean: Remove built binaries and test artifacts
clean:
	rm -f $(BINARY_NAME) pagebox coverage.out profile.out

## run: Build and run the binary
run: build
	./$(BINARY_NAME)

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':'
