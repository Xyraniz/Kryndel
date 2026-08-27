package kry

type OpCode uint8

const (
	OpExpr OpCode = iota
	OpLet
	OpAssign
	OpIf
	OpWhile
	OpReturn
	OpBreak
	OpContinue
	OpMatch
	OpFunction
)

type Instruction struct {
	Op           OpCode
	Source       *Source
	Line, Column int
	Depth        int
}
type ValidatedIR struct {
	Instructions []Instruction
	MaxDepth     int
}

func CompileIR(p *Program, lim Limits) (*ValidatedIR, *Diagnostic) {
	ir := &ValidatedIR{}
	add := func(op OpCode, t Token, d int) {
		if uint64(len(ir.Instructions)) >= lim.MaxInstructions {
			return
		}
		if d > lim.MaxNesting {
			ir = nil
			return
		}
		ir.Instructions = append(ir.Instructions, Instruction{Op: op, Source: t.Source, Line: t.Line, Column: t.Column, Depth: d})
		if d > ir.MaxDepth {
			ir.MaxDepth = d
		}
	}
	var ex func(*Expr, int)
	var st func(*Stmt, int)
	ex = func(e *Expr, d int) {
		if ir == nil || e == nil {
			return
		}
		add(OpExpr, e.Tok, d)
		ex(e.Left, d+1)
		ex(e.Right, d+1)
		ex(e.Operand, d+1)
		ex(e.Base, d+1)
		for _, x := range e.Args {
			ex(x, d+1)
		}
		for _, x := range e.Items {
			ex(x, d+1)
		}
		for _, x := range e.Values {
			ex(x, d+1)
		}
	}
	st = func(s *Stmt, d int) {
		if ir == nil || s == nil {
			return
		}
		op := OpExpr
		switch s.Kind {
		case StLet:
			op = OpLet
			ex(s.Init, d+1)
		case StAssign:
			op = OpAssign
			ex(s.Target, d+1)
			ex(s.Value, d+1)
		case StIf:
			op = OpIf
			ex(s.Cond, d+1)
			for _, x := range s.Then {
				st(x, d+1)
			}
			for _, x := range s.Else {
				st(x, d+1)
			}
		case StWhile:
			op = OpWhile
			ex(s.Cond, d+1)
			for _, x := range s.Body {
				st(x, d+1)
			}
		case StReturn:
			op = OpReturn
			ex(s.Return, d+1)
		case StBreak:
			op = OpBreak
		case StContinue:
			op = OpContinue
		case StMatch:
			op = OpMatch
			ex(s.Scrutinee, d+1)
			for _, a := range s.Arms {
				for _, x := range a.Body {
					st(x, d+1)
				}
			}
		case StExpr:
			ex(s.Expr, d+1)
		}
		add(op, s.Tok, d)
	}
	for _, f := range p.Functions {
		add(OpFunction, f.Tok, 0)
		for _, s := range f.Body {
			st(s, 1)
		}
	}
	for _, s := range p.Statements {
		st(s, 0)
	}
	if ir == nil {
		return nil, Diag(CatResource, p.Source, 1, 1, "IR nesting or instruction limit exceeded")
	}
	return ir, nil
}
func ValidateIR(p *Program, lim Limits) (*ValidatedIR, *Diagnostic) {
	ir, d := CompileIR(p, lim)
	if d != nil {
		return nil, d
	}
	if len(ir.Instructions) == 0 && len(p.Statements) == 0 && len(p.Functions) == 0 {
		return ir, nil
	}
	return ir, nil
}
