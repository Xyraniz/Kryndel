package kry

import "fmt"

// directStaticKIROutput lowers the constant-output ELF subset from the
// validated arena. Unsupported nodes are left to the dynamic backend.
func directStaticKIROutput(mir *ValidatedMIR, maxOutputBytes int64) ([]byte, error) {
	if mir == nil || mir.arena == nil {
		return nil, fmt.Errorf("missing validated MIR arena")
	}
	return directStaticOutputELFMIR(mir.arena, maxOutputBytes)
}
