package kry

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelfhostPEBackendBuildsGroupedDLLImportsAndIndexedIATPatches(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	backendPath := filepath.Join(root, "..", "..", "selfhost", "pe_backend.kry")
	backend, err := os.ReadFile(backendPath)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pe_backend.kry"), backend, 0o600); err != nil {
		t.Fatal(err)
	}

	imports := [][2]string{
		{"KERNEL32.dll", "GetStdHandle"},
		{"KERNEL32.dll", "WriteFile"},
		{"KERNEL32.dll", "ExitProcess"},
		{"USER32.dll", "MessageBoxA"},
		{"ADVAPI32.dll", "RegOpenKeyExW"},
		// Revisit the first DLL after other groups. Its IAT slot is therefore
		// different from the source import index and exercises index remapping.
		{"KERNEL32.dll", "GetCurrentProcessId"},
	}
	importRows := make([]string, 0, len(imports))
	for _, item := range imports {
		importRows = append(importRows, fmt.Sprintf("[\"%s\", \"%s\"]", item[0], item[1]))
	}
	code := []byte{0x55, 0x48, 0x89, 0xe5, 0x48, 0x81, 0xec, 16, 0, 0, 0}
	patchPositions := make([]int, 0, len(imports))
	patchRows := make([]string, 0, len(imports))
	for index := range imports {
		callOffset := len(code)
		code = append(code, 0xff, 0x15, 0, 0, 0, 0)
		patchPositions = append(patchPositions, callOffset)
		patchRows = append(patchRows, fmt.Sprintf("[%d, %d, %d]", callOffset+2, callOffset+6, index))
	}
	code = append(code, 0x48, 0x81, 0xc4, 16, 0, 0, 0, 0x5d, 0xc3)
	codeValues := make([]string, len(code))
	for index, value := range code {
		codeValues[index] = fmt.Sprintf("u8(%d)", value)
	}
	source := fmt.Sprintf(`
import "pe_backend"

fn main() -> Nil {
    let code: Array[UInt8] = [%s]
    let imports: Array[Array[String]] = [%s]
    let patches: Array[Array[Int]] = [%s]
    let ranges: Array[Array[Int]] = [[0, %d, 16]]
    let image: Result[Bytes, String] = emit_pe32plus_with_imports_and_data_patches(code, [], imports, [], patches, ranges)
    let arguments: Array[String] = process_args()
    let write_result: Result[Nil, String] = fs_write_bytes(arguments[0], result_unwrap(image))
    let written: Nil = result_unwrap(write_result)
    let empty_image: Result[Bytes, String] = emit_pe32plus_with_imports_and_data_patches(code, [], [], [], [], ranges)
    let empty_write_result: Result[Nil, String] = fs_write_bytes(arguments[1], result_unwrap(empty_image))
    let empty_written: Nil = result_unwrap(empty_write_result)
}
`, strings.Join(codeValues, ", "), strings.Join(importRows, ", "), strings.Join(patchRows, ", "), len(code))
	mainPath := filepath.Join(dir, "main.kry")
	if err := os.WriteFile(mainPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	program, diagnostic := LoadProgram(mainPath, DefaultLimits(), "")
	if diagnostic != nil {
		t.Fatalf("load test program: %s", diagnostic.Message)
	}
	checker, diagnostic := Check(program, DefaultLimits())
	if diagnostic != nil {
		t.Fatalf("check test program at %d:%d: %s", diagnostic.Line, diagnostic.Column, diagnostic.Message)
	}
	outputPath := filepath.Join(dir, "imports.exe")
	emptyOutputPath := filepath.Join(dir, "no-imports.exe")
	runtime, diagnostic := NewRuntimeWithArgs(program, checker, DefaultLimits(), Sandbox{}, []string{outputPath, emptyOutputPath})
	if diagnostic != nil {
		t.Fatalf("create runtime: %s", diagnostic.Message)
	}
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatalf("run import table fixture: %s", diagnostic.Message)
	}
	image, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	file, err := pe.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("parse generated PE: %v", err)
	}
	optional, ok := file.OptionalHeader.(*pe.OptionalHeader64)
	if !ok {
		t.Fatalf("optional header has type %T, want PE32+", file.OptionalHeader)
	}

	sectionBytes := func(rva uint32, size uint32) []byte {
		t.Helper()
		for _, section := range file.Sections {
			if rva >= section.VirtualAddress && rva+size <= section.VirtualAddress+section.VirtualSize {
				data, dataErr := section.Data()
				if dataErr != nil {
					t.Fatalf("read section %q: %v", section.Name, dataErr)
				}
				offset := rva - section.VirtualAddress
				if uint32(len(data)) < offset+size {
					t.Fatalf("RVA range %#x+%d exceeds section %q raw bytes", rva, size, section.Name)
				}
				return data[offset : offset+size]
			}
		}
		t.Fatalf("RVA range %#x+%d is not in a section", rva, size)
		return nil
	}
	readCString := func(rva uint32) string {
		t.Helper()
		for size := uint32(1); size <= 4096; size++ {
			data := sectionBytes(rva, size)
			if end := bytes.IndexByte(data, 0); end >= 0 {
				return string(data[:end])
			}
		}
		t.Fatalf("unterminated import name at RVA %#x", rva)
		return ""
	}
	importDirectory := optional.DataDirectory[1]
	if importDirectory.VirtualAddress == 0 || importDirectory.Size != 4*20 {
		t.Fatalf("import directory is RVA %#x size %d, want 4 descriptors including terminator", importDirectory.VirtualAddress, importDirectory.Size)
	}
	var symbols []string
	sawNullDescriptor := false
	for descriptorOffset := uint32(0); descriptorOffset < importDirectory.Size; descriptorOffset += 20 {
		descriptor := sectionBytes(importDirectory.VirtualAddress+descriptorOffset, 20)
		originalFirstThunk := binary.LittleEndian.Uint32(descriptor[0:4])
		nameRVA := binary.LittleEndian.Uint32(descriptor[12:16])
		firstThunk := binary.LittleEndian.Uint32(descriptor[16:20])
		if originalFirstThunk == 0 && nameRVA == 0 && firstThunk == 0 {
			if descriptorOffset+20 != importDirectory.Size {
				t.Fatalf("null import descriptor at %#x is not the final descriptor", descriptorOffset)
			}
			sawNullDescriptor = true
			break
		}
		dll := readCString(nameRVA)
		if originalFirstThunk == 0 || firstThunk == 0 {
			t.Fatalf("incomplete import descriptor for %s", dll)
		}
		for index := uint32(0); ; index++ {
			lookup := sectionBytes(originalFirstThunk+index*8, 8)
			nameEntryRVA := binary.LittleEndian.Uint64(lookup)
			if nameEntryRVA == 0 {
				if binary.LittleEndian.Uint64(sectionBytes(firstThunk+index*8, 8)) != 0 {
					t.Fatalf("IAT terminator for %s is not zero", dll)
				}
				break
			}
			if nameEntryRVA>>63 != 0 {
				t.Fatalf("ordinal import %#x is outside the name-import fixture", nameEntryRVA)
			}
			symbols = append(symbols, dll+"!"+readCString(uint32(nameEntryRVA)+2))
		}
	}
	if !sawNullDescriptor {
		t.Fatal("import directory has no terminating null descriptor")
	}
	wantSymbols := []string{
		"KERNEL32.dll!GetStdHandle",
		"KERNEL32.dll!WriteFile",
		"KERNEL32.dll!ExitProcess",
		"KERNEL32.dll!GetCurrentProcessId",
		"USER32.dll!MessageBoxA",
		"ADVAPI32.dll!RegOpenKeyExW",
	}
	if fmt.Sprint(symbols) != fmt.Sprint(wantSymbols) {
		t.Fatalf("generated import groups %v, want %v", symbols, wantSymbols)
	}

	iatDirectory := optional.DataDirectory[12]
	if iatDirectory.VirtualAddress == 0 || iatDirectory.Size != uint32((len(imports)+3)*8) {
		t.Fatalf("IAT directory is RVA %#x size %d, want %d bytes for six imports and three group terminators", iatDirectory.VirtualAddress, iatDirectory.Size, (len(imports)+3)*8)
	}
	gotIATSlotRVA := make(map[string]uint32, len(imports))
	for descriptorOffset := uint32(0); descriptorOffset < importDirectory.Size-20; descriptorOffset += 20 {
		descriptor := sectionBytes(importDirectory.VirtualAddress+descriptorOffset, 20)
		originalFirstThunk := binary.LittleEndian.Uint32(descriptor[0:4])
		nameRVA := binary.LittleEndian.Uint32(descriptor[12:16])
		firstThunk := binary.LittleEndian.Uint32(descriptor[16:20])
		dll := readCString(nameRVA)
		for index := uint32(0); ; index++ {
			lookup := sectionBytes(originalFirstThunk+index*8, 8)
			nameEntryRVA := binary.LittleEndian.Uint64(lookup)
			if nameEntryRVA == 0 {
				break
			}
			symbol := readCString(uint32(nameEntryRVA) + 2)
			gotIATSlotRVA[dll+"!"+symbol] = firstThunk + index*8
		}
	}
	textSection := file.Section(".text")
	if textSection == nil {
		t.Fatal("generated PE has no .text section")
	}
	textBytes, err := textSection.Data()
	if err != nil {
		t.Fatalf("read .text: %v", err)
	}
	for importIndex, callOffset := range patchPositions {
		call := textBytes[callOffset : callOffset+6]
		if !bytes.Equal(call[:2], []byte{0xff, 0x15}) {
			t.Fatalf("patch %d does not target an indirect call: % x", importIndex, call)
		}
		displacement := int64(int32(binary.LittleEndian.Uint32(call[2:])))
		patchTargetRVA := int64(textSection.VirtualAddress) + int64(callOffset+6) + displacement
		wantSlot := gotIATSlotRVA[imports[importIndex][0]+"!"+imports[importIndex][1]]
		if patchTargetRVA != int64(wantSlot) {
			t.Fatalf("code patch %d targets IAT RVA %#x, want import %s at %#x", importIndex, patchTargetRVA, imports[importIndex][0]+"!"+imports[importIndex][1], wantSlot)
		}
		if uint32(patchTargetRVA) < iatDirectory.VirtualAddress || uint32(patchTargetRVA)+8 > iatDirectory.VirtualAddress+iatDirectory.Size {
			t.Fatalf("code patch %d targets %#x outside IAT data directory %#x+%d", importIndex, patchTargetRVA, iatDirectory.VirtualAddress, iatDirectory.Size)
		}
	}

	emptyImage, err := os.ReadFile(emptyOutputPath)
	if err != nil {
		t.Fatal(err)
	}
	emptyFile, err := pe.NewFile(bytes.NewReader(emptyImage))
	if err != nil {
		t.Fatalf("parse generated PE with no imports: %v", err)
	}
	emptyOptional, ok := emptyFile.OptionalHeader.(*pe.OptionalHeader64)
	if !ok {
		t.Fatalf("no-import optional header has type %T, want PE32+", emptyFile.OptionalHeader)
	}
	if emptyDirectory := emptyOptional.DataDirectory[1]; emptyDirectory.VirtualAddress == 0 || emptyDirectory.Size != 20 {
		t.Fatalf("empty import directory is RVA %#x size %d, want a 20-byte null descriptor", emptyDirectory.VirtualAddress, emptyDirectory.Size)
	}
	if emptyIAT := emptyOptional.DataDirectory[12]; emptyIAT.VirtualAddress != 0 || emptyIAT.Size != 0 {
		t.Fatalf("no-import IAT directory is RVA %#x size %d, want 0/0", emptyIAT.VirtualAddress, emptyIAT.Size)
	}
	emptySymbols, err := emptyFile.ImportedSymbols()
	if err != nil {
		t.Fatalf("read empty image import table: %v", err)
	}
	if len(emptySymbols) != 0 {
		t.Fatalf("no-import PE reported symbols %v", emptySymbols)
	}
}
