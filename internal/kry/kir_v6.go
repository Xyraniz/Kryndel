package kry

import "fmt"

type kirV6MetadataValidator struct {
	sources       map[string]struct{}
	bindingsByID  map[string]KIRBinding
	idsByLocation map[string]string
}

func newKIRV6MetadataValidator(sources []string) *kirV6MetadataValidator {
	knownSources := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		knownSources[source] = struct{}{}
	}
	return &kirV6MetadataValidator{
		sources: knownSources, bindingsByID: map[string]KIRBinding{}, idsByLocation: map[string]string{},
	}
}

func (validator *kirV6MetadataValidator) node(label, source string, line, column int, span *KIRSourceSpan) error {
	if source == "" || line < 1 || column < 1 {
		return fmt.Errorf("%s has missing or invalid source coordinates", label)
	}
	if _, ok := validator.sources[source]; !ok {
		return fmt.Errorf("%s references source %q outside the document source table", label, source)
	}
	if span == nil || span.Start < 0 || span.End <= span.Start {
		return fmt.Errorf("%s has a missing or invalid source span", label)
	}
	return nil
}

func (validator *kirV6MetadataValidator) binding(binding *KIRBinding, label string) error {
	if !validKIRBinding(binding) {
		return fmt.Errorf("%s is incomplete", label)
	}
	if err := validator.node(label, binding.Source, binding.Line, binding.Column, binding.Span); err != nil {
		return err
	}
	if binding.Span.End-binding.Span.Start != len([]byte(binding.Name)) {
		return fmt.Errorf("%s span does not cover its binding name", label)
	}
	wantID := kirBindingIDFromParts(binding.Name, binding.Source, binding.Span.Start, binding.Line, binding.Column)
	if binding.ID != wantID {
		return fmt.Errorf("%s has a non-canonical binding ID", label)
	}
	location := fmt.Sprintf("%s:%d:%d:%d:%d:%s", binding.Source, binding.Span.Start, binding.Span.End, binding.Line, binding.Column, binding.Name)
	if previous, ok := validator.idsByLocation[location]; ok && previous != binding.ID {
		return fmt.Errorf("%s gives one source binding multiple IDs", label)
	}
	if previous, ok := validator.bindingsByID[binding.ID]; ok && (previous.Name != binding.Name || previous.Type != binding.Type || previous.Mutable != binding.Mutable || previous.Source != binding.Source || previous.Line != binding.Line || previous.Column != binding.Column || previous.Span == nil || *previous.Span != *binding.Span) {
		return fmt.Errorf("%s reuses a binding ID for inconsistent metadata", label)
	}
	validator.idsByLocation[location] = binding.ID
	validator.bindingsByID[binding.ID] = *binding
	return nil
}

func validateKIRV6Metadata(document *KIRDocument) error {
	if document == nil || document.Version != 6 {
		return fmt.Errorf("KIR v6 metadata validator received another version")
	}
	if len(document.Sources) == 0 {
		return fmt.Errorf("KIR v6 source table is empty")
	}
	validator := newKIRV6MetadataValidator(document.Sources)
	if _, ok := validator.sources[document.Source]; !ok {
		return fmt.Errorf("KIR root source is missing from the source table")
	}
	if len(document.ImportRecords) != len(document.Imports) {
		return fmt.Errorf("KIR v6 import records do not match the import table")
	}
	for index, record := range document.ImportRecords {
		if record == nil || record.Path == "" || record.Path != document.Imports[index] {
			return fmt.Errorf("KIR import record %d is incomplete or out of order", index)
		}
		if err := validator.node(fmt.Sprintf("KIR import %d", index), record.Source, record.Line, record.Column, record.Span); err != nil {
			return err
		}
	}
	for _, declaration := range document.Structs {
		if declaration == nil {
			continue
		}
		if err := validator.node("KIR struct "+declaration.Name, declaration.Source, declaration.Line, declaration.Column, declaration.Span); err != nil {
			return err
		}
		for _, parameter := range declaration.TypeParams {
			if parameter != nil {
				if err := validator.node("KIR struct type parameter "+parameter.Name, parameter.Source, parameter.Line, parameter.Column, parameter.Span); err != nil {
					return err
				}
			}
		}
		for _, field := range declaration.Fields {
			if field != nil {
				if err := validator.node("KIR field "+declaration.Name+"."+field.Name, field.Source, field.Line, field.Column, field.Span); err != nil {
					return err
				}
			}
		}
	}
	for _, declaration := range document.Enums {
		if declaration == nil {
			continue
		}
		if err := validator.node("KIR enum "+declaration.Name, declaration.Source, declaration.Line, declaration.Column, declaration.Span); err != nil {
			return err
		}
		if len(declaration.VariantSpans) != len(declaration.Variants) {
			return fmt.Errorf("KIR enum %q is missing variant spans", declaration.Name)
		}
		for index, span := range declaration.VariantSpans {
			if span == nil || span.Start < 0 || span.End <= span.Start {
				return fmt.Errorf("KIR enum %q variant %q has an invalid source span", declaration.Name, declaration.Variants[index])
			}
		}
	}
	for _, declaration := range document.Traits {
		if declaration == nil {
			continue
		}
		if err := validator.node("KIR trait "+declaration.Name, declaration.Source, declaration.Line, declaration.Column, declaration.Span); err != nil {
			return err
		}
		for _, method := range declaration.Methods {
			if method == nil {
				continue
			}
			if err := validator.node("KIR trait method "+method.Name, method.Source, method.Line, method.Column, method.Span); err != nil {
				return err
			}
			for _, parameter := range method.Params {
				if parameter != nil {
					if err := validator.node("KIR trait parameter "+parameter.Name, parameter.Source, parameter.Line, parameter.Column, parameter.Span); err != nil {
						return err
					}
				}
			}
		}
	}
	for _, declaration := range document.TraitImpls {
		if declaration == nil {
			continue
		}
		if err := validator.node("KIR trait implementation "+declaration.Trait, declaration.Source, declaration.Line, declaration.Column, declaration.Span); err != nil {
			return err
		}
		for _, method := range declaration.Methods {
			if method != nil {
				if err := validator.node("KIR trait implementation method "+method.Name, method.Source, method.Line, method.Column, method.Span); err != nil {
					return err
				}
			}
		}
	}
	var validateFunction func(*KIRFunction) error
	var validateExpression func(*KIRExpr) error
	var validateStatement func(*KIRStmt) error
	var validatePattern func(*KIRPattern) error
	validatePattern = func(pattern *KIRPattern) error {
		if pattern == nil {
			return nil
		}
		if err := validator.node("KIR pattern", pattern.Source, pattern.Line, pattern.Column, pattern.Span); err != nil {
			return err
		}
		if pattern.ResolvedBinding != nil {
			return validator.binding(pattern.ResolvedBinding, "KIR pattern binding")
		}
		return nil
	}
	validateExpression = func(expression *KIRExpr) error {
		if expression == nil {
			return nil
		}
		if err := validator.node("KIR expression "+expression.Kind, expression.Source, expression.Line, expression.Column, expression.Span); err != nil {
			return err
		}
		if expression.Binding != nil {
			if err := validator.binding(expression.Binding, "KIR expression binding"); err != nil {
				return err
			}
		}
		for _, child := range []*KIRExpr{expression.Left, expression.Right, expression.Operand, expression.Base, expression.Receiver, expression.Callee} {
			if err := validateExpression(child); err != nil {
				return err
			}
		}
		for _, group := range [][]*KIRExpr{expression.Args, expression.Items, expression.MapKeys, expression.Values} {
			for _, child := range group {
				if err := validateExpression(child); err != nil {
					return err
				}
			}
		}
		return validateFunction(expression.Lambda)
	}
	validateStatement = func(statement *KIRStmt) error {
		if statement == nil {
			return nil
		}
		if err := validator.node("KIR statement "+statement.Kind, statement.Source, statement.Line, statement.Column, statement.Span); err != nil {
			return err
		}
		if statement.Binding != nil {
			if err := validator.binding(statement.Binding, "KIR statement binding"); err != nil {
				return err
			}
		}
		for _, child := range []*KIRExpr{statement.Init, statement.Expr, statement.Target, statement.Value, statement.Cond, statement.Iter, statement.Return, statement.Scrutinee} {
			if err := validateExpression(child); err != nil {
				return err
			}
		}
		for _, group := range [][]*KIRStmt{statement.Then, statement.Else, statement.Body} {
			for _, child := range group {
				if err := validateStatement(child); err != nil {
					return err
				}
			}
		}
		for _, arm := range statement.Arms {
			if arm == nil {
				continue
			}
			if err := validator.node("KIR match arm", arm.Source, arm.Line, arm.Column, arm.Span); err != nil {
				return err
			}
			if err := validatePattern(arm.Pattern); err != nil {
				return err
			}
			for _, child := range arm.Body {
				if err := validateStatement(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	validateFunction = func(function *KIRFunction) error {
		if function == nil {
			return nil
		}
		if err := validator.node("KIR function "+function.Name, function.Source, function.Line, function.Column, function.Span); err != nil {
			return err
		}
		for _, parameter := range function.TypeParams {
			if parameter != nil {
				if err := validator.node("KIR function type parameter "+parameter.Name, parameter.Source, parameter.Line, parameter.Column, parameter.Span); err != nil {
					return err
				}
			}
		}
		for _, parameter := range function.Params {
			if parameter == nil {
				continue
			}
			if err := validator.node("KIR parameter "+parameter.Name, parameter.Source, parameter.Line, parameter.Column, parameter.Span); err != nil {
				return err
			}
			if parameter.Binding != nil {
				if err := validator.binding(parameter.Binding, "KIR parameter binding"); err != nil {
					return err
				}
			}
			if err := validateExpression(parameter.Default); err != nil {
				return err
			}
		}
		for _, capture := range function.Captures {
			if capture != nil && capture.Binding != nil {
				if err := validator.binding(capture.Binding, "KIR capture binding"); err != nil {
					return err
				}
			}
		}
		for _, statement := range function.Body {
			if err := validateStatement(statement); err != nil {
				return err
			}
		}
		return nil
	}
	for _, function := range document.Functions {
		if err := validateFunction(function); err != nil {
			return err
		}
	}
	for _, statement := range document.Statements {
		if err := validateStatement(statement); err != nil {
			return err
		}
	}
	return nil
}

func validateKIRArenaV6Metadata(arena *KIRArena) error {
	if arena == nil || arena.Version != 6 {
		return fmt.Errorf("KIR v6 arena metadata validator received another version")
	}
	if len(arena.Sources) == 0 {
		return fmt.Errorf("KIR v6 source table is empty")
	}
	validator := newKIRV6MetadataValidator(arena.Sources)
	if _, ok := validator.sources[arena.Source]; !ok {
		return fmt.Errorf("KIR root source is missing from the source table")
	}
	if len(arena.ImportRecords) != len(arena.Imports) {
		return fmt.Errorf("KIR v6 import records do not match the import table")
	}
	for index, record := range arena.ImportRecords {
		if record == nil || record.Path == "" || record.Path != arena.Imports[index] {
			return fmt.Errorf("KIR import record %d is incomplete or out of order", index)
		}
		if err := validator.node(fmt.Sprintf("KIR import %d", index), record.Source, record.Line, record.Column, record.Span); err != nil {
			return err
		}
	}
	for _, structure := range arena.Structs {
		if structure == nil {
			continue
		}
		if err := validator.node("KIR struct "+structure.Name, structure.Source, structure.Line, structure.Column, structure.Span); err != nil {
			return err
		}
		for _, parameter := range structure.TypeParams {
			if parameter != nil {
				if err := validator.node("KIR struct type parameter "+parameter.Name, parameter.Source, parameter.Line, parameter.Column, parameter.Span); err != nil {
					return err
				}
			}
		}
		for _, field := range structure.Fields {
			if field != nil {
				if err := validator.node("KIR field "+structure.Name+"."+field.Name, field.Source, field.Line, field.Column, field.Span); err != nil {
					return err
				}
			}
		}
	}
	for _, enumeration := range arena.Enums {
		if enumeration == nil {
			continue
		}
		if err := validator.node("KIR enum "+enumeration.Name, enumeration.Source, enumeration.Line, enumeration.Column, enumeration.Span); err != nil {
			return err
		}
		if len(enumeration.VariantSpans) != len(enumeration.Variants) {
			return fmt.Errorf("KIR enum %q is missing variant spans", enumeration.Name)
		}
		for index, span := range enumeration.VariantSpans {
			if span == nil || span.Start < 0 || span.End <= span.Start {
				return fmt.Errorf("KIR enum %q variant %q has an invalid source span", enumeration.Name, enumeration.Variants[index])
			}
		}
	}
	for _, trait := range arena.Traits {
		if trait == nil {
			continue
		}
		if err := validator.node("KIR trait "+trait.Name, trait.Source, trait.Line, trait.Column, trait.Span); err != nil {
			return err
		}
		for _, method := range trait.Methods {
			if method == nil {
				continue
			}
			if err := validator.node("KIR trait method "+method.Name, method.Source, method.Line, method.Column, method.Span); err != nil {
				return err
			}
			for _, parameter := range method.Params {
				if parameter != nil {
					if err := validator.node("KIR trait parameter "+parameter.Name, parameter.Source, parameter.Line, parameter.Column, parameter.Span); err != nil {
						return err
					}
				}
			}
		}
	}
	for _, implementation := range arena.TraitImpls {
		if implementation == nil {
			continue
		}
		if err := validator.node("KIR trait implementation "+implementation.Trait, implementation.Source, implementation.Line, implementation.Column, implementation.Span); err != nil {
			return err
		}
		for _, method := range implementation.Methods {
			if method != nil {
				if err := validator.node("KIR trait implementation method "+method.Name, method.Source, method.Line, method.Column, method.Span); err != nil {
					return err
				}
			}
		}
	}
	for _, function := range arena.Functions {
		if err := validator.node("KIR function "+function.Value.Name, function.Value.Source, function.Value.Line, function.Value.Column, function.Value.Span); err != nil {
			return err
		}
		for _, parameter := range function.Value.TypeParams {
			if parameter != nil {
				if err := validator.node("KIR function type parameter "+parameter.Name, parameter.Source, parameter.Line, parameter.Column, parameter.Span); err != nil {
					return err
				}
			}
		}
	}
	for _, parameter := range arena.Parameters {
		if err := validator.node("KIR parameter "+parameter.Value.Name, parameter.Value.Source, parameter.Value.Line, parameter.Value.Column, parameter.Value.Span); err != nil {
			return err
		}
	}
	for _, expression := range arena.Expressions {
		if err := validator.node("KIR expression "+expression.Value.Kind, expression.Value.Source, expression.Value.Line, expression.Value.Column, expression.Value.Span); err != nil {
			return err
		}
	}
	for _, statement := range arena.Statements {
		if err := validator.node("KIR statement "+statement.Value.Kind, statement.Value.Source, statement.Value.Line, statement.Value.Column, statement.Value.Span); err != nil {
			return err
		}
	}
	for _, pattern := range arena.Patterns {
		if err := validator.node("KIR pattern", pattern.Value.Source, pattern.Value.Line, pattern.Value.Column, pattern.Value.Span); err != nil {
			return err
		}
	}
	for _, arm := range arena.Arms {
		if err := validator.node("KIR match arm", arm.Value.Source, arm.Value.Line, arm.Value.Column, arm.Value.Span); err != nil {
			return err
		}
	}
	for index := range arena.Bindings {
		if err := validator.binding(&arena.Bindings[index], "KIR binding"); err != nil {
			return err
		}
	}
	return nil
}
