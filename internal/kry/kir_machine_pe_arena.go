package kry

import "fmt"

// directStaticOutputMIR folds the PE backend's compile-time output subset
// directly from validated arena indexes. It deliberately does not create a
// recursive KIR document before deciding whether static lowering applies.
func directStaticOutputMIR(arena *KIRArena, outputLimit int64) ([][]byte, error) {
	if arena == nil {
		return nil, errDirectPEStaticNotApplicable("missing validated MIR arena")
	}
	if err := arena.validateReferences(); err != nil {
		return nil, errDirectPEStaticNotApplicable("invalid validated MIR arena: %v", err)
	}
	statementIndexes, err := arena.indexList(arena.StatementRefs, arena.TopStatements)
	if err != nil {
		return nil, errDirectPEStaticNotApplicable("invalid top-level statement range: %v", err)
	}
	functionIndexes, err := arena.indexList(arena.FunctionRefs, arena.TopFunctions)
	if err != nil {
		return nil, errDirectPEStaticNotApplicable("invalid top-level function range: %v", err)
	}
	if len(statementIndexes) == 0 {
		var main *MIRFunction
		for _, index := range functionIndexes {
			function := &arena.Functions[index]
			if function.Value.Name != "main" {
				continue
			}
			if main != nil {
				return nil, errDirectPEStaticNotApplicable("direct PE backend has duplicate main functions")
			}
			main = function
		}
		if main == nil {
			if len(functionIndexes) != 0 {
				return nil, errDirectPEStaticNotApplicable("program requires top-level statements or main() -> Nil")
			}
		} else {
			parameters, err := arena.indexList(arena.ParameterRefs, main.Params)
			if err != nil {
				return nil, errDirectPEStaticNotApplicable("invalid main parameter range: %v", err)
			}
			if len(parameters) != 0 || main.Value.Return != "Nil" {
				return nil, errDirectPEStaticNotApplicable("direct PE backend requires main() -> Nil")
			}
			statementIndexes, err = arena.indexList(arena.StatementRefs, main.Body)
			if err != nil {
				return nil, errDirectPEStaticNotApplicable("invalid main body range: %v", err)
			}
			if len(statementIndexes) > 0 {
				last := arena.Statements[statementIndexes[len(statementIndexes)-1]]
				if last.Value.Kind == "return" {
					if last.Return.Present && arena.Expressions[last.Return.Index].Value.Kind != "nil" {
						return nil, errDirectPEStaticNotApplicable("direct PE backend supports only return nil in main")
					}
					statementIndexes = statementIndexes[:len(statementIndexes)-1]
				}
			}
		}
	} else {
		for _, index := range functionIndexes {
			if arena.Functions[index].Value.Name == "main" {
				return nil, errDirectPEStaticNotApplicable("direct PE backend does not support both top-level statements and main")
			}
		}
	}

	environment := map[string]Value{}
	var output [][]byte
	var outputBytes int64
	for _, index := range statementIndexes {
		statement := arena.Statements[index]
		switch statement.Value.Kind {
		case "let", "const":
			if !statement.Init.Present {
				return nil, errDirectPEStaticNotApplicable("direct PE backend requires an initializer for '%s'", statement.Value.Name)
			}
			value, ok := directPEStaticMIRValue(arena, statement.Init, environment)
			if !ok {
				return nil, errDirectPEStaticNotApplicable("direct PE backend requires a compile-time value for '%s'", statement.Value.Name)
			}
			environment[statement.Value.Name] = value
			output = append(output, nil)
		case "expr":
			if !statement.Expr.Present {
				return nil, errDirectPEStaticNotApplicable("direct PE backend supports only print/println of static values")
			}
			expression := arena.Expressions[statement.Expr.Index]
			if expression.Value.Kind != "call" || expression.Receiver.Present ||
				(expression.Value.CallTarget != "builtin:print" && expression.Value.CallTarget != "builtin:println") || expression.Args.Count != 1 {
				return nil, errDirectPEStaticNotApplicable("direct PE backend supports only print/println of static values")
			}
			arguments, err := arena.indexList(arena.ExpressionRefs, expression.Args)
			if err != nil || len(arguments) != 1 {
				return nil, errDirectPEStaticNotApplicable("invalid static print argument range")
			}
			value, ok := directPEStaticMIRValue(arena, MIRRef{Index: arguments[0], Present: true}, environment)
			if !ok {
				return nil, errDirectPEStaticNotApplicable("direct PE backend requires a compile-time print value")
			}
			text := display(value)
			if expression.Value.CallTarget == "builtin:println" {
				text += "\n"
			}
			chunk := []byte(text)
			output = append(output, chunk)
			outputBytes += int64(len(chunk))
		default:
			return nil, errDirectPEStaticNotApplicable("direct PE backend does not support statement kind %s", statement.Value.Kind)
		}
		if outputBytes > outputLimit {
			return nil, errDirectOutputLimit
		}
	}
	return output, nil
}

func errDirectPEStaticNotApplicable(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

func directPEStaticMIRValue(arena *KIRArena, ref MIRRef, environment map[string]Value) (Value, bool) {
	if !ref.Present {
		return nilVal(), false
	}
	expression := arena.Expressions[ref.Index]
	if expression.Const.Present {
		return directStaticMIRArenaConstant(arena, expression.Const)
	}
	switch expression.Value.Kind {
	case "int":
		return intVal(expression.Value.Int), true
	case "bool":
		return boolVal(expression.Value.Bool), true
	case "string":
		return stringVal(expression.Value.String), true
	case "float":
		return floatVal(expression.Value.Float), true
	case "nil":
		return nilVal(), true
	case "var":
		value, ok := environment[expression.Value.Name]
		return value, ok
	case "call":
		if expression.Receiver.Present || expression.Value.CallTarget != "builtin:str" || expression.Args.Count != 1 {
			return nilVal(), false
		}
		arguments, err := arena.indexList(arena.ExpressionRefs, expression.Args)
		if err != nil || len(arguments) != 1 {
			return nilVal(), false
		}
		value, ok := directPEStaticMIRValue(arena, MIRRef{Index: arguments[0], Present: true}, environment)
		if !ok {
			return nilVal(), false
		}
		return stringVal(display(value)), true
	case "binary":
		left, leftOK := directPEStaticMIRValue(arena, expression.Left, environment)
		right, rightOK := directPEStaticMIRValue(arena, expression.Right, environment)
		if !leftOK || !rightOK {
			return nilVal(), false
		}
		if expression.Value.Operator == "+" && left.Kind == VString && right.Kind == VString {
			return stringVal(left.S + right.S), true
		}
		if left.Kind == VInt && right.Kind == VInt {
			var value int64
			var ok bool
			switch expression.Value.Operator {
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
			switch expression.Value.Operator {
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

func directStaticMIRArenaConstant(arena *KIRArena, ref MIRRef) (Value, bool) {
	if !ref.Present {
		return nilVal(), false
	}
	constant := arena.Values[ref.Index]
	switch constant.Value.Kind {
	case "nil":
		return nilVal(), true
	case "int":
		return intVal(constant.Value.Int), true
	case "uint":
		return uintVal(constant.Value.UIntBits, constant.Value.UInt), true
	case "float":
		return floatVal(constant.Value.Float), true
	case "bool":
		return boolVal(constant.Value.Bool), true
	case "string", "json":
		return stringVal(constant.Value.String), true
	case "bytes":
		return bytesVal(constant.Value.Bytes), true
	case "array":
		indexes, err := arena.indexList(arena.ValueRefs, constant.Array)
		if err != nil {
			return nilVal(), false
		}
		items := make([]Value, len(indexes))
		for index, child := range indexes {
			value, ok := directStaticMIRArenaConstant(arena, MIRRef{Index: child, Present: true})
			if !ok {
				return nilVal(), false
			}
			items[index] = value
		}
		return arrVal(items), true
	case "option":
		if !constant.Value.Present {
			return optVal(false, nilVal()), true
		}
		inner, ok := directStaticMIRArenaConstant(arena, constant.Inner)
		if !ok {
			return nilVal(), false
		}
		return optVal(true, inner), true
	case "result":
		inner, ok := directStaticMIRArenaConstant(arena, constant.Inner)
		if !ok {
			return nilVal(), false
		}
		return resVal(constant.Value.OK, inner), true
	default:
		return nilVal(), false
	}
}
