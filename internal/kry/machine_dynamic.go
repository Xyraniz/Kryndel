package kry

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// directMachine is the second direct-ELF slice. It lowers checked Int/Bool
// state, assignments, comparisons, if/while control flow, scalar functions,
// and static or dynamic integer output to x86-64 instructions. It intentionally
// has no runtime or C dependency.
// Unsupported values fail during compilation instead of silently changing
// Kryndel semantics.
type directMachine struct {
	code              []byte
	data              []byte
	dataByText        map[string]int
	stringObjects     map[string]int
	dataRefs          []machineDataRef
	labels            []machineLabel
	slots             map[string]machineSlot
	nextSlot          int32
	loops             []machineLoop
	endLabel          int
	trapLabel         int
	staticEnv         map[string]Value
	bufferOffset      int32
	functionLabels    map[string]int
	functionOrder     []*Function
	functionSlots     map[string]map[string]machineSlot
	functionNextSlot  map[string]int32
	functionReturns   map[string]*Type
	stringConcatLabel int
	stringConcatUsed  bool
	arrayAllocLabel   int
	arrayPushLabel    int
	arrayConcatLabel  int
	arrayRuntimeUsed  bool
	boxAllocLabel     int
	boxRuntimeUsed    bool
	inFunction        bool
	currentFunction   string
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

func newDirectMachine() *directMachine {
	m := &directMachine{
		dataByText:       map[string]int{},
		stringObjects:    map[string]int{},
		slots:            map[string]machineSlot{},
		staticEnv:        map[string]Value{},
		functionLabels:   map[string]int{},
		functionSlots:    map[string]map[string]machineSlot{},
		functionNextSlot: map[string]int32{},
		functionReturns:  map[string]*Type{},
	}
	m.endLabel = m.newLabel()
	m.trapLabel = m.newLabel()
	m.stringConcatLabel = m.newLabel()
	m.arrayAllocLabel = m.newLabel()
	m.arrayPushLabel = m.newLabel()
	m.arrayConcatLabel = m.newLabel()
	m.boxAllocLabel = m.newLabel()
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

func (m *directMachine) emitJump(label int) error {
	m.code = append(m.code, 0xe9)
	return m.emitLabelDisplacement(label)
}

func (m *directMachine) emitCall(name string) error {
	label, ok := m.functionLabels[name]
	if !ok {
		return fmt.Errorf("direct ELF backend has no function '%s'", name)
	}
	m.code = append(m.code, 0xe8)
	return m.emitLabelDisplacement(label)
}

func (m *directMachine) emitLabelCall(label int) error {
	m.code = append(m.code, 0xe8)
	return m.emitLabelDisplacement(label)
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

func (m *directMachine) emitStringWrite() {
	// rax points to {u64 length, u8 bytes[length]}.
	m.code = append(m.code, 0x48, 0x8b, 0x10) // mov rdx, [rax]
	m.code = append(m.code, 0x48, 0x8d, 0x70, 0x08)
	m.code = append(m.code, 0xb8, 0x01, 0, 0, 0, 0xbf, 0x01, 0, 0, 0, 0x0f, 0x05)
}

func (m *directMachine) emitStringConcatCall() error {
	// emitBinary leaves the left pointer in RAX and the right pointer in RCX.
	m.code = append(m.code, 0x48, 0x89, 0xc7, 0x48, 0x89, 0xce) // mov rdi, rax; mov rsi, rcx
	m.stringConcatUsed = true
	return m.emitLabelCall(m.stringConcatLabel)
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

func (m *directMachine) emitArrayLiteral(e *Expr) error {
	m.emitMoveImmediate(uint64(len(e.Items)))
	if err := m.emitArrayAllocCall(); err != nil {
		return err
	}
	// Keep the allocated object on the machine stack while each element is
	// evaluated. Nested expressions balance their own temporary stack usage.
	m.code = append(m.code, 0x50) // push rax
	for index, item := range e.Items {
		if err := m.emitExpr(item); err != nil {
			return err
		}
		m.code = append(m.code, 0x49, 0x89, 0xc0)       // mov r8, rax
		m.code = append(m.code, 0x48, 0x8b, 0x0c, 0x24) // mov rcx, [rsp]
		m.code = append(m.code, 0x48, 0xba)
		var offset [8]byte
		binary.LittleEndian.PutUint64(offset[:], uint64(8+index*8))
		m.code = append(m.code, offset[:]...)
		m.code = append(m.code, 0x48, 0x01, 0xca, 0x4c, 0x89, 0x02) // add rdx, rcx; mov [rdx], r8
	}
	m.code = append(m.code, 0x58) // pop rax
	return nil
}

func (m *directMachine) emitArrayIndex(e *Expr) error {
	if e.Base == nil || e.Base.Type == nil || e.Base.Type.Kind != TyArray {
		return fmt.Errorf("direct ELF backend supports indexing only Array[T] in the dynamic slice")
	}
	if err := m.emitExpr(e.Base); err != nil {
		return err
	}
	m.code = append(m.code, 0x50) // push array pointer
	if err := m.emitExpr(e.Left); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0xc1, 0x58)                  // mov rcx, rax; pop rax
	m.code = append(m.code, 0x48, 0x85, 0xc9)                        // test rcx, rcx
	if err := m.emitConditionalJump(0x88, m.trapLabel); err != nil { // js: negative index
		return err
	}
	m.code = append(m.code, 0x48, 0x3b, 0x08)                        // cmp rcx, [rax]
	if err := m.emitConditionalJump(0x83, m.trapLabel); err != nil { // jae: index >= length
		return err
	}
	m.code = append(m.code, 0x48, 0x8b, 0x44, 0xc8, 0x08) // mov rax, [rax+rcx*8+8]
	return nil
}

func (m *directMachine) emitArrayPushCall(e *Expr) error {
	if len(e.Args) != 2 || e.Args[0].Type == nil || e.Args[0].Type.Kind != TyArray {
		return fmt.Errorf("direct ELF backend expects array_push(Array[T], T)")
	}
	if err := m.emitExpr(e.Args[0]); err != nil {
		return err
	}
	m.code = append(m.code, 0x50) // push array pointer
	if err := m.emitExpr(e.Args[1]); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0xc6, 0x58) // mov rsi, rax; pop rax
	m.code = append(m.code, 0x48, 0x89, 0xc7)       // mov rdi, rax
	m.arrayRuntimeUsed = true
	return m.emitLabelCall(m.arrayPushLabel)
}

func (m *directMachine) emitArrayConcatCall() error {
	// emitBinary leaves the left and right array pointers in RAX and RCX.
	m.code = append(m.code, 0x48, 0x89, 0xc7, 0x48, 0x89, 0xce) // mov rdi, rax; mov rsi, rcx
	m.arrayRuntimeUsed = true
	return m.emitLabelCall(m.arrayConcatLabel)
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

func (m *directMachine) emitOptionPredicate(e *Expr, none bool) error {
	if err := m.emitExpr(e); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x85, 0xc0)
	set := byte(0x95)
	if none {
		set = 0x94
	}
	m.code = append(m.code, 0x0f, set, 0xc0, 0x48, 0x0f, 0xb6, 0xc0)
	return nil
}

func (m *directMachine) emitResultPredicate(e *Expr, failed bool) error {
	if err := m.emitExpr(e); err != nil {
		return err
	}
	nilLabel := m.newLabel()
	joinLabel := m.newLabel()
	m.code = append(m.code, 0x48, 0x85, 0xc0)
	if err := m.emitConditionalJump(0x84, nilLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x83, 0x38, 0)
	set := byte(0x94)
	if failed {
		set = 0x95
	}
	m.code = append(m.code, 0x0f, set, 0xc0, 0x48, 0x0f, 0xb6, 0xc0)
	if err := m.emitJump(joinLabel); err != nil {
		return err
	}
	if err := m.bind(nilLabel); err != nil {
		return err
	}
	m.emitMoveImmediate(0)
	if err := m.bind(joinLabel); err != nil {
		return err
	}
	return nil
}

func (m *directMachine) emitUnwrapOr(e *Expr) error {
	if len(e.Args) != 2 {
		return fmt.Errorf("direct ELF backend unwrap_or expects two arguments")
	}
	if err := m.emitExpr(e.Args[0]); err != nil {
		return err
	}
	m.code = append(m.code, 0x50)
	if err := m.emitExpr(e.Args[1]); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0xc6, 0x5f, 0x48, 0x85, 0xff)
	noneLabel := m.newLabel()
	joinLabel := m.newLabel()
	if err := m.emitConditionalJump(0x84, noneLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x8b, 0x47, 0x08)
	if err := m.emitJump(joinLabel); err != nil {
		return err
	}
	if err := m.bind(noneLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0xf0) // mov rax, rsi
	return m.bind(joinLabel)
}

func (m *directMachine) emitResultUnwrap(e *Expr) error {
	if err := m.emitExpr(e); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x85, 0xc0)
	if err := m.emitConditionalJump(0x84, m.trapLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x83, 0x38, 0)
	if err := m.emitConditionalJump(0x85, m.trapLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x8b, 0x40, 0x08)
	return nil
}

func (m *directMachine) emitResultError(e *Expr) error {
	if err := m.emitExpr(e); err != nil {
		return err
	}
	nilLabel := m.newLabel()
	joinLabel := m.newLabel()
	m.code = append(m.code, 0x48, 0x85, 0xc0)
	if err := m.emitConditionalJump(0x84, nilLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x83, 0x38, 0)
	if err := m.emitConditionalJump(0x84, nilLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x8b, 0x78, 0x08, 0x48, 0xbe, 1, 0, 0, 0, 0, 0, 0, 0)
	m.boxRuntimeUsed = true
	if err := m.emitLabelCall(m.boxAllocLabel); err != nil {
		return err
	}
	if err := m.emitJump(joinLabel); err != nil {
		return err
	}
	if err := m.bind(nilLabel); err != nil {
		return err
	}
	m.emitMoveImmediate(0)
	return m.bind(joinLabel)
}

func (m *directMachine) emitArrayGet(e *Expr) error {
	if len(e.Args) != 2 {
		return fmt.Errorf("direct ELF backend array_get expects two arguments")
	}
	if err := m.emitExpr(e.Args[0]); err != nil {
		return err
	}
	m.code = append(m.code, 0x50)
	if err := m.emitExpr(e.Args[1]); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0xc6, 0x5f, 0x48, 0x85, 0xf6)
	nilLabel := m.newLabel()
	joinLabel := m.newLabel()
	if err := m.emitConditionalJump(0x88, nilLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x3b, 0x37)
	if err := m.emitConditionalJump(0x83, nilLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x8b, 0x44, 0xf7, 0x08, 0x48, 0x89, 0xc7, 0x48, 0xbe, 1, 0, 0, 0, 0, 0, 0, 0)
	m.boxRuntimeUsed = true
	if err := m.emitLabelCall(m.boxAllocLabel); err != nil {
		return err
	}
	if err := m.emitJump(joinLabel); err != nil {
		return err
	}
	if err := m.bind(nilLabel); err != nil {
		return err
	}
	m.emitMoveImmediate(0)
	return m.bind(joinLabel)
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

func (m *directMachine) emitWrite(text string) {
	offset := m.addData(text)
	// mov eax, SYS_write; mov edi, STDOUT_FILENO
	m.code = append(m.code, 0xb8, 0x01, 0, 0, 0, 0xbf, 0x01, 0, 0, 0)
	// lea rsi, [rip + disp32]
	start := len(m.code)
	m.code = append(m.code, 0x48, 0x8d, 0x35, 0, 0, 0, 0)
	// mov edx, length; syscall
	m.code = append(m.code, 0xba, 0, 0, 0, 0, 0x0f, 0x05)
	binary.LittleEndian.PutUint32(m.code[start+8:start+12], uint32(len(text)))
	m.dataRefs = append(m.dataRefs, machineDataRef{displacement: start + 3, instructionEnd: start + 7, dataOffset: offset})
}

func (m *directMachine) emitExit(status byte) {
	m.code = append(m.code, 0xb8, 0x3c, 0, 0, 0, 0xbf, status, 0, 0, 0, 0x0f, 0x05)
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

func (m *directMachine) emitInteger(unsigned bool) error {
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
		m.emitTrapOnOverflow()
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
	// write(1, r8, bufferEnd-r8)
	m.code = append(m.code, 0xb8, 0x01, 0, 0, 0, 0xbf, 0x01, 0, 0, 0)
	m.code = append(m.code, 0x4c, 0x89, 0xc6, 0x48, 0x8d, 0x95)
	m.code = append(m.code, buffer[:]...)
	m.code = append(m.code, 0x48, 0x29, 0xf2, 0x0f, 0x05)
	return nil
}

func (m *directMachine) emitBooleanOutput(e *Expr, newline bool) error {
	if err := m.emitExpr(e); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x85, 0xc0)
	falseLabel := m.newLabel()
	joinLabel := m.newLabel()
	if err := m.emitConditionalJump(0x84, falseLabel); err != nil {
		return err
	}
	m.emitWrite("true")
	if err := m.emitJump(joinLabel); err != nil {
		return err
	}
	if err := m.bind(falseLabel); err != nil {
		return err
	}
	m.emitWrite("false")
	if err := m.bind(joinLabel); err != nil {
		return err
	}
	if newline {
		m.emitWrite("\n")
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
		return nil, false
	}
}

func (m *directMachine) emitMoveArg(index int) error {
	if index < 0 || index >= 6 {
		return fmt.Errorf("direct ELF backend supports at most six scalar arguments")
	}
	// SysV AMD64 integer argument registers: rdi, rsi, rdx, rcx, r8, r9.
	registerMoves := [6][]byte{
		{0x48, 0x89, 0xc7},
		{0x48, 0x89, 0xc6},
		{0x48, 0x89, 0xc2},
		{0x48, 0x89, 0xc1},
		{0x49, 0x89, 0xc0},
		{0x49, 0x89, 0xc1},
	}
	m.code = append(m.code, registerMoves[index]...)
	return nil
}

func (m *directMachine) emitStoreArg(index int, slot machineSlot) error {
	if index < 0 || index >= 6 {
		return fmt.Errorf("direct ELF backend supports at most six scalar arguments")
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

func (m *directMachine) emitFunctionCall(e *Expr) error {
	if e.Function == nil {
		return fmt.Errorf("direct ELF backend has no resolved function call")
	}
	if len(e.Args) > 6 {
		return fmt.Errorf("direct ELF backend function '%s' has too many arguments", e.Function.Name)
	}
	for index, argument := range e.Args {
		if err := m.emitExpr(argument); err != nil {
			return err
		}
		if err := m.emitMoveArg(index); err != nil {
			return err
		}
	}
	return m.emitCall(e.Function.Name)
}

func machineBits(t *Type) uint8 {
	if t != nil && t.Kind == TyUInt {
		return t.Bits
	}
	return 0
}

func (m *directMachine) emitExpr(e *Expr) error {
	if e == nil {
		return fmt.Errorf("direct ELF backend cannot lower a missing expression")
	}
	if e.ConstValue != nil {
		switch e.ConstValue.Kind {
		case VInt:
			m.emitMoveImmediate(uint64(e.ConstValue.I))
			return nil
		case VUInt:
			m.emitMoveImmediate(e.ConstValue.U)
			m.emitUIntMask(e.ConstValue.UBits)
			return nil
		case VBool:
			if e.ConstValue.Bool {
				m.emitMoveImmediate(1)
			} else {
				m.emitMoveImmediate(0)
			}
			return nil
		case VString:
			m.emitStringAddress(e.ConstValue.S)
			return nil
		}
	}
	switch e.Kind {
	case ExInt:
		m.emitMoveImmediate(uint64(e.Int))
		return nil
	case ExBool:
		if e.Bool {
			m.emitMoveImmediate(1)
		} else {
			m.emitMoveImmediate(0)
		}
		return nil
	case ExString:
		m.emitStringAddress(e.Str)
		return nil
	case ExArray:
		return m.emitArrayLiteral(e)
	case ExVar:
		slot, ok := m.slots[e.Name]
		if !ok {
			return fmt.Errorf("direct ELF backend has no storage for binding '%s'", e.Name)
		}
		m.emitLoadSlot(slot)
		return nil
	case ExUnary:
		if err := m.emitExpr(e.Operand); err != nil {
			return err
		}
		switch e.Op {
		case PLUS:
			return nil
		case MINUS:
			m.code = append(m.code, 0x48, 0xf7, 0xd8)
			if e.Type == nil || e.Type.Kind != TyUInt {
				m.emitTrapOnOverflow()
			}
			return nil
		case BANG:
			m.code = append(m.code, 0x48, 0x85, 0xc0, 0x0f, 0x94, 0xc0, 0x48, 0x0f, 0xb6, 0xc0)
			return nil
		case BITNOT:
			m.code = append(m.code, 0x48, 0xf7, 0xd0)
			m.emitUIntMask(machineBits(e.Type))
			return nil
		default:
			return fmt.Errorf("direct ELF backend does not support unary operator %s", opText(e.Op))
		}
	case ExBinary:
		return m.emitBinary(e)
	case ExIndex:
		return m.emitArrayIndex(e)
	case ExCall:
		if e.Function != nil {
			return m.emitFunctionCall(e)
		}
		if e.Receiver != nil {
			return fmt.Errorf("direct ELF backend does not support receiver call %s", e.Name)
		}
		switch e.Name {
		case "len":
			if len(e.Args) != 1 || e.Args[0].Type == nil || e.Args[0].Type.Kind != TyArray {
				return fmt.Errorf("direct ELF backend supports len(Array[T]) only")
			}
			if err := m.emitExpr(e.Args[0]); err != nil {
				return err
			}
			m.code = append(m.code, 0x48, 0x8b, 0x00) // mov rax, [rax]
			return nil
		case "array_push":
			return m.emitArrayPushCall(e)
		case "array_concat":
			if len(e.Args) != 2 || e.Args[0].Type == nil || e.Args[0].Type.Kind != TyArray || e.Args[1].Type == nil || e.Args[1].Type.Kind != TyArray {
				return fmt.Errorf("direct ELF backend expects array_concat(Array[T], Array[T])")
			}
			if err := m.emitExpr(e.Args[0]); err != nil {
				return err
			}
			m.code = append(m.code, 0x50)
			if err := m.emitExpr(e.Args[1]); err != nil {
				return err
			}
			m.code = append(m.code, 0x48, 0x89, 0xc1, 0x58)
			return m.emitArrayConcatCall()
		case "some":
			if len(e.Args) != 1 {
				return fmt.Errorf("direct ELF backend some expects one argument")
			}
			if err := m.emitExpr(e.Args[0]); err != nil {
				return err
			}
			return m.emitBoxCall(1)
		case "none":
			if len(e.Args) != 0 {
				return fmt.Errorf("direct ELF backend none expects no arguments")
			}
			m.emitMoveImmediate(0)
			return nil
		case "ok", "err":
			if len(e.Args) != 1 {
				return fmt.Errorf("direct ELF backend %s expects one argument", e.Name)
			}
			if err := m.emitExpr(e.Args[0]); err != nil {
				return err
			}
			if e.Name == "ok" {
				return m.emitBoxCall(0)
			}
			return m.emitBoxCall(1)
		case "is_some":
			if len(e.Args) != 1 {
				return fmt.Errorf("direct ELF backend is_some expects one argument")
			}
			return m.emitOptionPredicate(e.Args[0], false)
		case "is_none":
			if len(e.Args) != 1 {
				return fmt.Errorf("direct ELF backend is_none expects one argument")
			}
			return m.emitOptionPredicate(e.Args[0], true)
		case "is_ok":
			if len(e.Args) != 1 {
				return fmt.Errorf("direct ELF backend is_ok expects one argument")
			}
			return m.emitResultPredicate(e.Args[0], false)
		case "is_err":
			if len(e.Args) != 1 {
				return fmt.Errorf("direct ELF backend is_err expects one argument")
			}
			return m.emitResultPredicate(e.Args[0], true)
		case "unwrap_or":
			return m.emitUnwrapOr(e)
		case "result_unwrap":
			if len(e.Args) != 1 {
				return fmt.Errorf("direct ELF backend result_unwrap expects one argument")
			}
			return m.emitResultUnwrap(e.Args[0])
		case "result_error":
			if len(e.Args) != 1 {
				return fmt.Errorf("direct ELF backend result_error expects one argument")
			}
			return m.emitResultError(e.Args[0])
		case "array_get":
			return m.emitArrayGet(e)
		case "u8", "u16", "u32", "u64":
			if len(e.Args) != 1 {
				return fmt.Errorf("direct ELF backend conversion %s expects one argument", e.Name)
			}
			value, ok := directStaticValue(e.Args[0], m.staticEnv)
			if !ok || (value.Kind != VInt && value.Kind != VUInt) {
				return fmt.Errorf("direct ELF backend requires a static argument for %s", e.Name)
			}
			if value.Kind == VInt {
				m.emitMoveImmediate(uint64(value.I))
			} else {
				m.emitMoveImmediate(value.U)
			}
			m.emitUIntMask(machineBits(e.Type))
			return nil
		default:
			return fmt.Errorf("direct ELF backend does not support call %s", e.Name)
		}
	default:
		return fmt.Errorf("direct ELF backend does not support expression kind %s", exprName(e.Kind))
	}
}

func (m *directMachine) emitBinary(e *Expr) error {
	if err := m.emitExpr(e.Left); err != nil {
		return err
	}
	m.code = append(m.code, 0x50) // push rax
	if err := m.emitExpr(e.Right); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0xc1, 0x58) // mov rcx, rax; pop rax
	leftType := e.Left.Type
	unsigned := leftType != nil && leftType.Kind == TyUInt
	if e.Type != nil && e.Type.Kind == TyString {
		if e.Op != PLUS {
			return fmt.Errorf("direct ELF backend does not support String operator %s", opText(e.Op))
		}
		return m.emitStringConcatCall()
	}
	if e.Type != nil && e.Type.Kind == TyArray {
		if e.Op != PLUS {
			return fmt.Errorf("direct ELF backend does not support Array operator %s", opText(e.Op))
		}
		return m.emitArrayConcatCall()
	}
	switch e.Op {
	case PLUS:
		m.code = append(m.code, 0x48, 0x01, 0xc8)
		if !unsigned {
			m.emitTrapOnOverflow()
		}
		m.emitUIntMask(machineBits(e.Type))
	case MINUS:
		m.code = append(m.code, 0x48, 0x29, 0xc8)
		if !unsigned {
			m.emitTrapOnOverflow()
		}
		m.emitUIntMask(machineBits(e.Type))
	case STAR:
		m.code = append(m.code, 0x48, 0x0f, 0xaf, 0xc1)
		if !unsigned {
			m.emitTrapOnOverflow()
		}
		m.emitUIntMask(machineBits(e.Type))
	case SLASH, PERCENT:
		m.code = append(m.code, 0x48, 0x85, 0xc9) // test rcx, rcx
		_ = m.emitConditionalJump(0x84, m.trapLabel)
		if unsigned {
			m.code = append(m.code, 0x48, 0x31, 0xd2, 0x48, 0xf7, 0xf1) // xor edx, edx; div rcx
		} else {
			// Signed division has one extra overflowing pair: MinInt / -1.
			safeDiv := m.newLabel()
			m.code = append(m.code, 0x48, 0xba)
			var min [8]byte
			binary.LittleEndian.PutUint64(min[:], uint64(1<<63))
			m.code = append(m.code, min[:]...)
			m.code = append(m.code, 0x48, 0x39, 0xd0) // cmp rax, rdx
			_ = m.emitConditionalJump(0x85, safeDiv)
			m.code = append(m.code, 0x48, 0x83, 0xf9, 0xff) // cmp rcx, -1
			_ = m.emitConditionalJump(0x85, safeDiv)
			_ = m.emitJump(m.trapLabel)
			if err := m.bind(safeDiv); err != nil {
				return err
			}
			m.code = append(m.code, 0x48, 0x99, 0x48, 0xf7, 0xf9) // cqo; idiv rcx
		}
		if e.Op == PERCENT {
			m.code = append(m.code, 0x48, 0x89, 0xd0) // mov rax, rdx
		}
		m.emitUIntMask(machineBits(e.Type))
	case BITAND:
		m.code = append(m.code, 0x48, 0x21, 0xc8)
		m.emitUIntMask(machineBits(e.Type))
	case BITXOR:
		m.code = append(m.code, 0x48, 0x31, 0xc8)
		m.emitUIntMask(machineBits(e.Type))
	case PIPE:
		m.code = append(m.code, 0x48, 0x09, 0xc8)
		m.emitUIntMask(machineBits(e.Type))
	case SHL:
		m.code = append(m.code, 0x48, 0xd3, 0xe0)
		m.emitUIntMask(machineBits(e.Type))
	case SHR:
		m.code = append(m.code, 0x48, 0xd3, 0xe8)
		m.emitUIntMask(machineBits(e.Type))
	case AND:
		m.code = append(m.code, 0x48, 0x21, 0xc8)
	case OR:
		m.code = append(m.code, 0x48, 0x09, 0xc8)
	case EQEQ, NEQ, LESS, LEQ, GREATER, GEQ:
		m.code = append(m.code, 0x48, 0x39, 0xc8) // cmp rax, rcx
		m.code = append(m.code, 0x0f, machineSetcc(e.Op, unsigned), 0xc0, 0x48, 0x0f, 0xb6, 0xc0)
	default:
		return fmt.Errorf("direct ELF backend does not support binary operator %s", opText(e.Op))
	}
	return nil
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

func directDynamicStatements(p *Program) ([]*Stmt, error) {
	if len(p.Statements) > 0 {
		return p.Statements, nil
	}
	for _, f := range p.Functions {
		if f.Name == "main" {
			if len(f.Params) != 0 {
				return nil, fmt.Errorf("direct ELF backend does not support main parameters")
			}
			return f.Body, nil
		}
	}
	return nil, fmt.Errorf("direct ELF backend found no top-level statements or main function")
}

func directHasDynamicControl(stmts []*Stmt) bool {
	for _, s := range stmts {
		if s == nil {
			continue
		}
		switch s.Kind {
		case StAssign, StBreak, StContinue:
			return true
		case StIf:
			return true
		case StWhile:
			return true
		case StFor, StUnsafe, StDefer:
			if directHasDynamicControl(s.Body) {
				return true
			}
		case StMatch:
			for _, arm := range s.Arms {
				if directHasDynamicControl(arm.Body) {
					return true
				}
			}
		}
	}
	return false
}

func directHasArrayFeatureExpr(e *Expr) bool {
	if e == nil {
		return false
	}
	if e.Kind == ExArray || e.Kind == ExIndex {
		return true
	}
	if e.Type != nil && e.Type.Kind == TyArray {
		return true
	}
	if e.Kind == ExCall {
		if e.Name == "len" || e.Name == "array_push" || e.Name == "array_concat" || e.Name == "array_get" || e.Name == "some" || e.Name == "none" || e.Name == "ok" || e.Name == "err" || e.Name == "is_some" || e.Name == "is_none" || e.Name == "is_ok" || e.Name == "is_err" || e.Name == "unwrap_or" || e.Name == "result_unwrap" || e.Name == "result_error" {
			return true
		}
		for _, argument := range e.Args {
			if directHasArrayFeatureExpr(argument) {
				return true
			}
		}
	}
	if directHasArrayFeatureExpr(e.Left) || directHasArrayFeatureExpr(e.Right) || directHasArrayFeatureExpr(e.Operand) || directHasArrayFeatureExpr(e.Base) {
		return true
	}
	for _, item := range e.Items {
		if directHasArrayFeatureExpr(item) {
			return true
		}
	}
	return false
}

func directHasArrayFeatures(stmts []*Stmt) bool {
	for _, s := range stmts {
		if s == nil {
			continue
		}
		if directHasArrayFeatureExpr(s.Init) || directHasArrayFeatureExpr(s.Expr) || directHasArrayFeatureExpr(s.Target) || directHasArrayFeatureExpr(s.Value) || directHasArrayFeatureExpr(s.Cond) || directHasArrayFeatureExpr(s.Return) {
			return true
		}
		if directHasArrayFeatures(s.Then) || directHasArrayFeatures(s.Else) || directHasArrayFeatures(s.Body) {
			return true
		}
	}
	return false
}

func directHasUserFunctions(p *Program) bool {
	for _, f := range p.Functions {
		if f != nil && f.Name != "main" {
			return true
		}
	}
	return false
}

func (m *directMachine) collect(stmts []*Stmt) error {
	for _, s := range stmts {
		if s == nil {
			continue
		}
		switch s.Kind {
		case StLet, StConst:
			if s.Init == nil {
				return fmt.Errorf("direct ELF backend requires an initializer for '%s'", s.Name)
			}
			if _, exists := m.slots[s.Name]; exists {
				return fmt.Errorf("direct ELF backend does not support shadowed binding '%s'", s.Name)
			}
			m.nextSlot += 8
			m.slots[s.Name] = machineSlot{offset: m.nextSlot, typ: s.Init.Type}
		case StIf:
			if err := m.collect(s.Then); err != nil {
				return err
			}
			if err := m.collect(s.Else); err != nil {
				return err
			}
		case StWhile:
			if err := m.collect(s.Body); err != nil {
				return err
			}
		case StFor, StMatch, StDefer, StUnsafe:
			return fmt.Errorf("direct ELF backend does not support statement kind %s", stmtName(s.Kind))
		}
	}
	return nil
}

func (m *directMachine) collectFunction(stmts []*Stmt, slots map[string]machineSlot, nextSlot *int32) error {
	for _, s := range stmts {
		if s == nil {
			continue
		}
		switch s.Kind {
		case StLet, StConst:
			if s.Init == nil {
				return fmt.Errorf("direct ELF backend requires an initializer for '%s'", s.Name)
			}
			if _, exists := slots[s.Name]; exists {
				return fmt.Errorf("direct ELF backend does not support shadowed binding '%s'", s.Name)
			}
			localType, ok := machineScalarType(s.Init.Type.String())
			if !ok || localType.Kind == TyNil {
				return fmt.Errorf("direct ELF backend function local '%s' has unsupported type %s", s.Name, s.Init.Type.String())
			}
			*nextSlot += 8
			slots[s.Name] = machineSlot{offset: *nextSlot, typ: localType}
		case StAssign:
			if s.Target == nil || s.Target.Kind != ExVar {
				return fmt.Errorf("direct ELF backend function assignment target must be a binding")
			}
			if _, exists := slots[s.Target.Name]; !exists {
				return fmt.Errorf("direct ELF backend function assignment uses unknown binding '%s'", s.Target.Name)
			}
		case StIf:
			if err := m.collectFunction(s.Then, slots, nextSlot); err != nil {
				return err
			}
			if err := m.collectFunction(s.Else, slots, nextSlot); err != nil {
				return err
			}
		case StWhile:
			if err := m.collectFunction(s.Body, slots, nextSlot); err != nil {
				return err
			}
		case StExpr, StReturn, StBreak, StContinue:
			// Call targets, return forms, and loop nesting are checked while
			// emitting the body.
		default:
			return fmt.Errorf("direct ELF backend does not support function statement kind %s", stmtName(s.Kind))
		}
	}
	return nil
}

func (m *directMachine) prepareFunctions(p *Program) error {
	for _, f := range p.Functions {
		if f == nil || f.Name == "main" {
			continue
		}
		if _, exists := m.functionLabels[f.Name]; exists {
			return fmt.Errorf("direct ELF backend has duplicate function '%s'", f.Name)
		}
		if len(f.Params) > 6 {
			return fmt.Errorf("direct ELF backend function '%s' has too many parameters", f.Name)
		}
		returnTypeName := typeSpecString(f.Return)
		returnType, ok := machineScalarType(returnTypeName)
		if !ok {
			return fmt.Errorf("direct ELF backend function '%s' has unsupported return type %s", f.Name, returnTypeName)
		}
		m.functionLabels[f.Name] = m.newLabel()
		m.functionOrder = append(m.functionOrder, f)
		m.functionReturns[f.Name] = returnType
		slots := map[string]machineSlot{}
		for index, param := range f.Params {
			paramTypeName := typeSpecString(param.Type)
			paramType, ok := machineScalarType(paramTypeName)
			if !ok || paramType.Kind == TyNil {
				return fmt.Errorf("direct ELF backend function '%s' parameter '%s' has unsupported type %s", f.Name, param.Name, paramTypeName)
			}
			slots[param.Name] = machineSlot{offset: int32((index + 1) * 8), typ: paramType}
		}
		nextSlot := int32(len(f.Params) * 8)
		if err := m.collectFunction(f.Body, slots, &nextSlot); err != nil {
			return fmt.Errorf("function '%s': %w", f.Name, err)
		}
		m.functionSlots[f.Name] = slots
		m.functionNextSlot[f.Name] = nextSlot
	}
	return nil
}

func (m *directMachine) emitStaticOutput(e *Expr, name string) error {
	if e == nil || e.Kind != ExCall || e.Receiver != nil || len(e.Args) != 1 {
		return fmt.Errorf("direct ELF backend supports only one-argument print calls")
	}
	v, ok := directStaticValue(e.Args[0], m.staticEnv)
	if !ok || (v.Kind != VString && v.Kind != VInt && v.Kind != VBool && v.Kind != VUInt) {
		if e.Args[0].Type != nil && e.Args[0].Type.Kind == TyString {
			if err := m.emitExpr(e.Args[0]); err != nil {
				return err
			}
			m.emitStringWrite()
			if name == "println" {
				m.emitWrite("\n")
			}
			return nil
		}
		if e.Args[0].Type == nil || (e.Args[0].Type.Kind != TyInt && e.Args[0].Type.Kind != TyUInt) {
			if e.Args[0].Type != nil && e.Args[0].Type.Kind == TyBool {
				return m.emitBooleanOutput(e.Args[0], name == "println")
			}
			return fmt.Errorf("direct ELF backend supports only statically displayable print values")
		}
		if err := m.emitExpr(e.Args[0]); err != nil {
			return err
		}
		if err := m.emitInteger(e.Args[0].Type.Kind == TyUInt); err != nil {
			return err
		}
		if name == "println" {
			m.emitWrite("\n")
		}
		return nil
	}
	text := display(v)
	if name == "println" {
		text += "\n"
	}
	m.emitWrite(text)
	return nil
}

func (m *directMachine) emitStatements(stmts []*Stmt) error {
	for _, s := range stmts {
		if s == nil {
			continue
		}
		switch s.Kind {
		case StLet, StConst:
			if err := m.emitExpr(s.Init); err != nil {
				return err
			}
			slot := m.slots[s.Name]
			m.emitStoreSlot(slot)
			if !s.Mutable {
				if value, ok := directStaticValue(s.Init, m.staticEnv); ok {
					m.staticEnv[s.Name] = value
				}
			}
		case StAssign:
			if s.Target == nil || s.Target.Kind != ExVar {
				return fmt.Errorf("direct ELF backend assignment target must be a binding")
			}
			if err := m.emitExpr(s.Value); err != nil {
				return err
			}
			slot, ok := m.slots[s.Target.Name]
			if !ok {
				return fmt.Errorf("direct ELF backend has no storage for '%s'", s.Target.Name)
			}
			m.emitStoreSlot(slot)
			delete(m.staticEnv, s.Target.Name)
		case StExpr:
			if s.Expr == nil || s.Expr.Kind != ExCall || s.Expr.Receiver != nil {
				return fmt.Errorf("direct ELF backend supports only calls without receivers")
			}
			if s.Expr.Function != nil {
				if len(s.Expr.Args) != 0 {
					return fmt.Errorf("direct ELF backend function '%s' requires zero arguments", s.Expr.Function.Name)
				}
				if err := m.emitCall(s.Expr.Function.Name); err != nil {
					return err
				}
				continue
			}
			if len(s.Expr.Args) != 1 || (s.Expr.Name != "print" && s.Expr.Name != "println") {
				return fmt.Errorf("direct ELF backend supports only print/println or zero-argument user calls")
			}
			if err := m.emitStaticOutput(s.Expr, s.Expr.Name); err != nil {
				return err
			}
		case StIf:
			if err := m.emitExpr(s.Cond); err != nil {
				return err
			}
			m.code = append(m.code, 0x48, 0x85, 0xc0)
			elseLabel := m.newLabel()
			joinLabel := m.newLabel()
			if err := m.emitConditionalJump(0x84, elseLabel); err != nil {
				return err
			}
			if err := m.emitStatements(s.Then); err != nil {
				return err
			}
			if len(s.Else) > 0 {
				if err := m.emitJump(joinLabel); err != nil {
					return err
				}
				if err := m.bind(elseLabel); err != nil {
					return err
				}
				if err := m.emitStatements(s.Else); err != nil {
					return err
				}
			}
			if err := m.bind(joinLabel); err != nil {
				return err
			}
		case StWhile:
			conditionLabel := m.newLabel()
			endLabel := m.newLabel()
			if err := m.bind(conditionLabel); err != nil {
				return err
			}
			if err := m.emitExpr(s.Cond); err != nil {
				return err
			}
			m.code = append(m.code, 0x48, 0x85, 0xc0)
			if err := m.emitConditionalJump(0x84, endLabel); err != nil {
				return err
			}
			m.loops = append(m.loops, machineLoop{breakLabel: endLabel, continueLabel: conditionLabel})
			if err := m.emitStatements(s.Body); err != nil {
				return err
			}
			m.loops = m.loops[:len(m.loops)-1]
			if err := m.emitJump(conditionLabel); err != nil {
				return err
			}
			if err := m.bind(endLabel); err != nil {
				return err
			}
		case StBreak:
			if len(m.loops) == 0 {
				return fmt.Errorf("direct ELF backend break is outside a loop")
			}
			if err := m.emitJump(m.loops[len(m.loops)-1].breakLabel); err != nil {
				return err
			}
		case StContinue:
			if len(m.loops) == 0 {
				return fmt.Errorf("direct ELF backend continue is outside a loop")
			}
			if err := m.emitJump(m.loops[len(m.loops)-1].continueLabel); err != nil {
				return err
			}
		case StReturn:
			if m.inFunction {
				returnType := m.functionReturns[m.currentFunction]
				if s.Return != nil && s.Return.Kind != ExNil {
					if returnType == nil || returnType.Kind == TyNil {
						return fmt.Errorf("direct ELF backend Nil function cannot return a value")
					}
					if err := m.emitExpr(s.Return); err != nil {
						return err
					}
				} else if returnType != nil && returnType.Kind != TyNil {
					return fmt.Errorf("direct ELF backend scalar function must return a value")
				}
				m.code = append(m.code, 0xc9, 0xc3)
				continue
			}
			if err := m.emitJump(m.endLabel); err != nil {
				return err
			}
		default:
			return fmt.Errorf("direct ELF backend does not support statement kind %s", stmtName(s.Kind))
		}
	}
	return nil
}

func (m *directMachine) build(stmts []*Stmt) ([]byte, error) {
	if err := m.collect(stmts); err != nil {
		return nil, err
	}
	// push rbp; mov rbp, rsp; reserve aligned local storage.
	m.code = append(m.code, 0x55, 0x48, 0x89, 0xe5)
	frame := (int(m.nextSlot) + 15) &^ 15
	m.bufferOffset = m.nextSlot + 1
	frame = (int(m.nextSlot) + 64 + 15) &^ 15
	if frame > 0 {
		m.code = append(m.code, 0x48, 0x81, 0xec)
		var size [4]byte
		binary.LittleEndian.PutUint32(size[:], uint32(frame))
		m.code = append(m.code, size[:]...)
	}
	if err := m.emitStatements(stmts); err != nil {
		return nil, err
	}
	if len(m.functionOrder) > 0 || m.stringConcatUsed || m.arrayRuntimeUsed || m.boxRuntimeUsed {
		if err := m.emitJump(m.endLabel); err != nil {
			return nil, err
		}
		mainSlots := m.slots
		mainStatic := m.staticEnv
		mainLoops := m.loops
		for _, f := range m.functionOrder {
			if err := m.bind(m.functionLabels[f.Name]); err != nil {
				return nil, err
			}
			m.inFunction = true
			m.currentFunction = f.Name
			m.slots = m.functionSlots[f.Name]
			m.staticEnv = map[string]Value{}
			m.loops = nil
			m.bufferOffset = m.functionNextSlot[f.Name] + 1
			functionFrame := (int(m.bufferOffset) + 63 + 15) &^ 15
			m.code = append(m.code, 0x55, 0x48, 0x89, 0xe5)
			m.code = append(m.code, 0x48, 0x81, 0xec)
			var functionSize [4]byte
			binary.LittleEndian.PutUint32(functionSize[:], uint32(functionFrame))
			m.code = append(m.code, functionSize[:]...)
			for index, param := range f.Params {
				if err := m.emitStoreArg(index, m.functionSlots[f.Name][param.Name]); err != nil {
					return nil, fmt.Errorf("function '%s': %w", f.Name, err)
				}
			}
			if err := m.emitStatements(f.Body); err != nil {
				return nil, fmt.Errorf("function '%s': %w", f.Name, err)
			}
			m.inFunction = false
			m.currentFunction = ""
			m.slots = mainSlots
			m.staticEnv = mainStatic
			m.loops = mainLoops
			m.code = append(m.code, 0xc9, 0xc3)
		}
	}
	if m.stringConcatUsed {
		if err := m.emitStringConcatRuntime(); err != nil {
			return nil, err
		}
	}
	if m.arrayRuntimeUsed {
		if err := m.emitArrayAllocRuntime(); err != nil {
			return nil, err
		}
		if err := m.emitArrayPushRuntime(); err != nil {
			return nil, err
		}
		if err := m.emitArrayConcatRuntime(); err != nil {
			return nil, err
		}
	}
	if m.boxRuntimeUsed {
		if err := m.emitBoxAllocRuntime(); err != nil {
			return nil, err
		}
	}
	if err := m.bind(m.endLabel); err != nil {
		return nil, err
	}
	m.emitExit(0)
	if err := m.bind(m.trapLabel); err != nil {
		return nil, err
	}
	m.emitExit(1)
	for _, ref := range m.dataRefs {
		dataAddress := elfCodeOffset + len(m.code) + ref.dataOffset
		nextInstruction := elfCodeOffset + ref.instructionEnd
		delta := dataAddress - nextInstruction
		if delta < -1<<31 || delta > 1<<31-1 {
			return nil, fmt.Errorf("direct ELF backend data is out of RIP-relative range")
		}
		binary.LittleEndian.PutUint32(m.code[ref.displacement:ref.displacement+4], uint32(int32(delta)))
	}
	return emitELF64CodeData(m.code, m.data), nil
}

func buildDirectDynamicELF(p *Program) ([]byte, error) {
	stmts, err := directDynamicStatements(p)
	if err != nil {
		return nil, err
	}
	machine := newDirectMachine()
	if err := machine.prepareFunctions(p); err != nil {
		return nil, err
	}
	return machine.build(stmts)
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
