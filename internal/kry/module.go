package kry

import (
	"os"
	"path/filepath"
	"strings"
)

type ModuleLoader struct {
	Lim      Limits
	Root     string
	State    map[string]int
	Programs map[string]*Program
	Stack    map[string]bool
}

func LoadProgram(path string, lim Limits, restrictedRoot string) (*Program, *Diagnostic) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, Diag(CatIO, nil, 1, 1, "cannot resolve source path: %v", err)
	}
	root := findModuleRoot(filepath.Dir(abs))
	if restrictedRoot != "" {
		r, _ := filepath.Abs(restrictedRoot)
		root = filepath.Clean(r)
		if !within(root, abs) {
			return nil, Diag(CatIO, nil, 1, 1, "source path is outside restricted root")
		}
		if !safeComponents(root, abs) {
			return nil, Diag(CatIO, nil, 1, 1, "source path traverses a symlink")
		}
	}
	l := &ModuleLoader{Lim: lim, Root: root, State: map[string]int{}, Programs: map[string]*Program{}, Stack: map[string]bool{}}
	p, d := l.load(abs)
	if d != nil {
		return nil, d
	}
	p.Module = abs
	p.Source.Name = abs
	return l.merge(p), nil
}
func findModuleRoot(start string) string {
	cur, _ := filepath.Abs(start)
	for {
		if fileExists(filepath.Join(cur, "kry.toml")) || fileExists(filepath.Join(cur, ".git")) {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return start
		}
		cur = parent
	}
}

func within(root, p string) bool {
	r, _ := filepath.Abs(root)
	q, _ := filepath.Abs(p)
	rel, err := filepath.Rel(r, q)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func (l *ModuleLoader) load(path string) (*Program, *Diagnostic) {
	path = filepath.Clean(path)
	if !within(l.Root, path) {
		return nil, Diag(CatIO, nil, 1, 1, "module path escapes the program root")
	}
	if l.Stack[path] {
		return nil, Diag(CatType, nil, 1, 1, "module import cycle involving %s", path)
	}
	if p := l.Programs[path]; p != nil {
		return p, nil
	}
	l.Stack[path] = true
	defer delete(l.Stack, path)
	src, d := ReadSource(path, l.Lim)
	if d != nil {
		return nil, d
	}
	prog, d := Parse(src, l.Lim)
	if d != nil {
		return nil, d
	}
	prog.Module = path
	for i := range prog.Imports {
		imp := prog.Imports[i]
		if filepath.IsAbs(imp.Path) || strings.ContainsRune(imp.Path, 0) || hasParent(imp.Path) {
			return nil, Diag(CatIO, imp.Tok.Source, imp.Tok.Line, imp.Tok.Column, "unsafe module path '%s'", imp.Path)
		}
		candidate := resolveImportPath(filepath.Dir(path), imp.Path)
		if !fileExists(candidate) {
			candidate = resolveImportPath(l.Root, imp.Path)
		}

		if !within(l.Root, candidate) {
			return nil, Diag(CatIO, imp.Tok.Source, imp.Tok.Line, imp.Tok.Column, "module path escapes the program root")
		}
		if !safeComponents(l.Root, candidate) {
			return nil, Diag(CatIO, imp.Tok.Source, imp.Tok.Line, imp.Tok.Column, "module path traverses a symlink")
		}
		if _, err := os.Stat(candidate); err != nil {
			return nil, Diag(CatIO, imp.Tok.Source, imp.Tok.Line, imp.Tok.Column, "cannot resolve module '%s': %v", imp.Path, err)
		}
		if _, d = l.load(candidate); d != nil {
			return nil, d
		}
	}
	l.Programs[path] = prog
	return prog, nil
}
func resolveImportPath(base, imp string) string {
	candidate := filepath.Join(base, imp)
	if filepath.Ext(candidate) == "" {
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			candidate = filepath.Join(candidate, "main.kry")
		} else {
			candidate += ".kry"
		}
	}
	if filepath.Ext(candidate) == ".kry" && !fileExists(candidate) {
		packageCandidate := filepath.Join(base, "vendor", imp, "main.kry")
		if fileExists(packageCandidate) {
			candidate = packageCandidate
		}
	}
	return filepath.Clean(candidate)
}
func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }

func hasParent(p string) bool {
	for _, x := range strings.FieldsFunc(filepath.ToSlash(p), func(r rune) bool { return r == '/' }) {
		if x == ".." {
			return true
		}
	}
	return false
}
func safeComponents(root, target string) bool {
	if root == "" {
		return true
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	cur := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}
func (l *ModuleLoader) merge(root *Program) *Program {
	out := &Program{Source: root.Source, Module: root.Module, Sources: []*Source{root.Source}, Statements: root.Statements, Functions: append([]*Function{}, root.Functions...), Structs: append([]*StructDecl{}, root.Structs...), Enums: append([]*EnumDecl{}, root.Enums...)}
	seen := map[string]bool{root.Source.Name: true}
	var add func(*Program)
	add = func(p *Program) {
		for _, imp := range p.Imports {
			candidate := resolveImportPath(filepath.Dir(p.Source.Name), imp.Path)
			if !fileExists(candidate) {
				candidate = resolveImportPath(l.Root, imp.Path)
			}
			if seen[candidate] {
				continue
			}
			seen[candidate] = true
			q := l.Programs[candidate]
			if q == nil {
				continue
			}
			add(q)
			out.Sources = append(out.Sources, q.Source)
			for _, f := range q.Functions {
				out.Functions = append(out.Functions, f)
			}
			for _, s := range q.Structs {
				out.Structs = append(out.Structs, s)
			}
			for _, e := range q.Enums {
				out.Enums = append(out.Enums, e)
			}
		}
	}
	add(root)
	return out
}
