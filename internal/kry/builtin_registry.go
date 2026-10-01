package kry

var builtinRegistry map[string]Builtin

func initBuiltinRegistry() {
	builtinRegistry = make(map[string]Builtin, len(builtinList))
	for _, builtin := range builtinList {
		builtinRegistry[builtin.Name] = builtin
	}
}

func lookupBuiltin(name string) (Builtin, bool) {
	builtin, ok := builtinRegistry[name]
	return builtin, ok
}
