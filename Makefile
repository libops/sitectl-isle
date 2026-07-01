.PHONY: build deps lint test check work docker docs plugins install-plugins install

BINARY_NAME=sitectl-isle
INSTALL_DIR ?= /usr/local/bin/
HOMEBREW_BIN_DIRS ?= /opt/homebrew/bin /home/linuxbrew/.linuxbrew/bin

deps: work
	go mod tidy

build:
	go build -o $(BINARY_NAME) .

install: build
	sudo cp $(BINARY_NAME) $(INSTALL_DIR)$(BINARY_NAME)
	@for dir in $(HOMEBREW_BIN_DIRS); do \
		if [ -d "$$dir" ] && [ "$$(cd "$$dir" && pwd -P)/" != "$$(cd "$(INSTALL_DIR)" && pwd -P)/" ]; then \
			sudo cp $(BINARY_NAME) "$$dir/$(BINARY_NAME)"; \
		fi; \
	done
	@if [ -d /usr/local/Homebrew ] && [ -d /usr/local/bin ] && [ "$$(cd /usr/local/bin && pwd -P)/" != "$$(cd "$(INSTALL_DIR)" && pwd -P)/" ]; then \
		sudo cp $(BINARY_NAME) /usr/local/bin/$(BINARY_NAME); \
	fi

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
