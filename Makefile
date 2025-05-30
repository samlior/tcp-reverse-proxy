BUILD_TIME := $(shell TZ=UTC date +%Y-%m-%dT%H:%M:%S)

GIT_COMMIT := $(shell git rev-parse --short HEAD)

APPS := relay-server reverse-proxy entry-point gen-cert

OUTPUT_DIR := build

PLATFORMS ?= linux/amd64 darwin/amd64 darwin/arm64 windows/amd64

all: clean build

build:
	@echo "Building ..."
	@for platform in $(PLATFORMS); do \
		OS=$${platform%%/*}; \
		ARCH=$${platform##*/}; \
		echo "→ $$OS/$$ARCH"; \
		for app in $(APPS); do \
			OUT=$(OUTPUT_DIR)/$$OS/$$ARCH/$$app; \
			[ "$$OS" = "windows" ] && OUT=$$OUT.exe; \
			GOOS=$$OS GOARCH=$$ARCH CGO_ENABLED=0\
			go build -ldflags "-s -w -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)" \
			-o $$OUT ./cmd/$$app/main.go; \
		done; \
	done

build-linux-amd64:
	$(MAKE) build PLATFORMS=linux/amd64

clean:
	@rm -rf $(OUTPUT_DIR)
	@echo "Cleaned."

test:
	go test ./...

.PHONY: all build build-linux-amd64 clean run test