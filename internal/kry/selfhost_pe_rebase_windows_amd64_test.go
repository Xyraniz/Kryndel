//go:build windows && amd64

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
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	peProbeDirectoryImport    = 1
	peProbeDirectoryBaseReloc = 5
	peProbeRelBasedDir64      = 10

	// PROCESS_CREATION_MITIGATION_POLICY_FORCE_RELOCATE_IMAGES_ALWAYS_ON_REQ_RELOCS.
	// The option is encoded in bits 8-9 of the 64-bit creation policy.
	peProbeForceRelocateImagesAlwaysOnRequireRelocs = uint64(0x00000300)
)

func TestSelfhostPEForcedRebaseFixesDIR64AnchorInMemory(t *testing.T) {
	image, diagnostic := runSelfhostPEBackend(t, `fn main() -> Nil { println("relocation probe") }`)
	if diagnostic != nil {
		t.Fatalf("selfhost PE backend failed: %v", diagnostic)
	}

	probe, err := makeSelfhostPERebaseProbe(image)
	if err != nil {
		t.Fatalf("instrument generated PE with a relocation probe: %v", err)
	}
	executable := filepath.Join(t.TempDir(), "selfhost-pe-forced-rebase.exe")
	if err := os.WriteFile(executable, probe, 0o700); err != nil {
		t.Fatal(err)
	}

	exitCode, err := runWithForcedRelocationPolicy(executable)
	if err != nil {
		t.Fatalf("start probe with forced-rebase mitigation: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("forced-rebase probe exited %d: 1 means GetModuleHandleW(NULL) returned the preferred ImageBase; 2 means the loaded DIR64 anchor did not equal module base + 0x1000", exitCode)
	}
}

// makeSelfhostPERebaseProbe keeps the selfhost writer's imports, headers and
// relocation table, but replaces its entry code with a tiny Win64 probe. It
// reuses the GetStdHandle thunk as GetModuleHandleW, then reads the writer's
// DIR64 anchor and exits with a distinct code if either runtime check fails.
func makeSelfhostPERebaseProbe(image []byte) ([]byte, error) {
	file, err := pe.NewFile(bytes.NewReader(image))
	if err != nil {
		return nil, fmt.Errorf("parse generated PE: %w", err)
	}
	optional, ok := file.OptionalHeader.(*pe.OptionalHeader64)
	if !ok {
		return nil, fmt.Errorf("optional header has type %T, want PE32+", file.OptionalHeader)
	}
	textSection := file.Section(".text")
	rdataSection := file.Section(".rdata")
	if textSection == nil || rdataSection == nil {
		return nil, fmt.Errorf("generated PE is missing .text or .rdata")
	}

	anchorRVA, err := findSelfhostPEAnchorRVA(image, file, optional)
	if err != nil {
		return nil, err
	}
	anchorOffset, err := selfhostPERVAFileOffset(image, file, anchorRVA, 8)
	if err != nil {
		return nil, fmt.Errorf("locate DIR64 anchor: %w", err)
	}
	if got, want := binary.LittleEndian.Uint64(image[anchorOffset:]), optional.ImageBase+0x1000; got != want {
		return nil, fmt.Errorf("on-disk DIR64 anchor is %#x, want preferred-base pointer %#x", got, want)
	}

	getModuleHandleIATRVA, exitProcessIATRVA, getStdHandleINTOffset, err := selfhostPEProbeImportSlots(image, file, optional)
	if err != nil {
		return nil, err
	}

	// Store IMAGE_IMPORT_BY_NAME in unused .rdata raw padding and extend the
	// section's virtual size so the loader can resolve the rewritten thunk.
	rdataRaw, err := rdataSection.Data()
	if err != nil {
		return nil, fmt.Errorf("read generated .rdata: %w", err)
	}
	nameOffset := int(rdataSection.VirtualSize)
	if nameOffset%2 != 0 {
		nameOffset++
	}
	nameRecord := append([]byte{0, 0}, []byte("GetModuleHandleW\x00")...)
	if nameOffset+len(nameRecord) > len(rdataRaw) {
		return nil, fmt.Errorf(".rdata has no room for import probe name: virtual size %d, raw size %d", rdataSection.VirtualSize, len(rdataRaw))
	}
	nextRVA := uint64(rdataSection.VirtualAddress) + uint64(nameOffset+len(nameRecord))
	if nextRVA > uint64(rdataSection.VirtualAddress)+0x1000 {
		return nil, fmt.Errorf("probe import name exceeds the .rdata section page")
	}
	nameRVA := rdataSection.VirtualAddress + uint32(nameOffset)
	rdataFileOffset := int(rdataSection.Offset) + nameOffset
	copy(image[rdataFileOffset:rdataFileOffset+len(nameRecord)], nameRecord)
	binary.LittleEndian.PutUint64(image[getStdHandleINTOffset:], uint64(nameRVA))

	textHeaderOffset, err := selfhostPESectionHeaderOffset(image, file, ".text")
	if err != nil {
		return nil, err
	}
	rdataHeaderOffset, err := selfhostPESectionHeaderOffset(image, file, ".rdata")
	if err != nil {
		return nil, err
	}
	probeCode, err := buildSelfhostPERebaseProbe(uint32(textSection.VirtualAddress), optional.ImageBase, anchorRVA, getModuleHandleIATRVA, exitProcessIATRVA)
	if err != nil {
		return nil, err
	}
	if len(probeCode) > int(textSection.Size) {
		return nil, fmt.Errorf("probe needs %d .text bytes; generated section has raw size %d", len(probeCode), textSection.Size)
	}
	copy(image[int(textSection.Offset):int(textSection.Offset)+len(probeCode)], probeCode)
	binary.LittleEndian.PutUint32(image[textHeaderOffset+8:], uint32(len(probeCode)))
	binary.LittleEndian.PutUint32(image[rdataHeaderOffset+8:], uint32(nameOffset+len(nameRecord)))

	// Make the fixture depend on the explicit process policy rather than on
	// ordinary IMAGE_DLLCHARACTERISTICS_DYNAMIC_BASE randomization. Forcing
	// relocation on an image without that opt-in is the collision case described
	// by the Win32 mitigation contract. The original generated image is unchanged.
	ntHeaderOffset := int(binary.LittleEndian.Uint32(image[0x3c:]))
	optionalHeaderOffset := ntHeaderOffset + 4 + 20
	const imageDLLCharacteristicsDynamicBase = uint16(0x0040)
	dllCharacteristicsOffset := optionalHeaderOffset + 70
	characteristics := binary.LittleEndian.Uint16(image[dllCharacteristicsOffset:])
	binary.LittleEndian.PutUint16(image[dllCharacteristicsOffset:], characteristics&^imageDLLCharacteristicsDynamicBase)

	return image, nil
}

func findSelfhostPEAnchorRVA(image []byte, file *pe.File, optional *pe.OptionalHeader64) (uint32, error) {
	directory := optional.DataDirectory[peProbeDirectoryBaseReloc]
	if directory.VirtualAddress == 0 || directory.Size < 8 {
		return 0, fmt.Errorf("generated PE has no usable base relocation directory")
	}
	offset, err := selfhostPERVAFileOffset(image, file, directory.VirtualAddress, int(directory.Size))
	if err != nil {
		return 0, fmt.Errorf("locate base relocation directory: %w", err)
	}
	dir := image[offset : offset+int(directory.Size)]
	var targets []uint32
	for blockOffset := 0; blockOffset < len(dir); {
		if blockOffset+8 > len(dir) {
			return 0, fmt.Errorf("truncated relocation block at offset %d", blockOffset)
		}
		pageRVA := binary.LittleEndian.Uint32(dir[blockOffset:])
		blockSize := int(binary.LittleEndian.Uint32(dir[blockOffset+4:]))
		if blockSize < 8 || blockSize%2 != 0 || blockOffset+blockSize > len(dir) {
			return 0, fmt.Errorf("invalid relocation block at offset %d: size %d", blockOffset, blockSize)
		}
		for entryOffset := blockOffset + 8; entryOffset < blockOffset+blockSize; entryOffset += 2 {
			entry := binary.LittleEndian.Uint16(dir[entryOffset:])
			if entry>>12 == peProbeRelBasedDir64 {
				targets = append(targets, pageRVA+uint32(entry&0x0fff))
			} else if entry>>12 != 0 {
				return 0, fmt.Errorf("generated relocation directory contains unsupported type %d", entry>>12)
			}
		}
		blockOffset += blockSize
	}
	if len(targets) != 1 {
		return 0, fmt.Errorf("generated PE has %d DIR64 targets, want exactly the writer-owned anchor", len(targets))
	}
	if section := file.Section(".rdata"); section == nil || targets[0] < section.VirtualAddress || uint64(targets[0])+8 > uint64(section.VirtualAddress)+uint64(section.VirtualSize) {
		return 0, fmt.Errorf("DIR64 target RVA %#x is outside the generated .rdata data", targets[0])
	}
	return targets[0], nil
}

func selfhostPEProbeImportSlots(image []byte, file *pe.File, optional *pe.OptionalHeader64) (getModuleHandleIATRVA uint32, exitProcessIATRVA uint32, getStdHandleINTOffset int, err error) {
	directory := optional.DataDirectory[peProbeDirectoryImport]
	if directory.VirtualAddress == 0 || directory.Size < 20 {
		return 0, 0, 0, fmt.Errorf("generated PE has no usable import directory")
	}
	for descriptorOffset := uint32(0); descriptorOffset+20 <= directory.Size; descriptorOffset += 20 {
		descriptorRVA := directory.VirtualAddress + descriptorOffset
		descriptorFileOffset, offsetErr := selfhostPERVAFileOffset(image, file, descriptorRVA, 20)
		if offsetErr != nil {
			return 0, 0, 0, fmt.Errorf("locate import descriptor: %w", offsetErr)
		}
		descriptor := image[descriptorFileOffset : descriptorFileOffset+20]
		lookupRVA := binary.LittleEndian.Uint32(descriptor[0:4])
		dllNameRVA := binary.LittleEndian.Uint32(descriptor[12:16])
		iatRVA := binary.LittleEndian.Uint32(descriptor[16:20])
		if lookupRVA == 0 && dllNameRVA == 0 && iatRVA == 0 {
			break
		}
		dllName, nameErr := selfhostPECStringAtRVA(image, file, dllNameRVA)
		if nameErr != nil {
			return 0, 0, 0, nameErr
		}
		if !strings.EqualFold(dllName, "KERNEL32.dll") {
			continue
		}
		if lookupRVA == 0 {
			lookupRVA = iatRVA
		}
		for index := uint32(0); ; index++ {
			entryRVA := lookupRVA + index*8
			entryOffset, entryErr := selfhostPERVAFileOffset(image, file, entryRVA, 8)
			if entryErr != nil {
				return 0, 0, 0, fmt.Errorf("locate KERNEL32 import thunk: %w", entryErr)
			}
			nameRVA := binary.LittleEndian.Uint64(image[entryOffset:])
			if nameRVA == 0 {
				break
			}
			if nameRVA>>63 != 0 {
				return 0, 0, 0, fmt.Errorf("generated KERNEL32 import uses an ordinal, expected name imports")
			}
			symbol, symbolErr := selfhostPECStringAtRVA(image, file, uint32(nameRVA)+2)
			if symbolErr != nil {
				return 0, 0, 0, symbolErr
			}
			switch symbol {
			case "GetStdHandle":
				getStdHandleINTOffset = entryOffset
				getModuleHandleIATRVA = iatRVA + index*8
			case "ExitProcess":
				exitProcessIATRVA = iatRVA + index*8
			}
		}
	}
	if getStdHandleINTOffset == 0 || getModuleHandleIATRVA == 0 || exitProcessIATRVA == 0 {
		return 0, 0, 0, fmt.Errorf("generated KERNEL32 imports lack GetStdHandle or ExitProcess")
	}
	return getModuleHandleIATRVA, exitProcessIATRVA, getStdHandleINTOffset, nil
}

func selfhostPECStringAtRVA(image []byte, file *pe.File, rva uint32) (string, error) {
	offset, err := selfhostPERVAFileOffset(image, file, rva, 1)
	if err != nil {
		return "", err
	}
	for end := offset; end < len(image) && end-offset < 4096; end++ {
		if image[end] == 0 {
			return string(image[offset:end]), nil
		}
	}
	return "", fmt.Errorf("unterminated PE string at RVA %#x", rva)
}

func selfhostPERVAFileOffset(image []byte, file *pe.File, rva uint32, size int) (int, error) {
	if size < 0 {
		return 0, fmt.Errorf("negative RVA range size %d", size)
	}
	for _, section := range file.Sections {
		if rva < section.VirtualAddress {
			continue
		}
		delta := uint64(rva - section.VirtualAddress)
		if delta+uint64(size) > uint64(section.Size) {
			continue
		}
		offset := uint64(section.Offset) + delta
		if offset+uint64(size) > uint64(len(image)) {
			return 0, fmt.Errorf("RVA %#x range exceeds file bytes", rva)
		}
		return int(offset), nil
	}
	return 0, fmt.Errorf("RVA %#x with size %d is not backed by section raw data", rva, size)
}

func selfhostPESectionHeaderOffset(image []byte, file *pe.File, name string) (int, error) {
	ntHeaderOffset := int(binary.LittleEndian.Uint32(image[0x3c:]))
	sectionTableOffset := ntHeaderOffset + 4 + 20 + int(file.FileHeader.SizeOfOptionalHeader)
	for index, section := range file.Sections {
		if section.Name == name {
			offset := sectionTableOffset + index*40
			if offset+40 > len(image) {
				return 0, fmt.Errorf("section header for %s exceeds PE bytes", name)
			}
			return offset, nil
		}
	}
	return 0, fmt.Errorf("section %s is not present", name)
}

func buildSelfhostPERebaseProbe(textRVA uint32, preferredBase uint64, anchorRVA, getModuleHandleIATRVA, exitProcessIATRVA uint32) ([]byte, error) {
	var err error
	code := []byte{
		0x55,             // push rbp
		0x48, 0x89, 0xe5, // mov rbp,rsp
		0x48, 0x83, 0xec, 0x20, // sub rsp,32 (Win64 shadow space)
		0x31, 0xc9, // xor ecx,ecx (GetModuleHandleW(NULL))
		0xff, 0x15, 0, 0, 0, 0, // call [rip+GetModuleHandleW IAT]
		0x48, 0x89, 0x45, 0xf8, // mov [rbp-8],rax
		0x48, 0xb9, // mov rcx, preferred ImageBase
	}
	code = binary.LittleEndian.AppendUint64(code, preferredBase)
	code = append(code,
		0x48, 0x39, 0xc8, // cmp rax,rcx
		0x75, 0x00, // jne relocated (disp patched below)
	)
	relocatedBranchDisp := len(code) - 1
	if err := patchSelfhostPEProbeRel32(code, 12, textRVA+16, getModuleHandleIATRVA); err != nil {
		return nil, err
	}
	code, err = appendSelfhostPEExitProbe(code, textRVA, exitProcessIATRVA, 1)
	if err != nil {
		return nil, err
	}
	relocatedLabel := len(code)
	if relocatedLabel-(relocatedBranchDisp+1) > 127 {
		return nil, fmt.Errorf("relocated branch exceeds rel8 range")
	}
	code[relocatedBranchDisp] = byte(relocatedLabel - (relocatedBranchDisp + 1))

	code = append(code, 0x48, 0x8b, 0x0d)
	anchorDispOffset := len(code)
	code = append(code, 0, 0, 0, 0)
	anchorInstructionEnd := uint32(len(code))
	if err := patchSelfhostPEProbeRel32(code, anchorDispOffset, textRVA+anchorInstructionEnd, anchorRVA); err != nil {
		return nil, err
	}
	code = append(code,
		0x48, 0x8b, 0x55, 0xf8, // mov rdx,[rbp-8]
		0x48, 0x81, 0xc2, 0x00, 0x10, 0x00, 0x00, // add rdx,0x1000
		0x48, 0x39, 0xd1, // cmp rcx,rdx
		0x74, 0x00, // je fixed (disp patched below)
	)
	fixedBranchDisp := len(code) - 1
	code, err = appendSelfhostPEExitProbe(code, textRVA, exitProcessIATRVA, 2)
	if err != nil {
		return nil, err
	}
	fixedLabel := len(code)
	if fixedLabel-(fixedBranchDisp+1) > 127 {
		return nil, fmt.Errorf("anchor comparison branch exceeds rel8 range")
	}
	code[fixedBranchDisp] = byte(fixedLabel - (fixedBranchDisp + 1))
	code = append(code, 0x31, 0xc9) // xor ecx,ecx (success)
	code, err = appendSelfhostPEIATCall(code, textRVA, exitProcessIATRVA)
	if err != nil {
		return nil, err
	}
	code = append(code, 0xcc) // int3 if ExitProcess unexpectedly returns
	return code, nil
}

func appendSelfhostPEExitProbe(code []byte, textRVA, exitProcessIATRVA uint32, exitCode uint32) ([]byte, error) {
	code = append(code, 0xb9) // mov ecx,exitCode
	code = binary.LittleEndian.AppendUint32(code, exitCode)
	return appendSelfhostPEIATCall(code, textRVA, exitProcessIATRVA)
}

func appendSelfhostPEIATCall(code []byte, textRVA, iatRVA uint32) ([]byte, error) {
	code = append(code, 0xff, 0x15) // call qword ptr [rip+disp32]
	displacementOffset := len(code)
	code = append(code, 0, 0, 0, 0)
	instructionEnd := textRVA + uint32(len(code))
	if err := patchSelfhostPEProbeRel32(code, displacementOffset, instructionEnd, iatRVA); err != nil {
		return nil, err
	}
	return code, nil
}

func patchSelfhostPEProbeRel32(code []byte, displacementOffset int, instructionEnd, targetRVA uint32) error {
	delta := int64(targetRVA) - int64(instructionEnd)
	if delta < -1<<31 || delta > 1<<31-1 {
		return fmt.Errorf("RIP-relative target %#x is outside signed rel32 range from %#x", targetRVA, instructionEnd)
	}
	binary.LittleEndian.PutUint32(code[displacementOffset:], uint32(int32(delta)))
	return nil
}

func runWithForcedRelocationPolicy(executable string) (uint32, error) {
	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return 0, fmt.Errorf("initialize STARTUPINFOEX attribute list: %w", err)
	}
	defer attributes.Delete()
	policy := peProbeForceRelocateImagesAlwaysOnRequireRelocs
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_MITIGATION_POLICY, unsafe.Pointer(&policy), unsafe.Sizeof(policy)); err != nil {
		return 0, fmt.Errorf("set force-relocate-images mitigation: %w", err)
	}
	startup := windows.StartupInfoEx{ProcThreadAttributeList: attributes.List()}
	startup.StartupInfo.Cb = uint32(unsafe.Sizeof(startup))
	applicationName, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return 0, fmt.Errorf("encode probe executable path: %w", err)
	}
	var process windows.ProcessInformation
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_NO_WINDOW)
	if err := windows.CreateProcess(applicationName, nil, nil, nil, false, flags, nil, nil, &startup.StartupInfo, &process); err != nil {
		return 0, fmt.Errorf("CreateProcessW with forced-rebase policy %#x: %w", policy, err)
	}
	defer windows.CloseHandle(process.Thread)
	defer windows.CloseHandle(process.Process)
	waitResult, err := windows.WaitForSingleObject(process.Process, windows.INFINITE)
	if err != nil {
		return 0, fmt.Errorf("wait for relocation probe: %w", err)
	}
	if waitResult != windows.WAIT_OBJECT_0 {
		return 0, fmt.Errorf("wait for relocation probe returned %#x", waitResult)
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(process.Process, &exitCode); err != nil {
		return 0, fmt.Errorf("read relocation probe exit code: %w", err)
	}
	return exitCode, nil
}
