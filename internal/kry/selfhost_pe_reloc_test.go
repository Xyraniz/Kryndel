package kry

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSelfhostPEBackendEmitsASLRRelocations(t *testing.T) {
	image, diagnostic := runSelfhostPEBackend(t, `fn main() -> Nil { println("ASLR relocation ok") }`)
	if diagnostic != nil {
		t.Fatalf("selfhost PE backend failed: %v", diagnostic)
	}

	file, err := pe.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("parse selfhost PE image: %v", err)
	}
	optional, ok := file.OptionalHeader.(*pe.OptionalHeader64)
	if !ok {
		t.Fatalf("optional header has type %T, want PE32+", file.OptionalHeader)
	}
	const imageDLLCharacteristicsDynamicBase = 0x0040
	if optional.DllCharacteristics&imageDLLCharacteristicsDynamicBase == 0 {
		t.Fatalf("DllCharacteristics %#x does not set IMAGE_DLLCHARACTERISTICS_DYNAMIC_BASE", optional.DllCharacteristics)
	}

	const imageDirectoryEntryBaseReloc = 5
	directory := optional.DataDirectory[imageDirectoryEntryBaseReloc]
	if directory.VirtualAddress == 0 || directory.Size < 12 || directory.Size%4 != 0 {
		t.Fatalf("base relocation data directory is invalid: RVA=%#x size=%d", directory.VirtualAddress, directory.Size)
	}
	relocSection := file.Section(".reloc")
	if relocSection == nil {
		t.Fatal("PE image has no .reloc section")
	}
	if relocSection.VirtualAddress != directory.VirtualAddress || directory.Size > relocSection.VirtualSize {
		t.Fatalf("base relocation directory RVA/size (%#x/%d) does not fit .reloc section (%#x/%d)", directory.VirtualAddress, directory.Size, relocSection.VirtualAddress, relocSection.VirtualSize)
	}
	relocBytes, err := relocSection.Data()
	if err != nil {
		t.Fatalf("read .reloc section: %v", err)
	}
	if uint32(len(relocBytes)) < directory.Size {
		t.Fatalf(".reloc raw data has %d bytes, directory requires %d", len(relocBytes), directory.Size)
	}

	rdataSection := file.Section(".rdata")
	if rdataSection == nil {
		t.Fatal("PE image has no .rdata section")
	}
	rdataBytes, err := rdataSection.Data()
	if err != nil {
		t.Fatalf("read .rdata section: %v", err)
	}

	dirSize := int(directory.Size)
	dirBytes := relocBytes[:dirSize]
	baseFixups := 0
	for blockOffset := 0; blockOffset < dirSize; {
		if blockOffset+8 > dirSize {
			t.Fatalf("truncated base relocation block header at offset %d", blockOffset)
		}
		pageRVA := binary.LittleEndian.Uint32(dirBytes[blockOffset:])
		blockSize := int(binary.LittleEndian.Uint32(dirBytes[blockOffset+4:]))
		if pageRVA%4096 != 0 || blockSize < 8 || blockSize%4 != 0 || blockOffset+blockSize > dirSize {
			t.Fatalf("invalid base relocation block at offset %d: page RVA=%#x size=%d", blockOffset, pageRVA, blockSize)
		}
		for entryOffset := blockOffset + 8; entryOffset < blockOffset+blockSize; entryOffset += 2 {
			entry := binary.LittleEndian.Uint16(dirBytes[entryOffset:])
			relocType := entry >> 12
			if relocType == 0 { // IMAGE_REL_BASED_ABSOLUTE padding
				continue
			}
			if relocType != 10 { // IMAGE_REL_BASED_DIR64
				t.Fatalf("unexpected x64 base relocation type %d", relocType)
			}
			targetRVA := pageRVA + uint32(entry&0x0fff)
			if targetRVA < rdataSection.VirtualAddress || targetRVA+8 > rdataSection.VirtualAddress+rdataSection.VirtualSize {
				t.Fatalf("DIR64 target RVA %#x is outside .rdata", targetRVA)
			}
			dataOffset := targetRVA - rdataSection.VirtualAddress
			if int(dataOffset)+8 > len(rdataBytes) {
				t.Fatalf("DIR64 target RVA %#x exceeds .rdata raw bytes", targetRVA)
			}
			wantPointer := optional.ImageBase + 0x1000
			gotPointer := binary.LittleEndian.Uint64(rdataBytes[dataOffset:])
			if gotPointer != wantPointer {
				t.Fatalf("DIR64 target stores %#x, want preferred-base pointer %#x", gotPointer, wantPointer)
			}
			baseFixups++
		}
		blockOffset += blockSize
	}
	if baseFixups != 1 {
		t.Fatalf("base relocation directory contains %d DIR64 fixups, want one", baseFixups)
	}

	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		executable := filepath.Join(t.TempDir(), "selfhost-pe-aslr.exe")
		if err := os.WriteFile(executable, image, 0o700); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(executable).CombinedOutput()
		if err != nil {
			t.Fatalf("ASLR-enabled selfhost PE failed to run: %v; output: %s", err, output)
		}
		if string(output) != "ASLR relocation ok\n" {
			t.Fatalf("unexpected ASLR-enabled PE output %q", output)
		}
	}
}
