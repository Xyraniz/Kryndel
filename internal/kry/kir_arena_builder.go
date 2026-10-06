package kry

import (
	"fmt"
	"sort"
)

// buildKIRArenaFromCheckedSource lowers checked frontend nodes straight into
// typed arena rows. It never builds or parses a recursive KIR document.
func buildKIRArenaFromCheckedSource(program *Program, checker *Checker, target NativeTarget) (*KIRArena, error) {
	metadata, err := buildKIRMetadata(program, target)
	if err != nil {
		return nil, err
	}
	arena := &KIRArena{
		Format: metadata.Format, Version: metadata.Version, LanguageVersion: metadata.LanguageVersion,
		Module: metadata.Module, Source: metadata.Source, Target: metadata.Target,
		Imports: cloneKIRStrings(metadata.Imports), ImportRecords: cloneKIRImports(metadata.ImportRecords), Sources: cloneKIRStrings(metadata.Sources),
		Structs: cloneKIRStructs(metadata.Structs), Enums: cloneKIREnums(metadata.Enums),
		Traits: cloneKIRTraits(metadata.Traits), TraitImpls: cloneKIRTraitImpls(metadata.TraitImpls),
	}
	paths := newKIRPathNames(program)
	builder := kirArenaBuilder{arena: arena, checker: checker, paths: paths, functionTargets: kirFunctionTargets(program, paths)}
	functions := make([]MIRIndex, 0, len(program.Functions))
	for _, function := range program.Functions {
		index, err := builder.function(function, nil, true)
		if err != nil {
			return nil, err
		}
		functions = append(functions, index)
	}
	arena.TopFunctions = builder.addFunctionRefs(functions)
	statements, err := builder.statements(program.Statements)
	if err != nil {
		return nil, err
	}
	arena.TopStatements = builder.addStatementRefs(statements)
	if err := arena.validateReferences(); err != nil {
		return nil, fmt.Errorf("invalid checked-source MIR arena: %w", err)
	}
	return arena, nil
}

type KIRMetadata struct {
	Format, LanguageVersion, Module, Source string
	Version                                 int
	Target                                  KIRTarget
	Imports, Sources                        []string
	ImportRecords                           []*KIRImport
	Structs                                 []*KIRStruct
	Enums                                   []*KIREnum
	Traits                                  []*KIRTrait
	TraitImpls                              []*KIRTraitImpl
}

// buildKIRMetadata creates only document-level metadata and declarations. It is
// shared by the portable tree builder and direct source-to-arena lowering.
func buildKIRMetadata(program *Program, target NativeTarget) (KIRMetadata, error) {
	if program == nil {
		return KIRMetadata{}, fmt.Errorf("missing checked program")
	}
	paths := newKIRPathNames(program)
	metadata := KIRMetadata{
		Format: KIRFormat, Version: KIRVersion, LanguageVersion: LanguageVersion, Module: paths.name(program.Module),
		Source: paths.source(program.Source), Target: KIRTarget{OS: target.OS, Arch: target.Arch, GUI: target.GUI},
		Imports: make([]string, 0, len(program.Imports)), ImportRecords: make([]*KIRImport, 0, len(program.Imports)), Sources: make([]string, 0, len(program.Sources)),
		Structs: make([]*KIRStruct, 0, len(program.Structs)), Enums: make([]*KIREnum, 0, len(program.Enums)),
		Traits: make([]*KIRTrait, 0, len(program.Traits)), TraitImpls: make([]*KIRTraitImpl, 0, len(program.TraitImpls)),
	}
	for _, item := range program.Imports {
		metadata.Imports = append(metadata.Imports, item.Path)
		metadata.ImportRecords = append(metadata.ImportRecords, &KIRImport{Path: item.Path, Source: paths.tokenSource(item.Tok), Line: item.Tok.Line, Column: item.Tok.Column, Span: kirTokenSpan(item.Tok)})
	}
	for _, source := range program.Sources {
		metadata.Sources = append(metadata.Sources, paths.source(source))
	}
	for _, declaration := range program.Structs {
		structure := &KIRStruct{Name: declaration.Name, Source: paths.tokenSource(declaration.Tok), Line: declaration.Tok.Line, Column: declaration.Tok.Column, Span: kirSourceSpan(declaration.Tok, declaration.EndToken), Public: declaration.Public, Module: paths.name(declaration.Module), TypeParams: make([]*KIRTypeParam, 0, len(declaration.TypeParams)), Fields: make([]*KIRField, 0, len(declaration.Fields))}
		for _, parameter := range declaration.TypeParams {
			structure.TypeParams = append(structure.TypeParams, &KIRTypeParam{Name: parameter.Name, Constraint: parameter.Constraint, Source: paths.tokenSource(parameter.Tok), Line: parameter.Tok.Line, Column: parameter.Tok.Column, Span: kirTokenSpan(parameter.Tok)})
		}
		for _, field := range declaration.Fields {
			structure.Fields = append(structure.Fields, &KIRField{Name: field.Name, Source: paths.tokenSource(field.Tok), Line: field.Tok.Line, Column: field.Tok.Column, Span: kirTokenSpan(field.Tok), Public: field.Public, Type: typeString(field.Type, field.Spec)})
		}
		metadata.Structs = append(metadata.Structs, structure)
	}
	for _, declaration := range program.Enums {
		enumeration := &KIREnum{Name: declaration.Name, Source: paths.tokenSource(declaration.Tok), Line: declaration.Tok.Line, Column: declaration.Tok.Column, Span: kirSourceSpan(declaration.Tok, declaration.EndToken), Public: declaration.Public, Module: paths.name(declaration.Module), Variants: append([]string(nil), declaration.Variants...), VariantSpans: make([]*KIRSourceSpan, 0, len(declaration.VariantTokens))}
		for _, token := range declaration.VariantTokens {
			enumeration.VariantSpans = append(enumeration.VariantSpans, kirTokenSpan(token))
		}
		metadata.Enums = append(metadata.Enums, enumeration)
	}
	for _, declaration := range program.Traits {
		trait := &KIRTrait{Name: declaration.Name, Source: paths.tokenSource(declaration.Tok), Line: declaration.Tok.Line, Column: declaration.Tok.Column, Span: kirSourceSpan(declaration.Tok, declaration.EndToken), Public: declaration.Public, Module: paths.name(declaration.Module), Methods: make([]*KIRTraitMethod, 0, len(declaration.Methods))}
		for _, method := range declaration.Methods {
			entry := &KIRTraitMethod{Name: method.Name, Source: paths.tokenSource(method.Tok), Line: method.Tok.Line, Column: method.Tok.Column, Span: kirSourceSpan(method.Tok, method.EndToken), Return: typeSpecString(method.Return), Params: make([]*KIRParam, 0, len(method.Params))}
			for _, parameter := range method.Params {
				entry.Params = append(entry.Params, &KIRParam{Name: parameter.Name, Type: typeSpecString(parameter.Type), Source: paths.tokenSource(parameter.Tok), Line: parameter.Tok.Line, Column: parameter.Tok.Column, Span: kirSourceSpan(parameter.Tok, parameter.EndToken)})
			}
			trait.Methods = append(trait.Methods, entry)
		}
		metadata.Traits = append(metadata.Traits, trait)
	}
	functionTargets := kirFunctionTargets(program, paths)
	for _, implementation := range program.TraitImpls {
		entry := &KIRTraitImpl{Trait: implementation.Trait, Source: paths.tokenSource(implementation.Tok), Line: implementation.Tok.Line, Column: implementation.Tok.Column, Span: kirSourceSpan(implementation.Tok, implementation.EndToken), For: typeSpecString(implementation.Target), Module: paths.name(implementation.Module), Methods: make([]*KIRTraitImplMethod, 0, len(implementation.Methods))}
		for _, method := range implementation.Methods {
			entry.Methods = append(entry.Methods, &KIRTraitImplMethod{Name: method.Name, Source: paths.tokenSource(method.Tok), Line: method.Tok.Line, Column: method.Tok.Column, Span: kirSourceSpan(method.Tok, method.EndToken), Target: "function:" + functionTargets[method]})
		}
		metadata.TraitImpls = append(metadata.TraitImpls, entry)
	}
	return metadata, nil
}

type kirArenaBuilder struct {
	arena           *KIRArena
	checker         *Checker
	paths           kirPathNames
	functionTargets map[*Function]string
}

func (builder *kirArenaBuilder) addFunctionRefs(indices []MIRIndex) MIRNodeRefList {
	start := uint32(len(builder.arena.FunctionRefs))
	builder.arena.FunctionRefs = append(builder.arena.FunctionRefs, indices...)
	return MIRNodeRefList{Start: start, Count: uint32(len(indices))}
}

func (builder *kirArenaBuilder) addStatementRefs(indices []MIRIndex) MIRNodeRefList {
	start := uint32(len(builder.arena.StatementRefs))
	builder.arena.StatementRefs = append(builder.arena.StatementRefs, indices...)
	return MIRNodeRefList{Start: start, Count: uint32(len(indices))}
}

func (builder *kirArenaBuilder) addExpressionRefs(indices []MIRIndex) MIRNodeRefList {
	start := uint32(len(builder.arena.ExpressionRefs))
	builder.arena.ExpressionRefs = append(builder.arena.ExpressionRefs, indices...)
	return MIRNodeRefList{Start: start, Count: uint32(len(indices))}
}

func (builder *kirArenaBuilder) addParameterRefs(indices []MIRIndex) MIRNodeRefList {
	start := uint32(len(builder.arena.ParameterRefs))
	builder.arena.ParameterRefs = append(builder.arena.ParameterRefs, indices...)
	return MIRNodeRefList{Start: start, Count: uint32(len(indices))}
}

func (builder *kirArenaBuilder) addCaptureRefs(indices []MIRIndex) MIRNodeRefList {
	start := uint32(len(builder.arena.CaptureRefs))
	builder.arena.CaptureRefs = append(builder.arena.CaptureRefs, indices...)
	return MIRNodeRefList{Start: start, Count: uint32(len(indices))}
}

func (builder *kirArenaBuilder) addArmRefs(indices []MIRIndex) MIRNodeRefList {
	start := uint32(len(builder.arena.ArmRefs))
	builder.arena.ArmRefs = append(builder.arena.ArmRefs, indices...)
	return MIRNodeRefList{Start: start, Count: uint32(len(indices))}
}

func (builder *kirArenaBuilder) addValueRefs(indices []MIRIndex) MIRNodeRefList {
	start := uint32(len(builder.arena.ValueRefs))
	builder.arena.ValueRefs = append(builder.arena.ValueRefs, indices...)
	return MIRNodeRefList{Start: start, Count: uint32(len(indices))}
}

func (builder *kirArenaBuilder) binding(value *KIRBinding) MIRRef {
	if value == nil {
		return MIRRef{}
	}
	index := MIRIndex(len(builder.arena.Bindings))
	builder.arena.Bindings = append(builder.arena.Bindings, *value)
	return MIRRef{Index: index, Present: true}
}

func (builder *kirArenaBuilder) expression(expression *Expr) (MIRRef, error) {
	if expression == nil {
		return MIRRef{}, nil
	}
	index := MIRIndex(len(builder.arena.Expressions))
	value := *kirExprScalars(expression, builder.checker, builder.functionTargets, builder.paths)
	value.Binding, value.Const, value.Lambda, value.Callee = nil, nil, nil, nil
	row := MIRExpression{Value: value}
	row.Value.GenericArguments = cloneKIRStrings(value.GenericArguments)
	row.Value.Fields = cloneKIRStrings(value.Fields)
	builder.arena.Expressions = append(builder.arena.Expressions, row)

	left, err := builder.expression(expression.Left)
	if err != nil {
		return MIRRef{}, err
	}
	right, err := builder.expression(expression.Right)
	if err != nil {
		return MIRRef{}, err
	}
	operand, err := builder.expression(expression.Operand)
	if err != nil {
		return MIRRef{}, err
	}
	base, err := builder.expression(expression.Base)
	if err != nil {
		return MIRRef{}, err
	}
	receiver, err := builder.expression(expression.Receiver)
	if err != nil {
		return MIRRef{}, err
	}
	builder.arena.Expressions[index].Left = left
	builder.arena.Expressions[index].Right = right
	builder.arena.Expressions[index].Operand = operand
	builder.arena.Expressions[index].Base = base
	builder.arena.Expressions[index].Receiver = receiver
	if expression.Kind == ExCall && expression.Function == nil && expression.Receiver == nil && expression.Callee != nil {
		if _, builtin := builder.checker.Env.Builtins[expression.Name]; !builtin {
			callee, err := builder.expression(expression.Callee)
			if err != nil {
				return MIRRef{}, err
			}
			builder.arena.Expressions[index].Callee = callee
		}
	}
	args, err := builder.expressions(expression.Args)
	if err != nil {
		return MIRRef{}, err
	}
	items, err := builder.expressions(expression.Items)
	if err != nil {
		return MIRRef{}, err
	}
	keys, err := builder.expressions(expression.MapKeys)
	if err != nil {
		return MIRRef{}, err
	}
	values, err := builder.expressions(expression.Values)
	if err != nil {
		return MIRRef{}, err
	}
	builder.arena.Expressions[index].Args = builder.addExpressionRefs(args)
	builder.arena.Expressions[index].Items = builder.addExpressionRefs(items)
	builder.arena.Expressions[index].MapKeys = builder.addExpressionRefs(keys)
	builder.arena.Expressions[index].Values = builder.addExpressionRefs(values)
	if expression.Kind == ExLambda {
		function, err := builder.function(expression.Lambda, expression.Captures, false)
		if err != nil {
			return MIRRef{}, err
		}
		builder.arena.Expressions[index].Lambda = MIRRef{Index: function, Present: true}
	}
	if expression.ConstValue != nil {
		constant, err := builder.value(*expression.ConstValue)
		if err != nil {
			return MIRRef{}, err
		}
		builder.arena.Expressions[index].Const = constant
	}
	if expression.Kind == ExVar && expression.Function == nil && expression.Scope != nil {
		if binding, _, ok := expression.Scope.lookupOwner(expression.Name); ok {
			ref := builder.binding(kirBinding(expression.Name, typeString(binding.Type, nil), binding.Mutable, binding.Token, builder.paths))
			builder.arena.Expressions[index].Binding = ref
		}
	}
	return MIRRef{Index: index, Present: true}, nil
}

func (builder *kirArenaBuilder) expressions(expressions []*Expr) ([]MIRIndex, error) {
	indices := make([]MIRIndex, 0, len(expressions))
	for _, expression := range expressions {
		ref, err := builder.expression(expression)
		if err != nil {
			return nil, err
		}
		if !ref.Present {
			return nil, fmt.Errorf("checked expression list contains a missing node")
		}
		indices = append(indices, ref.Index)
	}
	return indices, nil
}

func (builder *kirArenaBuilder) statements(statements []*Stmt) ([]MIRIndex, error) {
	indices := make([]MIRIndex, 0, len(statements))
	for _, statement := range statements {
		ref, err := builder.statement(statement)
		if err != nil {
			return nil, err
		}
		if !ref.Present {
			return nil, fmt.Errorf("checked statement list contains a missing node")
		}
		indices = append(indices, ref.Index)
	}
	return indices, nil
}

func (builder *kirArenaBuilder) statement(statement *Stmt) (MIRRef, error) {
	if statement == nil {
		return MIRRef{}, nil
	}
	index := MIRIndex(len(builder.arena.Statements))
	value := KIRStmt{Kind: stmtName(statement.Kind), Source: builder.paths.tokenSource(statement.Tok), Line: statement.Tok.Line, Column: statement.Tok.Column, Span: kirSourceSpan(statement.Tok, statement.EndToken), Name: statement.Name, Mutable: statement.Mutable, Const: statement.Const, Annotation: typeSpecString(statement.Annotation)}
	builder.arena.Statements = append(builder.arena.Statements, MIRStatement{Value: value})
	if statement.Kind == StLet || statement.Kind == StConst || statement.Kind == StFor {
		binding := kirBinding(statement.Name, typeString(statement.Type, statement.Annotation), statement.Mutable, statement.NameToken, builder.paths)
		builder.arena.Statements[index].Binding = builder.binding(binding)
	}
	for _, edge := range []struct {
		source *Expr
		target string
	}{
		{statement.Init, "init"},
		{statement.Expr, "expr"},
		{statement.Target, "target"},
		{statement.Value, "value"},
		{statement.Cond, "cond"},
		{statement.Iter, "iter"},
		{statement.Return, "return"},
		{statement.Scrutinee, "scrutinee"},
	} {
		ref, err := builder.expression(edge.source)
		if err != nil {
			return MIRRef{}, err
		}
		row := &builder.arena.Statements[index]
		switch edge.target {
		case "init":
			row.Init = ref
		case "expr":
			row.Expr = ref
		case "target":
			row.Target = ref
		case "value":
			row.ValueExpr = ref
		case "cond":
			row.Cond = ref
		case "iter":
			row.Iter = ref
		case "return":
			row.Return = ref
		case "scrutinee":
			row.Scrutinee = ref
		}
	}
	for _, edge := range []struct {
		source []*Stmt
		target string
	}{
		{statement.Then, "then"},
		{statement.Else, "else"},
		{statement.Body, "body"},
	} {
		refs, err := builder.statements(edge.source)
		if err != nil {
			return MIRRef{}, err
		}
		list := builder.addStatementRefs(refs)
		row := &builder.arena.Statements[index]
		switch edge.target {
		case "then":
			row.Then = list
		case "else":
			row.Else = list
		case "body":
			row.Body = list
		}
	}
	armIndices := make([]MIRIndex, 0, len(statement.Arms))
	for _, arm := range statement.Arms {
		armIndex := MIRIndex(len(builder.arena.Arms))
		builder.arena.Arms = append(builder.arena.Arms, MIRArm{})
		pattern := builder.pattern(arm.Pattern)
		body, err := builder.statements(arm.Body)
		if err != nil {
			return MIRRef{}, err
		}
		endToken := arm.Pattern.EndToken
		if len(arm.Body) != 0 {
			endToken = arm.Body[len(arm.Body)-1].EndToken
		}
		value := KIRArm{Source: builder.paths.tokenSource(arm.Pattern.Tok), Line: arm.Pattern.Tok.Line, Column: arm.Pattern.Tok.Column, Span: kirSourceSpan(arm.Pattern.Tok, endToken)}
		builder.arena.Arms[armIndex] = MIRArm{Value: value, Pattern: pattern, Body: builder.addStatementRefs(body)}
		armIndices = append(armIndices, armIndex)
	}
	builder.arena.Statements[index].Arms = builder.addArmRefs(armIndices)
	return MIRRef{Index: index, Present: true}, nil
}

func (builder *kirArenaBuilder) pattern(pattern Pattern) MIRRef {
	value := KIRPattern{Kind: patternName(pattern.Kind), Source: builder.paths.tokenSource(pattern.Tok), Line: pattern.Tok.Line, Column: pattern.Tok.Column, Span: kirSourceSpan(pattern.Tok, pattern.EndToken), Bool: pattern.Bool, Int: pattern.Int, String: pattern.Str, Type: pattern.TypeName, Variant: pattern.Variant, Binding: pattern.Binding, Present: pattern.Present, OK: pattern.OK}
	ref := MIRRef{}
	if pattern.Binding != "" {
		ref = builder.binding(kirBinding(pattern.Binding, typeString(pattern.BindingType, nil), false, pattern.BindingTok, builder.paths))
	}
	index := MIRIndex(len(builder.arena.Patterns))
	builder.arena.Patterns = append(builder.arena.Patterns, MIRPattern{Value: value, Binding: ref})
	return MIRRef{Index: index, Present: true}
}

func (builder *kirArenaBuilder) function(function *Function, captures []Capture, topLevel bool) (MIRIndex, error) {
	if function == nil {
		return 0, fmt.Errorf("checked function is missing")
	}
	index := MIRIndex(len(builder.arena.Functions))
	value := KIRFunction{Name: function.Name, Source: builder.paths.tokenSource(function.Tok), Line: function.Tok.Line, Column: function.Tok.Column, Span: kirSourceSpan(function.Tok, function.EndToken), Worker: function.Worker, Unsafe: function.Unsafe, Module: builder.paths.name(function.Module), Return: typeSpecString(function.Return), TypeParams: make([]*KIRTypeParam, 0, len(function.TypeParams))}
	if topLevel {
		value.Public, value.Trait, value.Receiver = function.Public, function.Trait, typeSpecString(function.Receiver)
	}
	for _, parameter := range function.TypeParams {
		value.TypeParams = append(value.TypeParams, &KIRTypeParam{Name: parameter.Name, Constraint: parameter.Constraint, Source: builder.paths.tokenSource(parameter.Tok), Line: parameter.Tok.Line, Column: parameter.Tok.Column, Span: kirTokenSpan(parameter.Tok)})
	}
	builder.arena.Functions = append(builder.arena.Functions, MIRFunction{Value: value})
	parameterIndices := make([]MIRIndex, 0, len(function.Params))
	for _, parameter := range function.Params {
		parameterIndex := MIRIndex(len(builder.arena.Parameters))
		defaultRef, err := builder.expression(parameter.Default)
		if err != nil {
			return 0, err
		}
		param := KIRParam{Name: parameter.Name, Type: typeSpecString(parameter.Type), Source: builder.paths.tokenSource(parameter.Tok), Line: parameter.Tok.Line, Column: parameter.Tok.Column, Span: kirSourceSpan(parameter.Tok, parameter.EndToken)}
		if topLevel {
			param.Binding = nil
		}
		binding := builder.binding(kirBinding(parameter.Name, typeSpecString(parameter.Type), false, parameter.Tok, builder.paths))
		builder.arena.Parameters = append(builder.arena.Parameters, MIRParameter{Value: param, Default: defaultRef, Binding: binding})
		parameterIndices = append(parameterIndices, parameterIndex)
	}
	body, err := builder.statements(function.Body)
	if err != nil {
		return 0, err
	}
	ordered := append([]Capture(nil), captures...)
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i].Token, ordered[j].Token
		aSource, bSource := builder.paths.tokenSource(a), builder.paths.tokenSource(b)
		if aSource != bSource {
			return aSource < bSource
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		return ordered[i].Name < ordered[j].Name
	})
	captureIndices := make([]MIRIndex, 0, len(ordered))
	for _, capture := range ordered {
		captureIndex := MIRIndex(len(builder.arena.Captures))
		binding := builder.binding(kirBinding(capture.Name, capture.Type.String(), capture.Mutable, capture.Token, builder.paths))
		builder.arena.Captures = append(builder.arena.Captures, MIRCapture{Binding: binding})
		captureIndices = append(captureIndices, captureIndex)
	}
	functionRow := &builder.arena.Functions[index]
	functionRow.Body = builder.addStatementRefs(body)
	functionRow.Params = builder.addParameterRefs(parameterIndices)
	functionRow.Captures = builder.addCaptureRefs(captureIndices)
	return index, nil
}

func (builder *kirArenaBuilder) value(value Value) (MIRRef, error) {
	index := MIRIndex(len(builder.arena.Values))
	items := arrayValues(value)
	row := MIRValue{Value: KIRValue{Kind: valueName(value.Kind), Int: value.I, UInt: value.U, UIntBits: value.UBits, Float: value.F, Bool: value.Bool, String: value.S, Bytes: append([]byte(nil), value.Bytes...), Present: value.Present, OK: value.OK}}
	builder.arena.Values = append(builder.arena.Values, row)
	inner := MIRRef{}
	if value.Inner != nil {
		ref, err := builder.value(*value.Inner)
		if err != nil {
			return MIRRef{}, err
		}
		inner = ref
	}
	array := make([]MIRIndex, 0, len(items))
	for _, item := range items {
		ref, err := builder.value(item)
		if err != nil {
			return MIRRef{}, err
		}
		array = append(array, ref.Index)
	}
	builder.arena.Values[index].Inner = inner
	builder.arena.Values[index].Array = builder.addValueRefs(array)
	return MIRRef{Index: index, Present: true}, nil
}
