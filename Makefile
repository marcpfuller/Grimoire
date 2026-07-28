BINARY_NAME := grim
BUILD_DIR   := dist
CMD_PATH    := ./cmd/grim

.PHONY: all build build-frontend build-linux test test-verbose lint coverage clean mocks tidy typegen

all: build

# Build the frontend first, then embed it into the binary.
build: build-frontend
	go build -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_PATH)

build-frontend:
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

clean:
	rm -rf $(BUILD_DIR)

# Generate TypeScript types from Go API response structs.
typegen:
	go tool tygo generate

mocks:
	go tool mockery

tidy:
	go mod tidy
