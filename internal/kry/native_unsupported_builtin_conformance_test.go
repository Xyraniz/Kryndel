package kry

import (
	"fmt"
	"testing"
)

func TestDirectBackendsRejectUnsupportedJSONSQLiteAndTCPClientBuiltins(t *testing.T) {
	fixtures := []struct {
		name   string
		source string
	}{
		{name: "json_parse", source: `fn subject(value: String) -> Result[Json, String] { return json_parse(value) }`},
		{name: "json_stringify", source: `fn subject(value: Json) -> String { return json_stringify(value) }`},
		{name: "json_kind", source: `fn subject(value: Json) -> String { return json_kind(value) }`},
		{name: "json_is_null", source: `fn subject(value: Json) -> Bool { return json_is_null(value) }`},
		{name: "json_object_get", source: `fn subject(value: Json) -> Result[Json, String] { return json_object_get(value, "key") }`},
		{name: "json_array_len", source: `fn subject(value: Json) -> Result[Int, String] { return json_array_len(value) }`},
		{name: "json_array_get", source: `fn subject(value: Json) -> Result[Json, String] { return json_array_get(value, 0) }`},
		{name: "json_string", source: `fn subject(value: Json) -> Result[String, String] { return json_string(value) }`},
		{name: "json_int", source: `fn subject(value: Json) -> Result[Int, String] { return json_int(value) }`},
		{name: "json_uint", source: `fn subject(value: Json) -> Result[UInt64, String] { return json_uint(value) }`},
		{name: "json_float", source: `fn subject(value: Json) -> Result[Float, String] { return json_float(value) }`},
		{name: "json_bool", source: `fn subject(value: Json) -> Result[Bool, String] { return json_bool(value) }`},
		{name: "sqlite_open", source: `fn subject() -> Result[SQLite, String] { return sqlite_open(":memory:") }`},
		{name: "sqlite_exec", source: `fn subject(db: SQLite) -> Result[Int, String] { return sqlite_exec(db, "SELECT 1") }`},
		{name: "sqlite_query", source: `fn subject(db: SQLite) -> Result[Array[Array[String]], String] { return sqlite_query(db, "SELECT 1") }`},
		{name: "sqlite_close", source: `fn subject(db: SQLite) -> Nil { sqlite_close(db); return nil }`},
		{name: "tcp_connect", source: `fn subject() -> Result[TcpSocket, String] { return tcp_connect("127.0.0.1", 1) }`},
		{name: "tcp_send", source: `fn subject(socket: TcpSocket, data: Bytes) -> Result[Int, String] { return tcp_send(socket, data) }`},
		{name: "tcp_receive", source: `fn subject(socket: TcpSocket, size: Int) -> Result[Bytes, String] { return tcp_receive(socket, size) }`},
		{name: "tcp_close", source: `fn subject(socket: TcpSocket) -> Nil { tcp_close(socket); return nil }`},
	}
	backends := []struct {
		format string
		target NativeTarget
	}{
		{format: "elf-direct", target: NativeTarget{OS: "linux", Arch: "amd64"}},
		{format: "pe-direct", target: NativeTarget{OS: "windows", Arch: "amd64"}},
	}

	for _, backend := range backends {
		for _, fixture := range fixtures {
			t.Run(backend.format+"/"+fixture.name, func(t *testing.T) {
				source := fixture.source + "\nfn main() -> Nil { return nil }\n"
				program, checker := testProgram(t, source)
				image, err := BuildNative(program, checker, backend.target, backend.format)
				want := fmt.Sprintf("builtin %q is not listed as supported by the %s backend for %s-%s; use the interpreter for this feature", fixture.name, backend.format, backend.target.OS, backend.target.Arch)
				if err == nil || err.Error() != want {
					t.Fatalf("build error = %v, want exact builtin diagnostic %q", err, want)
				}
				if len(image) != 0 {
					t.Fatalf("rejected %s build returned a partial image of %d bytes", fixture.name, len(image))
				}
			})
		}
	}
}
