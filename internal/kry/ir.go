package kry

import "fmt"

// ValidatedMIR is the opaque in-memory lowering boundary for the interpreter
// and native backends. Its canonical storage is a flat typed arena created
// from checked source or validated KIR; callers cannot construct or mutate it
// through the public Go API. CompileMIR fills this arena directly from checked
// source. Production lowering reads indexed arena edges. The source-compiled
// form also carries immutable source-text and package
// visibility sidecars. Decoded KIR retains source names and coordinates for
// diagnostics but does not include source text or those sidecars.
//
// The recursive JSON KIR document remains the interchange and artifact format.
// Keeping this wrapper separate makes the validation boundary explicit and
// avoids serializing and decoding the document between in-process compiler
// stages.
type ValidatedMIR struct {
	arena            *KIRArena
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
	// The frontend AST is intentionally mutable for parser and editor users.
	// Recheck it at the trust boundary so a caller cannot mutate a checked tree
	// and reuse the checker's stale expression types or scopes to mint MIR.
	if diagnostic := ValidateASTLimits(program, checker.Lim); diagnostic != nil {
		return nil, fmt.Errorf("%s", diagnostic.Message)
	}
	clearProgramAnalysis(program)
	checked, diagnostic := Check(program, checker.Lim)
	if diagnostic != nil {
		return nil, fmt.Errorf("program no longer passes type checking: %s", diagnostic.Message)
	}
	checker = checked
	kirTarget := KIRTarget{OS: target.OS, Arch: target.Arch, GUI: target.GUI}
	if !validKIRTarget(kirTarget) {
		return nil, fmt.Errorf("invalid lowered MIR: unsupported target %s-%s", kirTarget.OS, kirTarget.Arch)
	}
	arena, err := buildKIRArenaFromCheckedSource(program, checker, target)
	if err != nil {
		return nil, fmt.Errorf("build validated MIR arena from checked source: %w", err)
	}
	if err := arena.validateReferences(); err != nil {
		return nil, fmt.Errorf("invalid lowered MIR arena: %w", err)
	}
	if arena.Version == KIRVersion {
		if err := validateKIRArenaV6Metadata(arena); err != nil {
			return nil, fmt.Errorf("invalid lowered KIR v6 metadata: %w", err)
		}
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
	return &ValidatedMIR{arena: arena, limits: checker.Lim, sources: sources, visibilityScopes: visibilityScopes, hasSourceContext: true}, nil
}

// clearProgramAnalysis removes checker and constant-folding caches before the
// fresh trust-boundary check. Those fields are derived from the mutable AST and
// can otherwise make Check return stale results after a caller edits a node.
func clearProgramAnalysis(program *Program) {
	if program == nil {
		return
	}
	var clearExpression func(*Expr)
	var clearStatement func(*Stmt)
	var clearFunction func(*Function)
	clearExpression = func(expression *Expr) {
		if expression == nil {
			return
		}
		expression.Type = nil
		expression.Scope = nil
		expression.Function = nil
		expression.GenericArguments = nil
		expression.Captures = nil
		expression.ConstValue = nil
		for _, child := range []*Expr{expression.Left, expression.Right, expression.Operand, expression.Callee, expression.Base, expression.Receiver} {
			clearExpression(child)
		}
		for _, list := range [][]*Expr{expression.Args, expression.Items, expression.MapKeys, expression.Values} {
			for _, child := range list {
				clearExpression(child)
			}
		}
		if expression.Lambda != nil {
			clearFunction(expression.Lambda)
		}
	}
	clearStatement = func(statement *Stmt) {
		if statement == nil {
			return
		}
		statement.Type = nil
		for _, expression := range []*Expr{statement.Init, statement.Expr, statement.Target, statement.Value, statement.Cond, statement.Iter, statement.Return, statement.Scrutinee} {
			clearExpression(expression)
		}
		for _, list := range [][]*Stmt{statement.Then, statement.Else, statement.Body} {
			for _, child := range list {
				clearStatement(child)
			}
		}
		for index := range statement.Arms {
			statement.Arms[index].Pattern.BindingType = nil
			for _, child := range statement.Arms[index].Body {
				clearStatement(child)
			}
		}
	}
	clearFunction = func(function *Function) {
		if function == nil {
			return
		}
		for index := range function.Params {
			clearExpression(function.Params[index].Default)
		}
		for _, statement := range function.Body {
			clearStatement(statement)
		}
	}
	for _, function := range program.Functions {
		clearFunction(function)
	}
	for _, statement := range program.Statements {
		clearStatement(statement)
	}
	for _, structure := range program.Structs {
		if structure == nil {
			continue
		}
		structure.Type = nil
		for index := range structure.Fields {
			structure.Fields[index].Type = nil
		}
	}
	for _, enumeration := range program.Enums {
		if enumeration != nil {
			enumeration.Type = nil
		}
	}
	for _, trait := range program.Traits {
		if trait == nil {
			continue
		}
		for _, method := range trait.Methods {
			clearFunction(method)
		}
	}
	for _, implementation := range program.TraitImpls {
		if implementation == nil {
			continue
		}
		for _, method := range implementation.Methods {
			clearFunction(method)
		}
	}
}

// documentView creates the recursive KIR wire projection from the canonical
// arena for serialization. Production lowerers consume indexed arena edges;
// this temporary view is never retained by ValidatedMIR.
func (mir *ValidatedMIR) documentView() (*KIRDocument, error) {
	if mir == nil || mir.arena == nil {
		return nil, fmt.Errorf("missing validated MIR")
	}
	return mir.arena.toKIRDocument()
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
	checkArrayLength := func(label string, count int) {
		if limitFailure == "" && limits.MaxArrayElements > 0 && count > limits.MaxArrayElements {
			limitFailure = "IR array element limit exceeded in " + label
		}
	}
	var visitExpr func(*Expr, int)
	var visitStmt func(*Stmt, int)
	visitExpr = func(expression *Expr, depth int) {
		if expression == nil || limitFailure != "" || !add(depth) {
			return
		}
		checkArrayLength("expression arguments", len(expression.Args))
		checkArrayLength("expression items", len(expression.Items))
		checkArrayLength("expression values", len(expression.Values))
		checkArrayLength("expression map keys", len(expression.MapKeys))
		checkArrayLength("expression fields", len(expression.Fields))
		checkArrayLength("generic arguments", len(expression.GenericArguments))
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
		checkArrayLength("then block", len(statement.Then))
		checkArrayLength("else block", len(statement.Else))
		checkArrayLength("statement body", len(statement.Body))
		checkArrayLength("match arms", len(statement.Arms))
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
		checkArrayLength("function parameters", len(function.Params))
		checkArrayLength("function type parameters", len(function.TypeParams))
		checkArrayLength("function body", len(function.Body))
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
	checkArrayLength("top-level functions", len(program.Functions))
	checkArrayLength("top-level statements", len(program.Statements))
	checkArrayLength("imports", len(program.Imports))
	checkArrayLength("sources", len(program.Sources))
	checkArrayLength("struct declarations", len(program.Structs))
	checkArrayLength("enum declarations", len(program.Enums))
	checkArrayLength("trait declarations", len(program.Traits))
	checkArrayLength("trait implementations", len(program.TraitImpls))
	for _, structure := range program.Structs {
		if structure != nil {
			checkArrayLength("struct fields", len(structure.Fields))
			checkArrayLength("struct type parameters", len(structure.TypeParams))
		}
	}
	for _, enumeration := range program.Enums {
		if enumeration != nil {
			checkArrayLength("enum variants", len(enumeration.Variants))
		}
	}
	for _, trait := range program.Traits {
		if trait != nil {
			checkArrayLength("trait methods", len(trait.Methods))
		}
	}
	for _, implementation := range program.TraitImpls {
		if implementation != nil {
			checkArrayLength("trait implementation methods", len(implementation.Methods))
		}
	}
	if limitFailure != "" {
		return Diag(CatResource, program.Source, 1, 1, "%s", limitFailure)
	}
	return nil
}
