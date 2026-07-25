BINARY_NAME := grim
BUILD_DIR   := dist
CMD_PATH    := ./cmd/grim

.PHONY: all build test lint clean web mocks

all: web build

build:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_PATH)

build-static:
	CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' -o $(BUILD_DIR)/$(BINARY_NAME)-linux $(CMD_PATH)

test:
	go test ./...

test-verbose:
	go test -v -race ./...

lint:
	golangci-lint run ./...

web:
	cd web && npm run build

clean:
	rm -rf $(BUILD_DIR)
	rm -rf web/dist

mocks:
	go tool mockery

tidy:
	go mod tidy
