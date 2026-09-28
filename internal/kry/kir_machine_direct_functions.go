package kry

import (
	"encoding/binary"
	"fmt"
	"strings"
)

func kirDirectFunctionTarget(function *KIRFunction, counts map[string]int) string {
	if function == nil {
		return ""
	}
	if counts[function.Name] > 1 {
		return kirFunctionTargetFromDocument(function)
	}
	return function.Name
}

// directKIRLegacySubsetRejection preserves the public scalar validator's
// established diagnostics before the expanded, private helper-function slice
// validates its additional KIR contracts.
func directKIRLegacySubsetRejection(document *KIRDocument) error {
	if document == nil {
		return nil
	}
	if len(document.Statements) != 0 && len(document.Functions) != 0 {
		return fmt.Errorf("%w: direct ELF backend does not execute top-level function declarations", errKIRSubsetUnsupported)
	}
	for _, function := range document.Functions {
		if function != nil && len(function.Captures) != 0 {
			return fmt.Errorf("%w: direct ELF backend does not lower lambdas or captured bindings", errKIRSubsetUnsupported)
		}
	}
	seenExpressions := map[*KIRExpr]bool{}
	var visitExpression func(*KIRExpr) error
	var visitBlock func([]*KIRStmt) error
	visitExpression = func(expression *KIRExpr) error {
		if expression == nil || seenExpressions[expression] {
			return nil
		}
		seenExpressions[expression] = true
		if expression.Kind == "lambda" || expression.Lambda != nil || strings.HasPrefix(expression.Type, "fn(") {
			return fmt.Errorf("%w: direct ELF backend does not lower lambdas or captured bindings", errKIRSubsetUnsupported)
		}
		for _, child := range []*KIRExpr{expression.Left, expression.Right, expression.Operand, expression.Base, expression.Receiver, expression.Callee} {
			if err := visitExpression(child); err != nil {
				return err
			}
		}
		for _, list := range [][]*KIRExpr{expression.Args, expression.Items, expression.MapKeys, expression.Values} {
			for _, child := range list {
				if err := visitExpression(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	visitBlock = func(statements []*KIRStmt) error {
		for _, statement := range statements {
			if statement == nil {
				continue
			}
			for _, expression := range []*KIRExpr{statement.Init, statement.Expr, statement.Target, statement.Value, statement.Cond, statement.Iter, statement.Return, statement.Scrutinee} {
				if err := visitExpression(expression); err != nil {
					return err
				}
			}
			for _, nested := range [][]*KIRStmt{statement.Then, statement.Else, statement.Body} {
				if err := visitBlock(nested); err != nil {
					return err
				}
			}
			for _, arm := range statement.Arms {
				if arm != nil {
					if err := visitBlock(arm.Body); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	if err := visitBlock(document.Statements); err != nil {
		return err
	}
	for _, function := range document.Functions {
		if function != nil {
			if err := visitBlock(function.Body); err != nil {
				return err
			}
		}
	}
	functionMap := kirExecFunctions(document)
	entryFunction, _ := kirExecEntryFunction(document, functionMap)
	for _, function := range document.Functions {
		if function != nil && function != entryFunction {
			return fmt.Errorf("%w: direct ELF backend helper functions are not lowered", errKIRSubsetUnsupported)
		}
	}
	return nil
}

func (builder *kirDirectBuilder) prepareFunctions() error {
	counts := make(map[string]int, len(builder.document.Functions))
	for _, function := range builder.document.Functions {
		if function != nil {
			counts[function.Name]++
		}
	}
	for _, function := range builder.document.Functions {
		if function == nil || function == builder.entryFunction {
			continue
		}
		target := kirDirectFunctionTarget(function, counts)
		if _, exists := builder.functionDecls[target]; exists {
			return fmt.Errorf("invalid KIR executable: duplicate direct function target %q", target)
		}
		builder.functionDecls[target] = function
	}
	rootStatements := builder.document.Statements
	if len(rootStatements) == 0 && builder.entryFunction != nil {
		rootStatements = builder.entryFunction.Body
	}
	instantiated := make(map[*KIRFunction]bool, len(builder.functionDecls))
	if err := builder.expandDirectFunctionCalls(rootStatements, nil, nil, instantiated); err != nil {
		return err
	}
	for _, function := range builder.document.Functions {
		if function == nil || function == builder.entryFunction || instantiated[function] {
			continue
		}
		target := kirDirectFunctionTarget(function, counts)
		instance, err := builder.newDirectFunctionInstance(target, function)
		if err != nil {
			return err
		}
		instantiated[function] = true
		active := map[*KIRFunction]*kirDirectFunction{function: instance}
		if err := builder.expandDirectFunctionCalls(function.Body, instance, active, instantiated); err != nil {
			return err
		}
	}
	return nil
}

func (builder *kirDirectBuilder) newDirectFunctionInstance(target string, function *KIRFunction) (*kirDirectFunction, error) {
	if len(builder.functions) >= maxKIRDirectNodes {
		return nil, fmt.Errorf("direct KIR ELF has too many static helper call paths")
	}
	instance := &kirDirectFunction{
		target:    target,
		function:  function,
		label:     builder.machine.newLabel(),
		bodyLabel: builder.machine.newLabel(),
		calls:     map[*KIRExpr]*kirDirectFunction{},
	}
	builder.functions = append(builder.functions, instance)
	if _, exists := builder.machine.functionLabels[target]; !exists {
		builder.machine.functionLabels[target] = instance.label
	}
	return instance, nil
}

func (builder *kirDirectBuilder) expandDirectFunctionCalls(statements []*KIRStmt, caller *kirDirectFunction, active map[*KIRFunction]*kirDirectFunction, instantiated map[*KIRFunction]bool) error {
	calls := builder.rootFunctionCall
	if caller != nil {
		calls = caller.calls
	}
	seen := make(map[*KIRExpr]bool)
	var visitExpression func(*KIRExpr) error
	var visitBlock func([]*KIRStmt) error
	visitExpression = func(expression *KIRExpr) error {
		if expression == nil || seen[expression] {
			return nil
		}
		seen[expression] = true
		for _, child := range []*KIRExpr{expression.Left, expression.Right, expression.Operand, expression.Base, expression.Receiver, expression.Callee} {
			if err := visitExpression(child); err != nil {
				return err
			}
		}
		for _, list := range [][]*KIRExpr{expression.Args, expression.Items, expression.MapKeys, expression.Values} {
			for _, child := range list {
				if err := visitExpression(child); err != nil {
					return err
				}
			}
		}
		prefix, target, ok := strings.Cut(expression.CallTarget, ":")
		if !ok || prefix != "function" {
			return nil
		}
		callee := builder.functionDecls[target]
		if callee == nil || callee.Name != expression.Name {
			return fmt.Errorf("invalid KIR executable: direct function call target %q is not lowered", expression.CallTarget)
		}
		if _, exists := builder.callSiteIDs[expression]; !exists {
			if len(builder.callSiteFrames) >= maxKIRDirectNodes {
				return fmt.Errorf("direct KIR ELF has too many static function callsites")
			}
			builder.callSiteIDs[expression] = uint32(len(builder.callSiteFrames) + 1)
			builder.callSiteFrames = append(builder.callSiteFrames, StackFrame{Function: callee.Name, Source: expression.Source, Line: expression.Line, Column: expression.Column})
		}
		if existing := active[callee]; existing != nil {
			calls[expression] = existing
			return nil
		}
		instance, err := builder.newDirectFunctionInstance(target, callee)
		if err != nil {
			return err
		}
		calls[expression] = instance
		instantiated[callee] = true
		nextActive := make(map[*KIRFunction]*kirDirectFunction, len(active)+1)
		for function, activeInstance := range active {
			nextActive[function] = activeInstance
		}
		nextActive[callee] = instance
		return builder.expandDirectFunctionCalls(callee.Body, instance, nextActive, instantiated)
	}
	visitBlock = func(block []*KIRStmt) error {
		for _, statement := range block {
			if statement == nil {
				continue
			}
			for _, expression := range []*KIRExpr{statement.Init, statement.Expr, statement.Target, statement.Value, statement.Cond, statement.Iter, statement.Return, statement.Scrutinee} {
				if err := visitExpression(expression); err != nil {
					return err
				}
			}
			for _, nested := range [][]*KIRStmt{statement.Then, statement.Else, statement.Body} {
				if err := visitBlock(nested); err != nil {
					return err
				}
			}
			for _, arm := range statement.Arms {
				if arm != nil {
					if err := visitBlock(arm.Body); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	return visitBlock(statements)
}

func (builder *kirDirectBuilder) allocateFunctionBindings() error {
	allocated := make(map[*KIRFunction]int, len(builder.functions))
	for index, function := range builder.functions {
		if index, exists := allocated[function.function]; exists {
			function.nextSlot = builder.functions[index].nextSlot
			continue
		}
		if len(function.function.Params) > 6 {
			return fmt.Errorf("direct KIR ELF function %q has too many parameters", function.function.Name)
		}
		nextSlot := int32(16 + len(function.function.Params)*8)
		for index, parameter := range function.function.Params {
			if parameter == nil || parameter.Binding == nil || !validKIRBinding(parameter.Binding) || parameter.Binding.Name != parameter.Name || parameter.Binding.Type != parameter.Type {
				return fmt.Errorf("invalid KIR executable: function %q parameter %d has invalid binding metadata", function.function.Name, index+1)
			}
			if _, exists := builder.bindingSlots[kirBindingIdentity(parameter.Binding)]; exists {
				return fmt.Errorf("invalid KIR executable: duplicate function parameter binding %q", parameter.Name)
			}
			builder.bindingSlots[kirBindingIdentity(parameter.Binding)] = machineSlot{offset: int32(16 + (index+1)*8)}
		}
		if err := builder.allocateBlockBindings(function.function.Body, &nextSlot); err != nil {
			return fmt.Errorf("function %q: %w", function.function.Name, err)
		}
		if nextSlot > maxKIRDirectFrameBytes-96 {
			return fmt.Errorf("direct KIR ELF function %q frame exceeds %d bytes", function.function.Name, maxKIRDirectFrameBytes)
		}
		function.nextSlot = nextSlot
		allocated[function.function] = index
	}
	return nil
}

func (builder *kirDirectBuilder) allocateBlockBindings(block []*KIRStmt, nextSlot *int32) error {
	reserveBinding := func(binding *KIRBinding, name string) error {
		if binding == nil || !validKIRBinding(binding) {
			return fmt.Errorf("invalid KIR executable: declaration %q has invalid binding metadata", name)
		}
		identity := kirBindingIdentity(binding)
		if _, exists := builder.bindingSlots[identity]; exists {
			return fmt.Errorf("invalid KIR executable: duplicate slot for binding %q", name)
		}
		if *nextSlot > maxKIRDirectFrameBytes-96-8 {
			return fmt.Errorf("direct KIR ELF function frame exceeds %d bytes", maxKIRDirectFrameBytes)
		}
		*nextSlot += 8
		builder.bindingSlots[identity] = machineSlot{offset: *nextSlot}
		return nil
	}
	var walk func([]*KIRStmt) error
	walk = func(statements []*KIRStmt) error {
		for _, statement := range statements {
			if statement == nil {
				return fmt.Errorf("invalid KIR executable: function contains a missing statement")
			}
			switch statement.Kind {
			case "let", "const":
				if err := reserveBinding(statement.Binding, statement.Name); err != nil {
					return err
				}
			case "if":
				if err := walk(statement.Then); err != nil {
					return err
				}
				if err := walk(statement.Else); err != nil {
					return err
				}
			case "while":
				if err := walk(statement.Body); err != nil {
					return err
				}
			case "for":
				if err := reserveBinding(statement.Binding, statement.Name); err != nil {
					return err
				}
				if *nextSlot > maxKIRDirectFrameBytes-96-16 {
					return fmt.Errorf("direct KIR ELF function frame exceeds %d bytes", maxKIRDirectFrameBytes)
				}
				*nextSlot += 8
				builder.forIterSlots[statement] = machineSlot{offset: *nextSlot}
				*nextSlot += 8
				builder.forIndexSlots[statement] = machineSlot{offset: *nextSlot}
				if err := walk(statement.Body); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(block)
}

func (builder *kirDirectBuilder) emitDirectFunctions() error {
	machine := builder.machine
	oldBufferOffset := machine.bufferOffset
	oldLoops := machine.loops
	for _, function := range builder.functions {
		if err := machine.bind(function.label); err != nil {
			return err
		}
		machine.loops = nil
		machine.bufferOffset = function.nextSlot + 1
		builder.currentFunction = function
		functionFrame := (int(machine.bufferOffset) + 63 + 15) &^ 15
		machine.code = append(machine.code, 0x55, 0x48, 0x89, 0xe5, 0x48, 0x81, 0xec)
		var frameBytes [4]byte
		binary.LittleEndian.PutUint32(frameBytes[:], uint32(functionFrame))
		machine.code = append(machine.code, frameBytes[:]...)
		machine.code = append(machine.code,
			0x49, 0x8b, 0x47, 0xc8, // mov rax, [r15-56]
			0x48, 0x89, 0x45, 0xf8, // mov [rbp-8], rax
			0x44, 0x89, 0x5d, 0xf0, // mov [rbp-16], r11d
			0x49, 0x89, 0x6f, 0xc8, // mov [r15-56], rbp
		)
		for index, parameter := range function.function.Params {
			slot := builder.bindingSlots[kirBindingIdentity(parameter.Binding)]
			if err := machine.emitStoreArg(index, slot); err != nil {
				return fmt.Errorf("function %q: %w", function.function.Name, err)
			}
		}
		if err := machine.bind(function.bodyLabel); err != nil {
			return err
		}
		if err := builder.emitBlock(function.function.Body, false); err != nil {
			return fmt.Errorf("function %q: %w", function.function.Name, err)
		}
		machine.emitMoveImmediate(0)
		builder.emitFunctionReturnEpilog()
	}
	builder.currentFunction = nil
	machine.bufferOffset = oldBufferOffset
	machine.loops = oldLoops
	return nil
}

func (builder *kirDirectBuilder) emitFunctionCall(expression *KIRExpr) error {
	if expression == nil || expression.Receiver != nil || expression.Callee != nil {
		return fmt.Errorf("direct KIR ELF does not lower methods or indirect calls")
	}
	prefix, target, ok := strings.Cut(expression.CallTarget, ":")
	var function *kirDirectFunction
	if ok && prefix == "function" {
		if builder.currentFunction != nil {
			function = builder.currentFunction.calls[expression]
		} else {
			function = builder.rootFunctionCall[expression]
		}
	}
	if !ok || prefix != "function" || function == nil || function.target != target || function.function.Name != expression.Name {
		return fmt.Errorf("invalid KIR executable: direct function call target %q is not lowered", expression.CallTarget)
	}
	if len(expression.Args) != len(function.function.Params) {
		return fmt.Errorf("direct KIR ELF function %q does not lower omitted default arguments", function.function.Name)
	}
	if err := builder.emitCallDepthGuard(expression); err != nil {
		return err
	}
	for _, argument := range expression.Args {
		if err := builder.emitExpr(argument); err != nil {
			return err
		}
		builder.machine.code = append(builder.machine.code, 0x50)
	}
	registerPops := [6][]byte{{0x5f}, {0x5e}, {0x5a}, {0x59}, {0x41, 0x58}, {0x41, 0x59}}
	for index := len(expression.Args) - 1; index >= 0; index-- {
		builder.machine.code = append(builder.machine.code, registerPops[index]...)
	}
	if err := builder.emitCallSiteID(expression); err != nil {
		return err
	}
	builder.machine.code = append(builder.machine.code, 0x49, 0xff, 0x47, 0xb8) // inc qword [r15-72]
	return builder.machine.emitLabelCall(function.label)
}

func validDirectKIRFunction(function *KIRFunction, document *KIRDocument) error {
	if function == nil || function.Worker || function.Unsafe || function.Trait != "" || function.Receiver != "" || len(function.TypeParams) != 0 || len(function.Captures) != 0 {
		return fmt.Errorf("%w: direct KIR ELF supports only non-generic, non-method functions without captures", errKIRSubsetUnsupported)
	}
	if !directKIRTypeSupported(function.Return, document) {
		return fmt.Errorf("%w: direct KIR ELF function %q returns unsupported type %q", errKIRSubsetUnsupported, function.Name, function.Return)
	}
	for _, parameter := range function.Params {
		if parameter == nil || parameter.Type == "Nil" || !directKIRTypeSupported(parameter.Type, document) {
			return fmt.Errorf("%w: direct KIR ELF function %q has an unsupported parameter", errKIRSubsetUnsupported, function.Name)
		}
	}
	return nil
}
