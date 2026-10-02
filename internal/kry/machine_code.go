package kry

import (
	"encoding/binary"
	"fmt"
	"strings"
)

const directPEWindowsMaxArgs = 8

// directMachine stores instruction emission state shared by the direct ELF and
// PE KIR lowerers. Its machine-code primitives do not depend on source AST.
type directMachine struct {
	code                []byte
	data                []byte
	dataByText          map[string]int
	stringObjects       map[string]int
	dataRefs            []machineDataRef
	labels              []machineLabel
	nextSlot            int32
	loops               []machineLoop
	endLabel            int
	trapLabel           int
	bufferOffset        int32
	functionLabels      map[string]int
	stringConcatLabel   int
	stringConcatUsed    bool
	arrayAllocLabel     int
	arrayPushLabel      int
	arrayConcatLabel    int
	arrayRuntimeUsed    bool
	boxAllocLabel       int
	boxRuntimeUsed      bool
	stringAllocLabel    int
	stringCharsLabel    int
	stringEqualLabel    int
	mapFindLabel        int
	mapInsertLabel      int
	substringLabel      int
	intToStringLabel    int
	intFromStringLabel  int
	u8ArrayLabel        int
	arraySetLabel       int
	arraySliceLabel     int
	arrayReverseLabel   int
	mapRemoveLabel      int
	mapRuntimeUsed      bool
	stringEqualUsed     bool
	stringCharsUsed     bool
	substringUsed       bool
	intToStringUsed     bool
	intFromStringUsed   bool
	bytesFromArrayLabel int
	processArgsLabel    int
	fsReadTextLabel     int
	fsWriteBytesLabel   int
	bytesFromArrayUsed  bool
	u8ArrayUsed         bool
	fsReadTextUsed      bool
	fsWriteBytesUsed    bool
	hostRuntimeUsed     bool
	windowsABI          bool
	windowsStackDepth   int
	outputLimit         int64
	outputLimitSet      bool
	outputLimitLabel    int
	peImportRefs        []peImportRef
	peFunctions         []peFunctionRange
}

type machineSlot struct {
	offset int32
	typ    *Type
}

type machineLoop struct {
	breakLabel    int
	continueLabel int
}

type machineLabel struct {
	position int
	patches  []int
}

type machineDataRef struct {
	displacement   int
	instructionEnd int
	dataOffset     int
}

type peImportRef struct {
	displacement   int
	instructionEnd int
	importIndex    int
}

type peFunctionRange struct {
	begin int
	end   int
	frame uint32
}

func newDirectMachine() *directMachine {
	m := &directMachine{
		dataByText:       map[string]int{},
		stringObjects:    map[string]int{},
		functionLabels:   map[string]int{},
		outputLimitLabel: -1,
	}
	m.endLabel = m.newLabel()
	m.trapLabel = m.newLabel()
	m.stringConcatLabel = m.newLabel()
	m.arrayAllocLabel = m.newLabel()
	m.arrayPushLabel = m.newLabel()
	m.arrayConcatLabel = m.newLabel()
	m.boxAllocLabel = m.newLabel()
	m.stringAllocLabel = m.newLabel()
	m.stringCharsLabel = m.newLabel()
	m.stringEqualLabel = m.newLabel()
	m.mapFindLabel = m.newLabel()
	m.mapInsertLabel = m.newLabel()
	m.substringLabel = m.newLabel()
	m.intToStringLabel = m.newLabel()
	m.intFromStringLabel = m.newLabel()
	m.u8ArrayLabel = m.newLabel()
	m.arraySetLabel = m.newLabel()
	m.arraySliceLabel = m.newLabel()
	m.arrayReverseLabel = m.newLabel()
	m.mapRemoveLabel = m.newLabel()
	m.bytesFromArrayLabel = m.newLabel()
	m.processArgsLabel = m.newLabel()
	m.fsReadTextLabel = m.newLabel()
	m.fsWriteBytesLabel = m.newLabel()
	return m
}

func (m *directMachine) newLabel() int {
	m.labels = append(m.labels, machineLabel{position: -1})
	return len(m.labels) - 1
}

func (m *directMachine) bind(label int) error {
	if label < 0 || label >= len(m.labels) || m.labels[label].position >= 0 {
		return fmt.Errorf("direct ELF internal error: invalid label binding")
	}
	m.labels[label].position = len(m.code)
	for _, patch := range m.labels[label].patches {
		delta := m.labels[label].position - (patch + 4)
		if delta < -1<<31 || delta > 1<<31-1 {
			return fmt.Errorf("direct ELF internal error: jump is out of range")
		}
		binary.LittleEndian.PutUint32(m.code[patch:patch+4], uint32(int32(delta)))
	}
	return nil
}

func (m *directMachine) validateLabelReferences() error {
	for index, label := range m.labels {
		if label.position < 0 && len(label.patches) > 0 {
			return fmt.Errorf("direct machine internal error: label %d has unresolved references", index)
		}
	}
	return nil
}

func (m *directMachine) emitJump(label int) error {
	m.code = append(m.code, 0xe9)
	return m.emitLabelDisplacement(label)
}

func (m *directMachine) emitStringConcatCall() error {
	// The KIR emitter leaves the two String pointers in RAX and RCX.
	m.code = append(m.code, 0x48, 0x89, 0xc7, 0x48, 0x89, 0xce)
	m.stringConcatUsed = true
	return m.emitLabelCall(m.stringConcatLabel)
}

func (m *directMachine) emitArrayConcatCall() error {
	// The KIR emitter leaves the two Array pointers in RAX and RCX.
	m.code = append(m.code, 0x48, 0x89, 0xc7, 0x48, 0x89, 0xce)
	m.arrayRuntimeUsed = true
	return m.emitLabelCall(m.arrayConcatLabel)
}

func (m *directMachine) emitLabelCall(label int) error {
	var adjust byte
	if m.windowsABI {
		adjust = byte(32 + ((16 - m.windowsStackDepth%16) % 16))
		m.code = append(m.code, 0x48, 0x83, 0xec, adjust) // shadow space and call-site alignment
	}
	m.code = append(m.code, 0xe8)
	if err := m.emitLabelDisplacement(label); err != nil {
		return err
	}
	if m.windowsABI {
		m.code = append(m.code, 0x48, 0x83, 0xc4, adjust)
	}
	return nil
}

func (m *directMachine) emitConditionalJump(op byte, label int) error {
	m.code = append(m.code, 0x0f, op)
	return m.emitLabelDisplacement(label)
}

func (m *directMachine) emitLabelDisplacement(label int) error {
	if label < 0 || label >= len(m.labels) {
		return fmt.Errorf("direct ELF internal error: invalid jump label")
	}
	patch := len(m.code)
	m.code = append(m.code, 0, 0, 0, 0)
	if m.labels[label].position >= 0 {
		delta := m.labels[label].position - (patch + 4)
		binary.LittleEndian.PutUint32(m.code[patch:patch+4], uint32(int32(delta)))
	} else {
		m.labels[label].patches = append(m.labels[label].patches, patch)
	}
	return nil
}

func (m *directMachine) addData(text string) int {
	if offset, ok := m.dataByText[text]; ok {
		return offset
	}
	offset := len(m.data)
	m.data = append(m.data, []byte(text)...)
	m.dataByText[text] = offset
	return offset
}

func (m *directMachine) addStringObject(text string) int {
	if offset, ok := m.stringObjects[text]; ok {
		return offset
	}
	offset := len(m.data)
	var length [8]byte
	binary.LittleEndian.PutUint64(length[:], uint64(len(text)))
	m.data = append(m.data, length[:]...)
	m.data = append(m.data, []byte(text)...)
	m.stringObjects[text] = offset
	return offset
}

func (m *directMachine) emitStringAddress(text string) {
	offset := m.addStringObject(text)
	start := len(m.code)
	m.code = append(m.code, 0x48, 0x8d, 0x05, 0, 0, 0, 0) // lea rax, [rip+disp32]
	m.dataRefs = append(m.dataRefs, machineDataRef{displacement: start + 3, instructionEnd: start + 7, dataOffset: offset})
}

func (m *directMachine) emitStringWrite() error {
	return m.emitStringOutput(false)
}

func (m *directMachine) emitStringOutput(newline bool) error {
	// RAX points to {u64 length, u8 bytes[length]}. Preflight the complete
	// print/println operation before writing either the bytes or its newline.
	m.code = append(m.code, 0x49, 0x89, 0xc1) // mov r9, rax
	if m.windowsABI {
		// WriteFile accepts a DWORD length. Reject wider strings instead of
		// silently truncating their length during the ABI conversion.
		m.code = append(m.code, 0x41, 0x83, 0x79, 0x04, 0x00) // cmp dword [r9+4], 0
		if err := m.emitConditionalJump(0x85, m.trapLabel); err != nil {
			return err
		}
	}
	m.code = append(m.code, 0x4d, 0x8b, 0x01) // mov r8, [r9]
	if newline {
		m.code = append(m.code, 0x49, 0xff, 0xc0) // inc r8
	}
	if err := m.emitOutputBudgetAddR8(); err != nil {
		return err
	}
	if m.windowsABI {
		m.code = append(m.code, 0x41, 0x8b, 0x01)       // mov eax, [r9]
		m.code = append(m.code, 0x41, 0x89, 0xc0)       // mov r8d, eax
		m.code = append(m.code, 0x4c, 0x89, 0xca)       // mov rdx, r9
		m.code = append(m.code, 0x48, 0x83, 0xc2, 0x08) // lea rdx, [rdx+8]
		if err := m.emitPEWriteRDXR8(); err != nil {
			return err
		}
	} else {
		m.code = append(m.code, 0x49, 0x8b, 0x11)       // mov rdx, [r9]
		m.code = append(m.code, 0x49, 0x8d, 0x71, 0x08) // lea rsi, [r9+8]
		if err := m.emitELFWriteRSIRDX(); err != nil {
			return err
		}
	}
	if newline {
		return m.emitWriteRaw("\n")
	}
	return nil
}

// emitOutputCounterInit reserves an aligned qword in the entry frame and puts
// its address in R15, a callee-saved register shared with generated functions.
func (m *directMachine) emitOutputCounterInit(offset int32) {
	displacement := -offset
	m.code = append(m.code, 0x48, 0xc7, 0x85)
	var disp [4]byte
	binary.LittleEndian.PutUint32(disp[:], uint32(displacement))
	m.code = append(m.code, disp[:]...)
	m.code = append(m.code, 0, 0, 0, 0) // mov qword [rbp+disp32], 0
	m.code = append(m.code, 0x4c, 0x8d, 0xbd)
	m.code = append(m.code, disp[:]...) // lea r15, [rbp+disp32]
}

// emitOutputBudgetAddR8 charges a whole logical output operation, including
// println's newline, before any bytes are sent. It preserves R8 and the
// output pointer registers used by both native ABIs.
func (m *directMachine) emitOutputBudgetAddR8() error {
	if !m.outputLimitSet {
		return nil
	}
	limitLabel := m.outputLimitLabel
	if limitLabel < 0 {
		limitLabel = m.trapLabel
	}
	if m.outputLimit < 0 {
		return m.emitJump(limitLabel)
	}
	m.code = append(m.code,
		0x49, 0x8b, 0x07, // mov rax, [r15]
		0x4c, 0x01, 0xc0, // add rax, r8
	)
	if err := m.emitConditionalJump(0x82, limitLabel); err != nil { // jc: qword addition overflowed
		return err
	}
	m.code = append(m.code, 0x48, 0xb9)
	var limit [8]byte
	binary.LittleEndian.PutUint64(limit[:], uint64(m.outputLimit))
	m.code = append(m.code, limit[:]...)                            // mov rcx, configured output limit
	m.code = append(m.code, 0x48, 0x39, 0xc8)                       // cmp rax, rcx
	if err := m.emitConditionalJump(0x87, limitLabel); err != nil { // ja: cumulative bytes exceed the limit
		return err
	}
	m.code = append(m.code, 0x49, 0x89, 0x07) // mov [r15], rax
	return nil
}

// emitELFWriteRSIRDX emits Linux write(1, RSI, RDX), retrying EINTR, handling
// short writes, and trapping on zero progress or any other syscall error.
func (m *directMachine) emitELFWriteRSIRDX() error {
	return m.emitELFWriteRSIRDXFD(1)
}

// emitELFWriteRSIRDXFD emits Linux write(fd, RSI, RDX), retrying EINTR,
// handling short writes, and trapping on zero progress or any other syscall
// error.
func (m *directMachine) emitELFWriteRSIRDXFD(fd byte) error {
	empty := m.newLabel()
	loop := m.newLabel()
	done := m.newLabel()
	if fd > 2 {
		return fmt.Errorf("direct ELF write descriptor %d is unsupported", fd)
	}
	m.code = append(m.code, 0x48, 0x85, 0xd2) // test rdx, rdx
	if err := m.emitConditionalJump(0x84, empty); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x89, 0xf1) // mov r9, rsi
	m.code = append(m.code, 0x49, 0x89, 0xd2) // mov r10, rdx
	if err := m.bind(loop); err != nil {
		return err
	}
	m.code = append(m.code,
		0xb8, 0x01, 0, 0, 0, // mov eax, SYS_write
		0xbf, fd, 0, 0, 0, // mov edi, file descriptor
		0x4c, 0x89, 0xce, // mov rsi, r9
		0x4c, 0x89, 0xd2, // mov rdx, r10
		0x0f, 0x05, // syscall
		0x48, 0x83, 0xf8, 0xfc, // cmp rax, -EINTR
	)
	if err := m.emitConditionalJump(0x84, loop); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x85, 0xc0)                        // test rax, rax
	if err := m.emitConditionalJump(0x8e, m.trapLabel); err != nil { // jle: zero progress or negative errno
		return err
	}
	m.code = append(m.code, 0x49, 0x01, 0xc1)                 // add r9, rax
	m.code = append(m.code, 0x49, 0x29, 0xc2)                 // sub r10, rax
	if err := m.emitConditionalJump(0x85, loop); err != nil { // jnz while bytes remain
		return err
	}
	if err := m.emitJump(done); err != nil {
		return err
	}
	if err := m.bind(empty); err != nil {
		return err
	}
	if err := m.bind(done); err != nil {
		return err
	}
	return nil
}

// emitELFWriteRawTo writes static bytes to a selected Linux file descriptor.
// It is used for diagnostics so executable KIR errors remain visible on stderr.
func (m *directMachine) emitELFWriteRawTo(text string, fd byte) error {
	if uint64(len(text)) > uint64(^uint32(0)) {
		return fmt.Errorf("direct ELF write exceeds the syscall byte-count limit")
	}
	offset := m.addData(text)
	start := len(m.code)
	m.code = append(m.code, 0x48, 0x8d, 0x35, 0, 0, 0, 0) // lea rsi, [rip + text]
	m.code = append(m.code, 0xba, 0, 0, 0, 0)             // mov edx, byte length
	binary.LittleEndian.PutUint32(m.code[len(m.code)-4:], uint32(len(text)))
	m.dataRefs = append(m.dataRefs, machineDataRef{displacement: start + 3, instructionEnd: start + 7, dataOffset: offset})
	return m.emitELFWriteRSIRDXFD(fd)
}

const (
	peImportGetStdHandle = iota
	peImportWriteFile
	peImportExitProcess
	peImportGetProcessHeap
	peImportHeapAlloc
	peImportGetTickCount64
)

// emitPEImportedCall emits a RIP-relative indirect call through the PE IAT.
// The IAT RVA is known only after calculating the final read-only data size.
func (m *directMachine) emitPEImportedCall(importIndex int) {
	m.code = append(m.code, 0xff, 0x15)
	displacement := len(m.code)
	m.code = append(m.code, 0, 0, 0, 0)
	m.peImportRefs = append(m.peImportRefs, peImportRef{
		displacement: displacement, instructionEnd: len(m.code), importIndex: importIndex,
	})
}

// emitPEStringFromInteger returns a heap-backed String object in RAX. PE
// programs are one-shot processes, so Windows reclaims these immutable values
// at process exit; allocating each conversion keeps returned and aliased
// strings valid when the same str expression runs again.
func (m *directMachine) emitPEStringFromInteger(unsigned bool) error {
	m.code = append(m.code, 0x50) // preserve input while querying the process heap
	m.windowsStackDepth += 8
	outgoing := 32
	if m.windowsStackDepth%16 != 0 {
		outgoing += 16 - m.windowsStackDepth%16
	}
	m.code = append(m.code, 0x48, 0x83, 0xec, byte(outgoing))
	m.windowsStackDepth += outgoing
	m.emitPEImportedCall(peImportGetProcessHeap)
	m.code = append(m.code, 0x48, 0x89, 0xc1, 0x31, 0xd2, 0x41, 0xb8, 28, 0, 0, 0)
	m.emitPEImportedCall(peImportHeapAlloc)
	m.code = append(m.code, 0x48, 0x85, 0xc0)
	if err := m.emitConditionalJump(0x84, m.trapLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x89, 0xc3) // preserve object in R11
	m.code = append(m.code, 0x48, 0x8b, 0x84, 0x24)
	var saved [4]byte
	binary.LittleEndian.PutUint32(saved[:], uint32(outgoing))
	m.code = append(m.code, saved[:]...) // restore source value from above shadow space
	m.code = append(m.code, 0x48, 0x83, 0xc4, byte(outgoing+8))
	m.windowsStackDepth -= outgoing + 8
	m.code = append(m.code, 0x4d, 0x8d, 0x43, 28) // R8 = object + 28 (20-byte digit area)
	m.code = append(m.code, 0x49, 0xb9)
	var ten [8]byte
	binary.LittleEndian.PutUint64(ten[:], 10)
	m.code = append(m.code, ten[:]...)
	m.code = append(m.code, 0x45, 0x31, 0xd2) // R10b records a minus sign
	zero, digits, addSign, ready := m.newLabel(), m.newLabel(), m.newLabel(), m.newLabel()
	m.code = append(m.code, 0x48, 0x85, 0xc0)
	if err := m.emitConditionalJump(0x84, zero); err != nil {
		return err
	}
	if !unsigned {
		if err := m.emitConditionalJump(0x89, digits); err != nil { // non-negative
			return err
		}
		m.code = append(m.code, 0x41, 0xb2, 1, 0x48, 0xf7, 0xd8) // mark and negate
	}
	if err := m.bind(digits); err != nil {
		return err
	}
	loop := m.newLabel()
	if err := m.bind(loop); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x31, 0xd2, 0x49, 0xf7, 0xf1, 0x80, 0xc2, '0', 0x49, 0xff, 0xc8, 0x41, 0x88, 0x10)
	m.code = append(m.code, 0x48, 0x85, 0xc0)
	if err := m.emitConditionalJump(0x85, loop); err != nil {
		return err
	}
	if err := m.emitJump(addSign); err != nil {
		return err
	}
	if err := m.bind(zero); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc8, 0x41, 0xc6, 0x00, '0')
	if err := m.emitJump(addSign); err != nil {
		return err
	}
	if err := m.bind(addSign); err != nil {
		return err
	}
	if !unsigned {
		m.code = append(m.code, 0x45, 0x84, 0xd2)
		if err := m.emitConditionalJump(0x84, ready); err != nil {
			return err
		}
		m.code = append(m.code, 0x49, 0xff, 0xc8, 0x41, 0xc6, 0x00, '-')
	}
	if err := m.bind(ready); err != nil {
		return err
	}
	// The emitted digits are right-aligned. Put the length immediately before
	// them so the ordinary {u64 length, bytes} String layout needs no copy.
	m.code = append(m.code, 0x49, 0x8d, 0x4b, 28, 0x4c, 0x29, 0xc1, 0x4c, 0x89, 0xc0, 0x48, 0x83, 0xe8, 8, 0x48, 0x89, 0x08)
	return nil
}

// emitPEWriteRDXR8 writes the bytes addressed by RDX with a DWORD length in
// R8D. It loops over partial WriteFile results and treats invalid handles,
// failed writes, and successful zero-byte writes as process failure.
func (m *directMachine) emitPEWriteRDXR8() error {
	return m.emitPEWriteRDXR8To(0xfffffff5, m.trapLabel) // STD_OUTPUT_HANDLE
}

func (m *directMachine) emitPEWriteRDXR8To(stdHandle uint32, failureLabel int) error {
	empty := m.newLabel()
	loop := m.newLabel()
	failed := m.newLabel()
	done := m.newLabel()
	m.code = append(m.code, 0x45, 0x85, 0xc0) // test r8d, r8d
	if err := m.emitConditionalJump(0x84, empty); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x83, 0xec, 0x40)                   // home area plus aligned stack temporaries
	m.code = append(m.code, 0x48, 0x89, 0x54, 0x24, 0x30)             // [rsp+48] = buffer
	m.code = append(m.code, 0x44, 0x89, 0x44, 0x24, 0x38)             // [rsp+56] = remaining DWORD
	m.code = append(m.code, 0x48, 0xc7, 0x44, 0x24, 0x20, 0, 0, 0, 0) // fifth argument = NULL
	m.code = append(m.code, 0xb9)
	var handle [4]byte
	binary.LittleEndian.PutUint32(handle[:], stdHandle)
	m.code = append(m.code, handle[:]...)
	m.emitPEImportedCall(peImportGetStdHandle)
	m.code = append(m.code, 0x48, 0x85, 0xc0) // reject NULL
	if err := m.emitConditionalJump(0x84, failed); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x83, 0xf8, 0xff) // reject INVALID_HANDLE_VALUE
	if err := m.emitConditionalJump(0x84, failed); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0x44, 0x24, 0x28) // [rsp+40] = handle
	if err := m.bind(loop); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x8b, 0x4c, 0x24, 0x28) // rcx = handle
	m.code = append(m.code, 0x48, 0x8b, 0x54, 0x24, 0x30) // rdx = buffer
	m.code = append(m.code, 0x44, 0x8b, 0x44, 0x24, 0x38) // r8d = remaining
	m.code = append(m.code, 0x4c, 0x8d, 0x4c, 0x24, 0x3c) // r9 = &bytesWritten
	m.emitPEImportedCall(peImportWriteFile)
	m.code = append(m.code, 0x85, 0xc0) // test eax, eax
	if err := m.emitConditionalJump(0x84, failed); err != nil {
		return err
	}
	m.code = append(m.code, 0x8b, 0x44, 0x24, 0x3c, 0x85, 0xc0) // bytesWritten; reject zero progress
	if err := m.emitConditionalJump(0x84, failed); err != nil {
		return err
	}
	m.code = append(m.code, 0x3b, 0x44, 0x24, 0x38) // reject bytesWritten > remaining
	if err := m.emitConditionalJump(0x87, failed); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x01, 0x44, 0x24, 0x30) // buffer += bytesWritten
	m.code = append(m.code, 0x29, 0x44, 0x24, 0x38)       // remaining -= bytesWritten
	if err := m.emitConditionalJump(0x85, loop); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x83, 0xc4, 0x40)
	if err := m.emitJump(done); err != nil {
		return err
	}
	if err := m.bind(failed); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x83, 0xc4, 0x40)
	if err := m.emitJump(failureLabel); err != nil {
		return err
	}
	if err := m.bind(empty); err != nil {
		return err
	}
	return m.bind(done)
}

func (m *directMachine) emitStringConcatRuntime() error {
	if err := m.bind(m.stringConcatLabel); err != nil {
		return err
	}
	// Preserve the callee-saved registers used as the two source pointers and
	// lengths. The allocator uses the Linux mmap syscall directly; no libc or
	// C runtime is involved.
	m.code = append(m.code,
		0x53, 0x55, 0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x41, 0x57,
		0x49, 0x89, 0xfc, 0x49, 0x89, 0xf5,
		0x49, 0x8b, 0x04, 0x24, 0x49, 0x89, 0xc6,
		0x49, 0x8b, 0x45, 0x00, 0x49, 0x89, 0xc7,
		0x4c, 0x89, 0xf5, 0x4d, 0x01, 0xfe,
	)
	m.emitTrapOnOverflow()
	m.code = append(m.code,
		0x4c, 0x89, 0xf7, // mov rdi, r14
		0x48, 0x83, 0xc7, 0x08,
		0x48, 0x89, 0xfe, 0x48, 0x31, 0xff,
		0xb8, 0x09, 0x00, 0x00, 0x00,
		0xba, 0x03, 0x00, 0x00, 0x00,
		0x41, 0xba, 0x22, 0x00, 0x00, 0x00,
		0x41, 0xb8, 0xff, 0xff, 0xff, 0xff,
		0x45, 0x31, 0xc9,
		0x0f, 0x05,
		0x48, 0x85, 0xc0,
	)
	if err := m.emitConditionalJump(0x88, m.trapLabel); err != nil { // js on mmap error
		return err
	}
	m.code = append(m.code,
		0x48, 0x89, 0xc3,
		0x4c, 0x89, 0x33,
		0x4d, 0x8d, 0x64, 0x24, 0x08,
		0x4c, 0x89, 0xe6, 0x48, 0x8d, 0x7b, 0x08,
		0x48, 0x89, 0xe9, 0xf3, 0xa4,
		0x4d, 0x8d, 0x6d, 0x08, 0x4c, 0x89, 0xee,
		0x48, 0x8d, 0x7b, 0x08, 0x48, 0x01, 0xef,
		0x4c, 0x89, 0xf9, 0xf3, 0xa4,
		0x48, 0x89, 0xd8,
		0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3,
	)
	return nil
}

func (m *directMachine) emitArrayAllocCall() error {
	// The element count is in RAX; the allocator receives it in RDI and
	// returns an immutable {u64 length, u64 elements[]} object in RAX.
	m.code = append(m.code, 0x48, 0x89, 0xc7)
	m.arrayRuntimeUsed = true
	return m.emitLabelCall(m.arrayAllocLabel)
}

func (m *directMachine) emitArrayAllocRuntime() error {
	if err := m.bind(m.arrayAllocLabel); err != nil {
		return err
	}
	// mmap((count + 1) * 8, PROT_READ|PROT_WRITE, MAP_PRIVATE|MAP_ANON,
	//      -1, 0), with the count written into the first word.
	m.code = append(m.code,
		0x53, 0x55,
		0x48, 0x89, 0xfd, // mov rbp, rdi (preserve count)
		0x48, 0x89, 0xf8, 0x48, 0xff, 0xc0,
	)
	m.emitTrapOnOverflow()
	m.code = append(m.code,
		0x48, 0xc1, 0xe0, 0x03,
	)
	m.emitTrapOnOverflow()
	m.code = append(m.code,
		0x48, 0x89, 0xc7,
		0x48, 0x89, 0xfe, 0x48, 0x31, 0xff,
		0xba, 0x03, 0x00, 0x00, 0x00,
		0x41, 0xba, 0x22, 0x00, 0x00, 0x00,
		0x41, 0xb8, 0xff, 0xff, 0xff, 0xff,
		0x45, 0x31, 0xc9,
		0xb8, 0x09, 0x00, 0x00, 0x00, 0x0f, 0x05,
		0x48, 0x85, 0xc0,
	)
	if err := m.emitConditionalJump(0x88, m.trapLabel); err != nil { // js: mmap error
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0x28, 0x5d, 0x5b, 0xc3) // [rax]=count; restore; return
	return nil
}

func (m *directMachine) emitArrayPushRuntime() error {
	if err := m.bind(m.arrayPushLabel); err != nil {
		return err
	}
	// rdi=array, rsi=value. The object is immutable, so allocate a new
	// object, copy the old qwords, and append the value.
	m.code = append(m.code,
		0x53, 0x55, 0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x41, 0x57,
		0x49, 0x89, 0xfc, 0x49, 0x89, 0xf5,
		0x49, 0x8b, 0x04, 0x24, 0x49, 0x89, 0xc6,
		0x4d, 0x89, 0xf7, 0x49, 0x83, 0xc7, 0x01,
	)
	m.emitTrapOnOverflow()
	m.code = append(m.code, 0x4c, 0x89, 0xff) // mov rdi, r15
	if err := m.emitLabelCall(m.arrayAllocLabel); err != nil {
		return err
	}
	m.code = append(m.code,
		0x48, 0x89, 0xc3,
		0x49, 0x8d, 0x74, 0x24, 0x08,
		0x48, 0x8d, 0x7b, 0x08,
		0x4c, 0x89, 0xf1, 0xf3, 0x48, 0xa5,
		0x4c, 0x89, 0xf2, 0x48, 0xc1, 0xe2, 0x03,
		0x48, 0x01, 0xda, 0x48, 0x83, 0xc2, 0x08,
		0x4c, 0x89, 0x2a,
		0x48, 0x89, 0xd8,
		0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3,
	)
	return nil
}

func (m *directMachine) emitArrayConcatRuntime() error {
	if err := m.bind(m.arrayConcatLabel); err != nil {
		return err
	}
	// rdi=left, rsi=right. Both operands and the result use the same qword
	// object ABI as array_push.
	m.code = append(m.code,
		0x53, 0x55, 0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x41, 0x57,
		0x49, 0x89, 0xfc, 0x49, 0x89, 0xf5,
		0x49, 0x8b, 0x04, 0x24, 0x49, 0x89, 0xc6,
		0x49, 0x8b, 0x45, 0x00, 0x49, 0x89, 0xc7,
		0x4c, 0x89, 0xf5, 0x4c, 0x01, 0xfd,
	)
	m.emitTrapOnOverflow()
	m.code = append(m.code, 0x48, 0x89, 0xef) // mov rdi, rbp
	if err := m.emitLabelCall(m.arrayAllocLabel); err != nil {
		return err
	}
	m.code = append(m.code,
		0x48, 0x89, 0xc3,
		0x49, 0x8d, 0x74, 0x24, 0x08,
		0x48, 0x8d, 0x7b, 0x08,
		0x4c, 0x89, 0xf1, 0xf3, 0x48, 0xa5,
		0x49, 0x8d, 0x75, 0x08,
		0x4c, 0x89, 0xf9, 0xf3, 0x48, 0xa5,
		0x48, 0x89, 0xd8,
		0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3,
	)
	return nil
}

func (m *directMachine) emitBoxCall(tag uint64) error {
	m.code = append(m.code, 0x48, 0x89, 0xc7, 0x48, 0xbe)
	var value [8]byte
	binary.LittleEndian.PutUint64(value[:], tag)
	m.code = append(m.code, value[:]...)
	m.boxRuntimeUsed = true
	return m.emitLabelCall(m.boxAllocLabel)
}

func (m *directMachine) emitELFWriteStringFD(fd byte) error {
	// RAX points at {u64 byte length, u8 UTF-8 data[length]}.
	m.code = append(m.code, 0x48, 0x8b, 0x10, 0x48, 0x8d, 0x70, 0x08)
	return m.emitELFWriteRSIRDXFD(fd)
}

func (m *directMachine) emitELFIntegerToFD(unsigned bool, fd byte) error {
	if fd > 2 {
		return fmt.Errorf("direct ELF integer output descriptor %d is unsupported", fd)
	}
	// Use the reserved per-frame digit buffer so formatting a diagnostic cannot
	// allocate memory and fail before the source diagnostic reaches stderr.
	m.code = append(m.code, 0x4c, 0x8d, 0x85)
	var buffer [4]byte
	binary.LittleEndian.PutUint32(buffer[:], uint32(-m.bufferOffset))
	m.code = append(m.code, buffer[:]...)
	m.code = append(m.code, 0x49, 0xb9)
	var ten [8]byte
	binary.LittleEndian.PutUint64(ten[:], 10)
	m.code = append(m.code, ten[:]...)
	zero := m.newLabel()
	digits := m.newLabel()
	addSign := m.newLabel()
	ready := m.newLabel()
	m.code = append(m.code, 0x45, 0x31, 0xd2, 0x48, 0x85, 0xc0) // r10d=0; test value
	if err := m.emitConditionalJump(0x84, zero); err != nil {
		return err
	}
	if !unsigned {
		if err := m.emitConditionalJump(0x89, digits); err != nil { // jns
			return err
		}
		m.code = append(m.code, 0x41, 0xb2, 0x01, 0x48, 0xf7, 0xd8) // negative flag; abs (MinInt remains unsigned magnitude)
	}
	if err := m.bind(digits); err != nil {
		return err
	}
	loop := m.newLabel()
	if err := m.bind(loop); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x31, 0xd2, 0x49, 0xf7, 0xf1, 0x80, 0xc2, 0x30, 0x49, 0xff, 0xc8, 0x41, 0x88, 0x10, 0x48, 0x85, 0xc0)
	if err := m.emitConditionalJump(0x85, loop); err != nil {
		return err
	}
	if err := m.emitJump(addSign); err != nil {
		return err
	}
	if err := m.bind(zero); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc8, 0x41, 0xc6, 0x00, 0x30) // emit zero
	if err := m.emitJump(addSign); err != nil {
		return err
	}
	if err := m.bind(addSign); err != nil {
		return err
	}
	if !unsigned {
		m.code = append(m.code, 0x45, 0x84, 0xd2)
		if err := m.emitConditionalJump(0x84, ready); err != nil {
			return err
		}
		m.code = append(m.code, 0x49, 0xff, 0xc8, 0x41, 0xc6, 0x00, 0x2d) // prepend '-'
	}
	if err := m.bind(ready); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xc6, 0x48, 0x8d, 0x95) // rsi=first digit; rdx=fixed buffer end
	m.code = append(m.code, buffer[:]...)
	m.code = append(m.code, 0x48, 0x29, 0xf2) // rdx = byte count
	return m.emitELFWriteRSIRDXFD(fd)
}

func (m *directMachine) emitStructAllocCall(fieldCount int) error {
	if fieldCount < 0 {
		return fmt.Errorf("direct ELF backend received a negative struct field count")
	}
	m.emitMoveImmediate(uint64(fieldCount))
	return m.emitArrayAllocCall()
}

func (m *directMachine) emitBoxAllocRuntime() error {
	if err := m.bind(m.boxAllocLabel); err != nil {
		return err
	}
	m.code = append(m.code,
		0x53, 0x55, 0x48, 0x89, 0xfd, 0x48, 0x89, 0xf3,
		0x48, 0x31, 0xff,
		0x48, 0xbe, 0x10, 0, 0, 0, 0, 0, 0, 0,
		0xba, 0x03, 0, 0, 0,
		0x41, 0xba, 0x22, 0, 0, 0, 0x41, 0xb8, 0xff, 0xff, 0xff, 0xff,
		0x45, 0x31, 0xc9, 0xb8, 0x09, 0, 0, 0, 0x0f, 0x05, 0x48, 0x85, 0xc0,
	)
	if err := m.emitConditionalJump(0x88, m.trapLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0x18, 0x48, 0x89, 0x68, 0x08, 0x5d, 0x5b, 0xc3)
	return nil
}

func (m *directMachine) emitStringAllocRuntime() error {
	if err := m.bind(m.stringAllocLabel); err != nil {
		return err
	}
	// rdi=source bytes, rsi=length. Return a {length, bytes[]} String object.
	m.code = append(m.code,
		0x53, 0x55, 0x41, 0x54, 0x41, 0x55, 0x41, 0x56,
		0x49, 0x89, 0xfe, // mov r14, rdi
		0x49, 0x89, 0xf5, // mov r13, rsi
	)
	m.code = append(m.code,
		0x48, 0x89, 0xf0, // mov rax, rsi
		0x48, 0x83, 0xc0, 0x08, // add rax, 8
	)
	m.emitTrapOnOverflow()
	m.code = append(m.code,
		0x48, 0x89, 0xc6,
		0x48, 0x31, 0xff,
		0xba, 0x03, 0x00, 0x00, 0x00,
		0x41, 0xba, 0x22, 0x00, 0x00, 0x00,
		0x41, 0xb8, 0xff, 0xff, 0xff, 0xff,
		0x45, 0x31, 0xc9,
		0xb8, 0x09, 0x00, 0x00, 0x00,
		0x0f, 0x05,
		0x48, 0x85, 0xc0,
	)
	if err := m.emitConditionalJump(0x88, m.trapLabel); err != nil {
		return err
	}
	m.code = append(m.code,
		0x49, 0x89, 0xc4, // mov r12, rax
		0x4c, 0x89, 0x28, // mov [rax], r13
	)
	m.code = append(m.code,
		0x48, 0x8d, 0x78, 0x08,
		0x4c, 0x89, 0xf6,
		0x4c, 0x89, 0xe9,
		0xf3, 0xa4,
	)
	m.code = append(m.code,
		0x4c, 0x89, 0xe0,
		0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3,
	)
	return nil
}

func (m *directMachine) emitStringCharsRuntime() error {
	if err := m.bind(m.stringCharsLabel); err != nil {
		return err
	}
	// rdi points to a String object. Build an Array[String] of UTF-8 code-point
	// slices using the existing mmap-backed String and Array allocators.
	m.code = append(m.code,
		0x53, 0x55, 0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x41, 0x57,
		0x49, 0x89, 0xfc, // r12=source object
		0x4d, 0x8b, 0x2c, 0x24, // r13=source byte length
		0x4d, 0x31, 0xf6, // r14=byte index
		0x4d, 0x31, 0xff, // r15=code-point count
	)
	countLoop := m.newLabel()
	countOne := m.newLabel()
	countTwo := m.newLabel()
	countThree := m.newLabel()
	countAdvance := m.newLabel()
	allocate := m.newLabel()
	if err := m.bind(countLoop); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x39, 0xee)                     // cmp r14, r13
	if err := m.emitConditionalJump(0x83, allocate); err != nil { // jae: all source bytes consumed
		return err
	}
	m.code = append(m.code, 0x4b, 0x0f, 0xb6, 0x44, 0x34, 0x08, 0x3c, 0x80)
	if err := m.emitConditionalJump(0x82, countOne); err != nil { // jb: ASCII
		return err
	}
	m.code = append(m.code, 0x3c, 0xe0)
	if err := m.emitConditionalJump(0x82, countTwo); err != nil { // jb: two-byte sequence
		return err
	}
	m.code = append(m.code, 0x3c, 0xf0)
	if err := m.emitConditionalJump(0x82, countThree); err != nil { // jb: three-byte sequence
		return err
	}
	m.code = append(m.code, 0x49, 0x83, 0xc6, 0x04)
	if err := m.emitJump(countAdvance); err != nil {
		return err
	}
	if err := m.bind(countThree); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x83, 0xc6, 0x03)
	if err := m.emitJump(countAdvance); err != nil {
		return err
	}
	if err := m.bind(countTwo); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x83, 0xc6, 0x02)
	if err := m.emitJump(countAdvance); err != nil {
		return err
	}
	if err := m.bind(countOne); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x83, 0xc6, 0x01)
	if err := m.bind(countAdvance); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc7)
	if err := m.emitJump(countLoop); err != nil {
		return err
	}
	if err := m.bind(allocate); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xf8, 0x48, 0x89, 0xc7)
	if err := m.emitLabelCall(m.arrayAllocLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0xc3, 0x4d, 0x31, 0xf6, 0x4d, 0x31, 0xff)

	fillLoop := m.newLabel()
	fillOne := m.newLabel()
	fillTwo := m.newLabel()
	fillThree := m.newLabel()
	fillCopy := m.newLabel()
	done := m.newLabel()
	if err := m.bind(fillLoop); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x39, 0xee)
	if err := m.emitConditionalJump(0x83, done); err != nil {
		return err
	}
	m.code = append(m.code, 0x4b, 0x0f, 0xb6, 0x44, 0x34, 0x08, 0x3c, 0x80)
	if err := m.emitConditionalJump(0x82, fillOne); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, 0xe0)
	if err := m.emitConditionalJump(0x82, fillTwo); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, 0xf0)
	if err := m.emitConditionalJump(0x82, fillThree); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0xc7, 0xc2, 0x04, 0x00, 0x00, 0x00)
	if err := m.emitJump(fillCopy); err != nil {
		return err
	}
	if err := m.bind(fillThree); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0xc7, 0xc2, 0x03, 0x00, 0x00, 0x00)
	if err := m.emitJump(fillCopy); err != nil {
		return err
	}
	if err := m.bind(fillTwo); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0xc7, 0xc2, 0x02, 0x00, 0x00, 0x00)
	if err := m.emitJump(fillCopy); err != nil {
		return err
	}
	if err := m.bind(fillOne); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0xc7, 0xc2, 0x01, 0x00, 0x00, 0x00)
	if err := m.bind(fillCopy); err != nil {
		return err
	}
	m.code = append(m.code,
		0x4c, 0x89, 0xe7, // rdi=source byte base
		0x48, 0x83, 0xc7, 0x08,
		0x4c, 0x01, 0xf7, // add rdi, r14
		0x48, 0x89, 0xd6, // rsi=code-point byte length
		0x52, // preserve length across string allocation
	)
	if err := m.emitLabelCall(m.stringAllocLabel); err != nil {
		return err
	}
	m.code = append(m.code,
		0x5a,
		0x4a, 0x89, 0x44, 0xfb, 0x08, // output[r15]=new String
		0x49, 0x01, 0xd6, // r14 += code-point byte length
		0x49, 0xff, 0xc7,
	)
	if err := m.emitJump(fillLoop); err != nil {
		return err
	}
	if err := m.bind(done); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0xd8, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3)
	return nil
}

func (m *directMachine) emitStringEqualRuntime() error {
	if err := m.bind(m.stringEqualLabel); err != nil {
		return err
	}
	// rdi and rsi point to immutable {length, bytes} String objects. Return
	// one when the byte sequences are identical and zero otherwise.
	pointerEqual := m.newLabel()
	falseLabel := m.newLabel()
	loop := m.newLabel()
	done := m.newLabel()
	m.code = append(m.code,
		0x53, 0x55, 0x41, 0x54, 0x41, 0x55, 0x41, 0x56,
		0x49, 0x89, 0xfc, // r12=left
		0x49, 0x89, 0xf5, // r13=right
		0x4d, 0x39, 0xec,
	)
	if err := m.emitConditionalJump(0x84, pointerEqual); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x8b, 0x04, 0x24, 0x49, 0x3b, 0x45, 0x00)
	if err := m.emitConditionalJump(0x85, falseLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x31, 0xf6) // r14=byte index
	if err := m.bind(loop); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x3b, 0x34, 0x24)
	if err := m.emitConditionalJump(0x83, pointerEqual); err != nil {
		return err
	}
	m.code = append(m.code,
		0x4b, 0x0f, 0xb6, 0x44, 0x34, 0x08,
		0x4b, 0x0f, 0xb6, 0x54, 0x35, 0x08,
		0x48, 0x39, 0xd0,
	)
	if err := m.emitConditionalJump(0x85, falseLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc6)
	if err := m.emitJump(loop); err != nil {
		return err
	}
	if err := m.bind(pointerEqual); err != nil {
		return err
	}
	m.emitMoveImmediate(1)
	if err := m.emitJump(done); err != nil {
		return err
	}
	if err := m.bind(falseLabel); err != nil {
		return err
	}
	m.emitMoveImmediate(0)
	if err := m.bind(done); err != nil {
		return err
	}
	m.code = append(m.code, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3)
	return nil
}

func (m *directMachine) emitMapFindRuntime() error {
	if err := m.bind(m.mapFindLabel); err != nil {
		return err
	}
	// rdi=map, rsi=key, rdx=key kind (0=scalar, 1=String). Maps use the
	// same immutable qword storage as arrays, with alternating key/value
	// words and a word count in the first slot. Return the address of the
	// matching value word, or nil when the key is absent.
	stringKey := m.newLabel()
	scalarKey := m.newLabel()
	found := m.newLabel()
	notFound := m.newLabel()
	done := m.newLabel()
	loop := m.newLabel()
	m.code = append(m.code,
		0x53, 0x55, 0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x41, 0x57,
		0x49, 0x89, 0xfc, // r12=map
		0x49, 0x89, 0xf5, // r13=key
		0x49, 0x89, 0xd6, // r14=key kind
		0x4d, 0x31, 0xff, // r15=word index
	)
	if err := m.bind(loop); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x3b, 0x3c, 0x24)
	if err := m.emitConditionalJump(0x83, notFound); err != nil {
		return err
	}
	m.code = append(m.code,
		0x4c, 0x89, 0xfb, // rbx=r15
		0x48, 0xc1, 0xe3, 0x03,
		0x4c, 0x01, 0xe3, // rbx += r12
		0x48, 0x83, 0xc3, 0x08,
		0x48, 0x8b, 0x03, // rax=stored key
		0x49, 0x83, 0xfe, 0x01,
	)
	if err := m.emitConditionalJump(0x84, stringKey); err != nil {
		return err
	}
	if err := m.emitJump(scalarKey); err != nil {
		return err
	}
	if err := m.bind(stringKey); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0xc7, 0x4c, 0x89, 0xee)
	if err := m.emitLabelCall(m.stringEqualLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x85, 0xc0)
	if err := m.emitConditionalJump(0x85, found); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x83, 0xc7, 0x02)
	if err := m.emitJump(loop); err != nil {
		return err
	}
	if err := m.bind(scalarKey); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x39, 0xe8)
	if err := m.emitConditionalJump(0x84, found); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x83, 0xc7, 0x02)
	if err := m.emitJump(loop); err != nil {
		return err
	}
	if err := m.bind(found); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x8d, 0x43, 0x08)
	if err := m.emitJump(done); err != nil {
		return err
	}
	if err := m.bind(notFound); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x31, 0xc0)
	if err := m.bind(done); err != nil {
		return err
	}
	m.code = append(m.code, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3)
	return nil
}

func (m *directMachine) emitMapInsertRuntime() error {
	if err := m.bind(m.mapInsertLabel); err != nil {
		return err
	}
	// rdi=map, rsi=key, rdx=key kind, rcx=value. Copy the immutable map and
	// replace the existing value or append a new key/value pair.
	notFound := m.newLabel()
	found := m.newLabel()
	done := m.newLabel()
	m.code = append(m.code,
		0x53, 0x55, 0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x41, 0x57,
		0x49, 0x89, 0xfc, // r12=map
		0x49, 0x89, 0xf5, // r13=key
		0x49, 0x89, 0xce, // r14=value
		0x49, 0x89, 0xd7, // r15=key kind
		0x4c, 0x89, 0xe7, 0x4c, 0x89, 0xee, 0x4c, 0x89, 0xfa,
	)
	if err := m.emitLabelCall(m.mapFindLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x85, 0xc0)
	if err := m.emitConditionalJump(0x84, notFound); err != nil {
		return err
	}
	if err := m.bind(found); err != nil {
		return err
	}
	m.code = append(m.code,
		0x50,
		0x49, 0x8b, 0x3c, 0x24,
	)
	if err := m.emitLabelCall(m.arrayAllocLabel); err != nil {
		return err
	}
	m.code = append(m.code,
		0x48, 0x89, 0xc5,
		0x5b,
		0x49, 0x8b, 0x0c, 0x24,
		0x49, 0x8d, 0x74, 0x24, 0x08,
		0x48, 0x8d, 0x7d, 0x08,
		0xf3, 0x48, 0xa5,
		0x48, 0x89, 0xd8,
		0x4c, 0x29, 0xe0,
		0x48, 0x01, 0xe8,
		0x4c, 0x89, 0x30,
	)
	if err := m.emitJump(done); err != nil {
		return err
	}
	if err := m.bind(notFound); err != nil {
		return err
	}
	m.code = append(m.code,
		0x49, 0x8b, 0x1c, 0x24,
		0x48, 0x89, 0xdf,
		0x48, 0x83, 0xc7, 0x02,
	)
	m.emitTrapOnOverflow()
	if err := m.emitLabelCall(m.arrayAllocLabel); err != nil {
		return err
	}
	m.code = append(m.code,
		0x48, 0x89, 0xc5,
		0x49, 0x8b, 0x0c, 0x24,
		0x49, 0x8d, 0x74, 0x24, 0x08,
		0x48, 0x8d, 0x7d, 0x08,
		0xf3, 0x48, 0xa5,
		0x48, 0x89, 0xda,
		0x48, 0xc1, 0xe2, 0x03,
		0x48, 0x01, 0xea,
		0x48, 0x83, 0xc2, 0x08,
		0x4c, 0x89, 0x2a,
		0x4c, 0x89, 0x72, 0x08,
	)
	if err := m.bind(done); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0xe8, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3)
	return nil
}

func (m *directMachine) emitMapRemoveRuntime() error {
	if err := m.bind(m.mapRemoveLabel); err != nil {
		return err
	}
	// rdi=map, rsi=key, rdx=key kind. Map storage is an immutable array of
	// alternating key/value qwords; remove the matching pair and preserve the
	// original map when the key is absent.
	notFound := m.newLabel()
	found := m.newLabel()
	copyLoop := m.newLabel()
	copyDone := m.newLabel()
	skipPair := m.newLabel()
	copyPair := m.newLabel()
	done := m.newLabel()

	m.code = append(m.code,
		0x53, 0x55, 0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x41, 0x57,
		0x49, 0x89, 0xfc, // r12=map
		0x49, 0x89, 0xf5, // r13=key
		0x49, 0x89, 0xd6, // r14=key kind
		0x4c, 0x89, 0xe7, 0x4c, 0x89, 0xee, 0x4c, 0x89, 0xf2,
	)
	m.mapRuntimeUsed = true
	if err := m.emitLabelCall(m.mapFindLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x85, 0xc0)
	if err := m.emitConditionalJump(0x84, notFound); err != nil {
		return err
	}
	if err := m.bind(found); err != nil {
		return err
	}
	// r15 points at the key qword that precedes map_find's value pointer.
	// Keep it in a callee-saved register across arrayAlloc; rdi is reused for
	// the allocation size and is clobbered by mmap.
	m.code = append(m.code, 0x49, 0x89, 0xc7, 0x49, 0x83, 0xef, 0x08)
	m.code = append(m.code, 0x49, 0x8b, 0x2c, 0x24, 0x48, 0x83, 0xed, 0x02, 0x48, 0x89, 0xef)
	m.arrayRuntimeUsed = true
	if err := m.emitLabelCall(m.arrayAllocLabel); err != nil {
		return err
	}
	m.code = append(m.code,
		0x49, 0x89, 0xc6, // r14=new map
		0x49, 0x8d, 0x5c, 0x24, 0x08, // rbx=source key pointer
		0x4d, 0x8d, 0x46, 0x08, // r8=destination key pointer
		0x48, 0xd1, 0xed, // rbp=(original word count - removed pair) / 2
	)
	if err := m.emitJump(copyLoop); err != nil {
		return err
	}
	if err := m.bind(copyLoop); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x85, 0xed)
	if err := m.emitConditionalJump(0x84, copyDone); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x39, 0xfb)
	if err := m.emitConditionalJump(0x84, skipPair); err != nil {
		return err
	}
	if err := m.emitJump(copyPair); err != nil {
		return err
	}
	if err := m.bind(skipPair); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x83, 0xc3, 0x10, 0x48, 0xff, 0xcd)
	if err := m.emitJump(copyLoop); err != nil {
		return err
	}
	if err := m.bind(copyPair); err != nil {
		return err
	}
	m.code = append(m.code,
		0x48, 0x8b, 0x03, 0x49, 0x89, 0x00,
		0x48, 0x8b, 0x43, 0x08, 0x49, 0x89, 0x40, 0x08,
		0x48, 0x83, 0xc3, 0x10, 0x49, 0x83, 0xc0, 0x10,
		0x48, 0xff, 0xcd,
	)
	if err := m.emitJump(copyLoop); err != nil {
		return err
	}
	if err := m.bind(copyDone); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xf0)
	if err := m.emitBoxCall(0); err != nil {
		return err
	}
	if err := m.emitJump(done); err != nil {
		return err
	}
	if err := m.bind(notFound); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xe0)
	if err := m.emitBoxCall(0); err != nil {
		return err
	}
	if err := m.emitJump(done); err != nil {
		return err
	}
	if err := m.bind(done); err != nil {
		return err
	}
	m.code = append(m.code, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3)
	return nil
}

func (m *directMachine) emitSubstringRuntime() error {
	if err := m.bind(m.substringLabel); err != nil {
		return err
	}
	// rdi=String, rsi=start code-point index, rdx=code-point length. Return
	// Result[String,String] as the same boxed {tag,payload} value used by the
	// interpreter and C runtime: tag 0 is ok and tag 1 is err.
	negativeError := m.newLabel()
	startError := m.newLabel()
	endError := m.newLabel()
	startFound := m.newLabel()
	endFound := m.newLabel()
	done := m.newLabel()
	startLoop := m.newLabel()
	startOne := m.newLabel()
	startTwo := m.newLabel()
	startThree := m.newLabel()
	startAdvance := m.newLabel()
	endLoop := m.newLabel()
	endOne := m.newLabel()
	endTwo := m.newLabel()
	endThree := m.newLabel()
	endAdvance := m.newLabel()
	m.code = append(m.code,
		0x53, 0x55, 0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x41, 0x57,
		0x49, 0x89, 0xfc, // r12=source String
		0x49, 0x89, 0xf5, // r13=start code-point index
		0x49, 0x89, 0xd6, // r14=remaining code-point length
		0x4d, 0x8b, 0x3c, 0x24, // r15=source byte length
		0x48, 0x31, 0xdb, // rbx=byte index
		0x48, 0x31, 0xed, // rbp=code-point index
		0x4d, 0x85, 0xed, // validate start >= 0
	)
	if err := m.emitConditionalJump(0x88, negativeError); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x85, 0xf6)
	if err := m.emitConditionalJump(0x88, negativeError); err != nil {
		return err
	}
	if err := m.bind(startLoop); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x39, 0xed)
	if err := m.emitConditionalJump(0x84, startFound); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x39, 0xfb)
	if err := m.emitConditionalJump(0x83, startError); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x0f, 0xb6, 0x44, 0x1c, 0x08, 0x3c, 0x80)
	if err := m.emitConditionalJump(0x82, startOne); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, 0xe0)
	if err := m.emitConditionalJump(0x82, startTwo); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, 0xf0)
	if err := m.emitConditionalJump(0x82, startThree); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0xc7, 0xc1, 0x04, 0x00, 0x00, 0x00)
	if err := m.emitJump(startAdvance); err != nil {
		return err
	}
	if err := m.bind(startThree); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0xc7, 0xc1, 0x03, 0x00, 0x00, 0x00)
	if err := m.emitJump(startAdvance); err != nil {
		return err
	}
	if err := m.bind(startTwo); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0xc7, 0xc1, 0x02, 0x00, 0x00, 0x00)
	if err := m.emitJump(startAdvance); err != nil {
		return err
	}
	if err := m.bind(startOne); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0xc7, 0xc1, 0x01, 0x00, 0x00, 0x00)
	if err := m.bind(startAdvance); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0xd8, 0x48, 0x01, 0xc8)
	if err := m.emitConditionalJump(0x80, startError); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x39, 0xf8)
	if err := m.emitConditionalJump(0x87, startError); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0xc3, 0x48, 0xff, 0xc5)
	if err := m.emitJump(startLoop); err != nil {
		return err
	}
	if err := m.bind(startFound); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x89, 0xd8, 0x4d, 0x85, 0xf6)
	if err := m.emitConditionalJump(0x84, endFound); err != nil {
		return err
	}
	if err := m.bind(endLoop); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x85, 0xf6)
	if err := m.emitConditionalJump(0x84, endFound); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x39, 0xfb)
	if err := m.emitConditionalJump(0x83, endError); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x0f, 0xb6, 0x44, 0x1c, 0x08, 0x3c, 0x80)
	if err := m.emitConditionalJump(0x82, endOne); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, 0xe0)
	if err := m.emitConditionalJump(0x82, endTwo); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, 0xf0)
	if err := m.emitConditionalJump(0x82, endThree); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0xc7, 0xc1, 0x04, 0x00, 0x00, 0x00)
	if err := m.emitJump(endAdvance); err != nil {
		return err
	}
	if err := m.bind(endThree); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0xc7, 0xc1, 0x03, 0x00, 0x00, 0x00)
	if err := m.emitJump(endAdvance); err != nil {
		return err
	}
	if err := m.bind(endTwo); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0xc7, 0xc1, 0x02, 0x00, 0x00, 0x00)
	if err := m.emitJump(endAdvance); err != nil {
		return err
	}
	if err := m.bind(endOne); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0xc7, 0xc1, 0x01, 0x00, 0x00, 0x00)
	if err := m.bind(endAdvance); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0xd8, 0x48, 0x01, 0xc8)
	if err := m.emitConditionalJump(0x80, endError); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x39, 0xf8)
	if err := m.emitConditionalJump(0x87, endError); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0xc3, 0x49, 0xff, 0xce)
	if err := m.emitJump(endLoop); err != nil {
		return err
	}
	if err := m.bind(endFound); err != nil {
		return err
	}
	m.code = append(m.code,
		0x49, 0x89, 0xd9, // r9=end byte index
		0x4c, 0x89, 0xe7,
		0x48, 0x83, 0xc7, 0x08,
		0x4c, 0x01, 0xc7,
		0x4c, 0x89, 0xce,
		0x4c, 0x29, 0xc6,
	)
	if err := m.emitLabelCall(m.stringAllocLabel); err != nil {
		return err
	}
	if err := m.emitBoxCall(0); err != nil {
		return err
	}
	if err := m.emitJump(done); err != nil {
		return err
	}
	if err := m.bind(negativeError); err != nil {
		return err
	}
	m.emitStringAddress("substring range is out of bounds")
	if err := m.emitBoxCall(1); err != nil {
		return err
	}
	if err := m.emitJump(done); err != nil {
		return err
	}
	if err := m.bind(startError); err != nil {
		return err
	}
	m.emitStringAddress("substring range is out of bounds")
	if err := m.emitBoxCall(1); err != nil {
		return err
	}
	if err := m.emitJump(done); err != nil {
		return err
	}
	if err := m.bind(endError); err != nil {
		return err
	}
	m.emitStringAddress("substring range is out of bounds")
	if err := m.emitBoxCall(1); err != nil {
		return err
	}
	if err := m.bind(done); err != nil {
		return err
	}
	m.code = append(m.code, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3)
	return nil
}

func (m *directMachine) emitIntToStringRuntime() error {
	if err := m.bind(m.intToStringLabel); err != nil {
		return err
	}
	// rdi=value, rsi=kind (0=signed Int, 1=UInt, 2=Bool). The numeric
	// conversion writes backwards into a private stack buffer and then uses
	// the checked mmap-backed String allocator. String arguments do not enter
	// this runtime: the lowering returns their immutable pointer unchanged.
	boolFalse := m.newLabel()
	numeric := m.newLabel()
	signed := m.newLabel()
	digits := m.newLabel()
	digitLoop := m.newLabel()
	addSign := m.newLabel()
	ready := m.newLabel()
	restore := m.newLabel()
	m.code = append(m.code,
		0x53, 0x55, 0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x41, 0x57,
		0x49, 0x89, 0xfc, // r12=value
		0x49, 0x89, 0xf5, // r13=display kind
		0x49, 0x83, 0xfd, 0x02, // cmp r13, 2
	)
	if err := m.emitConditionalJump(0x85, numeric); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x85, 0xe4) // test r12, r12
	if err := m.emitConditionalJump(0x84, boolFalse); err != nil {
		return err
	}
	m.emitStringAddress("true")
	if err := m.emitJump(restore); err != nil {
		return err
	}
	if err := m.bind(boolFalse); err != nil {
		return err
	}
	m.emitStringAddress("false")
	if err := m.emitJump(restore); err != nil {
		return err
	}
	if err := m.bind(numeric); err != nil {
		return err
	}
	m.code = append(m.code,
		0x48, 0x83, 0xec, 0x20, // reserve 32-byte digit buffer
		0x4c, 0x8d, 0x74, 0x24, 0x20, // r14=&buffer[32] (one past the buffer)
		0x45, 0x31, 0xff, // r15=negative flag
		0x4d, 0x85, 0xed, // signed kind?
	)
	if err := m.emitConditionalJump(0x84, signed); err != nil {
		return err
	}
	if err := m.emitJump(digits); err != nil {
		return err
	}
	if err := m.bind(signed); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x85, 0xe4)                   // test value
	if err := m.emitConditionalJump(0x89, digits); err != nil { // jns
		return err
	}
	m.code = append(m.code,
		0x41, 0xb7, 0x01, // r15b=1
		0x49, 0xf7, 0xdc, // neg r12; MinInt remains its unsigned magnitude
	)
	if err := m.bind(digits); err != nil {
		return err
	}
	if err := m.bind(digitLoop); err != nil {
		return err
	}
	m.code = append(m.code,
		0x48, 0x31, 0xd2, // zero rdx for div
		0x4c, 0x89, 0xe0, // rax=r12
		0x48, 0xc7, 0xc1, 0x0a, 0x00, 0x00, 0x00, // rcx=10
		0x48, 0xf7, 0xf1, // div rcx
		0x80, 0xc2, 0x30, // dl += '0'
		0x49, 0xff, 0xce, // --r14
		0x41, 0x88, 0x16, // [r14]=dl
		0x49, 0x89, 0xc4, // r12=quotient
		0x4d, 0x85, 0xe4, // test r12
	)
	if err := m.emitConditionalJump(0x85, digitLoop); err != nil { // jnz
		return err
	}
	if err := m.emitJump(addSign); err != nil {
		return err
	}
	if err := m.bind(addSign); err != nil {
		return err
	}
	m.code = append(m.code, 0x45, 0x84, 0xff) // test r15b
	if err := m.emitConditionalJump(0x84, ready); err != nil {
		return err
	}
	m.code = append(m.code,
		0x49, 0xff, 0xce, // --r14
		0x41, 0xc6, 0x06, 0x2d, // [r14]='-'
	)
	if err := m.bind(ready); err != nil {
		return err
	}
	m.code = append(m.code,
		0x48, 0x8d, 0x74, 0x24, 0x20, // rsi=&buffer[32]
		0x4c, 0x29, 0xf6, // rsi-=r14
		0x4c, 0x89, 0xf7, // rdi=r14
	)
	if err := m.emitLabelCall(m.stringAllocLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x83, 0xc4, 0x20) // release digit buffer
	if err := m.bind(restore); err != nil {
		return err
	}
	m.code = append(m.code, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3)
	return nil
}

func (m *directMachine) emitIntFromStringRuntime() error {
	if err := m.bind(m.intFromStringLabel); err != nil {
		return err
	}
	// rdi points to a String object. Parse an optional sign followed by a
	// complete ASCII decimal sequence. Accumulation stays negative so the
	// exact Int minimum remains representable; malformed input and overflow
	// branch to the normal checked trap path.
	digits := m.newLabel()
	negative := m.newLabel()
	positive := m.newLabel()
	done := m.newLabel()
	returnValue := m.newLabel()
	m.code = append(m.code,
		0x53, 0x55, 0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x41, 0x57,
		0x49, 0x89, 0xfc, // r12=String
		0x4d, 0x8b, 0x2c, 0x24, // r13=byte length
		0x4d, 0x31, 0xf6, // r14=byte index
		0x4d, 0x31, 0xff, // r15=negative flag
		0x48, 0x31, 0xed, // rbp=minimum digit index (0 or 1 after a sign)
		0x48, 0x31, 0xdb, // rbx=negative accumulator
		0x4d, 0x85, 0xed, // reject empty String
	)
	if err := m.emitConditionalJump(0x84, m.trapLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x4b, 0x0f, 0xb6, 0x44, 0x34, 0x08) // al=first byte
	m.code = append(m.code, 0x3c, 0x2d)                         // '-'
	if err := m.emitConditionalJump(0x84, negative); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, 0x2b) // '+'
	if err := m.emitConditionalJump(0x84, positive); err != nil {
		return err
	}
	if err := m.emitJump(digits); err != nil {
		return err
	}
	if err := m.bind(negative); err != nil {
		return err
	}
	m.code = append(m.code, 0x41, 0xb7, 0x01, 0xbd, 0x01, 0x00, 0x00, 0x00, 0x49, 0xff, 0xc6) // sign=negative; minimum index=1
	if err := m.emitJump(digits); err != nil {
		return err
	}
	if err := m.bind(positive); err != nil {
		return err
	}
	m.code = append(m.code, 0xbd, 0x01, 0x00, 0x00, 0x00, 0x49, 0xff, 0xc6) // minimum index=1; consume '+'
	if err := m.emitJump(digits); err != nil {
		return err
	}
	if err := m.bind(digits); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x39, 0xee)                 // index versus length
	if err := m.emitConditionalJump(0x84, done); err != nil { // je: all bytes consumed
		return err
	}
	m.code = append(m.code, 0x4b, 0x0f, 0xb6, 0x44, 0x34, 0x08)
	m.code = append(m.code, 0x3c, 0x30)
	if err := m.emitConditionalJump(0x82, m.trapLabel); err != nil { // jb
		return err
	}
	m.code = append(m.code, 0x3c, 0x39)
	if err := m.emitConditionalJump(0x87, m.trapLabel); err != nil { // ja
		return err
	}
	m.code = append(m.code,
		0x83, 0xe8, 0x30, // eax -= '0'
		0x48, 0x6b, 0xdb, 0x0a, // rbx *= 10
	)
	m.emitTrapOnOverflow()
	m.code = append(m.code, 0x48, 0x29, 0xc3) // rbx -= digit
	m.emitTrapOnOverflow()
	m.code = append(m.code, 0x49, 0xff, 0xc6) // index++
	if err := m.emitJump(digits); err != nil {
		return err
	}
	if err := m.bind(done); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xf0, 0x48, 0x39, 0xe8)      // cmp index, minimum digit index
	if err := m.emitConditionalJump(0x84, m.trapLabel); err != nil { // reject sign-only strings
		return err
	}
	m.code = append(m.code, 0x45, 0x84, 0xff) // negative input?
	if err := m.emitConditionalJump(0x85, returnValue); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0xf7, 0xdb) // positive result = -negative accumulator
	m.emitTrapOnOverflow()
	if err := m.bind(returnValue); err != nil {
		return err
	}
	m.code = append(m.code,
		0x48, 0x89, 0xd8, // rax=parsed Int
		0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3,
	)
	return nil
}

func (m *directMachine) emitArraySetRuntime() error {
	if err := m.bind(m.arraySetLabel); err != nil {
		return err
	}
	// rdi=array, rsi=index, rdx=replacement. Clone the immutable qword array
	// and return a boxed Result with one changed element.
	failure := m.newLabel()
	m.code = append(m.code,
		0x53, 0x55, 0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x41, 0x57,
		0x49, 0x89, 0xfc, // r12=source array
		0x49, 0x89, 0xf5, // r13=index
		0x49, 0x89, 0xd6, // r14=replacement
		0x49, 0x8b, 0x2c, 0x24, // rbp=length
		0x4d, 0x85, 0xed, // reject negative index
	)
	if err := m.emitConditionalJump(0x88, failure); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x39, 0xed) // cmp index, length
	if err := m.emitConditionalJump(0x83, failure); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0xef) // rdi=length
	m.arrayRuntimeUsed = true
	if err := m.emitLabelCall(m.arrayAllocLabel); err != nil {
		return err
	}
	arrayValue := m.newLabel()
	copyDone := m.newLabel()
	if err := m.bind(arrayValue); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x89, 0xc7) // r15=new array
	m.code = append(m.code,
		0x4c, 0x89, 0xe6, // rsi=source
		0x4c, 0x89, 0xff, // rdi=destination
		0x48, 0x89, 0xe9, 0x48, 0xff, 0xc1, // rcx=length+1
		0xf3, 0x48, 0xa5, // copy header and qword elements
		0x4c, 0x89, 0xe8, // rax=index
		0x48, 0xc1, 0xe0, 0x03,
		0x4c, 0x01, 0xf8, // rax+=new array
		0x48, 0x83, 0xc0, 0x08,
		0x4c, 0x89, 0x30, // store replacement
	)
	if err := m.emitJump(copyDone); err != nil {
		return err
	}
	if err := m.bind(copyDone); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xf8)
	if err := m.emitBoxCall(0); err != nil {
		return err
	}
	m.code = append(m.code, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3)
	if err := m.bind(failure); err != nil {
		return err
	}
	m.emitStringAddress("array index out of range")
	if err := m.emitBoxCall(1); err != nil {
		return err
	}
	m.code = append(m.code, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3)
	return nil
}

func (m *directMachine) emitArraySliceRuntime() error {
	if err := m.bind(m.arraySliceLabel); err != nil {
		return err
	}
	// rdi=array, rsi=start, rdx=length. Validate the half-open range, copy
	// the selected qwords into a fresh immutable array, and box the Result.
	failure := m.newLabel()
	copyDone := m.newLabel()
	m.code = append(m.code,
		0x53, 0x55, 0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x41, 0x57,
		0x49, 0x89, 0xfc, // r12=source array
		0x49, 0x89, 0xf5, // r13=start
		0x49, 0x89, 0xd6, // r14=length
		0x49, 0x8b, 0x2c, 0x24, // rbp=array length
		0x4d, 0x85, 0xed, // start >= 0
	)
	if err := m.emitConditionalJump(0x88, failure); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x85, 0xf6) // length >= 0
	if err := m.emitConditionalJump(0x88, failure); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x39, 0xed)
	if err := m.emitConditionalJump(0x87, failure); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0xe8, 0x4c, 0x29, 0xe8, 0x49, 0x39, 0xc6)
	if err := m.emitConditionalJump(0x87, failure); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xf7)
	m.arrayRuntimeUsed = true
	if err := m.emitLabelCall(m.arrayAllocLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x89, 0xc7)
	m.code = append(m.code,
		0x4c, 0x89, 0xe6, // rsi=source
		0x4c, 0x89, 0xe8, // rax=start
		0x48, 0xc1, 0xe0, 0x03,
		0x48, 0x01, 0xc6, // rsi+=start*8
		0x48, 0x83, 0xc6, 0x08,
		0x4c, 0x89, 0xff, // rdi=destination
		0x48, 0x83, 0xc7, 0x08,
		0x4c, 0x89, 0xf1, // rcx=length
		0xf3, 0x48, 0xa5,
	)
	if err := m.emitJump(copyDone); err != nil {
		return err
	}
	if err := m.bind(copyDone); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xf8, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3)
	if err := m.bind(failure); err != nil {
		return err
	}
	if err := m.emitJump(m.trapLabel); err != nil {
		return err
	}
	return nil
}

func (m *directMachine) emitArrayReverseRuntime() error {
	if err := m.bind(m.arrayReverseLabel); err != nil {
		return err
	}
	done, loop := m.newLabel(), m.newLabel()
	m.code = append(m.code,
		0x53, 0x55, 0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x41, 0x57,
		0x49, 0x89, 0xfc,
		0x49, 0x8b, 0x2c, 0x24,
		0x48, 0x89, 0xef,
	)
	m.arrayRuntimeUsed = true
	if err := m.emitLabelCall(m.arrayAllocLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x89, 0xc5, 0x45, 0x31, 0xf6)
	if err := m.bind(loop); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xf0, 0x48, 0x39, 0xe8)
	if err := m.emitConditionalJump(0x8d, done); err != nil {
		return err
	}
	m.code = append(m.code,
		0x48, 0x89, 0xe8,
		0x48, 0xff, 0xc8,
		0x4c, 0x29, 0xf0,
		0x48, 0xc1, 0xe0, 0x03,
		0x49, 0x8b, 0x54, 0x04, 0x08,
		0x4c, 0x89, 0xf0,
		0x48, 0xc1, 0xe0, 0x03,
		0x49, 0x89, 0x54, 0x05, 0x08,
		0x49, 0xff, 0xc6,
	)
	if err := m.emitJump(loop); err != nil {
		return err
	}
	if err := m.bind(done); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xe8, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3)
	return nil
}

func (m *directMachine) emitBytesFromArrayRuntime() error {
	if err := m.bind(m.bytesFromArrayLabel); err != nil {
		return err
	}
	// rdi=Array[Int]. Return a {length, bytes[]} Bytes object after
	// validating every element as an unsigned octet.
	m.code = append(m.code,
		0x53, 0x55, 0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x41, 0x57,
		0x49, 0x89, 0xfc, // mov r12, rdi (source array)
		0x49, 0x8b, 0x2c, 0x24, // mov rbp, [r12] (length)
		0x49, 0x89, 0xed, // mov r13, rbp
		0x48, 0x89, 0xe8, // mov rax, rbp
		0x48, 0x83, 0xc0, 0x08, // add rax, 8
	)
	m.emitTrapOnOverflow()
	m.code = append(m.code,
		0x48, 0x89, 0xc6, // mov rsi, rax
		0x48, 0x31, 0xff, // addr=0
		0xba, 0x03, 0x00, 0x00, 0x00,
		0x41, 0xba, 0x22, 0x00, 0x00, 0x00,
		0x41, 0xb8, 0xff, 0xff, 0xff, 0xff,
		0x45, 0x31, 0xc9,
		0xb8, 0x09, 0x00, 0x00, 0x00,
		0x0f, 0x05,
		0x48, 0x85, 0xc0,
	)
	if err := m.emitConditionalJump(0x88, m.trapLabel); err != nil {
		return err
	}
	m.code = append(m.code,
		0x49, 0x89, 0xc6, // mov r14, rax (destination Bytes)
	)
	m.code = append(m.code,
		0x4c, 0x89, 0x28, // [r14]=length
		0x45, 0x31, 0xff, // index=0
	)
	loop := m.newLabel()
	done := m.newLabel()
	if err := m.bind(loop); err != nil {
		return err
	}
	m.code = append(m.code,
		0x4c, 0x89, 0xf8, // mov rax, r15
		0x4c, 0x39, 0xe8, // cmp rax, rbp
	)
	if err := m.emitConditionalJump(0x83, done); err != nil {
		return err
	}
	m.code = append(m.code,
		0x4c, 0x89, 0xff, // mov rdi, r15
		0x48, 0xc1, 0xe7, 0x03,
		0x4c, 0x01, 0xe7,
		0x48, 0x83, 0xc7, 0x08,
		0x48, 0x8b, 0x07, // load Array[Int] element
		0x48, 0x85, 0xc0,
	)
	if err := m.emitConditionalJump(0x88, m.trapLabel); err != nil {
		return err
	}
	m.code = append(m.code,
		0x48, 0x3d, 0xff, 0x00, 0x00, 0x00,
	)
	if err := m.emitConditionalJump(0x87, m.trapLabel); err != nil {
		return err
	}
	m.code = append(m.code,
		0x49, 0x8d, 0x7e, 0x08,
		0x4c, 0x01, 0xff,
		0x88, 0x07,
		0x49, 0xff, 0xc7,
	)
	if err := m.emitJump(loop); err != nil {
		return err
	}
	if err := m.bind(done); err != nil {
		return err
	}
	m.code = append(m.code,
		0x4c, 0x89, 0xf0,
		0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3,
	)
	return nil
}

func (m *directMachine) emitU8ArrayRuntime() error {
	if err := m.bind(m.u8ArrayLabel); err != nil {
		return err
	}
	// rdi=Bytes. Convert the immutable {length, bytes[]} object into a fresh
	// Array[UInt8] whose elements use the normal qword array ABI.
	m.code = append(m.code,
		0x53, 0x55, 0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x41, 0x57,
		0x49, 0x89, 0xfc, // r12=Bytes
		0x49, 0x8b, 0x2c, 0x24, // rbp=length
		0x48, 0x89, 0xef, // rdi=element count
	)
	m.arrayRuntimeUsed = true
	if err := m.emitLabelCall(m.arrayAllocLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x89, 0xc6, 0x45, 0x31, 0xff) // r14=array, r15=index
	loop := m.newLabel()
	done := m.newLabel()
	if err := m.bind(loop); err != nil {
		return err
	}
	// The element count lives in RBP. R13 is callee-saved but is not
	// initialized by this runtime, so comparing against it could skip the copy
	// loop and leave every UInt8 at the allocator's zero value.
	m.code = append(m.code, 0x4c, 0x89, 0xf8, 0x48, 0x39, 0xe8)
	if err := m.emitConditionalJump(0x83, done); err != nil {
		return err
	}
	m.code = append(m.code,
		0x4c, 0x89, 0xfa, // rdx=index
		0x48, 0xc1, 0xe2, 0x03,
		0x4c, 0x01, 0xf2, // rdx+=array
		0x48, 0x83, 0xc2, 0x08,
		0x4b, 0x0f, 0xb6, 0x44, 0x3c, 0x08, // rax=Bytes byte
		0x48, 0x89, 0x02, // store qword element
		0x49, 0xff, 0xc7,
	)
	if err := m.emitJump(loop); err != nil {
		return err
	}
	if err := m.bind(done); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xf0, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3)
	return nil
}

func (m *directMachine) emitProcessArgsRuntime() error {
	if err := m.bind(m.processArgsLabel); err != nil {
		return err
	}
	m.arrayRuntimeUsed = true
	m.code = append(m.code,
		0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x41, 0x57,
		0x4c, 0x8b, 0x65, 0x08, // mov r12, [rbp+8] (argc)
		0x49, 0xff, 0xcc, // exclude argv[0]
		0x4c, 0x89, 0xe7, // mov rdi, r12 (array allocator count)
		0x4c, 0x89, 0xe0, // mov rax, r12
	)
	if err := m.emitLabelCall(m.arrayAllocLabel); err != nil {
		return err
	}
	m.code = append(m.code,
		0x49, 0x89, 0xc5,
		0x45, 0x31, 0xf6,
		0x4c, 0x8d, 0x7d, 0x18,
	)
	loop := m.newLabel()
	done := m.newLabel()
	if err := m.bind(loop); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xf0) // mov rax, r14
	m.code = append(m.code, 0x4c, 0x39, 0xe0) // cmp rax, r12
	if err := m.emitConditionalJump(0x83, done); err != nil {
		return err
	}
	m.code = append(m.code,
		0x4c, 0x89, 0xf7,
		0x48, 0xc1, 0xe7, 0x03,
		0x4c, 0x01, 0xff,
		0x4c, 0x8b, 0x07, // mov r8, [rdi]
		0x45, 0x31, 0xc9,
	)
	lengthLoop := m.newLabel()
	lengthDone := m.newLabel()
	if err := m.bind(lengthLoop); err != nil {
		return err
	}
	m.code = append(m.code, 0x43, 0x80, 0x3c, 0x08, 0x00)
	if err := m.emitConditionalJump(0x84, lengthDone); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc1)
	if err := m.emitJump(lengthLoop); err != nil {
		return err
	}
	if err := m.bind(lengthDone); err != nil {
		return err
	}
	m.code = append(m.code,
		0x4c, 0x89, 0xc7,
		0x4c, 0x89, 0xce,
	)
	if err := m.emitLabelCall(m.stringAllocLabel); err != nil {
		return err
	}
	m.code = append(m.code,
		0x4c, 0x89, 0xea,
		0x4c, 0x89, 0xf7,
		0x48, 0xc1, 0xe7, 0x03,
		0x4c, 0x01, 0xef,
		0x48, 0x83, 0xc7, 0x08,
		0x48, 0x89, 0x07,
		0x49, 0xff, 0xc6,
	)
	if err := m.emitJump(loop); err != nil {
		return err
	}
	if err := m.bind(done); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xe8) // mov rax, r13 (result array)
	m.code = append(m.code, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0xc3)
	return nil
}

func (m *directMachine) emitFSReadTextRuntime() error {
	if err := m.bind(m.fsReadTextLabel); err != nil {
		return err
	}
	// rdi=String path. The syscall path is a temporary NUL-terminated copy;
	// the returned Result owns a freshly allocated String object.
	m.code = append(m.code,
		0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x41, 0x57, 0x53, 0x55,
		0x49, 0x89, 0xfc,
		0x49, 0x8d, 0x7c, 0x24, 0x08,
		0x49, 0x8b, 0x34, 0x24,
	)
	if err := m.emitLabelCall(m.stringAllocLabel); err != nil {
		return err
	}
	m.code = append(m.code,
		0x49, 0x89, 0xc5,
		0x49, 0x8b, 0x4d, 0x00,
		0x4c, 0x01, 0xe9,
		0xc6, 0x41, 0x08, 0x00,
		0x48, 0x81, 0xec, 0x90, 0x00, 0x00, 0x00,
		0x49, 0x8d, 0x7d, 0x08,
		0xbe, 0x00, 0x00, 0x02, 0x00, // mov esi, O_NOFOLLOW: reject a symlink at the final path component
		0x48, 0x31, 0xd2,
		0xb8, 0x02, 0x00, 0x00, 0x00,
		0x0f, 0x05,
		0x48, 0x85, 0xc0,
	)
	openFail := m.newLabel()
	closeFail := m.newLabel()
	errorLabel := m.newLabel()
	if err := m.emitConditionalJump(0x88, openFail); err != nil {
		return err
	}
	m.code = append(m.code,
		0x49, 0x89, 0xc6,
		0x4c, 0x89, 0xf7,
		0x48, 0x89, 0xe6,
		0xb8, 0x05, 0x00, 0x00, 0x00,
		0x0f, 0x05,
		0x48, 0x85, 0xc0,
	)
	if err := m.emitConditionalJump(0x88, closeFail); err != nil {
		return err
	}
	m.code = append(m.code,
		0x4c, 0x8b, 0x7c, 0x24, 0x30,
		0x4d, 0x85, 0xff,
	)
	nonZero := m.newLabel()
	allocSize := m.newLabel()
	if err := m.emitConditionalJump(0x85, nonZero); err != nil {
		return err
	}
	m.emitMoveImmediate(1)
	if err := m.emitJump(allocSize); err != nil {
		return err
	}
	if err := m.bind(nonZero); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xf8) // mov rax, r15
	if err := m.bind(allocSize); err != nil {
		return err
	}
	m.code = append(m.code,
		0x48, 0x89, 0xc6,
		0x48, 0x31, 0xff,
		0xba, 0x03, 0x00, 0x00, 0x00,
		0x41, 0xba, 0x22, 0x00, 0x00, 0x00,
		0x41, 0xb8, 0xff, 0xff, 0xff, 0xff,
		0x45, 0x31, 0xc9,
		0xb8, 0x09, 0x00, 0x00, 0x00,
		0x0f, 0x05,
		0x48, 0x85, 0xc0,
	)
	if err := m.emitConditionalJump(0x88, closeFail); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0xc3, 0x48, 0x31, 0xed)
	readLoop := m.newLabel()
	readDone := m.newLabel()
	if err := m.bind(readLoop); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x39, 0xfd) // cmp rbp, r15
	if err := m.emitConditionalJump(0x83, readDone); err != nil {
		return err
	}
	m.code = append(m.code,
		0x4c, 0x89, 0xf7,
		0x48, 0x8d, 0x34, 0x2b,
		0x4c, 0x89, 0xfa,
		0x48, 0x29, 0xea,
		0x31, 0xc0,
		0x0f, 0x05,
		0x48, 0x85, 0xc0,
	)
	if err := m.emitConditionalJump(0x8e, closeFail); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x01, 0xc5)
	if err := m.emitJump(readLoop); err != nil {
		return err
	}
	if err := m.bind(readDone); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xf7, 0xb8, 0x03, 0x00, 0x00, 0x00, 0x0f, 0x05)
	m.code = append(m.code, 0x48, 0x89, 0xdf, 0x4c, 0x89, 0xfe)
	if err := m.emitLabelCall(m.stringAllocLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x81, 0xc4, 0x90, 0x00, 0x00, 0x00, 0x5d, 0x5b, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c)
	if err := m.emitBoxCall(0); err != nil {
		return err
	}
	returnLabel := m.newLabel()
	if err := m.emitJump(returnLabel); err != nil {
		return err
	}
	if err := m.bind(openFail); err != nil {
		return err
	}
	if err := m.emitJump(errorLabel); err != nil {
		return err
	}
	if err := m.bind(closeFail); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xf7, 0xb8, 0x03, 0x00, 0x00, 0x00, 0x0f, 0x05)
	if err := m.emitJump(errorLabel); err != nil {
		return err
	}
	if err := m.bind(errorLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x81, 0xc4, 0x90, 0x00, 0x00, 0x00, 0x5d, 0x5b, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c)
	m.emitStringAddress("fs_read_text failed")
	if err := m.emitBoxCall(1); err != nil {
		return err
	}
	if err := m.bind(returnLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0xc3)
	return nil
}

func (m *directMachine) emitFSWriteBytesRuntime() error {
	if err := m.bind(m.fsWriteBytesLabel); err != nil {
		return err
	}
	// rdi=String path, rsi=Bytes. Both use the same {length, bytes[]} layout.
	m.code = append(m.code,
		0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x41, 0x57, 0x53, 0x55,
		0x49, 0x89, 0xfc,
		0x49, 0x89, 0xf5,
		0x49, 0x8d, 0x7c, 0x24, 0x08,
		0x49, 0x8b, 0x34, 0x24,
	)
	if err := m.emitLabelCall(m.stringAllocLabel); err != nil {
		return err
	}
	m.code = append(m.code,
		0x49, 0x89, 0xc7,
		0x49, 0x8b, 0x4f, 0x00,
		0x4c, 0x01, 0xf9,
		0xc6, 0x41, 0x08, 0x00,
		0x49, 0x8d, 0x7f, 0x08,
		0x48, 0xc7, 0xc6, 0x41, 0x02, 0x00, 0x00,
		0xba, 0xb6, 0x01, 0x00, 0x00,
		0xb8, 0x02, 0x00, 0x00, 0x00,
		0x0f, 0x05,
		0x48, 0x85, 0xc0,
	)
	openFail := m.newLabel()
	closeFail := m.newLabel()
	errorLabel := m.newLabel()
	if err := m.emitConditionalJump(0x88, openFail); err != nil {
		return err
	}
	m.code = append(m.code,
		0x49, 0x89, 0xc6,
		0x48, 0x31, 0xed,
	)
	writeLoop := m.newLabel()
	writeDone := m.newLabel()
	if err := m.bind(writeLoop); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x3b, 0x6d, 0x00) // cmp rbp, [r13]
	if err := m.emitConditionalJump(0x83, writeDone); err != nil {
		return err
	}
	m.code = append(m.code,
		0x4c, 0x89, 0xf7,
		0x49, 0x8d, 0x75, 0x08,
		0x48, 0x01, 0xee,
		0x49, 0x8b, 0x55, 0x00,
		0x48, 0x29, 0xea,
		0xb8, 0x01, 0x00, 0x00, 0x00,
		0x0f, 0x05,
		0x48, 0x85, 0xc0,
	)
	if err := m.emitConditionalJump(0x8e, closeFail); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x01, 0xc5)
	if err := m.emitJump(writeLoop); err != nil {
		return err
	}
	if err := m.bind(writeDone); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xf7, 0xb8, 0x03, 0x00, 0x00, 0x00, 0x0f, 0x05)
	m.code = append(m.code, 0x5d, 0x5b, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c)
	m.emitMoveImmediate(0)
	if err := m.emitBoxCall(0); err != nil {
		return err
	}
	returnLabel := m.newLabel()
	if err := m.emitJump(returnLabel); err != nil {
		return err
	}
	if err := m.bind(openFail); err != nil {
		return err
	}
	if err := m.emitJump(errorLabel); err != nil {
		return err
	}
	if err := m.bind(closeFail); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xf7, 0xb8, 0x03, 0x00, 0x00, 0x00, 0x0f, 0x05)
	if err := m.emitJump(errorLabel); err != nil {
		return err
	}
	if err := m.bind(errorLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x5d, 0x5b, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c)
	m.emitStringAddress("fs_write_bytes failed")
	if err := m.emitBoxCall(1); err != nil {
		return err
	}
	if err := m.bind(returnLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0xc3)
	return nil
}

func (m *directMachine) emitWrite(text string) error {
	if m.windowsABI && uint64(len(text)) > uint64(^uint32(0)) {
		return fmt.Errorf("direct PE output exceeds WriteFile's DWORD length")
	}
	m.code = append(m.code, 0x41, 0xb8) // mov r8d, byte length
	var length [4]byte
	binary.LittleEndian.PutUint32(length[:], uint32(len(text)))
	m.code = append(m.code, length[:]...)
	if err := m.emitOutputBudgetAddR8(); err != nil {
		return err
	}
	return m.emitWriteRaw(text)
}

func (m *directMachine) emitWriteRaw(text string) error {
	offset := m.addData(text)
	if m.windowsABI {
		start := len(m.code)
		m.code = append(m.code, 0x48, 0x8d, 0x15, 0, 0, 0, 0) // lea rdx, [rip+text]
		m.dataRefs = append(m.dataRefs, machineDataRef{displacement: start + 3, instructionEnd: start + 7, dataOffset: offset})
		m.code = append(m.code, 0x41, 0xb8, 0, 0, 0, 0) // mov r8d, length
		binary.LittleEndian.PutUint32(m.code[len(m.code)-4:], uint32(len(text)))
		return m.emitPEWriteRDXR8()
	}
	// lea rsi, [rip + text]; mov edx, byte length
	start := len(m.code)
	m.code = append(m.code, 0x48, 0x8d, 0x35, 0, 0, 0, 0)
	m.code = append(m.code, 0xba, 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(m.code[len(m.code)-4:], uint32(len(text)))
	m.dataRefs = append(m.dataRefs, machineDataRef{displacement: start + 3, instructionEnd: start + 7, dataOffset: offset})
	return m.emitELFWriteRSIRDX()
}

func (m *directMachine) emitPEWriteStderr(text string) error {
	if !m.windowsABI {
		return fmt.Errorf("standard error output requires the Win64 ABI")
	}
	if uint64(len(text)) > uint64(^uint32(0)) {
		return fmt.Errorf("direct PE error output exceeds WriteFile's DWORD length")
	}
	offset := m.addData(text)
	start := len(m.code)
	m.code = append(m.code, 0x48, 0x8d, 0x15, 0, 0, 0, 0) // lea rdx, [rip+text]
	m.dataRefs = append(m.dataRefs, machineDataRef{displacement: start + 3, instructionEnd: start + 7, dataOffset: offset})
	m.code = append(m.code, 0x41, 0xb8, 0, 0, 0, 0) // mov r8d, length
	binary.LittleEndian.PutUint32(m.code[len(m.code)-4:], uint32(len(text)))
	return m.emitPEWriteStderrRDXR8()
}

func (m *directMachine) emitPEWriteStderrRDXR8() error {
	writeError := m.newLabel()
	if err := m.emitPEWriteRDXR8To(0xfffffff4, writeError); err != nil { // STD_ERROR_HANDLE
		return err
	}
	if err := m.emitExit(1); err != nil {
		return err
	}
	if err := m.bind(writeError); err != nil {
		return err
	}
	return m.emitExit(1)
}

func (m *directMachine) emitExit(status byte) error {
	if m.windowsABI {
		// Restore the current frame before tail-calling. Its caller supplied a
		// return slot and Win64 home area, which form a valid stack for
		// ExitProcess (which never returns).
		m.code = append(m.code, 0xc9, 0xb9, status, 0, 0, 0) // leave; mov ecx, status
		m.code = append(m.code, 0xff, 0x25)                  // jmp [rip+ExitProcess IAT]
		displacement := len(m.code)
		m.code = append(m.code, 0, 0, 0, 0)
		m.peImportRefs = append(m.peImportRefs, peImportRef{
			displacement: displacement, instructionEnd: len(m.code), importIndex: peImportExitProcess,
		})
		return nil
	}
	m.code = append(m.code, 0xb8, 0x3c, 0, 0, 0, 0xbf, status, 0, 0, 0, 0x0f, 0x05)
	return nil
}

func (m *directMachine) emitLoadSlot(slot machineSlot) {
	m.code = append(m.code, 0x48, 0x8b, 0x85)
	var disp [4]byte
	binary.LittleEndian.PutUint32(disp[:], uint32(-slot.offset))
	m.code = append(m.code, disp[:]...)
}

func (m *directMachine) emitStoreSlot(slot machineSlot) {
	m.code = append(m.code, 0x48, 0x89, 0x85)
	var disp [4]byte
	binary.LittleEndian.PutUint32(disp[:], uint32(-slot.offset))
	m.code = append(m.code, disp[:]...)
}

func (m *directMachine) emitMoveImmediate(value uint64) {
	m.code = append(m.code, 0x48, 0xb8)
	var imm [8]byte
	binary.LittleEndian.PutUint64(imm[:], value)
	m.code = append(m.code, imm[:]...)
}

func (m *directMachine) emitTrapOnOverflow() {
	// jo rel32
	_ = m.emitConditionalJump(0x80, m.trapLabel)
}

func (m *directMachine) emitUIntMask(bits uint8) {
	if bits == 0 || bits == 64 {
		return
	}
	// and eax, imm32 is sufficient for UInt8/16/32 and clears high bits.
	m.code = append(m.code, 0x25)
	var mask [4]byte
	binary.LittleEndian.PutUint32(mask[:], uint32((uint64(1)<<bits)-1))
	m.code = append(m.code, mask[:]...)
}

func (m *directMachine) emitInteger(unsigned, newline bool) error {
	// The buffer grows down from rbp-(nextSlot+1), leaving 63 bytes for the
	// longest signed decimal Int plus its sign. R8 is the moving end pointer;
	// R9 is the divisor and R10b records a negative signed input.
	m.code = append(m.code, 0x4c, 0x8d, 0x85)
	var buffer [4]byte
	binary.LittleEndian.PutUint32(buffer[:], uint32(-m.bufferOffset))
	m.code = append(m.code, buffer[:]...)
	m.code = append(m.code, 0x49, 0xb9)
	var ten [8]byte
	binary.LittleEndian.PutUint64(ten[:], 10)
	m.code = append(m.code, ten[:]...)
	zero := m.newLabel()
	digits := m.newLabel()
	addSign := m.newLabel()
	ready := m.newLabel()
	m.code = append(m.code, 0x45, 0x31, 0xd2)                 // xor r10d, r10d
	m.code = append(m.code, 0x48, 0x85, 0xc0)                 // test rax, rax
	if err := m.emitConditionalJump(0x84, zero); err != nil { // jz zero
		return err
	}
	if !unsigned {
		if err := m.emitConditionalJump(0x89, digits); err != nil { // jns digits
			return err
		}
		m.code = append(m.code, 0x41, 0xb2, 0x01) // mov r10b, 1
		m.code = append(m.code, 0x48, 0xf7, 0xd8) // neg rax
		// MinInt remains its unsigned two's-complement magnitude here. The
		// following unsigned division emits 9223372036854775808 safely, so
		// trapping on the negation would reject a valid Int display value.
	}
	if err := m.bind(digits); err != nil {
		return err
	}
	loop := m.newLabel()
	if err := m.bind(loop); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x31, 0xd2, 0x49, 0xf7, 0xf1) // xor edx, edx; div r9
	m.code = append(m.code, 0x80, 0xc2, 0x30, 0x49, 0xff, 0xc8, 0x41, 0x88, 0x10)
	m.code = append(m.code, 0x48, 0x85, 0xc0)                 // test rax, rax
	if err := m.emitConditionalJump(0x85, loop); err != nil { // jnz loop
		return err
	}
	if err := m.emitJump(addSign); err != nil {
		return err
	}
	if err := m.bind(zero); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc8, 0x41, 0xc6, 0x00, 0x30)
	if err := m.emitJump(addSign); err != nil {
		return err
	}
	if err := m.bind(addSign); err != nil {
		return err
	}
	if !unsigned {
		m.code = append(m.code, 0x45, 0x84, 0xd2) // test r10b, r10b
		if err := m.emitConditionalJump(0x84, ready); err != nil {
			return err
		}
		m.code = append(m.code, 0x49, 0xff, 0xc8, 0x41, 0xc6, 0x00, 0x2d)
	}
	if err := m.bind(ready); err != nil {
		return err
	}
	if m.windowsABI {
		// R8 points to the first digit; compute its length from the fixed end
		// of the current stack buffer, then pass (RDX=buffer, R8D=length).
		m.code = append(m.code, 0x4c, 0x89, 0xc2) // mov rdx, r8
		m.code = append(m.code, 0x4c, 0x8d, 0x85)
		m.code = append(m.code, buffer[:]...)
		m.code = append(m.code, 0x49, 0x29, 0xd0) // sub r8, rdx
		if newline {
			m.code = append(m.code, 0x49, 0xff, 0xc0) // inc r8
		}
		if err := m.emitOutputBudgetAddR8(); err != nil {
			return err
		}
		if newline {
			m.code = append(m.code, 0x49, 0xff, 0xc8) // dec r8 to restore the string length
		}
		if err := m.emitPEWriteRDXR8(); err != nil {
			return err
		}
		if newline {
			return m.emitWriteRaw("\n")
		}
		return nil
	}
	// Pass the first digit and its byte count to the reliable syscall loop.
	m.code = append(m.code, 0x4c, 0x89, 0xc6, 0x48, 0x8d, 0x95)
	m.code = append(m.code, buffer[:]...)
	m.code = append(m.code, 0x48, 0x29, 0xf2) // sub rdx, rsi -> length
	m.code = append(m.code, 0x49, 0x89, 0xd0) // mov r8, rdx
	if newline {
		m.code = append(m.code, 0x49, 0xff, 0xc0) // inc r8
	}
	if err := m.emitOutputBudgetAddR8(); err != nil {
		return err
	}
	if err := m.emitELFWriteRSIRDX(); err != nil {
		return err
	}
	if newline {
		return m.emitWriteRaw("\n")
	}
	return nil
}

func machineScalarType(name string) (*Type, bool) {
	switch name {
	case "Int":
		return TInt, true
	case "Bool":
		return TBool, true
	case "UInt8":
		return TUInt8, true
	case "UInt16":
		return TUInt16, true
	case "UInt32":
		return TUInt32, true
	case "UInt64":
		return TUInt64, true
	case "String":
		return TString, true
	case "Bytes":
		return TBytes, true
	case "Float":
		return TFloat, true
	case "Json":
		return TJSON, true
	case "Nil":
		return TNil, true
	default:
		if strings.HasPrefix(name, "Array[") && strings.HasSuffix(name, "]") {
			return Arr(TUnknown), true
		}
		if strings.HasPrefix(name, "Option[") && strings.HasSuffix(name, "]") {
			return Opt(TUnknown), true
		}
		if strings.HasPrefix(name, "Result[") && strings.HasSuffix(name, "]") {
			return Res(TUnknown, TUnknown), true
		}
		if strings.HasPrefix(name, "Map[") && strings.HasSuffix(name, "]") {
			return MapOf(TUnknown, TUnknown), true
		}
		return nil, false
	}
}

func (m *directMachine) emitStoreArg(index int, slot machineSlot) error {
	maxArgs := 6
	if m.windowsABI {
		maxArgs = directPEWindowsMaxArgs
	}
	if index < 0 || index >= maxArgs {
		return fmt.Errorf("direct backend supports at most %d scalar arguments", maxArgs)
	}
	if m.windowsABI {
		if index >= 4 {
			// The caller's 32-byte home area follows the return address. After
			// push rbp; mov rbp,rsp, argument five is at [rbp+48].
			offset := 48 + (index-4)*8
			m.code = append(m.code, 0x48, 0x8b, 0x45, byte(offset)) // mov rax,[rbp+offset]
			m.emitStoreSlot(slot)
			return nil
		}
		registerLoads := [4][]byte{
			{0x48, 0x89, 0xc8}, // mov rax, rcx
			{0x48, 0x89, 0xd0}, // mov rax, rdx
			{0x4c, 0x89, 0xc0}, // mov rax, r8
			{0x4c, 0x89, 0xc8}, // mov rax, r9
		}
		m.code = append(m.code, registerLoads[index]...)
		m.emitStoreSlot(slot)
		return nil
	}
	// Move the incoming register through RAX so the existing checked slot
	// store remains the single encoding path for local storage.
	registerLoads := [6][]byte{
		{0x48, 0x89, 0xf8},
		{0x48, 0x89, 0xf0},
		{0x48, 0x89, 0xd0},
		{0x48, 0x89, 0xc8},
		{0x4c, 0x89, 0xc0},
		{0x4c, 0x89, 0xc8},
	}
	m.code = append(m.code, registerLoads[index]...)
	m.emitStoreSlot(slot)
	return nil
}

func (m *directMachine) emitFunctionEpilog() {
	if m.windowsABI {
		// Win64 epilogs must use an unwind-recognizable stack reset followed by
		// the nonvolatile pop and return.
		m.code = append(m.code, 0x48, 0x8d, 0x65, 0x00, 0x5d, 0xc3) // lea rsp,[rbp]; pop rbp; ret
		return
	}
	m.code = append(m.code, 0xc9, 0xc3) // leave; ret
}

func machineBits(t *Type) uint8 {
	if t != nil && t.Kind == TyUInt {
		return t.Bits
	}
	return 0
}

func machineSetcc(op TokenKind, unsigned bool) byte {
	switch op {
	case EQEQ:
		return 0x94
	case NEQ:
		return 0x95
	case LESS:
		if unsigned {
			return 0x92
		}
		return 0x9c
	case LEQ:
		if unsigned {
			return 0x96
		}
		return 0x9e
	case GREATER:
		if unsigned {
			return 0x97
		}
		return 0x9f
	case GEQ:
		if unsigned {
			return 0x93
		}
		return 0x9d
	default:
		return 0x94
	}
}

func emitELF64CodeData(code, data []byte) []byte {
	fileSize := elfCodeOffset + len(code) + len(data)
	out := make([]byte, fileSize)
	copy(out[elfCodeOffset:], code)
	copy(out[elfCodeOffset+len(code):], data)
	copy(out[0:4], []byte{0x7f, 'E', 'L', 'F'})
	out[4] = 2
	out[5] = 1
	out[6] = 1
	binary.LittleEndian.PutUint16(out[16:18], 2)
	binary.LittleEndian.PutUint16(out[18:20], 0x3e)
	binary.LittleEndian.PutUint32(out[20:24], 1)
	binary.LittleEndian.PutUint64(out[24:32], elfBase+elfCodeOffset)
	binary.LittleEndian.PutUint64(out[32:40], elfHeaderLen)
	binary.LittleEndian.PutUint64(out[40:48], 0)
	binary.LittleEndian.PutUint16(out[52:54], elfHeaderLen)
	binary.LittleEndian.PutUint16(out[54:56], elfProgramLen)
	binary.LittleEndian.PutUint16(out[56:58], 1)
	ph := elfHeaderLen
	binary.LittleEndian.PutUint32(out[ph:ph+4], 1)
	binary.LittleEndian.PutUint32(out[ph+4:ph+8], 5)
	binary.LittleEndian.PutUint64(out[ph+8:ph+16], 0)
	binary.LittleEndian.PutUint64(out[ph+16:ph+24], elfBase)
	binary.LittleEndian.PutUint64(out[ph+24:ph+32], elfBase)
	binary.LittleEndian.PutUint64(out[ph+32:ph+40], uint64(fileSize))
	binary.LittleEndian.PutUint64(out[ph+40:ph+48], uint64(fileSize))
	binary.LittleEndian.PutUint64(out[ph+48:ph+56], 0x1000)
	return out
}
