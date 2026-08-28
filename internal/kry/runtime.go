package kry

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
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
)

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

type Thread struct {
	Done   chan struct{}
	Cancel context.CancelFunc
	mu     sync.Mutex
	Result Value
	Diag   *Diagnostic
	Joined bool
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
	r := &Runtime{Prog: prog, Checker: c, Funcs: c.Env.Functions, Global: newRunScope(nil), Lim: lim, Sandbox: sb, Ctx: &ExecContext{Ctx: ctx, Cancel: cancel, Lim: lim}, Channels: nil, Threads: nil}
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
	case StLet:
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
		f = r.Funcs[e.Name]
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
	if x.Code == evalReturn {
		return x.Value, nil
	}
	return nilVal(), nil
}
func (r *Runtime) evalBuiltin(e *Expr, b Builtin, a []Value) (Value, *Diagnostic) {
	bad := func(m string) (Value, *Diagnostic) { return nilVal(), r.fail(e, m) }
	switch b.Name {
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
		var raw any
		if err := json.Unmarshal([]byte(a[0].S), &raw); err != nil {
			return resVal(false, stringVal(err.Error())), nil
		}
		canon, err := json.Marshal(raw)
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
	case "thread_join_timeout":
		if a[1].I < 0 {
			return bad("timeout duration cannot be negative")
		}
		return r.joinTimeout(e, a[0].Th, a[1].I)
	case "thread_cancel":
		a[0].Th.Cancel()
		return nilVal(), nil
	case "thread_close":
		a[0].Ch.close()
		return nilVal(), nil
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
	}
	return bad("unknown builtin")
}
func (r *Runtime) httpRequestAuth(e *Expr, method, rawURL, body, token string) (Value, *Diagnostic) {
	if token == "" {
		return resVal(false, stringVal("empty authentication token")), nil
	}
	req, err := http.NewRequestWithContext(r.Ctx.Ctx, method, rawURL, bytes.NewBufferString(body))
	if err != nil {
		return resVal(false, stringVal(err.Error())), nil
	}
	req.Header.Set("Authorization", "Bearer "+token)
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
	if strings.IndexByte(rawURL, 0) >= 0 {
		return resVal(false, stringVal("URL contains NUL")), nil
	}
	req, err := http.NewRequestWithContext(r.Ctx.Ctx, method, rawURL, bytes.NewBufferString(body))
	if err != nil {
		return resVal(false, stringVal(err.Error())), nil
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
	channelSnapshot := make(map[string]Value)
	for n, b := range r.Global.Values {
		if b.Value.Kind == VChannel {
			channelSnapshot[n] = b.Value
		}
	}
	go func() {
		defer close(t.Done)
		wr := &Runtime{Prog: r.Prog, Checker: r.Checker, Funcs: r.Funcs, Global: newRunScope(nil), Lim: r.Lim, Sandbox: r.Sandbox, Ctx: &ExecContext{Ctx: ctx, Cancel: cancel, Lim: r.Lim}, Channels: r.Channels, Threads: r.Threads, Worker: true}
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
