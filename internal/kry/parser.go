package kry

import (
	"strconv"
)

type Parser struct {
	Tokens []Token
	Pos    int
	Lim    Limits
	Nodes  int
	Err    *Diagnostic
}

func Parse(src *Source, lim Limits) (*Program, *Diagnostic) {
	ts, d := Lex(src, lim)
	if d != nil {
		return nil, d
	}
	p := &Parser{Tokens: ts, Lim: lim}
	prog := &Program{Source: src, Module: src.Name, Sources: []*Source{src}}
	for !p.check(EOF) && p.Err == nil {
		pub := p.match(PUB)
		switch {
		case p.check(FN):
			f := p.function(pub)
			prog.Functions = append(prog.Functions, f)
		case p.check(STRUCT):
			s := p.structDecl(pub)
			prog.Structs = append(prog.Structs, s)
		case p.check(ENUM):
			e := p.enumDecl(pub)
			prog.Enums = append(prog.Enums, e)
		case p.match(IMPORT):
			t := p.expect(STRING, "import expects a quoted module path")
			if p.Err != nil {
				break
			}
			path, err := DecodeString(t)
			if err != nil {
				p.fail(t, "invalid import string")
			}
			prog.Imports = append(prog.Imports, ImportDecl{Path: path, Tok: t})
			p.end()
		default:
			if pub {
				p.fail(p.peek(), "'pub' must be followed by a function, struct, or enum")
				break
			}
			s := p.statement()
			if s != nil {
				prog.Statements = append(prog.Statements, s)
			}
			p.end()
		}
	}
	if p.Err != nil {
		return nil, p.Err
	}
	return prog, nil
}
func (p *Parser) peek() Token            { return p.Tokens[p.Pos] }
func (p *Parser) prev() Token            { return p.Tokens[p.Pos-1] }
func (p *Parser) check(k TokenKind) bool { return p.peek().Kind == k }
func (p *Parser) advance() Token {
	if p.Pos < len(p.Tokens)-1 {
		p.Pos++
	}
	return p.prev()
}
func (p *Parser) match(k TokenKind) bool {
	if p.check(k) {
		p.advance()
		return true
	}
	return false
}
func (p *Parser) expect(k TokenKind, msg string) Token {
	if p.check(k) {
		return p.advance()
	}
	p.fail(p.peek(), msg)
	return p.peek()
}
func (p *Parser) fail(t Token, msg string, args ...any) {
	if p.Err == nil {
		p.Err = Diag(CatParse, t.Source, t.Line, t.Column, msg, args...)
	}
}
func (p *Parser) node(t Token, k ExprKind) *Expr {
	p.Nodes++
	if p.Nodes > p.Lim.MaxASTNodes && p.Err == nil {
		p.fail(t, "AST node limit exceeded (%d)", p.Lim.MaxASTNodes)
	}
	return &Expr{Kind: k, Tok: t}
}
func (p *Parser) stmtNode(t Token, k StmtKind) *Stmt {
	p.Nodes++
	if p.Nodes > p.Lim.MaxASTNodes && p.Err == nil {
		p.fail(t, "AST node limit exceeded (%d)", p.Lim.MaxASTNodes)
	}
	return &Stmt{Kind: k, Tok: t}
}
func (p *Parser) end() {
	for p.match(SEMICOLON) {
	}
}
func (p *Parser) typeSpec() *TypeSpec {
	t := p.expect(ID, "expected a type name")
	s := &TypeSpec{Name: t.Text(), Tok: t}
	if p.match(LBRACKET) {
		if !p.check(RBRACKET) {
			for {
				s.Params = append(s.Params, p.typeSpec())
				if !p.match(COMMA) {
					break
				}
			}
		}
		p.expect(RBRACKET, "expected ']' after type parameters")
	}
	return s
}
func (p *Parser) function(pub bool) *Function {
	t := p.expect(FN, "expected 'fn'")
	n := p.expect(ID, "expected a function name")
	f := &Function{Name: n.Text(), Public: pub, Tok: t, Return: &TypeSpec{Name: "Nil", Tok: t}, Module: t.Source.Name}
	p.expect(LPAREN, "expected '(' after function name")
	if !p.check(RPAREN) {
		for {
			pt := p.expect(ID, "expected a parameter name")
			p.expect(COLON, "function parameters require an explicit type")
			f.Params = append(f.Params, Param{Name: pt.Text(), Type: p.typeSpec(), Tok: pt})
			if !p.match(COMMA) {
				break
			}
		}
	}
	p.expect(RPAREN, "expected ')' after parameters")
	if p.match(ARROW) {
		f.Return = p.typeSpec()
	}
	f.Body = p.block()
	return f
}
func (p *Parser) structDecl(pub bool) *StructDecl {
	t := p.expect(STRUCT, "expected 'struct'")
	n := p.expect(ID, "expected a struct name")
	d := &StructDecl{Name: n.Text(), Public: pub, Tok: t, Module: t.Source.Name}
	p.expect(LBRACE, "expected '{' after struct name")
	for !p.check(RBRACE) && !p.check(EOF) && p.Err == nil {
		ft := p.expect(ID, "expected a struct field name")
		p.expect(COLON, "expected ':' after field name")
		d.Fields = append(d.Fields, FieldDecl{Name: ft.Text(), Spec: p.typeSpec(), Tok: ft})
		if !p.match(COMMA) {
			p.end()
		}
	}
	p.expect(RBRACE, "expected '}' after struct declaration")
	return d
}
func (p *Parser) enumDecl(pub bool) *EnumDecl {
	t := p.expect(ENUM, "expected 'enum'")
	n := p.expect(ID, "expected an enum name")
	d := &EnumDecl{Name: n.Text(), Public: pub, Tok: t, Module: t.Source.Name}
	p.expect(LBRACE, "expected '{' after enum name")
	for !p.check(RBRACE) && !p.check(EOF) && p.Err == nil {
		v := p.expect(ID, "expected an enum variant")
		d.Variants = append(d.Variants, v.Text())
		if !p.match(COMMA) {
			p.end()
		}
	}
	p.expect(RBRACE, "expected '}' after enum declaration")
	return d
}
func (p *Parser) block() []*Stmt {
	p.expect(LBRACE, "expected '{'")
	var out []*Stmt
	for !p.check(RBRACE) && !p.check(EOF) && p.Err == nil {
		s := p.statement()
		if s != nil {
			out = append(out, s)
		}
		p.end()
	}
	p.expect(RBRACE, "expected '}' after block")
	return out
}
func (p *Parser) statement() *Stmt {
	t := p.peek()
	switch {
	case p.match(LET):
		s := p.stmtNode(t, StLet)
		s.Mutable = p.match(MUT)
		n := p.expect(ID, "expected a binding name after 'let'")
		s.Name = n.Text()
		if p.match(COLON) {
			s.Annotation = p.typeSpec()
		}
		p.expect(EQUAL, "expected '=' in binding declaration")
		s.Init = p.expression()
		return s
	case p.match(IF):
		return p.ifStmt(t)
	case p.match(WHILE):
		s := p.stmtNode(t, StWhile)
		s.Cond = p.expression()
		s.Body = p.block()
		return s
	case p.match(MATCH):
		return p.matchStmt(t)
	case p.match(RETURN):
		s := p.stmtNode(t, StReturn)
		if !p.check(RBRACE) && !p.check(EOF) && !p.check(SEMICOLON) {
			s.Return = p.expression()
		}
		return s
	case p.match(BREAK):
		return p.stmtNode(t, StBreak)
	case p.match(CONTINUE):
		return p.stmtNode(t, StContinue)
	}
	first := p.expression()
	if p.match(EQUAL) {
		s := p.stmtNode(t, StAssign)
		s.Target = first
		s.Value = p.expression()
		return s
	}
	return &Stmt{Kind: StExpr, Tok: t, Expr: first}
}
func (p *Parser) ifStmt(t Token) *Stmt {
	s := p.stmtNode(t, StIf)
	s.Cond = p.expression()
	s.Then = p.block()
	if p.match(ELSE) {
		if p.match(IF) {
			s.Else = []*Stmt{p.ifStmt(p.prev())}
		} else {
			s.Else = p.block()
		}
	}
	return s
}
func (p *Parser) matchStmt(t Token) *Stmt {
	s := p.stmtNode(t, StMatch)
	s.Scrutinee = p.expression()
	p.expect(LBRACE, "expected '{' after match expression")
	for !p.check(RBRACE) && !p.check(EOF) && p.Err == nil {
		pat := p.pattern()
		p.expect(FATARROW, "expected '=>' after match pattern")
		s.Arms = append(s.Arms, MatchArm{Pattern: pat, Body: p.block()})
		p.end()
	}
	p.expect(RBRACE, "expected '}' after match arms")
	return s
}
func (p *Parser) pattern() Pattern {
	t := p.peek()
	pat := Pattern{Kind: PatWildcard, Tok: t}
	if p.match(ID) {
		name := t.Text()
		if name == "_" {
			return pat
		}
		if name == "none" {
			pat.Kind = PatOption
			pat.Present = false
			return pat
		}
		if name == "some" || name == "ok" || name == "err" {
			pat.Kind = PatOption
			pat.Present = name == "some"
			pat.OK = name == "ok"
			if name != "some" {
				pat.Kind = PatResult
			}
			p.expect(LPAREN, "expected '(' in option/result pattern")
			b := p.expect(ID, "expected a binding name in pattern")
			pat.Binding = b.Text()
			p.expect(RPAREN, "expected ')' after pattern binding")
			return pat
		}
		if p.match(DCOLON) {
			pat.Kind = PatEnum
			pat.TypeName = name
			v := p.expect(ID, "expected an enum variant")
			pat.Variant = v.Text()
			return pat
		}
		pat.Kind = PatEnum
		pat.Variant = name
		return pat
	}
	if p.match(NIL) {
		pat.Kind = PatNil
		return pat
	}
	if p.match(TRUE) || p.match(FALSE) {
		pat.Kind = PatBool
		pat.Bool = t.Kind == TRUE
		return pat
	}
	if p.match(INT) {
		pat.Kind = PatInt
		v, err := strconv.ParseInt(t.Text(), 10, 64)
		if err != nil {
			p.fail(t, "integer literal is outside the supported Int range")
		} else {
			pat.Int = v
		}
		return pat
	}
	if p.match(STRING) {
		pat.Kind = PatString
		s, err := DecodeString(t)
		if err != nil {
			p.fail(t, "invalid string pattern")
		} else {
			pat.Str = s
		}
		return pat
	}
	p.fail(t, "invalid match pattern")
	return pat
}
func (p *Parser) expression() *Expr { return p.precedence(1) }
func precedence(k TokenKind) int {
	switch k {
	case OR:
		return 1
	case AND:
		return 2
	case EQEQ, NEQ:
		return 3
	case LESS, LEQ, GREATER, GEQ:
		return 4
	case PLUS, MINUS:
		return 5
	case STAR, SLASH, PERCENT:
		return 6
	}
	return 0
}
func (p *Parser) precedence(min int) *Expr {
	left := p.unary()
	for p.Err == nil && precedence(p.peek().Kind) >= min {
		op := p.advance()
		right := p.precedence(precedence(op.Kind) + 1)
		e := p.node(op, ExBinary)
		e.Op = op.Kind
		e.Left = left
		e.Right = right
		left = e
	}
	return left
}
func (p *Parser) unary() *Expr {
	if p.match(BANG) || p.match(MINUS) || p.match(PLUS) {
		t := p.prev()
		if t.Kind == MINUS && p.check(INT) && p.peek().Text() == "9223372036854775808" {
			lit := p.advance()
			e := p.node(lit, ExInt)
			e.Int = -1 << 63
			return e
		}
		e := p.node(t, ExUnary)
		e.Op = t.Kind
		e.Operand = p.unary()
		return e
	}
	e := p.primary()
	for p.Err == nil {
		if p.match(LPAREN) {
			if e.Kind != ExVar {
				p.fail(p.prev(), "only named functions can be called")
			}
			c := p.node(p.prev(), ExCall)
			if e.Kind == ExVar {
				c.Name = e.Name
			}
			if !p.check(RPAREN) {
				for {
					c.Args = append(c.Args, p.expression())
					if !p.match(COMMA) {
						break
					}
				}
			}
			p.expect(RPAREN, "expected ')' after arguments")
			e = c
		} else if p.match(LBRACKET) {
			x := p.node(p.prev(), ExIndex)
			x.Base = e
			x.Left = p.expression()
			p.expect(RBRACKET, "expected ']' after index")
			e = x
		} else if p.match(DOT) {
			ft := p.expect(ID, "expected a field name after '.'")
			x := p.node(ft, ExField)
			x.Base = e
			x.Field = ft.Text()
			e = x
		} else {
			break
		}
	}
	return e
}
func (p *Parser) primary() *Expr {
	t := p.peek()
	switch {
	case p.match(INT):
		e := p.node(t, ExInt)
		v, err := strconv.ParseInt(t.Text(), 10, 64)
		if err != nil {
			p.fail(t, "integer literal is outside the supported Int range")
		} else {
			e.Int = v
		}
		return e
	case p.match(FLOAT):
		e := p.node(t, ExFloat)
		v, err := strconv.ParseFloat(t.Text(), 64)
		if err != nil || !isFinite(v) {
			p.fail(t, "floating-point literal must be finite and representable")
		} else {
			e.Float = v
		}
		return e
	case p.match(STRING):
		e := p.node(t, ExString)
		s, err := DecodeString(t)
		if err != nil {
			p.fail(t, "invalid string literal")
		} else {
			e.Str = s
		}
		return e
	case p.match(TRUE) || p.match(FALSE):
		e := p.node(t, ExBool)
		e.Bool = t.Kind == TRUE
		return e
	case p.match(NIL):
		return p.node(t, ExNil)
	case p.match(ID):
		e := p.node(t, ExVar)
		e.Name = t.Text()
		if p.match(DCOLON) {
			v := p.expect(ID, "expected an enum variant after '::'")
			x := p.node(t, ExEnum)
			x.EnumType = e.Name
			x.EnumVariant = v.Text()
			return x
		}
		if p.check(LBRACE) && p.Pos+2 < len(p.Tokens) && p.Tokens[p.Pos+1].Kind == ID && p.Tokens[p.Pos+2].Kind == COLON {
			p.advance()
			x := p.node(t, ExStruct)
			x.StructName = e.Name
			for !p.check(RBRACE) && !p.check(EOF) && p.Err == nil {
				f := p.expect(ID, "expected a struct field name")
				p.expect(COLON, "expected ':' after struct field name")
				x.Fields = append(x.Fields, f.Text())
				x.Values = append(x.Values, p.expression())
				if !p.match(COMMA) {
					break
				}
			}
			p.expect(RBRACE, "expected '}' after struct literal")
			return x
		}
		return e
	case p.match(LPAREN):
		e := p.expression()
		p.expect(RPAREN, "expected ')' after expression")
		return e
	case p.match(LBRACKET):
		e := p.node(t, ExArray)
		if !p.check(RBRACKET) {
			for {
				e.Items = append(e.Items, p.expression())
				if !p.match(COMMA) {
					break
				}
			}
		}
		p.expect(RBRACKET, "expected ']' after array literal")
		return e
	}
	p.fail(t, "expected an expression")
	return p.node(t, ExNil)
}
