package kry

import (
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// cgen translates a checked Kryndel program into C source that links against
// the embedded runtime in internal/cruntime. The generated C is compiled by the host
// C compiler (gcc/clang) or a cross compiler (x86_64-w64-mingw32-gcc) to
// produce real, runnable PE/ELF executables.
//
// The translation mirrors the interpreter's semantics exactly: values are
// immutable and arena-allocated, arithmetic is checked, display is
// deterministic, and unsupported constructs are rejected with a clear
// diagnostic instead of silently degrading.
type cgen struct {
	doc     *KIRDocument
	limits  Limits
	buf     strings.Builder
	tmp     int
	structs []*KIRStruct
	// structInstances are the concrete runtime types represented by the
	// generated KValue structs. Generic declarations contribute one entry per
	// closed type used by a reachable function specialization.
	structInstances []cStructInstance
	enums           []*KIREnum
	// structID maps the instantiated type identity to its runtime id.
	structID map[string]int
	enumID   map[string]int
	// fnName maps a resolved function to its generated C symbol.
	fnName                map[*KIRFunction]string
	functionBodyNames     map[string]string
	functionIndex         map[*KIRFunction]int
	functionByTarget      map[string]*KIRFunction
	functionsByName       map[string][]*KIRFunction
	builtins              map[string]Builtin
	functionInstances     []*cFunctionInstance
	functionInstanceByKey map[string]*cFunctionInstance
	functionSubstitution  map[string]string
	regularFunctions      []*KIRFunction
	traitImpls            []*KIRTraitImpl
	// unsupported records the first construct the backend cannot lower.
	unsupported string
	// deferBases tracks the runtime defer-stack depth at each enclosing block.
	deferBases []string
	// fnBase is the defer-stack depth at function entry.
	fnBase string
	// loopBases tracks the defer-stack depth at each enclosing loop.
	loopBases []string
	// polyFns lists the top-level fn(String) -> String handlers referenced by
	// poly_register, in registration order, so the runtime dispatch table can
	// be emitted as a static array of function pointers.
	polyFns []*KIRFunction
	// globals holds the names of top-level bindings, emitted as file-scope
	// static KValue cells so functions can reference them.
	globals map[string]bool
	// locals is a stack of in-scope local binding names, used to decide
	// whether an identifier refers to a global or a local.
	locals []map[string]bool
	// topLevel is true while emitting the top-level statement list.
	topLevel bool
	// obfuscate masks string literals so their plaintext does not appear in the
	// produced executable.
	obfuscate bool
}

type cStructInstance struct {
	name string
	decl *KIRStruct
}

type cFunctionInstance struct {
	function      *KIRFunction
	symbol        string
	key           string
	substitutions map[string]string
}

// GenerateC lowers a checked program to C source. It returns an error when the
// program uses a construct the native backend does not support, so callers can
// surface an honest diagnostic rather than emitting a broken binary.
func GenerateC(p *Program, c *Checker) (string, error) {
	return generateC(p, c, false)
}

// GenerateCObfuscated lowers a checked program to C with string literals masked
// so their plaintext is not recoverable from the produced executable.
func GenerateCObfuscated(p *Program, c *Checker) (string, error) {
	return generateC(p, c, true)
}

func generateC(p *Program, c *Checker, obfuscate bool) (string, error) {
	if p == nil || c == nil || c.Env == nil {
		return "", fmt.Errorf("missing checked program")
	}
	mir, err := CompileMIR(p, c, NativeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH})
	if err != nil {
		return "", fmt.Errorf("compile MIR for C AOT: %w", err)
	}
	return generateCFromValidatedKIR(mir, mir.limits, obfuscate)
}

func generateCFromValidatedKIR(mir *ValidatedMIR, limits Limits, obfuscate bool) (string, error) {
	if mir == nil || mir.document == nil {
		return "", fmt.Errorf("missing validated MIR")
	}
	document := mir.document
	if kirDocumentUsesFunctionValues(document) {
		return "", fmt.Errorf("C backend does not support function values or closures; use the interpreter")
	}
	g := &cgen{
		doc:                   document,
		limits:                limits,
		structID:              map[string]int{},
		enumID:                map[string]int{},
		fnName:                map[*KIRFunction]string{},
		functionBodyNames:     map[string]string{},
		functionIndex:         map[*KIRFunction]int{},
		functionByTarget:      map[string]*KIRFunction{},
		functionsByName:       map[string][]*KIRFunction{},
		builtins:              Builtins(),
		functionInstanceByKey: map[string]*cFunctionInstance{},
		globals:               map[string]bool{},
		traitImpls:            document.TraitImpls,
		obfuscate:             obfuscate,
	}
	g.structs = append(g.structs, document.Structs...)
	g.enums = append(g.enums, document.Enums...)
	for i, e := range g.enums {
		g.enumID[e.Name] = i
	}
	for i, f := range document.Functions {
		g.functionIndex[f] = i
		g.functionsByName[f.Name] = append(g.functionsByName[f.Name], f)
	}
	for _, f := range document.Functions {
		target := f.Name
		if len(g.functionsByName[f.Name]) > 1 {
			target = kirFunctionTargetFromDocument(f)
		}
		g.functionByTarget[target] = f
		name := g.symbol(f)
		if len(g.functionsByName[f.Name]) > 1 {
			name += "_" + sanitize(strings.TrimPrefix(target, f.Name+"@"))
		}
		g.fnName[f] = name
	}
	if err := g.planInstances(); err != nil {
		return "", err
	}
	g.planFunctionBodyNames()
	g.collectPolyHandlers()
	g.collectGlobals()

	g.emitRuntime()
	g.emitGlobals()
	g.emitMetadata()
	g.emitPrototypes()
	g.emitPolyTable()
	g.emitFunctions()
	g.emitMain()
	if g.unsupported != "" {
		return "", fmt.Errorf("%s", g.unsupported)
	}
	return g.buf.String(), nil
}

func (g *cgen) symbol(f *KIRFunction) string {
	name := sanitize(f.Name)
	if f.Trait != "" {
		name = sanitize(f.Trait) + "_" + name
	}
	if f.Receiver != "" {
		name = sanitize(f.Receiver) + "_" + name
	}
	return "kfn_" + name
}

func (g *cgen) functionNeedsSpecialization(f *KIRFunction) bool {
	if f == nil {
		return false
	}
	if len(f.TypeParams) != 0 {
		return true
	}
	if f.Receiver == "" {
		return false
	}
	if receiver, ok := parseKIRTypeExpression(f.Receiver); ok {
		if declaration := g.structDecl(receiver.Name); declaration != nil {
			return len(declaration.TypeParams) != 0
		}
	}
	return false
}

func (g *cgen) functionHasGenericReceiver(f *KIRFunction) bool {
	if f == nil || f.Receiver == "" {
		return false
	}
	receiver, ok := parseKIRTypeExpression(f.Receiver)
	if !ok {
		return false
	}
	declaration := g.structDecl(receiver.Name)
	return declaration != nil && len(declaration.TypeParams) != 0
}

func (g *cgen) planInstances() error {
	for _, declaration := range g.structs {
		if len(declaration.TypeParams) == 0 {
			if err := g.addStructInstance(declaration.Name); err != nil {
				return err
			}
		}
	}
	for _, function := range g.doc.Functions {
		if g.functionNeedsSpecialization(function) {
			continue
		}
		g.regularFunctions = append(g.regularFunctions, function)
		if err := g.walkFunctionForInstances(function, nil); err != nil {
			return err
		}
	}
	if err := g.walkStatementsForInstances(g.doc.Statements, nil); err != nil {
		return err
	}
	for next := 0; next < len(g.functionInstances); next++ {
		instance := g.functionInstances[next]
		if err := g.walkFunctionForInstances(instance.function, instance.substitutions); err != nil {
			return err
		}
		if len(g.functionInstances) > 4096 {
			return fmt.Errorf("C AOT generic monomorphization exceeded 4096 function instances")
		}
	}
	return nil
}

func (g *cgen) walkFunctionForInstances(function *KIRFunction, substitutions map[string]string) error {
	if function == nil {
		return nil
	}
	for _, parameter := range function.Params {
		if err := g.walkExprForInstances(parameter.Default, substitutions); err != nil {
			return err
		}
	}
	return g.walkStatementsForInstances(function.Body, substitutions)
}

func (g *cgen) walkStatementsForInstances(statements []*KIRStmt, substitutions map[string]string) error {
	for _, statement := range statements {
		if statement == nil {
			continue
		}
		for _, expression := range []*KIRExpr{statement.Init, statement.Expr, statement.Target, statement.Value, statement.Cond, statement.Iter, statement.Return, statement.Scrutinee} {
			if err := g.walkExprForInstances(expression, substitutions); err != nil {
				return err
			}
		}
		for _, branch := range [][]*KIRStmt{statement.Then, statement.Else, statement.Body} {
			if err := g.walkStatementsForInstances(branch, substitutions); err != nil {
				return err
			}
		}
		for _, arm := range statement.Arms {
			if err := g.walkStatementsForInstances(arm.Body, substitutions); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *cgen) walkExprForInstances(expression *KIRExpr, substitutions map[string]string) error {
	if expression == nil {
		return nil
	}
	if expression.Kind == "struct" {
		instanceType := substituteKIRType(expression.Type, substitutions)
		if err := g.addStructInstance(instanceType); err != nil {
			return err
		}
	}
	if expression.Kind == "call" && expression.TraitName != "" {
		function, err := g.traitImplementationMethod(expression, substitutions)
		if err != nil {
			return err
		}
		if g.functionNeedsSpecialization(function) {
			if _, err := g.functionInstanceForCall(function, expression.Receiver, expression.GenericArguments, substitutions, true); err != nil {
				return err
			}
		}
	} else if expression.Kind == "call" && strings.HasPrefix(expression.CallTarget, "function:") {
		function := g.functionByTarget[strings.TrimPrefix(expression.CallTarget, "function:")]
		if function != nil && g.functionNeedsSpecialization(function) {
			if _, err := g.functionInstanceForCall(function, expression.Receiver, expression.GenericArguments, substitutions, true); err != nil {
				return err
			}
		}
	}
	for _, child := range []*KIRExpr{expression.Left, expression.Right, expression.Operand, expression.Callee, expression.Base, expression.Receiver} {
		if err := g.walkExprForInstances(child, substitutions); err != nil {
			return err
		}
	}
	for _, children := range [][]*KIRExpr{expression.Args, expression.Items, expression.MapKeys, expression.Values} {
		for _, child := range children {
			if err := g.walkExprForInstances(child, substitutions); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *cgen) traitImplementationMethod(expression *KIRExpr, substitutions map[string]string) (*KIRFunction, error) {
	if expression == nil || expression.Receiver == nil || expression.Receiver.Type == "" || expression.TraitName == "" {
		return nil, fmt.Errorf("C AOT cannot resolve an incomplete trait method call")
	}
	receiverType := substituteKIRType(expression.Receiver.Type, substitutions)
	parsedReceiver, ok := parseKIRTypeExpression(receiverType)
	if !ok || g.structDecl(parsedReceiver.Name) == nil || typeContainsKIRVariable(receiverType, kirOpenTypeParameters(substitutions)) {
		return nil, fmt.Errorf("C AOT trait dispatch for %s.%s requires a concrete struct receiver after specialization", expression.TraitName, expression.Name)
	}
	if err := g.addStructInstance(receiverType); err != nil {
		return nil, err
	}
	for _, implementation := range g.traitImpls {
		if implementation.Trait != expression.TraitName || substituteKIRType(implementation.For, substitutions) != receiverType {
			continue
		}
		for _, method := range implementation.Methods {
			if method.Name == expression.Name && strings.HasPrefix(method.Target, "function:") {
				if function := g.functionByTarget[strings.TrimPrefix(method.Target, "function:")]; function != nil {
					return function, nil
				}
				return nil, fmt.Errorf("C AOT trait %q implementation references an unknown function target", expression.TraitName)
			}
		}
	}
	return nil, fmt.Errorf("C AOT trait %q implementation for %s has no method %q", expression.TraitName, receiverType, expression.Name)
}

func (g *cgen) addStructInstance(instanceType string) error {
	parsed, ok := parseKIRTypeExpression(instanceType)
	if !ok || parsed.Function {
		return fmt.Errorf("C AOT cannot lower malformed struct type %q", instanceType)
	}
	declaration := g.structDecl(parsed.Name)
	if declaration == nil {
		return fmt.Errorf("C AOT cannot lower a struct expression without resolved struct type metadata")
	}
	if len(parsed.Params) != len(declaration.TypeParams) {
		return fmt.Errorf("C AOT cannot lower malformed generic struct type %s: expected %d type argument(s), got %d", instanceType, len(declaration.TypeParams), len(parsed.Params))
	}
	parameters := make([]string, len(declaration.TypeParams))
	for i, parameter := range declaration.TypeParams {
		parameters[i] = parameter.Name
	}
	if typeContainsKIRVariable(instanceType, parameters) {
		return fmt.Errorf("C AOT cannot lower open generic struct type %s without concrete type arguments", instanceType)
	}
	name := TypeSpecString(parsed)
	key := name
	if _, exists := g.structID[key]; exists {
		return nil
	}
	g.structID[key] = len(g.structInstances)
	g.structInstances = append(g.structInstances, cStructInstance{name: name, decl: declaration})
	return nil
}

func (g *cgen) functionInstanceForCall(function *KIRFunction, receiver *KIRExpr, arguments []string, outer map[string]string, create bool) (*cFunctionInstance, error) {
	if function == nil {
		return nil, fmt.Errorf("C AOT cannot specialize a missing function")
	}
	index, known := g.functionIndex[function]
	if !known {
		return nil, fmt.Errorf("C AOT cannot specialize function %q outside the checked program", function.Name)
	}
	substitutions := make(map[string]string, len(function.TypeParams)+2)
	keyParts := make([]string, 0, len(function.TypeParams)+2)
	if g.functionHasGenericReceiver(function) {
		if receiver == nil || receiver.Type == "" {
			return nil, fmt.Errorf("C AOT cannot specialize method %q without a resolved receiver type", function.Name)
		}
		receiverType := substituteKIRType(receiver.Type, outer)
		parsedReceiver, ok := parseKIRTypeExpression(receiverType)
		functionReceiver, functionReceiverOK := parseKIRTypeExpression(function.Receiver)
		declaration := g.structDecl(functionReceiver.Name)
		if !ok || !functionReceiverOK || declaration == nil || parsedReceiver.Name != declaration.Name || len(parsedReceiver.Params) != len(declaration.TypeParams) {
			return nil, fmt.Errorf("C AOT cannot specialize method %q for invalid receiver type %s", function.Name, receiverType)
		}
		for i, parameter := range declaration.TypeParams {
			argument := TypeSpecString(parsedReceiver.Params[i])
			if typeContainsKIRVariable(argument, kirOpenTypeParameters(outer)) {
				return nil, fmt.Errorf("C AOT cannot specialize method %q with unresolved receiver argument %s", function.Name, argument)
			}
			if kirTypeDepth(argument) > g.limits.MaxTypeDepth {
				return nil, fmt.Errorf("C AOT generic method specialization for %q exceeds the configured type-depth limit", function.Name)
			}
			substitutions[parameter.Name] = argument
			keyParts = append(keyParts, "receiver="+argument)
		}
	}
	if len(arguments) != len(function.TypeParams) {
		return nil, fmt.Errorf("C AOT cannot specialize generic function %q: expected %d checked type argument(s), got %d", function.Name, len(function.TypeParams), len(arguments))
	}
	for i, parameter := range function.TypeParams {
		argument := substituteKIRType(arguments[i], outer)
		if typeContainsKIRVariable(argument, kirOpenTypeParameters(outer)) {
			return nil, fmt.Errorf("C AOT cannot specialize generic function %q with unresolved type argument %s", function.Name, argument)
		}
		if kirTypeDepth(argument) > g.limits.MaxTypeDepth {
			return nil, fmt.Errorf("C AOT generic function specialization for %q exceeds the configured type-depth limit", function.Name)
		}
		substitutions[parameter.Name] = argument
		keyParts = append(keyParts, "function="+argument)
	}
	key := fmt.Sprintf("%d|%s", index, strings.Join(keyParts, "|"))
	if instance := g.functionInstanceByKey[key]; instance != nil {
		return instance, nil
	}
	if !create {
		return nil, fmt.Errorf("C AOT generic function specialization for %q was not planned", function.Name)
	}
	if len(g.functionInstances) >= 4096 {
		return nil, fmt.Errorf("C AOT generic monomorphization exceeded 4096 function instances")
	}
	symbol := fmt.Sprintf("%s_g%d", g.fnName[function], len(g.functionInstances))
	instance := &cFunctionInstance{function: function, symbol: symbol, key: key, substitutions: substitutions}
	g.functionInstanceByKey[key] = instance
	g.functionInstances = append(g.functionInstances, instance)
	return instance, nil
}

func (g *cgen) functionSymbolForCall(function *KIRFunction, receiver *KIRExpr, arguments []string) (string, error) {
	if !g.functionNeedsSpecialization(function) {
		if symbol := g.fnName[function]; symbol != "" {
			return symbol, nil
		}
		return "", fmt.Errorf("C AOT has no emitted symbol for function %q", function.Name)
	}
	instance, err := g.functionInstanceForCall(function, receiver, arguments, g.functionSubstitution, false)
	if err != nil {
		return "", err
	}
	return instance.symbol, nil
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func cBindingName(name string) string {
	return "k_user_" + sanitize(name)
}

func (g *cgen) next() string {
	g.tmp++
	return fmt.Sprintf("_t%d", g.tmp)
}

func (g *cgen) fail(format string, args ...any) {
	if g.unsupported == "" {
		g.unsupported = fmt.Sprintf(format, args...)
	}
}

// collectPolyHandlers records every top-level fn(String) -> String so the
// runtime dispatch table can resolve handler names dynamically (the name may
// be passed through a wrapper function rather than written as a literal).
func (g *cgen) collectPolyHandlers() {
	for _, f := range g.doc.Functions {
		if g.functionNeedsSpecialization(f) {
			continue
		}
		if f.Receiver != "" || len(f.Params) != 1 {
			continue
		}
		if f.Params[0].Type == "String" && f.Return == "String" {
			g.polyFns = append(g.polyFns, f)
		}
	}
}

// collectGlobals records top-level let/const bindings so they can be emitted
// as file-scope cells and referenced from functions.
func (g *cgen) collectGlobals() {
	for _, st := range g.doc.Statements {
		if st.Kind == "let" || st.Kind == "const" {
			g.globals[st.Name] = true
		}
	}
}

// isGlobal reports whether an identifier resolves to a top-level binding that
// is not shadowed by an in-scope local.
func (g *cgen) isGlobal(name string) bool {
	if !g.globals[name] {
		return false
	}
	for i := len(g.locals) - 1; i >= 0; i-- {
		if g.locals[i][name] {
			return false
		}
	}
	return true
}

// emitGlobals declares file-scope cells for every top-level binding.
func (g *cgen) emitGlobals() {
	for _, st := range g.doc.Statements {
		if st.Kind == "let" || st.Kind == "const" {
			fmt.Fprintf(&g.buf, "static KValue %s;\n", cBindingName(st.Name))
		}
	}
}

// emitPrototypes forward-declares every generated function so the dispatch
// table and mutually recursive calls compile without ordering constraints.
func (g *cgen) emitPrototypes() {
	for _, f := range g.regularFunctions {
		name := g.fnName[f]
		fmt.Fprintf(&g.buf, "static KValue %s(void);\n", name)
		fmt.Fprintf(&g.buf, "static KValue %s(void);\n", g.functionBodyNames[name])
	}
	for _, instance := range g.functionInstances {
		fmt.Fprintf(&g.buf, "static KValue %s(void);\n", instance.symbol)
		fmt.Fprintf(&g.buf, "static KValue %s(void);\n", g.functionBodyNames[instance.symbol])
	}
}

// emitPolyTable emits the handler name table and function-pointer table used
// by poly_register/poly_dispatch.
func (g *cgen) emitPolyTable() {
	fmt.Fprintf(&g.buf, "static const char *k_poly_names_data[%d] = {", len(g.polyFns)+1)
	for i, f := range g.polyFns {
		if i > 0 {
			g.buf.WriteString(", ")
		}
		g.buf.WriteString(cString(f.Name))
	}
	if len(g.polyFns) == 0 {
		g.buf.WriteString("0")
	}
	g.buf.WriteString("};\n")
	fmt.Fprintf(&g.buf, "static KValue (*k_poly_fns_data[%d])(void) = {", len(g.polyFns)+1)
	for i, f := range g.polyFns {
		if i > 0 {
			g.buf.WriteString(", ")
		}
		g.buf.WriteString(g.fnName[f])
	}
	if len(g.polyFns) == 0 {
		g.buf.WriteString("0")
	}
	g.buf.WriteString("};\n")
	fmt.Fprintf(&g.buf, "static int k_poly_nfns_data = %d;\n", len(g.polyFns))
}

func (g *cgen) emitRuntime() {
	g.buf.WriteString(cRuntimeSource())
	g.buf.WriteString("\n/* ---- generated program ------------------------------------------------ */\n")
}

func (g *cgen) emitMetadata() {
	for i, instance := range g.structInstances {
		fmt.Fprintf(&g.buf, "static const char *k_sf_%d[] = {", i)
		for j, f := range instance.decl.Fields {
			if j > 0 {
				g.buf.WriteString(", ")
			}
			g.buf.WriteString(cString(f.Name))
		}
		g.buf.WriteString("};\n")
	}
	if len(g.structInstances) == 0 {
		g.buf.WriteString("static const char *k_sf_empty[] = {0};\n")
	}
	g.buf.WriteString("static KStructDesc k_structs_data[] = {\n")
	for i, instance := range g.structInstances {
		fmt.Fprintf(&g.buf, "  {%s, %d, k_sf_%d},\n", cString(instance.name), len(instance.decl.Fields), i)
	}
	if len(g.structInstances) == 0 {
		g.buf.WriteString("  {\"\", 0, k_sf_empty},\n")
	}
	g.buf.WriteString("};\n")

	for i, e := range g.enums {
		fmt.Fprintf(&g.buf, "static const char *k_ev_%d[] = {", i)
		for j, v := range e.Variants {
			if j > 0 {
				g.buf.WriteString(", ")
			}
			g.buf.WriteString(cString(v))
		}
		g.buf.WriteString("};\n")
	}
	if len(g.enums) == 0 {
		g.buf.WriteString("static const char *k_ev_empty[] = {0};\n")
	}
	g.buf.WriteString("static KEnumDesc k_enums_data[] = {\n")
	for i, e := range g.enums {
		fmt.Fprintf(&g.buf, "  {%s, %d, k_ev_%d},\n", cString(e.Name), len(e.Variants), i)
	}
	if len(g.enums) == 0 {
		g.buf.WriteString("  {\"\", 0, k_ev_empty},\n")
	}
	g.buf.WriteString("};\n")
}

func (g *cgen) emitFunctions() {
	for _, function := range g.regularFunctions {
		name := g.fnName[function]
		g.emitFunction(function, name, g.functionBodyNames[name], nil)
	}
	for _, instance := range g.functionInstances {
		g.emitFunction(instance.function, instance.symbol, g.functionBodyNames[instance.symbol], instance.substitutions)
	}
}

func (g *cgen) planFunctionBodyNames() {
	index := 0
	for _, function := range g.regularFunctions {
		name := g.fnName[function]
		g.functionBodyNames[name] = fmt.Sprintf("kry_generated_fn_body_%d", index)
		index++
	}
	for _, instance := range g.functionInstances {
		g.functionBodyNames[instance.symbol] = fmt.Sprintf("kry_generated_fn_body_%d", index)
		index++
	}
}

func (g *cgen) emitFunction(f *KIRFunction, name, bodyName string, substitutions map[string]string) {
	fmt.Fprintf(&g.buf, "static KValue %s(void) {\n", name)
	fmt.Fprintf(&g.buf, "  if (k_call_depth >= k_max_call_depth) kfail(\"call depth limit exceeded\");\n")
	fmt.Fprintf(&g.buf, "  k_call_depth++; k_tail_pending = 0; KValue _result = %s();\n", bodyName)
	fmt.Fprintf(&g.buf, "  while (k_tail_pending) { KValue (*_tail_next)(void) = k_tail_target; k_tail_pending = 0; _result = _tail_next(); }\n")
	fmt.Fprintf(&g.buf, "  k_call_depth--; return _result;\n}\n")
	fmt.Fprintf(&g.buf, "static KValue %s(void) {\n", bodyName)
	// Parameters are passed through a global argument frame so that the
	// generated C stays simple and recursion works without prototypes.
	fmt.Fprintf(&g.buf, "  int _fb = k_ndefers;\n")
	fmt.Fprintf(&g.buf, "  int _argc = k_argc;\n")
	g.fnBase = "_fb"
	g.deferBases = nil
	g.loopBases = nil
	previousSubstitution := g.functionSubstitution
	g.functionSubstitution = substitutions
	g.locals = append(g.locals, map[string]bool{})
	if f.Receiver != "" {
		fmt.Fprintf(&g.buf, "  KValue %s = k_args[0];\n", cBindingName("self"))
		g.locals[len(g.locals)-1]["self"] = true
	}
	for i, p := range f.Params {
		off := i
		if f.Receiver != "" {
			off = i + 1
		}
		if p.Default != nil {
			fmt.Fprintf(&g.buf, "  KValue %s = (_argc > %d) ? k_args[%d] : %s;\n", cBindingName(p.Name), off, off, g.expr(p.Default))
		} else {
			fmt.Fprintf(&g.buf, "  KValue %s = k_args[%d];\n", cBindingName(p.Name), off)
		}
		g.locals[len(g.locals)-1][p.Name] = true
	}
	g.block(f.Body, "  ")
	g.locals = g.locals[:len(g.locals)-1]
	g.functionSubstitution = previousSubstitution
	fmt.Fprintf(&g.buf, "  return kv_nil();\n}\n")
}

func (g *cgen) emitMain() {
	g.buf.WriteString("int main(void) {\n")
	fmt.Fprintf(&g.buf, "  k_max_instructions = %dULL;\n", g.limits.MaxInstructions)
	fmt.Fprintf(&g.buf, "  k_max_call_depth = %d;\n", g.limits.MaxCallDepth)
	fmt.Fprintf(&g.buf, "  k_max_json = %dLL;\n", g.limits.MaxJSONBytes)
	fmt.Fprintf(&g.buf, "  k_max_out = %dLL;\n", g.limits.MaxOutputBytes)
	maxMemory := g.limits.MaxMemoryBytes
	if maxMemory <= 0 {
		maxMemory = DefaultLimits().MaxMemoryBytes
	}
	fmt.Fprintf(&g.buf, "  k_max_mem = %dLL;\n", maxMemory)
	fmt.Fprintf(&g.buf, "  k_max_tcp_receive = %dLL;\n", g.limits.MaxSourceBytes)
	fmt.Fprintf(&g.buf, "  k_max_array_elements = %dLL;\n", g.limits.MaxArrayElements)
	wallMS := g.limits.MaxWallTimeMS
	fmt.Fprintf(&g.buf, "  k_max_wall_ms = %dLL;\n", wallMS)
	g.buf.WriteString("  k_structs = k_structs_data;\n")
	g.buf.WriteString("  k_enums = k_enums_data;\n")
	g.buf.WriteString("  k_poly_names = k_poly_names_data;\n")
	g.buf.WriteString("  k_poly_fns = k_poly_fns_data;\n")
	g.buf.WriteString("  k_poly_nfns = k_poly_nfns_data;\n")
	g.buf.WriteString("  if (setjmp(k_jmp)) { k_sqlite_cleanup(); k_tcp_cleanup(); fflush(stdout); fprintf(stderr, \"kryndel: %s\\n\", k_errbuf); return 1; }\n")
	g.buf.WriteString("  k_runtime_deadline_ms = k_max_wall_ms > 0 ? k_tcp_deadline_after(k_tcp_now_ms(), (unsigned long long)k_max_wall_ms) : 0ULL;\n")
	g.buf.WriteString("  int _fb = k_ndefers;\n")
	g.fnBase = "_fb"
	g.deferBases = nil
	g.loopBases = nil
	g.topLevel = true
	g.block(g.doc.Statements, "  ")
	g.topLevel = false
	if len(g.doc.Statements) == 0 {
		if functions := g.functionsByName["main"]; len(functions) == 1 {
			f := functions[0]
			fmt.Fprintf(&g.buf, "  { k_argc = 0; k_args[0] = kv_nil(); KValue _r = %s(); (void)_r; k_check_wall_time(); }\n", g.fnName[f])
		}
	}
	g.buf.WriteString("  while (k_ndefers > _fb) k_defers[--k_ndefers]();\n")
	g.buf.WriteString("  if (k_tcp_has_open_sockets()) { k_sqlite_cleanup(); k_tcp_cleanup(); fflush(stdout); fprintf(stderr, \"kryndel: resource 'TcpSocket' was not closed before its owner finished\\n\"); return 1; }\n")
	g.buf.WriteString("  if (k_sqlite_has_open_handles()) { k_sqlite_cleanup(); k_tcp_cleanup(); fflush(stdout); fprintf(stderr, \"kryndel: resource 'SQLite' was not closed before its owner finished\\n\"); return 1; }\n")
	g.buf.WriteString("  k_sqlite_cleanup(); k_tcp_cleanup();\n")
	g.buf.WriteString("  fflush(stdout);\n  return 0;\n}\n")
}

// block emits a sequence of statements inside a new C scope, running any
// registered defers when the scope exits.
func (g *cgen) block(body []*KIRStmt, indent string) {
	base := g.next()
	fmt.Fprintf(&g.buf, "%s{ int %s = k_ndefers;\n", indent, base)
	g.deferBases = append(g.deferBases, base)
	g.locals = append(g.locals, map[string]bool{})
	for _, s := range body {
		g.stmt(s, indent+"  ")
	}
	fmt.Fprintf(&g.buf, "%s  while (k_ndefers > %s) k_defers[--k_ndefers]();\n", indent, base)
	fmt.Fprintf(&g.buf, "%s}\n", indent)
	g.deferBases = g.deferBases[:len(g.deferBases)-1]
	g.locals = g.locals[:len(g.locals)-1]
}

// nestedBlock emits a block that is not the top-level statement list, so
// bindings inside it are locals rather than globals.
func (g *cgen) nestedBlock(body []*KIRStmt, indent string) {
	wasTop := g.topLevel
	g.topLevel = false
	g.block(body, indent)
	g.topLevel = wasTop
}

func (g *cgen) stmt(s *KIRStmt, indent string) {
	if s == nil {
		return
	}
	fmt.Fprintf(&g.buf, "%sk_step();\n", indent)
	switch kirStmtKind(s.Kind) {
	case StLet, StConst:
		if g.topLevel {
			fmt.Fprintf(&g.buf, "%s%s = %s;\n", indent, cBindingName(s.Name), g.expr(s.Init))
		} else {
			fmt.Fprintf(&g.buf, "%sKValue %s = %s;\n", indent, cBindingName(s.Name), g.expr(s.Init))
			if len(g.locals) > 0 {
				g.locals[len(g.locals)-1][s.Name] = true
			}
		}
	case StAssign:
		if s.Target == nil || s.Target.Kind != "var" {
			g.fail("assignment target must be a binding")
			return
		}
		fmt.Fprintf(&g.buf, "%s%s = %s;\n", indent, cBindingName(s.Target.Name), g.expr(s.Value))
	case StExpr:
		fmt.Fprintf(&g.buf, "%s{ KValue _e = %s; (void)_e; }\n", indent, g.expr(s.Expr))
	case StIf:
		cond := g.next()
		fmt.Fprintf(&g.buf, "%s{ KValue %s = %s; if (%s.tag != K_BOOL) kfail(\"condition must be Bool\");\n", indent, cond, g.expr(s.Cond), cond)
		fmt.Fprintf(&g.buf, "%s  if (%s.u.b) {\n", indent, cond)
		g.nestedBlock(s.Then, indent+"    ")
		if len(s.Else) > 0 {
			fmt.Fprintf(&g.buf, "%s  } else {\n", indent)
			g.nestedBlock(s.Else, indent+"    ")
		}
		fmt.Fprintf(&g.buf, "%s  }\n%s}\n", indent, indent)
	case StWhile:
		lb := g.next()
		fmt.Fprintf(&g.buf, "%s{ int %s = k_ndefers;\n", indent, lb)
		g.loopBases = append(g.loopBases, lb)
		fmt.Fprintf(&g.buf, "%s  while (1) {\n", indent)
		cond := g.next()
		fmt.Fprintf(&g.buf, "%s    KValue %s = %s; if (%s.tag != K_BOOL) kfail(\"condition must be Bool\"); if (!%s.u.b) break;\n", indent, cond, g.expr(s.Cond), cond, cond)
		g.nestedBlock(s.Body, indent+"    ")
		fmt.Fprintf(&g.buf, "%s  }\n", indent)
		g.loopBases = g.loopBases[:len(g.loopBases)-1]
		fmt.Fprintf(&g.buf, "%s}\n", indent)
	case StFor:
		lb := g.next()
		it := g.next()
		idx := g.next()
		fmt.Fprintf(&g.buf, "%s{ int %s = k_ndefers;\n", indent, lb)
		g.loopBases = append(g.loopBases, lb)
		fmt.Fprintf(&g.buf, "%s  KValue %s = k_iter_items(%s);\n", indent, it, g.expr(s.Iter))
		fmt.Fprintf(&g.buf, "%s  for (size_t %s = 0; %s < %s.u.a.len; %s++) {\n", indent, idx, idx, it, idx)
		fmt.Fprintf(&g.buf, "%s    KValue %s = %s.u.a.items[%s];\n", indent, cBindingName(s.Name), it, idx)
		g.nestedBlock(s.Body, indent+"    ")
		fmt.Fprintf(&g.buf, "%s  }\n", indent)
		g.loopBases = g.loopBases[:len(g.loopBases)-1]
		fmt.Fprintf(&g.buf, "%s}\n", indent)
	case StReturn:
		g.emitReturn(s, indent)
	case StBreak:
		g.emitUnwind(indent)
		fmt.Fprintf(&g.buf, "%sbreak;\n", indent)
	case StContinue:
		g.emitUnwind(indent)
		fmt.Fprintf(&g.buf, "%scontinue;\n", indent)
	case StMatch:
		g.emitMatch(s, indent)
	case StDefer:
		g.emitDefer(s, indent)
	case StUnsafe:
		g.nestedBlock(s.Body, indent)
	default:
		g.fail("unsupported statement kind %q", s.Kind)
	}
	if g.topLevel {
		fmt.Fprintf(&g.buf, "%sk_check_wall_time();\n", indent)
	}
}

func (g *cgen) emitUnwind(indent string) {
	if len(g.loopBases) == 0 {
		return
	}
	base := g.loopBases[len(g.loopBases)-1]
	fmt.Fprintf(&g.buf, "%swhile (k_ndefers > %s) k_defers[--k_ndefers]();\n", indent, base)
}

func (g *cgen) emitReturn(s *KIRStmt, indent string) {
	fmt.Fprintf(&g.buf, "%swhile (k_ndefers > %s) k_defers[--k_ndefers]();\n", indent, g.fnBase)
	if s.Return == nil {
		fmt.Fprintf(&g.buf, "%sreturn kv_nil();\n", indent)
		return
	}
	if g.emitTailCallReturn(s.Return, indent) {
		return
	}
	// `return expr?` where the function returns Option/Result yields the
	// operand itself: some(x) wrapped is identical to the original Option.
	if s.Return.Kind == "propagate" {
		fmt.Fprintf(&g.buf, "%sreturn %s;\n", indent, g.expr(s.Return.Operand))
		return
	}
	fmt.Fprintf(&g.buf, "%sreturn %s;\n", indent, g.expr(s.Return))
}

func (g *cgen) emitTailCallReturn(expression *KIRExpr, indent string) bool {
	if expression == nil || expression.Kind != "call" || !expression.Tail {
		return false
	}
	var function *KIRFunction
	if expression.Receiver != nil {
		if expression.TraitName != "" {
			var err error
			function, err = g.traitImplementationMethod(expression, g.functionSubstitution)
			if err != nil {
				g.fail("%s", err)
				return false
			}
		} else if strings.HasPrefix(expression.CallTarget, "function:") {
			function = g.functionByTarget[strings.TrimPrefix(expression.CallTarget, "function:")]
		}
	} else if strings.HasPrefix(expression.CallTarget, "function:") {
		function = g.functionByTarget[strings.TrimPrefix(expression.CallTarget, "function:")]
	}
	if function == nil {
		return false
	}
	name, err := g.functionSymbolForCall(function, expression.Receiver, expression.GenericArguments)
	if err != nil {
		g.fail("%s", err)
		return false
	}
	bodyName := g.functionBodyNames[name]
	if bodyName == "" {
		g.fail("C AOT has no generated body for function %q", function.Name)
		return false
	}
	frame := g.next()
	fmt.Fprintf(&g.buf, "%s{ KValue %s[K_MAX_ARGS];\n", indent, frame)
	argumentOffset := 0
	if expression.Receiver != nil {
		fmt.Fprintf(&g.buf, "%s  %s[0] = %s;\n", indent, frame, g.expr(expression.Receiver))
		argumentOffset = 1
	}
	for index, argument := range expression.Args {
		fmt.Fprintf(&g.buf, "%s  %s[%d] = %s;\n", indent, frame, index+argumentOffset, g.expr(argument))
	}
	argumentCount := argumentOffset + len(expression.Args)
	fmt.Fprintf(&g.buf, "%s  k_argc = %d; memcpy(k_args, %s, sizeof(KValue)*%d); k_tail_target = %s; k_tail_pending = 1; return kv_nil();\n", indent, argumentCount, frame, argumentCount, bodyName)
	fmt.Fprintf(&g.buf, "%s}\n", indent)
	return true
}

func (g *cgen) emitDefer(s *KIRStmt, indent string) {
	name := "k_defer_" + g.next()
	fmt.Fprintf(&g.buf, "%svoid %s(void) {\n", indent, name)
	g.block(s.Body, indent+"  ")
	fmt.Fprintf(&g.buf, "%s}\n", indent)
	fmt.Fprintf(&g.buf, "%sk_defers[k_ndefers++] = %s;\n", indent, name)
}

func (g *cgen) emitMatch(s *KIRStmt, indent string) {
	scrut := g.next()
	fmt.Fprintf(&g.buf, "%s{ KValue %s = %s;\n", indent, scrut, g.expr(s.Scrutinee))
	for i, arm := range s.Arms {
		cond := g.patternCond(arm.Pattern, scrut)
		if i == 0 {
			fmt.Fprintf(&g.buf, "%s  if (%s) {\n", indent, cond)
		} else {
			fmt.Fprintf(&g.buf, "%s  else if (%s) {\n", indent, cond)
		}
		if arm.Pattern.Binding != "" {
			inner := "*(%s.u.opt.inner)"
			if arm.Pattern.Kind == "result" {
				inner = "*(%s.u.res.inner)"
			}
			fmt.Fprintf(&g.buf, "%s    KValue %s = "+inner+";\n", indent, cBindingName(arm.Pattern.Binding), scrut)
		}
		g.block(arm.Body, indent+"    ")
		fmt.Fprintf(&g.buf, "%s  }\n", indent)
	}
	fmt.Fprintf(&g.buf, "%s  else { kfail(\"no match arm matched\"); }\n", indent)
	fmt.Fprintf(&g.buf, "%s}\n", indent)
}

func (g *cgen) patternCond(p *KIRPattern, scrut string) string {
	switch kirPatternKind(p.Kind) {
	case PatWildcard:
		return "1"
	case PatNil:
		return fmt.Sprintf("(%s.tag == K_NIL || (%s.tag == K_OPTION && !%s.u.opt.present))", scrut, scrut, scrut)
	case PatBool:
		v := "0"
		if p.Bool {
			v = "1"
		}
		return fmt.Sprintf("(%s.tag == K_BOOL && %s.u.b == %s)", scrut, scrut, v)
	case PatInt:
		return fmt.Sprintf("(%s.tag == K_INT && %s.u.i == %dLL)", scrut, scrut, p.Int)
	case PatString:
		return fmt.Sprintf("(%s.tag == K_STRING && %s.u.s.len == %d && memcmp(%s.u.s.data, %s, %d) == 0)",
			scrut, scrut, len(p.String), scrut, cString(p.String), len(p.String))
	case PatEnum:
		if p.Type != "" {
			id, ok := g.enumID[p.Type]
			if !ok {
				g.fail("unknown enum '%s'", p.Type)
				return "0"
			}
			v := g.variantIndex(p.Type, p.Variant)
			return fmt.Sprintf("(%s.tag == K_ENUM && %s.u.en.type_id == %d && %s.u.en.variant == %d)", scrut, scrut, id, scrut, v)
		}
		return fmt.Sprintf("(%s.tag == K_ENUM && %s.u.en.variant == %d)", scrut, scrut, g.variantIndexAny(p.Variant))
	case PatOption:
		v := "0"
		if p.Present {
			v = "1"
		}
		return fmt.Sprintf("(%s.tag == K_OPTION && %s.u.opt.present == %s)", scrut, scrut, v)
	case PatResult:
		v := "0"
		if p.OK {
			v = "1"
		}
		return fmt.Sprintf("(%s.tag == K_RESULT && %s.u.res.ok == %s)", scrut, scrut, v)
	}
	return "0"
}

func (g *cgen) variantIndex(enumName, variant string) int {
	for _, e := range g.enums {
		if e.Name == enumName {
			for i, v := range e.Variants {
				if v == variant {
					return i
				}
			}
		}
	}
	g.fail("unknown variant '%s::%s'", enumName, variant)
	return 0
}

func (g *cgen) variantIndexAny(variant string) int {
	for _, e := range g.enums {
		for i, v := range e.Variants {
			if v == variant {
				return i
			}
		}
	}
	g.fail("unknown variant '%s'", variant)
	return 0
}

// expr lowers an expression to a C expression of type KValue.
func (g *cgen) expr(e *KIRExpr) string {
	if e == nil {
		return "kv_nil()"
	}
	switch kirExprKind(e.Kind) {
	case ExInt:
		return fmt.Sprintf("kv_int(%dLL)", e.Int)
	case ExFloat:
		return fmt.Sprintf("kv_float(%s)", cFloat(e.Float))
	case ExBool:
		if e.Bool {
			return "kv_bool(1)"
		}
		return "kv_bool(0)"
	case ExNil:
		return "kv_nil()"
	case ExString:
		if g.obfuscate {
			return fmt.Sprintf("k_obf_str((const unsigned char*)%s, %d)", cObfString(e.String), len(e.String))
		}
		return fmt.Sprintf("kv_strn(%s, %d)", cString(e.String), len(e.String))
	case ExVar:
		return cBindingName(e.Name)
	case ExEnum:
		id, ok := g.enumID[e.EnumType]
		if !ok {
			g.fail("unknown enum '%s'", e.EnumType)
			return "kv_nil()"
		}
		return fmt.Sprintf("kv_enum(%d, %d)", id, g.variantIndex(e.EnumType, e.EnumVariant))
	case ExArray:
		return g.arrayLiteral(e.Items)
	case ExSet:
		return g.setLiteral(e.Items)
	case ExMap:
		return g.mapLiteral(e)
	case ExStruct:
		return g.structLiteral(e)
	case ExUnary:
		return g.unary(e)
	case ExBinary:
		return g.binary(e)
	case ExIndex:
		return fmt.Sprintf("k_index(%s, %s)", g.expr(e.Base), g.expr(e.Left))
	case ExField:
		return g.field(e)
	case ExPropagate:
		return g.propagate(e)
	case ExCall:
		return g.call(e)
	}
	g.fail("unsupported expression kind %q", e.Kind)
	return "kv_nil()"
}

func (g *cgen) arrayLiteral(items []*KIRExpr) string {
	if len(items) == 0 {
		return "kv_arr((KValue*)kalloc(1), 0)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "({ KValue *_a = (KValue*)kalloc(sizeof(KValue)*%d);", len(items))
	for i, it := range items {
		fmt.Fprintf(&b, " _a[%d] = %s;", i, g.expr(it))
	}
	fmt.Fprintf(&b, " kv_arr(_a, %d); })", len(items))
	return b.String()
}

func (g *cgen) setLiteral(items []*KIRExpr) string {
	if len(items) == 0 {
		return "kv_set((KValue*)kalloc(1), 0)"
	}
	// Deduplicate at runtime to match the interpreter's set semantics.
	var b strings.Builder
	fmt.Fprintf(&b, "({ KValue *_s = (KValue*)kalloc(sizeof(KValue)*%d); size_t _n = 0;", len(items))
	for _, it := range items {
		v := g.next()
		f := g.next()
		i := g.next()
		fmt.Fprintf(&b, " KValue %s = %s; int %s = 0; for (size_t %s=0;%s<_n;%s++) if (k_equal(_s[%s],%s)) %s=1; if (!%s) _s[_n++] = %s;", v, g.expr(it), f, i, i, i, i, v, f, f, v)
	}
	fmt.Fprintf(&b, " kv_set(_s, _n); })")
	return b.String()
}

func (g *cgen) mapLiteral(e *KIRExpr) string {
	if len(e.MapKeys) == 0 {
		return "kv_map((KValue*)kalloc(1), (KValue*)kalloc(1), 0)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "({ KValue *_k = (KValue*)kalloc(sizeof(KValue)*%d); KValue *_v = (KValue*)kalloc(sizeof(KValue)*%d); size_t _n = 0;", len(e.MapKeys), len(e.MapKeys))
	for i := range e.MapKeys {
		kv := g.next()
		vv := g.next()
		fmt.Fprintf(&b, " KValue %s = %s; KValue %s = %s;", kv, g.expr(e.MapKeys[i]), vv, g.expr(e.Values[i]))
		fmt.Fprintf(&b, " for (size_t _i=0;_i<_n;_i++) if (k_equal(_k[_i],%s)) kfail(\"duplicate map key\");", kv)
		fmt.Fprintf(&b, " _k[_n] = %s; _v[_n] = %s; _n++;", kv, vv)
	}
	fmt.Fprintf(&b, " kv_map(_k, _v, _n); })")
	return b.String()
}

func (g *cgen) structLiteral(e *KIRExpr) string {
	decl := g.structDecl(e.StructName)
	if decl == nil {
		g.fail("unknown struct '%s'", e.StructName)
		return "kv_nil()"
	}
	instanceType := substituteKIRType(e.Type, g.functionSubstitution)
	parsedInstance, validType := parseKIRTypeExpression(instanceType)
	if !validType || parsedInstance.Name != decl.Name {
		g.fail("C AOT cannot lower struct '%s' without resolved instantiation metadata", e.StructName)
		return "kv_nil()"
	}
	instanceName := instanceType
	id, ok := g.structID[instanceName]
	if !ok {
		g.fail("C AOT did not plan concrete struct type %s", instanceName)
		return "kv_nil()"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "({ KValue *_f = (KValue*)kalloc(sizeof(KValue)*%d);", len(decl.Fields))
	for i := range decl.Fields {
		fmt.Fprintf(&b, " _f[%d] = kv_nil();", i)
	}
	for i, name := range e.Fields {
		idx := -1
		for j, f := range decl.Fields {
			if f.Name == name {
				idx = j
			}
		}
		if idx < 0 {
			g.fail("unknown field '%s'", name)
			continue
		}
		fmt.Fprintf(&b, " _f[%d] = %s;", idx, g.expr(e.Values[i]))
	}
	fmt.Fprintf(&b, " kv_struct(%d, _f); })", id)
	return b.String()
}

func (g *cgen) structDecl(name string) *KIRStruct {
	for _, s := range g.structs {
		if s.Name == name {
			return s
		}
	}
	return nil
}

func (g *cgen) unary(e *KIRExpr) string {
	op := g.expr(e.Operand)
	switch e.Operator {
	case "!":
		return fmt.Sprintf("k_not(%s)", op)
	case "-":
		return fmt.Sprintf("k_neg(%s)", op)
	case "+":
		return op
	}
	g.fail("unsupported unary operator")
	return "kv_nil()"
}

func (g *cgen) binary(e *KIRExpr) string {
	l := g.expr(e.Left)
	r := g.expr(e.Right)
	intOperands := e.Left != nil && e.Right != nil && e.Left.Type == "Int" && e.Right.Type == "Int"
	switch e.Operator {
	case "&&":
		return fmt.Sprintf("k_and(%s, %s)", l, r)
	case "||":
		return fmt.Sprintf("k_or(%s, %s)", l, r)
	case "==":
		if intOperands {
			return fmt.Sprintf("kv_bool((%s).u.i == (%s).u.i)", l, r)
		}
		return fmt.Sprintf("k_eq(%s, %s)", l, r)
	case "!=":
		if intOperands {
			return fmt.Sprintf("kv_bool((%s).u.i != (%s).u.i)", l, r)
		}
		return fmt.Sprintf("k_neq(%s, %s)", l, r)
	case "+":
		if e.Type == "Int" {
			return fmt.Sprintf("kv_int(k_add_i((%s).u.i, (%s).u.i))", l, r)
		}
		return fmt.Sprintf("k_add(%s, %s)", l, r)
	case "-":
		if e.Type == "Int" {
			return fmt.Sprintf("kv_int(k_sub_i((%s).u.i, (%s).u.i))", l, r)
		}
		return fmt.Sprintf("k_sub(%s, %s)", l, r)
	case "*":
		if e.Type == "Int" {
			return fmt.Sprintf("kv_int(k_mul_i((%s).u.i, (%s).u.i))", l, r)
		}
		return fmt.Sprintf("k_mul(%s, %s)", l, r)
	case "/":
		if e.Type == "Int" {
			return fmt.Sprintf("kv_int(k_div_i((%s).u.i, (%s).u.i))", l, r)
		}
		return fmt.Sprintf("k_div(%s, %s)", l, r)
	case "%":
		if e.Type == "Int" {
			return fmt.Sprintf("kv_int(k_rem_i((%s).u.i, (%s).u.i))", l, r)
		}
		return fmt.Sprintf("k_rem(%s, %s)", l, r)
	case "<":
		if intOperands {
			return fmt.Sprintf("kv_bool((%s).u.i < (%s).u.i)", l, r)
		}
		return fmt.Sprintf("k_lt(%s, %s)", l, r)
	case "<=":
		if intOperands {
			return fmt.Sprintf("kv_bool((%s).u.i <= (%s).u.i)", l, r)
		}
		return fmt.Sprintf("k_le(%s, %s)", l, r)
	case ">":
		if intOperands {
			return fmt.Sprintf("kv_bool((%s).u.i > (%s).u.i)", l, r)
		}
		return fmt.Sprintf("k_gt(%s, %s)", l, r)
	case ">=":
		if intOperands {
			return fmt.Sprintf("kv_bool((%s).u.i >= (%s).u.i)", l, r)
		}
		return fmt.Sprintf("k_ge(%s, %s)", l, r)
	}
	g.fail("unsupported binary operator")
	return "kv_nil()"
}

func (g *cgen) field(e *KIRExpr) string {
	base := g.expr(e.Base)
	// Resolve the field index from the base expression's static type.
	var decl *KIRStruct
	if e.Base != nil && e.Base.Type != "" {
		if baseType, ok := parseKIRTypeExpression(e.Base.Type); ok {
			decl = g.structDecl(baseType.Name)
		}
	}
	if decl == nil {
		g.fail("field access requires a known struct type")
		return "kv_nil()"
	}
	idx := -1
	for i, f := range decl.Fields {
		if f.Name == e.Field {
			idx = i
		}
	}
	if idx < 0 {
		g.fail("unknown field '%s'", e.Field)
		return "kv_nil()"
	}
	return fmt.Sprintf("k_field(%s, %d)", base, idx)
}

func (g *cgen) propagate(e *KIRExpr) string {
	operand := g.expr(e.Operand)
	return fmt.Sprintf("({ KValue _p = %s; if (_p.tag == K_OPTION) { if (!_p.u.opt.present) return _p; _p = *_p.u.opt.inner; } else if (_p.tag == K_RESULT) { if (!_p.u.res.ok) return _p; _p = *_p.u.res.inner; } else kfail(\"'?' requires an Option or Result\"); _p; })", operand)
}

func (g *cgen) call(e *KIRExpr) string {
	if e.Receiver != nil {
		return g.methodCall(e)
	}
	if strings.HasPrefix(e.CallTarget, "builtin:") {
		name := strings.TrimPrefix(e.CallTarget, "builtin:")
		builtin, ok := g.builtins[name]
		if !ok || builtin.ID != e.BuiltinID {
			g.fail("KIR references an unknown or mismatched builtin %q", name)
			return "kv_nil()"
		}
		return g.builtinCall(e, builtin)
	}
	if !strings.HasPrefix(e.CallTarget, "function:") {
		g.fail("C AOT cannot lower indirect or invalid call target %q", e.CallTarget)
		return "kv_nil()"
	}
	target := strings.TrimPrefix(e.CallTarget, "function:")
	f := g.functionByTarget[target]
	if f == nil {
		g.fail("unknown function target '%s'", target)
		return "kv_nil()"
	}
	return g.functionCall(f, nil, e.Args, e.GenericArguments)
}

func (g *cgen) methodCall(e *KIRExpr) string {
	var f *KIRFunction
	if e.TraitName != "" {
		var err error
		f, err = g.traitImplementationMethod(e, g.functionSubstitution)
		if err != nil {
			g.fail("%s", err)
			return "kv_nil()"
		}
	} else if strings.HasPrefix(e.CallTarget, "function:") {
		f = g.functionByTarget[strings.TrimPrefix(e.CallTarget, "function:")]
	}
	if f == nil {
		g.fail("unknown method '%s'", e.Name)
		return "kv_nil()"
	}
	return g.functionCall(f, e.Receiver, e.Args, e.GenericArguments)
}

func (g *cgen) functionCall(f *KIRFunction, receiver *KIRExpr, args []*KIRExpr, genericArguments []string) string {
	name, err := g.functionSymbolForCall(f, receiver, genericArguments)
	if err != nil {
		g.fail("%s", err)
		return "kv_nil()"
	}
	var b strings.Builder
	b.WriteString("({ if (k_call_depth >= k_max_call_depth) kfail(\"call depth limit exceeded\"); KValue _frame[K_MAX_ARGS];")
	off := 0
	if receiver != nil {
		fmt.Fprintf(&b, " _frame[0] = %s;", g.expr(receiver))
		off = 1
	}
	for i, a := range args {
		fmt.Fprintf(&b, " _frame[%d] = %s;", i+off, g.expr(a))
	}
	argc := off + len(args)
	fmt.Fprintf(&b, " k_argc = %d; memcpy(k_args, _frame, sizeof(KValue)*%d); %s(); })", argc, argc, name)
	return b.String()
}

// builtinCall lowers a builtin invocation. Unsupported builtins are rejected
// with a clear diagnostic so the native backend never silently misbehaves.
func (g *cgen) builtinCall(e *KIRExpr, b Builtin) string {
	arg := func(i int) string { return g.expr(e.Args[i]) }
	switch b.Name {
	case "print":
		return fmt.Sprintf("({ k_print(%s, 0); kv_nil(); })", arg(0))
	case "println":
		return fmt.Sprintf("({ k_print(%s, 1); kv_nil(); })", arg(0))
	case "len":
		return fmt.Sprintf("k_len(%s)", arg(0))
	case "bytes":
		return fmt.Sprintf("k_bytes(%s)", arg(0))
	case "string_to_bytes":
		return fmt.Sprintf("k_string_to_bytes(%s)", arg(0))
	case "bytes_to_string":
		return fmt.Sprintf("k_bytes_to_string(%s)", arg(0))
	case "array_push":
		return fmt.Sprintf("k_array_push(%s, %s)", arg(0), arg(1))
	case "array_pop":
		return fmt.Sprintf("k_array_pop(%s)", arg(0))
	case "array_get":
		return fmt.Sprintf("k_array_get(%s, %s)", arg(0), arg(1))
	case "array_concat":
		return fmt.Sprintf("k_array_concat(%s, %s)", arg(0), arg(1))
	case "array_contains":
		return fmt.Sprintf("k_array_contains(%s, %s)", arg(0), arg(1))
	case "array_slice":
		return fmt.Sprintf("k_array_slice(%s, %s, %s)", arg(0), arg(1), arg(2))
	case "array_reverse":
		return fmt.Sprintf("k_array_reverse(%s)", arg(0))
	case "array_join":
		return fmt.Sprintf("k_array_join(%s, %s)", arg(0), arg(1))
	case "int":
		return fmt.Sprintf("k_to_int(%s)", arg(0))
	case "u8", "u16", "u32", "u64":
		bits := map[string]int{"u8": 8, "u16": 16, "u32": 32, "u64": 64}[b.Name]
		return fmt.Sprintf("k_to_uint(%s, %d)", arg(0), bits)
	case "float":
		return fmt.Sprintf("k_to_float(%s)", arg(0))
	case "str":
		return fmt.Sprintf("k_str(%s)", arg(0))
	case "bool":
		return fmt.Sprintf("k_bool(%s)", arg(0))
	case "assert":
		return fmt.Sprintf("k_assert(%s)", arg(0))
	case "assert_eq":
		return fmt.Sprintf("k_assert_eq(%s, %s)", arg(0), arg(1))
	case "abs":
		return fmt.Sprintf("k_abs(%s)", arg(0))
	case "sqrt":
		return fmt.Sprintf("k_sqrt(%s)", arg(0))
	case "min":
		return fmt.Sprintf("k_min(%s, %s)", arg(0), arg(1))
	case "max":
		return fmt.Sprintf("k_max(%s, %s)", arg(0), arg(1))
	case "floor":
		return fmt.Sprintf("k_floor(%s)", arg(0))
	case "ceil":
		return fmt.Sprintf("k_ceil(%s)", arg(0))
	case "round":
		return fmt.Sprintf("k_round(%s)", arg(0))
	case "pow":
		return fmt.Sprintf("k_pow(%s, %s)", arg(0), arg(1))
	case "log":
		return fmt.Sprintf("k_log(%s)", arg(0))
	case "sin":
		return fmt.Sprintf("k_sin(%s)", arg(0))
	case "cos":
		return fmt.Sprintf("k_cos(%s)", arg(0))
	case "is_nan":
		return fmt.Sprintf("k_is_nan(%s)", arg(0))
	case "is_finite":
		return fmt.Sprintf("k_is_finite(%s)", arg(0))
	case "is_some":
		return fmt.Sprintf("k_is_some(%s)", arg(0))
	case "is_none":
		return fmt.Sprintf("k_is_none(%s)", arg(0))
	case "is_ok":
		return fmt.Sprintf("k_is_ok(%s)", arg(0))
	case "is_err":
		return fmt.Sprintf("k_is_err(%s)", arg(0))
	case "unwrap_or":
		return fmt.Sprintf("k_unwrap_or(%s, %s)", arg(0), arg(1))
	case "some":
		return fmt.Sprintf("k_some(%s)", arg(0))
	case "none":
		return "k_none()"
	case "ok":
		return fmt.Sprintf("k_ok(%s)", arg(0))
	case "err":
		return fmt.Sprintf("k_err(%s)", arg(0))
	case "substring":
		return fmt.Sprintf("k_substring(%s, %s, %s)", arg(0), arg(1), arg(2))
	case "contains":
		return fmt.Sprintf("k_contains(%s, %s)", arg(0), arg(1))
	case "starts_with":
		return fmt.Sprintf("k_starts_with(%s, %s)", arg(0), arg(1))
	case "ends_with":
		return fmt.Sprintf("k_ends_with(%s, %s)", arg(0), arg(1))
	case "trim":
		return fmt.Sprintf("k_trim(%s)", arg(0))
	case "split":
		return fmt.Sprintf("k_split(%s, %s)", arg(0), arg(1))
	case "replace":
		return fmt.Sprintf("k_replace(%s, %s, %s)", arg(0), arg(1), arg(2))
	case "codepoints":
		return fmt.Sprintf("k_codepoints(%s)", arg(0))
	case "byte_at":
		return fmt.Sprintf("k_byte_at(%s, %s)", arg(0), arg(1))
	case "hex_encode":
		return fmt.Sprintf("k_hex_encode(%s)", arg(0))
	case "hex_decode":
		return fmt.Sprintf("k_hex_decode(%s)", arg(0))
	case "base64_encode":
		return fmt.Sprintf("k_base64_encode(%s)", arg(0))
	case "base64_decode":
		return fmt.Sprintf("k_base64_decode(%s)", arg(0))
	case "map_get":
		return fmt.Sprintf("k_map_get(%s, %s)", arg(0), arg(1))
	case "map_insert":
		return fmt.Sprintf("k_map_insert(%s, %s, %s)", arg(0), arg(1), arg(2))
	case "map_keys":
		return fmt.Sprintf("k_map_keys(%s)", arg(0))
	case "set_contains":
		return fmt.Sprintf("k_set_contains(%s, %s)", arg(0), arg(1))
	case "set_insert":
		return fmt.Sprintf("k_set_insert(%s, %s)", arg(0), arg(1))
	case "set_len":
		return fmt.Sprintf("k_set_len(%s)", arg(0))
	case "fs_read_text":
		return fmt.Sprintf("k_fs_read_text(%s)", arg(0))
	case "fs_write_text":
		return fmt.Sprintf("k_fs_write_text(%s, %s)", arg(0), arg(1))
	case "fs_read_bytes":
		return fmt.Sprintf("k_fs_read_bytes(%s)", arg(0))
	case "fs_write_bytes":
		return fmt.Sprintf("k_fs_write_bytes(%s, %s)", arg(0), arg(1))
	case "fs_exists":
		return fmt.Sprintf("k_fs_exists(%s)", arg(0))
	case "env_get":
		return fmt.Sprintf("k_env_get(%s)", arg(0))
	case "http_request":
		return fmt.Sprintf("k_http_request(%s, %s, %s)", arg(0), arg(1), arg(2))
	case "tcp_connect":
		return fmt.Sprintf("k_tcp_connect(%s, %s)", arg(0), arg(1))
	case "tcp_send":
		return fmt.Sprintf("k_tcp_send(%s, %s)", arg(0), arg(1))
	case "tcp_receive":
		return fmt.Sprintf("k_tcp_receive(%s, %s)", arg(0), arg(1))
	case "tcp_close":
		return fmt.Sprintf("k_tcp_close(%s)", arg(0))
	case "sqlite_open":
		return fmt.Sprintf("k_sqlite_open(%s)", arg(0))
	case "sqlite_exec":
		return fmt.Sprintf("k_sqlite_exec(%s, %s)", arg(0), arg(1))
	case "sqlite_query":
		return fmt.Sprintf("k_sqlite_query(%s, %s)", arg(0), arg(1))
	case "sqlite_close":
		return fmt.Sprintf("k_sqlite_close(%s)", arg(0))
	case "json_parse":
		return fmt.Sprintf("k_json_parse(%s)", arg(0))
	case "json_stringify":
		return fmt.Sprintf("k_json_stringify(%s)", arg(0))
	case "json_kind":
		return fmt.Sprintf("k_json_kind(%s)", arg(0))
	case "json_object_get":
		return fmt.Sprintf("k_json_object_get(%s, %s)", arg(0), arg(1))
	case "json_object_keys":
		return fmt.Sprintf("k_json_object_keys(%s)", arg(0))
	case "json_object_fields_empty_except":
		return fmt.Sprintf("k_json_object_fields_empty_except(%s, %s, %s, %s)", arg(0), arg(1), arg(2), arg(3))
	case "json_array_len":
		return fmt.Sprintf("k_json_array_len(%s)", arg(0))
	case "json_array_get":
		return fmt.Sprintf("k_json_array_get(%s, %s)", arg(0), arg(1))
	case "json_string":
		return fmt.Sprintf("k_json_string_value(%s)", arg(0))
	case "json_int":
		return fmt.Sprintf("k_json_int(%s)", arg(0))
	case "json_uint":
		return fmt.Sprintf("k_json_uint(%s)", arg(0))
	case "json_float":
		return fmt.Sprintf("k_json_to_float(%s)", arg(0))
	case "json_bool":
		return fmt.Sprintf("k_json_bool(%s)", arg(0))
	case "json_is_null":
		return fmt.Sprintf("k_json_is_null(%s)", arg(0))
	case "crypto_sha256":
		return fmt.Sprintf("k_crypto_sha256(%s)", arg(0))
	case "crypto_hmac_sha256":
		return fmt.Sprintf("k_crypto_hmac_sha256(%s, %s)", arg(0), arg(1))
	case "crypto_random_bytes":
		return fmt.Sprintf("k_crypto_random_bytes(%s)", arg(0))
	case "crypto_sha512":
		return fmt.Sprintf("k_crypto_sha512(%s)", arg(0))
	case "crypto_sha384":
		return fmt.Sprintf("k_crypto_sha384(%s)", arg(0))
	case "crypto_sha1":
		return fmt.Sprintf("k_crypto_sha1(%s)", arg(0))
	case "crypto_md5":
		return fmt.Sprintf("k_crypto_md5(%s)", arg(0))
	case "crypto_aes_gcm_encrypt":
		return fmt.Sprintf("k_crypto_aes_gcm_encrypt(%s, %s, %s)", arg(0), arg(1), arg(2))
	case "crypto_aes_gcm_decrypt":
		return fmt.Sprintf("k_crypto_aes_gcm_decrypt(%s, %s, %s)", arg(0), arg(1), arg(2))
	case "crypto_pbkdf2_sha256":
		return fmt.Sprintf("k_crypto_pbkdf2_sha256(%s, %s, %s, %s)", arg(0), arg(1), arg(2), arg(3))
	case "crypto_hkdf_sha256":
		return fmt.Sprintf("k_crypto_hkdf_sha256(%s, %s, %s, %s)", arg(0), arg(1), arg(2), arg(3))
	case "crypto_constant_time_equal":
		return fmt.Sprintf("k_crypto_constant_time_equal(%s, %s)", arg(0), arg(1))
	case "crypto_xor":
		return fmt.Sprintf("k_crypto_xor(%s, %s)", arg(0), arg(1))
	case "base64url_encode":
		return fmt.Sprintf("k_base64url_encode(%s)", arg(0))
	case "base64url_decode":
		return fmt.Sprintf("k_base64url_decode(%s)", arg(0))
	case "string_slice":
		return fmt.Sprintf("k_string_slice(%s, %s, %s)", arg(0), arg(1), arg(2))
	case "array_slice_range":
		return fmt.Sprintf("k_array_slice_range(%s, %s, %s)", arg(0), arg(1), arg(2))
	case "string_format":
		return fmt.Sprintf("k_string_format(%s, %s)", arg(0), arg(1))
	case "array_indices":
		return fmt.Sprintf("k_array_indices(%s)", arg(0))
	case "array_zip":
		return fmt.Sprintf("k_array_zip(%s, %s)", arg(0), arg(1))
	case "fs_read_dir":
		return fmt.Sprintf("k_fs_read_dir(%s)", arg(0))
	case "fs_create_dir":
		return fmt.Sprintf("k_fs_create_dir(%s)", arg(0))
	case "fs_create_dir_all":
		return fmt.Sprintf("k_fs_create_dir_all(%s)", arg(0))
	case "fs_remove_file":
		return fmt.Sprintf("k_fs_remove_file(%s)", arg(0))
	case "fs_remove_dir_all":
		return fmt.Sprintf("k_fs_remove_dir_all(%s)", arg(0))
	case "fs_copy_file":
		return fmt.Sprintf("k_fs_copy_file(%s, %s)", arg(0), arg(1))
	case "fs_move_file":
		return fmt.Sprintf("k_fs_move_file(%s, %s)", arg(0), arg(1))
	case "fs_is_file":
		return fmt.Sprintf("k_fs_is_file(%s)", arg(0))
	case "fs_is_dir":
		return fmt.Sprintf("k_fs_is_dir(%s)", arg(0))
	case "fs_file_size":
		return fmt.Sprintf("k_fs_file_size(%s)", arg(0))
	case "fs_file_modified_time":
		return fmt.Sprintf("k_fs_file_modified_time(%s)", arg(0))
	case "fs_join_path":
		return fmt.Sprintf("k_fs_join_path(%s, %s)", arg(0), arg(1))
	case "fs_absolute_path":
		return fmt.Sprintf("k_fs_absolute_path(%s)", arg(0))
	case "fs_temp_dir":
		return "k_fs_temp_dir(kv_nil())"
	case "fs_temp_file":
		return fmt.Sprintf("k_fs_temp_file(%s)", arg(0))
	case "sleep_ms":
		return fmt.Sprintf("k_sleep(%s)", arg(0))
	case "yield_now":
		return "k_yield_now()"
	case "process_run":
		return fmt.Sprintf("k_process_run(%s, %s)", arg(0), arg(1))
	case "string_repeat":
		return fmt.Sprintf("k_string_repeat(%s, %s)", arg(0), arg(1))
	case "string_index_of":
		return fmt.Sprintf("k_string_index_of(%s, %s)", arg(0), arg(1))
	case "string_pad_start":
		return fmt.Sprintf("k_string_pad(%s, %s, %s, 1)", arg(0), arg(1), arg(2))
	case "string_pad_end":
		return fmt.Sprintf("k_string_pad(%s, %s, %s, 0)", arg(0), arg(1), arg(2))
	case "string_lines":
		return fmt.Sprintf("k_string_lines(%s)", arg(0))
	case "string_chars":
		return fmt.Sprintf("k_string_chars(%s)", arg(0))
	case "string_to_upper":
		return fmt.Sprintf("k_string_case(%s, 1)", arg(0))
	case "string_to_lower":
		return fmt.Sprintf("k_string_case(%s, 0)", arg(0))
	case "array_sort":
		return fmt.Sprintf("k_array_sort(%s)", arg(0))
	case "array_index_of":
		return fmt.Sprintf("k_array_index_of(%s, %s)", arg(0), arg(1))
	case "array_sum":
		return fmt.Sprintf("k_array_sum(%s)", arg(0))
	case "array_min":
		return fmt.Sprintf("k_array_minmax(%s, 0)", arg(0))
	case "array_max":
		return fmt.Sprintf("k_array_minmax(%s, 1)", arg(0))
	case "array_take":
		return fmt.Sprintf("k_array_take_drop(%s, %s, 1)", arg(0), arg(1))
	case "array_drop":
		return fmt.Sprintf("k_array_take_drop(%s, %s, 0)", arg(0), arg(1))
	case "map_contains_key":
		return fmt.Sprintf("k_map_contains_key(%s, %s)", arg(0), arg(1))
	case "map_values":
		return fmt.Sprintf("k_map_values(%s)", arg(0))
	case "map_remove":
		return fmt.Sprintf("k_map_remove(%s, %s)", arg(0), arg(1))
	case "set_remove":
		return fmt.Sprintf("k_set_remove(%s, %s)", arg(0), arg(1))
	case "set_to_array":
		return fmt.Sprintf("k_set_to_array(%s)", arg(0))
	case "tan":
		return fmt.Sprintf("k_tan(%s)", arg(0))
	case "atan":
		return fmt.Sprintf("k_atan(%s)", arg(0))
	case "atan2":
		return fmt.Sprintf("k_atan2(%s, %s)", arg(0), arg(1))
	case "exp":
		return fmt.Sprintf("k_exp(%s)", arg(0))
	case "log10":
		return fmt.Sprintf("k_log10(%s)", arg(0))
	case "log2":
		return fmt.Sprintf("k_log2(%s)", arg(0))
	case "trunc":
		return fmt.Sprintf("k_trunc(%s)", arg(0))
	case "sign":
		return fmt.Sprintf("k_sign(%s)", arg(0))
	case "clamp":
		return fmt.Sprintf("k_clamp(%s, %s, %s)", arg(0), arg(1), arg(2))
	case "shared_new":
		return fmt.Sprintf("k_shared_new(%s)", arg(0))
	case "shared_read":
		return fmt.Sprintf("k_shared_read(%s)", arg(0))
	case "shared_write":
		return fmt.Sprintf("k_shared_write(%s, %s)", arg(0), arg(1))
	case "shared_swap":
		return fmt.Sprintf("k_shared_swap(%s, %s)", arg(0), arg(1))
	case "actor_channel":
		return "k_actor_channel()"
	case "actor_channel_with_capacity":
		return fmt.Sprintf("k_actor_channel_cap(%s)", arg(0))
	case "actor_send":
		return fmt.Sprintf("k_actor_send(%s, %s)", arg(0), arg(1))
	case "actor_try_receive":
		return fmt.Sprintf("k_actor_try_receive(%s)", arg(0))
	case "actor_receive_timeout":
		return fmt.Sprintf("k_actor_receive_timeout(%s, %s)", arg(0), arg(1))
	case "actor_close":
		return fmt.Sprintf("k_actor_close(%s)", arg(0))
	case "task_group":
		return "k_task_group()"
	case "task_spawn":
		return g.taskSpawn(e)
	case "task_group_cancel":
		return fmt.Sprintf("k_task_group_cancel(%s)", arg(0))
	case "task_group_wait":
		return fmt.Sprintf("k_task_group_wait(%s)", arg(0))
	case "thread_spawn":
		return g.threadSpawn(e)
	case "await":
		return fmt.Sprintf("k_await(%s)", arg(0))
	case "await_timeout":
		return fmt.Sprintf("k_await_timeout(%s, %s)", arg(0), arg(1))
	case "poly_register":
		return g.polyRegister(e)
	case "poly_reorder":
		return g.polyReorder(e)
	case "poly_dispatch":
		return fmt.Sprintf("k_poly_dispatch(%s, %s)", arg(0), arg(1))
	}
	g.fail("builtin '%s' is not supported by the C AOT backend; use the interpreter for this host integration", b.Name)
	return "kv_nil()"
}

// threadSpawn lowers thread_spawn("worker") to a thread handle wrapping the
// worker function pointer.
func (g *cgen) threadSpawn(e *KIRExpr) string {
	if len(e.Args) != 1 || e.Args[0].Kind != "string" {
		g.fail("thread_spawn requires a literal worker function name")
		return "kv_nil()"
	}
	f := g.uniqueFunction(e.Args[0].String)
	if f == nil {
		g.fail("thread_spawn references unknown or overloaded worker '%s'", e.Args[0].String)
		return "kv_nil()"
	}
	if g.functionNeedsSpecialization(f) {
		g.fail("C AOT does not support generic worker function values without explicit type arguments")
		return "kv_nil()"
	}
	return fmt.Sprintf("k_thread_spawn(%s)", g.fnName[f])
}

// taskSpawn lowers task_spawn(group, "worker") to a task-group-owned thread.
func (g *cgen) taskSpawn(e *KIRExpr) string {
	if len(e.Args) != 2 || e.Args[1].Kind != "string" {
		g.fail("task_spawn requires a literal worker function name")
		return "kv_nil()"
	}
	f := g.uniqueFunction(e.Args[1].String)
	if f == nil {
		g.fail("task_spawn references unknown or overloaded worker '%s'", e.Args[1].String)
		return "kv_nil()"
	}
	if g.functionNeedsSpecialization(f) {
		g.fail("C AOT does not support generic worker function values without explicit type arguments")
		return "kv_nil()"
	}
	return fmt.Sprintf("k_task_spawn(%s, %s, 0, 0)", g.expr(e.Args[0]), g.fnName[f])
}

// polyRegister lowers poly_register(slot, handler, priority). The checker has
// already constrained handler to an unambiguous literal function name.
func (g *cgen) polyRegister(e *KIRExpr) string {
	return fmt.Sprintf("k_poly_register(%s, %s, %s)", g.expr(e.Args[0]), g.expr(e.Args[1]), g.expr(e.Args[2]))
}

// polyReorder lowers poly_reorder(slot, handler, before).
func (g *cgen) polyReorder(e *KIRExpr) string {
	return fmt.Sprintf("k_poly_reorder(%s, %s, %s)", g.expr(e.Args[0]), g.expr(e.Args[1]), g.expr(e.Args[2]))
}

// cString renders a Go string as a C string literal.
func cString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString("\\\"")
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		default:
			if c < 0x20 || c >= 0x7f {
				fmt.Fprintf(&b, "\\%03o", c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// cObfString renders a string as a C literal whose bytes are XOR-masked with
// the same mask k_obf_str reverses at runtime. The mask depends only on the
// length and index, so no key is embedded and the plaintext never appears in
// the binary.
func cObfString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	n := len(s)
	for i := 0; i < n; i++ {
		c := s[i] ^ byte(0x5a+(i*31)+(n*7))
		fmt.Fprintf(&b, "\\x%02x", c)
	}
	b.WriteByte('"')
	return b.String()
}

// cFloat renders a float64 as a C double literal using its exact hexadecimal
// form so the compiler reproduces the identical bit pattern.
func cFloat(f float64) string {
	// strconv.FormatFloat with 'x' yields a C99 hexadecimal floating literal
	// (e.g. 0x1.921f9f01b866ep+01) that reproduces the exact IEEE-754 bits.
	return strconv.FormatFloat(f, 'x', -1, 64)
}

// sortedStructNames is used by tests to keep metadata deterministic.
func sortedStructNames(p *Program) []string {
	names := make([]string, 0, len(p.Structs))
	for _, s := range p.Structs {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	return names
}
