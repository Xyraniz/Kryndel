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
	function *KIRFunction
	target   string
	label    int
	slots    map[kirPEBindingID]machineSlot
	nextSlot int32
}

type kirPEValidatedProgram struct {
	entry     []*KIRStmt
	main      *KIRFunction
	functions []*kirPEFunction
	byTarget  map[string]*kirPEFunction
}

// lowerDirectPEKIR accepts only validated MIR. Static output and capability
// checks use the typed arena; dynamic PE traversal still uses a compatibility
// document while that lowering path is being migrated.
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
	document, err := mir.documentView()
	if err != nil {
		return nil, err
	}
	validated, err := validateKIRDirectPE(document)
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
	entrySlots, entryNext, err := collectKIRPEBindings(validated.entry, nil, "<entry>")
	if err != nil {
		return nil, fmt.Errorf("direct PE entry setup: %w", err)
	}
	entry := &kirPEFunction{target: "<entry>", slots: entrySlots, nextSlot: entryNext}
	if err := collectKIRPEFunctions(validated.functions); err != nil {
		return nil, fmt.Errorf("direct PE function setup: %w", err)
	}
	lowerer := &kirPEMachine{machine: machine, program: validated, entry: entry, maxWallTimeMS: limits.MaxWallTimeMS}
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
	if document.Target.GUI {
		pePut16(image, peSubsystemOffset, peSubsystemGUI)
	}
	return image, nil
}

type kirPEMachine struct {
	machine       *directMachine
	program       *kirPEValidatedProgram
	entry         *kirPEFunction
	current       *kirPEFunction
	scopes        []map[kirPEBindingID]machineSlot
	statementSlot map[*KIRStmt]machineSlot
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

func validateKIRDirectPE(document *KIRDocument) (*kirPEValidatedProgram, error) {
	if document == nil {
		return nil, fmt.Errorf("missing validated KIR document")
	}
	if document.Format != KIRFormat || document.Version <= 0 || document.Version > KIRVersion {
		return nil, fmt.Errorf("unsupported validated KIR header")
	}
	if document.Target.OS != "windows" || document.Target.Arch != "amd64" {
		return nil, fmt.Errorf("direct PE backend requires windows-amd64")
	}
	if len(document.Imports) != 0 || len(document.Structs) != 0 || len(document.Enums) != 0 {
		return nil, fmt.Errorf("module imports, structs, and enums are not supported")
	}
	program := &kirPEValidatedProgram{entry: document.Statements, byTarget: map[string]*kirPEFunction{}}
	if len(document.Statements) > 0 {
		for _, function := range document.Functions {
			if function != nil && function.Name == "main" {
				return nil, fmt.Errorf("direct PE backend does not support both top-level statements and main")
			}
		}
	} else {
		for _, function := range document.Functions {
			if function == nil || function.Name != "main" {
				continue
			}
			if program.main != nil {
				return nil, fmt.Errorf("direct PE backend has duplicate main functions")
			}
			program.main = function
		}
		if program.main == nil {
			return nil, fmt.Errorf("program requires top-level statements or main() -> Nil")
		}
		if len(program.main.Params) != 0 || program.main.Return != "Nil" {
			return nil, fmt.Errorf("main must have signature main() -> Nil")
		}
		program.entry = program.main.Body
	}

	nameCounts := map[string]int{}
	for _, function := range document.Functions {
		if function != nil {
			nameCounts[function.Name]++
		}
	}
	for _, function := range document.Functions {
		if function == nil {
			return nil, fmt.Errorf("function metadata is missing")
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
		for _, parameter := range function.Params {
			if parameter == nil || parameter.Binding == nil {
				return nil, fmt.Errorf("function '%s' has incomplete parameter binding metadata", function.Name)
			}
			if !directPETypeName(parameter.Type, false) || parameter.Binding.Type != parameter.Type {
				return nil, fmt.Errorf("function '%s' parameter '%s' has unsupported type %s", function.Name, parameter.Name, parameter.Type)
			}
		}
	}
	for _, function := range document.Functions {
		if function.Name == "main" {
			continue
		}
		target := function.Name
		if nameCounts[function.Name] > 1 {
			target = kirFunctionTargetFromDocument(function)
		}
		if _, duplicate := program.byTarget[target]; duplicate {
			return nil, fmt.Errorf("function '%s' has duplicate KIR call target %q", function.Name, target)
		}
		entry := &kirPEFunction{function: function, target: target}
		program.byTarget[target] = entry
		program.functions = append(program.functions, entry)
	}
	if err := validateKIRDirectPEStatements(program.entry, false, false, program.byTarget); err != nil {
		return nil, err
	}
	for _, function := range program.functions {
		if err := validateKIRDirectPEStatements(function.function.Body, true, false, program.byTarget); err != nil {
			return nil, fmt.Errorf("function '%s': %w", function.function.Name, err)
		}
	}
	return program, nil
}

func validateKIRDirectPEStatements(statements []*KIRStmt, inFunction, inLoop bool, functions map[string]*kirPEFunction) error {
	for _, statement := range statements {
		if statement == nil {
			return fmt.Errorf("missing statement metadata")
		}
		switch statement.Kind {
		case "let", "const":
			if statement.Init == nil || statement.Binding == nil || statement.Binding.Name != statement.Name || statement.Binding.Mutable != statement.Mutable || statement.Binding.Type == "" || !directPETypeName(statement.Binding.Type, false) || statement.Init.Type != statement.Binding.Type {
				return fmt.Errorf("binding '%s' has an unsupported or incomplete type", statement.Name)
			}
			if err := validateKIRDirectPEExpr(statement.Init, false, functions); err != nil {
				return err
			}
		case "assign":
			if statement.Target == nil || statement.Target.Kind != "var" || statement.Target.Binding == nil || statement.Value == nil || statement.Value.Type == "" || !directPETypeName(statement.Value.Type, false) || statement.Target.Binding.Type != statement.Value.Type {
				return fmt.Errorf("assignment requires a scalar or String binding")
			}
			if err := validateKIRDirectPEExpr(statement.Value, false, functions); err != nil {
				return err
			}
		case "expr":
			if statement.Expr == nil || statement.Expr.Kind != "call" || statement.Expr.Receiver != nil {
				return fmt.Errorf("expression statements must be direct calls")
			}
			if strings.HasPrefix(statement.Expr.CallTarget, "builtin:") && (statement.Expr.CallTarget == "builtin:print" || statement.Expr.CallTarget == "builtin:println") {
				if err := validateKIRDirectPEExpr(statement.Expr, true, functions); err != nil {
					return err
				}
			} else if err := validateKIRDirectPEExpr(statement.Expr, false, functions); err != nil {
				return err
			}
		case "if":
			if statement.Cond == nil || statement.Cond.Type != "Bool" {
				return fmt.Errorf("if condition must be Bool")
			}
			if err := validateKIRDirectPEExpr(statement.Cond, false, functions); err != nil {
				return err
			}
			if err := validateKIRDirectPEStatements(statement.Then, inFunction, inLoop, functions); err != nil {
				return err
			}
			if err := validateKIRDirectPEStatements(statement.Else, inFunction, inLoop, functions); err != nil {
				return err
			}
		case "while":
			if statement.Cond == nil || statement.Cond.Type != "Bool" {
				return fmt.Errorf("while condition must be Bool")
			}
			if err := validateKIRDirectPEExpr(statement.Cond, false, functions); err != nil {
				return err
			}
			if err := validateKIRDirectPEStatements(statement.Body, inFunction, true, functions); err != nil {
				return err
			}
		case "break", "continue":
			if !inLoop {
				return fmt.Errorf("%s is outside a loop", statement.Kind)
			}
		case "return":
			if statement.Return != nil && statement.Return.Kind != "nil" {
				if err := validateKIRDirectPEExpr(statement.Return, false, functions); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("statement kind %s is not supported", statement.Kind)
		}
	}
	return nil
}

func validateKIRDirectPEExpr(expression *KIRExpr, allowOutput bool, functions map[string]*kirPEFunction) error {
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
		if expression.Binding == nil || expression.Binding.Name != expression.Name || expression.Binding.Type != expression.Type {
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
		return validateKIRDirectPEExpr(expression.Operand, false, functions)
	case "binary":
		if expression.Type == "String" || expression.Type == "Nil" {
			return fmt.Errorf("String concatenation and non-scalar binary operations are not supported")
		}
		if expression.Left != nil && expression.Left.Type == "String" {
			return fmt.Errorf("String comparison is not supported by the direct PE runtime")
		}
		switch expression.Operator {
		case "+", "-", "*", "/", "%", "&", "^", "|", "<<", ">>", "&&", "||", "==", "!=", "<", "<=", ">", ">=":
		default:
			return fmt.Errorf("binary operator %s is not supported", expression.Operator)
		}
		if err := validateKIRDirectPEExpr(expression.Left, false, functions); err != nil {
			return err
		}
		return validateKIRDirectPEExpr(expression.Right, false, functions)
	case "call":
		if expression.Receiver != nil || expression.Callee != nil || expression.CallTarget == "" {
			return fmt.Errorf("receiver calls and function values are not supported")
		}
		if strings.HasPrefix(expression.CallTarget, "function:") {
			target := strings.TrimPrefix(expression.CallTarget, "function:")
			entry, ok := functions[target]
			if !ok {
				return fmt.Errorf("function call %q has no supported KIR target", target)
			}
			function := entry.function
			if len(expression.Args) != len(function.Params) || len(expression.Args) > directPEWindowsMaxArgs {
				return fmt.Errorf("function '%s' must be called with all arguments and at most %d parameters", function.Name, directPEWindowsMaxArgs)
			}
			for index, argument := range expression.Args {
				if argument == nil || argument.Type != function.Params[index].Type {
					return fmt.Errorf("function '%s' argument %d is missing matching checked type metadata", function.Name, index+1)
				}
				if err := validateKIRDirectPEExpr(argument, false, functions); err != nil {
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
			if argument == nil || (argument.Type != "Int" && !strings.HasPrefix(argument.Type, "UInt")) {
				return fmt.Errorf("conversion %s requires an Int or UInt argument", expression.Name)
			}
			return validateKIRDirectPEExpr(argument, false, functions)
		case "builtin:str":
			if len(expression.Args) != 1 || expression.Type != "String" {
				return fmt.Errorf("conversion str must have one argument and return String")
			}
			argument := expression.Args[0]
			if argument == nil || (argument.Type != "Int" && argument.Type != "Bool" && argument.Type != "String" && !strings.HasPrefix(argument.Type, "UInt")) {
				return fmt.Errorf("direct PE str supports Int, UInt, Bool, and String values")
			}
			return validateKIRDirectPEExpr(argument, false, functions)
		case "builtin:print", "builtin:println":
			if !allowOutput || len(expression.Args) != 1 {
				return fmt.Errorf("only statement-form print(value) and println(value) are supported")
			}
			return validateKIRDirectPEExpr(expression.Args[0], false, functions)
		default:
			return fmt.Errorf("builtin %q is not supported by the direct PE backend", expression.Name)
		}
	default:
		return fmt.Errorf("expression kind %s is not supported", expression.Kind)
	}
}

func collectKIRPEBindings(statements []*KIRStmt, params []*KIRParam, function string) (map[kirPEBindingID]machineSlot, int32, error) {
	slots := make(map[kirPEBindingID]machineSlot)
	var next int32
	for index, parameter := range params {
		if parameter == nil || parameter.Binding == nil {
			return nil, 0, fmt.Errorf("parameter binding metadata is missing")
		}
		next += 8
		if _, duplicate := slots[kirPEBindingKey(parameter.Binding)]; duplicate {
			return nil, 0, fmt.Errorf("function '%s' has duplicate parameter binding metadata", function)
		}
		slots[kirPEBindingKey(parameter.Binding)] = machineSlot{offset: int32((index + 1) * 8), typ: kirPEType(parameter.Type)}
	}
	statementSlots := map[*KIRStmt]machineSlot{}
	if err := collectKIRPEBlock(statements, []map[kirPEBindingID]bool{{}}, &next, statementSlots); err != nil {
		return nil, 0, err
	}
	for statement, slot := range statementSlots {
		slots[kirPEBindingKey(statement.Binding)] = slot
	}
	return slots, next, nil
}

func collectKIRPEBlock(statements []*KIRStmt, scopes []map[kirPEBindingID]bool, next *int32, slots map[*KIRStmt]machineSlot) error {
	for _, statement := range statements {
		if statement == nil {
			return fmt.Errorf("missing statement metadata")
		}
		switch statement.Kind {
		case "let", "const":
			key := kirPEBindingKey(statement.Binding)
			current := scopes[len(scopes)-1]
			if _, duplicate := current[key]; duplicate {
				return fmt.Errorf("duplicate binding metadata for '%s'", statement.Name)
			}
			current[key] = true
			*next += 8
			slots[statement] = machineSlot{offset: *next, typ: kirPEType(statement.Binding.Type)}
		case "if":
			if err := collectKIRPEBlock(statement.Then, append(scopes, map[kirPEBindingID]bool{}), next, slots); err != nil {
				return err
			}
			if err := collectKIRPEBlock(statement.Else, append(scopes, map[kirPEBindingID]bool{}), next, slots); err != nil {
				return err
			}
		case "while":
			if err := collectKIRPEBlock(statement.Body, append(scopes, map[kirPEBindingID]bool{}), next, slots); err != nil {
				return err
			}
		}
	}
	return nil
}

func collectKIRPEFunctions(functions []*kirPEFunction) error {
	for _, entry := range functions {
		if entry == nil || entry.function == nil {
			return fmt.Errorf("function metadata is missing")
		}
		slots, next, err := collectKIRPEBindings(entry.function.Body, entry.function.Params, entry.function.Name)
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
	for _, parameter := range function.function.Params {
		if parameter == nil || parameter.Binding == nil || function.slots[kirPEBindingKey(parameter.Binding)].typ == nil {
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
	lowerer.statementSlot = map[*KIRStmt]machineSlot{}
	lowerer.loops = nil
	emitPEWallClockStart(machine, lowerer.maxWallTimeMS)
	if err := lowerer.emitWallClockCheck(); err != nil {
		return err
	}
	if err := lowerer.emitStatements(lowerer.program.entry, lowerer.program.main == nil); err != nil {
		return err
	}
	if lowerer.program.main != nil {
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
		lowerer.statementSlot = map[*KIRStmt]machineSlot{}
		machine.bufferOffset = function.nextSlot + 1
		for index, parameter := range function.function.Params {
			key := kirPEBindingKey(parameter.Binding)
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

func (lowerer *kirPEMachine) lookupSlot(binding *KIRBinding) (machineSlot, bool) {
	key := kirPEBindingKey(binding)
	for scope := len(lowerer.scopes) - 1; scope >= 0; scope-- {
		if slot, ok := lowerer.scopes[scope][key]; ok {
			return slot, true
		}
	}
	return machineSlot{}, false
}

func (lowerer *kirPEMachine) emitScopedStatements(statements []*KIRStmt) error {
	lowerer.scopes = append(lowerer.scopes, map[kirPEBindingID]machineSlot{})
	err := lowerer.emitStatements(statements, false)
	lowerer.scopes = lowerer.scopes[:len(lowerer.scopes)-1]
	return err
}

func (lowerer *kirPEMachine) emitStatements(statements []*KIRStmt, topLevel bool) error {
	for _, statement := range statements {
		if statement == nil {
			return fmt.Errorf("direct PE lowering encountered missing statement metadata")
		}
		if err := lowerer.emitWallClockCheck(); err != nil {
			return err
		}
		switch statement.Kind {
		case "let", "const":
			if err := lowerer.emitExpr(statement.Init); err != nil {
				return err
			}
			slot, ok := lowerer.current.slots[kirPEBindingKey(statement.Binding)]
			if !ok {
				return fmt.Errorf("direct PE has no slot for binding '%s'", statement.Name)
			}
			lowerer.machine.emitStoreSlot(slot)
			lowerer.bindSlot(kirPEBindingKey(statement.Binding), slot)
		case "assign":
			if err := lowerer.emitExpr(statement.Value); err != nil {
				return err
			}
			slot, ok := lowerer.lookupSlot(statement.Target.Binding)
			if !ok {
				return fmt.Errorf("direct PE has no storage for binding '%s'", statement.Target.Name)
			}
			lowerer.machine.emitStoreSlot(slot)
		case "expr":
			if statement.Expr.CallTarget == "builtin:print" || statement.Expr.CallTarget == "builtin:println" {
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
			if len(statement.Else) != 0 {
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
			if statement.Return != nil && statement.Return.Kind != "nil" {
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

func (lowerer *kirPEMachine) emitOutput(expression *KIRExpr) error {
	if len(expression.Args) != 1 {
		return fmt.Errorf("direct PE %s expects one argument", expression.Name)
	}
	argument := expression.Args[0]
	if err := lowerer.emitExpr(argument); err != nil {
		return err
	}
	newline := expression.CallTarget == "builtin:println"
	switch argument.Type {
	case "String":
		return lowerer.machine.emitStringOutput(newline)
	case "Bool":
		return lowerer.emitBooleanOutput(newline)
	default:
		return lowerer.machine.emitInteger(strings.HasPrefix(argument.Type, "UInt"), newline)
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

func (lowerer *kirPEMachine) emitStringConversion(argument *KIRExpr) error {
	if argument == nil {
		return fmt.Errorf("direct PE str is missing its value")
	}
	if err := lowerer.emitExpr(argument); err != nil {
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

func (lowerer *kirPEMachine) emitExpr(expression *KIRExpr) error {
	if expression == nil {
		return fmt.Errorf("direct PE cannot lower a missing KIR expression")
	}
	if expression.Const != nil {
		switch expression.Const.Kind {
		case "int":
			lowerer.machine.emitMoveImmediate(uint64(expression.Const.Int))
			return nil
		case "uint":
			lowerer.machine.emitMoveImmediate(expression.Const.UInt)
			lowerer.machine.emitUIntMask(expression.Const.UIntBits)
			return nil
		case "bool":
			if expression.Const.Bool {
				lowerer.machine.emitMoveImmediate(1)
			} else {
				lowerer.machine.emitMoveImmediate(0)
			}
			return nil
		case "string":
			lowerer.machine.emitStringAddress(expression.Const.String)
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

func (lowerer *kirPEMachine) emitUnsignedConversionCheck(value *KIRExpr, bits uint8) error {
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

func (lowerer *kirPEMachine) emitBinary(expression *KIRExpr) error {
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
	leftType := kirPEType(expression.Left.Type)
	unsigned := strings.HasPrefix(expression.Left.Type, "UInt")
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

func (lowerer *kirPEMachine) emitFunctionCall(expression *KIRExpr) error {
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
