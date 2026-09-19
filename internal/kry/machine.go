package kry

import (
	"encoding/binary"
	"fmt"
)

const (
	elfBase       uint64 = 0x400000
	elfHeaderLen         = 64
	elfProgramLen        = 56
	elfCodeOffset        = elfHeaderLen + elfProgramLen
)

// BuildDirectELF emits a small, dependency-free ELF64 executable directly.
// The static path preserves the first byte-stable bootstrap slice; programs
// containing assignments or control flow use the second machine-code slice.
// Both paths emit genuine x86-64 instructions and reject unsupported language
// constructs before bytes are returned. The current dynamic slice also
// supports scalar and pointer-like SysV AMD64 function calls, including the
// immutable qword-array ABI and its Linux mmap runtime.
func BuildDirectELF(p *Program, c *Checker, target NativeTarget) ([]byte, error) {
	if target.OS != "linux" || target.Arch != "amd64" {
		return nil, fmt.Errorf("direct ELF backend currently supports only linux-amd64")
	}
	if p == nil || c == nil {
		return nil, fmt.Errorf("missing checked program")
	}
	// Force every direct backend build through the public interchange format.
	// This catches schema drift before the machine emitter is allowed to run.
	kir, err := EmitKIR(p, c, target)
	if err != nil {
		return nil, err
	}
	if _, err = DecodeKIR(kir, c.Env.Lim); err != nil {
		return nil, fmt.Errorf("direct backend rejected KIR: %w", err)
	}
	output, err := directStaticOutput(p, c)
	if err == nil {
		return emitELF64WriteExit(output), nil
	}
	stmts, stmtErr := directDynamicStatements(p)
	if stmtErr == nil && (directHasDynamicControl(stmts) || directHasArrayFeatures(stmts) || directHasUserFunctions(p)) {
		return buildDirectDynamicELF(p)
	}
	return nil, err
}

func directStaticOutput(p *Program, c *Checker) ([]byte, error) {
	statements := p.Statements
	if len(statements) == 0 {
		for _, f := range p.Functions {
			if f.Name == "main" {
				statements = f.Body
				break
			}
		}
	}
	env := map[string]Value{}
	output := make([]byte, 0)
	for _, s := range statements {
		if s == nil {
			continue
		}
		switch s.Kind {
		case StLet, StConst:
			if s.Init == nil {
				return nil, fmt.Errorf("direct ELF backend requires an initializer for '%s'", s.Name)
			}
			v, ok := directStaticValue(s.Init, env)
			if !ok {
				return nil, fmt.Errorf("direct ELF backend requires a compile-time value for '%s'", s.Name)
			}
			env[s.Name] = v
		case StExpr:
			if s.Expr == nil || s.Expr.Kind != ExCall || s.Expr.Receiver != nil || (s.Expr.Name != "print" && s.Expr.Name != "println") || len(s.Expr.Args) != 1 {
				return nil, fmt.Errorf("direct ELF backend supports only print/println of static values")
			}
			v, ok := directStaticValue(s.Expr.Args[0], env)
			if !ok {
				return nil, fmt.Errorf("direct ELF backend requires a compile-time print value")
			}
			text := display(v)
			if s.Expr.Name == "println" {
				text += "\n"
			}
			output = append(output, []byte(text)...)
		default:
			return nil, fmt.Errorf("direct ELF backend does not support statement kind %s", stmtName(s.Kind))
		}
		if int64(len(output)) > c.Env.Lim.MaxOutputBytes {
			return nil, fmt.Errorf("direct ELF output exceeds configured output limit")
		}
	}
	return output, nil
}

func directStaticValue(e *Expr, env map[string]Value) (Value, bool) {
	if e == nil {
		return nilVal(), false
	}
	if e.ConstValue != nil {
		return cloneValue(*e.ConstValue), true
	}
	switch e.Kind {
	case ExInt:
		return intVal(e.Int), true
	case ExFloat:
		return floatVal(e.Float), true
	case ExBool:
		return boolVal(e.Bool), true
	case ExString:
		return stringVal(e.Str), true
	case ExNil:
		return nilVal(), true
	case ExVar:
		v, ok := env[e.Name]
		return v, ok
	case ExCall:
		if e.Receiver != nil || len(e.Args) != 1 || e.Name != "str" {
			return nilVal(), false
		}
		v, ok := directStaticValue(e.Args[0], env)
		if !ok {
			return nilVal(), false
		}
		return stringVal(display(v)), true
	case ExBinary:
		left, lok := directStaticValue(e.Left, env)
		right, rok := directStaticValue(e.Right, env)
		if !lok || !rok {
			return nilVal(), false
		}
		if e.Op == PLUS && left.Kind == VString && right.Kind == VString {
			return stringVal(left.S + right.S), true
		}
		if left.Kind == VInt && right.Kind == VInt {
			var v int64
			var ok bool
			switch e.Op {
			case PLUS:
				v, ok = addI(left.I, right.I)
			case MINUS:
				v, ok = subI(left.I, right.I)
			case STAR:
				v, ok = mulI(left.I, right.I)
			case SLASH:
				v, ok = divI(left.I, right.I)
			case PERCENT:
				v, ok = remI(left.I, right.I)
			default:
				return nilVal(), false
			}
			if ok {
				return intVal(v), true
			}
		}
		if left.Kind == VUInt && right.Kind == VUInt && left.UBits == right.UBits {
			var v uint64
			switch e.Op {
			case PLUS:
				v = left.U + right.U
			case MINUS:
				v = left.U - right.U
			case STAR:
				v = left.U * right.U
			case BITAND:
				v = left.U & right.U
			case BITXOR:
				v = left.U ^ right.U
			case PIPE:
				v = left.U | right.U
			default:
				return nilVal(), false
			}
			return uintVal(left.UBits, v), true
		}
	}
	return nilVal(), false
}

func emitELF64WriteExit(data []byte) []byte {
	code := []byte{
		0xb8, 0x01, 0x00, 0x00, 0x00, // mov eax, SYS_write
		0xbf, 0x01, 0x00, 0x00, 0x00, // mov edi, stdout
		0x48, 0x8d, 0x35, 0, 0, 0, 0, // lea rsi, [rip + data]
		0xba, 0, 0, 0, 0, // mov edx, len(data)
		0x0f, 0x05, // syscall
		0xb8, 0x3c, 0x00, 0x00, 0x00, // mov eax, SYS_exit
		0x31, 0xff, // xor edi, edi
		0x0f, 0x05, // syscall
	}
	dataOffset := elfCodeOffset + len(code)
	nextRIP := elfCodeOffset + 17
	binary.LittleEndian.PutUint32(code[13:17], uint32(dataOffset-nextRIP))
	binary.LittleEndian.PutUint32(code[18:22], uint32(len(data)))
	fileSize := dataOffset + len(data)
	out := make([]byte, fileSize)
	copy(out[elfCodeOffset:], code)
	copy(out[dataOffset:], data)
	copy(out[0:4], []byte{0x7f, 'E', 'L', 'F'})
	out[4] = 2                                      // ELFCLASS64
	out[5] = 1                                      // ELFDATA2LSB
	out[6] = 1                                      // EV_CURRENT
	binary.LittleEndian.PutUint16(out[16:18], 2)    // ET_EXEC
	binary.LittleEndian.PutUint16(out[18:20], 0x3e) // EM_X86_64
	binary.LittleEndian.PutUint32(out[20:24], 1)    // EV_CURRENT
	binary.LittleEndian.PutUint64(out[24:32], elfBase+elfCodeOffset)
	binary.LittleEndian.PutUint64(out[32:40], elfHeaderLen)
	binary.LittleEndian.PutUint64(out[40:48], 0) // no section table
	binary.LittleEndian.PutUint32(out[48:52], 0)
	binary.LittleEndian.PutUint16(out[52:54], elfHeaderLen)
	binary.LittleEndian.PutUint16(out[54:56], elfProgramLen)
	binary.LittleEndian.PutUint16(out[56:58], 1)
	ph := elfHeaderLen
	binary.LittleEndian.PutUint32(out[ph:ph+4], 1) // PT_LOAD
	binary.LittleEndian.PutUint32(out[ph+4:ph+8], 5)
	binary.LittleEndian.PutUint64(out[ph+8:ph+16], 0)
	binary.LittleEndian.PutUint64(out[ph+16:ph+24], elfBase)
	binary.LittleEndian.PutUint64(out[ph+24:ph+32], elfBase)
	binary.LittleEndian.PutUint64(out[ph+32:ph+40], uint64(fileSize))
	binary.LittleEndian.PutUint64(out[ph+40:ph+48], uint64(fileSize))
	binary.LittleEndian.PutUint64(out[ph+48:ph+56], 0x1000)
	return out
}
