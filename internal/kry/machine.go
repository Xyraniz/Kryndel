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

// BuildDirectELF emits a dependency-free ELF64 executable from validated MIR.
func BuildDirectELF(p *Program, c *Checker, target NativeTarget) ([]byte, error) {
	if target.OS != "linux" || target.Arch != "amd64" {
		return nil, fmt.Errorf("direct ELF backend currently supports only linux-amd64")
	}
	if p == nil || c == nil {
		return nil, fmt.Errorf("missing checked program")
	}
	mir, err := CompileMIR(p, c, target)
	if err != nil {
		return nil, err
	}
	return buildDirectELFFromMIR(mir)
}

// buildDirectELFFromMIR is the production ELF lowering boundary. It accepts
// source-compiled or wire-decoded ValidatedMIR and never materializes an AST.
func buildDirectELFFromMIR(mir *ValidatedMIR) ([]byte, error) {
	document, err := validatedMIRDocument(mir)
	if err != nil {
		return nil, err
	}
	if err := validateMIRFunctionValueSupport(mir, "elf-direct"); err != nil {
		return nil, err
	}
	target := NativeTarget{OS: document.Target.OS, Arch: document.Target.Arch, GUI: document.Target.GUI}
	if err := validateMIRNativeFeatureSupport(mir, "elf-direct", target); err != nil {
		return nil, err
	}
	output, staticErr := directStaticKIROutput(mir, mir.limits.MaxOutputBytes)
	if staticErr == nil {
		image := emitELF64WriteExit(output)
		if limit := mir.limits.MaxArtifactBytes; limit > 0 && len(image) > limit {
			return nil, fmt.Errorf("direct ELF exceeds configured artifact limit")
		}
		return image, nil
	}
	subsetErr := validateKIRDirectELFValueSubset(mir)
	if subsetErr == nil {
		return buildDirectKIRELF(mir, mir.limits, mir.sources)
	} else if !errors.Is(subsetErr, errKIRSubsetUnsupported) {
		return nil, fmt.Errorf("direct backend rejected KIR: %w", subsetErr)
	}
	if errors.Is(staticErr, errDirectOutputLimit) {
		return nil, staticErr
	}
	return nil, subsetErr
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
