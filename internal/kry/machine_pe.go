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
	peSubsystemOffset  = 0x80 + 4 + 20 + 68
	peSubsystemConsole = uint16(3)
	peSubsystemGUI     = uint16(2)
)

// BuildDirectPE validates the program into MIR before lowering the supported
// KIR subset for Windows amd64.
func BuildDirectPE(p *Program, c *Checker, target NativeTarget) ([]byte, error) {
	if err := validateNativeOutputTarget("pe-direct", target); err != nil {
		return nil, err
	}
	if p == nil || c == nil || c.Env == nil {
		return nil, fmt.Errorf("missing checked program")
	}
	mir, err := CompileMIR(p, c, target)
	if err != nil {
		return nil, err
	}
	return lowerDirectPEKIR(mir, mir.limits)
}

// directPETypeName reports whether a KIR type spelling is supported by PE lowering.
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

func peAlign(value, alignment uint32) uint32 {
	return (value + alignment - 1) &^ (alignment - 1)
}

func pePut16(data []byte, at int, value uint16) { binary.LittleEndian.PutUint16(data[at:], value) }
func pePut32(data []byte, at int, value uint32) { binary.LittleEndian.PutUint32(data[at:], value) }
func pePut64(data []byte, at int, value uint64) { binary.LittleEndian.PutUint64(data[at:], value) }

func buildDirectStaticPE(output [][]byte, gui bool, limits Limits) ([]byte, error) {
	var outputLength uint64
	for _, chunk := range output {
		if uint64(len(chunk)) > math.MaxUint32-outputLength {
			return nil, fmt.Errorf("direct PE output exceeds WriteFile's DWORD length")
		}
		outputLength += uint64(len(chunk))
	}
	machine := newDirectMachine()
	machine.windowsABI = true
	machine.outputLimit = limits.MaxOutputBytes
	machine.outputLimitSet = true
	// The entry frame gives the shared WriteFile loop aligned scratch space and
	// a standard Win64 unwindable prolog.
	machine.code = append(machine.code, 0x55, 0x48, 0x89, 0xe5)
	machine.code = append(machine.code, 0x48, 0x81, 0xec, 0x40, 0, 0, 0)
	machine.emitOutputCounterInit(8)
	wallFailure := -1
	if limits.MaxWallTimeMS != 0 {
		wallFailure = machine.newLabel()
	}
	emitPEWallClockStart(machine, limits.MaxWallTimeMS)
	if err := emitPEWallClockCheck(machine, limits.MaxWallTimeMS, wallFailure); err != nil {
		return nil, err
	}
	if limits.MaxWallTimeMS == 0 {
		var combined []byte
		for _, chunk := range output {
			combined = append(combined, chunk...)
		}
		if err := machine.emitWrite(string(combined)); err != nil {
			return nil, err
		}
	} else {
		for _, chunk := range output {
			if err := emitPEWallClockCheck(machine, limits.MaxWallTimeMS, wallFailure); err != nil {
				return nil, err
			}
			if len(chunk) != 0 {
				if err := machine.emitWrite(string(chunk)); err != nil {
					return nil, err
				}
			}
			if err := emitPEWallClockCheck(machine, limits.MaxWallTimeMS, wallFailure); err != nil {
				return nil, err
			}
		}
		if err := emitPEWallClockCheck(machine, limits.MaxWallTimeMS, wallFailure); err != nil {
			return nil, err
		}
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
	if wallFailure >= 0 {
		if err := machine.bind(wallFailure); err != nil {
			return nil, err
		}
		if err := machine.emitPEWriteStderr("kryndel: wall-clock execution limit exceeded\n"); err != nil {
			return nil, err
		}
	}
	image, err := buildDirectDynamicPE(machine.code, machine.data, machine.dataRefs, machine.peImportRefs, machine.peFunctions)
	if err != nil {
		return nil, err
	}
	if gui {
		pePut16(image, peSubsystemOffset, peSubsystemGUI)
	}
	return image, nil
}

// The .idata section contains the descriptor, ILT/IAT entries, and import names.
func peImportData(idataRVA uint32) (data []byte, iat [6]uint32) {
	names := [...]string{"GetStdHandle", "WriteFile", "ExitProcess", "GetProcessHeap", "HeapAlloc", "GetTickCount64"}
	data = make([]byte, 40)
	dllName := uint32(len(data))
	data = append(data, "KERNEL32.dll\x00"...)
	for len(data)%8 != 0 {
		data = append(data, 0)
	}
	ilt := uint32(len(data))
	data = append(data, make([]byte, (len(names)+1)*8)...)
	iatStart := uint32(len(data))
	data = append(data, make([]byte, (len(names)+1)*8)...)
	for i, name := range names {
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
	pePut16(image, opt+68, peSubsystemConsole)
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
	pePut32(image, opt+112+8*12+4, uint32((len(iat)+1)*8))
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
