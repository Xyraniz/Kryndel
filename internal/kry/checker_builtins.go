package kry

func (c *Checker) checkBuiltin(sc *Scope, e *Expr, b Builtin, expected *Type) (*Type, *Diagnostic) {
	arg := func(i int, want *Type) (*Type, *Diagnostic) { return c.checkExpr(sc, e.Args[i], want) }
	bad := func(msg string) (*Type, *Diagnostic) {
		return TError, Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "%s", msg)
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
	case "bytes_from_u8":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyArray || !typeEqual(t.A, TUInt8) {
			return bad("bytes_from_u8 expects Array[UInt8]")
		}
		return TBytes, nil
	case "u8_array":
		t, d := arg(0, TBytes)
		if d != nil {
			return TError, d
		}
		if !typeEqual(t, TBytes) {
			return bad("u8_array expects Bytes")
		}
		return Arr(TUInt8), nil
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
		if t.Kind != TyInt && t.Kind != TyUInt && t.Kind != TyFloat && t.Kind != TyBool && t.Kind != TyString {
			return bad("int accepts Int, UInt, Float, Bool, or String")
		}
		return TInt, nil
	case "u8", "u16", "u32", "u64":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyInt && t.Kind != TyUInt {
			return bad("unsigned conversion accepts Int or UInt")
		}
		bits := map[string]uint8{"u8": 8, "u16": 16, "u32": 32, "u64": 64}[b.Name]
		return uintType(bits), nil
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
	case "result_unwrap":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyResult || !typeKnown(t.A) || !typeKnown(t.B) {
			return bad("result_unwrap expects Result[T, E]")
		}
		return t.A, nil
	case "result_error":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyResult || !typeKnown(t.A) || !typeKnown(t.B) {
			return bad("result_error expects Result[T, E]")
		}
		return Opt(t.B), nil
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
	case "array_set":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		z, d := arg(1, TInt)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyArray || !typeEqual(z, TInt) {
			return bad("array_set expects Array[T] and Int")
		}
		v, d := arg(2, t.A)
		if d != nil {
			return TError, d
		}
		if !compatible(t.A, v) {
			return bad("array_set replacement type mismatch")
		}
		return Res(t, TString), nil
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
	case "json_kind":
		if t, d := arg(0, TJSON); d != nil {
			return TError, d
		} else if !typeEqual(t, TJSON) {
			return bad("json_kind expects Json")
		}
		return TString, nil
	case "json_object_get":
		if t, d := arg(0, TJSON); d != nil {
			return TError, d
		} else if !typeEqual(t, TJSON) {
			return bad("json_object_get expects Json")
		}
		if t, d := arg(1, TString); d != nil {
			return TError, d
		} else if !typeEqual(t, TString) {
			return bad("json_object_get expects a String key")
		}
		return Res(TJSON, TString), nil
	case "json_array_len":
		if t, d := arg(0, TJSON); d != nil {
			return TError, d
		} else if !typeEqual(t, TJSON) {
			return bad("json_array_len expects Json")
		}
		return Res(TInt, TString), nil
	case "json_array_get":
		if t, d := arg(0, TJSON); d != nil {
			return TError, d
		} else if !typeEqual(t, TJSON) {
			return bad("json_array_get expects Json")
		}
		if t, d := arg(1, TInt); d != nil {
			return TError, d
		} else if !typeEqual(t, TInt) {
			return bad("json_array_get expects an Int index")
		}
		return Res(TJSON, TString), nil
	case "json_string":
		if t, d := arg(0, TJSON); d != nil {
			return TError, d
		} else if !typeEqual(t, TJSON) {
			return bad("json_string expects Json")
		}
		return Res(TString, TString), nil
	case "json_int":
		if t, d := arg(0, TJSON); d != nil {
			return TError, d
		} else if !typeEqual(t, TJSON) {
			return bad("json_int expects Json")
		}
		return Res(TInt, TString), nil
	case "json_uint":
		if t, d := arg(0, TJSON); d != nil {
			return TError, d
		} else if !typeEqual(t, TJSON) {
			return bad("json_uint expects Json")
		}
		return Res(TUInt64, TString), nil
	case "json_float":
		if t, d := arg(0, TJSON); d != nil {
			return TError, d
		} else if !typeEqual(t, TJSON) {
			return bad("json_float expects Json")
		}
		return Res(TFloat, TString), nil
	case "json_bool":
		if t, d := arg(0, TJSON); d != nil {
			return TError, d
		} else if !typeEqual(t, TJSON) {
			return bad("json_bool expects Json")
		}
		return Res(TBool, TString), nil
	case "json_is_null":
		if t, d := arg(0, TJSON); d != nil {
			return TError, d
		} else if !typeEqual(t, TJSON) {
			return bad("json_is_null expects Json")
		}
		return TBool, nil
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
	case "discord_api_request":
		for i := 0; i < 4; i++ {
			t, d := arg(i, TString)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, TString) {
				return bad("discord_api_request expects String arguments")
			}
		}
		return Res(TString, TString), nil
	case "discord_user_api_request":
		for i := 0; i < 4; i++ {
			t, d := arg(i, TString)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, TString) {
				return bad("discord_user_api_request expects String arguments")
			}
		}
		return Res(TString, TString), nil
	case "discord_api_request_with_reason":
		for i := 0; i < 5; i++ {
			t, d := arg(i, TString)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, TString) {
				return bad("discord_api_request_with_reason expects String arguments")
			}
		}
		return Res(TString, TString), nil
	case "discord_gateway_session_bind":
		socket, d := arg(0, &Type{Kind: TyWebSocket, Name: "WebSocket"})
		if d != nil {
			return TError, d
		}
		intents, d := arg(1, TInt)
		if d != nil {
			return TError, d
		}
		if socket.Kind != TyWebSocket || !typeEqual(intents, TInt) {
			return bad("discord_gateway_session_bind expects WebSocket and Int")
		}
		return Res(TNil, TString), nil
	case "discord_gateway_send":
		opcode, d := arg(0, TInt)
		if d != nil {
			return TError, d
		}
		data, d := arg(1, TString)
		if d != nil {
			return TError, d
		}
		if !typeEqual(opcode, TInt) || !typeEqual(data, TString) {
			return bad("discord_gateway_send expects Int and String arguments")
		}
		return Res(TNil, TString), nil
	case "discord_gateway_session_unbind":
		return Arr(TString), nil
	case "discord_gateway_request_members":
		want := []*Type{TString, TString, TInt, Arr(TString), TBool, TString, TBool}
		for i, expected := range want {
			t, d := arg(i, expected)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, expected) {
				return bad("discord_gateway_request_members expects guild ID, query, limit, user IDs, presences, nonce, and all-members flag")
			}
		}
		return Res(TString, TString), nil
	case "discord_gateway_member_chunk":
		event, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		if !typeEqual(event, TString) {
			return bad("discord_gateway_member_chunk expects event JSON")
		}
		return Res(TString, TString), nil
	case "discord_gateway_rate_limit":
		event, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		if !typeEqual(event, TString) {
			return bad("discord_gateway_rate_limit expects event JSON")
		}
		return Res(TNil, TString), nil
	case "discord_gateway_take_member_query_failures":
		return Arr(TString), nil
	case "discord_interaction_request":
		for i := 0; i < 3; i++ {
			t, d := arg(i, TString)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, TString) {
				return bad("discord_interaction_request expects String arguments")
			}
		}
		return Res(TString, TString), nil
	case "discord_api_upload":
		for i := 0; i < 6; i++ {
			want := TString
			if i == 4 {
				want = TBytes
			}
			t, d := arg(i, want)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, want) {
				return bad("discord_api_upload expects String arguments and Bytes file data")
			}
		}
		return Res(TString, TString), nil
	case "discord_api_upload_with_reason":
		for i := 0; i < 7; i++ {
			want := TString
			if i == 4 {
				want = TBytes
			}
			t, d := arg(i, want)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, want) {
				return bad("discord_api_upload_with_reason expects String arguments and Bytes file data")
			}
		}
		return Res(TString, TString), nil
	case "discord_api_upload_files", "discord_user_api_upload_files":
		want := []*Type{TString, TString, TString, Arr(TString), Arr(TBytes), TString}
		for i, expected := range want {
			t, d := arg(i, expected)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, expected) {
				return bad("Discord multipart upload expects method, route, payload, filenames, file bytes, and token")
			}
		}
		return Res(TString, TString), nil
	case "discord_api_upload_files_with_reason":
		want := []*Type{TString, TString, TString, Arr(TString), Arr(TBytes), TString, TString}
		for i, expected := range want {
			t, d := arg(i, expected)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, expected) {
				return bad("discord_api_upload_files_with_reason expects method, route, payload, filenames, file bytes, token, and reason")
			}
		}
		return Res(TString, TString), nil
	case "discord_webhook_upload":
		want := []*Type{TString, TString, TString, Arr(TString), Arr(TBytes), TString}
		for i, expected := range want {
			t, d := arg(i, expected)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, expected) {
				return bad("discord_webhook_upload expects method, route, payload, filenames, file bytes, and token")
			}
		}
		return Res(TString, TString), nil
	case "discord_verify_interaction":
		for i := 0; i < 4; i++ {
			t, d := arg(i, TString)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, TString) {
				return bad("discord_verify_interaction expects String arguments")
			}
		}
		return Res(TBool, TString), nil
	case "discord_cache_get":
		for i := 0; i < 2; i++ {
			t, d := arg(i, TString)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, TString) {
				return bad("discord_cache_get expects String arguments")
			}
		}
		return Res(TString, TString), nil
	case "discord_cache_put":
		for i := 0; i < 3; i++ {
			t, d := arg(i, TString)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, TString) {
				return bad("discord_cache_put expects String arguments")
			}
		}
		return Res(TNil, TString), nil
	case "discord_cache_delete":
		for i := 0; i < 2; i++ {
			t, d := arg(i, TString)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, TString) {
				return bad("discord_cache_delete expects String arguments")
			}
		}
		return TNil, nil
	case "discord_cache_clear":
		return TNil, nil
	case "discord_cache_ingest":
		for i := 0; i < 2; i++ {
			t, d := arg(i, TString)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, TString) {
				return bad("discord_cache_ingest expects String arguments")
			}
		}
		return Res(TNil, TString), nil
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
	case "websocket_send_binary":
		s, d := arg(0, &Type{Kind: TyWebSocket, Name: "WebSocket"})
		if d != nil {
			return TError, d
		}
		data, d := arg(1, TBytes)
		if d != nil {
			return TError, d
		}
		if s.Kind != TyWebSocket || !typeEqual(data, TBytes) {
			return bad("websocket_send_binary expects WebSocket and Bytes")
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
	case "websocket_receive_timeout":
		s, d := arg(0, &Type{Kind: TyWebSocket, Name: "WebSocket"})
		if d != nil {
			return TError, d
		}
		ms, d := arg(1, TInt)
		if d != nil {
			return TError, d
		}
		if s.Kind != TyWebSocket || !typeEqual(ms, TInt) {
			return bad("websocket_receive_timeout expects WebSocket and Int")
		}
		return Res(TString, TString), nil
	case "websocket_receive_binary", "websocket_receive_binary_timeout":
		s, d := arg(0, &Type{Kind: TyWebSocket, Name: "WebSocket"})
		if d != nil {
			return TError, d
		}
		if s.Kind != TyWebSocket {
			return bad(b.Name + " expects WebSocket")
		}
		if b.Name == "websocket_receive_binary_timeout" {
			ms, d := arg(1, TInt)
			if d != nil {
				return TError, d
			}
			if !typeEqual(ms, TInt) {
				return bad("websocket_receive_binary_timeout expects WebSocket and Int")
			}
		}
		return Res(TBytes, TString), nil
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
	case "process_args":
		return Arr(TString), nil
	case "actor_channel", "actor_channel_with_capacity":
		if b.Name == "actor_channel_with_capacity" {
			t, d := arg(0, TInt)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, TInt) {
				return bad("actor_channel_with_capacity expects Int")
			}
		}
		if expected != nil && expected.Kind == TyActor {
			return expected, nil
		}
		return bad(b.Name + " requires an Actor[T] type context")
	case "actor_send":
		actor, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if actor.Kind != TyActor || !typeKnown(actor.A) {
			return bad("actor_send expects Actor[T]")
		}
		value, d := arg(1, actor.A)
		if d != nil || !typeEqual(value, actor.A) {
			return bad("actor_send value type mismatch")
		}
		if !TypeCopyable(value) {
			return bad("actor_send requires a recursively Copy value")
		}
		return TNil, nil
	case "actor_try_receive":
		actor, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if actor.Kind != TyActor {
			return bad("actor_try_receive expects Actor[T]")
		}
		return Res(actor.A, TString), nil
	case "actor_receive_timeout":
		actor, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		ms, d := arg(1, TInt)
		if d != nil {
			return TError, d
		}
		if actor.Kind != TyActor || !typeEqual(ms, TInt) {
			return bad("actor_receive_timeout expects Actor[T] and Int")
		}
		return actor.A, nil
	case "actor_close":
		actor, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if actor.Kind != TyActor {
			return bad("actor_close expects Actor[T]")
		}
		return TNil, nil
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
	case "await":
		th, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if th.Kind != TyThread {
			return bad("await expects Thread[T]")
		}
		return th.A, nil
	case "await_timeout":
		th, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		ms, d := arg(1, TInt)
		if d != nil {
			return TError, d
		}
		if th.Kind != TyThread || !typeEqual(ms, TInt) {
			return bad("await_timeout expects Thread[T] and Int")
		}
		return Res(th.A, TString), nil
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
	case "yield_now":
		return TNil, nil
	case "sleep_ms":
		ms, d := arg(0, TInt)
		if d != nil {
			return TError, d
		}
		if !typeEqual(ms, TInt) {
			return bad("sleep_ms expects Int")
		}
		return Res(TNil, TString), nil
	case "shared_new":
		value, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if !TypeCopyable(value) {
			return bad("shared_new requires a recursively Copy value")
		}
		if expected != nil && expected.Kind == TyShared {
			if !typeEqual(expected.A, value) {
				return bad("shared_new value type mismatch")
			}
			return expected, nil
		}
		return bad("shared_new requires a Shared[T] type context")
	case "shared_read":
		cell, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if cell.Kind != TyShared || !typeKnown(cell.A) {
			return bad("shared_read expects Shared[T]")
		}
		return cell.A, nil
	case "shared_write", "shared_swap":
		cell, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if cell.Kind != TyShared || !typeKnown(cell.A) {
			return bad(b.Name + " expects Shared[T]")
		}
		value, d := arg(1, cell.A)
		if d != nil {
			return TError, d
		}
		if !typeEqual(value, cell.A) || !TypeCopyable(value) {
			return bad(b.Name + " value must match a recursively Copy T")
		}
		if b.Name == "shared_swap" {
			return cell.A, nil
		}
		return TNil, nil
	case "task_group":
		if expected != nil && expected.Kind != TyTaskGroup {
			return bad("task_group result must be used as TaskGroup")
		}
		return TTaskGroup, nil
	case "task_spawn":
		group, d := arg(0, TTaskGroup)
		if d != nil {
			return TError, d
		}
		name, d := arg(1, TString)
		if d != nil {
			return TError, d
		}
		if group.Kind != TyTaskGroup || !typeEqual(name, TString) || e.Args[1].Kind != ExString {
			return bad("task_spawn expects TaskGroup and a literal worker name")
		}
		f := c.Env.Functions[e.Args[1].Str]
		if f == nil || len(f.Params) != 0 {
			return bad("task_spawn requires a zero-argument worker function")
		}
		return TypeThread(mustResolve(c.Env, f.Return)), nil
	case "task_group_wait":
		group, d := arg(0, TTaskGroup)
		if d != nil {
			return TError, d
		}
		if group.Kind != TyTaskGroup {
			return bad("task_group_wait expects TaskGroup")
		}
		return Res(TNil, TString), nil
	case "task_group_cancel":
		group, d := arg(0, TTaskGroup)
		if d != nil {
			return TError, d
		}
		if group.Kind != TyTaskGroup {
			return bad("task_group_cancel expects TaskGroup")
		}
		return TNil, nil
	case "crypto_sha256":
		if _, d := arg(0, TBytes); d != nil {
			return TError, d
		}
		return TBytes, nil
	case "crypto_hmac_sha256":
		if _, d := arg(0, TBytes); d != nil {
			return TError, d
		}
		if _, d := arg(1, TBytes); d != nil {
			return TError, d
		}
		return TBytes, nil
	case "crypto_random_bytes":
		if _, d := arg(0, TInt); d != nil {
			return TError, d
		}
		return Res(TBytes, TString), nil
	case "crypto_sha512", "crypto_sha384", "crypto_sha1", "crypto_md5":
		if _, d := arg(0, TBytes); d != nil {
			return TError, d
		}
		return TBytes, nil
	case "crypto_aes_gcm_encrypt", "crypto_aes_gcm_decrypt":
		if _, d := arg(0, TBytes); d != nil {
			return TError, d
		}
		if _, d := arg(1, TBytes); d != nil {
			return TError, d
		}
		if _, d := arg(2, TBytes); d != nil {
			return TError, d
		}
		return Res(TBytes, TString), nil
	case "crypto_pbkdf2_sha256":
		if _, d := arg(0, TBytes); d != nil {
			return TError, d
		}
		if _, d := arg(1, TBytes); d != nil {
			return TError, d
		}
		if _, d := arg(2, TInt); d != nil {
			return TError, d
		}
		if _, d := arg(3, TInt); d != nil {
			return TError, d
		}
		return Res(TBytes, TString), nil
	case "crypto_hkdf_sha256":
		if _, d := arg(0, TBytes); d != nil {
			return TError, d
		}
		if _, d := arg(1, TBytes); d != nil {
			return TError, d
		}
		if _, d := arg(2, TBytes); d != nil {
			return TError, d
		}
		if _, d := arg(3, TInt); d != nil {
			return TError, d
		}
		return Res(TBytes, TString), nil
	case "crypto_constant_time_equal":
		if _, d := arg(0, TBytes); d != nil {
			return TError, d
		}
		if _, d := arg(1, TBytes); d != nil {
			return TError, d
		}
		return TBool, nil
	case "crypto_xor":
		if _, d := arg(0, TBytes); d != nil {
			return TError, d
		}
		if _, d := arg(1, TBytes); d != nil {
			return TError, d
		}
		return Res(TBytes, TString), nil
	case "base64url_encode":
		if _, d := arg(0, TBytes); d != nil {
			return TError, d
		}
		return TString, nil
	case "base64url_decode":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		return Res(TBytes, TString), nil
	case "string_slice":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		if _, d := arg(1, TInt); d != nil {
			return TError, d
		}
		if _, d := arg(2, TInt); d != nil {
			return TError, d
		}
		return Res(TString, TString), nil
	case "array_slice_range":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyArray {
			return bad("array_slice_range expects Array[T] as its first argument")
		}
		if _, d := arg(1, TInt); d != nil {
			return TError, d
		}
		if _, d := arg(2, TInt); d != nil {
			return TError, d
		}
		return t, nil
	case "string_format":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		t, d := arg(1, Arr(TString))
		if d != nil {
			return TError, d
		}
		if t.Kind != TyArray || !typeEqual(t.A, TString) {
			return bad("string_format expects Array[String] as its second argument")
		}
		return TString, nil
	case "array_indices":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyArray {
			return bad("array_indices expects Array[T]")
		}
		return Arr(TInt), nil
	case "array_zip":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyArray {
			return bad("array_zip expects Array[T] as its first argument")
		}
		u, d := arg(1, t)
		if d != nil {
			return TError, d
		}
		if u.Kind != TyArray {
			return bad("array_zip expects Array[T] as its second argument")
		}
		if t.A.Kind == TyUnknown {
			return Arr(Arr(u.A)), nil
		}
		if !compatible(t.A, u.A) {
			return bad("array_zip element types must match")
		}
		return Arr(Arr(t.A)), nil
	case "fs_read_dir":
		_, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		return Res(Arr(TString), TString), nil
	case "fs_create_dir", "fs_create_dir_all", "fs_remove_file", "fs_remove_dir_all":
		_, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		return Res(TNil, TString), nil
	case "fs_copy_file", "fs_move_file":
		for i := 0; i < 2; i++ {
			if _, d := arg(i, TString); d != nil {
				return TError, d
			}
		}
		return Res(TNil, TString), nil
	case "fs_is_file", "fs_is_dir":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		return TBool, nil
	case "fs_file_size", "fs_file_modified_time":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		return Res(TInt, TString), nil
	case "fs_join_path":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		if _, d := arg(1, Arr(TString)); d != nil {
			return TError, d
		}
		return TString, nil
	case "fs_absolute_path":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		return Res(TString, TString), nil
	case "fs_temp_dir":
		return TString, nil
	case "fs_temp_file":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		return Res(TString, TString), nil
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
	case "poly_register":
		for i := 0; i < 2; i++ {
			t, d := arg(i, TString)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, TString) {
				return bad("poly_register expects String slot and handler")
			}
		}
		// Literal names can be checked early; variable names are resolved by the
		// runtime and report incompatible handlers through the returned Result.
		if e.Args[1].Kind == ExString && !isStaticPolyHandler(c, e.Args[1]) {
			return bad("poly_register requires a literal name of one top-level fn(String) -> String handler")
		}
		priority, d := arg(2, TInt)
		if d != nil {
			return TError, d
		}
		if !typeEqual(priority, TInt) {
			return bad("poly_register expects an Int priority")
		}
		return Res(TNil, TString), nil
	case "poly_reorder":
		for i := 0; i < 3; i++ {
			t, d := arg(i, TString)
			if d != nil {
				return TError, d
			}
			if !typeEqual(t, TString) {
				return bad("poly_reorder expects String slot, handler, and predecessor")
			}
		}
		if (e.Args[1].Kind == ExString && !isStaticPolyHandler(c, e.Args[1])) ||
			(e.Args[2].Kind == ExString && !isStaticPolyHandler(c, e.Args[2])) {
			return bad("poly_reorder requires literal names of top-level fn(String) -> String handlers")
		}
		return Res(TNil, TString), nil
	case "poly_dispatch":
		slot, d := arg(0, TString)
		if d != nil {
			return TError, d
		}
		input, d := arg(1, TString)
		if d != nil {
			return TError, d
		}
		if !typeEqual(slot, TString) || !typeEqual(input, TString) {
			return bad("poly_dispatch expects String slot and input")
		}
		return Res(TString, TString), nil
	case "string_repeat":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		if _, d := arg(1, TInt); d != nil {
			return TError, d
		}
		return Res(TString, TString), nil
	case "string_index_of":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		if _, d := arg(1, TString); d != nil {
			return TError, d
		}
		return Opt(TInt), nil
	case "string_pad_start", "string_pad_end":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		if _, d := arg(1, TInt); d != nil {
			return TError, d
		}
		if _, d := arg(2, TString); d != nil {
			return TError, d
		}
		return Res(TString, TString), nil
	case "string_lines", "string_chars":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		return Arr(TString), nil
	case "string_to_upper", "string_to_lower":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		return TString, nil
	case "array_sort":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyArray || (t.A.Kind != TyInt && t.A.Kind != TyUInt && t.A.Kind != TyFloat && t.A.Kind != TyString) {
			return bad("array_sort expects Array[Int], Array[UInt], Array[Float], or Array[String]")
		}
		return t, nil
	case "array_index_of":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyArray {
			return bad("array_index_of expects Array[T]")
		}
		if _, d := arg(1, t.A); d != nil {
			return TError, d
		}
		return Opt(TInt), nil
	case "array_sum":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyArray || !typeEqual(t.A, TInt) {
			return bad("array_sum expects Array[Int]")
		}
		return TInt, nil
	case "array_min", "array_max":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyArray || !typeEqual(t.A, TInt) {
			return bad("array_min/array_max expect Array[Int]")
		}
		return Opt(TInt), nil
	case "array_take", "array_drop":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyArray {
			return bad("array_take/array_drop expect Array[T]")
		}
		if _, d := arg(1, TInt); d != nil {
			return TError, d
		}
		return t, nil
	case "map_contains_key":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyMap {
			return bad("map_contains_key expects Map[K,V]")
		}
		if _, d := arg(1, t.A); d != nil {
			return TError, d
		}
		return TBool, nil
	case "map_values":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyMap {
			return bad("map_values expects Map[K,V]")
		}
		return Arr(t.B), nil
	case "map_remove":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyMap {
			return bad("map_remove expects Map[K,V]")
		}
		if _, d := arg(1, t.A); d != nil {
			return TError, d
		}
		return t, nil
	case "set_remove":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TySet {
			return bad("set_remove expects Set[T]")
		}
		if _, d := arg(1, t.A); d != nil {
			return TError, d
		}
		return t, nil
	case "set_to_array":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TySet {
			return bad("set_to_array expects Set[T]")
		}
		return Arr(t.A), nil
	case "tan", "atan", "exp", "log10", "log2":
		if _, d := arg(0, TFloat); d != nil {
			return TError, d
		}
		return TFloat, nil
	case "atan2":
		if _, d := arg(0, TFloat); d != nil {
			return TError, d
		}
		if _, d := arg(1, TFloat); d != nil {
			return TError, d
		}
		return TFloat, nil
	case "trunc":
		if _, d := arg(0, TFloat); d != nil {
			return TError, d
		}
		return TInt, nil
	case "sign":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyInt && t.Kind != TyFloat {
			return bad("sign expects Int or Float")
		}
		return TInt, nil
	case "clamp":
		t, d := arg(0, nil)
		if d != nil {
			return TError, d
		}
		if t.Kind != TyInt && t.Kind != TyFloat {
			return bad("clamp expects Int or Float")
		}
		if _, d := arg(1, t); d != nil {
			return TError, d
		}
		if _, d := arg(2, t); d != nil {
			return TError, d
		}
		return t, nil
	case "uuid_v4":
		return Res(TString, TString), nil
	case "uuid_v5":
		for i := 0; i < 2; i++ {
			if _, d := arg(i, TString); d != nil {
				return TError, d
			}
		}
		return Res(TString, TString), nil
	case "uuid_is_valid":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		return TBool, nil
	case "platform_os", "platform_arch", "platform_runtime":
		return TString, nil
	case "platform_os_version":
		return Res(TString, TString), nil
	case "platform_hostname":
		return Res(TString, TString), nil
	case "dotenv_load":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		return Res(MapOf(TString, TString), TString), nil
	case "datetime_now":
		return TString, nil
	case "datetime_unix_ms":
		return TInt, nil
	case "datetime_format", "datetime_parse":
		want := TInt
		if b.Name == "datetime_parse" {
			want = TString
		}
		if _, d := arg(0, want); d != nil {
			return TError, d
		}
		if _, d := arg(1, TString); d != nil {
			return TError, d
		}
		if b.Name == "datetime_parse" {
			return Res(TInt, TString), nil
		}
		return Res(TString, TString), nil
	case "random_new":
		if _, d := arg(0, TInt); d != nil {
			return TError, d
		}
		return &Type{Kind: TyRandom, Name: "Random"}, nil
	case "random_int":
		r, d := arg(0, &Type{Kind: TyRandom, Name: "Random"})
		if d != nil {
			return TError, d
		}
		if r.Kind != TyRandom {
			return bad("random_int expects Random as its first argument")
		}
		if _, d := arg(1, TInt); d != nil {
			return TError, d
		}
		if _, d := arg(2, TInt); d != nil {
			return TError, d
		}
		return Res(TInt, TString), nil
	case "random_float":
		if r, d := arg(0, &Type{Kind: TyRandom, Name: "Random"}); d != nil || r.Kind != TyRandom {
			if d != nil {
				return TError, d
			}
			return bad("random_float expects Random")
		}
		return TFloat, nil
	case "random_choice":
		r, d := arg(0, &Type{Kind: TyRandom, Name: "Random"})
		if d != nil {
			return TError, d
		}
		if r.Kind != TyRandom {
			return bad("random_choice expects Random as its first argument")
		}
		v, d := arg(1, nil)
		if d != nil {
			return TError, d
		}
		if v.Kind != TyArray {
			return bad("random_choice expects Array[T]")
		}
		return Opt(v.A), nil
	case "regex_compile":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		return Res(&Type{Kind: TyRegex, Name: "Regex"}, TString), nil
	case "regex_is_match":
		if r, d := arg(0, &Type{Kind: TyRegex, Name: "Regex"}); d != nil || r.Kind != TyRegex {
			if d != nil {
				return TError, d
			}
			return bad("regex_is_match expects Regex")
		}
		if _, d := arg(1, TString); d != nil {
			return TError, d
		}
		return TBool, nil
	case "regex_find":
		if r, d := arg(0, &Type{Kind: TyRegex, Name: "Regex"}); d != nil || r.Kind != TyRegex {
			if d != nil {
				return TError, d
			}
			return bad("regex_find expects Regex")
		}
		if _, d := arg(1, TString); d != nil {
			return TError, d
		}
		return Opt(TString), nil
	case "regex_find_all", "regex_split":
		if r, d := arg(0, &Type{Kind: TyRegex, Name: "Regex"}); d != nil || r.Kind != TyRegex {
			if d != nil {
				return TError, d
			}
			return bad(b.Name + " expects Regex")
		}
		if _, d := arg(1, TString); d != nil {
			return TError, d
		}
		return Arr(TString), nil
	case "regex_replace_all":
		if r, d := arg(0, &Type{Kind: TyRegex, Name: "Regex"}); d != nil || r.Kind != TyRegex {
			if d != nil {
				return TError, d
			}
			return bad("regex_replace_all expects Regex")
		}
		for i := 1; i < 3; i++ {
			if _, d := arg(i, TString); d != nil {
				return TError, d
			}
		}
		return TString, nil
	case "sqlite_open":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		return Res(&Type{Kind: TySQLite, Name: "SQLite"}, TString), nil
	case "sqlite_exec":
		if db, d := arg(0, &Type{Kind: TySQLite, Name: "SQLite"}); d != nil || db.Kind != TySQLite {
			if d != nil {
				return TError, d
			}
			return bad("sqlite_exec expects SQLite")
		}
		if _, d := arg(1, TString); d != nil {
			return TError, d
		}
		return Res(TInt, TString), nil
	case "sqlite_query":
		if db, d := arg(0, &Type{Kind: TySQLite, Name: "SQLite"}); d != nil || db.Kind != TySQLite {
			if d != nil {
				return TError, d
			}
			return bad("sqlite_query expects SQLite")
		}
		if _, d := arg(1, TString); d != nil {
			return TError, d
		}
		return Res(Arr(Arr(TString)), TString), nil
	case "sqlite_close":
		if db, d := arg(0, &Type{Kind: TySQLite, Name: "SQLite"}); d != nil || db.Kind != TySQLite {
			if d != nil {
				return TError, d
			}
			return bad("sqlite_close expects SQLite")
		}
		return TNil, nil
	case "tcp_connect":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		if _, d := arg(1, TInt); d != nil {
			return TError, d
		}
		return Res(&Type{Kind: TyTCPSocket, Name: "TcpSocket"}, TString), nil
	case "tcp_listen":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		if _, d := arg(1, TInt); d != nil {
			return TError, d
		}
		return Res(&Type{Kind: TyTCPListener, Name: "TcpListener"}, TString), nil
	case "tcp_accept":
		if listener, d := arg(0, &Type{Kind: TyTCPListener, Name: "TcpListener"}); d != nil || listener.Kind != TyTCPListener {
			if d != nil {
				return TError, d
			}
			return bad("tcp_accept expects TcpListener")
		}
		return Res(&Type{Kind: TyTCPSocket, Name: "TcpSocket"}, TString), nil
	case "tcp_send":
		if socket, d := arg(0, &Type{Kind: TyTCPSocket, Name: "TcpSocket"}); d != nil || socket.Kind != TyTCPSocket {
			if d != nil {
				return TError, d
			}
			return bad("tcp_send expects TcpSocket")
		}
		if _, d := arg(1, TBytes); d != nil {
			return TError, d
		}
		return Res(TInt, TString), nil
	case "tcp_receive":
		if socket, d := arg(0, &Type{Kind: TyTCPSocket, Name: "TcpSocket"}); d != nil || socket.Kind != TyTCPSocket {
			if d != nil {
				return TError, d
			}
			return bad("tcp_receive expects TcpSocket")
		}
		if _, d := arg(1, TInt); d != nil {
			return TError, d
		}
		return Res(TBytes, TString), nil
	case "tcp_local_port":
		if listener, d := arg(0, &Type{Kind: TyTCPListener, Name: "TcpListener"}); d != nil || listener.Kind != TyTCPListener {
			if d != nil {
				return TError, d
			}
			return bad("tcp_local_port expects TcpListener")
		}
		return Res(TInt, TString), nil
	case "tcp_close":
		if socket, d := arg(0, &Type{Kind: TyTCPSocket, Name: "TcpSocket"}); d != nil || socket.Kind != TyTCPSocket {
			if d != nil {
				return TError, d
			}
			return bad("tcp_close expects TcpSocket")
		}
		return TNil, nil
	case "tcp_listener_close":
		if listener, d := arg(0, &Type{Kind: TyTCPListener, Name: "TcpListener"}); d != nil || listener.Kind != TyTCPListener {
			if d != nil {
				return TError, d
			}
			return bad("tcp_listener_close expects TcpListener")
		}
		return TNil, nil
	case "udp_bind":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		if _, d := arg(1, TInt); d != nil {
			return TError, d
		}
		return Res(&Type{Kind: TyUDPSocket, Name: "UdpSocket"}, TString), nil
	case "udp_send":
		if socket, d := arg(0, &Type{Kind: TyUDPSocket, Name: "UdpSocket"}); d != nil || socket.Kind != TyUDPSocket {
			if d != nil {
				return TError, d
			}
			return bad("udp_send expects UdpSocket")
		}
		if _, d := arg(1, TString); d != nil {
			return TError, d
		}
		if _, d := arg(2, TInt); d != nil {
			return TError, d
		}
		if _, d := arg(3, TBytes); d != nil {
			return TError, d
		}
		return Res(TInt, TString), nil
	case "udp_receive":
		if socket, d := arg(0, &Type{Kind: TyUDPSocket, Name: "UdpSocket"}); d != nil || socket.Kind != TyUDPSocket {
			if d != nil {
				return TError, d
			}
			return bad("udp_receive expects UdpSocket")
		}
		if _, d := arg(1, TInt); d != nil {
			return TError, d
		}
		return Res(TBytes, TString), nil
	case "udp_receive_from":
		if socket, d := arg(0, &Type{Kind: TyUDPSocket, Name: "UdpSocket"}); d != nil || socket.Kind != TyUDPSocket {
			if d != nil {
				return TError, d
			}
			return bad("udp_receive_from expects UdpSocket")
		}
		if _, d := arg(1, TInt); d != nil {
			return TError, d
		}
		return Res(TJSON, TString), nil
	case "udp_close":
		if socket, d := arg(0, &Type{Kind: TyUDPSocket, Name: "UdpSocket"}); d != nil || socket.Kind != TyUDPSocket {
			if d != nil {
				return TError, d
			}
			return bad("udp_close expects UdpSocket")
		}
		return TNil, nil
	case "ffi_library_open":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		return Res(&Type{Kind: TyFFILibrary, Name: "FFILibrary"}, TString), nil
	case "ffi_thread_pin", "ffi_thread_unpin":
		return TNil, nil
	case "ffi_symbol":
		if library, d := arg(0, &Type{Kind: TyFFILibrary, Name: "FFILibrary"}); d != nil || library.Kind != TyFFILibrary {
			if d != nil {
				return TError, d
			}
			return bad("ffi_symbol expects FFILibrary")
		}
		if _, d := arg(1, TString); d != nil {
			return TError, d
		}
		return Res(&Type{Kind: TyFFISymbol, Name: "FFISymbol"}, TString), nil
	case "ffi_call":
		if symbol, d := arg(0, &Type{Kind: TyFFISymbol, Name: "FFISymbol"}); d != nil || symbol.Kind != TyFFISymbol {
			if d != nil {
				return TError, d
			}
			return bad("ffi_call expects FFISymbol")
		}
		if _, d := arg(1, TString); d != nil {
			return TError, d
		}
		args, d := arg(2, Arr(TInt))
		if d != nil {
			return TError, d
		}
		if !typeEqual(args, Arr(TInt)) {
			return bad("ffi_call expects Array[Int] arguments")
		}
		return Res(TInt, TString), nil
	case "ffi_buffer_new":
		if _, d := arg(0, TBytes); d != nil {
			return TError, d
		}
		return &Type{Kind: TyFFIBuffer, Name: "FFIBuffer"}, nil
	case "ffi_buffer_new_sized":
		if _, d := arg(0, TInt); d != nil {
			return TError, d
		}
		return Res(&Type{Kind: TyFFIBuffer, Name: "FFIBuffer"}, TString), nil
	case "ffi_buffer_address":
		if buffer, d := arg(0, &Type{Kind: TyFFIBuffer, Name: "FFIBuffer"}); d != nil || buffer.Kind != TyFFIBuffer {
			if d != nil {
				return TError, d
			}
			return bad("ffi_buffer_address expects FFIBuffer")
		}
		return Res(TInt, TString), nil
	case "ffi_buffer_read":
		if buffer, d := arg(0, &Type{Kind: TyFFIBuffer, Name: "FFIBuffer"}); d != nil || buffer.Kind != TyFFIBuffer {
			if d != nil {
				return TError, d
			}
			return bad("ffi_buffer_read expects FFIBuffer")
		}
		return Res(TBytes, TString), nil
	case "ffi_buffer_close":
		if buffer, d := arg(0, &Type{Kind: TyFFIBuffer, Name: "FFIBuffer"}); d != nil || buffer.Kind != TyFFIBuffer {
			if d != nil {
				return TError, d
			}
			return bad("ffi_buffer_close expects FFIBuffer")
		}
		return TNil, nil
	case "ffi_library_close":
		if library, d := arg(0, &Type{Kind: TyFFILibrary, Name: "FFILibrary"}); d != nil || library.Kind != TyFFILibrary {
			if d != nil {
				return TError, d
			}
			return bad("ffi_library_close expects FFILibrary")
		}
		return TNil, nil
	case "process_list":
		return Res(TJSON, TString), nil
	case "process_info":
		if _, d := arg(0, TInt); d != nil {
			return TError, d
		}
		return Res(TJSON, TString), nil
	case "geocode_ip":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		return Res(TJSON, TString), nil
	case "screen_display_count":
		return Res(TInt, TString), nil
	case "screen_display_bounds":
		if _, d := arg(0, TInt); d != nil {
			return TError, d
		}
		return Res(TJSON, TString), nil
	case "screen_capture":
		for i := 0; i < 4; i++ {
			if _, d := arg(i, TInt); d != nil {
				return TError, d
			}
		}
		return Res(TBytes, TString), nil
	case "screen_capture_display":
		if _, d := arg(0, TInt); d != nil {
			return TError, d
		}
		return Res(TBytes, TString), nil
	case "camera_capture":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		if _, d := arg(1, TInt); d != nil {
			return TError, d
		}
		if _, d := arg(2, TInt); d != nil {
			return TError, d
		}
		return Res(TBytes, TString), nil
	case "win_input_read":
		if _, d := arg(0, TString); d != nil {
			return TError, d
		}
		if _, d := arg(1, TInt); d != nil {
			return TError, d
		}
		return Res(TJSON, TString), nil
	}
	return TError, Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "builtin '%s' is not implemented", b.Name)
}
func mustResolve(e *TypeEnv, s *TypeSpec) *Type { t, _ := resolveSpec(e, s, 0); return t }

// isStaticPolyHandler confines runtime polymorphism to the declared handler
// ABI. Function names are not values in Kryndel, so the name must be a literal
// and resolve to one unambiguous top-level function with signature String->String.
func isStaticPolyHandler(c *Checker, name *Expr) bool {
	if c == nil || c.Env == nil || name == nil || name.Kind != ExString {
		return false
	}
	candidates := c.Env.Overloads[name.Str]
	if len(candidates) != 1 {
		return false
	}
	f := candidates[0]
	if f.Receiver != nil || len(f.TypeParams) != 0 || len(f.Params) != 1 {
		return false
	}
	param, err := resolveSpec(c.Env, f.Params[0].Type, 0)
	if err != nil || !typeEqual(param, TString) {
		return false
	}
	result, err := resolveSpec(c.Env, f.Return, 0)
	return err == nil && typeEqual(result, TString)
}
