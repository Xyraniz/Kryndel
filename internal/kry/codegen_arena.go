package kry

// The C backend keeps scalar KIR rows as short lived typed views and follows
// every edge through KIRArena indexes. These adapters intentionally never
// attach recursive child pointers to an expression, statement, arm, or
// function.

func (g *cgen) expression(ref MIRRef) *KIRExpr {
	if !ref.Present {
		return nil
	}
	if uint64(ref.Index) >= uint64(len(g.arena.Expressions)) {
		g.fail("invalid validated MIR expression reference %d", ref.Index)
		return nil
	}
	if expression := g.expressionNodes[ref.Index]; expression != nil {
		return expression
	}
	row := g.arena.Expressions[ref.Index]
	value := row.Value
	value.GenericArguments = append([]string(nil), value.GenericArguments...)
	value.Fields = append([]string(nil), value.Fields...)
	value.Binding = g.binding(row.Binding)
	expression := &value
	g.expressionNodes[ref.Index] = expression
	g.expressionIndex[expression] = ref.Index
	return expression
}

func (g *cgen) statement(ref MIRRef) *KIRStmt {
	if !ref.Present {
		return nil
	}
	if uint64(ref.Index) >= uint64(len(g.arena.Statements)) {
		g.fail("invalid validated MIR statement reference %d", ref.Index)
		return nil
	}
	if statement := g.statementNodes[ref.Index]; statement != nil {
		return statement
	}
	row := g.arena.Statements[ref.Index]
	value := row.Value
	value.Binding = g.binding(row.Binding)
	statement := &value
	g.statementNodes[ref.Index] = statement
	g.statementIndex[statement] = ref.Index
	return statement
}

func (g *cgen) function(ref MIRRef) *KIRFunction {
	if !ref.Present {
		return nil
	}
	if uint64(ref.Index) >= uint64(len(g.arena.Functions)) {
		g.fail("invalid validated MIR function reference %d", ref.Index)
		return nil
	}
	if function := g.functionNodes[ref.Index]; function != nil {
		return function
	}
	row := g.arena.Functions[ref.Index]
	value := row.Value
	value.TypeParams = append([]*KIRTypeParam(nil), value.TypeParams...)
	value.Params = g.parameterList(row.Params)
	function := &value
	g.functionNodes[ref.Index] = function
	g.functionIndex[function] = int(ref.Index)
	return function
}

func (g *cgen) functionList(list MIRNodeRefList) []*KIRFunction {
	indexes, err := g.arena.indexList(g.arena.FunctionRefs, list)
	if err != nil {
		g.fail("invalid validated MIR function range: %v", err)
		return nil
	}
	functions := make([]*KIRFunction, len(indexes))
	for i, index := range indexes {
		functions[i] = g.function(MIRRef{Index: index, Present: true})
	}
	return functions
}

func (g *cgen) parameterList(list MIRNodeRefList) []*KIRParam {
	indexes, err := g.arena.indexList(g.arena.ParameterRefs, list)
	if err != nil {
		g.fail("invalid validated MIR parameter range: %v", err)
		return nil
	}
	parameters := make([]*KIRParam, len(indexes))
	for i, index := range indexes {
		if uint64(index) >= uint64(len(g.arena.Parameters)) {
			g.fail("invalid validated MIR parameter reference %d", index)
			return nil
		}
		row := g.arena.Parameters[index]
		value := row.Value
		value.Default = g.expression(row.Default)
		value.Binding = g.binding(row.Binding)
		parameters[i] = &value
	}
	return parameters
}

func (g *cgen) expressionList(list MIRNodeRefList) []*KIRExpr {
	indexes, err := g.arena.indexList(g.arena.ExpressionRefs, list)
	if err != nil {
		g.fail("invalid validated MIR expression range: %v", err)
		return nil
	}
	expressions := make([]*KIRExpr, len(indexes))
	for i, index := range indexes {
		expressions[i] = g.expression(MIRRef{Index: index, Present: true})
	}
	return expressions
}

func (g *cgen) statementList(list MIRNodeRefList) []*KIRStmt {
	indexes, err := g.arena.indexList(g.arena.StatementRefs, list)
	if err != nil {
		g.fail("invalid validated MIR statement range: %v", err)
		return nil
	}
	statements := make([]*KIRStmt, len(indexes))
	for i, index := range indexes {
		statements[i] = g.statement(MIRRef{Index: index, Present: true})
	}
	return statements
}

func (g *cgen) pattern(ref MIRRef) *KIRPattern {
	if !ref.Present {
		return nil
	}
	if uint64(ref.Index) >= uint64(len(g.arena.Patterns)) {
		g.fail("invalid validated MIR pattern reference %d", ref.Index)
		return nil
	}
	row := g.arena.Patterns[ref.Index]
	value := row.Value
	value.ResolvedBinding = g.binding(row.Binding)
	return &value
}

func (g *cgen) armList(list MIRNodeRefList) []*KIRArm {
	indexes, err := g.arena.indexList(g.arena.ArmRefs, list)
	if err != nil {
		g.fail("invalid validated MIR match-arm range: %v", err)
		return nil
	}
	arms := make([]*KIRArm, len(indexes))
	for i, index := range indexes {
		if uint64(index) >= uint64(len(g.arena.Arms)) {
			g.fail("invalid validated MIR match-arm reference %d", index)
			return nil
		}
		if arm := g.armNodes[index]; arm != nil {
			arms[i] = arm
			continue
		}
		row := g.arena.Arms[index]
		arm := &KIRArm{Pattern: g.pattern(row.Pattern)}
		g.armNodes[index] = arm
		g.armIndex[arm] = index
		arms[i] = arm
	}
	return arms
}

func (g *cgen) binding(ref MIRRef) *KIRBinding {
	if !ref.Present {
		return nil
	}
	if uint64(ref.Index) >= uint64(len(g.arena.Bindings)) {
		g.fail("invalid validated MIR binding reference %d", ref.Index)
		return nil
	}
	value := g.arena.Bindings[ref.Index]
	return &value
}

func (g *cgen) expressionRow(expression *KIRExpr) *MIRExpression {
	index, ok := g.expressionIndex[expression]
	if !ok || uint64(index) >= uint64(len(g.arena.Expressions)) {
		g.fail("C AOT received an expression outside validated MIR")
		return nil
	}
	return &g.arena.Expressions[index]
}

func (g *cgen) expressionEdge(expression *KIRExpr, edge func(MIRExpression) MIRRef) *KIRExpr {
	if row := g.expressionRow(expression); row != nil {
		return g.expression(edge(*row))
	}
	return nil
}

func (g *cgen) expressionEdges(expression *KIRExpr, edges func(MIRExpression) MIRNodeRefList) []*KIRExpr {
	if row := g.expressionRow(expression); row != nil {
		return g.expressionList(edges(*row))
	}
	return nil
}

func (g *cgen) statementRow(statement *KIRStmt) *MIRStatement {
	index, ok := g.statementIndex[statement]
	if !ok || uint64(index) >= uint64(len(g.arena.Statements)) {
		g.fail("C AOT received a statement outside validated MIR")
		return nil
	}
	return &g.arena.Statements[index]
}

func (g *cgen) statementEdge(statement *KIRStmt, edge func(MIRStatement) MIRRef) *KIRExpr {
	if row := g.statementRow(statement); row != nil {
		return g.expression(edge(*row))
	}
	return nil
}

func (g *cgen) statementEdges(statement *KIRStmt, edges func(MIRStatement) MIRNodeRefList) []*KIRStmt {
	if row := g.statementRow(statement); row != nil {
		return g.statementList(edges(*row))
	}
	return nil
}

func (g *cgen) statementArms(statement *KIRStmt) []*KIRArm {
	if row := g.statementRow(statement); row != nil {
		return g.armList(row.Arms)
	}
	return nil
}

func (g *cgen) armRow(arm *KIRArm) *MIRArm {
	index, ok := g.armIndex[arm]
	if !ok || uint64(index) >= uint64(len(g.arena.Arms)) {
		g.fail("C AOT received a match arm outside validated MIR")
		return nil
	}
	return &g.arena.Arms[index]
}

func (g *cgen) functionBody(function *KIRFunction) []*KIRStmt {
	index, ok := g.functionIndex[function]
	if !ok || index < 0 || index >= len(g.arena.Functions) {
		g.fail("C AOT received a function outside validated MIR")
		return nil
	}
	return g.statementList(g.arena.Functions[index].Body)
}

func (g *cgen) armBody(arm *KIRArm) []*KIRStmt {
	if row := g.armRow(arm); row != nil {
		return g.statementList(row.Body)
	}
	return nil
}
