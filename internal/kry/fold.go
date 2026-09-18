package kry

import "math"

// foldConstExpr records an immutable primitive result in the checked AST. The
// runtime still validates its instruction budget before reading the fast path,
// so folding improves execution without bypassing resource accounting.
func foldConstExpr(e *Expr) {
	if e == nil || e.ConstValue != nil {
		return
	}
	if v, ok := constantValue(e); ok {
		x := cloneValue(v)
		e.ConstValue = &x
	}
}

func constantValue(e *Expr) (Value, bool) {
	if e == nil {
		return nilVal(), false
	}
	if e.ConstValue != nil {
		return cloneValue(*e.ConstValue), true
	}
	switch e.Kind {
	case ExInt:
		return intVal(e.Int), true
	case ExFloat:
		return floatVal(e.Float), true
	case ExBool:
		return boolVal(e.Bool), true
	case ExNil:
		return nilVal(), true
	case ExString:
		return stringVal(e.Str), true
	case ExUnary:
		v, ok := constantValue(e.Operand)
		if !ok {
			return nilVal(), false
		}
		if e.Op == BANG && v.Kind == VBool {
			return boolVal(!v.Bool), true
		}
		if v.Kind == VInt {
			if e.Op == PLUS {
				return v, true
			}
			if v.I == math.MinInt64 {
				return nilVal(), false
			}
			return intVal(-v.I), true
		}
		if v.Kind == VFloat {
			if e.Op == PLUS {
				return v, true
			}
			return floatVal(-v.F), true
		}
	case ExBinary:
		left, lok := constantValue(e.Left)
		right, rok := constantValue(e.Right)
		if !lok || !rok {
			return nilVal(), false
		}
		if left.Kind == VBool && right.Kind == VBool {
			if e.Op == AND {
				return boolVal(left.Bool && right.Bool), true
			}
			if e.Op == OR {
				return boolVal(left.Bool || right.Bool), true
			}
		}
		if e.Op == EQEQ {
			return boolVal(equalValue(left, right)), true
		}
		if e.Op == NEQ {
			return boolVal(!equalValue(left, right)), true
		}
		if left.Kind == VString && right.Kind == VString && e.Op == PLUS {
			return stringVal(left.S + right.S), true
		}
		if left.Kind == VInt && right.Kind == VInt {
			var value int64
			var ok bool
			switch e.Op {
			case PLUS:
				value, ok = addI(left.I, right.I)
			case MINUS:
				value, ok = subI(left.I, right.I)
			case STAR:
				value, ok = mulI(left.I, right.I)
			case SLASH:
				value, ok = divI(left.I, right.I)
			case PERCENT:
				value, ok = remI(left.I, right.I)
			case LESS:
				return boolVal(left.I < right.I), true
			case LEQ:
				return boolVal(left.I <= right.I), true
			case GREATER:
				return boolVal(left.I > right.I), true
			case GEQ:
				return boolVal(left.I >= right.I), true
			}
			if ok {
				return intVal(value), true
			}
		}
		if left.Kind == VFloat && right.Kind == VFloat {
			if e.Op == LESS {
				return boolVal(left.F < right.F), true
			}
			if e.Op == LEQ {
				return boolVal(left.F <= right.F), true
			}
			if e.Op == GREATER {
				return boolVal(left.F > right.F), true
			}
			if e.Op == GEQ {
				return boolVal(left.F >= right.F), true
			}
			var value float64
			switch e.Op {
			case PLUS:
				value = left.F + right.F
			case MINUS:
				value = left.F - right.F
			case STAR:
				value = left.F * right.F
			case SLASH:
				if right.F == 0 {
					return nilVal(), false
				}
				value = left.F / right.F
			default:
				return nilVal(), false
			}
			if isFinite(value) {
				return floatVal(value), true
			}
		}
	}
	return nilVal(), false
}
