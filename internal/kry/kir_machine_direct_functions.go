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

func (builder *kirDirectBuilder) prepareFunctions() error {
	counts := make(map[string]int, len(builder.topFunctions))
	for _, function := range builder.topFunctions {
		if function != nil {
			counts[function.Name]++
		}
	}
	for _, function := range builder.topFunctions {
		if function == nil || function == builder.entryFunction {
			continue
		}
		target := kirDirectFunctionTarget(function, counts)
		if _, exists := builder.functionDecls[target]; exists {
			return fmt.Errorf("invalid KIR executable: duplicate direct function target %q", target)
		}
		builder.functionDecls[target] = function
	}
	rootStatements := builder.topStatements
	if len(rootStatements) == 0 && builder.entryFunction != nil {
		rootStatements = builder.functionBody(builder.entryFunction)
	}
	instantiated := make(map[*KIRFunction]bool, len(builder.functionDecls))
	if err := builder.expandDirectFunctionCalls(rootStatements, nil, nil, instantiated); err != nil {
		return err
	}
	for _, function := range builder.topFunctions {
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
		if err := builder.expandDirectFunctionCalls(builder.functionBody(function), instance, active, instantiated); err != nil {
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
		for _, child := range []*KIRExpr{builder.exprLeft(expression), builder.exprRight(expression), builder.exprOperand(expression), builder.exprBase(expression), builder.exprReceiver(expression), builder.exprCallee(expression)} {
			if err := visitExpression(child); err != nil {
				return err
			}
		}
		for _, list := range [][]*KIRExpr{builder.exprArgs(expression), builder.exprItems(expression), builder.exprMapKeys(expression), builder.exprValues(expression)} {
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
		return builder.expandDirectFunctionCalls(builder.functionBody(callee), instance, nextActive, instantiated)
	}
	visitBlock = func(block []*KIRStmt) error {
		for _, statement := range block {
			if statement == nil {
				continue
			}
			for _, expression := range []*KIRExpr{builder.stmtInit(statement), builder.stmtExpr(statement), builder.stmtTarget(statement), builder.stmtValue(statement), builder.stmtCond(statement), builder.stmtIter(statement), builder.stmtReturn(statement), builder.stmtScrutinee(statement)} {
				if err := visitExpression(expression); err != nil {
					return err
				}
			}
			for _, nested := range [][]*KIRStmt{builder.stmtThen(statement), builder.stmtElse(statement), builder.stmtBody(statement)} {
				if err := visitBlock(nested); err != nil {
					return err
				}
			}
			for _, arm := range builder.stmtArms(statement) {
				if arm != nil {
					if err := visitBlock(builder.armBody(arm)); err != nil {
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
		if err := builder.allocateBlockBindings(builder.functionBody(function.function), &nextSlot); err != nil {
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
				if err := walk(builder.stmtThen(statement)); err != nil {
					return err
				}
				if err := walk(builder.stmtElse(statement)); err != nil {
					return err
				}
			case "while":
				if err := walk(builder.stmtBody(statement)); err != nil {
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
				if err := walk(builder.stmtBody(statement)); err != nil {
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
		if err := builder.emitBlock(builder.functionBody(function.function), false); err != nil {
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
	if expression == nil || builder.exprReceiver(expression) != nil || builder.exprCallee(expression) != nil {
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
	if len(builder.exprArgs(expression)) != len(function.function.Params) {
		return fmt.Errorf("direct KIR ELF function %q does not lower omitted default arguments", function.function.Name)
	}
	if err := builder.emitCallDepthGuard(expression); err != nil {
		return err
	}
	for _, argument := range builder.exprArgs(expression) {
		if err := builder.emitExpr(argument); err != nil {
			return err
		}
		builder.machine.code = append(builder.machine.code, 0x50)
	}
	registerPops := [6][]byte{{0x5f}, {0x5e}, {0x5a}, {0x59}, {0x41, 0x58}, {0x41, 0x59}}
	for index := len(builder.exprArgs(expression)) - 1; index >= 0; index-- {
		builder.machine.code = append(builder.machine.code, registerPops[index]...)
	}
	if err := builder.emitCallSiteID(expression); err != nil {
		return err
	}
	builder.machine.code = append(builder.machine.code, 0x49, 0xff, 0x47, 0xb8) // inc qword [r15-72]
	return builder.machine.emitLabelCall(function.label)
}

func validDirectKIRFunction(function *KIRFunction, document kirExecMetadataProvider) error {
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
