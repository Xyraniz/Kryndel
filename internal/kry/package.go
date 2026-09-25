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
	"strings"
	"sync"
	"time"
)

type PackageManifest struct {
	Name, Version, Kryndel, LanguageVersion string
	Dependencies                            map[string]string
	TargetDependencies                      map[string]map[string]string
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
	Version            string                       `json:"version"`
	URL                string                       `json:"url"`
	SHA256             string                       `json:"sha256"`
	Dependencies       map[string]string            `json:"dependencies,omitempty"`
	TargetDependencies map[string]map[string]string `json:"target_dependencies,omitempty"`
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
		reg = "https://raw.githubusercontent.com/Xyraniz/Kryndel/main/registry"
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
	m := PackageManifest{LanguageVersion: LanguageVersion, Dependencies: map[string]string{}, TargetDependencies: map[string]map[string]string{}}
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
			case "language_version":
				m.LanguageVersion = value
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
	if !validVersion(m.Version) {
		return m, fmt.Errorf("package version %q is invalid; expected MAJOR.MINOR.PATCH", m.Version)
	}
	if m.Kryndel == "" {
		return m, fmt.Errorf("package Kryndel requirement is required")
	}
	if _, err := parseVersionConstraint(m.Kryndel); err != nil {
		return m, fmt.Errorf("invalid Kryndel requirement: %w", err)
	}
	if err := validateDependencyConstraints(m.Dependencies); err != nil {
		return m, err
	}
	for target, dependencies := range m.TargetDependencies {
		if err := validateDependencyConstraints(dependencies); err != nil {
			return m, fmt.Errorf("target %q: %w", target, err)
		}
	}
	if err := checkLanguageVersion(m.LanguageVersion); err != nil {
		return m, err
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
	b.WriteString("\"\nlanguage_version = \"")
	languageVersion := m.LanguageVersion
	if languageVersion == "" {
		languageVersion = LanguageVersion
	}
	b.WriteString(languageVersion)
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

func validateDependencyConstraints(dependencies map[string]string) error {
	for name, constraint := range dependencies {
		if !validPackageName(name) {
			return fmt.Errorf("invalid dependency name %q", name)
		}
		if _, err := parseVersionConstraint(constraint); err != nil {
			return fmt.Errorf("dependency %q: %w", name, err)
		}
	}
	return nil
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

func ValidateProjectForPath(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(absPath)
	if info, statErr := os.Stat(absPath); statErr == nil && info.IsDir() {
		dir = absPath
	}
	for {
		manifestPath := filepath.Join(dir, "kry.toml")
		if _, statErr := os.Stat(manifestPath); statErr == nil {
			manifest, readErr := ReadManifest(dir)
			if readErr != nil {
				return fmt.Errorf("invalid project manifest %s: %w", manifestPath, readErr)
			}
			return ValidateCompilerRequirement(manifest.Name, manifest.Kryndel)
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf("cannot inspect project manifest %s: %w", manifestPath, statErr)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
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
	seenVersions := make(map[string]bool, len(idx.Versions))
	for _, version := range idx.Versions {
		if !validVersion(version.Version) {
			return RegistryIndex{}, fmt.Errorf("registry index for %s contains invalid version %q", name, version.Version)
		}
		if seenVersions[version.Version] {
			return RegistryIndex{}, fmt.Errorf("registry index for %s contains duplicate version %q", name, version.Version)
		}
		seenVersions[version.Version] = true
		if err := validateDependencyConstraints(version.Dependencies); err != nil {
			return RegistryIndex{}, fmt.Errorf("registry index for %s@%s: %w", name, version.Version, err)
		}
		for target, dependencies := range version.TargetDependencies {
			if err := validateDependencyConstraints(dependencies); err != nil {
				return RegistryIndex{}, fmt.Errorf("registry index for %s@%s target %q: %w", name, version.Version, target, err)
			}
		}
	}
	sort.Slice(idx.Versions, func(i, j int) bool { return compareVersion(idx.Versions[i].Version, idx.Versions[j].Version) > 0 })
	return idx, nil
}

func (pm *PackageManager) resolve(name, constraint string) (RegistryVersion, error) {
	if _, err := parseVersionConstraint(constraint); err != nil {
		return RegistryVersion{}, fmt.Errorf("invalid version constraint for %s: %w", name, err)
	}
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

func (pm *PackageManager) download(name string, v RegistryVersion) (LockedPackage, error) {
	if v.URL == "" || v.SHA256 == "" {
		return LockedPackage{}, fmt.Errorf("registry entry for %s@%s lacks URL or SHA-256", name, v.Version)
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
	}
	if len(data) > 64<<20 {
		return LockedPackage{}, fmt.Errorf("package is too large")
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, v.SHA256) {
		if pm.Offline {
			_ = os.Remove(archivePath)
		}
		return LockedPackage{}, fmt.Errorf("hash mismatch for %s@%s", name, v.Version)
	}
	manifest, err := validatePackageArchive(data, name, v.Version)
	if err != nil {
		if pm.Offline {
			_ = os.Remove(archivePath)
		}
		return LockedPackage{}, fmt.Errorf("invalid package %s@%s: %w", name, v.Version, err)
	}
	if !sameStringMap(manifest.Dependencies, v.Dependencies) || !sameTargetDependencyMap(manifest.TargetDependencies, v.TargetDependencies) {
		if pm.Offline {
			_ = os.Remove(archivePath)
		}
		return LockedPackage{}, fmt.Errorf("package %s@%s dependency metadata does not match the registry index", name, v.Version)
	}
	if err := ValidateCompilerRequirement(manifest.Name, manifest.Kryndel); err != nil {
		return LockedPackage{}, err
	}
	dir := filepath.Join(pm.CacheDir, name+"-"+v.Version)
	if err := os.RemoveAll(dir); err != nil {
		return LockedPackage{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return LockedPackage{}, err
	}
	if err := extractPackage(data, dir); err != nil {
		_ = os.RemoveAll(dir)
		if pm.Offline {
			_ = os.Remove(archivePath)
		}
		return LockedPackage{}, err
	}
	if !pm.Offline {
		if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
			_ = os.RemoveAll(dir)
			return LockedPackage{}, err
		}
		if err := atomicWrite(archivePath, data); err != nil {
			_ = os.RemoveAll(dir)
			return LockedPackage{}, err
		}
	}
	return LockedPackage{Name: name, Version: v.Version, URL: downloadURL, SHA256: got, Dependencies: v.Dependencies}, nil
}

func sameStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func sameTargetDependencyMap(a, b map[string]map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for target, dependencies := range a {
		if !sameStringMap(dependencies, b[target]) {
			return false
		}
	}
	return true
}

func resolvedDependencies(base map[string]string, targeted map[string]map[string]string, target string) map[string]string {
	dependencies := make(map[string]string, len(base)+len(targeted[target]))
	for name, constraint := range base {
		dependencies[name] = constraint
	}
	for name, constraint := range targeted[target] {
		dependencies[name] = constraint
	}
	return dependencies
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
	if err := ValidateCompilerRequirement(m.Name, m.Kryndel); err != nil {
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
		lp.Dependencies = resolvedDependencies(v.Dependencies, v.TargetDependencies, target)
		lock.Packages = append(lock.Packages, lp)
		for dep, constraint := range lp.Dependencies {
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

// Uninstall removes direct dependencies and prunes only packages recorded in
// kry.lock that are no longer reachable from the remaining manifest roots.
// It never touches the global cache, so a later install can reuse downloads.
func (pm *PackageManager) Uninstall(dir string, requested []string) (LockFile, error) {
	m, err := ReadManifest(dir)
	if err != nil {
		return LockFile{}, err
	}
	if len(requested) == 0 {
		return LockFile{}, fmt.Errorf("uninstall expects at least one package")
	}
	for _, name := range requested {
		if !validPackageName(name) {
			return LockFile{}, fmt.Errorf("invalid package name %q", name)
		}
		if _, ok := m.Dependencies[name]; !ok {
			return LockFile{}, fmt.Errorf("package %q is not a direct dependency", name)
		}
		delete(m.Dependencies, name)
	}
	lock := LockFile{Version: 1}
	if data, readErr := os.ReadFile(filepath.Join(dir, "kry.lock")); readErr == nil {
		if err := json.Unmarshal(data, &lock); err != nil {
			return LockFile{}, fmt.Errorf("invalid kry.lock: %w", err)
		}
	}
	byName := make(map[string]LockedPackage, len(lock.Packages))
	for _, pkg := range lock.Packages {
		byName[pkg.Name] = pkg
	}
	oldPackages := append([]LockedPackage(nil), lock.Packages...)
	lock.Packages = nil
	reachable := map[string]bool{}
	queue := sortedKeys(m.Dependencies)
	target := runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		target = "windows-x64"
	} else if runtime.GOOS == "windows" && runtime.GOARCH == "arm64" {
		target = "windows-arm64"
	}
	for targetPackage := range m.TargetDependencies[target] {
		queue = append(queue, targetPackage)
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if reachable[name] {
			continue
		}
		reachable[name] = true
		if pkg, ok := byName[name]; ok {
			for dep := range pkg.Dependencies {
				queue = append(queue, dep)
			}
			sort.Strings(queue)
		}
	}
	for _, pkg := range oldPackages {
		if !reachable[pkg.Name] {
			if err := os.RemoveAll(filepath.Join(dir, "vendor", pkg.Name)); err != nil {
				return LockFile{}, err
			}
			continue
		}
		lock.Packages = append(lock.Packages, pkg)
	}
	if err := WriteManifest(dir, m); err != nil {
		return LockFile{}, err
	}
	sort.Slice(lock.Packages, func(i, j int) bool { return lock.Packages[i].Name < lock.Packages[j].Name })
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return LockFile{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "kry.lock"), append(data, '\n'), 0o644); err != nil {
		return LockFile{}, err
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
	return ensureProjectFiles(dir, name, true)
}

// EnsureProject makes `kry install package` useful in a freshly created
// directory without overwriting an existing entrypoint.
func EnsureProject(dir, name string) error {
	if name == "" {
		name = filepath.Base(dir)
	}
	if !validPackageName(name) {
		name = "kryndel-app"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return ensureProjectFiles(dir, name, false)
}

func ensureProjectFiles(dir, name string, replaceMain bool) error {
	m := PackageManifest{Name: name, Version: "0.1.0", Kryndel: ">=" + CompilerVersion, LanguageVersion: LanguageVersion, Dependencies: map[string]string{}, TargetDependencies: map[string]map[string]string{}}
	if _, err := os.Stat(filepath.Join(dir, "kry.toml")); os.IsNotExist(err) {
		if err := WriteManifest(dir, m); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	mainPath := filepath.Join(dir, "main.kry")
	if replaceMain {
		return os.WriteFile(mainPath, []byte("fn main() -> Nil {\n    println(\"Hello from Kryndel\")\n}\n"), 0o644)
	}
	if _, err := os.Stat(mainPath); os.IsNotExist(err) {
		return os.WriteFile(mainPath, []byte("fn main() -> Nil {\n    println(\"Hello from Kryndel\")\n}\n"), 0o644)
	} else if err != nil {
		return err
	}
	return nil
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
	var publishMu sync.Mutex
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
		w.Header().Set("Cache-Control", "public, max-age=300")
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
		publishMu.Lock()
		defer publishMu.Unlock()
		manifest, err := validatePackageArchive(data, parts[0], parts[1])
		if err != nil {
			http.Error(w, "invalid package archive: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := os.MkdirAll(filepath.Join(root, "packages"), 0o755); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		fileName := parts[0] + "-" + parts[1] + ".tar.gz"
		if err := atomicWrite(filepath.Join(root, "packages", fileName), data); err != nil {
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
				idx.Versions[i] = RegistryVersion{Version: parts[1], URL: "/packages/" + fileName, SHA256: hex.EncodeToString(sum[:]), Dependencies: manifest.Dependencies, TargetDependencies: manifest.TargetDependencies}
				found = true
			}
		}
		if !found {
			idx.Versions = append(idx.Versions, RegistryVersion{Version: parts[1], URL: "/packages/" + fileName, SHA256: hex.EncodeToString(sum[:]), Dependencies: manifest.Dependencies, TargetDependencies: manifest.TargetDependencies})
		}
		encoded, _ := json.MarshalIndent(idx, "", "  ")
		if err := atomicWrite(indexPath, append(encoded, '\n')); err != nil {
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
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	})
	mux.Handle("/packages/", http.StripPrefix("/packages/", http.FileServer(http.Dir(filepath.Join(root, "packages")))))
	return http.ListenAndServe(addr, mux)
}

func atomicWrite(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".kry-registry-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func validatePublishedArchive(data []byte, expectedName, expectedVersion string) error {
	_, err := validatePackageArchive(data, expectedName, expectedVersion)
	return err
}

func validatePackageArchive(data []byte, expectedName, expectedVersion string) (PackageManifest, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return PackageManifest{}, fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	var manifest []byte
	mainFound := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return PackageManifest{}, err
		}
		if h.Typeflag != tar.TypeReg {
			return PackageManifest{}, fmt.Errorf("entry %q is not a regular file", h.Name)
		}
		name, err := safeArchivePath(h.Name)
		if err != nil {
			return PackageManifest{}, err
		}
		if seen[name] {
			return PackageManifest{}, fmt.Errorf("duplicate entry %q", name)
		}
		seen[name] = true
		if h.Size < 0 || h.Size > 64<<20 {
			return PackageManifest{}, fmt.Errorf("entry %q is too large", name)
		}
		content, err := io.ReadAll(io.LimitReader(tr, h.Size+1))
		if err != nil || int64(len(content)) != h.Size {
			return PackageManifest{}, fmt.Errorf("truncated entry %q", name)
		}
		if name == "kry.toml" {
			manifest = content
		}
		if name == "main.kry" {
			mainFound = true
		}
	}
	if len(manifest) == 0 {
		return PackageManifest{}, fmt.Errorf("archive must contain kry.toml")
	}
	if !mainFound {
		return PackageManifest{}, fmt.Errorf("archive must contain main.kry")
	}
	m, err := ParseManifest(string(manifest))
	if err != nil {
		return PackageManifest{}, err
	}
	if m.Name != expectedName || m.Version != expectedVersion {
		return PackageManifest{}, fmt.Errorf("manifest coordinates are %s@%s, expected %s@%s", m.Name, m.Version, expectedName, expectedVersion)
	}
	return m, nil
}
