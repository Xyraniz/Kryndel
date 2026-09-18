// Command kry-installer installs the Kryndel CLI release on Windows.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var version = "latest"

func main() {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "kry-installer: this installer targets Windows")
		os.Exit(1)
	}
	root, err := os.UserConfigDir()
	if err != nil {
		fail(err)
	}
	bin := filepath.Join(root, "Kryndel", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		fail(err)
	}
	asset := "kry-windows-amd64.exe"
	base := "https://github.com/Xyraniz/Kryndel/releases/" + version + "/download/"
	if version == "latest" {
		base = "https://github.com/Xyraniz/Kryndel/releases/latest/download/"
	}
	data, err := download(base + asset)
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "kry.exe"), data, 0o755); err != nil {
		fail(err)
	}
	path := os.Getenv("PATH")
	if !strings.Contains(strings.ToLower(path), strings.ToLower(bin)) {
		if err := runSetx(path + ";" + bin); err != nil {
			fail(err)
		}
	}
	fmt.Println("Kryndel installed in " + bin)
	fmt.Println("Open a new terminal and run: kry version")
}

func download(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 128<<20))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("downloaded empty executable")
	}
	return data, nil
}

func runSetx(value string) error {
	cmd := exec.Command("setx", "PATH", value)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "kry-installer:", err)
	os.Exit(1)
}
