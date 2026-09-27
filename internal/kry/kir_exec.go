package kry

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

// errKIRSubsetUnsupported distinguishes a valid KIR program outside this
// executor's deliberately small executable subset from malformed KIR.
var errKIRSubsetUnsupported = fmt.Errorf("KIR executor does not support this program")

type kirExecResult struct {
	Output     []byte
	Diagnostic *Diagnostic
}

type kirExecBinding struct {
	meta  *KIRBinding
	value Value
}

type kirExecScope struct {
	parent *kirExecScope
	names  map[string]string
	values map[string]*kirExecBinding
	defers [][]*KIRStmt
}

func newKIRExecScope(parent *kirExecScope) *kirExecScope {
	return &kirExecScope{parent: parent, names: map[string]string{}, values: map[string]*kirExecBinding{}}
}

func (scope *kirExecScope) find(identity string) (*kirExecBinding, bool) {
	for current := scope; current != nil; current = current.parent {
		if binding, ok := current.values[identity]; ok {
			return binding, true
		}
	}
	return nil, false
}

func (scope *kirExecScope) local(name string) bool { return scope.names[name] != "" }

func (scope *kirExecScope) define(binding *KIRBinding, value Value) error {
	if binding == nil || !validKIRBinding(binding) {
		return fmt.Errorf("declaration has invalid binding metadata")
	}
	if scope.local(binding.Name) {
		return fmt.Errorf("binding '%s' is already defined in this scope", binding.Name)
	}
	identity := kirBindingIdentity(binding)
	if _, exists := scope.values[identity]; exists {
		return fmt.Errorf("binding '%s' has a duplicate identity", binding.Name)
	}
	scope.names[binding.Name] = identity
	scope.values[identity] = &kirExecBinding{meta: binding, value: cloneValue(value)}
	return nil
}

type kirClosure struct {
	function    *KIRFunction
	environment *kirExecScope
}

type kirExecFlow struct {
	value    Value
	returned bool
	control  kirExecControl
	tail     *kirExecTailCall
}

type kirExecControl uint8

const (
	kirExecNormal kirExecControl = iota
	kirExecBreak
	kirExecContinue
)

type kirExecTailCall struct {
	call        *KIRExpr
	function    *KIRFunction
	environment *kirExecScope
	arguments   []Value
}

type kirExecutor struct {
	limits      Limits
	context     *ExecContext
	sources     map[string]*Source
	output      []byte
	functions   map[string]*KIRFunction
	closures    map[*FunctionValue]*kirClosure
	structs     map[string]*StructDecl
	enums       map[string]*EnumDecl
	types       map[string]*Type
	builtins    *Runtime
	global      *kirExecScope
	propagated  *Value
	returnTypes []string
}

// executeKIRSubset runs the scalar executable KIR slice directly. Callers
// must pass a decoded document; this function repeats structural validation
// because it is also the boundary used by native code generation.
func executeKIRSubset(document *KIRDocument, limits Limits, sources map[string]*Source) (kirExecResult, error) {
	if document == nil {
		return kirExecResult{}, fmt.Errorf("invalid KIR executable: missing document")
	}
	if document.Format != KIRFormat || document.Version < 3 || document.Version > KIRVersion {
		return kirExecResult{}, fmt.Errorf("invalid KIR executable: unsupported format or version")
	}
	if err := validateKIRDocument(document, limits); err != nil {
		return kirExecResult{}, fmt.Errorf("invalid KIR executable: %w", err)
	}
	functions := kirExecFunctions(document)
	if err := validateKIRExecSubset(document); err != nil {
		return kirExecResult{}, err
	}

	var ctx context.Context
	var cancel context.CancelFunc
	if limits.MaxWallTimeMS > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(limits.MaxWallTimeMS)*time.Millisecond)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()
	executor := &kirExecutor{
		limits:    limits,
		context:   &ExecContext{Ctx: ctx, Cancel: cancel, Lim: limits},
		sources:   sources,
		functions: functions,
		closures:  make(map[*FunctionValue]*kirClosure),
		structs:   make(map[string]*StructDecl, len(document.Structs)),
		enums:     make(map[string]*EnumDecl, len(document.Enums)),
		types:     make(map[string]*Type),
	}
	if err := executor.initializeKIRTypes(document); err != nil {
		return kirExecResult{}, err
	}
	executor.initializeKIRBuiltins(document)
	scope := newKIRExecScope(nil)
	executor.global = scope
	var diagnostic *Diagnostic
	if len(document.Statements) != 0 {
		_, diagnostic = executor.execBlock(scope, document.Statements, true)
	} else if main := executor.functionNamed("main"); main != nil {
		call := &KIRExpr{Kind: "call", Name: "main", Source: main.Source, Line: main.Line, Column: main.Column, Type: main.Return, CallTarget: "function:main"}
		if executor.context.Calls >= limits.MaxCallDepth {
			diagnostic = executor.fail(CatResource, call.Source, call.Line, call.Column, "call depth limit exceeded")
		} else {
			_, diagnostic = executor.invokeKIR(call, main, scope, nil)
			if diagnostic == nil {
				diagnostic = executor.context.contextFailure(executor.source(main.Source), main.Line, main.Column)
			}
		}
	}
	return kirExecResult{Output: append([]byte(nil), executor.output...), Diagnostic: diagnostic}, nil
}

func kirExecFunctions(document *KIRDocument) map[string]*KIRFunction {
	counts := make(map[string]int, len(document.Functions))
	for _, function := range document.Functions {
		if function != nil {
			counts[function.Name]++
		}
	}
	functions := make(map[string]*KIRFunction, len(document.Functions))
	for _, function := range document.Functions {
		if function == nil {
			continue
		}
		target := function.Name
		if counts[function.Name] > 1 {
			target = kirFunctionTargetFromDocument(function)
		}
		functions[target] = function
	}
	return functions
}

func (executor *kirExecutor) functionNamed(name string) *KIRFunction {
	var found *KIRFunction
	for _, function := range executor.functions {
		if function != nil && function.Name == name {
			if found != nil {
				return nil
			}
			found = function
		}
	}
	return found
}

func (executor *kirExecutor) invokeKIR(call *KIRExpr, function *KIRFunction, environment *kirExecScope, arguments []Value) (Value, *Diagnostic) {
	if executor.context.Calls >= executor.limits.MaxCallDepth {
		return nilVal(), executor.fail(CatResource, call.Source, call.Line, call.Column, "call depth limit exceeded")
	}
	executor.context.Calls++
	defer func() { executor.context.Calls-- }()
	for {
		if len(arguments) != len(function.Params) {
			return nilVal(), executor.fail(CatArtifact, call.Source, call.Line, call.Column, "KIR function call has %d arguments for %d parameters", len(arguments), len(function.Params))
		}
		child := newKIRExecScope(environment)
		for i, parameter := range function.Params {
			if err := child.define(parameter.Binding, arguments[i]); err != nil {
				return nilVal(), executor.fail(CatArtifact, call.Source, call.Line, call.Column, "%s", err)
			}
		}
		executor.returnTypes = append(executor.returnTypes, function.Return)
		flow, diagnostic := executor.execBlock(child, function.Body, false)
		executor.returnTypes = executor.returnTypes[:len(executor.returnTypes)-1]
		if diagnostic != nil {
			diagnostic.Stack = append(diagnostic.Stack, StackFrame{Function: function.Name, Source: call.Source, Line: call.Line, Column: call.Column})
			return nilVal(), diagnostic
		}
		if flow.tail != nil {
			function = flow.tail.function
			environment = flow.tail.environment
			arguments = flow.tail.arguments
			continue
		}
		if flow.returned {
			if !executor.valueMatchesType(flow.value, function.Return) {
				return nilVal(), executor.fail(CatArtifact, call.Source, call.Line, call.Column, "KIR function returned a value that does not match %q", function.Return)
			}
			return cloneValue(flow.value), nil
		}
		if function.Return != "Nil" {
			return nilVal(), executor.fail(CatArtifact, function.Source, function.Line, function.Column, "KIR function %q completed without returning %q", function.Name, function.Return)
		}
		return nilVal(), nil
	}
}

// validateKIRExecSubset is the semantic guard for this executor. DecodeKIR
// checks the wire structure and resource limits, while this pass proves the
// scalar type and lexical-binding invariants used below, including branches
// that execution may not visit.
func validateKIRExecSubset(document *KIRDocument) error {
	return validateKIRExecSubsetWithFunctions(document, kirExecFunctions(document))
}

func validateKIRExecSubsetWithFunctions(document *KIRDocument, functions map[string]*KIRFunction) error {
	if document.Version < 3 || document.Version > KIRVersion {
		return fmt.Errorf("%w: KIR v3 or newer resolved bindings are required", errKIRSubsetUnsupported)
	}
	if len(document.Imports) != 0 || len(document.Traits) != 0 || len(document.TraitImpls) != 0 {
		return fmt.Errorf("%w: imports, traits, and trait implementations are not yet executable from KIR", errKIRSubsetUnsupported)
	}
	for _, declaration := range document.Structs {
		if declaration == nil {
			return fmt.Errorf("invalid KIR executable: struct list contains a missing node")
		}
		if len(declaration.TypeParams) != 0 {
			return fmt.Errorf("%w: generic struct %q", errKIRSubsetUnsupported, declaration.Name)
		}
		for _, field := range declaration.Fields {
			if field == nil || !kirExecTypeInDocument(field.Type, document) {
				return fmt.Errorf("%w: struct %q field has unsupported type", errKIRSubsetUnsupported, declaration.Name)
			}
		}
	}
	if len(document.Statements) == 0 && len(document.Functions) == 0 {
		return fmt.Errorf("%w: no executable statements", errKIRSubsetUnsupported)
	}
	if len(document.Statements) == 0 {
		main := functions["main"]
		if main == nil || main.Name != "main" {
			return fmt.Errorf("%w: programs without top-level statements require a unique main()", errKIRSubsetUnsupported)
		}
		if main.Return != "Nil" || len(main.Params) != 0 {
			return fmt.Errorf("%w: main must be a zero-argument function returning Nil", errKIRSubsetUnsupported)
		}
	}
	for _, function := range document.Functions {
		if function == nil {
			return fmt.Errorf("invalid KIR executable: function list contains a missing node")
		}
		if function.Trait != "" || function.Receiver != "" || len(function.TypeParams) != 0 || function.Worker || function.Unsafe || len(function.Captures) != 0 {
			return fmt.Errorf("%w: trait methods, receivers, generics, workers, unsafe functions, and top-level captures are outside the KIR function/closure subset", errKIRSubsetUnsupported)
		}
		if !kirExecTypeInDocument(function.Return, document) {
			return fmt.Errorf("%w: function %q return type %q", errKIRSubsetUnsupported, function.Name, function.Return)
		}
		for _, parameter := range function.Params {
			if parameter == nil || parameter.Default != nil || !kirExecTypeInDocument(parameter.Type, document) {
				return fmt.Errorf("%w: function %q has a default or unsupported parameter type", errKIRSubsetUnsupported, function.Name)
			}
		}
	}
	root := newKIRExecScope(nil)
	if err := validateKIRExecBlock(document.Statements, root, "", functions, document); err != nil {
		return err
	}
	for _, function := range document.Functions {
		functionScope := newKIRExecScope(root)
		for _, parameter := range function.Params {
			if err := functionScope.define(parameter.Binding, nilVal()); err != nil {
				return fmt.Errorf("invalid KIR executable: function %q: %w", function.Name, err)
			}
		}
		if err := validateKIRExecBlock(function.Body, functionScope, function.Return, functions, document); err != nil {
			return fmt.Errorf("invalid KIR executable: function %q: %w", function.Name, err)
		}
		if function.Return != "Nil" && !kirExecBlockReturns(function.Body) {
			return fmt.Errorf("invalid KIR executable: function %q can finish without returning %s", function.Name, function.Return)
		}
	}
	return nil
}

func kirExecBlockReturns(statements []*KIRStmt) bool {
	for _, statement := range statements {
		if statement == nil {
			continue
		}
		switch statement.Kind {
		case "return":
			return true
		case "if":
			if len(statement.Else) != 0 && kirExecBlockReturns(statement.Then) && kirExecBlockReturns(statement.Else) {
				return true
			}
		}
	}
	return false
}

func validateKIRExecBlock(statements []*KIRStmt, scope *kirExecScope, returnType string, functions map[string]*KIRFunction, document *KIRDocument) error {
	return validateKIRExecBlockAtDepth(statements, scope, returnType, functions, document, 0)
}

func validateKIRExecBlockAtDepth(statements []*KIRStmt, scope *kirExecScope, returnType string, functions map[string]*KIRFunction, document *KIRDocument, loopDepth int) error {
	for _, statement := range statements {
		if statement == nil {
			return fmt.Errorf("invalid KIR executable: statement list contains a missing node")
		}
		switch statement.Kind {
		case "let", "const":
			if err := validateKIRExecExpr(statement.Init, scope, false, functions, document); err != nil {
				return err
			}
			if statement.Kind == "const" && statement.Binding.Mutable {
				return fmt.Errorf("invalid KIR executable: const binding '%s' is mutable", statement.Name)
			}
			if !kirExecTypeInDocument(statement.Binding.Type, document) || statement.Binding.Type != statement.Init.Type {
				return fmt.Errorf("invalid KIR executable: declaration '%s' has incompatible executable type %q", statement.Name, statement.Binding.Type)
			}
			if statement.Annotation != "" && statement.Annotation != statement.Binding.Type {
				return fmt.Errorf("%w: declaration annotation %q", errKIRSubsetUnsupported, statement.Annotation)
			}
			if scope.local(statement.Name) {
				return fmt.Errorf("invalid KIR executable: binding '%s' is duplicated in one scope", statement.Name)
			}
			if err := scope.define(statement.Binding, nilVal()); err != nil {
				return fmt.Errorf("invalid KIR executable: %w", err)
			}
		case "assign":
			if err := validateKIRExecExpr(statement.Target, scope, false, functions, document); err != nil {
				return err
			}
			binding, ok := scope.find(kirBindingIdentity(statement.Target.Binding))
			if !ok || !sameKIRBinding(binding.meta, statement.Target.Binding) {
				return fmt.Errorf("invalid KIR executable: assignment to unresolved binding '%s'", statement.Target.Name)
			}
			if !binding.meta.Mutable {
				return fmt.Errorf("invalid KIR executable: assignment to immutable binding '%s'", statement.Target.Name)
			}
			if err := validateKIRExecExpr(statement.Value, scope, false, functions, document); err != nil {
				return err
			}
			if statement.Target.Type != statement.Value.Type {
				return fmt.Errorf("invalid KIR executable: assignment to '%s' changes its type", statement.Target.Name)
			}
		case "expr":
			if err := validateKIRExecExpr(statement.Expr, scope, true, functions, document); err != nil {
				return err
			}
		case "if":
			if err := validateKIRExecExpr(statement.Cond, scope, false, functions, document); err != nil {
				return err
			}
			if statement.Cond.Type != "Bool" {
				return fmt.Errorf("invalid KIR executable: if condition has type %q, want Bool", statement.Cond.Type)
			}
			if err := validateKIRExecBlockAtDepth(statement.Then, newKIRExecScope(scope), returnType, functions, document, loopDepth); err != nil {
				return err
			}
			if err := validateKIRExecBlockAtDepth(statement.Else, newKIRExecScope(scope), returnType, functions, document, loopDepth); err != nil {
				return err
			}
		case "while":
			if err := validateKIRExecExpr(statement.Cond, scope, false, functions, document); err != nil {
				return err
			}
			if statement.Cond.Type != "Bool" {
				return fmt.Errorf("invalid KIR executable: while condition has type %q, want Bool", statement.Cond.Type)
			}
			if err := validateKIRExecBlockAtDepth(statement.Body, newKIRExecScope(scope), returnType, functions, document, loopDepth+1); err != nil {
				return err
			}
		case "for":
			if err := validateKIRExecExpr(statement.Iter, scope, false, functions, document); err != nil {
				return err
			}
			itemType := ""
			if name, arguments, ok := parseKIRContainerType(statement.Iter.Type); ok && len(arguments) == 1 && (name == "Array" || name == "Set") {
				itemType = arguments[0]
			} else {
				switch statement.Iter.Type {
				case "String":
					itemType = "String"
				case "Bytes":
					itemType = "Int"
				}
			}
			if itemType == "" || !validKIRBinding(statement.Binding) || statement.Binding.Name != statement.Name || statement.Binding.Mutable || statement.Binding.Type != itemType {
				return fmt.Errorf("invalid KIR executable: for binding %q has inconsistent iterator type metadata", statement.Name)
			}
			loopScope := newKIRExecScope(scope)
			if err := loopScope.define(statement.Binding, nilVal()); err != nil {
				return fmt.Errorf("invalid KIR executable: %w", err)
			}
			if err := validateKIRExecBlockAtDepth(statement.Body, loopScope, returnType, functions, document, loopDepth+1); err != nil {
				return err
			}
		case "match":
			if err := validateKIRExecExpr(statement.Scrutinee, scope, false, functions, document); err != nil {
				return err
			}
			for _, arm := range statement.Arms {
				if arm == nil || arm.Pattern == nil {
					return fmt.Errorf("invalid KIR executable: match has a missing arm or pattern")
				}
				armScope := newKIRExecScope(scope)
				if err := validateKIRExecPattern(arm.Pattern, statement.Scrutinee.Type, armScope, document); err != nil {
					return err
				}
				if err := validateKIRExecBlockAtDepth(arm.Body, armScope, returnType, functions, document, loopDepth); err != nil {
					return err
				}
			}
		case "break", "continue":
			if loopDepth == 0 {
				return fmt.Errorf("invalid KIR executable: %s outside a loop", statement.Kind)
			}
		case "defer", "unsafe":
			if err := validateKIRExecBlockAtDepth(statement.Body, newKIRExecScope(scope), returnType, functions, document, loopDepth); err != nil {
				return err
			}
		case "return":
			if returnType == "" {
				return fmt.Errorf("invalid KIR executable: return escapes top-level execution")
			}
			if statement.Return != nil {
				if err := validateKIRExecExpr(statement.Return, scope, false, functions, document); err != nil {
					return err
				}
				if statement.Return.Type != returnType && !kirExecPropagationMatches(statement.Return, returnType) {
					return fmt.Errorf("invalid KIR executable: return has type %q, want %q", statement.Return.Type, returnType)
				}
				if returnType == "Nil" && statement.Return.Kind != "nil" {
					return fmt.Errorf("invalid KIR executable: Nil function must return nil")
				}
			} else if returnType != "Nil" {
				return fmt.Errorf("invalid KIR executable: bare return cannot satisfy result type %q", returnType)
			}
		default:
			return fmt.Errorf("%w: statement kind %q", errKIRSubsetUnsupported, statement.Kind)
		}
	}
	return nil
}

func validateKIRExecPattern(pattern *KIRPattern, scrutineeType string, scope *kirExecScope, document *KIRDocument) error {
	if pattern == nil {
		return fmt.Errorf("invalid KIR executable: missing match pattern")
	}
	valid := false
	var bindingType string
	switch pattern.Kind {
	case "wildcard":
		valid = true
	case "nil":
		valid = scrutineeType == "Nil" || strings.HasPrefix(scrutineeType, "Option[")
	case "bool":
		valid = scrutineeType == "Bool"
	case "int":
		valid = scrutineeType == "Int"
	case "string":
		valid = scrutineeType == "String"
	case "enum":
		declaration := findKIREnum(document, pattern.Type)
		valid = declaration != nil && scrutineeType == pattern.Type && kirExecContainsString(declaration.Variants, pattern.Variant)
	case "option":
		name, arguments, ok := parseKIRContainerType(scrutineeType)
		valid = ok && name == "Option" && len(arguments) == 1
		if valid && pattern.Present && pattern.Binding != "" {
			bindingType = arguments[0]
		}
	case "result":
		name, arguments, ok := parseKIRContainerType(scrutineeType)
		valid = ok && name == "Result" && len(arguments) == 2
		if valid && pattern.Binding != "" {
			if pattern.OK {
				bindingType = arguments[0]
			} else {
				bindingType = arguments[1]
			}
		}
	default:
		return fmt.Errorf("%w: match pattern kind %q", errKIRSubsetUnsupported, pattern.Kind)
	}
	if !valid {
		return fmt.Errorf("invalid KIR executable: pattern %q is incompatible with %q", pattern.Kind, scrutineeType)
	}
	if pattern.Binding == "" {
		if pattern.ResolvedBinding != nil {
			return fmt.Errorf("invalid KIR executable: match pattern has binding metadata without a binding")
		}
		return nil
	}
	if bindingType == "" || pattern.ResolvedBinding == nil || !validKIRBinding(pattern.ResolvedBinding) || pattern.ResolvedBinding.Name != pattern.Binding || pattern.ResolvedBinding.Type != bindingType {
		return fmt.Errorf("invalid KIR executable: match binding %q has inconsistent type metadata", pattern.Binding)
	}
	return scope.define(pattern.ResolvedBinding, nilVal())
}

func findKIREnum(document *KIRDocument, name string) *KIREnum {
	if document == nil {
		return nil
	}
	for _, declaration := range document.Enums {
		if declaration != nil && declaration.Name == name {
			return declaration
		}
	}
	return nil
}

func findKIRStruct(document *KIRDocument, name string) *KIRStruct {
	if document == nil {
		return nil
	}
	for _, declaration := range document.Structs {
		if declaration != nil && declaration.Name == name {
			return declaration
		}
	}
	return nil
}

func kirExecPropagationMatches(expression *KIRExpr, returnType string) bool {
	if expression == nil || expression.Kind != "propagate" {
		return false
	}
	name, arguments, ok := parseKIRContainerType(expression.Operand.Type)
	if !ok {
		return false
	}
	return (name == "Option" || name == "Result") && returnType == expression.Operand.Type && expression.Type == arguments[0]
}

func kirExecBuiltinSupported(builtin Builtin) bool {
	switch builtin.Effects {
	case "pure", "diagnostic", "collections", "json", "crypto":
		return true
	case "io":
		return builtin.Name == "print" || builtin.Name == "println"
	default:
		return false
	}
}

func validateKIRExecExpr(expression *KIRExpr, scope *kirExecScope, allowOutput bool, functions map[string]*KIRFunction, document *KIRDocument) error {
	if expression == nil {
		return fmt.Errorf("invalid KIR executable: missing expression")
	}
	if !kirExecTypeInDocument(expression.Type, document) {
		return fmt.Errorf("%w: expression type %q", errKIRSubsetUnsupported, expression.Type)
	}
	switch expression.Kind {
	case "int":
		if expression.Type != "Int" {
			return fmt.Errorf("invalid KIR executable: integer literal has type %q", expression.Type)
		}
	case "float":
		if expression.Type != "Float" || math.IsNaN(expression.Float) || math.IsInf(expression.Float, 0) {
			return fmt.Errorf("invalid KIR executable: malformed Float literal")
		}
	case "bool":
		if expression.Type != "Bool" {
			return fmt.Errorf("invalid KIR executable: Bool literal has type %q", expression.Type)
		}
	case "string":
		if expression.Type != "String" {
			return fmt.Errorf("invalid KIR executable: String literal has type %q", expression.Type)
		}
	case "nil":
		if expression.Type != "Nil" {
			return fmt.Errorf("%w: nil literal typed as %q", errKIRSubsetUnsupported, expression.Type)
		}
	case "var":
		if expression.CallTarget != "" {
			prefix, target, ok := strings.Cut(expression.CallTarget, ":")
			function := functions[target]
			if !ok || prefix != "function" || expression.Binding != nil || function == nil || function.Name != expression.Name || kirExecFunctionType(function) != expression.Type {
				return fmt.Errorf("invalid KIR executable: function value has an unknown or mismatched target")
			}
			break
		}
		if !validKIRBinding(expression.Binding) {
			return fmt.Errorf("invalid KIR executable: variable has no local binding")
		}
		binding, ok := scope.find(kirBindingIdentity(expression.Binding))
		if !ok || !sameKIRBinding(binding.meta, expression.Binding) {
			return fmt.Errorf("invalid KIR executable: variable '%s' references an undeclared binding", expression.Name)
		}
		if expression.Type != binding.meta.Type {
			return fmt.Errorf("invalid KIR executable: variable '%s' has a mismatched type", expression.Name)
		}
	case "unary":
		if err := validateKIRExecExpr(expression.Operand, scope, false, functions, document); err != nil {
			return err
		}
		switch expression.Operator {
		case "!":
			if expression.Type != "Bool" || expression.Operand.Type != "Bool" {
				return fmt.Errorf("invalid KIR executable: ! requires Bool")
			}
		case "+", "-":
			if (expression.Operand.Type != "Int" && expression.Operand.Type != "Float") || expression.Type != expression.Operand.Type {
				return fmt.Errorf("%w: unary %s on %s", errKIRSubsetUnsupported, expression.Operator, expression.Operand.Type)
			}
		default:
			return fmt.Errorf("%w: unary operator %q", errKIRSubsetUnsupported, expression.Operator)
		}
	case "binary":
		if err := validateKIRExecExpr(expression.Left, scope, false, functions, document); err != nil {
			return err
		}
		if err := validateKIRExecExpr(expression.Right, scope, false, functions, document); err != nil {
			return err
		}
		left, right := expression.Left.Type, expression.Right.Type
		switch expression.Operator {
		case "&&", "||":
			if left != "Bool" || right != "Bool" || expression.Type != "Bool" {
				return fmt.Errorf("invalid KIR executable: logical operators require Bool")
			}
		case "==", "!=":
			if left != right || expression.Type != "Bool" {
				return fmt.Errorf("invalid KIR executable: equality operands must have matching types")
			}
			if strings.HasPrefix(left, "fn(") {
				return fmt.Errorf("%w: function values are not comparable", errKIRSubsetUnsupported)
			}
		case "<", "<=", ">", ">=":
			if (left != "Int" && left != "Float") || right != left || expression.Type != "Bool" {
				return fmt.Errorf("%w: ordered comparison of %s and %s", errKIRSubsetUnsupported, left, right)
			}
		case "+":
			if left == "String" && right == "String" && expression.Type == "String" {
				break
			}
			fallthrough
		case "-", "*", "/", "%":
			if left != right || (left != "Int" && left != "Float") || expression.Type != left || (expression.Operator == "%" && left != "Int") {
				return fmt.Errorf("%w: operator %s on %s and %s", errKIRSubsetUnsupported, expression.Operator, left, right)
			}
		default:
			return fmt.Errorf("%w: binary operator %q", errKIRSubsetUnsupported, expression.Operator)
		}
	case "array":
		name, arguments, ok := parseKIRContainerType(expression.Type)
		if !ok || name != "Array" || len(arguments) != 1 {
			return fmt.Errorf("invalid KIR executable: array literal has a non-Array type")
		}
		for _, item := range expression.Items {
			if err := validateKIRExecExpr(item, scope, false, functions, document); err != nil {
				return err
			}
			if item.Type != arguments[0] {
				return fmt.Errorf("invalid KIR executable: array item type %q does not match %q", item.Type, arguments[0])
			}
		}
	case "map":
		name, arguments, ok := parseKIRContainerType(expression.Type)
		if !ok || name != "Map" || len(arguments) != 2 || len(expression.MapKeys) != len(expression.Values) {
			return fmt.Errorf("invalid KIR executable: map literal has malformed type or entries")
		}
		for i, key := range expression.MapKeys {
			if err := validateKIRExecExpr(key, scope, false, functions, document); err != nil {
				return err
			}
			if err := validateKIRExecExpr(expression.Values[i], scope, false, functions, document); err != nil {
				return err
			}
			if key.Type != arguments[0] || expression.Values[i].Type != arguments[1] {
				return fmt.Errorf("invalid KIR executable: map entry type does not match %q", expression.Type)
			}
		}
	case "set":
		name, arguments, ok := parseKIRContainerType(expression.Type)
		if !ok || name != "Set" || len(arguments) != 1 {
			return fmt.Errorf("invalid KIR executable: set literal has a non-Set type")
		}
		for _, item := range expression.Items {
			if err := validateKIRExecExpr(item, scope, false, functions, document); err != nil {
				return err
			}
			if item.Type != arguments[0] {
				return fmt.Errorf("invalid KIR executable: set item type %q does not match %q", item.Type, arguments[0])
			}
		}
	case "enum":
		declaration := findKIREnum(document, expression.EnumType)
		if declaration == nil || expression.Type != expression.EnumType || !kirExecContainsString(declaration.Variants, expression.EnumVariant) {
			return fmt.Errorf("invalid KIR executable: enum literal has an unknown or mismatched type/variant")
		}
	case "struct":
		declaration := findKIRStruct(document, expression.StructName)
		if declaration == nil || expression.Type != expression.StructName || expression.StructType != expression.StructName || len(expression.Fields) != len(expression.Values) || len(expression.Fields) != len(declaration.Fields) {
			return fmt.Errorf("invalid KIR executable: struct literal has an unknown or mismatched declaration")
		}
		seen := make(map[string]bool, len(expression.Fields))
		fieldTypes := make(map[string]string, len(declaration.Fields))
		for _, field := range declaration.Fields {
			fieldTypes[field.Name] = field.Type
		}
		for i, name := range expression.Fields {
			want, exists := fieldTypes[name]
			if !exists || seen[name] {
				return fmt.Errorf("invalid KIR executable: struct literal has unknown or duplicate field %q", name)
			}
			seen[name] = true
			if err := validateKIRExecExpr(expression.Values[i], scope, false, functions, document); err != nil {
				return err
			}
			if expression.Values[i].Type != want {
				return fmt.Errorf("invalid KIR executable: struct field %q has type %q, want %q", name, expression.Values[i].Type, want)
			}
		}
	case "index":
		if err := validateKIRExecExpr(expression.Base, scope, false, functions, document); err != nil {
			return err
		}
		if err := validateKIRExecExpr(expression.Left, scope, false, functions, document); err != nil {
			return err
		}
		baseType := expression.Base.Type
		name, arguments, composite := parseKIRContainerType(baseType)
		switch {
		case composite && name == "Array" && len(arguments) == 1:
			if expression.Left.Type != "Int" || expression.Type != arguments[0] {
				return fmt.Errorf("invalid KIR executable: Array index has inconsistent types")
			}
		case composite && name == "Map" && len(arguments) == 2:
			if expression.Left.Type != arguments[0] || expression.Type != arguments[1] {
				return fmt.Errorf("invalid KIR executable: Map index has inconsistent types")
			}
		case baseType == "String":
			if expression.Left.Type != "Int" || expression.Type != "String" {
				return fmt.Errorf("invalid KIR executable: String index has inconsistent types")
			}
		case baseType == "Bytes":
			if expression.Left.Type != "Int" || expression.Type != "Int" {
				return fmt.Errorf("invalid KIR executable: Bytes index has inconsistent types")
			}
		default:
			return fmt.Errorf("%w: indexing %q", errKIRSubsetUnsupported, baseType)
		}
	case "field":
		if err := validateKIRExecExpr(expression.Base, scope, false, functions, document); err != nil {
			return err
		}
		declaration := findKIRStruct(document, expression.Base.Type)
		if declaration == nil {
			return fmt.Errorf("%w: field access on %q", errKIRSubsetUnsupported, expression.Base.Type)
		}
		found := false
		for _, field := range declaration.Fields {
			if field.Name == expression.Field {
				found = true
				if expression.Type != field.Type {
					return fmt.Errorf("invalid KIR executable: field type %q does not match %q", expression.Type, field.Type)
				}
				break
			}
		}
		if !found {
			return fmt.Errorf("invalid KIR executable: struct %q has no field %q", declaration.Name, expression.Field)
		}
	case "propagate":
		if err := validateKIRExecExpr(expression.Operand, scope, false, functions, document); err != nil {
			return err
		}
		name, arguments, ok := parseKIRContainerType(expression.Operand.Type)
		if !ok || (name != "Option" && name != "Result") || expression.Type != arguments[0] {
			return fmt.Errorf("invalid KIR executable: propagation has inconsistent Option/Result type")
		}
	case "call":
		if expression.Receiver != nil {
			return fmt.Errorf("%w: method call", errKIRSubsetUnsupported)
		}
		if expression.Callee != nil {
			if expression.Name != "" || expression.CallTarget != "" || expression.BuiltinID != "" {
				return fmt.Errorf("invalid KIR executable: indirect call has named-target metadata")
			}
			parameters, result, ok := parseKIRFunctionType(expression.Callee.Type)
			if !ok || len(parameters) != len(expression.Args) || result != expression.Type {
				return fmt.Errorf("invalid KIR executable: indirect call signature does not match its type")
			}
			if err := validateKIRExecExpr(expression.Callee, scope, false, functions, document); err != nil {
				return err
			}
			for i, argument := range expression.Args {
				if err := validateKIRExecExpr(argument, scope, false, functions, document); err != nil {
					return err
				}
				if argument.Type != parameters[i] {
					return fmt.Errorf("invalid KIR executable: indirect call argument %d has mismatched type", i+1)
				}
			}
			break
		}
		prefix, target, ok := strings.Cut(expression.CallTarget, ":")
		if !ok {
			return fmt.Errorf("invalid KIR executable: call has no target")
		}
		if prefix == "function" {
			function := functions[target]
			if function == nil || function.Name != expression.Name || expression.Type != function.Return || len(expression.Args) != len(function.Params) {
				return fmt.Errorf("invalid KIR executable: direct function call has a mismatched target or signature")
			}
			if expression.BuiltinID != "" || expression.TraitName != "" {
				return fmt.Errorf("invalid KIR executable: direct function call contains builtin or trait metadata")
			}
			for i, argument := range expression.Args {
				if err := validateKIRExecExpr(argument, scope, false, functions, document); err != nil {
					return err
				}
				if argument.Type != function.Params[i].Type {
					return fmt.Errorf("invalid KIR executable: call to %q has mismatched argument %d", function.Name, i+1)
				}
			}
			break
		}
		if prefix != "builtin" {
			return fmt.Errorf("%w: trait or unknown call target %q", errKIRSubsetUnsupported, prefix)
		}
		builtin, exists := Builtins()[expression.Name]
		if !exists || expression.BuiltinID == "" || expression.Name != target || expression.CallTarget != "builtin:"+expression.Name || len(expression.Args) != builtin.Arity {
			return fmt.Errorf("invalid KIR executable: builtin call has mismatched target or arity")
		}
		for _, argument := range expression.Args {
			if err := validateKIRExecExpr(argument, scope, false, functions, document); err != nil {
				return err
			}
		}
		if expression.BuiltinID != builtin.ID {
			return fmt.Errorf("invalid KIR executable: builtin call has an unknown or mismatched id")
		}
		if (expression.Name == "print" || expression.Name == "println") && expression.Type != "Nil" {
			return fmt.Errorf("invalid KIR executable: output builtin has a non-Nil type")
		}
		if expression.Name == "str" && expression.Type != "String" {
			return fmt.Errorf("invalid KIR executable: str call has invalid result type")
		}
		if !kirExecBuiltinSupported(builtin) {
			return fmt.Errorf("%w: builtin %q has host effect %q outside the KIR executor capability boundary", errKIRSubsetUnsupported, expression.Name, builtin.Effects)
		}
	case "lambda":
		function := expression.Lambda
		if function == nil || expression.Callee != nil || function.Unsafe || function.Worker || function.Receiver != "" || function.Trait != "" || len(function.TypeParams) != 0 {
			return fmt.Errorf("%w: malformed, generic, unsafe, or receiver lambda", errKIRSubsetUnsupported)
		}
		if expression.Type != kirExecFunctionType(function) || !kirExecTypeInDocument(expression.Type, document) || !kirExecTypeInDocument(function.Return, document) {
			return fmt.Errorf("invalid KIR executable: lambda type does not match its signature")
		}
		captureScope := newKIRExecScope(nil)
		for _, capture := range function.Captures {
			if capture == nil || !validKIRBinding(capture.Binding) {
				return fmt.Errorf("invalid KIR executable: lambda has invalid capture metadata")
			}
			visible, ok := scope.find(kirBindingIdentity(capture.Binding))
			if !ok || !sameKIRBinding(visible.meta, capture.Binding) {
				return fmt.Errorf("invalid KIR executable: lambda captures unavailable binding %q", capture.Binding.Name)
			}
			if err := captureScope.define(capture.Binding, nilVal()); err != nil {
				return fmt.Errorf("invalid KIR executable: lambda capture: %w", err)
			}
		}
		lambdaScope := newKIRExecScope(captureScope)
		for _, parameter := range function.Params {
			if parameter == nil || parameter.Default != nil || !kirExecTypeInDocument(parameter.Type, document) {
				return fmt.Errorf("%w: lambda has default or unsupported parameter", errKIRSubsetUnsupported)
			}
			if err := lambdaScope.define(parameter.Binding, nilVal()); err != nil {
				return fmt.Errorf("invalid KIR executable: lambda parameter: %w", err)
			}
		}
		if err := validateKIRExecBlock(function.Body, lambdaScope, function.Return, functions, document); err != nil {
			return err
		}
		if function.Return != "Nil" && !kirExecBlockReturns(function.Body) {
			return fmt.Errorf("invalid KIR executable: lambda %q can finish without returning %s", function.Name, function.Return)
		}
	default:
		return fmt.Errorf("%w: expression kind %q", errKIRSubsetUnsupported, expression.Kind)
	}
	return nil
}

func kirExecScalarType(typ string) bool {
	switch typ {
	case "Int", "Float", "Bool", "String", "Nil":
		return true
	default:
		return false
	}
}

func kirExecType(typ string) bool {
	if kirExecScalarType(typ) {
		return true
	}
	if typ == "Bytes" || typ == "Json" {
		return true
	}
	if parameters, result, ok := parseKIRFunctionType(typ); ok {
		if !kirExecType(result) {
			return false
		}
		for _, parameter := range parameters {
			if !kirExecType(parameter) {
				return false
			}
		}
		return true
	}
	name, arguments, ok := parseKIRContainerType(typ)
	if !ok {
		return false
	}
	want := 1
	if name == "Map" || name == "Result" {
		want = 2
	}
	if len(arguments) != want || (name != "Array" && name != "Option" && name != "Result" && name != "Set" && name != "Map") {
		return false
	}
	for _, argument := range arguments {
		if !kirExecType(argument) {
			return false
		}
	}
	return true
}

func parseKIRContainerType(encoded string) (string, []string, bool) {
	open := strings.IndexByte(encoded, '[')
	if open <= 0 || !strings.HasSuffix(encoded, "]") {
		return "", nil, false
	}
	name := encoded[:open]
	body := encoded[open+1 : len(encoded)-1]
	if body == "" {
		return "", nil, false
	}
	depth := 0
	start := 0
	arguments := make([]string, 0, 2)
	for i, r := range body {
		switch r {
		case '[':
			depth++
		case ']':
			depth--
			if depth < 0 {
				return "", nil, false
			}
		case ',':
			if depth == 0 {
				arguments = append(arguments, strings.TrimSpace(body[start:i]))
				start = i + 1
			}
		}
	}
	if depth != 0 {
		return "", nil, false
	}
	arguments = append(arguments, strings.TrimSpace(body[start:]))
	return name, arguments, true
}

func kirExecTypeInDocument(encoded string, document *KIRDocument) bool {
	if kirExecType(encoded) {
		return true
	}
	if document == nil {
		return false
	}
	for _, declaration := range document.Structs {
		if declaration != nil && declaration.Name == encoded && len(declaration.TypeParams) == 0 {
			return true
		}
	}
	for _, declaration := range document.Enums {
		if declaration != nil && declaration.Name == encoded {
			return true
		}
	}
	if parameters, result, ok := parseKIRFunctionType(encoded); ok {
		if !kirExecTypeInDocument(result, document) {
			return false
		}
		for _, parameter := range parameters {
			if !kirExecTypeInDocument(parameter, document) {
				return false
			}
		}
		return true
	}
	name, arguments, ok := parseKIRContainerType(encoded)
	if !ok {
		return false
	}
	want := 1
	if name == "Map" || name == "Result" {
		want = 2
	}
	if len(arguments) != want || (name != "Array" && name != "Option" && name != "Result" && name != "Set" && name != "Map") {
		return false
	}
	for _, argument := range arguments {
		if !kirExecTypeInDocument(argument, document) {
			return false
		}
	}
	return true
}

func (executor *kirExecutor) initializeKIRTypes(document *KIRDocument) error {
	for _, declaration := range document.Structs {
		if len(declaration.TypeParams) != 0 {
			return fmt.Errorf("%w: generic struct %q", errKIRSubsetUnsupported, declaration.Name)
		}
		structDecl := &StructDecl{Name: declaration.Name}
		typ := &Type{Kind: TyStruct, Name: declaration.Name, Struct: structDecl}
		structDecl.Type = typ
		executor.structs[declaration.Name] = structDecl
		executor.types[declaration.Name] = typ
	}
	for _, declaration := range document.Enums {
		enumDecl := &EnumDecl{Name: declaration.Name, Variants: append([]string(nil), declaration.Variants...)}
		typ := &Type{Kind: TyEnum, Name: declaration.Name, Enum: enumDecl}
		enumDecl.Type = typ
		executor.enums[declaration.Name] = enumDecl
		executor.types[declaration.Name] = typ
	}
	for _, declaration := range document.Structs {
		structDecl := executor.structs[declaration.Name]
		for _, field := range declaration.Fields {
			typ, ok := executor.resolveKIRType(field.Type)
			if !ok {
				return fmt.Errorf("invalid KIR executable: struct %q field %q has unsupported type %q", declaration.Name, field.Name, field.Type)
			}
			structDecl.Fields = append(structDecl.Fields, FieldDecl{Name: field.Name, Type: typ})
		}
	}
	return nil
}

func (executor *kirExecutor) resolveKIRType(encoded string) (*Type, bool) {
	if typ := executor.types[encoded]; typ != nil {
		return typ, true
	}
	switch encoded {
	case "Nil":
		return TNil, true
	case "Int":
		return TInt, true
	case "Float":
		return TFloat, true
	case "Bool":
		return TBool, true
	case "String":
		return TString, true
	case "Bytes":
		return TBytes, true
	case "Json":
		return TJSON, true
	}
	if parameters, result, ok := parseKIRFunctionType(encoded); ok {
		params := make([]*Type, len(parameters))
		for i, parameter := range parameters {
			parsed, ok := executor.resolveKIRType(parameter)
			if !ok {
				return nil, false
			}
			params[i] = parsed
		}
		resultType, ok := executor.resolveKIRType(result)
		if !ok {
			return nil, false
		}
		return FunctionType(params, resultType), true
	}
	name, arguments, ok := parseKIRContainerType(encoded)
	if !ok {
		return nil, false
	}
	parsed := make([]*Type, len(arguments))
	for i, argument := range arguments {
		parsed[i], ok = executor.resolveKIRType(argument)
		if !ok {
			return nil, false
		}
	}
	switch name {
	case "Array":
		return Arr(parsed[0]), true
	case "Option":
		return Opt(parsed[0]), true
	case "Result":
		return Res(parsed[0], parsed[1]), true
	case "Map":
		return MapOf(parsed[0], parsed[1]), true
	case "Set":
		return SetOf(parsed[0]), true
	}
	return nil, false
}

type kirExecOutputWriter struct{ executor *kirExecutor }

func (writer kirExecOutputWriter) Write(output []byte) (int, error) {
	writer.executor.output = append(writer.executor.output, output...)
	return len(output), nil
}

func (executor *kirExecutor) initializeKIRBuiltins(document *KIRDocument) {
	program := &Program{}
	env := &TypeEnv{Types: executor.types, Functions: map[string]*Function{}, Overloads: map[string][]*Function{}, Traits: map[string]*TraitDecl{}, TraitImpls: map[string]map[string]*TraitImplDecl{}, Builtins: Builtins(), TypeParams: map[string]*Type{}, Lim: executor.limits}
	checker := &Checker{Prog: program, Env: env, Lim: executor.limits}
	runtime := &Runtime{Prog: program, Checker: checker, Funcs: map[string]*Function{}, Global: newRunScope(nil), Lim: executor.limits, Ctx: executor.context, Dispatch: map[string][]DispatchEntry{}, discordRates: newDiscordRateLimiter(), discordCache: newDiscordObjectCache(10_000, 30*time.Minute), discordAPIBaseURL: discordAPIBase, discordGateway: newDiscordGatewayState()}
	if source := document.Source; source != "" {
		runtime.Prog.Source = executor.source(source)
	}
	runtime.output = kirExecOutputWriter{executor: executor}
	executor.builtins = runtime
}

func kirExecFunctionType(function *KIRFunction) string {
	if function == nil {
		return ""
	}
	parameters := make([]string, len(function.Params))
	for i, parameter := range function.Params {
		if parameter == nil {
			return ""
		}
		parameters[i] = parameter.Type
	}
	return "fn(" + strings.Join(parameters, ", ") + ") -> " + function.Return
}

func sameKIRBinding(left, right *KIRBinding) bool {
	return left != nil && right != nil && kirBindingIdentity(left) == kirBindingIdentity(right) && left.Name == right.Name && left.Type == right.Type && left.Mutable == right.Mutable
}

func (executor *kirExecutor) source(name string) *Source {
	if source := executor.sources[name]; source != nil {
		return source
	}
	if name == "" {
		return nil
	}
	return &Source{Name: name}
}

func (executor *kirExecutor) fail(category Category, source string, line, column int, format string, args ...any) *Diagnostic {
	return Diag(category, executor.source(source), line, column, format, args...)
}

func (executor *kirExecutor) step(source string, line, column int) *Diagnostic {
	return executor.context.step(executor.source(source), line, column)
}

func (executor *kirExecutor) execBlock(scope *kirExecScope, statements []*KIRStmt, topLevel bool) (kirExecFlow, *Diagnostic) {
	flow := kirExecFlow{value: nilVal()}
	var resultDiagnostic *Diagnostic
	for _, statement := range statements {
		if diagnostic := executor.step(statement.Source, statement.Line, statement.Column); diagnostic != nil {
			resultDiagnostic = diagnostic
			break
		}
		statementFlow, diagnostic := executor.execStmt(scope, statement)
		if diagnostic != nil {
			resultDiagnostic = diagnostic
			break
		}
		if topLevel {
			if diagnostic := executor.context.contextFailure(executor.source(statement.Source), statement.Line, statement.Column); diagnostic != nil {
				resultDiagnostic = diagnostic
				break
			}
		}
		flow = statementFlow
		if flow.returned || flow.control != kirExecNormal || flow.tail != nil {
			break
		}
	}
	for index := len(scope.defers) - 1; index >= 0; index-- {
		_, diagnostic := executor.execBlock(newKIRExecScope(scope), scope.defers[index], false)
		if diagnostic != nil && resultDiagnostic == nil {
			resultDiagnostic = diagnostic
		}
	}
	if resultDiagnostic != nil {
		return kirExecFlow{}, resultDiagnostic
	}
	return flow, nil
}

func (executor *kirExecutor) execStmt(scope *kirExecScope, statement *KIRStmt) (kirExecFlow, *Diagnostic) {
	switch statement.Kind {
	case "let", "const":
		value, diagnostic := executor.evalExpr(scope, statement.Init)
		if propagated := executor.takeKIRPropagated(); propagated != nil {
			return kirExecFlow{value: *propagated, returned: true}, nil
		}
		if diagnostic != nil {
			return kirExecFlow{}, diagnostic
		}
		if err := scope.define(statement.Binding, value); err != nil {
			return kirExecFlow{}, executor.fail(CatRuntime, statement.Source, statement.Line, statement.Column, "%s", err)
		}
		return kirExecFlow{value: nilVal()}, nil
	case "assign":
		target := statement.Target
		binding, ok := scope.find(kirBindingIdentity(target.Binding))
		if !ok {
			return kirExecFlow{}, executor.fail(CatRuntime, target.Source, target.Line, target.Column, "unknown binding '%s'", target.Name)
		}
		if !binding.meta.Mutable {
			return kirExecFlow{}, executor.fail(CatRuntime, target.Source, target.Line, target.Column, "immutable binding '%s' cannot be assigned", target.Name)
		}
		value, diagnostic := executor.evalExpr(scope, statement.Value)
		if propagated := executor.takeKIRPropagated(); propagated != nil {
			return kirExecFlow{value: *propagated, returned: true}, nil
		}
		if diagnostic != nil {
			return kirExecFlow{}, diagnostic
		}
		binding.value = cloneValue(value)
		return kirExecFlow{value: nilVal()}, nil
	case "expr":
		_, diagnostic := executor.evalExpr(scope, statement.Expr)
		if propagated := executor.takeKIRPropagated(); propagated != nil {
			return kirExecFlow{value: *propagated, returned: true}, nil
		}
		return kirExecFlow{value: nilVal()}, diagnostic
	case "if":
		condition, diagnostic := executor.evalExpr(scope, statement.Cond)
		if propagated := executor.takeKIRPropagated(); propagated != nil {
			return kirExecFlow{value: *propagated, returned: true}, nil
		}
		if diagnostic != nil {
			return kirExecFlow{}, diagnostic
		}
		if condition.Kind != VBool {
			return kirExecFlow{}, executor.fail(CatRuntime, statement.Cond.Source, statement.Cond.Line, statement.Cond.Column, "condition must be Bool")
		}
		body := statement.Else
		if condition.Bool {
			body = statement.Then
		}
		return executor.execBlock(newKIRExecScope(scope), body, false)
	case "while":
		for {
			// Runtime.execStmt accounts for the while statement once per
			// condition check, in addition to the enclosing block's step.
			if diagnostic := executor.step(statement.Source, statement.Line, statement.Column); diagnostic != nil {
				return kirExecFlow{}, diagnostic
			}
			condition, diagnostic := executor.evalExpr(scope, statement.Cond)
			if propagated := executor.takeKIRPropagated(); propagated != nil {
				return kirExecFlow{value: *propagated, returned: true}, nil
			}
			if diagnostic != nil {
				return kirExecFlow{}, diagnostic
			}
			if condition.Kind != VBool {
				return kirExecFlow{}, executor.fail(CatRuntime, statement.Cond.Source, statement.Cond.Line, statement.Cond.Column, "condition must be Bool")
			}
			if !condition.Bool {
				return kirExecFlow{value: nilVal()}, nil
			}
			flow, diagnostic := executor.execBlock(newKIRExecScope(scope), statement.Body, false)
			if diagnostic != nil || flow.returned || flow.tail != nil {
				return flow, diagnostic
			}
			if flow.control == kirExecBreak {
				return kirExecFlow{value: nilVal()}, nil
			}
			if flow.control == kirExecContinue {
				continue
			}
		}
	case "for":
		iterable, diagnostic := executor.evalExpr(scope, statement.Iter)
		if propagated := executor.takeKIRPropagated(); propagated != nil {
			return kirExecFlow{value: *propagated, returned: true}, nil
		}
		if diagnostic != nil {
			return kirExecFlow{}, diagnostic
		}
		var items []Value
		switch iterable.Kind {
		case VArray:
			items = arrayValues(iterable)
		case VSet:
			items = iterable.Set
		case VString:
			for _, character := range iterable.S {
				items = append(items, stringVal(string(character)))
			}
		case VBytes:
			for _, item := range iterable.Bytes {
				items = append(items, intVal(int64(item)))
			}
		default:
			return kirExecFlow{}, executor.fail(CatRuntime, statement.Iter.Source, statement.Iter.Line, statement.Iter.Column, "for expects Array, Set, String, or Bytes")
		}
		for _, item := range items {
			iterationScope := newKIRExecScope(scope)
			if err := iterationScope.define(statement.Binding, item); err != nil {
				return kirExecFlow{}, executor.fail(CatRuntime, statement.Iter.Source, statement.Iter.Line, statement.Iter.Column, "%s", err)
			}
			flow, diagnostic := executor.execBlock(iterationScope, statement.Body, false)
			if diagnostic != nil || flow.returned || flow.tail != nil {
				return flow, diagnostic
			}
			if flow.control == kirExecBreak {
				break
			}
		}
		return kirExecFlow{value: nilVal()}, nil
	case "break":
		return kirExecFlow{value: nilVal(), control: kirExecBreak}, nil
	case "continue":
		return kirExecFlow{value: nilVal(), control: kirExecContinue}, nil
	case "defer":
		scope.defers = append(scope.defers, statement.Body)
		return kirExecFlow{value: nilVal()}, nil
	case "unsafe":
		return executor.execBlock(newKIRExecScope(scope), statement.Body, false)
	case "match":
		value, diagnostic := executor.evalExpr(scope, statement.Scrutinee)
		if propagated := executor.takeKIRPropagated(); propagated != nil {
			return kirExecFlow{value: *propagated, returned: true}, nil
		}
		if diagnostic != nil {
			return kirExecFlow{}, diagnostic
		}
		for _, arm := range statement.Arms {
			pattern := Pattern{Bool: arm.Pattern.Bool, Int: arm.Pattern.Int, Str: arm.Pattern.String, TypeName: arm.Pattern.Type, Variant: arm.Pattern.Variant, Binding: arm.Pattern.Binding, Present: arm.Pattern.Present, OK: arm.Pattern.OK}
			switch arm.Pattern.Kind {
			case "wildcard":
				pattern.Kind = PatWildcard
			case "nil":
				pattern.Kind = PatNil
			case "bool":
				pattern.Kind = PatBool
			case "int":
				pattern.Kind = PatInt
			case "string":
				pattern.Kind = PatString
			case "enum":
				pattern.Kind = PatEnum
			case "option":
				pattern.Kind = PatOption
			case "result":
				pattern.Kind = PatResult
			}
			if !matchPattern(value, pattern) {
				continue
			}
			armScope := newKIRExecScope(scope)
			if arm.Pattern.Binding != "" {
				if value.Inner == nil {
					return kirExecFlow{}, executor.fail(CatArtifact, arm.Pattern.Source, arm.Pattern.Line, arm.Pattern.Column, "KIR match binding has no payload")
				}
				if err := armScope.define(arm.Pattern.ResolvedBinding, *value.Inner); err != nil {
					return kirExecFlow{}, executor.fail(CatArtifact, arm.Pattern.Source, arm.Pattern.Line, arm.Pattern.Column, "%s", err)
				}
			}
			return executor.execBlock(armScope, arm.Body, false)
		}
		return kirExecFlow{}, executor.fail(CatRuntime, statement.Scrutinee.Source, statement.Scrutinee.Line, statement.Scrutinee.Column, "no match arm matched")
	case "return":
		if statement.Return != nil && statement.Return.Kind == "call" && statement.Return.Tail {
			return executor.evalKIRTailCall(scope, statement.Return)
		}
		if statement.Return != nil {
			value, diagnostic := executor.evalExpr(scope, statement.Return)
			if propagated := executor.takeKIRPropagated(); propagated != nil {
				return kirExecFlow{value: *propagated, returned: true}, nil
			}
			if diagnostic != nil {
				return kirExecFlow{}, diagnostic
			}
			if statement.Return.Kind == "propagate" {
				if len(executor.returnTypes) == 0 {
					return kirExecFlow{}, executor.fail(CatArtifact, statement.Source, statement.Line, statement.Column, "KIR propagation return has no enclosing function type")
				}
				switch {
				case strings.HasPrefix(executor.returnTypes[len(executor.returnTypes)-1], "Option["):
					value = optVal(true, value)
				case strings.HasPrefix(executor.returnTypes[len(executor.returnTypes)-1], "Result["):
					value = resVal(true, value)
				}
			}
			return kirExecFlow{value: value, returned: true}, nil
		}
		return kirExecFlow{value: nilVal(), returned: true}, nil
	default:
		return kirExecFlow{}, executor.fail(CatRuntime, statement.Source, statement.Line, statement.Column, "unsupported KIR statement kind %s", statement.Kind)
	}
}

func (executor *kirExecutor) evalExpr(scope *kirExecScope, expression *KIRExpr) (Value, *Diagnostic) {
	if diagnostic := executor.step(expression.Source, expression.Line, expression.Column); diagnostic != nil {
		return nilVal(), diagnostic
	}
	var value Value
	switch expression.Kind {
	case "int":
		value = intVal(expression.Int)
	case "float":
		value = floatVal(expression.Float)
	case "bool":
		value = boolVal(expression.Bool)
	case "string":
		value = stringVal(expression.String)
	case "nil":
		value = nilVal()
	case "var":
		if expression.CallTarget != "" {
			prefix, target, ok := strings.Cut(expression.CallTarget, ":")
			function := executor.functions[target]
			if !ok || prefix != "function" || function == nil {
				return nilVal(), executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR function value references an unknown target")
			}
			if diagnostic := executor.context.account(48, executor.source(expression.Source), expression.Line, expression.Column); diagnostic != nil {
				return nilVal(), diagnostic
			}
			identity := &FunctionValue{}
			executor.closures[identity] = &kirClosure{function: function, environment: executor.global}
			value = Value{Kind: VFunction, Callable: identity}
			break
		}
		binding, ok := scope.find(kirBindingIdentity(expression.Binding))
		if !ok {
			return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "unknown name '%s'", expression.Name)
		}
		value = cloneValue(binding.value)
	case "lambda":
		closure, diagnostic := executor.makeKIRClosure(expression.Lambda, scope, expression.Source, expression.Line, expression.Column)
		if diagnostic != nil {
			return nilVal(), diagnostic
		}
		value = closure
	case "unary":
		operand, diagnostic := executor.evalExpr(scope, expression.Operand)
		if diagnostic != nil {
			return nilVal(), diagnostic
		}
		var ok bool
		value, ok = evalKIRUnary(expression.Operator, operand)
		if !ok {
			if operand.Kind == VInt && operand.I == math.MinInt64 && expression.Operator == "-" {
				return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "negation overflow")
			}
			if operand.Kind == VFloat && expression.Operator == "-" {
				return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "floating-point result must be finite")
			}
			return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "unsupported unary operator %s for %s", expression.Operator, expression.Operand.Type)
		}
	case "binary":
		left, diagnostic := executor.evalExpr(scope, expression.Left)
		if diagnostic != nil {
			return nilVal(), diagnostic
		}
		if expression.Operator == "&&" || expression.Operator == "||" {
			if left.Kind != VBool {
				return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "logical operators require Bool")
			}
			if expression.Operator == "&&" && !left.Bool {
				return boolVal(false), nil
			}
			if expression.Operator == "||" && left.Bool {
				return boolVal(true), nil
			}
			right, diagnostic := executor.evalExpr(scope, expression.Right)
			if diagnostic != nil {
				return nilVal(), diagnostic
			}
			if right.Kind != VBool {
				return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "logical operators require Bool")
			}
			return boolVal(right.Bool), nil
		}
		right, diagnostic := executor.evalExpr(scope, expression.Right)
		if diagnostic != nil {
			return nilVal(), diagnostic
		}
		if expression.Operator == "+" && left.Kind == VString && right.Kind == VString {
			if diagnostic := executor.context.account(int64(len(left.S)+len(right.S)), executor.source(expression.Source), expression.Line, expression.Column); diagnostic != nil {
				return nilVal(), diagnostic
			}
		}
		var ok bool
		value, ok = executor.evalKIRBinary(expression, left, right)
		if !ok {
			if left.Kind == VInt && right.Kind == VInt && (expression.Operator == "/" || expression.Operator == "%") && right.I == 0 {
				if expression.Operator == "%" {
					return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "remainder by zero")
				}
				return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "division by zero")
			}
			if left.Kind == VInt && right.Kind == VInt {
				return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "checked integer arithmetic overflow")
			}
			if left.Kind == VFloat && right.Kind == VFloat {
				if expression.Operator == "/" && right.F == 0 {
					return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "floating division by zero")
				}
				return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "floating-point result must be finite")
			}
			return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "operator operands have incompatible types")
		}
	case "array":
		items := make([]Value, len(expression.Items))
		for i, item := range expression.Items {
			var diagnostic *Diagnostic
			items[i], diagnostic = executor.evalExpr(scope, item)
			if diagnostic != nil {
				return nilVal(), diagnostic
			}
		}
		if diagnostic := executor.context.account(int64(len(items))*32, executor.source(expression.Source), expression.Line, expression.Column); diagnostic != nil {
			return nilVal(), diagnostic
		}
		value = arrVal(items)
	case "map":
		entries := make([]MapEntry, 0, len(expression.MapKeys))
		for i, keyExpression := range expression.MapKeys {
			key, keyDiagnostic := executor.evalExpr(scope, keyExpression)
			if keyDiagnostic != nil {
				return nilVal(), keyDiagnostic
			}
			item, itemDiagnostic := executor.evalExpr(scope, expression.Values[i])
			if itemDiagnostic != nil {
				return nilVal(), itemDiagnostic
			}
			for _, old := range entries {
				if equalValue(old.Key, key) {
					return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "duplicate map key")
				}
			}
			entries = append(entries, MapEntry{Key: cloneValue(key), Value: cloneValue(item)})
		}
		if int64(len(entries))*48 > executor.limits.MaxMemoryBytes {
			return nilVal(), executor.fail(CatResource, expression.Source, expression.Line, expression.Column, "map allocation exceeds memory budget")
		}
		value = Value{Kind: VMap, Map: entries}
	case "set":
		items := make([]Value, 0, len(expression.Items))
		for _, itemExpression := range expression.Items {
			item, itemDiagnostic := executor.evalExpr(scope, itemExpression)
			if itemDiagnostic != nil {
				return nilVal(), itemDiagnostic
			}
			found := false
			for _, old := range items {
				if equalValue(old, item) {
					found = true
					break
				}
			}
			if !found {
				items = append(items, cloneValue(item))
			}
		}
		value = Value{Kind: VSet, Set: items}
	case "enum":
		declaration := executor.enums[expression.EnumType]
		if declaration == nil || expression.Type != expression.EnumType {
			return nilVal(), executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR enum expression has an unknown or mismatched type")
		}
		if !kirExecContainsString(declaration.Variants, expression.EnumVariant) {
			return nilVal(), executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR enum expression has an unknown variant")
		}
		value = Value{Kind: VEnum, Enum: declaration, Variant: expression.EnumVariant}
	case "struct":
		declaration := executor.structs[expression.StructName]
		structType, ok := executor.resolveKIRType(expression.Type)
		if declaration == nil || !ok || structType.Kind != TyStruct || structType.Struct != declaration {
			return nilVal(), executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR struct expression has an unknown or mismatched type")
		}
		fields := make([]Value, len(declaration.Fields))
		initialized := make([]bool, len(declaration.Fields))
		for i, fieldName := range expression.Fields {
			fieldIndex := -1
			for candidateIndex, field := range declaration.Fields {
				if field.Name == fieldName {
					fieldIndex = candidateIndex
					break
				}
			}
			if fieldIndex < 0 || initialized[fieldIndex] {
				return nilVal(), executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR struct expression has an unknown or duplicate field %q", fieldName)
			}
			item, itemDiagnostic := executor.evalExpr(scope, expression.Values[i])
			if itemDiagnostic != nil {
				return nilVal(), itemDiagnostic
			}
			fields[fieldIndex] = cloneValue(item)
			initialized[fieldIndex] = true
		}
		for _, present := range initialized {
			if !present {
				return nilVal(), executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR struct expression is missing a declared field")
			}
		}
		value = Value{Kind: VStruct, Struct: declaration, StructType: structType, Fields: fields}
	case "index":
		base, baseDiagnostic := executor.evalExpr(scope, expression.Base)
		if baseDiagnostic != nil {
			return nilVal(), baseDiagnostic
		}
		index, indexDiagnostic := executor.evalExpr(scope, expression.Left)
		if indexDiagnostic != nil {
			return nilVal(), indexDiagnostic
		}
		if base.Kind != VMap && (index.Kind != VInt || index.I < 0) {
			return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "index must be a non-negative Int")
		}
		if base.Kind == VMap {
			found := false
			for _, entry := range base.Map {
				if equalValue(entry.Key, index) {
					value = cloneValue(entry.Value)
					found = true
					break
				}
			}
			if !found {
				return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "map key not found")
			}
		} else {
			i := index.I
			switch base.Kind {
			case VArray:
				if i >= int64(arrayLength(base)) {
					return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "array index out of range")
				}
				value = cloneValue(arrayAt(base, int(i)))
			case VString:
				runes := []rune(base.S)
				if i >= int64(len(runes)) {
					return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "string index out of range")
				}
				value = stringVal(string(runes[i]))
			case VBytes:
				if i >= int64(len(base.Bytes)) {
					return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "byte index out of range")
				}
				value = intVal(int64(base.Bytes[i]))
			default:
				return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "indexing expects String, Bytes, Array, or Map")
			}
		}
	case "field":
		base, baseDiagnostic := executor.evalExpr(scope, expression.Base)
		if baseDiagnostic != nil {
			return nilVal(), baseDiagnostic
		}
		if base.Kind != VStruct || base.Struct == nil {
			return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "field access expects a struct")
		}
		found := false
		for i, field := range base.Struct.Fields {
			if field.Name == expression.Field {
				value = cloneValue(base.Fields[i])
				found = true
				break
			}
		}
		if !found {
			return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "unknown field '%s'", expression.Field)
		}
	case "propagate":
		inner, innerDiagnostic := executor.evalExpr(scope, expression.Operand)
		if innerDiagnostic != nil {
			return nilVal(), innerDiagnostic
		}
		switch inner.Kind {
		case VOption:
			if !inner.Present {
				propagated := cloneValue(inner)
				executor.propagated = &propagated
				return nilVal(), nil
			}
			value = cloneValue(*inner.Inner)
		case VResult:
			if !inner.OK {
				propagated := cloneValue(inner)
				executor.propagated = &propagated
				return nilVal(), nil
			}
			value = cloneValue(*inner.Inner)
		default:
			return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "'?' requires an Option or Result")
		}
	case "call":
		called, diagnostic := executor.evalKIRCall(scope, expression)
		if diagnostic != nil {
			return nilVal(), diagnostic
		}
		value = called
	default:
		return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "unsupported KIR expression kind %s", expression.Kind)
	}
	if !executor.valueMatchesType(value, expression.Type) {
		return nilVal(), executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR expression value does not match checked type %q", expression.Type)
	}
	return value, nil
}

func (executor *kirExecutor) makeKIRClosure(function *KIRFunction, scope *kirExecScope, source string, line, column int) (Value, *Diagnostic) {
	if function == nil {
		return nilVal(), executor.fail(CatArtifact, source, line, column, "KIR lambda has no function body")
	}
	if diagnostic := executor.context.account(64+int64(len(function.Captures))*24, executor.source(source), line, column); diagnostic != nil {
		return nilVal(), diagnostic
	}
	environment := newKIRExecScope(nil)
	for _, capture := range function.Captures {
		if capture == nil || !validKIRBinding(capture.Binding) {
			return nilVal(), executor.fail(CatArtifact, source, line, column, "KIR lambda has invalid capture metadata")
		}
		binding, ok := scope.find(kirBindingIdentity(capture.Binding))
		if !ok || !sameKIRBinding(binding.meta, capture.Binding) {
			return nilVal(), executor.fail(CatArtifact, source, line, column, "KIR lambda captures unavailable binding '%s'", capture.Binding.Name)
		}
		cell := binding
		if !capture.Binding.Mutable {
			copy := *binding
			copy.value = cloneValue(binding.value)
			cell = &copy
		}
		identity := kirBindingIdentity(capture.Binding)
		if environment.local(capture.Binding.Name) {
			return nilVal(), executor.fail(CatArtifact, source, line, column, "KIR lambda has duplicate capture '%s'", capture.Binding.Name)
		}
		environment.names[capture.Binding.Name] = identity
		environment.values[identity] = cell
	}
	identity := &FunctionValue{}
	executor.closures[identity] = &kirClosure{function: function, environment: environment}
	return Value{Kind: VFunction, Callable: identity}, nil
}

func (executor *kirExecutor) resolveKIRCall(scope *kirExecScope, expression *KIRExpr) (*KIRFunction, *kirExecScope, bool, *Diagnostic) {
	if expression.Callee != nil {
		value, diagnostic := executor.evalExpr(scope, expression.Callee)
		if diagnostic != nil {
			return nil, nil, false, diagnostic
		}
		if value.Kind != VFunction || value.Callable == nil {
			return nil, nil, false, executor.fail(CatRuntime, expression.Callee.Source, expression.Callee.Line, expression.Callee.Column, "value is not callable")
		}
		closure := executor.closures[value.Callable]
		if closure == nil || closure.function == nil {
			return nil, nil, false, executor.fail(CatArtifact, expression.Callee.Source, expression.Callee.Line, expression.Callee.Column, "KIR function value has no executor closure")
		}
		return closure.function, closure.environment, false, nil
	}
	prefix, target, ok := strings.Cut(expression.CallTarget, ":")
	if !ok {
		return nil, nil, false, executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR call has no target")
	}
	if prefix == "builtin" {
		return nil, nil, true, nil
	}
	if prefix != "function" {
		return nil, nil, false, executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR call uses unsupported target %q", prefix)
	}
	function := executor.functions[target]
	if function == nil {
		return nil, nil, false, executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR call references unknown function target %q", target)
	}
	return function, executor.global, false, nil
}

func (executor *kirExecutor) evalKIRCall(scope *kirExecScope, expression *KIRExpr) (Value, *Diagnostic) {
	function, environment, builtin, diagnostic := executor.resolveKIRCall(scope, expression)
	if diagnostic != nil {
		return nilVal(), diagnostic
	}
	if builtin {
		arguments := make([]Value, len(expression.Args))
		for i, argument := range expression.Args {
			arguments[i], diagnostic = executor.evalExpr(scope, argument)
			if diagnostic != nil {
				return nilVal(), diagnostic
			}
		}
		if expression.Name == "str" && len(arguments) == 1 {
			return stringVal(executor.displayValue(arguments[0])), nil
		}
		if expression.Name == "print" || expression.Name == "println" {
			if len(arguments) != 1 {
				return nilVal(), executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR output builtin has invalid arity")
			}
			text := executor.displayValue(arguments[0])
			if expression.Name == "println" {
				text += "\n"
			}
			if int64(len(text)) > executor.limits.MaxOutputBytes-int64(len(executor.output)) {
				return nilVal(), executor.fail(CatResource, expression.Source, expression.Line, expression.Column, "output limit exceeded")
			}
			executor.output = append(executor.output, text...)
			executor.context.Output = int64(len(executor.output))
			return nilVal(), nil
		}
		metadata, exists := Builtins()[expression.Name]
		if !exists || !kirExecBuiltinSupported(metadata) || executor.builtins == nil {
			return nilVal(), executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR builtin %q passed validation without an executable implementation", expression.Name)
		}
		call := &Expr{Kind: ExCall, Name: expression.Name, Tok: Token{Source: executor.source(expression.Source), Line: expression.Line, Column: expression.Column}}
		return executor.builtins.evalBuiltin(call, metadata, arguments)
	}
	if executor.context.Calls >= executor.limits.MaxCallDepth {
		return nilVal(), executor.fail(CatResource, expression.Source, expression.Line, expression.Column, "call depth limit exceeded")
	}
	arguments := make([]Value, len(expression.Args))
	for i, argument := range expression.Args {
		arguments[i], diagnostic = executor.evalExpr(scope, argument)
		if diagnostic != nil {
			return nilVal(), diagnostic
		}
	}
	return executor.invokeKIR(expression, function, environment, arguments)
}

func (executor *kirExecutor) evalKIRTailCall(scope *kirExecScope, expression *KIRExpr) (kirExecFlow, *Diagnostic) {
	if diagnostic := executor.step(expression.Source, expression.Line, expression.Column); diagnostic != nil {
		return kirExecFlow{}, diagnostic
	}
	function, environment, builtin, diagnostic := executor.resolveKIRCall(scope, expression)
	if diagnostic != nil {
		return kirExecFlow{}, diagnostic
	}
	if builtin {
		value, diagnostic := executor.evalKIRCall(scope, expression)
		if diagnostic != nil {
			return kirExecFlow{}, diagnostic
		}
		return kirExecFlow{value: value, returned: true}, nil
	}
	if executor.context.Calls >= executor.limits.MaxCallDepth {
		return kirExecFlow{}, executor.fail(CatResource, expression.Source, expression.Line, expression.Column, "call depth limit exceeded")
	}
	arguments := make([]Value, len(expression.Args))
	for i, argument := range expression.Args {
		arguments[i], diagnostic = executor.evalExpr(scope, argument)
		if diagnostic != nil {
			return kirExecFlow{}, diagnostic
		}
	}
	return kirExecFlow{
		value:    nilVal(),
		returned: true,
		tail:     &kirExecTailCall{call: expression, function: function, environment: environment, arguments: arguments},
	}, nil
}

func (executor *kirExecutor) valueMatchesType(value Value, typ string) bool {
	if kirValueMatchesScalarType(value, typ) {
		return true
	}
	if typ == "Bytes" {
		return value.Kind == VBytes
	}
	if typ == "Json" {
		return value.Kind == VJSON
	}
	if parameters, result, ok := parseKIRFunctionType(typ); ok {
		_, _ = parameters, result
		if value.Kind != VFunction || value.Callable == nil {
			return false
		}
		closure := executor.closures[value.Callable]
		return closure != nil && closure.function != nil && kirExecFunctionType(closure.function) == typ
	}
	name, arguments, ok := parseKIRContainerType(typ)
	if ok {
		switch name {
		case "Array":
			if value.Kind != VArray {
				return false
			}
			for _, item := range arrayValues(value) {
				if !executor.valueMatchesType(item, arguments[0]) {
					return false
				}
			}
			return true
		case "Set":
			if value.Kind != VSet {
				return false
			}
			for _, item := range value.Set {
				if !executor.valueMatchesType(item, arguments[0]) {
					return false
				}
			}
			return true
		case "Map":
			if value.Kind != VMap {
				return false
			}
			for _, entry := range value.Map {
				if !executor.valueMatchesType(entry.Key, arguments[0]) || !executor.valueMatchesType(entry.Value, arguments[1]) {
					return false
				}
			}
			return true
		case "Option":
			return value.Kind == VOption && (!value.Present || value.Inner != nil && executor.valueMatchesType(*value.Inner, arguments[0]))
		case "Result":
			if value.Kind != VResult || value.Inner == nil {
				return false
			}
			if value.OK {
				return executor.valueMatchesType(*value.Inner, arguments[0])
			}
			return executor.valueMatchesType(*value.Inner, arguments[1])
		}
	}
	if typ == "Nil" && value.Kind == VNil {
		return true
	}
	if declaration := executor.structs[typ]; declaration != nil {
		return value.Kind == VStruct && value.Struct == declaration
	}
	if declaration := executor.enums[typ]; declaration != nil {
		return value.Kind == VEnum && value.Enum == declaration
	}
	return false
}

func (executor *kirExecutor) displayValue(value Value) string {
	if value.Kind == VFunction && value.Callable != nil {
		if closure := executor.closures[value.Callable]; closure != nil && closure.function != nil {
			return "<function " + closure.function.Name + ">"
		}
	}
	return display(value)
}

func evalKIRUnary(operator string, value Value) (Value, bool) {
	switch operator {
	case "!":
		if value.Kind == VBool {
			return boolVal(!value.Bool), true
		}
	case "+":
		if value.Kind == VInt || value.Kind == VFloat {
			return value, true
		}
	case "-":
		switch value.Kind {
		case VInt:
			if value.I != math.MinInt64 {
				return intVal(-value.I), true
			}
		case VFloat:
			negated := -value.F
			if isFinite(negated) {
				return floatVal(negated), true
			}
		}
	}
	return nilVal(), false
}

func (executor *kirExecutor) evalKIRBinary(expression *KIRExpr, left, right Value) (Value, bool) {
	operator := expression.Operator
	if operator == "==" {
		return boolVal(equalValue(left, right)), true
	}
	if operator == "!=" {
		return boolVal(!equalValue(left, right)), true
	}
	if left.Kind == VString && right.Kind == VString && operator == "+" {
		return stringVal(left.S + right.S), true
	}
	if left.Kind == VInt && right.Kind == VInt {
		var result int64
		var ok bool
		switch operator {
		case "+":
			result, ok = addI(left.I, right.I)
		case "-":
			result, ok = subI(left.I, right.I)
		case "*":
			result, ok = mulI(left.I, right.I)
		case "/":
			result, ok = divI(left.I, right.I)
		case "%":
			result, ok = remI(left.I, right.I)
		case "<":
			return boolVal(left.I < right.I), true
		case "<=":
			return boolVal(left.I <= right.I), true
		case ">":
			return boolVal(left.I > right.I), true
		case ">=":
			return boolVal(left.I >= right.I), true
		}
		if ok {
			return intVal(result), true
		}
		return nilVal(), false
	}
	if left.Kind == VFloat && right.Kind == VFloat {
		switch operator {
		case "<":
			return boolVal(left.F < right.F), true
		case "<=":
			return boolVal(left.F <= right.F), true
		case ">":
			return boolVal(left.F > right.F), true
		case ">=":
			return boolVal(left.F >= right.F), true
		}
		var result float64
		switch operator {
		case "+":
			result = left.F + right.F
		case "-":
			result = left.F - right.F
		case "*":
			result = left.F * right.F
		case "/":
			if right.F == 0 {
				return nilVal(), false
			}
			result = left.F / right.F
		}
		if !isFinite(result) {
			return nilVal(), false
		}
		return floatVal(result), true
	}
	return nilVal(), false
}

func kirValueMatchesScalarType(value Value, typ string) bool {
	switch typ {
	case "Int":
		return value.Kind == VInt
	case "Float":
		return value.Kind == VFloat
	case "Bool":
		return value.Kind == VBool
	case "String":
		return value.Kind == VString
	case "Nil":
		return value.Kind == VNil
	default:
		return false
	}
}

func (executor *kirExecutor) takeKIRPropagated() *Value {
	value := executor.propagated
	executor.propagated = nil
	return value
}

func kirExecContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func kirSourceMap(program *Program) map[string]*Source {
	if program == nil {
		return nil
	}
	paths := newKIRPathNames(program)
	sources := make(map[string]*Source, len(program.Sources)+1)
	if program.Source != nil {
		sources[paths.source(program.Source)] = program.Source
	}
	for _, source := range program.Sources {
		if source != nil {
			sources[paths.source(source)] = source
		}
	}
	return sources
}
