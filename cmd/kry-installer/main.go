// Command kry-installer installs the Kryndel CLI release on Windows.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
	base := "https://github.com/Xyraniz/Kryndel/releases/latest/download/"
	
	fmt.Println("Downloading Kryndel from " + base + asset)
	data, err := download(base + asset)
	if err != nil {
		fail(err)
	}
	
	expectedSHA := os.Getenv("KRY_EXPECTED_SHA")
	if expectedSHA != "" {
		actualSHA := sha256.Sum256(data)
		if hex.EncodeToString(actualSHA[:]) != expectedSHA {
			fail(fmt.Errorf("SHA256 mismatch: expected %s, got %s", expectedSHA, hex.EncodeToString(actualSHA[:])))
		}
		fmt.Println("SHA256 verified successfully")
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
	
	fmt.Println("")
	fmt.Println("╔════════════════════════════════════════════════════════╗")
	fmt.Println("║         Kryndel installed successfully!                ║")
	fmt.Println("╚════════════════════════════════════════════════════════╝")
	fmt.Println("")
	fmt.Println("Installation directory: " + bin)
	fmt.Println("")
	fmt.Println("Next steps:")
	fmt.Println("  1. Open a new terminal window")
	fmt.Println("  2. Run: kry version")
	fmt.Println("  3. Create your first project: kry new mybot")
	fmt.Println("  4. Install packages: kry install discord")
	fmt.Println("")
	fmt.Println("Quick start - Create a Discord bot:")
	fmt.Println("  kry new mydiscordbot")
	fmt.Println("  cd mydiscordbot")
	fmt.Println("  kry add discord")
	fmt.Println("  kry install")
	fmt.Println("  [Edit main.kry with your bot code]")
	fmt.Println("  kry run main.kry")
	fmt.Println("")
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

// Helper for extracting zip archives (for future use)
func extractZip(data []byte, dest string) error {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, file := range reader.File {
		path := filepath.Join(dest, file.Name)
		if file.FileInfo().IsDir() {
			os.MkdirAll(path, 0o755)
			continue
		}
		os.MkdirAll(filepath.Dir(path), 0o755)
		rc, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(path)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, io.LimitReader(rc, 64<<20))
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
