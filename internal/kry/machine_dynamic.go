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
	code                []byte
	data                []byte
	dataByText          map[string]int
	stringObjects       map[string]int
	dataRefs            []machineDataRef
	labels              []machineLabel
	slots               map[string]machineSlot
	nextSlot            int32
	loops               []machineLoop
	endLabel            int
	trapLabel           int
	staticEnv           map[string]Value
	bufferOffset        int32
	functionLabels      map[string]int
	functionOrder       []*Function
	functionSlots       map[string]map[string]machineSlot
	functionNextSlot    map[string]int32
	statementSlots      map[*Stmt]machineSlot
	forIterSlots        map[*Stmt]machineSlot
	forIndexSlots       map[*Stmt]machineSlot
	scopeStack          []map[string]machineSlot
	functionReturns     map[string]*Type
	stringConcatLabel   int
	stringConcatUsed    bool
	arrayAllocLabel     int
	arrayPushLabel      int
	arrayConcatLabel    int
	arrayRuntimeUsed    bool
	boxAllocLabel       int
	boxRuntimeUsed      bool
	structAllocLabel    int
	structRuntimeUsed   bool
	stringAllocLabel    int
	stringCharsLabel    int
	stringEqualLabel    int
	mapFindLabel        int
	mapInsertLabel      int
	substringLabel      int
	intToStringLabel    int
	intFromStringLabel  int
	jsonParseLabel      int
	jsonSkipLabel       int
	jsonKindLabel       int
	jsonObjectGetLabel  int
	mapRuntimeUsed      bool
	stringCharsUsed     bool
	substringUsed       bool
	intToStringUsed     bool
	intFromStringUsed   bool
	jsonRuntimeUsed     bool
	bytesFromArrayLabel int
	processArgsLabel    int
	fsReadTextLabel     int
	fsWriteBytesLabel   int
	hostRuntimeUsed     bool
	inFunction          bool
	currentFunction     string
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
		statementSlots:   map[*Stmt]machineSlot{},
		forIterSlots:     map[*Stmt]machineSlot{},
		forIndexSlots:    map[*Stmt]machineSlot{},
		functionReturns:  map[string]*Type{},
	}
	m.endLabel = m.newLabel()
	m.trapLabel = m.newLabel()
	m.stringConcatLabel = m.newLabel()
	m.arrayAllocLabel = m.newLabel()
	m.arrayPushLabel = m.newLabel()
	m.arrayConcatLabel = m.newLabel()
	m.boxAllocLabel = m.newLabel()
	m.structAllocLabel = m.newLabel()
	m.stringAllocLabel = m.newLabel()
	m.stringCharsLabel = m.newLabel()
	m.stringEqualLabel = m.newLabel()
	m.mapFindLabel = m.newLabel()
	m.mapInsertLabel = m.newLabel()
	m.substringLabel = m.newLabel()
	m.intToStringLabel = m.newLabel()
	m.intFromStringLabel = m.newLabel()
	m.jsonParseLabel = m.newLabel()
	m.jsonSkipLabel = m.newLabel()
	m.jsonKindLabel = m.newLabel()
	m.jsonObjectGetLabel = m.newLabel()
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

func functionKey(f *Function) string {
	if f == nil {
		return "<nil>"
	}
	if f.Module == "" {
		return f.Name
	}
	return f.Module + "::" + f.Name
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

func (m *directMachine) emitArrayIndices(e *Expr) error {
	if len(e.Args) != 1 || e.Args[0].Type == nil || e.Args[0].Type.Kind != TyArray {
		return fmt.Errorf("direct ELF backend array_indices expects one Array argument")
	}
	if err := m.emitExpr(e.Args[0]); err != nil {
		return err
	}
	m.code = append(m.code, 0x50)                   // input array
	m.code = append(m.code, 0x48, 0x8b, 0x04, 0x24) // mov rax, [rsp]
	m.code = append(m.code, 0x48, 0x8b, 0x00)       // mov rax, [rax]
	if err := m.emitArrayAllocCall(); err != nil {
		return err
	}
	m.code = append(m.code, 0x50)             // result array
	m.code = append(m.code, 0x48, 0x31, 0xc9) // xor rcx, rcx
	loop := m.newLabel()
	done := m.newLabel()
	if err := m.bind(loop); err != nil {
		return err
	}
	m.code = append(m.code,
		0x48, 0x8b, 0x54, 0x24, 0x08, // mov rdx, [rsp+8] (input array)
		0x48, 0x3b, 0x0a, // cmp rcx, [rdx]
	)
	if err := m.emitConditionalJump(0x83, done); err != nil { // jae
		return err
	}
	m.code = append(m.code,
		0x48, 0x8b, 0x14, 0x24, // mov rdx, [rsp] (result array)
		0x49, 0x89, 0xc8, // mov r8, rcx
		0x49, 0xc1, 0xe0, 0x03, // shl r8, 3
		0x49, 0x83, 0xc0, 0x08, // add r8, 8
		0x4c, 0x01, 0xc2, // add rdx, r8
		0x48, 0x89, 0x0a, // mov [rdx], rcx
		0x48, 0xff, 0xc1, // inc rcx
	)
	if err := m.emitJump(loop); err != nil {
		return err
	}
	if err := m.bind(done); err != nil {
		return err
	}
	m.code = append(m.code, 0x58, 0x48, 0x83, 0xc4, 0x08) // pop result; drop input
	return nil
}

func (m *directMachine) emitStructAllocCall(fieldCount int) error {
	if fieldCount < 0 {
		return fmt.Errorf("direct ELF backend received a negative struct field count")
	}
	m.emitMoveImmediate(uint64(fieldCount))
	return m.emitArrayAllocCall()
}

func (m *directMachine) emitStructLiteral(e *Expr) error {
	if e.Type == nil || e.Type.Kind != TyStruct || e.Type.Struct == nil {
		return fmt.Errorf("direct ELF backend struct literal has no checked declaration")
	}
	if len(e.Fields) != len(e.Values) || len(e.Fields) != len(e.Type.Struct.Fields) {
		return fmt.Errorf("direct ELF backend struct literal has an invalid field count")
	}
	if err := m.emitStructAllocCall(len(e.Type.Struct.Fields)); err != nil {
		return err
	}
	m.code = append(m.code, 0x50) // keep object pointer while evaluating fields
	for i, name := range e.Fields {
		fieldIndex := -1
		for index, field := range e.Type.Struct.Fields {
			if field.Name == name {
				fieldIndex = index
				break
			}
		}
		if fieldIndex < 0 {
			return fmt.Errorf("direct ELF backend struct literal has unknown field '%s'", name)
		}
		if err := m.emitExpr(e.Values[i]); err != nil {
			return err
		}
		m.code = append(m.code, 0x49, 0x89, 0xc0)       // mov r8, rax
		m.code = append(m.code, 0x48, 0x8b, 0x0c, 0x24) // mov rcx, [rsp]
		m.code = append(m.code, 0x48, 0xba)
		var offset [8]byte
		binary.LittleEndian.PutUint64(offset[:], uint64((fieldIndex+1)*8))
		m.code = append(m.code, offset[:]...)
		m.code = append(m.code, 0x48, 0x01, 0xca, 0x4c, 0x89, 0x02) // add rdx, rcx; mov [rdx], r8
	}
	m.code = append(m.code, 0x58) // pop object pointer
	return nil
}

func (m *directMachine) emitStructField(e *Expr) error {
	if e.Base == nil || e.Base.Type == nil || e.Base.Type.Kind != TyStruct || e.Base.Type.Struct == nil {
		return fmt.Errorf("direct ELF backend field access has no checked struct type")
	}
	fieldIndex := -1
	for index, field := range e.Base.Type.Struct.Fields {
		if field.Name == e.Field {
			fieldIndex = index
			break
		}
	}
	if fieldIndex < 0 {
		return fmt.Errorf("direct ELF backend has no struct field '%s'", e.Field)
	}
	if err := m.emitExpr(e.Base); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x8b, 0x80)
	var offset [4]byte
	binary.LittleEndian.PutUint32(offset[:], uint32((fieldIndex+1)*8))
	m.code = append(m.code, offset[:]...)
	return nil
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

func (m *directMachine) emitStructAllocRuntime() error {
	if err := m.bind(m.structAllocLabel); err != nil {
		return err
	}
	// rdi=count. Allocate count qwords for the boxed struct payload.
	m.code = append(m.code,
		0x48, 0x89, 0xf8, // mov rax, rdi
		0x48, 0xc1, 0xe0, 0x03, // shl rax, 3
	)
	m.emitTrapOnOverflow()
	m.code = append(m.code,
		0x48, 0x89, 0xc6, // mov rsi, rax
		0x48, 0x31, 0xff, // xor rdi, rdi
		0xba, 0x03, 0x00, 0x00, 0x00, // PROT_READ|PROT_WRITE
		0x41, 0xba, 0x22, 0x00, 0x00, 0x00, // MAP_PRIVATE|MAP_ANONYMOUS
		0x41, 0xb8, 0xff, 0xff, 0xff, 0xff, // fd=-1
		0x45, 0x31, 0xc9, // offset=0
		0xb8, 0x09, 0x00, 0x00, 0x00, // mmap
		0x0f, 0x05,
		0x48, 0x85, 0xc0,
	)
	if err := m.emitConditionalJump(0x88, m.trapLabel); err != nil { // js: mmap error
		return err
	}
	m.code = append(m.code, 0xc3)
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

func (m *directMachine) emitJSONSkipRuntime() error {
	if err := m.bind(m.jsonSkipLabel); err != nil {
		return err
	}
	// rdi=String/Json text, rsi=start byte offset. Return the exclusive end
	// offset in rax, or -1 for a truncated value. JSON values are scanned as
	// strings, containers, or primitive tokens; nested braces are ignored while
	// inside quoted strings and escaped bytes consume their following byte.
	start := m.newLabel()
	stringStart := m.newLabel()
	stringLoop := m.newLabel()
	stringEscape := m.newLabel()
	stringEnd := m.newLabel()
	containerStart := m.newLabel()
	containerLoop := m.newLabel()
	containerString := m.newLabel()
	containerEscape := m.newLabel()
	containerEnterString := m.newLabel()
	containerLeaveString := m.newLabel()
	containerOpen := m.newLabel()
	containerClose := m.newLabel()
	primitiveStart := m.newLabel()
	primitiveLoop := m.newLabel()
	primitiveDone := m.newLabel()
	success := m.newLabel()
	failure := m.newLabel()
	whitespace := m.newLabel()

	m.code = append(m.code,
		0x53, 0x55, 0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x41, 0x57,
		0x49, 0x89, 0xfc, // r12=String
		0x4d, 0x8b, 0x2c, 0x24, // r13=length
		0x49, 0x89, 0xf6, // r14=start offset
		0x4d, 0x31, 0xff, // r15=container depth
		0x31, 0xdb, // bl=string mode
	)
	if err := m.emitJump(start); err != nil {
		return err
	}
	if err := m.bind(start); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x39, 0xee) // cmp r14, r13
	if err := m.emitConditionalJump(0x83, failure); err != nil {
		return err
	}
	m.code = append(m.code, 0x4b, 0x0f, 0xb6, 0x44, 0x34, 0x08) // al=byte at offset
	for _, value := range []byte{' ', '\t', '\n', '\r'} {
		m.code = append(m.code, 0x3c, value)
		if err := m.emitConditionalJump(0x84, whitespace); err != nil {
			return err
		}
	}
	m.code = append(m.code, 0x3c, '"')
	if err := m.emitConditionalJump(0x84, stringStart); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, '{')
	if err := m.emitConditionalJump(0x84, containerStart); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, '[')
	if err := m.emitConditionalJump(0x84, containerStart); err != nil {
		return err
	}
	if err := m.emitJump(primitiveStart); err != nil {
		return err
	}

	if err := m.bind(whitespace); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc6)
	if err := m.emitJump(start); err != nil {
		return err
	}

	if err := m.bind(stringStart); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc6)
	if err := m.emitJump(stringLoop); err != nil {
		return err
	}
	if err := m.bind(stringLoop); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x39, 0xee)
	if err := m.emitConditionalJump(0x83, failure); err != nil {
		return err
	}
	m.code = append(m.code, 0x4b, 0x0f, 0xb6, 0x44, 0x34, 0x08, 0x3c, '\\')
	if err := m.emitConditionalJump(0x84, stringEscape); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, '"')
	if err := m.emitConditionalJump(0x84, stringEnd); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc6)
	if err := m.emitJump(stringLoop); err != nil {
		return err
	}
	if err := m.bind(stringEscape); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x83, 0xc6, 0x02)
	if err := m.emitJump(stringLoop); err != nil {
		return err
	}
	if err := m.bind(stringEnd); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc6)
	if err := m.emitJump(success); err != nil {
		return err
	}

	if err := m.bind(containerStart); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc6, 0x41, 0xbf, 0x01, 0x00, 0x00, 0x00, 0x31, 0xdb)
	if err := m.emitJump(containerLoop); err != nil {
		return err
	}
	if err := m.bind(containerLoop); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x39, 0xee)
	if err := m.emitConditionalJump(0x83, failure); err != nil {
		return err
	}
	m.code = append(m.code, 0x4b, 0x0f, 0xb6, 0x44, 0x34, 0x08, 0x84, 0xdb)
	if err := m.emitConditionalJump(0x85, containerString); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, '"')
	if err := m.emitConditionalJump(0x84, containerEnterString); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, '{')
	if err := m.emitConditionalJump(0x84, containerOpen); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, '[')
	if err := m.emitConditionalJump(0x84, containerOpen); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, '}')
	if err := m.emitConditionalJump(0x84, containerClose); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, ']')
	if err := m.emitConditionalJump(0x84, containerClose); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc6)
	if err := m.emitJump(containerLoop); err != nil {
		return err
	}

	if err := m.bind(containerString); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, '\\')
	if err := m.emitConditionalJump(0x84, containerEscape); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, '"')
	if err := m.emitConditionalJump(0x84, containerLeaveString); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc6)
	if err := m.emitJump(containerLoop); err != nil {
		return err
	}
	if err := m.bind(containerEscape); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x83, 0xc6, 0x02)
	if err := m.emitJump(containerLoop); err != nil {
		return err
	}
	if err := m.bind(containerEnterString); err != nil {
		return err
	}
	m.code = append(m.code, 0xb3, 0x01, 0x49, 0xff, 0xc6)
	if err := m.emitJump(containerLoop); err != nil {
		return err
	}
	if err := m.bind(containerLeaveString); err != nil {
		return err
	}
	m.code = append(m.code, 0x31, 0xdb, 0x49, 0xff, 0xc6)
	if err := m.emitJump(containerLoop); err != nil {
		return err
	}
	if err := m.bind(containerOpen); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc7, 0x49, 0xff, 0xc6)
	if err := m.emitJump(containerLoop); err != nil {
		return err
	}
	if err := m.bind(containerClose); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xcf, 0x49, 0xff, 0xc6, 0x4d, 0x85, 0xff)
	if err := m.emitConditionalJump(0x84, success); err != nil {
		return err
	}
	if err := m.emitJump(containerLoop); err != nil {
		return err
	}

	if err := m.bind(primitiveStart); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xf5)
	if err := m.emitJump(primitiveLoop); err != nil {
		return err
	}
	if err := m.bind(primitiveLoop); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x39, 0xee)
	if err := m.emitConditionalJump(0x83, primitiveDone); err != nil {
		return err
	}
	m.code = append(m.code, 0x4b, 0x0f, 0xb6, 0x44, 0x34, 0x08)
	for _, value := range []byte{' ', '\t', '\n', '\r', ',', ']', '}'} {
		m.code = append(m.code, 0x3c, value)
		if err := m.emitConditionalJump(0x84, primitiveDone); err != nil {
			return err
		}
	}
	m.code = append(m.code, 0x49, 0xff, 0xc6)
	if err := m.emitJump(primitiveLoop); err != nil {
		return err
	}
	if err := m.bind(primitiveDone); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xf0, 0x48, 0x39, 0xe8)
	if err := m.emitConditionalJump(0x84, failure); err != nil {
		return err
	}
	if err := m.emitJump(success); err != nil {
		return err
	}

	if err := m.bind(success); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xf0, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3)
	if err := m.bind(failure); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0xc7, 0xc0, 0xff, 0xff, 0xff, 0xff, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3)
	return nil
}

func (m *directMachine) emitJSONParseRuntime() error {
	if err := m.bind(m.jsonParseLabel); err != nil {
		return err
	}
	// Json uses the same immutable {length, bytes} layout as String. Parsing
	// validates one complete value and preserves the source pointer on success;
	// accessors later return freshly allocated slices for nested values.
	valueEnd := m.newLabel()
	trailingLoop := m.newLabel()
	trailingWhitespace := m.newLabel()
	success := m.newLabel()
	failure := m.newLabel()
	m.code = append(m.code, 0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x49, 0x89, 0xfc, 0x4d, 0x8b, 0x2c, 0x24)
	m.code = append(m.code, 0x48, 0x31, 0xf6)
	if err := m.emitLabelCall(m.jsonSkipLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x83, 0xf8, 0xff)
	if err := m.emitConditionalJump(0x84, failure); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x89, 0xc6)
	if err := m.emitJump(valueEnd); err != nil {
		return err
	}
	if err := m.bind(valueEnd); err != nil {
		return err
	}
	if err := m.bind(trailingLoop); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x39, 0xee)
	if err := m.emitConditionalJump(0x84, success); err != nil {
		return err
	}
	m.code = append(m.code, 0x4b, 0x0f, 0xb6, 0x44, 0x34, 0x08)
	for _, value := range []byte{' ', '\t', '\n', '\r'} {
		m.code = append(m.code, 0x3c, value)
		if err := m.emitConditionalJump(0x84, trailingWhitespace); err != nil {
			return err
		}
	}
	if err := m.emitJump(failure); err != nil {
		return err
	}
	if err := m.bind(trailingWhitespace); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc6)
	if err := m.emitJump(trailingLoop); err != nil {
		return err
	}
	if err := m.bind(success); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xe0)
	if err := m.emitBoxCall(0); err != nil {
		return err
	}
	m.code = append(m.code, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0xc3)
	if err := m.bind(failure); err != nil {
		return err
	}
	m.emitStringAddress("invalid JSON")
	if err := m.emitBoxCall(1); err != nil {
		return err
	}
	m.code = append(m.code, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0xc3)
	return nil
}

func (m *directMachine) emitJSONKindRuntime() error {
	if err := m.bind(m.jsonKindLabel); err != nil {
		return err
	}
	// rdi points to the validated Json/String text. Return the stable kind name
	// as an immutable String object without allocating a second copy.
	object := m.newLabel()
	array := m.newLabel()
	stringValue := m.newLabel()
	boolean := m.newLabel()
	null := m.newLabel()
	number := m.newLabel()
	invalid := m.newLabel()
	loop := m.newLabel()
	m.code = append(m.code, 0x48, 0x31, 0xc9, 0x48, 0x8b, 0x17) // rcx=index, rdx=length
	if err := m.bind(loop); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x39, 0xd1)
	if err := m.emitConditionalJump(0x83, invalid); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x8d, 0x44, 0x0f, 0x08, 0x0f, 0xb6, 0x00)
	for _, value := range []byte{' ', '\t', '\n', '\r'} {
		m.code = append(m.code, 0x3c, value)
		if err := m.emitConditionalJump(0x84, loop); err != nil {
			return err
		}
	}
	m.code = append(m.code, 0x3c, '{')
	if err := m.emitConditionalJump(0x84, object); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, '[')
	if err := m.emitConditionalJump(0x84, array); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, '"')
	if err := m.emitConditionalJump(0x84, stringValue); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, 't')
	if err := m.emitConditionalJump(0x84, boolean); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, 'f')
	if err := m.emitConditionalJump(0x84, boolean); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, 'n')
	if err := m.emitConditionalJump(0x84, null); err != nil {
		return err
	}
	if err := m.emitJump(number); err != nil {
		return err
	}

	for _, item := range []struct {
		label int
		text  string
	}{
		{object, "object"},
		{array, "array"},
		{stringValue, "string"},
		{boolean, "bool"},
		{null, "null"},
		{number, "number"},
		{invalid, "invalid"},
	} {
		if err := m.bind(item.label); err != nil {
			return err
		}
		m.emitStringAddress(item.text)
		m.code = append(m.code, 0xc3)
	}
	return nil
}

func (m *directMachine) emitJSONObjectGetRuntime() error {
	if err := m.bind(m.jsonObjectGetLabel); err != nil {
		return err
	}
	// rdi=Json object text, rsi=String key. Return Result[Json,String]. The
	// returned Json is a freshly allocated String slice containing the selected
	// value, so nested access never aliases a temporary parser buffer.
	start := m.newLabel()
	objectLoop := m.newLabel()
	keyLoop := m.newLabel()
	keyEscape := m.newLabel()
	keyEnd := m.newLabel()
	keyMismatch := m.newLabel()
	keyAdvance := m.newLabel()
	afterKey := m.newLabel()
	keyLengthMismatch := m.newLabel()
	valueStart := m.newLabel()
	afterValue := m.newLabel()
	found := m.newLabel()
	notFound := m.newLabel()
	typeError := m.newLabel()
	failure := m.newLabel()
	whitespace := m.newLabel()
	afterKeyWhitespace := m.newLabel()
	afterKeyWhitespaceNext := m.newLabel()
	afterValueWhitespace := m.newLabel()
	afterValueWhitespaceNext := m.newLabel()
	valueWhitespaceNext := m.newLabel()
	objectWhitespace := m.newLabel()
	nextEntry := m.newLabel()

	m.code = append(m.code,
		0x53, 0x55, 0x41, 0x54, 0x41, 0x55, 0x41, 0x56, 0x41, 0x57,
		0x49, 0x89, 0xfc, // r12=Json text
		0x49, 0x89, 0xf5, // r13=key String
		0x4d, 0x8b, 0x34, 0x24, // r14=Json length
		0x4d, 0x31, 0xff, // r15=cursor
	)
	if err := m.emitJump(start); err != nil {
		return err
	}
	if err := m.bind(start); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x39, 0xfe)
	if err := m.emitConditionalJump(0x86, failure); err != nil {
		return err
	}
	m.code = append(m.code, 0x4b, 0x0f, 0xb6, 0x44, 0x3c, 0x08)
	for _, value := range []byte{' ', '\t', '\n', '\r'} {
		m.code = append(m.code, 0x3c, value)
		if err := m.emitConditionalJump(0x84, whitespace); err != nil {
			return err
		}
	}
	m.code = append(m.code, 0x3c, '{')
	if err := m.emitConditionalJump(0x85, typeError); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc7)
	if err := m.emitJump(objectLoop); err != nil {
		return err
	}

	if err := m.bind(objectWhitespace); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc7)
	if err := m.emitJump(objectLoop); err != nil {
		return err
	}

	if err := m.bind(whitespace); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc7)
	if err := m.emitJump(start); err != nil {
		return err
	}

	if err := m.bind(objectLoop); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x39, 0xfe)
	if err := m.emitConditionalJump(0x86, failure); err != nil {
		return err
	}
	m.code = append(m.code, 0x4b, 0x0f, 0xb6, 0x44, 0x3c, 0x08)
	for _, value := range []byte{' ', '\t', '\n', '\r'} {
		m.code = append(m.code, 0x3c, value)
		if err := m.emitConditionalJump(0x84, objectWhitespace); err != nil {
			return err
		}
	}
	m.code = append(m.code, 0x3c, '}')
	if err := m.emitConditionalJump(0x84, notFound); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, '"')
	if err := m.emitConditionalJump(0x85, failure); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc7, 0x41, 0xb9, 0x01, 0x00, 0x00, 0x00, 0x45, 0x31, 0xc0) // cursor++, match=true, key index=0
	if err := m.emitJump(keyLoop); err != nil {
		return err
	}

	if err := m.bind(keyLoop); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x39, 0xfe)
	if err := m.emitConditionalJump(0x86, failure); err != nil {
		return err
	}
	m.code = append(m.code, 0x4b, 0x0f, 0xb6, 0x44, 0x3c, 0x08)
	m.code = append(m.code, 0x3c, '\\')
	if err := m.emitConditionalJump(0x84, keyEscape); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, '"')
	if err := m.emitConditionalJump(0x84, keyEnd); err != nil {
		return err
	}
	m.code = append(m.code, 0x45, 0x85, 0xc9)
	if err := m.emitConditionalJump(0x84, keyAdvance); err != nil {
		return err
	}
	// Compare the unescaped key byte with the requested String key. The
	// source compiler emits identifier keys without escapes; escaped keys are
	// rejected as a mismatch rather than being silently decoded incorrectly.
	m.code = append(m.code, 0x4d, 0x89, 0xea, 0x4d, 0x8b, 0x12, 0x4d, 0x39, 0xd0)
	if err := m.emitConditionalJump(0x83, keyMismatch); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x89, 0xea, 0x4d, 0x01, 0xc2, 0x41, 0x0f, 0xb6, 0x4a, 0x08, 0x38, 0xc8)
	if err := m.emitConditionalJump(0x85, keyMismatch); err != nil {
		return err
	}
	if err := m.emitJump(keyAdvance); err != nil {
		return err
	}

	if err := m.bind(keyMismatch); err != nil {
		return err
	}
	m.code = append(m.code, 0x45, 0x31, 0xc9)
	if err := m.emitJump(keyAdvance); err != nil {
		return err
	}
	if err := m.bind(keyEscape); err != nil {
		return err
	}
	m.code = append(m.code, 0x45, 0x31, 0xc9, 0x49, 0x83, 0xc7, 0x02)
	if err := m.emitJump(keyLoop); err != nil {
		return err
	}
	if err := m.bind(keyAdvance); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc7, 0x49, 0xff, 0xc0)
	if err := m.emitJump(keyLoop); err != nil {
		return err
	}

	if err := m.bind(keyEnd); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc7, 0x45, 0x85, 0xc9)
	if err := m.emitConditionalJump(0x84, afterKey); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x89, 0xea, 0x4d, 0x8b, 0x12, 0x4d, 0x39, 0xd0)
	if err := m.emitConditionalJump(0x85, keyLengthMismatch); err != nil {
		return err
	}
	if err := m.emitJump(afterKey); err != nil {
		return err
	}

	if err := m.bind(keyLengthMismatch); err != nil {
		return err
	}
	m.code = append(m.code, 0x45, 0x31, 0xc9)
	if err := m.emitJump(afterKey); err != nil {
		return err
	}

	if err := m.bind(afterKey); err != nil {
		return err
	}
	if err := m.emitJump(afterKeyWhitespace); err != nil {
		return err
	}
	if err := m.bind(afterKeyWhitespace); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x39, 0xfe)
	if err := m.emitConditionalJump(0x86, failure); err != nil {
		return err
	}
	m.code = append(m.code, 0x4b, 0x0f, 0xb6, 0x44, 0x3c, 0x08)
	for _, value := range []byte{' ', '\t', '\n', '\r'} {
		m.code = append(m.code, 0x3c, value)
		if err := m.emitConditionalJump(0x84, afterKeyWhitespaceNext); err != nil {
			return err
		}
	}
	m.code = append(m.code, 0x3c, ':')
	if err := m.emitConditionalJump(0x85, failure); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc7)
	if err := m.emitJump(valueStart); err != nil {
		return err
	}
	if err := m.bind(afterKeyWhitespaceNext); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc7)
	if err := m.emitJump(afterKeyWhitespace); err != nil {
		return err
	}

	if err := m.bind(valueStart); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x39, 0xfe)
	if err := m.emitConditionalJump(0x86, failure); err != nil {
		return err
	}
	m.code = append(m.code, 0x4b, 0x0f, 0xb6, 0x44, 0x3c, 0x08)
	for _, value := range []byte{' ', '\t', '\n', '\r'} {
		m.code = append(m.code, 0x3c, value)
		if err := m.emitConditionalJump(0x84, valueWhitespaceNext); err != nil {
			return err
		}
	}
	m.code = append(m.code, 0x4c, 0x89, 0xfd)                   // rbp=value start
	m.code = append(m.code, 0x4c, 0x89, 0xe7, 0x4c, 0x89, 0xfe) // rdi=Json, rsi=value offset
	if err := m.emitLabelCall(m.jsonSkipLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x83, 0xf8, 0xff)
	if err := m.emitConditionalJump(0x84, failure); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0xc3) // rbx=value end
	m.code = append(m.code, 0x45, 0x85, 0xc9) // key matched?
	if err := m.emitConditionalJump(0x85, found); err != nil {
		return err
	}
	if err := m.emitJump(afterValue); err != nil {
		return err
	}

	if err := m.bind(afterValue); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0x89, 0xdf)
	if err := m.emitJump(afterValueWhitespace); err != nil {
		return err
	}
	if err := m.bind(afterValueWhitespaceNext); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc7)
	if err := m.emitJump(afterValueWhitespace); err != nil {
		return err
	}
	if err := m.bind(valueWhitespaceNext); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc7)
	if err := m.emitJump(valueStart); err != nil {
		return err
	}
	if err := m.bind(afterValueWhitespace); err != nil {
		return err
	}
	m.code = append(m.code, 0x4d, 0x39, 0xfe)
	if err := m.emitConditionalJump(0x86, failure); err != nil {
		return err
	}
	m.code = append(m.code, 0x4b, 0x0f, 0xb6, 0x44, 0x3c, 0x08)
	for _, value := range []byte{' ', '\t', '\n', '\r'} {
		m.code = append(m.code, 0x3c, value)
		if err := m.emitConditionalJump(0x84, afterValueWhitespaceNext); err != nil {
			return err
		}
	}
	m.code = append(m.code, 0x3c, ',')
	if err := m.emitConditionalJump(0x84, nextEntry); err != nil {
		return err
	}
	m.code = append(m.code, 0x3c, '}')
	if err := m.emitConditionalJump(0x84, notFound); err != nil {
		return err
	}
	if err := m.emitJump(failure); err != nil {
		return err
	}
	if err := m.bind(nextEntry); err != nil {
		return err
	}
	m.code = append(m.code, 0x49, 0xff, 0xc7) // consume ','
	if err := m.emitJump(objectLoop); err != nil {
		return err
	}

	if err := m.bind(found); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x89, 0xe2, 0x48, 0x01, 0xea, 0x48, 0x8d, 0x7a, 0x08, 0x48, 0x89, 0xde, 0x48, 0x29, 0xee)
	if err := m.emitLabelCall(m.stringAllocLabel); err != nil {
		return err
	}
	if err := m.emitBoxCall(0); err != nil {
		return err
	}
	m.code = append(m.code, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3)

	if err := m.bind(notFound); err != nil {
		return err
	}
	m.emitStringAddress("JSON object key not found")
	if err := m.emitBoxCall(1); err != nil {
		return err
	}
	m.code = append(m.code, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3)
	if err := m.bind(typeError); err != nil {
		return err
	}
	m.emitStringAddress("JSON value is not an object")
	if err := m.emitBoxCall(1); err != nil {
		return err
	}
	m.code = append(m.code, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3)
	if err := m.bind(failure); err != nil {
		return err
	}
	m.emitStringAddress("JSON object access failed")
	if err := m.emitBoxCall(1); err != nil {
		return err
	}
	m.code = append(m.code, 0x41, 0x5f, 0x41, 0x5e, 0x41, 0x5d, 0x41, 0x5c, 0x5d, 0x5b, 0xc3)
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
		0x48, 0x31, 0xf6,
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

func (m *directMachine) lookupSlot(name string) (machineSlot, bool) {
	for index := len(m.scopeStack) - 1; index >= 0; index-- {
		if slot, ok := m.scopeStack[index][name]; ok {
			return slot, true
		}
	}
	return machineSlot{}, false
}

func (m *directMachine) pushScope() {
	m.scopeStack = append(m.scopeStack, map[string]machineSlot{})
}

func (m *directMachine) popScope() {
	if len(m.scopeStack) > 0 {
		m.scopeStack = m.scopeStack[:len(m.scopeStack)-1]
	}
}

func (m *directMachine) bindSlot(name string, slot machineSlot) {
	if len(m.scopeStack) == 0 {
		m.pushScope()
	}
	m.scopeStack[len(m.scopeStack)-1][name] = slot
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

func machineValueTypeSupported(t *Type) bool {
	if t == nil || t.Kind == TyNil || t.Kind == TyVoid || t.Kind == TyError || t.Kind == TyUnknown {
		return false
	}
	switch t.Kind {
	case TyInt, TyUInt, TyBool, TyString, TyBytes, TyFloat, TyJSON, TyStruct,
		TyArray, TyOption, TyResult, TyMap:
		return true
	default:
		return false
	}
}

func machineTypeFromSpec(p *Program, spec *TypeSpec) (*Type, bool) {
	name := typeSpecString(spec)
	if typ, ok := machineScalarType(name); ok {
		return typ, true
	}
	if p != nil {
		for _, decl := range p.Structs {
			if decl != nil && decl.Name == name {
				return &Type{Kind: TyStruct, Name: name, Struct: decl}, true
			}
		}
	}
	return nil, false
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
	return m.emitCall(functionKey(e.Function))
}

func machineBits(t *Type) uint8 {
	if t != nil && t.Kind == TyUInt {
		return t.Bits
	}
	return 0
}

func (m *directMachine) emitAssert(e *Expr, equal bool) error {
	if equal {
		if len(e.Args) != 2 || e.Args[0].Type == nil || e.Args[1].Type == nil || e.Args[0].Type.Kind != e.Args[1].Type.Kind {
			return fmt.Errorf("direct ELF assert_eq expects two values of the same scalar type")
		}
		kind := e.Args[0].Type.Kind
		if kind != TyInt && kind != TyUInt && kind != TyBool {
			return fmt.Errorf("direct ELF assert_eq supports Int, UInt, and Bool values")
		}
		if err := m.emitExpr(e.Args[0]); err != nil {
			return err
		}
		m.code = append(m.code, 0x50)
		if err := m.emitExpr(e.Args[1]); err != nil {
			return err
		}
		m.code = append(m.code, 0x48, 0x89, 0xc1, 0x58, 0x48, 0x39, 0xc8)
		if err := m.emitConditionalJump(0x85, m.trapLabel); err != nil { // jne: assertion failed
			return err
		}
	} else {
		if len(e.Args) != 1 || e.Args[0].Type == nil || e.Args[0].Type.Kind != TyBool {
			return fmt.Errorf("direct ELF assert expects one Bool value")
		}
		if err := m.emitExpr(e.Args[0]); err != nil {
			return err
		}
		m.code = append(m.code, 0x48, 0x85, 0xc0)
		if err := m.emitConditionalJump(0x84, m.trapLabel); err != nil { // je: assertion failed
			return err
		}
	}
	m.emitMoveImmediate(0)
	return nil
}

func (m *directMachine) emitStringPredicate(e *Expr, mode string) error {
	if len(e.Args) != 2 || e.Args[0].Type == nil || e.Args[1].Type == nil || e.Args[0].Type.Kind != TyString || e.Args[1].Type.Kind != TyString {
		return fmt.Errorf("direct ELF %s expects two String arguments", mode)
	}
	if err := m.emitExpr(e.Args[0]); err != nil {
		return err
	}
	m.code = append(m.code, 0x50)
	if err := m.emitExpr(e.Args[1]); err != nil {
		return err
	}
	// r8=haystack, r9=needle, r10=haystack length, r11=needle length.
	m.code = append(m.code, 0x48, 0x89, 0xc1, 0x58, 0x49, 0x89, 0xc0, 0x49, 0x89, 0xc9, 0x4d, 0x8b, 0x10, 0x4d, 0x8b, 0x19)
	falseLabel := m.newLabel()
	foundLabel := m.newLabel()
	doneLabel := m.newLabel()
	if mode == "contains" || mode == "starts_with" || mode == "ends_with" {
		m.code = append(m.code, 0x4d, 0x39, 0xda)                       // cmp r10, r11
		if err := m.emitConditionalJump(0x82, falseLabel); err != nil { // jb: needle longer than haystack
			return err
		}
	}
	if mode == "ends_with" {
		m.code = append(m.code, 0x4c, 0x89, 0xd2, 0x4c, 0x29, 0xda) // rdx = haystack length - needle length
	} else {
		m.code = append(m.code, 0x48, 0x31, 0xd2) // rdx = 0
	}
	innerLabel := m.newLabel()
	mismatchLabel := m.newLabel()
	outerLabel := innerLabel
	if mode == "contains" {
		outerLabel = m.newLabel()
		if err := m.bind(outerLabel); err != nil {
			return err
		}
		m.code = append(m.code, 0x4c, 0x89, 0xd0, 0x4c, 0x29, 0xd8, 0x48, 0x39, 0xc2) // compare index with last valid start
		if err := m.emitConditionalJump(0x87, falseLabel); err != nil {               // ja: index beyond last valid start
			return err
		}
	}
	m.code = append(m.code, 0x48, 0x31, 0xf6) // rsi=0 for this candidate start
	if err := m.bind(innerLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x4c, 0x39, 0xde)                       // compare with needle length
	if err := m.emitConditionalJump(0x84, foundLabel); err != nil { // je: all bytes matched
		return err
	}
	m.code = append(m.code,
		0x48, 0x89, 0xd7, 0x48, 0x01, 0xf7,
		0x49, 0x0f, 0xb6, 0x44, 0x38, 0x08,
		0x41, 0x0f, 0xb6, 0x4c, 0x31, 0x08,
		0x39, 0xc8,
	)
	if err := m.emitConditionalJump(0x85, mismatchLabel); err != nil { // jne: byte mismatch
		return err
	}
	m.code = append(m.code, 0x48, 0xff, 0xc6)
	if err := m.emitJump(innerLabel); err != nil {
		return err
	}
	if err := m.bind(mismatchLabel); err != nil {
		return err
	}
	if mode == "contains" {
		m.code = append(m.code, 0x48, 0xff, 0xc2)
		if err := m.emitJump(outerLabel); err != nil {
			return err
		}
	} else {
		if err := m.emitJump(falseLabel); err != nil {
			return err
		}
	}
	if err := m.bind(foundLabel); err != nil {
		return err
	}
	m.emitMoveImmediate(1)
	if err := m.emitJump(doneLabel); err != nil {
		return err
	}
	if err := m.bind(falseLabel); err != nil {
		return err
	}
	m.emitMoveImmediate(0)
	return m.bind(doneLabel)
}

func directMapKeyKind(t *Type) (uint64, error) {
	if t == nil {
		return 0, fmt.Errorf("direct ELF map key has no checked type")
	}
	switch t.Kind {
	case TyString:
		return 1, nil
	case TyInt, TyUInt, TyBool, TyEnum:
		return 0, nil
	default:
		return 0, fmt.Errorf("direct ELF backend supports Map keys of String, Int, UInt, Bool, or enum type")
	}
}

func (m *directMachine) emitMapLiteral(e *Expr) error {
	if e.Type == nil || e.Type.Kind != TyMap {
		return fmt.Errorf("direct ELF backend map literal has no checked map type")
	}
	if len(e.MapKeys) != len(e.Values) {
		return fmt.Errorf("direct ELF backend map literal has mismatched key/value counts")
	}
	if _, err := directMapKeyKind(e.Type.A); err != nil {
		return err
	}
	m.emitMoveImmediate(uint64(len(e.MapKeys) * 2))
	m.arrayRuntimeUsed = true
	m.mapRuntimeUsed = true
	if err := m.emitArrayAllocCall(); err != nil {
		return err
	}
	m.code = append(m.code, 0x50) // keep map pointer while evaluating entries
	for index := range e.MapKeys {
		if err := m.emitExpr(e.MapKeys[index]); err != nil {
			return err
		}
		m.code = append(m.code, 0x50) // key
		if err := m.emitExpr(e.Values[index]); err != nil {
			return err
		}
		m.code = append(m.code,
			0x49, 0x89, 0xc0, // r8=value
			0x58,                   // rax=key
			0x48, 0x8b, 0x0c, 0x24, // rcx=map
			0x48, 0xba,
		)
		var offset [8]byte
		binary.LittleEndian.PutUint64(offset[:], uint64(8+index*16))
		m.code = append(m.code, offset[:]...)
		m.code = append(m.code,
			0x48, 0x01, 0xca, // rdx += map
			0x48, 0x89, 0x02, // map[key]
			0x48, 0x83, 0xc2, 0x08,
			0x4c, 0x89, 0x02, // map[value]
		)
	}
	m.code = append(m.code, 0x58)
	return nil
}

func (m *directMachine) emitMapKeyImmediate(kind uint64) {
	m.code = append(m.code, 0x48, 0xba)
	var value [8]byte
	binary.LittleEndian.PutUint64(value[:], kind)
	m.code = append(m.code, value[:]...)
}

func (m *directMachine) emitMapGet(e *Expr) error {
	if len(e.Args) != 2 || e.Args[0].Type == nil || e.Args[0].Type.Kind != TyMap {
		return fmt.Errorf("direct ELF backend map_get expects Map[K,V] and K")
	}
	kind, err := directMapKeyKind(e.Args[0].Type.A)
	if err != nil {
		return err
	}
	if err := m.emitExpr(e.Args[0]); err != nil {
		return err
	}
	m.code = append(m.code, 0x50)
	if err := m.emitExpr(e.Args[1]); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0xc6, 0x5f) // rsi=key; rdi=map
	m.emitMapKeyImmediate(kind)
	m.mapRuntimeUsed = true
	if err := m.emitLabelCall(m.mapFindLabel); err != nil {
		return err
	}
	none := m.newLabel()
	done := m.newLabel()
	m.code = append(m.code, 0x48, 0x85, 0xc0)
	if err := m.emitConditionalJump(0x84, none); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x8b, 0x00)
	if err := m.emitBoxCall(1); err != nil {
		return err
	}
	if err := m.emitJump(done); err != nil {
		return err
	}
	if err := m.bind(none); err != nil {
		return err
	}
	m.emitMoveImmediate(0)
	return m.bind(done)
}

func (m *directMachine) emitMapContains(e *Expr) error {
	if len(e.Args) != 2 || e.Args[0].Type == nil || e.Args[0].Type.Kind != TyMap {
		return fmt.Errorf("direct ELF backend map_contains_key expects Map[K,V] and K")
	}
	kind, err := directMapKeyKind(e.Args[0].Type.A)
	if err != nil {
		return err
	}
	if err := m.emitExpr(e.Args[0]); err != nil {
		return err
	}
	m.code = append(m.code, 0x50)
	if err := m.emitExpr(e.Args[1]); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x89, 0xc6, 0x5f)
	m.emitMapKeyImmediate(kind)
	m.mapRuntimeUsed = true
	if err := m.emitLabelCall(m.mapFindLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x85, 0xc0, 0x0f, 0x95, 0xc0, 0x48, 0x0f, 0xb6, 0xc0)
	return nil
}

func (m *directMachine) emitMapInsert(e *Expr) error {
	if len(e.Args) != 3 || e.Args[0].Type == nil || e.Args[0].Type.Kind != TyMap {
		return fmt.Errorf("direct ELF backend map_insert expects Map[K,V], K, and V")
	}
	kind, err := directMapKeyKind(e.Args[0].Type.A)
	if err != nil {
		return err
	}
	if err := m.emitExpr(e.Args[0]); err != nil {
		return err
	}
	m.code = append(m.code, 0x50)
	if err := m.emitExpr(e.Args[1]); err != nil {
		return err
	}
	m.code = append(m.code, 0x50)
	if err := m.emitExpr(e.Args[2]); err != nil {
		return err
	}
	m.code = append(m.code,
		0x48, 0x89, 0xc1, // rcx=value
		0x5e, // rsi=key
		0x5f, // rdi=map
	)
	m.emitMapKeyImmediate(kind)
	m.arrayRuntimeUsed = true
	m.mapRuntimeUsed = true
	return m.emitLabelCall(m.mapInsertLabel)
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
	case ExStruct:
		return m.emitStructLiteral(e)
	case ExArray:
		return m.emitArrayLiteral(e)
	case ExMap:
		return m.emitMapLiteral(e)
	case ExVar:
		slot, ok := m.lookupSlot(e.Name)
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
	case ExField:
		return m.emitStructField(e)
	case ExCall:
		if e.Function != nil {
			return m.emitFunctionCall(e)
		}
		if e.Receiver != nil {
			return fmt.Errorf("direct ELF backend does not support receiver call %s", e.Name)
		}
		switch e.Name {
		case "assert":
			return m.emitAssert(e, false)
		case "assert_eq":
			return m.emitAssert(e, true)
		case "contains", "starts_with", "ends_with":
			return m.emitStringPredicate(e, e.Name)
		case "string_chars":
			if len(e.Args) != 1 || e.Args[0].Type == nil || e.Args[0].Type.Kind != TyString {
				return fmt.Errorf("direct ELF string_chars expects one String argument")
			}
			if err := m.emitExpr(e.Args[0]); err != nil {
				return err
			}
			m.code = append(m.code, 0x48, 0x89, 0xc7) // rdi=String
			m.hostRuntimeUsed = true
			m.arrayRuntimeUsed = true
			m.stringCharsUsed = true
			return m.emitLabelCall(m.stringCharsLabel)
		case "substring":
			if len(e.Args) != 3 || e.Args[0].Type == nil || e.Args[0].Type.Kind != TyString || e.Args[1].Type == nil || e.Args[1].Type.Kind != TyInt || e.Args[2].Type == nil || e.Args[2].Type.Kind != TyInt {
				return fmt.Errorf("direct ELF substring expects String, Int, and Int arguments")
			}
			if err := m.emitExpr(e.Args[0]); err != nil {
				return err
			}
			m.code = append(m.code, 0x50)
			if err := m.emitExpr(e.Args[1]); err != nil {
				return err
			}
			m.code = append(m.code, 0x50)
			if err := m.emitExpr(e.Args[2]); err != nil {
				return err
			}
			m.code = append(m.code, 0x48, 0x89, 0xc2, 0x5e, 0x5f) // rdx=length, rsi=start, rdi=String
			m.hostRuntimeUsed = true
			m.substringUsed = true
			return m.emitLabelCall(m.substringLabel)
		case "str":
			if len(e.Args) != 1 || e.Args[0].Type == nil {
				return fmt.Errorf("direct ELF str expects one Display argument")
			}
			argument := e.Args[0]
			if argument.Type.Kind == TyString {
				return m.emitExpr(argument)
			}
			kind := uint64(0)
			switch argument.Type.Kind {
			case TyInt:
				kind = 0
			case TyUInt:
				kind = 1
			case TyBool:
				kind = 2
			default:
				return fmt.Errorf("direct ELF backend str does not support %s", argument.Type.String())
			}
			if err := m.emitExpr(argument); err != nil {
				return err
			}
			m.code = append(m.code, 0x48, 0x89, 0xc7, 0x48, 0xbe)
			var displayKind [8]byte
			binary.LittleEndian.PutUint64(displayKind[:], kind)
			m.code = append(m.code, displayKind[:]...)
			m.hostRuntimeUsed = true
			m.intToStringUsed = true
			return m.emitLabelCall(m.intToStringLabel)
		case "int":
			if len(e.Args) != 1 || e.Args[0].Type == nil {
				return fmt.Errorf("direct ELF int expects one numeric, Bool, or String argument")
			}
			argument := e.Args[0]
			switch argument.Type.Kind {
			case TyInt, TyBool:
				return m.emitExpr(argument)
			case TyUInt:
				if err := m.emitExpr(argument); err != nil {
					return err
				}
				m.code = append(m.code, 0x48, 0x85, 0xc0) // reject UInt values above Int max
				if err := m.emitConditionalJump(0x88, m.trapLabel); err != nil {
					return err
				}
				return nil
			case TyString:
				if err := m.emitExpr(argument); err != nil {
					return err
				}
				m.code = append(m.code, 0x48, 0x89, 0xc7) // rdi=String
				m.hostRuntimeUsed = true
				m.intFromStringUsed = true
				return m.emitLabelCall(m.intFromStringLabel)
			default:
				return fmt.Errorf("direct ELF backend int does not support %s", argument.Type.String())
			}
		case "json_parse":
			if len(e.Args) != 1 || e.Args[0].Type == nil || e.Args[0].Type.Kind != TyString {
				return fmt.Errorf("direct ELF json_parse expects one String argument")
			}
			if err := m.emitExpr(e.Args[0]); err != nil {
				return err
			}
			m.code = append(m.code, 0x48, 0x89, 0xc7) // rdi=JSON text String
			m.hostRuntimeUsed = true
			m.jsonRuntimeUsed = true
			return m.emitLabelCall(m.jsonParseLabel)
		case "json_kind":
			if len(e.Args) != 1 || e.Args[0].Type == nil || e.Args[0].Type.Kind != TyJSON {
				return fmt.Errorf("direct ELF json_kind expects one Json argument")
			}
			if err := m.emitExpr(e.Args[0]); err != nil {
				return err
			}
			m.code = append(m.code, 0x48, 0x89, 0xc7) // rdi=Json text
			m.hostRuntimeUsed = true
			m.jsonRuntimeUsed = true
			return m.emitLabelCall(m.jsonKindLabel)
		case "json_object_get":
			if len(e.Args) != 2 || e.Args[0].Type == nil || e.Args[0].Type.Kind != TyJSON || e.Args[1].Type == nil || e.Args[1].Type.Kind != TyString {
				return fmt.Errorf("direct ELF json_object_get expects (Json, String)")
			}
			if err := m.emitExpr(e.Args[0]); err != nil {
				return err
			}
			m.code = append(m.code, 0x50) // preserve Json while evaluating key
			if err := m.emitExpr(e.Args[1]); err != nil {
				return err
			}
			m.code = append(m.code, 0x48, 0x89, 0xc6, 0x58, 0x48, 0x89, 0xc7) // rsi=key, rdi=Json
			m.hostRuntimeUsed = true
			m.jsonRuntimeUsed = true
			return m.emitLabelCall(m.jsonObjectGetLabel)
		case "len":
			if len(e.Args) != 1 || e.Args[0].Type == nil || (e.Args[0].Type.Kind != TyArray && e.Args[0].Type.Kind != TyBytes) {
				return fmt.Errorf("direct ELF backend supports len(Array[T]) and len(Bytes)")
			}
			if err := m.emitExpr(e.Args[0]); err != nil {
				return err
			}
			m.code = append(m.code, 0x48, 0x8b, 0x00) // mov rax, [rax]
			return nil
		case "bytes":
			if len(e.Args) != 1 || e.Args[0].Type == nil || e.Args[0].Type.Kind != TyArray {
				return fmt.Errorf("direct ELF backend bytes expects Array[Int]")
			}
			if err := m.emitExpr(e.Args[0]); err != nil {
				return err
			}
			m.code = append(m.code, 0x48, 0x89, 0xc7) // mov rdi, rax
			m.hostRuntimeUsed = true
			return m.emitLabelCall(m.bytesFromArrayLabel)
		case "map_get":
			return m.emitMapGet(e)
		case "map_contains_key":
			return m.emitMapContains(e)
		case "map_insert":
			return m.emitMapInsert(e)
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
		case "array_indices":
			return m.emitArrayIndices(e)
		case "process_args":
			if len(e.Args) != 0 {
				return fmt.Errorf("direct ELF backend process_args expects no arguments")
			}
			m.hostRuntimeUsed = true
			return m.emitLabelCall(m.processArgsLabel)
		case "fs_read_text":
			if len(e.Args) != 1 {
				return fmt.Errorf("direct ELF backend fs_read_text expects one argument")
			}
			if err := m.emitExpr(e.Args[0]); err != nil {
				return err
			}
			m.code = append(m.code, 0x48, 0x89, 0xc7) // mov rdi, rax
			m.hostRuntimeUsed = true
			return m.emitLabelCall(m.fsReadTextLabel)
		case "fs_write_bytes":
			if len(e.Args) != 2 {
				return fmt.Errorf("direct ELF backend fs_write_bytes expects two arguments")
			}
			if err := m.emitExpr(e.Args[0]); err != nil {
				return err
			}
			m.code = append(m.code, 0x50) // path pointer
			if err := m.emitExpr(e.Args[1]); err != nil {
				return err
			}
			m.code = append(m.code, 0x48, 0x89, 0xc6, 0x5f) // rsi=bytes; pop rdi=path
			m.hostRuntimeUsed = true
			return m.emitLabelCall(m.fsWriteBytesLabel)
		case "u8", "u16", "u32", "u64":
			if len(e.Args) != 1 {
				return fmt.Errorf("direct ELF backend conversion %s expects one argument", e.Name)
			}
			if err := m.emitExpr(e.Args[0]); err != nil {
				return err
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
		case StFor:
			return true
		case StUnsafe, StDefer:
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

func directHasStructuredFeatureExpr(e *Expr) bool {
	if e == nil {
		return false
	}
	if e.Kind == ExStruct || e.Kind == ExField {
		return true
	}
	if e.Type != nil {
		switch e.Type.Kind {
		case TyStruct, TyMap, TyJSON, TyBytes, TyOption, TyResult:
			return true
		}
	}
	if e.Kind == ExCall {
		switch e.Name {
		case "assert", "assert_eq", "contains", "starts_with", "ends_with", "u8", "u16", "u32", "u64":
			return true
		}
	}
	if directHasStructuredFeatureExpr(e.Left) || directHasStructuredFeatureExpr(e.Right) || directHasStructuredFeatureExpr(e.Operand) || directHasStructuredFeatureExpr(e.Base) || directHasStructuredFeatureExpr(e.Receiver) {
		return true
	}
	for _, argument := range e.Args {
		if directHasStructuredFeatureExpr(argument) {
			return true
		}
	}
	for _, item := range e.Items {
		if directHasStructuredFeatureExpr(item) {
			return true
		}
	}
	for _, value := range e.Values {
		if directHasStructuredFeatureExpr(value) {
			return true
		}
	}
	return false
}

func directHasStructuredFeatures(stmts []*Stmt) bool {
	for _, s := range stmts {
		if s == nil {
			continue
		}
		if directHasStructuredFeatureExpr(s.Init) || directHasStructuredFeatureExpr(s.Expr) || directHasStructuredFeatureExpr(s.Target) || directHasStructuredFeatureExpr(s.Value) || directHasStructuredFeatureExpr(s.Cond) || directHasStructuredFeatureExpr(s.Return) {
			return true
		}
		if directHasStructuredFeatures(s.Then) || directHasStructuredFeatures(s.Else) || directHasStructuredFeatures(s.Body) {
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
	return m.collectFunctionBlock(stmts, []map[string]machineSlot{m.slots}, &m.nextSlot)
}

func (m *directMachine) collectFunctionBlock(stmts []*Stmt, scopes []map[string]machineSlot, nextSlot *int32) error {
	for _, s := range stmts {
		if s == nil {
			continue
		}
		switch s.Kind {
		case StLet, StConst:
			if s.Init == nil {
				return fmt.Errorf("direct ELF backend requires an initializer for '%s'", s.Name)
			}
			current := scopes[len(scopes)-1]
			if _, exists := current[s.Name]; exists {
				return fmt.Errorf("direct ELF backend does not support shadowed binding '%s'", s.Name)
			}
			if !machineValueTypeSupported(s.Init.Type) {
				return fmt.Errorf("direct ELF backend function local '%s' has unsupported type %s", s.Name, s.Init.Type.String())
			}
			*nextSlot += 8
			slot := machineSlot{offset: *nextSlot, typ: s.Init.Type}
			m.statementSlots[s] = slot
			current[s.Name] = slot
		case StAssign:
			// The checker has already validated the target. Its slot is resolved
			// from the active scope stack during emission.
		case StIf:
			if err := m.collectFunctionBlock(s.Then, append(scopes, map[string]machineSlot{}), nextSlot); err != nil {
				return err
			}
			if err := m.collectFunctionBlock(s.Else, append(scopes, map[string]machineSlot{}), nextSlot); err != nil {
				return err
			}
		case StWhile:
			if err := m.collectFunctionBlock(s.Body, append(scopes, map[string]machineSlot{}), nextSlot); err != nil {
				return err
			}
		case StFor:
			if s.Iter == nil || s.Iter.Type == nil || s.Iter.Type.Kind != TyArray {
				return fmt.Errorf("direct ELF backend for requires an Array iterator")
			}
			*nextSlot += 8
			m.forIterSlots[s] = machineSlot{offset: *nextSlot, typ: s.Iter.Type}
			*nextSlot += 8
			m.forIndexSlots[s] = machineSlot{offset: *nextSlot, typ: TInt}
			*nextSlot += 8
			itemSlot := machineSlot{offset: *nextSlot, typ: s.Iter.Type.A}
			m.statementSlots[s] = itemSlot
			loopScope := append(scopes, map[string]machineSlot{s.Name: itemSlot})
			if err := m.collectFunctionBlock(s.Body, loopScope, nextSlot); err != nil {
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

func (m *directMachine) collectFunction(stmts []*Stmt, slots map[string]machineSlot, nextSlot *int32) error {
	return m.collectFunctionBlock(stmts, []map[string]machineSlot{slots}, nextSlot)
}

func (m *directMachine) prepareFunctions(p *Program) error {
	for _, f := range p.Functions {
		if f == nil || f.Name == "main" {
			continue
		}
		key := functionKey(f)
		if _, exists := m.functionLabels[key]; exists {
			return fmt.Errorf("direct ELF backend has duplicate function '%s'", f.Name)
		}
		if len(f.Params) > 6 {
			return fmt.Errorf("direct ELF backend function '%s' has too many parameters", f.Name)
		}
		returnTypeName := typeSpecString(f.Return)
		returnType, ok := machineTypeFromSpec(p, f.Return)
		if !ok {
			return fmt.Errorf("direct ELF backend function '%s' has unsupported return type %s", f.Name, returnTypeName)
		}
		m.functionLabels[key] = m.newLabel()
		m.functionOrder = append(m.functionOrder, f)
		m.functionReturns[key] = returnType
		slots := map[string]machineSlot{}
		for index, param := range f.Params {
			paramTypeName := typeSpecString(param.Type)
			paramType, ok := machineTypeFromSpec(p, param.Type)
			if !ok || paramType.Kind == TyNil {
				return fmt.Errorf("direct ELF backend function '%s' parameter '%s' has unsupported type %s", f.Name, param.Name, paramTypeName)
			}
			slots[param.Name] = machineSlot{offset: int32((index + 1) * 8), typ: paramType}
		}
		nextSlot := int32(len(f.Params) * 8)
		if err := m.collectFunction(f.Body, slots, &nextSlot); err != nil {
			return fmt.Errorf("function '%s': %w", f.Name, err)
		}
		m.functionSlots[key] = slots
		m.functionNextSlot[key] = nextSlot
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

func (m *directMachine) emitScopedStatements(stmts []*Stmt) error {
	m.pushScope()
	defer m.popScope()
	return m.emitStatements(stmts)
}

func (m *directMachine) emitFor(s *Stmt) error {
	iterSlot, ok := m.forIterSlots[s]
	if !ok {
		return fmt.Errorf("direct ELF backend has no iterator slot for '%s'", s.Name)
	}
	indexSlot, ok := m.forIndexSlots[s]
	if !ok {
		return fmt.Errorf("direct ELF backend has no index slot for '%s'", s.Name)
	}
	itemSlot, ok := m.statementSlots[s]
	if !ok {
		return fmt.Errorf("direct ELF backend has no loop binding slot for '%s'", s.Name)
	}
	if err := m.emitExpr(s.Iter); err != nil {
		return err
	}
	m.emitStoreSlot(iterSlot)
	m.emitMoveImmediate(0)
	m.emitStoreSlot(indexSlot)
	conditionLabel := m.newLabel()
	incrementLabel := m.newLabel()
	endLabel := m.newLabel()
	if err := m.bind(conditionLabel); err != nil {
		return err
	}
	// Load the iterator and index, then check the same bounds that array_get
	// uses before loading the element into the loop binding.
	m.emitLoadSlot(iterSlot)
	m.code = append(m.code, 0x50) // array pointer
	m.emitLoadSlot(indexSlot)
	m.code = append(m.code, 0x48, 0x89, 0xc1, 0x58) // rcx=index, rax=array
	m.code = append(m.code, 0x48, 0x85, 0xc9)       // test index
	if err := m.emitConditionalJump(0x88, m.trapLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x3b, 0x08) // cmp index, [array]
	if err := m.emitConditionalJump(0x83, endLabel); err != nil {
		return err
	}
	m.code = append(m.code, 0x48, 0x8b, 0x44, 0xc8, 0x08) // load element
	m.emitStoreSlot(itemSlot)
	m.loops = append(m.loops, machineLoop{breakLabel: endLabel, continueLabel: incrementLabel})
	m.pushScope()
	m.bindSlot(s.Name, itemSlot)
	err := m.emitStatements(s.Body)
	m.popScope()
	m.loops = m.loops[:len(m.loops)-1]
	if err != nil {
		return err
	}
	if err := m.bind(incrementLabel); err != nil {
		return err
	}
	m.emitLoadSlot(indexSlot)
	m.code = append(m.code, 0x48, 0xff, 0xc0) // increment index
	m.emitStoreSlot(indexSlot)
	if err := m.emitJump(conditionLabel); err != nil {
		return err
	}
	return m.bind(endLabel)
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
			slot, ok := m.statementSlots[s]
			if !ok {
				return fmt.Errorf("direct ELF backend has no slot for binding '%s'", s.Name)
			}
			m.emitStoreSlot(slot)
			m.bindSlot(s.Name, slot)
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
			slot, ok := m.lookupSlot(s.Target.Name)
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
				if err := m.emitCall(functionKey(s.Expr.Function)); err != nil {
					return err
				}
				continue
			}
			if s.Expr.Name != "print" && s.Expr.Name != "println" {
				if err := m.emitExpr(s.Expr); err != nil {
					return err
				}
				continue
			}
			if len(s.Expr.Args) != 1 {
				return fmt.Errorf("direct ELF backend %s expects one argument", s.Expr.Name)
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
			if err := m.emitScopedStatements(s.Then); err != nil {
				return err
			}
			if len(s.Else) > 0 {
				if err := m.emitJump(joinLabel); err != nil {
					return err
				}
				if err := m.bind(elseLabel); err != nil {
					return err
				}
				if err := m.emitScopedStatements(s.Else); err != nil {
					return err
				}
				if err := m.bind(joinLabel); err != nil {
					return err
				}
			} else {
				if err := m.bind(elseLabel); err != nil {
					return err
				}
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
			if err := m.emitScopedStatements(s.Body); err != nil {
				return err
			}
			m.loops = m.loops[:len(m.loops)-1]
			if err := m.emitJump(conditionLabel); err != nil {
				return err
			}
			if err := m.bind(endLabel); err != nil {
				return err
			}
		case StFor:
			if err := m.emitFor(s); err != nil {
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
	m.scopeStack = []map[string]machineSlot{{}}
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
	if len(m.functionOrder) > 0 || m.stringConcatUsed || m.arrayRuntimeUsed || m.boxRuntimeUsed || m.structRuntimeUsed || m.hostRuntimeUsed || m.stringCharsUsed || m.mapRuntimeUsed {
		if err := m.emitJump(m.endLabel); err != nil {
			return nil, err
		}
		mainSlots := m.slots
		mainStatic := m.staticEnv
		mainLoops := m.loops
		mainScopes := m.scopeStack
		for _, f := range m.functionOrder {
			key := functionKey(f)
			if err := m.bind(m.functionLabels[key]); err != nil {
				return nil, err
			}
			m.inFunction = true
			m.currentFunction = key
			m.slots = m.functionSlots[key]
			m.staticEnv = map[string]Value{}
			m.loops = nil
			m.scopeStack = []map[string]machineSlot{{}}
			for _, param := range f.Params {
				m.bindSlot(param.Name, m.functionSlots[key][param.Name])
			}
			m.bufferOffset = m.functionNextSlot[key] + 1
			functionFrame := (int(m.bufferOffset) + 63 + 15) &^ 15
			m.code = append(m.code, 0x55, 0x48, 0x89, 0xe5)
			m.code = append(m.code, 0x48, 0x81, 0xec)
			var functionSize [4]byte
			binary.LittleEndian.PutUint32(functionSize[:], uint32(functionFrame))
			m.code = append(m.code, functionSize[:]...)
			for index, param := range f.Params {
				if err := m.emitStoreArg(index, m.functionSlots[key][param.Name]); err != nil {
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
			m.scopeStack = mainScopes
			m.code = append(m.code, 0xc9, 0xc3)
		}
	}
	if m.hostRuntimeUsed {
		if err := m.emitStringAllocRuntime(); err != nil {
			return nil, err
		}
		if m.stringCharsUsed {
			if err := m.emitStringCharsRuntime(); err != nil {
				return nil, err
			}
		}
		if m.substringUsed {
			if err := m.emitSubstringRuntime(); err != nil {
				return nil, err
			}
		}
		if m.intToStringUsed {
			if err := m.emitIntToStringRuntime(); err != nil {
				return nil, err
			}
		}
		if m.intFromStringUsed {
			if err := m.emitIntFromStringRuntime(); err != nil {
				return nil, err
			}
		}
		if err := m.emitBytesFromArrayRuntime(); err != nil {
			return nil, err
		}
		if err := m.emitProcessArgsRuntime(); err != nil {
			return nil, err
		}
		if err := m.emitFSReadTextRuntime(); err != nil {
			return nil, err
		}
		if err := m.emitFSWriteBytesRuntime(); err != nil {
			return nil, err
		}
	}
	if m.jsonRuntimeUsed {
		if err := m.emitJSONParseRuntime(); err != nil {
			return nil, err
		}
		if err := m.emitJSONSkipRuntime(); err != nil {
			return nil, err
		}
		if err := m.emitJSONKindRuntime(); err != nil {
			return nil, err
		}
		if err := m.emitJSONObjectGetRuntime(); err != nil {
			return nil, err
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
	if m.mapRuntimeUsed {
		if err := m.emitStringEqualRuntime(); err != nil {
			return nil, err
		}
		if err := m.emitMapFindRuntime(); err != nil {
			return nil, err
		}
		if err := m.emitMapInsertRuntime(); err != nil {
			return nil, err
		}
	}
	if m.boxRuntimeUsed {
		if err := m.emitBoxAllocRuntime(); err != nil {
			return nil, err
		}
	}
	if m.structRuntimeUsed {
		if err := m.emitStructAllocRuntime(); err != nil {
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
