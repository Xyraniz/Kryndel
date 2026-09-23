package kry

import "fmt"

// LanguageCapability reports one language item for one output target. The
// inventories come from the interpreter/backend dispatch and the Stage 3
// source compiler; native rows describe registered lowering support, while
// "partial" marks implementations whose accepted combinations remain bounded.
type LanguageCapability struct {
	Category    string `json:"category"`
	Feature     string `json:"feature"`
	Target      string `json:"target"`
	Interpreter string `json:"interpreter"`
	CAOT        string `json:"c_aot"`
	ELFDirect   string `json:"elf_direct"`
	SelfHosted  string `json:"self_hosted"`
}

type languageCapabilityInventory struct {
	category string
	values   map[string]struct{}
	self     map[string]struct{}
	caot     map[string]struct{}
	direct   map[string]struct{}
	interp   map[string]struct{}
}

// LanguageCapabilityMatrix returns generated rows for every registered
// builtin, AST expression, statement, pattern, operator, and language type on
// every declared native target.
func LanguageCapabilityMatrix() []LanguageCapability {
	inventories := []languageCapabilityInventory{
		{category: "builtin", values: builtinNameSet(), interp: generatedInterpreterBuiltinCases, caot: generatedCAOTBuiltinCases, direct: generatedDirectELFBuiltinCases, self: generatedSelfHostedBuiltinNames},
		{category: "expression", values: generatedLanguageExprKinds, interp: generatedInterpreterExprKinds, caot: generatedCAOTExprKinds, direct: generatedDirectELFExprKinds, self: generatedSelfHostedExprKinds},
		{category: "statement", values: generatedLanguageStmtKinds, interp: generatedInterpreterStmtKinds, caot: generatedCAOTStmtKinds, direct: generatedDirectELFStmtKinds, self: generatedSelfHostedStmtKinds},
		{category: "pattern", values: generatedLanguagePatternKinds, interp: generatedInterpreterPatternKinds, caot: generatedCAOTPatternKinds, direct: map[string]struct{}{}, self: map[string]struct{}{}},
		{category: "unary_operator", values: generatedLanguageUnaryOperators, interp: generatedInterpreterUnaryOperators, caot: generatedCAOTUnaryOperators, direct: generatedDirectELFUnaryOperators, self: generatedSelfHostedUnaryOperators},
		{category: "binary_operator", values: generatedLanguageBinaryOperators, interp: generatedInterpreterBinaryOperators, caot: generatedCAOTBinaryOperators, direct: generatedDirectELFBinaryOperators, self: generatedSelfHostedBinaryOperators},
		{category: "type", values: generatedLanguageTypeKinds, interp: generatedLanguageTypeKinds, caot: generatedLanguageTypeKinds, direct: generatedDirectELFTypes, self: generatedSelfHostedTypes},
	}
	rowCount := 0
	for _, inventory := range inventories {
		rowCount += len(inventory.values)
	}
	rows := make([]LanguageCapability, 0, rowCount*len(nativeCapabilityTargets))
	for _, inventory := range inventories {
		for feature := range inventory.values {
			for _, target := range nativeCapabilityTargets {
				rows = append(rows, LanguageCapability{
					Category:    inventory.category,
					Feature:     feature,
					Target:      target.name,
					Interpreter: inventoryStatus(inventory.interp, feature, true),
					CAOT:        languageBackendStatus(inventory, feature, "c-aot", target.target),
					ELFDirect:   languageBackendStatus(inventory, feature, "elf-direct", target.target),
					SelfHosted:  languageBackendStatus(inventory, feature, "self-hosted", target.target),
				})
			}
		}
	}
	return rows
}

func builtinNameSet() map[string]struct{} {
	values := make(map[string]struct{}, len(builtinList))
	for _, builtin := range builtinList {
		values[builtin.Name] = struct{}{}
	}
	return values
}

func inventoryStatus(values map[string]struct{}, name string, presentMeansSupported bool) string {
	_, present := values[name]
	if present == presentMeansSupported {
		return "supported"
	}
	return "unsupported"
}

func languageBackendStatus(inventory languageCapabilityInventory, feature, backend string, target NativeTarget) string {
	switch backend {
	case "c-aot":
		format := "elf"
		if target.OS == "windows" {
			format = "exe"
		}
		if nativeOutputTargetReason(format, target) != "" {
			return "unsupported"
		}
		if inventory.category == "builtin" {
			return nativeBuiltinBackendStatus(feature, format, target)
		}
		if _, ok := inventory.caot[feature]; ok {
			return "supported"
		}
	case "elf-direct":
		if nativeOutputTargetReason("elf-direct", target) != "" {
			return "unsupported"
		}
		if inventory.category == "builtin" {
			return nativeBuiltinBackendStatus(feature, "elf-direct", target)
		}
		if _, ok := inventory.direct[feature]; ok {
			return "partial"
		}
	case "self-hosted":
		if target.OS != "linux" || target.Arch != "amd64" {
			return "unsupported"
		}
		if _, ok := inventory.self[feature]; ok {
			return "partial"
		}
	}
	return "unsupported"
}

func nativeLanguageItemStatus(category, feature, format string, target NativeTarget) string {
	if category == "builtin" {
		return nativeBuiltinBackendStatus(feature, format, target)
	}
	var inventory languageCapabilityInventory
	switch category {
	case "expression":
		inventory = languageCapabilityInventory{category: category, caot: generatedCAOTExprKinds, direct: generatedDirectELFExprKinds}
	case "statement":
		inventory = languageCapabilityInventory{category: category, caot: generatedCAOTStmtKinds, direct: generatedDirectELFStmtKinds}
	case "pattern":
		inventory = languageCapabilityInventory{category: category, caot: generatedCAOTPatternKinds}
	case "unary_operator":
		inventory = languageCapabilityInventory{category: category, caot: generatedCAOTUnaryOperators, direct: generatedDirectELFUnaryOperators}
	case "binary_operator":
		inventory = languageCapabilityInventory{category: category, caot: generatedCAOTBinaryOperators, direct: generatedDirectELFBinaryOperators}
	case "type":
		inventory = languageCapabilityInventory{category: category, caot: generatedLanguageTypeKinds, direct: generatedDirectELFTypes}
	default:
		return "unsupported"
	}
	if format == "elf-direct" {
		return languageBackendStatus(inventory, feature, format, target)
	}
	if nativeOutputTargetReason(format, target) != "" {
		return "unsupported"
	}
	if _, ok := inventory.caot[feature]; ok {
		return "supported"
	}
	return "unsupported"
}

func validateLanguageItem(category, feature, format string, target NativeTarget) error {
	status := nativeLanguageItemStatus(category, feature, format, target)
	if status == "unsupported" {
		return fmt.Errorf("%s %q is not listed as supported by the %s backend for %s-%s; use the interpreter for this feature", category, feature, format, target.OS, target.Arch)
	}
	return nil
}

func expressionKindName(kind ExprKind) string {
	switch kind {
	case ExInt:
		return "ExInt"
	case ExFloat:
		return "ExFloat"
	case ExBool:
		return "ExBool"
	case ExNil:
		return "ExNil"
	case ExString:
		return "ExString"
	case ExVar:
		return "ExVar"
	case ExUnary:
		return "ExUnary"
	case ExBinary:
		return "ExBinary"
	case ExCall:
		return "ExCall"
	case ExArray:
		return "ExArray"
	case ExIndex:
		return "ExIndex"
	case ExField:
		return "ExField"
	case ExStruct:
		return "ExStruct"
	case ExEnum:
		return "ExEnum"
	case ExMap:
		return "ExMap"
	case ExSet:
		return "ExSet"
	case ExPropagate:
		return "ExPropagate"
	default:
		return ""
	}
}

func statementKindName(kind StmtKind) string {
	switch kind {
	case StLet:
		return "StLet"
	case StExpr:
		return "StExpr"
	case StAssign:
		return "StAssign"
	case StIf:
		return "StIf"
	case StWhile:
		return "StWhile"
	case StReturn:
		return "StReturn"
	case StBreak:
		return "StBreak"
	case StContinue:
		return "StContinue"
	case StMatch:
		return "StMatch"
	case StFor:
		return "StFor"
	case StDefer:
		return "StDefer"
	case StUnsafe:
		return "StUnsafe"
	case StConst:
		return "StConst"
	default:
		return ""
	}
}

func patternKindName(kind PatternKind) string {
	switch kind {
	case PatWildcard:
		return "PatWildcard"
	case PatNil:
		return "PatNil"
	case PatBool:
		return "PatBool"
	case PatInt:
		return "PatInt"
	case PatString:
		return "PatString"
	case PatEnum:
		return "PatEnum"
	case PatOption:
		return "PatOption"
	case PatResult:
		return "PatResult"
	default:
		return ""
	}
}

func operatorKindName(kind TokenKind) string {
	switch opText(kind) {
	case "+":
		return "PLUS"
	case "-":
		return "MINUS"
	case "*":
		return "STAR"
	case "/":
		return "SLASH"
	case "%":
		return "PERCENT"
	case "!":
		return "BANG"
	case "~":
		return "BITNOT"
	case "==":
		return "EQEQ"
	case "!=":
		return "NEQ"
	case "<":
		return "LESS"
	case "<=":
		return "LEQ"
	case ">":
		return "GREATER"
	case ">=":
		return "GEQ"
	case "&&":
		return "AND"
	case "||":
		return "OR"
	case "&":
		return "BITAND"
	case "|":
		return "PIPE"
	case "^":
		return "BITXOR"
	case "<<":
		return "SHL"
	case ">>":
		return "SHR"
	default:
		return ""
	}
}

func typeKindName(kind TypeKind) string {
	switch kind {
	case TyVoid:
		return "TyVoid"
	case TyNil:
		return "TyNil"
	case TyInt:
		return "TyInt"
	case TyUInt:
		return "TyUInt"
	case TyFloat:
		return "TyFloat"
	case TyBool:
		return "TyBool"
	case TyString:
		return "TyString"
	case TyBytes:
		return "TyBytes"
	case TyArray:
		return "TyArray"
	case TyOption:
		return "TyOption"
	case TyResult:
		return "TyResult"
	case TyChannel:
		return "TyChannel"
	case TyThread:
		return "TyThread"
	case TyStruct:
		return "TyStruct"
	case TyEnum:
		return "TyEnum"
	case TyMap:
		return "TyMap"
	case TySet:
		return "TySet"
	case TyJSON:
		return "TyJSON"
	case TyWebSocket:
		return "TyWebSocket"
	case TyGeneric:
		return "TyGeneric"
	case TyActor:
		return "TyActor"
	case TyShared:
		return "TyShared"
	case TyTaskGroup:
		return "TyTaskGroup"
	case TyRegex:
		return "TyRegex"
	case TyRandom:
		return "TyRandom"
	case TySQLite:
		return "TySQLite"
	case TyTCPSocket:
		return "TyTCPSocket"
	case TyTCPListener:
		return "TyTCPListener"
	case TyUDPSocket:
		return "TyUDPSocket"
	case TyFFILibrary:
		return "TyFFILibrary"
	case TyFFISymbol:
		return "TyFFISymbol"
	case TyFFIBuffer:
		return "TyFFIBuffer"
	default:
		return ""
	}
}
