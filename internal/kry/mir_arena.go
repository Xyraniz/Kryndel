package kry

import "fmt"

// MIRIndex names an entry in one of the typed node tables in KIRArena. Child
// edges are stored as indexes instead of recursive pointers. Optional edges
// carry an explicit Present bit; zero is therefore a valid node index.
type MIRIndex uint32

type MIRRef struct {
	Index   MIRIndex
	Present bool
}

// MIRNodeRefList is an index range into the matching reference table.
type MIRNodeRefList struct {
	Start uint32
	Count uint32
}

// KIRArena is the canonical, non-recursive typed storage used by ValidatedMIR.
// The KIR* values in node rows contain scalar payload only: recursive edges
// and collections are empty there and live in the indexed tables below.
// Declaration metadata is copied into its own tables because it has no child
// expression edges in the current KIR schema.
type KIRArena struct {
	Format          string
	Version         int
	LanguageVersion string
	Module          string
	Source          string
	Target          KIRTarget
	Imports         []string
	ImportRecords   []*KIRImport
	Sources         []string
	Structs         []*KIRStruct
	Enums           []*KIREnum
	Traits          []*KIRTrait
	TraitImpls      []*KIRTraitImpl

	Expressions []MIRExpression
	Statements  []MIRStatement
	Functions   []MIRFunction
	Parameters  []MIRParameter
	Captures    []MIRCapture
	Patterns    []MIRPattern
	Arms        []MIRArm
	Bindings    []KIRBinding
	Values      []MIRValue

	ExpressionRefs []MIRIndex
	StatementRefs  []MIRIndex
	FunctionRefs   []MIRIndex
	ParameterRefs  []MIRIndex
	CaptureRefs    []MIRIndex
	ArmRefs        []MIRIndex
	ValueRefs      []MIRIndex

	TopFunctions  MIRNodeRefList
	TopStatements MIRNodeRefList
}

type MIRExpression struct {
	Value    KIRExpr
	Left     MIRRef
	Right    MIRRef
	Operand  MIRRef
	Base     MIRRef
	Receiver MIRRef
	Callee   MIRRef
	Lambda   MIRRef
	Binding  MIRRef
	Const    MIRRef
	Args     MIRNodeRefList
	Items    MIRNodeRefList
	MapKeys  MIRNodeRefList
	Values   MIRNodeRefList
}

type MIRStatement struct {
	Value     KIRStmt
	Binding   MIRRef
	Init      MIRRef
	Expr      MIRRef
	Target    MIRRef
	ValueExpr MIRRef
	Cond      MIRRef
	Iter      MIRRef
	Return    MIRRef
	Scrutinee MIRRef
	Then      MIRNodeRefList
	Else      MIRNodeRefList
	Body      MIRNodeRefList
	Arms      MIRNodeRefList
}

type MIRFunction struct {
	Value    KIRFunction
	Body     MIRNodeRefList
	Params   MIRNodeRefList
	Captures MIRNodeRefList
}

type MIRParameter struct {
	Value   KIRParam
	Default MIRRef
	Binding MIRRef
}

type MIRCapture struct{ Binding MIRRef }

type MIRPattern struct {
	Value   KIRPattern
	Binding MIRRef
}

type MIRArm struct {
	Value   KIRArm
	Pattern MIRRef
	Body    MIRNodeRefList
}

type MIRValue struct {
	Value KIRValue
	Inner MIRRef
	Array MIRNodeRefList
}

func (arena *KIRArena) toKIRDocument() (*KIRDocument, error) {
	if arena == nil {
		return nil, fmt.Errorf("missing validated KIR arena")
	}
	if err := arena.validateReferences(); err != nil {
		return nil, fmt.Errorf("invalid validated KIR arena: %w", err)
	}
	document := &KIRDocument{
		Format: arena.Format, Version: arena.Version, LanguageVersion: arena.LanguageVersion,
		Module: arena.Module, Source: arena.Source, Target: arena.Target,
		Imports: cloneKIRStrings(arena.Imports), ImportRecords: cloneKIRImports(arena.ImportRecords), Sources: cloneKIRStrings(arena.Sources),
		Structs: cloneKIRStructs(arena.Structs), Enums: cloneKIREnums(arena.Enums),
		Traits: cloneKIRTraits(arena.Traits), TraitImpls: cloneKIRTraitImpls(arena.TraitImpls),
	}
	var err error
	if document.Functions, err = arena.functionList(arena.TopFunctions); err != nil {
		return nil, err
	}
	if document.Statements, err = arena.statementList(arena.TopStatements); err != nil {
		return nil, err
	}
	return document, nil
}

func (arena *KIRArena) expression(ref MIRRef) (*KIRExpr, error) {
	if !ref.Present {
		return nil, nil
	}
	if uint64(ref.Index) >= uint64(len(arena.Expressions)) {
		return nil, fmt.Errorf("expression reference %d is out of bounds", ref.Index)
	}
	row := arena.Expressions[ref.Index]
	value := row.Value
	value.Span = cloneKIRSourceSpan(row.Value.Span)
	value.GenericArguments = cloneKIRStrings(row.Value.GenericArguments)
	value.Fields = cloneKIRStrings(row.Value.Fields)
	var err error
	if value.Left, err = arena.expression(row.Left); err != nil {
		return nil, err
	}
	if value.Right, err = arena.expression(row.Right); err != nil {
		return nil, err
	}
	if value.Operand, err = arena.expression(row.Operand); err != nil {
		return nil, err
	}
	if value.Base, err = arena.expression(row.Base); err != nil {
		return nil, err
	}
	if value.Receiver, err = arena.expression(row.Receiver); err != nil {
		return nil, err
	}
	if value.Callee, err = arena.expression(row.Callee); err != nil {
		return nil, err
	}
	if value.Lambda, err = arena.function(row.Lambda); err != nil {
		return nil, err
	}
	if value.Binding, err = arena.binding(row.Binding); err != nil {
		return nil, err
	}
	if value.Const, err = arena.value(row.Const); err != nil {
		return nil, err
	}
	if value.Args, err = arena.expressionList(row.Args); err != nil {
		return nil, err
	}
	if value.Items, err = arena.expressionList(row.Items); err != nil {
		return nil, err
	}
	if value.MapKeys, err = arena.expressionList(row.MapKeys); err != nil {
		return nil, err
	}
	if value.Values, err = arena.expressionList(row.Values); err != nil {
		return nil, err
	}
	return &value, nil
}

func (arena *KIRArena) expressionList(list MIRNodeRefList) ([]*KIRExpr, error) {
	indexes, err := arena.indexList(arena.ExpressionRefs, list)
	if err != nil {
		return nil, err
	}
	values := make([]*KIRExpr, len(indexes))
	for i, index := range indexes {
		values[i], err = arena.expression(MIRRef{Index: index, Present: true})
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

func (arena *KIRArena) statement(ref MIRRef) (*KIRStmt, error) {
	if !ref.Present {
		return nil, nil
	}
	if uint64(ref.Index) >= uint64(len(arena.Statements)) {
		return nil, fmt.Errorf("statement reference %d is out of bounds", ref.Index)
	}
	row := arena.Statements[ref.Index]
	value := row.Value
	value.Span = cloneKIRSourceSpan(row.Value.Span)
	var err error
	if value.Binding, err = arena.binding(row.Binding); err != nil {
		return nil, err
	}
	if value.Init, err = arena.expression(row.Init); err != nil {
		return nil, err
	}
	if value.Expr, err = arena.expression(row.Expr); err != nil {
		return nil, err
	}
	if value.Target, err = arena.expression(row.Target); err != nil {
		return nil, err
	}
	if value.Value, err = arena.expression(row.ValueExpr); err != nil {
		return nil, err
	}
	if value.Cond, err = arena.expression(row.Cond); err != nil {
		return nil, err
	}
	if value.Iter, err = arena.expression(row.Iter); err != nil {
		return nil, err
	}
	if value.Return, err = arena.expression(row.Return); err != nil {
		return nil, err
	}
	if value.Scrutinee, err = arena.expression(row.Scrutinee); err != nil {
		return nil, err
	}
	if value.Then, err = arena.statementList(row.Then); err != nil {
		return nil, err
	}
	if value.Else, err = arena.statementList(row.Else); err != nil {
		return nil, err
	}
	if value.Body, err = arena.statementList(row.Body); err != nil {
		return nil, err
	}
	if value.Arms, err = arena.armList(row.Arms); err != nil {
		return nil, err
	}
	return &value, nil
}

func (arena *KIRArena) statementList(list MIRNodeRefList) ([]*KIRStmt, error) {
	indexes, err := arena.indexList(arena.StatementRefs, list)
	if err != nil {
		return nil, err
	}
	values := make([]*KIRStmt, len(indexes))
	for i, index := range indexes {
		values[i], err = arena.statement(MIRRef{Index: index, Present: true})
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

func (arena *KIRArena) function(ref MIRRef) (*KIRFunction, error) {
	if !ref.Present {
		return nil, nil
	}
	if uint64(ref.Index) >= uint64(len(arena.Functions)) {
		return nil, fmt.Errorf("function reference %d is out of bounds", ref.Index)
	}
	row := arena.Functions[ref.Index]
	value := row.Value
	value.Span = cloneKIRSourceSpan(row.Value.Span)
	value.TypeParams = cloneKIRTypeParams(row.Value.TypeParams)
	var err error
	if value.Body, err = arena.statementList(row.Body); err != nil {
		return nil, err
	}
	if value.Params, err = arena.parameterList(row.Params); err != nil {
		return nil, err
	}
	if value.Captures, err = arena.captureList(row.Captures); err != nil {
		return nil, err
	}
	return &value, nil
}

func (arena *KIRArena) functionList(list MIRNodeRefList) ([]*KIRFunction, error) {
	indexes, err := arena.indexList(arena.FunctionRefs, list)
	if err != nil {
		return nil, err
	}
	values := make([]*KIRFunction, len(indexes))
	for i, index := range indexes {
		values[i], err = arena.function(MIRRef{Index: index, Present: true})
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

func (arena *KIRArena) parameterList(list MIRNodeRefList) ([]*KIRParam, error) {
	indexes, err := arena.indexList(arena.ParameterRefs, list)
	if err != nil {
		return nil, err
	}
	values := make([]*KIRParam, len(indexes))
	for i, index := range indexes {
		if uint64(index) >= uint64(len(arena.Parameters)) {
			return nil, fmt.Errorf("parameter reference %d is out of bounds", index)
		}
		row := arena.Parameters[index]
		value := row.Value
		value.Span = cloneKIRSourceSpan(row.Value.Span)
		if value.Default, err = arena.expression(row.Default); err != nil {
			return nil, err
		}
		if value.Binding, err = arena.binding(row.Binding); err != nil {
			return nil, err
		}
		values[i] = &value
	}
	return values, nil
}

func (arena *KIRArena) captureList(list MIRNodeRefList) ([]*KIRCapture, error) {
	indexes, err := arena.indexList(arena.CaptureRefs, list)
	if err != nil {
		return nil, err
	}
	values := make([]*KIRCapture, len(indexes))
	for i, index := range indexes {
		if uint64(index) >= uint64(len(arena.Captures)) {
			return nil, fmt.Errorf("capture reference %d is out of bounds", index)
		}
		binding, err := arena.binding(arena.Captures[index].Binding)
		if err != nil {
			return nil, err
		}
		values[i] = &KIRCapture{Binding: binding}
	}
	return values, nil
}

func (arena *KIRArena) armList(list MIRNodeRefList) ([]*KIRArm, error) {
	indexes, err := arena.indexList(arena.ArmRefs, list)
	if err != nil {
		return nil, err
	}
	values := make([]*KIRArm, len(indexes))
	for i, index := range indexes {
		if uint64(index) >= uint64(len(arena.Arms)) {
			return nil, fmt.Errorf("match arm reference %d is out of bounds", index)
		}
		row := arena.Arms[index]
		pattern, err := arena.pattern(row.Pattern)
		if err != nil {
			return nil, err
		}
		body, err := arena.statementList(row.Body)
		if err != nil {
			return nil, err
		}
		value := row.Value
		value.Span = cloneKIRSourceSpan(value.Span)
		value.Pattern, value.Body = pattern, body
		values[i] = &value
	}
	return values, nil
}

func (arena *KIRArena) pattern(ref MIRRef) (*KIRPattern, error) {
	if !ref.Present {
		return nil, nil
	}
	if uint64(ref.Index) >= uint64(len(arena.Patterns)) {
		return nil, fmt.Errorf("pattern reference %d is out of bounds", ref.Index)
	}
	value := arena.Patterns[ref.Index].Value
	binding, err := arena.binding(arena.Patterns[ref.Index].Binding)
	if err != nil {
		return nil, err
	}
	value.ResolvedBinding = binding
	return &value, nil
}

func (arena *KIRArena) value(ref MIRRef) (*KIRValue, error) {
	if !ref.Present {
		return nil, nil
	}
	if uint64(ref.Index) >= uint64(len(arena.Values)) {
		return nil, fmt.Errorf("constant reference %d is out of bounds", ref.Index)
	}
	row := arena.Values[ref.Index]
	value := row.Value
	value.Bytes = cloneKIRBytes(row.Value.Bytes)
	var err error
	if value.Inner, err = arena.value(row.Inner); err != nil {
		return nil, err
	}
	indexes, err := arena.indexList(arena.ValueRefs, row.Array)
	if err != nil {
		return nil, err
	}
	value.Array = make([]*KIRValue, len(indexes))
	for i, index := range indexes {
		value.Array[i], err = arena.value(MIRRef{Index: index, Present: true})
		if err != nil {
			return nil, err
		}
	}
	return &value, nil
}

func (arena *KIRArena) binding(ref MIRRef) (*KIRBinding, error) {
	if !ref.Present {
		return nil, nil
	}
	if uint64(ref.Index) >= uint64(len(arena.Bindings)) {
		return nil, fmt.Errorf("binding reference %d is out of bounds", ref.Index)
	}
	value := arena.Bindings[ref.Index]
	return &value, nil
}

func (arena *KIRArena) indexList(refs []MIRIndex, list MIRNodeRefList) ([]MIRIndex, error) {
	end := uint64(list.Start) + uint64(list.Count)
	if end > uint64(len(refs)) {
		return nil, fmt.Errorf("node reference range is out of bounds")
	}
	return refs[list.Start:uint32(end)], nil
}

func newKIRArena(document *KIRDocument) (*KIRArena, error) {
	if document == nil {
		return nil, fmt.Errorf("missing KIR document")
	}
	arena := &KIRArena{
		Format: document.Format, Version: document.Version, LanguageVersion: document.LanguageVersion,
		Module: document.Module, Source: document.Source, Target: document.Target,
		Imports: cloneKIRStrings(document.Imports), ImportRecords: cloneKIRImports(document.ImportRecords), Sources: cloneKIRStrings(document.Sources),
		Structs: cloneKIRStructs(document.Structs), Enums: cloneKIREnums(document.Enums),
		Traits: cloneKIRTraits(document.Traits), TraitImpls: cloneKIRTraitImpls(document.TraitImpls),
	}
	var err error
	arena.TopFunctions, err = arena.appendFunctionList(document.Functions)
	if err != nil {
		return nil, err
	}
	arena.TopStatements, err = arena.appendStatementList(document.Statements)
	if err != nil {
		return nil, err
	}
	if err := arena.validateReferences(); err != nil {
		return nil, err
	}
	return arena, nil
}

func (arena *KIRArena) appendBinding(binding *KIRBinding) MIRRef {
	if binding == nil {
		return MIRRef{}
	}
	index := MIRIndex(len(arena.Bindings))
	arena.Bindings = append(arena.Bindings, *binding)
	return MIRRef{Index: index, Present: true}
}

func (arena *KIRArena) appendExpression(expression *KIRExpr) (MIRRef, error) {
	if expression == nil {
		return MIRRef{}, nil
	}
	index := MIRIndex(len(arena.Expressions))
	node := MIRExpression{Value: *expression}
	node.Value.Left, node.Value.Right, node.Value.Operand = nil, nil, nil
	node.Value.Base, node.Value.Receiver, node.Value.Callee = nil, nil, nil
	node.Value.Lambda, node.Value.Binding, node.Value.Const = nil, nil, nil
	node.Value.Args, node.Value.Items, node.Value.MapKeys, node.Value.Values = nil, nil, nil, nil
	node.Value.GenericArguments = cloneKIRStrings(expression.GenericArguments)
	node.Value.Fields = cloneKIRStrings(expression.Fields)
	arena.Expressions = append(arena.Expressions, node)
	ref, err := arena.appendExpression(expression.Left)
	if err != nil {
		return MIRRef{}, err
	}
	arena.Expressions[index].Left = ref
	ref, err = arena.appendExpression(expression.Right)
	if err != nil {
		return MIRRef{}, err
	}
	arena.Expressions[index].Right = ref
	ref, err = arena.appendExpression(expression.Operand)
	if err != nil {
		return MIRRef{}, err
	}
	arena.Expressions[index].Operand = ref
	ref, err = arena.appendExpression(expression.Base)
	if err != nil {
		return MIRRef{}, err
	}
	arena.Expressions[index].Base = ref
	ref, err = arena.appendExpression(expression.Receiver)
	if err != nil {
		return MIRRef{}, err
	}
	arena.Expressions[index].Receiver = ref
	ref, err = arena.appendExpression(expression.Callee)
	if err != nil {
		return MIRRef{}, err
	}
	arena.Expressions[index].Callee = ref
	if expression.Lambda != nil {
		ref, err := arena.appendFunction(expression.Lambda)
		if err != nil {
			return MIRRef{}, err
		}
		arena.Expressions[index].Lambda = ref
	}
	arena.Expressions[index].Binding = arena.appendBinding(expression.Binding)
	if expression.Const != nil {
		ref, err := arena.appendValue(expression.Const)
		if err != nil {
			return MIRRef{}, err
		}
		arena.Expressions[index].Const = ref
	}
	if arena.Expressions[index].Args, err = arena.appendExpressionList(expression.Args); err != nil {
		return MIRRef{}, err
	}
	if arena.Expressions[index].Items, err = arena.appendExpressionList(expression.Items); err != nil {
		return MIRRef{}, err
	}
	if arena.Expressions[index].MapKeys, err = arena.appendExpressionList(expression.MapKeys); err != nil {
		return MIRRef{}, err
	}
	if arena.Expressions[index].Values, err = arena.appendExpressionList(expression.Values); err != nil {
		return MIRRef{}, err
	}
	return MIRRef{Index: index, Present: true}, nil
}

func (arena *KIRArena) appendExpressionList(expressions []*KIRExpr) (MIRNodeRefList, error) {
	indexes := make([]MIRIndex, 0, len(expressions))
	for _, expression := range expressions {
		ref, err := arena.appendExpression(expression)
		if err != nil {
			return MIRNodeRefList{}, err
		}
		if !ref.Present {
			return MIRNodeRefList{}, fmt.Errorf("expression list contains a missing node")
		}
		indexes = append(indexes, ref.Index)
	}
	start := len(arena.ExpressionRefs)
	arena.ExpressionRefs = append(arena.ExpressionRefs, indexes...)
	return MIRNodeRefList{Start: uint32(start), Count: uint32(len(expressions))}, nil
}

func (arena *KIRArena) appendStatement(statement *KIRStmt) (MIRRef, error) {
	if statement == nil {
		return MIRRef{}, nil
	}
	index := MIRIndex(len(arena.Statements))
	node := MIRStatement{Value: *statement}
	node.Value.Binding = nil
	node.Value.Init, node.Value.Expr, node.Value.Target, node.Value.Value = nil, nil, nil, nil
	node.Value.Cond, node.Value.Iter, node.Value.Return, node.Value.Scrutinee = nil, nil, nil, nil
	node.Value.Then, node.Value.Else, node.Value.Body, node.Value.Arms = nil, nil, nil, nil
	arena.Statements = append(arena.Statements, node)
	arena.Statements[index].Binding = arena.appendBinding(statement.Binding)
	ref, err := arena.appendExpression(statement.Init)
	if err != nil {
		return MIRRef{}, err
	}
	arena.Statements[index].Init = ref
	ref, err = arena.appendExpression(statement.Expr)
	if err != nil {
		return MIRRef{}, err
	}
	arena.Statements[index].Expr = ref
	ref, err = arena.appendExpression(statement.Target)
	if err != nil {
		return MIRRef{}, err
	}
	arena.Statements[index].Target = ref
	ref, err = arena.appendExpression(statement.Value)
	if err != nil {
		return MIRRef{}, err
	}
	arena.Statements[index].ValueExpr = ref
	ref, err = arena.appendExpression(statement.Cond)
	if err != nil {
		return MIRRef{}, err
	}
	arena.Statements[index].Cond = ref
	ref, err = arena.appendExpression(statement.Iter)
	if err != nil {
		return MIRRef{}, err
	}
	arena.Statements[index].Iter = ref
	ref, err = arena.appendExpression(statement.Return)
	if err != nil {
		return MIRRef{}, err
	}
	arena.Statements[index].Return = ref
	ref, err = arena.appendExpression(statement.Scrutinee)
	if err != nil {
		return MIRRef{}, err
	}
	arena.Statements[index].Scrutinee = ref
	if arena.Statements[index].Then, err = arena.appendStatementList(statement.Then); err != nil {
		return MIRRef{}, err
	}
	if arena.Statements[index].Else, err = arena.appendStatementList(statement.Else); err != nil {
		return MIRRef{}, err
	}
	if arena.Statements[index].Body, err = arena.appendStatementList(statement.Body); err != nil {
		return MIRRef{}, err
	}
	if arena.Statements[index].Arms, err = arena.appendArmList(statement.Arms); err != nil {
		return MIRRef{}, err
	}
	return MIRRef{Index: index, Present: true}, nil
}

func (arena *KIRArena) appendStatementList(statements []*KIRStmt) (MIRNodeRefList, error) {
	indexes := make([]MIRIndex, 0, len(statements))
	for _, statement := range statements {
		ref, err := arena.appendStatement(statement)
		if err != nil {
			return MIRNodeRefList{}, err
		}
		if !ref.Present {
			return MIRNodeRefList{}, fmt.Errorf("statement list contains a missing node")
		}
		indexes = append(indexes, ref.Index)
	}
	start := len(arena.StatementRefs)
	arena.StatementRefs = append(arena.StatementRefs, indexes...)
	return MIRNodeRefList{Start: uint32(start), Count: uint32(len(statements))}, nil
}

func (arena *KIRArena) appendFunction(function *KIRFunction) (MIRRef, error) {
	if function == nil {
		return MIRRef{}, nil
	}
	index := MIRIndex(len(arena.Functions))
	node := MIRFunction{Value: *function}
	node.Value.Body, node.Value.Params, node.Value.Captures = nil, nil, nil
	node.Value.TypeParams = cloneKIRTypeParams(function.TypeParams)
	arena.Functions = append(arena.Functions, node)
	var err error
	if arena.Functions[index].Body, err = arena.appendStatementList(function.Body); err != nil {
		return MIRRef{}, err
	}
	if arena.Functions[index].Params, err = arena.appendParameterList(function.Params); err != nil {
		return MIRRef{}, err
	}
	if arena.Functions[index].Captures, err = arena.appendCaptureList(function.Captures); err != nil {
		return MIRRef{}, err
	}
	return MIRRef{Index: index, Present: true}, nil
}

func (arena *KIRArena) appendFunctionList(functions []*KIRFunction) (MIRNodeRefList, error) {
	indexes := make([]MIRIndex, 0, len(functions))
	for _, function := range functions {
		ref, err := arena.appendFunction(function)
		if err != nil {
			return MIRNodeRefList{}, err
		}
		if !ref.Present {
			return MIRNodeRefList{}, fmt.Errorf("function list contains a missing node")
		}
		indexes = append(indexes, ref.Index)
	}
	start := len(arena.FunctionRefs)
	arena.FunctionRefs = append(arena.FunctionRefs, indexes...)
	return MIRNodeRefList{Start: uint32(start), Count: uint32(len(functions))}, nil
}

func (arena *KIRArena) appendParameterList(parameters []*KIRParam) (MIRNodeRefList, error) {
	indexes := make([]MIRIndex, 0, len(parameters))
	for _, parameter := range parameters {
		if parameter == nil {
			return MIRNodeRefList{}, fmt.Errorf("parameter list contains a missing node")
		}
		index := MIRIndex(len(arena.Parameters))
		node := MIRParameter{Value: *parameter}
		node.Value.Span = cloneKIRSourceSpan(parameter.Span)
		node.Value.Default, node.Value.Binding = nil, nil
		arena.Parameters = append(arena.Parameters, node)
		if parameter.Default != nil {
			ref, err := arena.appendExpression(parameter.Default)
			if err != nil {
				return MIRNodeRefList{}, err
			}
			arena.Parameters[index].Default = ref
		}
		arena.Parameters[index].Binding = arena.appendBinding(parameter.Binding)
		indexes = append(indexes, index)
	}
	start := len(arena.ParameterRefs)
	arena.ParameterRefs = append(arena.ParameterRefs, indexes...)
	return MIRNodeRefList{Start: uint32(start), Count: uint32(len(parameters))}, nil
}

func (arena *KIRArena) appendCaptureList(captures []*KIRCapture) (MIRNodeRefList, error) {
	indexes := make([]MIRIndex, 0, len(captures))
	for _, capture := range captures {
		if capture == nil {
			return MIRNodeRefList{}, fmt.Errorf("capture list contains a missing node")
		}
		index := MIRIndex(len(arena.Captures))
		arena.Captures = append(arena.Captures, MIRCapture{Binding: arena.appendBinding(capture.Binding)})
		indexes = append(indexes, index)
	}
	start := len(arena.CaptureRefs)
	arena.CaptureRefs = append(arena.CaptureRefs, indexes...)
	return MIRNodeRefList{Start: uint32(start), Count: uint32(len(captures))}, nil
}

func (arena *KIRArena) appendPattern(pattern *KIRPattern) (MIRRef, error) {
	if pattern == nil {
		return MIRRef{}, nil
	}
	index := MIRIndex(len(arena.Patterns))
	node := MIRPattern{Value: *pattern}
	node.Value.Span = cloneKIRSourceSpan(pattern.Span)
	node.Value.ResolvedBinding = nil
	arena.Patterns = append(arena.Patterns, node)
	arena.Patterns[index].Binding = arena.appendBinding(pattern.ResolvedBinding)
	return MIRRef{Index: index, Present: true}, nil
}

func (arena *KIRArena) appendArmList(arms []*KIRArm) (MIRNodeRefList, error) {
	indexes := make([]MIRIndex, 0, len(arms))
	for _, arm := range arms {
		if arm == nil {
			return MIRNodeRefList{}, fmt.Errorf("match arm list contains a missing node")
		}
		index := MIRIndex(len(arena.Arms))
		arena.Arms = append(arena.Arms, MIRArm{})
		pattern, err := arena.appendPattern(arm.Pattern)
		if err != nil {
			return MIRNodeRefList{}, err
		}
		body, err := arena.appendStatementList(arm.Body)
		if err != nil {
			return MIRNodeRefList{}, err
		}
		value := *arm
		value.Pattern, value.Body = nil, nil
		value.Span = cloneKIRSourceSpan(value.Span)
		arena.Arms[index] = MIRArm{Value: value, Pattern: pattern, Body: body}
		indexes = append(indexes, index)
	}
	start := len(arena.ArmRefs)
	arena.ArmRefs = append(arena.ArmRefs, indexes...)
	return MIRNodeRefList{Start: uint32(start), Count: uint32(len(arms))}, nil
}

func (arena *KIRArena) appendValue(value *KIRValue) (MIRRef, error) {
	if value == nil {
		return MIRRef{}, nil
	}
	index := MIRIndex(len(arena.Values))
	node := MIRValue{Value: *value}
	node.Value.Array, node.Value.Inner = nil, nil
	node.Value.Bytes = cloneKIRBytes(value.Bytes)
	arena.Values = append(arena.Values, node)
	if value.Inner != nil {
		ref, err := arena.appendValue(value.Inner)
		if err != nil {
			return MIRRef{}, err
		}
		arena.Values[index].Inner = ref
	}
	indexes := make([]MIRIndex, 0, len(value.Array))
	for _, child := range value.Array {
		ref, err := arena.appendValue(child)
		if err != nil {
			return MIRRef{}, err
		}
		if !ref.Present {
			return MIRRef{}, fmt.Errorf("constant value array contains a missing node")
		}
		indexes = append(indexes, ref.Index)
	}
	start := len(arena.ValueRefs)
	arena.ValueRefs = append(arena.ValueRefs, indexes...)
	arena.Values[index].Array = MIRNodeRefList{Start: uint32(start), Count: uint32(len(value.Array))}
	return MIRRef{Index: index, Present: true}, nil
}

func (arena *KIRArena) validateReferences() error {
	checkList := func(name string, refs []MIRIndex, list MIRNodeRefList, bound int) error {
		end := uint64(list.Start) + uint64(list.Count)
		if end > uint64(len(refs)) {
			return fmt.Errorf("%s reference range is out of bounds", name)
		}
		for _, ref := range refs[list.Start:uint32(end)] {
			if uint64(ref) >= uint64(bound) {
				return fmt.Errorf("%s reference %d is out of bounds", name, ref)
			}
		}
		return nil
	}
	checkOptional := func(name string, ref MIRRef, bound int) error {
		if !ref.Present && ref.Index != 0 {
			return fmt.Errorf("absent %s reference has a nonzero index", name)
		}
		if ref.Present && uint64(ref.Index) >= uint64(bound) {
			return fmt.Errorf("%s reference %d is out of bounds", name, ref.Index)
		}
		return nil
	}
	if err := checkList("top-level function", arena.FunctionRefs, arena.TopFunctions, len(arena.Functions)); err != nil {
		return err
	}
	if err := checkList("top-level statement", arena.StatementRefs, arena.TopStatements, len(arena.Statements)); err != nil {
		return err
	}
	for _, node := range arena.Expressions {
		if node.Value.Left != nil || node.Value.Right != nil || node.Value.Operand != nil || node.Value.Base != nil || node.Value.Receiver != nil || node.Value.Callee != nil || node.Value.Lambda != nil || node.Value.Binding != nil || node.Value.Const != nil || len(node.Value.Args) != 0 || len(node.Value.Items) != 0 || len(node.Value.MapKeys) != 0 || len(node.Value.Values) != 0 {
			return fmt.Errorf("expression node contains recursive wire fields")
		}
		for name, ref := range map[string]MIRRef{"left": node.Left, "right": node.Right, "operand": node.Operand, "base": node.Base, "receiver": node.Receiver, "callee": node.Callee} {
			if err := checkOptional(name, ref, len(arena.Expressions)); err != nil {
				return err
			}
		}
		for name, refBound := range map[string]struct {
			ref   MIRRef
			bound int
		}{"lambda": {node.Lambda, len(arena.Functions)}, "binding": {node.Binding, len(arena.Bindings)}, "constant": {node.Const, len(arena.Values)}} {
			if err := checkOptional(name, refBound.ref, refBound.bound); err != nil {
				return err
			}
		}
		for name, list := range map[string]MIRNodeRefList{"arguments": node.Args, "items": node.Items, "map keys": node.MapKeys, "values": node.Values} {
			if err := checkList(name, arena.ExpressionRefs, list, len(arena.Expressions)); err != nil {
				return err
			}
		}
	}
	for _, node := range arena.Statements {
		if node.Value.Binding != nil || node.Value.Init != nil || node.Value.Expr != nil || node.Value.Target != nil || node.Value.Value != nil || node.Value.Cond != nil || node.Value.Iter != nil || node.Value.Return != nil || node.Value.Scrutinee != nil || len(node.Value.Then) != 0 || len(node.Value.Else) != 0 || len(node.Value.Body) != 0 || len(node.Value.Arms) != 0 {
			return fmt.Errorf("statement node contains recursive wire fields")
		}
		for name, ref := range map[string]MIRRef{"init": node.Init, "expression": node.Expr, "target": node.Target, "value": node.ValueExpr, "condition": node.Cond, "iterator": node.Iter, "return": node.Return, "scrutinee": node.Scrutinee} {
			if err := checkOptional(name, ref, len(arena.Expressions)); err != nil {
				return err
			}
		}
		if err := checkOptional("binding", node.Binding, len(arena.Bindings)); err != nil {
			return err
		}
		for name, list := range map[string]MIRNodeRefList{"then": node.Then, "else": node.Else, "body": node.Body} {
			if err := checkList(name, arena.StatementRefs, list, len(arena.Statements)); err != nil {
				return err
			}
		}
		if err := checkList("match arm", arena.ArmRefs, node.Arms, len(arena.Arms)); err != nil {
			return err
		}
	}
	for _, node := range arena.Functions {
		if len(node.Value.Body) != 0 || len(node.Value.Params) != 0 || len(node.Value.Captures) != 0 {
			return fmt.Errorf("function node contains recursive wire fields")
		}
		if err := checkList("function body", arena.StatementRefs, node.Body, len(arena.Statements)); err != nil {
			return err
		}
		if err := checkList("function parameter", arena.ParameterRefs, node.Params, len(arena.Parameters)); err != nil {
			return err
		}
		if err := checkList("function capture", arena.CaptureRefs, node.Captures, len(arena.Captures)); err != nil {
			return err
		}
	}
	for _, node := range arena.Parameters {
		if node.Value.Default != nil || node.Value.Binding != nil {
			return fmt.Errorf("parameter node contains recursive wire fields")
		}
		if err := checkOptional("parameter default", node.Default, len(arena.Expressions)); err != nil {
			return err
		}
		if err := checkOptional("parameter binding", node.Binding, len(arena.Bindings)); err != nil {
			return err
		}
	}
	for _, node := range arena.Captures {
		if err := checkOptional("capture binding", node.Binding, len(arena.Bindings)); err != nil {
			return err
		}
	}
	for _, node := range arena.Patterns {
		if node.Value.ResolvedBinding != nil {
			return fmt.Errorf("pattern node contains recursive wire fields")
		}
		if err := checkOptional("pattern binding", node.Binding, len(arena.Bindings)); err != nil {
			return err
		}
	}
	for _, node := range arena.Arms {
		if err := checkOptional("arm pattern", node.Pattern, len(arena.Patterns)); err != nil {
			return err
		}
		if err := checkList("arm body", arena.StatementRefs, node.Body, len(arena.Statements)); err != nil {
			return err
		}
	}
	for _, node := range arena.Values {
		if node.Value.Inner != nil || len(node.Value.Array) != 0 {
			return fmt.Errorf("constant value node contains recursive wire fields")
		}
		if err := checkOptional("constant inner", node.Inner, len(arena.Values)); err != nil {
			return err
		}
		if err := checkList("constant array", arena.ValueRefs, node.Array, len(arena.Values)); err != nil {
			return err
		}
	}
	return nil
}

func cloneKIRTypeParams(values []*KIRTypeParam) []*KIRTypeParam {
	output := make([]*KIRTypeParam, len(values))
	for index, value := range values {
		if value != nil {
			copy := *value
			copy.Span = cloneKIRSourceSpan(value.Span)
			output[index] = &copy
		}
	}
	return output
}

func cloneKIRStructs(values []*KIRStruct) []*KIRStruct {
	output := make([]*KIRStruct, len(values))
	for index, value := range values {
		if value == nil {
			continue
		}
		copy := *value
		copy.Span = cloneKIRSourceSpan(value.Span)
		copy.TypeParams = cloneKIRTypeParams(value.TypeParams)
		copy.Fields = make([]*KIRField, len(value.Fields))
		for fieldIndex, field := range value.Fields {
			if field != nil {
				item := *field
				item.Span = cloneKIRSourceSpan(field.Span)
				copy.Fields[fieldIndex] = &item
			}
		}
		output[index] = &copy
	}
	return output
}

func cloneKIREnums(values []*KIREnum) []*KIREnum {
	output := make([]*KIREnum, len(values))
	for index, value := range values {
		if value != nil {
			copy := *value
			copy.Span = cloneKIRSourceSpan(value.Span)
			copy.Variants = cloneKIRStrings(value.Variants)
			copy.VariantSpans = make([]*KIRSourceSpan, len(value.VariantSpans))
			for spanIndex, span := range value.VariantSpans {
				copy.VariantSpans[spanIndex] = cloneKIRSourceSpan(span)
			}
			output[index] = &copy
		}
	}
	return output
}

func cloneKIRStrings(values []string) []string {
	if values == nil {
		return nil
	}
	output := make([]string, len(values))
	copy(output, values)
	return output
}

func cloneKIRImports(values []*KIRImport) []*KIRImport {
	output := make([]*KIRImport, len(values))
	for index, value := range values {
		if value != nil {
			copy := *value
			copy.Span = cloneKIRSourceSpan(value.Span)
			output[index] = &copy
		}
	}
	return output
}

func cloneKIRBytes(values []byte) []byte {
	if values == nil {
		return nil
	}
	output := make([]byte, len(values))
	copy(output, values)
	return output
}

func cloneKIRTraits(values []*KIRTrait) []*KIRTrait {
	output := make([]*KIRTrait, len(values))
	for index, value := range values {
		if value == nil {
			continue
		}
		copy := *value
		copy.Span = cloneKIRSourceSpan(value.Span)
		copy.Methods = make([]*KIRTraitMethod, len(value.Methods))
		for methodIndex, method := range value.Methods {
			if method == nil {
				continue
			}
			methodCopy := *method
			methodCopy.Span = cloneKIRSourceSpan(method.Span)
			methodCopy.Params = make([]*KIRParam, len(method.Params))
			for paramIndex, param := range method.Params {
				if param != nil {
					paramCopy := *param
					paramCopy.Span = cloneKIRSourceSpan(param.Span)
					paramCopy.Default = nil
					paramCopy.Binding = nil
					methodCopy.Params[paramIndex] = &paramCopy
				}
			}
			copy.Methods[methodIndex] = &methodCopy
		}
		output[index] = &copy
	}
	return output
}

func cloneKIRTraitImpls(values []*KIRTraitImpl) []*KIRTraitImpl {
	output := make([]*KIRTraitImpl, len(values))
	for index, value := range values {
		if value == nil {
			continue
		}
		copy := *value
		copy.Span = cloneKIRSourceSpan(value.Span)
		copy.Methods = make([]*KIRTraitImplMethod, len(value.Methods))
		for methodIndex, method := range value.Methods {
			if method != nil {
				methodCopy := *method
				copy.Methods[methodIndex] = &methodCopy
			}
		}
		output[index] = &copy
	}
	return output
}
