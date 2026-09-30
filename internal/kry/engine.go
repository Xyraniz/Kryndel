package kry

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
)

type Engine struct {
	Limits         Limits
	JSON           bool
	RestrictedRoot string
	// Passphrase unlocks sealed (encrypted) artifacts. It is never written to
	// disk and is only held for the lifetime of the process.
	Passphrase string
}

func NewEngine() *Engine { return &Engine{Limits: DefaultLimits()} }
func (e *Engine) CheckPath(path string) (*Program, *Checker, *Diagnostic) {
	program, checker, _, diagnostic := e.checkPathWithKIR(path)
	return program, checker, diagnostic
}

// checkPathWithKIR returns the validated KIR embedded in a current artifact
// when one exists. Source paths and legacy artifacts return nil KIR so callers
// can emit it for the current host target.
func (e *Engine) checkPathWithKIR(path string) (*Program, *Checker, *ValidatedMIR, *Diagnostic) {
	if err := ValidateProjectForPath(path); err != nil {
		return nil, nil, nil, Diag(CatCLI, nil, 1, 1, "%v", err)
	}
	if filepath.Ext(path) == ".kexe" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, nil, Diag(CatIO, nil, 1, 1, "cannot read artifact: %v", err)
		}
		if IsSealedArtifact(data) {
			plain, err := DecryptArtifact(data, e.Passphrase, e.Limits)
			if err != nil {
				return nil, nil, nil, Diag(CatArtifact, nil, 1, 1, "%v", err)
			}
			data = plain
		}
		a, d := DecodeArtifact(data, e.Limits)
		if d != nil {
			return nil, nil, nil, d
		}
		p, d := ProgramFromArtifact(a, e.Limits)
		if d != nil {
			return nil, nil, nil, d
		}
		c, d := Check(p, e.Limits)
		if d != nil {
			return nil, nil, nil, d
		}
		if d = ValidateASTLimits(p, e.Limits); d != nil {
			return nil, nil, nil, d
		}
		var embeddedKIR *ValidatedMIR
		if len(a.KIR) > 0 {
			embeddedKIR, err = DecodeMIR(a.KIR, e.Limits)
			if err != nil {
				return nil, nil, nil, Diag(CatArtifact, p.Source, 1, 1, "cannot decode artifact KIR: %v", err)
			}
			target := NativeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH}
			if a.Target == "portable/any" {
				if embeddedKIR.document.Target.OS != "portable" || embeddedKIR.document.Target.Arch != "any" || embeddedKIR.document.Target.GUI {
					return nil, nil, nil, Diag(CatArtifact, p.Source, 1, 1, "malformed native artifact: portable KIR has an incompatible target")
				}
				target = NativeTarget{OS: embeddedKIR.document.Target.OS, Arch: embeddedKIR.document.Target.Arch, GUI: embeddedKIR.document.Target.GUI}
			}
			compiled, err := CompileMIR(p, c, target)
			if err != nil {
				return nil, nil, nil, Diag(CatArtifact, p.Source, 1, 1, "cannot validate artifact KIR: %v", err)
			}
			generated, err := compiled.MarshalKIR()
			if err != nil {
				return nil, nil, nil, Diag(CatArtifact, p.Source, 1, 1, "cannot validate artifact KIR: %v", err)
			}
			if !bytes.Equal(a.KIR, generated) {
				return nil, nil, nil, Diag(CatArtifact, p.Source, 1, 1, "malformed native artifact: typed KIR does not match embedded sources")
			}
			embeddedKIR = compiled
		}
		return p, c, embeddedKIR, nil
	}
	p, d := LoadProgram(path, e.Limits, e.RestrictedRoot)
	if d != nil {
		return nil, nil, nil, d
	}
	c, d := Check(p, e.Limits)
	if d != nil {
		return nil, nil, nil, d
	}
	if d = ValidateASTLimits(p, e.Limits); d != nil {
		return nil, nil, nil, d
	}
	return p, c, nil, nil
}
func (e *Engine) RunPath(path string) (string, *Diagnostic) {
	return e.RunPathWithArgs(path, nil)
}
func (e *Engine) RunPathWithArgs(path string, args []string) (string, *Diagnostic) {
	p, c, mir, d := e.checkPathWithKIR(path)
	if d != nil {
		return "", d
	}
	if mir == nil {
		var err error
		mir, err = CompileMIR(p, c, NativeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH})
		if err != nil {
			return "", Diag(CatArtifact, p.Source, 1, 1, "cannot compile checked MIR: %v", err)
		}
	}
	sandbox := Sandbox{Root: e.RestrictedRoot, Restricted: e.RestrictedRoot != ""}
	runtime, diagnostic := newRuntimeFromMIR(mir, e.Limits, sandbox, args)
	if diagnostic != nil {
		return "", diagnostic
	}
	return "", runtime.run()
}

// DebugPathWithArgs runs a checked source or artifact through the interpreter
// and pauses at locations selected by debugger.ShouldPause.
func (e *Engine) DebugPathWithArgs(path string, args []string, debugger Debugger) *Diagnostic {
	if debugger.ShouldPause == nil || debugger.OnPause == nil {
		return Diag(CatCLI, nil, 1, 1, "debugger requires pause and resume handlers")
	}
	p, c, mir, d := e.checkPathWithKIR(path)
	if d != nil {
		return d
	}
	if mir == nil {
		var err error
		mir, err = CompileMIR(p, c, NativeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH})
		if err != nil {
			return Diag(CatArtifact, p.Source, 1, 1, "cannot compile checked MIR: %v", err)
		}
	}
	sandbox := Sandbox{Root: e.RestrictedRoot, Restricted: e.RestrictedRoot != ""}
	r, d := newRuntimeFromMIR(mir, e.Limits, sandbox, args)
	if d != nil {
		return d
	}
	r.debugger = &debugger
	return r.run()
}
func (e *Engine) BuildPath(path, out string) *Diagnostic {
	p, c, d := e.CheckPath(path)
	if d != nil {
		return d
	}
	if filepath.Ext(path) == ".kexe" {
		return Diag(CatCLI, nil, 1, 1, "build expects a source .kry file")
	}
	data, d := BuildArtifactWithChecker(p, c, path, e.Limits)
	if d != nil {
		return d
	}
	if e.RestrictedRoot != "" {
		sb := Sandbox{Root: e.RestrictedRoot, Restricted: true}
		if err := sb.Write(out, data); err != nil {
			return Diag(CatIO, nil, 1, 1, "cannot write artifact: %v", err)
		}
	} else if err := WriteArtifact(out, data); err != nil {
		return Diag(CatIO, nil, 1, 1, "cannot write artifact: %v", err)
	}
	return nil
}

// BuildSealedPath builds a .kexe artifact and wraps it in an authenticated,
// passphrase-protected container. The plaintext artifact never touches disk.
func (e *Engine) BuildSealedPath(path, out, passphrase string, iterations int) *Diagnostic {
	p, c, d := e.CheckPath(path)
	if d != nil {
		return d
	}
	if filepath.Ext(path) == ".kexe" {
		return Diag(CatCLI, nil, 1, 1, "build expects a source .kry file")
	}
	data, d := BuildArtifactWithChecker(p, c, path, e.Limits)
	if d != nil {
		return d
	}
	sealed, err := EncryptArtifact(data, passphrase, iterations)
	if err != nil {
		return Diag(CatArtifact, nil, 1, 1, "cannot encrypt artifact: %v", err)
	}
	if e.RestrictedRoot != "" {
		sb := Sandbox{Root: e.RestrictedRoot, Restricted: true}
		if err := sb.Write(out, sealed); err != nil {
			return Diag(CatIO, nil, 1, 1, "cannot write artifact: %v", err)
		}
	} else if err := WriteArtifact(out, sealed); err != nil {
		return Diag(CatIO, nil, 1, 1, "cannot write artifact: %v", err)
	}
	return nil
}

func (e *Engine) FormatPath(path string, write, check bool) (string, *Diagnostic, int) {
	src, d := ReadSource(path, e.Limits)
	if d != nil {
		return "", d, 1
	}
	formatted, d := FormatSource(src, e.Limits)
	if d != nil {
		return "", d, 1
	}
	if check {
		if formatted != src.Text {
			return formatted, nil, 1
		}
		return formatted, nil, 0
	}
	if write {
		if e.RestrictedRoot != "" {
			if err := (Sandbox{Root: e.RestrictedRoot, Restricted: true}).Write(path, []byte(formatted)); err != nil {
				return "", Diag(CatIO, nil, 1, 1, "cannot write formatted source: %v", err), 1
			}
		} else {
			if err := WriteTextAtomic(path, []byte(formatted)); err != nil {
				return "", Diag(CatIO, nil, 1, 1, "cannot write formatted source: %v", err), 1
			}
		}
		return "", nil, 0
	}
	return formatted, nil, 0
}
func WriteTextAtomic(path string, data []byte) error { return WriteArtifact(path, data) }
func (e *Engine) Doctor() bool {
	return len(Builtins()) == len(builtinList) && e.Limits.MaxSourceBytes > 0 && e.Limits.MaxArtifactBytes > 0 && e.Limits.MaxJSONBytes > 0
}
