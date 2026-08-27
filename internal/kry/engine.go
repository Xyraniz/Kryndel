package kry

import (
	"os"
	"path/filepath"
)

type Engine struct {
	Limits         Limits
	JSON           bool
	RestrictedRoot string
}

func NewEngine() *Engine { return &Engine{Limits: DefaultLimits()} }
func (e *Engine) CheckPath(path string) (*Program, *Checker, *Diagnostic) {
	if filepath.Ext(path) == ".kexe" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, Diag(CatIO, nil, 1, 1, "cannot read artifact: %v", err)
		}
		a, d := DecodeArtifact(data, e.Limits)
		if d != nil {
			return nil, nil, d
		}
		p, d := ProgramFromArtifact(a, e.Limits)
		if d != nil {
			return nil, nil, d
		}
		c, d := Check(p, e.Limits)
		return p, c, d
	}
	p, d := LoadProgram(path, e.Limits, e.RestrictedRoot)
	if d != nil {
		return nil, nil, d
	}
	c, d := Check(p, e.Limits)
	if d != nil {
		return nil, nil, d
	}
	if _, d = ValidateIR(p, e.Limits); d != nil {
		return nil, nil, d
	}
	return p, c, nil
}
func (e *Engine) RunPath(path string) (string, *Diagnostic) {
	p, c, d := e.CheckPath(path)
	if d != nil {
		return "", d
	}
	sb := Sandbox{Root: e.RestrictedRoot, Restricted: e.RestrictedRoot != ""}
	r, d := NewRuntime(p, c, e.Limits, sb)
	if d != nil {
		return "", d
	}
	if d = r.run(); d != nil {
		return "", d
	}
	return "", nil
}
func (e *Engine) BuildPath(path, out string) *Diagnostic {
	p, _, d := e.CheckPath(path)
	if d != nil {
		return d
	}
	if filepath.Ext(path) == ".kexe" {
		return Diag(CatCLI, nil, 1, 1, "build expects a source .kry file")
	}
	data, d := BuildArtifact(p, path)
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
	return len(Builtins()) == len(builtinList) && e.Limits.MaxSourceBytes > 0 && e.Limits.MaxArtifactBytes > 0
}
