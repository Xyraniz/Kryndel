package main

import (
	"bufio"
	"fmt"
	"os"
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
	if len(a) < 1 || len(a) > 3 {
		return usage("build expects FILE [-o OUTPUT]")
	}
	src := a[0]
	out := ""
	for i := 1; i < len(a); i++ {
		if a[i] == "-o" && i+1 < len(a) {
			out = a[i+1]
			i++
		} else {
			return usage("unknown build option")
		}
	}
	if out == "" {
		out = strings.TrimSuffix(src, ".kry") + ".kexe"
	}
	if d := e.BuildPath(src, out); d != nil {
		return report(d, jsonMode)
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
func printHelp() {
	fmt.Println("Kryndel " + version + " — self-contained language toolchain")
	fmt.Println("usage: kry [global-options] command [arguments]")
	fmt.Println("commands: check, run, build, fmt, repl, doctor, version")
	fmt.Println("global options: --json, --restricted ROOT, --max-source BYTES, --max-artifact BYTES, --max-instructions N, --max-wall-ms N")
}
