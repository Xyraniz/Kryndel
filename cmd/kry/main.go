package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Xyraniz/Kryndel/internal/kry"
)

const version = "1.2.0"

func main() { os.Exit(run(os.Args[1:])) }
func run(args []string) int {
	e := kry.NewEngine()
	jsonMode := false
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "--") {
		switch args[i] {
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
		fmt.Println("implementation: Go 1.22 standard library")
		fmt.Println("runtime: self-contained executable")
		fmt.Printf("limits: source=%d artifact=%d instructions=%d\n", e.Limits.MaxSourceBytes, e.Limits.MaxArtifactBytes, e.Limits.MaxInstructions)
		return 0
	case "check":
		if len(rest) != 1 {
			return usage("check expects one source or artifact path")
		}
		_, _, d := e.CheckPath(rest[0])
		return report(d, jsonMode)
	case "run":
		if len(rest) != 1 {
			return usage("run expects one source or artifact path")
		}
		_, d := e.RunPath(rest[0])
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
	default:
		return usage("unknown command " + cmd)
	}
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
func usage(msg string) int {
	if msg != "" {
		fmt.Fprintln(os.Stderr, "kry:", msg)
	}
	fmt.Fprintln(os.Stderr, "try 'kry --help'")
	return 2
}
func buildCmd(e *kry.Engine, a []string, jsonMode bool) int {
	if len(a) < 1 {
		return usage("build expects FILE")
	}
	src, out, format, target := a[0], "", "kexe", "host"
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
		case x == "--release", x == "--debug", x == "--gui":
		default:
			return usage("unknown build option " + x)
		}
	}
	if format == "kexe" || format == "" {
		if out == "" {
			out = strings.TrimSuffix(src, ".kry") + ".kexe"
		}
		if d := e.BuildPath(src, out); d != nil {
			return report(d, jsonMode)
		}
		fmt.Println("built " + out)
		return 0
	}
	p, _, d := e.CheckPath(src)
	if d != nil {
		return report(d, jsonMode)
	}
	t, err := kry.ParseNativeTarget(target)
	if err != nil {
		return report(kry.Diag(kry.CatCLI, nil, 1, 1, "%v", err), jsonMode)
	}
	data, err := kry.BuildNative(p, t, format)
	if err != nil {
		return report(kry.Diag(kry.CatCLI, nil, 1, 1, "native build failed: %v", err), jsonMode)
	}
	if out == "" {
		if format == "exe" || format == "pe" {
			out = strings.TrimSuffix(src, ".kry") + ".exe"
		} else {
			out = strings.TrimSuffix(src, ".kry")
		}
	}
	if err := kry.WriteTextAtomic(out, data); err != nil {
		return report(kry.Diag(kry.CatIO, nil, 1, 1, "cannot write native output: %v", err), jsonMode)
	}
	fmt.Println("built " + out)
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
	src, format, out := a[0], "llvm-ir", ""
	for i := 1; i < len(a); i++ {
		switch {
		case strings.HasPrefix(a[i], "--format="):
			format = strings.TrimPrefix(a[i], "--format=")
		case a[i] == "-o" && i+1 < len(a):
			out = a[i+1]
			i++
		default:
			return usage("unknown emit option")
		}
	}
	p, _, d := e.CheckPath(src)
	if d != nil {
		return report(d, jsonMode)
	}
	if format != "llvm-ir" {
		return report(kry.Diag(kry.CatCLI, nil, 1, 1, "unsupported emit format %s", format), jsonMode)
	}
	t, _ := kry.ParseNativeTarget("host")
	data := kry.EmitLLVMIR(p, t)
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
	lock, err := kry.NewPackageManager().Install(projectDir(), a)
	if err != nil {
		fmt.Fprintln(os.Stderr, "kry install:", err)
		return 1
	}
	fmt.Printf("installed %d package(s)\n", len(lock.Packages))
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
		return usage("registry serve ROOT [--addr HOST:PORT]")
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
	fmt.Println("commands: check, run, build, emit, inspect, fmt, repl, doctor, version")
	fmt.Println("project: new, init, add, remove, install, update, search, test, package, publish, cache clean, registry serve")
	fmt.Println("build formats: kexe, exe/pe, elf; targets: windows-x64, windows-arm64, linux-x64")
	fmt.Println("global options: --json, --restricted ROOT, --max-source BYTES, --max-artifact BYTES, --max-instructions N, --max-wall-ms N")
}
