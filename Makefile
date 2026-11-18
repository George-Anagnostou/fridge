# Names of binaries
CLI_LOCAL_BINARY = fridge-local
CLI_REMOTE_BINARY = fridge-remote
HTTP_BINARY = fridge-server
INVITE_BINARY = fridge-invite

BUILD_DIR = ./build

.PHONY: all build build-local-cli build-remote-cli build-server build-invite clean

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

build-invite:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(INVITE_BINARY) ./cmd/invite

build: build-local-cli build-remote-cli build-server build-invite

clean:
	rm -rf $(BUILD_DIR)
