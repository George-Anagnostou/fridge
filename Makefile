# Names of binaries
CLI_LOCAL_BINARY = fridge-local
CLI_REMOTE_BINARY = fridge-remote
HTTP_BINARY = fridge-server

# Source directories
CLI_LOCAL_SRC = ./cmd/cli/remoteCLI/main.go
CLI_REMOTE_SRC = ./cmd/cli/localCLI/main.go
HTTP_SRC = ./cmd/http/main.go

# Build and output directories
BUILD_DIR = ./build

# Default target
all: build

# Build local CLI binary
build-local-cli: $(BUILD_DIR)/$(CLI_LOCAL_BINARY)

$(BUILD_DIR)/$(CLI_LOCAL_BINARY): $(CLI_LOCAL_SRC)
	@mkdir -p $(BUILD_DIR)
	go build -o $@ $(CLI_LOCAL_SRC)

build-remote-cli: $(BUILD_DIR)/$(CLI_REMOTE_BINARY)

$(BUILD_DIR)/$(CLI_REMOTE_BINARY): $(CLI_REMOTE_SRC)
	@mkdir -p $(BUILD_DIR)
	go build -o $@ $(CLI_REMOTE_SRC)

# Build HTTP server binary
build-server: $(BUILD_DIR)/$(HTTP_BINARY)

$(BUILD_DIR)/$(HTTP_BINARY): $(HTTP_SRC)
	@mkdir -p $(BUILD_DIR)
	go build -o $@ $(HTTP_SRC)

# Build both local CLI and HTTP server binaries
build: build-local-cli build-remote-cli build-server

clean:
	rm -rf $(BUILD_DIR)
