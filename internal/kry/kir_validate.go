package kry

import (
	"fmt"
	"math"
	"strings"
)

// validateKIRDocument enforces invariants that a backend may otherwise rely
// on accidentally. DecodeKIR calls it for both the current schema and the
// explicitly supported legacy version.
func validateKIRDocument(document *KIRDocument, limits Limits) error {
	if document == nil {
		return fmt.Errorf("missing document")
	}
	if !validKIRTarget(document.Target) {
		return fmt.Errorf("unsupported target %s-%s", document.Target.OS, document.Target.Arch)
	}
	if err := checkKIRCount("imports", len(document.Imports), limits.MaxImports); err != nil {
		return err
	}
	if err := checkKIRCount("sources", len(document.Sources), limits.MaxImports+1); err != nil {
		return err
	}
	if err := checkKIRCount("structs", len(document.Structs), limits.MaxASTNodes); err != nil {
		return err
	}
	if err := checkKIRCount("enums", len(document.Enums), limits.MaxASTNodes); err != nil {
		return err
	}
	if err := checkKIRCount("traits", len(document.Traits), limits.MaxASTNodes); err != nil {
		return err
	}
	if err := checkKIRCount("trait implementations", len(document.TraitImpls), limits.MaxASTNodes); err != nil {
		return err
	}
	if err := checkKIRCount("functions", len(document.Functions), limits.MaxASTNodes); err != nil {
		return err
	}
	if err := checkKIRCount("top-level statements", len(document.Statements), limits.MaxASTNodes); err != nil {
		return err
	}
	structs := make(map[string]*KIRStruct, len(document.Structs))
	enums := make(map[string]*KIREnum, len(document.Enums))
	traits := make(map[string]*KIRTrait, len(document.Traits))
	functionCounts := make(map[string]int, len(document.Functions))
	functionTargets := make(map[string]string, len(document.Functions))
	functionDeclarations := make(map[string]*KIRFunction, len(document.Functions))
	functionGenericArguments := make(map[string]int, len(document.Functions))
	if document.Version < 5 && (len(document.Traits) != 0 || len(document.TraitImpls) != 0) {
		return fmt.Errorf("trait declarations and implementations require KIR version 5")
	}
	for _, trait := range document.Traits {
		if trait == nil || trait.Name == "" {
			return fmt.Errorf("trait declaration has no name")
		}
		if _, duplicate := traits[trait.Name]; duplicate {
			return fmt.Errorf("duplicate trait declaration %q", trait.Name)
		}
		if len(trait.Methods) == 0 {
			return fmt.Errorf("trait %q has no methods", trait.Name)
		}
		if err := checkKIRCount("trait methods", len(trait.Methods), limits.MaxArrayElements); err != nil {
			return err
		}
		methods := map[string]bool{}
		for _, method := range trait.Methods {
			if method == nil || method.Name == "" || method.Return == "" || methods[method.Name] {
				return fmt.Errorf("trait %q has an incomplete or duplicate method", trait.Name)
			}
			methods[method.Name] = true
			if !validKIRTypeExpression(method.Return) || !validKIRTypeVariables(method.Return, map[string]bool{}) {
				return fmt.Errorf("trait %q method %q has an invalid return type", trait.Name, method.Name)
			}
			if err := checkKIRCount("trait method parameters", len(method.Params), limits.MaxArrayElements); err != nil {
				return err
			}
			parameterNames := map[string]bool{}
			for _, parameter := range method.Params {
				if parameter == nil || parameter.Name == "" || parameter.Type == "" || parameterNames[parameter.Name] || parameter.Default != nil || parameter.Binding != nil {
					return fmt.Errorf("trait %q method %q has an invalid parameter", trait.Name, method.Name)
				}
				if !validKIRTypeExpression(parameter.Type) || !validKIRTypeVariables(parameter.Type, map[string]bool{}) {
					return fmt.Errorf("trait %q method %q has an invalid parameter type", trait.Name, method.Name)
				}
				parameterNames[parameter.Name] = true
			}
		}
		traits[trait.Name] = trait
	}
	for _, decl := range document.Structs {
		if decl == nil || decl.Name == "" {
			return fmt.Errorf("struct declaration has no name")
		}
		if _, duplicate := structs[decl.Name]; duplicate {
			return fmt.Errorf("duplicate struct declaration %q", decl.Name)
		}
		if err := checkKIRCount("struct fields", len(decl.Fields), limits.MaxArrayElements); err != nil {
			return err
		}
		if err := checkKIRCount("struct type parameters", len(decl.TypeParams), limits.MaxArrayElements); err != nil {
			return err
		}
		if document.Version < 4 && len(decl.TypeParams) != 0 {
			return fmt.Errorf("generic struct declarations require KIR version 4")
		}
		parameters := map[string]bool{}
		for _, parameter := range decl.TypeParams {
			if parameter == nil || parameter.Name == "" || parameters[parameter.Name] {
				return fmt.Errorf("struct %q has an empty or duplicate type parameter", decl.Name)
			}
			if !validKIRTypeConstraint(parameter.Constraint, document.Version, traits) {
				return fmt.Errorf("struct %q has unknown constraint %q", decl.Name, parameter.Constraint)
			}
			parameters[parameter.Name] = true
		}
		fields := map[string]bool{}
		for _, field := range decl.Fields {
			if field == nil || field.Name == "" || field.Type == "" {
				return fmt.Errorf("struct %q has an incomplete field", decl.Name)
			}
			if fields[field.Name] {
				return fmt.Errorf("struct %q has duplicate field %q", decl.Name, field.Name)
			}
			fields[field.Name] = true
			if document.Version >= 4 && !validKIRTypeVariables(field.Type, parameters) {
				return fmt.Errorf("struct %q field %q has an invalid type expression", decl.Name, field.Name)
			}
		}
		structs[decl.Name] = decl
	}
	for _, decl := range document.Enums {
		if decl == nil || decl.Name == "" {
			return fmt.Errorf("enum declaration has no name")
		}
		if _, duplicate := enums[decl.Name]; duplicate {
			return fmt.Errorf("duplicate enum declaration %q", decl.Name)
		}
		if len(decl.Variants) == 0 {
			return fmt.Errorf("enum %q has no variants", decl.Name)
		}
		if err := checkKIRCount("enum variants", len(decl.Variants), limits.MaxArrayElements); err != nil {
			return err
		}
		variants := map[string]bool{}
		for _, variant := range decl.Variants {
			if variant == "" || variants[variant] {
				return fmt.Errorf("enum %q has an empty or duplicate variant", decl.Name)
			}
			variants[variant] = true
		}
		enums[decl.Name] = decl
	}
	for name := range traits {
		if structs[name] != nil || enums[name] != nil {
			return fmt.Errorf("trait %q conflicts with a type declaration", name)
		}
	}
	for _, function := range document.Functions {
		if function == nil || function.Name == "" || function.Return == "" {
			return fmt.Errorf("function declaration is incomplete")
		}
		if document.Version >= 5 && (function.Source == "" || function.Line < 1 || function.Column < 1) {
			return fmt.Errorf("function %q has invalid source location metadata", function.Name)
		}
		if document.Version < 5 && (function.Source != "" || function.Line != 0 || function.Column != 0 || function.Trait != "") {
			return fmt.Errorf("function source locations and trait implementation metadata require KIR version 5")
		}
		if function.Trait != "" && (traits[function.Trait] == nil || function.Receiver == "") {
			return fmt.Errorf("function %q has an invalid trait implementation marker", function.Name)
		}
		if len(function.Captures) != 0 {
			return fmt.Errorf("top-level function declarations cannot have closure captures")
		}
		if document.Version < 3 && len(function.Captures) != 0 {
			return fmt.Errorf("function captures require KIR version 3")
		}
		functionCounts[function.Name]++
		if err := checkKIRCount("function parameters", len(function.Params), limits.MaxArrayElements); err != nil {
			return err
		}
		if err := checkKIRCount("function type parameters", len(function.TypeParams), limits.MaxArrayElements); err != nil {
			return err
		}
		seenTypeParams := map[string]bool{}
		for _, parameter := range function.TypeParams {
			if parameter == nil || parameter.Name == "" || seenTypeParams[parameter.Name] {
				return fmt.Errorf("function %q has an empty or duplicate type parameter", function.Name)
			}
			if !validKIRTypeConstraint(parameter.Constraint, document.Version, traits) {
				return fmt.Errorf("function %q has unknown constraint %q", function.Name, parameter.Constraint)
			}
			seenTypeParams[parameter.Name] = true
		}
		for _, parameter := range function.Params {
			if parameter == nil || parameter.Name == "" || parameter.Type == "" {
				return fmt.Errorf("function %q has an incomplete parameter", function.Name)
			}
			if document.Version < 3 && parameter.Binding != nil {
				return fmt.Errorf("resolved parameter bindings require KIR version 3")
			}
			if document.Version >= 3 && (!validKIRBinding(parameter.Binding) || parameter.Binding.Name != parameter.Name || parameter.Binding.Type != parameter.Type || parameter.Binding.Mutable) {
				return fmt.Errorf("function %q has an invalid resolved parameter binding", function.Name)
			}
		}
		if err := checkKIRCount("function body statements", len(function.Body), limits.MaxArrayElements); err != nil {
			return err
		}
	}
	for _, function := range document.Functions {
		target := function.Name
		if functionCounts[function.Name] > 1 {
			target = kirFunctionTargetFromDocument(function)
		}
		if _, duplicate := functionTargets[target]; duplicate {
			return fmt.Errorf("duplicate function target %q", target)
		}
		functionTargets[target] = function.Name
		functionDeclarations[target] = function
		functionGenericArguments[target] = len(function.TypeParams)
	}
	traitImplTargets := map[string]bool{}
	traitImplementations := map[string]bool{}
	for _, implementation := range document.TraitImpls {
		if implementation == nil || implementation.Trait == "" || implementation.For == "" {
			return fmt.Errorf("trait implementation is incomplete")
		}
		trait := traits[implementation.Trait]
		if trait == nil {
			return fmt.Errorf("trait implementation references unknown trait %q", implementation.Trait)
		}
		if !validKIRTraitTargetType(implementation.For, structs, enums) {
			return fmt.Errorf("trait %q implementation has an invalid or non-concrete target type", implementation.Trait)
		}
		implementationKey := implementation.Trait + " for " + implementation.For
		if traitImplementations[implementationKey] {
			return fmt.Errorf("duplicate implementation of trait %q for %s", implementation.Trait, implementation.For)
		}
		traitImplementations[implementationKey] = true
		if len(implementation.Methods) != len(trait.Methods) {
			return fmt.Errorf("trait %q implementation for %s has missing or extra methods", implementation.Trait, implementation.For)
		}
		if err := checkKIRCount("trait implementation methods", len(implementation.Methods), limits.MaxArrayElements); err != nil {
			return err
		}
		methods := map[string]bool{}
		for _, method := range implementation.Methods {
			if method == nil || method.Name == "" || methods[method.Name] {
				return fmt.Errorf("trait %q implementation has an empty or duplicate method", implementation.Trait)
			}
			methods[method.Name] = true
			traitMethod := kirTraitMethod(trait, method.Name)
			if traitMethod == nil {
				return fmt.Errorf("trait %q does not declare implementation method %q", implementation.Trait, method.Name)
			}
			prefix, target, ok := strings.Cut(method.Target, ":")
			if !ok || prefix != "function" || target == "" {
				return fmt.Errorf("trait %q method %q has an invalid function target", implementation.Trait, method.Name)
			}
			functionName, exists := functionTargets[target]
			if !exists || functionName != method.Name {
				return fmt.Errorf("trait %q method %q references an undeclared function target", implementation.Trait, method.Name)
			}
			var function *KIRFunction
			for _, candidate := range document.Functions {
				candidateTarget := candidate.Name
				if functionCounts[candidate.Name] > 1 {
					candidateTarget = kirFunctionTargetFromDocument(candidate)
				}
				if candidateTarget == target {
					function = candidate
					break
				}
			}
			if function == nil || function.Trait != implementation.Trait || function.Receiver != implementation.For {
				return fmt.Errorf("trait %q method %q targets a function with mismatched impl metadata", implementation.Trait, method.Name)
			}
			if !kirTraitFunctionMatches(traitMethod, function) {
				return fmt.Errorf("trait %q method %q does not match its declared signature", implementation.Trait, method.Name)
			}
			traitImplTargets[target] = true
		}
		for _, declared := range trait.Methods {
			if !methods[declared.Name] {
				return fmt.Errorf("trait %q implementation is missing method %q", implementation.Trait, declared.Name)
			}
		}
	}
	for _, function := range document.Functions {
		if function.Trait == "" {
			continue
		}
		target := function.Name
		if functionCounts[function.Name] > 1 {
			target = kirFunctionTargetFromDocument(function)
		}
		if !traitImplTargets[target] {
			return fmt.Errorf("trait implementation function %q is not referenced by an impl", function.Name)
		}
	}
	// Declarations count toward the same document-wide node budget as the
	// recursive expression, statement, pattern, and constant trees below.
	nodeCount := len(document.Structs) + len(document.Enums) + len(document.Traits) + len(document.TraitImpls) + len(document.Functions) + len(document.Statements)
	for _, trait := range document.Traits {
		if trait != nil {
			nodeCount += len(trait.Methods)
			for _, method := range trait.Methods {
				if method != nil {
					nodeCount += len(method.Params)
				}
			}
		}
	}
	for _, implementation := range document.TraitImpls {
		if implementation != nil {
			nodeCount += len(implementation.Methods)
		}
	}
	if limits.MaxASTNodes > 0 && nodeCount > limits.MaxASTNodes {
		return fmt.Errorf("node count exceeds configured limit")
	}
	if limits.MaxInstructions > 0 && uint64(nodeCount) > limits.MaxInstructions {
		return fmt.Errorf("node count exceeds configured instruction limit")
	}
	addNode := func(kind string, depth int) error {
		nodeCount++
		if limits.MaxASTNodes > 0 && nodeCount > limits.MaxASTNodes {
			return fmt.Errorf("node count exceeds configured limit")
		}
		if limits.MaxInstructions > 0 && uint64(nodeCount) > limits.MaxInstructions {
			return fmt.Errorf("node count exceeds configured instruction limit")
		}
		if limits.MaxNesting > 0 && depth > limits.MaxNesting {
			return fmt.Errorf("tree depth exceeds configured nesting limit")
		}
		return nil
	}
	genericConstraints := map[string]string(nil)
	expectedReturnType := ""
	loopDepth := 0
	var validateExpr func(*KIRExpr, int) error
	var validateStmt func(*KIRStmt, int) error
	var validatePattern func(*KIRPattern, int) error
	var validateValue func(*KIRValue, int) error
	validateValue = func(value *KIRValue, depth int) error {
		if value == nil {
			return nil
		}
		if err := addNode("constant", depth); err != nil {
			return err
		}
		switch value.Kind {
		case "nil", "int", "uint", "float", "bool", "string", "bytes", "array", "struct", "enum", "option", "result", "channel", "thread", "map", "set", "json", "websocket", "actor", "shared", "task_group", "regex", "random", "sqlite", "tcp", "tcp_listener", "udp", "ffi_library", "ffi_symbol", "ffi_buffer", "tail_call":
		default:
			return fmt.Errorf("unknown constant value kind %q", value.Kind)
		}
		if value.Kind == "float" && (math.IsNaN(value.Float) || math.IsInf(value.Float, 0)) {
			return fmt.Errorf("non-finite Float constant")
		}
		if value.Kind == "uint" && value.UIntBits != 8 && value.UIntBits != 16 && value.UIntBits != 32 && value.UIntBits != 64 {
			return fmt.Errorf("UInt constant has invalid bit width %d", value.UIntBits)
		}
		if err := checkKIRCount("constant array elements", len(value.Array), limits.MaxArrayElements); err != nil {
			return err
		}
		for _, item := range value.Array {
			if item == nil {
				return fmt.Errorf("constant array contains a missing value")
			}
			if err := validateValue(item, depth+1); err != nil {
				return err
			}
		}
		return validateValue(value.Inner, depth+1)
	}
	validateExpr = func(expression *KIRExpr, depth int) error {
		if expression == nil {
			return nil
		}
		if err := addNode("expression", depth); err != nil {
			return err
		}
		if expression.Type == "" {
			return fmt.Errorf("%s expression %q at %s:%d has no checked type", expression.Kind, expression.Name, expression.Source, expression.Line)
		}
		if document.Version < 5 && expression.TraitName != "" {
			return fmt.Errorf("trait method calls require KIR version 5")
		}
		if expression.Kind != "call" && expression.TraitName != "" {
			return fmt.Errorf("non-call expression has a trait method target")
		}
		if len(expression.Type) > limits.MaxSourceBytes && limits.MaxSourceBytes > 0 {
			return fmt.Errorf("expression type exceeds configured string limit")
		}
		if document.Version < 3 && (expression.Binding != nil || expression.Callee != nil || expression.Lambda != nil) {
			return fmt.Errorf("function values and resolved expression bindings require KIR version 3")
		}
		if document.Version < 4 && (expression.StructType != "" || len(expression.GenericArguments) != 0) {
			return fmt.Errorf("generic type call metadata requires KIR version 4")
		}
		if document.Version >= 4 {
			if len(expression.GenericArguments) != 0 && (expression.Kind != "call" || expression.Callee != nil || !strings.HasPrefix(expression.CallTarget, "function:")) {
				return fmt.Errorf("generic type arguments require a direct function call")
			}
			for _, argument := range expression.GenericArguments {
				if !validKIRTypeExpression(argument) {
					return fmt.Errorf("expression has an invalid generic type argument")
				}
			}
			if expression.Kind == "call" && strings.HasPrefix(expression.CallTarget, "function:") {
				target := strings.TrimPrefix(expression.CallTarget, "function:")
				if arity, exists := functionGenericArguments[target]; exists && len(expression.GenericArguments) != arity {
					return fmt.Errorf("function call %q has %d generic type argument(s), expected %d", target, len(expression.GenericArguments), arity)
				}
			}
		}
		if expression.Kind != "var" && expression.Binding != nil {
			return fmt.Errorf("non-variable expression has a resolved binding")
		}
		if strings.HasPrefix(expression.Type, "fn(") {
			if _, _, ok := parseKIRFunctionType(expression.Type); !ok {
				return fmt.Errorf("expression has malformed function type %q", expression.Type)
			}
		}
		require := func(child *KIRExpr, field string) error {
			if child == nil {
				return fmt.Errorf("%s expression is missing %s", expression.Kind, field)
			}
			return nil
		}
		switch expression.Kind {
		case "int", "float", "bool", "nil", "string":
			want := ""
			switch expression.Kind {
			case "int":
				want = "Int"
			case "float":
				want = "Float"
			case "bool":
				want = "Bool"
			case "nil":
				want = "Nil"
			case "string":
				want = "String"
			}
			if expression.Type != want {
				return fmt.Errorf("%s literal has checked type %q, want %q", expression.Kind, expression.Type, want)
			}
		case "var":
			if expression.Name == "" {
				return fmt.Errorf("variable expression has no name")
			}
			if expression.CallTarget != "" {
				if expression.Binding != nil {
					return fmt.Errorf("function reference cannot also name a local binding")
				}
				if document.Version < 3 || !validKIRCallTarget(expression.CallTarget) || !strings.HasPrefix(expression.Type, "fn(") {
					return fmt.Errorf("function value has an invalid target or function type")
				}
				prefix, target, _ := strings.Cut(expression.CallTarget, ":")
				resolvedName, ok := functionTargets[target]
				if prefix != "function" || !ok || resolvedName != expression.Name {
					return fmt.Errorf("function value references undeclared function or unresolved overload %q", target)
				}
				function := functionDeclarations[target]
				if function != nil && len(function.TypeParams) == 0 && function.Receiver == "" && expression.Type != kirFunctionValueType(function) {
					return fmt.Errorf("function value type %q does not match target signature %q", expression.Type, kirFunctionValueType(function))
				}
			} else if document.Version >= 3 {
				if !validKIRBinding(expression.Binding) || expression.Binding.Name != expression.Name || expression.Binding.Type != expression.Type {
					return fmt.Errorf("variable %q has an invalid resolved binding", expression.Name)
				}
			}
		case "unary":
			if !isKIRUnaryOperator(expression.Operator) {
				return fmt.Errorf("unary expression has invalid operator %q", expression.Operator)
			}
			if err := require(expression.Operand, "operand"); err != nil {
				return err
			}
			switch expression.Operator {
			case "+":
				if !isKIRNumericType(expression.Operand.Type, genericConstraints) || expression.Type != expression.Operand.Type {
					return fmt.Errorf("unary operator + requires a numeric operand and matching result type")
				}
			case "-":
				if expression.Operand.Type != "Int" && expression.Operand.Type != "Float" || expression.Type != expression.Operand.Type {
					return fmt.Errorf("unary operator - requires an Int or Float operand and matching result type")
				}
			case "!":
				if expression.Operand.Type != "Bool" || expression.Type != "Bool" {
					return fmt.Errorf("unary operator ! requires Bool operand and result")
				}
			case "~":
				if !isKIRUIntType(expression.Operand.Type) || expression.Type != expression.Operand.Type {
					return fmt.Errorf("unary operator ~ requires a UInt operand and matching result type")
				}
			}
		case "binary":
			if !isKIRBinaryOperator(expression.Operator) {
				return fmt.Errorf("binary expression has invalid operator %q", expression.Operator)
			}
			if err := require(expression.Left, "left operand"); err != nil {
				return err
			}
			if err := require(expression.Right, "right operand"); err != nil {
				return err
			}
			if expression.Operator == "&&" || expression.Operator == "||" {
				if expression.Left.Type != "Bool" || expression.Right.Type != "Bool" || expression.Type != "Bool" {
					return fmt.Errorf("logical operator %q requires Bool operands and result", expression.Operator)
				}
			}
			switch expression.Operator {
			case "==", "!=":
				if expression.Type != "Bool" {
					return fmt.Errorf("comparison operator %q requires Bool result", expression.Operator)
				}
				if !compatibleKIRTypes(expression.Left.Type, expression.Right.Type) {
					return fmt.Errorf("equality operator %q requires matching operand types", expression.Operator)
				}
				if containsKIRFunctionType(expression.Left.Type) {
					return fmt.Errorf("equality operator %q does not support function values", expression.Operator)
				}
			case "<", "<=", ">", ">=":
				if expression.Type != "Bool" {
					return fmt.Errorf("comparison operator %q requires Bool result", expression.Operator)
				}
				if expression.Left.Type != expression.Right.Type || !isKIRNumericType(expression.Left.Type, genericConstraints) {
					return fmt.Errorf("ordered comparison %q requires matching numeric operands", expression.Operator)
				}
			case "&", "|", "^":
				if !isKIRUIntType(expression.Left.Type) || expression.Right.Type != expression.Left.Type || expression.Type != expression.Left.Type {
					return fmt.Errorf("bitwise operator %q requires matching UInt operands and result", expression.Operator)
				}
			case "<<", ">>":
				if !isKIRUIntType(expression.Left.Type) || expression.Right.Type != "Int" || expression.Type != expression.Left.Type {
					return fmt.Errorf("shift operator %q requires a UInt value, Int count, and matching result", expression.Operator)
				}
			case "+", "-", "*", "/", "%":
				left, right := expression.Left.Type, expression.Right.Type
				if !compatibleKIRTypes(left, right) || expression.Type != left {
					return fmt.Errorf("arithmetic operator %q requires matching operand and result types", expression.Operator)
				}
				switch expression.Operator {
				case "+":
					concatenable := left == "String" || left == "Bytes" || isKIRArrayType(left)
					if !concatenable && !isKIRNumericType(left, genericConstraints) {
						return fmt.Errorf("operator + does not support checked type %q", left)
					}
				case "%":
					if !isKIRIntegerType(left, genericConstraints) {
						return fmt.Errorf("operator %% requires Int or UInt operands")
					}
				default:
					if !isKIRNumericType(left, genericConstraints) {
						return fmt.Errorf("operator %q requires numeric operands", expression.Operator)
					}
				}
			}
		case "call":
			if expression.Callee != nil {
				if document.Version < 3 || expression.Name != "" || expression.CallTarget != "" || expression.BuiltinID != "" || expression.TraitName != "" {
					return fmt.Errorf("indirect call has an invalid name or target")
				}
				params, result, ok := parseKIRFunctionType(expression.Callee.Type)
				if !ok {
					return fmt.Errorf("indirect call callee does not have a function type")
				}
				if len(params) != len(expression.Args) {
					return fmt.Errorf("indirect call has %d arguments for a %d-parameter function", len(expression.Args), len(params))
				}
				if expression.Type != result {
					return fmt.Errorf("indirect call result type does not match its function type")
				}
				for i, arg := range expression.Args {
					if arg == nil || arg.Type != params[i] {
						return fmt.Errorf("indirect call argument %d has a mismatched type", i+1)
					}
				}
			} else {
				if expression.Name == "" || !validKIRCallTarget(expression.CallTarget) {
					return fmt.Errorf("call expression has an invalid name or target")
				}
				prefix, name, _ := strings.Cut(expression.CallTarget, ":")
				if prefix == "builtin" {
					if name != expression.Name || expression.TraitName != "" {
						return fmt.Errorf("call expression name does not match its target")
					}
					builtin, ok := lookupBuiltin(name)
					if !ok {
						return fmt.Errorf("call references unknown builtin %q", name)
					}
					if len(expression.Args) != builtin.Arity {
						return fmt.Errorf("call to builtin %q has %d arguments, want %d", name, len(expression.Args), builtin.Arity)
					}
					if (expression.BuiltinID != "" && expression.BuiltinID != builtin.ID) || (document.Version >= 5 && expression.BuiltinID == "") {
						return fmt.Errorf("call to builtin %q has a mismatched builtin id", name)
					}
				} else if prefix == "trait" {
					traitName, methodName, ok := strings.Cut(name, "::")
					trait := traits[traitName]
					if document.Version < 5 || !ok || trait == nil || methodName != expression.Name || expression.TraitName != traitName || expression.Receiver == nil {
						return fmt.Errorf("call references an invalid trait method target")
					}
					var signature *KIRTraitMethod
					for _, method := range trait.Methods {
						if method.Name == methodName {
							signature = method
							break
						}
					}
					if signature == nil || len(signature.Params) != len(expression.Args) || expression.Type != signature.Return {
						return fmt.Errorf("call to trait method %q does not match its signature", methodName)
					}
					for i, parameter := range signature.Params {
						if expression.Args[i] == nil || expression.Args[i].Type != parameter.Type {
							return fmt.Errorf("call to trait method %q has a mismatched argument", methodName)
						}
					}
				} else {
					if expression.TraitName != "" {
						return fmt.Errorf("function call has unexpected trait metadata")
					}
					resolvedName, ok := functionTargets[name]
					if !ok {
						return fmt.Errorf("call references undeclared function or unresolved overload %q", name)
					}
					if resolvedName != expression.Name {
						return fmt.Errorf("call expression name does not match its target")
					}
					function := functionDeclarations[name]
					if function == nil {
						return fmt.Errorf("call target %q has no function declaration", name)
					}
					requiredArity := len(function.Params)
					for index, parameter := range function.Params {
						if parameter.Default != nil {
							requiredArity = index
							break
						}
					}
					if len(expression.Args) < requiredArity || len(expression.Args) > len(function.Params) {
						return fmt.Errorf("call to function %q has %d arguments, expected %d to %d", name, len(expression.Args), requiredArity, len(function.Params))
					}
					if len(function.TypeParams) == 0 && function.Receiver == "" {
						if !compatibleKIRTypes(expression.Type, function.Return) {
							return fmt.Errorf("call to function %q has result type %q, want %q", name, expression.Type, function.Return)
						}
						for index, argument := range expression.Args {
							if argument == nil || !compatibleKIRTypes(argument.Type, function.Params[index].Type) {
								return fmt.Errorf("call to function %q argument %d has a mismatched type", name, index+1)
							}
						}
					}
				}
			}
		case "lambda":
			if document.Version < 3 || expression.Lambda == nil || expression.Callee != nil {
				return fmt.Errorf("lambda expression is missing its versioned function body")
			}
			lambda := expression.Lambda
			if lambda.Name == "" || lambda.Return == "" || len(lambda.TypeParams) != 0 || lambda.Receiver != "" || lambda.Public || lambda.Worker || lambda.Trait != "" {
				return fmt.Errorf("lambda function metadata is invalid")
			}
			if document.Version >= 5 && (lambda.Source == "" || lambda.Line < 1 || lambda.Column < 1) {
				return fmt.Errorf("lambda has invalid source location metadata")
			}
			if document.Version < 5 && (lambda.Source != "" || lambda.Line != 0 || lambda.Column != 0) {
				return fmt.Errorf("lambda source locations require KIR version 5")
			}
			if err := checkKIRCount("lambda parameters", len(lambda.Params), limits.MaxArrayElements); err != nil {
				return err
			}
			parts := make([]string, len(lambda.Params))
			parameterNames := map[string]bool{}
			for i, parameter := range lambda.Params {
				if parameter == nil || parameter.Name == "" || parameter.Type == "" || parameter.Default != nil || parameterNames[parameter.Name] {
					return fmt.Errorf("lambda has an invalid or duplicate parameter")
				}
				if !validKIRBinding(parameter.Binding) || parameter.Binding.Name != parameter.Name || parameter.Binding.Type != parameter.Type || parameter.Binding.Mutable {
					return fmt.Errorf("lambda has an invalid resolved parameter binding")
				}
				parameterNames[parameter.Name] = true
				parts[i] = parameter.Type
			}
			if expression.Type != "fn("+strings.Join(parts, ", ")+") -> "+lambda.Return {
				return fmt.Errorf("lambda expression type does not match its signature")
			}
			if _, _, ok := parseKIRFunctionType(expression.Type); !ok {
				return fmt.Errorf("lambda has a malformed function type")
			}
			if err := checkKIRCount("lambda captures", len(lambda.Captures), limits.MaxArrayElements); err != nil {
				return err
			}
			captures := map[string]bool{}
			for _, capture := range lambda.Captures {
				if err := addNode("capture", depth+1); err != nil {
					return err
				}
				if capture == nil || !validKIRBinding(capture.Binding) {
					return fmt.Errorf("lambda has an incomplete capture")
				}
				identity := kirBindingIdentity(capture.Binding)
				if captures[identity] {
					return fmt.Errorf("lambda has a duplicate capture %q", capture.Binding.Name)
				}
				captures[identity] = true
			}
			if err := checkKIRCount("lambda body statements", len(lambda.Body), limits.MaxArrayElements); err != nil {
				return err
			}
			previousReturnType := expectedReturnType
			previousLoopDepth := loopDepth
			expectedReturnType = lambda.Return
			loopDepth = 0
			for _, statement := range lambda.Body {
				if err := validateStmt(statement, depth+1); err != nil {
					return fmt.Errorf("lambda body: %w", err)
				}
			}
			expectedReturnType = previousReturnType
			loopDepth = previousLoopDepth
			if err := validateKIRLambdaCaptures(lambda); err != nil {
				return err
			}
		case "array", "set":
		case "index":
			if err := require(expression.Base, "base"); err != nil {
				return err
			}
			if err := require(expression.Left, "index"); err != nil {
				return err
			}
		case "field":
			if err := require(expression.Base, "base"); err != nil {
				return err
			}
			if expression.Field == "" {
				return fmt.Errorf("field expression has no field name")
			}
		case "struct":
			decl := structs[expression.StructName]
			if expression.StructName == "" || decl == nil || len(expression.Fields) != len(expression.Values) || len(expression.Fields) != len(decl.Fields) {
				return fmt.Errorf("struct expression has an unknown type or mismatched fields and values")
			}
			instanceType := expression.StructName
			if document.Version >= 4 {
				instanceType = expression.StructType
			}
			if expression.Type != instanceType || !validKIRStructInstanceType(instanceType, decl) {
				return fmt.Errorf("struct expression has an invalid generic instantiation")
			}
			declaredFields := make(map[string]*KIRField, len(decl.Fields))
			for _, field := range decl.Fields {
				declaredFields[field.Name] = field
			}
			seenFields := make(map[string]bool, len(expression.Fields))
			substitutions := map[string]string{}
			if len(decl.TypeParams) > 0 {
				arguments, ok := splitKIRGenericArguments(instanceType, decl.Name)
				if !ok || len(arguments) != len(decl.TypeParams) {
					return fmt.Errorf("struct expression has invalid generic field arguments")
				}
				for index, parameter := range decl.TypeParams {
					substitutions[parameter.Name] = arguments[index]
				}
			}
			for index, field := range expression.Fields {
				declaration := declaredFields[field]
				if declaration == nil || seenFields[field] {
					return fmt.Errorf("struct expression references an unknown or duplicate field %q", field)
				}
				seenFields[field] = true
				expectedType := substituteKIRType(declaration.Type, substitutions)
				value := expression.Values[index]
				if value == nil {
					return fmt.Errorf("struct field %q has no value", field)
				}
				if !compatibleKIRTypes(expectedType, value.Type) {
					return fmt.Errorf("struct field %q has checked type %q, want %q", field, value.Type, expectedType)
				}
			}
		case "enum":
			decl := enums[expression.EnumType]
			if expression.Type != expression.EnumType || decl == nil || expression.EnumVariant == "" || !containsKIRString(decl.Variants, expression.EnumVariant) {
				return fmt.Errorf("enum expression references an unknown type or variant")
			}
		case "map":
			if len(expression.MapKeys) != len(expression.Values) {
				return fmt.Errorf("map expression has mismatched keys and values")
			}
		case "propagate":
			if err := require(expression.Operand, "operand"); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown expression kind %q", expression.Kind)
		}
		if err := checkKIRCount("expression list", len(expression.Args), limits.MaxArrayElements); err != nil {
			return err
		}
		if err := checkKIRCount("struct expression fields", len(expression.Fields), limits.MaxArrayElements); err != nil {
			return err
		}
		for _, child := range []*KIRExpr{expression.Left, expression.Right, expression.Operand, expression.Base, expression.Receiver, expression.Callee} {
			if err := validateExpr(child, depth+1); err != nil {
				return err
			}
		}
		for _, list := range [][]*KIRExpr{expression.Args, expression.Items, expression.MapKeys, expression.Values} {
			if err := checkKIRCount("expression list", len(list), limits.MaxArrayElements); err != nil {
				return err
			}
			for _, child := range list {
				if child == nil {
					return fmt.Errorf("expression list contains a missing node")
				}
				if err := validateExpr(child, depth+1); err != nil {
					return err
				}
			}
		}
		if err := validateValue(expression.Const, depth+1); err != nil {
			return err
		}
		return nil
	}
	validatePattern = func(pattern *KIRPattern, depth int) error {
		if pattern == nil {
			return fmt.Errorf("match arm has no pattern")
		}
		if err := addNode("pattern", depth); err != nil {
			return err
		}
		if document.Version < 3 && pattern.ResolvedBinding != nil {
			return fmt.Errorf("resolved pattern bindings require KIR version 3")
		}
		if pattern.Binding == "" {
			if pattern.ResolvedBinding != nil {
				return fmt.Errorf("pattern without a binding has resolved binding metadata")
			}
		} else if document.Version >= 3 && (!validKIRBinding(pattern.ResolvedBinding) || pattern.ResolvedBinding.Name != pattern.Binding || pattern.ResolvedBinding.Mutable) {
			return fmt.Errorf("pattern has an invalid resolved binding")
		}
		switch pattern.Kind {
		case "wildcard", "nil", "bool", "int", "string", "option", "result":
		case "enum":
			decl := enums[pattern.Type]
			if decl == nil || !containsKIRString(decl.Variants, pattern.Variant) {
				return fmt.Errorf("enum pattern references an unknown type or variant")
			}
		default:
			return fmt.Errorf("unknown pattern kind %q", pattern.Kind)
		}
		return nil
	}
	validateStmt = func(statement *KIRStmt, depth int) error {
		if statement == nil {
			return fmt.Errorf("statement list contains a missing node")
		}
		if err := addNode("statement", depth); err != nil {
			return err
		}
		if document.Version < 3 && statement.Binding != nil {
			return fmt.Errorf("resolved statement bindings require KIR version 3")
		}
		if statement.Binding != nil && statement.Kind != "let" && statement.Kind != "const" && statement.Kind != "for" {
			return fmt.Errorf("%s statement cannot have a resolved declaration binding", statement.Kind)
		}
		requireExpr := func(expression *KIRExpr, field string) error {
			if expression == nil {
				return fmt.Errorf("%s statement is missing %s", statement.Kind, field)
			}
			return nil
		}
		switch statement.Kind {
		case "let", "const":
			if statement.Name == "" {
				return fmt.Errorf("%s statement has no binding name", statement.Kind)
			}
			if statement.Const != (statement.Kind == "const") || statement.Kind == "const" && statement.Mutable {
				return fmt.Errorf("%s statement has inconsistent const or mutability flags", statement.Kind)
			}
			if err := requireExpr(statement.Init, "initializer"); err != nil {
				return err
			}
			if document.Version >= 3 && (!validKIRBinding(statement.Binding) || statement.Binding.Name != statement.Name || statement.Binding.Mutable != statement.Mutable) {
				return fmt.Errorf("%s statement has an invalid resolved binding", statement.Kind)
			}
		case "expr":
			if err := requireExpr(statement.Expr, "expression"); err != nil {
				return err
			}
		case "assign":
			if err := requireExpr(statement.Target, "target"); err != nil {
				return err
			}
			if statement.Target.Kind != "var" || statement.Target.Name == "" {
				return fmt.Errorf("assignment target is not a binding")
			}
			if err := requireExpr(statement.Value, "value"); err != nil {
				return err
			}
			if document.Version >= 3 {
				if !validKIRBinding(statement.Target.Binding) || statement.Target.Binding.Name != statement.Target.Name {
					return fmt.Errorf("assignment target has an invalid resolved binding")
				}
				if !statement.Target.Binding.Mutable {
					return fmt.Errorf("assignment target %q is immutable", statement.Target.Name)
				}
				if !compatibleKIRTypes(statement.Target.Type, statement.Value.Type) {
					return fmt.Errorf("assignment value type %q does not match target type %q", statement.Value.Type, statement.Target.Type)
				}
			}
		case "if", "while":
			if err := requireExpr(statement.Cond, "condition"); err != nil {
				return err
			}
			if statement.Cond.Type != "Bool" {
				return fmt.Errorf("%s condition has checked type %q, want %q", statement.Kind, statement.Cond.Type, "Bool")
			}
		case "for":
			if statement.Name == "" {
				return fmt.Errorf("for statement has no binding name")
			}
			if statement.Mutable || statement.Const {
				return fmt.Errorf("for statement binding cannot be mutable or const")
			}
			if err := requireExpr(statement.Iter, "iterator"); err != nil {
				return err
			}
			if document.Version >= 3 && (!validKIRBinding(statement.Binding) || statement.Binding.Name != statement.Name || statement.Binding.Type == "") {
				return fmt.Errorf("for statement has an invalid resolved binding")
			}
			if document.Version >= 3 && statement.Binding.Mutable {
				return fmt.Errorf("for statement binding must be immutable")
			}
			if document.Version >= 3 {
				elementType, ok := kirIterableElementType(statement.Iter.Type)
				if !ok {
					return fmt.Errorf("for statement iterator has non-iterable checked type %q", statement.Iter.Type)
				}
				if elementType != "" && !compatibleKIRTypes(statement.Binding.Type, elementType) {
					return fmt.Errorf("for statement binding type %q does not match iterator element type %q", statement.Binding.Type, elementType)
				}
			}
		case "return":
			if expectedReturnType == "" {
				return fmt.Errorf("return statement appears outside a function")
			}
			if !kirReturnTypeCompatible(expectedReturnType, statement.Return) {
				returnType := "Nil"
				if statement.Return != nil {
					returnType = statement.Return.Type
				}
				return fmt.Errorf("return value has checked type %q, function expects %q", returnType, expectedReturnType)
			}
		case "break", "continue":
			if loopDepth == 0 {
				return fmt.Errorf("%s statement appears outside a loop", statement.Kind)
			}
		case "defer", "unsafe":
		case "match":
			if err := requireExpr(statement.Scrutinee, "scrutinee"); err != nil {
				return err
			}
			if len(statement.Arms) == 0 {
				return fmt.Errorf("match statement has no arms")
			}
		default:
			return fmt.Errorf("unknown statement kind %q", statement.Kind)
		}
		for _, expression := range []*KIRExpr{statement.Init, statement.Expr, statement.Target, statement.Value, statement.Cond, statement.Iter, statement.Return, statement.Scrutinee} {
			if err := validateExpr(expression, depth+1); err != nil {
				return err
			}
		}
		if document.Version >= 3 && (statement.Kind == "let" || statement.Kind == "const") && !compatibleKIRTypes(statement.Binding.Type, statement.Init.Type) {
			return fmt.Errorf("%s statement binding type does not match its initializer", statement.Kind)
		}
		for _, list := range [][]*KIRStmt{statement.Then, statement.Else} {
			if err := checkKIRCount("statement block", len(list), limits.MaxArrayElements); err != nil {
				return err
			}
			for _, child := range list {
				if err := validateStmt(child, depth+1); err != nil {
					return err
				}
			}
		}
		if err := checkKIRCount("statement block", len(statement.Body), limits.MaxArrayElements); err != nil {
			return err
		}
		previousLoopDepth := loopDepth
		if statement.Kind == "while" || statement.Kind == "for" {
			loopDepth++
		}
		for _, child := range statement.Body {
			if err := validateStmt(child, depth+1); err != nil {
				return err
			}
		}
		loopDepth = previousLoopDepth
		if err := checkKIRCount("match arms", len(statement.Arms), limits.MaxArrayElements); err != nil {
			return err
		}
		for _, arm := range statement.Arms {
			if arm == nil {
				return fmt.Errorf("match statement contains a missing arm")
			}
			if err := validatePattern(arm.Pattern, depth+1); err != nil {
				return err
			}
			if err := validateKIRPatternType(arm.Pattern, statement.Scrutinee.Type, enums, document.Version); err != nil {
				return err
			}
			if err := checkKIRCount("match arm statements", len(arm.Body), limits.MaxArrayElements); err != nil {
				return err
			}
			for _, child := range arm.Body {
				if err := validateStmt(child, depth+1); err != nil {
					return err
				}
			}
		}
		if statement.Kind == "match" && !kirMatchIsExhaustive(statement.Arms, statement.Scrutinee.Type, enums) {
			return fmt.Errorf("non-exhaustive match for checked type %q", statement.Scrutinee.Type)
		}
		return nil
	}
	for _, function := range document.Functions {
		previousGenericConstraints := genericConstraints
		previousReturnType := expectedReturnType
		previousLoopDepth := loopDepth
		genericConstraints = make(map[string]string, len(function.TypeParams))
		expectedReturnType = function.Return
		loopDepth = 0
		for _, parameter := range function.TypeParams {
			genericConstraints[parameter.Name] = parameter.Constraint
		}
		for _, parameter := range function.Params {
			if err := validateExpr(parameter.Default, 1); err != nil {
				return fmt.Errorf("function %q default: %w", function.Name, err)
			}
			if parameter.Default != nil && !compatibleKIRTypes(parameter.Type, parameter.Default.Type) {
				return fmt.Errorf("function %q parameter %q default type does not match its declaration", function.Name, parameter.Name)
			}
		}
		for _, statement := range function.Body {
			if err := validateStmt(statement, 1); err != nil {
				return fmt.Errorf("function %q: %w", function.Name, err)
			}
		}
		genericConstraints = previousGenericConstraints
		expectedReturnType = previousReturnType
		loopDepth = previousLoopDepth
	}
	if err := checkKIRCount("top-level statements", len(document.Statements), limits.MaxArrayElements); err != nil {
		return err
	}
	for _, statement := range document.Statements {
		if err := validateStmt(statement, 1); err != nil {
			return err
		}
	}
	return nil
}

func kirFunctionValueType(function *KIRFunction) string {
	if function == nil {
		return ""
	}
	parameters := make([]string, len(function.Params))
	for index, parameter := range function.Params {
		if parameter == nil {
			return ""
		}
		parameters[index] = parameter.Type
	}
	return "fn(" + strings.Join(parameters, ", ") + ") -> " + function.Return
}

func compatibleKIRTypes(binding, initializer string) bool {
	if binding == initializer {
		return true
	}
	return isKIRUnspecifiedArray(binding) && isKIRArrayType(initializer) ||
		isKIRUnspecifiedArray(initializer) && isKIRArrayType(binding)
}

func isKIRUIntType(encoded string) bool {
	switch encoded {
	case "UInt8", "UInt16", "UInt32", "UInt64":
		return true
	default:
		return false
	}
}

func isKIRNumericType(encoded string, genericConstraints map[string]string) bool {
	if encoded == "Int" || encoded == "Float" || isKIRUIntType(encoded) {
		return true
	}
	constraint, generic := genericConstraints[encoded]
	return generic && (constraint == "Numeric" || constraint == "Integer")
}

func validateKIRPatternType(pattern *KIRPattern, scrutineeType string, enums map[string]*KIREnum, version int) error {
	if pattern == nil {
		return fmt.Errorf("match arm has no pattern")
	}
	bindingType := ""
	valid := false
	switch pattern.Kind {
	case "wildcard":
		valid = true
	case "nil":
		valid = scrutineeType == "Nil"
		if spec, ok := parseKIRTypeExpression(scrutineeType); ok {
			valid = valid || spec.Name == "Option" && len(spec.Params) == 1
		}
	case "bool":
		valid = scrutineeType == "Bool"
	case "int":
		valid = scrutineeType == "Int"
	case "string":
		valid = scrutineeType == "String"
	case "enum":
		valid = pattern.Type == scrutineeType && enums[pattern.Type] != nil
	case "option":
		if spec, ok := parseKIRTypeExpression(scrutineeType); ok && spec.Name == "Option" && len(spec.Params) == 1 {
			valid = true
			if pattern.Present && pattern.Binding != "" {
				bindingType = TypeSpecString(spec.Params[0])
			}
		}
	case "result":
		if spec, ok := parseKIRTypeExpression(scrutineeType); ok && spec.Name == "Result" && len(spec.Params) == 2 {
			valid = true
			if pattern.Binding != "" {
				index := 1
				if pattern.OK {
					index = 0
				}
				bindingType = TypeSpecString(spec.Params[index])
			}
		}
	}
	if !valid {
		return fmt.Errorf("%s pattern is incompatible with checked scrutinee type %q", pattern.Kind, scrutineeType)
	}
	if pattern.Binding != "" && version >= 3 && (bindingType == "" || pattern.ResolvedBinding == nil || pattern.ResolvedBinding.Type != bindingType) {
		return fmt.Errorf("pattern binding %q does not match its checked payload type", pattern.Binding)
	}
	return nil
}

func kirMatchIsExhaustive(arms []*KIRArm, scrutineeType string, enums map[string]*KIREnum) bool {
	seen := map[string]bool{}
	for _, arm := range arms {
		if arm == nil || arm.Pattern == nil {
			continue
		}
		pattern := arm.Pattern
		if pattern.Kind == "wildcard" {
			return true
		}
		switch pattern.Kind {
		case "bool":
			seen[fmt.Sprint(pattern.Bool)] = true
		case "nil":
			seen["nil"] = true
		case "option":
			seen[fmt.Sprint(pattern.Present)] = true
		case "result":
			seen["ok"] = seen["ok"] || pattern.OK
			seen["err"] = seen["err"] || !pattern.OK
		case "enum":
			if pattern.Type == scrutineeType {
				seen[pattern.Variant] = true
			}
		}
	}
	switch scrutineeType {
	case "Bool":
		return seen["true"] && seen["false"]
	case "Nil":
		return seen["nil"]
	}
	if spec, ok := parseKIRTypeExpression(scrutineeType); ok && len(spec.Params) > 0 {
		switch spec.Name {
		case "Option":
			return len(spec.Params) == 1 && seen["true"] && seen["false"]
		case "Result":
			return len(spec.Params) == 2 && seen["ok"] && seen["err"]
		}
	}
	if declaration := enums[scrutineeType]; declaration != nil {
		for _, variant := range declaration.Variants {
			if !seen[variant] {
				return false
			}
		}
		return len(declaration.Variants) > 0
	}
	return false
}

func kirReturnTypeCompatible(expected string, value *KIRExpr) bool {
	if value == nil {
		return expected == "Nil"
	}
	if value.Kind != "propagate" {
		return compatibleKIRTypes(expected, value.Type)
	}
	if value.Operand == nil {
		return false
	}
	expectedType, expectedOK := parseKIRTypeExpression(expected)
	operandType, operandOK := parseKIRTypeExpression(value.Operand.Type)
	if !expectedOK || !operandOK || expectedType.Name != operandType.Name || len(expectedType.Params) != len(operandType.Params) {
		return false
	}
	switch expectedType.Name {
	case "Option":
		return len(expectedType.Params) == 1 && compatibleKIRTypes(TypeSpecString(expectedType.Params[0]), value.Type) && compatibleKIRTypes(TypeSpecString(expectedType.Params[0]), TypeSpecString(operandType.Params[0]))
	case "Result":
		return len(expectedType.Params) == 2 && compatibleKIRTypes(TypeSpecString(expectedType.Params[0]), value.Type) && compatibleKIRTypes(TypeSpecString(expectedType.Params[1]), TypeSpecString(operandType.Params[1]))
	default:
		return false
	}
}

func isKIRIntegerType(encoded string, genericConstraints map[string]string) bool {
	if encoded == "Int" || isKIRUIntType(encoded) {
		return true
	}
	return genericConstraints[encoded] == "Integer"
}

func containsKIRFunctionType(encoded string) bool {
	spec, ok := parseKIRTypeExpression(encoded)
	if !ok {
		return false
	}
	var visit func(*TypeSpec) bool
	visit = func(current *TypeSpec) bool {
		if current == nil {
			return false
		}
		if current.Function {
			return true
		}
		if visit(current.Return) {
			return true
		}
		for _, parameter := range current.Params {
			if visit(parameter) {
				return true
			}
		}
		return false
	}
	return visit(spec)
}

func kirIterableElementType(encoded string) (string, bool) {
	switch encoded {
	case "String":
		return "String", true
	case "Bytes":
		return "Int", true
	case "Array", "Array[<unknown>]":
		return "", true
	}
	spec, ok := parseKIRTypeExpression(encoded)
	if !ok || spec.Function || len(spec.Params) != 1 || spec.Name != "Array" && spec.Name != "Set" {
		return "", false
	}
	return TypeSpecString(spec.Params[0]), true
}

func isKIRUnspecifiedArray(encoded string) bool {
	return encoded == "Array" || encoded == "Array[<unknown>]"
}

func isKIRArrayType(encoded string) bool {
	if isKIRUnspecifiedArray(encoded) {
		return true
	}
	spec, ok := parseKIRTypeExpression(encoded)
	return ok && !spec.Function && spec.Name == "Array" && len(spec.Params) == 1
}

func checkKIRCount(label string, count, maximum int) error {
	if maximum > 0 && count > maximum {
		return fmt.Errorf("%s count %d exceeds configured limit %d", label, count, maximum)
	}
	return nil
}

func validKIRTarget(target KIRTarget) bool {
	if target.OS == "portable" {
		return target.Arch == "any" && !target.GUI
	}
	switch target.OS {
	case "linux", "windows", "darwin":
	default:
		return false
	}
	return target.Arch == "amd64" || target.Arch == "arm64"
}

func validKIRCallTarget(target string) bool {
	prefix, name, ok := strings.Cut(target, ":")
	if !ok || name == "" {
		return false
	}
	if prefix == "builtin" || prefix == "function" {
		return true
	}
	if prefix == "trait" {
		trait, method, found := strings.Cut(name, "::")
		return found && trait != "" && method != "" && !strings.Contains(method, "::")
	}
	return false
}

func validKIRTypeConstraint(constraint string, version int, traits map[string]*KIRTrait) bool {
	switch constraint {
	case "", "Any", "Copy", "Integer", "Numeric", "Comparable":
		return true
	default:
		return version >= 5 && traits[constraint] != nil
	}
}

func kirTraitMethod(trait *KIRTrait, name string) *KIRTraitMethod {
	if trait == nil {
		return nil
	}
	for _, method := range trait.Methods {
		if method != nil && method.Name == name {
			return method
		}
	}
	return nil
}

func kirTraitFunctionMatches(signature *KIRTraitMethod, function *KIRFunction) bool {
	if signature == nil || function == nil || len(signature.Params) != len(function.Params) || len(function.TypeParams) != 0 {
		return false
	}
	for i, parameter := range signature.Params {
		if parameter == nil || function.Params[i] == nil || parameter.Type != function.Params[i].Type || function.Params[i].Default != nil {
			return false
		}
	}
	return signature.Return == function.Return
}

func validKIRTraitTargetType(encoded string, structs map[string]*KIRStruct, enums map[string]*KIREnum) bool {
	spec, ok := parseKIRTypeExpression(encoded)
	if !ok || spec.Function || structs[spec.Name] == nil || !validKIRStructInstanceType(encoded, structs[spec.Name]) {
		return false
	}
	for _, argument := range spec.Params {
		if !validKIRConcreteType(argument, structs, enums, 0) {
			return false
		}
	}
	return true
}

func validKIRConcreteType(spec *TypeSpec, structs map[string]*KIRStruct, enums map[string]*KIREnum, depth int) bool {
	if spec == nil || depth > 128 {
		return false
	}
	if spec.Function {
		if !validKIRConcreteType(spec.Return, structs, enums, depth+1) {
			return false
		}
		for _, parameter := range spec.Params {
			if !validKIRConcreteType(parameter, structs, enums, depth+1) {
				return false
			}
		}
		return true
	}
	if declaration := structs[spec.Name]; declaration != nil {
		if len(spec.Params) != len(declaration.TypeParams) {
			return false
		}
		for _, argument := range spec.Params {
			if !validKIRConcreteType(argument, structs, enums, depth+1) {
				return false
			}
		}
		return true
	}
	if enums[spec.Name] != nil {
		return len(spec.Params) == 0
	}
	if len(spec.Params) == 0 {
		switch spec.Name {
		case "Void", "Nil", "Int", "UInt8", "UInt16", "UInt32", "UInt64", "Float", "Bool", "String", "Bytes", "Json", "WebSocket", "Regex", "Random", "SQLite", "TcpSocket", "TcpListener", "UdpSocket", "FFILibrary", "FFISymbol", "FFIBuffer", "TaskGroup":
			return true
		}
		return false
	}
	arities := map[string]int{"Array": 1, "Option": 1, "Result": 2, "Channel": 1, "Thread": 1, "Map": 2, "Set": 1, "Actor": 1, "Shared": 1}
	arity, known := arities[spec.Name]
	if !known || len(spec.Params) != arity {
		return false
	}
	for _, argument := range spec.Params {
		if !validKIRConcreteType(argument, structs, enums, depth+1) {
			return false
		}
	}
	return true
}

func validKIRStructInstanceType(encoded string, declaration *KIRStruct) bool {
	if declaration == nil {
		return false
	}
	if len(declaration.TypeParams) == 0 {
		return encoded == declaration.Name
	}
	arguments, ok := splitKIRGenericArguments(encoded, declaration.Name)
	if !ok || len(arguments) != len(declaration.TypeParams) {
		return false
	}
	for _, argument := range arguments {
		if !validKIRTypeExpression(argument) {
			return false
		}
	}
	return true
}

func splitKIRGenericArguments(encoded, name string) ([]string, bool) {
	prefix := name + "["
	if !strings.HasPrefix(encoded, prefix) || !strings.HasSuffix(encoded, "]") {
		return nil, false
	}
	inner := encoded[len(prefix) : len(encoded)-1]
	if inner == "" {
		return nil, false
	}
	var arguments []string
	start, square, round := 0, 0, 0
	for i, r := range inner {
		switch r {
		case '[':
			square++
		case ']':
			square--
			if square < 0 {
				return nil, false
			}
		case '(':
			round++
		case ')':
			round--
			if round < 0 {
				return nil, false
			}
		case ',':
			if square == 0 && round == 0 {
				argument := strings.TrimSpace(inner[start:i])
				if argument == "" {
					return nil, false
				}
				arguments = append(arguments, argument)
				start = i + 1
			}
		}
	}
	if square != 0 || round != 0 {
		return nil, false
	}
	last := strings.TrimSpace(inner[start:])
	if last == "" {
		return nil, false
	}
	arguments = append(arguments, last)
	return arguments, true
}

func validKIRTypeExpression(encoded string) bool {
	if encoded == "" {
		return false
	}
	_, ok := parseKIRTypeExpression(encoded)
	return ok
}

func validKIRTypeVariables(encoded string, parameters map[string]bool) bool {
	spec, ok := parseKIRTypeExpression(encoded)
	if !ok {
		return false
	}
	var visit func(*TypeSpec) bool
	visit = func(current *TypeSpec) bool {
		if current == nil {
			return false
		}
		if parameters[current.Name] && len(current.Params) != 0 {
			return false
		}
		if current.Function && !visit(current.Return) {
			return false
		}
		for _, child := range current.Params {
			if !visit(child) {
				return false
			}
		}
		return true
	}
	return visit(spec)
}

func parseKIRTypeExpression(encoded string) (*TypeSpec, bool) {
	limits := DefaultLimits()
	source := &Source{Name: "<KIR type>", Text: encoded}
	tokens, diagnostic := Lex(source, limits)
	if diagnostic != nil {
		return nil, false
	}
	parser := &Parser{Tokens: tokens, Lim: limits}
	spec := parser.typeSpec()
	if parser.Err != nil || !parser.check(EOF) || TypeSpecString(spec) != encoded {
		return nil, false
	}
	return spec, true
}

func isKIRUnaryOperator(operator string) bool {
	switch operator {
	case "+", "-", "!", "~":
		return true
	default:
		return false
	}
}

func isKIRBinaryOperator(operator string) bool {
	switch operator {
	case "+", "-", "*", "/", "%", "==", "!=", "<", "<=", ">", ">=", "&&", "||", "&", "|", "^", "<<", ">>":
		return true
	default:
		return false
	}
}

func containsKIRString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func parseKIRFunctionType(raw string) ([]string, string, bool) {
	if !strings.HasPrefix(raw, "fn(") {
		return nil, "", false
	}
	var params []string
	start, parentheses, brackets := 3, 1, 0
	end := -1
	for i := 3; i < len(raw); i++ {
		switch raw[i] {
		case '[':
			brackets++
		case ']':
			brackets--
			if brackets < 0 {
				return nil, "", false
			}
		case '(':
			parentheses++
		case ')':
			parentheses--
			if parentheses < 0 {
				return nil, "", false
			}
			if parentheses == 0 {
				end = i
				rawParam := strings.TrimSpace(raw[start:i])
				if rawParam != "" {
					params = append(params, rawParam)
				}
				break
			}
		case ',':
			if parentheses == 1 && brackets == 0 {
				rawParam := strings.TrimSpace(raw[start:i])
				if rawParam == "" {
					return nil, "", false
				}
				params = append(params, rawParam)
				start = i + 1
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 || brackets != 0 || parentheses != 0 {
		return nil, "", false
	}
	remainder := strings.TrimSpace(raw[end+1:])
	if !strings.HasPrefix(remainder, "->") {
		return nil, "", false
	}
	result := strings.TrimSpace(strings.TrimPrefix(remainder, "->"))
	if result == "" {
		return nil, "", false
	}
	return params, result, true
}

func validKIRBinding(binding *KIRBinding) bool {
	return binding != nil && binding.Name != "" && binding.Type != "" && binding.Source != "" && binding.Line >= 1 && binding.Column >= 1
}

func kirBindingIdentity(binding *KIRBinding) string {
	if binding == nil {
		return ""
	}
	return fmt.Sprintf("%s:%d:%d:%s", binding.Source, binding.Line, binding.Column, binding.Name)
}

func validateKIRLambdaCaptures(function *KIRFunction) error {
	locals := map[string]*KIRBinding{}
	uses := map[string]*KIRBinding{}
	captures := map[string]*KIRBinding{}
	for _, parameter := range function.Params {
		locals[kirBindingIdentity(parameter.Binding)] = parameter.Binding
	}
	for _, capture := range function.Captures {
		captures[kirBindingIdentity(capture.Binding)] = capture.Binding
	}
	addUse := func(binding *KIRBinding) error {
		if !validKIRBinding(binding) {
			return fmt.Errorf("lambda contains an unresolved variable binding")
		}
		identity := kirBindingIdentity(binding)
		if previous := uses[identity]; previous != nil && (previous.Type != binding.Type || previous.Mutable != binding.Mutable) {
			return fmt.Errorf("lambda references a binding with inconsistent type or mutability")
		}
		uses[identity] = binding
		return nil
	}
	var walkExpr func(*KIRExpr) error
	var walkStmt func(*KIRStmt) error
	walkExpr = func(expression *KIRExpr) error {
		if expression == nil {
			return nil
		}
		if expression.Kind == "lambda" {
			if expression.Lambda == nil {
				return fmt.Errorf("nested lambda has no function body")
			}
			for _, capture := range expression.Lambda.Captures {
				if capture == nil {
					return fmt.Errorf("nested lambda has an incomplete capture")
				}
				if err := addUse(capture.Binding); err != nil {
					return err
				}
			}
			return nil
		}
		if expression.Kind == "var" && expression.Binding != nil {
			if err := addUse(expression.Binding); err != nil {
				return err
			}
		}
		for _, child := range []*KIRExpr{expression.Left, expression.Right, expression.Operand, expression.Base, expression.Receiver, expression.Callee} {
			if err := walkExpr(child); err != nil {
				return err
			}
		}
		for _, list := range [][]*KIRExpr{expression.Args, expression.Items, expression.MapKeys, expression.Values} {
			for _, child := range list {
				if err := walkExpr(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	walkStmt = func(statement *KIRStmt) error {
		if statement == nil {
			return nil
		}
		if statement.Binding != nil {
			locals[kirBindingIdentity(statement.Binding)] = statement.Binding
		}
		for _, expression := range []*KIRExpr{statement.Init, statement.Expr, statement.Target, statement.Value, statement.Cond, statement.Iter, statement.Return, statement.Scrutinee} {
			if err := walkExpr(expression); err != nil {
				return err
			}
		}
		for _, list := range [][]*KIRStmt{statement.Then, statement.Else, statement.Body} {
			for _, child := range list {
				if err := walkStmt(child); err != nil {
					return err
				}
			}
		}
		for _, arm := range statement.Arms {
			if arm == nil {
				continue
			}
			if arm.Pattern != nil && arm.Pattern.ResolvedBinding != nil {
				locals[kirBindingIdentity(arm.Pattern.ResolvedBinding)] = arm.Pattern.ResolvedBinding
			}
			for _, child := range arm.Body {
				if err := walkStmt(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, statement := range function.Body {
		if err := walkStmt(statement); err != nil {
			return err
		}
	}
	for identity, binding := range uses {
		if local := locals[identity]; local != nil {
			if local.Type != binding.Type || local.Mutable != binding.Mutable {
				return fmt.Errorf("lambda local binding %q has inconsistent type or mutability", binding.Name)
			}
			continue
		}
		capture := captures[identity]
		if capture == nil {
			return fmt.Errorf("lambda is missing capture %q", binding.Name)
		}
		if capture.Type != binding.Type || capture.Mutable != binding.Mutable {
			return fmt.Errorf("lambda capture %q does not match its resolved binding", binding.Name)
		}
	}
	for identity, capture := range captures {
		if locals[identity] != nil {
			return fmt.Errorf("lambda capture %q is local to its own body", capture.Name)
		}
		if uses[identity] == nil {
			return fmt.Errorf("lambda has unused capture %q", capture.Name)
		}
	}
	return nil
}
