package kry

import (
	"os"
	"path/filepath"
	"strings"
)

type ModuleLoader struct {
	Lim      Limits
	Root     string
	Overlay  map[string]*Source
	State    map[string]int
	Programs map[string]*Program
	Stack    map[string]bool
}

func LoadProgram(path string, lim Limits, restrictedRoot string) (*Program, *Diagnostic) {
	return loadProgram(path, nil, lim, restrictedRoot)
}

// LoadProgramWithSources loads a source tree with in-memory text overriding
// files on disk. Editors use it to type-check unsaved buffers with the same
// module resolution and visibility rules as normal source files.
func LoadProgramWithSources(path string, sources map[string]string, lim Limits) (*Program, *Diagnostic) {
	overlay := make(map[string]*Source, len(sources))
	for name, text := range sources {
		abs, err := filepath.Abs(name)
		if err != nil {
			return nil, Diag(CatIO, nil, 1, 1, "cannot resolve source path: %v", err)
		}
		abs = filepath.Clean(abs)
		overlay[abs] = &Source{Name: abs, Text: text}
	}
	return loadProgram(path, overlay, lim, "")
}

func loadProgram(path string, overlay map[string]*Source, lim Limits, restrictedRoot string) (*Program, *Diagnostic) {
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
	l := &ModuleLoader{Lim: lim, Root: root, Overlay: overlay, State: map[string]int{}, Programs: map[string]*Program{}, Stack: map[string]bool{}}
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
	src := l.Overlay[path]
	if src == nil {
		var d *Diagnostic
		src, d = ReadSource(path, l.Lim)
		if d != nil {
			return nil, d
		}
	}
	prog, d := Parse(src, l.Lim)
	if d != nil {
		return nil, d
	}
	prog.Module = path
	prog.VisibilityScope = moduleVisibilityScope(path, l.Root)
	prog.Source.VisibilityScope = prog.VisibilityScope
	for _, f := range prog.Functions {
		f.VisibilityScope = prog.VisibilityScope
	}
	for _, s := range prog.Structs {
		s.VisibilityScope = prog.VisibilityScope
	}
	for _, e := range prog.Enums {
		e.VisibilityScope = prog.VisibilityScope
	}
	for i := range prog.Imports {
		imp := prog.Imports[i]
		if filepath.IsAbs(imp.Path) || strings.ContainsRune(imp.Path, 0) || hasParent(imp.Path) {
			return nil, Diag(CatIO, imp.Tok.Source, imp.Tok.Line, imp.Tok.Column, "unsafe module path '%s'", imp.Path)
		}
		candidate := l.resolveImportPath(filepath.Dir(path), imp.Path)
		if !l.hasSource(candidate) {
			candidate = l.resolveImportPath(l.Root, imp.Path)
		}

		if !within(l.Root, candidate) {
			return nil, Diag(CatIO, imp.Tok.Source, imp.Tok.Line, imp.Tok.Column, "module path escapes the program root")
		}
		if !safeComponents(l.Root, candidate) {
			return nil, Diag(CatIO, imp.Tok.Source, imp.Tok.Line, imp.Tok.Column, "module path traverses a symlink")
		}
		if !l.hasSource(candidate) {
			if _, err := os.Stat(candidate); err != nil {
				return nil, Diag(CatIO, imp.Tok.Source, imp.Tok.Line, imp.Tok.Column, "cannot resolve module '%s': %v", imp.Path, err)
			}
		}
		if _, d = l.load(candidate); d != nil {
			return nil, d
		}
	}
	l.Programs[path] = prog
	return prog, nil
}

func (l *ModuleLoader) hasSource(path string) bool {
	if l.Overlay[path] != nil {
		return true
	}
	return fileExists(path)
}

func (l *ModuleLoader) resolveImportPath(base, imp string) string {
	candidate := resolveImportPath(base, imp)
	if l.hasSource(candidate) || filepath.Ext(filepath.Join(base, imp)) != "" {
		return candidate
	}
	// An unsaved package module may introduce its directory before the editor
	// has created that directory on disk, so stat alone cannot find main.kry.
	main := filepath.Clean(filepath.Join(base, imp, "main.kry"))
	if l.Overlay[main] != nil {
		return main
	}
	return candidate
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

// moduleVisibilityScope gives files in the same manifest-backed package access
// to private declarations without changing their module identity used by KIR.
// Files outside a package manifest retain file-local visibility.
func moduleVisibilityScope(path, root string) string {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if fileExists(filepath.Join(dir, "kry.toml")) {
			return dir
		}
		if dir == root || filepath.Dir(dir) == dir {
			break
		}
	}
	return path
}

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
	out := &Program{Source: root.Source, Module: root.Module, VisibilityScope: root.VisibilityScope, Imports: append([]ImportDecl{}, root.Imports...), Sources: []*Source{root.Source}, Statements: root.Statements, Functions: append([]*Function{}, root.Functions...), Structs: append([]*StructDecl{}, root.Structs...), Enums: append([]*EnumDecl{}, root.Enums...)}
	seen := map[string]bool{root.Source.Name: true}
	var add func(*Program)
	add = func(p *Program) {
		for _, imp := range p.Imports {
			candidate := l.resolveImportPath(filepath.Dir(p.Source.Name), imp.Path)
			if !l.hasSource(candidate) {
				candidate = l.resolveImportPath(l.Root, imp.Path)
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
