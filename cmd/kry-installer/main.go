// Command kry-installer installs the Kryndel CLI release on Windows.
package main

import (
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
	"time"
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

	executable := filepath.Join(bin, "kry.exe")
	if err := os.WriteFile(executable, data, 0o755); err != nil {
		fail(err)
	}
	if err := registerFileAssociations(executable); err != nil {
		fail(err)
	}

	path := os.Getenv("PATH")
	if !pathContains(path, bin) {
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
	fmt.Println("File associations: .kry and .kexe open with Kryndel")
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
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(url)
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

func pathContains(path, entry string) bool {
	for _, item := range strings.Split(path, ";") {
		if strings.EqualFold(strings.TrimSpace(item), entry) {
			return true
		}
	}
	return false
}

// registerFileAssociations uses HKCU so installation never requires elevation
// and never changes another user's file associations.
func registerFileAssociations(executable string) error {
	const classes = `HKCU\Software\Classes`
	openCommand := fmt.Sprintf(`"%s" "%%1"`, executable)
	entries := [][2]string{
		{classes + `\.kry`, "Kryndel.Source"},
		{classes + `\.kexe`, "Kryndel.Artifact"},
		{classes + `\Kryndel.Source`, "Kryndel source file"},
		{classes + `\Kryndel.Source\DefaultIcon`, executable + ",0"},
		{classes + `\Kryndel.Source\shell\open\command`, openCommand},
		{classes + `\Kryndel.Artifact`, "Kryndel native artifact"},
		{classes + `\Kryndel.Artifact\DefaultIcon`, executable + ",0"},
		{classes + `\Kryndel.Artifact\shell\open\command`, openCommand},
	}
	for _, entry := range entries {
		cmd := exec.Command("reg.exe", "ADD", entry[0], "/ve", "/d", entry[1], "/f")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("register %s: %v: %s", entry[0], err, strings.TrimSpace(string(output)))
		}
	}
	return nil
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
