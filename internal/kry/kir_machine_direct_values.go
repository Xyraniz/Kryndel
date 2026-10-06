package kry

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

type directELFUnsupportedResultErrorPayload struct {
	typeName string
}

func (err *directELFUnsupportedResultErrorPayload) Error() string {
	return fmt.Sprintf("direct ELF backend cannot report result_unwrap error payload type %s; supported error payload types are String, Int, UInt, and Bool", err.typeName)
}

func isDirectELFUnsupportedResultErrorPayload(err error) bool {
	var payloadError *directELFUnsupportedResultErrorPayload
	return errors.As(err, &payloadError)
}

func validateDirectKIRBuiltinCall(expression *KIRExpr, args []*KIRExpr, document kirExecMetadataProvider) error {
	if expression == nil {
		return fmt.Errorf("invalid KIR executable: nil direct builtin")
	}
	unsupported := func() error {
		return fmt.Errorf("%w: direct KIR ELF does not lower builtin %q with this signature", errKIRSubsetUnsupported, expression.Name)
	}
	container := func(encoded, expected string, count int) ([]string, bool) {
		name, arguments, composite := parseKIRContainerType(encoded)
		return arguments, composite && name == expected && len(arguments) == count
	}
	integer := func(encoded string) bool {
		return encoded == "Int" || strings.HasPrefix(encoded, "UInt")
	}
	supportedPrint := func(encoded string) bool {
		return encoded == "Int" || encoded == "Bool" || encoded == "String" || encoded == "UInt8" || encoded == "UInt16" || encoded == "UInt32" || encoded == "UInt64"
	}
	supportedEqual := func(encoded string) bool {
		return supportedPrint(encoded) || encoded == "Nil"
	}
	singleArg := func() bool { return len(args) == 1 && args[0] != nil }
	if len(args) > 0 {
		for _, argument := range args {
			if argument == nil || !directKIRTypeSupported(argument.Type, document) {
				return unsupported()
			}
		}
	}
	switch expression.Name {
	case "print", "println":
		if !singleArg() || !supportedPrint(args[0].Type) || expression.Type != "Nil" {
			return unsupported()
		}
	case "str":
		if !singleArg() || !(supportedPrint(args[0].Type) || args[0].Type == "Nil") || expression.Type != "String" {
			return unsupported()
		}
	case "int":
		if !singleArg() || !(integer(args[0].Type) || args[0].Type == "Bool" || args[0].Type == "String") || expression.Type != "Int" {
			return unsupported()
		}
	case "u8", "u16", "u32", "u64":
		expectedType := map[string]string{"u8": "UInt8", "u16": "UInt16", "u32": "UInt32", "u64": "UInt64"}[expression.Name]
		if !singleArg() || !integer(args[0].Type) || expression.Type != expectedType {
			return unsupported()
		}
	case "assert":
		if !singleArg() || args[0].Type != "Bool" || expression.Type != "Nil" {
			return unsupported()
		}
	case "assert_eq":
		if len(args) != 2 || args[0] == nil || args[1] == nil || args[0].Type != args[1].Type || !supportedEqual(args[0].Type) || expression.Type != "Nil" {
			return unsupported()
		}
	case "contains", "starts_with", "ends_with":
		if len(args) != 2 || args[0] == nil || args[1] == nil || args[0].Type != "String" || args[1].Type != "String" || expression.Type != "Bool" {
			return unsupported()
		}
	case "len":
		if !singleArg() {
			return unsupported()
		}
		_, arrayOK := container(args[0].Type, "Array", 1)
		if (!arrayOK && args[0].Type != "Bytes") || expression.Type != "Int" {
			return unsupported()
		}
	case "process_args":
		if len(args) != 0 || expression.Type != "Array[String]" {
			return unsupported()
		}
	case "string_chars":
		if !singleArg() || args[0].Type != "String" || expression.Type != "Array[String]" {
			return unsupported()
		}
	case "substring":
		if len(args) != 3 || args[0] == nil || args[1] == nil || args[2] == nil || args[0].Type != "String" || args[1].Type != "Int" || args[2].Type != "Int" {
			return unsupported()
		}
		resultTypes, ok := container(expression.Type, "Result", 2)
		if !ok || resultTypes[0] != "String" || resultTypes[1] != "String" {
			return unsupported()
		}
	case "fs_read_text":
		if !singleArg() || args[0].Type != "String" {
			return unsupported()
		}
		resultTypes, ok := container(expression.Type, "Result", 2)
		if !ok || resultTypes[0] != "String" || resultTypes[1] != "String" {
			return unsupported()
		}
	case "bytes", "bytes_from_u8":
		if !singleArg() {
			return unsupported()
		}
		itemType := "Int"
		if expression.Name == "bytes_from_u8" {
			itemType = "UInt8"
		}
		arrayTypes, ok := container(args[0].Type, "Array", 1)
		if !ok || arrayTypes[0] != itemType || expression.Type != "Bytes" {
			return unsupported()
		}
	case "u8_array":
		if !singleArg() || args[0].Type != "Bytes" || expression.Type != "Array[UInt8]" {
			return unsupported()
		}
	case "string_to_bytes":
		if !singleArg() || args[0].Type != "String" || expression.Type != "Bytes" {
			return unsupported()
		}
	case "fs_write_bytes":
		if len(args) != 2 || args[0] == nil || args[0].Type != "String" || args[1] == nil || args[1].Type != "Bytes" {
			return unsupported()
		}
		resultTypes, ok := container(expression.Type, "Result", 2)
		if !ok || resultTypes[0] != "Nil" || resultTypes[1] != "String" {
			return unsupported()
		}
	case "array_push":
		if len(args) != 2 || args[0] == nil || args[1] == nil {
			return unsupported()
		}
		types, ok := container(args[0].Type, "Array", 1)
		if !ok || types[0] != args[1].Type || expression.Type != args[0].Type {
			return unsupported()
		}
	case "array_concat":
		if len(args) != 2 || args[0] == nil || args[1] == nil || args[0].Type != args[1].Type || expression.Type != args[0].Type {
			return unsupported()
		}
		if _, ok := container(args[0].Type, "Array", 1); !ok {
			return unsupported()
		}
	case "array_get":
		if len(args) != 2 || args[0] == nil || args[1] == nil || args[1].Type != "Int" {
			return unsupported()
		}
		types, ok := container(args[0].Type, "Array", 1)
		result, resultOK := container(expression.Type, "Option", 1)
		if !ok || !resultOK || types[0] != result[0] {
			return unsupported()
		}
	case "array_indices":
		if !singleArg() {
			return unsupported()
		}
		if _, ok := container(args[0].Type, "Array", 1); !ok || expression.Type != "Array[Int]" {
			return unsupported()
		}
	case "array_slice":
		if len(args) != 3 || args[0] == nil || args[1] == nil || args[2] == nil || args[1].Type != "Int" || args[2].Type != "Int" || expression.Type != args[0].Type {
			return unsupported()
		}
		if _, ok := container(args[0].Type, "Array", 1); !ok {
			return unsupported()
		}
	case "array_take", "array_drop":
		if len(args) != 2 || args[0] == nil || args[1] == nil || args[1].Type != "Int" || expression.Type != args[0].Type {
			return unsupported()
		}
		if _, ok := container(args[0].Type, "Array", 1); !ok {
			return unsupported()
		}
	case "array_reverse":
		if !singleArg() || expression.Type != args[0].Type {
			return unsupported()
		}
		if _, ok := container(args[0].Type, "Array", 1); !ok {
			return unsupported()
		}
	case "array_set":
		if len(args) != 3 || args[0] == nil || args[1] == nil || args[2] == nil || args[1].Type != "Int" {
			return unsupported()
		}
		arrayTypes, arrayOK := container(args[0].Type, "Array", 1)
		resultTypes, resultOK := container(expression.Type, "Result", 2)
		if !arrayOK || args[2].Type != arrayTypes[0] || !resultOK || resultTypes[0] != args[0].Type || resultTypes[1] != "String" {
			return unsupported()
		}
	case "map_get", "map_contains_key", "map_insert", "map_remove":
		if len(args) < 2 || args[0] == nil {
			return unsupported()
		}
		mapTypes, ok := directKIRMapType(args[0].Type)
		if !ok || args[1] == nil || args[1].Type != mapTypes[0] {
			return unsupported()
		}
		if _, ok := directKIRMapKeyKind(mapTypes[0]); !ok {
			return unsupported()
		}
		switch expression.Name {
		case "map_get":
			resultTypes, resultOK := container(expression.Type, "Option", 1)
			if len(args) != 2 || !resultOK || resultTypes[0] != mapTypes[1] {
				return unsupported()
			}
		case "map_contains_key":
			if len(args) != 2 || expression.Type != "Bool" {
				return unsupported()
			}
		case "map_insert":
			if len(args) != 3 || args[2] == nil || args[2].Type != mapTypes[1] || expression.Type != args[0].Type {
				return unsupported()
			}
		case "map_remove":
			if len(args) != 2 || expression.Type != args[0].Type {
				return unsupported()
			}
		}
	case "some":
		if !singleArg() {
			return unsupported()
		}
		types, ok := container(expression.Type, "Option", 1)
		if !ok || types[0] != args[0].Type {
			return unsupported()
		}
	case "none":
		if len(args) != 0 {
			return unsupported()
		}
		if _, ok := container(expression.Type, "Option", 1); !ok {
			return unsupported()
		}
	case "ok", "err":
		if !singleArg() {
			return unsupported()
		}
		types, ok := container(expression.Type, "Result", 2)
		if !ok || (expression.Name == "ok" && types[0] != args[0].Type) || (expression.Name == "err" && types[1] != args[0].Type) {
			return unsupported()
		}
	case "is_some", "is_none":
		if !singleArg() || expression.Type != "Bool" {
			return unsupported()
		}
		if _, ok := container(args[0].Type, "Option", 1); !ok {
			return unsupported()
		}
	case "is_ok", "is_err":
		if !singleArg() || expression.Type != "Bool" {
			return unsupported()
		}
		if _, ok := container(args[0].Type, "Result", 2); !ok {
			return unsupported()
		}
	case "unwrap_or":
		if len(args) != 2 || args[0] == nil || args[1] == nil || expression.Type != args[1].Type {
			return unsupported()
		}
		types, ok := container(args[0].Type, "Option", 1)
		if !ok || types[0] != args[1].Type {
			return unsupported()
		}
	case "result_unwrap":
		if !singleArg() {
			return unsupported()
		}
		types, ok := container(args[0].Type, "Result", 2)
		if !ok || expression.Type != types[0] {
			return unsupported()
		}
		if !supportedPrint(types[1]) {
			return &directELFUnsupportedResultErrorPayload{typeName: types[1]}
		}
	case "result_error":
		if !singleArg() {
			return unsupported()
		}
		types, ok := container(args[0].Type, "Result", 2)
		result, resultOK := container(expression.Type, "Option", 1)
		if !ok || !resultOK || types[1] != result[0] {
			return unsupported()
		}
	default:
		return unsupported()
	}
	return nil
}

func directKIRUnarySupportedWithOperand(expression *KIRExpr, operandNode *KIRExpr) bool {
	if expression == nil || operandNode == nil {
		return false
	}
	operand := operandNode.Type
	switch expression.Operator {
	case "!":
		return operand == "Bool" && expression.Type == "Bool"
	case "+":
		return (operand == "Int" || strings.HasPrefix(operand, "UInt")) && expression.Type == operand
	case "-":
		return operand == "Int" && expression.Type == "Int"
	case "~":
		return strings.HasPrefix(operand, "UInt") && expression.Type == operand
	default:
		return false
	}
}

func directKIRBinarySupportedWithOperands(expression, leftNode, rightNode *KIRExpr) bool {
	if expression == nil || leftNode == nil || rightNode == nil {
		return false
	}
	left, right := leftNode.Type, rightNode.Type
	if expression.Operator == "<<" || expression.Operator == ">>" {
		return strings.HasPrefix(left, "UInt") && right == "Int" && expression.Type == left
	}
	if left != right {
		return false
	}
	if expression.Operator == "+" {
		name, arguments, composite := parseKIRContainerType(left)
		if composite && name == "Array" && len(arguments) == 1 && expression.Type == left {
			return true
		}
	}
	switch expression.Operator {
	case "&&", "||":
		return left == "Bool" && expression.Type == "Bool"
	case "+":
		if left == "String" {
			return expression.Type == "String"
		}
		return left == "Int" && expression.Type == "Int" || strings.HasPrefix(left, "UInt") && expression.Type == left
	case "-", "*", "/", "%":
		return left == "Int" && expression.Type == "Int" || strings.HasPrefix(left, "UInt") && expression.Type == left
	case "&", "|", "^":
		return strings.HasPrefix(left, "UInt") && expression.Type == left
	case "==", "!=":
		if expression.Type != "Bool" {
			return false
		}
		switch left {
		case "Int", "Bool", "Nil", "String", "UInt8", "UInt16", "UInt32", "UInt64":
			return true
		default:
			return false
		}
	case "<", "<=", ">", ">=":
		return (left == "Int" || strings.HasPrefix(left, "UInt")) && expression.Type == "Bool"
	default:
		return false
	}
}

func (builder *kirDirectBuilder) emitMapLiteral(expression *KIRExpr) error {
	mapTypes, ok := directKIRMapType(expression.Type)
	if !ok || len(builder.exprMapKeys(expression)) != len(builder.exprValues(expression)) {
		return fmt.Errorf("invalid KIR executable: map literal has invalid type or entry counts")
	}
	machine := builder.machine
	machine.emitMoveImmediate(uint64(len(builder.exprMapKeys(expression)) * 2))
	machine.arrayRuntimeUsed = true
	machine.mapRuntimeUsed = true
	if err := machine.emitArrayAllocCall(); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x50) // keep map pointer while evaluating entries
	for index, key := range builder.exprMapKeys(expression) {
		value := builder.exprValues(expression)[index]
		if key == nil || value == nil || key.Type != mapTypes[0] || value.Type != mapTypes[1] {
			return fmt.Errorf("invalid KIR executable: map literal entry does not match %q", expression.Type)
		}
		if err := builder.emitExpr(key); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x50) // key
		if err := builder.emitExpr(value); err != nil {
			return err
		}
		machine.code = append(machine.code,
			0x49, 0x89, 0xc0, // r8=value
			0x58,                   // rax=key
			0x48, 0x8b, 0x0c, 0x24, // rcx=map
			0x48, 0xba,
		)
		var offset [8]byte
		binary.LittleEndian.PutUint64(offset[:], uint64(8+index*16))
		machine.code = append(machine.code, offset[:]...)
		machine.code = append(machine.code,
			0x48, 0x01, 0xca, // rdx += map
			0x48, 0x89, 0x02, // map[key]
			0x48, 0x83, 0xc2, 0x08,
			0x4c, 0x89, 0x02, // map[value]
		)
	}
	machine.code = append(machine.code, 0x58)
	return nil
}

func (builder *kirDirectBuilder) emitMapBuiltin(expression *KIRExpr) error {
	if expression == nil || len(builder.exprArgs(expression)) < 2 || builder.exprArgs(expression)[0] == nil {
		return fmt.Errorf("invalid KIR executable: direct map builtin has missing arguments")
	}
	mapTypes, ok := directKIRMapType(builder.exprArgs(expression)[0].Type)
	if !ok {
		return fmt.Errorf("invalid KIR executable: direct map builtin has a non-Map argument")
	}
	keyKind, ok := directKIRMapKeyKind(mapTypes[0])
	if !ok {
		return fmt.Errorf("direct KIR ELF does not lower map keys of type %q", mapTypes[0])
	}
	machine := builder.machine
	machine.mapRuntimeUsed = true
	switch expression.Name {
	case "map_get", "map_contains_key":
		if len(builder.exprArgs(expression)) != 2 || builder.exprArgs(expression)[1] == nil {
			return fmt.Errorf("direct KIR ELF %s expects a map and key", expression.Name)
		}
		if err := builder.emitExpr(builder.exprArgs(expression)[0]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x50)
		if err := builder.emitExpr(builder.exprArgs(expression)[1]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x89, 0xc6, 0x5f) // rsi=key; rdi=map
		builder.emitMapKeyKind(keyKind)
		if err := machine.emitLabelCall(machine.mapFindLabel); err != nil {
			return err
		}
		if expression.Name == "map_contains_key" {
			machine.code = append(machine.code, 0x48, 0x85, 0xc0, 0x0f, 0x95, 0xc0, 0x48, 0x0f, 0xb6, 0xc0)
			return nil
		}
		none, done := machine.newLabel(), machine.newLabel()
		machine.code = append(machine.code, 0x48, 0x85, 0xc0)
		if err := machine.emitConditionalJump(0x84, none); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x8b, 0x00)
		if err := machine.emitBoxCall(1); err != nil {
			return err
		}
		if err := machine.emitJump(done); err != nil {
			return err
		}
		if err := machine.bind(none); err != nil {
			return err
		}
		machine.emitMoveImmediate(0)
		return machine.bind(done)
	case "map_insert":
		if len(builder.exprArgs(expression)) != 3 || builder.exprArgs(expression)[1] == nil || builder.exprArgs(expression)[2] == nil {
			return fmt.Errorf("direct KIR ELF map_insert expects a map, key, and value")
		}
		if err := builder.emitExpr(builder.exprArgs(expression)[0]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x50)
		if err := builder.emitExpr(builder.exprArgs(expression)[1]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x50)
		if err := builder.emitExpr(builder.exprArgs(expression)[2]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x89, 0xc1, 0x5e, 0x5f) // rcx=value, rsi=key, rdi=map
		builder.emitMapKeyKind(keyKind)
		machine.arrayRuntimeUsed = true
		return machine.emitLabelCall(machine.mapInsertLabel)
	case "map_remove":
		if len(builder.exprArgs(expression)) != 2 || builder.exprArgs(expression)[1] == nil {
			return fmt.Errorf("direct KIR ELF map_remove expects a map and key")
		}
		if err := builder.emitExpr(builder.exprArgs(expression)[0]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x50)
		if err := builder.emitExpr(builder.exprArgs(expression)[1]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x89, 0xc6, 0x5f) // rsi=key; rdi=map
		builder.emitMapKeyKind(keyKind)
		machine.arrayRuntimeUsed = true
		machine.boxRuntimeUsed = true
		if err := machine.emitLabelCall(machine.mapRemoveLabel); err != nil {
			return err
		}
		// The shared dynamic-machine helper boxes its result. KIR's static
		// Map[K,V] value is the array payload, so discard the box wrapper.
		machine.code = append(machine.code, 0x48, 0x8b, 0x40, 0x08)
		return nil
	default:
		return fmt.Errorf("direct KIR ELF does not lower map builtin %q", expression.Name)
	}
}

func (builder *kirDirectBuilder) emitMapKeyKind(kind uint64) {
	builder.machine.code = append(builder.machine.code, 0x48, 0xba)
	var immediate [8]byte
	binary.LittleEndian.PutUint64(immediate[:], kind)
	builder.machine.code = append(builder.machine.code, immediate[:]...)
}

func (builder *kirDirectBuilder) emitArrayLiteral(expression *KIRExpr) error {
	machine := builder.machine
	machine.emitMoveImmediate(uint64(len(builder.exprItems(expression))))
	if err := machine.emitArrayAllocCall(); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x50) // retain the allocated array
	for index, item := range builder.exprItems(expression) {
		if err := builder.emitExpr(item); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x49, 0x89, 0xc0, 0x48, 0x8b, 0x0c, 0x24)
		machine.code = append(machine.code, 0x48, 0xba)
		var offset [8]byte
		binary.LittleEndian.PutUint64(offset[:], uint64(8+index*8))
		machine.code = append(machine.code, offset[:]...)
		machine.code = append(machine.code, 0x48, 0x01, 0xca, 0x4c, 0x89, 0x02)
	}
	machine.code = append(machine.code, 0x58)
	return nil
}

func (builder *kirDirectBuilder) emitArrayIndex(expression *KIRExpr) error {
	if builder.exprBase(expression) == nil || builder.exprLeft(expression) == nil {
		return fmt.Errorf("invalid KIR executable: array index is missing an operand")
	}
	if err := builder.emitExpr(builder.exprBase(expression)); err != nil {
		return err
	}
	machine := builder.machine
	machine.code = append(machine.code, 0x50)
	if err := builder.emitExpr(builder.exprLeft(expression)); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x48, 0x89, 0xc1, 0x58) // rcx=index; rax=array
	negative := builder.diag(expression, CatRuntime, "index must be a non-negative Int")
	outOfRange := builder.diag(expression, CatRuntime, "array index out of range")
	machine.code = append(machine.code, 0x48, 0x85, 0xc9)
	if err := machine.emitConditionalJump(0x88, negative); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x48, 0x3b, 0x08)
	if err := machine.emitConditionalJump(0x83, outOfRange); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x48, 0x8b, 0x44, 0xc8, 0x08)
	return nil
}

func (builder *kirDirectBuilder) emitStructLiteral(expression *KIRExpr) error {
	structure := directKIRStruct(builder.metadata, expression.StructName)
	if structure == nil || len(expression.Fields) != len(builder.exprValues(expression)) || len(expression.Fields) != len(structure.Fields) {
		return fmt.Errorf("invalid KIR executable: struct %q literal has invalid metadata", expression.StructName)
	}
	if err := builder.machine.emitStructAllocCall(len(structure.Fields)); err != nil {
		return err
	}
	builder.machine.code = append(builder.machine.code, 0x50) // retain object while evaluating fields
	for valueIndex, name := range expression.Fields {
		field, fieldIndex, ok := directKIRStructField(builder.metadata, expression.StructName, name)
		value := builder.exprValues(expression)[valueIndex]
		if !ok || value == nil || value.Type != field.Type {
			return fmt.Errorf("invalid KIR executable: struct %q literal has invalid field %q", expression.StructName, name)
		}
		if err := builder.emitExpr(value); err != nil {
			return err
		}
		builder.machine.code = append(builder.machine.code, 0x49, 0x89, 0xc0, 0x48, 0x8b, 0x0c, 0x24, 0x48, 0xba)
		var offset [8]byte
		binary.LittleEndian.PutUint64(offset[:], uint64((fieldIndex+1)*8))
		builder.machine.code = append(builder.machine.code, offset[:]...)
		builder.machine.code = append(builder.machine.code, 0x48, 0x01, 0xca, 0x4c, 0x89, 0x02)
	}
	builder.machine.code = append(builder.machine.code, 0x58) // return object pointer
	return nil
}

func (builder *kirDirectBuilder) emitStructField(expression *KIRExpr) error {
	if expression == nil || builder.exprBase(expression) == nil {
		return fmt.Errorf("invalid KIR executable: direct struct field access has no base")
	}
	field, index, ok := directKIRStructField(builder.metadata, builder.exprBase(expression).Type, expression.Field)
	if !ok || field.Type != expression.Type {
		return fmt.Errorf("invalid KIR executable: direct struct field %q is not in %q", expression.Field, builder.exprBase(expression).Type)
	}
	if err := builder.emitExpr(builder.exprBase(expression)); err != nil {
		return err
	}
	builder.machine.code = append(builder.machine.code, 0x48, 0x8b, 0x80)
	var offset [4]byte
	binary.LittleEndian.PutUint32(offset[:], uint32((index+1)*8))
	builder.machine.code = append(builder.machine.code, offset[:]...)
	return nil
}

func (builder *kirDirectBuilder) emitStringPredicate(expression *KIRExpr, mode string) error {
	if expression == nil || len(builder.exprArgs(expression)) != 2 || builder.exprArgs(expression)[0].Type != "String" || builder.exprArgs(expression)[1].Type != "String" {
		return fmt.Errorf("direct KIR ELF %s expects two String arguments", mode)
	}
	if mode != "contains" && mode != "starts_with" && mode != "ends_with" {
		return fmt.Errorf("direct KIR ELF does not lower string predicate %q", mode)
	}
	if err := builder.emitExpr(builder.exprArgs(expression)[0]); err != nil {
		return err
	}
	builder.machine.code = append(builder.machine.code, 0x50)
	if err := builder.emitExpr(builder.exprArgs(expression)[1]); err != nil {
		return err
	}
	machine := builder.machine
	// r8=haystack, r9=needle, r10=haystack length, r11=needle length.
	machine.code = append(machine.code, 0x48, 0x89, 0xc1, 0x58, 0x49, 0x89, 0xc0, 0x49, 0x89, 0xc9, 0x4d, 0x8b, 0x10, 0x4d, 0x8b, 0x19)
	falseLabel := machine.newLabel()
	foundLabel := machine.newLabel()
	doneLabel := machine.newLabel()
	machine.code = append(machine.code, 0x4d, 0x39, 0xda) // cmp haystack length, needle length
	if err := machine.emitConditionalJump(0x82, falseLabel); err != nil {
		return err
	}
	if mode == "ends_with" {
		machine.code = append(machine.code, 0x4c, 0x89, 0xd2, 0x4c, 0x29, 0xda) // rdx = haystack length - needle length
	} else {
		machine.code = append(machine.code, 0x48, 0x31, 0xd2) // rdx = 0
	}
	innerLabel := machine.newLabel()
	mismatchLabel := machine.newLabel()
	outerLabel := innerLabel
	if mode == "contains" {
		outerLabel = machine.newLabel()
		if err := machine.bind(outerLabel); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x4c, 0x89, 0xd0, 0x4c, 0x29, 0xd8, 0x48, 0x39, 0xc2) // compare index with last valid start
		if err := machine.emitConditionalJump(0x87, falseLabel); err != nil {
			return err
		}
	}
	machine.code = append(machine.code, 0x48, 0x31, 0xf6) // rsi=0 for this candidate start
	if err := machine.bind(innerLabel); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x4c, 0x39, 0xde) // compare with needle length
	if err := machine.emitConditionalJump(0x84, foundLabel); err != nil {
		return err
	}
	machine.code = append(machine.code,
		0x48, 0x89, 0xd7, 0x48, 0x01, 0xf7,
		0x49, 0x0f, 0xb6, 0x44, 0x38, 0x08,
		0x41, 0x0f, 0xb6, 0x4c, 0x31, 0x08,
		0x39, 0xc8,
	)
	if err := machine.emitConditionalJump(0x85, mismatchLabel); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x48, 0xff, 0xc6)
	if err := machine.emitJump(innerLabel); err != nil {
		return err
	}
	if err := machine.bind(mismatchLabel); err != nil {
		return err
	}
	if mode == "contains" {
		machine.code = append(machine.code, 0x48, 0xff, 0xc2)
		if err := machine.emitJump(outerLabel); err != nil {
			return err
		}
	} else {
		if err := machine.emitJump(falseLabel); err != nil {
			return err
		}
	}
	if err := machine.bind(foundLabel); err != nil {
		return err
	}
	machine.emitMoveImmediate(1)
	if err := machine.emitJump(doneLabel); err != nil {
		return err
	}
	if err := machine.bind(falseLabel); err != nil {
		return err
	}
	machine.emitMoveImmediate(0)
	return machine.bind(doneLabel)
}

func (builder *kirDirectBuilder) emitBuiltin(expression *KIRExpr) error {
	if expression == nil || expression.CallTarget != "builtin:"+expression.Name {
		return fmt.Errorf("invalid KIR executable: direct ELF builtin has no resolved target")
	}
	machine := builder.machine
	args := builder.exprArgs(expression)
	switch expression.Name {
	case "print", "println":
		return builder.emitPrint(expression)
	case "str":
		return builder.emitStringCast(expression)
	case "int":
		return builder.emitIntCast(expression)
	case "u8", "u16", "u32", "u64":
		return builder.emitUnsignedCast(expression)
	case "assert":
		if len(args) != 1 || args[0].Type != "Bool" {
			return fmt.Errorf("direct KIR ELF assert expects one Bool argument")
		}
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		failure := builder.diag(expression, CatRuntime, "assertion failed")
		machine.code = append(machine.code, 0x48, 0x85, 0xc0)
		if err := machine.emitConditionalJump(0x84, failure); err != nil {
			return err
		}
		machine.emitMoveImmediate(0)
		return nil
	case "assert_eq":
		if len(args) != 2 || args[0].Type != args[1].Type {
			return fmt.Errorf("direct KIR ELF assert_eq expects two values of the same scalar type")
		}
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x50)
		if err := builder.emitExpr(args[1]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x89, 0xc1, 0x58) // rcx=right; rax=left
		if args[0].Type == "String" {
			machine.code = append(machine.code, 0x48, 0x89, 0xc7, 0x48, 0x89, 0xce)
			machine.stringEqualUsed = true
			if err := machine.emitLabelCall(machine.stringEqualLabel); err != nil {
				return err
			}
			machine.code = append(machine.code, 0x48, 0x85, 0xc0)
			failure := builder.diag(expression, CatRuntime, "assertion failed: values are not equal")
			if err := machine.emitConditionalJump(0x84, failure); err != nil {
				return err
			}
		} else {
			switch args[0].Type {
			case "Int", "Bool", "Nil", "UInt8", "UInt16", "UInt32", "UInt64":
			default:
				return fmt.Errorf("direct KIR ELF assert_eq does not support %s values", args[0].Type)
			}
			machine.code = append(machine.code, 0x48, 0x39, 0xc8)
			failure := builder.diag(expression, CatRuntime, "assertion failed: values are not equal")
			if err := machine.emitConditionalJump(0x85, failure); err != nil {
				return err
			}
		}
		machine.emitMoveImmediate(0)
		return nil
	case "contains", "starts_with", "ends_with":
		return builder.emitStringPredicate(expression, expression.Name)
	case "len":
		if len(args) != 1 {
			return fmt.Errorf("direct KIR ELF len expects one argument")
		}
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x8b, 0x00)
		return nil
	case "bytes", "bytes_from_u8":
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x89, 0xc7)
		machine.hostRuntimeUsed = true
		machine.bytesFromArrayUsed = true
		return machine.emitLabelCall(machine.bytesFromArrayLabel)
	case "u8_array":
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x89, 0xc7)
		machine.hostRuntimeUsed = true
		machine.arrayRuntimeUsed = true
		machine.u8ArrayUsed = true
		return machine.emitLabelCall(machine.u8ArrayLabel)
	case "string_to_bytes":
		return builder.emitExpr(args[0])
	case "fs_read_text":
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x89, 0xc7)
		machine.hostRuntimeUsed = true
		machine.fsReadTextUsed = true
		machine.boxRuntimeUsed = true
		return machine.emitLabelCall(machine.fsReadTextLabel)
	case "fs_write_bytes":
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x50)
		if err := builder.emitExpr(args[1]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x89, 0xc6, 0x5f)
		machine.hostRuntimeUsed = true
		machine.fsWriteBytesUsed = true
		machine.boxRuntimeUsed = true
		return machine.emitLabelCall(machine.fsWriteBytesLabel)
	case "process_args":
		if len(args) != 0 {
			return fmt.Errorf("direct KIR ELF process_args expects no arguments")
		}
		builder.processArgsUsed = true
		machine.arrayRuntimeUsed = true
		machine.hostRuntimeUsed = true
		return machine.emitLabelCall(machine.processArgsLabel)
	case "string_chars":
		if len(args) != 1 {
			return fmt.Errorf("direct KIR ELF string_chars expects one argument")
		}
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x89, 0xc7)
		machine.arrayRuntimeUsed = true
		machine.hostRuntimeUsed = true
		machine.stringCharsUsed = true
		return machine.emitLabelCall(machine.stringCharsLabel)
	case "substring":
		if len(args) != 3 {
			return fmt.Errorf("direct KIR ELF substring expects three arguments")
		}
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x50)
		if err := builder.emitExpr(args[1]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x50)
		if err := builder.emitExpr(args[2]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x89, 0xc2, 0x5e, 0x5f)
		machine.hostRuntimeUsed = true
		machine.substringUsed = true
		machine.boxRuntimeUsed = true
		return machine.emitLabelCall(machine.substringLabel)
	case "array_push":
		if len(args) != 2 {
			return fmt.Errorf("direct KIR ELF array_push expects two arguments")
		}
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x50)
		if err := builder.emitExpr(args[1]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x89, 0xc6, 0x58, 0x48, 0x89, 0xc7)
		machine.arrayRuntimeUsed = true
		return machine.emitLabelCall(machine.arrayPushLabel)
	case "array_concat":
		if len(args) != 2 {
			return fmt.Errorf("direct KIR ELF array_concat expects two arguments")
		}
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x50)
		if err := builder.emitExpr(args[1]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x89, 0xc1, 0x58)
		machine.code = append(machine.code, 0x48, 0x89, 0xc7, 0x48, 0x89, 0xce)
		machine.arrayRuntimeUsed = true
		return machine.emitLabelCall(machine.arrayConcatLabel)
	case "array_get":
		if len(args) != 2 {
			return fmt.Errorf("direct KIR ELF array_get expects two arguments")
		}
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x50)
		if err := builder.emitExpr(args[1]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x89, 0xc6, 0x5f, 0x48, 0x85, 0xf6)
		none, join := machine.newLabel(), machine.newLabel()
		if err := machine.emitConditionalJump(0x88, none); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x3b, 0x37)
		if err := machine.emitConditionalJump(0x83, none); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x8b, 0x44, 0xf7, 0x08)
		if err := machine.emitBoxCall(1); err != nil {
			return err
		}
		if err := machine.emitJump(join); err != nil {
			return err
		}
		if err := machine.bind(none); err != nil {
			return err
		}
		machine.emitMoveImmediate(0)
		return machine.bind(join)
	case "array_indices":
		if len(args) != 1 {
			return fmt.Errorf("direct KIR ELF array_indices expects one argument")
		}
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x50, 0x48, 0x8b, 0x04, 0x24, 0x48, 0x8b, 0x00)
		if err := machine.emitArrayAllocCall(); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x50, 0x48, 0x31, 0xc9)
		loop, done := machine.newLabel(), machine.newLabel()
		if err := machine.bind(loop); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x8b, 0x54, 0x24, 0x08, 0x48, 0x3b, 0x0a)
		if err := machine.emitConditionalJump(0x83, done); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x8b, 0x14, 0x24, 0x49, 0x89, 0xc8, 0x49, 0xc1, 0xe0, 0x03, 0x49, 0x83, 0xc0, 0x08, 0x4c, 0x01, 0xc2, 0x48, 0x89, 0x0a, 0x48, 0xff, 0xc1)
		if err := machine.emitJump(loop); err != nil {
			return err
		}
		if err := machine.bind(done); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x58, 0x48, 0x83, 0xc4, 0x08)
		return nil
	case "array_slice":
		if len(args) != 3 {
			return fmt.Errorf("direct KIR ELF array_slice expects three arguments")
		}
		for index := 0; index < 2; index++ {
			if err := builder.emitExpr(args[index]); err != nil {
				return err
			}
			machine.code = append(machine.code, 0x50)
		}
		if err := builder.emitExpr(args[2]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x50)
		failure := builder.diag(expression, CatRuntime, "array slice range is out of bounds")
		machine.code = append(machine.code, 0x48, 0x83, 0x3c, 0x24, 0x00)
		if err := machine.emitConditionalJump(0x88, failure); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x83, 0x7c, 0x24, 0x08, 0x00)
		if err := machine.emitConditionalJump(0x88, failure); err != nil {
			return err
		}
		machine.code = append(machine.code,
			0x48, 0x8b, 0x44, 0x24, 0x10,
			0x48, 0x8b, 0x08,
			0x48, 0x39, 0x4c, 0x24, 0x08,
		)
		if err := machine.emitConditionalJump(0x87, failure); err != nil {
			return err
		}
		machine.code = append(machine.code,
			0x48, 0x2b, 0x4c, 0x24, 0x08,
			0x48, 0x39, 0x0c, 0x24,
		)
		if err := machine.emitConditionalJump(0x87, failure); err != nil {
			return err
		}
		machine.code = append(machine.code,
			0x48, 0x8b, 0x7c, 0x24, 0x10,
			0x48, 0x8b, 0x74, 0x24, 0x08,
			0x48, 0x8b, 0x14, 0x24,
			0x48, 0x83, 0xc4, 0x18,
		)
		machine.arrayRuntimeUsed = true
		return machine.emitLabelCall(machine.arraySliceLabel)
	case "array_take", "array_drop":
		if len(args) != 2 {
			return fmt.Errorf("direct KIR ELF %s expects two arguments", expression.Name)
		}
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x50)
		if err := builder.emitExpr(args[1]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x50)
		failure := builder.diag(expression, CatRuntime, "array_take/array_drop count out of range")
		machine.code = append(machine.code, 0x48, 0x83, 0x3c, 0x24, 0x00)
		if err := machine.emitConditionalJump(0x8c, failure); err != nil {
			return err
		}
		machine.code = append(machine.code,
			0x48, 0x8b, 0x44, 0x24, 0x08,
			0x48, 0x8b, 0x08,
			0x48, 0x39, 0x0c, 0x24,
		)
		if err := machine.emitConditionalJump(0x8f, failure); err != nil {
			return err
		}
		if expression.Name == "array_take" {
			machine.code = append(machine.code, 0x48, 0x8b, 0x7c, 0x24, 0x08, 0x31, 0xf6, 0x48, 0x8b, 0x14, 0x24)
		} else {
			machine.code = append(machine.code,
				0x48, 0x8b, 0x7c, 0x24, 0x08,
				0x48, 0x8b, 0x34, 0x24,
				0x48, 0x8b, 0x07,
				0x48, 0x2b, 0x04, 0x24,
				0x48, 0x89, 0xc2,
			)
		}
		machine.code = append(machine.code, 0x48, 0x83, 0xc4, 0x10)
		machine.arrayRuntimeUsed = true
		return machine.emitLabelCall(machine.arraySliceLabel)
	case "array_reverse":
		if len(args) != 1 {
			return fmt.Errorf("direct KIR ELF array_reverse expects one argument")
		}
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x89, 0xc7)
		machine.arrayRuntimeUsed = true
		return machine.emitLabelCall(machine.arrayReverseLabel)
	case "array_set":
		if len(args) != 3 {
			return fmt.Errorf("direct KIR ELF array_set expects three arguments")
		}
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x50)
		if err := builder.emitExpr(args[1]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x50)
		if err := builder.emitExpr(args[2]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x89, 0xc2, 0x5e, 0x5f)
		machine.arrayRuntimeUsed = true
		return machine.emitLabelCall(machine.arraySetLabel)
	case "map_get", "map_contains_key", "map_insert", "map_remove":
		return builder.emitMapBuiltin(expression)
	case "some", "ok", "err":
		if len(args) != 1 {
			return fmt.Errorf("direct KIR ELF %s expects one argument", expression.Name)
		}
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		tag := uint64(1)
		if expression.Name == "ok" {
			tag = 0
		}
		return machine.emitBoxCall(tag)
	case "none":
		if len(args) != 0 {
			return fmt.Errorf("direct KIR ELF none expects no arguments")
		}
		machine.emitMoveImmediate(0)
		return nil
	case "is_some", "is_none":
		if len(args) != 1 {
			return fmt.Errorf("direct KIR ELF %s expects one argument", expression.Name)
		}
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		set := byte(0x95)
		if expression.Name == "is_none" {
			set = 0x94
		}
		machine.code = append(machine.code, 0x48, 0x85, 0xc0, 0x0f, set, 0xc0, 0x48, 0x0f, 0xb6, 0xc0)
		return nil
	case "is_ok", "is_err":
		if len(args) != 1 {
			return fmt.Errorf("direct KIR ELF %s expects one argument", expression.Name)
		}
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		nilLabel, done := machine.newLabel(), machine.newLabel()
		machine.code = append(machine.code, 0x48, 0x85, 0xc0)
		if err := machine.emitConditionalJump(0x84, nilLabel); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x83, 0x38, 0)
		condition := byte(0x94)
		if expression.Name == "is_err" {
			condition = 0x95
		}
		machine.code = append(machine.code, 0x0f, condition, 0xc0, 0x48, 0x0f, 0xb6, 0xc0)
		if err := machine.emitJump(done); err != nil {
			return err
		}
		if err := machine.bind(nilLabel); err != nil {
			return err
		}
		machine.emitMoveImmediate(0)
		return machine.bind(done)
	case "unwrap_or":
		if len(args) != 2 {
			return fmt.Errorf("direct KIR ELF unwrap_or expects two arguments")
		}
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x50)
		if err := builder.emitExpr(args[1]); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x89, 0xc6, 0x5f, 0x48, 0x85, 0xff)
		none, done := machine.newLabel(), machine.newLabel()
		if err := machine.emitConditionalJump(0x84, none); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x8b, 0x47, 0x08)
		if err := machine.emitJump(done); err != nil {
			return err
		}
		if err := machine.bind(none); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x89, 0xf0)
		return machine.bind(done)
	case "result_error":
		if len(args) != 1 {
			return fmt.Errorf("direct KIR ELF result_error expects one argument")
		}
		if err := builder.emitExpr(args[0]); err != nil {
			return err
		}
		none, done := machine.newLabel(), machine.newLabel()
		machine.code = append(machine.code, 0x48, 0x85, 0xc0)
		if err := machine.emitConditionalJump(0x84, none); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x83, 0x38, 0)
		if err := machine.emitConditionalJump(0x84, none); err != nil {
			return err
		}
		machine.code = append(machine.code, 0x48, 0x8b, 0x78, 0x08, 0x48, 0xbe, 1, 0, 0, 0, 0, 0, 0, 0)
		machine.boxRuntimeUsed = true
		if err := machine.emitLabelCall(machine.boxAllocLabel); err != nil {
			return err
		}
		if err := machine.emitJump(done); err != nil {
			return err
		}
		if err := machine.bind(none); err != nil {
			return err
		}
		machine.emitMoveImmediate(0)
		return machine.bind(done)
	case "result_unwrap":
		if len(args) != 1 {
			return fmt.Errorf("direct KIR ELF result_unwrap expects one argument")
		}
		return builder.emitResultUnwrap(expression, args[0])
	default:
		return fmt.Errorf("direct KIR ELF does not lower builtin %q", expression.Name)
	}
}

func (builder *kirDirectBuilder) emitStringCast(expression *KIRExpr) error {
	if expression == nil || len(builder.exprArgs(expression)) != 1 {
		return fmt.Errorf("direct KIR ELF str expects one Display argument")
	}
	argument := builder.exprArgs(expression)[0]
	typ, err := builder.machineType(argument.Type)
	if err != nil {
		return err
	}
	if typ.Kind == TyString {
		return builder.emitExpr(argument)
	}
	if typ.Kind == TyNil {
		builder.machine.emitStringAddress("nil")
		return nil
	}
	kind := uint64(0)
	switch typ.Kind {
	case TyInt:
		kind = 0
	case TyUInt:
		kind = 1
	case TyBool:
		kind = 2
	default:
		return fmt.Errorf("direct KIR ELF str does not support %s", argument.Type)
	}
	if err := builder.emitExpr(argument); err != nil {
		return err
	}
	builder.machine.code = append(builder.machine.code, 0x48, 0x89, 0xc7, 0x48, 0xbe)
	var displayKind [8]byte
	binary.LittleEndian.PutUint64(displayKind[:], kind)
	builder.machine.code = append(builder.machine.code, displayKind[:]...)
	builder.machine.hostRuntimeUsed = true
	builder.machine.intToStringUsed = true
	return builder.machine.emitLabelCall(builder.machine.intToStringLabel)
}

func (builder *kirDirectBuilder) emitIntCast(expression *KIRExpr) error {
	if expression == nil || len(builder.exprArgs(expression)) != 1 {
		return fmt.Errorf("direct KIR ELF int expects one numeric, Bool, or String argument")
	}
	argument := builder.exprArgs(expression)[0]
	typ, err := builder.machineType(argument.Type)
	if err != nil {
		return err
	}
	switch typ.Kind {
	case TyInt, TyBool:
		return builder.emitExpr(argument)
	case TyUInt:
		if err := builder.emitExpr(argument); err != nil {
			return err
		}
		if typ.Bits == 64 {
			failure := builder.diag(expression, CatRuntime, "UInt is outside Int range")
			builder.machine.code = append(builder.machine.code, 0x48, 0x85, 0xc0)
			return builder.machine.emitConditionalJump(0x88, failure) // js: UInt > MaxInt64
		}
		return nil
	case TyString:
		return builder.emitIntStringCast(expression, argument)
	default:
		return fmt.Errorf("direct KIR ELF int does not lower %s inputs", argument.Type)
	}
}

func (builder *kirDirectBuilder) emitIntStringCast(expression, argument *KIRExpr) error {
	if err := builder.emitExpr(argument); err != nil {
		return err
	}
	machine := builder.machine
	failure := builder.diag(expression, CatRuntime, "String must be a complete decimal String")
	negative, loop, parsedNegative, negativeResult, done := machine.newLabel(), machine.newLabel(), machine.newLabel(), machine.newLabel(), machine.newLabel()
	// Keep parser state in caller-saved registers: rdi=string, r8=byte length,
	// r9=byte index, r10=negative flag, r11=negative accumulator.
	machine.code = append(machine.code,
		0x48, 0x89, 0xc7, // mov rdi, rax
		0x4c, 0x8b, 0x07, // mov r8, [rdi]
		0x4d, 0x31, 0xc9, // xor r9, r9
		0x4d, 0x31, 0xd2, // xor r10, r10
		0x4d, 0x31, 0xdb, // xor r11, r11
		0x31, 0xf6, // xor esi, esi (digit start offset)
		0x4d, 0x85, 0xc0, // test r8, r8
	)
	if err := machine.emitConditionalJump(0x84, failure); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x42, 0x0f, 0xb6, 0x54, 0x0f, 0x08, 0x80, 0xfa, '-')
	if err := machine.emitConditionalJump(0x84, negative); err != nil {
		return err
	}
	positive := machine.newLabel()
	machine.code = append(machine.code, 0x80, 0xfa, '+')
	if err := machine.emitConditionalJump(0x84, positive); err != nil {
		return err
	}
	if err := machine.emitJump(loop); err != nil {
		return err
	}
	if err := machine.bind(negative); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x49, 0xc7, 0xc2, 1, 0, 0, 0, 0x49, 0xff, 0xc1, 0x48, 0xff, 0xc6)
	if err := machine.emitJump(loop); err != nil {
		return err
	}
	if err := machine.bind(positive); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x49, 0xff, 0xc1, 0x48, 0xff, 0xc6)
	if err := machine.emitJump(loop); err != nil {
		return err
	}
	if err := machine.bind(loop); err != nil {
		return err
	}
	machine.code = append(machine.code,
		0x4d, 0x39, 0xc1, // cmp r9, r8
	)
	if err := machine.emitConditionalJump(0x83, parsedNegative); err != nil {
		return err
	}
	machine.code = append(machine.code,
		0x42, 0x0f, 0xb6, 0x54, 0x0f, 0x08, // movzx edx, byte [rdi+r9+8]
		0x80, 0xfa, '0', // cmp dl, '0'
	)
	if err := machine.emitConditionalJump(0x82, failure); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x80, 0xfa, '9')
	if err := machine.emitConditionalJump(0x87, failure); err != nil {
		return err
	}
	machine.code = append(machine.code,
		0x83, 0xea, 0x30, // edx -= '0'
		0x4d, 0x6b, 0xdb, 10, // imul r11, r11, 10
	)
	if err := machine.emitConditionalJump(0x80, failure); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x49, 0x29, 0xd3) // sub r11, rdx
	if err := machine.emitConditionalJump(0x80, failure); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x49, 0xff, 0xc1)
	if err := machine.emitJump(loop); err != nil {
		return err
	}
	if err := machine.bind(parsedNegative); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x49, 0x39, 0xf1) // cmp r9, rsi (reject sign only)
	if err := machine.emitConditionalJump(0x84, failure); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x4d, 0x85, 0xd2) // test r10, r10
	if err := machine.emitConditionalJump(0x85, negativeResult); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x4c, 0x89, 0xd8, 0x48, 0xf7, 0xd8) // rax=-r11
	if err := machine.emitConditionalJump(0x80, failure); err != nil {
		return err
	}
	if err := machine.emitJump(done); err != nil {
		return err
	}
	if err := machine.bind(negativeResult); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x4c, 0x89, 0xd8) // rax=r11 for negative input
	if err := machine.bind(done); err != nil {
		return err
	}
	return nil
}

func (builder *kirDirectBuilder) emitUnsignedCast(expression *KIRExpr) error {
	if expression == nil || len(builder.exprArgs(expression)) != 1 {
		return fmt.Errorf("direct KIR ELF conversion %s expects one argument", expression.Name)
	}
	argument := builder.exprArgs(expression)[0]
	inputType, err := builder.machineType(argument.Type)
	if err != nil {
		return err
	}
	outputType, err := builder.machineType(expression.Type)
	if err != nil {
		return err
	}
	if inputType.Kind != TyInt && inputType.Kind != TyUInt {
		return fmt.Errorf("direct KIR ELF unsigned conversion expects Int or UInt")
	}
	if err := builder.emitExpr(argument); err != nil {
		return err
	}
	if inputType.Kind == TyInt {
		negative := builder.diag(expression, CatRuntime, "Int is outside unsigned range")
		builder.machine.code = append(builder.machine.code, 0x48, 0x85, 0xc0)
		if err := builder.machine.emitConditionalJump(0x88, negative); err != nil {
			return err
		}
	}
	if outputType.Bits < 64 {
		failure := builder.diag(expression, CatRuntime, "value is outside unsigned range")
		builder.machine.code = append(builder.machine.code, 0x48, 0xb9)
		var maximum [8]byte
		binary.LittleEndian.PutUint64(maximum[:], uint64(1)<<outputType.Bits)
		builder.machine.code = append(builder.machine.code, maximum[:]...)
		builder.machine.code = append(builder.machine.code, 0x48, 0x39, 0xc8)      // cmp rax, rcx
		if err := builder.machine.emitConditionalJump(0x83, failure); err != nil { // jae
			return err
		}
	}
	builder.machine.emitUIntMask(outputType.Bits)
	return nil
}

func (builder *kirDirectBuilder) emitResultUnwrap(call, value *KIRExpr) error {
	if call == nil || value == nil {
		return fmt.Errorf("direct KIR ELF result_unwrap expects one argument")
	}
	_, resultTypes, composite := parseKIRContainerType(value.Type)
	if !composite || !strings.HasPrefix(value.Type, "Result[") || len(resultTypes) != 2 {
		return fmt.Errorf("direct KIR ELF cannot determine result_unwrap payload type")
	}
	payloadType, err := builder.machineType(resultTypes[1])
	if err != nil {
		return err
	}
	switch payloadType.Kind {
	case TyString, TyInt, TyBool, TyUInt:
	default:
		return fmt.Errorf("direct KIR ELF cannot report result_unwrap error payload type %s", resultTypes[1])
	}
	if err := builder.emitExpr(value); err != nil {
		return err
	}
	machine := builder.machine
	machine.code = append(machine.code, 0x48, 0x85, 0xc0)
	if err := machine.emitConditionalJump(0x84, machine.trapLabel); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x48, 0x83, 0x38, 0)
	errorLabel, join := machine.newLabel(), machine.newLabel()
	if err := machine.emitConditionalJump(0x85, errorLabel); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x48, 0x8b, 0x40, 0x08)
	if err := machine.emitJump(join); err != nil {
		return err
	}
	if err := machine.bind(errorLabel); err != nil {
		return err
	}
	machine.code = append(machine.code, 0x4c, 0x8b, 0x60, 0x08) // preserve error payload in r12
	const payloadMarker = "__DIRECT_KIR_ELF_RESULT_ERROR__"
	source := builder.sources[call.Source]
	if source == nil && call.Source != "" {
		source = &Source{Name: call.Source}
	}
	diagnostic := Diag(CatRuntime, source, call.Line, call.Column, "cannot unwrap error Result: %s", payloadMarker)
	formatted := diagnostic.Format(false)
	parts := strings.SplitN(formatted, payloadMarker, 2)
	if len(parts) != 2 {
		return fmt.Errorf("direct KIR ELF could not format result_unwrap diagnostic")
	}
	if err := machine.emitELFWriteRawTo(parts[0], 2); err != nil {
		return err
	}
	switch payloadType.Kind {
	case TyString:
		machine.code = append(machine.code, 0x4c, 0x89, 0xe0)
		if err := machine.emitELFWriteStringFD(2); err != nil {
			return err
		}
	case TyInt:
		machine.code = append(machine.code, 0x4c, 0x89, 0xe0)
		if err := machine.emitELFIntegerToFD(false, 2); err != nil {
			return err
		}
	case TyUInt:
		machine.code = append(machine.code, 0x4c, 0x89, 0xe0)
		if err := machine.emitELFIntegerToFD(true, 2); err != nil {
			return err
		}
	case TyBool:
		falseLabel, boolDone := machine.newLabel(), machine.newLabel()
		machine.code = append(machine.code, 0x4d, 0x85, 0xe4)
		if err := machine.emitConditionalJump(0x84, falseLabel); err != nil {
			return err
		}
		if err := machine.emitELFWriteRawTo("true", 2); err != nil {
			return err
		}
		if err := machine.emitJump(boolDone); err != nil {
			return err
		}
		if err := machine.bind(falseLabel); err != nil {
			return err
		}
		if err := machine.emitELFWriteRawTo("false", 2); err != nil {
			return err
		}
		if err := machine.bind(boolDone); err != nil {
			return err
		}
	}
	if err := machine.emitELFWriteRawTo(parts[1], 2); err != nil {
		return err
	}
	if err := builder.emitDynamicDiagnosticStack(); err != nil {
		return err
	}
	if err := machine.emitExit(1); err != nil {
		return err
	}
	return machine.bind(join)
}
