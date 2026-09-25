package kry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscordRegistryContainsOnlyCurrentRelease(t *testing.T) {
	const current = "2.3.0"
	indexPath := filepath.Join("..", "..", "registry", "index", "discord.json")
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	var index struct {
		Name     string `json:"name"`
		Versions []struct {
			Version string `json:"version"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(indexData, &index); err != nil {
		t.Fatal(err)
	}
	if index.Name != "discord" || len(index.Versions) != 1 || index.Versions[0].Version != current {
		t.Fatalf("Discord registry index must contain only %s; got %#v", current, index)
	}

	packageDir := filepath.Join("..", "..", "registry", "packages")
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "discord-") || strings.HasPrefix(entry.Name(), "discord-self-") {
			continue
		}
		if entry.Name() != "discord-"+current+".tar.gz" {
			t.Errorf("stale Discord registry archive remains: %s", entry.Name())
		}
	}
}
