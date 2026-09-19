package kry

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	mrand "math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type ValueKind int

const (
	VNil ValueKind = iota
	VInt
	VFloat
	VBool
	VString
	VBytes
	VArray
	VStruct
	VEnum
	VOption
	VResult
	VChannel
	VThread
	VMap
	VSet
	VJSON
	VWebSocket
	VActor
	VShared
	VTaskGroup
	VRegex
	VRandom
	VSQLite
	VTCP
	VTCPListener
	VUDP
	VTailCall
)

type regexHandle struct{ re *regexp.Regexp }

type randomHandle struct {
	mu  sync.Mutex
	rng *mrand.Rand
}

type sqliteHandle struct {
	mu     sync.Mutex
	db     *sql.DB
	closed bool
}

type tcpSocketHandle struct {
	mu     sync.Mutex
	conn   net.Conn
	closed bool
}

type tcpListenerHandle struct {
	mu       sync.Mutex
	listener net.Listener
	closed   bool
}

type udpSocketHandle struct {
	mu     sync.Mutex
	conn   *net.UDPConn
	closed bool
}

type Value struct {
	Kind    ValueKind
	I       int64
	F       float64
	Bool    bool
	S       string
	Bytes   []byte
	Array   []Value
	Struct  *StructDecl
	Fields  []Value
	Enum    *EnumDecl
	Variant string
	Present bool
	OK      bool
	Inner   *Value
	Ch      *Channel
	Th      *Thread
	Map     []MapEntry
	Set     []Value
	WS      *websocketConn
	Actor   *Actor
	Shared  *SharedCell
	Group   *TaskGroup
	Regex   *regexHandle
	Random  *randomHandle
	SQLite  *sqliteHandle
	TCP     *tcpSocketHandle
	TCPList *tcpListenerHandle
	UDP     *udpSocketHandle
	Tail    *TailCall
}

type MapEntry struct{ Key, Value Value }

func nilVal() Value            { return Value{Kind: VNil} }
func intVal(v int64) Value     { return Value{Kind: VInt, I: v} }
func floatVal(v float64) Value { return Value{Kind: VFloat, F: v} }
func boolVal(v bool) Value     { return Value{Kind: VBool, Bool: v} }
func stringVal(v string) Value { return Value{Kind: VString, S: v} }
func bytesVal(v []byte) Value  { p := append([]byte(nil), v...); return Value{Kind: VBytes, Bytes: p} }
func arrVal(v []Value) Value   { return Value{Kind: VArray, Array: append([]Value(nil), v...)} }
func optVal(p bool, v Value) Value {
	r := Value{Kind: VOption, Present: p}
	if p {
		x := cloneValue(v)
		r.Inner = &x
	}
	return r
}
func resVal(ok bool, v Value) Value {
	x := cloneValue(v)
	return Value{Kind: VResult, OK: ok, Inner: &x}
}
func display(v Value) string {
	switch v.Kind {
	case VNil:
		return "nil"
	case VInt:
		return strconv.FormatInt(v.I, 10)
	case VFloat:
		return strconv.FormatFloat(v.F, 'g', -1, 64)
	case VBool:
		if v.Bool {
			return "true"
		}
		return "false"
	case VString, VJSON:
		return v.S
	case VBytes:
		return fmt.Sprintf("<Bytes:%d>", len(v.Bytes))
	case VArray:
		var b strings.Builder
		b.WriteByte('[')
		for i, x := range v.Array {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(display(x))
		}
		b.WriteByte(']')
		return b.String()
	case VStruct:
		var b strings.Builder
		b.WriteString(v.Struct.Name)
		b.WriteByte('{')
		for i, f := range v.Struct.Fields {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(f.Name)
			b.WriteString(": ")
			b.WriteString(display(v.Fields[i]))
		}
		b.WriteByte('}')
		return b.String()
	case VEnum:
		return v.Enum.Name + "::" + v.Variant
	case VOption:
		if !v.Present {
			return "none"
		}
		return "some(" + display(*v.Inner) + ")"
	case VResult:
		if v.OK {
			return "ok(" + display(*v.Inner) + ")"
		}
		return "err(" + display(*v.Inner) + ")"
	case VChannel:
		return "<Channel>"
	case VThread:
		return "<Thread>"
	case VWebSocket:
		return "<WebSocket>"
	case VActor:
		return "<Actor>"
	case VShared:
		return "<Shared>"
	case VTaskGroup:
		return "<TaskGroup>"
	case VRegex:
		return "<Regex>"
	case VRandom:
		return "<Random>"
	case VSQLite:
		return "<SQLite>"
	case VTCP:
		return "<TcpSocket>"
	case VTCPListener:
		return "<TcpListener>"
	case VUDP:
		return "<UdpSocket>"
	case VTailCall:
		return "<tail-call>"
	case VMap:
		var b strings.Builder
		b.WriteString("{")
		for i, x := range v.Map {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(display(x.Key))
			b.WriteString(": ")
			b.WriteString(display(x.Value))
		}
		b.WriteString("}")
		return b.String()
	case VSet:
		var b strings.Builder
		b.WriteString("|{")
		for i, x := range v.Set {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(display(x))
		}
		b.WriteString("}|")
		return b.String()
	}

	return "<invalid>"
}
func cloneValue(v Value) Value {
	switch v.Kind {
	case VString:
		return stringVal(v.S)
	case VJSON:
		return Value{Kind: VJSON, S: v.S}
	case VWebSocket:
		return v
	case VActor:
		return v
	case VShared:
		return v
	case VTaskGroup:
		return v
	case VRegex:
		return v
	case VRandom:
		return v
	case VSQLite, VTCP, VTCPListener, VUDP:
		return v
	case VTailCall:
		return v
	case VBytes:
		return bytesVal(v.Bytes)
	case VArray:
		a := make([]Value, len(v.Array))
		for i, x := range v.Array {
			a[i] = cloneValue(x)
		}
		return arrVal(a)
	case VStruct:
		r := Value{Kind: VStruct, Struct: v.Struct, Fields: make([]Value, len(v.Fields))}
		for i, x := range v.Fields {
			r.Fields[i] = cloneValue(x)
		}
		return r
	case VOption:
		if !v.Present {
			return optVal(false, nilVal())
		}
		return optVal(true, cloneValue(*v.Inner))
	case VResult:
		return resVal(v.OK, cloneValue(*v.Inner))
	case VMap:
		m := make([]MapEntry, len(v.Map))
		for i, x := range v.Map {
			m[i] = MapEntry{Key: cloneValue(x.Key), Value: cloneValue(x.Value)}
		}
		return Value{Kind: VMap, Map: m}
	case VSet:
		s := make([]Value, len(v.Set))
		for i, x := range v.Set {
			s[i] = cloneValue(x)
		}
		return Value{Kind: VSet, Set: s}
	default:

		return v
	}
}

// lessValue orders Int, Float, and String values for array_sort. Values of
// other kinds compare by their display form so sorting never panics.
func lessValue(a, b Value) bool {
	switch {
	case a.Kind == VInt && b.Kind == VInt:
		return a.I < b.I
	case a.Kind == VFloat && b.Kind == VFloat:
		return a.F < b.F
	case a.Kind == VInt && b.Kind == VFloat:
		return float64(a.I) < b.F
	case a.Kind == VFloat && b.Kind == VInt:
		return a.F < float64(b.I)
	case a.Kind == VString && b.Kind == VString:
		return a.S < b.S
	default:
		return display(a) < display(b)
	}
}

func equalValue(a, b Value) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case VNil:
		return true
	case VInt:
		return a.I == b.I
	case VFloat:
		return a.F == b.F
	case VBool:
		return a.Bool == b.Bool
	case VString, VJSON:
		return a.S == b.S
	case VBytes:
		return string(a.Bytes) == string(b.Bytes)
	case VArray:
		if len(a.Array) != len(b.Array) {
			return false
		}
		for i := range a.Array {
			if !equalValue(a.Array[i], b.Array[i]) {
				return false
			}
		}
		return true
	case VStruct:
		if a.Struct != b.Struct || len(a.Fields) != len(b.Fields) {
			return false
		}
		for i := range a.Fields {
			if !equalValue(a.Fields[i], b.Fields[i]) {
				return false
			}
		}
		return true
	case VEnum:
		return a.Enum == b.Enum && a.Variant == b.Variant
	case VOption:
		if a.Present != b.Present {
			return false
		}
		return !a.Present || equalValue(*a.Inner, *b.Inner)
	case VResult:
		return a.OK == b.OK && equalValue(*a.Inner, *b.Inner)
	case VChannel:
		return a.Ch == b.Ch
	case VThread:
		return a.Th == b.Th
	case VMap:
		if len(a.Map) != len(b.Map) {
			return false
		}
		for i := range a.Map {
			if !equalValue(a.Map[i].Key, b.Map[i].Key) || !equalValue(a.Map[i].Value, b.Map[i].Value) {
				return false
			}
		}
		return true
	case VWebSocket:
		return a.WS == b.WS
	case VActor:
		return a.Actor == b.Actor
	case VShared:
		return a.Shared == b.Shared
	case VTaskGroup:
		return a.Group == b.Group
	case VRegex:
		return a.Regex == b.Regex
	case VRandom:
		return a.Random == b.Random
	case VSQLite:
		return a.SQLite == b.SQLite
	case VTCP:
		return a.TCP == b.TCP
	case VTCPListener:
		return a.TCPList == b.TCPList
	case VUDP:
		return a.UDP == b.UDP
	case VTailCall:
		return a.Tail == b.Tail
	case VSet:
		if len(a.Set) != len(b.Set) {

			return false
		}
		for i := range a.Set {
			if !equalValue(a.Set[i], b.Set[i]) {
				return false
			}
		}
		return true
	}

	return false
}
func copyableValue(v Value, depth int) bool {
	if depth > 128 {
		return false
	}
	switch v.Kind {
	case VNil, VInt, VFloat, VBool, VString, VBytes, VEnum, VJSON:
		return true
	case VArray:
		for _, x := range v.Array {
			if !copyableValue(x, depth+1) {
				return false
			}
		}
		return true
	case VStruct:
		for _, x := range v.Fields {
			if !copyableValue(x, depth+1) {
				return false
			}
		}
		return true
	case VOption:
		return !v.Present || copyableValue(*v.Inner, depth+1)
	case VResult:
		return copyableValue(*v.Inner, depth+1)
	case VMap:
		for _, x := range v.Map {
			if !copyableValue(x.Key, depth+1) || !copyableValue(x.Value, depth+1) {
				return false
			}
		}
		return true
	case VSet:
		for _, x := range v.Set {
			if !copyableValue(x, depth+1) {
				return false
			}
		}
		return true
	case VWebSocket:
		return false
	case VActor:
		return false
	case VShared:
		return true
	case VTaskGroup:
		return false
	default:

		return false
	}
}

type ExecContext struct {
	Ctx          context.Context
	Cancel       context.CancelFunc
	Lim          Limits
	Instructions uint64
	Memory       int64
	Calls        int
	Output       int64
}

func (x *ExecContext) step(src *Source, line, col int) *Diagnostic {
	if x.Instructions >= x.Lim.MaxInstructions {
		return Diag(CatResource, src, line, col, "instruction limit exceeded")
	}
	x.Instructions++
	select {
	case <-x.Ctx.Done():
		if errors.Is(x.Ctx.Err(), context.DeadlineExceeded) {
			return Diag(CatResource, src, line, col, "wall-clock execution limit exceeded")
		}
		return Diag(CatRuntime, src, line, col, "execution cancelled")
	default:
		return nil
	}
}
func (x *ExecContext) account(n int64, src *Source, line, col int) *Diagnostic {
	if n < 0 || x.Memory > x.Lim.MaxMemoryBytes-n {
		return Diag(CatResource, src, line, col, "memory budget exceeded")
	}
	x.Memory += n
	return nil
}

type Channel struct {
	Data   chan Value
	Done   chan struct{}
	mu     sync.Mutex
	Closed bool
}

func newChannel(capacity int) *Channel {
	if capacity < 1 {
		capacity = 1
	}
	return &Channel{Data: make(chan Value, capacity), Done: make(chan struct{})}
}
func (c *Channel) close() {
	c.mu.Lock()
	if !c.Closed {
		c.Closed = true
		close(c.Done)
	}
	c.mu.Unlock()
}
func (c *Channel) isClosed() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.Closed }

type Actor struct {
	Mailbox *Channel
}

type SharedCell struct {
	mu    sync.RWMutex
	value Value
}

type TaskGroup struct {
	mu        sync.Mutex
	threads   []*Thread
	cancelled bool
}

type TailCall struct {
	Function *Function
	Receiver *Value
	Args     []Value
}

type Thread struct {
	Done   chan struct{}
	Cancel context.CancelFunc
	mu     sync.Mutex
	Result Value
	Diag   *Diagnostic
	Joined bool
}
type DispatchEntry struct {
	Handler  string
	Priority int64
}
type Runtime struct {
	Prog         *Program
	Checker      *Checker
	Funcs        map[string]*Function
	Global       *RunScope
	Lim          Limits
	Sandbox      Sandbox
	Ctx          *ExecContext
	Channels     []*Channel
	Threads      []*Thread
	Dispatch     map[string][]DispatchEntry
	nextTimerID  int64
	Worker       bool
	propagated   *Value
	shutdownOnce sync.Once
}
type RunBinding struct {
	Value   Value
	Mutable bool
}
type RunScope struct {
	Parent     *RunScope
	Values     map[string]RunBinding
	Defers     [][]*Stmt
	ReturnType *Type
}

func newRunScope(p *RunScope) *RunScope { return &RunScope{Parent: p, Values: map[string]RunBinding{}} }
func (s *RunScope) get(n string) (*RunBinding, bool) {
	for q := s; q != nil; q = q.Parent {
		if b, ok := q.Values[n]; ok {
			c := b
			return &c, true
		}
	}
	return nil, false
}
func (s *RunScope) define(n string, v Value, m bool) error {
	if _, ok := s.Values[n]; ok {
		return fmt.Errorf("binding '%s' is already defined in this scope", n)
	}
	s.Values[n] = RunBinding{Value: v, Mutable: m}
	return nil
}
func NewRuntime(prog *Program, c *Checker, lim Limits, sb Sandbox) (*Runtime, *Diagnostic) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(lim.MaxWallTimeMS)*time.Millisecond)
	r := &Runtime{Prog: prog, Checker: c, Funcs: c.Env.Functions, Global: newRunScope(nil), Lim: lim, Sandbox: sb, Ctx: &ExecContext{Ctx: ctx, Cancel: cancel, Lim: lim}, Channels: nil, Threads: nil, Dispatch: map[string][]DispatchEntry{}}
	return r, nil
}
func (r *Runtime) fail(e *Expr, format string, args ...any) *Diagnostic {
	msg := fmt.Sprintf(format, args...)
	if e == nil {
		return Diag(CatRuntime, r.Prog.Source, 1, 1, msg)
	}
	return Diag(CatRuntime, e.Tok.Source, e.Tok.Line, e.Tok.Column, msg)
}
func (r *Runtime) printValue(v Value, newline bool) *Diagnostic {
	s := display(v)
	if r.Ctx.Output+int64(len(s))+1 > r.Lim.MaxOutputBytes {
		return Diag(CatResource, r.Prog.Source, 1, 1, "output limit exceeded")
	}
	r.Ctx.Output += int64(len(s))
	if newline {
		fmt.Println(s)
	} else {
		fmt.Print(s)
	}
	return nil
}

type EvalCode int

const (
	evalNormal EvalCode = iota
	evalReturn
	evalBreak
	evalContinue
	evalError
)

type EvalResult struct {
	Code  EvalCode
	Value Value
	Diag  *Diagnostic
}

func normal() EvalResult                  { return EvalResult{Code: evalNormal, Value: nilVal()} }
func returned(v Value) EvalResult         { return EvalResult{Code: evalReturn, Value: v} }
func control(c EvalCode) EvalResult       { return EvalResult{Code: c, Value: nilVal()} }
func (r *Runtime) takePropagated() *Value { p := r.propagated; r.propagated = nil; return p }
func (r *Runtime) run() (result *Diagnostic) {
	defer func() { result = r.cleanup(result); r.Ctx.Cancel() }()
	for _, s := range r.Prog.Statements {
		if d := r.Ctx.step(s.Tok.Source, s.Tok.Line, s.Tok.Column); d != nil {
			return d
		}
		x := r.execStmt(r.Global, s)
		if x.Diag != nil {
			return x.Diag
		}
		if x.Code != evalNormal {
			return Diag(CatRuntime, s.Tok.Source, s.Tok.Line, s.Tok.Column, "control flow escaped top level")
		}
	}
	// Top-level defer blocks must run when the program finishes, mirroring
	// the block-scoped defer semantics used inside functions.
	for i := len(r.Global.Defers) - 1; i >= 0; i-- {
		if x := r.execBlock(newRunScope(r.Global), r.Global.Defers[i]); x.Diag != nil {
			return x.Diag
		}
	}
	if len(r.Prog.Statements) == 0 {
		if f := r.Funcs["main"]; f != nil {
			v, d := r.evalCall(r.Global, &Expr{Kind: ExCall, Name: "main", Tok: f.Tok})
			if d != nil {
				return d
			}
			if v.Kind == VResult && !v.OK {
				return r.fail(nil, "main returned error: %s", display(*v.Inner))
			}
		}
	}
	return nil
}
func (r *Runtime) cleanup(prior *Diagnostic) *Diagnostic {
	r.shutdownOnce.Do(func() {
		for _, t := range r.Threads {
			t.Cancel()
		}
		for _, c := range r.Channels {
			c.close()
		}
		deadline := time.NewTimer(time.Duration(r.Lim.ShutdownMS) * time.Millisecond)
		defer deadline.Stop()
		for _, t := range r.Threads {
			if t.Joined {
				continue
			}
			select {
			case <-t.Done:
				t.Joined = true
			case <-deadline.C:
				if prior == nil {
					prior = Diag(CatResource, r.Prog.Source, 1, 1, "worker shutdown exceeded configured deadline")
				}
			}
		}
	})
	return prior
}
func (r *Runtime) execBlock(sc *RunScope, body []*Stmt) (out EvalResult) {
	out = normal()
	for _, s := range body {
		if d := r.Ctx.step(s.Tok.Source, s.Tok.Line, s.Tok.Column); d != nil {
			out = EvalResult{Code: evalError, Diag: d}
			break
		}
		x := r.execStmt(sc, s)
		if x.Code != evalNormal {
			out = x
			break
		}
	}
	for i := len(sc.Defers) - 1; i >= 0; i-- {
		x := r.execBlock(newRunScope(sc), sc.Defers[i])
		if x.Diag != nil && out.Diag == nil {
			out = EvalResult{Code: evalError, Diag: x.Diag}
		}
	}
	return out
}
func (r *Runtime) execStmt(sc *RunScope, s *Stmt) EvalResult {
	switch s.Kind {
	case StLet, StConst:
		v, d := r.evalExpr(sc, s.Init)
		if p := r.takePropagated(); p != nil {
			return returned(*p)
		}
		if d != nil {
			return EvalResult{Code: evalError, Diag: d}
		}
		if err := sc.define(s.Name, v, s.Mutable); err != nil {
			return EvalResult{Code: evalError, Diag: r.fail(nil, err.Error())}
		}
		return normal()
	case StExpr:
		v, d := r.evalExpr(sc, s.Expr)
		_ = v
		if p := r.takePropagated(); p != nil {
			return returned(*p)
		}
		if d != nil {
			return EvalResult{Code: evalError, Diag: d}
		}
		return normal()
	case StAssign:
		if s.Target.Kind != ExVar {
			return EvalResult{Code: evalError, Diag: r.fail(s.Target, "assignment target must be a binding")}
		}
		b, ok := sc.get(s.Target.Name)
		if !ok {
			return EvalResult{Code: evalError, Diag: r.fail(s.Target, "unknown binding '%s'", s.Target.Name)}
		}
		if !b.Mutable {
			return EvalResult{Code: evalError, Diag: r.fail(s.Target, "immutable binding '%s' cannot be assigned", s.Target.Name)}
		}
		v, d := r.evalExpr(sc, s.Value)
		if p := r.takePropagated(); p != nil {
			return returned(*p)
		}
		if d != nil {
			return EvalResult{Code: evalError, Diag: d}
		}

		for q := sc; q != nil; q = q.Parent {
			if old, ok := q.Values[s.Target.Name]; ok {
				old.Value = v
				q.Values[s.Target.Name] = old
				break
			}
		}
		return normal()
	case StIf:
		c, d := r.evalExpr(sc, s.Cond)
		if p := r.takePropagated(); p != nil {
			return returned(*p)
		}
		if d != nil {
			return EvalResult{Code: evalError, Diag: d}
		}
		if c.Kind != VBool {
			return EvalResult{Code: evalError, Diag: r.fail(s.Cond, "condition must be Bool")}
		}
		if c.Bool {
			return r.execBlock(newRunScope(sc), s.Then)
		}
		return r.execBlock(newRunScope(sc), s.Else)
	case StWhile:
		for {
			if d := r.Ctx.step(s.Tok.Source, s.Tok.Line, s.Tok.Column); d != nil {
				return EvalResult{Code: evalError, Diag: d}
			}
			c, d := r.evalExpr(sc, s.Cond)
			if p := r.takePropagated(); p != nil {
				return returned(*p)
			}
			if d != nil {
				return EvalResult{Code: evalError, Diag: d}
			}
			if c.Kind != VBool {
				return EvalResult{Code: evalError, Diag: r.fail(s.Cond, "condition must be Bool")}
			}
			if !c.Bool {
				break
			}
			x := r.execBlock(newRunScope(sc), s.Body)
			if x.Diag != nil {
				return x
			}
			if x.Code == evalReturn {
				return x
			}
			if x.Code == evalBreak {
				break
			}
			if x.Code == evalContinue {
				continue
			}
		}
		return normal()
	case StFor:
		iter, d := r.evalExpr(sc, s.Iter)
		if p := r.takePropagated(); p != nil {
			return returned(*p)
		}
		if d != nil {
			return EvalResult{Code: evalError, Diag: d}
		}
		var items []Value
		switch iter.Kind {
		case VArray:
			items = iter.Array
		case VSet:
			items = iter.Set
		case VString:
			for _, rr := range iter.S {
				items = append(items, stringVal(string(rr)))
			}
		case VBytes:
			for _, bb := range iter.Bytes {
				items = append(items, intVal(int64(bb)))
			}
		default:
			return EvalResult{Code: evalError, Diag: r.fail(s.Iter, "for expects Array, Set, String, or Bytes")}
		}
		for _, item := range items {
			is := newRunScope(sc)
			if err := is.define(s.Name, cloneValue(item), false); err != nil {
				return EvalResult{Code: evalError, Diag: r.fail(s.Iter, err.Error())}
			}
			x := r.execBlock(is, s.Body)
			if x.Diag != nil || x.Code == evalReturn {
				return x
			}
			if x.Code == evalBreak {
				break
			}
		}
		return normal()
	case StDefer:
		sc.Defers = append(sc.Defers, s.Body)
		return normal()
	case StUnsafe:
		return r.execBlock(newRunScope(sc), s.Body)
	case StReturn:
		v := nilVal()
		var d *Diagnostic
		if s.Return != nil {
			v, d = r.evalExpr(sc, s.Return)
		}
		if p := r.takePropagated(); p != nil {
			return returned(*p)
		}
		if d != nil {
			return EvalResult{Code: evalError, Diag: d}
		}
		if s.Return != nil && s.Return.Kind == ExPropagate && sc.ReturnType != nil {
			if sc.ReturnType.Kind == TyOption {

				return returned(optVal(true, v))
			}
			if sc.ReturnType.Kind == TyResult {
				return returned(resVal(true, v))
			}
		}
		return returned(v)
	case StBreak:
		return control(evalBreak)
	case StContinue:
		return control(evalContinue)
	case StMatch:
		v, d := r.evalExpr(sc, s.Scrutinee)
		if p := r.takePropagated(); p != nil {
			return returned(*p)
		}
		if d != nil {
			return EvalResult{Code: evalError, Diag: d}
		}
		for _, a := range s.Arms {
			if ok := matchPattern(v, a.Pattern); ok {
				as := newRunScope(sc)
				if a.Pattern.Binding != "" {
					var x Value
					if a.Pattern.Kind == PatOption {
						x = *v.Inner
					} else if a.Pattern.Kind == PatResult {
						x = *v.Inner
					}
					_ = as.define(a.Pattern.Binding, x, false)
				}
				return r.execBlock(as, a.Body)
			}
		}
		return EvalResult{Code: evalError, Diag: r.fail(s.Scrutinee, "no match arm matched")}
	}
	return normal()
}
func matchPattern(v Value, p Pattern) bool {
	switch p.Kind {
	case PatWildcard:
		return true
	case PatNil:
		return v.Kind == VNil || (v.Kind == VOption && !v.Present)
	case PatBool:
		return v.Kind == VBool && v.Bool == p.Bool
	case PatInt:
		return v.Kind == VInt && v.I == p.Int
	case PatString:
		return v.Kind == VString && v.S == p.Str
	case PatEnum:
		return v.Kind == VEnum && v.Variant == p.Variant && (p.TypeName == "" || v.Enum.Name == p.TypeName)
	case PatOption:
		return v.Kind == VOption && v.Present == p.Present
	case PatResult:
		return v.Kind == VResult && v.OK == p.OK
	}
	return false
}
func (r *Runtime) evalExpr(sc *RunScope, e *Expr) (Value, *Diagnostic) {
	if d := r.Ctx.step(e.Tok.Source, e.Tok.Line, e.Tok.Column); d != nil {
		return nilVal(), d
	}
	if e.ConstValue != nil {
		return cloneValue(*e.ConstValue), nil
	}
	switch e.Kind {
	case ExInt:
		return intVal(e.Int), nil
	case ExFloat:
		return floatVal(e.Float), nil
	case ExBool:
		return boolVal(e.Bool), nil
	case ExNil:
		return nilVal(), nil
	case ExString:
		return stringVal(e.Str), nil
	case ExVar:
		b, ok := sc.get(e.Name)
		if !ok {
			return nilVal(), r.fail(e, "unknown name '%s'", e.Name)
		}
		return b.Value, nil
	case ExEnum:
		t := r.Checker.Env.Types[e.EnumType]
		return Value{Kind: VEnum, Enum: t.Enum, Variant: e.EnumVariant}, nil
	case ExMap:
		m := make([]MapEntry, 0, len(e.MapKeys))
		for i, keyExpr := range e.MapKeys {
			key, d := r.evalExpr(sc, keyExpr)
			if d != nil {
				return nilVal(), d
			}
			value, d := r.evalExpr(sc, e.Values[i])
			if d != nil {
				return nilVal(), d
			}
			for _, old := range m {
				if equalValue(old.Key, key) {
					return nilVal(), r.fail(e, "duplicate map key")
				}
			}
			m = append(m, MapEntry{Key: cloneValue(key), Value: cloneValue(value)})
		}
		if int64(len(m))*48 > r.Lim.MaxMemoryBytes {
			return nilVal(), r.fail(e, "map allocation exceeds memory budget")
		}
		return Value{Kind: VMap, Map: m}, nil
	case ExSet:
		s := make([]Value, 0, len(e.Items))
		for _, itemExpr := range e.Items {
			item, d := r.evalExpr(sc, itemExpr)
			if d != nil {
				return nilVal(), d
			}
			found := false
			for _, old := range s {
				if equalValue(old, item) {
					found = true
					break
				}
			}
			if !found {
				s = append(s, cloneValue(item))
			}
		}
		return Value{Kind: VSet, Set: s}, nil
	case ExArray:
		a := make([]Value, len(e.Items))
		for i, x := range e.Items {
			v, d := r.evalExpr(sc, x)
			if d != nil {
				return nilVal(), d
			}
			a[i] = v
		}
		if d := r.Ctx.account(int64(len(a))*32, e.Tok.Source, e.Tok.Line, e.Tok.Column); d != nil {
			return nilVal(), d
		}
		return arrVal(a), nil
	case ExStruct:
		t := r.Checker.Env.Types[e.StructName]
		if t == nil || t.Struct == nil {
			return nilVal(), r.fail(e, "unknown struct '%s'", e.StructName)
		}
		vals := make([]Value, len(t.Struct.Fields))
		for i, n := range e.Fields {
			idx := -1
			for j, f := range t.Struct.Fields {
				if f.Name == n {
					idx = j
				}
			}
			if idx < 0 {
				return nilVal(), r.fail(e, "unknown field '%s'", n)
			}
			v, d := r.evalExpr(sc, e.Values[i])
			if d != nil {
				return nilVal(), d
			}
			vals[idx] = v
		}
		return Value{Kind: VStruct, Struct: t.Struct, Fields: vals}, nil
	case ExUnary:
		v, d := r.evalExpr(sc, e.Operand)
		if d != nil {
			return nilVal(), d
		}
		if e.Op == BANG {
			if v.Kind != VBool {
				return nilVal(), r.fail(e, "'!' expects Bool")
			}
			return boolVal(!v.Bool), nil
		}
		if v.Kind == VInt {
			if e.Op == PLUS {
				return v, nil
			}
			if v.I == math.MinInt64 {
				return nilVal(), r.fail(e, "negation overflow")
			}
			return intVal(-v.I), nil
		}
		if v.Kind == VFloat {
			z := -v.F
			if !isFinite(z) {
				return nilVal(), r.fail(e, "floating-point result must be finite")
			}
			return floatVal(z), nil
		}
		return nilVal(), r.fail(e, "unary sign expects numeric value")
	case ExBinary:
		return r.evalBinary(sc, e)
	case ExIndex:
		base, d := r.evalExpr(sc, e.Base)
		if d != nil {
			return nilVal(), d
		}
		ix, d := r.evalExpr(sc, e.Left)
		if d != nil {
			return nilVal(), d
		}
		if base.Kind != VMap && (ix.Kind != VInt || ix.I < 0) {
			return nilVal(), r.fail(e, "index must be a non-negative Int")
		}
		if base.Kind == VMap {
			for _, item := range base.Map {
				if equalValue(item.Key, ix) {
					return cloneValue(item.Value), nil
				}
			}
			return nilVal(), r.fail(e, "map key not found")
		}
		i := ix.I
		if base.Kind == VArray {
			if i >= int64(len(base.Array)) {
				return nilVal(), r.fail(e, "array index out of range")
			}
			return cloneValue(base.Array[i]), nil
		}
		if base.Kind == VString {
			runes := []rune(base.S)
			if i >= int64(len(runes)) {
				return nilVal(), r.fail(e, "string index out of range")
			}
			return stringVal(string(runes[i])), nil
		}
		if base.Kind == VBytes {
			if i >= int64(len(base.Bytes)) {
				return nilVal(), r.fail(e, "byte index out of range")
			}
			return intVal(int64(base.Bytes[i])), nil
		}
		if base.Kind == VMap {
			for _, item := range base.Map {
				if equalValue(item.Key, ix) {
					return cloneValue(item.Value), nil
				}
			}
			return nilVal(), r.fail(e, "map key not found")
		}
		return nilVal(), r.fail(e, "indexing expects String, Bytes, Array, or Map")
	case ExField:
		base, d := r.evalExpr(sc, e.Base)
		if d != nil {
			return nilVal(), d
		}
		if base.Kind != VStruct {
			return nilVal(), r.fail(e, "field access expects a struct")
		}
		for i, f := range base.Struct.Fields {
			if f.Name == e.Field {
				return cloneValue(base.Fields[i]), nil
			}
		}
		return nilVal(), r.fail(e, "unknown field '%s'", e.Field)
	case ExPropagate:
		v, d := r.evalExpr(sc, e.Operand)
		if d != nil {
			return nilVal(), d
		}
		if v.Kind == VOption {
			if !v.Present {
				x := cloneValue(v)
				r.propagated = &x
				return nilVal(), nil
			}
			return cloneValue(*v.Inner), nil
		}
		if v.Kind == VResult {
			if !v.OK {
				x := cloneValue(v)
				r.propagated = &x
				return nilVal(), nil
			}
			return cloneValue(*v.Inner), nil
		}
		return nilVal(), r.fail(e, "'?' requires an Option or Result")
	case ExCall:
		return r.evalCall(sc, e)
	}

	return nilVal(), r.fail(e, "invalid expression")
}
func (r *Runtime) evalBinary(sc *RunScope, e *Expr) (Value, *Diagnostic) {
	l, d := r.evalExpr(sc, e.Left)
	if d != nil {
		return nilVal(), d
	}
	if e.Op == AND || e.Op == OR {
		if l.Kind != VBool {
			return nilVal(), r.fail(e, "logical operators require Bool")
		}
		if e.Op == AND && !l.Bool {
			return boolVal(false), nil
		}
		if e.Op == OR && l.Bool {
			return boolVal(true), nil
		}
		rr, d := r.evalExpr(sc, e.Right)
		if d != nil {
			return nilVal(), d
		}
		if rr.Kind != VBool {
			return nilVal(), r.fail(e, "logical operators require Bool")
		}
		return boolVal(rr.Bool), nil
	}
	rr, d := r.evalExpr(sc, e.Right)
	if d != nil {
		return nilVal(), d
	}
	if e.Op == EQEQ {
		return boolVal(equalValue(l, rr)), nil
	}
	if e.Op == NEQ {
		return boolVal(!equalValue(l, rr)), nil
	}
	if e.Op == PLUS && (l.Kind == VString && rr.Kind == VString) {
		if d := r.Ctx.account(int64(len(l.S)+len(rr.S)), e.Tok.Source, e.Tok.Line, e.Tok.Column); d != nil {
			return nilVal(), d
		}
		return stringVal(l.S + rr.S), nil
	}
	if e.Op == PLUS && (l.Kind == VBytes && rr.Kind == VBytes) {
		return bytesVal(append(append([]byte{}, l.Bytes...), rr.Bytes...)), nil
	}
	if e.Op == PLUS && (l.Kind == VArray && rr.Kind == VArray) {
		if len(l.Array) > r.Lim.MaxArrayElements-len(rr.Array) {
			return nilVal(), r.fail(e, "array size limit exceeded")
		}
		return arrVal(append(append([]Value{}, l.Array...), rr.Array...)), nil
	}
	if l.Kind == VInt && rr.Kind == VInt {
		var z int64
		var ok bool
		switch e.Op {
		case PLUS:
			z, ok = addI(l.I, rr.I)
		case MINUS:
			z, ok = subI(l.I, rr.I)
		case STAR:
			z, ok = mulI(l.I, rr.I)
		case SLASH:
			z, ok = divI(l.I, rr.I)
		case PERCENT:
			z, ok = remI(l.I, rr.I)
		case LESS:
			return boolVal(l.I < rr.I), nil
		case LEQ:
			return boolVal(l.I <= rr.I), nil
		case GREATER:
			return boolVal(l.I > rr.I), nil
		case GEQ:
			return boolVal(l.I >= rr.I), nil
		}
		if !ok {
			if (e.Op == SLASH || e.Op == PERCENT) && rr.I == 0 {
				if e.Op == PERCENT {
					return nilVal(), r.fail(e, "remainder by zero")
				}
				return nilVal(), r.fail(e, "division by zero")
			}
			return nilVal(), r.fail(e, "checked integer arithmetic overflow")
		}
		return intVal(z), nil
	}
	if l.Kind == VFloat && rr.Kind == VFloat {
		if e.Op == LESS {
			return boolVal(l.F < rr.F), nil
		}
		if e.Op == LEQ {
			return boolVal(l.F <= rr.F), nil
		}
		if e.Op == GREATER {
			return boolVal(l.F > rr.F), nil
		}
		if e.Op == GEQ {
			return boolVal(l.F >= rr.F), nil
		}
		var z float64
		switch e.Op {
		case PLUS:
			z = l.F + rr.F
		case MINUS:
			z = l.F - rr.F
		case STAR:
			z = l.F * rr.F
		case SLASH:
			if rr.F == 0 {
				return nilVal(), r.fail(e, "floating division by zero")
			}
			z = l.F / rr.F
		}
		if !isFinite(z) {
			return nilVal(), r.fail(e, "floating-point result must be finite")
		}
		return floatVal(z), nil
	}
	return nilVal(), r.fail(e, "operator operands have incompatible types")
}
func addI(a, b int64) (int64, bool) {
	if b > 0 && a > math.MaxInt64-b || b < 0 && a < math.MinInt64-b {
		return 0, false
	}
	return a + b, true
}
func subI(a, b int64) (int64, bool) {
	if b < 0 && a > math.MaxInt64+b || b > 0 && a < math.MinInt64+b {
		return 0, false
	}
	return a - b, true
}
func mulI(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	if a == math.MinInt64 && b == -1 || b == math.MinInt64 && a == -1 {
		return 0, false
	}
	z := a * b
	if z/b != a {
		return 0, false
	}
	return z, true
}
func divI(a, b int64) (int64, bool) {
	if b == 0 || (a == math.MinInt64 && b == -1) {
		return 0, false
	}
	return a / b, true
}
func remI(a, b int64) (int64, bool) {
	if b == 0 || (a == math.MinInt64 && b == -1) {
		return 0, false
	}
	return a % b, true
}

func (r *Runtime) evalCall(sc *RunScope, e *Expr) (Value, *Diagnostic) {
	if e.Receiver == nil {
		if b, ok := r.Checker.Env.Builtins[e.Name]; ok {
			args := make([]Value, len(e.Args))
			for i, a := range e.Args {
				v, d := r.evalExpr(sc, a)
				if d != nil {
					return nilVal(), d
				}
				args[i] = v
			}
			return r.evalBuiltin(e, b, args)
		}
	}
	var receiver *Value
	var f *Function
	if e.Receiver != nil {
		v, d := r.evalExpr(sc, e.Receiver)
		if d != nil {
			return nilVal(), d
		}
		if p := r.takePropagated(); p != nil {
			r.propagated = p
			return nilVal(), nil
		}
		x := cloneValue(v)
		receiver = &x
		f = r.Funcs[methodKey(r.Checker.Env.Types[e.Receiver.Type.String()], e.Name)]
		if f == nil {
			f = r.Funcs[methodKey(e.Receiver.Type, e.Name)]
		}
	} else {
		f = e.Function
		if f == nil {
			f = r.Funcs[e.Name]
		}
	}
	if f == nil {
		return nilVal(), r.fail(e, "unknown function or method '%s'", e.Name)
	}
	if r.Ctx.Calls >= r.Lim.MaxCallDepth {
		return nilVal(), Diag(CatResource, e.Tok.Source, e.Tok.Line, e.Tok.Column, "call depth limit exceeded")
	}
	args := make([]Value, len(e.Args))
	for i, a := range e.Args {
		v, d := r.evalExpr(sc, a)
		if d != nil {
			return nilVal(), d
		}
		if p := r.takePropagated(); p != nil {
			r.propagated = p
			return nilVal(), nil
		}
		args[i] = v
	}
	// Fill omitted trailing arguments from their declared defaults.
	for i := len(args); i < len(f.Params); i++ {
		if f.Params[i].Default == nil {
			break
		}
		v, d := r.evalExpr(sc, f.Params[i].Default)
		if d != nil {
			return nilVal(), d
		}
		args = append(args, v)
	}
	if e.Tail {
		return Value{Kind: VTailCall, Tail: &TailCall{Function: f, Receiver: receiver, Args: args}}, nil
	}
	return r.invokeFunction(e, f, receiver, args)
}

func (r *Runtime) invokeFunction(e *Expr, f *Function, receiver *Value, args []Value) (Value, *Diagnostic) {
	for {
		child := newRunScope(r.Global)
		if receiver != nil {
			_ = child.define("self", *receiver, false)
		}
		for i, p := range f.Params {
			_ = child.define(p.Name, args[i], false)
		}
		r.Ctx.Calls++
		child.ReturnType = mustResolve(r.Checker.Env, f.Return)
		x := r.execBlock(child, f.Body)
		r.Ctx.Calls--
		if x.Diag != nil {
			return nilVal(), x.Diag
		}
		if x.Code == evalReturn && x.Value.Kind == VTailCall && x.Value.Tail != nil {
			f = x.Value.Tail.Function
			receiver = x.Value.Tail.Receiver
			args = x.Value.Tail.Args
			continue
		}
		if x.Code == evalReturn {
			return x.Value, nil
		}
		return nilVal(), nil
	}
}
func (r *Runtime) evalBuiltin(e *Expr, b Builtin, a []Value) (Value, *Diagnostic) {
	bad := func(m string) (Value, *Diagnostic) { return nilVal(), r.fail(e, m) }
	switch b.Name {
	case "poly_register":
		slot, handler, priority := a[0].S, a[1].S, a[2].I
		f := r.Funcs[handler]
		if f == nil || f.Receiver != nil || len(f.Params) != 1 || mustResolve(r.Checker.Env, f.Params[0].Type).Kind != TyString || mustResolve(r.Checker.Env, f.Return).Kind != TyString {
			return resVal(false, stringVal("handler must be a top-level fn(String) -> String")), nil
		}
		entries := r.Dispatch[slot]
		for _, entry := range entries {
			if entry.Handler == handler {
				return resVal(false, stringVal("handler already registered in slot")), nil
			}
		}
		entries = append(entries, DispatchEntry{Handler: handler, Priority: priority})
		for i := len(entries) - 1; i > 0 && entries[i].Priority > entries[i-1].Priority; i-- {
			entries[i], entries[i-1] = entries[i-1], entries[i]
		}
		r.Dispatch[slot] = entries
		return resVal(true, nilVal()), nil
	case "poly_reorder":
		slot, handler, before := a[0].S, a[1].S, a[2].S
		entries := r.Dispatch[slot]
		from, target := -1, -1
		for i, entry := range entries {
			if entry.Handler == handler {
				from = i
			}
			if entry.Handler == before {
				target = i
			}
		}
		if from < 0 || target < 0 || handler == before {
			return resVal(false, stringVal("both handlers must already be registered and distinct")), nil
		}
		entry := entries[from]
		entries = append(entries[:from], entries[from+1:]...)
		if from < target {
			target--
		}
		entries = append(entries, DispatchEntry{})
		copy(entries[target+1:], entries[target:])
		entries[target] = entry
		r.Dispatch[slot] = entries
		return resVal(true, nilVal()), nil
	case "poly_dispatch":
		entries := r.Dispatch[a[0].S]
		if len(entries) == 0 {
			return resVal(false, stringVal("dispatch slot has no registered handlers")), nil
		}
		f := r.Funcs[entries[0].Handler]
		value, d := r.invokeFunction(e, f, nil, []Value{a[1]})
		if d != nil {
			return resVal(false, stringVal(d.Message)), nil
		}
		return resVal(true, value), nil
	case "print":
		return nilVal(), r.printValue(a[0], false)
	case "println":
		return nilVal(), r.printValue(a[0], true)
	case "len":
		switch a[0].Kind {
		case VString:
			return intVal(int64(utf8.RuneCountInString(a[0].S))), nil
		case VArray:
			return intVal(int64(len(a[0].Array))), nil
		case VBytes:
			return intVal(int64(len(a[0].Bytes))), nil
		case VMap:
			return intVal(int64(len(a[0].Map))), nil
		case VSet:
			return intVal(int64(len(a[0].Set))), nil
		}
		return bad("len expects String, Array, or Bytes")
	case "bytes":
		out := make([]byte, len(a[0].Array))
		for i, v := range a[0].Array {
			if v.Kind != VInt || v.I < 0 || v.I > 255 {
				return bad("bytes element must be in range 0..255")
			}
			out[i] = byte(v.I)
		}
		return bytesVal(out), nil
	case "string_to_bytes":
		return bytesVal([]byte(a[0].S)), nil
	case "bytes_to_string":
		if !validUTF8(a[0].Bytes) {
			return bad("bytes are not valid UTF-8")
		}
		return stringVal(string(a[0].Bytes)), nil
	case "array_push":
		if len(a[0].Array) >= r.Lim.MaxArrayElements {
			return bad("array size limit exceeded")
		}
		return arrVal(append(append([]Value{}, a[0].Array...), a[1])), nil
	case "int":
		return r.toInt(e, a[0])
	case "float":
		return r.toFloat(e, a[0])
	case "str":
		return stringVal(display(a[0])), nil
	case "bool":
		return boolVal(toBool(a[0])), nil
	case "assert":
		if !a[0].Bool {
			return bad("assertion failed")
		}
		return nilVal(), nil
	case "assert_eq":
		if !equalValue(a[0], a[1]) {
			return bad("assertion failed: values are not equal")
		}
		return nilVal(), nil
	case "abs":
		if a[0].Kind == VInt {
			if a[0].I == math.MinInt64 {
				return bad("minimum Int cannot be represented by abs")
			}
			if a[0].I < 0 {
				return intVal(-a[0].I), nil
			}
			return a[0], nil
		}
		z := math.Abs(a[0].F)
		if !isFinite(z) {
			return bad("absolute value must be finite")
		}
		return floatVal(z), nil
	case "sqrt":
		z := numericFloat(a[0])
		if z < 0 || !isFinite(z) {
			return bad("sqrt domain error")
		}
		q := math.Sqrt(z)
		if !isFinite(q) {
			return bad("sqrt result must be finite")
		}
		return floatVal(q), nil
	case "min", "max":
		if a[0].Kind == VInt {
			if b.Name == "min" && a[0].I < a[1].I || b.Name == "max" && a[0].I > a[1].I {
				return a[0], nil
			}
			return a[1], nil
		}
		if b.Name == "min" && a[0].F < a[1].F || b.Name == "max" && a[0].F > a[1].F {
			return a[0], nil
		}
		return a[1], nil
	case "floor", "ceil", "round":
		z := map[string]float64{"floor": math.Floor(a[0].F), "ceil": math.Ceil(a[0].F), "round": math.Round(a[0].F)}[b.Name]
		if z < float64(math.MinInt64) || z > float64(math.MaxInt64) {
			return bad("rounded value is outside Int range")
		}
		return intVal(int64(z)), nil
	case "pow", "log", "sin", "cos":
		var z float64
		if b.Name == "pow" {
			z = math.Pow(a[0].F, a[1].F)
		} else if b.Name == "log" {
			if a[0].F <= 0 {
				return bad("log domain error")
			}
			z = math.Log(a[0].F)
		} else if b.Name == "sin" {
			z = math.Sin(a[0].F)
		} else {
			z = math.Cos(a[0].F)
		}
		if !isFinite(z) {
			return bad("math result must be finite")
		}
		return floatVal(z), nil
	case "is_nan":
		return boolVal(math.IsNaN(a[0].F)), nil
	case "is_finite":
		return boolVal(isFinite(a[0].F)), nil
	case "is_some":
		return boolVal(a[0].Kind == VOption && a[0].Present), nil
	case "is_none":
		return boolVal(a[0].Kind == VOption && !a[0].Present), nil
	case "is_ok":
		return boolVal(a[0].Kind == VResult && a[0].OK), nil
	case "is_err":
		return boolVal(a[0].Kind == VResult && !a[0].OK), nil
	case "unwrap_or":
		if a[0].Present {
			return cloneValue(*a[0].Inner), nil
		}
		return a[1], nil
	case "some":
		return optVal(true, a[0]), nil
	case "none":
		return optVal(false, nilVal()), nil
	case "ok":
		return resVal(true, a[0]), nil
	case "err":
		return resVal(false, a[0]), nil
	case "substring":
		return r.substring(e, a)
	case "contains":
		return boolVal(strings.Contains(a[0].S, a[1].S)), nil
	case "starts_with":
		return boolVal(strings.HasPrefix(a[0].S, a[1].S)), nil
	case "ends_with":
		return boolVal(strings.HasSuffix(a[0].S, a[1].S)), nil
	case "trim":
		return stringVal(strings.TrimSpace(a[0].S)), nil
	case "split":
		parts := strings.Split(a[0].S, a[1].S)
		vs := make([]Value, len(parts))
		for i, x := range parts {
			vs[i] = stringVal(x)
		}
		return arrVal(vs), nil
	case "replace":
		return stringVal(strings.ReplaceAll(a[0].S, a[1].S, a[2].S)), nil
	case "codepoints":
		runes := []rune(a[0].S)
		vs := make([]Value, len(runes))
		for i, x := range runes {
			vs[i] = intVal(int64(x))
		}
		return arrVal(vs), nil
	case "byte_at":
		i := a[1].I
		if i < 0 || i >= int64(len([]byte(a[0].S))) {
			return resVal(false, stringVal("index out of range")), nil
		}
		return resVal(true, intVal(int64([]byte(a[0].S)[i]))), nil
	case "hex_encode":
		return stringVal(hex.EncodeToString(a[0].Bytes)), nil
	case "hex_decode":
		v, err := hex.DecodeString(a[0].S)
		if err != nil {
			return resVal(false, stringVal("invalid hex")), nil
		}
		return resVal(true, bytesVal(v)), nil
	case "base64_encode":
		return stringVal(base64.StdEncoding.EncodeToString(a[0].Bytes)), nil
	case "base64_decode":
		v, err := base64.StdEncoding.DecodeString(a[0].S)
		if err != nil {
			return resVal(false, stringVal("invalid base64")), nil
		}
		return resVal(true, bytesVal(v)), nil
	case "array_pop":
		if len(a[0].Array) == 0 {
			return optVal(false, nilVal()), nil
		}
		return optVal(true, a[0].Array[len(a[0].Array)-1]), nil
	case "array_get":
		if a[1].I < 0 || a[1].I >= int64(len(a[0].Array)) {
			return optVal(false, nilVal()), nil
		}
		return optVal(true, a[0].Array[a[1].I]), nil
	case "array_concat":
		if len(a[0].Array) > r.Lim.MaxArrayElements-len(a[1].Array) {
			return bad("array size limit exceeded")
		}
		return arrVal(append(append([]Value{}, a[0].Array...), a[1].Array...)), nil
	case "array_contains":
		for _, v := range a[0].Array {
			if equalValue(v, a[1]) {
				return boolVal(true), nil
			}
		}
		return boolVal(false), nil
	case "array_slice":
		start, n := a[1].I, a[2].I
		if start < 0 || n < 0 || start > int64(len(a[0].Array)) || n > int64(len(a[0].Array))-start {
			return bad("array slice range is out of bounds")
		}
		return arrVal(a[0].Array[start : start+n]), nil
	case "array_reverse":
		v := append([]Value{}, a[0].Array...)
		for i, j := 0, len(v)-1; i < j; i, j = i+1, j-1 {
			v[i], v[j] = v[j], v[i]
		}
		return arrVal(v), nil
	case "array_join":
		ss := make([]string, len(a[0].Array))
		for i, v := range a[0].Array {
			ss[i] = v.S
		}
		return stringVal(strings.Join(ss, a[1].S)), nil
	case "map_get":
		for _, x := range a[0].Map {
			if equalValue(x.Key, a[1]) {
				return optVal(true, x.Value), nil
			}
		}
		return optVal(false, nilVal()), nil
	case "map_insert":
		m := make([]MapEntry, len(a[0].Map))
		found := false
		for i, x := range a[0].Map {
			m[i] = MapEntry{Key: cloneValue(x.Key), Value: cloneValue(x.Value)}
			if equalValue(x.Key, a[1]) {
				m[i].Value = cloneValue(a[2])
				found = true
			}
		}
		if !found {
			if len(m) >= r.Lim.MaxArrayElements {
				return bad("map size limit exceeded")
			}
			m = append(m, MapEntry{Key: cloneValue(a[1]), Value: cloneValue(a[2])})
		}
		return Value{Kind: VMap, Map: m}, nil
	case "map_keys":
		keys := make([]Value, len(a[0].Map))
		for i, x := range a[0].Map {
			keys[i] = cloneValue(x.Key)
		}
		return arrVal(keys), nil
	case "set_contains":
		for _, x := range a[0].Set {
			if equalValue(x, a[1]) {
				return boolVal(true), nil
			}
		}
		return boolVal(false), nil
	case "set_insert":
		for _, x := range a[0].Set {
			if equalValue(x, a[1]) {
				return cloneValue(a[0]), nil
			}
		}
		if len(a[0].Set) >= r.Lim.MaxArrayElements {
			return bad("set size limit exceeded")
		}
		s := make([]Value, len(a[0].Set), len(a[0].Set)+1)
		for i, x := range a[0].Set {
			s[i] = cloneValue(x)
		}
		s = append(s, cloneValue(a[1]))
		return Value{Kind: VSet, Set: s}, nil
	case "set_len":
		return intVal(int64(len(a[0].Set))), nil
	case "json_parse":
		if len(a[0].S) > r.Lim.MaxStringBytes || !json.Valid([]byte(a[0].S)) {
			return resVal(false, stringVal("invalid JSON")), nil
		}
		// Use a number-preserving decoder so large integers keep their exact
		// value instead of being rounded through float64, then normalize each
		// number to Int when integral and Float otherwise. This matches the
		// native backend's JSON model exactly.
		dec := json.NewDecoder(strings.NewReader(a[0].S))
		dec.UseNumber()
		var raw any
		if err := dec.Decode(&raw); err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		canon, err := json.Marshal(normalizeJSON(raw))
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, Value{Kind: VJSON, S: string(canon)}), nil
	case "json_stringify":
		return stringVal(a[0].S), nil
	case "http_get":
		return r.httpRequest(e, "GET", a[0].S, "")
	case "http_request":
		return r.httpRequest(e, a[0].S, a[1].S, a[2].S)
	case "http_request_auth":
		return r.httpRequestAuth(e, a[0].S, a[1].S, a[2].S, a[3].S)
	case "win_registry_get":
		value, err := windowsRegistryGet(a[0].S, a[1].S)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, stringVal(value)), nil
	case "win_service_query":
		value, err := windowsServiceQuery(a[0].S)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, stringVal(value)), nil
	case "win_eventlog_write":
		if err := windowsEventLogWrite(a[0].S, a[1].S); err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, nilVal()), nil
	case "win_raw_input":
		data, err := windowsRawInputRead()
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, bytesVal(data)), nil
	case "win_device_io_control":
		data, err := windowsDeviceIoControl(a[0].S, a[1].I, a[2].Bytes)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, bytesVal(data)), nil
	case "websocket_connect":
		conn, err := connectWebSocket(a[0].S)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, Value{Kind: VWebSocket, WS: conn}), nil
	case "websocket_send":
		if a[0].WS == nil {
			return resVal(false, stringVal("closed WebSocket")), nil
		}
		if err := a[0].WS.sendText(a[1].S); err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, nilVal()), nil
	case "websocket_receive":
		if a[0].WS == nil {
			return resVal(false, stringVal("closed WebSocket")), nil
		}
		text, err := a[0].WS.receiveText(r.Lim.MaxSourceBytes)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, stringVal(text)), nil
	case "websocket_close":
		if a[0].WS != nil {
			_ = a[0].WS.close()
		}
		return nilVal(), nil
	case "process_run":
		args := make([]string, len(a[1].Array))
		for i, x := range a[1].Array {
			if x.Kind != VString {
				return bad("process_run arguments must be String")
			}
			args[i] = x.S
		}
		cmd := exec.CommandContext(r.Ctx.Ctx, a[0].S, args...)
		out, err := cmd.CombinedOutput()
		if int64(len(out)) > r.Lim.MaxOutputBytes {
			return bad("process output exceeds configured limit")
		}
		if err == nil {
			return resVal(true, intVal(0)), nil
		}
		if ee, ok := err.(*exec.ExitError); ok {
			return resVal(false, stringVal(fmt.Sprintf("process exited with code %d", ee.ExitCode()))), nil
		}
		return resVal(false, stringVal(err.Error())), nil
	case "uuid_v4":
		value, err := uuidV4()
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, stringVal(value)), nil
	case "uuid_v5":
		value, err := uuidV5(a[0].S, a[1].S)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, stringVal(value)), nil
	case "uuid_is_valid":
		return boolVal(validUUID(a[0].S)), nil
	case "platform_os":
		return stringVal(platformOS()), nil
	case "platform_arch":
		return stringVal(platformArch()), nil
	case "platform_runtime":
		return stringVal(platformRuntime()), nil
	case "platform_hostname":
		host, err := os.Hostname()
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, stringVal(host)), nil
	case "dotenv_load":
		data, err := r.Sandbox.ReadFile(a[0].S)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		values, err := dotenvParse(string(data))
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		entries := make([]MapEntry, 0, len(values))
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			entries = append(entries, MapEntry{Key: stringVal(key), Value: stringVal(values[key])})
		}
		return resVal(true, Value{Kind: VMap, Map: entries}), nil
	case "datetime_now":
		return stringVal(time.Now().UTC().Format(time.RFC3339Nano)), nil
	case "datetime_unix_ms":
		return intVal(time.Now().UnixMilli()), nil
	case "datetime_format":
		value := time.UnixMilli(a[0].I).UTC().Format(a[1].S)
		return resVal(true, stringVal(value)), nil
	case "datetime_parse":
		value, err := time.Parse(a[1].S, a[0].S)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, intVal(value.UnixMilli())), nil
	case "random_new":
		return Value{Kind: VRandom, Random: newRandom(a[0].I)}, nil
	case "random_int":
		value, err := randomInt(a[0].Random, a[1].I, a[2].I)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, intVal(value)), nil
	case "random_float":
		value, err := randomFloat(a[0].Random)
		if err != nil {
			return bad(err.Error())
		}
		return floatVal(value), nil
	case "random_choice":
		if a[0].Random == nil {
			return bad("invalid Random handle")
		}
		if len(a[1].Array) == 0 {
			return optVal(false, nilVal()), nil
		}
		index, err := randomInt(a[0].Random, 0, int64(len(a[1].Array)-1))
		if err != nil {
			return bad(err.Error())
		}
		return optVal(true, a[1].Array[index]), nil
	case "regex_compile":
		re, err := regexp.Compile(a[0].S)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, Value{Kind: VRegex, Regex: &regexHandle{re: re}}), nil
	case "regex_is_match":
		if a[0].Regex == nil || a[0].Regex.re == nil {
			return bad("invalid Regex handle")
		}
		return boolVal(a[0].Regex.re.MatchString(a[1].S)), nil
	case "regex_find":
		if a[0].Regex == nil || a[0].Regex.re == nil {
			return bad("invalid Regex handle")
		}
		indices := a[0].Regex.re.FindStringIndex(a[1].S)
		if indices == nil {
			return optVal(false, nilVal()), nil
		}
		return optVal(true, stringVal(a[1].S[indices[0]:indices[1]])), nil
	case "regex_find_all":
		if a[0].Regex == nil || a[0].Regex.re == nil {
			return bad("invalid Regex handle")
		}
		matches := a[0].Regex.re.FindAllString(a[1].S, r.Lim.MaxArrayElements)
		out := make([]Value, len(matches))
		for i, match := range matches {
			out[i] = stringVal(match)
		}
		return arrVal(out), nil
	case "regex_replace_all":
		if a[0].Regex == nil || a[0].Regex.re == nil {
			return bad("invalid Regex handle")
		}
		return stringVal(a[0].Regex.re.ReplaceAllString(a[1].S, a[2].S)), nil
	case "regex_split":
		if a[0].Regex == nil || a[0].Regex.re == nil {
			return bad("invalid Regex handle")
		}
		parts := a[0].Regex.re.Split(a[1].S, r.Lim.MaxArrayElements)
		out := make([]Value, len(parts))
		for i, part := range parts {
			out[i] = stringVal(part)
		}
		return arrVal(out), nil
	case "sqlite_open":
		db, err := sqliteOpen(r.Sandbox, a[0].S)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, Value{Kind: VSQLite, SQLite: db}), nil
	case "sqlite_exec":
		count, err := sqliteExec(a[0].SQLite, a[1].S)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, intVal(count)), nil
	case "sqlite_query":
		rows, err := sqliteQuery(a[0].SQLite, a[1].S, r.Lim.MaxArrayElements)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		result := make([]Value, len(rows))
		for i, row := range rows {
			cells := make([]Value, len(row))
			for j, cell := range row {
				cells[j] = stringVal(cell)
			}
			result[i] = arrVal(cells)
		}
		return resVal(true, arrVal(result)), nil
	case "sqlite_close":
		if err := sqliteClose(a[0].SQLite); err != nil {
			return bad(err.Error())
		}
		return nilVal(), nil
	case "tcp_connect":
		socket, err := tcpConnect(a[0].S, a[1].I, time.Duration(r.Lim.MaxWallTimeMS)*time.Millisecond)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, Value{Kind: VTCP, TCP: socket}), nil
	case "tcp_listen":
		listener, err := tcpListen(a[0].S, a[1].I)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, Value{Kind: VTCPListener, TCPList: listener}), nil
	case "tcp_accept":
		listener := a[0].TCPList
		if listener == nil {
			return bad("invalid TcpListener handle")
		}
		listener.mu.Lock()
		if listener.closed {
			listener.mu.Unlock()
			return resVal(false, stringVal("TcpListener handle is closed")), nil
		}
		if tcp, ok := listener.listener.(*net.TCPListener); ok {
			_ = tcp.SetDeadline(socketDeadline(r.Lim.MaxWallTimeMS))
		}
		conn, err := listener.listener.Accept()
		listener.mu.Unlock()
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, Value{Kind: VTCP, TCP: &tcpSocketHandle{conn: conn}}), nil
	case "tcp_send":
		socket := a[0].TCP
		if socket == nil {
			return bad("invalid TcpSocket handle")
		}
		socket.mu.Lock()
		defer socket.mu.Unlock()
		if socket.closed {
			return resVal(false, stringVal("TcpSocket handle is closed")), nil
		}
		if err := socket.conn.SetWriteDeadline(socketDeadline(r.Lim.MaxWallTimeMS)); err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		written := 0
		for written < len(a[1].Bytes) {
			n, err := socket.conn.Write(a[1].Bytes[written:])
			written += n
			if err != nil {
				return resVal(false, stringVal(err.Error())), nil
			}
			if n == 0 {
				return resVal(false, stringVal("TCP write made no progress")), nil
			}
		}
		return resVal(true, intVal(int64(written))), nil
	case "tcp_receive":
		socket := a[0].TCP
		if socket == nil {
			return bad("invalid TcpSocket handle")
		}
		if a[1].I < 1 || a[1].I > int64(r.Lim.MaxSourceBytes) {
			return resVal(false, stringVal("receive size is outside configured limits")), nil
		}
		socket.mu.Lock()
		defer socket.mu.Unlock()
		if socket.closed {
			return resVal(false, stringVal("TcpSocket handle is closed")), nil
		}
		if err := socket.conn.SetReadDeadline(socketDeadline(r.Lim.MaxWallTimeMS)); err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		data := make([]byte, int(a[1].I))
		n, err := socket.conn.Read(data)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, bytesVal(data[:n])), nil
	case "tcp_local_port":
		listener := a[0].TCPList
		if listener == nil {
			return bad("invalid TcpListener handle")
		}
		listener.mu.Lock()
		defer listener.mu.Unlock()
		if listener.closed {
			return resVal(false, stringVal("TcpListener handle is closed")), nil
		}
		addr, ok := listener.listener.Addr().(*net.TCPAddr)
		if !ok {
			return resVal(false, stringVal("listener does not expose a TCP port")), nil
		}
		return resVal(true, intVal(int64(addr.Port))), nil
	case "tcp_close":
		if err := tcpClose(a[0].TCP); err != nil {
			return bad(err.Error())
		}
		return nilVal(), nil
	case "tcp_listener_close":
		if err := tcpListenerClose(a[0].TCPList); err != nil {
			return bad(err.Error())
		}
		return nilVal(), nil
	case "udp_bind":
		socket, err := udpBind(a[0].S, a[1].I)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, Value{Kind: VUDP, UDP: socket}), nil
	case "udp_send":
		socket := a[0].UDP
		if socket == nil {
			return bad("invalid UdpSocket handle")
		}
		if err := validPort(a[2].I, false); err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(a[1].S, strconv.FormatInt(a[2].I, 10)))
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		socket.mu.Lock()
		defer socket.mu.Unlock()
		if socket.closed {
			return resVal(false, stringVal("UdpSocket handle is closed")), nil
		}
		if err := socket.conn.SetWriteDeadline(socketDeadline(r.Lim.MaxWallTimeMS)); err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		n, err := socket.conn.WriteToUDP(a[3].Bytes, addr)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, intVal(int64(n))), nil
	case "udp_receive", "udp_receive_from":
		if a[1].I < 1 || a[1].I > int64(r.Lim.MaxSourceBytes) {
			return resVal(false, stringVal("receive size is outside configured limits")), nil
		}
		data, addr, err := udpReceive(a[0].UDP, int(a[1].I), socketDeadline(r.Lim.MaxWallTimeMS))
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		if b.Name == "udp_receive" {
			return resVal(true, bytesVal(data)), nil
		}
		value, err := udpSenderJSON(data, addr)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, Value{Kind: VJSON, S: value}), nil
	case "udp_close":
		if err := udpClose(a[0].UDP); err != nil {
			return bad(err.Error())
		}
		return nilVal(), nil
	case "shared_new":
		return Value{Kind: VShared, Shared: &SharedCell{value: cloneValue(a[0])}}, nil
	case "shared_read":
		if a[0].Shared == nil {
			return bad("invalid shared cell")
		}
		a[0].Shared.mu.RLock()
		value := cloneValue(a[0].Shared.value)
		a[0].Shared.mu.RUnlock()
		return value, nil
	case "shared_write":
		if a[0].Shared == nil {
			return bad("invalid shared cell")
		}
		a[0].Shared.mu.Lock()
		a[0].Shared.value = cloneValue(a[1])
		a[0].Shared.mu.Unlock()
		return nilVal(), nil
	case "shared_swap":
		if a[0].Shared == nil {
			return bad("invalid shared cell")
		}
		a[0].Shared.mu.Lock()
		old := cloneValue(a[0].Shared.value)
		a[0].Shared.value = cloneValue(a[1])
		a[0].Shared.mu.Unlock()
		return old, nil
	case "task_group":
		return Value{Kind: VTaskGroup, Group: &TaskGroup{}}, nil
	case "task_spawn":
		if a[0].Group == nil {
			return bad("invalid task group")
		}
		a[0].Group.mu.Lock()
		cancelled := a[0].Group.cancelled
		a[0].Group.mu.Unlock()
		if cancelled {
			return nilVal(), r.fail(e, "task group is cancelled")
		}
		thread, d := r.spawn(e, a[1].S)
		if d != nil {
			return nilVal(), d
		}
		a[0].Group.mu.Lock()
		a[0].Group.threads = append(a[0].Group.threads, thread.Th)
		a[0].Group.mu.Unlock()
		return thread, nil
	case "task_group_cancel":
		if a[0].Group == nil {
			return bad("invalid task group")
		}
		a[0].Group.mu.Lock()
		a[0].Group.cancelled = true
		for _, thread := range a[0].Group.threads {
			thread.Cancel()
		}
		a[0].Group.mu.Unlock()
		return nilVal(), nil
	case "task_group_wait":
		if a[0].Group == nil {
			return bad("invalid task group")
		}
		a[0].Group.mu.Lock()
		threads := append([]*Thread(nil), a[0].Group.threads...)
		a[0].Group.mu.Unlock()
		for _, thread := range threads {
			if _, d := r.join(e, thread); d != nil {
				a[0].Group.mu.Lock()
				a[0].Group.cancelled = true
				for _, sibling := range threads {
					if sibling != thread {
						sibling.Cancel()
					}
				}
				a[0].Group.mu.Unlock()
				for _, sibling := range threads {
					if sibling != thread {
						<-sibling.Done
					}
				}
				return resVal(false, stringVal(d.Message)), nil
			}
		}
		a[0].Group.mu.Lock()
		cancelled := a[0].Group.cancelled
		a[0].Group.mu.Unlock()
		if cancelled {
			return resVal(false, stringVal("task group cancelled")), nil
		}
		return resVal(true, nilVal()), nil
	case "actor_channel", "actor_channel_with_capacity":
		capacity := minInt(r.Lim.MaxChannelCapacity, 64)
		if b.Name == "actor_channel_with_capacity" {
			if a[0].I < 1 || a[0].I > int64(r.Lim.MaxChannelCapacity) {
				return bad("actor mailbox capacity is outside configured limits")
			}
			capacity = int(a[0].I)
		}
		mailbox := newChannel(capacity)
		r.Channels = append(r.Channels, mailbox)
		return Value{Kind: VActor, Actor: &Actor{Mailbox: mailbox}}, nil
	case "actor_send":
		if a[0].Actor == nil || a[0].Actor.Mailbox == nil {
			return bad("invalid actor")
		}
		if !copyableValue(a[1], 0) {
			return bad("actor send requires a recursively Copy value")
		}
		if a[0].Actor.Mailbox.isClosed() {
			return bad("closed actor mailbox")
		}
		select {
		case a[0].Actor.Mailbox.Data <- cloneValue(a[1]):
			return nilVal(), nil
		case <-a[0].Actor.Mailbox.Done:
			return bad("closed actor mailbox")
		case <-r.Ctx.Ctx.Done():
			return nilVal(), Diag(CatRuntime, e.Tok.Source, e.Tok.Line, e.Tok.Column, "actor send cancelled")
		}
	case "actor_try_receive":
		if a[0].Actor == nil || a[0].Actor.Mailbox == nil {
			return bad("invalid actor")
		}
		select {
		case v := <-a[0].Actor.Mailbox.Data:
			return resVal(true, v), nil
		case <-a[0].Actor.Mailbox.Done:
			return resVal(false, stringVal("closed")), nil
		default:
			return resVal(false, stringVal("empty")), nil
		}
	case "actor_receive_timeout":
		if a[1].I < 0 {
			return bad("timeout duration cannot be negative")
		}
		if a[0].Actor == nil || a[0].Actor.Mailbox == nil {
			return bad("invalid actor")
		}
		timer := time.NewTimer(time.Duration(a[1].I) * time.Millisecond)
		defer timer.Stop()
		select {
		case v := <-a[0].Actor.Mailbox.Data:
			return v, nil
		case <-a[0].Actor.Mailbox.Done:
			return nilVal(), badDiag(e, "closed actor mailbox")
		case <-timer.C:
			return nilVal(), badDiag(e, "actor receive timed out")
		}
	case "actor_close":
		if a[0].Actor != nil && a[0].Actor.Mailbox != nil {
			a[0].Actor.Mailbox.close()
		}
		return nilVal(), nil
	case "thread_channel":
		ch := newChannel(minInt(r.Lim.MaxChannelCapacity, 64))
		r.Channels = append(r.Channels, ch)
		return Value{Kind: VChannel, Ch: ch}, nil
	case "thread_channel_with_capacity":
		n := a[0].I
		if n < 1 || n > int64(r.Lim.MaxChannelCapacity) {
			return bad("channel capacity is outside configured limits")
		}
		ch := newChannel(int(n))
		r.Channels = append(r.Channels, ch)
		return Value{Kind: VChannel, Ch: ch}, nil
	case "thread_spawn":
		return r.spawn(e, a[0].S)
	case "thread_send":
		if !copyableValue(a[1], 0) {
			return bad("thread send requires a recursively Copy value")
		}
		if a[0].Ch.isClosed() {
			return bad("closed channel")
		}
		select {
		case a[0].Ch.Data <- cloneValue(a[1]):
			return nilVal(), nil
		case <-a[0].Ch.Done:
			return bad("closed channel")
		case <-r.Ctx.Ctx.Done():
			return nilVal(), Diag(CatRuntime, e.Tok.Source, e.Tok.Line, e.Tok.Column, "send cancelled")
		}
	case "thread_try_send":
		if !copyableValue(a[1], 0) {
			return bad("thread send requires a recursively Copy value")
		}
		if a[0].Ch.isClosed() {
			return resVal(false, stringVal("closed")), nil
		}
		select {
		case a[0].Ch.Data <- cloneValue(a[1]):
			return resVal(true, nilVal()), nil
		default:
			return resVal(false, stringVal("full")), nil
		}
	case "thread_send_timeout":
		if a[2].I < 0 {
			return bad("timeout duration cannot be negative")
		}
		if !copyableValue(a[1], 0) {
			return bad("thread send requires a recursively Copy value")
		}
		timer := time.NewTimer(time.Duration(a[2].I) * time.Millisecond)
		defer timer.Stop()
		select {
		case a[0].Ch.Data <- cloneValue(a[1]):
			return resVal(true, nilVal()), nil
		case <-a[0].Ch.Done:
			return resVal(false, stringVal("closed")), nil
		case <-timer.C:
			return resVal(false, stringVal("timeout")), nil
		}
	case "thread_receive":
		select {
		case v := <-a[0].Ch.Data:
			return v, nil
		case <-a[0].Ch.Done:
			return nilVal(), badDiag(e, "closed channel")
		case <-r.Ctx.Ctx.Done():
			return nilVal(), badDiag(e, "receive cancelled")
		}
	case "thread_try_receive":
		select {
		case v := <-a[0].Ch.Data:
			return resVal(true, v), nil
		case <-a[0].Ch.Done:
			return resVal(false, stringVal("closed")), nil
		default:
			return resVal(false, stringVal("empty")), nil
		}
	case "thread_receive_timeout":
		if a[1].I < 0 {
			return bad("timeout duration cannot be negative")
		}
		timer := time.NewTimer(time.Duration(a[1].I) * time.Millisecond)
		defer timer.Stop()
		select {
		case v := <-a[0].Ch.Data:
			return v, nil
		case <-a[0].Ch.Done:
			return nilVal(), badDiag(e, "closed channel")
		case <-timer.C:
			return nilVal(), badDiag(e, "channel receive timed out")
		}
	case "thread_join":
		return r.join(e, a[0].Th)
	case "await":
		return r.join(e, a[0].Th)
	case "thread_join_timeout":
		if a[1].I < 0 {
			return bad("timeout duration cannot be negative")
		}
		return r.joinTimeout(e, a[0].Th, a[1].I)
	case "await_timeout":
		return r.joinTimeout(e, a[0].Th, a[1].I)
	case "thread_cancel":
		a[0].Th.Cancel()
		return nilVal(), nil
	case "thread_close":
		a[0].Ch.close()
		return nilVal(), nil
	case "yield_now":
		runtime.Gosched()
		return nilVal(), nil
	case "sleep_ms":
		if a[0].I < 0 {
			return bad("sleep duration cannot be negative")
		}
		timer := time.NewTimer(time.Duration(a[0].I) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			return resVal(true, nilVal()), nil
		case <-r.Ctx.Ctx.Done():
			return resVal(false, stringVal("sleep cancelled")), nil
		}
	case "fs_read_text":
		return r.readText(a[0].S)
	case "fs_write_text":
		if !validUTF8([]byte(a[1].S)) {
			return resVal(false, stringVal("invalid UTF-8")), nil
		}
		if err := r.Sandbox.Write(a[0].S, []byte(a[1].S)); err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, nilVal()), nil
	case "fs_read_bytes":
		v, err := r.Sandbox.Read(a[0].S)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		if len(v) > r.Lim.MaxSourceBytes {
			return resVal(false, stringVal("file exceeds configured input limit")), nil
		}
		return resVal(true, bytesVal(v)), nil
	case "fs_write_bytes":
		if err := r.Sandbox.Write(a[0].S, a[1].Bytes); err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, nilVal()), nil
	case "fs_exists":
		ok, err := r.Sandbox.Exists(a[0].S)
		if err != nil {
			return bad(err.Error())
		}
		return boolVal(ok), nil
	case "env_get":
		if strings.IndexByte(a[0].S, 0) >= 0 {
			return bad("environment name cannot contain NUL")
		}
		v, ok := os.LookupEnv(a[0].S)
		if !ok {
			return optVal(false, nilVal()), nil
		}
		if !validUTF8([]byte(v)) {
			return bad("environment value is not valid UTF-8")
		}
		return optVal(true, stringVal(v)), nil
	case "crypto_sha256":
		h := sha256.Sum256(a[0].Bytes)
		return bytesVal(h[:]), nil
	case "crypto_hmac_sha256":
		mac := hmac.New(sha256.New, a[0].Bytes)
		mac.Write(a[1].Bytes)
		return bytesVal(mac.Sum(nil)), nil
	case "crypto_random_bytes":
		n := int(a[0].I)
		if n <= 0 || n > 1024 {
			return resVal(false, stringVal("random bytes length must be between 1 and 1024")), nil
		}
		buf := make([]byte, n)
		if _, err := rand.Read(buf); err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, bytesVal(buf)), nil
	case "crypto_sha512", "crypto_sha384", "crypto_sha1", "crypto_md5":
		digest, err := cryptoHash(b.Name, a[0].Bytes)
		if err != nil {
			return bad(err.Error())
		}
		return bytesVal(digest), nil
	case "crypto_aes_gcm_encrypt":
		out, err := cryptoAESGCMEncrypt(a[0].Bytes, a[1].Bytes, a[2].Bytes)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, bytesVal(out)), nil
	case "crypto_aes_gcm_decrypt":
		out, err := cryptoAESGCMDecrypt(a[0].Bytes, a[1].Bytes, a[2].Bytes)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, bytesVal(out)), nil
	case "crypto_pbkdf2_sha256":
		out, err := cryptoPBKDF2SHA256(a[0].Bytes, a[1].Bytes, int(a[2].I), int(a[3].I))
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, bytesVal(out)), nil
	case "crypto_hkdf_sha256":
		out, err := cryptoHKDFSHA256(a[0].Bytes, a[1].Bytes, a[2].Bytes, int(a[3].I))
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, bytesVal(out)), nil
	case "crypto_constant_time_equal":
		return boolVal(cryptoConstantTimeEqual(a[0].Bytes, a[1].Bytes)), nil
	case "crypto_xor":
		out, err := cryptoXOR(a[0].Bytes, a[1].Bytes)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, bytesVal(out)), nil
	case "base64url_encode":
		return stringVal(base64URLEncode(a[0].Bytes)), nil
	case "base64url_decode":
		out, err := base64URLDecode(a[0].S)
		if err != nil {
			return resVal(false, stringVal("invalid base64url input")), nil
		}
		return resVal(true, bytesVal(out)), nil
	case "string_slice":
		return r.stringSlice(a), nil
	case "array_slice_range":
		return r.arraySliceRange(a), nil
	case "string_format":
		return stringVal(formatTemplate(a[0].S, a[1].Array)), nil
	case "array_indices":
		idx := make([]Value, len(a[0].Array))
		for i := range idx {
			idx[i] = intVal(int64(i))
		}
		return arrVal(idx), nil
	case "array_zip":
		left, right := a[0].Array, a[1].Array
		n := len(left)
		if len(right) < n {
			n = len(right)
		}
		pairs := make([]Value, n)
		for i := 0; i < n; i++ {
			pairs[i] = arrVal([]Value{left[i], right[i]})
		}
		return arrVal(pairs), nil
	case "fs_read_dir":
		entries, err := r.Sandbox.ReadDir(a[0].S)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		names := make([]Value, len(entries))
		for i, e := range entries {
			names[i] = stringVal(e.Name())
		}
		return resVal(true, arrVal(names)), nil
	case "fs_create_dir":
		err := r.Sandbox.Mkdir(a[0].S, 0755)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, nilVal()), nil
	case "fs_create_dir_all":
		err := r.Sandbox.MkdirAll(a[0].S, 0755)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, nilVal()), nil
	case "fs_remove_file":
		err := r.Sandbox.Remove(a[0].S)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, nilVal()), nil
	case "fs_remove_dir_all":
		err := r.Sandbox.RemoveAll(a[0].S)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, nilVal()), nil
	case "fs_copy_file":
		src, dst := a[0].S, a[1].S
		data, err := r.Sandbox.ReadFile(src)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		if err := r.Sandbox.WriteFile(dst, data, 0644); err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, nilVal()), nil
	case "fs_move_file":
		src, dst := a[0].S, a[1].S
		if err := r.Sandbox.Rename(src, dst); err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, nilVal()), nil
	case "fs_is_file":
		info, err := r.Sandbox.Stat(a[0].S)
		if err != nil {
			return boolVal(false), nil
		}
		return boolVal(!info.IsDir()), nil
	case "fs_is_dir":
		info, err := r.Sandbox.Stat(a[0].S)
		if err != nil {
			return boolVal(false), nil
		}
		return boolVal(info.IsDir()), nil
	case "fs_file_size":
		info, err := r.Sandbox.Stat(a[0].S)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, intVal(info.Size())), nil
	case "fs_file_modified_time":
		info, err := r.Sandbox.Stat(a[0].S)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, intVal(info.ModTime().Unix())), nil
	case "fs_join_path":
		base := a[0].S
		parts := a[1].Array
		partStrs := make([]string, len(parts))
		for i, p := range parts {
			partStrs[i] = p.S
		}
		return stringVal(filepath.Join(append([]string{base}, partStrs...)...)), nil
	case "fs_absolute_path":
		abs, err := filepath.Abs(a[0].S)
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		return resVal(true, stringVal(abs)), nil
	case "fs_temp_dir":
		return stringVal(os.TempDir()), nil
	case "fs_temp_file":
		f, err := os.CreateTemp("", a[0].S+"-*")
		if err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		f.Close()
		return resVal(true, stringVal(f.Name())), nil
	case "async_sleep_ms":
		time.Sleep(time.Duration(a[0].I) * time.Millisecond)
		return nilVal(), nil
	case "async_set_timeout":
		// Simplified: just sleep and return timer ID
		timerID := r.nextTimerID
		r.nextTimerID++
		go func() {
			time.Sleep(time.Duration(a[1].I) * time.Millisecond)
		}()
		return intVal(timerID), nil
	case "async_set_interval":
		timerID := r.nextTimerID
		r.nextTimerID++
		go func() {
			ticker := time.NewTicker(time.Duration(a[1].I) * time.Millisecond)
			defer ticker.Stop()
			for range ticker.C {
				// Would call callback in full implementation
			}
		}()
		return intVal(timerID), nil
	case "async_clear_timer":
		// Simplified: no-op for now
		return nilVal(), nil
	case "async_run":
		// Execute async task synchronously for now
		return nilVal(), nil
	case "async_spawn":
		return intVal(1), nil
	case "async_wait_all":
		return arrVal([]Value{}), nil
	case "async_wait_any":
		return intVal(0), nil
	case "async_yield":
		runtime.Gosched()
		return nilVal(), nil
	case "string_repeat":
		n := a[1].I
		if n < 0 || n > 1_000_000 {
			return resVal(false, stringVal("repeat count out of range")), nil
		}
		if int64(len(a[0].S))*n > int64(r.Lim.MaxStringBytes) {
			return resVal(false, stringVal("repeated string exceeds limit")), nil
		}
		return resVal(true, stringVal(strings.Repeat(a[0].S, int(n)))), nil
	case "string_index_of":
		idx := strings.Index(a[0].S, a[1].S)
		if idx < 0 {
			return optVal(false, nilVal()), nil
		}
		return optVal(true, intVal(int64(utf8.RuneCountInString(a[0].S[:idx])))), nil
	case "string_pad_start", "string_pad_end":
		width := a[1].I
		fill := a[2].S
		if width < 0 || width > 1_000_000 {
			return resVal(false, stringVal("pad width out of range")), nil
		}
		if fill == "" {
			return resVal(false, stringVal("pad fill must not be empty")), nil
		}
		cur := int64(utf8.RuneCountInString(a[0].S))
		if cur >= width {
			return resVal(true, stringVal(a[0].S)), nil
		}
		need := int(width - cur)
		fillRunes := []rune(fill)
		pad := make([]rune, 0, need)
		for len(pad) < need {
			pad = append(pad, fillRunes...)
		}
		pad = pad[:need]
		if b.Name == "string_pad_start" {
			return resVal(true, stringVal(string(pad)+a[0].S)), nil
		}
		return resVal(true, stringVal(a[0].S+string(pad))), nil
	case "string_lines":
		parts := strings.Split(a[0].S, "\n")
		vs := make([]Value, len(parts))
		for i, x := range parts {
			vs[i] = stringVal(x)
		}
		return arrVal(vs), nil
	case "string_chars":
		runes := []rune(a[0].S)
		vs := make([]Value, len(runes))
		for i, x := range runes {
			vs[i] = stringVal(string(x))
		}
		return arrVal(vs), nil
	case "string_to_upper":
		return stringVal(strings.ToUpper(a[0].S)), nil
	case "string_to_lower":
		return stringVal(strings.ToLower(a[0].S)), nil
	case "array_sort":
		v := append([]Value{}, a[0].Array...)
		sort.SliceStable(v, func(i, j int) bool { return lessValue(v[i], v[j]) })
		return arrVal(v), nil
	case "array_index_of":
		for i, x := range a[0].Array {
			if equalValue(x, a[1]) {
				return optVal(true, intVal(int64(i))), nil
			}
		}
		return optVal(false, nilVal()), nil
	case "array_sum":
		var total int64
		for _, x := range a[0].Array {
			sum, ok := addI(total, x.I)
			if !ok {
				return bad("array_sum overflow")
			}
			total = sum
		}
		return intVal(total), nil
	case "array_min", "array_max":
		if len(a[0].Array) == 0 {
			return optVal(false, nilVal()), nil
		}
		best := a[0].Array[0].I
		for _, x := range a[0].Array[1:] {
			if (b.Name == "array_min" && x.I < best) || (b.Name == "array_max" && x.I > best) {
				best = x.I
			}
		}
		return optVal(true, intVal(best)), nil
	case "array_take", "array_drop":
		n := a[1].I
		if n < 0 || n > int64(len(a[0].Array)) {
			return bad("array_take/array_drop count out of range")
		}
		if b.Name == "array_take" {
			return arrVal(append([]Value{}, a[0].Array[:n]...)), nil
		}
		return arrVal(append([]Value{}, a[0].Array[n:]...)), nil
	case "map_contains_key":
		for _, x := range a[0].Map {
			if equalValue(x.Key, a[1]) {
				return boolVal(true), nil
			}
		}
		return boolVal(false), nil
	case "map_values":
		vs := make([]Value, len(a[0].Map))
		for i, x := range a[0].Map {
			vs[i] = x.Value
		}
		return arrVal(vs), nil
	case "map_remove":
		out := make([]MapEntry, 0, len(a[0].Map))
		for _, x := range a[0].Map {
			if !equalValue(x.Key, a[1]) {
				out = append(out, x)
			}
		}
		return Value{Kind: VMap, Map: out}, nil
	case "set_remove":
		out := make([]Value, 0, len(a[0].Set))
		for _, x := range a[0].Set {
			if !equalValue(x, a[1]) {
				out = append(out, x)
			}
		}
		return Value{Kind: VSet, Set: out}, nil
	case "set_to_array":
		return arrVal(append([]Value{}, a[0].Set...)), nil
	case "tan":
		z := numericFloat(a[0])
		q := math.Tan(z)
		if !isFinite(q) {
			return bad("tan result must be finite")
		}
		return floatVal(q), nil
	case "atan":
		return floatVal(math.Atan(numericFloat(a[0]))), nil
	case "atan2":
		return floatVal(math.Atan2(numericFloat(a[0]), numericFloat(a[1]))), nil
	case "exp":
		q := math.Exp(numericFloat(a[0]))
		if !isFinite(q) {
			return bad("exp result must be finite")
		}
		return floatVal(q), nil
	case "log10":
		z := numericFloat(a[0])
		if z <= 0 {
			return bad("log10 domain error")
		}
		return floatVal(math.Log10(z)), nil
	case "log2":
		z := numericFloat(a[0])
		if z <= 0 {
			return bad("log2 domain error")
		}
		return floatVal(math.Log2(z)), nil
	case "trunc":
		z := numericFloat(a[0])
		if !isFinite(z) {
			return bad("trunc requires a finite value")
		}
		return intVal(int64(math.Trunc(z))), nil
	case "sign":
		if a[0].Kind == VInt {
			if a[0].I > 0 {
				return intVal(1), nil
			}
			if a[0].I < 0 {
				return intVal(-1), nil
			}
			return intVal(0), nil
		}
		z := numericFloat(a[0])
		if z > 0 {
			return intVal(1), nil
		}
		if z < 0 {
			return intVal(-1), nil
		}
		return intVal(0), nil
	case "clamp":
		if a[0].Kind == VInt {
			lo, hi := a[1].I, a[2].I
			if lo > hi {
				return bad("clamp lower bound exceeds upper bound")
			}
			v := a[0].I
			if v < lo {
				v = lo
			}
			if v > hi {
				v = hi
			}
			return intVal(v), nil
		}
		lo, hi := numericFloat(a[1]), numericFloat(a[2])
		if lo > hi {
			return bad("clamp lower bound exceeds upper bound")
		}
		v := numericFloat(a[0])
		if v < lo {
			v = lo
		}
		if v > hi {
			v = hi
		}
		return floatVal(v), nil
	}
	return bad("unknown builtin")
}

// normalizeJSON converts json.Number values into Int (when integral) or Float
// so the interpreter and the native backend agree on JSON number semantics.
func normalizeJSON(v any) any {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		if f, err := t.Float64(); err == nil {
			return f
		}
		return t.String()
	case []any:
		for i := range t {
			t[i] = normalizeJSON(t[i])
		}
		return t
	case map[string]any:
		for k := range t {
			t[k] = normalizeJSON(t[k])
		}
		return t
	default:
		return v
	}
}

func (r *Runtime) httpRequestAuth(e *Expr, method, rawURL, body, token string) (Value, *Diagnostic) {
	if token == "" {
		return resVal(false, stringVal("empty authentication token")), nil
	}
	return r.doHTTP(method, rawURL, body, token)
}

func (r *Runtime) doHTTP(method, rawURL, body, token string) (Value, *Diagnostic) {
	if strings.IndexByte(rawURL, 0) >= 0 {
		return resVal(false, stringVal("URL contains NUL")), nil
	}
	req, err := http.NewRequestWithContext(r.Ctx.Ctx, method, rawURL, bytes.NewBufferString(body))
	if err != nil {
		return resVal(false, stringVal(err.Error())), nil
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: time.Duration(r.Lim.MaxWallTimeMS) * time.Millisecond}).Do(req)
	if err != nil {
		return resVal(false, stringVal(err.Error())), nil
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(r.Lim.MaxSourceBytes)+1))
	if err != nil {
		return resVal(false, stringVal(err.Error())), nil
	}
	if len(data) > r.Lim.MaxSourceBytes {
		return resVal(false, stringVal("response exceeds configured input limit")), nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resVal(false, stringVal(fmt.Sprintf("HTTP status %d", resp.StatusCode))), nil
	}
	if !validUTF8(data) {
		return resVal(false, stringVal("response is not valid UTF-8")), nil
	}
	return resVal(true, stringVal(string(data))), nil
}

func (r *Runtime) httpRequest(e *Expr, method, rawURL, body string) (Value, *Diagnostic) {
	return r.doHTTP(method, rawURL, body, "")
}

func badDiag(e *Expr, msg string) *Diagnostic {
	return Diag(CatRuntime, e.Tok.Source, e.Tok.Line, e.Tok.Column, msg)
}
func numericFloat(v Value) float64 {
	if v.Kind == VInt {
		return float64(v.I)
	}
	return v.F
}
func isFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func toBool(v Value) bool {
	switch v.Kind {
	case VNil:
		return false
	case VBool:
		return v.Bool
	case VInt:
		return v.I != 0
	case VFloat:
		return v.F != 0 && !math.IsNaN(v.F)
	case VString:
		return v.S != ""
	case VBytes:
		return len(v.Bytes) > 0
	case VArray:
		return len(v.Array) > 0
	case VOption:
		return v.Present
	case VResult:
		return v.OK
	}
	return true
}
func (r *Runtime) toInt(e *Expr, v Value) (Value, *Diagnostic) {
	switch v.Kind {
	case VInt:
		return v, nil
	case VBool:
		if v.Bool {
			return intVal(1), nil
		}
		return intVal(0), nil
	case VFloat:
		if !isFinite(v.F) || v.F < float64(math.MinInt64) || v.F > float64(math.MaxInt64) {
			return nilVal(), r.fail(e, "float is outside Int range")
		}
		return intVal(int64(v.F)), nil
	case VString:
		n, err := strconv.ParseInt(v.S, 10, 64)
		if err != nil {
			return nilVal(), r.fail(e, "String must be a complete decimal String")
		}
		return intVal(n), nil
	}
	return nilVal(), r.fail(e, "int conversion is unsupported")
}
func (r *Runtime) toFloat(e *Expr, v Value) (Value, *Diagnostic) {
	switch v.Kind {
	case VInt:
		z := float64(v.I)
		if !isFinite(z) {
			return nilVal(), r.fail(e, "conversion produced non-finite Float")
		}
		return floatVal(z), nil
	case VFloat:
		if !isFinite(v.F) {
			return nilVal(), r.fail(e, "Float must be finite")
		}
		return v, nil
	case VString:
		z, err := strconv.ParseFloat(v.S, 64)
		if err != nil || !isFinite(z) {
			return nilVal(), r.fail(e, "invalid complete Float string")
		}
		return floatVal(z), nil
	}
	return nilVal(), r.fail(e, "float conversion is unsupported")
}

// normalizeSliceIndex maps a Python-style index (which may be negative) onto
// the range [0, length]. Indices outside the range are clamped, matching the
// forgiving behaviour Python programmers expect from slicing.
func normalizeSliceIndex(i, length int64) int64 {
	if i < 0 {
		i += length
	}
	if i < 0 {
		return 0
	}
	if i > length {
		return length
	}
	return i
}

// stringSlice implements string_slice with Python semantics: negative indices
// count from the end and out-of-range bounds are clamped rather than failing.
func (r *Runtime) stringSlice(a []Value) Value {
	rs := []rune(a[0].S)
	n := int64(len(rs))
	start := normalizeSliceIndex(a[1].I, n)
	end := normalizeSliceIndex(a[2].I, n)
	if end < start {
		end = start
	}
	return resVal(true, stringVal(string(rs[start:end])))
}

// arraySliceRange implements array_slice_range with the same Python semantics.
func (r *Runtime) arraySliceRange(a []Value) Value {
	items := a[0].Array
	n := int64(len(items))
	start := normalizeSliceIndex(a[1].I, n)
	end := normalizeSliceIndex(a[2].I, n)
	if end < start {
		end = start
	}
	out := make([]Value, end-start)
	copy(out, items[start:end])
	return arrVal(out)
}

// formatTemplate substitutes {} placeholders in order. {{ and }} are literal
// braces, and a missing argument leaves the placeholder untouched so mistakes
// are visible rather than silently dropped.
func formatTemplate(tmpl string, args []Value) string {
	var b strings.Builder
	next := 0
	for i := 0; i < len(tmpl); i++ {
		c := tmpl[i]
		if c == '{' {
			if i+1 < len(tmpl) && tmpl[i+1] == '{' {
				b.WriteByte('{')
				i++
				continue
			}
			if i+1 < len(tmpl) && tmpl[i+1] == '}' {
				if next < len(args) {
					b.WriteString(display(args[next]))
					next++
				} else {
					b.WriteString("{}")
				}
				i++
				continue
			}
		}
		if c == '}' && i+1 < len(tmpl) && tmpl[i+1] == '}' {
			b.WriteByte('}')
			i++
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func (r *Runtime) substring(e *Expr, a []Value) (Value, *Diagnostic) {
	rs := []rune(a[0].S)
	start, n := a[1].I, a[2].I
	if start < 0 || n < 0 || start > int64(len(rs)) || n > int64(len(rs))-start {
		return resVal(false, stringVal("substring range is out of bounds")), nil
	}
	return resVal(true, stringVal(string(rs[start:start+n]))), nil
}
func (r *Runtime) readText(path string) (Value, *Diagnostic) {
	data, err := r.Sandbox.Read(path)
	if err != nil {
		return resVal(false, stringVal(err.Error())), nil
	}
	if !validUTF8(data) {
		return resVal(false, stringVal("file is not valid UTF-8")), nil
	}
	return resVal(true, stringVal(string(data))), nil
}
func (r *Runtime) spawn(e *Expr, name string) (Value, *Diagnostic) {
	f := r.Funcs[name]
	if f == nil || len(f.Params) != 0 {
		return nilVal(), r.fail(e, "thread worker '%s' must take no arguments", name)
	}
	if len(r.Threads) >= r.Lim.MaxWorkers {
		return nilVal(), r.fail(e, "worker limit exceeded")
	}
	ctx, cancel := context.WithCancel(r.Ctx.Ctx)
	t := &Thread{Done: make(chan struct{}), Cancel: cancel}
	r.Threads = append(r.Threads, t)
	threadsSnapshot := append([]*Thread(nil), r.Threads...)
	channelsSnapshot := append([]*Channel(nil), r.Channels...)
	channelSnapshot := make(map[string]Value)
	for n, b := range r.Global.Values {
		if b.Value.Kind == VChannel || b.Value.Kind == VShared {
			channelSnapshot[n] = b.Value
		}
	}
	go func() {
		defer close(t.Done)
		wr := &Runtime{Prog: r.Prog, Checker: r.Checker, Funcs: r.Funcs, Global: newRunScope(nil), Lim: r.Lim, Sandbox: r.Sandbox, Ctx: &ExecContext{Ctx: ctx, Cancel: cancel, Lim: r.Lim}, Channels: channelsSnapshot, Threads: threadsSnapshot, Worker: true}
		wr.Worker = true
		for n, v := range channelSnapshot {
			_ = wr.Global.define(n, v, false)
		}
		child := newRunScope(wr.Global)
		child.ReturnType = mustResolve(wr.Checker.Env, f.Return)
		wr.Ctx.Calls = 1
		x := wr.execBlock(child, f.Body)
		t.mu.Lock()
		if x.Diag != nil {
			t.Diag = x.Diag
			t.Result = nilVal()
		} else if x.Code == evalReturn {
			t.Result = x.Value
		} else {
			t.Result = nilVal()
		}
		t.mu.Unlock()
	}()
	return Value{Kind: VThread, Th: t}, nil
}
func (r *Runtime) join(e *Expr, t *Thread) (Value, *Diagnostic) {
	if t == nil {
		return nilVal(), r.fail(e, "invalid thread")
	}
	<-t.Done
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Joined = true
	if t.Diag != nil {
		return nilVal(), t.Diag
	}
	return cloneValue(t.Result), nil
}
func (r *Runtime) joinTimeout(e *Expr, t *Thread, ms int64) (Value, *Diagnostic) {
	if t == nil {
		return nilVal(), r.fail(e, "invalid thread")
	}
	timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-t.Done:
		t.mu.Lock()
		defer t.mu.Unlock()
		t.Joined = true
		if t.Diag != nil {
			return resVal(false, stringVal(t.Diag.Message)), nil
		}
		return resVal(true, cloneValue(t.Result)), nil
	case <-timer.C:
		return resVal(false, stringVal("timeout")), nil
	}
}

// RunForREPL executes a checked snippet without discarding the session's observable output.
// The CLI rebuilds the checked declaration prefix for each snippet, so failed snippets
// never mutate the persistent definition set.
func (r *Runtime) RunForREPL() *Diagnostic {
	for _, s := range r.Prog.Statements {
		if d := r.Ctx.step(s.Tok.Source, s.Tok.Line, s.Tok.Column); d != nil {
			return d
		}
		if s.Kind == StExpr {
			v, d := r.evalExpr(r.Global, s.Expr)
			if d != nil {
				return d
			}
			if v.Kind != VNil {
				if d := r.printValue(v, true); d != nil {
					return d
				}
			}
			continue
		}
		x := r.execStmt(r.Global, s)
		if x.Diag != nil {
			return x.Diag
		}
		if x.Code != evalNormal {
			return Diag(CatRuntime, s.Tok.Source, s.Tok.Line, s.Tok.Column, "control flow escaped REPL")
		}
	}
	return nil
}
