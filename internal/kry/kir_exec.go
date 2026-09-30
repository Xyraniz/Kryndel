package kry

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// errKIRSubsetUnsupported distinguishes a valid KIR program outside this
// executor's deliberately small executable subset from malformed KIR.
var errKIRSubsetUnsupported = fmt.Errorf("KIR executor does not support this program")

type kirExecResult struct {
	Output       []byte
	Diagnostic   *Diagnostic
	Context      *ExecContext
	Global       *kirExecScope
	DebugStopped bool
}

type kirExecBinding struct {
	meta  *KIRBinding
	value Value
}

type kirExecScope struct {
	parent           *kirExecScope
	names            map[string]string
	values           map[kirExecBindingKey]*kirExecBinding
	defers           [][]*KIRStmt
	types            map[string]string
	allowProcessArgs bool
	workerBase       *kirExecScope
}

type kirExecBindingKey struct {
	source       string
	name         string
	line, column int
}

func kirExecKey(binding *KIRBinding) kirExecBindingKey {
	if binding == nil {
		return kirExecBindingKey{}
	}
	return kirExecBindingKey{source: binding.Source, name: binding.Name, line: binding.Line, column: binding.Column}
}

func newKIRExecScope(parent *kirExecScope) *kirExecScope {
	scope := &kirExecScope{parent: parent, names: map[string]string{}, values: map[kirExecBindingKey]*kirExecBinding{}, allowProcessArgs: parent != nil && parent.allowProcessArgs}
	if parent != nil {
		scope.workerBase = parent.workerBase
	}
	return scope
}

func (scope *kirExecScope) find(binding *KIRBinding) (*kirExecBinding, bool) {
	value, _, ok := scope.findScope(binding)
	return value, ok
}

func (scope *kirExecScope) findScope(binding *KIRBinding) (*kirExecBinding, *kirExecScope, bool) {
	key := kirExecKey(binding)
	for current := scope; current != nil; current = current.parent {
		if value, ok := current.values[key]; ok {
			return value, current, true
		}
	}
	return nil, nil, false
}

func (scope *kirExecScope) local(name string) bool { return scope.names[name] != "" }

func (scope *kirExecScope) typeParameter(name string) (string, bool) {
	for current := scope; current != nil; current = current.parent {
		if constraint, ok := current.types[name]; ok {
			return constraint, true
		}
	}
	return "", false
}

func (scope *kirExecScope) workerLocalBinding(bindingScope *kirExecScope) bool {
	if scope == nil || scope.workerBase == nil {
		return false
	}
	for current := scope; current != nil; current = current.parent {
		if current == scope.workerBase.parent {
			return false
		}
		if current == bindingScope {
			return true
		}
		if current == scope.workerBase {
			return false
		}
	}
	return false
}

func (scope *kirExecScope) define(binding *KIRBinding, value Value) error {
	if binding == nil || !validKIRBinding(binding) {
		return fmt.Errorf("declaration has invalid binding metadata")
	}
	if scope.local(binding.Name) {
		return fmt.Errorf("binding '%s' is already defined in this scope", binding.Name)
	}
	identity := kirBindingIdentity(binding)
	key := kirExecKey(binding)
	if _, exists := scope.values[key]; exists {
		return fmt.Errorf("binding '%s' has a duplicate identity", binding.Name)
	}
	scope.names[binding.Name] = identity
	scope.values[key] = &kirExecBinding{meta: binding, value: cloneValue(value)}
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
	receiver    *Value
	arguments   []Value
}

type kirExecutor struct {
	limits       Limits
	context      *ExecContext
	document     *KIRDocument
	sources      map[string]*Source
	output       []byte
	functions    map[string]*KIRFunction
	closures     map[*FunctionValue]*kirClosure
	closureMu    *sync.RWMutex
	structs      map[string]*StructDecl
	enums        map[string]*EnumDecl
	types        map[string]*Type
	builtins     *Runtime
	global       *kirExecScope
	propagated   *Value
	returnTypes  []string
	typeArgs     []map[string]string
	outputWriter io.Writer
	outputMu     *sync.Mutex
	debugger     *Debugger
	debugStopped bool
	callFrames   []StackFrame
}

type kirExecOptions struct {
	sandbox   Sandbox
	args      []string
	context   *ExecContext
	output    io.Writer
	repl      bool
	debugger  *Debugger
	global    *kirExecScope
	allowArgs bool
	runtime   *Runtime
}

// executeKIRSubset runs the bounded executable KIR slice directly. Callers
// must pass a decoded document; this function repeats structural validation
// because it is also the boundary used by native code generation.
func executeValidatedMIR(mir *ValidatedMIR, limits Limits, sources map[string]*Source, sandbox ...Sandbox) (kirExecResult, error) {
	if mir == nil || mir.document == nil {
		return kirExecResult{}, fmt.Errorf("invalid MIR executable: missing validated document")
	}
	return executeKIRSubset(mir.document, limits, sources, sandbox...)
}

func executeKIRSubset(document *KIRDocument, limits Limits, sources map[string]*Source, sandbox ...Sandbox) (kirExecResult, error) {
	if document == nil {
		return kirExecResult{}, fmt.Errorf("invalid KIR executable: missing document")
	}
	if len(sandbox) > 1 {
		return kirExecResult{}, fmt.Errorf("invalid KIR executable: multiple sandboxes supplied")
	}
	if document.Format != KIRFormat || document.Version < 3 || document.Version > KIRVersion {
		return kirExecResult{}, fmt.Errorf("invalid KIR executable: unsupported format or version")
	}
	if err := validateKIRDocument(document, limits); err != nil {
		return kirExecResult{}, fmt.Errorf("invalid KIR executable: %w", err)
	}
	if err := validateKIRExecSubset(document); err != nil {
		return kirExecResult{}, err
	}
	var executionSandbox Sandbox
	if len(sandbox) == 1 {
		executionSandbox = sandbox[0]
	}
	return executeKIRWithOptions(document, limits, sources, kirExecOptions{sandbox: executionSandbox})
}

// executeMIRRuntime drives the production interpreter from the validated KIR
// graph. It accepts runtime lifecycle inputs without creating a frontend AST.
func executeMIRRuntime(mir *ValidatedMIR, limits Limits, sources map[string]*Source, sandbox Sandbox, args []string, context *ExecContext, output io.Writer, repl bool, debugger *Debugger, global *kirExecScope, runtime *Runtime) (kirExecResult, error) {
	if mir == nil || mir.document == nil {
		return kirExecResult{}, fmt.Errorf("invalid MIR executable: missing validated document")
	}
	if context == nil || context.Ctx == nil {
		return kirExecResult{}, fmt.Errorf("invalid MIR executable: missing runtime context")
	}
	return executeKIRWithOptions(mir.document, limits, sources, kirExecOptions{sandbox: sandbox, args: args, context: context, output: output, repl: repl, debugger: debugger, global: global, allowArgs: true, runtime: runtime})
}

func executeKIRWithOptions(document *KIRDocument, limits Limits, sources map[string]*Source, options kirExecOptions) (kirExecResult, error) {
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
	if err := validateKIRExecSubsetWithFunctions(document, functions, options.allowArgs); err != nil {
		return kirExecResult{}, err
	}
	var cancel context.CancelFunc
	execContext := options.context
	if execContext == nil {
		var ctx context.Context
		if limits.MaxWallTimeMS > 0 {
			ctx, cancel = context.WithTimeout(context.Background(), time.Duration(limits.MaxWallTimeMS)*time.Millisecond)
		} else {
			ctx, cancel = context.WithCancel(context.Background())
		}
		execContext = &ExecContext{Ctx: ctx, Cancel: cancel, Lim: limits}
	}
	if options.context == nil {
		defer cancel()
	}
	outputMu := &sync.Mutex{}
	if options.runtime != nil {
		outputMu = &options.runtime.kirOutputMu
	}
	executor := (*kirExecutor)(nil)
	if options.runtime != nil {
		executor = options.runtime.kirExecutor
	}
	if executor == nil {
		executor = &kirExecutor{
			closures:  make(map[*FunctionValue]*kirClosure),
			closureMu: &sync.RWMutex{},
			structs:   make(map[string]*StructDecl, len(document.Structs)),
			enums:     make(map[string]*EnumDecl, len(document.Enums)),
			types:     make(map[string]*Type),
		}
	}
	if executor.closures == nil {
		executor.closures = make(map[*FunctionValue]*kirClosure)
	}
	if executor.closureMu == nil {
		executor.closureMu = &sync.RWMutex{}
	}
	if executor.structs == nil {
		executor.structs = make(map[string]*StructDecl, len(document.Structs))
	}
	if executor.enums == nil {
		executor.enums = make(map[string]*EnumDecl, len(document.Enums))
	}
	if executor.types == nil {
		executor.types = make(map[string]*Type)
	}
	executor.limits = limits
	executor.context = execContext
	executor.document = document
	executor.sources = sources
	executor.functions = functions
	executor.output = nil
	executor.outputWriter = options.output
	executor.outputMu = outputMu
	executor.debugger = options.debugger
	executor.debugStopped = false
	executor.propagated = nil
	executor.returnTypes = nil
	executor.typeArgs = nil
	executor.callFrames = nil
	if err := executor.initializeKIRTypes(document); err != nil {
		return kirExecResult{}, err
	}
	if options.runtime != nil {
		options.runtime.kirExecutor = executor
		executor.builtins = options.runtime
		executor.builtins.Ctx = execContext
		executor.builtins.Lim = limits
		executor.builtins.Sandbox = options.sandbox
		executor.builtins.Args = append([]string(nil), options.args...)
	} else {
		executor.initializeKIRBuiltins(document, options.sandbox)
		executor.builtins.Args = append([]string(nil), options.args...)
	}
	scope := options.global
	if scope == nil {
		scope = newKIRExecScope(nil)
	}
	scope.allowProcessArgs = options.allowArgs
	executor.global = scope
	var diagnostic *Diagnostic
	if len(document.Statements) != 0 {
		_, diagnostic = executor.execBlock(scope, document.Statements, true, options.repl)
	} else if main, target := kirExecEntryFunction(document, functions); main != nil {
		call := &KIRExpr{Kind: "call", Name: "main", Source: main.Source, Line: main.Line, Column: main.Column, Type: main.Return, CallTarget: "function:" + target}
		if executor.context.Calls >= limits.MaxCallDepth {
			diagnostic = executor.fail(CatResource, call.Source, call.Line, call.Column, "call depth limit exceeded")
		} else {
			var value Value
			value, diagnostic = executor.invokeKIR(call, main, scope, nil, nil)
			if diagnostic == nil {
				diagnostic = executor.context.contextFailure(executor.source(main.Source), main.Line, main.Column)
			}
			if diagnostic == nil && value.Kind == VResult && !value.OK {
				diagnostic = executor.fail(CatRuntime, main.Source, main.Line, main.Column, "main returned error: %s", executor.displayValue(*value.Inner))
			}
		}
	}
	if options.runtime == nil {
		diagnostic = executor.builtins.cleanup(diagnostic)
	}
	return kirExecResult{Output: append([]byte(nil), executor.output...), Diagnostic: diagnostic, Context: execContext, Global: scope, DebugStopped: executor.debugStopped}, nil
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

func kirExecRequiredParams(parameters []*KIRParam) int {
	for index, parameter := range parameters {
		if parameter != nil && parameter.Default != nil {
			return index
		}
	}
	return len(parameters)
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

func (executor *kirExecutor) registerKIRClosure(identity *FunctionValue, closure *kirClosure) {
	executor.closureMu.Lock()
	executor.closures[identity] = closure
	executor.closureMu.Unlock()
}

func (executor *kirExecutor) findKIRClosure(identity *FunctionValue) *kirClosure {
	executor.closureMu.RLock()
	closure := executor.closures[identity]
	executor.closureMu.RUnlock()
	return closure
}

func kirExecEntryFunction(document *KIRDocument, functions map[string]*KIRFunction) (*KIRFunction, string) {
	if document == nil || len(functions) == 0 {
		return nil, ""
	}
	var moduleMatch *KIRFunction
	var moduleTarget string
	for target, function := range functions {
		if function == nil || function.Name != "main" || function.Module != document.Module {
			continue
		}
		if moduleMatch != nil {
			return nil, ""
		}
		moduleMatch, moduleTarget = function, target
	}
	if moduleMatch != nil {
		return moduleMatch, moduleTarget
	}
	var unique *KIRFunction
	var uniqueTarget string
	for target, function := range functions {
		if function == nil || function.Name != "main" {
			continue
		}
		if unique != nil {
			return nil, ""
		}
		unique, uniqueTarget = function, target
	}
	return unique, uniqueTarget
}

func (executor *kirExecutor) invokeKIR(call *KIRExpr, function *KIRFunction, environment *kirExecScope, receiver *Value, arguments []Value) (Value, *Diagnostic) {
	if executor.context.Calls >= executor.limits.MaxCallDepth {
		return nilVal(), executor.fail(CatResource, call.Source, call.Line, call.Column, "call depth limit exceeded")
	}
	typeArguments, valid := executor.kirCallTypeArguments(call, function)
	if !valid {
		return nilVal(), executor.fail(CatArtifact, call.Source, call.Line, call.Column, "KIR call has invalid generic arguments for %q", function.Name)
	}
	executor.context.Calls++
	defer func() { executor.context.Calls-- }()
	executor.typeArgs = append(executor.typeArgs, typeArguments)
	defer func() { executor.typeArgs = executor.typeArgs[:len(executor.typeArgs)-1] }()
	for {
		if function.Receiver != "" {
			if receiver == nil || !executor.valueMatchesType(*receiver, function.Receiver) {
				return nilVal(), executor.fail(CatArtifact, call.Source, call.Line, call.Column, "KIR method receiver does not match %q", function.Receiver)
			}
		} else if receiver != nil {
			return nilVal(), executor.fail(CatArtifact, call.Source, call.Line, call.Column, "KIR function call unexpectedly has a receiver")
		}
		if len(arguments) < kirExecRequiredParams(function.Params) || len(arguments) > len(function.Params) {
			return nilVal(), executor.fail(CatArtifact, call.Source, call.Line, call.Column, "KIR function call has %d arguments for %d parameters", len(arguments), len(function.Params))
		}
		child := newKIRExecScope(environment)
		if function.Receiver != "" {
			binding, valid := kirExecSelfBinding(function)
			if !valid || receiver == nil {
				return nilVal(), executor.fail(CatArtifact, call.Source, call.Line, call.Column, "KIR method call has invalid receiver binding")
			}
			if binding != nil {
				if err := child.define(binding, *receiver); err != nil {
					return nilVal(), executor.fail(CatArtifact, call.Source, call.Line, call.Column, "%s", err)
				}
			}
		} else if receiver != nil {
			return nilVal(), executor.fail(CatArtifact, call.Source, call.Line, call.Column, "KIR function call unexpectedly has a receiver")
		}
		for i, parameter := range function.Params {
			var argument Value
			if i < len(arguments) {
				argument = arguments[i]
			} else if parameter.Default != nil {
				var diagnostic *Diagnostic
				argument, diagnostic = executor.evalExpr(child, parameter.Default)
				if diagnostic != nil {
					return nilVal(), diagnostic
				}
			} else {
				return nilVal(), executor.fail(CatArtifact, call.Source, call.Line, call.Column, "KIR function call omits required parameter %q", parameter.Name)
			}
			if err := child.define(parameter.Binding, argument); err != nil {
				return nilVal(), executor.fail(CatArtifact, call.Source, call.Line, call.Column, "%s", err)
			}
		}
		executor.returnTypes = append(executor.returnTypes, function.Return)
		executor.callFrames = append(executor.callFrames, StackFrame{Function: function.Name, Source: call.Source, Line: call.Line, Column: call.Column})
		flow, diagnostic := executor.execBlock(child, function.Body, false)
		executor.callFrames = executor.callFrames[:len(executor.callFrames)-1]
		executor.returnTypes = executor.returnTypes[:len(executor.returnTypes)-1]
		if diagnostic != nil {
			diagnostic.Stack = append(diagnostic.Stack, StackFrame{Function: function.Name, Source: call.Source, Line: call.Line, Column: call.Column})
			return nilVal(), diagnostic
		}
		if flow.tail != nil {
			call = flow.tail.call
			function = flow.tail.function
			environment = flow.tail.environment
			receiver = flow.tail.receiver
			arguments = flow.tail.arguments
			typeArguments, valid = executor.kirCallTypeArguments(call, function)
			if !valid {
				return nilVal(), executor.fail(CatArtifact, call.Source, call.Line, call.Column, "KIR tail call has invalid generic arguments for %q", function.Name)
			}
			executor.typeArgs[len(executor.typeArgs)-1] = typeArguments
			continue
		}
		if flow.returned {
			returnType := executor.instantiateKIRType(function.Return)
			if !executor.valueMatchesType(flow.value, returnType) {
				return nilVal(), executor.fail(CatArtifact, call.Source, call.Line, call.Column, "KIR function returned a value that does not match %q", function.Return)
			}
			return cloneValue(flow.value), nil
		}
		returnType := executor.instantiateKIRType(function.Return)
		if returnType != "Nil" {
			return nilVal(), executor.fail(CatArtifact, function.Source, function.Line, function.Column, "KIR function %q completed without returning %q", function.Name, function.Return)
		}
		return nilVal(), nil
	}
}

func (executor *kirExecutor) kirCallTypeArguments(call *KIRExpr, function *KIRFunction) (map[string]string, bool) {
	if call == nil {
		return nil, false
	}
	arguments := make([]string, len(call.GenericArguments))
	for i, argument := range call.GenericArguments {
		arguments[i] = executor.instantiateKIRType(argument)
	}
	receiverType := ""
	if call.Receiver != nil {
		receiverType = executor.instantiateKIRType(call.Receiver.Type)
	}
	return kirExecFunctionTypeSubstitutions(function, arguments, receiverType, executor.document)
}

func (executor *kirExecutor) instantiateKIRType(encoded string) string {
	if len(executor.typeArgs) == 0 {
		return encoded
	}
	merged := make(map[string]string)
	for _, arguments := range executor.typeArgs {
		for name, value := range arguments {
			merged[name] = value
		}
	}
	return substituteKIRType(encoded, merged)
}

// validateKIRExecSubset is the semantic guard for this executor. DecodeKIR
// checks the wire structure and resource limits, while this pass proves the
// scalar type and lexical-binding invariants used below, including branches
// that execution may not visit.
func validateKIRExecSubset(document *KIRDocument) error {
	return validateKIRExecSubsetWithFunctions(document, kirExecFunctions(document), false)
}

func validateKIRExecSubsetWithFunctions(document *KIRDocument, functions map[string]*KIRFunction, allowProcessArgs bool) error {
	if document.Version < 3 || document.Version > KIRVersion {
		return fmt.Errorf("%w: KIR v3 or newer resolved bindings are required", errKIRSubsetUnsupported)
	}
	for _, declaration := range document.Structs {
		if declaration == nil {
			return fmt.Errorf("invalid KIR executable: struct list contains a missing node")
		}
		structScope := newKIRExecScope(nil)
		structScope.types = make(map[string]string, len(declaration.TypeParams))
		for _, parameter := range declaration.TypeParams {
			if parameter == nil || parameter.Name == "" || !kirExecConstraintKnown(parameter.Constraint, document) {
				return fmt.Errorf("invalid KIR executable: struct %q has an invalid type parameter", declaration.Name)
			}
			if _, duplicate := structScope.types[parameter.Name]; duplicate {
				return fmt.Errorf("invalid KIR executable: struct %q repeats type parameter %q", declaration.Name, parameter.Name)
			}
			structScope.types[parameter.Name] = parameter.Constraint
		}
		for _, field := range declaration.Fields {
			if field == nil || !kirExecTypeInScope(field.Type, structScope, document) {
				return fmt.Errorf("%w: struct %q field has unsupported type", errKIRSubsetUnsupported, declaration.Name)
			}
		}
	}
	if len(document.Statements) == 0 && len(document.Functions) == 0 {
		return fmt.Errorf("%w: no executable statements", errKIRSubsetUnsupported)
	}
	if len(document.Statements) == 0 {
		main, _ := kirExecEntryFunction(document, functions)
		if main == nil || main.Name != "main" {
			return fmt.Errorf("%w: programs without top-level statements require a unique main()", errKIRSubsetUnsupported)
		}
		if (main.Return != "Nil" && main.Return != "Result[Nil, String]") || len(main.Params) != 0 {
			return fmt.Errorf("%w: main must be a zero-argument function returning Nil or Result[Nil, String]", errKIRSubsetUnsupported)
		}
	}
	for _, function := range document.Functions {
		if function == nil {
			return fmt.Errorf("invalid KIR executable: function list contains a missing node")
		}
		if function.Unsafe || len(function.Captures) != 0 {
			return fmt.Errorf("%w: unsafe functions and top-level captures are outside the KIR function/closure subset", errKIRSubsetUnsupported)
		}
		if function.Worker && (function.Receiver != "" || function.Trait != "" || len(function.TypeParams) != 0 || len(function.Params) != 0) {
			return fmt.Errorf("%w: workers must be non-generic zero-argument top-level functions", errKIRSubsetUnsupported)
		}
	}
	root := newKIRExecScope(nil)
	root.allowProcessArgs = allowProcessArgs
	if err := validateKIRExecBlock(document.Statements, root, "", functions, document); err != nil {
		return err
	}
	for _, function := range document.Functions {
		functionScope := newKIRExecScope(root)
		if function.Worker {
			functionScope.workerBase = functionScope
		}
		functionScope.types = make(map[string]string, len(function.TypeParams))
		if function.Receiver != "" {
			receiverStruct, receiverParameters, ok := kirExecStructType(document, function.Receiver)
			if !ok {
				return fmt.Errorf("%w: method %q has unsupported receiver type %q", errKIRSubsetUnsupported, function.Name, function.Receiver)
			}
			for _, parameter := range receiverStruct.TypeParams {
				if parameter == nil || parameter.Name == "" || !kirExecConstraintKnown(parameter.Constraint, document) {
					return fmt.Errorf("invalid KIR executable: method %q receiver has invalid type parameter", function.Name)
				}
				argument := receiverParameters[parameter.Name]
				if argument == parameter.Name {
					functionScope.types[argument] = parameter.Constraint
				} else if !kirExecTypeInScope(argument, functionScope, document) || !kirExecConstraintSatisfied(argument, parameter.Constraint, functionScope, document) {
					return fmt.Errorf("invalid KIR executable: method %q receiver type argument %q does not satisfy %s", function.Name, argument, parameter.Constraint)
				}
			}
		}
		for _, parameter := range function.TypeParams {
			if parameter == nil || parameter.Name == "" || !kirExecConstraintKnown(parameter.Constraint, document) {
				return fmt.Errorf("invalid KIR executable: function %q has an invalid type parameter", function.Name)
			}
			if _, duplicate := functionScope.types[parameter.Name]; duplicate {
				return fmt.Errorf("invalid KIR executable: function %q repeats type parameter %q", function.Name, parameter.Name)
			}
			functionScope.types[parameter.Name] = parameter.Constraint
		}
		if function.Receiver != "" {
			selfBinding, valid := kirExecSelfBinding(function)
			if !valid {
				return fmt.Errorf("invalid KIR executable: method %q has inconsistent self binding metadata", function.Name)
			}
			if selfBinding != nil {
				if selfBinding.Type != function.Receiver {
					return fmt.Errorf("invalid KIR executable: method %q self binding type %q does not match receiver %q", function.Name, selfBinding.Type, function.Receiver)
				}
				if err := functionScope.define(selfBinding, nilVal()); err != nil {
					return fmt.Errorf("invalid KIR executable: method %q: %w", function.Name, err)
				}
			}
		}
		if !kirExecTypeInScope(function.Return, functionScope, document) {
			return fmt.Errorf("%w: function %q return type %q", errKIRSubsetUnsupported, function.Name, function.Return)
		}
		for _, parameter := range function.Params {
			if parameter == nil || !kirExecTypeInScope(parameter.Type, functionScope, document) {
				return fmt.Errorf("%w: function %q has an unsupported parameter type", errKIRSubsetUnsupported, function.Name)
			}
			if parameter.Default != nil {
				if err := validateKIRExecExpr(parameter.Default, functionScope, false, functions, document); err != nil {
					return fmt.Errorf("invalid KIR executable: function %q default for %q: %w", function.Name, parameter.Name, err)
				}
				if parameter.Default.Type != parameter.Type {
					return fmt.Errorf("invalid KIR executable: function %q default for %q has type %q, want %q", function.Name, parameter.Name, parameter.Default.Type, parameter.Type)
				}
			}
			if err := functionScope.define(parameter.Binding, nilVal()); err != nil {
				return fmt.Errorf("invalid KIR executable: function %q: %w", function.Name, err)
			}
		}
		if err := validateKIRExecBlock(function.Body, functionScope, function.Return, functions, document); err != nil {
			return fmt.Errorf("invalid KIR executable: function %q: %w", function.Name, err)
		}
		if function.Return != "Nil" && !kirExecBlockReturns(function.Body, document) {
			return fmt.Errorf("invalid KIR executable: function %q can finish without returning %s", function.Name, function.Return)
		}
	}
	return nil
}

func kirExecBlockReturns(statements []*KIRStmt, document *KIRDocument) bool {
	for _, statement := range statements {
		if statement == nil {
			continue
		}
		switch statement.Kind {
		case "return":
			return true
		case "if":
			if len(statement.Else) != 0 && kirExecBlockReturns(statement.Then, document) && kirExecBlockReturns(statement.Else, document) {
				return true
			}
		case "match":
			if kirExecMatchReturns(statement, document) {
				return true
			}
		}
	}
	return false
}

func kirExecMatchReturns(statement *KIRStmt, document *KIRDocument) bool {
	if statement == nil || statement.Scrutinee == nil || len(statement.Arms) == 0 {
		return false
	}
	for _, arm := range statement.Arms {
		if arm == nil || arm.Pattern == nil || !kirExecBlockReturns(arm.Body, document) {
			return false
		}
	}
	if kirExecContainsPattern(statement.Arms, "wildcard", "") {
		return true
	}
	switch statement.Scrutinee.Type {
	case "Bool":
		seenTrue, seenFalse := false, false
		for _, arm := range statement.Arms {
			if arm.Pattern.Kind != "bool" {
				return false
			}
			if arm.Pattern.Bool {
				seenTrue = true
			} else {
				seenFalse = true
			}
		}
		return seenTrue && seenFalse
	case "Nil":
		return len(statement.Arms) == 1 && statement.Arms[0].Pattern.Kind == "nil"
	}
	name, _, ok := parseKIRContainerType(statement.Scrutinee.Type)
	if ok && name == "Option" {
		seenSome, seenNone := false, false
		for _, arm := range statement.Arms {
			if arm.Pattern.Kind != "option" {
				return false
			}
			if arm.Pattern.Present {
				seenSome = true
			} else {
				seenNone = true
			}
		}
		return seenSome && seenNone
	}
	if ok && name == "Result" {
		seenOK, seenErr := false, false
		for _, arm := range statement.Arms {
			if arm.Pattern.Kind != "result" {
				return false
			}
			if arm.Pattern.OK {
				seenOK = true
			} else {
				seenErr = true
			}
		}
		return seenOK && seenErr
	}
	if enumeration := findKIREnum(document, statement.Scrutinee.Type); enumeration != nil {
		seen := make(map[string]bool, len(enumeration.Variants))
		for _, arm := range statement.Arms {
			if arm.Pattern.Kind != "enum" || arm.Pattern.Type != enumeration.Name {
				return false
			}
			seen[arm.Pattern.Variant] = true
		}
		for _, variant := range enumeration.Variants {
			if !seen[variant] {
				return false
			}
		}
		return true
	}
	return false
}

func kirExecContainsPattern(arms []*KIRArm, kind, value string) bool {
	for _, arm := range arms {
		if arm != nil && arm.Pattern != nil && arm.Pattern.Kind == kind && (value == "" || arm.Pattern.Variant == value) {
			return true
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
			if !kirExecTypeInScope(statement.Binding.Type, scope, document) || !compatibleKIRTypes(statement.Binding.Type, statement.Init.Type) {
				return fmt.Errorf("invalid KIR executable: declaration '%s' has incompatible executable type %q", statement.Name, statement.Binding.Type)
			}
			if statement.Annotation != "" && !compatibleKIRTypes(statement.Annotation, statement.Binding.Type) {
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
			binding, ok := scope.find(statement.Target.Binding)
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

func kirExecTraitDefinition(document *KIRDocument, name string) *KIRTrait {
	if document == nil {
		return nil
	}
	for _, trait := range document.Traits {
		if trait != nil && trait.Name == name {
			return trait
		}
	}
	return nil
}

func kirExecTraitMethod(trait *KIRTrait, name string) *KIRTraitMethod {
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

func kirExecTraitImplementation(document *KIRDocument, trait, receiverType string) (*KIRTraitImpl, *KIRTraitImplMethod) {
	if document == nil || trait == "" || receiverType == "" {
		return nil, nil
	}
	for _, implementation := range document.TraitImpls {
		if implementation == nil || implementation.Trait != trait || implementation.For != receiverType {
			continue
		}
		return implementation, nil
	}
	return nil, nil
}

func kirExecTraitImplementationMethod(document *KIRDocument, trait, receiverType, methodName string) (*KIRTraitImpl, *KIRTraitImplMethod) {
	implementation, _ := kirExecTraitImplementation(document, trait, receiverType)
	if implementation == nil {
		return nil, nil
	}
	for _, method := range implementation.Methods {
		if method != nil && method.Name == methodName {
			return implementation, method
		}
	}
	return implementation, nil
}

func kirExecStructType(document *KIRDocument, encoded string) (*KIRStruct, map[string]string, bool) {
	name, arguments, generic := parseKIRContainerType(encoded)
	if !generic {
		name = encoded
	}
	declaration := findKIRStruct(document, name)
	if declaration == nil || len(declaration.TypeParams) != len(arguments) {
		if declaration == nil || len(declaration.TypeParams) != 0 || generic {
			return nil, nil, false
		}
	}
	substitutions := make(map[string]string, len(arguments))
	for i, parameter := range declaration.TypeParams {
		if parameter == nil || parameter.Name == "" {
			return nil, nil, false
		}
		substitutions[parameter.Name] = arguments[i]
	}
	return declaration, substitutions, true
}

func kirExecSelfBinding(function *KIRFunction) (*KIRBinding, bool) {
	var found *KIRBinding
	valid := true
	var visitExpr func(*KIRExpr, int)
	var visitStmts func([]*KIRStmt, int)
	visitExpr = func(expression *KIRExpr, depth int) {
		if expression == nil || depth > 512 || !valid {
			return
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
		if expression.Lambda != nil {
			for _, capture := range expression.Lambda.Captures {
				if capture != nil && capture.Binding != nil && capture.Binding.Name == "self" {
					if found == nil {
						found = capture.Binding
					} else if !sameKIRBinding(found, capture.Binding) {
						valid = false
					}
				}
			}
			visitStmts(expression.Lambda.Body, depth+1)
		}
		for _, child := range []*KIRExpr{expression.Left, expression.Right, expression.Operand, expression.Callee, expression.Base, expression.Receiver} {
			visitExpr(child, depth+1)
		}
		for _, list := range [][]*KIRExpr{expression.Args, expression.Items, expression.MapKeys, expression.Values} {
			for _, child := range list {
				visitExpr(child, depth+1)
			}
		}
	}
	visitStmts = func(statements []*KIRStmt, depth int) {
		if depth > 512 || !valid {
			return
		}
		for _, statement := range statements {
			if statement == nil {
				continue
			}
			for _, expression := range []*KIRExpr{statement.Init, statement.Expr, statement.Target, statement.Value, statement.Cond, statement.Iter, statement.Return, statement.Scrutinee} {
				visitExpr(expression, depth+1)
			}
			for _, nested := range [][]*KIRStmt{statement.Then, statement.Else, statement.Body} {
				visitStmts(nested, depth+1)
			}
			for _, arm := range statement.Arms {
				if arm != nil {
					visitStmts(arm.Body, depth+1)
				}
			}
		}
	}
	if function == nil {
		return nil, false
	}
	visitStmts(function.Body, 0)
	return found, valid
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
	if builtin.Name == "process_args" {
		return false
	}
	if isSandboxAwareFilesystemBuiltin(builtin.Name) {
		return true
	}
	switch builtin.Effects {
	case "pure", "diagnostic", "collections", "json", "crypto", "filesystem", "fs", "environment", "process", "network", "windows", "graphics", "discord", "database", "ffi", "random", "time", "actor", "shared", "concurrency":
		return true
	case "io":
		return builtin.Name == "print" || builtin.Name == "println"
	case "thread":
		return true
	case "async":
		switch builtin.Name {
		case "await", "await_timeout", "yield_now", "sleep_ms":
			return true
		default:
			return false
		}
	case "dispatch":
		return builtin.Name == "poly_register" || builtin.Name == "poly_reorder" || builtin.Name == "poly_dispatch"
	default:
		return false
	}
}

func validateKIRExecExpr(expression *KIRExpr, scope *kirExecScope, allowOutput bool, functions map[string]*KIRFunction, document *KIRDocument) error {
	if expression == nil {
		return fmt.Errorf("invalid KIR executable: missing expression")
	}
	if !kirExecTypeInScope(expression.Type, scope, document) {
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
		binding, bindingScope, ok := scope.findScope(expression.Binding)
		if !ok || !sameKIRBinding(binding.meta, expression.Binding) {
			return fmt.Errorf("invalid KIR executable: variable '%s' references an undeclared binding", expression.Name)
		}
		if scope.workerBase != nil {
			if !scope.workerLocalBinding(bindingScope) && !kirExecWorkerSharedType(binding.meta.Type) {
				return fmt.Errorf("%w: worker captures non-shared global %q", errKIRSubsetUnsupported, binding.meta.Name)
			}
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
			numeric := expression.Operand.Type == "Int" || expression.Operand.Type == "Float"
			if _, unsigned := kirExecUIntBits(expression.Operand.Type); unsigned && expression.Operator == "+" {
				numeric = true
			}
			if constraint, generic := scope.typeParameter(expression.Operand.Type); generic && expression.Operator == "+" {
				numeric = kirExecConstraintImplies(constraint, "Numeric")
			}
			if !numeric || expression.Type != expression.Operand.Type {
				return fmt.Errorf("%w: unary %s on %s", errKIRSubsetUnsupported, expression.Operator, expression.Operand.Type)
			}
		case "~":
			if _, unsigned := kirExecUIntBits(expression.Operand.Type); !unsigned || expression.Type != expression.Operand.Type {
				return fmt.Errorf("%w: unary ~ on %s", errKIRSubsetUnsupported, expression.Operand.Type)
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
			if constraint, generic := scope.typeParameter(left); generic && !kirExecConstraintImplies(constraint, "Comparable") {
				return fmt.Errorf("%w: equality on generic type %q without Comparable constraint", errKIRSubsetUnsupported, left)
			}
			if strings.HasPrefix(left, "fn(") {
				return fmt.Errorf("%w: function values are not comparable", errKIRSubsetUnsupported)
			}
		case "<", "<=", ">", ">=":
			_, unsigned := kirExecUIntBits(left)
			ordered := left == "Int" || left == "Float" || unsigned
			if constraint, generic := scope.typeParameter(left); generic {
				ordered = kirExecConstraintImplies(constraint, "Numeric")
			}
			if !ordered || right != left || expression.Type != "Bool" {
				return fmt.Errorf("%w: ordered comparison of %s and %s", errKIRSubsetUnsupported, left, right)
			}
		case "|", "&", "^":
			if _, unsigned := kirExecUIntBits(left); !unsigned || right != left || expression.Type != left {
				return fmt.Errorf("%w: bitwise operator %s on %s and %s", errKIRSubsetUnsupported, expression.Operator, left, right)
			}
		case "<<", ">>":
			if _, unsigned := kirExecUIntBits(left); !unsigned || right != "Int" || expression.Type != left {
				return fmt.Errorf("%w: shift operator %s on %s and %s", errKIRSubsetUnsupported, expression.Operator, left, right)
			}
		case "+":
			if left == "String" && right == "String" && expression.Type == "String" {
				break
			}
			if left == "Bytes" && right == "Bytes" && expression.Type == "Bytes" {
				break
			}
			if name, _, ok := parseKIRContainerType(left); ok && name == "Array" && right == left && expression.Type == left {
				break
			}
			fallthrough
		case "-", "*", "/", "%":
			_, unsigned := kirExecUIntBits(left)
			numeric := left == "Int" || left == "Float" || unsigned
			integer := left == "Int" || unsigned
			if constraint, generic := scope.typeParameter(left); generic {
				if expression.Operator == "%" {
					integer = kirExecConstraintImplies(constraint, "Integer")
					numeric = integer
				} else {
					numeric = kirExecConstraintImplies(constraint, "Numeric")
				}
			}
			if left != right || !numeric || expression.Type != left || (expression.Operator == "%" && !integer) {
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
		declaration, substitutions, valid := kirExecStructType(document, expression.Type)
		if !valid || declaration.Name != expression.StructName || expression.StructType != expression.Type || len(expression.Fields) != len(expression.Values) || len(expression.Fields) != len(declaration.Fields) {
			return fmt.Errorf("invalid KIR executable: struct literal has an unknown or mismatched declaration")
		}
		for _, parameter := range declaration.TypeParams {
			argument := substitutions[parameter.Name]
			if !kirExecTypeInScope(argument, scope, document) || !kirExecConstraintSatisfied(argument, parameter.Constraint, scope, document) {
				return fmt.Errorf("invalid KIR executable: struct type argument %q does not satisfy %s for %q", argument, parameter.Constraint, parameter.Name)
			}
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
			if expression.Values[i].Type != substituteKIRType(want, substitutions) {
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
		declaration, substitutions, valid := kirExecStructType(document, expression.Base.Type)
		if !valid {
			return fmt.Errorf("%w: field access on %q", errKIRSubsetUnsupported, expression.Base.Type)
		}
		found := false
		for _, field := range declaration.Fields {
			if field.Name == expression.Field {
				found = true
				if expression.Type != substituteKIRType(field.Type, substitutions) {
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
		if expression.Callee != nil {
			if expression.Name != "" || expression.CallTarget != "" || expression.BuiltinID != "" || expression.Receiver != nil || len(expression.GenericArguments) != 0 {
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
			if function == nil || function.Name != expression.Name || len(expression.Args) < kirExecRequiredParams(function.Params) || len(expression.Args) > len(function.Params) {
				return fmt.Errorf("invalid KIR executable: direct function call has a mismatched target or signature")
			}
			if (function.Receiver != "") != (expression.Receiver != nil) {
				return fmt.Errorf("invalid KIR executable: method call receiver metadata does not match its target")
			}
			if expression.BuiltinID != "" || expression.TraitName != "" {
				return fmt.Errorf("invalid KIR executable: direct function call contains builtin or trait metadata")
			}
			receiverType := ""
			if expression.Receiver != nil {
				if err := validateKIRExecExpr(expression.Receiver, scope, false, functions, document); err != nil {
					return err
				}
				receiverType = expression.Receiver.Type
			}
			substitutions, valid := kirExecFunctionTypeSubstitutions(function, expression.GenericArguments, receiverType, document)
			if !valid {
				return fmt.Errorf("invalid KIR executable: call to %q has mismatched receiver or generic arguments", function.Name)
			}
			for i, parameter := range function.TypeParams {
				argument := expression.GenericArguments[i]
				if !kirExecTypeInScope(argument, scope, document) {
					return fmt.Errorf("%w: generic argument %q to %q", errKIRSubsetUnsupported, argument, function.Name)
				}
				if !kirExecConstraintSatisfied(argument, parameter.Constraint, scope, document) {
					return fmt.Errorf("invalid KIR executable: generic argument %q does not satisfy %s for %q", argument, parameter.Constraint, parameter.Name)
				}
			}
			if function.Receiver != "" {
				receiverStruct, _, _ := kirExecStructType(document, function.Receiver)
				for _, parameter := range receiverStruct.TypeParams {
					argument := substitutions[parameter.Name]
					if !kirExecTypeInScope(argument, scope, document) || !kirExecConstraintSatisfied(argument, parameter.Constraint, scope, document) {
						return fmt.Errorf("invalid KIR executable: receiver type argument %q does not satisfy %s for %q", argument, parameter.Constraint, parameter.Name)
					}
				}
			}
			if expression.Type != substituteKIRType(function.Return, substitutions) {
				return fmt.Errorf("invalid KIR executable: call to %q has mismatched result type", function.Name)
			}
			for i, argument := range expression.Args {
				if err := validateKIRExecExpr(argument, scope, false, functions, document); err != nil {
					return err
				}
				if argument.Type != substituteKIRType(function.Params[i].Type, substitutions) {
					return fmt.Errorf("invalid KIR executable: call to %q has mismatched argument %d", function.Name, i+1)
				}
			}
			break
		}
		if prefix == "trait" {
			traitName, methodName, validTarget := strings.Cut(target, "::")
			trait := kirExecTraitDefinition(document, traitName)
			method := kirExecTraitMethod(trait, methodName)
			if !validTarget || traitName == "" || methodName == "" || strings.Contains(methodName, "::") || method == nil || expression.CallTarget != "trait:"+traitName+"::"+methodName || expression.Name != methodName || expression.TraitName != traitName || expression.Receiver == nil || expression.BuiltinID != "" || len(expression.GenericArguments) != 0 || len(expression.Args) != len(method.Params) || expression.Type != method.Return {
				return fmt.Errorf("invalid KIR executable: trait method call has mismatched target or signature")
			}
			if err := validateKIRExecExpr(expression.Receiver, scope, false, functions, document); err != nil {
				return err
			}
			if constraint, generic := scope.typeParameter(expression.Receiver.Type); generic {
				if !kirExecConstraintImplies(constraint, traitName) {
					return fmt.Errorf("invalid KIR executable: generic receiver %q is not bounded by trait %q", expression.Receiver.Type, traitName)
				}
			} else {
				implementation, _ := kirExecTraitImplementation(document, traitName, expression.Receiver.Type)
				if implementation == nil {
					return fmt.Errorf("invalid KIR executable: trait %q has no implementation for receiver type %q", traitName, expression.Receiver.Type)
				}
			}
			for i, argument := range expression.Args {
				if err := validateKIRExecExpr(argument, scope, false, functions, document); err != nil {
					return err
				}
				if method.Params[i] == nil || argument.Type != method.Params[i].Type {
					return fmt.Errorf("invalid KIR executable: trait method %q argument %d has a mismatched type", methodName, i+1)
				}
			}
			break
		}
		if prefix != "builtin" {
			return fmt.Errorf("%w: unknown call target %q", errKIRSubsetUnsupported, prefix)
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
		if scope.workerBase != nil && (isSandboxAwareFilesystemBuiltin(expression.Name) || strings.HasPrefix(expression.Name, "fs_") || expression.Name == "env_get" || expression.Name == "thread_spawn" || expression.Name == "task_spawn") {
			return fmt.Errorf("%w: builtin %q is unavailable in a worker", errKIRSubsetUnsupported, expression.Name)
		}
		if (expression.Name == "print" || expression.Name == "println") && expression.Type != "Nil" {
			return fmt.Errorf("invalid KIR executable: output builtin has a non-Nil type")
		}
		if expression.Name == "str" && expression.Type != "String" {
			return fmt.Errorf("invalid KIR executable: str call has invalid result type")
		}
		if !kirExecBuiltinSupported(builtin) && !(scope.allowProcessArgs && builtin.Name == "process_args") && builtin.Name != "thread_spawn" && builtin.Name != "task_spawn" {
			return fmt.Errorf("%w: builtin %q has host effect %q outside the KIR executor capability boundary", errKIRSubsetUnsupported, expression.Name, builtin.Effects)
		}
		if expression.Name == "thread_spawn" || expression.Name == "task_spawn" {
			nameIndex := 0
			if expression.Name == "task_spawn" {
				nameIndex = 1
			}
			nameArgument := expression.Args[nameIndex]
			if nameArgument.Kind != "string" {
				return fmt.Errorf("%w: worker spawn requires a literal function name", errKIRSubsetUnsupported)
			}
			var worker *KIRFunction
			for _, candidate := range document.Functions {
				if candidate != nil && candidate.Name == nameArgument.String {
					if worker != nil {
						return fmt.Errorf("%w: worker name %q is overloaded", errKIRSubsetUnsupported, nameArgument.String)
					}
					worker = candidate
				}
			}
			if worker == nil || !worker.Worker || len(worker.Params) != 0 || worker.Receiver != "" || worker.Trait != "" || len(worker.TypeParams) != 0 || expression.Type != "Thread["+worker.Return+"]" {
				return fmt.Errorf("invalid KIR executable: worker spawn target or result type does not match")
			}
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
		captureScope.workerBase = scope.workerBase
		for _, capture := range function.Captures {
			if capture == nil || !validKIRBinding(capture.Binding) {
				return fmt.Errorf("invalid KIR executable: lambda has invalid capture metadata")
			}
			visible, visibleScope, ok := scope.findScope(capture.Binding)
			if !ok || !sameKIRBinding(visible.meta, capture.Binding) {
				return fmt.Errorf("invalid KIR executable: lambda captures unavailable binding %q", capture.Binding.Name)
			}
			if scope.workerBase != nil && !scope.workerLocalBinding(visibleScope) && !kirExecWorkerSharedType(visible.meta.Type) {
				return fmt.Errorf("%w: worker lambda captures non-shared global %q", errKIRSubsetUnsupported, visible.meta.Name)
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
		if function.Return != "Nil" && !kirExecBlockReturns(function.Body, document) {
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
		_, unsigned := kirExecUIntBits(typ)
		return unsigned
	}
}

func kirExecUIntBits(typ string) (uint8, bool) {
	switch typ {
	case "UInt8":
		return 8, true
	case "UInt16":
		return 16, true
	case "UInt32":
		return 32, true
	case "UInt64":
		return 64, true
	default:
		return 0, false
	}
}

func kirExecType(typ string) bool {
	if kirExecScalarType(typ) {
		return true
	}
	if typ == "Bytes" || typ == "Json" || kirExecOpaqueType(typ) {
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
	if len(arguments) != want || !kirExecContainerTypeName(name) {
		return false
	}
	if name == "Array" && arguments[0] == "<unknown>" {
		return true
	}
	for _, argument := range arguments {
		if !kirExecType(argument) {
			return false
		}
	}
	return true
}

func kirExecContainerTypeName(name string) bool {
	switch name {
	case "Array", "Option", "Result", "Set", "Map", "Channel", "Thread", "Actor", "Shared":
		return true
	default:
		return false
	}
}

func kirExecOpaqueType(name string) bool {
	switch name {
	case "SQLite", "WebSocket", "Regex", "Random", "TcpSocket", "TcpListener", "UdpSocket", "FFILibrary", "FFISymbol", "FFIBuffer", "TaskGroup":
		return true
	default:
		return false
	}
}

func kirExecWorkerSharedType(encoded string) bool {
	name, arguments, ok := parseKIRContainerType(encoded)
	return ok && len(arguments) == 1 && (name == "Channel" || name == "Shared")
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
	return kirExecTypeInScope(encoded, nil, document)
}

func kirExecTypeInScope(encoded string, scope *kirExecScope, document *KIRDocument) bool {
	if kirExecType(encoded) {
		return true
	}
	if _, ok := scope.typeParameter(encoded); ok {
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
		if !kirExecTypeInScope(result, scope, document) {
			return false
		}
		for _, parameter := range parameters {
			if !kirExecTypeInScope(parameter, scope, document) {
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
	if len(arguments) == want && kirExecContainerTypeName(name) {
		for _, argument := range arguments {
			if !kirExecTypeInScope(argument, scope, document) {
				return false
			}
		}
		return true
	}
	declaration := findKIRStruct(document, name)
	if declaration == nil || len(declaration.TypeParams) != len(arguments) {
		return false
	}
	for i, argument := range arguments {
		parameter := declaration.TypeParams[i]
		if parameter == nil || !kirExecTypeInScope(argument, scope, document) || !kirExecConstraintSatisfied(argument, parameter.Constraint, scope, document) {
			return false
		}
	}
	return true
}

func kirFunctionTypeArguments(function *KIRFunction, arguments []string) (map[string]string, bool) {
	if function == nil || len(function.TypeParams) != len(arguments) {
		return nil, false
	}
	substitutions := make(map[string]string, len(arguments))
	for i, parameter := range function.TypeParams {
		if parameter == nil || parameter.Name == "" || arguments[i] == "" {
			return nil, false
		}
		if _, duplicate := substitutions[parameter.Name]; duplicate {
			return nil, false
		}
		substitutions[parameter.Name] = arguments[i]
	}
	return substitutions, true
}

func kirExecFunctionTypeSubstitutions(function *KIRFunction, genericArguments []string, receiverType string, document *KIRDocument) (map[string]string, bool) {
	substitutions, ok := kirFunctionTypeArguments(function, genericArguments)
	if !ok {
		return nil, false
	}
	if function.Receiver == "" {
		return substitutions, receiverType == ""
	}
	if receiverType == "" {
		return nil, false
	}
	patternStruct, patternArguments, ok := kirExecStructType(document, function.Receiver)
	if !ok {
		return nil, false
	}
	actualStruct, actualArguments, ok := kirExecStructType(document, receiverType)
	if !ok || actualStruct.Name != patternStruct.Name {
		return nil, false
	}
	variables := make(map[string]bool, len(function.TypeParams)+len(patternStruct.TypeParams))
	for _, parameter := range function.TypeParams {
		variables[parameter.Name] = true
	}
	for _, parameter := range patternStruct.TypeParams {
		variables[parameter.Name] = true
	}
	for _, parameter := range patternStruct.TypeParams {
		if !kirExecUnifyTypePattern(patternArguments[parameter.Name], actualArguments[parameter.Name], variables, substitutions) {
			return nil, false
		}
	}
	return substitutions, true
}

func kirExecUnifyTypePattern(pattern, actual string, variables map[string]bool, substitutions map[string]string) bool {
	if variables[pattern] {
		if previous := substitutions[pattern]; previous != "" {
			return previous == actual
		}
		substitutions[pattern] = actual
		return true
	}
	patternName, patternArguments, patternGeneric := parseKIRContainerType(pattern)
	actualName, actualArguments, actualGeneric := parseKIRContainerType(actual)
	if patternGeneric || actualGeneric {
		if !patternGeneric || !actualGeneric || patternName != actualName || len(patternArguments) != len(actualArguments) {
			return false
		}
		for i := range patternArguments {
			if !kirExecUnifyTypePattern(patternArguments[i], actualArguments[i], variables, substitutions) {
				return false
			}
		}
		return true
	}
	return pattern == actual
}

func kirExecConstraintKnown(constraint string, document *KIRDocument) bool {
	switch constraint {
	case "", "Any", "Copy", "Integer", "Numeric", "Comparable":
		return true
	default:
		return kirExecTraitDefinition(document, constraint) != nil
	}
}

func kirExecConstraintImplies(provided, required string) bool {
	if required == "" || required == "Any" || provided == required {
		return true
	}
	switch provided {
	case "Integer":
		return required == "Copy" || required == "Numeric" || required == "Comparable"
	case "Numeric":
		return required == "Copy" || required == "Comparable"
	case "Comparable":
		return required == "Copy"
	default:
		return false
	}
}

func kirExecConstraintSatisfied(encoded, constraint string, scope *kirExecScope, document *KIRDocument) bool {
	if provided, ok := scope.typeParameter(encoded); ok {
		return kirExecConstraintImplies(provided, constraint)
	}
	return kirExecTypeSatisfiesConstraint(encoded, constraint, scope, document, make(map[string]bool))
}

func kirExecTypeSatisfiesConstraint(encoded, constraint string, scope *kirExecScope, document *KIRDocument, visiting map[string]bool) bool {
	if provided, ok := scope.typeParameter(encoded); ok {
		return kirExecConstraintImplies(provided, constraint)
	}
	switch constraint {
	case "", "Any":
		return kirExecTypeInScope(encoded, scope, document)
	case "Numeric":
		_, unsigned := kirExecUIntBits(encoded)
		return encoded == "Int" || encoded == "Float" || unsigned
	case "Integer":
		_, unsigned := kirExecUIntBits(encoded)
		return encoded == "Int" || unsigned
	case "Comparable":
		_, unsigned := kirExecUIntBits(encoded)
		if encoded == "Int" || unsigned || encoded == "Float" || encoded == "Bool" || encoded == "String" || encoded == "Nil" || encoded == "Bytes" {
			return true
		}
		return findKIREnum(document, encoded) != nil
	case "Copy":
		_, unsigned := kirExecUIntBits(encoded)
		if encoded == "Int" || unsigned || encoded == "Float" || encoded == "Bool" || encoded == "String" || encoded == "Nil" || encoded == "Bytes" || encoded == "Json" || findKIREnum(document, encoded) != nil {
			return true
		}
		if _, _, ok := parseKIRFunctionType(encoded); ok {
			return false
		}
		if name, arguments, ok := parseKIRContainerType(encoded); ok {
			want := 1
			if name == "Map" || name == "Result" {
				want = 2
			}
			if len(arguments) != want {
				return false
			}
			switch name {
			case "Array", "Option", "Set":
				return kirExecTypeSatisfiesConstraint(arguments[0], "Copy", scope, document, visiting)
			case "Map", "Result":
				return kirExecTypeSatisfiesConstraint(arguments[0], "Copy", scope, document, visiting) && kirExecTypeSatisfiesConstraint(arguments[1], "Copy", scope, document, visiting)
			}
			declaration, substitutions, valid := kirExecStructType(document, encoded)
			if !valid || visiting[encoded] {
				return false
			}
			visiting[encoded] = true
			defer delete(visiting, encoded)
			for _, field := range declaration.Fields {
				if field == nil || !kirExecTypeSatisfiesConstraint(substituteKIRType(field.Type, substitutions), "Copy", scope, document, visiting) {
					return false
				}
			}
			return true
		}
		if declaration := findKIRStruct(document, encoded); declaration != nil && len(declaration.TypeParams) == 0 {
			if visiting[encoded] {
				return false
			}
			visiting[encoded] = true
			defer delete(visiting, encoded)
			for _, field := range declaration.Fields {
				if field == nil || !kirExecTypeSatisfiesConstraint(field.Type, "Copy", scope, document, visiting) {
					return false
				}
			}
			return true
		}
	}
	if kirExecTraitDefinition(document, constraint) != nil {
		implementation, _ := kirExecTraitImplementation(document, constraint, encoded)
		return implementation != nil
	}
	return false
}

func (executor *kirExecutor) initializeKIRTypes(document *KIRDocument) error {
	for _, declaration := range document.Structs {
		structDecl := executor.structs[declaration.Name]
		if structDecl == nil {
			structDecl = &StructDecl{Name: declaration.Name}
			executor.structs[declaration.Name] = structDecl
		}
		structDecl.TypeParams = make([]TypeParam, 0, len(declaration.TypeParams))
		structDecl.Fields = nil
		for _, parameter := range declaration.TypeParams {
			structDecl.TypeParams = append(structDecl.TypeParams, TypeParam{Name: parameter.Name, Constraint: parameter.Constraint})
		}
		typ := executor.types[declaration.Name]
		if typ == nil || typ.Kind != TyStruct || typ.Struct != structDecl {
			typ = &Type{Kind: TyStruct, Name: declaration.Name, Struct: structDecl}
		} else {
			typ.Name = declaration.Name
			typ.Struct = structDecl
		}
		structDecl.Type = typ
		executor.types[declaration.Name] = typ
	}
	for _, declaration := range document.Enums {
		enumDecl := executor.enums[declaration.Name]
		if enumDecl == nil {
			enumDecl = &EnumDecl{Name: declaration.Name}
			executor.enums[declaration.Name] = enumDecl
		}
		enumDecl.Variants = append(enumDecl.Variants[:0], declaration.Variants...)
		typ := executor.types[declaration.Name]
		if typ == nil || typ.Kind != TyEnum || typ.Enum != enumDecl {
			typ = &Type{Kind: TyEnum, Name: declaration.Name, Enum: enumDecl}
		} else {
			typ.Name = declaration.Name
			typ.Enum = enumDecl
		}
		enumDecl.Type = typ
		executor.types[declaration.Name] = typ
	}
	for _, declaration := range document.Structs {
		structDecl := executor.structs[declaration.Name]
		typeParameters := make(map[string]*Type, len(declaration.TypeParams))
		for _, parameter := range declaration.TypeParams {
			typeParameters[parameter.Name] = Generic(parameter.Name, parameter.Constraint)
		}
		for _, field := range declaration.Fields {
			typ, ok := executor.resolveKIRTypeWithTypeParameters(field.Type, typeParameters)
			if !ok {
				return fmt.Errorf("invalid KIR executable: struct %q field %q has unsupported type %q", declaration.Name, field.Name, field.Type)
			}
			structDecl.Fields = append(structDecl.Fields, FieldDecl{Name: field.Name, Type: typ})
		}
	}
	return nil
}

func (executor *kirExecutor) resolveKIRType(encoded string) (*Type, bool) {
	return executor.resolveKIRTypeWithTypeParameters(encoded, nil)
}

func (executor *kirExecutor) resolveKIRTypeWithTypeParameters(encoded string, typeParameters map[string]*Type) (*Type, bool) {
	encoded = executor.instantiateKIRType(encoded)
	if typ := typeParameters[encoded]; typ != nil {
		return typ, true
	}
	if typ := executor.types[encoded]; typ != nil {
		return typ, true
	}
	switch encoded {
	case "Nil":
		return TNil, true
	case "Int":
		return TInt, true
	case "UInt8":
		return TUInt8, true
	case "UInt16":
		return TUInt16, true
	case "UInt32":
		return TUInt32, true
	case "UInt64":
		return TUInt64, true
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
	case "SQLite":
		return &Type{Kind: TySQLite, Name: "SQLite"}, true
	case "WebSocket":
		return &Type{Kind: TyWebSocket, Name: "WebSocket"}, true
	case "Regex":
		return &Type{Kind: TyRegex, Name: "Regex"}, true
	case "Random":
		return &Type{Kind: TyRandom, Name: "Random"}, true
	case "TcpSocket":
		return &Type{Kind: TyTCPSocket, Name: "TcpSocket"}, true
	case "TcpListener":
		return &Type{Kind: TyTCPListener, Name: "TcpListener"}, true
	case "UdpSocket":
		return &Type{Kind: TyUDPSocket, Name: "UdpSocket"}, true
	case "FFILibrary":
		return &Type{Kind: TyFFILibrary, Name: "FFILibrary"}, true
	case "FFISymbol":
		return &Type{Kind: TyFFISymbol, Name: "FFISymbol"}, true
	case "FFIBuffer":
		return &Type{Kind: TyFFIBuffer, Name: "FFIBuffer"}, true
	case "TaskGroup":
		return TTaskGroup, true
	}
	if parameters, result, ok := parseKIRFunctionType(encoded); ok {
		params := make([]*Type, len(parameters))
		for i, parameter := range parameters {
			parsed, ok := executor.resolveKIRTypeWithTypeParameters(parameter, typeParameters)
			if !ok {
				return nil, false
			}
			params[i] = parsed
		}
		resultType, ok := executor.resolveKIRTypeWithTypeParameters(result, typeParameters)
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
		parsed[i], ok = executor.resolveKIRTypeWithTypeParameters(argument, typeParameters)
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
	case "Channel":
		return Chan(parsed[0]), true
	case "Thread":
		return TypeThread(parsed[0]), true
	case "Actor":
		return ActorOf(parsed[0]), true
	case "Shared":
		return SharedOf(parsed[0]), true
	}
	if declaration := executor.structs[name]; declaration != nil && len(declaration.TypeParams) == len(parsed) {
		for i, parameter := range declaration.TypeParams {
			if !satisfiesConstraint(parsed[i], parameter.Constraint) {
				return nil, false
			}
		}
		return &Type{Kind: TyStruct, Name: name, Struct: declaration, Params: parsed}, true
	}
	return nil, false
}

type kirExecOutputWriter struct{ executor *kirExecutor }

func (writer kirExecOutputWriter) Write(output []byte) (int, error) {
	if diagnostic := writer.executor.emitOutput(string(output), "", 1, 1); diagnostic != nil {
		return 0, fmt.Errorf("%s", diagnostic.Message)
	}
	return len(output), nil
}

func (executor *kirExecutor) emitOutput(output, source string, line, column int) *Diagnostic {
	if executor.outputMu != nil {
		executor.outputMu.Lock()
		defer executor.outputMu.Unlock()
	}
	if int64(len(output)) > executor.limits.MaxOutputBytes-executor.context.Output {
		return executor.fail(CatResource, source, line, column, "output limit exceeded")
	}
	if executor.outputWriter != nil {
		written, err := io.WriteString(executor.outputWriter, output)
		if err != nil || written != len(output) {
			return executor.fail(CatIO, source, line, column, "stream failure")
		}
	}
	executor.output = append(executor.output, output...)
	executor.context.Output += int64(len(output))
	return nil
}

func (executor *kirExecutor) debugKIRStatement(scope *kirExecScope, statement *KIRStmt) bool {
	if executor.debugger == nil || executor.debugger.ShouldPause == nil || executor.debugger.OnPause == nil {
		return true
	}
	sourceName := statement.Source
	if sourceName == "" {
		sourceName = "<input>"
	}
	function := "<module>"
	if count := len(executor.callFrames); count != 0 {
		function = executor.callFrames[count-1].Function
	}
	location := DebugLocation{Source: sourceName, Line: statement.Line, Column: statement.Column, Function: function, Depth: executor.context.Calls}
	if statement.Return != nil && statement.Return.Kind == "call" && statement.Return.Tail {
		location.TailCall = true
	}
	if !executor.debugger.ShouldPause(location) {
		return true
	}
	state := DebugState{DebugLocation: location}
	if source := executor.source(statement.Source); source != nil {
		lines := strings.Split(source.Text, "\n")
		if location.Line > 0 && location.Line <= len(lines) {
			state.LineText = strings.TrimSuffix(lines[location.Line-1], "\r")
		}
	}
	if len(executor.callFrames) == 0 {
		state.Stack = []StackFrame{{Function: function, Source: sourceName, Line: location.Line, Column: location.Column}}
	} else {
		state.Stack = make([]StackFrame, 0, len(executor.callFrames))
		for i := len(executor.callFrames) - 1; i >= 0; i-- {
			state.Stack = append(state.Stack, executor.callFrames[i])
		}
		state.Stack[0].Source, state.Stack[0].Line, state.Stack[0].Column = sourceName, location.Line, location.Column
	}
	seen := make(map[string]struct{})
	for current := scope; current != nil; current = current.parent {
		for _, binding := range current.values {
			if binding == nil || binding.meta == nil {
				continue
			}
			name := binding.meta.Name
			if _, shadowed := seen[name]; shadowed {
				continue
			}
			seen[name] = struct{}{}
			state.Variables = append(state.Variables, DebugVariable{Name: name, Value: display(binding.value), Mutable: binding.meta.Mutable})
		}
	}
	sort.Slice(state.Variables, func(i, j int) bool { return state.Variables[i].Name < state.Variables[j].Name })
	if executor.debugger.OnPause(state) {
		return true
	}
	executor.debugStopped = true
	return false
}

func (executor *kirExecutor) initializeKIRBuiltins(document *KIRDocument, sandbox Sandbox) {
	program := &Program{}
	env := &TypeEnv{Types: executor.types, Functions: map[string]*Function{}, Overloads: map[string][]*Function{}, Traits: map[string]*TraitDecl{}, TraitImpls: map[string]map[string]*TraitImplDecl{}, Builtins: Builtins(), TypeParams: map[string]*Type{}, Lim: executor.limits}
	checker := &Checker{Prog: program, Env: env, Lim: executor.limits}
	runtime := &Runtime{Prog: program, Checker: checker, Funcs: map[string]*Function{}, Global: newRunScope(nil), Lim: executor.limits, Sandbox: sandbox, Ctx: executor.context, Dispatch: map[string][]DispatchEntry{}, discordRates: newDiscordRateLimiter(), discordCache: newDiscordObjectCache(10_000, 30*time.Minute), discordAPIBaseURL: discordAPIBase, discordGateway: newDiscordGatewayState()}
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

func (executor *kirExecutor) execBlock(scope *kirExecScope, statements []*KIRStmt, topLevel bool, replMode ...bool) (kirExecFlow, *Diagnostic) {
	flow := kirExecFlow{value: nilVal()}
	var resultDiagnostic *Diagnostic
	repl := len(replMode) != 0 && replMode[0]
	for _, statement := range statements {
		if !executor.debugKIRStatement(scope, statement) {
			resultDiagnostic = executor.fail(CatCLI, statement.Source, statement.Line, statement.Column, "execution stopped by debugger")
			break
		}
		if diagnostic := executor.step(statement.Source, statement.Line, statement.Column); diagnostic != nil {
			resultDiagnostic = diagnostic
			break
		}
		statementFlow, diagnostic := executor.execStmt(scope, statement)
		if repl && diagnostic == nil && statement.Kind == "expr" && statement.Expr != nil {
			if statementFlow.value.Kind != VNil {
				diagnostic = executor.emitOutput(executor.displayValue(statementFlow.value)+"\n", statement.Source, statement.Line, statement.Column)
			}
		}
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
		binding, ok := scope.find(target.Binding)
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
		value, diagnostic := executor.evalExpr(scope, statement.Expr)
		if propagated := executor.takeKIRPropagated(); propagated != nil {
			return kirExecFlow{value: *propagated, returned: true}, nil
		}
		return kirExecFlow{value: value}, diagnostic
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
			executor.registerKIRClosure(identity, &kirClosure{function: function, environment: executor.global})
			value = Value{Kind: VFunction, Callable: identity}
			break
		}
		binding, ok := scope.find(expression.Binding)
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
		if expression.Operator == "+" && left.Kind == VArray && right.Kind == VArray && arrayLength(left) > executor.limits.MaxArrayElements-arrayLength(right) {
			return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "array size limit exceeded")
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
			if left.Kind == VUInt && right.Kind == VUInt {
				if left.UBits != right.UBits {
					return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "bitwise and unsigned arithmetic operands must have matching UInt widths")
				}
				if (expression.Operator == "/" || expression.Operator == "%") && right.U == 0 {
					if expression.Operator == "%" {
						return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "remainder by zero")
					}
					return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "division by zero")
				}
			}
			if left.Kind == VUInt && right.Kind == VInt && (expression.Operator == "<<" || expression.Operator == ">>") && (right.I < 0 || uint64(right.I) >= uint64(left.UBits)) {
				return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "shift count must be between 0 and UInt width minus one")
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
		binding, ok := scope.find(capture.Binding)
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
		environment.values[kirExecKey(capture.Binding)] = cell
	}
	identity := &FunctionValue{}
	executor.registerKIRClosure(identity, &kirClosure{function: function, environment: environment})
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
		closure := executor.findKIRClosure(value.Callable)
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
	if prefix == "trait" {
		return nil, executor.global, false, nil
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

func kirExecRuntimeReceiverType(receiver Value) string {
	if receiver.Kind == VStruct && receiver.StructType != nil {
		return receiver.StructType.String()
	}
	return ""
}

func (executor *kirExecutor) resolveKIRTraitCall(expression *KIRExpr, receiver *Value) (*KIRFunction, *KIRExpr, *Diagnostic) {
	if expression == nil || receiver == nil || expression.Receiver == nil {
		return nil, nil, executor.fail(CatArtifact, "", 0, 0, "KIR trait call has no evaluated receiver")
	}
	prefix, target, ok := strings.Cut(expression.CallTarget, ":")
	traitName, methodName, validMethod := strings.Cut(target, "::")
	if !ok || prefix != "trait" || !validMethod || traitName == "" || methodName == "" || expression.TraitName != traitName || expression.Name != methodName {
		return nil, nil, executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR trait call has malformed dispatch metadata")
	}
	receiverType := kirExecRuntimeReceiverType(*receiver)
	if receiverType == "" {
		return nil, nil, executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR static trait dispatch requires a struct receiver")
	}
	_, method := kirExecTraitImplementationMethod(executor.document, traitName, receiverType, methodName)
	if method == nil {
		return nil, nil, executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR trait %q has no %q implementation for receiver type %q", traitName, methodName, receiverType)
	}
	prefix, functionTarget, ok := strings.Cut(method.Target, ":")
	if !ok || prefix != "function" || functionTarget == "" {
		return nil, nil, executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR trait implementation has an invalid function target")
	}
	function := executor.functions[functionTarget]
	if function == nil || function.Trait != traitName || function.Name != methodName || function.Receiver != receiverType {
		return nil, nil, executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR trait implementation target does not match %q for %q", traitName, receiverType)
	}
	invocation := *expression
	invocation.CallTarget = method.Target
	invocation.TraitName = ""
	receiverExpression := *expression.Receiver
	receiverExpression.Type = receiverType
	invocation.Receiver = &receiverExpression
	return function, &invocation, nil
}

func (executor *kirExecutor) evalKIRCall(scope *kirExecScope, expression *KIRExpr) (Value, *Diagnostic) {
	function, environment, builtin, diagnostic := executor.resolveKIRCall(scope, expression)
	if diagnostic != nil {
		return nilVal(), diagnostic
	}
	if builtin {
		if expression.Receiver != nil {
			return nilVal(), executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR builtin call unexpectedly has a receiver")
		}
		if executor.builtins == nil {
			return nilVal(), executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR builtin %q passed validation without an executable implementation", expression.Name)
		}
		metadata, exists := Builtins()[expression.Name]
		if !exists || (!kirExecBuiltinSupported(metadata) && !(scope.allowProcessArgs && expression.Name == "process_args")) {
			return nilVal(), executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR builtin %q passed validation without an executable implementation", expression.Name)
		}
		if !executor.builtins.Sandbox.allowsBuiltin(metadata) {
			return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "builtin %q is unavailable with --restricted", expression.Name)
		}
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
		switch expression.Name {
		case "poly_register", "poly_reorder", "poly_dispatch":
			return executor.evalKIRDispatch(expression, arguments)
		case "thread_spawn":
			return executor.spawnKIRWorker(expression, arguments[0].S)
		case "task_spawn":
			return executor.spawnKIRTask(expression, arguments)
		}
		if expression.Name == "print" || expression.Name == "println" {
			if len(arguments) != 1 {
				return nilVal(), executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR output builtin has invalid arity")
			}
			text := executor.displayValue(arguments[0])
			if expression.Name == "println" {
				text += "\n"
			}
			if diagnostic := executor.emitOutput(text, expression.Source, expression.Line, expression.Column); diagnostic != nil {
				return nilVal(), diagnostic
			}
			return nilVal(), nil
		}
		call := &Expr{Kind: ExCall, Name: expression.Name, Tok: Token{Source: executor.source(expression.Source), Line: expression.Line, Column: expression.Column}}
		return executor.builtins.evalBuiltin(call, metadata, arguments)
	}
	if executor.context.Calls >= executor.limits.MaxCallDepth {
		return nilVal(), executor.fail(CatResource, expression.Source, expression.Line, expression.Column, "call depth limit exceeded")
	}
	var receiver *Value
	if expression.Receiver != nil {
		value, receiverDiagnostic := executor.evalExpr(scope, expression.Receiver)
		if receiverDiagnostic != nil {
			return nilVal(), receiverDiagnostic
		}
		receiver = &value
	}
	arguments := make([]Value, len(expression.Args))
	for i, argument := range expression.Args {
		arguments[i], diagnostic = executor.evalExpr(scope, argument)
		if diagnostic != nil {
			return nilVal(), diagnostic
		}
	}
	invokeCall := expression
	if strings.HasPrefix(expression.CallTarget, "trait:") {
		function, invokeCall, diagnostic = executor.resolveKIRTraitCall(expression, receiver)
		if diagnostic != nil {
			return nilVal(), diagnostic
		}
		environment = executor.global
	}
	return executor.invokeKIR(invokeCall, function, environment, receiver, arguments)
}

func (executor *kirExecutor) targetForKIRFunction(function *KIRFunction) string {
	for target, candidate := range executor.functions {
		if candidate == function {
			return target
		}
	}
	return ""
}

func (executor *kirExecutor) evalKIRDispatch(expression *KIRExpr, arguments []Value) (Value, *Diagnostic) {
	if executor.builtins == nil {
		return nilVal(), executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR dispatch builtin has no runtime state")
	}
	bad := func(message string) (Value, *Diagnostic) { return resVal(false, stringVal(message)), nil }
	switch expression.Name {
	case "poly_register":
		slot, handler, priority := arguments[0].S, arguments[1].S, arguments[2].I
		function := executor.functionNamed(handler)
		if function == nil || function.Receiver != "" || function.Worker || len(function.TypeParams) != 0 || len(function.Params) != 1 || function.Params[0] == nil || function.Params[0].Type != "String" || function.Return != "String" {
			return bad("handler must be a top-level fn(String) -> String")
		}
		if !executor.builtins.registerDispatchEntry(slot, handler, priority) {
			return bad("handler already registered in slot")
		}
		return resVal(true, nilVal()), nil
	case "poly_reorder":
		slot, handler, before := arguments[0].S, arguments[1].S, arguments[2].S
		if !executor.builtins.reorderDispatchEntry(slot, handler, before) {
			return bad("both handlers must already be registered and distinct")
		}
		return resVal(true, nilVal()), nil
	case "poly_dispatch":
		handler, ok := executor.builtins.firstDispatchHandler(arguments[0].S)
		if !ok {
			return bad("dispatch slot has no registered handlers")
		}
		function := executor.functionNamed(handler)
		target := executor.targetForKIRFunction(function)
		if function == nil || target == "" {
			return bad("registered handler is unavailable")
		}
		call := &KIRExpr{Kind: "call", Name: function.Name, Source: expression.Source, Line: expression.Line, Column: expression.Column, Type: function.Return, CallTarget: "function:" + target}
		value, diagnostic := executor.invokeKIR(call, function, executor.global, nil, []Value{arguments[1]})
		if diagnostic != nil {
			return bad(diagnostic.Message)
		}
		return resVal(true, value), nil
	default:
		return nilVal(), executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "unknown KIR dispatch builtin %q", expression.Name)
	}
}

func (executor *kirExecutor) spawnKIRTask(expression *KIRExpr, arguments []Value) (Value, *Diagnostic) {
	if len(arguments) != 2 || arguments[0].Group == nil {
		return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "invalid task group")
	}
	group := arguments[0].Group
	group.mu.Lock()
	cancelled := group.cancelled
	group.mu.Unlock()
	if cancelled {
		return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "task group is cancelled")
	}
	thread, diagnostic := executor.spawnKIRWorker(expression, arguments[1].S)
	if diagnostic != nil {
		return nilVal(), diagnostic
	}
	group.mu.Lock()
	group.threads = append(group.threads, thread.Th)
	group.mu.Unlock()
	return thread, nil
}

func (executor *kirExecutor) spawnKIRWorker(expression *KIRExpr, name string) (Value, *Diagnostic) {
	function := executor.functionNamed(name)
	if function == nil || !function.Worker || function.Receiver != "" || len(function.Params) != 0 {
		return nilVal(), executor.fail(CatRuntime, expression.Source, expression.Line, expression.Column, "thread worker '%s' must be a zero-argument worker function", name)
	}
	runtime := executor.builtins
	if runtime == nil {
		return nilVal(), executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "KIR worker has no runtime lifecycle state")
	}
	if len(runtime.Threads) >= executor.limits.MaxWorkers {
		return nilVal(), executor.fail(CatResource, expression.Source, expression.Line, expression.Column, "worker limit exceeded")
	}
	workerContext, cancel := context.WithCancel(runtime.Ctx.Ctx)
	thread := &Thread{Done: make(chan struct{}), Cancel: cancel}
	runtime.Threads = append(runtime.Threads, thread)
	global := newKIRExecScope(nil)
	for _, binding := range executor.global.values {
		if binding == nil || binding.meta == nil || !kirExecWorkerSharedType(binding.meta.Type) {
			continue
		}
		if err := global.define(binding.meta, binding.value); err != nil {
			cancel()
			return nilVal(), executor.fail(CatArtifact, expression.Source, expression.Line, expression.Column, "cannot capture worker binding: %s", err)
		}
	}
	go func() {
		defer close(thread.Done)
		defer cancel()
		ctx := &ExecContext{Ctx: workerContext, Cancel: cancel, Lim: executor.limits}
		worker := &kirExecutor{
			limits: executor.limits, context: ctx, document: executor.document, sources: executor.sources,
			functions: executor.functions, closures: make(map[*FunctionValue]*kirClosure), closureMu: &sync.RWMutex{},
			structs: executor.structs, enums: executor.enums, types: executor.types,
			outputWriter: kirExecOutputWriter{executor: executor},
		}
		worker.global = global
		worker.initializeKIRBuiltins(executor.document, runtime.Sandbox)
		worker.builtins.Args = append([]string(nil), runtime.Args...)
		worker.builtins.Ctx = ctx
		worker.builtins.Worker = true
		worker.builtins.Channels = append([]*Channel(nil), runtime.Channels...)
		worker.builtins.Threads = append([]*Thread(nil), runtime.Threads...)
		worker.builtins.sharedDispatchMu, worker.builtins.Dispatch = runtime.dispatchStateForWorker()
		worker.builtins.discordRates = runtime.discordRates
		worker.builtins.discordCache = runtime.discordCache
		worker.builtins.discordAPIBaseURL = runtime.discordAPIBaseURL
		worker.builtins.discordGateway = runtime.discordGateway
		call := &KIRExpr{Kind: "call", Name: function.Name, Source: expression.Source, Line: expression.Line, Column: expression.Column, Type: function.Return, CallTarget: "function:" + executor.targetForKIRFunction(function)}
		value, diagnostic := worker.invokeKIR(call, function, global, nil, nil)
		if diagnostic == nil {
			diagnostic = ctx.contextFailure(worker.source(expression.Source), expression.Line, expression.Column)
		}
		diagnostic = worker.builtins.closeResources(diagnostic)
		if diagnostic == nil {
			executor.closureMu.Lock()
			for identity, closure := range worker.closures {
				executor.closures[identity] = closure
			}
			executor.closureMu.Unlock()
		}
		thread.mu.Lock()
		if diagnostic != nil {
			thread.Diag = diagnostic
			thread.Result = nilVal()
		} else {
			thread.Result = cloneValue(value)
		}
		thread.mu.Unlock()
	}()
	return Value{Kind: VThread, Th: thread}, nil
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
	var receiver *Value
	if expression.Receiver != nil {
		value, receiverDiagnostic := executor.evalExpr(scope, expression.Receiver)
		if receiverDiagnostic != nil {
			return kirExecFlow{}, receiverDiagnostic
		}
		receiver = &value
	}
	arguments := make([]Value, len(expression.Args))
	for i, argument := range expression.Args {
		arguments[i], diagnostic = executor.evalExpr(scope, argument)
		if diagnostic != nil {
			return kirExecFlow{}, diagnostic
		}
	}
	invokeCall := expression
	if strings.HasPrefix(expression.CallTarget, "trait:") {
		function, invokeCall, diagnostic = executor.resolveKIRTraitCall(expression, receiver)
		if diagnostic != nil {
			return kirExecFlow{}, diagnostic
		}
		environment = executor.global
	}
	return kirExecFlow{
		value:    nilVal(),
		returned: true,
		tail:     &kirExecTailCall{call: invokeCall, function: function, environment: environment, receiver: receiver, arguments: arguments},
	}, nil
}

func (executor *kirExecutor) valueMatchesType(value Value, typ string) bool {
	// KIR's semantic preflight validates every expression and binding type.
	// At runtime we only verify the outer representation of returned values;
	// recursively scanning each element makes repeated generic array returns
	// quadratic for collection-heavy programs without adding type safety.
	typ = executor.instantiateKIRType(typ)
	if kirValueMatchesScalarType(value, typ) {
		return true
	}
	if typ == "Bytes" {
		return value.Kind == VBytes
	}
	if typ == "Json" {
		return value.Kind == VJSON
	}
	switch typ {
	case "SQLite":
		return value.Kind == VSQLite && value.SQLite != nil
	case "WebSocket":
		return value.Kind == VWebSocket && value.WS != nil
	case "Regex":
		return value.Kind == VRegex && value.Regex != nil
	case "Random":
		return value.Kind == VRandom && value.Random != nil
	case "TcpSocket":
		return value.Kind == VTCP && value.TCP != nil
	case "TcpListener":
		return value.Kind == VTCPListener && value.TCPList != nil
	case "UdpSocket":
		return value.Kind == VUDP && value.UDP != nil
	case "FFILibrary":
		return value.Kind == VFFILibrary && value.FFILib != nil
	case "FFISymbol":
		return value.Kind == VFFISymbol && value.FFISym != nil
	case "FFIBuffer":
		return value.Kind == VFFIBuffer && value.FFIBuf != nil
	case "TaskGroup":
		return value.Kind == VTaskGroup && value.Group != nil
	}
	if parameters, result, ok := parseKIRFunctionType(typ); ok {
		_, _ = parameters, result
		if value.Kind != VFunction || value.Callable == nil {
			return false
		}
		closure := executor.findKIRClosure(value.Callable)
		return closure != nil && closure.function != nil && kirExecFunctionType(closure.function) == typ
	}
	name, arguments, ok := parseKIRContainerType(typ)
	if ok {
		switch name {
		case "Array":
			return value.Kind == VArray
		case "Set":
			return value.Kind == VSet
		case "Map":
			return value.Kind == VMap
		case "Channel":
			return value.Kind == VChannel && value.Ch != nil
		case "Thread":
			return value.Kind == VThread && value.Th != nil
		case "Actor":
			return value.Kind == VActor && value.Actor != nil
		case "Shared":
			return value.Kind == VShared && value.Shared != nil
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
	if resolved, ok := executor.resolveKIRType(typ); ok && resolved.Kind == TyStruct {
		return value.Kind == VStruct && value.Struct == resolved.Struct && typeEqual(value.StructType, resolved)
	}
	if declaration := executor.enums[typ]; declaration != nil {
		return value.Kind == VEnum && value.Enum == declaration
	}
	return false
}

func (executor *kirExecutor) displayValue(value Value) string {
	if value.Kind == VFunction && value.Callable != nil {
		if closure := executor.findKIRClosure(value.Callable); closure != nil && closure.function != nil {
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
		if value.Kind == VInt || value.Kind == VUInt || value.Kind == VFloat {
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
	case "~":
		if value.Kind == VUInt {
			return uintVal(value.UBits, ^value.U), true
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
	if left.Kind == VBytes && right.Kind == VBytes && operator == "+" {
		return bytesVal(append(append([]byte{}, left.Bytes...), right.Bytes...)), true
	}
	if left.Kind == VArray && right.Kind == VArray && operator == "+" {
		leftValues, rightValues := arrayValues(left), arrayValues(right)
		if len(leftValues) > executor.limits.MaxArrayElements-len(rightValues) {
			return nilVal(), false
		}
		return arrVal(append(append([]Value{}, leftValues...), rightValues...)), true
	}
	if left.Kind == VUInt && right.Kind == VUInt {
		if left.UBits != right.UBits {
			return nilVal(), false
		}
		bits := left.UBits
		switch operator {
		case "|":
			return uintVal(bits, left.U|right.U), true
		case "&":
			return uintVal(bits, left.U&right.U), true
		case "^":
			return uintVal(bits, left.U^right.U), true
		case "<":
			return boolVal(left.U < right.U), true
		case "<=":
			return boolVal(left.U <= right.U), true
		case ">":
			return boolVal(left.U > right.U), true
		case ">=":
			return boolVal(left.U >= right.U), true
		}
		var result uint64
		switch operator {
		case "+":
			result = left.U + right.U
		case "-":
			result = left.U - right.U
		case "*":
			result = left.U * right.U
		case "/":
			if right.U == 0 {
				return nilVal(), false
			}
			result = left.U / right.U
		case "%":
			if right.U == 0 {
				return nilVal(), false
			}
			result = left.U % right.U
		default:
			return nilVal(), false
		}
		return uintVal(bits, result), true
	}
	if left.Kind == VUInt && right.Kind == VInt {
		if operator != "<<" && operator != ">>" || right.I < 0 || uint64(right.I) >= uint64(left.UBits) {
			return nilVal(), false
		}
		shift := uint(right.I)
		if operator == "<<" {
			return uintVal(left.UBits, left.U<<shift), true
		}
		return uintVal(left.UBits, left.U>>shift), true
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
		bits, unsigned := kirExecUIntBits(typ)
		return unsigned && value.Kind == VUInt && value.UBits == bits
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
