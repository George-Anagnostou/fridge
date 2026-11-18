GO ?= go
DIST_DIR ?= dist
BUILD_DIR ?= ./build
SERVER_GOOS ?= linux
SERVER_GOARCH ?= amd64

CLI_LOCAL_BINARY = fridge-local
CLI_REMOTE_BINARY = fridge-remote
INVITE_BINARY = fridge-invite
SERVER_BIN := $(DIST_DIR)/fridge-server-$(SERVER_GOOS)-$(SERVER_GOARCH)

.PHONY: all build build-local-cli build-remote-cli build-server build-invite clean fmt-check vet test test-race check

all: build

fmt-check:
	test -z "$$(gofmt -l $$(git ls-files '*.go'))"

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

test-race:
	CGO_ENABLED=1 $(GO) test -race ./...

check: fmt-check vet test test-race

build-local-cli:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GO) build -o $(BUILD_DIR)/$(CLI_LOCAL_BINARY) ./cmd/cli/localCLI

build-remote-cli:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GO) build -o $(BUILD_DIR)/$(CLI_REMOTE_BINARY) ./cmd/cli/remoteCLI

build-invite:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GO) build -o $(BUILD_DIR)/$(INVITE_BINARY) ./cmd/invite

build-server:
	mkdir -p "$(DIST_DIR)"
	CGO_ENABLED=0 GOOS=$(SERVER_GOOS) GOARCH=$(SERVER_GOARCH) $(GO) build -trimpath -o "$(SERVER_BIN)" ./cmd/http

build: build-local-cli build-remote-cli build-server build-invite

clean:
	rm -rf $(BUILD_DIR) $(DIST_DIR)
