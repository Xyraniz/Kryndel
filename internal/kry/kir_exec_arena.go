package kry

import (
	"fmt"
	"math"
	"strings"
)

// kirExecArenaView exposes scalar rows to the interpreter and resolves every
// recursive edge explicitly through KIRArena. Its node adapters never attach
// expression or statement child pointers.
type kirExecArenaView struct {
	arena       *KIRArena
	expressions []*KIRExpr
	statements  []*KIRStmt
	functions   []*KIRFunction
	parameters  []*KIRParam
	patterns    []*KIRPattern
	arms        []*KIRArm
}

type kirExprEdge uint8

const (
	kirExprLeft kirExprEdge = iota
	kirExprRight
	kirExprOperand
	kirExprBase
	kirExprReceiver
	kirExprCallee
)

type kirExprList uint8

const (
	kirExprArgs kirExprList = iota
	kirExprItems
	kirExprMapKeys
	kirExprValues
)

type kirStmtEdge uint8

const (
	kirStmtInit kirStmtEdge = iota
	kirStmtExpr
	kirStmtTarget
	kirStmtValue
	kirStmtCond
	kirStmtIter
	kirStmtReturn
	kirStmtScrutinee
)

type kirStmtList uint8

const (
	kirStmtThen kirStmtList = iota
	kirStmtElse
	kirStmtBody
)

func newKIRExecArenaView(arena *KIRArena) (*kirExecArenaView, error) {
	if arena == nil {
		return nil, fmt.Errorf("missing validated MIR arena")
	}
	if err := arena.validateReferences(); err != nil {
		return nil, fmt.Errorf("invalid validated MIR arena: %w", err)
	}
	view := &kirExecArenaView{
		arena:       arena,
		expressions: make([]*KIRExpr, len(arena.Expressions)),
		statements:  make([]*KIRStmt, len(arena.Statements)),
		functions:   make([]*KIRFunction, len(arena.Functions)),
		parameters:  make([]*KIRParam, len(arena.Parameters)),
		patterns:    make([]*KIRPattern, len(arena.Patterns)),
		arms:        make([]*KIRArm, len(arena.Arms)),
	}
	// Materialize scalar adapters before execution so the shared interpreter
	// view is immutable while worker goroutines read it.
	for index := range arena.Expressions {
		view.expression(MIRRef{Index: MIRIndex(index), Present: true})
	}
	for index := range arena.Statements {
		view.statement(MIRRef{Index: MIRIndex(index), Present: true})
	}
	for index := range arena.Parameters {
		view.parameter(MIRIndex(index))
	}
	for index := range arena.Functions {
		view.function(MIRRef{Index: MIRIndex(index), Present: true})
	}
	for index := range arena.Patterns {
		view.pattern(MIRRef{Index: MIRIndex(index), Present: true})
	}
	for index := range arena.Arms {
		view.arm(MIRIndex(index))
	}
	return view, nil
}

func (view *kirExecArenaView) binding(ref MIRRef) *KIRBinding {
	if !ref.Present || uint64(ref.Index) >= uint64(len(view.arena.Bindings)) {
		return nil
	}
	value := view.arena.Bindings[ref.Index]
	return &value
}

func (view *kirExecArenaView) expression(ref MIRRef) *KIRExpr {
	if !ref.Present || uint64(ref.Index) >= uint64(len(view.arena.Expressions)) {
		return nil
	}
	if value := view.expressions[ref.Index]; value != nil {
		return value
	}
	row := view.arena.Expressions[ref.Index]
	value := row.Value
	value.GenericArguments = cloneKIRStrings(row.Value.GenericArguments)
	value.Fields = cloneKIRStrings(row.Value.Fields)
	value.Binding = view.binding(row.Binding)
	value.arenaRef = ref
	node := &value
	view.expressions[ref.Index] = node
	return node
}

func (view *kirExecArenaView) expressionRow(expression *KIRExpr) (*MIRExpression, bool) {
	if expression == nil || !expression.arenaRef.Present || uint64(expression.arenaRef.Index) >= uint64(len(view.arena.Expressions)) {
		return nil, false
	}
	return &view.arena.Expressions[expression.arenaRef.Index], true
}

func (view *kirExecArenaView) expressionEdge(expression *KIRExpr, edge kirExprEdge) *KIRExpr {
	if row, ok := view.expressionRow(expression); ok {
		var ref MIRRef
		switch edge {
		case kirExprLeft:
			ref = row.Left
		case kirExprRight:
			ref = row.Right
		case kirExprOperand:
			ref = row.Operand
		case kirExprBase:
			ref = row.Base
		case kirExprReceiver:
			ref = row.Receiver
		case kirExprCallee:
			ref = row.Callee
		default:
			return nil
		}
		return view.expression(ref)
	}
	return nil
}

func (view *kirExecArenaView) expressionList(list MIRNodeRefList) []*KIRExpr {
	indices, err := view.arena.indexList(view.arena.ExpressionRefs, list)
	if err != nil {
		return nil
	}
	values := make([]*KIRExpr, len(indices))
	for index, ref := range indices {
		values[index] = view.expression(MIRRef{Index: ref, Present: true})
	}
	return values
}

func (view *kirExecArenaView) expressionEdges(expression *KIRExpr, edges kirExprList) []*KIRExpr {
	if row, ok := view.expressionRow(expression); ok {
		var list MIRNodeRefList
		switch edges {
		case kirExprArgs:
			list = row.Args
		case kirExprItems:
			list = row.Items
		case kirExprMapKeys:
			list = row.MapKeys
		case kirExprValues:
			list = row.Values
		default:
			return nil
		}
		return view.expressionList(list)
	}
	return nil
}

func (view *kirExecArenaView) statement(ref MIRRef) *KIRStmt {
	if !ref.Present || uint64(ref.Index) >= uint64(len(view.arena.Statements)) {
		return nil
	}
	if value := view.statements[ref.Index]; value != nil {
		return value
	}
	row := view.arena.Statements[ref.Index]
	value := row.Value
	value.Binding = view.binding(row.Binding)
	value.arenaRef = ref
	node := &value
	view.statements[ref.Index] = node
	return node
}

func (view *kirExecArenaView) statementRow(statement *KIRStmt) (*MIRStatement, bool) {
	if statement == nil || !statement.arenaRef.Present || uint64(statement.arenaRef.Index) >= uint64(len(view.arena.Statements)) {
		return nil, false
	}
	return &view.arena.Statements[statement.arenaRef.Index], true
}

func (view *kirExecArenaView) statementEdge(statement *KIRStmt, edge kirStmtEdge) *KIRExpr {
	if row, ok := view.statementRow(statement); ok {
		var ref MIRRef
		switch edge {
		case kirStmtInit:
			ref = row.Init
		case kirStmtExpr:
			ref = row.Expr
		case kirStmtTarget:
			ref = row.Target
		case kirStmtValue:
			ref = row.ValueExpr
		case kirStmtCond:
			ref = row.Cond
		case kirStmtIter:
			ref = row.Iter
		case kirStmtReturn:
			ref = row.Return
		case kirStmtScrutinee:
			ref = row.Scrutinee
		default:
			return nil
		}
		return view.expression(ref)
	}
	return nil
}

func (view *kirExecArenaView) statementList(list MIRNodeRefList) []*KIRStmt {
	indices, err := view.arena.indexList(view.arena.StatementRefs, list)
	if err != nil {
		return nil
	}
	values := make([]*KIRStmt, len(indices))
	for index, ref := range indices {
		values[index] = view.statement(MIRRef{Index: ref, Present: true})
	}
	return values
}

func (view *kirExecArenaView) statementEdges(statement *KIRStmt, edges kirStmtList) []*KIRStmt {
	if row, ok := view.statementRow(statement); ok {
		var list MIRNodeRefList
		switch edges {
		case kirStmtThen:
			list = row.Then
		case kirStmtElse:
			list = row.Else
		case kirStmtBody:
			list = row.Body
		default:
			return nil
		}
		return view.statementList(list)
	}
	return nil
}

func (view *kirExecArenaView) parameter(ref MIRIndex) *KIRParam {
	if uint64(ref) >= uint64(len(view.arena.Parameters)) {
		return nil
	}
	if value := view.parameters[ref]; value != nil {
		return value
	}
	row := view.arena.Parameters[ref]
	value := row.Value
	value.Binding = view.binding(row.Binding)
	value.arenaRef = MIRRef{Index: ref, Present: true}
	node := &value
	view.parameters[ref] = node
	return node
}

func (view *kirExecArenaView) parameterDefault(parameter *KIRParam) *KIRExpr {
	if parameter == nil || !parameter.arenaRef.Present || uint64(parameter.arenaRef.Index) >= uint64(len(view.arena.Parameters)) {
		return nil
	}
	return view.expression(view.arena.Parameters[parameter.arenaRef.Index].Default)
}

func (view *kirExecArenaView) parameterList(list MIRNodeRefList) []*KIRParam {
	indices, err := view.arena.indexList(view.arena.ParameterRefs, list)
	if err != nil {
		return nil
	}
	values := make([]*KIRParam, len(indices))
	for index, ref := range indices {
		values[index] = view.parameter(ref)
	}
	return values
}

func (view *kirExecArenaView) function(ref MIRRef) *KIRFunction {
	if !ref.Present || uint64(ref.Index) >= uint64(len(view.arena.Functions)) {
		return nil
	}
	if value := view.functions[ref.Index]; value != nil {
		return value
	}
	row := view.arena.Functions[ref.Index]
	value := row.Value
	value.TypeParams = append([]*KIRTypeParam(nil), row.Value.TypeParams...)
	value.Params = view.parameterList(row.Params)
	value.Captures = view.captureList(row.Captures)
	value.arenaRef = ref
	node := &value
	view.functions[ref.Index] = node
	return node
}

func (view *kirExecArenaView) functionRow(function *KIRFunction) (*MIRFunction, bool) {
	if function == nil || !function.arenaRef.Present || uint64(function.arenaRef.Index) >= uint64(len(view.arena.Functions)) {
		return nil, false
	}
	return &view.arena.Functions[function.arenaRef.Index], true
}

func (view *kirExecArenaView) functionBody(function *KIRFunction) []*KIRStmt {
	if row, ok := view.functionRow(function); ok {
		return view.statementList(row.Body)
	}
	return nil
}

func (view *kirExecArenaView) functionList(list MIRNodeRefList) []*KIRFunction {
	indices, err := view.arena.indexList(view.arena.FunctionRefs, list)
	if err != nil {
		return nil
	}
	values := make([]*KIRFunction, len(indices))
	for index, ref := range indices {
		values[index] = view.function(MIRRef{Index: ref, Present: true})
	}
	return values
}

func (view *kirExecArenaView) captureList(list MIRNodeRefList) []*KIRCapture {
	indices, err := view.arena.indexList(view.arena.CaptureRefs, list)
	if err != nil {
		return nil
	}
	values := make([]*KIRCapture, len(indices))
	for index, ref := range indices {
		if uint64(ref) < uint64(len(view.arena.Captures)) {
			values[index] = &KIRCapture{Binding: view.binding(view.arena.Captures[ref].Binding)}
		}
	}
	return values
}

func (view *kirExecArenaView) pattern(ref MIRRef) *KIRPattern {
	if !ref.Present || uint64(ref.Index) >= uint64(len(view.arena.Patterns)) {
		return nil
	}
	if value := view.patterns[ref.Index]; value != nil {
		return value
	}
	row := view.arena.Patterns[ref.Index]
	value := row.Value
	value.ResolvedBinding = view.binding(row.Binding)
	value.arenaRef = ref
	node := &value
	view.patterns[ref.Index] = node
	return node
}

func (view *kirExecArenaView) arm(ref MIRIndex) *KIRArm {
	if uint64(ref) >= uint64(len(view.arena.Arms)) {
		return nil
	}
	if value := view.arms[ref]; value != nil {
		return value
	}
	row := view.arena.Arms[ref]
	copy := row.Value
	copy.Span = cloneKIRSourceSpan(row.Value.Span)
	copy.Pattern, copy.Body = view.pattern(row.Pattern), nil
	copy.arenaRef = MIRRef{Index: ref, Present: true}
	value := &copy
	view.arms[ref] = value
	return value
}

func (view *kirExecArenaView) armList(list MIRNodeRefList) []*KIRArm {
	indices, err := view.arena.indexList(view.arena.ArmRefs, list)
	if err != nil {
		return nil
	}
	values := make([]*KIRArm, len(indices))
	for index, ref := range indices {
		values[index] = view.arm(ref)
	}
	return values
}

func (view *kirExecArenaView) armBody(arm *KIRArm) []*KIRStmt {
	if arm == nil || !arm.arenaRef.Present || uint64(arm.arenaRef.Index) >= uint64(len(view.arena.Arms)) {
		return nil
	}
	return view.statementList(view.arena.Arms[arm.arenaRef.Index].Body)
}

func (view *kirExecArenaView) statementArms(statement *KIRStmt) []*KIRArm {
	if row, ok := view.statementRow(statement); ok {
		return view.armList(row.Arms)
	}
	return nil
}

func (executor *kirExecutor) expressionEdge(expression *KIRExpr, edge kirExprEdge) *KIRExpr {
	if expression == nil {
		return nil
	}
	if executor.arenaView != nil {
		return executor.arenaView.expressionEdge(expression, edge)
	}
	switch edge {
	case kirExprLeft:
		return expression.Left
	case kirExprRight:
		return expression.Right
	case kirExprOperand:
		return expression.Operand
	case kirExprBase:
		return expression.Base
	case kirExprReceiver:
		return expression.Receiver
	case kirExprCallee:
		return expression.Callee
	default:
		return nil
	}
}

func (executor *kirExecutor) expressionList(expression *KIRExpr, edges kirExprList) []*KIRExpr {
	if expression == nil {
		return nil
	}
	if executor.arenaView != nil {
		return executor.arenaView.expressionEdges(expression, edges)
	}
	switch edges {
	case kirExprArgs:
		return expression.Args
	case kirExprItems:
		return expression.Items
	case kirExprMapKeys:
		return expression.MapKeys
	case kirExprValues:
		return expression.Values
	default:
		return nil
	}
}

func (executor *kirExecutor) exprLeft(value *KIRExpr) *KIRExpr {
	return executor.expressionEdge(value, kirExprLeft)
}
func (executor *kirExecutor) exprRight(value *KIRExpr) *KIRExpr {
	return executor.expressionEdge(value, kirExprRight)
}
func (executor *kirExecutor) exprOperand(value *KIRExpr) *KIRExpr {
	return executor.expressionEdge(value, kirExprOperand)
}
func (executor *kirExecutor) exprBase(value *KIRExpr) *KIRExpr {
	return executor.expressionEdge(value, kirExprBase)
}
func (executor *kirExecutor) exprReceiver(value *KIRExpr) *KIRExpr {
	return executor.expressionEdge(value, kirExprReceiver)
}
func (executor *kirExecutor) exprCallee(value *KIRExpr) *KIRExpr {
	return executor.expressionEdge(value, kirExprCallee)
}
func (executor *kirExecutor) exprLambda(value *KIRExpr) *KIRFunction {
	if executor.arenaView == nil {
		return value.Lambda
	}
	if row, ok := executor.arenaView.expressionRow(value); ok {
		return executor.arenaView.function(row.Lambda)
	}
	return nil
}
func (executor *kirExecutor) exprArgs(value *KIRExpr) []*KIRExpr {
	return executor.expressionList(value, kirExprArgs)
}
func (executor *kirExecutor) exprItems(value *KIRExpr) []*KIRExpr {
	return executor.expressionList(value, kirExprItems)
}
func (executor *kirExecutor) exprMapKeys(value *KIRExpr) []*KIRExpr {
	return executor.expressionList(value, kirExprMapKeys)
}
func (executor *kirExecutor) exprValues(value *KIRExpr) []*KIRExpr {
	return executor.expressionList(value, kirExprValues)
}

func (executor *kirExecutor) statementEdge(statement *KIRStmt, edge kirStmtEdge) *KIRExpr {
	if statement == nil {
		return nil
	}
	if executor.arenaView != nil {
		return executor.arenaView.statementEdge(statement, edge)
	}
	switch edge {
	case kirStmtInit:
		return statement.Init
	case kirStmtExpr:
		return statement.Expr
	case kirStmtTarget:
		return statement.Target
	case kirStmtValue:
		return statement.Value
	case kirStmtCond:
		return statement.Cond
	case kirStmtIter:
		return statement.Iter
	case kirStmtReturn:
		return statement.Return
	case kirStmtScrutinee:
		return statement.Scrutinee
	default:
		return nil
	}
}

func (executor *kirExecutor) statementList(statement *KIRStmt, edges kirStmtList) []*KIRStmt {
	if statement == nil {
		return nil
	}
	if executor.arenaView != nil {
		return executor.arenaView.statementEdges(statement, edges)
	}
	switch edges {
	case kirStmtThen:
		return statement.Then
	case kirStmtElse:
		return statement.Else
	case kirStmtBody:
		return statement.Body
	default:
		return nil
	}
}

func (executor *kirExecutor) stmtInit(value *KIRStmt) *KIRExpr {
	return executor.statementEdge(value, kirStmtInit)
}
func (executor *kirExecutor) stmtExpr(value *KIRStmt) *KIRExpr {
	return executor.statementEdge(value, kirStmtExpr)
}
func (executor *kirExecutor) stmtTarget(value *KIRStmt) *KIRExpr {
	return executor.statementEdge(value, kirStmtTarget)
}
func (executor *kirExecutor) stmtValue(value *KIRStmt) *KIRExpr {
	return executor.statementEdge(value, kirStmtValue)
}
func (executor *kirExecutor) stmtCond(value *KIRStmt) *KIRExpr {
	return executor.statementEdge(value, kirStmtCond)
}
func (executor *kirExecutor) stmtIter(value *KIRStmt) *KIRExpr {
	return executor.statementEdge(value, kirStmtIter)
}
func (executor *kirExecutor) stmtReturn(value *KIRStmt) *KIRExpr {
	return executor.statementEdge(value, kirStmtReturn)
}
func (executor *kirExecutor) stmtScrutinee(value *KIRStmt) *KIRExpr {
	return executor.statementEdge(value, kirStmtScrutinee)
}
func (executor *kirExecutor) stmtThen(value *KIRStmt) []*KIRStmt {
	return executor.statementList(value, kirStmtThen)
}
func (executor *kirExecutor) stmtElse(value *KIRStmt) []*KIRStmt {
	return executor.statementList(value, kirStmtElse)
}
func (executor *kirExecutor) stmtBody(value *KIRStmt) []*KIRStmt {
	return executor.statementList(value, kirStmtBody)
}
func (executor *kirExecutor) stmtArms(value *KIRStmt) []*KIRArm {
	if executor.arenaView == nil {
		return value.Arms
	}
	return executor.arenaView.statementArms(value)
}
func (executor *kirExecutor) armBody(value *KIRArm) []*KIRStmt {
	if executor.arenaView == nil {
		return value.Body
	}
	return executor.arenaView.armBody(value)
}
func (executor *kirExecutor) functionBody(value *KIRFunction) []*KIRStmt {
	if executor.arenaView == nil {
		return value.Body
	}
	return executor.arenaView.functionBody(value)
}
func (executor *kirExecutor) parameterDefault(value *KIRParam) *KIRExpr {
	if executor.arenaView == nil {
		return value.Default
	}
	return executor.arenaView.parameterDefault(value)
}
func (executor *kirExecutor) requiredParams(parameters []*KIRParam) int {
	for index, parameter := range parameters {
		if parameter != nil && executor.parameterDefault(parameter) != nil {
			return index
		}
	}
	return len(parameters)
}

func (executor *kirExecutor) selfBinding(function *KIRFunction) (*KIRBinding, bool) {
	if executor.arenaView == nil {
		return kirExecSelfBinding(function)
	}
	view := executor.arenaView
	var found *KIRBinding
	valid := true
	seenExpressions := make(map[MIRIndex]bool)
	seenStatements := make(map[MIRIndex]bool)
	seenFunctions := make(map[MIRIndex]bool)
	var visitFunction func(*KIRFunction)
	var visitExpr func(*KIRExpr)
	var visitStmt func(*KIRStmt)
	visitFunction = func(candidate *KIRFunction) {
		if candidate == nil || !valid {
			return
		}
		if index := candidate.arenaRef; index.Present {
			if seenFunctions[index.Index] {
				return
			}
			seenFunctions[index.Index] = true
		}
		for _, capture := range candidate.Captures {
			if capture != nil && capture.Binding != nil && capture.Binding.Name == "self" {
				if found == nil {
					found = capture.Binding
				} else if !sameKIRBinding(found, capture.Binding) {
					valid = false
				}
			}
		}
		for _, statement := range view.functionBody(candidate) {
			visitStmt(statement)
		}
	}
	visitExpr = func(expression *KIRExpr) {
		if expression == nil || !valid {
			return
		}
		if index := expression.arenaRef; index.Present {
			if seenExpressions[index.Index] {
				return
			}
			seenExpressions[index.Index] = true
		}
		if expression.Kind == "var" && expression.Name == "self" {
			if expression.Binding == nil || !validKIRBinding(expression.Binding) {
				valid = false
			} else if found == nil {
				found = expression.Binding
			} else if !sameKIRBinding(found, expression.Binding) {
				valid = false
			}
		}
		visitFunction(executor.exprLambda(expression))
		visitExpr(executor.exprLeft(expression))
		visitExpr(executor.exprRight(expression))
		visitExpr(executor.exprOperand(expression))
		visitExpr(executor.exprCallee(expression))
		visitExpr(executor.exprBase(expression))
		visitExpr(executor.exprReceiver(expression))
		for _, child := range executor.exprArgs(expression) {
			visitExpr(child)
		}
		for _, child := range executor.exprItems(expression) {
			visitExpr(child)
		}
		for _, child := range executor.exprMapKeys(expression) {
			visitExpr(child)
		}
		for _, child := range executor.exprValues(expression) {
			visitExpr(child)
		}
	}
	visitStmt = func(statement *KIRStmt) {
		if statement == nil || !valid {
			return
		}
		if index := statement.arenaRef; index.Present {
			if seenStatements[index.Index] {
				return
			}
			seenStatements[index.Index] = true
		}
		visitExpr(executor.stmtInit(statement))
		visitExpr(executor.stmtExpr(statement))
		visitExpr(executor.stmtTarget(statement))
		visitExpr(executor.stmtValue(statement))
		visitExpr(executor.stmtCond(statement))
		visitExpr(executor.stmtIter(statement))
		visitExpr(executor.stmtReturn(statement))
		visitExpr(executor.stmtScrutinee(statement))
		for _, child := range executor.stmtThen(statement) {
			visitStmt(child)
		}
		for _, child := range executor.stmtElse(statement) {
			visitStmt(child)
		}
		for _, child := range executor.stmtBody(statement) {
			visitStmt(child)
		}
		for _, arm := range executor.stmtArms(statement) {
			for _, child := range executor.armBody(arm) {
				visitStmt(child)
			}
		}
	}
	visitFunction(function)
	return found, valid
}

func (executor *kirExecutor) validateArenaExecSubset(allowProcessArgs bool) error {
	view := executor.arenaView
	if view == nil || view.arena == nil || executor.metadata.Format != KIRFormat {
		return fmt.Errorf("invalid MIR executable: interpreter preflight has no typed arena")
	}
	arena := view.arena
	if arena.Version < 3 || arena.Version > KIRVersion {
		return fmt.Errorf("%w: KIR v3 or newer resolved bindings are required", errKIRSubsetUnsupported)
	}
	if len(view.statementList(arena.TopStatements)) == 0 && len(view.functionList(arena.TopFunctions)) == 0 {
		return fmt.Errorf("%w: no executable statements", errKIRSubsetUnsupported)
	}
	topFunctions := view.functionList(arena.TopFunctions)
	if len(view.statementList(arena.TopStatements)) == 0 {
		main, _ := kirExecEntryFunctionFromArena(view, executor.functions)
		if main == nil {
			return fmt.Errorf("%w: programs without top-level statements require a unique main()", errKIRSubsetUnsupported)
		}
		if (main.Return != "Nil" && main.Return != "Result[Nil, String]") || len(main.Params) != 0 {
			return fmt.Errorf("%w: main must be a zero-argument function returning Nil or Result[Nil, String]", errKIRSubsetUnsupported)
		}
	}
	root := newKIRExecScope(nil)
	root.types = make(map[string]string)
	root.allowProcessArgs = allowProcessArgs
	for _, declaration := range arena.Structs {
		if declaration == nil {
			return fmt.Errorf("invalid MIR executable: struct list contains a missing node")
		}
		structScope := newKIRExecScope(root)
		structScope.types = make(map[string]string, len(declaration.TypeParams))
		for _, parameter := range declaration.TypeParams {
			if parameter == nil || parameter.Name == "" || !kirExecConstraintKnown(parameter.Constraint, executor.metadata) {
				return fmt.Errorf("invalid MIR executable: struct %q has an invalid type parameter", declaration.Name)
			}
			if _, exists := structScope.types[parameter.Name]; exists {
				return fmt.Errorf("invalid MIR executable: struct %q repeats type parameter %q", declaration.Name, parameter.Name)
			}
			structScope.types[parameter.Name] = parameter.Constraint
			if _, exists := root.types[parameter.Name]; !exists {
				root.types[parameter.Name] = parameter.Constraint
			}
		}
		for _, field := range declaration.Fields {
			if field == nil || !kirExecTypeInScope(field.Type, structScope, executor.metadata) {
				return fmt.Errorf("%w: struct %q field has unsupported type", errKIRSubsetUnsupported, declaration.Name)
			}
		}
	}
	topFunctionSet := make(map[*KIRFunction]bool, len(topFunctions))
	for _, function := range topFunctions {
		if function == nil {
			return fmt.Errorf("invalid MIR executable: function list contains a missing node")
		}
		topFunctionSet[function] = true
		if function.Unsafe || len(function.Captures) != 0 {
			return fmt.Errorf("%w: unsafe functions and top-level captures are outside the interpreter function subset", errKIRSubsetUnsupported)
		}
		if function.Worker && (function.Receiver != "" || function.Trait != "" || len(function.TypeParams) != 0 || len(function.Params) != 0) {
			return fmt.Errorf("%w: workers must be non-generic zero-argument top-level functions", errKIRSubsetUnsupported)
		}
	}
	for index := range arena.Functions {
		function := view.function(MIRRef{Index: MIRIndex(index), Present: true})
		functionScope := newKIRExecScope(root)
		functionScope.types = make(map[string]string, len(function.TypeParams))
		if function.Receiver != "" {
			receiverStruct, receiverParameters, ok := kirExecStructType(executor.metadata, function.Receiver)
			if !ok {
				return fmt.Errorf("%w: method %q has unsupported receiver type %q", errKIRSubsetUnsupported, function.Name, function.Receiver)
			}
			for _, parameter := range receiverStruct.TypeParams {
				if parameter == nil || !kirExecConstraintKnown(parameter.Constraint, executor.metadata) {
					return fmt.Errorf("invalid MIR executable: method %q receiver has invalid type parameter", function.Name)
				}
				argument := receiverParameters[parameter.Name]
				if argument == parameter.Name {
					functionScope.types[argument] = parameter.Constraint
				} else if !kirExecTypeInScope(argument, functionScope, executor.metadata) || !kirExecConstraintSatisfied(argument, parameter.Constraint, functionScope, executor.metadata) {
					return fmt.Errorf("invalid MIR executable: method %q receiver type argument %q does not satisfy %s", function.Name, argument, parameter.Constraint)
				}
			}
		}
		for _, parameter := range function.TypeParams {
			if parameter == nil || parameter.Name == "" || !kirExecConstraintKnown(parameter.Constraint, executor.metadata) {
				return fmt.Errorf("invalid MIR executable: function %q has an invalid type parameter", function.Name)
			}
			if _, exists := functionScope.types[parameter.Name]; exists {
				return fmt.Errorf("invalid MIR executable: function %q repeats type parameter %q", function.Name, parameter.Name)
			}
			functionScope.types[parameter.Name] = parameter.Constraint
			if _, exists := root.types[parameter.Name]; !exists {
				root.types[parameter.Name] = parameter.Constraint
			}
		}
		if topFunctionSet[function] && function.Receiver != "" {
			selfBinding, valid := executor.selfBinding(function)
			if !valid || (selfBinding != nil && selfBinding.Type != function.Receiver) {
				return fmt.Errorf("invalid MIR executable: method %q has inconsistent self binding metadata", function.Name)
			}
		}
		if !kirExecTypeInScope(function.Return, functionScope, executor.metadata) {
			return fmt.Errorf("%w: function %q return type %q", errKIRSubsetUnsupported, function.Name, function.Return)
		}
		for _, parameter := range function.Params {
			if parameter == nil || !kirExecTypeInScope(parameter.Type, functionScope, executor.metadata) {
				return fmt.Errorf("%w: function %q has an unsupported parameter type", errKIRSubsetUnsupported, function.Name)
			}
		}
	}
	for index := range arena.Expressions {
		expression := &arena.Expressions[index].Value
		if !kirExecTypeInScope(expression.Type, root, executor.metadata) {
			return fmt.Errorf("%w: expression type %q", errKIRSubsetUnsupported, expression.Type)
		}
		switch expression.Kind {
		case "int", "float", "bool", "string", "nil", "var", "lambda", "unary", "binary", "array", "map", "set", "enum", "struct", "index", "field", "propagate", "call":
		default:
			return fmt.Errorf("%w: expression kind %q", errKIRSubsetUnsupported, expression.Kind)
		}
		if expression.Kind == "float" && (math.IsNaN(expression.Float) || math.IsInf(expression.Float, 0)) {
			return fmt.Errorf("invalid MIR executable: malformed Float literal")
		}
		if expression.Kind == "call" || (expression.Kind == "var" && expression.CallTarget != "") {
			if expression.CallTarget == "" && arena.Expressions[index].Callee.Present {
				continue
			}
			prefix, target, ok := strings.Cut(expression.CallTarget, ":")
			if !ok || target == "" {
				return fmt.Errorf("invalid MIR executable: call has malformed resolved target")
			}
			switch prefix {
			case "builtin":
				builtin, exists := lookupBuiltin(target)
				if !exists || (!kirExecBuiltinSupported(builtin) && !(allowProcessArgs && target == "process_args")) {
					return fmt.Errorf("%w: builtin %q has no interpreter implementation", errKIRSubsetUnsupported, target)
				}
			case "function":
				function := executor.functions[target]
				if function == nil || function.Name != expression.Name {
					return fmt.Errorf("invalid MIR executable: function call references unknown target %q", target)
				}
			case "trait":
				traitName, methodName, found := strings.Cut(target, "::")
				if !found || kirExecTraitMethod(kirExecTraitDefinition(executor.metadata, traitName), methodName) == nil {
					return fmt.Errorf("invalid MIR executable: trait call references unknown target %q", target)
				}
			default:
				return fmt.Errorf("%w: call target kind %q", errKIRSubsetUnsupported, prefix)
			}
		}
	}
	for index := range arena.Statements {
		switch arena.Statements[index].Value.Kind {
		case "let", "const", "assign", "expr", "if", "while", "for", "break", "continue", "defer", "unsafe", "match", "return":
		default:
			return fmt.Errorf("%w: statement kind %q", errKIRSubsetUnsupported, arena.Statements[index].Value.Kind)
		}
	}
	for index := range arena.Patterns {
		switch arena.Patterns[index].Value.Kind {
		case "wildcard", "nil", "bool", "int", "string", "enum", "option", "result":
		default:
			return fmt.Errorf("%w: match pattern kind %q", errKIRSubsetUnsupported, arena.Patterns[index].Value.Kind)
		}
	}
	return nil
}
