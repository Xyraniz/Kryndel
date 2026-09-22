SHELL := /bin/bash
GO ?= go
BINARY := build/kry
TIMEOUT ?= 10m
RACE_TIMEOUT ?= 40m

.PHONY: all build check test test-static test-race coverage fuzz-smoke check-docs native-smoke native-parity package-smoke benchmark verify-fast verify-full verify install-association release clean

all: build

build:
	mkdir -p build
	$(GO) build -trimpath -ldflags='-s -w' -o $(BINARY) ./cmd/kry

check: build
	$(BINARY) check examples/hello.kry
	$(BINARY) check examples/fibonacci.kry
	$(BINARY) check examples/bytes.kry
	$(BINARY) check examples/control_flow.kry
	$(BINARY) check examples/typed_data.kry
	$(BINARY) check examples/module_demo.kry
	$(BINARY) check examples/collections.kry
	$(BINARY) check examples/native_features.kry
	$(BINARY) check examples/discord_bot.kry
	$(BINARY) check examples/runtime_polymorphism.kry
	$(BINARY) check examples/dispatch_library.kry
	$(BINARY) check examples/new_builtins.kry
	$(BINARY) check examples/concurrency.kry

test: build check
	$(GO) test ./...
	$(BINARY) run examples/hello.kry
	$(BINARY) run examples/fibonacci.kry
	$(BINARY) run examples/bytes.kry
	$(BINARY) run examples/control_flow.kry
	$(BINARY) run examples/typed_data.kry
	$(BINARY) run examples/module_demo.kry
	$(BINARY) run examples/collections.kry
	$(BINARY) run examples/native_features.kry
	$(BINARY) run examples/runtime_polymorphism.kry
	$(BINARY) run examples/dispatch_library.kry
	$(BINARY) run examples/new_builtins.kry
	$(BINARY) run examples/concurrency.kry
	$(BINARY) build examples/hello.kry --format=elf --target=linux-x64 -o build/hello.elf
	$(BINARY) inspect build/hello.elf

test-static: build
	@test -z "$$($(GO)fmt -l cmd internal)" || (echo 'gofmt check failed' >&2; exit 1)
	$(GO) vet ./...
	$(GO) test -count=1 ./...

test-race:
	KRY_RACE=1 $(GO) test -race -count=1 -timeout=$(RACE_TIMEOUT) ./...

coverage:
	mkdir -p build
	$(GO) test -covermode=atomic -coverprofile=build/coverage.out ./...

fuzz-smoke:
	$(GO) test -run='TestFuzzSmoke' -count=1 -timeout=$(TIMEOUT) ./...

check-docs:
	$(GO) test -run='TestDocumentation' -count=1 ./...

native-smoke: build
	$(BINARY) build examples/hello.kry --format=exe --target=windows-x64 -o build/hello.exe
	$(BINARY) inspect build/hello.exe
	$(BINARY) build examples/hello.kry --format=elf --target=linux-x64 -o build/hello.elf
	$(BINARY) inspect build/hello.elf

native-parity: build
	mkdir -p build/verify-logs
	@set -o pipefail; $(BINARY) build examples/native_features.kry --format=elf --target=linux-x64 -o build/native_features.elf 2>&1 | tee build/verify-logs/native_features.build.log
	$(BINARY) run examples/native_features.kry > build/native_features.interp.out
	./build/native_features.elf > build/native_features.native.out
	diff -u build/native_features.interp.out build/native_features.native.out
	@set -o pipefail; $(BINARY) build examples/new_builtins.kry --format=elf --target=linux-x64 -o build/new_builtins.elf 2>&1 | tee build/verify-logs/new_builtins.build.log
	$(BINARY) run examples/new_builtins.kry > build/new_builtins.interp.out
	./build/new_builtins.elf > build/new_builtins.native.out
	diff -u build/new_builtins.interp.out build/new_builtins.native.out

package-smoke: build
	$(BINARY) package build/kryndel-self-test.kpkg

benchmark:
	$(GO) test -run='^$$' -bench=. -benchmem ./...

verify-fast: build check test-static check-docs fuzz-smoke

verify-full: verify-fast test native-smoke native-parity test-race coverage benchmark

verify: verify-full

install-association: build
	tools/install-association.sh

release: build
	mkdir -p dist
	for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do \
	  os=$${target%/*}; arch=$${target#*/}; ext=; test "$$os" = windows && ext=.exe; \
	  GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 $(GO) build -trimpath -ldflags='-s -w' -o "dist/kry-$$os-$$arch$$ext" ./cmd/kry; \
	done
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -ldflags='-s -w' -o dist/kry-installer-windows-amd64.exe ./cmd/kry-installer
	sha256sum dist/kry-* > dist/SHA256SUMS

clean:
	rm -rf build dist
