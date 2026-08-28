package kry

import (
	"strconv"
	"strings"
)

type Formatter struct {
	b      strings.Builder
	indent int
}

func FormatSource(src *Source, lim Limits) (string, *Diagnostic) {
	p, d := Parse(src, lim)
	if d != nil {
		return "", d
	}
	f := &Formatter{}
	for _, imp := range p.Imports {
		f.line("import \"" + escapeFormat(imp.Path) + "\"")
	}
	for _, s := range p.Structs {
		f.structDecl(s)
	}
	for _, e := range p.Enums {
		f.enumDecl(e)
	}
	for _, fn := range p.Functions {
		f.function(fn)
	}
	for _, s := range p.Statements {
		f.stmt(s)
	}
	return strings.TrimRight(f.b.String(), "\n") + "\n", nil
}
func (f *Formatter) line(s string) {
	f.b.WriteString(strings.Repeat("    ", f.indent))
	f.b.WriteString(s)
	f.b.WriteByte('\n')
}
func escapeFormat(s string) string {
	return strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n", "\r", "\\r", "\t", "\\t").Replace(s)
}
func (f *Formatter) typeSpec(s *TypeSpec) string {
	if s == nil {
		return "Nil"
	}
	if len(s.Params) == 0 {
		return s.Name
	}
	x := s.Name + "["
	for i, p := range s.Params {
		if i > 0 {
			x += ", "
		}
		x += f.typeSpec(p)
	}
	return x + "]"
}
func (f *Formatter) structDecl(s *StructDecl) {
	pre := ""
	if s.Public {
		pre = "pub "
	}
	f.line(pre + "struct " + s.Name + " {")
	f.indent++
	for _, x := range s.Fields {
		f.line(x.Name + ": " + f.typeSpec(x.Spec) + ",")
	}
	f.indent--
	f.line("}")
	f.line("")
}
func (f *Formatter) enumDecl(e *EnumDecl) {
	pre := ""
	if e.Public {
		pre = "pub "
	}
	f.line(pre + "enum " + e.Name + " {")
	f.indent++
	for _, v := range e.Variants {
		f.line(v + ",")
	}
	f.indent--
	f.line("}")
	f.line("")
}
func (f *Formatter) function(fn *Function) {
	if fn.Receiver != nil {
		f.line("impl " + f.typeSpec(fn.Receiver) + " {")
		f.indent++
	}
	pre := ""
	if fn.Public {
		pre = "pub "
	}
	x := pre + "fn " + fn.Name + "("
	for i, p := range fn.Params {
		if i > 0 {
			x += ", "
		}
		x += p.Name + ": " + f.typeSpec(p.Type)
	}
	x += ") -> " + f.typeSpec(fn.Return) + " "
	f.line(x + "{")
	f.indent++
	for _, s := range fn.Body {
		f.stmt(s)
	}
	f.indent--
	f.line("}")
	if fn.Receiver != nil {
		f.indent--
		f.line("}")
	}
	f.line("")
}
func (f *Formatter) stmt(s *Stmt) {
	switch s.Kind {
	case StLet:
		x := "let "
		if s.Mutable {
			x += "mut "
		}
		x += s.Name
		if s.Annotation != nil {
			x += ": " + f.typeSpec(s.Annotation)
		}
		f.line(x + " = " + f.expr(s.Init))
	case StExpr:
		f.line(f.expr(s.Expr))
	case StAssign:
		f.line(f.expr(s.Target) + " = " + f.expr(s.Value))
	case StReturn:
		if s.Return == nil {
			f.line("return")
		} else {
			f.line("return " + f.expr(s.Return))
		}
	case StBreak:
		f.line("break")
	case StContinue:
		f.line("continue")
	case StIf:
		f.line("if " + f.expr(s.Cond) + " {")
		f.indent++
		for _, x := range s.Then {
			f.stmt(x)
		}
		f.indent--
		if len(s.Else) == 0 {
			f.line("}")
		} else {
			f.line("} else {")
			f.indent++
			for _, x := range s.Else {
				f.stmt(x)
			}
			f.indent--
			f.line("}")
		}
	case StWhile:
		f.line("while " + f.expr(s.Cond) + " {")
		f.indent++
		for _, x := range s.Body {
			f.stmt(x)
		}
		f.indent--
		f.line("}")
	case StFor:
		f.line("for " + s.Name + " in " + f.expr(s.Iter) + " {")
		f.indent++
		for _, x := range s.Body {
			f.stmt(x)
		}
		f.indent--
		f.line("}")
	case StDefer:
		f.line("defer {")
		f.indent++
		for _, x := range s.Body {
			f.stmt(x)
		}
		f.indent--
		f.line("}")
	case StUnsafe:
		f.line("unsafe {")
		f.indent++
		for _, x := range s.Body {
			f.stmt(x)
		}
		f.indent--
		f.line("}")
	case StMatch:
		f.line("match " + f.expr(s.Scrutinee) + " {")
		f.indent++
		for _, a := range s.Arms {
			f.line(f.pattern(a.Pattern) + " => {")
			f.indent++
			for _, x := range a.Body {
				f.stmt(x)
			}
			f.indent--
			f.line("}")
		}
		f.indent--
		f.line("}")
	}
}
func (f *Formatter) pattern(p Pattern) string {
	switch p.Kind {
	case PatWildcard:
		return "_"
	case PatNil:
		return "nil"
	case PatBool:
		if p.Bool {
			return "true"
		}
		return "false"
	case PatInt:
		return formatInt(p.Int)
	case PatString:
		return "\"" + escapeFormat(p.Str) + "\""
	case PatEnum:
		if p.TypeName != "" {
			return p.TypeName + "::" + p.Variant
		}
		return p.Variant
	case PatOption:
		if !p.Present {
			return "none"
		}
		return "some(" + p.Binding + ")"
	case PatResult:
		if p.OK {
			return "ok(" + p.Binding + ")"
		}
		return "err(" + p.Binding + ")"
	}
	return "_"
}
func formatInt(v int64) string {
	if v < 0 {
		return "(" + itoa(v) + ")"
	}
	return itoa(v)
}
func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	var b [32]byte
	i := len(b)
	var u uint64
	if neg {
		u = uint64(-(v + 1)) + 1
	} else {
		u = uint64(v)
	}
	for u > 0 {
		i--
		b[i] = byte('0' + u%10)
		u /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
func (f *Formatter) expr(e *Expr) string {
	if e == nil {
		return "nil"
	}
	switch e.Kind {
	case ExInt:
		return itoa(e.Int)
	case ExFloat:
		return formatFloat(e.Float)
	case ExBool:
		if e.Bool {
			return "true"
		}
		return "false"
	case ExNil:
		return "nil"
	case ExString:
		return "\"" + escapeFormat(e.Str) + "\""
	case ExVar:
		return e.Name
	case ExEnum:
		return e.EnumType + "::" + e.EnumVariant
	case ExMap:
		x := "{"
		for i, k := range e.MapKeys {
			if i > 0 {
				x += ", "
			}
			x += f.expr(k) + ": " + f.expr(e.Values[i])
		}
		return x + "}"
	case ExSet:
		x := "|{"
		for i, v := range e.Items {
			if i > 0 {
				x += ", "
			}
			x += f.expr(v)
		}
		return x + "}|"
	case ExArray:
		x := "["
		for i, v := range e.Items {
			if i > 0 {
				x += ", "
			}
			x += f.expr(v)
		}
		return x + "]"
	case ExStruct:
		x := e.StructName + "{"
		for i, n := range e.Fields {
			if i > 0 {
				x += ", "
			}
			x += n + ": " + f.expr(e.Values[i])
		}
		return x + "}"
	case ExUnary:
		return opText(e.Op) + f.expr(e.Operand)
	case ExBinary:
		return "(" + f.expr(e.Left) + " " + opText(e.Op) + " " + f.expr(e.Right) + ")"
	case ExPropagate:
		return f.expr(e.Operand) + "?"
	case ExCall:
		x := ""
		if e.Receiver != nil {
			x = f.expr(e.Receiver) + "."
		}
		x += e.Name + "("
		for i, a := range e.Args {
			if i > 0 {
				x += ", "
			}
			x += f.expr(a)
		}
		return x + ")"
	case ExIndex:
		return f.expr(e.Base) + "[" + f.expr(e.Left) + "]"
	case ExField:
		return f.expr(e.Base) + "." + e.Field
	}
	return "nil"
}
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}
func opText(k TokenKind) string {
	switch k {
	case PLUS:
		return "+"
	case MINUS:
		return "-"
	case STAR:
		return "*"
	case SLASH:
		return "/"
	case PERCENT:
		return "%"
	case EQEQ:
		return "=="
	case NEQ:
		return "!="
	case LESS:
		return "<"
	case LEQ:
		return "<="
	case GREATER:
		return ">"
	case GEQ:
		return ">="
	case AND:
		return "&&"
	case OR:
		return "||"
	case QUESTION:
		return "?"

	}
	return "?"
}
