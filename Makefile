GO ?= $(firstword $(shell command -v go 2>/dev/null) $(wildcard $(HOME)/environment/go*/bin/go) $(wildcard /opt/homebrew/bin/go /usr/local/go/bin/go))

.PHONY: build cli app release test clean
build:
	@if [ -z "$(filter cli app,$(MAKECMDGOALS))" ]; then printf 'Usage: make build cli, or on macOS: make build app\n'; exit 2; fi

cli:
	GO="$(GO)" ./scripts/build.sh cli

app:
	@if [ "$(shell uname -s)" != Darwin ]; then printf 'Nagi.app can only be built on macOS\n' >&2; exit 2; fi
	./macos/NagiApp/package-app.sh

release:
	./scripts/release.sh

test:
	$(GO) test ./...
	./scripts/test-install.sh

clean:
	rm -rf bin dist
