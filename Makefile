CC ?= cc
CFLAGS ?= -std=c11 -O2 -Wall -Wextra -Wpedantic
LDFLAGS ?= -lm

.PHONY: all native check test test-sanitized test-static check-docs clean

all: native

native: build/kry

build/kry: native/kry.c
	mkdir -p $(@D)
	$(CC) $(CFLAGS) $< $(LDFLAGS) -o $@

check: native
	./build/kry check examples/hello.kry
	./build/kry check examples/fibonacci.kry
	./build/kry check examples/bytes.kry
	./build/kry check examples/control_flow.kry
	./build/kry check examples/typed_data.kry
	./build/kry check examples/module_demo.kry

# The integration suite invokes the same executable exposed to users.
test: native check
	./tests/native-core.sh
	KRY_BIN=./build/kry ./tests/edge-cases.sh

# Sanitizers are intentionally enabled without disabling leak detection.
test-sanitized:
	mkdir -p build
	$(CC) -std=c11 -O1 -g -Wall -Wextra -Wpedantic -fsanitize=address,undefined,leak native/kry.c $(LDFLAGS) -o build/kry-sanitized
	KRY_BIN=./build/kry-sanitized ./tests/native-core.sh
	KRY_BIN=./build/kry-sanitized ./tests/edge-cases.sh

# Static compilation is kept separate so CI can use both GCC and Clang.
test-static:
	$(CC) -std=c11 -Wall -Wextra -Wpedantic -Wconversion -Wshadow -Werror -fsyntax-only native/kry.c

check-docs:
	./tests/check-docs.sh

clean:
	rm -rf build
