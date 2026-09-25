package kry

import (
	"bytes"
	"crypto/sha256"
	"runtime"
	"testing"
)

func TestManifestLanguageVersionDefaultsAndRoundTrips(t *testing.T) {
	legacy, err := ParseManifest("[package]\nname = \"demo\"\nversion = \"0.1.0\"\nkryndel = \">=1.3.0\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.LanguageVersion != LanguageVersion {
		t.Fatalf("legacy manifest defaulted to %q", legacy.LanguageVersion)
	}
	formatted := FormatManifest(legacy)
	if !bytes.Contains([]byte(formatted), []byte("language_version = \""+LanguageVersion+"\"")) {
		t.Fatalf("formatted manifest omits language version: %s", formatted)
	}
	roundTrip, err := ParseManifest(formatted)
	if err != nil || roundTrip.LanguageVersion != LanguageVersion {
		t.Fatalf("manifest round trip: version=%q err=%v", roundTrip.LanguageVersion, err)
	}
}

func TestManifestRejectsInvalidLanguageVersions(t *testing.T) {
	for _, version := range []string{"1", "01.0.0", "1.0.0-beta", "2.0.0", "1.0.1"} {
		manifest := "[package]\nname = \"demo\"\nversion = \"0.1.0\"\nkryndel = \">=1.3.0\"\nlanguage_version = \"" + version + "\"\n"
		if _, err := ParseManifest(manifest); err == nil {
			t.Errorf("accepted unsupported language version %q", version)
		}
	}
}

func TestArtifactV3CompatibilityAndV4Metadata(t *testing.T) {
	payload := []byte("println(\"legacy\")\n")
	hash := sha256.Sum256(payload)
	var legacy bytes.Buffer
	legacy.WriteString(legacyArtifactMagic)
	writeU32(&legacy, 3)
	writeString(&legacy, legacyCompilerID)
	legacy.Write([]byte("KRY"))
	writeString(&legacy, runtime.GOOS+"/"+runtime.GOARCH)
	writeU32(&legacy, 1)
	writeString(&legacy, "<root>")
	writeU64(&legacy, uint64(len(payload)))
	legacy.Write(hash[:])
	legacy.Write(payload)
	decoded, diagnostic := DecodeArtifact(legacy.Bytes(), DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if decoded.LanguageVersion != LanguageVersion || decoded.Compiler != legacyCompilerID {
		t.Fatalf("legacy artifact metadata mismatch: %#v", decoded)
	}

	program, _ := testProgram(t, "println(\"current\")\n")
	data, diagnostic := BuildArtifact(program, "main.kry")
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if !bytes.HasPrefix(data, []byte(artifactMagic)) {
		t.Fatalf("new artifact does not use %q", artifactMagic)
	}
	current, diagnostic := DecodeArtifact(data, DefaultLimits())
	if diagnostic != nil || current.LanguageVersion != LanguageVersion {
		t.Fatalf("new artifact metadata mismatch: %#v %v", current, diagnostic)
	}
}
