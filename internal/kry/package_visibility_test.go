package kry

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writePackageFixture(t *testing.T, root, relative, source string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestManifestPackageVisibilityAndArtifactReplay(t *testing.T) {
	root := t.TempDir()
	writePackageFixture(t, root, "kry.toml", "[package]\nname = \"app\"\nversion = \"0.1.0\"\n")
	writePackageFixture(t, root, "vendor/discord/kry.toml", "[package]\nname = \"discord\"\nversion = \"0.1.0\"\n")
	writePackageFixture(t, root, "vendor/discord/models.kry", "pub struct Bot { private token: String }\nstruct Hidden { value: Int }\nenum Secret { Hidden }\n")
	writePackageFixture(t, root, "vendor/discord/validation.kry", "fn valid_token(value: String) -> Bool { return value != \"\" }\n")
	writePackageFixture(t, root, "vendor/discord/rest.kry", "import \"models\"\nimpl Bot { pub fn token_length() -> Int { return len(self.token) } }\n")
	writePackageFixture(t, root, "vendor/discord/main.kry", "import \"models\"\nimport \"validation\"\nimport \"rest\"\npub fn new_bot() -> Bot {\n    if valid_token(\"bot-token\") {\n        return Bot{token: \"bot-token\"}\n    }\n    return Bot{token: \"\"}\n}\n")
	mainPath := filepath.Join(root, "main.kry")
	writePackageFixture(t, root, "main.kry", "import \"vendor/discord\"\nlet bot: Bot = new_bot()\nprintln(bot.token_length())\n")
	program, d := LoadProgram(mainPath, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	if _, d = Check(program, DefaultLimits()); d != nil {
		t.Fatalf("same-package private validation or field access failed: %s", d.Message)
	}
	artifact, d := BuildArtifact(program, mainPath)
	if d != nil {
		t.Fatal(d.Message)
	}
	decoded, d := DecodeArtifact(artifact, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	replayed, d := ProgramFromArtifact(decoded, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	if _, d = Check(replayed, DefaultLimits()); d != nil {
		t.Fatalf("package visibility changed after artifact replay: %s", d.Message)
	}

	writePackageFixture(t, root, "main.kry", "import \"vendor/discord\"\nfn leak() -> Bool { return valid_token(\"token\") }\n")
	program, d = LoadProgram(mainPath, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	if _, d = Check(program, DefaultLimits()); d == nil || !strings.Contains(d.Message, "unknown function 'valid_token'") {
		t.Fatalf("private package helper escaped through its entrypoint: %#v", d)
	}

	writePackageFixture(t, root, "main.kry", "import \"vendor/discord\"\nfn leak() -> String {\n    let bot: Bot = new_bot()\n    return bot.token\n}\n")
	program, d = LoadProgram(mainPath, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	if _, d = Check(program, DefaultLimits()); d == nil || !strings.Contains(d.Message, "field 'token' is private") {
		t.Fatalf("private bot token field escaped its package: %#v", d)
	}

	for label, source := range map[string]string{
		"private struct type": "import \"vendor/discord\"\nfn leak() -> Hidden { return Hidden{value: 1} }\n",
		"private enum type":   "import \"vendor/discord\"\nfn leak() -> Secret { return Secret::Hidden }\n",
	} {
		t.Run(label, func(t *testing.T) {
			writePackageFixture(t, root, "main.kry", source)
			program, d := LoadProgram(mainPath, DefaultLimits(), "")
			if d != nil {
				t.Fatal(d.Message)
			}
			if _, d = Check(program, DefaultLimits()); d == nil || !strings.Contains(d.Message, "private") {
				t.Fatalf("private imported type was visible: %#v", d)
			}
		})
	}
}

func TestArtifactV3AndV4RemainReadable(t *testing.T) {
	for _, version := range []uint32{3, 4} {
		name := "v3"
		if version == 4 {
			name = "v4"
		}
		t.Run(name, func(t *testing.T) {
			artifact := makeLegacyArtifact(version, []byte("println(7)\n"))
			decoded, d := DecodeArtifact(artifact, DefaultLimits())
			if d != nil {
				t.Fatal(d.Message)
			}
			program, d := ProgramFromArtifact(decoded, DefaultLimits())
			if d != nil {
				t.Fatal(d.Message)
			}
			if _, d := Check(program, DefaultLimits()); d != nil {
				t.Fatalf("KRYNATIVE%d replay failed: %s", version, d.Message)
			}
		})
	}
}

func makeLegacyArtifact(version uint32, source []byte) []byte {
	var b bytes.Buffer
	if version == 3 {
		b.WriteString(legacyArtifactMagic)
	} else {
		b.WriteString(previousArtifactMagic)
	}
	writeU32(&b, version)
	compiler := legacyCompilerID
	if version == 4 {
		compiler = previousCompilerIdentity
	}
	writeString(&b, compiler)
	if version >= 4 {
		writeString(&b, LanguageVersion)
	}
	b.Write([]byte{'K', 'R', 'Y'})
	writeString(&b, runtime.GOOS+"/"+runtime.GOARCH)
	writeU32(&b, 1)
	writeString(&b, "<root>")
	writeU64(&b, uint64(len(source)))
	hash := sha256.Sum256(source)
	b.Write(hash[:])
	b.Write(source)
	return b.Bytes()
}
