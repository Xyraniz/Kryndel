package kry

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"runtime"
)

// NativeTarget describes a machine-code output target. The backend deliberately
// starts with a tiny, dependency-free entry point that exits successfully; the
// VM and KRYNATIVE3 bundle remain the development path for the full language.
type NativeTarget struct {
	OS   string
	Arch string
	GUI  bool
}

func ParseNativeTarget(raw string) (NativeTarget, error) {
	if raw == "" || raw == "host" {
		return NativeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH}, nil
	}
	var t NativeTarget
	switch raw {
	case "windows-x64":
		t = NativeTarget{OS: "windows", Arch: "amd64"}
	case "windows-arm64":
		t = NativeTarget{OS: "windows", Arch: "arm64"}
	case "linux-x64":
		t = NativeTarget{OS: "linux", Arch: "amd64"}
	case "linux-arm64":
		t = NativeTarget{OS: "linux", Arch: "arm64"}
	case "darwin-x64":
		t = NativeTarget{OS: "darwin", Arch: "amd64"}
	case "darwin-arm64":
		t = NativeTarget{OS: "darwin", Arch: "arm64"}
	default:
		return NativeTarget{}, fmt.Errorf("unsupported native target %q", raw)
	}
	return t, nil
}

func BuildNative(p *Program, target NativeTarget, format string) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("missing checked program")
	}
	switch format {
	case "exe", "pe":
		if target.OS != "windows" {
			return nil, fmt.Errorf("PE output requires a Windows target")
		}
		return buildPE(target), nil
	case "elf":
		if target.OS != "linux" {
			return nil, fmt.Errorf("ELF output requires a Linux target")
		}
		if target.Arch != "amd64" {
			return nil, fmt.Errorf("ELF backend currently supports linux-x64")
		}
		return buildELF(), nil
	case "macho":
		return nil, fmt.Errorf("Mach-O backend is not yet available; use KRYNATIVE3 or a supported target")
	default:
		return nil, fmt.Errorf("unsupported native format %q", format)
	}
}

func EmitLLVMIR(p *Program, target NativeTarget) []byte {
	name := "kryndel_main"
	return []byte(fmt.Sprintf("; Kryndel checked IR emission for %s/%s\nsource_filename = \"kryndel\"\n\ndefine i32 @%s() {\nentry:\n  ret i32 0\n}\n", target.OS, target.Arch, name))
}

func InspectNative(data []byte) (string, error) {
	if len(data) >= 2 && data[0] == 'M' && data[1] == 'Z' {
		if len(data) < 0x40 {
			return "PE: truncated DOS header", fmt.Errorf("truncated DOS header")
		}
		pe := int(binary.LittleEndian.Uint32(data[0x3c:0x40]))
		if pe < 0 || pe+24 > len(data) || !bytes.Equal(data[pe:pe+4], []byte("PE\x00\x00")) {
			return "PE: invalid signature", fmt.Errorf("invalid PE signature")
		}
		machine := binary.LittleEndian.Uint16(data[pe+4 : pe+6])
		sections := binary.LittleEndian.Uint16(data[pe+6 : pe+8])
		var arch string
		switch machine {
		case 0x8664:
			arch = "windows-x64"
		case 0xaa64:
			arch = "windows-arm64"
		default:
			arch = fmt.Sprintf("machine-0x%x", machine)
		}
		return fmt.Sprintf("PE\narchitecture: %s\nsections: %d\n", arch, sections), nil
	}
	if len(data) >= 20 && bytes.Equal(data[:4], []byte{0x7f, 'E', 'L', 'F'}) {
		if data[4] != 2 || data[5] != 1 {
			return "ELF: unsupported class or endianness", fmt.Errorf("unsupported ELF")
		}
		machine := binary.LittleEndian.Uint16(data[18:20])
		return fmt.Sprintf("ELF64\nmachine: 0x%x\n", machine), nil
	}
	return "unknown binary format", fmt.Errorf("unrecognized executable format")
}

func buildPE(target NativeTarget) []byte {
	const (
		fileAlign = 0x200
		textRVA   = 0x1000
		idataRVA  = 0x2000
	)
	text := []byte{0x31, 0xc9, 0xff, 0x15, 0x48, 0x10, 0x00, 0x00, 0xc3}
	if target.Arch == "arm64" {
		// mov w0,#0; ldr x16,[pc,#8]; blr x16; brk #0
		text = []byte{0x00, 0x00, 0x80, 0x52, 0x50, 0x00, 0x00, 0x58, 0x00, 0x02, 0x3f, 0xd6, 0x00, 0x00, 0x20, 0xd4}
	}
	idata := make([]byte, fileAlign)
	put32 := func(off int, v uint32) { binary.LittleEndian.PutUint32(idata[off:off+4], v) }
	put64 := func(off int, v uint64) { binary.LittleEndian.PutUint64(idata[off:off+8], v) }
	put32(0, idataRVA+0x40)
	put32(12, idataRVA+0x60)
	put32(16, idataRVA+0x50)
	put64(0x40, uint64(idataRVA+0x70))
	put64(0x50, uint64(idataRVA+0x70))
	copy(idata[0x60:], []byte("kernel32.dll\x00"))
	binary.LittleEndian.PutUint16(idata[0x70:0x72], 0)
	copy(idata[0x72:], []byte("ExitProcess\x00"))
	buf := bytes.NewBuffer(nil)
	dos := make([]byte, 0x80)
	dos[0], dos[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(dos[0x3c:], 0x80)
	buf.Write(dos)
	buf.Write([]byte{'P', 'E', 0, 0})
	coff := make([]byte, 20)
	machine := uint16(0x8664)
	if target.Arch == "arm64" {
		machine = 0xaa64
	}
	binary.LittleEndian.PutUint16(coff[0:], machine)
	binary.LittleEndian.PutUint16(coff[2:], 2)
	binary.LittleEndian.PutUint16(coff[16:], 0xf0)
	binary.LittleEndian.PutUint16(coff[18:], 0x0022)
	buf.Write(coff)
	opt := make([]byte, 0xf0)
	binary.LittleEndian.PutUint16(opt[0:], 0x20b)
	opt[2] = 14
	opt[3] = 0
	binary.LittleEndian.PutUint32(opt[4:], 0x200)
	binary.LittleEndian.PutUint32(opt[16:], textRVA)
	binary.LittleEndian.PutUint32(opt[20:], textRVA)
	binary.LittleEndian.PutUint64(opt[24:], 0x140000000)
	binary.LittleEndian.PutUint32(opt[32:], 0x1000)
	binary.LittleEndian.PutUint32(opt[36:], fileAlign)
	binary.LittleEndian.PutUint16(opt[40:], 6)
	binary.LittleEndian.PutUint16(opt[48:], 6)
	binary.LittleEndian.PutUint32(opt[56:], 0x3000)
	binary.LittleEndian.PutUint32(opt[60:], 0x200)
	binary.LittleEndian.PutUint16(opt[68:], 3)
	binary.LittleEndian.PutUint64(opt[72:], 0x100000)
	binary.LittleEndian.PutUint64(opt[80:], 0x1000)
	binary.LittleEndian.PutUint64(opt[88:], 0x100000)
	binary.LittleEndian.PutUint64(opt[96:], 0x1000)
	binary.LittleEndian.PutUint32(opt[108:], 16)
	binary.LittleEndian.PutUint32(opt[120:], idataRVA)
	binary.LittleEndian.PutUint32(opt[124:], 40)
	buf.Write(opt)
	section := func(name string, virtualSize, rva, rawSize, rawPtr, characteristics uint32) []byte {
		s := make([]byte, 40)
		copy(s[:8], []byte(name))
		binary.LittleEndian.PutUint32(s[8:], virtualSize)
		binary.LittleEndian.PutUint32(s[12:], rva)
		binary.LittleEndian.PutUint32(s[16:], rawSize)
		binary.LittleEndian.PutUint32(s[20:], rawPtr)
		binary.LittleEndian.PutUint32(s[36:], characteristics)
		return s
	}
	buf.Write(section(".text", uint32(len(text)), textRVA, fileAlign, 0x200, 0x60000020))
	buf.Write(section(".idata", 0x100, idataRVA, fileAlign, 0x400, 0xc0000040))
	for buf.Len() < 0x200 {
		buf.WriteByte(0)
	}
	buf.Write(text)
	for buf.Len() < 0x400 {
		buf.WriteByte(0)
	}
	buf.Write(idata)
	return buf.Bytes()
}

func buildELF() []byte {
	code := []byte{0xb8, 60, 0, 0, 0, 0x31, 0xff, 0x0f, 0x05}
	const phoff = 64
	const codeOff = 0x1000
	out := make([]byte, codeOff+len(code))
	copy(out[codeOff:], code)
	copy(out[:4], []byte{0x7f, 'E', 'L', 'F'})
	out[4], out[5], out[6] = 2, 1, 1
	binary.LittleEndian.PutUint16(out[16:], 2)
	binary.LittleEndian.PutUint16(out[18:], 0x3e)
	binary.LittleEndian.PutUint32(out[20:], 1)
	binary.LittleEndian.PutUint64(out[24:], 0x400000+codeOff)
	binary.LittleEndian.PutUint64(out[32:], phoff)
	binary.LittleEndian.PutUint64(out[40:], 0)
	binary.LittleEndian.PutUint32(out[48:], 0)
	binary.LittleEndian.PutUint16(out[52:], 64)
	binary.LittleEndian.PutUint16(out[54:], 56)
	binary.LittleEndian.PutUint16(out[56:], 1)
	binary.LittleEndian.PutUint16(out[58:], 0)
	binary.LittleEndian.PutUint16(out[60:], 0)
	binary.LittleEndian.PutUint16(out[62:], 0)
	ph := out[phoff : phoff+56]
	binary.LittleEndian.PutUint32(ph[0:], 1)
	binary.LittleEndian.PutUint32(ph[4:], 5)
	binary.LittleEndian.PutUint64(ph[8:], 0)
	binary.LittleEndian.PutUint64(ph[16:], 0x400000)
	binary.LittleEndian.PutUint64(ph[24:], 0x400000)
	binary.LittleEndian.PutUint64(ph[32:], uint64(len(out)))
	binary.LittleEndian.PutUint64(ph[40:], uint64(len(out)))
	binary.LittleEndian.PutUint64(ph[48:], 0x1000)
	return out
}
