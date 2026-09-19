package kry

import (
	"encoding/binary"
	"fmt"
)

// directMachine is the second direct-ELF slice. It lowers checked Int/Bool
// state, assignments, comparisons, if/while control flow, and static or
// dynamic integer output to x86-64 instructions. It intentionally has no
// runtime or C dependency.
// Unsupported values fail during compilation instead of silently changing
// Kryndel semantics.
type directMachine struct {
	code         []byte
	data         []byte
	dataByText   map[string]int
	dataRefs     []machineDataRef
	labels       []machineLabel
	slots        map[string]machineSlot
	nextSlot     int32
	loops        []machineLoop
	endLabel     int
	trapLabel    int
	staticEnv    map[string]Value
	bufferOffset int32
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
		dataByText: map[string]int{},
		slots:      map[string]machineSlot{},
		staticEnv:  map[string]Value{},
	}
	m.endLabel = m.newLabel()
	m.trapLabel = m.newLabel()
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

func (m *directMachine) emitStaticOutput(e *Expr, name string) error {
	if e == nil || e.Kind != ExCall || e.Receiver != nil || len(e.Args) != 1 {
		return fmt.Errorf("direct ELF backend supports only one-argument print calls")
	}
	v, ok := directStaticValue(e.Args[0], m.staticEnv)
	if !ok || (v.Kind != VString && v.Kind != VInt && v.Kind != VBool && v.Kind != VUInt) {
		if e.Args[0].Type == nil || (e.Args[0].Type.Kind != TyInt && e.Args[0].Type.Kind != TyUInt) {
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
			if s.Init.Type != nil && s.Init.Type.Kind == TyString {
				if _, ok := directStaticValue(s.Init, m.staticEnv); !ok {
					return fmt.Errorf("direct ELF backend requires a static String initializer for '%s'", s.Name)
				}
				// String storage is not yet a native pointer value in this slice;
				// immutable static strings stay in staticEnv for print lowering.
				m.emitMoveImmediate(0)
			} else if err := m.emitExpr(s.Init); err != nil {
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
			if s.Expr == nil || s.Expr.Kind != ExCall || s.Expr.Receiver != nil || len(s.Expr.Args) != 1 || (s.Expr.Name != "print" && s.Expr.Name != "println") {
				return fmt.Errorf("direct ELF backend supports only print/println expressions")
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
	return newDirectMachine().build(stmts)
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
