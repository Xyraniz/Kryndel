GO ?= go
BINARY := build/kry
.PHONY: all build check test test-static test-race coverage fuzz-smoke check-docs native-smoke package-smoke release clean
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
	$(BINARY) check examples/discord_bot.kry

test: build check
	$(GO) test ./...
	$(BINARY) run examples/hello.kry
	$(BINARY) run examples/fibonacci.kry
	$(BINARY) run examples/bytes.kry
	$(BINARY) run examples/control_flow.kry
	$(BINARY) run examples/typed_data.kry
	$(BINARY) run examples/module_demo.kry
	$(BINARY) run examples/collections.kry
	$(BINARY) build examples/hello.kry --format=elf --target=linux-x64 -o build/hello.elf
	$(BINARY) inspect build/hello.elf

test-static: build
	@test -z "$$($(GO)fmt -l cmd internal)" || (echo 'gofmt check failed' >&2; exit 1)
	$(GO) vet ./...
	$(GO) test -count=1 ./...

test-race:
	$(GO) test -race -count=1 ./...

coverage:
	mkdir -p build
	$(GO) test -covermode=atomic -coverprofile=build/coverage.out ./...

fuzz-smoke:
	$(GO) test -run='TestFuzzSmoke' -count=1 ./...

check-docs:
	$(GO) test -run='TestDocumentation' -count=1 ./...

native-smoke: build
	$(BINARY) build examples/hello.kry --format=exe --target=windows-x64 -o build/hello.exe
	$(BINARY) inspect build/hello.exe
	$(BINARY) build examples/hello.kry --format=elf --target=linux-x64 -o build/hello.elf
	$(BINARY) inspect build/hello.elf

package-smoke: build
	$(BINARY) package build/kryndel-self-test.kpkg

release: build
	mkdir -p dist
	for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do \
	  os=$${target%/*}; arch=$${target#*/}; ext=; test "$$os" = windows && ext=.exe; \
	  GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 $(GO) build -trimpath -ldflags='-s -w' -o "dist/kry-$$os-$$arch$$ext" ./cmd/kry; \
	done
	sha256sum dist/kry-* > dist/SHA256SUMS

clean:
	rm -rf build dist
