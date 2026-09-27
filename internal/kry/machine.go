package kry

import (
	"encoding/binary"
	"errors"
	"fmt"
)

var errDirectOutputLimit = errors.New("direct ELF output exceeds configured output limit")

const (
	elfBase       uint64 = 0x400000
	elfHeaderLen         = 64
	elfProgramLen        = 56
	elfCodeOffset        = elfHeaderLen + elfProgramLen
)

// BuildDirectELF emits a small, dependency-free ELF64 executable directly.
// The bounded scalar/control-flow slice is lowered from decoded KIR into
// runtime x86-64 instructions. Other programs continue through the existing
// static or AST-backed dynamic emitters, which support additional constructs
// including SysV AMD64 function calls and the immutable qword-array ABI.
// Every path rejects unsupported semantics before returning executable bytes.
func BuildDirectELF(p *Program, c *Checker, target NativeTarget) ([]byte, error) {
	if target.OS != "linux" || target.Arch != "amd64" {
		return nil, fmt.Errorf("direct ELF backend currently supports only linux-amd64")
	}
	if p == nil || c == nil {
		return nil, fmt.Errorf("missing checked program")
	}
	if err := validateFunctionValueSupport(p, "elf-direct"); err != nil {
		return nil, err
	}
	if err := validateNativeFeatureSupport(p, c, "elf-direct", target); err != nil {
		return nil, err
	}
	// Force every direct backend build through the public interchange format.
	// This catches schema drift before the machine emitter is allowed to run.
	kir, err := EmitKIR(p, c, target)
	if err != nil {
		return nil, err
	}
	document, err := DecodeKIR(kir, c.Env.Lim)
	if err != nil {
		return nil, fmt.Errorf("direct backend rejected KIR: %w", err)
	}
	if err := validateKIRDirectELFSubset(document); err == nil {
		return buildDirectKIRELF(document, c.Env.Lim, kirSourceMap(p))
	} else if !errors.Is(err, errKIRSubsetUnsupported) {
		return nil, fmt.Errorf("direct backend rejected KIR: %w", err)
	}
	output, err := directStaticKIROutput(document, c.Env.Lim.MaxOutputBytes)
	if err == nil {
		return emitELF64WriteExit(output), nil
	}
	if !errors.Is(err, errDirectOutputLimit) {
		if legacyOutput, legacyErr := directStaticOutput(p, c); legacyErr == nil {
			return emitELF64WriteExit(legacyOutput), nil
		} else {
			err = legacyErr
		}
	}
	stmts, stmtErr := directDynamicStatements(p)
	if stmtErr == nil && (errors.Is(err, errDirectOutputLimit) || directHasDynamicControl(stmts) || directHasArrayFeatures(stmts) || directHasStructuredFeatures(stmts) || directHasUserFunctions(p)) {
		return buildDirectDynamicELF(p, c)
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
			return nil, errDirectOutputLimit
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
	// Keep the static byte-stable source path small, but use the same reliable
	// write loop as dynamic ELF output so short writes and syscall errors fail.
	machine := newDirectMachine()
	if len(data) > 0 {
		_ = machine.emitWriteRaw(string(data))
	}
	_ = machine.bind(machine.endLabel)
	_ = machine.emitExit(0)
	_ = machine.bind(machine.trapLabel)
	_ = machine.emitExit(1)
	for _, ref := range machine.dataRefs {
		dataAddress := elfCodeOffset + len(machine.code) + ref.dataOffset
		nextInstruction := elfCodeOffset + ref.instructionEnd
		binary.LittleEndian.PutUint32(machine.code[ref.displacement:ref.displacement+4], uint32(int32(dataAddress-nextInstruction)))
	}
	return emitELF64CodeData(machine.code, machine.data)
}
