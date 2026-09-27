package main

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/Xyraniz/Kryndel/internal/kry"
)

type debugBreakpoint struct {
	id     int
	source string
	line   int
}

type debugMode uint8

const (
	debugContinue debugMode = iota
	debugStep
	debugNext
	debugFinish
)

type debugCLI struct {
	scanner      *bufio.Scanner
	out          io.Writer
	err          io.Writer
	breakpoints  map[int]debugBreakpoint
	nextID       int
	started      bool
	mode         debugMode
	anchorDepth  int
	nextTailCall bool
	quit         bool
}

func debugCmd(engine *kry.Engine, args []string, input io.Reader, output, errorOutput io.Writer) int {
	if len(args) < 1 {
		return usage("debug expects a source or artifact path")
	}
	cli := &debugCLI{scanner: bufio.NewScanner(input), out: output, err: errorOutput, breakpoints: make(map[int]debugBreakpoint)}
	cli.scanner.Buffer(make([]byte, 4096), 1<<20)
	debugger := kry.Debugger{ShouldPause: cli.shouldPause, OnPause: cli.pause}
	if diagnostic := engine.DebugPathWithArgs(args[0], args[1:], debugger); diagnostic != nil {
		return report(diagnostic, false)
	}
	if cli.quit {
		fmt.Fprintln(output, "Program stopped by debugger.")
	} else {
		fmt.Fprintln(output, "Program exited.")
	}
	return 0
}

func (d *debugCLI) shouldPause(location kry.DebugLocation) bool {
	if !d.started {
		return true
	}
	switch d.mode {
	case debugStep:
		return true
	case debugNext:
		if d.nextTailCall {
			if location.Depth < d.anchorDepth {
				d.nextTailCall = false
				return true
			}
		} else if location.Depth <= d.anchorDepth {
			return true
		}
	case debugFinish:
		if location.Depth < d.anchorDepth {
			return true
		}
	}
	for _, breakpoint := range d.breakpoints {
		if breakpoint.line == location.Line && debugSourceKey(breakpoint.source) == debugSourceKey(location.Source) {
			return true
		}
	}
	return false
}

func (d *debugCLI) pause(state kry.DebugState) bool {
	d.started = true
	fmt.Fprintf(d.out, "\nPaused at %s:%d:%d in %s (depth %d)\n", state.Source, state.Line, state.Column, state.Function, state.Depth)
	if state.LineText != "" {
		fmt.Fprintf(d.out, "> %4d | %s\n", state.Line, strings.TrimSpace(state.LineText))
	}
	for {
		fmt.Fprint(d.out, "(kry-debug) ")
		if !d.scanner.Scan() {
			if err := d.scanner.Err(); err != nil {
				fmt.Fprintf(d.err, "debugger: cannot read command: %v\n", err)
			} else {
				fmt.Fprintln(d.err, "debugger: input closed; execution stopped")
			}
			d.quit = true
			return false
		}
		line := strings.TrimSpace(d.scanner.Text())
		if line == "" {
			continue
		}
		command, argument, _ := strings.Cut(line, " ")
		command = strings.ToLower(command)
		argument = strings.TrimSpace(argument)
		switch command {
		case "help", "h", "?":
			fmt.Fprintln(d.out, "break FILE:LINE | break LINE, breakpoints, clear ID|all, locals, print NAME, stack, continue, step, next, finish, quit")
		case "break", "b":
			d.addBreakpoint(argument, state.Source)
		case "breakpoints", "info":
			d.listBreakpoints()
		case "clear", "delete":
			d.clearBreakpoint(argument)
		case "locals":
			d.printLocals(state)
		case "print", "p":
			d.printVariable(state, argument)
		case "stack", "bt":
			for index, frame := range state.Stack {
				fmt.Fprintf(d.out, "#%d %s at %s:%d:%d\n", index, frame.Function, frame.Source, frame.Line, frame.Column)
			}
		case "continue", "c":
			d.mode = debugContinue
			d.nextTailCall = false
			return true
		case "step", "s":
			d.mode = debugStep
			d.anchorDepth = state.Depth
			d.nextTailCall = false
			return true
		case "next", "n":
			d.mode = debugNext
			d.anchorDepth = state.Depth
			d.nextTailCall = state.TailCall
			return true
		case "finish", "return":
			if state.Depth == 0 {
				fmt.Fprintln(d.err, "debugger: finish is available inside a function")
				continue
			}
			d.mode = debugFinish
			d.anchorDepth = state.Depth
			d.nextTailCall = false
			return true
		case "quit", "q":
			d.quit = true
			return false
		default:
			fmt.Fprintf(d.err, "debugger: unknown command %q; use help\n", command)
		}
	}
}

func (d *debugCLI) addBreakpoint(argument, currentSource string) {
	if argument == "" {
		fmt.Fprintln(d.err, "debugger: break expects LINE or FILE:LINE")
		return
	}
	source := currentSource
	lineText := argument
	if separator := strings.LastIndex(argument, ":"); separator >= 0 {
		source, lineText = argument[:separator], argument[separator+1:]
	}
	line, err := strconv.Atoi(lineText)
	if err != nil || line < 1 {
		fmt.Fprintln(d.err, "debugger: breakpoint line must be a positive integer")
		return
	}
	if source == "" {
		fmt.Fprintln(d.err, "debugger: breakpoint source is empty")
		return
	}
	d.nextID++
	d.breakpoints[d.nextID] = debugBreakpoint{id: d.nextID, source: source, line: line}
	fmt.Fprintf(d.out, "Breakpoint %d at %s:%d\n", d.nextID, source, line)
}

func (d *debugCLI) listBreakpoints() {
	ids := make([]int, 0, len(d.breakpoints))
	for id := range d.breakpoints {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	if len(ids) == 0 {
		fmt.Fprintln(d.out, "No breakpoints.")
		return
	}
	for _, id := range ids {
		breakpoint := d.breakpoints[id]
		fmt.Fprintf(d.out, "Breakpoint %d at %s:%d\n", id, breakpoint.source, breakpoint.line)
	}
}

func (d *debugCLI) clearBreakpoint(argument string) {
	if strings.EqualFold(argument, "all") {
		d.breakpoints = make(map[int]debugBreakpoint)
		fmt.Fprintln(d.out, "All breakpoints cleared.")
		return
	}
	id, err := strconv.Atoi(argument)
	if err != nil || id < 1 {
		fmt.Fprintln(d.err, "debugger: clear expects a breakpoint ID or all")
		return
	}
	if _, ok := d.breakpoints[id]; !ok {
		fmt.Fprintf(d.err, "debugger: breakpoint %d does not exist\n", id)
		return
	}
	delete(d.breakpoints, id)
	fmt.Fprintf(d.out, "Breakpoint %d cleared.\n", id)
}

func (d *debugCLI) printLocals(state kry.DebugState) {
	if len(state.Variables) == 0 {
		fmt.Fprintln(d.out, "No visible variables.")
		return
	}
	for _, variable := range state.Variables {
		mutability := ""
		if variable.Mutable {
			mutability = " (mutable)"
		}
		fmt.Fprintf(d.out, "%s%s = %s\n", variable.Name, mutability, variable.Value)
	}
}

func (d *debugCLI) printVariable(state kry.DebugState, name string) {
	if name == "" {
		fmt.Fprintln(d.err, "debugger: print expects a variable name")
		return
	}
	for _, variable := range state.Variables {
		if variable.Name == name {
			fmt.Fprintf(d.out, "%s = %s\n", variable.Name, variable.Value)
			return
		}
	}
	fmt.Fprintf(d.err, "debugger: variable %q is not visible\n", name)
}

func debugSourceKey(source string) string {
	path, err := filepath.Abs(source)
	if err == nil {
		source = path
	}
	source = filepath.Clean(source)
	if runtime.GOOS == "windows" {
		source = strings.ToLower(source)
	}
	return source
}
