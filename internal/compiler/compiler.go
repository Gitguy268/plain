// Package compiler loads modules, checks them and produces JavaScript.
package compiler

import (
	"fmt"
	"path"
	"strings"

	"github.com/Gitguy268/plain/internal/codegen"
	"github.com/Gitguy268/plain/internal/diag"
	"github.com/Gitguy268/plain/internal/runtime"
	"github.com/Gitguy268/plain/internal/std"
	"github.com/Gitguy268/plain/internal/syntax"
	"github.com/Gitguy268/plain/internal/types"
)

// ReadFile reads a source file. The playground passes one that only knows the editor's text.
type ReadFile func(path string) (string, error)

// Result is the outcome of compiling one program.
type Result struct {
	JS    string
	Diags []diag.Diagnostic
	Tests int
}

// OK reports whether compilation produced code.
func (r Result) OK() bool { return !diag.HasErrors(r.Diags) }

// Compile compiles the program whose entry file is entryPath.
func Compile(entryPath string, read ReadFile, mode codegen.Mode) Result {
	text, err := read(entryPath)
	if err != nil {
		return Result{Diags: []diag.Diagnostic{{Title: "FILE NOT FOUND", Message: fmt.Sprintf("I couldn't read %s: %v", entryPath, err)}}}
	}
	return CompileSource(entryPath, text, read, mode)
}

// CompileSource compiles text as if it were the file entryPath.
func CompileSource(entryPath, text string, read ReadFile, mode codegen.Mode) Result {
	var parseDiags []diag.Diagnostic
	parse := func(name, file, text string, isStd bool) *types.Module {
		f, ds := syntax.ParseFile(diag.NewSource(file, text))
		parseDiags = append(parseDiags, ds...)
		return types.NewModule(name, isStd, f)
	}
	coreSrc, _ := std.Source("core")
	core := parse("core", "std/core.pln", coreSrc, true)
	entry := parse(jsIdent(strings.TrimSuffix(path.Base(entryPath), ".pln")), entryPath, text, false)

	user := map[string]*types.Module{}
	stdMods := map[string]*types.Module{}
	resolve := func(name string, from *types.Module) *types.Module {
		if from != nil && !from.Std {
			if m, ok := user[name]; ok {
				return m
			}
			file := path.Join(path.Dir(from.File.Src.Name), name+".pln")
			if t, err := read(file); err == nil {
				m := parse(name, file, t, false)
				user[name] = m
				return m
			}
		}
		if m, ok := stdMods[name]; ok {
			return m
		}
		if t, ok := std.Source(name); ok && name != "core" {
			m := parse(name, "std/"+name+".pln", t, true)
			stdMods[name] = m
			return m
		}
		return nil
	}
	info, diags := types.Check(entry, core, resolve, std.Names())
	if diag.HasErrors(parseDiags) {
		return Result{Diags: parseDiags}
	}
	diags = append(diags, checkEntry(entry, mode)...)
	diags = append(diags, checkExterns(info)...)
	res := Result{Diags: diags}
	for _, it := range entry.File.Items {
		if _, ok := it.(*syntax.TestDecl); ok {
			res.Tests++
		}
	}
	if diag.HasErrors(diags) {
		return res
	}
	res.JS = codegen.Bundle(info, entry, mode, runtime.Prelude)
	return res
}

func jsIdent(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func checkEntry(m *types.Module, mode codegen.Mode) []diag.Diagnostic {
	src := m.File.Src
	b, ok := m.Scope.Lookup("main")
	if !ok || b.Kind != types.BFn {
		if mode == codegen.Run {
			return []diag.Diagnostic{{Title: "NO MAIN", Src: src, Span: diag.Span{}, Message: fmt.Sprintf("%s has no main function, so there is nothing to run.", src.Name),
				Hint: "Add one, like:\n\n    fn main() {\n      print(\"Hello!\")\n    }"}}
		}
		return nil
	}
	d := b.Decl.(*syntax.FnDecl)
	ft := b.Scheme.Type.(*types.TFn)
	if len(d.Params) > 0 {
		return []diag.Diagnostic{{Title: "MAIN TAKES NO ARGUMENTS", Src: src, Span: d.NameSp, Message: "main can't have parameters.",
			Hint: "Read command-line arguments with os.args()."}}
	}
	if !types.IsCon(ft.Ret, "Unit") {
		r, ok := types.Prune(ft.Ret).(*types.TCon)
		if !ok || r.Key != "Result" || !types.IsCon(r.Args[0], "Unit") {
			return []diag.Diagnostic{{Title: "WRONG MAIN TYPE", Src: src, Span: d.NameSp, Message: fmt.Sprintf("main returns %s, but it can only return nothing or a Result[Unit, E].", types.TypeString(ft.Ret)),
				Hint: "With -> Result[Unit, String], Plain prints the error and exits with code 1 when main returns Err."}}
		}
	}
	return nil
}

func checkExterns(info *types.Info) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, m := range info.Modules {
		if m.Std {
			continue
		}
		for _, it := range m.File.Items {
			d, ok := it.(*syntax.ExternDecl)
			if !ok {
				continue
			}
			if strings.HasPrefix(d.Target, "$") || !codegen.ValidTarget(d.Target) {
				out = append(out, diag.Diagnostic{Title: "BAD EXTERN TARGET", Src: m.File.Src, Span: d.Sp,
					Message: fmt.Sprintf("I don't understand the JavaScript name %q.", d.Target),
					Hint:    "Use a global path like \"Math.random\" or \"JSON.stringify\", or a module and export like \"node:path#join\"."})
			}
		}
	}
	return out
}
