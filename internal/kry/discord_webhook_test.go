package kry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type discordWebhookRequest struct {
	method string
	path   string
	body   string
}

func writeDiscordTestFixture(t *testing.T, source string) string {
	t.Helper()
	fixtureDir := filepath.Join("..", "..", "examples")
	fixture, err := os.CreateTemp(fixtureDir, ".discord-test-*.kry")
	if err != nil {
		t.Fatal(err)
	}
	fixturePath := fixture.Name()
	t.Cleanup(func() { _ = os.Remove(fixturePath) })
	if _, err := fixture.WriteString(source); err != nil {
		_ = fixture.Close()
		t.Fatal(err)
	}
	if err := fixture.Close(); err != nil {
		t.Fatal(err)
	}
	return fixturePath
}

func packageFileContents(root string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "kry.lock" || filepath.Base(path) == ".DS_Store" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	return files, err
}

func TestRegistryPackageArchivesMatchSources(t *testing.T) {
	indexDir := filepath.Join("..", "..", "registry", "index")
	entries, err := os.ReadDir(indexDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		t.Run(name, func(t *testing.T) {
			var index struct {
				Name     string `json:"name"`
				Versions []struct {
					Version string `json:"version"`
					SHA256  string `json:"sha256"`
				} `json:"versions"`
			}
			indexData, err := os.ReadFile(filepath.Join(indexDir, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(indexData, &index); err != nil {
				t.Fatal(err)
			}
			sourceDir := filepath.Join("..", "..", "packages", name)
			manifest, err := ReadManifest(sourceDir)
			if err != nil {
				t.Fatal(err)
			}
			if index.Name != name {
				t.Fatalf("registry index name = %q, want %q", index.Name, name)
			}
			var wantHash string
			for _, version := range index.Versions {
				if version.Version == manifest.Version {
					wantHash = version.SHA256
					break
				}
			}
			if wantHash == "" {
				t.Fatalf("registry index has no %s@%s release", name, manifest.Version)
			}
			archivePath := filepath.Join("..", "..", "registry", "packages", name+"-"+manifest.Version+".tar.gz")
			archive, err := os.ReadFile(archivePath)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(archive)
			if got := hex.EncodeToString(digest[:]); got != wantHash {
				t.Fatalf("archive SHA-256 = %s, registry index says %s", got, wantHash)
			}
			publishedDir := t.TempDir()
			if err := extractPackage(archive, publishedDir); err != nil {
				t.Fatalf("extract published package: %v", err)
			}
			sourceFiles, err := packageFileContents(sourceDir)
			if err != nil {
				t.Fatal(err)
			}
			publishedFiles, err := packageFileContents(publishedDir)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(sourceFiles, publishedFiles) {
				t.Fatal("published package files differ from the checked-in source")
			}
			rebuiltPath := filepath.Join(t.TempDir(), name+".tar.gz")
			if err := PackageArchive(sourceDir, rebuiltPath); err != nil {
				t.Fatal(err)
			}
			rebuiltData, err := os.ReadFile(rebuiltPath)
			if err != nil {
				t.Fatal(err)
			}
			rebuiltDir := t.TempDir()
			if err := extractPackage(rebuiltData, rebuiltDir); err != nil {
				t.Fatalf("extract rebuilt package: %v", err)
			}
			rebuiltFiles, err := packageFileContents(rebuiltDir)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(sourceFiles, rebuiltFiles) {
				t.Fatal("rebuilt package files differ from the checked-in source")
			}
		})
	}
}

func loadDiscordTestProgram(t *testing.T, source string) (*Program, *Checker) {
	t.Helper()
	fixturePath := writeDiscordTestFixture(t, source)
	program, diagnostic := LoadProgram(fixturePath, DefaultLimits(), "")
	if diagnostic != nil {
		t.Fatalf("load Discord test program: %s", diagnostic.Message)
	}
	checker, diagnostic := Check(program, DefaultLimits())
	if diagnostic != nil {
		t.Fatalf("type-check Discord test program at %d:%d: %s", diagnostic.Line, diagnostic.Column, diagnostic.Message)
	}
	return program, checker
}

func TestDiscordPackageArchiveMatchesRegistryIndex(t *testing.T) {
	const version = "2.2.0"
	archivePath := filepath.Join("..", "..", "registry", "packages", "discord-"+version+".tar.gz")
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join("..", "..", "registry", "index", "discord.json")
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	var index struct {
		Name     string `json:"name"`
		Versions []struct {
			Version string `json:"version"`
			SHA256  string `json:"sha256"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(indexData, &index); err != nil {
		t.Fatal(err)
	}
	wantHash := ""
	for _, candidate := range index.Versions {
		if candidate.Version == version {
			wantHash = candidate.SHA256
			break
		}
	}
	if index.Name != "discord" || wantHash == "" {
		t.Fatalf("Discord registry index is missing release %s", version)
	}
	digest := sha256.Sum256(archive)
	if got := hex.EncodeToString(digest[:]); got != wantHash {
		t.Fatalf("Discord archive SHA-256 = %s, registry index says %s", got, wantHash)
	}

	extracted := t.TempDir()
	if err := extractPackage(archive, extracted); err != nil {
		t.Fatalf("extract published Discord package: %v", err)
	}
	manifest, err := ReadManifest(extracted)
	if err != nil || manifest.Name != "discord" || manifest.Version != version {
		t.Fatalf("published Discord package manifest = %#v, %v", manifest, err)
	}
	for _, module := range []string{"models.kry", "validation.kry", "rest.kry", "interactions.kry", "application_commands.kry", "gateway.kry", "cache.kry"} {
		if _, err := os.Stat(filepath.Join(extracted, module)); err != nil {
			t.Fatalf("published Discord package is missing %s: %v", module, err)
		}
	}

	project := t.TempDir()
	vendorPackage := filepath.Join(project, "vendor", "discord")
	if err := os.MkdirAll(vendorPackage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractPackage(archive, vendorPackage); err != nil {
		t.Fatalf("install published Discord package into project fixture: %v", err)
	}
	root := filepath.Join(project, "main.kry")
	if err := os.WriteFile(root, []byte("import \"discord\"\nfn main() -> Result[Nil, String] {\n    let hook: Webhook = webhook_from_url(\"https://discord.com/api/webhooks/123/token\")?\n    let filenames: Array[String] = [\"fixture.txt\"]\n    let files: Array[Bytes] = [bytes_from_u8([u8(65)])]\n    let uploaded: String = hook.upload_files_json(\"{\\\"attachments\\\":[{\\\"id\\\":0,\\\"filename\\\":\\\"fixture.txt\\\"}]}\", filenames, files, \"\")?\n    let edited: Json = hook.edit_message_files_json(\"456\", \"{\\\"attachments\\\":[{\\\"id\\\":0,\\\"filename\\\":\\\"fixture.txt\\\"}] }\", filenames, files, \"\")?\n    return ok(nil)\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	program, diagnostic := LoadProgram(root, DefaultLimits(), "")
	if diagnostic != nil {
		t.Fatalf("load Discord Webhook through published package: %s", diagnostic.Message)
	}
	if _, diagnostic := Check(program, DefaultLimits()); diagnostic != nil {
		t.Fatalf("type-check Discord Webhook through published package: %s", diagnostic.Message)
	}

}

func TestDiscordCredentialsArePrivateToThePackage(t *testing.T) {
	fixture := writeDiscordTestFixture(t, `import "packages/discord"
fn main() -> Result[Nil, String] {
    let client: Bot = bot("synthetic-token", [])?
    println(client.token)
    return ok(nil)
}
`)
	program, diagnostic := LoadProgram(fixture, DefaultLimits(), "")
	if diagnostic != nil {
		t.Fatalf("load Discord credential visibility fixture: %s", diagnostic.Message)
	}
	_, diagnostic = Check(program, DefaultLimits())
	if diagnostic == nil || !strings.Contains(diagnostic.Message, "private") {
		t.Fatalf("reading Bot.token outside its package produced diagnostic %#v, want private-field rejection", diagnostic)
	}
}

func TestDiscordWebhookMethodsUseUnauthenticatedRoutesAndSafeTextDefaults(t *testing.T) {
	var seen []discordWebhookRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read Discord webhook request body: %v", err)
		}
		if request.Header.Get("Authorization") != "" {
			t.Errorf("webhook token request unexpectedly used bot Authorization: %q", request.Header.Get("Authorization"))
		}
		seen = append(seen, discordWebhookRequest{method: request.Method, path: request.URL.RequestURI(), body: string(body)})

		switch {
		case request.Method == http.MethodPost && request.URL.Query().Get("wait") == "true":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"789","content":"hello <@456>"}`)
		case request.Method == http.MethodPost && request.URL.Query().Get("wait") == "false":
			w.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/api/v10/webhooks/124/"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"rejected synthetic-webhook-token-with-more-than-thirty-two-characters"}`)
		case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/api/v10/webhooks/125/"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"short-token"}`)
		case request.Method == http.MethodGet && strings.Contains(request.URL.Path, "/messages/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"789","content":"hello"}`)
		case request.Method == http.MethodPatch && strings.Contains(request.URL.Path, "/messages/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"789","content":"edited"}`)
		case request.Method == http.MethodGet || request.Method == http.MethodPatch:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"123","name":"test"}`)
		case request.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected Discord webhook request: %s %s", request.Method, request.URL.RequestURI())
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	source := `import "packages/discord"
fn main() -> Result[Nil, String] {
    let invalid_webhook: Result[Webhook, String] = webhook("not-a-snowflake", "token")
    assert_eq(is_err(invalid_webhook), true)
    let parsed_url: Webhook = webhook_from_url("https://discord.com/api/webhooks/123/token")?
    assert_eq(is_err(webhook_from_url("http://discord.com/api/webhooks/123/token")), true)
    assert_eq(is_err(webhook_from_url("https://evil.example/api/webhooks/123/token")), true)
    assert_eq(is_err(webhook_from_url("https://discord.com:443/api/webhooks/123/token")), true)
    assert_eq(is_err(webhook_from_url("https://discord.com/api/webhooks/123/token/extra")), true)
    assert_eq(is_err(webhook_from_url("https://discord.com/api/webhooks/123/token?wait=true")), true)
    let parsed_response: Json = parsed_url.fetch()?
    let hook: Webhook = webhook("123", "token")?
    let invalid_thread: Result[String, String] = hook.execute_json("{}", true, "bad")
    assert_eq(is_err(invalid_thread), true)
    let invalid_message: Result[Json, String] = hook.fetch_message("bad", "")
    assert_eq(is_err(invalid_message), true)

    let sent: Json = hook.send("hello <@456>")?
    let sent_id: String = json_string(result_unwrap(json_object_get(sent, "id")))?
    assert_eq(sent_id, "789")
    let executed: String = hook.execute_json("{\"embeds\":[]}", true, "456")?
    assert_eq(executed, "{\"id\":\"789\",\"content\":\"hello <@456>\"}")
    let fire_and_forget: String = hook.execute_json("{\"content\":\"no wait\"}", false, "")?
    assert_eq(fire_and_forget, "")
    let fetched: Json = hook.fetch()?
    let edited: Json = hook.edit_json("{\"name\":\"renamed\"}")?
    let message: Json = hook.fetch_message("789", "456")?
    let changed: Json = hook.edit_message_json("789", "{\"content\":\"edited\"}", "456")?
    hook.delete_message("789", "456")?
    hook.delete()?
    let private_hook: Webhook = webhook("124", "synthetic-webhook-token-with-more-than-thirty-two-characters")?
    let private_error: Result[Json, String] = private_hook.fetch()
    assert_eq(is_err(private_error), true)
    let error_text: String = unwrap_or(result_error(private_error), "")
    assert_eq(contains(error_text, "Discord API HTTP 404"), true)
    assert_eq(contains(error_text, "synthetic-webhook-token-with-more-than-thirty-two-characters"), false)
    let short_hook: Webhook = webhook("125", "short-token")?
    let short_error: Result[Json, String] = short_hook.fetch()
    assert_eq(is_err(short_error), true)
    let short_error_text: String = unwrap_or(result_error(short_error), "")
    assert_eq(contains(short_error_text, "short-token"), false)
    return ok(nil)
}

`
	p, checker := loadDiscordTestProgram(t, source)
	runtime, diagnostic := NewRuntime(p, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	runtime.discordAPIBaseURL = server.URL + "/api/v10"
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatalf("run Discord webhook fixture: %s", diagnostic.Message)
	}

	want := []discordWebhookRequest{
		{method: "GET", path: "/api/v10/webhooks/123/token", body: ""},
		{method: "POST", path: "/api/v10/webhooks/123/token?wait=true", body: `{"content":"hello <@456>","allowed_mentions":{"parse":[]}}`},
		{method: "POST", path: "/api/v10/webhooks/123/token?wait=true&thread_id=456", body: `{"embeds":[]}`},
		{method: "POST", path: "/api/v10/webhooks/123/token?wait=false", body: `{"content":"no wait"}`},
		{method: "GET", path: "/api/v10/webhooks/123/token", body: ""},
		{method: "PATCH", path: "/api/v10/webhooks/123/token", body: `{"name":"renamed"}`},
		{method: "GET", path: "/api/v10/webhooks/123/token/messages/789?thread_id=456", body: ""},
		{method: "PATCH", path: "/api/v10/webhooks/123/token/messages/789?thread_id=456", body: `{"content":"edited"}`},
		{method: "DELETE", path: "/api/v10/webhooks/123/token/messages/789?thread_id=456", body: ""},
		{method: "DELETE", path: "/api/v10/webhooks/123/token", body: ""},
		{method: "GET", path: "/api/v10/webhooks/124/synthetic-webhook-token-with-more-than-thirty-two-characters", body: ""},
		{method: "GET", path: "/api/v10/webhooks/125/short-token", body: ""},
	}
	if !reflect.DeepEqual(seen, want) {
		got, _ := json.MarshalIndent(seen, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("Discord webhook requests differ\ngot:\n%s\nwant:\n%s", got, wantJSON)
	}
}
func TestDiscordWebhookMultipartUploadAndBotUploadAuth(t *testing.T) {
	type uploadRequest struct {
		method        string
		path          string
		authorization string
		contentType   string
		payload       string
		filenames     map[string]string
		files         map[string]string
	}
	var seen []uploadRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if err := request.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart request: %v", err)
			http.Error(w, "invalid multipart", http.StatusBadRequest)
			return
		}
		got := uploadRequest{
			method:        request.Method,
			path:          request.URL.RequestURI(),
			authorization: request.Header.Get("Authorization"),
			contentType:   request.Header.Get("Content-Type"),
			payload:       request.FormValue("payload_json"),
			filenames:     make(map[string]string),
			files:         make(map[string]string),
		}
		if request.MultipartForm != nil {
			defer request.MultipartForm.RemoveAll()
			for name, headers := range request.MultipartForm.File {
				if len(headers) != 1 {
					t.Errorf("multipart field %s has %d files, want 1", name, len(headers))
					continue
				}
				got.filenames[name] = headers[0].Filename
				file, err := headers[0].Open()
				if err != nil {
					t.Errorf("open multipart field %s: %v", name, err)
					continue
				}
				data, err := io.ReadAll(file)
				_ = file.Close()
				if err != nil {
					t.Errorf("read multipart field %s: %v", name, err)
					continue
				}
				got.files[name] = string(data)
			}
		}
		seen = append(seen, got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"987"}`)
	}))
	defer server.Close()

	source := `import "packages/discord"
fn main() -> Result[Nil, String] {
    let hook: Webhook = webhook("123", "short-upload-token")?
    let filenames: Array[String] = ["alpha.txt", "beta.bin"]
    let files: Array[Bytes] = [bytes_from_u8([u8(65), u8(66)]), bytes_from_u8([u8(0), u8(255)])]
    assert_eq(is_err(hook.upload_files_json("{}", filenames, [files[0]], "")), true)
    assert_eq(is_err(hook.upload_files_json("{}", filenames, files, "bad-thread")), true)
    assert_eq(is_err(hook.upload_files_json("{}", ["../bad"], [files[0]], "")), true)
    assert_eq(is_err(hook.edit_message_files_json("bad", "{}", ["edit.txt"], [files[0]], "")), true)
    let uploaded: String = hook.upload_files_json("{\"content\":\"files\",\"attachments\":[{\"id\":0,\"filename\":\"alpha.txt\"},{\"id\":1,\"filename\":\"beta.bin\"}]}", filenames, files, "456")?
    assert_eq(uploaded, "{\"id\":\"987\"}")
    let safe_names: Array[String] = ["safe-report.txt"]
    let safe_files: Array[Bytes] = [bytes_from_u8([u8(81)])]
    let sent_files: Json = hook.send_files("report <@456>", safe_names, safe_files, "456")?
    let sent_files_id: String = json_string(result_unwrap(json_object_get(sent_files, "id")))?
    assert_eq(sent_files_id, "987")
    let edited_files: Json = hook.edit_message_files_json("789", "{\"attachments\":[{\"id\":0,\"filename\":\"edit.txt\"}]}", ["edit.txt"], [bytes_from_u8([u8(68)])], "456")?
    let edited_id: String = json_string(result_unwrap(json_object_get(edited_files, "id")))?
    assert_eq(edited_id, "987")
    let client: Bot = bot("bot-test-token", [])?
    let bot_result: String = client.upload("POST", "/channels/101/messages", "{\"content\":\"bot file\"}", "bot.txt", bytes_from_u8([u8(67)]))?
    assert_eq(bot_result, "{\"id\":\"987\"}")
    return ok(nil)
}
`
	p, checker := loadDiscordTestProgram(t, source)
	runtime, diagnostic := NewRuntime(p, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	runtime.discordAPIBaseURL = server.URL + "/api/v10"
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatalf("run Discord webhook upload fixture: %s", diagnostic.Message)
	}

	want := []uploadRequest{
		{
			method:        http.MethodPost,
			path:          "/api/v10/webhooks/123/short-upload-token?wait=true&thread_id=456",
			authorization: "",
			contentType:   "multipart/form-data; boundary=",
			payload:       `{"content":"files","attachments":[{"id":0,"filename":"alpha.txt"},{"id":1,"filename":"beta.bin"}]}`,
			filenames:     map[string]string{"files[0]": "alpha.txt", "files[1]": "beta.bin"},
			files:         map[string]string{"files[0]": "AB", "files[1]": string([]byte{0, 255})},
		},
		{
			method:        http.MethodPost,
			path:          "/api/v10/webhooks/123/short-upload-token?wait=true&thread_id=456",
			authorization: "",
			contentType:   "multipart/form-data; boundary=",
			payload:       `{"content":"report <@456>","attachments":[{"id":0,"filename":"safe-report.txt"}],"allowed_mentions":{"parse":[]}}`,
			filenames:     map[string]string{"files[0]": "safe-report.txt"},
			files:         map[string]string{"files[0]": "Q"},
		},
		{
			method:        http.MethodPatch,
			path:          "/api/v10/webhooks/123/short-upload-token/messages/789?thread_id=456",
			authorization: "",
			contentType:   "multipart/form-data; boundary=",
			payload:       `{"attachments":[{"id":0,"filename":"edit.txt"}]}`,
			filenames:     map[string]string{"files[0]": "edit.txt"},
			files:         map[string]string{"files[0]": "D"},
		},
		{
			method:        http.MethodPost,
			path:          "/api/v10/channels/101/messages",
			authorization: "Bot bot-test-token",
			contentType:   "multipart/form-data; boundary=",
			payload:       `{"content":"bot file"}`,
			filenames:     map[string]string{"files[0]": "bot.txt"},
			files:         map[string]string{"files[0]": "C"},
		},
	}
	if len(seen) != len(want) {
		t.Fatalf("received %d multipart requests, want %d: %#v", len(seen), len(want), seen)
	}
	for index := range want {
		if seen[index].method != want[index].method || seen[index].path != want[index].path || seen[index].authorization != want[index].authorization || !strings.HasPrefix(seen[index].contentType, want[index].contentType) || seen[index].payload != want[index].payload || !reflect.DeepEqual(seen[index].filenames, want[index].filenames) || !reflect.DeepEqual(seen[index].files, want[index].files) {
			got, _ := json.MarshalIndent(seen[index], "", "  ")
			wantJSON, _ := json.MarshalIndent(want[index], "", "  ")
			t.Fatalf("multipart request %d differs\ngot:\n%s\nwant:\n%s", index, got, wantJSON)
		}
	}
}

func TestDiscordMultipartUploadRejectsMoreThanTenFilesBeforeNetwork(t *testing.T) {
	files := make([]discordUploadFile, discordMaxUploadFiles+1)
	for _, upload := range []func() (Value, *Diagnostic){
		func() (Value, *Diagnostic) {
			return (&Runtime{Lim: DefaultLimits()}).discordWebhookUpload("POST", "/webhooks/123/token?wait=true", "{}", files, "token")
		},
		func() (Value, *Diagnostic) {
			return (&Runtime{Lim: DefaultLimits()}).discordAPIUploadFiles("POST", "/channels/123/messages", "{}", files, "token")
		},
	} {
		value, diagnostic := upload()
		if diagnostic != nil {
			t.Fatalf("unexpected diagnostic: %v", diagnostic)
		}
		if value.Kind != VResult || value.OK {
			t.Fatalf("upload result = %#v, want an error Result", value)
		}
	}
}

func TestDiscordBotUploadFilesAndSendFiles(t *testing.T) {
	type uploadRequest struct {
		method        string
		path          string
		authorization string
		contentType   string
		payload       string
		filenames     map[string]string
		files         map[string]string
	}
	var seen []uploadRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if err := request.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart request: %v", err)
			http.Error(w, "invalid multipart", http.StatusBadRequest)
			return
		}
		got := uploadRequest{
			method:        request.Method,
			path:          request.URL.RequestURI(),
			authorization: request.Header.Get("Authorization"),
			contentType:   request.Header.Get("Content-Type"),
			payload:       request.FormValue("payload_json"),
			filenames:     make(map[string]string),
			files:         make(map[string]string),
		}
		if request.MultipartForm != nil {
			defer request.MultipartForm.RemoveAll()
			for name, headers := range request.MultipartForm.File {
				if len(headers) != 1 {
					t.Errorf("multipart field %s has %d files, want 1", name, len(headers))
					continue
				}
				got.filenames[name] = headers[0].Filename
				file, err := headers[0].Open()
				if err != nil {
					t.Errorf("open multipart field %s: %v", name, err)
					continue
				}
				data, err := io.ReadAll(file)
				_ = file.Close()
				if err != nil {
					t.Errorf("read multipart field %s: %v", name, err)
					continue
				}
				got.files[name] = string(data)
			}
		}
		seen = append(seen, got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"987"}`)
	}))
	defer server.Close()

	source := `import "packages/discord"
fn main() -> Result[Nil, String] {
    let client: Bot = bot("bot-multi-token", [])?
    let names: Array[String] = ["file0.bin", "file1.bin", "file2.bin", "file3.bin", "file4.bin", "file5.bin", "file6.bin", "file7.bin", "file8.bin", "file9.bin"]
    let files: Array[Bytes] = [bytes_from_u8([u8(65)]), bytes_from_u8([u8(66)]), bytes_from_u8([u8(67)]), bytes_from_u8([u8(68)]), bytes_from_u8([u8(69)]), bytes_from_u8([u8(70)]), bytes_from_u8([u8(71)]), bytes_from_u8([u8(72)]), bytes_from_u8([u8(73)]), bytes_from_u8([u8(74)])]
    let raw: String = client.upload_files("PATCH", "/channels/101/messages/202", "{\"attachments\":[{\"id\":0,\"filename\":\"file0.bin\"},{\"id\":1,\"filename\":\"file1.bin\"},{\"id\":2,\"filename\":\"file2.bin\"},{\"id\":3,\"filename\":\"file3.bin\"},{\"id\":4,\"filename\":\"file4.bin\"},{\"id\":5,\"filename\":\"file5.bin\"},{\"id\":6,\"filename\":\"file6.bin\"},{\"id\":7,\"filename\":\"file7.bin\"},{\"id\":8,\"filename\":\"file8.bin\"},{\"id\":9,\"filename\":\"file9.bin\"}]}", names, files)?
    assert_eq(raw, "{\"id\":\"987\"}")
    let sent: Json = client.send_files("101", "hello <@123456>", ["safe-report.txt", "safe-image.bin"], [bytes_from_u8([u8(0), u8(255)]), bytes_from_u8([u8(65)])])?
    let sent_id: String = json_string(result_unwrap(json_object_get(sent, "id")))?
    assert_eq(sent_id, "987")
    assert_eq(is_err(client.upload_files("POST", "/channels/101/messages", "{}", ["a.txt", "b.txt"], [files[0]])), true)
    assert_eq(is_err(client.upload_files("POST", "/channels/101/messages", "{}", ["../bad"], [files[0]])), true)
    assert_eq(is_err(client.send_files("bad", "", ["a.txt"], [files[0]])), true)
    let too_many_names: Array[String] = ["0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10"]
    let too_many_files: Array[Bytes] = [files[0], files[1], files[2], files[3], files[4], files[5], files[6], files[7], files[8], files[9], files[0]]
    assert_eq(is_err(client.upload_files("POST", "/channels/101/messages", "{}", too_many_names, too_many_files)), true)
    return ok(nil)
}
`
	p, checker := loadDiscordTestProgram(t, source)
	runtime, diagnostic := NewRuntime(p, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	runtime.discordAPIBaseURL = server.URL + "/api/v10"
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatalf("run Discord Bot multi-file upload fixture: %s", diagnostic.Message)
	}

	want := []uploadRequest{
		{
			method:        http.MethodPatch,
			path:          "/api/v10/channels/101/messages/202",
			authorization: "Bot bot-multi-token",
			contentType:   "multipart/form-data; boundary=",
			payload:       `{"attachments":[{"id":0,"filename":"file0.bin"},{"id":1,"filename":"file1.bin"},{"id":2,"filename":"file2.bin"},{"id":3,"filename":"file3.bin"},{"id":4,"filename":"file4.bin"},{"id":5,"filename":"file5.bin"},{"id":6,"filename":"file6.bin"},{"id":7,"filename":"file7.bin"},{"id":8,"filename":"file8.bin"},{"id":9,"filename":"file9.bin"}]}`,
			filenames: map[string]string{
				"files[0]": "file0.bin", "files[1]": "file1.bin", "files[2]": "file2.bin", "files[3]": "file3.bin", "files[4]": "file4.bin",
				"files[5]": "file5.bin", "files[6]": "file6.bin", "files[7]": "file7.bin", "files[8]": "file8.bin", "files[9]": "file9.bin",
			},
			files: map[string]string{
				"files[0]": "A", "files[1]": "B", "files[2]": "C", "files[3]": "D", "files[4]": "E",
				"files[5]": "F", "files[6]": "G", "files[7]": "H", "files[8]": "I", "files[9]": "J",
			},
		},
		{
			method:        http.MethodPost,
			path:          "/api/v10/channels/101/messages",
			authorization: "Bot bot-multi-token",
			contentType:   "multipart/form-data; boundary=",
			payload:       `{"content":"hello <@123456>","attachments":[{"id":0,"filename":"safe-report.txt"},{"id":1,"filename":"safe-image.bin"}],"allowed_mentions":{"parse":[]}}`,
			filenames:     map[string]string{"files[0]": "safe-report.txt", "files[1]": "safe-image.bin"},
			files:         map[string]string{"files[0]": string([]byte{0, 255}), "files[1]": "A"},
		},
	}
	if len(seen) != len(want) {
		t.Fatalf("received %d multipart requests, want %d: %#v", len(seen), len(want), seen)
	}
	for index := range want {
		if seen[index].method != want[index].method || seen[index].path != want[index].path || seen[index].authorization != want[index].authorization || !strings.HasPrefix(seen[index].contentType, want[index].contentType) || seen[index].payload != want[index].payload || !reflect.DeepEqual(seen[index].filenames, want[index].filenames) || !reflect.DeepEqual(seen[index].files, want[index].files) {
			got, _ := json.MarshalIndent(seen[index], "", "  ")
			wantJSON, _ := json.MarshalIndent(want[index], "", "  ")
			t.Fatalf("multipart request %d differs\ngot:\n%s\nwant:\n%s", index, got, wantJSON)
		}
	}
}

func TestDiscordBotCanCreateAndManageWebhooks(t *testing.T) {
	type request struct {
		method        string
		path          string
		body          string
		authorization string
	}
	var seen []request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("read bot webhook request body: %v", err)
		}
		seen = append(seen, request{method: req.Method, path: req.URL.RequestURI(), body: string(body), authorization: req.Header.Get("Authorization")})
		w.Header().Set("Content-Type", "application/json")
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/api/v10/channels/101/webhooks":
			_, _ = io.WriteString(w, `{"id":"202","token":"created-webhook-token","type":1}`)
		case req.Method == http.MethodGet && (req.URL.Path == "/api/v10/channels/101/webhooks" || req.URL.Path == "/api/v10/guilds/201/webhooks"):
			_, _ = io.WriteString(w, `[{"id":"202","type":1}]`)
		case req.Method == http.MethodGet && req.URL.Path == "/api/v10/webhooks/202":
			_, _ = io.WriteString(w, `{"id":"202","name":"Build notifications"}`)
		case req.Method == http.MethodPatch && req.URL.Path == "/api/v10/webhooks/202":
			_, _ = io.WriteString(w, `{"id":"202","name":"renamed"}`)
		case req.Method == http.MethodDelete && req.URL.Path == "/api/v10/webhooks/202":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected bot webhook request: %s %s", req.Method, req.URL.RequestURI())
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	source := `import "packages/discord"
fn main() -> Result[Nil, String] {
    let client: Bot = bot("bot-test-token", [])?
    assert_eq(is_err(client.create_webhook("bad", "Build notifications")), true)
    assert_eq(is_err(client.create_webhook("101", "")), true)
    let missing_token: Result[Webhook, String] = webhook_from_json(json_parse("{\"id\":\"202\"}")?)
    assert_eq(is_err(missing_token), true)
    let created: Webhook = client.create_webhook("101", "Build \\\"notifications\\\"")?
    let channel_webhooks: Json = client.list_channel_webhooks("101")?
    let guild_webhooks: Json = client.list_guild_webhooks("201")?
    let existing: Json = client.fetch_webhook("202")?
    let renamed: Json = client.edit_webhook_json("202", "{\"name\":\"renamed\"}")?
    client.delete_webhook("202")?
    return ok(nil)
}
`
	p, checker := loadDiscordTestProgram(t, source)
	runtime, diagnostic := NewRuntime(p, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	runtime.discordAPIBaseURL = server.URL + "/api/v10"
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatalf("run Discord webhook management fixture: %s", diagnostic.Message)
	}

	want := []request{
		{method: "POST", path: "/api/v10/channels/101/webhooks", body: `{"name":"Build \\\"notifications\\\""}`, authorization: "Bot bot-test-token"},
		{method: "GET", path: "/api/v10/channels/101/webhooks", authorization: "Bot bot-test-token"},
		{method: "GET", path: "/api/v10/guilds/201/webhooks", authorization: "Bot bot-test-token"},
		{method: "GET", path: "/api/v10/webhooks/202", authorization: "Bot bot-test-token"},
		{method: "PATCH", path: "/api/v10/webhooks/202", body: `{"name":"renamed"}`, authorization: "Bot bot-test-token"},
		{method: "DELETE", path: "/api/v10/webhooks/202", authorization: "Bot bot-test-token"},
	}
	if !reflect.DeepEqual(seen, want) {
		got, _ := json.MarshalIndent(seen, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("Discord bot webhook requests differ\ngot:\n%s\nwant:\n%s", got, wantJSON)
	}
}
