package kry

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// cgen translates a checked Kryndel program into C source that links against
// the embedded runtime in cruntime.go. The generated C is compiled by the host
// C compiler (gcc/clang) or a cross compiler (x86_64-w64-mingw32-gcc) to
// produce real, runnable PE/ELF executables.
//
// The translation mirrors the interpreter's semantics exactly: values are
// immutable and arena-allocated, arithmetic is checked, display is
// deterministic, and unsupported constructs are rejected with a clear
// diagnostic instead of silently degrading.
type cgen struct {
	prog    *Program
	env     *TypeEnv
	buf     strings.Builder
	tmp     int
	structs []*StructDecl
	enums   []*EnumDecl
	// structID maps a struct name to its runtime type id.
	structID map[string]int
	enumID   map[string]int
	// fnName maps a resolved function to its generated C symbol.
	fnName map[*Function]string
	// unsupported records the first construct the backend cannot lower.
	unsupported string
	// deferBases tracks the runtime defer-stack depth at each enclosing block.
	deferBases []string
	// fnBase is the defer-stack depth at function entry.
	fnBase string
	// loopBases tracks the defer-stack depth at each enclosing loop.
	loopBases []string
}

// GenerateC lowers a checked program to C source. It returns an error when the
// program uses a construct the native backend does not support, so callers can
// surface an honest diagnostic rather than emitting a broken binary.
func GenerateC(p *Program, c *Checker) (string, error) {
	if p == nil || c == nil {
		return "", fmt.Errorf("missing checked program")
	}
	g := &cgen{
		prog:     p,
		env:      c.Env,
		structID: map[string]int{},
		enumID:   map[string]int{},
		fnName:   map[*Function]string{},
	}
	g.structs = append(g.structs, p.Structs...)
	g.enums = append(g.enums, p.Enums...)
	for i, s := range g.structs {
		g.structID[s.Name] = i
	}
	for i, e := range g.enums {
		g.enumID[e.Name] = i
	}
	for _, f := range p.Functions {
		g.fnName[f] = g.symbol(f)
	}

	g.emitRuntime()
	g.emitMetadata()
	g.emitFunctions()
	g.emitMain()
	if g.unsupported != "" {
		return "", fmt.Errorf("%s", g.unsupported)
	}
	return g.buf.String(), nil
}

func (g *cgen) symbol(f *Function) string {
	name := sanitize(f.Name)
	if f.Receiver != nil {
		name = sanitize(TypeSpecString(f.Receiver)) + "_" + name
	}
	return "kfn_" + name
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func (g *cgen) next() string {
	g.tmp++
	return fmt.Sprintf("_t%d", g.tmp)
}

func (g *cgen) fail(format string, args ...any) {
	if g.unsupported == "" {
		g.unsupported = fmt.Sprintf(format, args...)
	}
}

func (g *cgen) emitRuntime() {
	g.buf.WriteString(cRuntimePrelude)
	g.buf.WriteString(cRuntimeDisplay)
	g.buf.WriteString(cRuntimeBuiltins)
	g.buf.WriteString(cRuntimeExtra)
	g.buf.WriteString("\n/* ---- generated program ------------------------------------------------ */\n")
}

func (g *cgen) emitMetadata() {
	for i, s := range g.structs {
		fmt.Fprintf(&g.buf, "static const char *k_sf_%d[] = {", i)
		for j, f := range s.Fields {
			if j > 0 {
				g.buf.WriteString(", ")
			}
			g.buf.WriteString(cString(f.Name))
		}
		g.buf.WriteString("};\n")
	}
	if len(g.structs) == 0 {
		g.buf.WriteString("static const char *k_sf_empty[] = {0};\n")
	}
	g.buf.WriteString("static KStructDesc k_structs_data[] = {\n")
	for i, s := range g.structs {
		fmt.Fprintf(&g.buf, "  {%s, %d, k_sf_%d},\n", cString(s.Name), len(s.Fields), i)
	}
	if len(g.structs) == 0 {
		g.buf.WriteString("  {\"\", 0, k_sf_empty},\n")
	}
	g.buf.WriteString("};\n")

	for i, e := range g.enums {
		fmt.Fprintf(&g.buf, "static const char *k_ev_%d[] = {", i)
		for j, v := range e.Variants {
			if j > 0 {
				g.buf.WriteString(", ")
			}
			g.buf.WriteString(cString(v))
		}
		g.buf.WriteString("};\n")
	}
	if len(g.enums) == 0 {
		g.buf.WriteString("static const char *k_ev_empty[] = {0};\n")
	}
	g.buf.WriteString("static KEnumDesc k_enums_data[] = {\n")
	for i, e := range g.enums {
		fmt.Fprintf(&g.buf, "  {%s, %d, k_ev_%d},\n", cString(e.Name), len(e.Variants), i)
	}
	if len(g.enums) == 0 {
		g.buf.WriteString("  {\"\", 0, k_ev_empty},\n")
	}
	g.buf.WriteString("};\n")
}

func (g *cgen) emitFunctions() {
	for _, f := range g.prog.Functions {
		g.emitFunction(f)
	}
}

func (g *cgen) emitFunction(f *Function) {
	name := g.fnName[f]
	fmt.Fprintf(&g.buf, "static KValue %s(void) {\n", name)
	// Parameters are passed through a global argument frame so that the
	// generated C stays simple and recursion works without prototypes.
	fmt.Fprintf(&g.buf, "  int _fb = k_ndefers;\n")
	g.fnBase = "_fb"
	g.deferBases = nil
	g.loopBases = nil
	if f.Receiver != nil {
		fmt.Fprintf(&g.buf, "  KValue self = k_args[0];\n")
	}
	for i, p := range f.Params {
		off := i
		if f.Receiver != nil {
			off = i + 1
		}
		fmt.Fprintf(&g.buf, "  KValue %s = k_args[%d];\n", sanitize(p.Name), off)
	}
	g.block(f.Body, "  ")
	fmt.Fprintf(&g.buf, "  return kv_nil();\n}\n")
}

func (g *cgen) emitMain() {
	g.buf.WriteString("int main(void) {\n")
	g.buf.WriteString("  k_structs = k_structs_data;\n")
	g.buf.WriteString("  k_enums = k_enums_data;\n")
	g.buf.WriteString("  if (setjmp(k_jmp)) { fflush(stdout); fprintf(stderr, \"kryndel: %s\\n\", k_errbuf); return 1; }\n")
	g.buf.WriteString("  int _fb = k_ndefers;\n")
	g.fnBase = "_fb"
	g.deferBases = nil
	g.loopBases = nil
	g.block(g.prog.Statements, "  ")
	if len(g.prog.Statements) == 0 {
		if f := g.env.Functions["main"]; f != nil {
			fmt.Fprintf(&g.buf, "  { k_args[0] = kv_nil(); KValue _r = %s(); (void)_r; }\n", g.fnName[f])
		}
	}
	g.buf.WriteString("  while (k_ndefers > _fb) k_defers[--k_ndefers]();\n")
	g.buf.WriteString("  fflush(stdout);\n  return 0;\n}\n")
}

// block emits a sequence of statements inside a new C scope, running any
// registered defers when the scope exits.
func (g *cgen) block(body []*Stmt, indent string) {
	base := g.next()
	fmt.Fprintf(&g.buf, "%s{ int %s = k_ndefers;\n", indent, base)
	g.deferBases = append(g.deferBases, base)
	for _, s := range body {
		g.stmt(s, indent+"  ")
	}
	fmt.Fprintf(&g.buf, "%s  while (k_ndefers > %s) k_defers[--k_ndefers]();\n", indent, base)
	fmt.Fprintf(&g.buf, "%s}\n", indent)
	g.deferBases = g.deferBases[:len(g.deferBases)-1]
}

func (g *cgen) stmt(s *Stmt, indent string) {
	if s == nil {
		return
	}
	switch s.Kind {
	case StLet, StConst:
		fmt.Fprintf(&g.buf, "%sKValue %s = %s;\n", indent, sanitize(s.Name), g.expr(s.Init))
	case StAssign:
		if s.Target == nil || s.Target.Kind != ExVar {
			g.fail("assignment target must be a binding")
			return
		}
		fmt.Fprintf(&g.buf, "%s%s = %s;\n", indent, sanitize(s.Target.Name), g.expr(s.Value))
	case StExpr:
		fmt.Fprintf(&g.buf, "%s{ KValue _e = %s; (void)_e; }\n", indent, g.expr(s.Expr))
	case StIf:
		cond := g.next()
		fmt.Fprintf(&g.buf, "%s{ KValue %s = %s; if (%s.tag != K_BOOL) kfail(\"condition must be Bool\");\n", indent, cond, g.expr(s.Cond), cond)
		fmt.Fprintf(&g.buf, "%s  if (%s.u.b) {\n", indent, cond)
		g.block(s.Then, indent+"    ")
		if len(s.Else) > 0 {
			fmt.Fprintf(&g.buf, "%s  } else {\n", indent)
			g.block(s.Else, indent+"    ")
		}
		fmt.Fprintf(&g.buf, "%s  }\n%s}\n", indent, indent)
	case StWhile:
		lb := g.next()
		fmt.Fprintf(&g.buf, "%s{ int %s = k_ndefers;\n", indent, lb)
		g.loopBases = append(g.loopBases, lb)
		fmt.Fprintf(&g.buf, "%s  while (1) {\n", indent)
		cond := g.next()
		fmt.Fprintf(&g.buf, "%s    KValue %s = %s; if (%s.tag != K_BOOL) kfail(\"condition must be Bool\"); if (!%s.u.b) break;\n", indent, cond, g.expr(s.Cond), cond, cond)
		g.block(s.Body, indent+"    ")
		fmt.Fprintf(&g.buf, "%s  }\n", indent)
		g.loopBases = g.loopBases[:len(g.loopBases)-1]
		fmt.Fprintf(&g.buf, "%s}\n", indent)
	case StFor:
		lb := g.next()
		it := g.next()
		idx := g.next()
		fmt.Fprintf(&g.buf, "%s{ int %s = k_ndefers;\n", indent, lb)
		g.loopBases = append(g.loopBases, lb)
		fmt.Fprintf(&g.buf, "%s  KValue %s = k_iter_items(%s);\n", indent, it, g.expr(s.Iter))
		fmt.Fprintf(&g.buf, "%s  for (size_t %s = 0; %s < %s.u.a.len; %s++) {\n", indent, idx, idx, it, idx)
		fmt.Fprintf(&g.buf, "%s    KValue %s = %s.u.a.items[%s];\n", indent, sanitize(s.Name), it, idx)
		g.block(s.Body, indent+"    ")
		fmt.Fprintf(&g.buf, "%s  }\n", indent)
		g.loopBases = g.loopBases[:len(g.loopBases)-1]
		fmt.Fprintf(&g.buf, "%s}\n", indent)
	case StReturn:
		g.emitReturn(s, indent)
	case StBreak:
		g.emitUnwind(indent)
		fmt.Fprintf(&g.buf, "%sbreak;\n", indent)
	case StContinue:
		g.emitUnwind(indent)
		fmt.Fprintf(&g.buf, "%scontinue;\n", indent)
	case StMatch:
		g.emitMatch(s, indent)
	case StDefer:
		g.emitDefer(s, indent)
	case StUnsafe:
		g.block(s.Body, indent)
	default:
		g.fail("unsupported statement kind %d", s.Kind)
	}
}

func (g *cgen) emitUnwind(indent string) {
	if len(g.loopBases) == 0 {
		return
	}
	base := g.loopBases[len(g.loopBases)-1]
	fmt.Fprintf(&g.buf, "%swhile (k_ndefers > %s) k_defers[--k_ndefers]();\n", indent, base)
}

func (g *cgen) emitReturn(s *Stmt, indent string) {
	fmt.Fprintf(&g.buf, "%swhile (k_ndefers > %s) k_defers[--k_ndefers]();\n", indent, g.fnBase)
	if s.Return == nil {
		fmt.Fprintf(&g.buf, "%sreturn kv_nil();\n", indent)
		return
	}
	// `return expr?` where the function returns Option/Result yields the
	// operand itself: some(x) wrapped is identical to the original Option.
	if s.Return.Kind == ExPropagate {
		fmt.Fprintf(&g.buf, "%sreturn %s;\n", indent, g.expr(s.Return.Operand))
		return
	}
	fmt.Fprintf(&g.buf, "%sreturn %s;\n", indent, g.expr(s.Return))
}

func (g *cgen) emitDefer(s *Stmt, indent string) {
	name := "k_defer_" + g.next()
	fmt.Fprintf(&g.buf, "%svoid %s(void) {\n", indent, name)
	g.block(s.Body, indent+"  ")
	fmt.Fprintf(&g.buf, "%s}\n", indent)
	fmt.Fprintf(&g.buf, "%sk_defers[k_ndefers++] = %s;\n", indent, name)
}

func (g *cgen) emitMatch(s *Stmt, indent string) {
	scrut := g.next()
	fmt.Fprintf(&g.buf, "%s{ KValue %s = %s;\n", indent, scrut, g.expr(s.Scrutinee))
	for i, arm := range s.Arms {
		cond := g.patternCond(arm.Pattern, scrut)
		if i == 0 {
			fmt.Fprintf(&g.buf, "%s  if (%s) {\n", indent, cond)
		} else {
			fmt.Fprintf(&g.buf, "%s  else if (%s) {\n", indent, cond)
		}
		if arm.Pattern.Binding != "" {
			inner := "*(%s.u.opt.inner)"
			if arm.Pattern.Kind == PatResult {
				inner = "*(%s.u.res.inner)"
			}
			fmt.Fprintf(&g.buf, "%s    KValue %s = "+inner+";\n", indent, sanitize(arm.Pattern.Binding), scrut)
		}
		g.block(arm.Body, indent+"    ")
		fmt.Fprintf(&g.buf, "%s  }\n", indent)
	}
	fmt.Fprintf(&g.buf, "%s  else { kfail(\"no match arm matched\"); }\n", indent)
	fmt.Fprintf(&g.buf, "%s}\n", indent)
}

func (g *cgen) patternCond(p Pattern, scrut string) string {
	switch p.Kind {
	case PatWildcard:
		return "1"
	case PatNil:
		return fmt.Sprintf("(%s.tag == K_NIL || (%s.tag == K_OPTION && !%s.u.opt.present))", scrut, scrut, scrut)
	case PatBool:
		v := "0"
		if p.Bool {
			v = "1"
		}
		return fmt.Sprintf("(%s.tag == K_BOOL && %s.u.b == %s)", scrut, scrut, v)
	case PatInt:
		return fmt.Sprintf("(%s.tag == K_INT && %s.u.i == %dLL)", scrut, scrut, p.Int)
	case PatString:
		return fmt.Sprintf("(%s.tag == K_STRING && %s.u.s.len == %d && memcmp(%s.u.s.data, %s, %d) == 0)",
			scrut, scrut, len(p.Str), scrut, cString(p.Str), len(p.Str))
	case PatEnum:
		if p.TypeName != "" {
			id, ok := g.enumID[p.TypeName]
			if !ok {
				g.fail("unknown enum '%s'", p.TypeName)
				return "0"
			}
			v := g.variantIndex(p.TypeName, p.Variant)
			return fmt.Sprintf("(%s.tag == K_ENUM && %s.u.en.type_id == %d && %s.u.en.variant == %d)", scrut, scrut, id, scrut, v)
		}
		return fmt.Sprintf("(%s.tag == K_ENUM && %s.u.en.variant == %d)", scrut, scrut, g.variantIndexAny(p.Variant))
	case PatOption:
		v := "0"
		if p.Present {
			v = "1"
		}
		return fmt.Sprintf("(%s.tag == K_OPTION && %s.u.opt.present == %s)", scrut, scrut, v)
	case PatResult:
		v := "0"
		if p.OK {
			v = "1"
		}
		return fmt.Sprintf("(%s.tag == K_RESULT && %s.u.res.ok == %s)", scrut, scrut, v)
	}
	return "0"
}

func (g *cgen) variantIndex(enumName, variant string) int {
	for _, e := range g.enums {
		if e.Name == enumName {
			for i, v := range e.Variants {
				if v == variant {
					return i
				}
			}
		}
	}
	g.fail("unknown variant '%s::%s'", enumName, variant)
	return 0
}

func (g *cgen) variantIndexAny(variant string) int {
	for _, e := range g.enums {
		for i, v := range e.Variants {
			if v == variant {
				return i
			}
		}
	}
	g.fail("unknown variant '%s'", variant)
	return 0
}

// expr lowers an expression to a C expression of type KValue.
func (g *cgen) expr(e *Expr) string {
	if e == nil {
		return "kv_nil()"
	}
	switch e.Kind {
	case ExInt:
		return fmt.Sprintf("kv_int(%dLL)", e.Int)
	case ExFloat:
		return fmt.Sprintf("kv_float(%s)", cFloat(e.Float))
	case ExBool:
		if e.Bool {
			return "kv_bool(1)"
		}
		return "kv_bool(0)"
	case ExNil:
		return "kv_nil()"
	case ExString:
		return fmt.Sprintf("kv_strn(%s, %d)", cString(e.Str), len(e.Str))
	case ExVar:
		return sanitize(e.Name)
	case ExEnum:
		id, ok := g.enumID[e.EnumType]
		if !ok {
			g.fail("unknown enum '%s'", e.EnumType)
			return "kv_nil()"
		}
		return fmt.Sprintf("kv_enum(%d, %d)", id, g.variantIndex(e.EnumType, e.EnumVariant))
	case ExArray:
		return g.arrayLiteral(e.Items)
	case ExSet:
		return g.setLiteral(e.Items)
	case ExMap:
		return g.mapLiteral(e)
	case ExStruct:
		return g.structLiteral(e)
	case ExUnary:
		return g.unary(e)
	case ExBinary:
		return g.binary(e)
	case ExIndex:
		return fmt.Sprintf("k_index(%s, %s)", g.expr(e.Base), g.expr(e.Left))
	case ExField:
		return g.field(e)
	case ExPropagate:
		return g.propagate(e)
	case ExCall:
		return g.call(e)
	}
	g.fail("unsupported expression kind %d", e.Kind)
	return "kv_nil()"
}

func (g *cgen) arrayLiteral(items []*Expr) string {
	if len(items) == 0 {
		return "kv_arr((KValue*)kalloc(1), 0)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "({ KValue *_a = (KValue*)kalloc(sizeof(KValue)*%d);", len(items))
	for i, it := range items {
		fmt.Fprintf(&b, " _a[%d] = %s;", i, g.expr(it))
	}
	fmt.Fprintf(&b, " kv_arr(_a, %d); })", len(items))
	return b.String()
}

func (g *cgen) setLiteral(items []*Expr) string {
	if len(items) == 0 {
		return "kv_set((KValue*)kalloc(1), 0)"
	}
	// Deduplicate at runtime to match the interpreter's set semantics.
	var b strings.Builder
	fmt.Fprintf(&b, "({ KValue *_s = (KValue*)kalloc(sizeof(KValue)*%d); size_t _n = 0;", len(items))
	for _, it := range items {
		v := g.next()
		f := g.next()
		i := g.next()
		fmt.Fprintf(&b, " KValue %s = %s; int %s = 0; for (size_t %s=0;%s<_n;%s++) if (k_equal(_s[%s],%s)) %s=1; if (!%s) _s[_n++] = %s;", v, g.expr(it), f, i, i, i, i, v, f, f, v)
	}
	fmt.Fprintf(&b, " kv_set(_s, _n); })")
	return b.String()
}

func (g *cgen) mapLiteral(e *Expr) string {
	if len(e.MapKeys) == 0 {
		return "kv_map((KValue*)kalloc(1), (KValue*)kalloc(1), 0)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "({ KValue *_k = (KValue*)kalloc(sizeof(KValue)*%d); KValue *_v = (KValue*)kalloc(sizeof(KValue)*%d); size_t _n = 0;", len(e.MapKeys), len(e.MapKeys))
	for i := range e.MapKeys {
		kv := g.next()
		vv := g.next()
		fmt.Fprintf(&b, " KValue %s = %s; KValue %s = %s;", kv, g.expr(e.MapKeys[i]), vv, g.expr(e.Values[i]))
		fmt.Fprintf(&b, " for (size_t _i=0;_i<_n;_i++) if (k_equal(_k[_i],%s)) kfail(\"duplicate map key\");", kv)
		fmt.Fprintf(&b, " _k[_n] = %s; _v[_n] = %s; _n++;", kv, vv)
	}
	fmt.Fprintf(&b, " kv_map(_k, _v, _n); })")
	return b.String()
}

func (g *cgen) structLiteral(e *Expr) string {
	decl := g.structDecl(e.StructName)
	if decl == nil {
		g.fail("unknown struct '%s'", e.StructName)
		return "kv_nil()"
	}
	id := g.structID[e.StructName]
	var b strings.Builder
	fmt.Fprintf(&b, "({ KValue *_f = (KValue*)kalloc(sizeof(KValue)*%d);", len(decl.Fields))
	for i := range decl.Fields {
		fmt.Fprintf(&b, " _f[%d] = kv_nil();", i)
	}
	for i, name := range e.Fields {
		idx := -1
		for j, f := range decl.Fields {
			if f.Name == name {
				idx = j
			}
		}
		if idx < 0 {
			g.fail("unknown field '%s'", name)
			continue
		}
		fmt.Fprintf(&b, " _f[%d] = %s;", idx, g.expr(e.Values[i]))
	}
	fmt.Fprintf(&b, " kv_struct(%d, _f); })", id)
	return b.String()
}

func (g *cgen) structDecl(name string) *StructDecl {
	for _, s := range g.structs {
		if s.Name == name {
			return s
		}
	}
	return nil
}

func (g *cgen) unary(e *Expr) string {
	op := g.expr(e.Operand)
	switch e.Op {
	case BANG:
		return fmt.Sprintf("k_not(%s)", op)
	case MINUS:
		return fmt.Sprintf("k_neg(%s)", op)
	case PLUS:
		return op
	}
	g.fail("unsupported unary operator")
	return "kv_nil()"
}

func (g *cgen) binary(e *Expr) string {
	l := g.expr(e.Left)
	r := g.expr(e.Right)
	switch e.Op {
	case AND:
		return fmt.Sprintf("k_and(%s, %s)", l, r)
	case OR:
		return fmt.Sprintf("k_or(%s, %s)", l, r)
	case EQEQ:
		return fmt.Sprintf("k_eq(%s, %s)", l, r)
	case NEQ:
		return fmt.Sprintf("k_neq(%s, %s)", l, r)
	case PLUS:
		return fmt.Sprintf("k_add(%s, %s)", l, r)
	case MINUS:
		return fmt.Sprintf("k_sub(%s, %s)", l, r)
	case STAR:
		return fmt.Sprintf("k_mul(%s, %s)", l, r)
	case SLASH:
		return fmt.Sprintf("k_div(%s, %s)", l, r)
	case PERCENT:
		return fmt.Sprintf("k_rem(%s, %s)", l, r)
	case LESS:
		return fmt.Sprintf("k_lt(%s, %s)", l, r)
	case LEQ:
		return fmt.Sprintf("k_le(%s, %s)", l, r)
	case GREATER:
		return fmt.Sprintf("k_gt(%s, %s)", l, r)
	case GEQ:
		return fmt.Sprintf("k_ge(%s, %s)", l, r)
	}
	g.fail("unsupported binary operator")
	return "kv_nil()"
}

func (g *cgen) field(e *Expr) string {
	base := g.expr(e.Base)
	// Resolve the field index from the base expression's static type.
	var decl *StructDecl
	if e.Base != nil && e.Base.Type != nil && e.Base.Type.Struct != nil {
		decl = e.Base.Type.Struct
	}
	if decl == nil {
		g.fail("field access requires a known struct type")
		return "kv_nil()"
	}
	idx := -1
	for i, f := range decl.Fields {
		if f.Name == e.Field {
			idx = i
		}
	}
	if idx < 0 {
		g.fail("unknown field '%s'", e.Field)
		return "kv_nil()"
	}
	return fmt.Sprintf("k_field(%s, %d)", base, idx)
}

func (g *cgen) propagate(e *Expr) string {
	operand := g.expr(e.Operand)
	return fmt.Sprintf("({ KValue _p = %s; if (_p.tag == K_OPTION) { if (!_p.u.opt.present) return _p; _p = *_p.u.opt.inner; } else if (_p.tag == K_RESULT) { if (!_p.u.res.ok) return _p; _p = *_p.u.res.inner; } else kfail(\"'?' requires an Option or Result\"); _p; })", operand)
}

func (g *cgen) call(e *Expr) string {
	if e.Receiver != nil {
		return g.methodCall(e)
	}
	if b, ok := g.env.Builtins[e.Name]; ok {
		return g.builtinCall(e, b)
	}
	f := e.Function
	if f == nil {
		f = g.env.Functions[e.Name]
	}
	if f == nil {
		g.fail("unknown function '%s'", e.Name)
		return "kv_nil()"
	}
	return g.functionCall(f, nil, e.Args)
}

func (g *cgen) methodCall(e *Expr) string {
	f := e.Function
	if f == nil {
		g.fail("unknown method '%s'", e.Name)
		return "kv_nil()"
	}
	return g.functionCall(f, e.Receiver, e.Args)
}

func (g *cgen) functionCall(f *Function, receiver *Expr, args []*Expr) string {
	name := g.fnName[f]
	if name == "" {
		g.fail("unknown function '%s'", f.Name)
		return "kv_nil()"
	}
	var b strings.Builder
	b.WriteString("({ KValue _frame[K_MAX_ARGS];")
	off := 0
	if receiver != nil {
		fmt.Fprintf(&b, " _frame[0] = %s;", g.expr(receiver))
		off = 1
	}
	for i, a := range args {
		fmt.Fprintf(&b, " _frame[%d] = %s;", i+off, g.expr(a))
	}
	fmt.Fprintf(&b, " memcpy(k_args, _frame, sizeof(KValue)*%d); %s(); })", off+len(args), name)
	return b.String()
}

// builtinCall lowers a builtin invocation. Unsupported builtins are rejected
// with a clear diagnostic so the native backend never silently misbehaves.
func (g *cgen) builtinCall(e *Expr, b Builtin) string {
	arg := func(i int) string { return g.expr(e.Args[i]) }
	switch b.Name {
	case "print":
		return fmt.Sprintf("({ k_print(%s, 0); kv_nil(); })", arg(0))
	case "println":
		return fmt.Sprintf("({ k_print(%s, 1); kv_nil(); })", arg(0))
	case "len":
		return fmt.Sprintf("k_len(%s)", arg(0))
	case "bytes":
		return fmt.Sprintf("k_bytes(%s)", arg(0))
	case "string_to_bytes":
		return fmt.Sprintf("k_string_to_bytes(%s)", arg(0))
	case "bytes_to_string":
		return fmt.Sprintf("k_bytes_to_string(%s)", arg(0))
	case "array_push":
		return fmt.Sprintf("k_array_push(%s, %s)", arg(0), arg(1))
	case "array_pop":
		return fmt.Sprintf("k_array_pop(%s)", arg(0))
	case "array_get":
		return fmt.Sprintf("k_array_get(%s, %s)", arg(0), arg(1))
	case "array_concat":
		return fmt.Sprintf("k_array_concat(%s, %s)", arg(0), arg(1))
	case "array_contains":
		return fmt.Sprintf("k_array_contains(%s, %s)", arg(0), arg(1))
	case "array_slice":
		return fmt.Sprintf("k_array_slice(%s, %s, %s)", arg(0), arg(1), arg(2))
	case "array_reverse":
		return fmt.Sprintf("k_array_reverse(%s)", arg(0))
	case "array_join":
		return fmt.Sprintf("k_array_join(%s, %s)", arg(0), arg(1))
	case "int":
		return fmt.Sprintf("k_to_int(%s)", arg(0))
	case "float":
		return fmt.Sprintf("k_to_float(%s)", arg(0))
	case "str":
		return fmt.Sprintf("k_str(%s)", arg(0))
	case "bool":
		return fmt.Sprintf("k_bool(%s)", arg(0))
	case "assert":
		return fmt.Sprintf("k_assert(%s)", arg(0))
	case "assert_eq":
		return fmt.Sprintf("k_assert_eq(%s, %s)", arg(0), arg(1))
	case "abs":
		return fmt.Sprintf("k_abs(%s)", arg(0))
	case "sqrt":
		return fmt.Sprintf("k_sqrt(%s)", arg(0))
	case "min":
		return fmt.Sprintf("k_min(%s, %s)", arg(0), arg(1))
	case "max":
		return fmt.Sprintf("k_max(%s, %s)", arg(0), arg(1))
	case "floor":
		return fmt.Sprintf("k_floor(%s)", arg(0))
	case "ceil":
		return fmt.Sprintf("k_ceil(%s)", arg(0))
	case "round":
		return fmt.Sprintf("k_round(%s)", arg(0))
	case "pow":
		return fmt.Sprintf("k_pow(%s, %s)", arg(0), arg(1))
	case "log":
		return fmt.Sprintf("k_log(%s)", arg(0))
	case "sin":
		return fmt.Sprintf("k_sin(%s)", arg(0))
	case "cos":
		return fmt.Sprintf("k_cos(%s)", arg(0))
	case "is_nan":
		return fmt.Sprintf("k_is_nan(%s)", arg(0))
	case "is_finite":
		return fmt.Sprintf("k_is_finite(%s)", arg(0))
	case "is_some":
		return fmt.Sprintf("k_is_some(%s)", arg(0))
	case "is_none":
		return fmt.Sprintf("k_is_none(%s)", arg(0))
	case "is_ok":
		return fmt.Sprintf("k_is_ok(%s)", arg(0))
	case "is_err":
		return fmt.Sprintf("k_is_err(%s)", arg(0))
	case "unwrap_or":
		return fmt.Sprintf("k_unwrap_or(%s, %s)", arg(0), arg(1))
	case "some":
		return fmt.Sprintf("k_some(%s)", arg(0))
	case "none":
		return "k_none()"
	case "ok":
		return fmt.Sprintf("k_ok(%s)", arg(0))
	case "err":
		return fmt.Sprintf("k_err(%s)", arg(0))
	case "substring":
		return fmt.Sprintf("k_substring(%s, %s, %s)", arg(0), arg(1), arg(2))
	case "contains":
		return fmt.Sprintf("k_contains(%s, %s)", arg(0), arg(1))
	case "starts_with":
		return fmt.Sprintf("k_starts_with(%s, %s)", arg(0), arg(1))
	case "ends_with":
		return fmt.Sprintf("k_ends_with(%s, %s)", arg(0), arg(1))
	case "trim":
		return fmt.Sprintf("k_trim(%s)", arg(0))
	case "split":
		return fmt.Sprintf("k_split(%s, %s)", arg(0), arg(1))
	case "replace":
		return fmt.Sprintf("k_replace(%s, %s, %s)", arg(0), arg(1), arg(2))
	case "codepoints":
		return fmt.Sprintf("k_codepoints(%s)", arg(0))
	case "byte_at":
		return fmt.Sprintf("k_byte_at(%s, %s)", arg(0), arg(1))
	case "hex_encode":
		return fmt.Sprintf("k_hex_encode(%s)", arg(0))
	case "hex_decode":
		return fmt.Sprintf("k_hex_decode(%s)", arg(0))
	case "base64_encode":
		return fmt.Sprintf("k_base64_encode(%s)", arg(0))
	case "base64_decode":
		return fmt.Sprintf("k_base64_decode(%s)", arg(0))
	case "map_get":
		return fmt.Sprintf("k_map_get(%s, %s)", arg(0), arg(1))
	case "map_insert":
		return fmt.Sprintf("k_map_insert(%s, %s, %s)", arg(0), arg(1), arg(2))
	case "map_keys":
		return fmt.Sprintf("k_map_keys(%s)", arg(0))
	case "set_contains":
		return fmt.Sprintf("k_set_contains(%s, %s)", arg(0), arg(1))
	case "set_insert":
		return fmt.Sprintf("k_set_insert(%s, %s)", arg(0), arg(1))
	case "set_len":
		return fmt.Sprintf("k_set_len(%s)", arg(0))
	case "fs_read_text":
		return fmt.Sprintf("k_fs_read_text(%s)", arg(0))
	case "fs_write_text":
		return fmt.Sprintf("k_fs_write_text(%s, %s)", arg(0), arg(1))
	case "fs_read_bytes":
		return fmt.Sprintf("k_fs_read_bytes(%s)", arg(0))
	case "fs_write_bytes":
		return fmt.Sprintf("k_fs_write_bytes(%s, %s)", arg(0), arg(1))
	case "fs_exists":
		return fmt.Sprintf("k_fs_exists(%s)", arg(0))
	case "env_get":
		return fmt.Sprintf("k_env_get(%s)", arg(0))
	case "json_parse":
		return fmt.Sprintf("k_json_parse(%s)", arg(0))
	case "json_stringify":
		return fmt.Sprintf("k_json_stringify(%s)", arg(0))
	case "crypto_sha256":
		return fmt.Sprintf("k_crypto_sha256(%s)", arg(0))
	case "crypto_hmac_sha256":
		return fmt.Sprintf("k_crypto_hmac_sha256(%s, %s)", arg(0), arg(1))
	case "crypto_random_bytes":
		return fmt.Sprintf("k_crypto_random_bytes(%s)", arg(0))
	case "fs_read_dir":
		return fmt.Sprintf("k_fs_read_dir(%s)", arg(0))
	case "fs_create_dir":
		return fmt.Sprintf("k_fs_create_dir(%s)", arg(0))
	case "fs_create_dir_all":
		return fmt.Sprintf("k_fs_create_dir_all(%s)", arg(0))
	case "fs_remove_file":
		return fmt.Sprintf("k_fs_remove_file(%s)", arg(0))
	case "fs_remove_dir_all":
		return fmt.Sprintf("k_fs_remove_dir_all(%s)", arg(0))
	case "fs_copy_file":
		return fmt.Sprintf("k_fs_copy_file(%s, %s)", arg(0), arg(1))
	case "fs_move_file":
		return fmt.Sprintf("k_fs_move_file(%s, %s)", arg(0), arg(1))
	case "fs_is_file":
		return fmt.Sprintf("k_fs_is_file(%s)", arg(0))
	case "fs_is_dir":
		return fmt.Sprintf("k_fs_is_dir(%s)", arg(0))
	case "fs_file_size":
		return fmt.Sprintf("k_fs_file_size(%s)", arg(0))
	case "fs_file_modified_time":
		return fmt.Sprintf("k_fs_file_modified_time(%s)", arg(0))
	case "fs_join_path":
		return fmt.Sprintf("k_fs_join_path(%s, %s)", arg(0), arg(1))
	case "fs_absolute_path":
		return fmt.Sprintf("k_fs_absolute_path(%s)", arg(0))
	case "fs_temp_dir":
		return "k_fs_temp_dir(kv_nil())"
	case "fs_temp_file":
		return fmt.Sprintf("k_fs_temp_file(%s)", arg(0))
	case "sleep_ms":
		return fmt.Sprintf("k_sleep(%s)", arg(0))
	case "yield_now":
		return "k_yield_now()"
	case "process_run":
		return fmt.Sprintf("k_process_run(%s, %s)", arg(0), arg(1))
	case "string_repeat":
		return fmt.Sprintf("k_string_repeat(%s, %s)", arg(0), arg(1))
	case "string_index_of":
		return fmt.Sprintf("k_string_index_of(%s, %s)", arg(0), arg(1))
	case "string_pad_start":
		return fmt.Sprintf("k_string_pad(%s, %s, %s, 1)", arg(0), arg(1), arg(2))
	case "string_pad_end":
		return fmt.Sprintf("k_string_pad(%s, %s, %s, 0)", arg(0), arg(1), arg(2))
	case "string_lines":
		return fmt.Sprintf("k_string_lines(%s)", arg(0))
	case "string_chars":
		return fmt.Sprintf("k_string_chars(%s)", arg(0))
	case "string_to_upper":
		return fmt.Sprintf("k_string_case(%s, 1)", arg(0))
	case "string_to_lower":
		return fmt.Sprintf("k_string_case(%s, 0)", arg(0))
	case "array_sort":
		return fmt.Sprintf("k_array_sort(%s)", arg(0))
	case "array_index_of":
		return fmt.Sprintf("k_array_index_of(%s, %s)", arg(0), arg(1))
	case "array_sum":
		return fmt.Sprintf("k_array_sum(%s)", arg(0))
	case "array_min":
		return fmt.Sprintf("k_array_minmax(%s, 0)", arg(0))
	case "array_max":
		return fmt.Sprintf("k_array_minmax(%s, 1)", arg(0))
	case "array_take":
		return fmt.Sprintf("k_array_take_drop(%s, %s, 1)", arg(0), arg(1))
	case "array_drop":
		return fmt.Sprintf("k_array_take_drop(%s, %s, 0)", arg(0), arg(1))
	case "map_contains_key":
		return fmt.Sprintf("k_map_contains_key(%s, %s)", arg(0), arg(1))
	case "map_values":
		return fmt.Sprintf("k_map_values(%s)", arg(0))
	case "map_remove":
		return fmt.Sprintf("k_map_remove(%s, %s)", arg(0), arg(1))
	case "set_remove":
		return fmt.Sprintf("k_set_remove(%s, %s)", arg(0), arg(1))
	case "set_to_array":
		return fmt.Sprintf("k_set_to_array(%s)", arg(0))
	case "tan":
		return fmt.Sprintf("k_tan(%s)", arg(0))
	case "atan":
		return fmt.Sprintf("k_atan(%s)", arg(0))
	case "atan2":
		return fmt.Sprintf("k_atan2(%s, %s)", arg(0), arg(1))
	case "exp":
		return fmt.Sprintf("k_exp(%s)", arg(0))
	case "log10":
		return fmt.Sprintf("k_log10(%s)", arg(0))
	case "log2":
		return fmt.Sprintf("k_log2(%s)", arg(0))
	case "trunc":
		return fmt.Sprintf("k_trunc(%s)", arg(0))
	case "sign":
		return fmt.Sprintf("k_sign(%s)", arg(0))
	case "clamp":
		return fmt.Sprintf("k_clamp(%s, %s, %s)", arg(0), arg(1), arg(2))
	}
	g.fail("builtin '%s' is not supported by the native backend", b.Name)
	return "kv_nil()"
}

// cString renders a Go string as a C string literal.
func cString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString("\\\"")
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		default:
			if c < 0x20 || c >= 0x7f {
				fmt.Fprintf(&b, "\\%03o", c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// cFloat renders a float64 as a C double literal using its exact hexadecimal
// form so the compiler reproduces the identical bit pattern.
func cFloat(f float64) string {
	// strconv.FormatFloat with 'x' yields a C99 hexadecimal floating literal
	// (e.g. 0x1.921f9f01b866ep+01) that reproduces the exact IEEE-754 bits.
	return strconv.FormatFloat(f, 'x', -1, 64)
}

// sortedStructNames is used by tests to keep metadata deterministic.
func sortedStructNames(p *Program) []string {
	names := make([]string, 0, len(p.Structs))
	for _, s := range p.Structs {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	return names
}
