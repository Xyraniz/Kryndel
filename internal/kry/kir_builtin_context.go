package kry

// kirBuiltinContextualTypesValid checks builtin rules that need declaration or
// expression metadata beyond the declared Builtin.Signature type pattern.
func kirBuiltinContextualTypesValid(name string, args []*KIRExpr, result string, genericConstraints map[string]string, structs map[string]*KIRStruct, enums map[string]*KIREnum, document *KIRDocument) bool {
	argument := func(index int) *KIRExpr {
		if index < 0 || index >= len(args) {
			return nil
		}
		return args[index]
	}
	argumentType := func(index int) string {
		if expression := argument(index); expression != nil {
			return expression.Type
		}
		return ""
	}
	switch name {
	case "assert_eq":
		return !kirTypeContainsFunctionInStructs(argumentType(0), structs)
	case "array_contains", "array_index_of", "set_contains", "set_insert", "set_remove":
		spec, ok := parseKIRTypeExpression(argumentType(0))
		if !ok || spec.Function || len(spec.Params) != 1 {
			return false
		}
		return !kirTypeContainsFunctionInStructs(TypeSpecString(spec.Params[0]), structs)
	case "thread_send", "thread_try_send", "thread_send_timeout":
		return kirTypeIsCopyable(argumentType(1), genericConstraints, structs, enums)
	case "shared_new":
		return kirTypeIsCopyable(argumentType(0), genericConstraints, structs, enums)
	case "shared_write", "shared_swap":
		return kirTypeIsCopyable(argumentType(1), genericConstraints, structs, enums)
	case "actor_send":
		return kirTypeIsCopyable(argumentType(1), genericConstraints, structs, enums)
	case "thread_receive_timeout":
		duration := argument(1)
		return duration == nil || duration.Kind != "unary" || duration.Operator != "-"
	case "thread_spawn":
		return kirWorkerCallResultMatches(argument(0), result, document)
	case "task_spawn":
		return kirWorkerCallResultMatches(argument(1), result, document)
	case "poly_register":
		handler := argument(1)
		return handler == nil || handler.Kind != "string" || kirStaticPolyHandlerExists(handler.String, document)
	case "poly_reorder":
		for _, index := range []int{1, 2} {
			handler := argument(index)
			if handler != nil && handler.Kind == "string" && !kirStaticPolyHandlerExists(handler.String, document) {
				return false
			}
		}
	}
	return true
}

func kirWorkerCallResultMatches(name *KIRExpr, result string, document *KIRDocument) bool {
	if name == nil || name.Kind != "string" || document == nil {
		return false
	}
	var worker *KIRFunction
	for _, candidate := range document.Functions {
		if candidate == nil || candidate.Name != name.String {
			continue
		}
		if worker != nil {
			return false
		}
		worker = candidate
	}
	return worker != nil && worker.Receiver == "" && len(worker.TypeParams) == 0 && len(worker.Params) == 0 && equivalentKIRTypes(result, "Thread["+worker.Return+"]")
}

func kirStaticPolyHandlerExists(name string, document *KIRDocument) bool {
	if document == nil {
		return false
	}
	var handler *KIRFunction
	for _, candidate := range document.Functions {
		if candidate == nil || candidate.Name != name {
			continue
		}
		if handler != nil {
			return false
		}
		handler = candidate
	}
	return handler != nil && handler.Receiver == "" && len(handler.TypeParams) == 0 && len(handler.Params) == 1 && handler.Params[0] != nil && equivalentKIRTypes(handler.Params[0].Type, "String") && equivalentKIRTypes(handler.Return, "String")
}

func kirTypeIsCopyable(encoded string, genericConstraints map[string]string, structs map[string]*KIRStruct, enums map[string]*KIREnum) bool {
	visiting := map[string]bool{}
	var visit func(string, int) bool
	visit = func(current string, depth int) bool {
		if depth > 128 {
			return false
		}
		spec, ok := parseKIRTypeExpression(current)
		if !ok || spec.Function {
			return false
		}
		if constraint, exists := genericConstraints[spec.Name]; exists && len(spec.Params) == 0 {
			return constraint == "Copy" || constraint == "Integer" || constraint == "Numeric" || constraint == "Comparable"
		}
		if len(spec.Params) == 0 {
			if spec.Name == "Nil" || spec.Name == "Int" || spec.Name == "Float" || spec.Name == "Bool" || spec.Name == "String" || spec.Name == "Bytes" || spec.Name == "Json" || isKIRUIntType(spec.Name) || enums[spec.Name] != nil {
				return true
			}
		}
		switch spec.Name {
		case "Shared":
			return len(spec.Params) == 1
		case "Array", "Option", "Set", "Actor":
			return len(spec.Params) == 1 && visit(TypeSpecString(spec.Params[0]), depth+1)
		case "Map", "Result":
			return len(spec.Params) == 2 && visit(TypeSpecString(spec.Params[0]), depth+1) && visit(TypeSpecString(spec.Params[1]), depth+1)
		}
		declaration := structs[spec.Name]
		if declaration == nil || len(declaration.TypeParams) != len(spec.Params) {
			return false
		}
		key := TypeSpecString(spec)
		if visiting[key] {
			return false
		}
		visiting[key] = true
		defer delete(visiting, key)
		substitutions := make(map[string]string, len(spec.Params))
		for index, parameter := range declaration.TypeParams {
			if parameter == nil || !visit(TypeSpecString(spec.Params[index]), depth+1) {
				return false
			}
			substitutions[parameter.Name] = TypeSpecString(spec.Params[index])
		}
		for _, field := range declaration.Fields {
			if field == nil || !visit(substituteKIRType(field.Type, substitutions), depth+1) {
				return false
			}
		}
		return true
	}
	return visit(encoded, 0)
}

func kirTypeSpecContainsFunction(spec *TypeSpec, structs map[string]*KIRStruct, visiting map[string]bool, depth int) bool {
	if spec == nil || depth > 128 {
		return depth > 128
	}
	if spec.Function {
		return true
	}
	for _, child := range spec.Params {
		if kirTypeSpecContainsFunction(child, structs, visiting, depth+1) {
			return true
		}
	}
	if kirTypeSpecContainsFunction(spec.Return, structs, visiting, depth+1) {
		return true
	}
	if structs == nil {
		return false
	}
	declaration := structs[spec.Name]
	if declaration == nil {
		return false
	}
	key := TypeSpecString(spec)
	if visiting[key] {
		return false
	}
	if len(declaration.TypeParams) != len(spec.Params) {
		return false
	}
	visiting[key] = true
	defer delete(visiting, key)
	substitutions := make(map[string]string, len(spec.Params))
	for index, parameter := range declaration.TypeParams {
		if parameter == nil {
			return true
		}
		substitutions[parameter.Name] = TypeSpecString(spec.Params[index])
	}
	for _, field := range declaration.Fields {
		if field == nil {
			return true
		}
		fieldSpec, valid := parseKIRTypeExpression(substituteKIRType(field.Type, substitutions))
		if !valid || kirTypeSpecContainsFunction(fieldSpec, structs, visiting, depth+1) {
			return true
		}
	}
	return false
}
