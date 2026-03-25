.PHONY: build deps lint test check work docker integration-test docs plugins install-plugins install

BINARY_NAME=sitectl-isle
FCREPO_STATE?=on
BLAZEGRAPH_STATE?=on
ISLE_FILE_SYSTEM_URI?=public
GIT_REMOTE_URL?=
SITECTL_CONTEXT?=integration-test
INSTALL_DIR ?= $(or $(dir $(shell which $(BINARY_NAME) 2>/dev/null)),/usr/local/bin/)

deps: work
	go mod tidy

build:
	go build -o $(BINARY_NAME) .

install: work build
	sudo cp $(BINARY_NAME) $(INSTALL_DIR)$(BINARY_NAME)

lint:
	go fmt ./...
	golangci-lint run

	@if command -v json5 > /dev/null 2>&1; then \
		echo "Running json5 validation on renovate.json5"; \
		json5 --validate renovate.json5 > /dev/null; \
	else \
		echo "json5 not found, skipping renovate validation"; \
	fi

test: build
	go test -v -race ./...

work:
	./scripts/use-go-work.sh

integration-test:
	SITECTL_CONTEXT="$(SITECTL_CONTEXT)" GIT_REMOTE_URL="$(GIT_REMOTE_URL)" ./scripts/test-create.sh $(FCREPO_STATE) $(ISLE_FILE_SYSTEM_URI) $(BLAZEGRAPH_STATE)

