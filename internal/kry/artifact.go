package kry

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	artifactMagic            = "KRYNATIVE5\x00"
	previousArtifactMagic    = "KRYNATIVE4\x00"
	legacyArtifactMagic      = "KRYNATIVE3\x00"
	compilerIdentity         = "kryndel-go-" + CompilerVersion
	previousCompilerIdentity = compilerIdentity
	legacyCompilerID         = "kryndel-go-1.2.0"
	artifactRootScope        = "<root>"
)

type ArtifactEntry struct {
	Path            string
	VisibilityScope string
	Data            []byte
	Hash            [32]byte
}
type Artifact struct {
	Compiler, Target, LanguageVersion string
	Entries                           []ArtifactEntry
}

func safeArtifactPath(p string) bool {
	if p == "" || strings.IndexByte(p, 0) >= 0 || filepath.IsAbs(p) || hasParent(p) {
		return false
	}
	return p != "." && p != ".."
}

func safeArtifactScope(scope string) bool {
	return scope == artifactRootScope || safeArtifactPath(scope)
}

func relativeArtifactScope(base, scope string) (string, error) {
	if scope == artifactRootScope {
		return artifactRootScope, nil
	}
	if !filepath.IsAbs(scope) {
		scope = filepath.Join(base, scope)
	}
	absScope, err := filepath.Abs(scope)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, absScope)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("scope escapes artifact root")
	}
	if rel == "." {
		return artifactRootScope, nil
	}
	rel = filepath.ToSlash(rel)
	if !safeArtifactPath(rel) {
		return "", fmt.Errorf("unsafe module scope")
	}
	return rel, nil
}

func BuildArtifact(prog *Program, root string) ([]byte, *Diagnostic) {
	absRoot, _ := filepath.Abs(root)
	base := filepath.Dir(absRoot)
	rootScope := sourceVisibilityScope(prog.Source)
	if rootScope == "" {
		rootScope = prog.VisibilityScope
	}
	if rootScope == "" {
		rootScope = prog.Module
	}
	rootScopePath, err := relativeArtifactScope(base, rootScope)
	if err != nil {
		return nil, Diag(CatArtifact, prog.Source, 1, 1, "module visibility scope is outside artifact root")
	}
	entries := []ArtifactEntry{{Path: "<root>", VisibilityScope: rootScopePath, Data: []byte(prog.Source.Text)}}
	seen := map[string]bool{"<root>": true}
	for _, s := range prog.Sources[1:] {
		rel, err := filepath.Rel(base, s.Name)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, Diag(CatArtifact, s, 1, 1, "module path is outside artifact root")
		}
		rel = filepath.ToSlash(rel)
		if filepath.Ext(rel) == "" {
			rel += ".kry"
		}
		if !safeArtifactPath(rel) {
			return nil, Diag(CatArtifact, s, 1, 1, "unsafe artifact path '%s'", rel)
		}
		scope := sourceVisibilityScope(s)
		scopePath, err := relativeArtifactScope(base, scope)
		if err != nil {
			return nil, Diag(CatArtifact, s, 1, 1, "module visibility scope is outside artifact root")
		}
		if seen[rel] {
			continue
		}
		seen[rel] = true
		entries = append(entries, ArtifactEntry{Path: rel, VisibilityScope: scopePath, Data: []byte(s.Text)})
	}
	for i := range entries {
		entries[i].Hash = sha256.Sum256(entries[i].Data)
	}
	sort.Slice(entries[1:], func(i, j int) bool { return entries[i+1].Path < entries[j+1].Path })
	var b bytes.Buffer
	b.WriteString(artifactMagic)
	writeU32(&b, 5)
	writeString(&b, compilerIdentity)
	writeString(&b, LanguageVersion)
	b.Write([]byte{'K', 'R', 'Y'})
	writeString(&b, runtime.GOOS+"/"+runtime.GOARCH)
	writeU32(&b, uint32(len(entries)))
	for _, e := range entries {
		writeString(&b, e.Path)
		writeString(&b, e.VisibilityScope)
		writeU64(&b, uint64(len(e.Data)))
		b.Write(e.Hash[:])
		b.Write(e.Data)
	}
	return b.Bytes(), nil
}
func writeU32(b *bytes.Buffer, x uint32) {
	var v [4]byte
	binary.LittleEndian.PutUint32(v[:], x)
	b.Write(v[:])
}
func writeU64(b *bytes.Buffer, x uint64) {
	var v [8]byte
	binary.LittleEndian.PutUint64(v[:], x)
	b.Write(v[:])
}
func writeString(b *bytes.Buffer, s string) { writeU64(b, uint64(len(s))); b.WriteString(s) }
func DecodeArtifact(data []byte, lim Limits) (*Artifact, *Diagnostic) {
	if len(data) > lim.MaxArtifactBytes {
		return nil, Diag(CatResource, nil, 1, 1, "artifact exceeds configured input size limit")
	}
	r := bytes.NewReader(data)
	magic := make([]byte, len(artifactMagic))
	if n, e := r.Read(magic); e != nil || n != len(magic) {
		return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: truncated header")
	}
	formatVersion := uint32(0)
	expectedCompiler := compilerIdentity
	switch string(magic) {
	case artifactMagic:
		formatVersion = 5
	case previousArtifactMagic:
		formatVersion = 4
		expectedCompiler = previousCompilerIdentity
	case legacyArtifactMagic:
		formatVersion = 3
		expectedCompiler = legacyCompilerID
	default:
		return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: invalid header")
	}
	ver, ok := readU32(r)
	if !ok || ver != formatVersion {
		return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: unsupported version")
	}
	compiler, ok := readString(r, 256)
	if !ok || compiler != expectedCompiler {
		return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: incompatible compiler or invalid length")
	}
	languageVersion := LanguageVersion
	if formatVersion >= 4 {
		languageVersion, ok = readString(r, 64)
		if !ok {
			return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: invalid language version")
		}
		if err := checkLanguageVersion(languageVersion); err != nil {
			return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: %s", err)
		}
	}
	var tag [3]byte
	if n, err := r.Read(tag[:]); err != nil || n != len(tag) || string(tag[:]) != "KRY" {
		return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: invalid header tag or length")
	}
	target, ok := readString(r, 64)
	if !ok || target != runtime.GOOS+"/"+runtime.GOARCH {
		return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: incompatible target")
	}
	n, ok := readU32(r)
	if !ok || n == 0 || int64(n) > int64(lim.MaxImports+1) {
		return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: invalid entry count")
	}
	a := &Artifact{Compiler: compiler, Target: target, LanguageVersion: languageVersion}
	seen := map[string]bool{}
	for i := uint32(0); i < n; i++ {
		path, ok := readString(r, lim.MaxSourceBytes)
		if !ok || !safeArtifactPath(path) && path != "<root>" {
			return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: unsafe path")
		}
		if i == 0 && path != "<root>" {
			return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: root entry must be first")
		}
		if i > 0 && path == "<root>" {
			return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: duplicate root entry")
		}
		if seen[path] {
			return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: duplicate entry")
		}
		seen[path] = true
		scope := ""
		if formatVersion >= 5 {
			scope, ok = readString(r, lim.MaxSourceBytes)
			if !ok || !safeArtifactScope(scope) {
				return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: unsafe module visibility scope")
			}
		}
		size, ok := readU64(r)
		if !ok || size > uint64(lim.MaxSourceBytes) || size > uint64(r.Len())-32 {
			return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: invalid length")
		}
		var hash [32]byte
		if n, e := r.Read(hash[:]); e != nil || n != len(hash) {
			return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: truncated hash")
		}
		payload := make([]byte, size)
		if n, e := r.Read(payload); e != nil || n != len(payload) {
			return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: truncated data")
		}
		if sha256.Sum256(payload) != hash {
			return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: invalid hash")
		}
		if !validUTF8(payload) {
			return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: invalid UTF-8")
		}
		a.Entries = append(a.Entries, ArtifactEntry{Path: path, VisibilityScope: scope, Data: payload, Hash: hash})
	}
	if r.Len() != 0 {
		return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: trailing bytes")
	}
	if len(a.Entries) == 0 || a.Entries[0].Path != "<root>" {
		return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: missing root")
	}
	return a, nil
}
func readU32(r *bytes.Reader) (uint32, bool) {
	var v [4]byte
	if _, e := r.Read(v[:]); e != nil {
		return 0, false
	}
	return binary.LittleEndian.Uint32(v[:]), true
}
func readU64(r *bytes.Reader) (uint64, bool) {
	var v [8]byte
	if _, e := r.Read(v[:]); e != nil {
		return 0, false
	}
	return binary.LittleEndian.Uint64(v[:]), true
}
func readString(r *bytes.Reader, max int) (string, bool) {
	n, ok := readU64(r)
	if !ok || n > uint64(max) || n > uint64(r.Len()) {
		return "", false
	}
	b := make([]byte, n)
	if _, e := r.Read(b); e != nil {
		return "", false
	}
	return string(b), true
}
func WriteArtifact(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, e := os.CreateTemp(dir, ".kryndel-artifact-")
	if e != nil {
		return e
	}
	name := tmp.Name()
	ok := false
	defer func() {
		tmp.Close()
		if !ok {
			os.Remove(name)
		}
	}()
	if _, e = tmp.Write(data); e != nil {
		return e
	}
	if e = tmp.Sync(); e != nil {
		return e
	}
	if e = tmp.Close(); e != nil {
		return e
	}
	if e = os.Rename(name, path); e != nil {
		return e
	}
	ok = true
	// Native executables must be runnable; the artifact writer is also used
	// for .kexe containers, so only widen permissions when the caller asked
	// for an executable by extension.
	if isExecutablePath(path) {
		_ = os.Chmod(path, 0o755)
	}
	return nil
}

// isExecutablePath reports whether a written file should be marked executable.
func isExecutablePath(path string) bool {
	switch filepath.Ext(path) {
	case ".exe", ".elf", ".out", ".bin":
		return true
	}
	return false
}
func ProgramFromArtifact(a *Artifact, lim Limits) (*Program, *Diagnostic) {
	if len(a.Entries) == 0 {
		return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: missing root")
	}
	rootSrc := &Source{Name: "<artifact-root>", Text: string(a.Entries[0].Data), VisibilityScope: a.Entries[0].VisibilityScope}
	root, d := Parse(rootSrc, lim)
	if d != nil {
		return nil, d
	}
	out := &Program{Source: rootSrc, Module: rootSrc.Name, VisibilityScope: root.VisibilityScope, Statements: root.Statements, Functions: append([]*Function{}, root.Functions...), Structs: append([]*StructDecl{}, root.Structs...), Enums: append([]*EnumDecl{}, root.Enums...), Sources: []*Source{rootSrc}}
	for _, e := range a.Entries[1:] {
		s := &Source{Name: e.Path, Text: string(e.Data), VisibilityScope: e.VisibilityScope}
		p, d := Parse(s, lim)
		if d != nil {
			return nil, d
		}
		out.Sources = append(out.Sources, s)
		out.Functions = append(out.Functions, p.Functions...)
		out.Structs = append(out.Structs, p.Structs...)
		out.Enums = append(out.Enums, p.Enums...)
	}
	return out, nil
}

var _ = fmt.Sprintf
