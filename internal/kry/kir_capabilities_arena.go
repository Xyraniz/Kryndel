package kry

import (
	"fmt"
	"strings"
)

// validateMIRNativeFeatureSupport checks backend capabilities by following
// indexes in the validated arena. It does not reconstruct the wire tree.
func validateMIRNativeFeatureSupport(mir *ValidatedMIR, format string, target NativeTarget) error {
	if mir == nil || mir.arena == nil {
		return fmt.Errorf("missing validated MIR")
	}
	arena := mir.arena
	if err := arena.validateReferences(); err != nil {
		return fmt.Errorf("invalid validated MIR arena: %w", err)
	}
	// Keep builtin diagnostics ahead of opaque handle type diagnostics.
	if err := validateMIRNativeBuiltinSupport(mir, format, target); err != nil {
		return err
	}
	checkType := func(encoded string, generics map[string]struct{}) error {
		feature, skip := kirTypeCapabilityNameInArena(encoded, generics, arena)
		if skip {
			return nil
		}
		if feature == "" {
			return fmt.Errorf("type kind %q is not listed as supported by the %s backend for %s-%s", encoded, format, target.OS, target.Arch)
		}
		return validateLanguageItem("type", feature, format, target)
	}
	var walkExpression func(MIRRef, map[string]struct{}, bool) error
	var walkStatements func(MIRNodeRefList, map[string]struct{}) error
	var walkFunction func(MIRRef) error
	walkExpression = func(ref MIRRef, generics map[string]struct{}, allowOutput bool) error {
		if !ref.Present {
			return nil
		}
		node := arena.Expressions[ref.Index]
		expression := node.Value
		if capability := kirExpressionCapabilityName(expression.Kind); capability == "" {
			return fmt.Errorf("expression kind %q is not listed as supported by the %s backend for %s-%s", expression.Kind, format, target.OS, target.Arch)
		} else if err := validateLanguageItem("expression", capability, format, target); err != nil {
			return err
		}
		if expression.Kind == "unary" || expression.Kind == "binary" {
			category := "binary_operator"
			if expression.Kind == "unary" {
				category = "unary_operator"
			}
			if capability := kirOperatorCapabilityName(expression.Operator); capability == "" {
				return fmt.Errorf("operator %q is not listed as supported by the %s backend for %s-%s", expression.Operator, format, target.OS, target.Arch)
			} else if err := validateLanguageItem(category, capability, format, target); err != nil {
				return err
			}
		}
		if err := checkType(expression.Type, generics); err != nil {
			return err
		}
		if expression.Kind == "call" && strings.HasPrefix(expression.CallTarget, "builtin:") {
			name := strings.TrimPrefix(expression.CallTarget, "builtin:")
			unsupported := nativeBuiltinBackendStatus(name, format, target) == "unsupported"
			if format == "elf-direct" && (name == "print" || name == "println") && (!allowOutput || node.Args.Count != 1) {
				unsupported = true
			}
			if unsupported {
				return fmt.Errorf("builtin %q is not listed as supported by the %s backend for %s-%s; use the interpreter for this feature", name, format, target.OS, target.Arch)
			}
		}
		for _, child := range []MIRRef{node.Left, node.Right, node.Operand, node.Base, node.Receiver, node.Callee} {
			if err := walkExpression(child, generics, false); err != nil {
				return err
			}
		}
		for _, list := range []MIRNodeRefList{node.Args, node.Items, node.MapKeys, node.Values} {
			indexes, _ := arena.indexList(arena.ExpressionRefs, list)
			for _, index := range indexes {
				if err := walkExpression(MIRRef{Index: index, Present: true}, generics, false); err != nil {
					return err
				}
			}
		}
		return walkFunction(node.Lambda)
	}
	walkStatements = func(list MIRNodeRefList, generics map[string]struct{}) error {
		indexes, _ := arena.indexList(arena.StatementRefs, list)
		for _, index := range indexes {
			node := arena.Statements[index]
			statement := node.Value
			if capability := kirStatementCapabilityName(statement.Kind); capability == "" {
				return fmt.Errorf("statement kind %q is not listed as supported by the %s backend for %s-%s", statement.Kind, format, target.OS, target.Arch)
			} else if err := validateLanguageItem("statement", capability, format, target); err != nil {
				return err
			}
			candidates := []struct {
				ref         MIRRef
				allowOutput bool
				nilReturn   bool
			}{
				{ref: node.Init},
				{ref: node.Expr, allowOutput: format == "elf-direct" && statement.Kind == "expr"},
				{ref: node.Target}, {ref: node.ValueExpr}, {ref: node.Cond}, {ref: node.Iter},
				{ref: node.Return, nilReturn: statement.Kind == "return"}, {ref: node.Scrutinee},
			}
			for _, candidate := range candidates {
				if candidate.nilReturn && candidate.ref.Present && arena.Expressions[candidate.ref.Index].Value.Kind == "nil" {
					continue
				}
				if err := walkExpression(candidate.ref, generics, candidate.allowOutput); err != nil {
					return err
				}
			}
			for _, block := range []MIRNodeRefList{node.Then, node.Else, node.Body} {
				if err := walkStatements(block, generics); err != nil {
					return err
				}
			}
			arms, _ := arena.indexList(arena.ArmRefs, node.Arms)
			for _, armIndex := range arms {
				arm := arena.Arms[armIndex]
				if !arm.Pattern.Present {
					return fmt.Errorf("pattern kind %q is not listed as supported by the %s backend for %s-%s", "<nil>", format, target.OS, target.Arch)
				}
				pattern := arena.Patterns[arm.Pattern.Index].Value
				if capability := kirPatternCapabilityName(pattern.Kind); capability == "" {
					return fmt.Errorf("pattern kind %q is not listed as supported by the %s backend for %s-%s", pattern.Kind, format, target.OS, target.Arch)
				} else if err := validateLanguageItem("pattern", capability, format, target); err != nil {
					return err
				}
				if err := walkStatements(arm.Body, generics); err != nil {
					return err
				}
			}
		}
		return nil
	}
	walkFunction = func(ref MIRRef) error {
		if !ref.Present {
			return nil
		}
		function := arena.Functions[ref.Index]
		generics := kirFunctionTypeParameters(&function.Value)
		if function.Value.Receiver != "" {
			if receiver := kirArenaStructType(arena, function.Value.Receiver); receiver != nil {
				for _, parameter := range receiver.TypeParams {
					if parameter != nil && parameter.Name != "" {
						generics[parameter.Name] = struct{}{}
					}
				}
			}
		}
		parameters, _ := arena.indexList(arena.ParameterRefs, function.Params)
		for _, index := range parameters {
			parameter := arena.Parameters[index]
			if err := checkType(parameter.Value.Type, generics); err != nil {
				return err
			}
			if err := walkExpression(parameter.Default, generics, false); err != nil {
				return err
			}
		}
		if err := checkType(function.Value.Return, generics); err != nil {
			return err
		}
		return walkStatements(function.Body, generics)
	}

	if err := walkStatements(arena.TopStatements, map[string]struct{}{}); err != nil {
		return err
	}
	for _, structure := range arena.Structs {
		if structure == nil {
			continue
		}
		generics := kirTypeParameterNames(structure.TypeParams)
		for _, field := range structure.Fields {
			if field != nil {
				if err := checkType(field.Type, generics); err != nil {
					return err
				}
			}
		}
	}
	functions, _ := arena.indexList(arena.FunctionRefs, arena.TopFunctions)
	for _, index := range functions {
		if err := walkFunction(MIRRef{Index: index, Present: true}); err != nil {
			return err
		}
	}
	return nil
}

func kirTypeCapabilityNameInArena(encoded string, generics map[string]struct{}, arena *KIRArena) (string, bool) {
	feature, skip := kirTypeCapabilityName(encoded, generics)
	if feature != "" || skip || arena == nil {
		return feature, skip
	}
	spec, ok := parseKIRTypeExpression(encoded)
	if !ok || spec == nil {
		return "", false
	}
	for _, structure := range arena.Structs {
		if structure != nil && structure.Name == spec.Name {
			return "TyStruct", false
		}
	}
	for _, enum := range arena.Enums {
		if enum != nil && enum.Name == spec.Name {
			return "TyEnum", false
		}
	}
	return "", false
}

func kirArenaStructType(arena *KIRArena, encoded string) *KIRStruct {
	spec, ok := parseKIRTypeExpression(encoded)
	if !ok || spec == nil {
		return nil
	}
	for _, structure := range arena.Structs {
		if structure != nil && structure.Name == spec.Name {
			return structure
		}
	}
	return nil
}
