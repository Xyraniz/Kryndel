package kry

// BuiltinCapability is one builtin/backend/target row. Interpreter and
// self-hosted refer to the checked runtime dispatch and the current
// source-compiler subset respectively. Native states describe registered
// lowering handlers, not proof that every runtime edge case is identical.
type BuiltinCapability struct {
	Builtin     string `json:"builtin"`
	Target      string `json:"target"`
	Interpreter string `json:"interpreter"`
	CAOT        string `json:"c_aot"`
	ELFDirect   string `json:"elf_direct"`
	PEDirect    string `json:"pe_direct"`
	SelfHosted  string `json:"self_hosted"`
}

// BuiltinCapabilityMatrix enumerates every registered builtin for every
// declared output target and reports the implementation inventory per backend.
func BuiltinCapabilityMatrix() []BuiltinCapability {
	rows := make([]BuiltinCapability, 0, len(builtinList)*len(nativeCapabilityTargets))
	for _, builtin := range builtinList {
		for _, target := range nativeCapabilityTargets {
			interpreter := "unsupported"
			if _, ok := generatedInterpreterBuiltinCases[builtin.Name]; ok {
				interpreter = "supported"
			}
			cFormat := "elf"
			if target.target.OS == "windows" {
				cFormat = "pe"
			} else if target.target.OS == "darwin" {
				cFormat = "macho"
			}
			selfHosted := "unsupported"
			if target.name == "linux-x64" {
				if _, ok := generatedSelfHostedBuiltinNames[builtin.Name]; ok {
					selfHosted = "partial"
				}
			}
			rows = append(rows, BuiltinCapability{
				Builtin:     builtin.Name,
				Target:      target.name,
				Interpreter: interpreter,
				CAOT:        nativeBuiltinBackendStatus(builtin.Name, cFormat, target.target),
				ELFDirect:   nativeBuiltinBackendStatus(builtin.Name, "elf-direct", target.target),
				PEDirect:    nativeBuiltinBackendStatus(builtin.Name, "pe-direct", target.target),
				SelfHosted:  selfHosted,
			})
		}
	}
	return rows
}

func nativeBuiltinBackendStatus(name, format string, target NativeTarget) string {
	switch format {
	case "elf", "exe", "pe", "macho", "c":
		if nativeOutputTargetReason(format, target) != "" {
			return "unsupported"
		}
		if _, ok := generatedCAOTBuiltinCases[name]; ok {
			if name == "http_request" {
				return "partial"
			}
			return "supported"
		}
	case "elf-direct":
		if nativeOutputTargetReason(format, target) != "" {
			return "unsupported"
		}
		if _, ok := generatedDirectELFBuiltinCases[name]; ok {
			if name == "print" || name == "println" || name == "str" {
				return "partial"
			}
			return "supported"
		}
	case "pe-direct":
		if nativeOutputTargetReason(format, target) != "" {
			return "unsupported"
		}
		if _, ok := generatedDirectPEBuiltinCases[name]; ok {
			if name == "print" || name == "println" || name == "str" {
				return "partial"
			}
			return "supported"
		}
	}
	return "unsupported"
}
