BINARY_NAME := grim
BUILD_DIR   := dist
CMD_PATH    := ./cmd/grim
GOLANGCI_LINT_VERSION ?= v2.8.0
GOLANGCI_LINT_BIN_DIR ?= $(shell go env GOPATH)/bin
MOCKERY_INSTALL_VERSION ?= v2.53.6
MOCKERY_INSTALL_BIN_DIR ?= $(shell go env GOPATH)/bin

.PHONY: all build build-frontend build-linux test test-verbose lint install-golangci-lint install-mockery coverage clean mocks tidy typegen

all: build

# Build the frontend first, then embed it into the binary.
build: build-frontend
	go build -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_PATH)

build-frontend: typegen
	cd web && npm run build

build-linux: build-frontend
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-linux $(CMD_PATH)

test:
	go test ./...

test-verbose:
	go test -v -race ./...

coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

lint:
	golangci-lint run ./...

# Install the CI-pinned golangci-lint release into the active Go/GVM package set.
install-golangci-lint:
	curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b "$(GOLANGCI_LINT_BIN_DIR)" "$(GOLANGCI_LINT_VERSION)"
	"$(GOLANGCI_LINT_BIN_DIR)/golangci-lint" version

clean:
	rm -rf $(BUILD_DIR)

# Generate TypeScript types from Go API response structs.
typegen:
	go tool tygo generate

mocks:
	go tool mockery

# Install the go.mod-pinned Mockery v2 release into the active Go/GVM package set.
install-mockery:
	GOBIN="$(MOCKERY_INSTALL_BIN_DIR)" go install github.com/vektra/mockery/v2@"$(MOCKERY_INSTALL_VERSION)"
	"$(MOCKERY_INSTALL_BIN_DIR)/mockery" --version

tidy:
	go mod tidy
