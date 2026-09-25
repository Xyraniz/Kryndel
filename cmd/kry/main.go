package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/Xyraniz/Kryndel/internal/kry"
	"github.com/Xyraniz/Kryndel/internal/kry/lsp"
)

const version = kry.CompilerVersion

func main() { os.Exit(run(os.Args[1:])) }
func run(args []string) int {
	e := kry.NewEngine()
	jsonMode := false
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "--") {
		switch args[i] {
		case "--help":
			printHelp()
			return 0
		case "--version":
			fmt.Println("Kryndel " + version)
			return 0
		case "--json":
			jsonMode = true
			i++
		case "--restricted":
			if i+1 >= len(args) {
				return usage("--restricted requires ROOT")
			}
			e.RestrictedRoot = args[i+1]
			i += 2
		case "--max-source":
			i = takeLimit(args, i, "source", &e.Limits.MaxSourceBytes)
			if i < 0 {
				return 2
			}
		case "--max-artifact":
			i = takeLimit(args, i, "artifact", &e.Limits.MaxArtifactBytes)
			if i < 0 {
				return 2
			}
		case "--max-json":
			i = takeLimit(args, i, "json", &e.Limits.MaxJSONBytes)
			if i < 0 {
				return 2
			}
		case "--max-instructions":
			v, n := nextInt(args, i)
			if n < 0 {
				return 2
			}
			e.Limits.MaxInstructions = uint64(v)
			i = n
		case "--max-wall-ms":
			v, n := nextInt(args, i)
			if n < 0 {
				return 2
			}
			e.Limits.MaxWallTimeMS = int64(v)
			i = n
		case "--passphrase":
			if i+1 >= len(args) {
				return usage("--passphrase requires a value")
			}
			e.Passphrase = args[i+1]
			i += 2
		case "--passphrase-file":
			if i+1 >= len(args) {
				return usage("--passphrase-file requires a path")
			}
			pass, err := readPassphraseFile(args[i+1])
			if err != nil {
				fmt.Fprintln(os.Stderr, "kry:", err)
				return 2
			}
			e.Passphrase = pass
			i += 2
		default:
			return usage("unknown option " + args[i])
		}
	}
	e.JSON = jsonMode
	if i >= len(args) {
		return usage("missing command")
	}
	cmd := args[i]
	rest := args[i+1:]
	// A source/artifact path is itself a valid command. This is intentional:
	// desktop file associations invoke `kry path/to/file.kry`, just like a
	// Python association invokes `python path/to/file.py`.
	if isProgramPath(cmd) {
		if len(rest) != 0 {
			return usage("a direct program invocation does not accept extra arguments")
		}
		if err := maybePromptPassphrase(e, cmd); err != nil {
			fmt.Fprintln(os.Stderr, "kry:", err)
			return 2
		}
		_, d := e.RunPath(cmd)
		return report(d, jsonMode)
	}
	switch cmd {
	case "help", "--help", "-h":
		printHelp()
		return 0
	case "version", "--version":
		fmt.Println("Kryndel " + version)
		return 0
	case "doctor":
		if !e.Doctor() {
			fmt.Fprintln(os.Stderr, "doctor: not ready")
			return 1
		}
		fmt.Println("doctor: ready")
		fmt.Println("implementation: Go standard library (minimum toolchain 1.25.2)")
		fmt.Println("runtime: self-contained executable")
		fmt.Printf("limits: source=%d artifact=%d json=%d instructions=%d\n", e.Limits.MaxSourceBytes, e.Limits.MaxArtifactBytes, e.Limits.MaxJSONBytes, e.Limits.MaxInstructions)
		return 0
	case "capabilities":
		return capabilitiesCmd(rest, jsonMode)
	case "check":
		return checkCmd(e, rest, jsonMode)
	case "run":
		if len(rest) < 1 {
			return usage("run expects one source or artifact path")
		}
		if err := maybePromptPassphrase(e, rest[0]); err != nil {
			fmt.Fprintln(os.Stderr, "kry:", err)
			return 2
		}
		_, d := e.RunPathWithArgs(rest[0], rest[1:])
		return report(d, jsonMode)
	case "build":
		return buildCmd(e, rest, jsonMode)
	case "emit":
		return emitCmd(e, rest, jsonMode)
	case "inspect":
		return inspectCmd(e, rest, jsonMode)
	case "new":
		return projectNew(rest)
	case "init":
		return projectInit(rest)
	case "add":
		return projectAdd(rest)
	case "remove":
		return projectRemove(rest)
	case "install", "update":
		return projectInstall(rest)
	case "uninstall":
		return projectUninstall(rest)
	case "search":
		return projectSearch(rest)
	case "test":
		return projectTest(e, rest, jsonMode)
	case "package":
		return projectPackage(rest)
	case "publish":
		return projectPublish(rest)
	case "cache":
		return projectCache(rest)
	case "registry":
		return registryCmd(rest)
	case "fmt", "format":
		return fmtCmd(e, rest, jsonMode)
	case "repl":
		if len(rest) != 0 {
			return usage("repl does not accept positional arguments")
		}
		return repl(e)
	case "lsp":
		if len(rest) != 0 {
			return usage("lsp does not accept positional arguments")
		}
		status, err := lsp.Run(context.Background(), os.Stdin, os.Stdout)
		if err != nil {
			fmt.Fprintln(os.Stderr, "kry lsp:", err)
			return 1
		}
		return status
	default:
		return usage("unknown command " + cmd)
	}
}

func capabilitiesCmd(args []string, jsonMode bool) int {
	if len(args) == 1 && args[0] == "--features" {
		rows := kry.LanguageCapabilityMatrix()
		if jsonMode {
			if err := json.NewEncoder(os.Stdout).Encode(rows); err != nil {
				fmt.Fprintln(os.Stderr, "kry: cannot encode language capabilities:", err)
				return 1
			}
			return 0
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "CATEGORY\tFEATURE\tTARGET\tINTERPRETER\tC-AOT\tELF-DIRECT\tSELF-HOSTED")
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", row.Category, row.Feature, row.Target, row.Interpreter, row.CAOT, row.ELFDirect, row.SelfHosted)
		}
		if err := w.Flush(); err != nil {
			fmt.Fprintln(os.Stderr, "kry: cannot write language capabilities:", err)
			return 1
		}
		return 0
	}
	if len(args) == 1 && args[0] == "--builtins" {
		rows := kry.BuiltinCapabilityMatrix()
		if jsonMode {
			if err := json.NewEncoder(os.Stdout).Encode(rows); err != nil {
				fmt.Fprintln(os.Stderr, "kry: cannot encode builtin capabilities:", err)
				return 1
			}
			return 0
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "BUILTIN\tTARGET\tINTERPRETER\tC-AOT\tELF-DIRECT\tSELF-HOSTED")
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", row.Builtin, row.Target, row.Interpreter, row.CAOT, row.ELFDirect, row.SelfHosted)
		}
		if err := w.Flush(); err != nil {
			fmt.Fprintln(os.Stderr, "kry: cannot write builtin capabilities:", err)
			return 1
		}
		return 0
	}
	if len(args) != 0 {
		return usage("capabilities accepts only --builtins or --features")
	}
	rows := kry.NativeCapabilityMatrix()
	if jsonMode {
		if err := json.NewEncoder(os.Stdout).Encode(rows); err != nil {
			fmt.Fprintln(os.Stderr, "kry: cannot encode capabilities:", err)
			return 1
		}
		return 0
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "FORMAT\tTARGET\tSTATUS\tBACKEND\tTOOLCHAIN\tFEATURE SCOPE\tREASON")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", row.Format, row.Target, row.Status, row.Backend, row.Toolchain, row.FeatureScope, row.Reason)
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, "kry: cannot write capabilities:", err)
		return 1
	}
	return 0
}
func isProgramPath(path string) bool {
	if filepath.Ext(path) != ".kry" && filepath.Ext(path) != ".kexe" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
func takeLimit(a []string, i int, name string, dst *int) int {
	v, n := nextInt(a, i)
	if n < 0 || v < 0 {
		return -1
	}
	*dst = v
	return n
}
func nextInt(a []string, i int) (int, int) {
	if i+1 >= len(a) {
		fmt.Fprintln(os.Stderr, "kry: option requires an integer value")
		return 0, -1
	}
	v, err := strconv.Atoi(a[i+1])
	if err != nil || v < 0 {
		fmt.Fprintln(os.Stderr, "kry: invalid non-negative integer:", a[i+1])
		return 0, -1
	}
	return v, i + 2
}
func report(d *kry.Diagnostic, jsonMode bool) int {
	if d != nil {
		if jsonMode {
			fmt.Print(d.Format(true))
		} else {
			fmt.Fprint(os.Stderr, d.Format(false))
		}
		if d.Category == kry.CatCLI {
			return 2
		}
		return 1
	}
	return 0
}

func checkCmd(e *kry.Engine, args []string, jsonMode bool) int {
	werror := false
	werrorCodes := map[string]bool{}
	disabledWarnings := map[string]bool{}
	path := ""
	for _, arg := range args {
		switch arg {
		case "-Werror", "--Werror":
			werror = true
		default:
			if strings.HasPrefix(arg, "-Werror=") {
				codes, err := parseWarningCodes(strings.TrimPrefix(arg, "-Werror="))
				if err != nil {
					return usage(err.Error())
				}
				for _, code := range codes {
					werrorCodes[code] = true
				}
				continue
			}
			if strings.HasPrefix(arg, "-Wno=") {
				codes, err := parseWarningCodes(strings.TrimPrefix(arg, "-Wno="))
				if err != nil {
					return usage(err.Error())
				}
				for _, code := range codes {
					disabledWarnings[code] = true
				}
				continue
			}
			if path != "" {
				return usage("check expects one source or artifact path")
			}
			path = arg
		}
	}
	if path == "" {
		return usage("check expects one source or artifact path")
	}
	program, _, diagnostic := e.CheckPath(path)
	if diagnostic != nil {
		return report(diagnostic, jsonMode)
	}
	warnings := kry.AnalyzeWarnings(program)
	hasError := false
	for _, warning := range warnings {
		if disabledWarnings[warning.Code] {
			continue
		}
		if werror || werrorCodes[warning.Code] {
			copy := *warning
			copy.Severity = "error"
			warning = &copy
			hasError = true
		}
		if jsonMode {
			fmt.Print(warning.Format(true))
		} else {
			fmt.Fprint(os.Stderr, warning.Format(false))
		}
	}
	if hasError {
		return 1
	}
	return 0
}

func parseWarningCodes(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	codes := make([]string, 0, len(parts))
	for _, part := range parts {
		code := strings.TrimSpace(part)
		if code == "" {
			return nil, fmt.Errorf("warning option requires one or more comma-separated KRYW codes")
		}
		switch code {
		case kry.WarnRedundantMatch, kry.WarnConstantBranch, kry.WarnInfiniteLoop, kry.WarnUnreachable:
			codes = append(codes, code)
		default:
			return nil, fmt.Errorf("unknown warning code %q", code)
		}
	}
	return codes, nil
}
func usage(msg string) int {
	if msg != "" {
		fmt.Fprintln(os.Stderr, "kry:", msg)
	}
	fmt.Fprintln(os.Stderr, "try 'kry --help'")
	return 2
}

// readPassphraseFile reads a passphrase from a file, trimming a single trailing
// newline so files created by editors or `echo` work as expected.
func readPassphraseFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("cannot read passphrase file: %v", err)
	}
	s := strings.TrimRight(string(data), "\r\n")
	if s == "" {
		return "", fmt.Errorf("passphrase file is empty")
	}
	return s, nil
}

// maybePromptPassphrase asks for a passphrase on the terminal when the target is
// a sealed artifact and none was supplied on the command line. Non-interactive
// callers (pipes, CI) simply get the "passphrase required" error from the
// engine instead of hanging on a prompt.
func maybePromptPassphrase(e *kry.Engine, path string) error {
	if e.Passphrase != "" || filepath.Ext(path) != ".kexe" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil // let the engine report the read error
	}
	if !kry.IsSealedArtifact(data) {
		return nil
	}
	info, err := os.Stdin.Stat()
	if err != nil || (info.Mode()&os.ModeCharDevice) == 0 {
		return nil // not a terminal; engine will report the missing passphrase
	}
	fmt.Fprint(os.Stderr, "passphrase: ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return fmt.Errorf("cannot read passphrase: %v", err)
	}
	e.Passphrase = strings.TrimRight(line, "\r\n")
	return nil
}

func buildCmd(e *kry.Engine, a []string, jsonMode bool) int {
	if len(a) < 1 {
		return usage("build expects FILE")
	}
	src, out, format, target := a[0], "", "kexe", "host"
	encrypt := false
	obfuscate := false
	noExternalToolchain := false
	iterations := 0
	for i := 1; i < len(a); i++ {
		x := a[i]
		switch {
		case x == "-o" && i+1 < len(a):
			out = a[i+1]
			i++
		case strings.HasPrefix(x, "--format="):
			format = strings.TrimPrefix(x, "--format=")
		case x == "--format" && i+1 < len(a):
			format = a[i+1]
			i++
		case strings.HasPrefix(x, "--target="):
			target = strings.TrimPrefix(x, "--target=")
		case x == "--target" && i+1 < len(a):
			target = a[i+1]
			i++
		case x == "--encrypt":
			encrypt = true
		case x == "--obfuscate":
			obfuscate = true
		case x == "--no-external-toolchain":
			noExternalToolchain = true
		case strings.HasPrefix(x, "--iterations="):
			v, err := strconv.Atoi(strings.TrimPrefix(x, "--iterations="))
			if err != nil || v < 0 {
				return usage("invalid --iterations value")
			}
			iterations = v
		case x == "--iterations" && i+1 < len(a):
			v, err := strconv.Atoi(a[i+1])
			if err != nil || v < 0 {
				return usage("invalid --iterations value")
			}
			iterations = v
			i++
		case x == "--release", x == "--debug", x == "--gui":
		default:
			return usage("unknown build option " + x)
		}
	}
	if encrypt && format != "kexe" && format != "" {
		return usage("--encrypt only applies to the kexe container format")
	}
	if format == "kexe" || format == "" {
		if out == "" {
			out = strings.TrimSuffix(src, ".kry") + ".kexe"
		}
		if encrypt {
			if e.Passphrase == "" {
				return usage("--encrypt requires --passphrase or --passphrase-file")
			}
			if d := e.BuildSealedPath(src, out, e.Passphrase, iterations); d != nil {
				return report(d, jsonMode)
			}
			fmt.Println("built " + out + " (encrypted; backend=portable KRYNATIVE4; external-toolchain=none)")
			return 0
		}
		if d := e.BuildPath(src, out); d != nil {
			return report(d, jsonMode)
		}
		fmt.Println("built " + out + " (backend=portable KRYNATIVE4; external-toolchain=none)")
		return 0
	}
	backend, err := kry.DescribeNativeBackend(format)
	if err != nil {
		return report(kry.Diag(kry.CatCLI, nil, 1, 1, "%v", err), jsonMode)
	}
	if noExternalToolchain && backend.RequiresExternalToolchain {
		return report(kry.Diag(kry.CatCLI, nil, 1, 1, "--no-external-toolchain forbids --format=%s: the %s backend requires an external C compiler; use --format=elf-direct for the supported direct ELF backend", format, backend.Name), jsonMode)
	}
	p, c, d := e.CheckPath(src)
	if d != nil {
		return report(d, jsonMode)
	}
	t, err := kry.ParseNativeTarget(target)
	if err != nil {
		return report(kry.Diag(kry.CatCLI, nil, 1, 1, "%v", err), jsonMode)
	}
	data, err := kry.BuildNativeWithPolicyOpts(p, c, t, format, obfuscate, noExternalToolchain)
	if err != nil {
		return report(kry.Diag(kry.CatCLI, nil, 1, 1, "native build failed: %v", err), jsonMode)
	}
	if out == "" {
		if format == "exe" || format == "pe" || format == "pe-direct" {
			out = strings.TrimSuffix(src, ".kry") + ".exe"
		} else if format == "c" {
			out = strings.TrimSuffix(src, ".kry") + ".c"
		} else {
			out = strings.TrimSuffix(src, ".kry")
		}
	}
	if err := kry.WriteTextAtomic(out, data); err != nil {
		return report(kry.Diag(kry.CatIO, nil, 1, 1, "cannot write native output: %v", err), jsonMode)
	}
	// Native executables must be runnable even when the output path has no
	// extension (the default for ELF targets).
	if format == "exe" || format == "pe" || format == "pe-direct" || format == "elf" || format == "elf-direct" {
		_ = os.Chmod(out, 0o755)
	}
	fmt.Printf("built %s (backend=%s; external-toolchain=%s)\n", out, backend.Name, backend.ExternalToolchain)
	return 0
}
func fmtCmd(e *kry.Engine, a []string, jsonMode bool) int {
	write, check := false, false
	var path string
	for _, x := range a {
		switch x {
		case "-w":
			write = true
		case "--check":
			check = true
		default:
			if path != "" {
				return usage("fmt expects one file")
			}
			path = x
		}
	}
	if path == "" {
		return usage("fmt expects a file")
	}
	out, d, status := e.FormatPath(path, write, check)
	if d != nil {
		return report(d, jsonMode)
	}
	if check && status != 0 {
		fmt.Fprintln(os.Stderr, "format: file is not formatted")
		return 1
	}
	if !write && !check {
		fmt.Print(out)
	}
	return status
}
func repl(e *kry.Engine) int {
	var definitions []string
	buf := ""
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), e.Limits.MaxSourceBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if line == ":quit" || line == ":q" {
			return 0
		}
		if line == ":reset" {
			definitions = nil
			buf = ""
			continue
		}
		if strings.HasPrefix(line, ":type ") {
			fmt.Println("type queries are available through checked expressions")
			continue
		}
		buf += line + "\n"
		if !balanced(buf) {
			continue
		}
		trimmed := strings.TrimSpace(buf)
		candidate := buf
		if len(definitions) > 0 {
			candidate = strings.Join(definitions, "\n") + "\n" + buf
		}
		src := &kry.Source{Name: "<repl>", Text: candidate}
		p, d := kry.Parse(src, e.Limits)
		if d != nil {
			fmt.Fprint(os.Stderr, d.Format(false))
			buf = ""
			continue
		}
		c, d := kry.Check(p, e.Limits)
		if d != nil {
			fmt.Fprint(os.Stderr, d.Format(false))
			buf = ""
			continue
		}
		r, _ := kry.NewRuntime(p, c, e.Limits, kry.Sandbox{})
		if d := r.RunForREPL(); d != nil {
			fmt.Fprint(os.Stderr, d.Format(false))
			buf = ""
			continue
		}
		if strings.HasPrefix(trimmed, "let ") || strings.HasPrefix(trimmed, "fn ") || strings.HasPrefix(trimmed, "pub fn ") || strings.HasPrefix(trimmed, "struct ") || strings.HasPrefix(trimmed, "enum ") || strings.HasPrefix(trimmed, "import ") {
			definitions = append(definitions, buf)
		}
		buf = ""
	}
	if buf != "" {
		if d := replEval(e, buf); d != nil {
			fmt.Fprint(os.Stderr, d.Format(false))
		}
	}
	return 0
}
func balanced(s string) bool {
	depth := 0
	quoted, esc := false, false
	for _, r := range s {
		if quoted {
			if esc {
				esc = false
			} else if r == '\\' {
				esc = true
			} else if r == '"' {
				quoted = false
			}
			continue
		}
		if r == '"' {
			quoted = true
		}
		if r == '{' || r == '(' || r == '[' {
			depth++
		}
		if r == '}' || r == ')' || r == ']' {
			depth--
		}
	}
	return depth <= 0 && !quoted
}
func replEval(e *kry.Engine, s string) *kry.Diagnostic {
	src := &kry.Source{Name: "<repl>", Text: s}
	p, d := kry.Parse(src, e.Limits)
	if d != nil {
		return d
	}
	c, d := kry.Check(p, e.Limits)
	if d != nil {
		return d
	}
	r, _ := kry.NewRuntime(p, c, e.Limits, kry.Sandbox{})
	return r.RunForREPL()
}
func emitCmd(e *kry.Engine, a []string, jsonMode bool) int {
	if len(a) < 1 {
		return usage("emit expects FILE")
	}
	src, format, out, targetName := a[0], "kry-ir", "", "host"
	for i := 1; i < len(a); i++ {
		switch {
		case strings.HasPrefix(a[i], "--format="):
			format = strings.TrimPrefix(a[i], "--format=")
		case a[i] == "-o" && i+1 < len(a):
			out = a[i+1]
			i++
		case strings.HasPrefix(a[i], "--target="):
			targetName = strings.TrimPrefix(a[i], "--target=")
		case a[i] == "--target" && i+1 < len(a):
			targetName = a[i+1]
			i++
		default:
			return usage("unknown emit option")
		}
	}
	p, _, d := e.CheckPath(src)
	if d != nil {
		return report(d, jsonMode)
	}
	if format != "kry-ir" {
		if format == "llvm-ir" {
			return report(kry.Diag(kry.CatCLI, nil, 1, 1, "LLVM IR emission is not implemented; use --format=kry-ir"), jsonMode)
		}
		return report(kry.Diag(kry.CatCLI, nil, 1, 1, "unsupported emit format %s", format), jsonMode)
	}
	t, err := kry.ParseNativeTarget(targetName)
	if err != nil {
		return report(kry.Diag(kry.CatCLI, nil, 1, 1, "invalid target %s: %v", targetName, err), jsonMode)
	}
	c, d := kry.Check(p, e.Limits)
	if d != nil {
		return report(d, jsonMode)
	}
	data, err := kry.EmitKIR(p, c, t)
	if err != nil {
		return report(kry.Diag(kry.CatArtifact, nil, 1, 1, "cannot emit KIR: %v", err), jsonMode)
	}
	if out != "" {
		if err := kry.WriteTextAtomic(out, data); err != nil {
			return report(kry.Diag(kry.CatIO, nil, 1, 1, "cannot write emitted IR: %v", err), jsonMode)
		}
		fmt.Println("emitted " + out)
	} else {
		fmt.Print(string(data))
	}
	return 0
}
func inspectCmd(e *kry.Engine, a []string, jsonMode bool) int {
	if len(a) != 1 {
		return usage("inspect expects one binary path")
	}
	data, err := os.ReadFile(a[0])
	if err != nil {
		return report(kry.Diag(kry.CatIO, nil, 1, 1, "cannot read binary: %v", err), jsonMode)
	}
	text, err := kry.InspectNative(data)
	if err != nil {
		return report(kry.Diag(kry.CatArtifact, nil, 1, 1, "%v", err), jsonMode)
	}
	fmt.Print(text)
	return 0
}
func projectDir() string { d, _ := os.Getwd(); return d }
func projectNew(a []string) int {
	if len(a) != 1 {
		return usage("new expects PROJECT")
	}
	dir := a[0]
	if err := kry.NewProject(dir, filepath.Base(dir)); err != nil {
		fmt.Fprintln(os.Stderr, "kry new:", err)
		return 1
	}
	fmt.Println("created " + dir)
	return 0
}
func projectInit(a []string) int {
	if len(a) != 0 {
		return usage("init takes no arguments")
	}
	if err := kry.NewProject(projectDir(), filepath.Base(projectDir())); err != nil {
		fmt.Fprintln(os.Stderr, "kry init:", err)
		return 1
	}
	fmt.Println("initialized project")
	return 0
}
func projectAdd(a []string) int {
	if len(a) < 1 || len(a) > 2 {
		return usage("add expects PACKAGE [VERSION]")
	}
	v := "*"
	if len(a) == 2 {
		v = a[1]
	}
	if err := kry.AddDependency(projectDir(), a[0], v); err != nil {
		fmt.Fprintln(os.Stderr, "kry add:", err)
		return 1
	}
	fmt.Println("added " + a[0])
	return 0
}
func projectRemove(a []string) int {
	if len(a) != 1 {
		return usage("remove expects PACKAGE")
	}
	m, err := kry.ReadManifest(projectDir())
	if err != nil {
		fmt.Fprintln(os.Stderr, "kry remove:", err)
		return 1
	}
	delete(m.Dependencies, a[0])
	if err := kry.WriteManifest(projectDir(), m); err != nil {
		fmt.Fprintln(os.Stderr, "kry remove:", err)
		return 1
	}
	fmt.Println("removed " + a[0])
	return 0
}
func projectInstall(a []string) int {
	if _, err := os.Stat(filepath.Join(projectDir(), "kry.toml")); os.IsNotExist(err) {
		if err := kry.EnsureProject(projectDir(), filepath.Base(projectDir())); err != nil {
			fmt.Fprintln(os.Stderr, "kry install:", err)
			return 1
		}
	} else if err != nil {
		fmt.Fprintln(os.Stderr, "kry install:", err)
		return 1
	}
	lock, err := kry.NewPackageManager().Install(projectDir(), a)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kry install:", err)
		return 1
	}
	fmt.Printf("installed %d package(s)\n", len(lock.Packages))
	return 0
}
func projectUninstall(a []string) int {
	if len(a) == 0 {
		return usage("uninstall expects PACKAGE [PACKAGE ...]")
	}
	lock, err := kry.NewPackageManager().Uninstall(projectDir(), a)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kry uninstall:", err)
		return 1
	}
	fmt.Printf("uninstalled %d package(s); %d package(s) remain\n", len(a), len(lock.Packages))
	return 0
}
func projectSearch(a []string) int {
	if len(a) != 1 {
		return usage("search expects a word")
	}
	names, err := kry.NewPackageManager().Search(a[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "kry search:", err)
		return 1
	}
	for _, name := range names {
		fmt.Println(name)
	}
	return 0
}
func projectTest(e *kry.Engine, a []string, jsonMode bool) int {
	if len(a) > 1 {
		return usage("test accepts an optional source")
	}
	p := "main.kry"
	if len(a) == 1 {
		p = a[0]
	}
	_, d := e.RunPath(p)
	return report(d, jsonMode)
}
func projectPackage(a []string) int {
	dir := projectDir()
	out := ""
	if len(a) > 1 {
		return usage("package accepts optional output")
	}
	if len(a) == 1 {
		out = a[0]
	}
	m, err := kry.ReadManifest(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kry package:", err)
		return 1
	}
	if out == "" {
		out = filepath.Join(dir, m.Name+"-"+m.Version+".kpkg")
	}
	if err := kry.PackageArchive(dir, out); err != nil {
		fmt.Fprintln(os.Stderr, "kry package:", err)
		return 1
	}
	fmt.Println("packaged " + out)
	return 0
}
func projectPublish(a []string) int {
	if len(a) != 0 {
		return usage("publish takes no arguments")
	}
	if err := kry.NewPackageManager().Publish(projectDir()); err != nil {
		fmt.Fprintln(os.Stderr, "kry publish:", err)
		return 1
	}
	fmt.Println("published project")
	return 0
}
func projectCache(a []string) int {
	if len(a) != 1 || a[0] != "clean" {
		return usage("cache clean")
	}
	if err := kry.NewPackageManager().CleanCache(); err != nil {
		fmt.Fprintln(os.Stderr, "kry cache clean:", err)
		return 1
	}
	fmt.Println("cache cleaned")
	return 0
}
func registryCmd(a []string) int {
	if len(a) < 2 || a[0] != "serve" {
		return usage("registry serve ROOT [--addr LOOPBACK:PORT]")
	}
	addr := "127.0.0.1:8765"
	for i := 2; i+1 < len(a); i++ {
		if a[i] == "--addr" {
			addr = a[i+1]
		}
	}
	if err := kry.ServeRegistry(a[1], addr); err != nil {
		fmt.Fprintln(os.Stderr, "kry registry:", err)
		return 1
	}
	return 0
}
func printHelp() {
	fmt.Println("Kryndel " + version + " — self-contained language toolchain")
	fmt.Println("usage: kry [global-options] command [arguments]")
	fmt.Println("       kry [global-options] FILE.kry|FILE.kexe")
	fmt.Println("commands: help, check, run, build, emit, inspect, capabilities, fmt, lsp, repl, doctor, version")
	fmt.Println("project: new, init, add, remove, install, uninstall, update, search, test, package, publish, cache clean, registry serve")
	fmt.Println("build formats: kexe, exe/pe, elf (C AOT); elf-direct (Linux subset); pe-direct (Windows x64 scalar/function subset); c; targets: windows-x64, windows-arm64, linux-x64, linux-arm64, darwin-x64, darwin-arm64")
	fmt.Println("build options: -o OUT, --format F, --target T, --encrypt, --iterations N, --obfuscate, --no-external-toolchain")
	fmt.Println("check options: -Werror, -Werror=KRYW002,KRYW004, -Wno=KRYW003")
	fmt.Println("global options: --help, --version, --json, --restricted ROOT, --max-source BYTES, --max-artifact BYTES, --max-json BYTES, --max-instructions N, --max-wall-ms N (0 disables the wall-time limit)")
	fmt.Println("sealed artifacts: --passphrase VALUE, --passphrase-file PATH (AES-256-GCM + PBKDF2-SHA256)")
}
