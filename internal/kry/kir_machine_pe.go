package kry

import (
	"encoding/binary"
	"fmt"
	"strings"
)

type kirPEBindingID struct {
	name   string
	source string
	line   int
	column int
}

type kirPEFunction struct {
	function kirPEFunctionView
	index    MIRIndex
	target   string
	label    int
	slots    map[kirPEBindingID]machineSlot
	nextSlot int32
}

type kirPEValidatedProgram struct {
	entry     MIRNodeRefList
	main      MIRRef
	functions []*kirPEFunction
	byTarget  map[string]*kirPEFunction
}

// PE lowering views copy only one validated arena row. Recursive edges stay
// explicit MIR references and collections are consumed through arena ranges.
type kirPEExpression struct {
	KIRExpr
	Left, Right, Operand, Base, Receiver, Callee MIRRef
	Lambda, Binding, Const                       MIRRef
	Args, Items, MapKeys, Values                 []MIRRef
}

type kirPEStatement struct {
	KIRStmt
	Binding, Init, Expr, Target, Value MIRRef
	Cond, Iter, Return, Scrutinee      MIRRef
	Then, Else, Body                   MIRNodeRefList
	Arms                               MIRNodeRefList
}

type kirPEFunctionView struct {
	KIRFunction
	Body     MIRNodeRefList
	Params   []MIRIndex
	Captures MIRNodeRefList
}

type kirPEParameter struct {
	KIRParam
	Default, Binding MIRRef
}

// lowerDirectPEKIR accepts only validated MIR. Both static and dynamic PE
// lowering read the arena's typed rows and checked references.
func lowerDirectPEKIR(mir *ValidatedMIR, limits Limits) ([]byte, error) {
	if mir == nil || mir.arena == nil {
		return nil, fmt.Errorf("direct PE backend requires validated MIR")
	}
	arena := mir.arena
	if err := arena.validateReferences(); err != nil {
		return nil, fmt.Errorf("invalid validated MIR arena: %w", err)
	}
	target := NativeTarget{OS: arena.Target.OS, Arch: arena.Target.Arch, GUI: arena.Target.GUI}
	if err := validateNativeOutputTarget("pe-direct", target); err != nil {
		return nil, err
	}
	if err := validateMIRFunctionValueSupport(mir, "pe-direct"); err != nil {
		return nil, err
	}
	if err := validateMIRNativeFeatureSupport(mir, "pe-direct", target); err != nil {
		return nil, err
	}
	output, staticErr := directStaticOutputMIR(arena, limits.MaxOutputBytes)
	if staticErr == nil {
		image, err := buildDirectStaticPE(output, arena.Target.GUI, limits)
		if err != nil {
			return nil, err
		}
		if limit := limits.MaxArtifactBytes; limit > 0 && len(image) > limit {
			return nil, fmt.Errorf("direct PE exceeds configured artifact limit")
		}
		return image, nil
	}
	validated, err := validateKIRDirectPE(arena)
	if err != nil {
		return nil, fmt.Errorf("direct PE dynamic subset: %w (static output path: %v)", err, staticErr)
	}
	machine := newDirectMachine()
	machine.windowsABI = true
	machine.outputLimit = limits.MaxOutputBytes
	machine.outputLimitSet = true
	for _, function := range validated.functions {
		function.label = machine.newLabel()
		machine.functionLabels[function.target] = function.label
	}
	entrySlots, entryNext, err := collectKIRPEBindings(arena, validated.entry, nil, "<entry>")
	if err != nil {
		return nil, fmt.Errorf("direct PE entry setup: %w", err)
	}
	entry := &kirPEFunction{target: "<entry>", slots: entrySlots, nextSlot: entryNext}
	if err := collectKIRPEFunctions(arena, validated.functions); err != nil {
		return nil, fmt.Errorf("direct PE function setup: %w", err)
	}
	lowerer := &kirPEMachine{machine: machine, arena: arena, program: validated, entry: entry, maxWallTimeMS: limits.MaxWallTimeMS}
	machine.outputLimitLabel = lowerer.runtimeFailureLabel("output limit exceeded")
	for _, function := range validated.functions {
		if err := lowerer.collectFunctionParameters(function); err != nil {
			return nil, fmt.Errorf("direct PE function setup: %w", err)
		}
	}
	if err := lowerer.emitEntry(); err != nil {
		return nil, err
	}
	if err := lowerer.emitFunctions(); err != nil {
		return nil, err
	}
	if err := machine.emitJump(machine.endLabel); err != nil {
		return nil, err
	}
	if err := machine.bind(machine.endLabel); err != nil {
		return nil, err
	}
	if err := machine.emitExit(0); err != nil {
		return nil, err
	}
	if err := machine.bind(machine.trapLabel); err != nil {
		return nil, err
	}
	if err := machine.emitExit(1); err != nil {
		return nil, err
	}
	for _, failure := range lowerer.failures {
		if err := machine.bind(failure.label); err != nil {
			return nil, err
		}
		if err := machine.emitPEWriteStderr("kryndel: " + failure.message + "\n"); err != nil {
			return nil, err
		}
	}
	if err := machine.validateLabelReferences(); err != nil {
		return nil, err
	}
	image, err := buildDirectDynamicPE(machine.code, machine.data, machine.dataRefs, machine.peImportRefs, machine.peFunctions)
	if err != nil {
		return nil, err
	}
	if arena.Target.GUI {
		pePut16(image, peSubsystemOffset, peSubsystemGUI)
	}
	return image, nil
}

type kirPEMachine struct {
	machine       *directMachine
	arena         *KIRArena
	program       *kirPEValidatedProgram
	entry         *kirPEFunction
	current       *kirPEFunction
	scopes        []map[kirPEBindingID]machineSlot
	loops         []machineLoop
	inFunction    bool
	maxWallTimeMS int64
	failures      []kirPERuntimeFailure
}

type kirPERuntimeFailure struct {
	label   int
	message string
}

func emitPEWallClockStart(machine *directMachine, maxWallTimeMS int64) {
	if maxWallTimeMS > 0 {
		machine.emitPEImportedCall(peImportGetTickCount64)
		machine.code = append(machine.code, 0x49, 0x89, 0xc6)
	}
}

func emitPEWallClockCheck(machine *directMachine, maxWallTimeMS int64, failureLabel int) error {
	if maxWallTimeMS == 0 {
		return nil
	}
	if maxWallTimeMS < 0 {
		return machine.emitJump(failureLabel)
	}
	machine.emitPEImportedCall(peImportGetTickCount64)
	machine.code = append(machine.code, 0x4c, 0x29, 0xf0, 0x48, 0xb9)
	var limit [8]byte
	binary.LittleEndian.PutUint64(limit[:], uint64(maxWallTimeMS))
	machine.code = append(machine.code, limit[:]...)
	machine.code = append(machine.code, 0x48, 0x39, 0xc8)
	return machine.emitConditionalJump(0x83, failureLabel)
}

func (lowerer *kirPEMachine) emitWallClockCheck() error {
	if lowerer.maxWallTimeMS == 0 {
		return nil
	}
	return emitPEWallClockCheck(lowerer.machine, lowerer.maxWallTimeMS, lowerer.runtimeFailureLabel("wall-clock execution limit exceeded"))
}

func (lowerer *kirPEMachine) runtimeFailureLabel(message string) int {
	for _, failure := range lowerer.failures {
		if failure.message == message {
			return failure.label
		}
	}
	label := lowerer.machine.newLabel()
	lowerer.failures = append(lowerer.failures, kirPERuntimeFailure{label: label, message: message})
	return label
}

func kirPERefs(arena *KIRArena, list MIRNodeRefList) ([]MIRRef, error) {
	indexes, err := arena.indexList(arena.ExpressionRefs, list)
	if err != nil {
		return nil, err
	}
	refs := make([]MIRRef, len(indexes))
	for index, node := range indexes {
		refs[index] = MIRRef{Index: node, Present: true}
	}
	return refs, nil
}

func kirPEExpressionAt(arena *KIRArena, ref MIRRef) (*kirPEExpression, error) {
	if !ref.Present {
		return nil, nil
	}
	if uint64(ref.Index) >= uint64(len(arena.Expressions)) {
		return nil, fmt.Errorf("expression reference %d is out of bounds", ref.Index)
	}
	row := arena.Expressions[ref.Index]
	args, err := kirPERefs(arena, row.Args)
	if err != nil {
		return nil, err
	}
	items, err := kirPERefs(arena, row.Items)
	if err != nil {
		return nil, err
	}
	mapKeys, err := kirPERefs(arena, row.MapKeys)
	if err != nil {
		return nil, err
	}
	values, err := kirPERefs(arena, row.Values)
	if err != nil {
		return nil, err
	}
	return &kirPEExpression{
		KIRExpr: row.Value, Left: row.Left, Right: row.Right, Operand: row.Operand,
		Base: row.Base, Receiver: row.Receiver, Callee: row.Callee, Lambda: row.Lambda,
		Binding: row.Binding, Const: row.Const, Args: args, Items: items, MapKeys: mapKeys, Values: values,
	}, nil
}

func kirPEStatementAt(arena *KIRArena, index MIRIndex) (*kirPEStatement, error) {
	if uint64(index) >= uint64(len(arena.Statements)) {
		return nil, fmt.Errorf("statement reference %d is out of bounds", index)
	}
	row := arena.Statements[index]
	return &kirPEStatement{
		KIRStmt: row.Value, Binding: row.Binding, Init: row.Init, Expr: row.Expr,
		Target: row.Target, Value: row.ValueExpr, Cond: row.Cond, Iter: row.Iter,
		Return: row.Return, Scrutinee: row.Scrutinee, Then: row.Then, Else: row.Else,
		Body: row.Body, Arms: row.Arms,
	}, nil
}

func kirPEFunctionAt(arena *KIRArena, index MIRIndex) (*kirPEFunctionView, error) {
	if uint64(index) >= uint64(len(arena.Functions)) {
		return nil, fmt.Errorf("function reference %d is out of bounds", index)
	}
	row := arena.Functions[index]
	parameters, err := arena.indexList(arena.ParameterRefs, row.Params)
	if err != nil {
		return nil, err
	}
	return &kirPEFunctionView{KIRFunction: row.Value, Body: row.Body, Params: parameters, Captures: row.Captures}, nil
}

func kirPEParameterAt(arena *KIRArena, index MIRIndex) (*kirPEParameter, error) {
	if uint64(index) >= uint64(len(arena.Parameters)) {
		return nil, fmt.Errorf("parameter reference %d is out of bounds", index)
	}
	row := arena.Parameters[index]
	return &kirPEParameter{KIRParam: row.Value, Default: row.Default, Binding: row.Binding}, nil
}

func kirPEBindingAt(arena *KIRArena, ref MIRRef) (*KIRBinding, error) {
	if !ref.Present {
		return nil, nil
	}
	if uint64(ref.Index) >= uint64(len(arena.Bindings)) {
		return nil, fmt.Errorf("binding reference %d is out of bounds", ref.Index)
	}
	return &arena.Bindings[ref.Index], nil
}

func validateKIRDirectPE(arena *KIRArena) (*kirPEValidatedProgram, error) {
	if arena == nil {
		return nil, fmt.Errorf("missing validated KIR arena")
	}
	if arena.Format != KIRFormat || arena.Version <= 0 || arena.Version > KIRVersion {
		return nil, fmt.Errorf("unsupported validated KIR header")
	}
	if arena.Target.OS != "windows" || arena.Target.Arch != "amd64" {
		return nil, fmt.Errorf("direct PE backend requires windows-amd64")
	}
	if len(arena.Imports) != 0 || len(arena.Structs) != 0 || len(arena.Enums) != 0 {
		return nil, fmt.Errorf("module imports, structs, and enums are not supported")
	}
	functionIndexes, err := arena.indexList(arena.FunctionRefs, arena.TopFunctions)
	if err != nil {
		return nil, err
	}
	statementIndexes, err := arena.indexList(arena.StatementRefs, arena.TopStatements)
	if err != nil {
		return nil, err
	}
	program := &kirPEValidatedProgram{entry: arena.TopStatements, byTarget: map[string]*kirPEFunction{}}
	if len(statementIndexes) != 0 {
		for _, index := range functionIndexes {
			function, err := kirPEFunctionAt(arena, index)
			if err != nil {
				return nil, err
			}
			if function.Name == "main" {
				return nil, fmt.Errorf("direct PE backend does not support both top-level statements and main")
			}
		}
	} else {
		for _, index := range functionIndexes {
			function, err := kirPEFunctionAt(arena, index)
			if err != nil {
				return nil, err
			}
			if function.Name != "main" {
				continue
			}
			if program.main.Present {
				return nil, fmt.Errorf("direct PE backend has duplicate main functions")
			}
			program.main = MIRRef{Index: index, Present: true}
		}
		if !program.main.Present {
			return nil, fmt.Errorf("program requires top-level statements or main() -> Nil")
		}
		main, err := kirPEFunctionAt(arena, program.main.Index)
		if err != nil {
			return nil, err
		}
		if len(main.Params) != 0 || main.Return != "Nil" {
			return nil, fmt.Errorf("main must have signature main() -> Nil")
		}
		program.entry = main.Body
	}

	nameCounts := map[string]int{}
	for _, index := range functionIndexes {
		function, err := kirPEFunctionAt(arena, index)
		if err != nil {
			return nil, err
		}
		nameCounts[function.Name]++
	}
	for _, index := range functionIndexes {
		function, err := kirPEFunctionAt(arena, index)
		if err != nil {
			return nil, err
		}
		if function.Receiver != "" || function.Worker || function.Unsafe || len(function.TypeParams) != 0 {
			return nil, fmt.Errorf("function '%s' has unsupported metadata (module=%q receiver=%t worker=%t unsafe=%t type-parameters=%d)", function.Name, function.Module, function.Receiver != "", function.Worker, function.Unsafe, len(function.TypeParams))
		}
		if function.Name == "main" {
			continue
		}
		if len(function.Params) > directPEWindowsMaxArgs {
			return nil, fmt.Errorf("function '%s' has %d parameters; Win64 direct PE currently supports at most %d", function.Name, len(function.Params), directPEWindowsMaxArgs)
		}
		if !directPETypeName(function.Return, true) {
			return nil, fmt.Errorf("function '%s' has unsupported return type %s", function.Name, function.Return)
		}
		for _, parameterIndex := range function.Params {
			parameter, err := kirPEParameterAt(arena, parameterIndex)
			if err != nil {
				return nil, err
			}
			binding, err := kirPEBindingAt(arena, parameter.Binding)
			if err != nil {
				return nil, err
			}
			if binding == nil {
				return nil, fmt.Errorf("function '%s' has incomplete parameter binding metadata", function.Name)
			}
			if !directPETypeName(parameter.Type, false) || binding.Type != parameter.Type {
				return nil, fmt.Errorf("function '%s' parameter '%s' has unsupported type %s", function.Name, parameter.Name, parameter.Type)
			}
		}
	}
	for _, index := range functionIndexes {
		function, err := kirPEFunctionAt(arena, index)
		if err != nil {
			return nil, err
		}
		if function.Name == "main" {
			continue
		}
		target := function.Name
		if nameCounts[function.Name] > 1 {
			params := make([]string, 0, len(function.Params))
			for _, parameterIndex := range function.Params {
				parameter, err := kirPEParameterAt(arena, parameterIndex)
				if err != nil {
					return nil, err
				}
				params = append(params, parameter.Type)
			}
			target = kirFunctionIdentity(function.Name, function.Module, kirFunctionReceiverIdentity(function.Trait, function.Receiver), params)
		}
		if _, duplicate := program.byTarget[target]; duplicate {
			return nil, fmt.Errorf("function '%s' has duplicate KIR call target %q", function.Name, target)
		}
		entry := &kirPEFunction{function: *function, target: target, index: index}
		program.byTarget[target] = entry
		program.functions = append(program.functions, entry)
	}
	if err := validateKIRDirectPEStatements(arena, program.entry, false, false, program.byTarget); err != nil {
		return nil, err
	}
	for _, function := range program.functions {
		if err := validateKIRDirectPEStatements(arena, function.function.Body, true, false, program.byTarget); err != nil {
			return nil, fmt.Errorf("function '%s': %w", function.function.Name, err)
		}
	}
	return program, nil
}

func validateKIRDirectPEStatements(arena *KIRArena, statements MIRNodeRefList, inFunction, inLoop bool, functions map[string]*kirPEFunction) error {
	indexes, err := arena.indexList(arena.StatementRefs, statements)
	if err != nil {
		return err
	}
	for _, index := range indexes {
		statement, err := kirPEStatementAt(arena, index)
		if err != nil {
			return err
		}
		binding, err := kirPEBindingAt(arena, statement.Binding)
		if err != nil {
			return err
		}
		switch statement.Kind {
		case "let", "const":
			init, err := kirPEExpressionAt(arena, statement.Init)
			if err != nil {
				return err
			}
			if init == nil || binding == nil || binding.Name != statement.Name || binding.Mutable != statement.Mutable || binding.Type == "" || !directPETypeName(binding.Type, false) || init.Type != binding.Type {
				return fmt.Errorf("binding '%s' has an unsupported or incomplete type", statement.Name)
			}
			if err := validateKIRDirectPEExpr(arena, statement.Init, false, functions); err != nil {
				return err
			}
		case "assign":
			target, err := kirPEExpressionAt(arena, statement.Target)
			if err != nil {
				return err
			}
			value, err := kirPEExpressionAt(arena, statement.Value)
			if err != nil {
				return err
			}
			if target == nil || value == nil {
				return fmt.Errorf("assignment requires a scalar or String binding")
			}
			targetBinding, err := kirPEBindingAt(arena, target.Binding)
			if err != nil {
				return err
			}
			if target.Kind != "var" || targetBinding == nil || value.Type == "" || !directPETypeName(value.Type, false) || targetBinding.Type != value.Type {
				return fmt.Errorf("assignment requires a scalar or String binding")
			}
			if err := validateKIRDirectPEExpr(arena, statement.Value, false, functions); err != nil {
				return err
			}
		case "expr":
			expression, err := kirPEExpressionAt(arena, statement.Expr)
			if err != nil {
				return err
			}
			if expression == nil || expression.Kind != "call" || expression.Receiver.Present {
				return fmt.Errorf("expression statements must be direct calls")
			}
			allowOutput := expression.CallTarget == "builtin:print" || expression.CallTarget == "builtin:println"
			if err := validateKIRDirectPEExpr(arena, statement.Expr, allowOutput, functions); err != nil {
				return err
			}
		case "if":
			condition, err := kirPEExpressionAt(arena, statement.Cond)
			if err != nil {
				return err
			}
			if condition == nil || condition.Type != "Bool" {
				return fmt.Errorf("if condition must be Bool")
			}
			if err := validateKIRDirectPEExpr(arena, statement.Cond, false, functions); err != nil {
				return err
			}
			if err := validateKIRDirectPEStatements(arena, statement.Then, inFunction, inLoop, functions); err != nil {
				return err
			}
			if err := validateKIRDirectPEStatements(arena, statement.Else, inFunction, inLoop, functions); err != nil {
				return err
			}
		case "while":
			condition, err := kirPEExpressionAt(arena, statement.Cond)
			if err != nil {
				return err
			}
			if condition == nil || condition.Type != "Bool" {
				return fmt.Errorf("while condition must be Bool")
			}
			if err := validateKIRDirectPEExpr(arena, statement.Cond, false, functions); err != nil {
				return err
			}
			if err := validateKIRDirectPEStatements(arena, statement.Body, inFunction, true, functions); err != nil {
				return err
			}
		case "break", "continue":
			if !inLoop {
				return fmt.Errorf("%s is outside a loop", statement.Kind)
			}
		case "return":
			result, err := kirPEExpressionAt(arena, statement.Return)
			if err != nil {
				return err
			}
			if result != nil && result.Kind != "nil" {
				if err := validateKIRDirectPEExpr(arena, statement.Return, false, functions); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("statement kind %s is not supported", statement.Kind)
		}
	}
	return nil
}

func validateKIRDirectPEExpr(arena *KIRArena, ref MIRRef, allowOutput bool, functions map[string]*kirPEFunction) error {
	expression, err := kirPEExpressionAt(arena, ref)
	if err != nil {
		return err
	}
	if expression == nil {
		return fmt.Errorf("missing expression")
	}
	if expression.Type == "" {
		return fmt.Errorf("expression is missing checked type metadata")
	}
	if expression.Type != "Nil" && !directPETypeName(expression.Type, false) {
		return fmt.Errorf("expression type %s is not supported", expression.Type)
	}
	switch expression.Kind {
	case "int", "bool", "string":
		return nil
	case "var":
		binding, err := kirPEBindingAt(arena, expression.Binding)
		if err != nil {
			return err
		}
		if binding == nil || binding.Name != expression.Name || binding.Type != expression.Type {
			return fmt.Errorf("variable '%s' is missing resolved binding metadata", expression.Name)
		}
		return nil
	case "nil":
		return fmt.Errorf("Nil is supported only as a return value")
	case "unary":
		switch expression.Operator {
		case "+", "-", "!", "~":
		default:
			return fmt.Errorf("unary operator %s is not supported", expression.Operator)
		}
		return validateKIRDirectPEExpr(arena, expression.Operand, false, functions)
	case "binary":
		if expression.Type == "String" || expression.Type == "Nil" {
			return fmt.Errorf("String concatenation and non-scalar binary operations are not supported")
		}
		if expression.Left.Present && arena.Expressions[expression.Left.Index].Value.Type == "String" {
			return fmt.Errorf("String comparison is not supported by the direct PE runtime")
		}
		switch expression.Operator {
		case "+", "-", "*", "/", "%", "&", "^", "|", "<<", ">>", "&&", "||", "==", "!=", "<", "<=", ">", ">=":
		default:
			return fmt.Errorf("binary operator %s is not supported", expression.Operator)
		}
		if err := validateKIRDirectPEExpr(arena, expression.Left, false, functions); err != nil {
			return err
		}
		return validateKIRDirectPEExpr(arena, expression.Right, false, functions)
	case "call":
		if expression.Receiver.Present || expression.Callee.Present || expression.CallTarget == "" {
			return fmt.Errorf("receiver calls and function values are not supported")
		}
		if strings.HasPrefix(expression.CallTarget, "function:") {
			target := strings.TrimPrefix(expression.CallTarget, "function:")
			entry, ok := functions[target]
			if !ok {
				return fmt.Errorf("function call %q has no supported KIR target", target)
			}
			if len(expression.Args) != len(entry.function.Params) || len(expression.Args) > directPEWindowsMaxArgs {
				return fmt.Errorf("function '%s' must be called with all arguments and at most %d parameters", entry.function.Name, directPEWindowsMaxArgs)
			}
			for index, argument := range expression.Args {
				parameter, err := kirPEParameterAt(arena, entry.function.Params[index])
				if err != nil {
					return err
				}
				if !argument.Present || arena.Expressions[argument.Index].Value.Type != parameter.Type {
					return fmt.Errorf("function '%s' argument %d is missing matching checked type metadata", entry.function.Name, index+1)
				}
				if err := validateKIRDirectPEExpr(arena, argument, false, functions); err != nil {
					return err
				}
			}
			return nil
		}
		switch expression.CallTarget {
		case "builtin:u8", "builtin:u16", "builtin:u32", "builtin:u64":
			bits := uint8(0)
			switch expression.CallTarget {
			case "builtin:u8":
				bits = 8
			case "builtin:u16":
				bits = 16
			case "builtin:u32":
				bits = 32
			case "builtin:u64":
				bits = 64
			}
			if len(expression.Args) != 1 || expression.Type != fmt.Sprintf("UInt%d", bits) {
				return fmt.Errorf("conversion %s must have one argument and return UInt%d", expression.Name, bits)
			}
			argument := expression.Args[0]
			if !argument.Present || (arena.Expressions[argument.Index].Value.Type != "Int" && !strings.HasPrefix(arena.Expressions[argument.Index].Value.Type, "UInt")) {
				return fmt.Errorf("conversion %s requires an Int or UInt argument", expression.Name)
			}
			return validateKIRDirectPEExpr(arena, argument, false, functions)
		case "builtin:str":
			if len(expression.Args) != 1 || expression.Type != "String" {
				return fmt.Errorf("conversion str must have one argument and return String")
			}
			argument := expression.Args[0]
			if !argument.Present {
				return fmt.Errorf("direct PE str supports Int, UInt, Bool, and String values")
			}
			argumentType := arena.Expressions[argument.Index].Value.Type
			if argumentType != "Int" && argumentType != "Bool" && argumentType != "String" && !strings.HasPrefix(argumentType, "UInt") {
				return fmt.Errorf("direct PE str supports Int, UInt, Bool, and String values")
			}
			return validateKIRDirectPEExpr(arena, argument, false, functions)
		case "builtin:print", "builtin:println":
			if !allowOutput || len(expression.Args) != 1 {
				return fmt.Errorf("only statement-form print(value) and println(value) are supported")
			}
			return validateKIRDirectPEExpr(arena, expression.Args[0], false, functions)
		default:
			return fmt.Errorf("builtin %q is not supported by the direct PE backend", expression.Name)
		}
	default:
		return fmt.Errorf("expression kind %s is not supported", expression.Kind)
	}
}

func collectKIRPEBindings(arena *KIRArena, statements MIRNodeRefList, params []MIRIndex, function string) (map[kirPEBindingID]machineSlot, int32, error) {
	slots := make(map[kirPEBindingID]machineSlot)
	var next int32
	for index, parameterIndex := range params {
		parameter, err := kirPEParameterAt(arena, parameterIndex)
		if err != nil {
			return nil, 0, err
		}
		binding, err := kirPEBindingAt(arena, parameter.Binding)
		if err != nil {
			return nil, 0, err
		}
		if binding == nil {
			return nil, 0, fmt.Errorf("parameter binding metadata is missing")
		}
		next += 8
		if _, duplicate := slots[kirPEBindingKey(binding)]; duplicate {
			return nil, 0, fmt.Errorf("function '%s' has duplicate parameter binding metadata", function)
		}
		slots[kirPEBindingKey(binding)] = machineSlot{offset: int32((index + 1) * 8), typ: kirPEType(parameter.Type)}
	}
	if err := collectKIRPEBlock(arena, statements, []map[kirPEBindingID]bool{{}}, &next, slots); err != nil {
		return nil, 0, err
	}
	return slots, next, nil
}

func collectKIRPEBlock(arena *KIRArena, statements MIRNodeRefList, scopes []map[kirPEBindingID]bool, next *int32, slots map[kirPEBindingID]machineSlot) error {
	indexes, err := arena.indexList(arena.StatementRefs, statements)
	if err != nil {
		return err
	}
	for _, index := range indexes {
		statement, err := kirPEStatementAt(arena, index)
		if err != nil {
			return err
		}
		binding, err := kirPEBindingAt(arena, statement.Binding)
		if err != nil {
			return err
		}
		switch statement.Kind {
		case "let", "const":
			key := kirPEBindingKey(binding)
			current := scopes[len(scopes)-1]
			if _, duplicate := current[key]; duplicate {
				return fmt.Errorf("duplicate binding metadata for '%s'", statement.Name)
			}
			current[key] = true
			*next += 8
			slot := machineSlot{offset: *next, typ: kirPEType(binding.Type)}
			slots[key] = slot
		case "if":
			if err := collectKIRPEBlock(arena, statement.Then, append(scopes, map[kirPEBindingID]bool{}), next, slots); err != nil {
				return err
			}
			if err := collectKIRPEBlock(arena, statement.Else, append(scopes, map[kirPEBindingID]bool{}), next, slots); err != nil {
				return err
			}
		case "while":
			if err := collectKIRPEBlock(arena, statement.Body, append(scopes, map[kirPEBindingID]bool{}), next, slots); err != nil {
				return err
			}
		}
	}
	return nil
}

func collectKIRPEFunctions(arena *KIRArena, functions []*kirPEFunction) error {
	for _, entry := range functions {
		if entry == nil {
			return fmt.Errorf("function metadata is missing")
		}
		slots, next, err := collectKIRPEBindings(arena, entry.function.Body, entry.function.Params, entry.function.Name)
		if err != nil {
			return fmt.Errorf("function '%s': %w", entry.function.Name, err)
		}
		entry.slots, entry.nextSlot = slots, next
	}
	return nil
}

func kirPEBindingKey(binding *KIRBinding) kirPEBindingID {
	if binding == nil {
		return kirPEBindingID{}
	}
	return kirPEBindingID{name: binding.Name, source: binding.Source, line: binding.Line, column: binding.Column}
}

func kirPEType(name string) *Type {
	if typ, ok := machineScalarType(name); ok {
		return typ
	}
	return nil
}

func (lowerer *kirPEMachine) collectFunctionParameters(function *kirPEFunction) error {
	for _, parameterIndex := range function.function.Params {
		parameter, err := kirPEParameterAt(lowerer.arena, parameterIndex)
		if err != nil {
			return err
		}
		binding, err := kirPEBindingAt(lowerer.arena, parameter.Binding)
		if err != nil {
			return err
		}
		if binding == nil || function.slots[kirPEBindingKey(binding)].typ == nil {
			return fmt.Errorf("function '%s' has unsupported parameter binding metadata", function.function.Name)
		}
	}
	return nil
}

func (lowerer *kirPEMachine) emitEntry() error {
	machine := lowerer.machine
	machine.code = append(machine.code, 0x55, 0x48, 0x89, 0xe5)
	frame := (int(lowerer.entry.nextSlot) + 64 + 15) &^ 15
	if frame > 0 {
		machine.code = append(machine.code, 0x48, 0x81, 0xec)
		var size [4]byte
		binary.LittleEndian.PutUint32(size[:], uint32(frame))
		machine.code = append(machine.code, size[:]...)
	}
	machine.bufferOffset = lowerer.entry.nextSlot + 1
	machine.emitOutputCounterInit(lowerer.entry.nextSlot + 32)
	lowerer.current = lowerer.entry
	lowerer.scopes = []map[kirPEBindingID]machineSlot{{}}
	lowerer.loops = nil
	emitPEWallClockStart(machine, lowerer.maxWallTimeMS)
	if err := lowerer.emitWallClockCheck(); err != nil {
		return err
	}
	if err := lowerer.emitStatements(lowerer.program.entry, !lowerer.program.main.Present); err != nil {
		return err
	}
	if lowerer.program.main.Present {
		if err := lowerer.emitWallClockCheck(); err != nil {
			return err
		}
	}
	if err := machine.emitJump(machine.endLabel); err != nil {
		return err
	}
	machine.peFunctions = append(machine.peFunctions, peFunctionRange{begin: 0, end: len(machine.code), frame: uint32(frame)})
	return nil
}

func (lowerer *kirPEMachine) emitFunctions() error {
	machine := lowerer.machine
	for _, function := range lowerer.program.functions {
		if err := machine.bind(function.label); err != nil {
			return err
		}
		begin := len(machine.code)
		functionFrame := (int(function.nextSlot+1) + 63 + 15) &^ 15
		machine.code = append(machine.code, 0x55, 0x48, 0x89, 0xe5, 0x48, 0x81, 0xec)
		var size [4]byte
		binary.LittleEndian.PutUint32(size[:], uint32(functionFrame))
		machine.code = append(machine.code, size[:]...)
		lowerer.current = function
		lowerer.inFunction = true
		lowerer.loops = nil
		lowerer.scopes = []map[kirPEBindingID]machineSlot{{}}
		machine.bufferOffset = function.nextSlot + 1
		for index, parameterIndex := range function.function.Params {
			parameter, err := kirPEParameterAt(lowerer.arena, parameterIndex)
			if err != nil {
				return err
			}
			binding, err := kirPEBindingAt(lowerer.arena, parameter.Binding)
			if err != nil {
				return err
			}
			key := kirPEBindingKey(binding)
			slot, ok := function.slots[key]
			if !ok {
				return fmt.Errorf("function '%s' has no slot for parameter '%s'", function.function.Name, parameter.Name)
			}
			lowerer.bindSlot(key, slot)
			if err := machine.emitStoreArg(index, slot); err != nil {
				return fmt.Errorf("function '%s': %w", function.function.Name, err)
			}
		}
		if err := lowerer.emitStatements(function.function.Body, false); err != nil {
			return fmt.Errorf("function '%s': %w", function.function.Name, err)
		}
		machine.emitFunctionEpilog()
		machine.peFunctions = append(machine.peFunctions, peFunctionRange{begin: begin, end: len(machine.code), frame: uint32(functionFrame)})
		lowerer.inFunction = false
	}
	return nil
}

func (lowerer *kirPEMachine) bindSlot(key kirPEBindingID, slot machineSlot) {
	if len(lowerer.scopes) == 0 {
		lowerer.scopes = append(lowerer.scopes, map[kirPEBindingID]machineSlot{})
	}
	lowerer.scopes[len(lowerer.scopes)-1][key] = slot
}

func (lowerer *kirPEMachine) lookupSlot(binding MIRRef) (machineSlot, bool) {
	metadata, err := kirPEBindingAt(lowerer.arena, binding)
	if err != nil || metadata == nil {
		return machineSlot{}, false
	}
	key := kirPEBindingKey(metadata)
	for scope := len(lowerer.scopes) - 1; scope >= 0; scope-- {
		if slot, ok := lowerer.scopes[scope][key]; ok {
			return slot, true
		}
	}
	return machineSlot{}, false
}

func (lowerer *kirPEMachine) emitScopedStatements(statements MIRNodeRefList) error {
	lowerer.scopes = append(lowerer.scopes, map[kirPEBindingID]machineSlot{})
	err := lowerer.emitStatements(statements, false)
	lowerer.scopes = lowerer.scopes[:len(lowerer.scopes)-1]
	return err
}

func (lowerer *kirPEMachine) emitStatements(statements MIRNodeRefList, topLevel bool) error {
	indexes, err := lowerer.arena.indexList(lowerer.arena.StatementRefs, statements)
	if err != nil {
		return err
	}
	for _, index := range indexes {
		statement, err := kirPEStatementAt(lowerer.arena, index)
		if err != nil {
			return err
		}
		if err := lowerer.emitWallClockCheck(); err != nil {
			return err
		}
		switch statement.Kind {
		case "let", "const":
			if err := lowerer.emitExpr(statement.Init); err != nil {
				return err
			}
			binding, err := kirPEBindingAt(lowerer.arena, statement.Binding)
			if err != nil {
				return err
			}
			slot, ok := lowerer.current.slots[kirPEBindingKey(binding)]
			if !ok {
				return fmt.Errorf("direct PE has no slot for binding '%s'", statement.Name)
			}
			lowerer.machine.emitStoreSlot(slot)
			lowerer.bindSlot(kirPEBindingKey(binding), slot)
		case "assign":
			if err := lowerer.emitExpr(statement.Value); err != nil {
				return err
			}
			target, err := kirPEExpressionAt(lowerer.arena, statement.Target)
			if err != nil {
				return err
			}
			slot, ok := lowerer.lookupSlot(target.Binding)
			if !ok {
				return fmt.Errorf("direct PE has no storage for binding '%s'", target.Name)
			}
			lowerer.machine.emitStoreSlot(slot)
		case "expr":
			expression, err := kirPEExpressionAt(lowerer.arena, statement.Expr)
			if err != nil {
				return err
			}
			if expression.CallTarget == "builtin:print" || expression.CallTarget == "builtin:println" {
				if err := lowerer.emitOutput(statement.Expr); err != nil {
					return err
				}
			} else if err := lowerer.emitExpr(statement.Expr); err != nil {
				return err
			}
		case "if":
			if err := lowerer.emitExpr(statement.Cond); err != nil {
				return err
			}
			lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x85, 0xc0)
			elseLabel, joinLabel := lowerer.machine.newLabel(), lowerer.machine.newLabel()
			if err := lowerer.machine.emitConditionalJump(0x84, elseLabel); err != nil {
				return err
			}
			if err := lowerer.emitScopedStatements(statement.Then); err != nil {
				return err
			}
			if statement.Else.Count != 0 {
				if err := lowerer.machine.emitJump(joinLabel); err != nil {
					return err
				}
				if err := lowerer.machine.bind(elseLabel); err != nil {
					return err
				}
				if err := lowerer.emitScopedStatements(statement.Else); err != nil {
					return err
				}
				if err := lowerer.machine.bind(joinLabel); err != nil {
					return err
				}
			} else if err := lowerer.machine.bind(elseLabel); err != nil {
				return err
			}
		case "while":
			conditionLabel, endLabel := lowerer.machine.newLabel(), lowerer.machine.newLabel()
			if err := lowerer.machine.bind(conditionLabel); err != nil {
				return err
			}
			if err := lowerer.emitExpr(statement.Cond); err != nil {
				return err
			}
			lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x85, 0xc0)
			if err := lowerer.machine.emitConditionalJump(0x84, endLabel); err != nil {
				return err
			}
			lowerer.loops = append(lowerer.loops, machineLoop{breakLabel: endLabel, continueLabel: conditionLabel})
			if err := lowerer.emitScopedStatements(statement.Body); err != nil {
				return err
			}
			lowerer.loops = lowerer.loops[:len(lowerer.loops)-1]
			if err := lowerer.machine.emitJump(conditionLabel); err != nil {
				return err
			}
			if err := lowerer.machine.bind(endLabel); err != nil {
				return err
			}
		case "break":
			if len(lowerer.loops) == 0 {
				return fmt.Errorf("direct PE break is outside a loop")
			}
			if err := lowerer.machine.emitJump(lowerer.loops[len(lowerer.loops)-1].breakLabel); err != nil {
				return err
			}
		case "continue":
			if len(lowerer.loops) == 0 {
				return fmt.Errorf("direct PE continue is outside a loop")
			}
			if err := lowerer.machine.emitJump(lowerer.loops[len(lowerer.loops)-1].continueLabel); err != nil {
				return err
			}
		case "return":
			result, err := kirPEExpressionAt(lowerer.arena, statement.Return)
			if err != nil {
				return err
			}
			if result != nil && result.Kind != "nil" {
				if err := lowerer.emitExpr(statement.Return); err != nil {
					return err
				}
			}
			if lowerer.inFunction {
				lowerer.machine.emitFunctionEpilog()
			} else if err := lowerer.machine.emitJump(lowerer.machine.endLabel); err != nil {
				return err
			}
		default:
			return fmt.Errorf("direct PE lowering does not support statement kind %s", statement.Kind)
		}
		if topLevel && statement.Kind != "return" {
			if err := lowerer.emitWallClockCheck(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (lowerer *kirPEMachine) emitOutput(ref MIRRef) error {
	expression, err := kirPEExpressionAt(lowerer.arena, ref)
	if err != nil {
		return err
	}
	if expression == nil {
		return fmt.Errorf("direct PE output is missing its call expression")
	}
	if len(expression.Args) != 1 {
		return fmt.Errorf("direct PE %s expects one argument", expression.Name)
	}
	argument := expression.Args[0]
	if err := lowerer.emitExpr(argument); err != nil {
		return err
	}
	newline := expression.CallTarget == "builtin:println"
	argumentView, err := kirPEExpressionAt(lowerer.arena, argument)
	if err != nil {
		return err
	}
	switch argumentView.Type {
	case "String":
		return lowerer.machine.emitStringOutput(newline)
	case "Bool":
		return lowerer.emitBooleanOutput(newline)
	default:
		return lowerer.machine.emitInteger(strings.HasPrefix(argumentView.Type, "UInt"), newline)
	}
}

func (lowerer *kirPEMachine) emitBooleanOutput(newline bool) error {
	falseLabel, doneLabel := lowerer.machine.newLabel(), lowerer.machine.newLabel()
	lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x85, 0xc0)
	if err := lowerer.machine.emitConditionalJump(0x84, falseLabel); err != nil {
		return err
	}
	lowerer.machine.emitStringAddress("true")
	if err := lowerer.machine.emitStringOutput(newline); err != nil {
		return err
	}
	if err := lowerer.machine.emitJump(doneLabel); err != nil {
		return err
	}
	if err := lowerer.machine.bind(falseLabel); err != nil {
		return err
	}
	lowerer.machine.emitStringAddress("false")
	if err := lowerer.machine.emitStringOutput(newline); err != nil {
		return err
	}
	return lowerer.machine.bind(doneLabel)
}

func (lowerer *kirPEMachine) emitStringConversion(ref MIRRef) error {
	argument, err := kirPEExpressionAt(lowerer.arena, ref)
	if err != nil {
		return err
	}
	if argument == nil {
		return fmt.Errorf("direct PE str is missing its value")
	}
	if err := lowerer.emitExpr(ref); err != nil {
		return err
	}
	switch argument.Type {
	case "String":
		return nil
	case "Bool":
		falseLabel, doneLabel := lowerer.machine.newLabel(), lowerer.machine.newLabel()
		lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x85, 0xc0)
		if err := lowerer.machine.emitConditionalJump(0x84, falseLabel); err != nil {
			return err
		}
		lowerer.machine.emitStringAddress("true")
		if err := lowerer.machine.emitJump(doneLabel); err != nil {
			return err
		}
		if err := lowerer.machine.bind(falseLabel); err != nil {
			return err
		}
		lowerer.machine.emitStringAddress("false")
		return lowerer.machine.bind(doneLabel)
	case "Int":
		return lowerer.machine.emitPEStringFromInteger(false)
	default:
		if strings.HasPrefix(argument.Type, "UInt") {
			return lowerer.machine.emitPEStringFromInteger(true)
		}
		return fmt.Errorf("direct PE str does not support %s", argument.Type)
	}
}

func (lowerer *kirPEMachine) emitExpr(ref MIRRef) error {
	expression, err := kirPEExpressionAt(lowerer.arena, ref)
	if err != nil {
		return err
	}
	if expression == nil {
		return fmt.Errorf("direct PE cannot lower a missing KIR expression")
	}
	if expression.Const.Present {
		if uint64(expression.Const.Index) >= uint64(len(lowerer.arena.Values)) {
			return fmt.Errorf("direct PE constant reference %d is out of bounds", expression.Const.Index)
		}
		constant := lowerer.arena.Values[expression.Const.Index].Value
		switch constant.Kind {
		case "int":
			lowerer.machine.emitMoveImmediate(uint64(constant.Int))
			return nil
		case "uint":
			lowerer.machine.emitMoveImmediate(constant.UInt)
			lowerer.machine.emitUIntMask(constant.UIntBits)
			return nil
		case "bool":
			if constant.Bool {
				lowerer.machine.emitMoveImmediate(1)
			} else {
				lowerer.machine.emitMoveImmediate(0)
			}
			return nil
		case "string":
			lowerer.machine.emitStringAddress(constant.String)
			return nil
		}
	}
	switch expression.Kind {
	case "int":
		lowerer.machine.emitMoveImmediate(uint64(expression.Int))
		return nil
	case "bool":
		if expression.Bool {
			lowerer.machine.emitMoveImmediate(1)
		} else {
			lowerer.machine.emitMoveImmediate(0)
		}
		return nil
	case "string":
		lowerer.machine.emitStringAddress(expression.String)
		return nil
	case "var":
		slot, ok := lowerer.lookupSlot(expression.Binding)
		if !ok {
			return fmt.Errorf("direct PE has no storage for binding '%s'", expression.Name)
		}
		lowerer.machine.emitLoadSlot(slot)
		return nil
	case "unary":
		if err := lowerer.emitExpr(expression.Operand); err != nil {
			return err
		}
		switch expression.Operator {
		case "+":
			return nil
		case "-":
			lowerer.machine.code = append(lowerer.machine.code, 0x48, 0xf7, 0xd8)
			if expression.Type == "Int" {
				if err := lowerer.machine.emitConditionalJump(0x80, lowerer.runtimeFailureLabel("negation overflow")); err != nil {
					return err
				}
			}
			return nil
		case "!":
			lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x85, 0xc0, 0x0f, 0x94, 0xc0, 0x48, 0x0f, 0xb6, 0xc0)
			return nil
		case "~":
			lowerer.machine.code = append(lowerer.machine.code, 0x48, 0xf7, 0xd0)
			lowerer.machine.emitUIntMask(kirPEBits(expression.Type))
			return nil
		default:
			return fmt.Errorf("direct PE does not support unary operator %s", expression.Operator)
		}
	case "binary":
		return lowerer.emitBinary(expression)
	case "call":
		if strings.HasPrefix(expression.CallTarget, "function:") {
			return lowerer.emitFunctionCall(expression)
		}
		switch expression.CallTarget {
		case "builtin:str":
			if len(expression.Args) != 1 {
				return fmt.Errorf("direct PE str expects one argument")
			}
			return lowerer.emitStringConversion(expression.Args[0])
		case "builtin:u8", "builtin:u16", "builtin:u32", "builtin:u64":
			if len(expression.Args) != 1 {
				return fmt.Errorf("direct PE conversion %s expects one argument", expression.Name)
			}
			if err := lowerer.emitExpr(expression.Args[0]); err != nil {
				return err
			}
			if err := lowerer.emitUnsignedConversionCheck(expression.Args[0], kirPEBits(expression.Type)); err != nil {
				return err
			}
			lowerer.machine.emitUIntMask(kirPEBits(expression.Type))
			return nil
		case "builtin:print", "builtin:println":
			return fmt.Errorf("print output is only valid as an expression statement")
		default:
			return fmt.Errorf("direct PE does not support builtin %q", expression.Name)
		}
	default:
		return fmt.Errorf("direct PE does not support expression kind %s", expression.Kind)
	}
}

func (lowerer *kirPEMachine) emitUnsignedConversionCheck(ref MIRRef, bits uint8) error {
	value, err := kirPEExpressionAt(lowerer.arena, ref)
	if err != nil {
		return err
	}
	if value == nil {
		return fmt.Errorf("direct PE backend cannot validate an incomplete unsigned conversion")
	}
	inputBits := kirPEBits(value.Type)
	if (value.Type != "Int" && inputBits == 0) || bits == 0 {
		return fmt.Errorf("direct PE backend cannot validate an incomplete unsigned conversion")
	}
	if value.Type == "Int" {
		lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x85, 0xc0)
		if err := lowerer.machine.emitConditionalJump(0x88, lowerer.runtimeFailureLabel("Int is outside unsigned range")); err != nil {
			return err
		}
		inputBits = 64
	}
	if bits >= inputBits || bits >= 64 {
		return nil
	}
	mask := ^((uint64(1) << bits) - 1)
	lowerer.machine.code = append(lowerer.machine.code, 0x48, 0xb9)
	var encoded [8]byte
	binary.LittleEndian.PutUint64(encoded[:], mask)
	lowerer.machine.code = append(lowerer.machine.code, encoded[:]...)
	lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x85, 0xc8)
	return lowerer.machine.emitConditionalJump(0x85, lowerer.runtimeFailureLabel("value is outside unsigned range"))
}

func (lowerer *kirPEMachine) emitBinary(expression *kirPEExpression) error {
	if expression.Operator == "&&" || expression.Operator == "||" {
		if err := lowerer.emitExpr(expression.Left); err != nil {
			return err
		}
		lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x85, 0xc0)
		shortCircuit, done := lowerer.machine.newLabel(), lowerer.machine.newLabel()
		condition := byte(0x84)
		if expression.Operator == "||" {
			condition = 0x85
		}
		if err := lowerer.machine.emitConditionalJump(condition, shortCircuit); err != nil {
			return err
		}
		if err := lowerer.emitExpr(expression.Right); err != nil {
			return err
		}
		lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x85, 0xc0, 0x0f, 0x95, 0xc0, 0x48, 0x0f, 0xb6, 0xc0)
		if err := lowerer.machine.emitJump(done); err != nil {
			return err
		}
		if err := lowerer.machine.bind(shortCircuit); err != nil {
			return err
		}
		if expression.Operator == "&&" {
			lowerer.machine.emitMoveImmediate(0)
		} else {
			lowerer.machine.emitMoveImmediate(1)
		}
		return lowerer.machine.bind(done)
	}
	if err := lowerer.emitExpr(expression.Left); err != nil {
		return err
	}
	lowerer.machine.code = append(lowerer.machine.code, 0x50)
	lowerer.machine.windowsStackDepth += 8
	if err := lowerer.emitExpr(expression.Right); err != nil {
		lowerer.machine.windowsStackDepth -= 8
		return err
	}
	lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x89, 0xc1, 0x58)
	lowerer.machine.windowsStackDepth -= 8
	leftExpression, err := kirPEExpressionAt(lowerer.arena, expression.Left)
	if err != nil {
		return err
	}
	leftType := kirPEType(leftExpression.Type)
	unsigned := strings.HasPrefix(leftExpression.Type, "UInt")
	switch expression.Operator {
	case "+":
		lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x01, 0xc8)
		if !unsigned {
			if err := lowerer.machine.emitConditionalJump(0x80, lowerer.runtimeFailureLabel("checked integer arithmetic overflow")); err != nil {
				return err
			}
		}
		lowerer.machine.emitUIntMask(kirPEBits(expression.Type))
	case "-":
		lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x29, 0xc8)
		if !unsigned {
			if err := lowerer.machine.emitConditionalJump(0x80, lowerer.runtimeFailureLabel("checked integer arithmetic overflow")); err != nil {
				return err
			}
		}
		lowerer.machine.emitUIntMask(kirPEBits(expression.Type))
	case "*":
		lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x0f, 0xaf, 0xc1)
		if !unsigned {
			if err := lowerer.machine.emitConditionalJump(0x80, lowerer.runtimeFailureLabel("checked integer arithmetic overflow")); err != nil {
				return err
			}
		}
		lowerer.machine.emitUIntMask(kirPEBits(expression.Type))
	case "/", "%":
		lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x85, 0xc9)
		zeroMessage := "division by zero"
		if expression.Operator == "%" {
			zeroMessage = "remainder by zero"
		}
		if err := lowerer.machine.emitConditionalJump(0x84, lowerer.runtimeFailureLabel(zeroMessage)); err != nil {
			return err
		}
		if unsigned {
			lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x31, 0xd2, 0x48, 0xf7, 0xf1)
		} else {
			safeDiv := lowerer.machine.newLabel()
			lowerer.machine.code = append(lowerer.machine.code, 0x48, 0xba)
			var minimum [8]byte
			binary.LittleEndian.PutUint64(minimum[:], uint64(1<<63))
			lowerer.machine.code = append(lowerer.machine.code, minimum[:]...)
			lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x39, 0xd0)
			_ = lowerer.machine.emitConditionalJump(0x85, safeDiv)
			lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x83, 0xf9, 0xff)
			_ = lowerer.machine.emitConditionalJump(0x85, safeDiv)
			if err := lowerer.machine.emitJump(lowerer.runtimeFailureLabel("checked integer arithmetic overflow")); err != nil {
				return err
			}
			if err := lowerer.machine.bind(safeDiv); err != nil {
				return err
			}
			lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x99, 0x48, 0xf7, 0xf9)
		}
		if expression.Operator == "%" {
			lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x89, 0xd0)
		}
		lowerer.machine.emitUIntMask(kirPEBits(expression.Type))
	case "&":
		lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x21, 0xc8)
		lowerer.machine.emitUIntMask(kirPEBits(expression.Type))
	case "^":
		lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x31, 0xc8)
		lowerer.machine.emitUIntMask(kirPEBits(expression.Type))
	case "|":
		lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x09, 0xc8)
		lowerer.machine.emitUIntMask(kirPEBits(expression.Type))
	case "<<", ">>":
		lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x83, 0xf9, machineBits(leftType))
		if err := lowerer.machine.emitConditionalJump(0x83, lowerer.runtimeFailureLabel("shift count must be between 0 and UInt width minus one")); err != nil {
			return err
		}
		if expression.Operator == "<<" {
			lowerer.machine.code = append(lowerer.machine.code, 0x48, 0xd3, 0xe0)
		} else {
			lowerer.machine.code = append(lowerer.machine.code, 0x48, 0xd3, 0xe8)
		}
		lowerer.machine.emitUIntMask(kirPEBits(expression.Type))
	default:
		token, ok := kirPEComparisonToken(expression.Operator)
		if !ok {
			return fmt.Errorf("direct PE does not support binary operator %s", expression.Operator)
		}
		lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x39, 0xc8, 0x0f, machineSetcc(token, unsigned), 0xc0, 0x48, 0x0f, 0xb6, 0xc0)
	}
	return nil
}

func kirPEComparisonToken(operator string) (TokenKind, bool) {
	switch operator {
	case "==":
		return EQEQ, true
	case "!=":
		return NEQ, true
	case "<":
		return LESS, true
	case "<=":
		return LEQ, true
	case ">":
		return GREATER, true
	case ">=":
		return GEQ, true
	default:
		return 0, false
	}
}

func kirPEBits(name string) uint8 {
	switch name {
	case "UInt8":
		return 8
	case "UInt16":
		return 16
	case "UInt32":
		return 32
	case "UInt64":
		return 64
	default:
		return 0
	}
}

func (lowerer *kirPEMachine) emitFunctionCall(expression *kirPEExpression) error {
	target := strings.TrimPrefix(expression.CallTarget, "function:")
	function, ok := lowerer.program.byTarget[target]
	if !ok {
		return fmt.Errorf("direct PE has no function target %q", target)
	}
	if len(expression.Args) != len(function.function.Params) {
		return fmt.Errorf("direct PE does not support omitted default arguments for function '%s'", function.function.Name)
	}
	spillBytes := (len(expression.Args)*8 + 15) &^ 15
	if spillBytes > 0 {
		lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x83, 0xec, byte(spillBytes))
	}
	for index, argument := range expression.Args {
		if err := lowerer.emitExpr(argument); err != nil {
			return err
		}
		lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x89, 0x44, 0x24, byte(index*8))
	}
	stackArgumentCount := len(expression.Args) - 4
	if stackArgumentCount < 0 {
		stackArgumentCount = 0
	}
	outgoingBytes := 32 + stackArgumentCount*8
	if remainder := (lowerer.machine.windowsStackDepth + outgoingBytes) % 16; remainder != 0 {
		outgoingBytes += 16 - remainder
	}
	lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x83, 0xec, byte(outgoingBytes))
	for index := 4; index < len(expression.Args); index++ {
		sourceOffset := outgoingBytes + index*8
		destinationOffset := 32 + (index-4)*8
		lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x8b, 0x44, 0x24, byte(sourceOffset), 0x48, 0x89, 0x44, 0x24, byte(destinationOffset))
	}
	registerLoads := [4][]byte{{0x48, 0x8b, 0x4c, 0x24}, {0x48, 0x8b, 0x54, 0x24}, {0x4c, 0x8b, 0x44, 0x24}, {0x4c, 0x8b, 0x4c, 0x24}}
	for index := 0; index < len(expression.Args) && index < 4; index++ {
		lowerer.machine.code = append(lowerer.machine.code, registerLoads[index]...)
		lowerer.machine.code = append(lowerer.machine.code, byte(outgoingBytes+index*8))
	}
	lowerer.machine.code = append(lowerer.machine.code, 0xe8)
	if err := lowerer.machine.emitLabelDisplacement(function.label); err != nil {
		return err
	}
	lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x83, 0xc4, byte(outgoingBytes))
	if spillBytes > 0 {
		lowerer.machine.code = append(lowerer.machine.code, 0x48, 0x83, 0xc4, byte(spillBytes))
	}
	return nil
}
