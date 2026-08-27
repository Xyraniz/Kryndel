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

const artifactMagic = "KRYNATIVE3\x00"
const compilerIdentity = "kryndel-go-1.2.0"

type ArtifactEntry struct {
	Path string
	Data []byte
	Hash [32]byte
}
type Artifact struct {
	Compiler, Target string
	Entries          []ArtifactEntry
}

func safeArtifactPath(p string) bool {
	if p == "" || strings.IndexByte(p, 0) >= 0 || filepath.IsAbs(p) || hasParent(p) {
		return false
	}
	return p != "." && p != ".."
}
func BuildArtifact(prog *Program, root string) ([]byte, *Diagnostic) {
	entries := []ArtifactEntry{{Path: "<root>", Data: []byte(prog.Source.Text)}}
	absRoot, _ := filepath.Abs(root)
	base := filepath.Dir(absRoot)
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
		if seen[rel] {
			continue
		}
		seen[rel] = true
		entries = append(entries, ArtifactEntry{Path: rel, Data: []byte(s.Text)})
	}
	for i := range entries {
		entries[i].Hash = sha256.Sum256(entries[i].Data)
	}
	sort.Slice(entries[1:], func(i, j int) bool { return entries[i+1].Path < entries[j+1].Path })
	var b bytes.Buffer
	b.WriteString(artifactMagic)
	writeU32(&b, 3)
	writeString(&b, compilerIdentity)
	b.Write([]byte{'K', 'R', 'Y'})
	writeString(&b, runtime.GOOS+"/"+runtime.GOARCH)
	writeU32(&b, uint32(len(entries)))
	for _, e := range entries {
		writeString(&b, e.Path)
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
	if string(magic) != artifactMagic {
		return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: invalid header")
	}
	ver, ok := readU32(r)
	if !ok || ver != 3 {
		return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: unsupported version")
	}
	compiler, ok := readString(r, 256)
	if !ok || compiler != compilerIdentity {
		return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: incompatible compiler or invalid length")
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
	a := &Artifact{Compiler: compiler, Target: target}
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
		a.Entries = append(a.Entries, ArtifactEntry{Path: path, Data: payload, Hash: hash})
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
	return nil
}
func ProgramFromArtifact(a *Artifact, lim Limits) (*Program, *Diagnostic) {
	if len(a.Entries) == 0 {
		return nil, Diag(CatArtifact, nil, 1, 1, "malformed native artifact: missing root")
	}
	rootSrc := &Source{Name: "<artifact-root>", Text: string(a.Entries[0].Data)}
	root, d := Parse(rootSrc, lim)
	if d != nil {
		return nil, d
	}
	out := &Program{Source: rootSrc, Module: rootSrc.Name, Statements: root.Statements, Functions: append([]*Function{}, root.Functions...), Structs: append([]*StructDecl{}, root.Structs...), Enums: append([]*EnumDecl{}, root.Enums...), Sources: []*Source{rootSrc}}
	for _, e := range a.Entries[1:] {
		s := &Source{Name: e.Path, Text: string(e.Data)}
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
