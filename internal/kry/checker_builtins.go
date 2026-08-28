package kry

func (c *Checker) checkBuiltin(sc *Scope, e *Expr, b Builtin, expected *Type) (*Type, *Diagnostic) {
	arg := func(i int, want *Type) (*Type, *Diagnostic) { return c.checkExpr(sc, e.Args[i], want) }
	bad := func(msg string) (*Type, *Diagnostic) {
		return TError, Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, msg)
	}
	switch b.Name {
	case "print", "println":
		if _, d := arg(0, nil); d != nil {
			return TError, d
		}
		return TNil, nil
	case "len":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyString && t.Kind != TyArray && t.Kind != TyBytes && t.Kind != TyMap && t.Kind != TySet {
			return bad("len expects String, Array[T], or Bytes")
		}
		return TInt, nil
	case "bytes":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyArray || !typeEqual(t.A, TInt) {
			return bad("bytes expects Array[Int]")
		}
		return TBytes, nil
	case "string_to_bytes":
		t, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TString) {
			return bad("string_to_bytes expects String")
		}
		return TBytes, nil
	case "bytes_to_string":
		t, d := arg(0, TBytes)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TBytes) {
			return bad("bytes_to_string expects Bytes")
		}
		return TString, nil
	case "array_push":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyArray {
			return bad("array_push expects Array[T] as its first argument")
		}
		v, d := arg(1, t.A)
		if d != nil {
			return TError, d
		}
		if t.A.Kind == TyUnknown {
			return Arr(v), nil
		}
		if !compatible(v, t.A) {
			return bad("array_push element type mismatch")
		}
		return t, nil
	case "int":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyInt && t.Kind != TyFloat && t.Kind != TyBool && t.Kind != TyString {
			return bad("int accepts Int, Float, Bool, or String")
		}
		return TInt, nil
	case "float":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyInt && t.Kind != TyFloat && t.Kind != TyString {
			return bad("float accepts Int, Float, or String")
		}
		return TFloat, nil
	case "str":
		if _, d := arg(0, nil); d != nil {
			return TError, d
		}
		return TString, nil
	case "bool":
		if _, d := arg(0, nil); d != nil {
			return TError, d
		}
		return TBool, nil
	case "assert":
		t, d := arg(0, TBool)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TBool) {
			return bad("assert expects Bool")
		}
		return TNil, nil
	case "assert_eq":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		u, d := arg(1, t)
		if d != nil {
			return TError, d
		}
		if !compatible(t, u) {
			return bad("assert_eq arguments must have the same type")
		}
		return TNil, nil
	case "abs":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if !numeric(t) {
			return bad("abs expects Int or Float")
		}
		return t, nil
	case "sqrt":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if !numeric(t) {
			return bad("sqrt expects Int or Float")
		}
		return TFloat, nil
	case "min", "max":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		u, d := arg(1, t)
		if d != nil {
			return TError, d
		}
		if !numeric(t) || !typeEqual(t, u) {
			return bad("min/max require matching Int or Float operands")
		}
		return t, nil
	case "floor", "ceil", "round":
		t, d := arg(0, TFloat)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TFloat) {
			return bad("floor, ceil, and round expect Float")
		}
		return TInt, nil
	case "pow":
		a, d := arg(0, TFloat)
		if d != nil {
			return TError, d
		}
		z, d := arg(1, TFloat)
		if d != nil {
			return TError, d
		}
		if !typeEqual(a, TFloat) || !typeEqual(z, TFloat) {
			return bad("pow expects Float operands")
		}
		return TFloat, nil
	case "log", "sin", "cos":
		t, d := arg(0, TFloat)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TFloat) {
			return bad("math function expects Float")
		}
		return TFloat, nil
	case "is_nan", "is_finite":
		t, d := arg(0, TFloat)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TFloat) {
			return bad("is_nan/is_finite expect Float")
		}
		return TBool, nil
	case "is_some", "is_none":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyOption || !typeKnown(t.A) {
			return bad("is_some/is_none expect Option[T]")
		}
		return TBool, nil
	case "is_ok", "is_err":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyResult || !typeKnown(t.A) || !typeKnown(t.B) {
			return bad("is_ok/is_err expect Result[T, E]")
		}
		return TBool, nil
	case "unwrap_or":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyOption || !typeKnown(t.A) {
			return bad("unwrap_or expects Option[T]")
		}
		u, d := arg(1, t.A)
		if d != nil {
			return TError, d
		}
		if !compatible(t.A, u) {
			return bad("unwrap_or fallback must match Option[T]")
		}
		return t.A, nil
	case "some":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		return Opt(t), nil
	case "none":
		if expected != nil && expected.Kind == TyOption {
			return expected, nil
		}
		return bad("none requires an Option[T] context")
	case "ok":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if expected != nil && expected.Kind == TyResult {
			return Res(t, expected.B), nil
		}
		return Res(t, TNil), nil
	case "err":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if expected != nil && expected.Kind == TyResult {
			return Res(expected.A, t), nil
		}
		return Res(TNil, t), nil
	case "substring":
		t, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TString) {
			return bad("substring expects String")
		}
		for i := 1; i < 3; i++ {
			z, d := arg(i, TInt)
			if d != nil {
				return TError, d
			}
			if !typeEqual(z, TInt) {
				return bad("substring indexes must be Int")
			}
		}
		return Res(TString, TString), nil
	case "contains", "starts_with", "ends_with":
		for i := 0; i < 2; i++ {
			t, d := arg(i, TString)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, TString) {
				return bad("text predicate expects String operands")
			}
		}
		return TBool, nil
	case "trim", "codepoints":
		t, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TString) {
			return bad("text operation expects String")
		}
		if b.Name == "codepoints" {
			return Arr(TInt), nil
		}
		return TString, nil
	case "split":
		for i := 0; i < 2; i++ {
			t, d := arg(i, TString)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, TString) {
				return bad("split expects String operands")
			}
		}
		return Arr(TString), nil
	case "replace":
		for i := 0; i < 3; i++ {
			t, d := arg(i, TString)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, TString) {
				return bad("replace expects String operands")
			}
		}
		return TString, nil
	case "byte_at":
		t, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		z, d := arg(1, TInt)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TString) || !typeEqual(z, TInt) {
			return bad("byte_at expects String and Int")
		}
		return Res(TInt, TString), nil
	case "hex_encode", "base64_encode":
		t, d := arg(0, TBytes)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TBytes) {
			return bad("encoding operation expects Bytes")
		}
		return TString, nil
	case "hex_decode", "base64_decode":
		t, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TString) {
			return bad("decoding operation expects String")
		}
		return Res(TBytes, TString), nil
	case "array_pop":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyArray || !typeKnown(t.A) {
			return bad("array_pop expects Array[T]")
		}
		return Opt(t.A), nil
	case "array_get":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		z, d := arg(1, TInt)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyArray || !typeEqual(z, TInt) {
			return bad("array_get expects Array[T] and Int")
		}
		return Opt(t.A), nil
	case "array_concat":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyArray {
			return bad("array_concat expects Array[T]")
		}
		u, d := arg(1, t)
		if d != nil {
			return TError, d
		}
		if t.A.Kind == TyUnknown && u.Kind == TyArray {
			return u, nil
		}
		if !typeEqual(t, u) {
			return bad("array_concat requires matching Array types")
		}
		return t, nil
	case "array_contains":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyArray {
			return bad("array_contains expects Array[T]")
		}
		u, d := arg(1, t.A)
		if d != nil {
			return TError, d
		}
		if !compatible(t.A, u) {
			return bad("array_contains needle type mismatch")
		}
		return TBool, nil
	case "array_slice":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyArray {
			return bad("array_slice expects Array[T]")
		}
		for i := 1; i < 3; i++ {
			z, d := arg(i, TInt)
			if d != nil {
				return TError, d
			}
			if !typeEqual(z, TInt) {
				return bad("array_slice indexes must be Int")
			}
		}
		return t, nil
	case "array_reverse":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyArray {
			return bad("array_reverse expects Array[T]")
		}
		return t, nil
	case "array_join":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		z, d := arg(1, TString)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyArray || !typeEqual(t.A, TString) || !typeEqual(z, TString) {
			return bad("array_join expects Array[String] and String")
		}
		return TString, nil
	case "map_get":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyMap || !typeKnown(t.A) || !typeKnown(t.B) {
			return bad("map_get expects Map[K,V]")
		}
		k, d := arg(1, t.A)
		if d != nil {
			return TError, d
		}
		if !compatible(t.A, k) {
			return bad("map_get key type mismatch")
		}
		return Opt(t.B), nil
	case "map_insert":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyMap {
			return bad("map_insert expects Map[K,V]")
		}
		k, d := arg(1, t.A)
		if d != nil {
			return TError, d
		}
		v, d := arg(2, t.B)
		if d != nil {
			return TError, d
		}
		if !compatible(t.A, k) || !compatible(t.B, v) {
			return bad("map_insert key/value type mismatch")
		}
		return t, nil
	case "map_keys":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyMap {
			return bad("map_keys expects Map[K,V]")
		}
		return Arr(t.A), nil
	case "set_contains":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TySet {
			return bad("set_contains expects Set[T]")
		}
		v, d := arg(1, t.A)
		if d != nil {
			return TError, d
		}
		if !compatible(t.A, v) {
			return bad("set_contains value type mismatch")
		}
		return TBool, nil
	case "set_insert":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TySet {
			return bad("set_insert expects Set[T]")
		}
		v, d := arg(1, t.A)
		if d != nil {
			return TError, d
		}
		if !compatible(t.A, v) {
			return bad("set_insert value type mismatch")
		}
		return t, nil
	case "set_len":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TySet {
			return bad("set_len expects Set[T]")
		}
		return TInt, nil
	case "json_parse":
		t, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TString) {
			return bad("json_parse expects String")
		}
		return Res(TJSON, TString), nil
	case "json_stringify":
		t, d := arg(0, TJSON)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TJSON) {
			return bad("json_stringify expects Json")
		}
		return TString, nil
	case "http_get":
		t, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TString) {
			return bad("http_get expects String URL")
		}
		return Res(TString, TString), nil
	case "http_request":
		m, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		u, d := arg(1, TString)
		if d != nil {
			return TError, d
		}
		body, d := arg(2, TString)
		if d != nil {
			return TError, d
		}
		if !typeEqual(m, TString) || !typeEqual(u, TString) || !typeEqual(body, TString) {
			return bad("http_request expects method, URL, and body Strings")
		}
		return Res(TString, TString), nil
	case "http_request_auth":
		for i := 0; i < 4; i++ {
			t, d := arg(i, TString)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, TString) {
				return bad("http_request_auth expects String arguments")
			}
		}
		return Res(TString, TString), nil
	case "win_registry_get":
		for i := 0; i < 2; i++ {
			t, d := arg(i, TString)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, TString) {
				return bad("win_registry_get expects String arguments")
			}
		}
		return Res(TString, TString), nil
	case "win_service_query":
		t, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TString) {
			return bad("win_service_query expects a String")
		}
		return Res(TString, TString), nil
	case "win_eventlog_write":
		for i := 0; i < 2; i++ {
			t, d := arg(i, TString)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, TString) {
				return bad("win_eventlog_write expects String arguments")
			}
		}
		return Res(TNil, TString), nil
	case "win_raw_input":
		return Res(TBytes, TString), nil
	case "win_device_io_control":
		d, e := arg(0, TString)
		if e != nil {
			return TError, e
		}
		c, e := arg(1, TInt)
		if e != nil {
			return TError, e
		}
		in, e := arg(2, TBytes)
		if e != nil {
			return TError, e
		}
		if !typeEqual(d, TString) || !typeEqual(c, TInt) || !typeEqual(in, TBytes) {
			return bad("win_device_io_control expects String, Int, and Bytes")
		}
		return Res(TBytes, TString), nil
	case "websocket_connect":
		t, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TString) {
			return bad("websocket_connect expects a String URL")
		}
		return Res(&Type{Kind: TyWebSocket, Name: "WebSocket"}, TString), nil
	case "websocket_send":
		s, d := arg(0, &Type{Kind: TyWebSocket, Name: "WebSocket"})
		if d != nil {
			return TError, d
		}
		m, d := arg(1, TString)
		if d != nil {
			return TError, d
		}
		if s.Kind != TyWebSocket || !typeEqual(m, TString) {
			return bad("websocket_send expects WebSocket and String")
		}
		return Res(TNil, TString), nil
	case "websocket_receive":
		s, d := arg(0, &Type{Kind: TyWebSocket, Name: "WebSocket"})
		if d != nil {
			return TError, d
		}
		if s.Kind != TyWebSocket {
			return bad("websocket_receive expects WebSocket")
		}
		return Res(TString, TString), nil
	case "websocket_close":
		s, d := arg(0, &Type{Kind: TyWebSocket, Name: "WebSocket"})
		if d != nil {
			return TError, d
		}
		if s.Kind != TyWebSocket {
			return bad("websocket_close expects WebSocket")
		}
		return TNil, nil
	case "process_run":
		p, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		a, d := arg(1, Arr(TString))
		if d != nil {
			return TError, d
		}
		if !typeEqual(p, TString) || !typeEqual(a, Arr(TString)) {
			return bad("process_run expects String and Array[String]")
		}
		return Res(TInt, TString), nil
	case "thread_channel", "thread_channel_with_capacity":
		if b.Name == "thread_channel_with_capacity" {
			t, d := arg(0, TInt)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, TInt) {
				return bad("thread_channel_with_capacity expects Int")
			}
		}
		if expected != nil && expected.Kind == TyChannel {
			return expected, nil
		}
		return bad(b.Name + " requires a Channel[T] type context")
	case "thread_spawn":
		t, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TString) {
			return bad("thread_spawn expects a worker function name String")
		}
		if e.Args[0].Kind != ExString {
			return bad("thread_spawn requires a literal worker function name")
		}
		f := c.Env.Functions[e.Args[0].Str]
		if f == nil || len(f.Params) != 0 {
			return bad("thread_spawn requires a zero-argument worker function")
		}
		return TypeThread(mustResolve(c.Env, f.Return)), nil
	case "thread_send", "thread_try_send", "thread_send_timeout":
		ch, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if ch.Kind != TyChannel || !typeKnown(ch.A) {
			return bad("thread send expects Channel[T]")
		}
		v, d := arg(1, ch.A)
		if d != nil {
			return TError, d
		}
		if !typeEqual(v, ch.A) {
			return bad("thread send value type mismatch")
		}
		if !TypeCopyable(v) {
			return bad("thread send requires a recursively Copy value")
		}
		if b.Name == "thread_send_timeout" {
			z, d := arg(2, TInt)
			if d != nil {
				return TError, d
			}
			if !typeEqual(z, TInt) {
				return bad("thread_send_timeout duration must be Int")
			}
		}
		if b.Name == "thread_send" {
			return TNil, nil
		}
		return Res(TNil, TString), nil
	case "thread_receive":
		ch, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if ch.Kind != TyChannel {
			return bad("thread_receive expects Channel[T]")
		}
		return ch.A, nil
	case "thread_try_receive":
		ch, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if ch.Kind != TyChannel {
			return TError, Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "thread_try_receive expects Channel[T]")
		}
		return Res(ch.A, TString), nil
	case "thread_receive_timeout":
		ch, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		z, d := arg(1, TInt)
		if d != nil {
			return TError, d
		}
		if ch.Kind != TyChannel || !typeEqual(z, TInt) {
			return bad("thread_receive_timeout expects Channel[T] and Int")
		}
		if e.Args[1].Kind == ExUnary && e.Args[1].Op == MINUS {
			return bad("timeout duration cannot be negative")
		}
		return ch.A, nil
	case "thread_join":
		th, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if th.Kind != TyThread {
			return bad("thread_join expects Thread[T]")
		}
		return th.A, nil
	case "thread_join_timeout":
		th, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		z, d := arg(1, TInt)
		if d != nil {
			return TError, d
		}
		if th.Kind != TyThread || !typeEqual(z, TInt) {
			return bad("thread_join_timeout expects Thread[T] and Int")
		}
		return Res(th.A, TString), nil
	case "thread_cancel":
		th, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if th.Kind != TyThread {
			return bad("thread_cancel expects Thread[T]")
		}
		return TNil, nil
	case "thread_close":
		ch, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if ch.Kind != TyChannel {
			return bad("thread_close expects Channel[T]")
		}
		return TNil, nil
	case "fs_read_text":
		t, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TString) {
			return bad("fs_read_text expects a String path")
		}
		return Res(TString, TString), nil
	case "fs_write_text":
		a, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		z, d := arg(1, TString)
		if d != nil {
			return TError, d
		}
		if !typeEqual(a, TString) || !typeEqual(z, TString) {
			return bad("fs_write_text expects String and String")
		}
		return Res(TNil, TString), nil
	case "fs_read_bytes":
		t, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TString) {
			return bad("fs_read_bytes expects a String path")
		}
		return Res(TBytes, TString), nil
	case "fs_write_bytes":
		a, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		z, d := arg(1, TBytes)
		if d != nil {
			return TError, d
		}
		if !typeEqual(a, TString) || !typeEqual(z, TBytes) {
			return bad("fs_write_bytes expects String and Bytes")
		}
		return Res(TNil, TString), nil
	case "fs_exists":
		t, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TString) {
			return bad("fs_exists expects a String path")
		}
		return TBool, nil
	case "env_get":
		t, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TString) {
			return bad("env_get expects String")
		}
		return Opt(TString), nil
	}
	return TError, Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "builtin '%s' is not implemented", b.Name)
}
func mustResolve(e *TypeEnv, s *TypeSpec) *Type { t, _ := resolveSpec(e, s, 0); return t }
