package kry

import (
	"fmt"
	"strings"
)

func kirMetadataFromArena(arena *KIRArena) KIRMetadata {
	if arena == nil {
		return KIRMetadata{}
	}
	return KIRMetadata{
		Format: arena.Format, Version: arena.Version, LanguageVersion: arena.LanguageVersion,
		Module: arena.Module, Source: arena.Source, Target: arena.Target,
		Imports: arena.Imports, Sources: arena.Sources, Structs: arena.Structs,
		Enums: arena.Enums, Traits: arena.Traits, TraitImpls: arena.TraitImpls,
	}
}

// Direct ELF lowering reads child edges through these accessors. The arena
// view keeps scalar node adapters stable so identity keyed call and slot maps
// remain valid without attaching a recursive KIR graph to those adapters.
func (builder *kirDirectBuilder) exprLeft(node *KIRExpr) *KIRExpr {
	return builder.executor.exprLeft(node)
}
func (builder *kirDirectBuilder) exprRight(node *KIRExpr) *KIRExpr {
	return builder.executor.exprRight(node)
}
func (builder *kirDirectBuilder) exprOperand(node *KIRExpr) *KIRExpr {
	return builder.executor.exprOperand(node)
}
func (builder *kirDirectBuilder) exprBase(node *KIRExpr) *KIRExpr {
	return builder.executor.exprBase(node)
}
func (builder *kirDirectBuilder) exprReceiver(node *KIRExpr) *KIRExpr {
	return builder.executor.exprReceiver(node)
}
func (builder *kirDirectBuilder) exprCallee(node *KIRExpr) *KIRExpr {
	return builder.executor.exprCallee(node)
}
func (builder *kirDirectBuilder) exprArgs(node *KIRExpr) []*KIRExpr {
	return builder.executor.exprArgs(node)
}
func (builder *kirDirectBuilder) exprItems(node *KIRExpr) []*KIRExpr {
	return builder.executor.exprItems(node)
}
func (builder *kirDirectBuilder) exprMapKeys(node *KIRExpr) []*KIRExpr {
	return builder.executor.exprMapKeys(node)
}
func (builder *kirDirectBuilder) exprValues(node *KIRExpr) []*KIRExpr {
	return builder.executor.exprValues(node)
}
func (builder *kirDirectBuilder) stmtInit(node *KIRStmt) *KIRExpr {
	return builder.executor.stmtInit(node)
}
func (builder *kirDirectBuilder) stmtExpr(node *KIRStmt) *KIRExpr {
	return builder.executor.stmtExpr(node)
}
func (builder *kirDirectBuilder) stmtTarget(node *KIRStmt) *KIRExpr {
	return builder.executor.stmtTarget(node)
}
func (builder *kirDirectBuilder) stmtValue(node *KIRStmt) *KIRExpr {
	return builder.executor.stmtValue(node)
}
func (builder *kirDirectBuilder) stmtCond(node *KIRStmt) *KIRExpr {
	return builder.executor.stmtCond(node)
}
func (builder *kirDirectBuilder) stmtIter(node *KIRStmt) *KIRExpr {
	return builder.executor.stmtIter(node)
}
func (builder *kirDirectBuilder) stmtReturn(node *KIRStmt) *KIRExpr {
	return builder.executor.stmtReturn(node)
}
func (builder *kirDirectBuilder) stmtScrutinee(node *KIRStmt) *KIRExpr {
	return builder.executor.stmtScrutinee(node)
}
func (builder *kirDirectBuilder) stmtThen(node *KIRStmt) []*KIRStmt {
	return builder.executor.stmtThen(node)
}
func (builder *kirDirectBuilder) stmtElse(node *KIRStmt) []*KIRStmt {
	return builder.executor.stmtElse(node)
}
func (builder *kirDirectBuilder) stmtBody(node *KIRStmt) []*KIRStmt {
	return builder.executor.stmtBody(node)
}
func (builder *kirDirectBuilder) stmtArms(node *KIRStmt) []*KIRArm {
	return builder.executor.stmtArms(node)
}
func (builder *kirDirectBuilder) armBody(node *KIRArm) []*KIRStmt {
	return builder.executor.armBody(node)
}
func (builder *kirDirectBuilder) functionBody(node *KIRFunction) []*KIRStmt {
	return builder.executor.functionBody(node)
}

func validateDirectKIRValueArena(view *kirExecArenaView, metadata KIRMetadata, supportValues bool) error {
	if view == nil || view.arena == nil {
		return fmt.Errorf("direct ELF backend requires validated MIR arena")
	}
	arena := view.arena
	functions := kirExecFunctionsFromArena(view)
	executor := &kirExecutor{arenaView: view, metadata: metadata, functions: functions}
	statements := view.statementList(arena.TopStatements)
	topFunctions := view.functionList(arena.TopFunctions)
	entry, entryTarget := kirExecEntryFunctionFromArena(view, functions)
	if !supportValues {
		if len(statements) != 0 && len(topFunctions) != 0 {
			return fmt.Errorf("%w: direct ELF backend does not execute top-level function declarations", errKIRSubsetUnsupported)
		}
		for _, function := range topFunctions {
			if function != nil && len(function.Captures) != 0 {
				return fmt.Errorf("%w: direct ELF backend does not lower lambdas or captured bindings", errKIRSubsetUnsupported)
			}
		}
		seenExpressions := make(map[MIRIndex]bool)
		var rejectFunctionClosures func(*KIRFunction) error
		var rejectBlockClosures func([]*KIRStmt) error
		var rejectExpressionClosure func(*KIRExpr) error
		rejectExpressionClosure = func(expression *KIRExpr) error {
			if expression == nil {
				return nil
			}
			if index, ok := view.expressionIndex[expression]; ok {
				if seenExpressions[index] {
					return nil
				}
				seenExpressions[index] = true
			}
			if expression.Kind == "lambda" || executor.exprLambda(expression) != nil || strings.HasPrefix(expression.Type, "fn(") {
				return fmt.Errorf("%w: direct ELF backend does not lower lambdas or captured bindings", errKIRSubsetUnsupported)
			}
			for _, child := range []*KIRExpr{executor.exprLeft(expression), executor.exprRight(expression), executor.exprOperand(expression), executor.exprBase(expression), executor.exprReceiver(expression), executor.exprCallee(expression)} {
				if err := rejectExpressionClosure(child); err != nil {
					return err
				}
			}
			for _, list := range [][]*KIRExpr{executor.exprArgs(expression), executor.exprItems(expression), executor.exprMapKeys(expression), executor.exprValues(expression)} {
				for _, child := range list {
					if err := rejectExpressionClosure(child); err != nil {
						return err
					}
				}
			}
			return nil
		}
		rejectBlockClosures = func(block []*KIRStmt) error {
			for _, statement := range block {
				if statement == nil {
					continue
				}
				for _, expression := range []*KIRExpr{executor.stmtInit(statement), executor.stmtExpr(statement), executor.stmtTarget(statement), executor.stmtValue(statement), executor.stmtCond(statement), executor.stmtIter(statement), executor.stmtReturn(statement), executor.stmtScrutinee(statement)} {
					if err := rejectExpressionClosure(expression); err != nil {
						return err
					}
				}
				for _, nested := range [][]*KIRStmt{executor.stmtThen(statement), executor.stmtElse(statement), executor.stmtBody(statement)} {
					if err := rejectBlockClosures(nested); err != nil {
						return err
					}
				}
				for _, arm := range executor.stmtArms(statement) {
					if arm != nil {
						if err := rejectBlockClosures(executor.armBody(arm)); err != nil {
							return err
						}
					}
				}
			}
			return nil
		}
		rejectFunctionClosures = func(function *KIRFunction) error {
			if function == nil {
				return nil
			}
			return rejectBlockClosures(executor.functionBody(function))
		}
		if err := rejectBlockClosures(statements); err != nil {
			return err
		}
		for _, function := range topFunctions {
			if err := rejectFunctionClosures(function); err != nil {
				return err
			}
		}
		for _, function := range topFunctions {
			if function != nil && function != entry {
				return fmt.Errorf("%w: direct ELF backend helper functions are not lowered", errKIRSubsetUnsupported)
			}
		}
	}
	if err := executor.validateArenaExecSubset(true); err != nil {
		return err
	}
	if !supportValues && len(topFunctions) != 0 {
		return fmt.Errorf("%w: direct scalar KIR ELF does not lower function declarations", errKIRSubsetUnsupported)
	}
	if supportValues && entry != nil && len(statements) != 0 {
		return fmt.Errorf("%w: direct KIR ELF does not combine a main() entry function with top-level statements", errKIRSubsetUnsupported)
	}
	if supportValues {
		for _, function := range topFunctions {
			if err := validDirectKIRFunction(function, metadata); err != nil {
				return err
			}
		}
		if entry != nil && (entryTarget == "" || entry.Return != "Nil" || len(entry.Params) != 0) {
			return fmt.Errorf("%w: direct KIR ELF main() entry must take no arguments and return Nil", errKIRSubsetUnsupported)
		}
	}
	if len(statements) == 0 && len(topFunctions) == 0 {
		return fmt.Errorf("%w: no executable statements", errKIRSubsetUnsupported)
	}

	seenExpressions := make(map[MIRIndex]bool)
	seenStatements := make(map[MIRIndex]bool)
	nodeCount := 0
	countNode := func() error {
		nodeCount++
		if nodeCount > maxKIRDirectNodes {
			return fmt.Errorf("%w: direct ELF lowering is limited to %d KIR nodes", errKIRSubsetUnsupported, maxKIRDirectNodes)
		}
		return nil
	}
	var validateExpr func(*KIRExpr) error
	var validateBlock func([]*KIRStmt) error
	validateExpr = func(expression *KIRExpr) error {
		if expression == nil {
			return nil
		}
		if index, ok := view.expressionIndex[expression]; ok {
			if seenExpressions[index] {
				return nil
			}
			seenExpressions[index] = true
		}
		if err := countNode(); err != nil {
			return err
		}
		if expression.Kind == "lambda" || executor.exprLambda(expression) != nil {
			return fmt.Errorf("%w: direct KIR ELF does not lower lambdas or captured bindings", errKIRSubsetUnsupported)
		}
		if strings.HasPrefix(expression.Type, "fn(") {
			return fmt.Errorf("%w: direct KIR ELF does not lower function values or closures", errKIRSubsetUnsupported)
		}
		if executor.exprCallee(expression) != nil {
			return fmt.Errorf("%w: direct KIR ELF does not lower indirect function calls", errKIRSubsetUnsupported)
		}
		args := executor.exprArgs(expression)
		functionCall := strings.HasPrefix(expression.CallTarget, "function:")
		if functionCall {
			_, target, ok := strings.Cut(expression.CallTarget, ":")
			function := functions[target]
			if expression.Kind != "call" || !supportValues || !ok || function == nil || function.Name != expression.Name || function == entry || executor.exprReceiver(expression) != nil || len(expression.GenericArguments) != 0 || expression.TraitName != "" {
				return fmt.Errorf("%w: direct KIR ELF call target %q is not a supported helper function", errKIRSubsetUnsupported, expression.CallTarget)
			}
			if len(args) < len(function.Params) {
				for _, parameter := range function.Params[len(args):] {
					if parameter != nil && executor.parameterDefault(parameter) != nil {
						return fmt.Errorf("%w: direct KIR ELF function %q does not support omitted default arguments", errKIRSubsetUnsupported, function.Name)
					}
				}
			}
			if len(args) != len(function.Params) || len(args) > 6 || expression.Type != function.Return {
				return fmt.Errorf("invalid KIR executable: direct call to %q has an unsupported signature", expression.Name)
			}
			for index, argument := range args {
				if argument == nil || argument.Type != function.Params[index].Type {
					return fmt.Errorf("invalid KIR executable: direct call to %q has mismatched argument %d", expression.Name, index+1)
				}
			}
		}
		if !supportValues && expression.Kind == "call" {
			return fmt.Errorf("%w: direct KIR ELF does not lower builtin or function call %q in expression position", errKIRSubsetUnsupported, expression.Name)
		}
		if supportValues {
			if !directKIRTypeSupported(expression.Type, metadata) {
				return fmt.Errorf("%w: direct KIR ELF does not lower values of type %q", errKIRSubsetUnsupported, expression.Type)
			}
		} else {
			if expression.Type == "Float" {
				return fmt.Errorf("%w: Float values are not yet lowered by the direct KIR ELF backend", errKIRSubsetUnsupported)
			}
			switch expression.Type {
			case "Int", "Bool", "String", "Nil":
			default:
				return fmt.Errorf("%w: direct KIR ELF does not lower values of type %q", errKIRSubsetUnsupported, expression.Type)
			}
		}
		if !supportValues {
			switch expression.Kind {
			case "int", "bool", "string", "nil", "var", "unary", "binary":
			default:
				return fmt.Errorf("%w: direct KIR ELF does not lower expression kind %q", errKIRSubsetUnsupported, expression.Kind)
			}
		} else {
			switch expression.Kind {
			case "int", "bool", "string", "nil", "var", "unary", "binary", "array", "map", "index", "field":
				if expression.Kind == "field" {
					base := executor.exprBase(expression)
					if base == nil {
						return fmt.Errorf("invalid KIR executable: direct struct field access has no base")
					}
					field, _, ok := directKIRStructField(metadata, base.Type, expression.Field)
					if !ok || field.Type != expression.Type || !directKIRTypeSupported(field.Type, metadata) {
						return fmt.Errorf("%w: direct KIR ELF does not lower field %q on type %q", errKIRSubsetUnsupported, expression.Field, base.Type)
					}
				}
			case "struct":
				structure := directKIRStruct(metadata, expression.StructName)
				values := executor.exprValues(expression)
				if structure == nil || expression.Type != expression.StructName || expression.StructType != expression.StructName || len(expression.GenericArguments) != 0 {
					return fmt.Errorf("%w: direct KIR ELF supports only non-generic struct literals", errKIRSubsetUnsupported)
				}
				if len(expression.Fields) != len(values) || len(expression.Fields) != len(structure.Fields) {
					return fmt.Errorf("invalid KIR executable: struct %q literal has mismatched fields", expression.StructName)
				}
				seen := make(map[string]bool, len(expression.Fields))
				for index, name := range expression.Fields {
					field, _, ok := directKIRStructField(metadata, expression.StructName, name)
					value := values[index]
					if !ok || value == nil || seen[name] || value.Type != field.Type {
						return fmt.Errorf("invalid KIR executable: struct %q literal has invalid field %q", expression.StructName, name)
					}
					seen[name] = true
				}
			case "call":
				if functionCall {
					break
				}
				if executor.exprReceiver(expression) != nil || expression.CallTarget != "builtin:"+expression.Name {
					return fmt.Errorf("%w: direct KIR ELF only lowers builtin calls with resolved targets", errKIRSubsetUnsupported)
				}
				if err := validateDirectKIRBuiltinCall(expression, args, metadata); err != nil {
					return err
				}
				switch expression.Name {
				case "print", "println", "len", "array_push", "array_concat", "array_get", "array_indices", "array_set", "array_slice", "array_take", "array_drop", "array_reverse", "map_get", "map_contains_key", "map_insert", "map_remove", "process_args", "some", "none", "ok", "err", "is_some", "is_none", "is_ok", "is_err", "unwrap_or", "result_unwrap", "result_error", "assert", "assert_eq", "u8", "u16", "u32", "u64", "int", "str", "string_chars", "substring", "contains", "starts_with", "ends_with", "bytes", "bytes_from_u8", "u8_array", "string_to_bytes", "fs_read_text", "fs_write_bytes":
				default:
					return fmt.Errorf("%w: direct KIR ELF does not lower builtin %q", errKIRSubsetUnsupported, expression.Name)
				}
			default:
				return fmt.Errorf("%w: direct KIR ELF does not lower expression kind %q", errKIRSubsetUnsupported, expression.Kind)
			}
			if expression.Kind == "unary" && !directKIRUnarySupportedWithOperand(expression, executor.exprOperand(expression)) {
				return fmt.Errorf("%w: direct KIR ELF does not lower unary operator %q for %s", errKIRSubsetUnsupported, expression.Operator, expression.Type)
			}
			if expression.Kind == "binary" && !directKIRBinarySupportedWithOperands(expression, executor.exprLeft(expression), executor.exprRight(expression)) {
				return fmt.Errorf("%w: direct KIR ELF does not lower binary operator %q for %s", errKIRSubsetUnsupported, expression.Operator, expression.Type)
			}
		}
		if supportValues && expression.Kind == "index" {
			base, index := executor.exprBase(expression), executor.exprLeft(expression)
			if base == nil || index == nil {
				return fmt.Errorf("%w: direct KIR ELF indexing supports Array[T] only", errKIRSubsetUnsupported)
			}
			name, arguments, composite := parseKIRContainerType(base.Type)
			if !composite || name != "Array" || len(arguments) != 1 || index.Type != "Int" || arguments[0] != expression.Type {
				return fmt.Errorf("%w: direct KIR ELF indexing supports Array[T] only", errKIRSubsetUnsupported)
			}
		}
		if supportValues && expression.Kind == "array" {
			arrayName, arrayTypes, arrayComposite := parseKIRContainerType(expression.Type)
			if !arrayComposite || arrayName != "Array" || len(arrayTypes) != 1 {
				return fmt.Errorf("invalid KIR executable: direct array expression has type %q", expression.Type)
			}
			for _, item := range executor.exprItems(expression) {
				if item == nil || !directKIRTypeSupported(item.Type, metadata) || item.Type != arrayTypes[0] {
					return fmt.Errorf("%w: direct KIR ELF array item has unsupported type", errKIRSubsetUnsupported)
				}
			}
		}
		if supportValues && expression.Kind == "map" {
			mapTypes, ok := directKIRMapType(expression.Type)
			keys, values := executor.exprMapKeys(expression), executor.exprValues(expression)
			if !ok || !directKIRTypeSupported(expression.Type, metadata) {
				return fmt.Errorf("invalid KIR executable: direct map expression has type %q", expression.Type)
			}
			if _, ok := directKIRMapKeyKind(mapTypes[0]); !ok {
				return fmt.Errorf("%w: direct KIR ELF map keys of type %q are unsupported", errKIRSubsetUnsupported, mapTypes[0])
			}
			if len(keys) != len(values) {
				return fmt.Errorf("invalid KIR executable: direct map literal has mismatched key/value counts")
			}
			for index, key := range keys {
				value := values[index]
				if key == nil || value == nil || key.Type != mapTypes[0] || value.Type != mapTypes[1] {
					return fmt.Errorf("invalid KIR executable: direct map literal entry does not match %q", expression.Type)
				}
			}
		}
		children := []*KIRExpr{executor.exprLeft(expression), executor.exprRight(expression), executor.exprOperand(expression), executor.exprBase(expression), executor.exprReceiver(expression), executor.exprCallee(expression)}
		for _, child := range children {
			if err := validateExpr(child); err != nil {
				return err
			}
		}
		for _, list := range [][]*KIRExpr{args, executor.exprItems(expression), executor.exprMapKeys(expression), executor.exprValues(expression)} {
			for _, child := range list {
				if err := validateExpr(child); err != nil {
					return err
				}
			}
		}
		if supportValues && expression.Kind == "call" {
			for _, argument := range args {
				if argument == nil || !directKIRTypeSupported(argument.Type, metadata) {
					return fmt.Errorf("%w: direct KIR ELF builtin %q has an unsupported argument type", errKIRSubsetUnsupported, expression.Name)
				}
			}
		}
		return nil
	}
	validateBlock = func(block []*KIRStmt) error {
		for _, statement := range block {
			if statement == nil {
				return fmt.Errorf("invalid KIR executable: direct ELF block contains a missing statement")
			}
			if index, ok := view.statementIndex[statement]; ok {
				if seenStatements[index] {
					continue
				}
				seenStatements[index] = true
			}
			if err := countNode(); err != nil {
				return err
			}
			switch statement.Kind {
			case "let", "const":
				if err := validateExpr(executor.stmtInit(statement)); err != nil {
					return err
				}
			case "assign":
				target, value := executor.stmtTarget(statement), executor.stmtValue(statement)
				if target == nil || target.Kind != "var" {
					return fmt.Errorf("%w: direct KIR ELF assignment targets must be local variables", errKIRSubsetUnsupported)
				}
				if err := validateExpr(target); err != nil {
					return err
				}
				if err := validateExpr(value); err != nil {
					return err
				}
			case "expr":
				expression := executor.stmtExpr(statement)
				if supportValues {
					if err := validateExpr(expression); err != nil {
						return err
					}
					if expression == nil || expression.Kind != "call" {
						return fmt.Errorf("%w: direct KIR ELF expression statement must be a supported builtin call", errKIRSubsetUnsupported)
					}
				} else {
					if expression == nil || expression.Kind != "call" || (expression.Name != "print" && expression.Name != "println") {
						return fmt.Errorf("%w: direct KIR ELF supports expression statements only for print/println", errKIRSubsetUnsupported)
					}
					args := executor.exprArgs(expression)
					if len(args) != 1 {
						return fmt.Errorf("%w: direct KIR ELF print/println requires exactly one argument", errKIRSubsetUnsupported)
					}
					if err := validateExpr(args[0]); err != nil {
						return err
					}
				}
				if expression.Name == "print" || expression.Name == "println" {
					args := executor.exprArgs(expression)
					if len(args) != 1 {
						return fmt.Errorf("%w: direct KIR ELF print/println requires exactly one argument", errKIRSubsetUnsupported)
					}
					argument := args[0]
					if argument == nil || (argument.Type != "Int" && !strings.HasPrefix(argument.Type, "UInt") && argument.Type != "Bool" && argument.Type != "String") {
						return fmt.Errorf("%w: native print does not support %s values", errKIRSubsetUnsupported, argument.Type)
					}
				}
			case "if":
				if err := validateExpr(executor.stmtCond(statement)); err != nil {
					return err
				}
				if err := validateBlock(executor.stmtThen(statement)); err != nil {
					return err
				}
				if err := validateBlock(executor.stmtElse(statement)); err != nil {
					return err
				}
			case "while":
				if err := validateExpr(executor.stmtCond(statement)); err != nil {
					return err
				}
				if err := validateBlock(executor.stmtBody(statement)); err != nil {
					return err
				}
			case "for":
				if !supportValues {
					return fmt.Errorf("%w: statement %q", errKIRSubsetUnsupported, statement.Kind)
				}
				iterator := executor.stmtIter(statement)
				if err := validateExpr(iterator); err != nil {
					return err
				}
				if iterator == nil {
					return fmt.Errorf("invalid KIR executable: direct for loop has no iterator")
				}
				name, arguments, composite := parseKIRContainerType(iterator.Type)
				if !composite || name != "Array" || len(arguments) != 1 || statement.Binding == nil || statement.Binding.Type != arguments[0] || statement.Binding.Name != statement.Name {
					return fmt.Errorf("%w: direct KIR ELF for loops support Array[T] with a matching binding", errKIRSubsetUnsupported)
				}
				if err := validateBlock(executor.stmtBody(statement)); err != nil {
					return err
				}
			case "break", "continue":
				if !supportValues {
					return fmt.Errorf("%w: statement %q", errKIRSubsetUnsupported, statement.Kind)
				}
			case "return":
				if err := validateExpr(executor.stmtReturn(statement)); err != nil {
					return err
				}
			default:
				return fmt.Errorf("%w: statement %q", errKIRSubsetUnsupported, statement.Kind)
			}
		}
		return nil
	}
	if len(statements) != 0 {
		if err := validateBlock(statements); err != nil {
			return err
		}
	}
	for _, function := range topFunctions {
		if function == nil {
			continue
		}
		if err := validateBlock(executor.functionBody(function)); err != nil {
			if isDirectELFUnsupportedResultErrorPayload(err) {
				return err
			}
			return fmt.Errorf("function %q: %w", function.Name, err)
		}
	}
	return nil
}
