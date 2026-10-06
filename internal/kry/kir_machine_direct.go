package kry

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

const maxKIRDirectFrameBytes = 1 << 20
const maxKIRDirectNodes = 20_000

const directELFWallTimeCheckInterval = 64

type kirDirectDiagnosticSite struct {
	label      int
	diagnostic *Diagnostic
}

type kirDirectFunction struct {
	target    string
	function  *KIRFunction
	label     int
	bodyLabel int
	nextSlot  int32
	calls     map[*KIRExpr]*kirDirectFunction
}

type kirDirectBuilder struct {
	machine          *directMachine
	executor         *kirExecutor
	metadata         KIRMetadata
	topStatements    []*KIRStmt
	topFunctions     []*KIRFunction
	limits           Limits
	sources          map[string]*Source
	bindingSlots     map[string]machineSlot
	forIterSlots     map[*KIRStmt]machineSlot
	forIndexSlots    map[*KIRStmt]machineSlot
	types            map[string]*Type
	structs          map[string]*KIRStruct
	enums            map[string]*KIREnum
	functions        []*kirDirectFunction
	functionDecls    map[string]*KIRFunction
	rootFunctionCall map[*KIRExpr]*kirDirectFunction
	callSiteIDs      map[*KIRExpr]uint32
	callSiteFrames   []StackFrame
	callSiteTable    int
	callSiteTableSet bool
	processArgsUsed  bool
	entryFunction    *KIRFunction
	currentFunction  *kirDirectFunction
	loops            []machineLoop
	entryFrame       *StackFrame
	failureSites     map[string]*kirDirectDiagnosticSite
	failureOrder     []*kirDirectDiagnosticSite
	instructionSlot  int32
	deadlineSecSlot  int32
	deadlineNsecSlot int32
	currentSecSlot   int32
	currentNsecSlot  int32
}

// validateKIRDirectELFSubset retains the scalar-only capability check used by
// the direct backend's compatibility tests. It validates the same typed arena
// as the production value subset and never creates a recursive wire document.
func validateKIRDirectELFSubset(mir *ValidatedMIR) error {
	if mir == nil || mir.arena == nil {
		return fmt.Errorf("direct ELF backend requires validated MIR")
	}
	view, err := newKIRExecArenaView(mir.arena)
	if err != nil {
		return err
	}
	return validateDirectKIRValueArena(view, kirMetadataFromArena(mir.arena), false)
}

func validateKIRDirectELFValueSubset(mir *ValidatedMIR) error {
	if mir == nil || mir.arena == nil {
		return fmt.Errorf("direct ELF backend requires validated MIR")
	}
	view, err := newKIRExecArenaView(mir.arena)
	if err != nil {
		return err
	}
	return validateDirectKIRValueArena(view, kirMetadataFromArena(mir.arena), true)
}

func directKIRMapType(encoded string) ([]string, bool) {
	name, arguments, composite := parseKIRContainerType(encoded)
	return arguments, composite && name == "Map" && len(arguments) == 2
}

func buildDirectKIRELF(mir *ValidatedMIR, limits Limits, sources map[string]*Source) ([]byte, error) {
	if mir == nil || mir.arena == nil {
		return nil, fmt.Errorf("direct ELF backend requires validated MIR")
	}
	view, err := newKIRExecArenaView(mir.arena)
	if err != nil {
		return nil, err
	}
	if err := validateKIRDirectELFValueSubset(mir); err != nil {
		return nil, err
	}
	metadata := kirMetadataFromArena(mir.arena)
	functions := kirExecFunctionsFromArena(view)
	executor := &kirExecutor{metadata: metadata, arenaView: view, functions: functions}
	builder := &kirDirectBuilder{
		machine:          newDirectMachine(),
		executor:         executor,
		metadata:         metadata,
		topStatements:    view.statementList(mir.arena.TopStatements),
		topFunctions:     view.functionList(mir.arena.TopFunctions),
		limits:           limits,
		sources:          sources,
		bindingSlots:     map[string]machineSlot{},
		forIterSlots:     map[*KIRStmt]machineSlot{},
		forIndexSlots:    map[*KIRStmt]machineSlot{},
		types:            map[string]*Type{},
		structs:          map[string]*KIRStruct{},
		enums:            map[string]*KIREnum{},
		functionDecls:    map[string]*KIRFunction{},
		rootFunctionCall: map[*KIRExpr]*kirDirectFunction{},
		callSiteIDs:      map[*KIRExpr]uint32{},
		callSiteTable:    -1,
		failureSites:     map[string]*kirDirectDiagnosticSite{},
	}
	for _, declaration := range metadata.Structs {
		if declaration != nil {
			builder.structs[declaration.Name] = declaration
		}
	}
	for _, declaration := range metadata.Enums {
		if declaration != nil {
			builder.enums[declaration.Name] = declaration
		}
	}
	if main, _ := kirExecEntryFunctionFromArena(view, functions); main != nil {
		builder.entryFunction = main
		builder.entryFrame = &StackFrame{Function: main.Name, Source: main.Source, Line: main.Line, Column: main.Column}
	}
	if err := builder.prepareFunctions(); err != nil {
		return nil, err
	}
	if err := builder.allocateBindings(); err != nil {
		return nil, err
	}
	return builder.build()
}

func (builder *kirDirectBuilder) allocateBindings() error {
	statements := builder.topStatements
	if len(statements) == 0 && builder.entryFunction != nil {
		statements = builder.functionBody(builder.entryFunction)
	}
	reserve := func(name string) (machineSlot, error) {
		if builder.machine.nextSlot > maxKIRDirectFrameBytes-128-8 {
			return machineSlot{}, fmt.Errorf("direct KIR ELF backend local frame exceeds %d bytes", maxKIRDirectFrameBytes)
		}
		builder.machine.nextSlot += 8
		return machineSlot{offset: builder.machine.nextSlot}, nil
	}
	reserveBinding := func(binding *KIRBinding, name string) error {
		if binding == nil || !validKIRBinding(binding) {
			return fmt.Errorf("invalid KIR executable: declaration %q has invalid binding metadata", name)
		}
		identity := kirBindingIdentity(binding)
		if _, exists := builder.bindingSlots[identity]; exists {
			return fmt.Errorf("invalid KIR executable: duplicate slot for binding %q", name)
		}
		slot, err := reserve(name)
		if err != nil {
			return err
		}
		builder.bindingSlots[identity] = slot
		return nil
	}
	var walk func([]*KIRStmt) error
	walk = func(block []*KIRStmt) error {
		for _, statement := range block {
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
				iterator, err := reserve(statement.Name + " iterator")
				if err != nil {
					return err
				}
				index, err := reserve(statement.Name + " index")
				if err != nil {
					return err
				}
				builder.forIterSlots[statement] = iterator
				builder.forIndexSlots[statement] = index
				if err := walk(builder.stmtBody(statement)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(statements); err != nil {
		return err
	}
	return builder.allocateFunctionBindings()
}

func (builder *kirDirectBuilder) build() ([]byte, error) {
	machine := builder.machine
	if machine.nextSlot > math.MaxInt32-128 {
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
	if err := builder.prepareCallSiteTable(); err != nil {
		return nil, err
	}
	machine.bufferOffset = machine.nextSlot + 1
	machine.outputLimit = builder.limits.MaxOutputBytes
	machine.outputLimitSet = true
	frame := (int(machine.nextSlot) + 128 + 15) &^ 15

	// _start enters a private frame: no AST/checker state participates in
	// variable resolution or machine-code lowering from this point onward.
	machine.code = append(machine.code, 0x55, 0x48, 0x89, 0xe5)
	machine.code = append(machine.code, 0x48, 0x81, 0xec)
	var frameBytes [4]byte
	binary.LittleEndian.PutUint32(frameBytes[:], uint32(frame))
	machine.code = append(machine.code, frameBytes[:]...)
	machine.emitOutputCounterInit(machine.nextSlot + 32)
	builder.emitZeroDynamicCallState()
	builder.emitZeroSlot(builder.instructionSlot)
	if builder.limits.MaxWallTimeMS > 0 {
		builder.emitContextQword(0xf0, directELFWallTimeCheckInterval) // countdown at [r15-16]
		builder.emitClockRead(builder.deadlineSecSlot)
		builder.emitDeadlineAdd(builder.limits.MaxWallTimeMS)
	}
	if builder.entryFrame != nil {
		if builder.limits.MaxCallDepth <= 0 {
			failure := builder.failure(builder.entryFrame.Source, builder.entryFrame.Line, builder.entryFrame.Column, CatResource, "call depth limit exceeded")
			if err := machine.emitJump(failure); err != nil {
				return nil, err
			}
		} else {
			builder.emitContextQword(0xb8, 1) // [r15-72], active KIR call depth
			builder.emitContextQword(0xb0, 1) // [r15-80], active main() frame
		}
	}

	statements := builder.topStatements
	if len(statements) == 0 && builder.entryFunction != nil {
		statements = builder.functionBody(builder.entryFunction)
	}
	if err := builder.emitBlock(statements, true); err != nil {
		return nil, err
	}
	if err := machine.emitJump(machine.endLabel); err != nil {
		return nil, err
	}
	if err := builder.emitDirectFunctions(); err != nil {
		return nil, err
	}
	if machine.arrayRuntimeUsed {
		for _, emit := range []func() error{
			machine.emitArrayAllocRuntime,
			machine.emitArrayPushRuntime,
			machine.emitArrayConcatRuntime,
			machine.emitArraySetRuntime,
			machine.emitArraySliceRuntime,
			machine.emitArrayReverseRuntime,
		} {
			if err := emit(); err != nil {
				return nil, err
			}
		}
	}
	if machine.boxRuntimeUsed {
		if err := machine.emitBoxAllocRuntime(); err != nil {
			return nil, err
		}
	}
	if machine.stringConcatUsed {
		if err := machine.emitStringConcatRuntime(); err != nil {
			return nil, err
		}
	}
	if machine.hostRuntimeUsed {
		// Keep the direct KIR host runtime set in parity with the AST backend.
		// Several helpers have transitive dependencies (for example,
		// fs_read_text allocates its returned String), so emitting only the
		// apparent call target can leave valid calls pointing at unbound labels.
		if err := machine.emitStringAllocRuntime(); err != nil {
			return nil, err
		}
		if machine.stringCharsUsed {
			if err := machine.emitStringCharsRuntime(); err != nil {
				return nil, err
			}
		}
		if machine.substringUsed {
			if err := machine.emitSubstringRuntime(); err != nil {
				return nil, err
			}
		}
		if machine.intToStringUsed {
			if err := machine.emitIntToStringRuntime(); err != nil {
				return nil, err
			}
		}
		if machine.intFromStringUsed {
			if err := machine.emitIntFromStringRuntime(); err != nil {
				return nil, err
			}
		}
		if machine.bytesFromArrayUsed {
			if err := machine.emitBytesFromArrayRuntime(); err != nil {
				return nil, err
			}
		}
		if machine.u8ArrayUsed {
			if err := machine.emitU8ArrayRuntime(); err != nil {
				return nil, err
			}
		}
		if builder.processArgsUsed {
			if err := machine.emitProcessArgsRuntime(); err != nil {
				return nil, err
			}
		}
		if machine.fsReadTextUsed {
			if err := machine.emitFSReadTextRuntime(); err != nil {
				return nil, err
			}
		}
		if machine.fsWriteBytesUsed {
			if err := machine.emitFSWriteBytesRuntime(); err != nil {
				return nil, err
			}
		}
	}
	if machine.mapRuntimeUsed {
		if err := machine.emitStringEqualRuntime(); err != nil {
			return nil, err
		}
		if err := machine.emitMapFindRuntime(); err != nil {
			return nil, err
		}
		if err := machine.emitMapInsertRuntime(); err != nil {
			return nil, err
		}
		if err := machine.emitMapRemoveRuntime(); err != nil {
			return nil, err
		}
	} else if machine.stringEqualUsed {
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
		if err := builder.emitDynamicDiagnosticStack(); err != nil {
			return nil, err
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
	if err := machine.validateLabelReferences(); err != nil {
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

func (builder *kirDirectBuilder) emitContextQword(displacement byte, value uint64) {
	builder.machine.emitMoveImmediate(value)
	builder.machine.code = append(builder.machine.code, 0x49, 0x89, 0x47, displacement)
}

func (builder *kirDirectBuilder) emitZeroDynamicCallState() {
	code := &builder.machine.code
	*code = append(*code, 0x31, 0xc0)                             // xor eax, eax
	for _, displacement := range []byte{0xc8, 0xc0, 0xb8, 0xb0} { // [r15-56], -64, -72, -80
		*code = append(*code, 0x49, 0x89, 0x47, displacement)
	}
}

func (builder *kirDirectBuilder) prepareCallSiteTable() error {
	if builder.callSiteTableSet || len(builder.callSiteFrames) == 0 {
		builder.callSiteTableSet = true
		return nil
	}
	machine := builder.machine
	frameOffsets := make([]int, len(builder.callSiteFrames))
	frameLengths := make([]uint32, len(builder.callSiteFrames))
	for index, frame := range builder.callSiteFrames {
		text := fmt.Sprintf("  at %s (%s:%d:%d)\n", frame.Function, frame.Source, frame.Line, frame.Column)
		if uint64(len(text)) > uint64(^uint32(0)) {
			return fmt.Errorf("direct KIR ELF call frame exceeds the diagnostic write limit")
		}
		frameOffsets[index] = machine.addData(text)
		frameLengths[index] = uint32(len(text))
	}
	builder.callSiteTable = len(machine.data)
	for index, offset := range frameOffsets {
		entryOffset := len(machine.data)
		delta := int64(offset) - int64(entryOffset)
		if delta < -1<<31 || delta > 1<<31-1 {
			return fmt.Errorf("direct KIR ELF call frame table exceeds rel32 range")
		}
		var entry [8]byte
		binary.LittleEndian.PutUint32(entry[:4], uint32(int32(delta)))
		binary.LittleEndian.PutUint32(entry[4:], frameLengths[index])
		machine.data = append(machine.data, entry[:]...)
	}
	builder.callSiteTableSet = true
	return nil
}

func (builder *kirDirectBuilder) emitDynamicDiagnosticStack() error {
	machine := builder.machine
	if len(builder.callSiteFrames) != 0 {
		loop, done := machine.newLabel(), machine.newLabel()
		machine.code = append(machine.code,
			0x49, 0x8b, 0x47, 0xc8, // mov rax, [r15-56]
			0x49, 0x89, 0x47, 0xc0, // mov [r15-64], rax
		)
		if err := machine.bind(loop); err != nil {
			return err
		}
		machine.code = append(machine.code,
			0x49, 0x8b, 0x47, 0xc0, // mov rax, [r15-64]
			0x48, 0x85, 0xc0, // test rax, rax
		)
		if err := machine.emitConditionalJump(0x84, done); err != nil {
			return err
		}
		machine.code = append(machine.code,
			0x8b, 0x48, 0xf0, // mov ecx, [rax-16] (callsite id)
			0xff, 0xc9, // dec ecx (zero-based table index)
			0x48, 0xc1, 0xe1, 0x03, // shl rcx, 3
		)
		start := len(machine.code)
		machine.code = append(machine.code, 0x48, 0x8d, 0x15, 0, 0, 0, 0) // lea rdx, [rip+table]
		machine.dataRefs = append(machine.dataRefs, machineDataRef{displacement: start + 3, instructionEnd: start + 7, dataOffset: builder.callSiteTable})
		machine.code = append(machine.code,
			0x48, 0x01, 0xca, // add rdx, rcx
			0x48, 0x63, 0x02, // movsxd rax, dword [rdx]
			0x48, 0x8d, 0x34, 0x02, // lea rsi, [rdx+rax]
			0x8b, 0x52, 0x04, // mov edx, [rdx+4]
		)
		if err := machine.emitELFWriteRSIRDXFD(2); err != nil {
			return err
		}
		machine.code = append(machine.code,
			0x49, 0x8b, 0x47, 0xc0, // mov rax, [r15-64]
			0x48, 0x8b, 0x40, 0xf8, // mov rax, [rax-8] (previous frame)
			0x49, 0x89, 0x47, 0xc0, // mov [r15-64], rax
		)
		if err := machine.emitJump(loop); err != nil {
			return err
		}
		if err := machine.bind(done); err != nil {
			return err
		}
	}
	if builder.entryFrame == nil {
		return nil
	}
	skipEntry := machine.newLabel()
	machine.code = append(machine.code, 0x49, 0x83, 0x7f, 0xb0, 0) // cmp qword [r15-80], 0
	if err := machine.emitConditionalJump(0x84, skipEntry); err != nil {
		return err
	}
	frame := *builder.entryFrame
	text := fmt.Sprintf("  at %s (%s:%d:%d)\n", frame.Function, frame.Source, frame.Line, frame.Column)
	if err := machine.emitELFWriteRawTo(text, 2); err != nil {
		return err
	}
	return machine.bind(skipEntry)
}

func (builder *kirDirectBuilder) emitStep(source string, line, column int) error {
	machine := builder.machine
	instructionFailure := builder.failure(source, line, column, CatResource, "instruction limit exceeded")
	// R15 points at the entry frame's shared execution context. Helpers keep
	// using the same instruction counter even though their RBP is a new frame.
	machine.code = append(machine.code, 0x49, 0x8b, 0x47, 0xf8) // rax = [r15-8]
	machine.code = append(machine.code, 0x48, 0xb9)
	var limit [8]byte
	binary.LittleEndian.PutUint64(limit[:], builder.limits.MaxInstructions)
	machine.code = append(machine.code, limit[:]...)
	machine.code = append(machine.code, 0x48, 0x39, 0xc8)                         // cmp rax, rcx
	if err := machine.emitConditionalJump(0x83, instructionFailure); err != nil { // jae
		return err
	}
	machine.code = append(machine.code, 0x49, 0xff, 0x47, 0xf8) // inc qword [r15-8]
	if builder.limits.MaxWallTimeMS != 0 {
		if builder.limits.MaxWallTimeMS < 0 {
			return builder.emitTimeCheck(source, line, column)
		}
		clockCheck := machine.newLabel()
		machine.code = append(machine.code, 0x49, 0xff, 0x4f, 0xf0)           // dec qword [r15-16]
		if err := machine.emitConditionalJump(0x85, clockCheck); err != nil { // jnz
			return err
		}
		builder.emitContextQword(0xf0, directELFWallTimeCheckInterval)
		if err := builder.emitTimeCheck(source, line, column); err != nil {
			return err
		}
		return machine.bind(clockCheck)
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
	// R15 points at the output counter. The context slots follow it toward
	// lower addresses: deadline seconds at -32, current seconds at -48.
	machine.code = append(machine.code, 0xb8, 0xe4, 0, 0, 0, 0xbf, 1, 0, 0, 0, 0x49, 0x8d, 0x77, 0xd0, 0x0f, 0x05)
	machine.code = append(machine.code, 0x49, 0x8b, 0x47, 0xd0, 0x49, 0x3b, 0x47, 0xe0)
	if err := machine.emitConditionalJump(0x87, failure); err != nil { // current seconds > deadline
		return err
	}
	if err := machine.emitConditionalJump(0x82, machineLabel); err != nil { // current seconds < deadline
		return err
	}
	machine.code = append(machine.code, 0x49, 0x8b, 0x47, 0xd8, 0x49, 0x3b, 0x47, 0xe8)
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
		if statement.Kind == "return" && builder.stmtReturn(statement) != nil && builder.stmtReturn(statement).Kind == "call" && builder.stmtReturn(statement).Tail && strings.HasPrefix(builder.stmtReturn(statement).CallTarget, "function:") {
			return builder.emitTailFunctionCall(builder.stmtReturn(statement))
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
			if builder.currentFunction != nil {
				builder.emitFunctionReturnEpilog()
			} else {
				if err := builder.machine.emitJump(builder.machine.endLabel); err != nil {
					return err
				}
			}
			return nil
		}
	}
	return nil
}

func (builder *kirDirectBuilder) emitCallDepthGuard(expression *KIRExpr) error {
	failure := builder.failure(expression.Source, expression.Line, expression.Column, CatResource, "call depth limit exceeded")
	if builder.limits.MaxCallDepth <= 0 {
		return builder.machine.emitJump(failure)
	}
	machine := builder.machine
	machine.code = append(machine.code, 0x49, 0x8b, 0x47, 0xb8) // mov rax, [r15-72]
	machine.code = append(machine.code, 0x48, 0xb9)
	var limit [8]byte
	binary.LittleEndian.PutUint64(limit[:], uint64(builder.limits.MaxCallDepth))
	machine.code = append(machine.code, limit[:]...)
	machine.code = append(machine.code, 0x48, 0x39, 0xc8) // cmp rax, rcx
	return machine.emitConditionalJump(0x83, failure)     // jae
}

func (builder *kirDirectBuilder) emitCallSiteID(expression *KIRExpr) error {
	id, ok := builder.callSiteIDs[expression]
	if !ok || id == 0 {
		return fmt.Errorf("invalid KIR executable: direct function call has no diagnostic callsite")
	}
	builder.machine.code = append(builder.machine.code, 0x41, 0xbb) // mov r11d, callsite id
	var immediate [4]byte
	binary.LittleEndian.PutUint32(immediate[:], id)
	builder.machine.code = append(builder.machine.code, immediate[:]...)
	return nil
}

func (builder *kirDirectBuilder) emitFunctionReturnEpilog() {
	// R11 is scratch here so the function result in RAX remains untouched.
	builder.machine.code = append(builder.machine.code,
		0x4c, 0x8b, 0x5d, 0xf8, // mov r11, [rbp-8]
		0x4d, 0x89, 0x5f, 0xc8, // mov [r15-56], r11
		0x49, 0xff, 0x4f, 0xb8, // dec qword [r15-72]
	)
	builder.machine.emitFunctionEpilog()
}

func (builder *kirDirectBuilder) emitTailFunctionCall(expression *KIRExpr) error {
	if expression == nil || builder.exprReceiver(expression) != nil || builder.exprCallee(expression) != nil {
		return fmt.Errorf("direct KIR ELF does not lower methods or indirect tail calls")
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
		return fmt.Errorf("invalid KIR executable: direct function tail-call target %q is not lowered", expression.CallTarget)
	}
	if len(builder.exprArgs(expression)) != len(function.function.Params) {
		return fmt.Errorf("direct KIR ELF function %q does not lower omitted default arguments", function.function.Name)
	}
	if builder.currentFunction != nil && function == builder.currentFunction {
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
		builder.machine.code = append(builder.machine.code, 0x44, 0x89, 0x5d, 0xf0) // mov [rbp-16], r11d
		for index, parameter := range function.function.Params {
			slot := builder.bindingSlots[kirBindingIdentity(parameter.Binding)]
			if err := builder.machine.emitStoreArg(index, slot); err != nil {
				return err
			}
		}
		return builder.machine.emitJump(function.bodyLabel)
	}
	if builder.currentFunction == nil && builder.entryFunction == nil {
		return fmt.Errorf("invalid KIR executable: tail call is outside a function")
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
	if builder.currentFunction != nil {
		builder.machine.code = append(builder.machine.code,
			0x48, 0x8b, 0x45, 0xf8, // mov rax, [rbp-8]
			0x49, 0x89, 0x47, 0xc8, // mov [r15-56], rax
			0x48, 0x89, 0xec, // mov rsp, rbp
			0x5d, // pop rbp
		)
		return builder.machine.emitJump(function.label)
	}
	// main() is executed in _start's frame. Tail replacement leaves the
	// activation count unchanged and omits main() from the dynamic stack.
	builder.machine.code = append(builder.machine.code, 0x49, 0xc7, 0x47, 0xb0, 0, 0, 0, 0) // mov qword [r15-80], 0
	if err := builder.machine.emitLabelCall(function.label); err != nil {
		return err
	}
	return builder.machine.emitJump(builder.machine.endLabel)
}

func (builder *kirDirectBuilder) emitStmt(statement *KIRStmt) error {
	machine := builder.machine
	switch statement.Kind {
	case "let", "const":
		if err := builder.emitExpr(builder.stmtInit(statement)); err != nil {
			return err
		}
		slot, ok := builder.bindingSlots[kirBindingIdentity(statement.Binding)]
		if !ok {
			return fmt.Errorf("direct KIR ELF backend has no slot for binding %q", statement.Name)
		}
		machine.emitStoreSlot(slot)
	case "assign":
		if err := builder.emitExpr(builder.stmtValue(statement)); err != nil {
			return err
		}
		slot, ok := builder.bindingSlots[kirBindingIdentity(builder.stmtTarget(statement).Binding)]
		if !ok {
			return fmt.Errorf("direct KIR ELF backend has no slot for binding %q", builder.stmtTarget(statement).Name)
		}
		machine.emitStoreSlot(slot)
	case "expr":
		if builder.stmtExpr(statement).Kind != "call" {
			return fmt.Errorf("direct KIR ELF backend encountered an unsupported expression statement")
		}
		if err := builder.emitExpr(builder.stmtExpr(statement)); err != nil {
			return err
		}
	case "if":
		if err := builder.emitExpr(builder.stmtCond(statement)); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x85, 0xc0)
		elseLabel := machine.newLabel()
		joinLabel := machine.newLabel()
		if err := machine.emitConditionalJump(0x84, elseLabel); err != nil {
			return err
		}
		if err := builder.emitBlock(builder.stmtThen(statement), false); err != nil {
			return err
		}
		if len(builder.stmtElse(statement)) > 0 {
			if err := machine.emitJump(joinLabel); err != nil {
				return err
			}
			if err := machine.bind(elseLabel); err != nil {
				return err
			}
			if err := builder.emitBlock(builder.stmtElse(statement), false); err != nil {
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
		if err := builder.emitExpr(builder.stmtCond(statement)); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x85, 0xc0)
		if err := machine.emitConditionalJump(0x84, endLabel); err != nil {
			return err
		}
		builder.loops = append(builder.loops, machineLoop{breakLabel: endLabel, continueLabel: conditionLabel})
		if err := builder.emitBlock(builder.stmtBody(statement), false); err != nil {
			builder.loops = builder.loops[:len(builder.loops)-1]
			return err
		}
		builder.loops = builder.loops[:len(builder.loops)-1]
		if err := machine.emitJump(conditionLabel); err != nil {
			return err
		}
		return machine.bind(endLabel)
	case "for":
		iterator, iteratorOK := builder.forIterSlots[statement]
		index, indexOK := builder.forIndexSlots[statement]
		binding, bindingOK := builder.bindingSlots[kirBindingIdentity(statement.Binding)]
		if !iteratorOK || !indexOK || !bindingOK {
			return fmt.Errorf("invalid KIR executable: direct for loop %q has no allocated slots", statement.Name)
		}
		if err := builder.emitExpr(builder.stmtIter(statement)); err != nil {
			return err
		}
		machine.emitStoreSlot(iterator)
		machine.emitMoveImmediate(0)
		machine.emitStoreSlot(index)
		conditionLabel, incrementLabel, endLabel := machine.newLabel(), machine.newLabel(), machine.newLabel()
		if err := machine.bind(conditionLabel); err != nil {
			return err
		}
		machine.emitLoadSlot(iterator)
		machine.code = append(machine.code, 0x50) // preserve array pointer
		machine.emitLoadSlot(index)
		machine.code = append(machine.code, 0x48, 0x89, 0xc1, 0x58) // rcx=index, rax=array
		machine.code = append(machine.code, 0x48, 0x85, 0xc9)
		if err := machine.emitConditionalJump(0x88, builder.diag(&KIRExpr{Source: statement.Source, Line: statement.Line, Column: statement.Column}, CatRuntime, "index must be a non-negative Int")); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x3b, 0x08) // cmp index, [array]
		if err := machine.emitConditionalJump(0x83, endLabel); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x8b, 0x44, 0xc8, 0x08)
		machine.emitStoreSlot(binding)
		builder.loops = append(builder.loops, machineLoop{breakLabel: endLabel, continueLabel: incrementLabel})
		if err := builder.emitBlock(builder.stmtBody(statement), false); err != nil {
			builder.loops = builder.loops[:len(builder.loops)-1]
			return err
		}
		builder.loops = builder.loops[:len(builder.loops)-1]
		if err := machine.bind(incrementLabel); err != nil {
			return err
		}
		machine.emitLoadSlot(index)
		machine.code = append(machine.code, 0x48, 0xff, 0xc0)
		machine.emitStoreSlot(index)
		if err := machine.emitJump(conditionLabel); err != nil {
			return err
		}
		return machine.bind(endLabel)
	case "break", "continue":
		if len(builder.loops) == 0 {
			return fmt.Errorf("invalid KIR executable: %s outside loop", statement.Kind)
		}
		loop := builder.loops[len(builder.loops)-1]
		if statement.Kind == "break" {
			return machine.emitJump(loop.breakLabel)
		}
		return machine.emitJump(loop.continueLabel)
	case "return":
		if builder.stmtReturn(statement) != nil {
			if err := builder.emitExpr(builder.stmtReturn(statement)); err != nil {
				return err
			}
		} else {
			machine.emitMoveImmediate(0)
		}
	default:
		return fmt.Errorf("direct KIR ELF backend does not support statement %q", statement.Kind)
	}
	return nil
}

func (builder *kirDirectBuilder) emitPrint(expression *KIRExpr) error {
	if len(builder.exprArgs(expression)) != 1 {
		return fmt.Errorf("direct KIR ELF print expects one argument")
	}
	if err := builder.emitExpr(builder.exprArgs(expression)[0]); err != nil {
		return err
	}
	machine := builder.machine
	lineFeed := expression.Name == "println"
	limitLabel := builder.diag(expression, CatResource, "output limit exceeded")
	streamLabel := builder.diag(expression, CatIO, "stream failure")
	oldLimit, oldTrap := machine.outputLimitLabel, machine.trapLabel
	machine.outputLimitLabel, machine.trapLabel = limitLabel, streamLabel
	var err error
	switch builder.exprArgs(expression)[0].Type {
	case "Int":
		err = machine.emitInteger(false, lineFeed)
	case "UInt8", "UInt16", "UInt32", "UInt64":
		err = machine.emitInteger(true, lineFeed)
	case "Bool":
		err = builder.emitBooleanOutput(lineFeed)
	case "String":
		err = machine.emitStringOutput(lineFeed)
	default:
		err = fmt.Errorf("direct KIR ELF cannot print %s", builder.exprArgs(expression)[0].Type)
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
		value := uint64(expression.Int)
		if expression.Const != nil && expression.Const.Kind == "uint" {
			value = expression.Const.UInt
		}
		machine.emitMoveImmediate(value)
		if expression.Type != "Int" {
			typ, err := builder.machineType(expression.Type)
			if err != nil {
				return err
			}
			machine.emitUIntMask(machineBits(typ))
		}
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
	case "struct":
		return builder.emitStructLiteral(expression)
	case "field":
		return builder.emitStructField(expression)
	case "array":
		return builder.emitArrayLiteral(expression)
	case "map":
		return builder.emitMapLiteral(expression)
	case "index":
		return builder.emitArrayIndex(expression)
	case "var":
		slot, ok := builder.bindingSlots[kirBindingIdentity(expression.Binding)]
		if !ok {
			return fmt.Errorf("invalid KIR executable: missing native slot for %q", expression.Name)
		}
		machine.emitLoadSlot(slot)
	case "unary":
		if err := builder.emitExpr(builder.exprOperand(expression)); err != nil {
			return err
		}
		switch expression.Operator {
		case "+":
		case "!":
			machine.code = append(machine.code, 0x48, 0x85, 0xc0, 0x0f, 0x94, 0xc0, 0x48, 0x0f, 0xb6, 0xc0)
		case "-":
			failure := builder.diag(expression, CatRuntime, "negation overflow")
			machine.code = append(machine.code, 0x48, 0xf7, 0xd8)
			operandType, err := builder.machineType(builder.exprOperand(expression).Type)
			if err != nil {
				return err
			}
			if operandType.Kind != TyUInt {
				if err := machine.emitConditionalJump(0x80, failure); err != nil { // jo
					return err
				}
			}
			machine.emitUIntMask(machineBits(operandType))
		case "~":
			operandType, err := builder.machineType(builder.exprOperand(expression).Type)
			if err != nil {
				return err
			}
			machine.code = append(machine.code, 0x48, 0xf7, 0xd0)
			machine.emitUIntMask(machineBits(operandType))
		default:
			return fmt.Errorf("direct KIR ELF does not support unary operator %q", expression.Operator)
		}
	case "binary":
		return builder.emitBinary(expression)
	case "call":
		if strings.HasPrefix(expression.CallTarget, "function:") {
			return builder.emitFunctionCall(expression)
		}
		return builder.emitBuiltin(expression)
	default:
		return fmt.Errorf("direct KIR ELF does not support expression %q", expression.Kind)
	}
	return nil
}

func (builder *kirDirectBuilder) emitBinary(expression *KIRExpr) error {
	machine := builder.machine
	operator := expression.Operator
	if operator == "&&" || operator == "||" {
		if err := builder.emitExpr(builder.exprLeft(expression)); err != nil {
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
		if err := builder.emitExpr(builder.exprRight(expression)); err != nil {
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
	if err := builder.emitExpr(builder.exprLeft(expression)); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x50) // preserve the left operand
	if err := builder.emitExpr(builder.exprRight(expression)); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x48, 0x89, 0xc1, 0x58) // rcx=right, rax=left
	if operator == "+" {
		name, arguments, composite := parseKIRContainerType(builder.exprLeft(expression).Type)
		if composite && name == "Array" && len(arguments) == 1 && builder.exprRight(expression).Type == builder.exprLeft(expression).Type && expression.Type == builder.exprLeft(expression).Type {
			return machine.emitArrayConcatCall()
		}
	}
	if builder.exprLeft(expression).Type == "String" {
		if operator == "+" {
			return machine.emitStringConcatCall()
		}
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
	unsigned := strings.HasPrefix(builder.exprLeft(expression).Type, "UInt")
	if operator == "==" || operator == "!=" || operator == "<" || operator == "<=" || operator == ">" || operator == ">=" {
		conditions := map[string]byte{"==": 0x94, "!=": 0x95, "<": 0x9c, "<=": 0x9e, ">": 0x9f, ">=": 0x9d}
		if unsigned {
			conditions["<"], conditions["<="], conditions[">"], conditions[">="] = 0x92, 0x96, 0x97, 0x93
		}
		condition := conditions[operator]
		machine.code = append(machine.code, 0x48, 0x39, 0xc8, 0x0f, condition, 0xc0, 0x48, 0x0f, 0xb6, 0xc0)
		return nil
	}
	switch operator {
	case "+":
		machine.code = append(machine.code, 0x48, 0x01, 0xc8)
		if unsigned {
			machine.emitUIntMask(kirDirectUIntBits(expression.Type))
			return nil
		}
	case "-":
		machine.code = append(machine.code, 0x48, 0x29, 0xc8)
		if unsigned {
			machine.emitUIntMask(kirDirectUIntBits(expression.Type))
			return nil
		}
	case "*":
		machine.code = append(machine.code, 0x48, 0x0f, 0xaf, 0xc1)
		if unsigned {
			machine.emitUIntMask(kirDirectUIntBits(expression.Type))
			return nil
		}
	case "&", "|", "^":
		opcode := byte(0x21)
		if operator == "|" {
			opcode = 0x09
		} else if operator == "^" {
			opcode = 0x31
		}
		machine.code = append(machine.code, 0x48, opcode, 0xc8)
		machine.emitUIntMask(kirDirectUIntBits(expression.Type))
		return nil
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
		if unsigned {
			machine.code = append(machine.code, 0x48, 0x31, 0xd2, 0x48, 0xf7, 0xf1)
			if operator == "%" {
				machine.code = append(machine.code, 0x48, 0x89, 0xd0)
			}
			machine.emitUIntMask(kirDirectUIntBits(expression.Type))
			return nil
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
	case "<<", ">>":
		shiftType, err := builder.machineType(builder.exprLeft(expression).Type)
		if err != nil {
			return err
		}
		bits := machineBits(shiftType)
		machine.code = append(machine.code, 0x48, 0x83, 0xf9, bits) // reject negative and out-of-width counts
		if err := machine.emitConditionalJump(0x83, machine.trapLabel); err != nil {
			return err
		}
		if operator == "<<" {
			machine.code = append(machine.code, 0x48, 0xd3, 0xe0)
		} else {
			machine.code = append(machine.code, 0x48, 0xd3, 0xe8)
		}
		machine.emitUIntMask(bits)
		return nil
	default:
		return fmt.Errorf("direct KIR ELF does not support binary operator %q", operator)
	}
	if unsigned {
		machine.emitUIntMask(kirDirectUIntBits(expression.Type))
		return nil
	}
	overflowFailure := builder.diag(expression, CatRuntime, "checked integer arithmetic overflow")
	return machine.emitConditionalJump(0x80, overflowFailure)
}
