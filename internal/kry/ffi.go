package kry

import (
	"fmt"
	"runtime"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego"
)

type ffiSignature struct {
	result byte
	args   []byte
}

func parseFFISignature(raw string, arity int) (ffiSignature, error) {
	value := strings.ReplaceAll(strings.TrimSpace(raw), " ", "")
	open := strings.IndexByte(value, '(')
	if open < 1 || !strings.HasSuffix(value, ")") {
		return ffiSignature{}, fmt.Errorf("FFI signature must look like i(i,p), p(), or v(i)")
	}
	result := value[0]
	if result != 'i' && result != 'u' && result != 'p' && result != 'v' {
		return ffiSignature{}, fmt.Errorf("FFI return type must be i, u, p, or v")
	}
	inside := value[open+1 : len(value)-1]
	var args []byte
	if inside != "" {
		for _, token := range strings.Split(inside, ",") {
			if len(token) != 1 || (token[0] != 'i' && token[0] != 'u' && token[0] != 'p') {
				return ffiSignature{}, fmt.Errorf("FFI arguments support only i, u, and p")
			}
			args = append(args, token[0])
		}
	}
	if len(args) != arity {
		return ffiSignature{}, fmt.Errorf("FFI signature declares %d argument(s), got %d", len(args), arity)
	}
	if len(args) > 8 {
		return ffiSignature{}, fmt.Errorf("FFI calls support at most 8 arguments")
	}
	return ffiSignature{result: result, args: args}, nil
}

func ffiLibraryOpen(path string) (*ffiLibraryHandle, error) {
	handle, err := ffiOpenLibrary(path)
	if err != nil {
		return nil, err
	}
	return &ffiLibraryHandle{handle: handle}, nil
}

func ffiLibraryClose(library *ffiLibraryHandle) error {
	if library == nil {
		return fmt.Errorf("invalid FFILibrary handle")
	}
	library.mu.Lock()
	defer library.mu.Unlock()
	if library.closed {
		return fmt.Errorf("FFILibrary handle is already closed")
	}
	library.closed = true
	return ffiCloseLibrary(library.handle)
}

func (library *ffiLibraryHandle) isClosed() bool {
	if library == nil {
		return true
	}
	library.mu.Lock()
	defer library.mu.Unlock()
	return library.closed
}

func ffiSymbol(library *ffiLibraryHandle, name string) (*ffiSymbolHandle, error) {
	if library == nil {
		return nil, fmt.Errorf("invalid FFILibrary handle")
	}
	if name == "" || strings.IndexByte(name, 0) >= 0 {
		return nil, fmt.Errorf("FFI symbol name must be non-empty and contain no NUL")
	}
	library.mu.Lock()
	defer library.mu.Unlock()
	if library.closed {
		return nil, fmt.Errorf("FFILibrary handle is closed")
	}
	address, err := ffiLookupSymbol(library.handle, name)
	if err != nil {
		return nil, err
	}
	return &ffiSymbolHandle{library: library, address: address, name: name}, nil
}

func ffiCall(symbol *ffiSymbolHandle, signature string, values []Value) (int64, error) {
	if symbol == nil || symbol.library == nil || symbol.address == 0 {
		return 0, fmt.Errorf("invalid FFISymbol handle")
	}
	symbol.library.mu.Lock()
	closed := symbol.library.closed
	symbol.library.mu.Unlock()
	if closed {
		return 0, fmt.Errorf("FFILibrary handle is closed")
	}
	parsed, err := parseFFISignature(signature, len(values))
	if err != nil {
		return 0, err
	}
	args := make([]uintptr, len(values))
	var keepAlive []*ffiBufferHandle
	lockedBuffers := make([]*ffiBufferHandle, 0, len(values))
	lockedSet := make(map[*ffiBufferHandle]struct{}, len(values))
	defer func() {
		for i := len(lockedBuffers) - 1; i >= 0; i-- {
			lockedBuffers[i].mu.Unlock()
		}
	}()
	for i, value := range values {
		if value.Kind != VInt {
			return 0, fmt.Errorf("FFI arguments must be Int values returned by ffi_buffer_address or integer literals")
		}
		if parsed.args[i] == 'p' {
			ffiBuffers.RLock()
			buffer := ffiBuffers.byID[value.I]
			ffiBuffers.RUnlock()
			if buffer != nil {
				if _, locked := lockedSet[buffer]; !locked {
					buffer.mu.Lock()
					if !buffer.closed && len(buffer.data) > 0 {
						lockedSet[buffer] = struct{}{}
						lockedBuffers = append(lockedBuffers, buffer)
					} else {
						buffer.mu.Unlock()
					}
				}
				if _, locked := lockedSet[buffer]; locked {
					args[i] = uintptr(unsafe.Pointer(&buffer.data[0]))
					keepAlive = append(keepAlive, buffer)
					continue
				}
			}
		}
		args[i] = uintptr(value.I)
	}
	result, err := ffiInvoke(symbol.address, args)
	if err != nil {
		return 0, err
	}
	for _, buffer := range keepAlive {
		runtime.KeepAlive(buffer)
	}
	if parsed.result == 'v' {
		return 0, nil
	}
	return int64(result), nil
}

// ffiInvoke uses one exact Go function shape per arity. Passing the exact
// number of arguments matters on Windows, where the purego variadic fallback
// has different stack behavior from a normal C call.
func ffiInvoke(address uintptr, args []uintptr) (uintptr, error) {
	switch len(args) {
	case 0:
		var call func() uintptr
		purego.RegisterFunc(&call, address)
		return call(), nil
	case 1:
		var call func(uintptr) uintptr
		purego.RegisterFunc(&call, address)
		return call(args[0]), nil
	case 2:
		var call func(uintptr, uintptr) uintptr
		purego.RegisterFunc(&call, address)
		return call(args[0], args[1]), nil
	case 3:
		var call func(uintptr, uintptr, uintptr) uintptr
		purego.RegisterFunc(&call, address)
		return call(args[0], args[1], args[2]), nil
	case 4:
		var call func(uintptr, uintptr, uintptr, uintptr) uintptr
		purego.RegisterFunc(&call, address)
		return call(args[0], args[1], args[2], args[3]), nil
	case 5:
		var call func(uintptr, uintptr, uintptr, uintptr, uintptr) uintptr
		purego.RegisterFunc(&call, address)
		return call(args[0], args[1], args[2], args[3], args[4]), nil
	case 6:
		var call func(uintptr, uintptr, uintptr, uintptr, uintptr, uintptr) uintptr
		purego.RegisterFunc(&call, address)
		return call(args[0], args[1], args[2], args[3], args[4], args[5]), nil
	case 7:
		var call func(uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr) uintptr
		purego.RegisterFunc(&call, address)
		return call(args[0], args[1], args[2], args[3], args[4], args[5], args[6]), nil
	case 8:
		var call func(uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr) uintptr
		purego.RegisterFunc(&call, address)
		return call(args[0], args[1], args[2], args[3], args[4], args[5], args[6], args[7]), nil
	default:
		return 0, fmt.Errorf("FFI calls support at most 8 arguments")
	}
}

func ffiBufferNew(data []byte) *ffiBufferHandle {
	buffer := make([]byte, len(data)+1)
	copy(buffer, data)
	return &ffiBufferHandle{data: buffer, length: len(data)}
}

func ffiBufferNewSized(capacity int) (*ffiBufferHandle, error) {
	if capacity < 1 || capacity > 16<<20 {
		return nil, fmt.Errorf("FFI buffer capacity must be in 1..16777216 bytes")
	}
	return &ffiBufferHandle{data: make([]byte, capacity+1), length: capacity}, nil
}

func ffiBufferAddress(buffer *ffiBufferHandle) (int64, error) {
	if buffer == nil {
		return 0, fmt.Errorf("invalid or closed FFIBuffer handle")
	}
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	if buffer.closed || len(buffer.data) == 0 {
		return 0, fmt.Errorf("invalid or closed FFIBuffer handle")
	}
	ffiBuffers.Lock()
	defer ffiBuffers.Unlock()
	for id, known := range ffiBuffers.byID {
		if known == buffer {
			return id, nil
		}
	}
	id := ffiBuffers.next
	ffiBuffers.next--
	ffiBuffers.byID[id] = buffer
	return id, nil
}

func ffiBufferRead(buffer *ffiBufferHandle) ([]byte, error) {
	if buffer == nil {
		return nil, fmt.Errorf("invalid or closed FFIBuffer handle")
	}
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	if buffer.closed {
		return nil, fmt.Errorf("invalid or closed FFIBuffer handle")
	}
	return append([]byte(nil), buffer.data[:buffer.length]...), nil
}

func ffiBufferClose(buffer *ffiBufferHandle) error {
	if buffer == nil {
		return fmt.Errorf("invalid FFIBuffer handle")
	}
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	if buffer.closed {
		return fmt.Errorf("FFIBuffer handle is already closed")
	}
	ffiBuffers.Lock()
	for id, known := range ffiBuffers.byID {
		if known == buffer {
			delete(ffiBuffers.byID, id)
		}
	}
	ffiBuffers.Unlock()
	buffer.closed = true
	buffer.data = nil
	buffer.length = 0
	return nil
}

func (buffer *ffiBufferHandle) isClosed() bool {
	if buffer == nil {
		return true
	}
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.closed
}
