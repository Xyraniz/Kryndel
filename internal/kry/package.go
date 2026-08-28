package kry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type PackageManifest struct {
	Name, Version, Kryndel string
	Dependencies           map[string]string
	TargetDependencies     map[string]map[string]string
}

type LockedPackage struct {
	Name, Version, URL, SHA256 string
	Dependencies               map[string]string
}

type LockFile struct {
	Version  int             `json:"version"`
	Packages []LockedPackage `json:"packages"`
}

type RegistryVersion struct {
	Version      string            `json:"version"`
	URL          string            `json:"url"`
	SHA256       string            `json:"sha256"`
	Dependencies map[string]string `json:"dependencies,omitempty"`
}

type RegistryIndex struct {
	Name     string            `json:"name"`
	Versions []RegistryVersion `json:"versions"`
}

type PackageManager struct {
	Registry string
	Client   *http.Client
	CacheDir string
	Offline  bool
}

func NewPackageManager() *PackageManager {
	reg := os.Getenv("KRY_REGISTRY")
	if reg == "" {
		reg = "http://127.0.0.1:8765"
	}
	cache := os.Getenv("KRY_CACHE")
	if cache == "" {
		if h, err := os.UserHomeDir(); err == nil {
			cache = filepath.Join(h, ".cache", "kryndel")
		} else {
			cache = filepath.Join(os.TempDir(), "kryndel-cache")
		}
	}
	return &PackageManager{Registry: strings.TrimRight(reg, "/"), Client: &http.Client{Timeout: 20 * time.Second}, CacheDir: cache, Offline: os.Getenv("KRY_OFFLINE") == "1"}
}

func ParseManifest(data string) (PackageManifest, error) {
	m := PackageManifest{Dependencies: map[string]string{}, TargetDependencies: map[string]map[string]string{}}
	section := ""
	for _, raw := range strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return m, fmt.Errorf("invalid manifest line %q", raw)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), "\"")
		switch section {
		case "package":
			switch key {
			case "name":
				m.Name = value
			case "version":
				m.Version = value
			case "kryndel":
				m.Kryndel = value
			default:
				return m, fmt.Errorf("unknown package key %q", key)
			}
		case "dependencies":
			m.Dependencies[key] = value
		default:
			if strings.HasPrefix(section, "target.") && strings.HasSuffix(section, ".dependencies") {
				target := strings.TrimSuffix(strings.TrimPrefix(section, "target."), ".dependencies")
				if m.TargetDependencies[target] == nil {
					m.TargetDependencies[target] = map[string]string{}
				}
				m.TargetDependencies[target][key] = value
			} else {
				return m, fmt.Errorf("unknown manifest section [%s]", section)
			}
		}
	}
	if !validPackageName(m.Name) {
		return m, fmt.Errorf("package name must contain only letters, digits, '-' or '_' and be non-empty")
	}
	if m.Version == "" {
		return m, fmt.Errorf("package version is required")
	}
	return m, nil
}

func FormatManifest(m PackageManifest) string {
	var b strings.Builder
	b.WriteString("[package]\nname = \"")
	b.WriteString(m.Name)
	b.WriteString("\"\nversion = \"")
	b.WriteString(m.Version)
	b.WriteString("\"\nkryndel = \"")
	b.WriteString(m.Kryndel)
	b.WriteString("\"\n\n[dependencies]\n")
	keys := sortedKeys(m.Dependencies)
	for _, k := range keys {
		b.WriteString(k + " = \"" + m.Dependencies[k] + "\"\n")
	}
	targets := sortedKeys(m.TargetDependencies)
	for _, target := range targets {
		b.WriteString("\n[target." + target + ".dependencies]\n")
		for _, k := range sortedKeys(m.TargetDependencies[target]) {
			b.WriteString(k + " = \"" + m.TargetDependencies[target][k] + "\"\n")
		}
	}
	return b.String()
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func validVersion(s string) bool {
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}
func validPackageName(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if !(r == '-' || r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func ReadManifest(dir string) (PackageManifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, "kry.toml"))
	if err != nil {
		return PackageManifest{}, err
	}
	return ParseManifest(string(data))
}

func WriteManifest(dir string, m PackageManifest) error {
	return os.WriteFile(filepath.Join(dir, "kry.toml"), []byte(FormatManifest(m)), 0o644)
}

func AddDependency(dir, name, version string) error {
	if !validPackageName(name) {
		return fmt.Errorf("invalid package name %q", name)
	}
	m, err := ReadManifest(dir)
	if err != nil {
		return err
	}
	if version == "" {
		version = "*"
	}
	m.Dependencies[name] = version
	return WriteManifest(dir, m)
}

func (pm *PackageManager) index(name string) (RegistryIndex, error) {
	if !validPackageName(name) {
		return RegistryIndex{}, fmt.Errorf("invalid package name %q", name)
	}
	cachePath := filepath.Join(pm.CacheDir, "index", name+".json")
	var data []byte
	if pm.Offline {
		var err error
		data, err = os.ReadFile(cachePath)
		if err != nil {
			return RegistryIndex{}, fmt.Errorf("offline mode: registry index for %s is not cached", name)
		}
	} else {
		resp, err := pm.Client.Get(pm.Registry + "/index/" + url.PathEscape(name) + ".json")
		if err != nil {
			return RegistryIndex{}, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return RegistryIndex{}, fmt.Errorf("registry returned HTTP %d", resp.StatusCode)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if err != nil {
			return RegistryIndex{}, err
		}
		_ = os.MkdirAll(filepath.Dir(cachePath), 0o755)
		_ = os.WriteFile(cachePath, data, 0o644)
	}
	var idx RegistryIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return idx, fmt.Errorf("invalid registry index: %w", err)
	}
	if idx.Name != name {
		return idx, fmt.Errorf("registry index name mismatch")
	}
	sort.Slice(idx.Versions, func(i, j int) bool { return compareVersion(idx.Versions[i].Version, idx.Versions[j].Version) > 0 })
	return idx, nil
}

func (pm *PackageManager) resolve(name, constraint string) (RegistryVersion, error) {
	idx, err := pm.index(name)
	if err != nil {
		return RegistryVersion{}, err
	}
	for _, v := range idx.Versions {
		if satisfies(v.Version, constraint) {
			return v, nil
		}
	}
	return RegistryVersion{}, fmt.Errorf("no version of %s satisfies %q", name, constraint)
}

func satisfies(version, constraint string) bool {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" || constraint == "*" {
		return true
	}
	if strings.HasPrefix(constraint, "^") {
		base := strings.TrimPrefix(constraint, "^")
		return major(version) == major(base) && compareVersion(version, base) >= 0
	}
	if strings.HasPrefix(constraint, ">=") {
		return compareVersion(version, strings.TrimSpace(strings.TrimPrefix(constraint, ">="))) >= 0
	}
	if strings.HasPrefix(constraint, "~") {
		base := strings.TrimPrefix(constraint, "~")
		return major(version) == major(base) && minor(version) == minor(base) && compareVersion(version, base) >= 0
	}
	return compareVersion(version, constraint) == 0
}

func versionParts(s string) [3]int {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	for i := 0; i < len(parts) && i < len(out); i++ {
		digits := strings.TrimLeftFunc(parts[i], func(r rune) bool { return r < '0' || r > '9' })
		if digits == "" {
			continue
		}
		out[i], _ = strconv.Atoi(digits)
	}
	return out
}

func compareVersion(a, b string) int {
	x, y := versionParts(a), versionParts(b)
	for i := 0; i < 3; i++ {
		if x[i] < y[i] {
			return -1
		}
		if x[i] > y[i] {
			return 1
		}
	}
	return 0
}
func major(s string) int { return versionParts(s)[0] }
func minor(s string) int { return versionParts(s)[1] }

func (pm *PackageManager) download(name string, v RegistryVersion) (LockedPackage, error) {
	if v.URL == "" || v.SHA256 == "" {
		return LockedPackage{}, fmt.Errorf("registry entry for %s@%s lacks URL or SHA-256", name, v.Version)
	}
	if pm.Offline {
		return LockedPackage{}, fmt.Errorf("offline mode: package %s@%s is not cached", name, v.Version)
	}
	downloadURL := v.URL
	if parsed, err := url.Parse(downloadURL); err == nil && !parsed.IsAbs() {
		downloadURL = pm.Registry + "/" + strings.TrimLeft(downloadURL, "/")
	}
	archivePath := filepath.Join(pm.CacheDir, "archives", name+"-"+v.Version+".tar.gz")
	var data []byte
	var err error
	if pm.Offline {
		data, err = os.ReadFile(archivePath)
		if err != nil {
			return LockedPackage{}, fmt.Errorf("offline mode: package %s@%s is not cached", name, v.Version)
		}
	} else {
		resp, getErr := pm.Client.Get(downloadURL)
		if getErr != nil {
			return LockedPackage{}, getErr
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return LockedPackage{}, fmt.Errorf("package download returned HTTP %d", resp.StatusCode)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, 64<<20+1))
		if err != nil {
			return LockedPackage{}, err
		}
		_ = os.MkdirAll(filepath.Dir(archivePath), 0o755)
		_ = os.WriteFile(archivePath, data, 0o644)
	}
	if len(data) > 64<<20 {
		return LockedPackage{}, fmt.Errorf("package is too large")
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, v.SHA256) {
		return LockedPackage{}, fmt.Errorf("hash mismatch for %s@%s", name, v.Version)
	}
	dir := filepath.Join(pm.CacheDir, name+"-"+v.Version)
	if err := os.RemoveAll(dir); err != nil {
		return LockedPackage{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return LockedPackage{}, err
	}
	if err := extractPackage(data, dir); err != nil {
		return LockedPackage{}, err
	}
	return LockedPackage{Name: name, Version: v.Version, URL: downloadURL, SHA256: got, Dependencies: v.Dependencies}, nil
}

func extractPackage(data []byte, dest string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("invalid package archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg {
			return fmt.Errorf("package contains unsupported entry %q", h.Name)
		}
		name, err := safeArchivePath(h.Name)
		if err != nil {
			return err
		}
		if seen[name] {
			return fmt.Errorf("package contains duplicate entry %q", name)
		}
		seen[name] = true
		if h.Size < 0 || h.Size > 64<<20 {
			return fmt.Errorf("package entry %q is too large", name)
		}
		target := filepath.Join(dest, name)
		if !within(dest, target) {
			return fmt.Errorf("package path escapes destination")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyN(f, tr, h.Size)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func safeArchivePath(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("unsafe package path %q", name)
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal in package entry %q", name)
	}
	return clean, nil
}

func (pm *PackageManager) Install(dir string, requested []string) (LockFile, error) {
	m, err := ReadManifest(dir)
	if err != nil {
		return LockFile{}, err
	}
	for _, name := range requested {
		if err := AddDependency(dir, name, "*"); err != nil {
			return LockFile{}, err
		}
		m.Dependencies[name] = "*"
	}
	target := runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		target = "windows-x64"
	}
	if runtime.GOOS == "windows" && runtime.GOARCH == "arm64" {
		target = "windows-arm64"
	}
	for name, constraint := range m.TargetDependencies[target] {
		m.Dependencies[name] = constraint
	}
	lock := LockFile{Version: 1}
	queue := sortedKeys(m.Dependencies)
	seen := map[string]bool{}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		seen[name] = true
		v, err := pm.resolve(name, m.Dependencies[name])
		if err != nil {
			return lock, err
		}
		lp, err := pm.download(name, v)
		if err != nil {
			return lock, err
		}
		cachePackage := filepath.Join(pm.CacheDir, name+"-"+v.Version)
		if err := copyPackageTree(cachePackage, filepath.Join(dir, "vendor", name)); err != nil {
			return lock, err
		}
		lock.Packages = append(lock.Packages, lp)
		for dep, constraint := range v.Dependencies {
			if !seen[dep] {
				if _, ok := m.Dependencies[dep]; !ok {
					m.Dependencies[dep] = constraint
				}
				queue = append(queue, dep)
			}
		}
		sort.Strings(queue)
	}
	sort.Slice(lock.Packages, func(i, j int) bool { return lock.Packages[i].Name < lock.Packages[j].Name })
	data, _ := json.MarshalIndent(lock, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "kry.lock"), append(data, '\n'), 0o644); err != nil {
		return lock, err
	}
	return lock, nil
}

func copyPackageTree(src, dest string) error {
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		if !within(src, path) || !within(dest, out) {
			return fmt.Errorf("package copy escaped destination")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	})
}

func (pm *PackageManager) CleanCache() error { return os.RemoveAll(pm.CacheDir) }

func (pm *PackageManager) Search(term string) ([]string, error) {
	if pm.Offline {
		return nil, fmt.Errorf("offline mode: registry search is unavailable")
	}
	resp, err := pm.Client.Get(pm.Registry + "/search?q=" + url.QueryEscape(term))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned HTTP %d", resp.StatusCode)
	}
	var names []string
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&names); err != nil {
		return nil, err
	}
	return names, nil
}

func (pm *PackageManager) Publish(dir string) error {
	m, err := ReadManifest(dir)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", m.Name+"-"+m.Version+"-*.tar.gz")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		return err
	}
	defer os.Remove(tmpPath)
	if err := PackageArchive(dir, tmpPath); err != nil {
		return err
	}
	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, pm.Registry+"/publish/"+url.PathEscape(m.Name)+"/"+url.PathEscape(m.Version), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/gzip")
	resp, err := pm.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("registry publish failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func NewProject(dir, name string) error {
	if name == "" {
		name = filepath.Base(dir)
	}
	if !validPackageName(name) {
		return fmt.Errorf("invalid project name")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	m := PackageManifest{Name: name, Version: "0.1.0", Kryndel: ">=1.3.0", Dependencies: map[string]string{}, TargetDependencies: map[string]map[string]string{}}
	if err := WriteManifest(dir, m); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "main.kry"), []byte("fn main() -> Nil {\n    println(\"Hello from Kryndel\")\n}\n"), 0o644)
}

func PackageArchive(dir, out string) error {
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	var files []string
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if rel == "kry.lock" || filepath.Base(path) == ".DS_Store" {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, rel := range files {
		name, err := safeArchivePath(rel)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return err
		}
		h := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), ModTime: time.Unix(0, 0), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	return nil
}

func ServeRegistry(root, addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		term := strings.ToLower(r.URL.Query().Get("q"))
		entries, _ := os.ReadDir(filepath.Join(root, "index"))
		var names []string
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			name := strings.TrimSuffix(entry.Name(), ".json")
			if term == "" || strings.Contains(strings.ToLower(name), term) {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(names)
	})
	mux.HandleFunc("/publish/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/publish/"), "/")
		if len(parts) != 2 || !validPackageName(parts[0]) || !validVersion(parts[1]) {
			http.Error(w, "invalid package coordinates", http.StatusBadRequest)
			return
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, 64<<20+1))
		if err != nil || len(data) > 64<<20 {
			http.Error(w, "invalid package body", http.StatusBadRequest)
			return
		}
		if err := os.MkdirAll(filepath.Join(root, "packages"), 0o755); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		fileName := parts[0] + "-" + parts[1] + ".tar.gz"
		if err := os.WriteFile(filepath.Join(root, "packages", fileName), data, 0o644); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		sum := sha256.Sum256(data)
		indexPath := filepath.Join(root, "index", parts[0]+".json")
		_ = os.MkdirAll(filepath.Dir(indexPath), 0o755)
		idx := RegistryIndex{Name: parts[0]}
		if old, err := os.ReadFile(indexPath); err == nil {
			_ = json.Unmarshal(old, &idx)
		}
		found := false
		for i := range idx.Versions {
			if idx.Versions[i].Version == parts[1] {
				idx.Versions[i] = RegistryVersion{Version: parts[1], URL: "/packages/" + fileName, SHA256: hex.EncodeToString(sum[:])}
				found = true
			}
		}
		if !found {
			idx.Versions = append(idx.Versions, RegistryVersion{Version: parts[1], URL: "/packages/" + fileName, SHA256: hex.EncodeToString(sum[:])})
		}
		encoded, _ := json.MarshalIndent(idx, "", "  ")
		if err := os.WriteFile(indexPath, append(encoded, '\n'), 0o644); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("/index/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/index/"), ".json")
		if !validPackageName(name) {
			http.Error(w, "invalid name", http.StatusBadRequest)
			return
		}
		data, err := os.ReadFile(filepath.Join(root, "index", name+".json"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	})
	mux.Handle("/packages/", http.StripPrefix("/packages/", http.FileServer(http.Dir(filepath.Join(root, "packages")))))
	return http.ListenAndServe(addr, mux)
}
