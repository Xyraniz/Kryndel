CC = gcc
CFLAGS = -std=c11 -O2 -Wall -Wextra -Wpedantic

.PHONY: native native-check clean

native: build/kry

build/kry: native/kry.c
	mkdir -p $(@D)
	$(CC) $(CFLAGS) $< -o $@

native-check: native
	./build/kry --version
	./build/kry check examples/hello.kry
	./build/kry check examples/fibonacci.kry
	./build/kry check examples/bytes.kry
	./build/kry run examples/hello.kry
	./build/kry run examples/fibonacci.kry
	./build/kry run examples/bytes.kry

clean:
	rm -rf build
