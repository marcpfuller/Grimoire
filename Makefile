BINARY_NAME := grim
BUILD_DIR   := dist
CMD_PATH    := ./cmd/grim

.PHONY: all build build-linux test test-verbose lint coverage clean mocks tidy typegen

all: build

build:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_PATH)

build-linux:
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

clean:
	rm -rf $(BUILD_DIR)

# Generate TypeScript types from Go API response structs.
# Requires: go install github.com/gzuidhof/tygo@latest
typegen:
	tygo generate

mocks:
	go tool mockery

tidy:
	go mod tidy
