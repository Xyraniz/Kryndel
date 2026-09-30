package kry

import "fmt"

// directStaticKIROutput compiles the constant-output ELF subset from the
// checked KIR document. Unsupported nodes are left to the dynamic backend.
func directStaticKIROutput(mir *ValidatedMIR, maxOutputBytes int64) ([]byte, error) {
	document, err := validatedMIRDocument(mir)
	if err != nil {
		return nil, err
	}
	if document == nil {
		return nil, fmt.Errorf("missing KIR document")
	}
	statements := document.Statements
	if len(statements) == 0 {
		for _, function := range document.Functions {
			if function.Name == "main" {
				statements = function.Body
				break
			}
		}
	}
	env := make(map[string]Value)
	output := make([]byte, 0)
	for _, statement := range statements {
		if statement == nil {
			continue
		}
		switch statement.Kind {
		case "let", "const":
			if statement.Init == nil {
				return nil, fmt.Errorf("direct ELF backend requires an initializer for '%s'", statement.Name)
			}
			value, ok := directStaticKIRValue(statement.Init, env)
			if !ok {
				return nil, fmt.Errorf("direct ELF backend requires a compile-time value for '%s'", statement.Name)
			}
			env[statement.Name] = value
		case "expr":
			expression := statement.Expr
			if expression == nil || expression.Kind != "call" || expression.Receiver != nil || (expression.CallTarget != "builtin:print" && expression.CallTarget != "builtin:println") || len(expression.Args) != 1 {
				return nil, fmt.Errorf("direct ELF backend supports only print/println of static values")
			}
			value, ok := directStaticKIRValue(expression.Args[0], env)
			if !ok {
				return nil, fmt.Errorf("direct ELF backend requires a compile-time print value")
			}
			text := display(value)
			if expression.CallTarget == "builtin:println" {
				text += "\n"
			}
			output = append(output, text...)
		default:
			return nil, fmt.Errorf("direct ELF backend does not support statement kind %s", statement.Kind)
		}
		if maxOutputBytes > 0 && int64(len(output)) > maxOutputBytes {
			return nil, errDirectOutputLimit
		}
	}
	return output, nil
}

func directStaticKIRValue(expression *KIRExpr, env map[string]Value) (Value, bool) {
	if expression == nil {
		return nilVal(), false
	}
	if expression.Const != nil {
		return directStaticKIRConstant(expression.Const)
	}
	switch expression.Kind {
	case "int":
		return intVal(expression.Int), true
	case "float":
		return floatVal(expression.Float), true
	case "bool":
		return boolVal(expression.Bool), true
	case "string":
		return stringVal(expression.String), true
	case "nil":
		return nilVal(), true
	case "var":
		value, ok := env[expression.Name]
		return value, ok
	case "call":
		if expression.Receiver != nil || expression.CallTarget != "builtin:str" || len(expression.Args) != 1 {
			return nilVal(), false
		}
		value, ok := directStaticKIRValue(expression.Args[0], env)
		if !ok {
			return nilVal(), false
		}
		return stringVal(display(value)), true
	case "binary":
		left, leftOK := directStaticKIRValue(expression.Left, env)
		right, rightOK := directStaticKIRValue(expression.Right, env)
		if !leftOK || !rightOK {
			return nilVal(), false
		}
		if expression.Operator == "+" && left.Kind == VString && right.Kind == VString {
			return stringVal(left.S + right.S), true
		}
		if left.Kind == VInt && right.Kind == VInt {
			var value int64
			var ok bool
			switch expression.Operator {
			case "+":
				value, ok = addI(left.I, right.I)
			case "-":
				value, ok = subI(left.I, right.I)
			case "*":
				value, ok = mulI(left.I, right.I)
			case "/":
				value, ok = divI(left.I, right.I)
			case "%":
				value, ok = remI(left.I, right.I)
			default:
				return nilVal(), false
			}
			if ok {
				return intVal(value), true
			}
		}
		if left.Kind == VUInt && right.Kind == VUInt && left.UBits == right.UBits {
			var value uint64
			switch expression.Operator {
			case "+":
				value = left.U + right.U
			case "-":
				value = left.U - right.U
			case "*":
				value = left.U * right.U
			case "&":
				value = left.U & right.U
			case "^":
				value = left.U ^ right.U
			case "|":
				value = left.U | right.U
			default:
				return nilVal(), false
			}
			return uintVal(left.UBits, value), true
		}
	}
	return nilVal(), false
}

func directStaticKIRConstant(constant *KIRValue) (Value, bool) {
	if constant == nil {
		return nilVal(), false
	}
	switch constant.Kind {
	case "nil":
		return nilVal(), true
	case "int":
		return intVal(constant.Int), true
	case "uint":
		return uintVal(constant.UIntBits, constant.UInt), true
	case "float":
		return floatVal(constant.Float), true
	case "bool":
		return boolVal(constant.Bool), true
	case "string", "json":
		return stringVal(constant.String), true
	case "bytes":
		return bytesVal(constant.Bytes), true
	case "array":
		items := make([]Value, len(constant.Array))
		for index, item := range constant.Array {
			value, ok := directStaticKIRConstant(item)
			if !ok {
				return nilVal(), false
			}
			items[index] = value
		}
		return arrVal(items), true
	case "option":
		if !constant.Present {
			return optVal(false, nilVal()), true
		}
		inner, ok := directStaticKIRConstant(constant.Inner)
		if !ok {
			return nilVal(), false
		}
		return optVal(true, inner), true
	case "result":
		inner, ok := directStaticKIRConstant(constant.Inner)
		if !ok {
			return nilVal(), false
		}
		return resVal(constant.OK, inner), true
	default:
		return nilVal(), false
	}
}
