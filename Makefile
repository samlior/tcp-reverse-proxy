BUILD_TIME := $(shell TZ=UTC date +%Y-%m-%dT%H:%M:%S)

GIT_COMMIT := $(shell git rev-parse --short HEAD)

PLATFORMS := linux/amd64 darwin/amd64 darwin/arm64 windows/amd64

APPS := relay-server reverse-proxy entry-point

OUTPUT_DIR := build

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
			GOOS=$$OS GOARCH=$$ARCH \
			go build -ldflags "-X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)" \
			-o $$OUT ./cmd/$$app/main.go; \
		done; \
	done

clean:
	@rm -rf $(OUTPUT_DIR)
	@echo "Cleaned."

run:
	go run ./cmd/main.go

test:
	go test ./...

.PHONY: all build clean run test