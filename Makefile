GO ?= go
PREFIX ?= /usr/local

.PHONY: build release test clean install update uninstall
build:
	./scripts/build.sh

install: build
	./scripts/install.sh install --artifact-dir "$(CURDIR)/bin" --prefix "$(PREFIX)"

update: build
	./scripts/install.sh update --artifact-dir "$(CURDIR)/bin" --prefix "$(PREFIX)"

uninstall:
	./scripts/install.sh uninstall --prefix "$(PREFIX)"

release:
	./scripts/release.sh

test:
	$(GO) test ./...
	./scripts/test-install.sh

clean:
	rm -rf bin dist
