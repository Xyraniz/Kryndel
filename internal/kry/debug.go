package kry

import (
	"sort"
	"strings"
)

// Debugger pauses the interpreter before selected statements. ShouldPause is
// called for each statement with only source metadata; OnPause receives the
// more expensive stack and variable snapshot only when execution stops.
type Debugger struct {
	ShouldPause func(DebugLocation) bool
	OnPause     func(DebugState) bool
}

type DebugLocation struct {
	Source   string
	Line     int
	Column   int
	Function string
	Depth    int
}

type DebugState struct {
	DebugLocation
	LineText  string
	Stack     []StackFrame
	Variables []DebugVariable
}

type DebugVariable struct {
	Name    string
	Value   string
	Mutable bool
}

func (r *Runtime) debugStatement(scope *RunScope, statement *Stmt) bool {
	if r.debugAbort != nil {
		return false
	}
	if r.debugger == nil || r.debugger.ShouldPause == nil || r.debugger.OnPause == nil {
		return true
	}
	source := "<input>"
	if statement.Tok.Source != nil && statement.Tok.Source.Name != "" {
		source = statement.Tok.Source.Name
	}
	function := "<module>"
	if count := len(r.debugFrames); count > 0 {
		function = r.debugFrames[count-1].Function
	}
	location := DebugLocation{Source: source, Line: statement.Tok.Line, Column: statement.Tok.Column, Function: function, Depth: r.Ctx.Calls}
	if !r.debugger.ShouldPause(location) {
		return true
	}
	state := DebugState{DebugLocation: location}
	if statement.Tok.Source != nil {
		lines := strings.Split(statement.Tok.Source.Text, "\n")
		if location.Line > 0 && location.Line <= len(lines) {
			state.LineText = strings.TrimSuffix(lines[location.Line-1], "\r")
		}
	}
	if count := len(r.debugFrames); count == 0 {
		state.Stack = []StackFrame{{Function: function, Source: source, Line: location.Line, Column: location.Column}}
	} else {
		state.Stack = make([]StackFrame, 0, count)
		for i := count - 1; i >= 0; i-- {
			state.Stack = append(state.Stack, r.debugFrames[i])
		}
		state.Stack[0].Source = source
		state.Stack[0].Line = location.Line
		state.Stack[0].Column = location.Column
	}
	seen := make(map[string]struct{})
	for current := scope; current != nil; current = current.Parent {
		for name, binding := range current.Values {
			if _, shadowed := seen[name]; shadowed {
				continue
			}
			seen[name] = struct{}{}
			state.Variables = append(state.Variables, DebugVariable{Name: name, Value: display(binding.Value), Mutable: binding.Mutable})
		}
	}
	sort.Slice(state.Variables, func(i, j int) bool { return state.Variables[i].Name < state.Variables[j].Name })
	if r.debugger.OnPause(state) {
		return true
	}
	r.debugAbort = Diag(CatCLI, statement.Tok.Source, statement.Tok.Line, statement.Tok.Column, "execution stopped by debugger")
	return false
}
