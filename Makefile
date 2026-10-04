GO ?= go

.PHONY: build release test clean
build:
	./scripts/build.sh

release:
	./scripts/release.sh

test:
	$(GO) test ./...

clean:
	rm -rf bin dist
