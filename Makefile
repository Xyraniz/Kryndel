CC ?= cc
CFLAGS ?= -std=c11 -O2 -Wall -Wextra -Wpedantic

.PHONY: all native check test clean

all: native

native: build/kry

build/kry: native/kry.c
	mkdir -p $(@D)
	$(CC) $(CFLAGS) $< -o $@

check: native
	./build/kry check examples/hello.kry
	./build/kry check examples/fibonacci.kry
	./build/kry check examples/bytes.kry
	./build/kry check examples/control_flow.kry

# Integration tests intentionally invoke the same executable exposed to users.
test: native check
	./tests/native-core.sh

clean:
	rm -rf build
