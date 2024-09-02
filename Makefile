# Names of binaries
CLI_LOCAL_BINARY = fridge-local-cli
HTTP_BINARY = fridge-server

# Source directories
CLI_SRC = ./cmd/cli/main.go
HTTP_SRC = ./cmd/http/main.go

# Build and output directories
BUILD_DIR = ./build

# Default target
all: build

# Build local CLI binary
build-local-cli: $(BUILD_DIR)/$(CLI_LOCAL_BINARY)

$(BUILD_DIR)/$(CLI_LOCAL_BINARY): $(CLI_SRC)
	@mkdir -p $(BUILD_DIR)
	go build -o $@ $(CLI_SRC)

# Build HTTP server binary
build-server: $(BUILD_DIR)/$(HTTP_BINARY)

$(BUILD_DIR)/$(HTTP_BINARY): $(HTTP_SRC)
	@mkdir -p $(BUILD_DIR)
	go build -o $@ $(HTTP_SRC)

# Build both local CLI and HTTP server binaries
build: build-local-cli build-server

clean:
	rm -rf $(BUILD_DIR)
