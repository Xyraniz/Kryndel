package kry

import "testing"

func TestQuestionMarkOnlyRequiresCompatibleErrorType(t *testing.T) {
	source := `
fn number() -> Result[Int, String] { return ok(7) }
fn no_number() -> Result[Int, String] { return err("missing") }
fn render_result() -> Result[String, String] {
    let value: Int = number()?
    return ok(str(value))
}
fn propagate_result_error() -> Result[String, String] {
    let value: Int = no_number()?
    return ok(str(value))
}
fn optional_number() -> Option[Int] { return some(8) }
fn no_optional_number() -> Option[Int] { return none() }
fn render_option() -> Option[String] {
    let value: Int = optional_number()?
    return some(str(value))
}
fn propagate_option_none() -> Option[String] {
    let value: Int = no_optional_number()?
    return some(str(value))
}
fn main() -> Nil {
    assert_eq(result_unwrap(render_result()), "7")
    let failed: Result[String, String] = propagate_result_error()
    assert_eq(is_err(failed), true)
    assert_eq(unwrap_or(result_error(failed), ""), "missing")
    assert_eq(unwrap_or(render_option(), ""), "8")
    assert_eq(is_none(propagate_option_none()), true)
    return nil
}
main()
`
	p, checker := testProgram(t, source)
	runtime, diagnostic := NewRuntime(p, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatalf("compatible Option/Result propagation failed: %s", diagnostic.Message)
	}

	bad, diagnostic := Parse(&Source{Name: "incompatible_propagation.kry", Text: `
fn string_error() -> Result[Int, String] { return err("failed") }
fn integer_error() -> Result[String, Int] {
    let value: Int = string_error()?
    return ok(str(value))
}
`}, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if _, diagnostic := Check(bad, DefaultLimits()); diagnostic == nil {
		t.Fatal("? accepted incompatible Result error types")
	}
}
