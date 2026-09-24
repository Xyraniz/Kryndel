package kry

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
)

const (
	peImageBase        = uint64(0x140000000)
	peFileAlignment    = uint32(0x200)
	peSectionAlignment = uint32(0x1000)
	peTextRVA          = uint32(0x1000)
	peHeaderSize       = uint32(0x400)
)

// BuildDirectPE emits a Windows x64 console executable without invoking a
// compiler, assembler, or linker. Its intentionally narrow language slice
// supports scalar and String values, up to four register arguments, basic
// control flow, and print/println. Unsupported constructs fail before an
// executable is produced. The output calls the Windows x64 kernel32 ABI.
func BuildDirectPE(p *Program, c *Checker, target NativeTarget) ([]byte, error) {
	if err := validateNativeOutputTarget("pe-direct", target); err != nil {
		return nil, err
	}
	if p == nil || c == nil || c.Env == nil {
		return nil, fmt.Errorf("missing checked program")
	}
	kir, err := EmitKIR(p, c, target)
	if err != nil {
		return nil, err
	}
	if _, err := DecodeKIR(kir, c.Env.Lim); err != nil {
		return nil, fmt.Errorf("direct PE backend rejected KIR: %w", err)
	}
	if len(p.Statements) > 0 {
		for _, f := range p.Functions {
			if f.Name == "main" {
				return nil, fmt.Errorf("direct PE backend does not support both top-level statements and main")
			}
		}
	}
	// A trailing explicit return nil is equivalent to falling off main. Check
	// the shape here instead of broadening the static ELF slice implicitly.
	copyProgram := *p
	if len(p.Statements) == 0 {
		for i, f := range p.Functions {
			if f.Name != "main" {
				continue
			}
			if len(f.Params) != 0 || (f.Return != nil && f.Return.Name != "Nil") {
				return nil, fmt.Errorf("direct PE backend requires main() -> Nil")
			}
			copyFunction := *f
			if n := len(copyFunction.Body); n > 0 && copyFunction.Body[n-1] != nil && copyFunction.Body[n-1].Kind == StReturn {
				ret := copyFunction.Body[n-1].Return
				if ret == nil || ret.Kind != ExNil {
					return nil, fmt.Errorf("direct PE backend supports only return nil in main")
				}
				copyFunction.Body = copyFunction.Body[:n-1]
			}
			functions := append([]*Function(nil), copyProgram.Functions...)
			functions[i] = &copyFunction
			copyProgram.Functions = functions
			break
		}
	}
	output, staticErr := directStaticOutput(&copyProgram, c)
	if staticErr == nil {
		return buildDirectStaticPE(output)
	}
	stmts, err := validateDirectPEProgram(&copyProgram)
	if err != nil {
		return nil, fmt.Errorf("direct PE dynamic subset: %w (static output path: %v)", err, staticErr)
	}
	machine := newDirectMachine()
	machine.windowsABI = true
	if err := machine.prepareFunctions(&copyProgram); err != nil {
		return nil, fmt.Errorf("direct PE function setup: %w", err)
	}
	return machine.build(stmts)
}

// validateDirectPEProgram prevents Linux syscalls or non-Win64 calling
// conventions from slipping into this deliberately bounded backend. Adding a
// feature here requires its semantics and calling convention to be implemented
// below before the PE writer can accept it.
func validateDirectPEProgram(p *Program) ([]*Stmt, error) {
	if p == nil {
		return nil, fmt.Errorf("missing checked program")
	}
	if len(p.Imports) != 0 || len(p.Structs) != 0 || len(p.Enums) != 0 {
		return nil, fmt.Errorf("module imports, structs, and enums are not supported")
	}
	if len(p.Statements) != 0 {
		for _, f := range p.Functions {
			if f != nil && f.Name == "main" {
				return nil, fmt.Errorf("top-level statements cannot be combined with main()")
			}
		}
	} else {
		foundMain := false
		for _, f := range p.Functions {
			if f == nil || f.Name != "main" {
				continue
			}
			foundMain = true
			if len(f.Params) != 0 || typeSpecString(f.Return) != "Nil" {
				return nil, fmt.Errorf("main must have signature main() -> Nil")
			}
		}
		if !foundMain {
			return nil, fmt.Errorf("program requires top-level statements or main() -> Nil")
		}
	}

	for _, f := range p.Functions {
		if f == nil {
			continue
		}
		if f.Receiver != nil || f.Worker || f.Unsafe || len(f.TypeParams) != 0 {
			return nil, fmt.Errorf("function '%s' has unsupported metadata (module=%q receiver=%t worker=%t unsafe=%t type-parameters=%d)", f.Name, f.Module, f.Receiver != nil, f.Worker, f.Unsafe, len(f.TypeParams))
		}
		if f.Name == "main" {
			continue
		}
		if len(f.Params) > 4 {
			return nil, fmt.Errorf("function '%s' has %d parameters; Win64 direct PE currently supports at most four", f.Name, len(f.Params))
		}
		if !directPETypeName(typeSpecString(f.Return), true) {
			return nil, fmt.Errorf("function '%s' has unsupported return type %s", f.Name, typeSpecString(f.Return))
		}
		for _, param := range f.Params {
			if !directPETypeName(typeSpecString(param.Type), false) {
				return nil, fmt.Errorf("function '%s' parameter '%s' has unsupported type %s", f.Name, param.Name, typeSpecString(param.Type))
			}
		}
	}
	stmts, err := directDynamicStatements(p)
	if err != nil {
		return nil, err
	}
	if err := validateDirectPEStatements(stmts); err != nil {
		return nil, err
	}
	for _, f := range p.Functions {
		if f != nil && f.Name != "main" {
			if err := validateDirectPEStatements(f.Body); err != nil {
				return nil, fmt.Errorf("function '%s': %w", f.Name, err)
			}
		}
	}
	return stmts, nil
}

func directPETypeName(name string, allowNil bool) bool {
	if allowNil && name == "Nil" {
		return true
	}
	switch name {
	case "Int", "Bool", "String", "UInt8", "UInt16", "UInt32", "UInt64":
		return true
	default:
		return false
	}
}

func validateDirectPEStatements(stmts []*Stmt) error {
	for _, s := range stmts {
		if s == nil {
			continue
		}
		switch s.Kind {
		case StLet, StConst:
			if s.Init == nil || s.Init.Type == nil || !directPETypeName(s.Init.Type.String(), false) {
				return fmt.Errorf("binding '%s' has an unsupported type", s.Name)
			}
			if err := validateDirectPEExpr(s.Init, false); err != nil {
				return err
			}
		case StAssign:
			if s.Target == nil || s.Target.Kind != ExVar || s.Value == nil || s.Value.Type == nil || !directPETypeName(s.Value.Type.String(), false) {
				return fmt.Errorf("assignment requires a scalar or String binding")
			}
			if err := validateDirectPEExpr(s.Value, false); err != nil {
				return err
			}
		case StExpr:
			if s.Expr == nil || s.Expr.Kind != ExCall || s.Expr.Receiver != nil {
				return fmt.Errorf("expression statements must be direct calls")
			}
			if s.Expr.Function == nil {
				if (s.Expr.Name != "print" && s.Expr.Name != "println") || len(s.Expr.Args) != 1 {
					return fmt.Errorf("only print(value) and println(value) builtins are supported")
				}
				if err := validateDirectPEExpr(s.Expr.Args[0], true); err != nil {
					return err
				}
			} else if err := validateDirectPEExpr(s.Expr, false); err != nil {
				return err
			}
		case StIf:
			if s.Cond == nil || s.Cond.Type == nil || s.Cond.Type.Kind != TyBool {
				return fmt.Errorf("if condition must be Bool")
			}
			if err := validateDirectPEExpr(s.Cond, false); err != nil {
				return err
			}
			if err := validateDirectPEStatements(s.Then); err != nil {
				return err
			}
			if err := validateDirectPEStatements(s.Else); err != nil {
				return err
			}
		case StWhile:
			if s.Cond == nil || s.Cond.Type == nil || s.Cond.Type.Kind != TyBool {
				return fmt.Errorf("while condition must be Bool")
			}
			if err := validateDirectPEExpr(s.Cond, false); err != nil {
				return err
			}
			if err := validateDirectPEStatements(s.Body); err != nil {
				return err
			}
		case StBreak, StContinue:
			// Nesting is checked by the machine emitter.
		case StReturn:
			if s.Return != nil && s.Return.Kind != ExNil {
				if err := validateDirectPEExpr(s.Return, false); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("statement kind %s is not supported", stmtName(s.Kind))
		}
	}
	return nil
}

func validateDirectPEExpr(e *Expr, allowOutput bool) error {
	if e == nil {
		return fmt.Errorf("missing expression")
	}
	if e.Type != nil && e.Type.Kind != TyNil && !directPETypeName(e.Type.String(), false) {
		return fmt.Errorf("expression type %s is not supported", e.Type.String())
	}
	switch e.Kind {
	case ExInt, ExBool, ExString, ExVar:
		return nil
	case ExNil:
		return fmt.Errorf("Nil is supported only as a return value")
	case ExUnary:
		if e.Op != PLUS && e.Op != MINUS && e.Op != BANG && e.Op != BITNOT {
			return fmt.Errorf("unary operator %s is not supported", opText(e.Op))
		}
		return validateDirectPEExpr(e.Operand, false)
	case ExBinary:
		if e.Type == nil || e.Type.Kind == TyString {
			return fmt.Errorf("String concatenation and non-scalar binary operations are not supported")
		}
		if e.Left != nil && e.Left.Type != nil && e.Left.Type.Kind == TyString {
			return fmt.Errorf("String comparison is not supported by the direct PE runtime")
		}
		switch e.Op {
		case PLUS, MINUS, STAR, SLASH, PERCENT, BITAND, BITXOR, PIPE, SHL, SHR,
			AND, OR, EQEQ, NEQ, LESS, LEQ, GREATER, GEQ:
		default:
			return fmt.Errorf("binary operator %s is not supported", opText(e.Op))
		}
		if err := validateDirectPEExpr(e.Left, false); err != nil {
			return err
		}
		return validateDirectPEExpr(e.Right, false)
	case ExCall:
		if e.Receiver != nil {
			return fmt.Errorf("receiver calls are not supported")
		}
		if e.Function != nil {
			if len(e.Args) != len(e.Function.Params) || len(e.Args) > 4 {
				return fmt.Errorf("function '%s' must be called with all arguments and at most four parameters", e.Function.Name)
			}
			for _, arg := range e.Args {
				if err := validateDirectPEExpr(arg, false); err != nil {
					return err
				}
			}
			return nil
		}
		switch e.Name {
		case "u8", "u16", "u32", "u64":
			var bits uint8
			switch e.Name {
			case "u8":
				bits = 8
			case "u16":
				bits = 16
			case "u32":
				bits = 32
			case "u64":
				bits = 64
			}
			if len(e.Args) != 1 || e.Type == nil || e.Type.Kind != TyUInt || e.Type.Bits != bits {
				return fmt.Errorf("conversion %s must have one argument and return UInt%d", e.Name, bits)
			}
			arg := e.Args[0]
			if arg == nil || arg.Type == nil || (arg.Type.Kind != TyInt && arg.Type.Kind != TyUInt) {
				return fmt.Errorf("conversion %s requires an Int or UInt argument", e.Name)
			}
			return validateDirectPEExpr(arg, false)
		}
		if !allowOutput || (e.Name != "print" && e.Name != "println") || len(e.Args) != 1 {
			return fmt.Errorf("only statement-form print(value) and println(value) are supported")
		}
		return validateDirectPEExpr(e.Args[0], false)
	default:
		return fmt.Errorf("expression kind %s is not supported", expressionKindName(e.Kind))
	}
}

func peAlign(value, alignment uint32) uint32 {
	return (value + alignment - 1) &^ (alignment - 1)
}

func pePut16(data []byte, at int, value uint16) { binary.LittleEndian.PutUint16(data[at:], value) }
func pePut32(data []byte, at int, value uint32) { binary.LittleEndian.PutUint32(data[at:], value) }
func pePut64(data []byte, at int, value uint64) { binary.LittleEndian.PutUint64(data[at:], value) }

func buildDirectStaticPE(output []byte) ([]byte, error) {
	if uint64(len(output)) > math.MaxUint32 {
		return nil, fmt.Errorf("direct PE output exceeds WriteFile's DWORD length")
	}
	machine := newDirectMachine()
	machine.windowsABI = true
	// The entry frame gives the shared WriteFile loop aligned scratch space and
	// a standard Win64 unwindable prolog.
	machine.code = append(machine.code, 0x55, 0x48, 0x89, 0xe5)
	machine.code = append(machine.code, 0x48, 0x81, 0xec, 0x40, 0, 0, 0)
	if err := machine.emitWrite(string(output)); err != nil {
		return nil, err
	}
	if err := machine.emitJump(machine.endLabel); err != nil {
		return nil, err
	}
	machine.peFunctions = append(machine.peFunctions, peFunctionRange{begin: 0, end: len(machine.code), frame: 64})
	if err := machine.bind(machine.endLabel); err != nil {
		return nil, err
	}
	if err := machine.emitExit(0); err != nil {
		return nil, err
	}
	if err := machine.bind(machine.trapLabel); err != nil {
		return nil, err
	}
	if err := machine.emitExit(1); err != nil {
		return nil, err
	}
	return buildDirectDynamicPE(machine.code, machine.data, machine.dataRefs, machine.peImportRefs, machine.peFunctions)
}

// The .idata section contains one import descriptor, a null descriptor,
// ILT/IAT entries, and three hint/name records.
func peImportData(idataRVA uint32) (data []byte, iat [3]uint32) {
	data = make([]byte, 40)
	dllName := uint32(len(data))
	data = append(data, "KERNEL32.dll\x00"...)
	for len(data)%8 != 0 {
		data = append(data, 0)
	}
	ilt := uint32(len(data))
	data = append(data, make([]byte, 4*8)...)
	iatStart := uint32(len(data))
	data = append(data, make([]byte, 4*8)...)
	for i, name := range []string{"GetStdHandle", "WriteFile", "ExitProcess"} {
		nameRVA := idataRVA + uint32(len(data))
		pePut64(data, int(ilt)+8*i, uint64(nameRVA))
		pePut64(data, int(iatStart)+8*i, uint64(nameRVA))
		iat[i] = idataRVA + iatStart + uint32(8*i)
		data = append(data, 0, 0) // import by name, hint 0
		data = append(data, name...)
		data = append(data, 0)
		if len(data)%2 != 0 {
			data = append(data, 0)
		}
	}
	for len(data)%8 != 0 {
		data = append(data, 0)
	}
	pePut32(data, 0, idataRVA+ilt)
	pePut32(data, 12, idataRVA+dllName)
	pePut32(data, 16, idataRVA+iatStart)
	return data, iat
}

func peSection(image []byte, at int, name string, rva, length, raw, flags uint32) {
	copy(image[at:at+8], name)
	pePut32(image, at+8, length)
	pePut32(image, at+12, rva)
	pePut32(image, at+16, peAlign(length, peFileAlignment))
	pePut32(image, at+20, raw)
	pePut32(image, at+36, flags)
}

func buildDirectDynamicPE(code, rdata []byte, dataRefs []machineDataRef, importRefs []peImportRef, functions []peFunctionRange) ([]byte, error) {
	if len(code) == 0 || uint64(len(code)) > math.MaxUint32 || uint64(len(rdata)) > math.MaxUint32 {
		return nil, fmt.Errorf("direct PE image has an invalid code or data size")
	}
	if len(functions) == 0 {
		return nil, fmt.Errorf("direct PE image has no unwindable entrypoint")
	}
	if len(rdata) == 0 {
		rdata = []byte{0}
	}
	rdataRVA64 := uint64(peTextRVA) + peAlign64(uint64(len(code)), uint64(peSectionAlignment))
	if rdataRVA64 > math.MaxUint32 {
		return nil, fmt.Errorf("direct PE code section exceeds the 32-bit RVA range")
	}
	rdataRVA := uint32(rdataRVA64)
	idataRVA64 := rdataRVA64 + peAlign64(uint64(len(rdata)), uint64(peSectionAlignment))
	if idataRVA64 > math.MaxUint32 {
		return nil, fmt.Errorf("direct PE import section exceeds the 32-bit RVA range")
	}
	idataRVA := uint32(idataRVA64)
	idata, iat := peImportData(idataRVA)

	code = append([]byte(nil), code...)
	for _, ref := range dataRefs {
		if ref.displacement < 0 || ref.displacement+4 > len(code) || ref.dataOffset < 0 || ref.dataOffset > len(rdata) {
			return nil, fmt.Errorf("direct PE image has an invalid code-to-data reference")
		}
		delta := int64(rdataRVA) + int64(ref.dataOffset) - int64(peTextRVA) - int64(ref.instructionEnd)
		if delta < math.MinInt32 || delta > math.MaxInt32 {
			return nil, fmt.Errorf("direct PE data reference exceeds the x64 RIP-relative address range")
		}
		binary.LittleEndian.PutUint32(code[ref.displacement:ref.displacement+4], uint32(int32(delta)))
	}
	for _, ref := range importRefs {
		if ref.displacement < 0 || ref.displacement+4 > len(code) || ref.importIndex < 0 || ref.importIndex >= len(iat) {
			return nil, fmt.Errorf("direct PE image has an invalid import reference")
		}
		delta := int64(iat[ref.importIndex]) - int64(peTextRVA) - int64(ref.instructionEnd)
		if delta < math.MinInt32 || delta > math.MaxInt32 {
			return nil, fmt.Errorf("direct PE import reference exceeds the x64 RIP-relative address range")
		}
		binary.LittleEndian.PutUint32(code[ref.displacement:ref.displacement+4], uint32(int32(delta)))
	}

	pdataRVA64 := idataRVA64 + peAlign64(uint64(len(idata)), uint64(peSectionAlignment))
	if pdataRVA64 > math.MaxUint32 {
		return nil, fmt.Errorf("direct PE unwind section exceeds the 32-bit RVA range")
	}
	pdataRVA := uint32(pdataRVA64)
	pdata, tableSize, err := peRuntimeFunctionData(functions, pdataRVA, uint32(len(code)))
	if err != nil {
		return nil, err
	}

	textRaw := peHeaderSize
	rdataRaw64 := uint64(textRaw) + uint64(peAlign(uint32(len(code)), peFileAlignment))
	idataRaw64 := rdataRaw64 + uint64(peAlign(uint32(len(rdata)), peFileAlignment))
	pdataRaw64 := idataRaw64 + uint64(peAlign(uint32(len(idata)), peFileAlignment))
	fileSize64 := pdataRaw64 + uint64(peAlign(uint32(len(pdata)), peFileAlignment))
	imageEndRVA64 := uint64(pdataRVA) + uint64(len(pdata))
	if imageEndRVA64 > math.MaxUint32 {
		return nil, fmt.Errorf("direct PE image size exceeds the 32-bit RVA range")
	}
	imageSize64 := peAlign64(imageEndRVA64, uint64(peSectionAlignment))
	if fileSize64 > math.MaxUint32 || imageSize64 > math.MaxUint32 || fileSize64 > uint64(int(^uint(0)>>1)) {
		return nil, fmt.Errorf("direct PE image exceeds the supported file or image size")
	}
	rdataRaw, idataRaw, pdataRaw := uint32(rdataRaw64), uint32(idataRaw64), uint32(pdataRaw64)
	image := make([]byte, int(fileSize64))
	copy(image[textRaw:], code)
	copy(image[rdataRaw:], rdata)
	copy(image[idataRaw:], idata)
	copy(image[pdataRaw:], pdata)

	copy(image[:2], "MZ")
	pePut32(image, 0x3c, 0x80)
	copy(image[0x80:], "PE\x00\x00")
	coff := 0x84
	pePut16(image, coff, 0x8664)
	pePut16(image, coff+2, 4)
	pePut16(image, coff+16, 0xf0)
	pePut16(image, coff+18, 0x0022)
	opt := coff + 20
	pePut16(image, opt, 0x20b)
	pePut32(image, opt+4, peAlign(uint32(len(code)), peFileAlignment))
	pePut32(image, opt+8, peAlign(uint32(len(rdata)), peFileAlignment)+peAlign(uint32(len(idata)), peFileAlignment)+peAlign(uint32(len(pdata)), peFileAlignment))
	pePut32(image, opt+16, peTextRVA)
	pePut32(image, opt+20, peTextRVA)
	pePut64(image, opt+24, peImageBase)
	pePut32(image, opt+32, peSectionAlignment)
	pePut32(image, opt+36, peFileAlignment)
	pePut16(image, opt+40, 6)
	pePut16(image, opt+48, 6)
	pePut32(image, opt+56, uint32(imageSize64))
	pePut32(image, opt+60, peHeaderSize)
	pePut16(image, opt+68, 3)
	pePut16(image, opt+70, 0x100)
	pePut64(image, opt+72, 0x100000)
	pePut64(image, opt+80, 0x1000)
	pePut64(image, opt+88, 0x100000)
	pePut64(image, opt+96, 0x1000)
	pePut32(image, opt+108, 16)
	pePut32(image, opt+112+8, idataRVA)
	pePut32(image, opt+112+12, 40)
	pePut32(image, opt+112+8*3, pdataRVA)
	pePut32(image, opt+112+8*3+4, tableSize)
	pePut32(image, opt+112+8*12, iat[0])
	pePut32(image, opt+112+8*12+4, 4*8)
	sections := opt + 0xf0
	peSection(image, sections, ".text", peTextRVA, uint32(len(code)), textRaw, 0x60000020)
	peSection(image, sections+40, ".rdata", rdataRVA, uint32(len(rdata)), rdataRaw, 0x40000040)
	peSection(image, sections+80, ".idata", idataRVA, uint32(len(idata)), idataRaw, 0xc0000040)
	peSection(image, sections+120, ".pdata", pdataRVA, uint32(len(pdata)), pdataRaw, 0x40000040)
	return image, nil
}

func peAlign64(value, alignment uint64) uint64 {
	return (value + alignment - 1) &^ (alignment - 1)
}

func peRuntimeFunctionData(functions []peFunctionRange, sectionRVA, codeSize uint32) ([]byte, uint32, error) {
	ordered := append([]peFunctionRange(nil), functions...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].begin < ordered[j].begin })
	if uint64(len(ordered))*12 > math.MaxUint32 {
		return nil, 0, fmt.Errorf("direct PE image has too many unwind ranges")
	}
	tableSize := uint32(len(ordered) * 12)
	data := make([]byte, tableSize)
	previousEnd := uint32(0)
	for index, function := range ordered {
		if function.begin >= function.end || function.end > int(codeSize) || (index > 0 && function.begin < int(previousEnd)) {
			return nil, 0, fmt.Errorf("direct PE image has overlapping or invalid unwind ranges")
		}
		if uint64(peTextRVA)+uint64(function.end) > math.MaxUint32 {
			return nil, 0, fmt.Errorf("direct PE function exceeds the 32-bit RVA range")
		}
		unwind, err := peFrameUnwindInfo(function.frame)
		if err != nil {
			return nil, 0, err
		}
		for len(data)%4 != 0 {
			data = append(data, 0)
		}
		unwindRVA := uint64(sectionRVA) + uint64(len(data))
		if unwindRVA > math.MaxUint32 {
			return nil, 0, fmt.Errorf("direct PE unwind info exceeds the 32-bit RVA range")
		}
		data = append(data, unwind...)
		at := index * 12
		pePut32(data, at, peTextRVA+uint32(function.begin))
		pePut32(data, at+4, peTextRVA+uint32(function.end))
		pePut32(data, at+8, uint32(unwindRVA))
		previousEnd = uint32(function.end)
	}
	return data, tableSize, nil
}

func peFrameUnwindInfo(frame uint32) ([]byte, error) {
	if frame == 0 || frame%16 != 0 {
		return nil, fmt.Errorf("direct PE frame size %d is not 16-byte aligned", frame)
	}
	var codes []byte
	if frame <= 128 {
		allocationInfo := byte((frame - 8) / 8)
		codes = append(codes, 11, (allocationInfo<<4)|2) // UWOP_ALLOC_SMALL
	} else {
		if frame/8 > math.MaxUint16 {
			return nil, fmt.Errorf("direct PE frame size %d is too large for x64 unwind metadata", frame)
		}
		codes = append(codes, 11, 1) // UWOP_ALLOC_LARGE, OpInfo 0
		codes = append(codes, byte(frame/8), byte(frame/8>>8))
	}
	codes = append(codes, 4, 3)    // UWOP_SET_FPREG (RBP)
	codes = append(codes, 1, 0x50) // UWOP_PUSH_NONVOL (RBP)
	count := byte(len(codes) / 2)
	info := []byte{1, 11, count, 0x50} // version 1, prolog length, frame register RBP
	info = append(info, codes...)
	for len(info)%4 != 0 {
		info = append(info, 0)
	}
	return info, nil
}
