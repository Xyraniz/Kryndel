package kry

import (
	"fmt"
	"sort"
)

const (
	WarnRedundantMatch = "KRYW001"
	WarnConstantBranch = "KRYW002"
	WarnInfiniteLoop   = "KRYW003"
	WarnUnreachable    = "KRYW004"
)

// AnalyzeWarnings finds conservative source-level warnings after a program has
// passed the checker. Warnings never change program execution by themselves.
func AnalyzeWarnings(program *Program) []*Diagnostic {
	if program == nil {
		return nil
	}
	var warnings []*Diagnostic
	add := func(code string, tok Token, message string) {
		d := Diag(CatType, tok.Source, tok.Line, tok.Column, "%s", message)
		d.Code = code
		d.Severity = "warning"
		warnings = append(warnings, d)
	}
	var block func([]*Stmt)
	block = func(statements []*Stmt) {
		terminated := false
		for _, stmt := range statements {
			if stmt == nil {
				continue
			}
			if terminated {
				add(WarnUnreachable, stmt.Tok, "statement is unreachable after control flow has exited this block")
			}
			switch stmt.Kind {
			case StIf:
				if v, ok := constantValue(stmt.Cond); ok && v.Kind == VBool {
					add(WarnConstantBranch, stmt.Cond.Tok, fmt.Sprintf("if condition is always %t", v.Bool))
				}
				block(stmt.Then)
				block(stmt.Else)
			case StWhile:
				if v, ok := constantValue(stmt.Cond); ok && v.Kind == VBool {
					if !v.Bool {
						add(WarnConstantBranch, stmt.Cond.Tok, "while condition is always false")
					} else if !hasCurrentLoopBreak(stmt.Body) {
						add(WarnInfiniteLoop, stmt.Cond.Tok, "while loop is constant true and has no break")
					}
				}
				block(stmt.Body)
			case StFor:
				block(stmt.Body)
			case StMatch:
				seen := map[string]bool{}
				wildcard := -1
				for i, arm := range stmt.Arms {
					key := patternIdentity(arm.Pattern)
					if seen[key] {
						add(WarnRedundantMatch, arm.Pattern.Tok, "match arm repeats an earlier pattern")
					}
					seen[key] = true
					if arm.Pattern.Kind == PatWildcard {
						if wildcard < 0 {
							wildcard = i
							if i > 0 {
								add(WarnRedundantMatch, arm.Pattern.Tok, "wildcard arm hides enum, option, or result cases that could be listed explicitly")
							}
						} else {
							add(WarnRedundantMatch, arm.Pattern.Tok, "match arm repeats an earlier wildcard")
						}
					}
					if wildcard >= 0 && i > wildcard {
						add(WarnUnreachable, arm.Pattern.Tok, "match arm is unreachable after a wildcard arm")
					}
					block(arm.Body)
				}
			case StUnsafe, StDefer:
				block(stmt.Body)
			}
			if alwaysExits(stmt) {
				terminated = true
			}
		}
	}
	for _, function := range program.Functions {
		block(function.Body)
	}
	block(program.Statements)
	sort.SliceStable(warnings, func(i, j int) bool {
		if warnings[i].Source != warnings[j].Source {
			return warnings[i].Source < warnings[j].Source
		}
		if warnings[i].Line != warnings[j].Line {
			return warnings[i].Line < warnings[j].Line
		}
		if warnings[i].Column != warnings[j].Column {
			return warnings[i].Column < warnings[j].Column
		}
		return warnings[i].Code < warnings[j].Code
	})
	return warnings
}

func patternIdentity(pattern Pattern) string {
	switch pattern.Kind {
	case PatWildcard:
		return "wildcard"
	case PatBool:
		return fmt.Sprintf("bool:%t", pattern.Bool)
	case PatInt:
		return fmt.Sprintf("int:%d", pattern.Int)
	case PatString:
		return "string:" + pattern.Str
	case PatEnum:
		return "enum:" + pattern.TypeName + "." + pattern.Variant
	case PatOption:
		return fmt.Sprintf("option:%t", pattern.Present)
	case PatResult:
		return fmt.Sprintf("result:%t", pattern.OK)
	case PatNil:
		return "nil"
	default:
		return fmt.Sprintf("pattern:%d", pattern.Kind)
	}
}

func alwaysExits(stmt *Stmt) bool {
	if stmt == nil {
		return false
	}
	switch stmt.Kind {
	case StReturn, StBreak, StContinue:
		return true
	case StIf:
		return len(stmt.Else) != 0 && blockExits(stmt.Then) && blockExits(stmt.Else)
	case StWhile:
		v, ok := constantValue(stmt.Cond)
		return ok && v.Kind == VBool && v.Bool && !hasCurrentLoopBreak(stmt.Body)
	case StMatch:
		if len(stmt.Arms) == 0 {
			return false
		}
		for _, arm := range stmt.Arms {
			if !blockExits(arm.Body) {
				return false
			}
		}
		return true
	case StUnsafe:
		return blockExits(stmt.Body)
	default:
		return false
	}
}

func blockExits(statements []*Stmt) bool {
	for _, stmt := range statements {
		if alwaysExits(stmt) {
			return true
		}
	}
	return false
}

func hasCurrentLoopBreak(statements []*Stmt) bool {
	for _, stmt := range statements {
		if stmt == nil {
			continue
		}
		if stmt.Kind == StBreak {
			return true
		}
		if stmt.Kind == StWhile || stmt.Kind == StFor {
			continue
		}
		if hasCurrentLoopBreak(stmt.Then) || hasCurrentLoopBreak(stmt.Else) || hasCurrentLoopBreak(stmt.Body) {
			return true
		}
		for _, arm := range stmt.Arms {
			if hasCurrentLoopBreak(arm.Body) {
				return true
			}
		}
	}
	return false
}
