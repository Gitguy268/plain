// Command plain is the one tool for Plain: new, run, build, check, test and fmt.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Gitguy268/plain/internal/codegen"
	"github.com/Gitguy268/plain/internal/compiler"
	"github.com/Gitguy268/plain/internal/diag"
	"github.com/Gitguy268/plain/internal/format"
)

const usage = `Plain %s: a small language without the usual surprises.

Usage:
  plain new <name>            make a project folder with a starter program
  plain run <file.pln> [args] compile and run (needs Bun or Node)
  plain build <file.pln> [-o out.js]
                              compile to one JavaScript file
  plain check <files...>      find problems without running anything
  plain test [files or dirs]  run the test blocks (default: this folder)
  plain fmt [--check] [files or dirs]
                              format code the one Plain way
  plain version               print the version

Docs and playground: https://gitguy268.github.io/plain/
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, usage, codegen.Version)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var code int
	switch cmd {
	case "run":
		code = runCmd(args)
	case "build":
		code = buildCmd(args)
	case "check":
		code = checkCmd(args)
	case "test":
		code = testCmd(args)
	case "fmt":
		code = fmtCmd(args)
	case "new":
		code = newCmd(args)
	case "version", "--version", "-v":
		fmt.Println("plain", codegen.Version)
	case "help", "--help", "-h":
		fmt.Printf(usage, codegen.Version)
	default:
		if strings.HasSuffix(cmd, ".pln") {
			code = runCmd(os.Args[1:])
			break
		}
		fmt.Fprintf(os.Stderr, "plain: unknown command %q\n\n", cmd)
		fmt.Fprintf(os.Stderr, usage, codegen.Version)
		code = 2
	}
	os.Exit(code)
}

func color() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	st, err := os.Stderr.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

func compile(path string, mode codegen.Mode) (compiler.Result, bool) {
	res := compiler.Compile(path, readFile, mode)
	if len(res.Diags) > 0 {
		fmt.Fprint(os.Stderr, diag.RenderAll(res.Diags, color()))
	}
	return res, res.OK()
}

// runtimeCmd finds Bun or Node.
func runtimeCmd() (string, error) {
	if p := os.Getenv("PLAIN_JS"); p != "" {
		return p, nil
	}
	for _, name := range []string{"bun", "node"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", errors.New("plain run needs Bun or Node to run the compiled JavaScript. Install one from https://bun.sh or https://nodejs.org, or use plain build and run the .js file yourself")
}

func execJS(js string, args []string) int {
	bin, err := runtimeCmd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "plain:", err)
		return 1
	}
	sum := sha256.Sum256([]byte(js))
	dir := filepath.Join(os.TempDir(), "plain-run")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "plain:", err)
		return 1
	}
	file := filepath.Join(dir, hex.EncodeToString(sum[:8])+".mjs")
	if _, err := os.Stat(file); err != nil {
		if err := os.WriteFile(file, []byte(js), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "plain:", err)
			return 1
		}
	}
	cmd := exec.Command(bin, append([]string{file}, args...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "plain:", err)
		return 1
	}
	return 0
}

func runCmd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "plain run: which file? For example: plain run main.pln")
		return 2
	}
	res, ok := compile(args[0], codegen.Run)
	if !ok {
		return 1
	}
	return execJS(res.JS, args[1:])
}

func buildCmd(args []string) int {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	out := fs.String("o", "", "output file (- for stdout)")
	var files []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return 2
		}
		if fs.NArg() == 0 {
			break
		}
		files = append(files, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if len(files) != 1 {
		fmt.Fprintln(os.Stderr, "plain build: give one .pln file, for example: plain build main.pln -o main.js")
		return 2
	}
	mode := codegen.Run
	res := compiler.Compile(files[0], readFile, mode)
	if len(res.Diags) > 0 {
		// a library without main is fine to build
		var keep []diag.Diagnostic
		for _, d := range res.Diags {
			if d.Title != "NO MAIN" {
				keep = append(keep, d)
			}
		}
		if len(keep) < len(res.Diags) {
			res = compiler.Compile(files[0], readFile, codegen.Lib)
			keep = res.Diags
		}
		if len(keep) > 0 {
			fmt.Fprint(os.Stderr, diag.RenderAll(keep, color()))
		}
	}
	if !res.OK() {
		return 1
	}
	target := *out
	if target == "" {
		target = strings.TrimSuffix(files[0], ".pln") + ".js"
	}
	if target == "-" {
		fmt.Print(res.JS)
		return 0
	}
	if err := os.WriteFile(target, []byte(res.JS), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "plain build:", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", target)
	return 0
}

func checkCmd(args []string) int {
	files, err := collect(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "plain check:", err)
		return 2
	}
	bad := 0
	for _, f := range files {
		if _, ok := compile(f, codegen.Lib); !ok {
			bad++
		}
	}
	if bad == 0 {
		fmt.Fprintf(os.Stderr, "No problems found in %d file(s).\n", len(files))
		return 0
	}
	return 1
}

// collect expands folders into the .pln files inside them.
func collect(args []string) ([]string, error) {
	if len(args) == 0 {
		args = []string{"."}
	}
	var out []string
	for _, a := range args {
		st, err := os.Stat(a)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			out = append(out, a)
			continue
		}
		err = filepath.WalkDir(a, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && p != a && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(p, ".pln") {
				out = append(out, p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

func testCmd(args []string) int {
	files, err := collect(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "plain test:", err)
		return 2
	}
	failed, ran := 0, 0
	for _, f := range files {
		res, ok := compile(f, codegen.Test)
		if !ok {
			failed++
			continue
		}
		if res.Tests == 0 {
			continue
		}
		ran++
		if execJS(res.JS, nil) != 0 {
			failed++
		}
	}
	if ran == 0 && failed == 0 {
		fmt.Fprintln(os.Stderr, "No tests found. Add one with: test \"name\" { assert 1 + 1 == 2 }")
		return 0
	}
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "\n%d file(s) had failures.\n", failed)
		return 1
	}
	return 0
}

func fmtCmd(args []string) int {
	check := false
	var rest []string
	for _, a := range args {
		if a == "--check" || a == "-check" {
			check = true
		} else {
			rest = append(rest, a)
		}
	}
	files, err := collect(rest)
	if err != nil {
		fmt.Fprintln(os.Stderr, "plain fmt:", err)
		return 2
	}
	code := 0
	for _, f := range files {
		text, err := readFile(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, "plain fmt:", err)
			code = 1
			continue
		}
		out, diags := format.Source(f, text)
		if diag.HasErrors(diags) {
			fmt.Fprint(os.Stderr, diag.RenderAll(diags, color()))
			code = 1
			continue
		}
		if out == text {
			continue
		}
		if check {
			fmt.Println(f)
			code = 1
			continue
		}
		if err := os.WriteFile(f, []byte(out), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "plain fmt:", err)
			code = 1
			continue
		}
		fmt.Fprintln(os.Stderr, "formatted", f)
	}
	return code
}

const starter = `// %s: run it with  plain run main.pln

type Pet = Dog(name: String) | Cat(name: String, lives: Int)

fn describe(pet: Pet) -> String {
  match pet {
    Dog(name) -> "{name} is a good dog"
    Cat(name, lives) -> "{name} has {lives} lives left"
  }
}

fn main() {
  let pets = [Dog("Spot"), Cat("Tom", 9)]
  for pet in pets {
    print(describe(pet))
  }
}

test "dogs are good" {
  assert describe(Dog("Rex")) == "Rex is a good dog"
}
`

func newCmd(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "plain new: give a folder name, for example: plain new hello")
		return 2
	}
	dir := args[0]
	if _, err := os.Stat(dir); err == nil {
		fmt.Fprintf(os.Stderr, "plain new: %s already exists\n", dir)
		return 1
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "plain new:", err)
		return 1
	}
	name := filepath.Base(dir)
	files := map[string]string{
		"main.pln":   fmt.Sprintf(starter, name),
		".gitignore": "*.js\n",
	}
	for f, body := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "plain new:", err)
			return 1
		}
	}
	fmt.Printf("Made %s. Next:\n\n  cd %s\n  plain run main.pln\n  plain test\n", dir, dir)
	return 0
}
