GO_ARGS ?=
GOARCH ?= amd64
PLUGIN_NAME = nginx-custom
GO_PLUGIN_MAKE_TARGET ?= build
BUILD_IMAGE := golang:1.24.2
GO_BUILD_CACHE ?= /tmp/dokku-go-build-cache-$(PLUGIN_NAME)
GO_MOD_CACHE   ?= /tmp/dokku-go-mod-cache-$(PLUGIN_NAME)

AVAILABLE_COMMANDS := $(shell find src/cmd -maxdepth 1 -type d -name "*" | sed 's|src/cmd/||' | grep -v "^src/cmd$$")
BUILD ?= $(AVAILABLE_COMMANDS)

.PHONY: build-in-docker build clean src-clean $(AVAILABLE_COMMANDS)

$(AVAILABLE_COMMANDS): %: clean-%
	@echo "Building $@ in Docker..."
	@mkdir -p $(GO_BUILD_CACHE) $(GO_MOD_CACHE)
	@docker run --rm \
		-v $(shell pwd):/go/src/nginx-custom \
		-v $(GO_BUILD_CACHE):/root/.cache \
		-v $(GO_MOD_CACHE):/go/pkg/mod \
		-e GO111MODULE=on \
		-w /go/src/nginx-custom \
		$(BUILD_IMAGE) \
		bash -c "CGO_ENABLED=0 GOFLAGS=-buildvcs=false GOOS=linux GOARCH=$(GOARCH) GOWORK=off go build -ldflags='-s -w' $(GO_ARGS) -o $@ ./src/cmd/$@" || exit $$?

clean-%:
	@rm -f $(shell echo $* | sed 's/.*/&/')

build: $(BUILD)

build-in-docker: clean $(BUILD)

clean:
	@echo "Cleaning up built binaries..."
	@rm -f $(AVAILABLE_COMMANDS)
	@find . -xtype l -delete

list-commands:
	@echo "Available commands:"
	@echo $(AVAILABLE_COMMANDS) | tr ' ' '\n' | sed 's/^/  /'
