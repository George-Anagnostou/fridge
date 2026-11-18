GO ?= go
DIST_DIR ?= dist
BUILD_DIR ?= ./build
SERVER_GOOS ?= linux
SERVER_GOARCH ?= amd64
GIT_SHA ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
LDFLAGS ?= -s -w -X main.Commit=$(GIT_SHA)

CLI_LOCAL_BINARY = fridge-local
CLI_REMOTE_BINARY = fridge-remote
INVITE_BINARY = fridge-invite
SERVER_BIN := $(DIST_DIR)/fridge-server-$(SERVER_GOOS)-$(SERVER_GOARCH)
INVITE_LINUX_BIN := $(DIST_DIR)/fridge-invite-$(SERVER_GOOS)-$(SERVER_GOARCH)

-include .env.deploy

export FRIDGE_DEPLOY_HOST
export FRIDGE_DEPLOY_USER
export FRIDGE_DEPLOY_PATH
export FRIDGE_DEPLOY_SERVICE
export FRIDGE_URL
export FRIDGE_EXPECTED_SHA

.PHONY: all build build-local-cli build-remote-cli build-server build-invite build-invite-linux clean fmt-check vet test test-race script-check check test-deploy verify update rollback

all: build

fmt-check:
	test -z "$$(gofmt -l $$(git ls-files '*.go'))"

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

test-race:
	CGO_ENABLED=1 $(GO) test -race ./...

script-check:
	@for script in scripts/*.sh; do sh -n "$$script" || exit; done
	@if command -v shellcheck >/dev/null; then shellcheck scripts/*.sh; else echo "shellcheck not installed; skipping lint"; fi

test-deploy: script-check
	sh scripts/deploy_test.sh

check: fmt-check vet test test-race script-check

build-local-cli:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GO) build -o $(BUILD_DIR)/$(CLI_LOCAL_BINARY) ./cmd/cli/localCLI

build-remote-cli:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GO) build -o $(BUILD_DIR)/$(CLI_REMOTE_BINARY) ./cmd/cli/remoteCLI

build-invite:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GO) build -o $(BUILD_DIR)/$(INVITE_BINARY) ./cmd/invite

# linux/amd64 invite CLI for the VPS (same arch assumption as bookmarks).
build-invite-linux:
	mkdir -p "$(DIST_DIR)"
	CGO_ENABLED=0 GOOS=$(SERVER_GOOS) GOARCH=$(SERVER_GOARCH) $(GO) build -trimpath -o "$(INVITE_LINUX_BIN)" ./cmd/invite

build-server:
	mkdir -p "$(DIST_DIR)"
	CGO_ENABLED=0 GOOS=$(SERVER_GOOS) GOARCH=$(SERVER_GOARCH) $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$(SERVER_BIN)" ./cmd/http

build: build-local-cli build-remote-cli build-server build-invite build-invite-linux

verify:
	test -n "$(FRIDGE_URL)"
	curl -fsS "$${FRIDGE_URL%/}/healthz" >/dev/null

update: check
	$(MAKE) build-server build-invite-linux
	./scripts/deploy.sh

rollback:
	./scripts/deploy.sh rollback

clean:
	rm -rf $(BUILD_DIR) $(DIST_DIR)
