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
	exprs  [][]Expr
	stmts  [][]Stmt
}

const parserNodeChunkSize = 16

func Parse(src *Source, lim Limits) (*Program, *Diagnostic) {
	ts, d := Lex(src, lim)
	if d != nil {
		return nil, d
	}
	p := &Parser{Tokens: ts, Lim: lim}
	prog := &Program{Source: src, Module: src.Name, VisibilityScope: sourceVisibilityScope(src), Sources: []*Source{src}}
	for !p.check(EOF) && p.Err == nil {
		pub := p.match(PUB)
		private := p.match(PRIVATE)
		if private && pub {
			p.fail(p.prev(), "a declaration cannot be both pub and private")
		}
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
		case p.check(TRAIT):
			t := p.traitDecl(pub)
			prog.Traits = append(prog.Traits, t)
		case p.match(IMPL):
			p.Pos--
			functions, traitImpl := p.implDecl()
			prog.Functions = append(prog.Functions, functions...)
			if traitImpl != nil {
				prog.TraitImpls = append(prog.TraitImpls, traitImpl)
			}
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
		case p.match(CONST):
			if pub || private {
				p.fail(p.peek(), "const declarations cannot be public or private")
				break
			}
			s := p.constStmt()
			s.EndToken = p.prev()
			prog.Statements = append(prog.Statements, s)
			p.end()
		default:
			if pub || private {
				p.fail(p.peek(), "'pub' must be followed by a function, struct, enum, or trait")
				break
			}
			s := p.statement()
			if s != nil {
				s.EndToken = p.prev()
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
func (p *Parser) peek() Token { return p.Tokens[p.Pos] }
func (p *Parser) prev() Token {
	if p.Pos == 0 {
		return p.Tokens[0]
	}
	return p.Tokens[p.Pos-1]
}
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
	p.fail(p.peek(), "%s", msg)
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
	if len(p.exprs) == 0 || len(p.exprs[len(p.exprs)-1]) == cap(p.exprs[len(p.exprs)-1]) {
		p.exprs = append(p.exprs, make([]Expr, 0, parserNodeChunkSize))
	}
	block := p.exprs[len(p.exprs)-1]
	block = append(block, Expr{Kind: k, Tok: t, StartToken: t, EndToken: t})
	p.exprs[len(p.exprs)-1] = block
	return &block[len(block)-1]
}
func (p *Parser) stmtNode(t Token, k StmtKind) *Stmt {
	p.Nodes++
	if p.Nodes > p.Lim.MaxASTNodes && p.Err == nil {
		p.fail(t, "AST node limit exceeded (%d)", p.Lim.MaxASTNodes)
	}
	if len(p.stmts) == 0 || len(p.stmts[len(p.stmts)-1]) == cap(p.stmts[len(p.stmts)-1]) {
		p.stmts = append(p.stmts, make([]Stmt, 0, parserNodeChunkSize))
	}
	block := p.stmts[len(p.stmts)-1]
	block = append(block, Stmt{Kind: k, Tok: t})
	p.stmts[len(p.stmts)-1] = block
	return &block[len(block)-1]
}
func (p *Parser) end() {
	for p.match(SEMICOLON) {
	}
}
func (p *Parser) typeSpec() *TypeSpec {
	if p.match(FN) {
		t := p.prev()
		s := &TypeSpec{Function: true, Tok: t}
		p.expect(LPAREN, "expected '(' after 'fn' in function type")
		if !p.check(RPAREN) {
			for {
				s.Params = append(s.Params, p.typeSpec())
				if !p.match(COMMA) {
					break
				}
			}
		}
		p.expect(RPAREN, "expected ')' after function type parameters")
		p.expect(ARROW, "function types require a return type after '->'")
		s.Return = p.typeSpec()
		return s
	}
	if p.check(ID) && p.peek().Text() == "dyn" && p.Pos+1 < len(p.Tokens) && p.Tokens[p.Pos+1].Kind == ID {
		t := p.advance()
		p.fail(t, "dynamic trait objects using 'dyn Trait' are not supported; use a generic trait bound")
		return &TypeSpec{Name: t.Text(), Tok: t}
	}
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
	f := &Function{Name: n.Text(), NameToken: n, Public: pub, Tok: t, Return: &TypeSpec{Name: "Nil", Tok: t}, Module: t.Source.Name, VisibilityScope: sourceVisibilityScope(t.Source)}
	f.TypeParams = p.typeParams()
	p.expect(LPAREN, "expected '(' after function name")
	if !p.check(RPAREN) {
		seenDefault := false
		for {
			pt := p.expect(ID, "expected a parameter name")
			p.expect(COLON, "function parameters require an explicit type")
			param := Param{Name: pt.Text(), Type: p.typeSpec(), Tok: pt}
			if p.match(EQUAL) {
				param.Default = p.expression()
				seenDefault = true
			} else if seenDefault {
				p.fail(pt, "required parameter '%s' cannot follow a parameter with a default value", pt.Text())
			}
			param.EndToken = p.prev()
			f.Params = append(f.Params, param)
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
	f.EndToken = p.prev()
	return f
}

func (p *Parser) typeParams() []TypeParam {
	if !p.match(LBRACKET) {
		return nil
	}
	var params []TypeParam
	if !p.check(RBRACKET) {
		for {
			pt := p.expect(ID, "expected a type parameter name")
			param := TypeParam{Name: pt.Text(), Tok: pt}
			if p.match(COLON) {
				constraint := p.expect(ID, "expected a type constraint")
				param.Constraint = constraint.Text()
				if p.match(PLUS) {
					p.fail(p.prev(), "multiple trait bounds are not supported; use one constraint per type parameter")
				}
			}
			params = append(params, param)
			if !p.match(COMMA) {
				break
			}
		}
	}
	p.expect(RBRACKET, "expected ']' after type parameters")
	return params
}

func (p *Parser) structDecl(pub bool) *StructDecl {
	t := p.expect(STRUCT, "expected 'struct'")
	n := p.expect(ID, "expected a struct name")
	d := &StructDecl{Name: n.Text(), NameToken: n, Public: pub, Tok: t, Module: t.Source.Name, VisibilityScope: sourceVisibilityScope(t.Source)}
	d.TypeParams = p.typeParams()
	p.expect(LBRACE, "expected '{' after struct name")
	for !p.check(RBRACE) && !p.check(EOF) && p.Err == nil {
		public := !p.match(PRIVATE)
		ft := p.expect(ID, "expected a struct field name")
		p.expect(COLON, "expected ':' after field name")
		d.Fields = append(d.Fields, FieldDecl{Name: ft.Text(), Public: public, Spec: p.typeSpec(), Tok: ft})
		if !p.match(COMMA) {
			p.end()
		}
	}
	p.expect(RBRACE, "expected '}' after struct declaration")
	d.EndToken = p.prev()
	return d
}
func (p *Parser) traitDecl(pub bool) *TraitDecl {
	t := p.expect(TRAIT, "expected 'trait'")
	n := p.expect(ID, "expected a trait name")
	d := &TraitDecl{Name: n.Text(), NameToken: n, Public: pub, Tok: t, Module: t.Source.Name, VisibilityScope: sourceVisibilityScope(t.Source)}
	if p.check(LBRACKET) {
		p.fail(p.peek(), "generic trait declarations are not supported")
		return d
	}
	p.expect(LBRACE, "expected '{' after trait name")
	seen := map[string]bool{}
	for !p.check(RBRACE) && !p.check(EOF) && p.Err == nil {
		if !p.check(FN) {
			p.fail(p.peek(), "traits may contain method signatures only")
			break
		}
		method := p.traitMethod(d.Name)
		if seen[method.Name] {
			p.fail(method.NameToken, "trait '%s' declares method '%s' more than once", d.Name, method.Name)
			break
		}
		seen[method.Name] = true
		d.Methods = append(d.Methods, method)
		p.end()
	}
	p.expect(RBRACE, "expected '}' after trait declaration")
	d.EndToken = p.prev()
	return d
}

func (p *Parser) traitMethod(trait string) *Function {
	t := p.expect(FN, "expected 'fn' in trait declaration")
	n := p.expect(ID, "expected a trait method name")
	f := &Function{Name: n.Text(), NameToken: n, Trait: trait, Public: true, Tok: t, Return: &TypeSpec{Name: "Nil", Tok: t}, Module: t.Source.Name, VisibilityScope: sourceVisibilityScope(t.Source)}
	if p.check(LBRACKET) {
		p.fail(p.peek(), "generic trait methods are not supported")
		return f
	}
	p.expect(LPAREN, "expected '(' after trait method name")
	if !p.check(RPAREN) {
		for {
			pt := p.expect(ID, "expected a trait method parameter name")
			p.expect(COLON, "trait method parameters require an explicit type")
			param := Param{Name: pt.Text(), Type: p.typeSpec(), Tok: pt}
			if p.match(EQUAL) {
				p.fail(p.prev(), "trait method parameters cannot have defaults")
			}
			param.EndToken = p.prev()
			f.Params = append(f.Params, param)
			if !p.match(COMMA) {
				break
			}
		}
	}
	p.expect(RPAREN, "expected ')' after trait method parameters")
	if p.match(ARROW) {
		f.Return = p.typeSpec()
	}
	if p.check(LBRACE) {
		p.fail(p.peek(), "default trait method bodies are not supported")
	}
	f.EndToken = p.prev()
	return f
}

func (p *Parser) implDecl() ([]*Function, *TraitImplDecl) {
	t := p.expect(IMPL, "expected 'impl'")
	head := p.typeSpec()
	traitImpl := (*TraitImplDecl)(nil)
	receiver := head
	if p.match(FOR) {
		receiver = p.typeSpec()
		traitImpl = &TraitImplDecl{Trait: head.Name, TraitToken: head.Tok, Target: receiver, Tok: t, Module: t.Source.Name, VisibilityScope: sourceVisibilityScope(t.Source)}
		if head.Function || len(head.Params) != 0 {
			p.fail(head.Tok, "trait implementations must name one non-generic trait")
		}
	}
	p.expect(LBRACE, "expected '{' after impl type")
	var out []*Function
	for !p.check(RBRACE) && !p.check(EOF) && p.Err == nil {
		pub := p.match(PUB)
		f := p.function(pub)
		f.Receiver = receiver
		if traitImpl != nil {
			f.Trait = traitImpl.Trait
			f.Public = true
			traitImpl.Methods = append(traitImpl.Methods, f)
		}
		f.Tok = t
		out = append(out, f)
		p.end()
	}
	p.expect(RBRACE, "expected '}' after impl block")
	if traitImpl != nil {
		traitImpl.EndToken = p.prev()
	}
	return out, traitImpl
}
func (p *Parser) enumDecl(pub bool) *EnumDecl {
	t := p.expect(ENUM, "expected 'enum'")
	n := p.expect(ID, "expected an enum name")
	d := &EnumDecl{Name: n.Text(), NameToken: n, Public: pub, Tok: t, Module: t.Source.Name, VisibilityScope: sourceVisibilityScope(t.Source)}
	p.expect(LBRACE, "expected '{' after enum name")
	for !p.check(RBRACE) && !p.check(EOF) && p.Err == nil {
		v := p.expect(ID, "expected an enum variant")
		d.Variants = append(d.Variants, v.Text())
		d.VariantTokens = append(d.VariantTokens, v)
		if !p.match(COMMA) {
			p.end()
		}
	}
	p.expect(RBRACE, "expected '}' after enum declaration")
	d.EndToken = p.prev()
	return d
}
func (p *Parser) block() []*Stmt {
	p.expect(LBRACE, "expected '{'")
	var out []*Stmt
	for !p.check(RBRACE) && !p.check(EOF) && p.Err == nil {
		s := p.statement()
		if s != nil {
			s.EndToken = p.prev()
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
		s.NameToken = n
		if p.match(COLON) {
			s.Annotation = p.typeSpec()
		}
		p.expect(EQUAL, "expected '=' in binding declaration")
		s.Init = p.expression()
		return s
	case p.match(CONST):
		return p.constStmt()
	case p.match(IF):
		return p.ifStmt(t)
	case p.match(WHILE):
		s := p.stmtNode(t, StWhile)
		s.Cond = p.expression()
		s.Body = p.block()
		return s
	case p.match(FOR):
		s := p.stmtNode(t, StFor)
		item := p.expect(ID, "for expects a binding name")
		s.Name = item.Text()
		s.NameToken = item
		p.expect(IN, "for expects 'in' after the binding name")
		s.Iter = p.expression()
		s.Body = p.block()
		return s
	case p.match(DEFER):
		s := p.stmtNode(t, StDefer)
		s.Body = p.block()
		return s
	case p.match(UNSAFE):
		s := p.stmtNode(t, StUnsafe)
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
	s := p.stmtNode(t, StExpr)
	s.Expr = first
	return s
}

func (p *Parser) constStmt() *Stmt {
	t := p.prev()
	s := p.stmtNode(t, StConst)
	s.Const = true
	n := p.expect(ID, "expected a constant name after 'const'")
	s.Name = n.Text()
	s.NameToken = n
	if p.match(COLON) {
		s.Annotation = p.typeSpec()
	}
	p.expect(EQUAL, "expected '=' in constant declaration")
	s.Init = p.expression()
	return s
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
	s.EndToken = p.prev()
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
	pattern := p.patternNode()
	pattern.EndToken = p.prev()
	return pattern
}

func (p *Parser) patternNode() Pattern {
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
			pat.BindingTok = b
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
	case PIPE:
		return 2
	case BITXOR:
		return 3
	case BITAND:
		return 4
	case AND:
		return 5
	case EQEQ, NEQ:
		return 6
	case LESS, LEQ, GREATER, GEQ:
		return 7
	case SHL, SHR:
		return 8
	case PLUS, MINUS:
		return 9
	case STAR, SLASH, PERCENT:
		return 10
	}
	return 0
}
func (p *Parser) precedence(min int) *Expr {
	left := p.unary()
	for p.Err == nil && precedence(p.peek().Kind) >= min {
		op := p.advance()
		right := p.precedence(precedence(op.Kind) + 1)
		e := p.node(op, ExBinary)
		e.StartToken = left.StartToken
		e.Op = op.Kind
		e.Left = left
		e.Right = right
		left = e
	}
	if left != nil {
		left.EndToken = p.prev()
	}
	return left
}
func (p *Parser) unary() *Expr {
	if p.match(BANG) || p.match(MINUS) || p.match(PLUS) || p.match(BITNOT) {
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
			if p.Pos >= 2 {
				e.EndToken = p.Tokens[p.Pos-2]
			}
			c := p.node(p.prev(), ExCall)
			c.StartToken = e.StartToken
			c.Callee = e
			if e.Kind == ExVar {
				c.Name = e.Name
				c.NameToken = e.Tok
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
			c.EndToken = p.prev()
			e = c
		} else if p.match(LBRACKET) {
			if p.Pos >= 2 {
				e.EndToken = p.Tokens[p.Pos-2]
			}
			x := p.node(p.prev(), ExIndex)
			x.StartToken = e.StartToken
			x.Base = e
			x.Left = p.expression()
			p.expect(RBRACKET, "expected ']' after index")
			x.EndToken = p.prev()
			e = x
		} else if p.match(DOT) {
			if p.Pos >= 2 {
				e.EndToken = p.Tokens[p.Pos-2]
			}
			ft := p.expect(ID, "expected a field or method name after '.'")
			if p.match(LPAREN) {
				x := p.node(ft, ExCall)
				x.StartToken = e.StartToken
				x.Name = ft.Text()
				x.NameToken = ft
				x.Receiver = e
				if !p.check(RPAREN) {
					for {
						x.Args = append(x.Args, p.expression())
						if !p.match(COMMA) {
							break
						}
					}
				}
				p.expect(RPAREN, "expected ')' after method arguments")
				x.EndToken = p.prev()
				e = x
			} else {
				x := p.node(ft, ExField)
				x.StartToken = e.StartToken
				x.Base = e
				x.Field = ft.Text()
				e = x
			}
		} else if p.match(QUESTION) {
			if p.Pos >= 2 {
				e.EndToken = p.Tokens[p.Pos-2]
			}
			x := p.node(p.prev(), ExPropagate)
			x.StartToken = e.StartToken
			x.Operand = e
			x.EndToken = p.prev()
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
	case p.check(FN):
		return p.lambda()
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
			x.NameToken = t
			x.EnumVariant = v.Text()
			x.VariantToken = v
			return x
		}
		if p.check(LBRACKET) && p.looksLikeGenericStructLiteral() {
			spec := &TypeSpec{Name: e.Name, Tok: t}
			p.advance()
			if !p.check(RBRACKET) {
				for {
					spec.Params = append(spec.Params, p.typeSpec())
					if !p.match(COMMA) {
						break
					}
				}
			}
			p.expect(RBRACKET, "expected ']' after generic struct type")
			p.expect(LBRACE, "expected '{' after generic struct type")
			x := p.structLiteral(t, spec)
			return x
		}
		if p.looksLikeStructLiteral() {
			p.advance()
			return p.structLiteral(t, &TypeSpec{Name: e.Name, Tok: t})
		}
		return e
	case p.match(LPAREN):
		e := p.expression()
		p.expect(RPAREN, "expected ')' after expression")
		e.StartToken = t
		e.EndToken = p.prev()
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
	case p.match(LBRACE):
		e := p.node(t, ExMap)
		if !p.check(RBRACE) {
			for {
				key := p.expression()
				p.expect(COLON, "map literals require ':' after each key")
				e.MapKeys = append(e.MapKeys, key)
				e.Values = append(e.Values, p.expression())
				if !p.match(COMMA) {
					break
				}
			}
		}
		p.expect(RBRACE, "expected '}' after map literal")
		return e
	case p.match(PIPE):
		e := p.node(t, ExSet)
		p.expect(LBRACE, "set literals require '|{'")
		if !p.check(RBRACE) {
			for {
				e.Items = append(e.Items, p.expression())
				if !p.match(COMMA) {
					break
				}
			}
		}
		p.expect(RBRACE, "expected '}' after set literal")
		p.expect(PIPE, "set literals require a closing '|'")
		return e
	}
	p.fail(t, "expected an expression")
	return p.node(t, ExNil)
}

func (p *Parser) looksLikeStructLiteral() bool {
	if !p.check(LBRACE) || p.Pos+1 >= len(p.Tokens) {
		return false
	}
	return p.Pos+2 < len(p.Tokens) && p.Tokens[p.Pos+1].Kind == ID && p.Tokens[p.Pos+2].Kind == COLON
}

func (p *Parser) looksLikeGenericStructLiteral() bool {
	if !p.check(LBRACKET) {
		return false
	}
	depth := 0
	for i := p.Pos; i < len(p.Tokens); i++ {
		switch p.Tokens[i].Kind {
		case LBRACKET:
			depth++
		case RBRACKET:
			depth--
			if depth == 0 {
				if i+1 >= len(p.Tokens) || p.Tokens[i+1].Kind != LBRACE {
					return false
				}
				if i+2 >= len(p.Tokens) {
					return false
				}
				return p.Tokens[i+2].Kind == RBRACE || i+3 < len(p.Tokens) && p.Tokens[i+2].Kind == ID && p.Tokens[i+3].Kind == COLON
			}
		}
	}
	return false
}

func (p *Parser) structLiteral(token Token, spec *TypeSpec) *Expr {
	x := p.node(token, ExStruct)
	x.StructName = spec.Name
	x.StructType = spec
	for !p.check(RBRACE) && !p.check(EOF) && p.Err == nil {
		f := p.expect(ID, "expected a struct field name")
		p.expect(COLON, "expected ':' after struct field name")
		x.Fields = append(x.Fields, f.Text())
		x.FieldTokens = append(x.FieldTokens, f)
		x.Values = append(x.Values, p.expression())
		if !p.match(COMMA) {
			break
		}
	}
	p.expect(RBRACE, "expected '}' after struct literal")
	return x
}

func (p *Parser) lambda() *Expr {
	t := p.expect(FN, "expected 'fn'")
	f := &Function{Name: "<closure>", Tok: t, Return: &TypeSpec{Name: "Nil", Tok: t}, Module: t.Source.Name, VisibilityScope: sourceVisibilityScope(t.Source)}
	p.expect(LPAREN, "expected '(' after 'fn' in closure")
	if !p.check(RPAREN) {
		for {
			pt := p.expect(ID, "expected a closure parameter name")
			p.expect(COLON, "closure parameters require an explicit type")
			param := Param{Name: pt.Text(), Type: p.typeSpec(), Tok: pt}
			if p.match(EQUAL) {
				p.fail(p.prev(), "closure parameters cannot have default values")
				_ = p.expression()
			}
			param.EndToken = p.prev()
			f.Params = append(f.Params, param)
			if !p.match(COMMA) {
				break
			}
		}
	}
	p.expect(RPAREN, "expected ')' after closure parameters")
	if p.match(ARROW) {
		f.Return = p.typeSpec()
	}
	f.Body = p.block()
	f.EndToken = p.prev()
	e := p.node(t, ExLambda)
	e.Lambda = f
	return e
}
