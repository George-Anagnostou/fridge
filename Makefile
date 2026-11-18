# Names of binaries
CLI_LOCAL_BINARY = fridge-local
CLI_REMOTE_BINARY = fridge-remote
HTTP_BINARY = fridge-server

BUILD_DIR = ./build

.PHONY: all build build-local-cli build-remote-cli build-server clean

all: build

build-local-cli:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(CLI_LOCAL_BINARY) ./cmd/cli/localCLI

build-remote-cli:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(CLI_REMOTE_BINARY) ./cmd/cli/remoteCLI

build-server:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(HTTP_BINARY) ./cmd/http

build: build-local-cli build-remote-cli build-server

clean:
	rm -rf $(BUILD_DIR)
