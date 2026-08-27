ifeq ($(origin CC),default)
CC := $(shell command -v cc 2>/dev/null || command -v gcc 2>/dev/null || command -v clang 2>/dev/null || echo no-usable-c11-compiler)
endif
CFLAGS ?= -std=c11 -O2 -Wall -Wextra -Wpedantic
LDFLAGS ?= -lm -pthread

.PHONY: all native check test test-features test-sanitized test-thread-sanitized test-static fuzz-smoke coverage benchmark check-docs clean

all: native

native: build/kry

build/kry: native/kry.c native/kry_artifacts.inc
	@if [ "$(CC)" = "no-usable-c11-compiler" ]; then echo 'kry: no usable C11 compiler found; set CC to GCC or Clang, or install a native compiler' >&2; exit 69; fi
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
test: native check test-features fuzz-smoke
	./tests/native-core.sh
	KRY_BIN=./build/kry ./tests/edge-cases.sh

test-features: native
	KRY_BIN=./build/kry ./tests/feature-regressions.sh

fuzz-smoke: native
	KRY_BIN=./build/kry ./tests/fuzz-smoke.sh

coverage: native
	@command -v gcov >/dev/null 2>&1 || { echo "gcov unavailable" >&2; exit 69; }
	rm -f native/*.gcda native/*.gcno *.gcov
	$(CC) -std=c11 -O0 -g --coverage native/kry.c $(LDFLAGS) -o build/kry-coverage
	KRY_BIN=./build/kry-coverage ./tests/native-core.sh
	KRY_BIN=./build/kry-coverage ./tests/edge-cases.sh
	KRY_BIN=./build/kry-coverage ./tests/feature-regressions.sh
	gcov -b -c build/kry-coverage-kry.gcno > build/coverage.txt
	mv -f *.gcov build/

benchmark: native
	@echo 'benchmark: startup'
	./build/kry version >/dev/null
	@echo 'benchmark: checker'
	./build/kry check examples/fibonacci.kry >/dev/null
	@echo 'benchmark: ok'

# Sanitizers are intentionally enabled without disabling leak detection.
test-sanitized:
	mkdir -p build
	$(CC) -std=c11 -O1 -g -Wall -Wextra -Wpedantic -fsanitize=address,undefined,leak native/kry.c $(LDFLAGS) -o build/kry-sanitized
	KRY_BIN=./build/kry-sanitized ./tests/native-core.sh
	KRY_BIN=./build/kry-sanitized ./tests/edge-cases.sh
	KRY_BIN=./build/kry-sanitized ./tests/feature-regressions.sh

test-thread-sanitized:
	mkdir -p build
	$(CC) -std=c11 -O1 -g -Wall -Wextra -Wpedantic -fsanitize=thread native/kry.c $(LDFLAGS) -o build/kry-thread-sanitized
	KRY_BIN=./build/kry-thread-sanitized ./tests/native-core.sh
	KRY_BIN=./build/kry-thread-sanitized ./tests/edge-cases.sh
	KRY_BIN=./build/kry-thread-sanitized ./tests/feature-regressions.sh

# Static compilation is kept separate so CI can use both GCC and Clang.
test-static:
	$(CC) -std=c11 -Wall -Wextra -Wpedantic -Wconversion -Wshadow -Werror -fsyntax-only native/kry.c

check-docs:
	./tests/check-docs.sh

clean:
	rm -rf build
