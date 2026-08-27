package kry

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Sandbox struct {
	Root       string
	Restricted bool
}

func (s Sandbox) Resolve(name string, write bool) (string, error) {
	if strings.IndexByte(name, 0) >= 0 {
		return "", errors.New("path contains NUL")
	}
	if s.Restricted {
		if filepath.IsAbs(name) || hasParent(name) {
			return "", errors.New("path denied by sandbox")
		}
		p := filepath.Join(s.Root, name)
		if !within(s.Root, p) || !safeComponents(s.Root, p) {
			return "", errors.New("path denied by sandbox")
		}
		if write {
			parent := filepath.Dir(p)
			if !safeComponents(s.Root, parent) {
				return "", errors.New("path denied by sandbox")
			}
			if fi, e := os.Lstat(p); e == nil && fi.Mode()&os.ModeSymlink != 0 {
				return "", errors.New("path denied by sandbox")
			}
		}
		return p, nil
	}
	if filepath.IsAbs(name) {
		return filepath.Clean(name), nil
	}
	p, _ := filepath.Abs(name)
	return filepath.Clean(p), nil
}
func (s Sandbox) Read(name string) ([]byte, error) {
	p, e := s.Resolve(name, false)
	if e != nil {
		return nil, e
	}
	if s.Restricted && !safeComponents(s.Root, p) {
		return nil, errors.New("path denied by sandbox")
	}
	return os.ReadFile(p)
}
func (s Sandbox) Exists(name string) (bool, error) {
	p, e := s.Resolve(name, false)
	if e != nil {
		return false, e
	}
	_, e = os.Lstat(p)
	if e == nil {
		return true, nil
	}
	if os.IsNotExist(e) {
		return false, nil
	}
	return false, e
}
func (s Sandbox) Write(name string, data []byte) error {
	p, e := s.Resolve(name, true)
	if e != nil {
		return e
	}
	parent := filepath.Dir(p)
	if s.Restricted && !safeComponents(s.Root, parent) {
		return errors.New("path denied by sandbox")
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	if s.Restricted && !safeComponents(s.Root, parent) {
		return errors.New("path denied by sandbox")
	}
	tmp, err := os.CreateTemp(parent, ".kryndel-write-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		tmp.Close()
		if !ok {
			os.Remove(tmpName)
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if s.Restricted && !safeComponents(s.Root, parent) {
		return errors.New("path denied by sandbox")
	}
	if fi, err := os.Lstat(p); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("path denied by sandbox")
	}
	if err = os.Rename(tmpName, p); err != nil {
		return err
	}
	ok = true
	return nil
}
func ReadAllLimit(r io.Reader, max int) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, int64(max)+1))
}
