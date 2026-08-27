package kry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Limits struct {
	MaxSourceBytes     int
	MaxArtifactBytes   int
	MaxTokens          int
	MaxASTNodes        int
	MaxNesting         int
	MaxTypeDepth       int
	MaxImports         int
	MaxCallDepth       int
	MaxInstructions    uint64
	MaxMemoryBytes     int64
	MaxStringBytes     int
	MaxArrayElements   int
	MaxChannelCapacity int
	MaxWorkers         int
	MaxOutputBytes     int64
	MaxWallTimeMS      int64
	ShutdownMS         int64
}

func DefaultLimits() Limits {
	return Limits{MaxSourceBytes: 16 << 20, MaxArtifactBytes: 64 << 20, MaxTokens: 1_000_000, MaxASTNodes: 1_000_000, MaxNesting: 512, MaxTypeDepth: 128, MaxImports: 256, MaxCallDepth: 1024, MaxInstructions: 5_000_000, MaxMemoryBytes: 256 << 20, MaxStringBytes: 16 << 20, MaxArrayElements: 1_000_000, MaxChannelCapacity: 1_000_000, MaxWorkers: 256, MaxOutputBytes: 16 << 20, MaxWallTimeMS: 10_000, ShutdownMS: 2_000}
}

type Source struct {
	Name string
	Text string
}

type Category string

const (
	CatLex      Category = "lex"
	CatParse    Category = "parse"
	CatType     Category = "type-mismatch"
	CatRuntime  Category = "runtime"
	CatArtifact Category = "artifact"
	CatCLI      Category = "cli"
	CatIO       Category = "io"
	CatResource Category = "resource"
)

type Diagnostic struct {
	Code     string   `json:"code"`
	Category Category `json:"category"`
	Severity string   `json:"severity"`
	Source   string   `json:"source"`
	Line     int      `json:"line"`
	Column   int      `json:"column"`
	Message  string   `json:"message"`
	Text     string   `json:"-"`
}

func codeFor(c Category) string {
	switch c {
	case CatLex:
		return "KRY001"
	case CatParse:
		return "KRY002"
	case CatType:
		return "KRY003"
	case CatRuntime:
		return "KRY004"
	case CatArtifact:
		return "KRY005"
	case CatCLI:
		return "KRY006"
	case CatIO:
		return "KRY007"
	case CatResource:
		return "KRY008"
	}
	return "KRY000"
}
func Diag(c Category, src *Source, line, col int, format string, args ...any) *Diagnostic {
	name, text := "<input>", ""
	if src != nil {
		name, text = src.Name, src.Text
	}
	if line < 1 {
		line = 1
	}
	if col < 1 {
		col = 1
	}
	return &Diagnostic{Code: codeFor(c), Category: c, Severity: "error", Source: name, Line: line, Column: col, Message: fmt.Sprintf(format, args...), Text: text}
}
func (d *Diagnostic) Error() string { return d.Message }
func (d *Diagnostic) Format(jsonMode bool) string {
	if jsonMode {
		b, _ := json.Marshal(d)
		return string(b) + "\n"
	}
	out := fmt.Sprintf("error[%s]: %s:%d:%d\n  %s\n", d.Category, d.Source, d.Line, d.Column, d.Message)
	if d.Text == "" {
		return out
	}
	lines := []rune(d.Text)
	line, start := 1, 0
	for i, r := range lines {
		if line == d.Line {
			start = i
			break
		}
		if r == '\n' {
			line++
		}
	}
	end := start
	for end < len(lines) && lines[end] != '\n' {
		end++
	}
	excerpt := string(lines[start:end])
	if len([]rune(excerpt)) > 220 {
		excerpt = string([]rune(excerpt)[:220])
	}
	if excerpt != "" {
		out += "  " + excerpt + "\n  "
		for i := 1; i < d.Column && i <= 220; i++ {
			out += " "
		}
		out += "^\n"
	}
	return out
}

func ReadSource(path string, lim Limits) (*Source, *Diagnostic) {
	f, err := os.Open(path)
	if err != nil {
		return nil, Diag(CatIO, nil, 1, 1, "cannot read %s: %v", path, err)
	}
	defer f.Close()
	data, err := ReadAllLimit(f, lim.MaxSourceBytes)
	if err != nil {
		return nil, Diag(CatIO, nil, 1, 1, "cannot read %s: %v", path, err)
	}
	if len(data) > lim.MaxSourceBytes {
		return nil, Diag(CatResource, &Source{Name: path}, 1, 1, "configured input size limit exceeded: more than %d bytes", lim.MaxSourceBytes)
	}
	if !validUTF8(data) {
		return nil, Diag(CatLex, &Source{Name: path, Text: string(data)}, 1, 1, "source is not valid UTF-8")
	}
	return &Source{Name: path, Text: string(data)}, nil
}
func NormalizePath(p string) string {
	q, _ := filepath.Abs(p)
	q, _ = filepath.EvalSymlinks(q)
	return filepath.Clean(q)
}
