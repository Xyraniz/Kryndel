package kry

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

const maxKIRDirectFrameBytes = 1 << 20
const maxKIRDirectNodes = 20_000

type kirDirectDiagnosticSite struct {
	label      int
	diagnostic *Diagnostic
}

type kirDirectBuilder struct {
	machine          *directMachine
	document         *KIRDocument
	limits           Limits
	sources          map[string]*Source
	bindingSlots     map[string]machineSlot
	failureSites     map[string]*kirDirectDiagnosticSite
	failureOrder     []*kirDirectDiagnosticSite
	instructionSlot  int32
	deadlineSecSlot  int32
	deadlineNsecSlot int32
	currentSecSlot   int32
	currentNsecSlot  int32
}

// validateKIRDirectELFSubset proves the accepted native slice using decoded
// KIR only. It first applies the executor's semantic checks, then narrows
// execution to operations for which the direct machine emits runtime code.
func validateKIRDirectELFSubset(document *KIRDocument) error {
	if err := validateKIRExecSubset(document); err != nil {
		return err
	}
	if len(document.Functions) != 0 {
		if len(document.Statements) != 0 {
			return fmt.Errorf("%w: direct KIR ELF does not execute top-level function declarations", errKIRSubsetUnsupported)
		}
		if len(document.Functions) != 1 || document.Functions[0] == nil || document.Functions[0].Name != "main" {
			return fmt.Errorf("%w: direct KIR ELF supports only a single main() function; helper functions are not lowered", errKIRSubsetUnsupported)
		}
	}
	var validateExpr func(*KIRExpr) error
	var validateBlock func([]*KIRStmt) error
	nodeCount := 0
	countNode := func() error {
		nodeCount++
		if nodeCount > maxKIRDirectNodes {
			return fmt.Errorf("%w: direct ELF lowering is limited to %d KIR nodes", errKIRSubsetUnsupported, maxKIRDirectNodes)
		}
		return nil
	}
	validateExpr = func(expression *KIRExpr) error {
		if expression == nil {
			return nil
		}
		if err := countNode(); err != nil {
			return err
		}
		if expression.Kind == "lambda" || expression.Lambda != nil {
			return fmt.Errorf("%w: direct KIR ELF does not lower lambdas or captured bindings", errKIRSubsetUnsupported)
		}
		if strings.HasPrefix(expression.Type, "fn(") {
			return fmt.Errorf("%w: direct KIR ELF does not lower function values or closures", errKIRSubsetUnsupported)
		}
		if expression.Callee != nil || strings.HasPrefix(expression.CallTarget, "function:") {
			return fmt.Errorf("%w: direct KIR ELF does not lower direct or indirect function calls", errKIRSubsetUnsupported)
		}
		if expression.Type == "Float" {
			return fmt.Errorf("%w: Float values are not yet lowered by the direct KIR ELF backend", errKIRSubsetUnsupported)
		}
		if expression.Kind == "binary" && expression.Operator == "+" && expression.Left != nil && expression.Left.Type == "String" {
			return fmt.Errorf("%w: dynamic String concatenation is not yet lowered by the direct KIR ELF backend", errKIRSubsetUnsupported)
		}
		if expression.Kind == "call" && expression.Name == "str" {
			return fmt.Errorf("%w: str conversion is not yet lowered by the direct KIR ELF backend", errKIRSubsetUnsupported)
		}
		for _, child := range []*KIRExpr{expression.Left, expression.Right, expression.Operand, expression.Base, expression.Receiver, expression.Callee} {
			if err := validateExpr(child); err != nil {
				return err
			}
		}
		for _, list := range [][]*KIRExpr{expression.Args, expression.Items, expression.MapKeys, expression.Values} {
			for _, child := range list {
				if err := validateExpr(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	validateBlock = func(statements []*KIRStmt) error {
		for _, statement := range statements {
			if err := countNode(); err != nil {
				return err
			}
			switch statement.Kind {
			case "let", "const":
				if err := validateExpr(statement.Init); err != nil {
					return err
				}
			case "assign":
				if err := validateExpr(statement.Value); err != nil {
					return err
				}
			case "expr":
				if statement.Expr.Kind == "call" && (statement.Expr.Name == "print" || statement.Expr.Name == "println") {
					argument := statement.Expr.Args[0]
					if argument.Type != "Int" && argument.Type != "Bool" && argument.Type != "String" {
						return fmt.Errorf("%w: native print does not support %s values", errKIRSubsetUnsupported, argument.Type)
					}
				}
				if err := validateExpr(statement.Expr); err != nil {
					return err
				}
			case "if":
				if err := validateExpr(statement.Cond); err != nil {
					return err
				}
				if err := validateBlock(statement.Then); err != nil {
					return err
				}
				if err := validateBlock(statement.Else); err != nil {
					return err
				}
			case "while":
				if err := validateExpr(statement.Cond); err != nil {
					return err
				}
				if err := validateBlock(statement.Body); err != nil {
					return err
				}
			case "return":
				if err := validateExpr(statement.Return); err != nil {
					return err
				}
			default:
				return fmt.Errorf("%w: statement %q", errKIRSubsetUnsupported, statement.Kind)
			}
		}
		return nil
	}
	statements := document.Statements
	if len(statements) == 0 && len(document.Functions) == 1 {
		statements = document.Functions[0].Body
	}
	return validateBlock(statements)
}

func buildDirectKIRELF(document *KIRDocument, limits Limits, sources map[string]*Source) ([]byte, error) {
	builder := &kirDirectBuilder{
		machine:      newDirectMachine(),
		document:     document,
		limits:       limits,
		sources:      sources,
		bindingSlots: map[string]machineSlot{},
		failureSites: map[string]*kirDirectDiagnosticSite{},
	}
	if err := builder.allocateBindings(); err != nil {
		return nil, err
	}
	return builder.build()
}

func (builder *kirDirectBuilder) allocateBindings() error {
	statements := builder.document.Statements
	if len(statements) == 0 && len(builder.document.Functions) == 1 {
		statements = builder.document.Functions[0].Body
	}
	var walk func([]*KIRStmt) error
	walk = func(block []*KIRStmt) error {
		for _, statement := range block {
			if statement.Kind == "let" || statement.Kind == "const" {
				identity := kirBindingIdentity(statement.Binding)
				if _, exists := builder.bindingSlots[identity]; exists {
					return fmt.Errorf("invalid KIR executable: duplicate slot for binding %q", statement.Name)
				}
				if builder.machine.nextSlot > maxKIRDirectFrameBytes-96-8 {
					return fmt.Errorf("direct KIR ELF backend local frame exceeds %d bytes", maxKIRDirectFrameBytes)
				}
				builder.machine.nextSlot += 8
				builder.bindingSlots[identity] = machineSlot{offset: builder.machine.nextSlot}
			}
			switch statement.Kind {
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
			}
		}
		return nil
	}
	return walk(statements)
}

func (builder *kirDirectBuilder) build() ([]byte, error) {
	machine := builder.machine
	if machine.nextSlot > math.MaxInt32-96 {
		return nil, fmt.Errorf("direct KIR ELF backend local frame exceeds x86-64 limits")
	}
	// Reserve language locals followed by the integer print buffer, output and
	// instruction counters, and two monotonic-clock timespecs.
	builder.instructionSlot = machine.nextSlot + 40
	// clock_gettime lays out tv_sec then tv_nsec at increasing addresses.
	// Since stack slots are expressed as positive rbp-relative distances, that
	// means the nanosecond offset is eight bytes smaller than the seconds offset.
	builder.deadlineSecSlot = machine.nextSlot + 64
	builder.deadlineNsecSlot = machine.nextSlot + 56
	builder.currentSecSlot = machine.nextSlot + 80
	builder.currentNsecSlot = machine.nextSlot + 72
	machine.bufferOffset = machine.nextSlot + 1
	machine.outputLimit = builder.limits.MaxOutputBytes
	machine.outputLimitSet = true
	frame := (int(machine.nextSlot) + 96 + 15) &^ 15

	// _start enters a private frame: no AST/checker state participates in
	// variable resolution or machine-code lowering from this point onward.
	machine.code = append(machine.code, 0x55, 0x48, 0x89, 0xe5)
	machine.code = append(machine.code, 0x48, 0x81, 0xec)
	var frameBytes [4]byte
	binary.LittleEndian.PutUint32(frameBytes[:], uint32(frame))
	machine.code = append(machine.code, frameBytes[:]...)
	machine.emitOutputCounterInit(machine.nextSlot + 32)
	builder.emitZeroSlot(builder.instructionSlot)
	if builder.limits.MaxWallTimeMS > 0 {
		builder.emitClockRead(builder.deadlineSecSlot)
		builder.emitDeadlineAdd(builder.limits.MaxWallTimeMS)
	}

	statements := builder.document.Statements
	if len(statements) == 0 && len(builder.document.Functions) == 1 {
		statements = builder.document.Functions[0].Body
	}
	if err := builder.emitBlock(statements, true); err != nil {
		return nil, err
	}
	if err := machine.emitJump(machine.endLabel); err != nil {
		return nil, err
	}
	if machine.stringEqualUsed {
		if err := machine.emitStringEqualRuntime(); err != nil {
			return nil, err
		}
	}
	if err := machine.bind(machine.endLabel); err != nil {
		return nil, err
	}
	if err := machine.emitExit(0); err != nil {
		return nil, err
	}
	for _, site := range builder.failureOrder {
		if err := machine.bind(site.label); err != nil {
			return nil, err
		}
		if err := machine.emitELFWriteRawTo(site.diagnostic.Format(false), 2); err != nil {
			return nil, fmt.Errorf("cannot emit direct KIR diagnostic: %w", err)
		}
		if err := machine.emitExit(1); err != nil {
			return nil, err
		}
	}
	if err := machine.bind(machine.trapLabel); err != nil {
		return nil, err
	}
	if err := machine.emitExit(1); err != nil {
		return nil, err
	}
	for _, ref := range machine.dataRefs {
		dataAddress := elfCodeOffset + len(machine.code) + ref.dataOffset
		nextInstruction := elfCodeOffset + ref.instructionEnd
		delta := dataAddress - nextInstruction
		if delta < -1<<31 || delta > 1<<31-1 {
			return nil, fmt.Errorf("direct KIR ELF data exceeds RIP-relative range")
		}
		binary.LittleEndian.PutUint32(machine.code[ref.displacement:ref.displacement+4], uint32(int32(delta)))
	}
	image := emitELF64CodeData(machine.code, machine.data)
	if builder.limits.MaxArtifactBytes > 0 && len(image) > builder.limits.MaxArtifactBytes {
		return nil, fmt.Errorf("direct KIR ELF exceeds configured artifact limit")
	}
	return image, nil
}

func (builder *kirDirectBuilder) emitZeroSlot(offset int32) {
	builder.machine.code = append(builder.machine.code, 0x48, 0xc7, 0x85)
	var displacement [4]byte
	binary.LittleEndian.PutUint32(displacement[:], uint32(-offset))
	builder.machine.code = append(builder.machine.code, displacement[:]...)
	builder.machine.code = append(builder.machine.code, 0, 0, 0, 0)
}

func (builder *kirDirectBuilder) emitClockRead(offset int32) {
	machine := builder.machine
	machine.code = append(machine.code, 0xb8, 0xe4, 0, 0, 0, 0xbf, 1, 0, 0, 0) // clock_gettime(CLOCK_MONOTONIC)
	machine.code = append(machine.code, 0x48, 0x8d, 0xb5)
	var displacement [4]byte
	binary.LittleEndian.PutUint32(displacement[:], uint32(-offset))
	machine.code = append(machine.code, displacement[:]...)
	machine.code = append(machine.code, 0x0f, 0x05)
}

func (builder *kirDirectBuilder) emitDeadlineAdd(milliseconds int64) {
	machine := builder.machine
	seconds := uint64(milliseconds / 1000)
	nanoseconds := uint32(milliseconds%1000) * 1_000_000
	if seconds != 0 {
		machine.code = append(machine.code, 0x48, 0x8b, 0x85)
		var displacement [4]byte
		binary.LittleEndian.PutUint32(displacement[:], uint32(-builder.deadlineSecSlot))
		machine.code = append(machine.code, displacement[:]...)
		machine.code = append(machine.code, 0x48, 0xb9)
		var delta [8]byte
		binary.LittleEndian.PutUint64(delta[:], seconds)
		machine.code = append(machine.code, delta[:]...)
		machine.code = append(machine.code, 0x48, 0x01, 0xc8, 0x48, 0x89, 0x85)
		machine.code = append(machine.code, displacement[:]...)
	}
	if nanoseconds == 0 {
		return
	}
	machine.code = append(machine.code, 0x48, 0x81, 0x85)
	var nsecDisplacement [4]byte
	binary.LittleEndian.PutUint32(nsecDisplacement[:], uint32(-builder.deadlineNsecSlot))
	machine.code = append(machine.code, nsecDisplacement[:]...)
	var nsecImmediate [4]byte
	binary.LittleEndian.PutUint32(nsecImmediate[:], nanoseconds)
	machine.code = append(machine.code, nsecImmediate[:]...)
	noCarry := machine.newLabel()
	machine.code = append(machine.code, 0x48, 0x81, 0xbd)
	machine.code = append(machine.code, nsecDisplacement[:]...)
	binary.LittleEndian.PutUint32(nsecImmediate[:], 1_000_000_000)
	machine.code = append(machine.code, nsecImmediate[:]...)
	_ = machine.emitConditionalJump(0x82, noCarry)
	machine.code = append(machine.code, 0x48, 0x81, 0xad)
	machine.code = append(machine.code, nsecDisplacement[:]...)
	binary.LittleEndian.PutUint32(nsecImmediate[:], 1_000_000_000)
	machine.code = append(machine.code, nsecImmediate[:]...)
	machine.code = append(machine.code, 0x48, 0xff, 0x85)
	var secDisplacement [4]byte
	binary.LittleEndian.PutUint32(secDisplacement[:], uint32(-builder.deadlineSecSlot))
	machine.code = append(machine.code, secDisplacement[:]...)
	_ = machine.bind(noCarry)
}

func (builder *kirDirectBuilder) diag(node *KIRExpr, category Category, message string) int {
	return builder.failure(node.Source, node.Line, node.Column, category, message)
}

func (builder *kirDirectBuilder) failure(source string, line, column int, category Category, message string) int {
	key := fmt.Sprintf("%s\x00%d\x00%d\x00%s\x00%s", source, line, column, category, message)
	if site := builder.failureSites[key]; site != nil {
		return site.label
	}
	name := source
	if mapped := builder.sources[source]; mapped != nil {
		name = mapped.Name
	}
	src := builder.sources[source]
	if src == nil {
		src = &Source{Name: name}
	}
	site := &kirDirectDiagnosticSite{
		label:      builder.machine.newLabel(),
		diagnostic: Diag(category, src, line, column, "%s", message),
	}
	builder.failureSites[key] = site
	builder.failureOrder = append(builder.failureOrder, site)
	return site.label
}

func (builder *kirDirectBuilder) emitStep(source string, line, column int) error {
	machine := builder.machine
	instructionFailure := builder.failure(source, line, column, CatResource, "instruction limit exceeded")
	machine.emitLoadSlot(machineSlot{offset: builder.instructionSlot})
	machine.code = append(machine.code, 0x48, 0xb9)
	var limit [8]byte
	binary.LittleEndian.PutUint64(limit[:], builder.limits.MaxInstructions)
	machine.code = append(machine.code, limit[:]...)
	machine.code = append(machine.code, 0x48, 0x39, 0xc8)                         // cmp rax, rcx
	if err := machine.emitConditionalJump(0x83, instructionFailure); err != nil { // jae
		return err
	}
	machine.code = append(machine.code, 0x48, 0xff, 0x85)
	var instructionDisplacement [4]byte
	binary.LittleEndian.PutUint32(instructionDisplacement[:], uint32(-builder.instructionSlot))
	machine.code = append(machine.code, instructionDisplacement[:]...)
	if builder.limits.MaxWallTimeMS != 0 {
		return builder.emitTimeCheck(source, line, column)
	}
	return nil
}

func (builder *kirDirectBuilder) emitTimeCheck(source string, line, column int) error {
	machine := builder.machine
	failure := builder.failure(source, line, column, CatResource, "wall-clock execution limit exceeded")
	if builder.limits.MaxWallTimeMS < 0 {
		return machine.emitJump(failure)
	}
	machineLabel := machine.newLabel()
	builder.emitClockRead(builder.currentSecSlot)
	var currentSec [4]byte
	binary.LittleEndian.PutUint32(currentSec[:], uint32(-builder.currentSecSlot))
	var deadlineSec [4]byte
	binary.LittleEndian.PutUint32(deadlineSec[:], uint32(-builder.deadlineSecSlot))
	machine.code = append(machine.code, 0x48, 0x8b, 0x85)
	machine.code = append(machine.code, currentSec[:]...)
	machine.code = append(machine.code, 0x48, 0x3b, 0x85)
	machine.code = append(machine.code, deadlineSec[:]...)
	if err := machine.emitConditionalJump(0x87, failure); err != nil { // current seconds > deadline
		return err
	}
	if err := machine.emitConditionalJump(0x82, machineLabel); err != nil { // current seconds < deadline
		return err
	}
	var currentNsec [4]byte
	binary.LittleEndian.PutUint32(currentNsec[:], uint32(-builder.currentNsecSlot))
	var deadlineNsec [4]byte
	binary.LittleEndian.PutUint32(deadlineNsec[:], uint32(-builder.deadlineNsecSlot))
	machine.code = append(machine.code, 0x48, 0x8b, 0x85)
	machine.code = append(machine.code, currentNsec[:]...)
	machine.code = append(machine.code, 0x48, 0x3b, 0x85)
	machine.code = append(machine.code, deadlineNsec[:]...)
	if err := machine.emitConditionalJump(0x83, failure); err != nil { // current nanoseconds >= deadline
		return err
	}
	return machine.bind(machineLabel)
}

func (builder *kirDirectBuilder) emitBlock(statements []*KIRStmt, topLevel bool) error {
	for _, statement := range statements {
		if err := builder.emitStep(statement.Source, statement.Line, statement.Column); err != nil {
			return err
		}
		if err := builder.emitStmt(statement); err != nil {
			return err
		}
		if topLevel && builder.limits.MaxWallTimeMS > 0 {
			if err := builder.emitTimeCheck(statement.Source, statement.Line, statement.Column); err != nil {
				return err
			}
		}
		if statement.Kind == "return" {
			if err := builder.machine.emitJump(builder.machine.endLabel); err != nil {
				return err
			}
			return nil
		}
	}
	return nil
}

func (builder *kirDirectBuilder) emitStmt(statement *KIRStmt) error {
	machine := builder.machine
	switch statement.Kind {
	case "let", "const":
		if err := builder.emitExpr(statement.Init); err != nil {
			return err
		}
		slot, ok := builder.bindingSlots[kirBindingIdentity(statement.Binding)]
		if !ok {
			return fmt.Errorf("direct KIR ELF backend has no slot for binding %q", statement.Name)
		}
		machine.emitStoreSlot(slot)
	case "assign":
		if err := builder.emitExpr(statement.Value); err != nil {
			return err
		}
		slot, ok := builder.bindingSlots[kirBindingIdentity(statement.Target.Binding)]
		if !ok {
			return fmt.Errorf("direct KIR ELF backend has no slot for binding %q", statement.Target.Name)
		}
		machine.emitStoreSlot(slot)
	case "expr":
		if statement.Expr.Kind != "call" || (statement.Expr.Name != "print" && statement.Expr.Name != "println") {
			return fmt.Errorf("direct KIR ELF backend encountered an unsupported expression statement")
		}
		if err := builder.emitPrint(statement.Expr); err != nil {
			return err
		}
	case "if":
		if err := builder.emitExpr(statement.Cond); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x85, 0xc0)
		elseLabel := machine.newLabel()
		joinLabel := machine.newLabel()
		if err := machine.emitConditionalJump(0x84, elseLabel); err != nil {
			return err
		}
		if err := builder.emitBlock(statement.Then, false); err != nil {
			return err
		}
		if len(statement.Else) > 0 {
			if err := machine.emitJump(joinLabel); err != nil {
				return err
			}
			if err := machine.bind(elseLabel); err != nil {
				return err
			}
			if err := builder.emitBlock(statement.Else, false); err != nil {
				return err
			}
			return machine.bind(joinLabel)
		}
		return machine.bind(elseLabel)
	case "while":
		conditionLabel := machine.newLabel()
		endLabel := machine.newLabel()
		if err := machine.bind(conditionLabel); err != nil {
			return err
		}
		// Runtime accounts one additional while-statement step each iteration.
		if err := builder.emitStep(statement.Source, statement.Line, statement.Column); err != nil {
			return err
		}
		if err := builder.emitExpr(statement.Cond); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x85, 0xc0)
		if err := machine.emitConditionalJump(0x84, endLabel); err != nil {
			return err
		}
		if err := builder.emitBlock(statement.Body, false); err != nil {
			return err
		}
		if err := machine.emitJump(conditionLabel); err != nil {
			return err
		}
		return machine.bind(endLabel)
	case "return":
		if statement.Return != nil {
			if err := builder.emitExpr(statement.Return); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("direct KIR ELF backend does not support statement %q", statement.Kind)
	}
	return nil
}

func (builder *kirDirectBuilder) emitPrint(expression *KIRExpr) error {
	if len(expression.Args) != 1 {
		return fmt.Errorf("direct KIR ELF print expects one argument")
	}
	if err := builder.emitStep(expression.Source, expression.Line, expression.Column); err != nil {
		return err
	}
	if err := builder.emitExpr(expression.Args[0]); err != nil {
		return err
	}
	machine := builder.machine
	lineFeed := expression.Name == "println"
	limitLabel := builder.diag(expression, CatResource, "output limit exceeded")
	streamLabel := builder.diag(expression, CatIO, "stream failure")
	oldLimit, oldTrap := machine.outputLimitLabel, machine.trapLabel
	machine.outputLimitLabel, machine.trapLabel = limitLabel, streamLabel
	var err error
	switch expression.Args[0].Type {
	case "Int":
		err = machine.emitInteger(false, lineFeed)
	case "Bool":
		err = builder.emitBooleanOutput(lineFeed)
	case "String":
		err = machine.emitStringOutput(lineFeed)
	default:
		err = fmt.Errorf("direct KIR ELF cannot print %s", expression.Args[0].Type)
	}
	machine.outputLimitLabel, machine.trapLabel = oldLimit, oldTrap
	return err
}

func (builder *kirDirectBuilder) emitBooleanOutput(newline bool) error {
	machine := builder.machine
	machine.code = append(machine.code, 0x48, 0x85, 0xc0)
	falseLabel := machine.newLabel()
	joinLabel := machine.newLabel()
	if err := machine.emitConditionalJump(0x84, falseLabel); err != nil {
		return err
	}
	trueLength := uint32(4)
	if newline {
		trueLength++
	}
	machine.code = append(machine.code, 0x41, 0xb8)
	var length [4]byte
	binary.LittleEndian.PutUint32(length[:], trueLength)
	machine.code = append(machine.code, length[:]...)
	if err := machine.emitOutputBudgetAddR8(); err != nil {
		return err
	}
	if err := machine.emitWriteRaw("true"); err != nil {
		return err
	}
	if newline {
		if err := machine.emitWriteRaw("\n"); err != nil {
			return err
		}
	}
	if err := machine.emitJump(joinLabel); err != nil {
		return err
	}
	if err := machine.bind(falseLabel); err != nil {
		return err
	}
	falseLength := uint32(5)
	if newline {
		falseLength++
	}
	machine.code = append(machine.code, 0x41, 0xb8)
	binary.LittleEndian.PutUint32(length[:], falseLength)
	machine.code = append(machine.code, length[:]...)
	if err := machine.emitOutputBudgetAddR8(); err != nil {
		return err
	}
	if err := machine.emitWriteRaw("false"); err != nil {
		return err
	}
	if newline {
		if err := machine.emitWriteRaw("\n"); err != nil {
			return err
		}
	}
	return machine.bind(joinLabel)
}

func (builder *kirDirectBuilder) emitExpr(expression *KIRExpr) error {
	if err := builder.emitStep(expression.Source, expression.Line, expression.Column); err != nil {
		return err
	}
	machine := builder.machine
	switch expression.Kind {
	case "int":
		machine.emitMoveImmediate(uint64(expression.Int))
	case "bool":
		if expression.Bool {
			machine.emitMoveImmediate(1)
		} else {
			machine.emitMoveImmediate(0)
		}
	case "string":
		machine.emitStringAddress(expression.String)
	case "nil":
		machine.emitMoveImmediate(0)
	case "var":
		slot, ok := builder.bindingSlots[kirBindingIdentity(expression.Binding)]
		if !ok {
			return fmt.Errorf("invalid KIR executable: missing native slot for %q", expression.Name)
		}
		machine.emitLoadSlot(slot)
	case "unary":
		if err := builder.emitExpr(expression.Operand); err != nil {
			return err
		}
		switch expression.Operator {
		case "+":
		case "!":
			machine.code = append(machine.code, 0x48, 0x85, 0xc0, 0x0f, 0x94, 0xc0, 0x48, 0x0f, 0xb6, 0xc0)
		case "-":
			failure := builder.diag(expression, CatRuntime, "negation overflow")
			machine.code = append(machine.code, 0x48, 0xf7, 0xd8)
			if err := machine.emitConditionalJump(0x80, failure); err != nil { // jo
				return err
			}
		default:
			return fmt.Errorf("direct KIR ELF does not support unary operator %q", expression.Operator)
		}
	case "binary":
		return builder.emitBinary(expression)
	case "call":
		return fmt.Errorf("direct KIR ELF call %q is not executable in expression position", expression.Name)
	default:
		return fmt.Errorf("direct KIR ELF does not support expression %q", expression.Kind)
	}
	return nil
}

func (builder *kirDirectBuilder) emitBinary(expression *KIRExpr) error {
	machine := builder.machine
	operator := expression.Operator
	if operator == "&&" || operator == "||" {
		if err := builder.emitExpr(expression.Left); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x85, 0xc0)
		shortCircuit, done := machine.newLabel(), machine.newLabel()
		branch := byte(0x84)
		shortValue := uint64(0)
		if operator == "||" {
			branch, shortValue = 0x85, 1
		}
		if err := machine.emitConditionalJump(branch, shortCircuit); err != nil {
			return err
		}
		if err := builder.emitExpr(expression.Right); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x85, 0xc0, 0x0f, 0x95, 0xc0, 0x48, 0x0f, 0xb6, 0xc0)
		if err := machine.emitJump(done); err != nil {
			return err
		}
		if err := machine.bind(shortCircuit); err != nil {
			return err
		}
		machine.emitMoveImmediate(shortValue)
		return machine.bind(done)
	}
	if err := builder.emitExpr(expression.Left); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x50) // preserve the left operand
	if err := builder.emitExpr(expression.Right); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x48, 0x89, 0xc1, 0x58) // rcx=right, rax=left
	if expression.Left.Type == "String" {
		if operator != "==" && operator != "!=" {
			return fmt.Errorf("direct KIR ELF does not support String operator %q", operator)
		}
		machine.code = append(machine.code, 0x48, 0x89, 0xc7, 0x48, 0x89, 0xce) // rdi=left, rsi=right
		machine.stringEqualUsed = true
		if err := machine.emitLabelCall(machine.stringEqualLabel); err != nil {
			return err
		}
		if operator == "!=" {
			machine.code = append(machine.code, 0x48, 0x83, 0xf0, 0x01)
		}
		return nil
	}
	if operator == "==" || operator == "!=" || operator == "<" || operator == "<=" || operator == ">" || operator == ">=" {
		condition := map[string]byte{"==": 0x94, "!=": 0x95, "<": 0x9c, "<=": 0x9e, ">": 0x9f, ">=": 0x9d}[operator]
		machine.code = append(machine.code, 0x48, 0x39, 0xc8, 0x0f, condition, 0xc0, 0x48, 0x0f, 0xb6, 0xc0)
		return nil
	}
	switch operator {
	case "+":
		machine.code = append(machine.code, 0x48, 0x01, 0xc8)
	case "-":
		machine.code = append(machine.code, 0x48, 0x29, 0xc8)
	case "*":
		machine.code = append(machine.code, 0x48, 0x0f, 0xaf, 0xc1)
	case "/", "%":
		divisionByZeroMessage := "division by zero"
		if operator == "%" {
			divisionByZeroMessage = "remainder by zero"
		}
		zeroFailure := builder.diag(expression, CatRuntime, divisionByZeroMessage)
		machine.code = append(machine.code, 0x48, 0x85, 0xc9)
		if err := machine.emitConditionalJump(0x84, zeroFailure); err != nil {
			return err
		}
		overflowFailure := builder.diag(expression, CatRuntime, "checked integer arithmetic overflow")
		machine.code = append(machine.code, 0x48, 0xba)
		var minInt [8]byte
		binary.LittleEndian.PutUint64(minInt[:], uint64(1)<<63)
		machine.code = append(machine.code, minInt[:]...)
		machine.code = append(machine.code, 0x48, 0x39, 0xd0)
		safeDivide := machine.newLabel()
		if err := machine.emitConditionalJump(0x85, safeDivide); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x83, 0xf9, 0xff)
		if err := machine.emitConditionalJump(0x84, overflowFailure); err != nil {
			return err
		}
		if err := machine.bind(safeDivide); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x99, 0x48, 0xf7, 0xf9)
		if operator == "%" {
			machine.code = append(machine.code, 0x48, 0x89, 0xd0)
		}
		return nil
	default:
		return fmt.Errorf("direct KIR ELF does not support binary operator %q", operator)
	}
	overflowFailure := builder.diag(expression, CatRuntime, "checked integer arithmetic overflow")
	return machine.emitConditionalJump(0x80, overflowFailure)
}
