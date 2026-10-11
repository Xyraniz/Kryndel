package kry

import (
	"fmt"
	"strings"
)

// validateBuiltinSignature checks the type-level contract declared by a
// Builtin.Signature against already-checked KIR type strings. It does not read
// or construct source AST. Rules that need KIR values, declarations, or
// constraints absent from the signature are reported by
// builtinSignatureContextualRules and must be checked separately.
func validateBuiltinSignature(builtin Builtin, argumentTypes []string, resultType string) error {
	return validateBuiltinSignatureWithConstraints(builtin, argumentTypes, resultType, nil)
}

// validateBuiltinSignatureWithConstraints also checks already-validated KIR
// generic constraints. Only builtins whose source checker uses numeric(t)
// accept Numeric/Integer type parameters in numeric signatures.
func validateBuiltinSignatureWithConstraints(builtin Builtin, argumentTypes []string, resultType string, genericConstraints map[string]string) error {
	signature, err := parseBuiltinSignature(builtin)
	if err != nil {
		return err
	}
	if len(argumentTypes) != len(signature.arguments) {
		return fmt.Errorf("builtin %q has %d argument types, want %d", builtin.Name, len(argumentTypes), len(signature.arguments))
	}
	if len(argumentTypes) != builtin.Arity {
		return fmt.Errorf("builtin %q signature arity %d does not match registry arity %d", builtin.Name, len(signature.arguments), builtin.Arity)
	}

	bindings := make(map[string]string)
	choices := make(map[string]string)
	for index, encoded := range argumentTypes {
		actual, parseErr := parseBuiltinActualType(encoded)
		if parseErr != nil {
			return fmt.Errorf("builtin %q argument %d: %w", builtin.Name, index+1, parseErr)
		}
		if builtin.Name == "len" && index == 0 && builtinLenAcceptsMapOrSet(actual) {
			continue
		}
		if matchErr := matchBuiltinTypePattern(builtin.Name, signature.arguments[index].typePattern, actual, bindings, choices, genericConstraints); matchErr != nil {
			return fmt.Errorf("builtin %q argument %d has type %q: %w", builtin.Name, index+1, encoded, matchErr)
		}
	}

	actualResult, err := parseBuiltinActualType(resultType)
	if err != nil {
		return fmt.Errorf("builtin %q result: %w", builtin.Name, err)
	}
	if matchErr := matchBuiltinTypePattern(builtin.Name, signature.result, actualResult, bindings, choices, genericConstraints); matchErr != nil {
		return fmt.Errorf("builtin %q result has type %q: %w", builtin.Name, resultType, matchErr)
	}
	return nil
}

// builtinSignatureContextualRules lists semantic checks that the prose
// signature cannot express. The returned slice is a copy and may be retained
// by callers.
func builtinSignatureContextualRules(name string) []string {
	rules := builtinContextualRules[name]
	return append([]string(nil), rules...)
}

var builtinContextualRules = map[string][]string{
	"len": {
		"the checker also accepts Map[K,V] and Set[T], omitted from Builtin.Signature",
	},
	"none": {
		"the source checker requires an Option[T] result context",
	},
	"ok": {
		"the source checker uses an expected Result[T,E] context when present; without it the error type defaults to Nil",
	},
	"err": {
		"the source checker uses an expected Result[T,E] context when present; without it the value type defaults to Nil",
	},
	"assert_eq": {
		"the checker rejects values whose type contains a function",
	},
	"array_contains": {
		"the checker rejects element types that contain a function",
	},
	"array_index_of": {
		"the checker rejects element types that contain a function",
	},
	"set_contains": {
		"the checker rejects element types that contain a function",
	},
	"set_insert": {
		"the checker rejects element types that contain a function",
	},
	"set_remove": {
		"the checker rejects element types that contain a function",
	},
	"thread_spawn": {
		"the argument must be a string literal naming a zero-argument worker, and T must equal that worker's return type",
	},
	"thread_channel": {
		"the source checker requires a Channel[T] result context",
	},
	"thread_channel_with_capacity": {
		"the source checker requires a Channel[T] result context",
	},
	"thread_receive_timeout": {
		"the checker rejects a syntactically negative duration literal",
	},
	"thread_send": {
		"the channel element type must be recursively Copy",
	},
	"thread_try_send": {
		"the channel element type must be recursively Copy",
	},
	"thread_send_timeout": {
		"the channel element type must be recursively Copy",
	},
	"shared_new": {
		"T must be recursively Copy and the result requires Shared[T] context",
	},
	"shared_write": {
		"T must be recursively Copy",
	},
	"shared_swap": {
		"T must be recursively Copy",
	},
	"actor_send": {
		"T must be recursively Copy",
	},
	"actor_channel": {
		"the source checker requires an Actor[T] result context",
	},
	"actor_channel_with_capacity": {
		"the source checker requires an Actor[T] result context",
	},
	"task_spawn": {
		"the argument must be a string literal naming a zero-argument worker, and T must equal that worker's return type",
	},
	"poly_register": {
		"a literal handler name must resolve to one top-level fn(String) -> String function",
	},
	"poly_reorder": {
		"literal handler names must resolve to top-level fn(String) -> String functions",
	},
}

type builtinTypeSignature struct {
	name      string
	arguments []builtinSignatureArgument
	result    *builtinTypePattern
}

type builtinSignatureArgument struct {
	name        string
	typePattern *builtinTypePattern
}

type builtinTypePattern struct {
	name         string
	parameters   []*builtinTypePattern
	alternatives []*builtinTypePattern
}

func parseBuiltinSignature(builtin Builtin) (*builtinTypeSignature, error) {
	text := strings.TrimSpace(builtin.Signature)
	open := strings.IndexByte(text, '(')
	if open <= 0 {
		return nil, fmt.Errorf("builtin %q has a malformed signature", builtin.Name)
	}
	name := strings.TrimSpace(text[:open])
	if name == "" || name != builtin.Name {
		return nil, fmt.Errorf("builtin %q signature names %q", builtin.Name, name)
	}
	close, err := builtinSignatureCloseParen(text, open)
	if err != nil {
		return nil, fmt.Errorf("builtin %q: %w", builtin.Name, err)
	}
	rest := strings.TrimSpace(text[close+1:])
	if !strings.HasPrefix(rest, "->") {
		return nil, fmt.Errorf("builtin %q signature is missing its result arrow", builtin.Name)
	}
	resultText := strings.TrimSpace(strings.TrimPrefix(rest, "->"))
	if resultText == "" {
		return nil, fmt.Errorf("builtin %q signature has an empty result type", builtin.Name)
	}

	arguments := make([]builtinSignatureArgument, 0)
	argumentText := strings.TrimSpace(text[open+1 : close])
	if argumentText != "" {
		parts, splitErr := splitBuiltinSignatureList(argumentText, ',')
		if splitErr != nil {
			return nil, fmt.Errorf("builtin %q arguments: %w", builtin.Name, splitErr)
		}
		for _, part := range parts {
			colon := strings.IndexByte(part, ':')
			if colon <= 0 {
				return nil, fmt.Errorf("builtin %q has a malformed parameter %q", builtin.Name, part)
			}
			argumentName := strings.TrimSpace(part[:colon])
			if !isBuiltinSignatureIdentifier(argumentName) {
				return nil, fmt.Errorf("builtin %q has an invalid parameter name %q", builtin.Name, argumentName)
			}
			pattern, parseErr := parseBuiltinTypePattern(strings.TrimSpace(part[colon+1:]))
			if parseErr != nil {
				return nil, fmt.Errorf("builtin %q parameter %q: %w", builtin.Name, argumentName, parseErr)
			}
			arguments = append(arguments, builtinSignatureArgument{name: argumentName, typePattern: pattern})
		}
	}
	if len(arguments) != builtin.Arity {
		return nil, fmt.Errorf("builtin %q signature has %d parameters, registry says %d", builtin.Name, len(arguments), builtin.Arity)
	}
	result, err := parseBuiltinTypePattern(resultText)
	if err != nil {
		return nil, fmt.Errorf("builtin %q result: %w", builtin.Name, err)
	}
	return &builtinTypeSignature{name: name, arguments: arguments, result: result}, nil
}

func builtinSignatureCloseParen(text string, open int) (int, error) {
	parenDepth, bracketDepth := 0, 0
	for index := open; index < len(text); index++ {
		switch text[index] {
		case '[':
			bracketDepth++
		case ']':
			bracketDepth--
			if bracketDepth < 0 {
				return 0, fmt.Errorf("signature has an unmatched closing bracket")
			}
		case '(':
			if bracketDepth == 0 {
				parenDepth++
			}
		case ')':
			if bracketDepth == 0 {
				parenDepth--
				if parenDepth == 0 {
					return index, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("signature has an unmatched opening parenthesis")
}

func splitBuiltinSignatureList(text string, delimiter byte) ([]string, error) {
	parts := make([]string, 0, 2)
	start, squareDepth, parenDepth := 0, 0, 0
	for index := 0; index < len(text); index++ {
		switch text[index] {
		case '[':
			squareDepth++
		case ']':
			squareDepth--
			if squareDepth < 0 {
				return nil, fmt.Errorf("unmatched closing bracket")
			}
		case '(':
			parenDepth++
		case ')':
			parenDepth--
			if parenDepth < 0 {
				return nil, fmt.Errorf("unmatched closing parenthesis")
			}
		default:
			if text[index] == delimiter && squareDepth == 0 && parenDepth == 0 {
				part := strings.TrimSpace(text[start:index])
				if part == "" {
					return nil, fmt.Errorf("empty signature item")
				}
				parts = append(parts, part)
				start = index + 1
			}
		}
	}
	if squareDepth != 0 || parenDepth != 0 {
		return nil, fmt.Errorf("unbalanced type delimiters")
	}
	last := strings.TrimSpace(text[start:])
	if last == "" {
		return nil, fmt.Errorf("empty signature item")
	}
	return append(parts, last), nil
}

func parseBuiltinTypePattern(encoded string) (*builtinTypePattern, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, fmt.Errorf("empty type pattern")
	}
	parts, err := splitBuiltinSignatureList(encoded, '|')
	if err != nil {
		return nil, err
	}
	if len(parts) > 1 {
		pattern := &builtinTypePattern{alternatives: make([]*builtinTypePattern, 0, len(parts))}
		for _, part := range parts {
			option, parseErr := parseBuiltinTypePattern(part)
			if parseErr != nil {
				return nil, parseErr
			}
			pattern.alternatives = append(pattern.alternatives, option)
		}
		return pattern, nil
	}

	if strings.HasSuffix(encoded, "]") {
		open := strings.IndexByte(encoded, '[')
		if open <= 0 {
			return nil, fmt.Errorf("malformed generic type %q", encoded)
		}
		name := strings.TrimSpace(encoded[:open])
		if !isBuiltinSignatureIdentifier(name) {
			return nil, fmt.Errorf("invalid type name %q", name)
		}
		inner := encoded[open+1 : len(encoded)-1]
		parts, splitErr := splitBuiltinSignatureList(inner, ',')
		if splitErr != nil {
			return nil, splitErr
		}
		pattern := &builtinTypePattern{name: name, parameters: make([]*builtinTypePattern, 0, len(parts))}
		for _, part := range parts {
			parameter, parseErr := parseBuiltinTypePattern(part)
			if parseErr != nil {
				return nil, parseErr
			}
			pattern.parameters = append(pattern.parameters, parameter)
		}
		return pattern, nil
	}
	if strings.ContainsAny(encoded, "[](),|") || !isBuiltinSignatureIdentifier(encoded) {
		return nil, fmt.Errorf("malformed type pattern %q", encoded)
	}
	return &builtinTypePattern{name: encoded}, nil
}

func isBuiltinSignatureIdentifier(encoded string) bool {
	if encoded == "" {
		return false
	}
	for index, character := range encoded {
		if character == '_' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || index > 0 && character >= '0' && character <= '9' {
			continue
		}
		return false
	}
	return true
}

func parseBuiltinActualType(encoded string) (*TypeSpec, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, fmt.Errorf("empty checked type")
	}
	// Array[<unknown>] is emitted by the source checker for a genuinely
	// unparameterized array. Use an identifier absent from the original type,
	// then restore only those inserted nodes. A fixed legal identifier could
	// collide with a real user-defined type and incorrectly bypass a contract.
	sentinel := "__KIR_UNKNOWN_SENTINEL__"
	for strings.Contains(encoded, sentinel) {
		sentinel += "_"
	}
	parserText := strings.ReplaceAll(encoded, "<unknown>", sentinel)
	spec, valid := parseKIRTypeExpression(parserText)
	if !valid || spec == nil {
		return nil, fmt.Errorf("invalid checked type %q", encoded)
	}
	spec = cloneKIRTypeSpec(spec)
	restoreBuiltinUnknownType(spec, sentinel)
	return spec, nil
}

func restoreBuiltinUnknownType(spec *TypeSpec, sentinel string) {
	if spec == nil {
		return
	}
	if spec.Name == sentinel {
		spec.Name = "<unknown>"
	}
	for _, parameter := range spec.Params {
		restoreBuiltinUnknownType(parameter, sentinel)
	}
	restoreBuiltinUnknownType(spec.Return, sentinel)
}

func builtinTypeSpecString(spec *TypeSpec) string {
	return TypeSpecString(spec)
}

func matchBuiltinTypePattern(builtinName string, pattern *builtinTypePattern, actual *TypeSpec, bindings, choices, genericConstraints map[string]string) error {
	if pattern == nil || actual == nil {
		return fmt.Errorf("incomplete signature type")
	}
	if len(pattern.alternatives) != 0 {
		key := builtinTypePatternString(pattern)
		actualText := builtinTypeSpecString(actual)
		if previous, exists := choices[key]; exists {
			if previous != actualText {
				return fmt.Errorf("does not match the already selected %s alternative %q", key, previous)
			}
			if builtinNumericGenericMatches(builtinName, pattern, actual, genericConstraints) {
				return nil
			}
			return matchBuiltinTypeAlternative(builtinName, pattern, actual, bindings, choices, genericConstraints)
		}
		if builtinNumericGenericMatches(builtinName, pattern, actual, genericConstraints) {
			choices[key] = actualText
			return nil
		}
		return matchBuiltinTypeAlternative(builtinName, pattern, actual, bindings, choices, genericConstraints)
	}
	if isBuiltinSignatureVariable(pattern.name) {
		if actual.Name == "<unknown>" && len(actual.Params) == 0 && !actual.Function {
			return nil
		}
		actualText := builtinTypeSpecString(actual)
		if previous, exists := bindings[pattern.name]; exists {
			if previous != actualText {
				return fmt.Errorf("generic %s resolves to both %q and %q", pattern.name, previous, actualText)
			}
			return nil
		}
		bindings[pattern.name] = actualText
		return nil
	}
	if pattern.name == "UInt" {
		if !actual.Function && len(actual.Params) == 0 {
			switch actual.Name {
			case "UInt8", "UInt16", "UInt32", "UInt64":
				return nil
			}
		}
		return fmt.Errorf("does not match signature type UInt (UInt8, UInt16, UInt32, or UInt64)")
	}
	if pattern.name == "Display" {
		// Display is a source-level marker rather than a type constructor. The
		// runtime can render every valid Value type; ordinary KIR type validation
		// remains responsible for rejecting unknown or malformed types.
		return nil
	}
	if actual.Function || actual.Name != pattern.name || len(actual.Params) != len(pattern.parameters) {
		return fmt.Errorf("does not match signature type %s", builtinTypePatternString(pattern))
	}
	for index, parameter := range pattern.parameters {
		if err := matchBuiltinTypePattern(builtinName, parameter, actual.Params[index], bindings, choices, genericConstraints); err != nil {
			return err
		}
	}
	return nil
}

func builtinNumericGenericMatches(builtinName string, pattern *builtinTypePattern, actual *TypeSpec, genericConstraints map[string]string) bool {
	if actual == nil || actual.Function || len(actual.Params) != 0 || genericConstraints == nil {
		return false
	}
	switch builtinName {
	case "abs", "sqrt", "min", "max":
	default:
		return false
	}
	constraint, exists := genericConstraints[actual.Name]
	if !exists {
		return false
	}
	var domain []string
	switch constraint {
	case "Numeric":
		domain = []string{"Int", "UInt8", "UInt16", "UInt32", "UInt64", "Float"}
	case "Integer":
		domain = []string{"Int", "UInt8", "UInt16", "UInt32", "UInt64"}
	default:
		return false
	}
	for _, concrete := range domain {
		if !builtinPatternAcceptsConcreteType(pattern, concrete) {
			return false
		}
	}
	return true
}

func builtinPatternAcceptsConcreteType(pattern *builtinTypePattern, concrete string) bool {
	if pattern == nil {
		return false
	}
	if len(pattern.alternatives) != 0 {
		for _, option := range pattern.alternatives {
			if builtinPatternAcceptsConcreteType(option, concrete) {
				return true
			}
		}
		return false
	}
	if len(pattern.parameters) != 0 {
		return false
	}
	if pattern.name == "UInt" {
		switch concrete {
		case "UInt8", "UInt16", "UInt32", "UInt64":
			return true
		default:
			return false
		}
	}
	return pattern.name == concrete
}

func matchBuiltinTypeAlternative(builtinName string, pattern *builtinTypePattern, actual *TypeSpec, bindings, choices, genericConstraints map[string]string) error {
	var lastErr error
	for _, option := range pattern.alternatives {
		candidateBindings := cloneBuiltinTypeBindings(bindings)
		candidateChoices := cloneBuiltinTypeBindings(choices)
		if err := matchBuiltinTypePattern(builtinName, option, actual, candidateBindings, candidateChoices, genericConstraints); err != nil {
			lastErr = err
			continue
		}
		key := builtinTypePatternString(pattern)
		candidateChoices[key] = builtinTypeSpecString(actual)
		copyBuiltinTypeBindings(bindings, candidateBindings)
		copyBuiltinTypeBindings(choices, candidateChoices)
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("does not match any signature alternative")
	}
	return lastErr
}

func cloneBuiltinTypeBindings(source map[string]string) map[string]string {
	clone := make(map[string]string, len(source)+1)
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func copyBuiltinTypeBindings(destination, source map[string]string) {
	for key, value := range source {
		destination[key] = value
	}
}

func isBuiltinSignatureVariable(name string) bool {
	return len(name) == 1 && name[0] >= 'A' && name[0] <= 'Z'
}

func builtinTypePatternString(pattern *builtinTypePattern) string {
	if len(pattern.alternatives) != 0 {
		parts := make([]string, len(pattern.alternatives))
		for index, option := range pattern.alternatives {
			parts[index] = builtinTypePatternString(option)
		}
		return strings.Join(parts, "|")
	}
	if len(pattern.parameters) == 0 {
		return pattern.name
	}
	parts := make([]string, len(pattern.parameters))
	for index, parameter := range pattern.parameters {
		parts[index] = builtinTypePatternString(parameter)
	}
	return pattern.name + "[" + strings.Join(parts, ",") + "]"
}

func builtinLenAcceptsMapOrSet(actual *TypeSpec) bool {
	if actual == nil || actual.Function {
		return false
	}
	switch actual.Name {
	case "Map":
		return len(actual.Params) == 2
	case "Set":
		return len(actual.Params) == 1
	default:
		return false
	}
}
