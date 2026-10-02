package kry

import "fmt"

// ValidatedMIR is the single in-memory lowering input for the interpreter and
// native backends. It is created from checked source or decoded from validated
// KIR; callers cannot construct or mutate it through the public Go API. The
// source-compiled form also carries immutable source-text and package
// visibility sidecars. Decoded KIR retains source names and coordinates for
// diagnostics but does not include source text or those sidecars.
//
// The canonical JSON KIR document remains the interchange and artifact format.
// Keeping this wrapper separate makes the validation boundary explicit and
// avoids serializing and decoding the document between in-process compiler
// stages.
type ValidatedMIR struct {
	document         *KIRDocument
	limits           Limits
	sources          map[string]*Source
	visibilityScopes map[string]string
	hasSourceContext bool
}

// CompileMIR lowers one successfully checked program to the canonical
// in-memory representation and validates its structural invariants before
// any interpreter or target backend can consume it.
func CompileMIR(program *Program, checker *Checker, target NativeTarget) (*ValidatedMIR, error) {
	if program == nil || checker == nil || checker.Env == nil {
		return nil, fmt.Errorf("missing checked program")
	}
	if checker.Prog != program {
		return nil, fmt.Errorf("checker does not describe the supplied program")
	}
	if !checker.checked {
		return nil, fmt.Errorf("checker has not completed successfully")
	}
	if diagnostic := ValidateASTLimits(program, checker.Lim); diagnostic != nil {
		return nil, fmt.Errorf("%s", diagnostic.Message)
	}
	document, err := buildKIRDocument(program, checker, target)
	if err != nil {
		return nil, err
	}
	if err := validateKIRDocument(document, checker.Lim); err != nil {
		return nil, fmt.Errorf("invalid lowered MIR: %w", err)
	}
	paths := newKIRPathNames(program)
	sources := make(map[string]*Source, len(program.Sources)+1)
	visibilityScopes := make(map[string]string, len(program.Sources)+1)
	addSource := func(source *Source) {
		if source == nil {
			return
		}
		name := paths.source(source)
		scope := source.VisibilityScope
		if scope == "" {
			scope = sourceVisibilityScope(source)
		}
		scope = paths.name(scope)
		// Keep diagnostics and package visibility as immutable MIR sidecars.
		// Retaining Source pointers here would let later mutations to the
		// frontend tree change the meaning of an already-validated MIR value.
		sources[name] = &Source{Name: source.Name, Text: source.Text, VisibilityScope: scope}
		visibilityScopes[name] = scope
	}
	addSource(program.Source)
	for _, source := range program.Sources {
		addSource(source)
	}
	return &ValidatedMIR{document: document, limits: checker.Lim, sources: sources, visibilityScopes: visibilityScopes, hasSourceContext: true}, nil
}

// ValidateASTLimits bounds the checked source tree before it is lowered to
// MIR. This is resource accounting only; it does not create an alternate IR.
func ValidateASTLimits(program *Program, limits Limits) *Diagnostic {
	if program == nil {
		return Diag(CatCLI, nil, 1, 1, "missing program")
	}

	var visited uint64
	var limitFailure string
	add := func(depth int) bool {
		if limitFailure != "" {
			return false
		}
		if visited >= limits.MaxInstructions {
			limitFailure = "IR instruction limit exceeded"
			return false
		}
		if depth > limits.MaxNesting {
			limitFailure = "IR nesting limit exceeded"
			return false
		}
		visited++
		return true
	}
	var visitExpr func(*Expr, int)
	var visitStmt func(*Stmt, int)
	visitExpr = func(expression *Expr, depth int) {
		if expression == nil || limitFailure != "" || !add(depth) {
			return
		}
		for _, child := range []*Expr{expression.Left, expression.Right, expression.Operand, expression.Base} {
			visitExpr(child, depth+1)
		}
		for _, list := range [][]*Expr{expression.Args, expression.Items, expression.Values, expression.MapKeys} {
			for _, child := range list {
				visitExpr(child, depth+1)
			}
		}
		for _, child := range []*Expr{expression.Receiver, expression.Callee} {
			visitExpr(child, depth+1)
		}
		if expression.Lambda != nil && add(depth+1) {
			for _, statement := range expression.Lambda.Body {
				visitStmt(statement, depth+2)
			}
		}
	}
	visitStmt = func(statement *Stmt, depth int) {
		if statement == nil || limitFailure != "" {
			return
		}
		switch statement.Kind {
		case StLet, StConst:
			visitExpr(statement.Init, depth+1)
		case StAssign:
			visitExpr(statement.Target, depth+1)
			visitExpr(statement.Value, depth+1)
		case StIf:
			visitExpr(statement.Cond, depth+1)
			for _, child := range statement.Then {
				visitStmt(child, depth+1)
			}
			for _, child := range statement.Else {
				visitStmt(child, depth+1)
			}
		case StWhile:
			visitExpr(statement.Cond, depth+1)
			for _, child := range statement.Body {
				visitStmt(child, depth+1)
			}
		case StFor:
			visitExpr(statement.Iter, depth+1)
			for _, child := range statement.Body {
				visitStmt(child, depth+1)
			}
		case StDefer, StUnsafe:
			for _, child := range statement.Body {
				visitStmt(child, depth+1)
			}
		case StReturn:
			visitExpr(statement.Return, depth+1)
		case StMatch:
			visitExpr(statement.Scrutinee, depth+1)
			for _, arm := range statement.Arms {
				for _, child := range arm.Body {
					visitStmt(child, depth+1)
				}
			}
		case StExpr:
			visitExpr(statement.Expr, depth+1)
		}
		add(depth)
	}

	for _, function := range program.Functions {
		if function == nil {
			continue
		}
		if !add(0) {
			break
		}
		for _, statement := range function.Body {
			visitStmt(statement, 1)
		}
	}
	for _, statement := range program.Statements {
		visitStmt(statement, 0)
	}
	if limitFailure != "" {
		return Diag(CatResource, program.Source, 1, 1, "%s", limitFailure)
	}
	return nil
}
