package kry

import (
	"fmt"
	"strings"
)

func (builder *kirDirectBuilder) machineType(encoded string) (*Type, error) {
	if typ, ok := builder.types[encoded]; ok {
		return typ, nil
	}
	typ, err := directKIRType(encoded, builder.structs, builder.enums)
	if err != nil {
		return nil, err
	}
	builder.types[encoded] = typ
	return typ, nil
}

// directKIRType resolves the already-typed KIR spelling into the small
// machine type descriptor used by the direct emitter. It reads no parser,
// checker, Program, or AST state.
func directKIRType(encoded string, structs map[string]*KIRStruct, enums map[string]*KIREnum) (*Type, error) {
	switch encoded {
	case "Int":
		return TInt, nil
	case "Bool":
		return TBool, nil
	case "UInt8":
		return TUInt8, nil
	case "UInt16":
		return TUInt16, nil
	case "UInt32":
		return TUInt32, nil
	case "UInt64":
		return TUInt64, nil
	case "Float":
		return TFloat, nil
	case "String":
		return TString, nil
	case "Bytes":
		return TBytes, nil
	case "Json":
		return TJSON, nil
	case "Nil":
		return TNil, nil
	}

	name, arguments, composite, ok := splitDirectKIRType(encoded)
	if !ok {
		return nil, fmt.Errorf("direct KIR ELF received an invalid typed value %q", encoded)
	}
	if composite {
		parsed := make([]*Type, len(arguments))
		for i, argument := range arguments {
			parsedType, err := directKIRType(argument, structs, enums)
			if err != nil {
				return nil, err
			}
			parsed[i] = parsedType
		}
		switch name {
		case "Array":
			if len(parsed) == 1 {
				return Arr(parsed[0]), nil
			}
		case "Option":
			if len(parsed) == 1 {
				return Opt(parsed[0]), nil
			}
		case "Result":
			if len(parsed) == 2 {
				return Res(parsed[0], parsed[1]), nil
			}
		case "Map":
			if len(parsed) == 2 {
				return MapOf(parsed[0], parsed[1]), nil
			}
		case "Struct":
			// Struct is not a source type name; this case makes malformed
			// spellings fail explicitly instead of falling through as a name.
		}
		if _, exists := structs[name]; exists {
			return &Type{Kind: TyStruct, Name: name, Params: parsed}, nil
		}
		return nil, fmt.Errorf("direct KIR ELF cannot lower type %q", encoded)
	}
	if _, exists := structs[name]; exists {
		return &Type{Kind: TyStruct, Name: name}, nil
	}
	if _, exists := enums[name]; exists {
		return &Type{Kind: TyEnum, Name: name}, nil
	}
	return nil, fmt.Errorf("direct KIR ELF cannot lower type %q", encoded)
}

func splitDirectKIRType(encoded string) (name string, arguments []string, composite, ok bool) {
	open := strings.IndexByte(encoded, '[')
	if open < 0 {
		if encoded == "" || strings.ContainsAny(encoded, "][],() ") {
			return "", nil, false, false
		}
		return encoded, nil, false, true
	}
	if open == 0 || !strings.HasSuffix(encoded, "]") {
		return "", nil, false, false
	}
	name = encoded[:open]
	body := encoded[open+1 : len(encoded)-1]
	if body == "" {
		return "", nil, false, false
	}
	depth, start := 0, 0
	for i, char := range body {
		switch char {
		case '[':
			depth++
		case ']':
			depth--
			if depth < 0 {
				return "", nil, false, false
			}
		case ',':
			if depth == 0 {
				argument := strings.TrimSpace(body[start:i])
				if argument == "" {
					return "", nil, false, false
				}
				arguments = append(arguments, argument)
				start = i + 1
			}
		}
	}
	if depth != 0 {
		return "", nil, false, false
	}
	last := strings.TrimSpace(body[start:])
	if last == "" {
		return "", nil, false, false
	}
	arguments = append(arguments, last)
	return name, arguments, true, true
}

func directKIRTypeSupported(encoded string, document *KIRDocument) bool {
	switch encoded {
	case "Int", "UInt8", "UInt16", "UInt32", "UInt64", "Bool", "String", "Bytes", "Json", "Nil":
		return true
	}
	name, arguments, composite, ok := splitDirectKIRType(encoded)
	if !ok {
		return false
	}
	if !composite {
		return directKIRStruct(document, name) != nil
	}
	switch name {
	case "Array", "Option":
		return len(arguments) == 1 && directKIRTypeSupported(arguments[0], document)
	case "Result":
		return len(arguments) == 2 && directKIRTypeSupported(arguments[0], document) && directKIRTypeSupported(arguments[1], document)
	case "Map":
		return len(arguments) == 2 && directKIRTypeSupported(arguments[0], document) && directKIRTypeSupported(arguments[1], document)
	default:
		return false
	}
}

func kirDirectUIntBits(encoded string) uint8 {
	bits, _ := kirExecUIntBits(encoded)
	return bits
}

func directKIRMapKeyKind(encoded string) (uint64, bool) {
	if encoded == "String" {
		return 1, true
	}
	switch encoded {
	case "Int", "UInt8", "UInt16", "UInt32", "UInt64", "Bool":
		return 0, true
	default:
		return 0, false
	}
}

func directKIRStruct(document *KIRDocument, name string) *KIRStruct {
	if document == nil || name == "" {
		return nil
	}
	for _, structure := range document.Structs {
		if structure != nil && structure.Name == name && len(structure.TypeParams) == 0 {
			return structure
		}
	}
	return nil
}

func directKIRStructField(document *KIRDocument, encodedType, name string) (*KIRField, int, bool) {
	structure := directKIRStruct(document, encodedType)
	if structure == nil {
		return nil, -1, false
	}
	for index, field := range structure.Fields {
		if field != nil && field.Name == name {
			return field, index, true
		}
	}
	return nil, -1, false
}
