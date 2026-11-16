package types

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Gitguy268/plain/internal/diag"
	"github.com/Gitguy268/plain/internal/syntax"
)

// Resolver finds a module by name for an import. It returns nil when there is none.
type Resolver func(name string, from *Module) *Module

type global struct {
	info     *Info
	diags    []diag.Diagnostic
	resolve  Resolver
	nextVar  int
	core     *Module
	universe *Scope
	stdNames map[string]bool
	stdMods  map[string]*Binding
	unions   map[string]*Union
}

type pendingCheck struct {
	t    Type
	kind string // num, concat, ord
	sp   diag.Span
	op   string
}

type checker struct {
	g          *global
	mod        *Module
	src        *diag.Source
	retStack   []Type
	fnName     string
	typeParams map[string]Type
	pending    []pendingCheck
}

// Check type-checks the entry module and everything it uses.
func Check(entry, core *Module, resolve Resolver, stdNames []string) (*Info, []diag.Diagnostic) {
	g := &global{
		info: &Info{
			Types:  map[syntax.Expr]Type{},
			Refs:   map[syntax.Node]*Binding{},
			Defs:   map[syntax.Node]*Binding{},
			Pipes:  map[*syntax.Binary]*syntax.Call{},
			Unions: map[string]*Union{},
			Core:   core,
		},
		resolve:  resolve,
		universe: newScope(nil),
		stdNames: map[string]bool{},
		stdMods:  map[string]*Binding{},
	}
	g.unions = g.info.Unions
	for _, n := range stdNames {
		g.stdNames[n] = true
	}
	g.core = core
	g.checkModule(core)
	for name, b := range core.Exports {
		g.universe.names[name] = b
	}
	g.universe.lookup = func(name string) *Binding {
		if b, ok := g.stdMods[name]; ok {
			return b
		}
		if !g.stdNames[name] {
			return nil
		}
		m := resolve(name, nil)
		if m == nil {
			return nil
		}
		g.checkModule(m)
		b := &Binding{Kind: BModule, Name: name, Module: m}
		g.stdMods[name] = b
		return b
	}
	g.checkModule(entry)
	return g.info, g.diags
}

func (g *global) checkModule(m *Module) {
	if m.state != 0 {
		return
	}
	m.state = 1
	c := &checker{g: g, mod: m, src: m.File.Src}
	parent := g.universe
	if m == g.core {
		parent = newScope(nil)
	}
	m.Scope = newScope(parent)
	c.run()
	m.state = 2
	g.info.Modules = append(g.info.Modules, m)
}

func (c *checker) fresh() *TVar {
	c.g.nextVar++
	return &TVar{ID: c.g.nextVar}
}

func (c *checker) report(sev diag.Severity, sp diag.Span, title, msg, hint string) {
	c.g.diags = append(c.g.diags, diag.Diagnostic{Severity: sev, Title: title, Src: c.src, Span: sp, Message: msg, Hint: hint})
}

func (c *checker) err(sp diag.Span, title, msg, hint string) {
	c.report(diag.Error, sp, title, msg, hint)
}

func (c *checker) errorCount() int { return len(c.g.diags) }

func rigid(name string) *TCon { return &TCon{Name: name, Key: name, Rigid: true} }

func ordinal(i int) string {
	switch i {
	case 1:
		return "1st"
	case 2:
		return "2nd"
	case 3:
		return "3rd"
	}
	return fmt.Sprintf("%dth", i)
}

// ---- module level ----

var builtinTypes = map[string]int{"Int": 0, "Float": 0, "Bool": 0, "String": 0, "Unit": 0, "List": 1, "Dict": 2}

func (c *checker) run() {
	m := c.mod
	f := m.File
	sc := m.Scope
	define := func(name string, sp diag.Span, b *Binding) {
		if old, ok := sc.names[name]; ok && old.Kind != BModule {
			c.err(sp, "DUPLICATE NAME", fmt.Sprintf("There are two top-level definitions called `%s` in this file.", name), "Rename one of them.")
			return
		}
		sc.names[name] = b
	}

	// imports
	for _, it := range f.Items {
		d, ok := it.(*syntax.ImportDecl)
		if !ok {
			continue
		}
		dep := c.g.resolve(d.Name, m)
		if dep == nil {
			c.err(d.NameSp, "MODULE NOT FOUND", fmt.Sprintf("I can't find a module called `%s`.", d.Name),
				fmt.Sprintf("I looked for a file named %s.pln next to %s.", d.Name, c.src.Name))
			continue
		}
		if dep.state == 1 {
			c.err(d.NameSp, "IMPORT CYCLE", fmt.Sprintf("`%s` imports this file, and this file imports `%s`, so neither can come first.", d.Name, d.Name),
				"Move the shared code into a third module that both can import.")
			continue
		}
		c.g.checkModule(dep)
		m.Imports = append(m.Imports, dep)
		sc.names[d.Name] = &Binding{Kind: BModule, Name: d.Name, Module: dep}
	}

	// types: register names first so they can refer to each other
	var unions []*syntax.TypeDecl
	for _, it := range f.Items {
		d, ok := it.(*syntax.TypeDecl)
		if !ok {
			continue
		}
		if _, ok := builtinTypes[d.Name]; ok || (m != c.g.core && (d.Name == "Option" || d.Name == "Result")) {
			c.err(d.NameSp, "DUPLICATE TYPE", fmt.Sprintf("`%s` is a built-in type, so you can't define your own.", d.Name), "Pick a different name.")
			continue
		}
		if _, ok := m.Types[d.Name]; ok {
			c.err(d.NameSp, "DUPLICATE TYPE", fmt.Sprintf("There are two types called `%s` in this file.", d.Name), "")
			continue
		}
		if d.Alias != nil {
			m.Types[d.Name] = &alias{decl: d, params: d.Params}
			continue
		}
		key := m.Name + "." + d.Name
		if m == c.g.core {
			key = d.Name
		}
		u := &Union{Key: key, Name: d.Name, Params: d.Params, Module: m}
		c.g.unions[key] = u
		m.Types[d.Name] = u
		for _, v := range d.Variants {
			ct := &Ctor{Union: u, Name: v.Name}
			u.Ctors = append(u.Ctors, ct)
			define(v.Name, v.Sp, &Binding{Kind: BCtor, Name: v.Name, Ctor: ct, Module: m, Decl: v, Pub: d.Pub})
		}
		unions = append(unions, d)
	}
	for _, d := range unions {
		u, ok := m.Types[d.Name].(*Union)
		if !ok {
			continue
		}
		tp := map[string]Type{}
		var args []Type
		for _, p := range d.Params {
			tp[p] = rigid(p)
			args = append(args, tp[p])
		}
		self := &TCon{Name: u.Name, Key: u.Key, Args: args}
		for i, v := range d.Variants {
			ct := u.Ctors[i]
			for _, fl := range v.Fields {
				ct.Fields = append(ct.Fields, c.resolveType(fl.Type, tp))
				ct.FieldNames = append(ct.FieldNames, fl.Name)
			}
			b := sc.names[v.Name]
			if b == nil || b.Ctor != ct {
				continue
			}
			if len(ct.Fields) == 0 {
				b.Scheme = &Scheme{Params: d.Params, Type: self}
			} else {
				b.Scheme = &Scheme{Params: d.Params, Type: &TFn{Params: ct.Fields, Ret: self, Required: len(ct.Fields)}}
			}
		}
	}
	for _, it := range f.Items {
		if d, ok := it.(*syntax.TypeDecl); ok && d.Alias != nil {
			if al, ok := m.Types[d.Name].(*alias); ok {
				c.resolveAlias(al)
			}
		}
	}

	// function signatures
	for _, it := range f.Items {
		switch d := it.(type) {
		case *syntax.FnDecl:
			sig := c.signature(d.TypeParams, d.Params, d.Ret)
			define(d.Name, d.NameSp, &Binding{Kind: BFn, Name: d.Name, Scheme: &Scheme{Params: d.TypeParams, Type: sig}, Module: m, Decl: d, Pub: d.Pub})
		case *syntax.ExternDecl:
			sig := c.signature(d.TypeParams, d.Params, d.Ret)
			for _, p := range d.Params {
				if p.Default != nil && !strings.HasPrefix(d.Target, "$") {
					c.err(p.Default.Span(), "DEFAULT IN EXTERN", "Extern functions can't have default values.", "")
				}
			}
			define(d.Name, d.NameSp, &Binding{Kind: BExtern, Name: d.Name, Scheme: &Scheme{Params: d.TypeParams, Type: sig}, Module: m, Extern: d, Decl: d, Pub: d.Pub})
		}
	}

	// constants, in order
	for _, it := range f.Items {
		d, ok := it.(*syntax.LetDecl)
		if !ok {
			continue
		}
		c.fnName = ""
		c.typeParams = nil
		var want Type
		if d.Type != nil {
			want = c.resolveType(d.Type, nil)
		}
		t := c.infer(d.Value, sc, want)
		if want != nil {
			c.expectType(d.Value.Span(), t, want, fmt.Sprintf("`%s` is declared as %s", d.Name, newNamer().describe(want)))
			t = want
		}
		c.flushPending()
		define(d.Name, d.NameSp, &Binding{Kind: BConst, Name: d.Name, Type: t, Module: m, Decl: d, Pub: d.Pub})
	}

	// bodies
	for _, it := range f.Items {
		switch d := it.(type) {
		case *syntax.FnDecl:
			if b := sc.names[d.Name]; b != nil && b.Decl == d {
				c.checkFn(d, b)
			}
		case *syntax.TestDecl:
			c.fnName = ""
			c.typeParams = nil
			c.retStack = nil
			c.stmtBlock(d.Body, newScope(sc))
			c.flushPending()
		}
	}

	// exports
	for name, b := range sc.names {
		if b.Pub && b.Kind != BModule {
			m.Exports[name] = b
		}
	}
	if m.Std {
		for name, b := range sc.names {
			if b.Kind != BModule {
				m.Exports[name] = b
			}
		}
	}
}

func (c *checker) signature(typeParams []string, params []*syntax.Param, ret syntax.TypeExpr) *TFn {
	tp := map[string]Type{}
	for _, p := range typeParams {
		if _, ok := builtinTypes[p]; ok {
			c.err(diag.Span{}, "BAD TYPE PARAMETER", fmt.Sprintf("`%s` is a built-in type and can't be a type parameter.", p), "")
		}
		tp[p] = rigid(p)
	}
	ft := &TFn{Required: len(params)}
	seenDefault := false
	seen := map[string]bool{}
	for i, p := range params {
		if seen[p.Name] && p.Name != "_" {
			c.err(p.Sp, "DUPLICATE PARAMETER", fmt.Sprintf("There are two parameters called `%s`.", p.Name), "")
		}
		seen[p.Name] = true
		var t Type = tErr
		if p.Type != nil {
			t = c.resolveType(p.Type, tp)
		}
		ft.Params = append(ft.Params, t)
		if p.Default != nil && !seenDefault {
			seenDefault = true
			ft.Required = i
		} else if p.Default == nil && seenDefault {
			c.err(p.Sp, "DEFAULT ORDER", "Parameters with default values have to come after the ones without.", "Move this parameter before the ones that have defaults.")
		}
	}
	if ret != nil {
		ft.Ret = c.resolveType(ret, tp)
	} else {
		ft.Ret = tUnit
	}
	return ft
}

func (c *checker) checkFn(d *syntax.FnDecl, b *Binding) {
	ft := b.Scheme.Type.(*TFn)
	c.fnName = d.Name
	c.typeParams = map[string]Type{}
	for _, p := range d.TypeParams {
		c.typeParams[p] = rigid(p)
	}
	sc := newScope(c.mod.Scope)
	for i, p := range d.Params {
		if p.Default != nil {
			dt := c.infer(p.Default, sc, ft.Params[i])
			c.expectType(p.Default.Span(), dt, ft.Params[i], fmt.Sprintf("The default for `%s`", p.Name))
		}
		if p.Name != "_" {
			pb := &Binding{Kind: BLocal, Name: p.Name, Type: ft.Params[i], Decl: p}
			sc.names[p.Name] = pb
			c.g.info.Defs[p] = pb
		}
	}
	c.retStack = []Type{ft.Ret}
	if isCon(ft.Ret, "Unit") {
		c.stmtBlock(d.Body, sc)
	} else {
		t := c.inferBlock(d.Body, sc, ft.Ret)
		if !unify(ft.Ret, t) {
			sp := d.Body.Sp
			if v := syntax.BlockValue(d.Body); v != nil {
				sp = v.Span()
			} else {
				sp = diag.Span{Start: d.Body.Sp.End - 1, End: d.Body.Sp.End}
			}
			n := newNamer()
			got := n.describe(t)
			if syntax.BlockValue(d.Body) == nil {
				got = "nothing, because it ends with a statement"
			}
			c.err(sp, "WRONG RETURN TYPE", fmt.Sprintf("`%s` says it returns %s, but its last expression gives %s.", d.Name, n.describe(ft.Ret), got),
				c.mismatchHint(t, ft.Ret, n))
		}
	}
	c.retStack = nil
	c.flushPending()
}

func (c *checker) flushPending() {
	for _, p := range c.pending {
		t := Prune(p.t)
		if _, ok := t.(*TVar); ok {
			switch p.kind {
			case "concat":
				unify(t, tString)
			default:
				unify(t, tInt)
			}
			continue
		}
		if _, ok := t.(*TErr); ok {
			continue
		}
		okType := false
		switch p.kind {
		case "num":
			okType = isCon(t, "Int") || isCon(t, "Float")
		case "ord":
			okType = isCon(t, "Int") || isCon(t, "Float") || isCon(t, "String")
		case "concat":
			okType = isCon(t, "String") || isCon(t, "List")
		}
		if !okType {
			c.err(p.sp, "WRONG TYPE FOR OPERATOR", fmt.Sprintf("`%s` can't be used on %s.", p.op, newNamer().describe(t)), "")
		}
	}
	c.pending = nil
}

// ---- types written in code ----

var typeHints = map[string]string{
	"int": "Int", "integer": "Int", "Integer": "Int", "float": "Float", "double": "Float", "Double": "Float", "Number": "Int or Float",
	"number": "Int or Float", "string": "String", "str": "String", "Str": "String", "bool": "Bool", "boolean": "Bool", "Boolean": "Bool",
	"list": "List[T]", "Array": "List[T]", "array": "List[T]", "Vec": "List[T]", "Map": "Dict[K, V]", "HashMap": "Dict[K, V]",
	"dict": "Dict[K, V]", "Maybe": "Option[T]", "Optional": "Option[T]", "Either": "Result[T, E]", "void": "Unit", "Void": "Unit",
}

func (c *checker) resolveType(te syntax.TypeExpr, tp map[string]Type) Type {
	switch t := te.(type) {
	case *syntax.FnType:
		ft := &TFn{}
		for _, p := range t.Params {
			ft.Params = append(ft.Params, c.resolveType(p, tp))
		}
		ft.Required = len(ft.Params)
		if t.Ret != nil {
			ft.Ret = c.resolveType(t.Ret, tp)
		} else {
			ft.Ret = tUnit
		}
		return ft
	case *syntax.RecordType:
		var names []string
		var ts []Type
		seen := map[string]bool{}
		for _, f := range t.Fields {
			if seen[f.Name] {
				c.err(t.Sp, "DUPLICATE FIELD", fmt.Sprintf("This record type has two fields called `%s`.", f.Name), "")
				continue
			}
			seen[f.Name] = true
			names = append(names, f.Name)
			ts = append(ts, c.resolveType(f.Type, tp))
		}
		return newRecord(names, ts)
	case *syntax.NamedType:
		var args []Type
		for _, a := range t.Args {
			args = append(args, c.resolveType(a, tp))
		}
		arity := func(want int) bool {
			if len(args) == want {
				return true
			}
			if want == 0 {
				c.err(t.Sp, "TOO MANY TYPE ARGUMENTS", fmt.Sprintf("`%s` doesn't take type arguments.", t.Name), "")
			} else {
				c.err(t.Sp, "WRONG NUMBER OF TYPE ARGUMENTS", fmt.Sprintf("`%s` needs %d type argument(s), but here it has %d.", t.Name, want, len(args)),
					map[string]string{"List": "For example List[Int].", "Dict": "For example Dict[String, Int].", "Option": "For example Option[String].", "Result": "For example Result[Int, String]."}[t.Name])
			}
			return false
		}
		var entry any
		if t.Module != "" {
			b := c.mod.Scope.find(t.Module)
			if b == nil || b.Kind != BModule {
				c.err(t.Sp, "MODULE NOT FOUND", fmt.Sprintf("I can't find a module called `%s`.", t.Module), "Add an import line at the top of the file, like: import "+t.Module)
				return tErr
			}
			e, ok := b.Module.Types[t.Name]
			if !ok || !typePub(e) && !b.Module.Std {
				c.err(t.Sp, "TYPE NOT FOUND", fmt.Sprintf("Module `%s` has no public type called `%s`.", t.Module, t.Name), "")
				return tErr
			}
			entry = e
		} else {
			if r, ok := tp[t.Name]; ok {
				arity(0)
				return r
			}
			if n, ok := builtinTypes[t.Name]; ok {
				if !arity(n) {
					return tErr
				}
				return &TCon{Name: t.Name, Key: t.Name, Args: args}
			}
			if e, ok := c.mod.Types[t.Name]; ok {
				entry = e
			} else if e, ok := c.g.core.Types[t.Name]; ok {
				entry = e
			}
		}
		switch e := entry.(type) {
		case *Union:
			if !arity(len(e.Params)) {
				return tErr
			}
			return &TCon{Name: e.Name, Key: e.Key, Args: args}
		case *alias:
			if !arity(len(e.params)) {
				return tErr
			}
			body := c.resolveAlias(e)
			m := map[string]Type{}
			for i, p := range e.params {
				m[p] = args[i]
			}
			return subst(body, m)
		}
		hint := ""
		if h, ok := typeHints[t.Name]; ok {
			hint = fmt.Sprintf("Did you mean %s?", h)
		} else if t.Name == "Any" || t.Name == "any" || t.Name == "Object" || t.Name == "object" || t.Name == "Dynamic" {
			hint = "Plain has no catch-all type. Make a type with a variant for each kind of value you need."
		} else if t.Name == "Null" || t.Name == "None" || t.Name == "Nil" {
			hint = "Plain has no null type. A value that might be missing is an Option[T]."
		} else {
			var names []string
			for n := range builtinTypes {
				names = append(names, n)
			}
			names = append(names, "Option", "Result")
			for n := range c.mod.Types {
				names = append(names, n)
			}
			for n := range tp {
				names = append(names, n)
			}
			if s := closest(t.Name, names); s != "" {
				hint = fmt.Sprintf("Did you mean `%s`?", s)
			}
		}
		c.err(t.Sp, "UNKNOWN TYPE", fmt.Sprintf("I don't know a type called `%s`.", t.Name), hint)
		return tErr
	}
	return tErr
}

func typePub(e any) bool {
	switch e := e.(type) {
	case *Union:
		for _, it := range e.Module.File.Items {
			if d, ok := it.(*syntax.TypeDecl); ok && d.Name == e.Name {
				return d.Pub
			}
		}
	case *alias:
		return e.decl.Pub
	}
	return false
}

func (c *checker) resolveAlias(al *alias) Type {
	if al.body != nil {
		return al.body
	}
	if al.resolving {
		c.err(al.decl.NameSp, "RECURSIVE RECORD", fmt.Sprintf("The record type `%s` contains itself, so it would be infinitely big.", al.decl.Name),
			"Use a type with variants instead, so there is a way to stop, like: type Tree = Leaf | Node(Tree, Int, Tree)")
		al.body = tErr
		return tErr
	}
	al.resolving = true
	tp := map[string]Type{}
	for _, p := range al.params {
		tp[p] = rigid(p)
	}
	b := c.resolveType(al.decl.Alias, tp)
	al.resolving = false
	if al.body == nil {
		al.body = b
	}
	return al.body
}

// ---- helpers for errors ----

func closest(name string, options []string) string {
	best, bestD := "", 3
	if len(name) <= 3 {
		bestD = 2
	}
	sort.Strings(options)
	for _, o := range options {
		if o == name || strings.HasPrefix(o, "$") {
			continue
		}
		d := levenshtein(strings.ToLower(name), strings.ToLower(o))
		if d < bestD {
			best, bestD = o, d
		}
	}
	return best
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

// mismatchHint explains the common mistakes behind a type mismatch.
func (c *checker) mismatchHint(got, want Type, n *namer) string {
	g, w := Prune(got), Prune(want)
	if isCon(g, "Option") {
		if gc := g.(*TCon); unify(copyType(gc.Args[0]), copyType(w)) {
			return "This value might be missing. Use match to handle both cases:\n\n    match value {\n      Some(x) -> ...\n      None -> ...\n    }\n\nor give a fallback with option.unwrap_or(value, fallback)."
		}
	}
	if isCon(g, "Result") {
		if gc := g.(*TCon); unify(copyType(gc.Args[0]), copyType(w)) {
			return "This can fail. Add ? to pass the error up to the caller, or use match to handle Ok and Err here."
		}
	}
	if isCon(w, "Option") {
		if wc := w.(*TCon); unify(copyType(wc.Args[0]), copyType(g)) {
			return "Wrap the value in Some(...) to make it an Option."
		}
	}
	if isCon(w, "Result") {
		if wc := w.(*TCon); unify(copyType(wc.Args[0]), copyType(g)) {
			return "Wrap the value in Ok(...) to make it a Result."
		}
	}
	switch {
	case isCon(g, "Int") && isCon(w, "Float"):
		return "Plain never turns an Int into a Float by itself. Use to_float(n), or write the number as 1.0."
	case isCon(g, "Float") && isCon(w, "Int"):
		return "Plain never drops the decimals by itself. Use math.round(x), math.floor(x) or math.ceil(x)."
	case (isCon(g, "Int") || isCon(g, "Float") || isCon(g, "Bool")) && isCon(w, "String"):
		return "Turn it into text with show(value), or put it inside text like \"{value}\"."
	case isCon(g, "String") && isCon(w, "Int"):
		return "To read a number from text use parse_int(text). It gives a Result, because the text might not be a number."
	case isCon(g, "String") && isCon(w, "Float"):
		return "To read a number from text use parse_float(text). It gives a Result, because the text might not be a number."
	case isCon(g, "Unit"):
		return "This expression doesn't produce a value."
	}
	if gf, ok := g.(*TFn); ok {
		if _, ok := w.(*TFn); !ok && unify(copyType(gf.Ret), copyType(w)) {
			return "This is a function. Did you forget to call it with ()?"
		}
	}
	return ""
}

// copyType copies a type with fresh variables so trial unification changes nothing.
func copyType(t Type) Type {
	m := map[*TVar]*TVar{}
	var cp func(Type) Type
	cp = func(t Type) Type {
		switch t := Prune(t).(type) {
		case *TVar:
			if n, ok := m[t]; ok {
				return n
			}
			n := &TVar{ID: -t.ID}
			m[t] = n
			return n
		case *TCon:
			args := make([]Type, len(t.Args))
			for i, a := range t.Args {
				args[i] = cp(a)
			}
			return &TCon{Name: t.Name, Key: t.Key, Args: args, Rigid: t.Rigid}
		case *TFn:
			ps := make([]Type, len(t.Params))
			for i, p := range t.Params {
				ps[i] = cp(p)
			}
			return &TFn{Params: ps, Ret: cp(t.Ret), Required: t.Required}
		case *TRecord:
			ts := make([]Type, len(t.Types))
			for i, f := range t.Types {
				ts[i] = cp(f)
			}
			return &TRecord{Names: t.Names, Types: ts}
		}
		return t
	}
	return cp(t)
}

// expectType unifies and reports "<place> needs X, but this is Y".
func (c *checker) expectType(sp diag.Span, got, want Type, place string) bool {
	if unify(want, got) {
		return true
	}
	n := newNamer()
	c.err(sp, "TYPE MISMATCH", fmt.Sprintf("%s, but this is %s.", place, n.describe(got)), c.mismatchHint(got, want, n))
	return false
}

func (c *checker) expectBool(e syntax.Expr, t Type, place string) {
	if unify(t, tBool) {
		return
	}
	n := newNamer()
	c.err(e.Span(), "NOT A BOOL", fmt.Sprintf("%s has to be a Bool (true or false), but this is %s.", place, n.describe(t)),
		"Plain has no \"truthy\" values: 0, \"\" and [] don't count as false. Write the check you mean, like n != 0 or list.is_empty(xs).")
}

func (c *checker) instantiate(s *Scheme) Type {
	if len(s.Params) == 0 {
		return s.Type
	}
	m := map[string]Type{}
	for _, p := range s.Params {
		m[p] = c.fresh()
	}
	return subst(s.Type, m)
}

func (c *checker) instantiateCtor(ct *Ctor) (Type, []Type) {
	u := ct.Union
	m := map[string]Type{}
	var args []Type
	for _, p := range u.Params {
		v := c.fresh()
		m[p] = v
		args = append(args, v)
	}
	fields := make([]Type, len(ct.Fields))
	for i, f := range ct.Fields {
		fields[i] = subst(f, m)
	}
	return &TCon{Name: u.Name, Key: u.Key, Args: args}, fields
}

var nameHints = map[string]string{
	"self":     "Plain has no `self` or `this`. Pass the value in as a parameter.",
	"this":     "Plain has no `self` or `this`. Pass the value in as a parameter.",
	"len":      "Use list.len(xs) for lists or string.len(s) for text.",
	"length":   "Use list.len(xs) for lists or string.len(s) for text.",
	"println":  "Use print(value).",
	"printf":   "Use print with text interpolation, like print(\"x is {x}\").",
	"console":  "Use print(value).",
	"str":      "Use show(value) to turn a value into text.",
	"toString": "Use show(value) to turn a value into text.",
	"int":      "To read a whole number from text use parse_int(text). To drop decimals use math.round(x).",
	"float":    "Use to_float(n) for an Int, or parse_float(text) for text.",
	"range":    "Use list.range(from, to), which includes both ends.",
	"map":      "Use list.map(xs, f), or xs |> list.map(f).",
	"filter":   "Use list.filter(xs, f), or xs |> list.filter(f).",
	"error":    "To report a failure, return Err(\"message\") from a function that returns a Result.",
	"throw":    "Plain has no exceptions. Return Err(\"message\") from a function that returns a Result.",
	"raise":    "Plain has no exceptions. Return Err(\"message\") from a function that returns a Result.",
}

func (c *checker) unknownName(sp diag.Span, name string, sc *Scope) {
	hint := nameHints[name]
	if hint == "" {
		opts := sc.all()
		for n := range c.g.stdNames {
			opts = append(opts, n)
		}
		if s := closest(name, opts); s != "" {
			hint = fmt.Sprintf("Did you mean `%s`?", s)
		}
	}
	c.err(sp, "NAME NOT FOUND", fmt.Sprintf("I can't find anything called `%s`.", name), hint)
}

func calleeName(e syntax.Expr) string {
	switch e := e.(type) {
	case *syntax.Ident:
		return "`" + e.Name + "`"
	case *syntax.Field:
		if id, ok := e.X.(*syntax.Ident); ok {
			return "`" + id.Name + "." + e.Name + "`"
		}
		return "`" + e.Name + "`"
	case *syntax.CtorRef:
		if e.Module != "" {
			return "`" + e.Module + "." + e.Name + "`"
		}
		return "`" + e.Name + "`"
	}
	return "this function"
}

// ---- expressions ----

func (c *checker) infer(e syntax.Expr, sc *Scope, want Type) Type {
	t := c.infer1(e, sc, want)
	c.g.info.Types[e] = t
	return t
}

const maxSafeInt = 9007199254740991

func (c *checker) infer1(e syntax.Expr, sc *Scope, want Type) Type {
	switch n := e.(type) {
	case *syntax.IntLit:
		v, err := strconv.ParseInt(strings.ReplaceAll(n.Text, "_", ""), 10, 64)
		if err != nil || v > maxSafeInt {
			c.err(n.Sp, "NUMBER TOO BIG", "This number doesn't fit in an Int. Ints go up to 9007199254740991, so they work the same in every JavaScript runtime.",
				"If you don't need every digit exact, write it as a Float by adding .0")
			return tErr
		}
		return tInt
	case *syntax.FloatLit:
		return tFloat
	case *syntax.BoolLit:
		return tBool
	case *syntax.StringLit:
		for _, p := range n.Parts {
			if p.X == nil {
				continue
			}
			t := Prune(c.infer(p.X, sc, nil))
			if _, ok := t.(*TFn); ok {
				c.err(p.X.Span(), "FUNCTION IN TEXT", "This is a function, so it can't be put into text.", "Did you forget to call it with ()?")
			}
		}
		return tString
	case *syntax.Paren:
		return c.infer(n.X, sc, want)
	case *syntax.Ident:
		b := sc.find(n.Name)
		if b == nil {
			c.unknownName(n.Sp, n.Name, sc)
			return tErr
		}
		c.g.info.Refs[n] = b
		return c.bindingType(b, n.Sp)
	case *syntax.CtorRef:
		b := c.lookupCtor(n.Module, n.Name, n.Sp, sc)
		if b == nil {
			return tErr
		}
		c.g.info.Refs[n] = b
		return c.instantiate(b.Scheme)
	case *syntax.Field:
		return c.inferField(n, sc)
	case *syntax.Call:
		return c.inferCall(n, sc)
	case *syntax.Binary:
		return c.inferBinary(n, sc, want)
	case *syntax.Unary:
		t := c.infer(n.X, sc, nil)
		if n.Op == "!" {
			c.expectBool(n.X, t, "The value after !")
			return tBool
		}
		pt := Prune(t)
		switch {
		case isCon(pt, "Int"), isCon(pt, "Float"):
		case isVar(pt):
			c.pending = append(c.pending, pendingCheck{t: pt, kind: "num", sp: n.Sp, op: "-"})
		case isErr(pt):
		default:
			c.err(n.X.Span(), "NOT A NUMBER", fmt.Sprintf("A minus sign needs a number, but this is %s.", newNamer().describe(pt)), "")
			return tErr
		}
		return t
	case *syntax.If:
		c.expectBool(n.Cond, c.infer(n.Cond, sc, nil), "The condition of an if")
		if n.Else == nil {
			c.stmtBlock(n.Then, sc)
			return tUnit
		}
		tt := c.inferBlock(n.Then, sc, want)
		et := c.infer(n.Else, sc, tt)
		if !unify(tt, et) {
			sp := n.Else.Span()
			if b, ok := n.Else.(*syntax.Block); ok {
				if v := syntax.BlockValue(b); v != nil {
					sp = v.Span()
				}
			}
			nm := newNamer()
			c.err(sp, "IF BRANCHES DIFFER", fmt.Sprintf("The first branch of this if gives %s, but the else branch gives %s.", nm.describe(tt), nm.describe(et)),
				"When an if produces a value, every branch has to give the same type.")
			return tErr
		}
		return tt
	case *syntax.Match:
		return c.inferMatch(n, sc, want, false)
	case *syntax.Block:
		return c.inferBlock(n, sc, want)
	case *syntax.Lambda:
		return c.inferLambda(n, sc, want)
	case *syntax.RecordLit:
		return c.inferRecord(n, sc, want)
	case *syntax.ListLit:
		elem := Type(c.fresh())
		if w, ok := Prune(want).(*TCon); ok && w.Key == "List" && !w.Rigid {
			unify(elem, w.Args[0])
		}
		for i, x := range n.Elems {
			t := c.infer(x, sc, elem)
			if !unify(elem, t) {
				nm := newNamer()
				first := "The list"
				if i > 0 {
					first = "The first item of this list is " + nm.str(elem) + ", but this"
				}
				c.err(x.Span(), "LIST ITEMS DIFFER", fmt.Sprintf("%s item is %s.", first, nm.describe(t)),
					"All items in a list have the same type. To mix kinds of values, make a type with one variant per kind.")
			}
		}
		return listOf(elem)
	case *syntax.Try:
		return c.inferTry(n, sc)
	}
	return tErr
}

func isVar(t Type) bool { _, ok := Prune(t).(*TVar); return ok }
func isErr(t Type) bool { _, ok := Prune(t).(*TErr); return ok }

func (c *checker) bindingType(b *Binding, sp diag.Span) Type {
	switch b.Kind {
	case BLocal, BConst:
		return b.Type
	case BFn, BExtern, BCtor:
		return c.instantiate(b.Scheme)
	case BModule:
		c.err(sp, "MODULE USED AS A VALUE", fmt.Sprintf("`%s` is a module, not a value.", b.Name), fmt.Sprintf("Use one of its functions, like %s.something(...).", b.Name))
	}
	return tErr
}

func (c *checker) lookupCtor(module, name string, sp diag.Span, sc *Scope) *Binding {
	if module != "" {
		mb := sc.find(module)
		if mb == nil || mb.Kind != BModule {
			c.err(sp, "MODULE NOT FOUND", fmt.Sprintf("I can't find a module called `%s`.", module), "Add an import line at the top of the file, like: import "+module)
			return nil
		}
		b, ok := mb.Module.Exports[name]
		if !ok || b.Kind != BCtor {
			c.err(sp, "CONSTRUCTOR NOT FOUND", fmt.Sprintf("Module `%s` has no public constructor called `%s`.", module, name), "")
			return nil
		}
		return b
	}
	b := sc.find(name)
	if b != nil && b.Kind == BCtor {
		return b
	}
	hint := ""
	if e, ok := c.mod.Types[name]; ok {
		if u, ok := e.(*Union); ok {
			var names []string
			for _, ct := range u.Ctors {
				names = append(names, ct.Name)
			}
			hint = fmt.Sprintf("`%s` is a type. To make a value of it, use one of its variants: %s.", name, strings.Join(names, ", "))
		} else {
			hint = fmt.Sprintf("`%s` is a record type. Make one with braces, like {field: value}.", name)
		}
	} else if name == "True" || name == "False" {
		hint = fmt.Sprintf("Write %s in lowercase: %s.", name, strings.ToLower(name))
	} else if name == "Null" || name == "Nil" || name == "Undefined" {
		hint = "Plain has no null. Use None for \"no value\", and Some(x) when there is one."
	} else if _, ok := builtinTypes[name]; ok {
		hint = fmt.Sprintf("`%s` is a type, not a function. To convert values use show, to_float, parse_int or parse_float.", name)
	} else {
		var opts []string
		for _, n := range sc.all() {
			if b := sc.find(n); b != nil && b.Kind == BCtor {
				opts = append(opts, n)
			}
		}
		if s := closest(name, opts); s != "" {
			hint = fmt.Sprintf("Did you mean `%s`?", s)
		}
	}
	c.err(sp, "CONSTRUCTOR NOT FOUND", fmt.Sprintf("I can't find a constructor called `%s`.", name), hint)
	return nil
}

func (c *checker) inferField(n *syntax.Field, sc *Scope) Type {
	if id, ok := n.X.(*syntax.Ident); ok {
		if b := sc.find(id.Name); b != nil && b.Kind == BModule {
			mb, ok := b.Module.Exports[n.Name]
			if !ok {
				var opts []string
				for k := range b.Module.Exports {
					opts = append(opts, k)
				}
				hint := ""
				if s := closest(n.Name, opts); s != "" {
					hint = fmt.Sprintf("Did you mean `%s.%s`?", id.Name, s)
				} else if _, private := b.Module.Scope.names[n.Name]; private {
					hint = fmt.Sprintf("`%s` exists but isn't marked pub in %s.", n.Name, id.Name)
				}
				c.err(n.NameSp, "NOT IN MODULE", fmt.Sprintf("Module `%s` has nothing called `%s`.", id.Name, n.Name), hint)
				return tErr
			}
			c.g.info.Refs[n] = mb
			return c.bindingType(mb, n.Sp)
		}
	}
	xt := Prune(c.infer(n.X, sc, nil))
	nm := newNamer()
	switch r := xt.(type) {
	case *TErr:
		return tErr
	case *TRecord:
		if t, ok := r.field(n.Name); ok {
			return t
		}
		hint := ""
		if s := closest(n.Name, r.Names); s != "" {
			hint = fmt.Sprintf("Did you mean `%s`?", s)
		}
		c.err(n.NameSp, "NO SUCH FIELD", fmt.Sprintf("This record has no field `%s`. Its fields are: %s.", n.Name, strings.Join(r.Names, ", ")), hint)
		return tErr
	case *TVar:
		c.err(n.Sp, "UNKNOWN TYPE", fmt.Sprintf("I can't tell what kind of value this is yet, so I don't know whether it has a field `%s`.", n.Name),
			"Add a type where the value comes in, like fn(user: User) ... or fn(p: {name: String}) ...")
		return tErr
	case *TCon:
		if (r.Key == "Option" || r.Key == "Result") && !r.Rigid {
			if inner, ok := Prune(r.Args[0]).(*TRecord); ok {
				if _, has := inner.field(n.Name); has {
					what := "might be missing"
					if r.Key == "Result" {
						what = "might be an error"
					}
					c.err(n.Sp, "MAYBE MISSING", fmt.Sprintf("This value %s (it is %s), so I can't read `.%s` from it yet.\n\nIn JavaScript this is where \"Cannot read properties of undefined\" comes from. Plain makes you deal with the missing case first.", what, nm.describe(r), n.Name),
						"Use match to get the value out:\n\n    match value {\n      "+map[string]string{"Option": "Some(v) -> v." + n.Name + "\n      None -> ...", "Result": "Ok(v) -> v." + n.Name + "\n      Err(e) -> ..."}[r.Key]+"\n    }")
					return tErr
				}
			}
		}
		hint := ""
		switch r.Key {
		case "String":
			hint = "Functions for text live in the string module, like string.len(s) or s |> string.upper."
		case "List":
			hint = "Functions for lists live in the list module, like list.len(xs) or xs |> list.map(f)."
		case "Dict":
			hint = "Functions for dicts live in the dict module, like dict.get(d, key)."
		}
		c.err(n.NameSp, "NO FIELDS", fmt.Sprintf("This is %s, which has no fields, so `.%s` doesn't work.", nm.describe(r), n.Name), hint)
		return tErr
	}
	c.err(n.NameSp, "NO FIELDS", fmt.Sprintf("This is %s, which has no fields.", nm.describe(xt)), "")
	return tErr
}

func (c *checker) inferCall(n *syntax.Call, sc *Scope) Type {
	ft := Prune(c.infer(n.Fn, sc, nil))
	var f *TFn
	switch t := ft.(type) {
	case *TErr:
		for _, a := range n.Args {
			c.infer(a, sc, nil)
		}
		return tErr
	case *TVar:
		f = &TFn{Ret: c.fresh(), Required: len(n.Args)}
		for range n.Args {
			f.Params = append(f.Params, c.fresh())
		}
		unify(t, f)
	case *TFn:
		f = t
	default:
		c.err(n.Fn.Span(), "NOT A FUNCTION", fmt.Sprintf("This is called like a function, but it is %s.", newNamer().describe(t)), "")
		for _, a := range n.Args {
			c.infer(a, sc, nil)
		}
		return tErr
	}
	name := calleeName(n.Fn)
	if len(n.Args) < f.Required || len(n.Args) > len(f.Params) {
		want := fmt.Sprint(len(f.Params))
		if f.Required < len(f.Params) {
			want = fmt.Sprintf("%d to %d", f.Required, len(f.Params))
		}
		plural := "s"
		if want == "1" {
			plural = ""
		}
		c.err(n.Sp, "WRONG NUMBER OF ARGUMENTS", fmt.Sprintf("%s takes %s argument%s, but here it gets %d.", upperFirst(name), want, plural, len(n.Args)),
			fmt.Sprintf("Its type is %s", TypeString(f)))
	}
	for i, a := range n.Args {
		if i >= len(f.Params) {
			c.infer(a, sc, nil)
			continue
		}
		at := c.infer(a, sc, f.Params[i])
		if !unify(f.Params[i], at) {
			nm := newNamer()
			c.err(a.Span(), "TYPE MISMATCH", fmt.Sprintf("The %s argument to %s should be %s, but it is %s.", ordinal(i+1), name, nm.describe(f.Params[i]), nm.describe(at)),
				c.mismatchHint(at, f.Params[i], nm))
		}
	}
	return f.Ret
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	if s[0] == '`' {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func isNum(t Type) bool { return isCon(t, "Int") || isCon(t, "Float") }

func (c *checker) inferBinary(n *syntax.Binary, sc *Scope, want Type) Type {
	switch n.Op {
	case "|>":
		call := &syntax.Call{Sp: n.Sp}
		r := n.R
		for {
			p, ok := r.(*syntax.Paren)
			if !ok {
				break
			}
			r = p.X
		}
		if rc, ok := r.(*syntax.Call); ok {
			call.Fn = rc.Fn
			call.Args = append([]syntax.Expr{n.L}, rc.Args...)
		} else {
			call.Fn = n.R
			call.Args = []syntax.Expr{n.L}
		}
		c.g.info.Pipes[n] = call
		return c.infer(call, sc, want)
	case "&&", "||":
		c.expectBool(n.L, c.infer(n.L, sc, nil), "The left side of "+n.Op)
		c.expectBool(n.R, c.infer(n.R, sc, nil), "The right side of "+n.Op)
		return tBool
	}
	lt := c.infer(n.L, sc, nil)
	rt := c.infer(n.R, sc, lt)
	return c.binaryTypes(n.Op, n.Sp, n.OpSp, lt, rt)
}

// binaryTypes checks an arithmetic, text or comparison operator.
func (c *checker) binaryTypes(op string, sp, opSp diag.Span, lt, rt Type) Type {
	a, b := Prune(lt), Prune(rt)
	if isErr(a) || isErr(b) {
		switch op {
		case "==", "!=", "<", "<=", ">", ">=":
			return tBool
		}
		return tErr
	}
	nm := newNamer()
	switch op {
	case "==", "!=":
		if !unify(a, b) {
			c.err(sp, "CAN'T COMPARE DIFFERENT TYPES", fmt.Sprintf("This compares %s with %s.\n\nIn JavaScript 1 == \"1\" is true, and in Python 1 == \"1\" is quietly False. Values of different types can never be equal, so Plain treats this as a mistake.", nm.describe(a), nm.describe(b)),
				c.mismatchHint(b, a, nm))
			return tBool
		}
		if _, ok := Prune(a).(*TFn); ok {
			c.err(sp, "CAN'T COMPARE FUNCTIONS", "Functions can't be compared with ==.", "Compare the results of calling them instead.")
		}
		return tBool
	case "<", "<=", ">", ">=":
		if !unify(a, b) {
			hint := c.mismatchHint(b, a, nm)
			if isNum(a) && isNum(b) {
				hint = "Plain never mixes Int and Float by itself. Use to_float(n) on the Int side."
			}
			c.err(sp, "CAN'T COMPARE DIFFERENT TYPES", fmt.Sprintf("`%s` compares %s with %s.", op, nm.describe(a), nm.describe(b)), hint)
			return tBool
		}
		t := Prune(a)
		switch {
		case isNum(t), isCon(t, "String"):
		case isVar(t):
			c.pending = append(c.pending, pendingCheck{t: t, kind: "ord", sp: sp, op: op})
		default:
			c.err(opSp, "CAN'T ORDER", fmt.Sprintf("`%s` works on numbers and text, but these are %s.", op, nm.str(t)+" values"), "")
		}
		return tBool
	case "++":
		if !unify(a, b) {
			hint := ""
			if isCon(a, "String") || isCon(b, "String") {
				hint = "Turn the other value into text first with show(value), or use interpolation: \"{a}{b}\"."
			}
			c.err(sp, "CAN'T JOIN DIFFERENT TYPES", fmt.Sprintf("`++` joins two values of the same kind, but here it has %s and %s.", nm.describe(a), nm.describe(b)), hint)
			return tErr
		}
		t := Prune(a)
		switch {
		case isCon(t, "String"), isCon(t, "List"):
		case isVar(t):
			c.pending = append(c.pending, pendingCheck{t: t, kind: "concat", sp: sp, op: op})
		case isNum(t):
			c.err(opSp, "NOT TEXT", "`++` joins text or lists. These are numbers.", "To add numbers use +. To put a number into text use show(n) or \"{n}\".")
			return tErr
		default:
			c.err(opSp, "CAN'T JOIN", fmt.Sprintf("`++` joins text or lists, but these are %s values.", nm.str(t)), "")
			return tErr
		}
		return t
	}
	// + - * / %
	switch {
	case isVar(a) && isVar(b):
		unify(a, b)
		c.pending = append(c.pending, pendingCheck{t: a, kind: "num", sp: sp, op: op})
		if op == "/" {
			return tFloat
		}
		return a
	case isVar(a) && isNum(b):
		unify(a, b)
		a = b
	case isVar(b) && isNum(a):
		unify(b, a)
		b = a
	}
	if isNum(a) && isNum(b) {
		ac, bc := a.(*TCon), b.(*TCon)
		if ac.Key == bc.Key {
			if op == "/" {
				return tFloat
			}
			return a
		}
		c.err(sp, "CAN'T MIX Int AND Float", fmt.Sprintf("`%s` has %s on one side and %s on the other.\n\nPlain never converts numbers behind your back, because that is where rounding and precision bugs hide.", op, nm.describe(a), nm.describe(b)),
			"Turn the Int into a Float with to_float(n), or the Float into an Int with math.round(x). Remember that / always gives a Float; div(a, b) divides whole numbers.")
		return tErr
	}
	if op == "+" && (isCon(a, "String") || isCon(b, "String")) {
		c.err(sp, "CAN'T ADD TEXT AND NUMBERS", fmt.Sprintf("You're using + with %s and %s.\n\nIn JavaScript \"1\" + 1 quietly becomes \"11\", and in Python it crashes when the line runs. Plain stops it here instead.", nm.describe(a), nm.describe(b)),
			"Join text with ++, and turn numbers into text first:\n\n    \"Total: \" ++ show(n)\n    \"Total: {n}\"")
		return tErr
	}
	if op == "+" && (isCon(a, "List") || isCon(b, "List")) {
		c.err(sp, "CAN'T ADD LISTS", "`+` is for numbers. Lists are joined with ++.", "For example: [1, 2] ++ [3]")
		return tErr
	}
	bad := a
	if isNum(a) {
		bad = b
	}
	hint := ""
	if isCon(bad, "Bool") {
		hint = "Plain doesn't treat true and false as numbers."
	} else if isCon(bad, "Option") {
		hint = c.mismatchHint(bad, tInt, nm)
	} else if isCon(bad, "Result") {
		hint = "This can fail. Add ? to pass the error up, or use match to handle Ok and Err."
	}
	c.err(sp, "NOT A NUMBER", fmt.Sprintf("`%s` works on two Ints or two Floats, but here it has %s and %s.", op, nm.describe(a), nm.describe(b)), hint)
	return tErr
}

func (c *checker) inferLambda(n *syntax.Lambda, sc *Scope, want Type) Type {
	var wantFn *TFn
	if f, ok := Prune(want).(*TFn); ok && len(f.Params) == len(n.Params) {
		wantFn = f
	}
	lsc := newScope(sc)
	var params []Type
	for i, p := range n.Params {
		var pt Type
		switch {
		case p.Type != nil:
			pt = c.resolveType(p.Type, c.typeParams)
		case wantFn != nil:
			pt = wantFn.Params[i]
		default:
			pt = c.fresh()
		}
		if p.Default != nil {
			c.err(p.Default.Span(), "DEFAULT IN FUNCTION VALUE", "Function values can't have default arguments.", "Move this into a top-level fn if you need defaults.")
		}
		params = append(params, pt)
		if p.Name != "_" {
			b := &Binding{Kind: BLocal, Name: p.Name, Type: pt, Decl: p}
			lsc.names[p.Name] = b
			c.g.info.Defs[p] = b
		}
	}
	var ret Type = c.fresh()
	if n.Ret != nil {
		ret = c.resolveType(n.Ret, c.typeParams)
	} else if wantFn != nil {
		unify(ret, wantFn.Ret)
	}
	c.retStack = append(c.retStack, ret)
	var bt Type
	if b, ok := n.Body.(*syntax.Block); ok && isCon(ret, "Unit") {
		c.stmtBlock(b, lsc)
		c.g.info.Types[b] = tUnit
		bt = tUnit
	} else {
		bt = c.infer(n.Body, lsc, ret)
	}
	c.retStack = c.retStack[:len(c.retStack)-1]
	if !unify(ret, bt) {
		nm := newNamer()
		c.err(n.Body.Span(), "TYPE MISMATCH", fmt.Sprintf("This function should give %s, but its body gives %s.", nm.describe(ret), nm.describe(bt)), c.mismatchHint(bt, ret, nm))
	}
	return &TFn{Params: params, Ret: ret, Required: len(params)}
}

func (c *checker) inferRecord(n *syntax.RecordLit, sc *Scope, want Type) Type {
	nm := newNamer()
	if n.Base != nil {
		bt := Prune(c.infer(n.Base, sc, want))
		rec, ok := bt.(*TRecord)
		if !ok {
			if !isErr(bt) {
				msg := fmt.Sprintf("`..` copies a record, but this is %s.", nm.describe(bt))
				if isVar(bt) {
					msg = "I can't tell what kind of record this is yet."
				}
				c.err(n.Base.Span(), "NOT A RECORD", msg, "")
			}
			for _, f := range n.Fields {
				c.infer(f.Value, sc, nil)
			}
			return tErr
		}
		for _, f := range n.Fields {
			ft, ok := rec.field(f.Name)
			if !ok {
				hint := ""
				if s := closest(f.Name, rec.Names); s != "" {
					hint = fmt.Sprintf("Did you mean `%s`?", s)
				}
				c.err(f.NameSp, "NO SUCH FIELD", fmt.Sprintf("The record being copied has no field `%s`. Its fields are: %s.", f.Name, strings.Join(rec.Names, ", ")), hint)
				c.infer(f.Value, sc, nil)
				continue
			}
			vt := c.infer(f.Value, sc, ft)
			c.expectType(f.Value.Span(), vt, ft, fmt.Sprintf("The field `%s` is %s", f.Name, nm.describe(ft)))
		}
		return rec
	}
	var wantRec *TRecord
	if r, ok := Prune(want).(*TRecord); ok {
		wantRec = r
	}
	var names []string
	var ts []Type
	seen := map[string]bool{}
	for _, f := range n.Fields {
		if seen[f.Name] {
			c.err(f.NameSp, "DUPLICATE FIELD", fmt.Sprintf("This record sets `%s` twice.", f.Name), "")
			continue
		}
		seen[f.Name] = true
		var fw Type
		if wantRec != nil {
			fw, _ = wantRec.field(f.Name)
		}
		names = append(names, f.Name)
		ts = append(ts, c.infer(f.Value, sc, fw))
	}
	return newRecord(names, ts)
}

func (c *checker) inferTry(n *syntax.Try, sc *Scope) Type {
	xt := Prune(c.infer(n.X, sc, nil))
	if isErr(xt) {
		return tErr
	}
	if len(c.retStack) == 0 {
		c.err(n.Sp, "? CAN'T BE USED HERE", "`?` hands an error to the function's caller, but this code isn't inside a function.",
			"In a test, compare the whole result instead, like: assert parse_int(\"4\") == Ok(4)")
		return tErr
	}
	ret := c.retStack[len(c.retStack)-1]
	nm := newNamer()
	where := "This function"
	if c.fnName != "" && len(c.retStack) == 1 {
		where = "`" + c.fnName + "`"
	}
	xc, ok := xt.(*TCon)
	if !ok || xc.Rigid || (xc.Key != "Result" && xc.Key != "Option") {
		msg := fmt.Sprintf("`?` works on a Result or an Option, but this is %s.", nm.describe(xt))
		if isVar(xt) {
			msg = "I can't tell whether this is a Result or an Option yet."
		}
		c.err(n.Sp, "? NEEDS A RESULT", msg, "")
		return tErr
	}
	if xc.Key == "Result" {
		if unify(ret, resultOf(c.fresh(), xc.Args[1])) {
			return xc.Args[0]
		}
		rt := Prune(ret)
		if isCon(rt, "Result") {
			c.err(n.Sp, "ERROR TYPES DIFFER", fmt.Sprintf("`?` would pass up an error of type %s, but %s returns errors of type %s.", nm.str(xc.Args[1]), strings.ToLower(where[:1])+where[1:], nm.str(rt.(*TCon).Args[1])),
				"Convert the error first, for example with result.map_err(value, fn(e) show(e)).")
		} else {
			hint := fmt.Sprintf("Change the return type to Result[%s, %s], or handle the error here with match.", nm.str(rt), nm.str(xc.Args[1]))
			if isCon(rt, "Unit") {
				hint = fmt.Sprintf("Add -> Result[Unit, %s] to the function, and end it with Ok({}). For main, Plain then prints the error and exits with code 1.", nm.str(xc.Args[1]))
			}
			c.err(n.Sp, "? NEEDS A RESULT FUNCTION", fmt.Sprintf("`?` hands the error to whoever called this function, so the function has to return a Result. %s returns %s.", where, nm.describe(rt)), hint)
		}
		return tErr
	}
	if unify(ret, optionOf(c.fresh())) {
		return xc.Args[0]
	}
	c.err(n.Sp, "? NEEDS AN OPTION FUNCTION", fmt.Sprintf("`?` on an Option returns None from the function when the value is missing, so the function has to return an Option. %s returns %s.", where, nm.describe(ret)),
		"Use match here instead, or option.unwrap_or(value, fallback). To turn it into a Result, use option.to_result(value, error).")
	return tErr
}

// ---- blocks and statements ----

func (c *checker) inferBlock(b *syntax.Block, sc *Scope, want Type) Type {
	bsc := newScope(sc)
	for i, s := range b.Stmts {
		if i == len(b.Stmts)-1 {
			if es, ok := s.(*syntax.ExprStmt); ok {
				t := c.infer(es.X, bsc, want)
				c.g.info.Types[b] = t
				return t
			}
		}
		c.stmt(s, bsc)
	}
	c.g.info.Types[b] = tUnit
	return tUnit
}

func (c *checker) stmtBlock(b *syntax.Block, sc *Scope) {
	bsc := newScope(sc)
	for _, s := range b.Stmts {
		c.stmt(s, bsc)
	}
	c.g.info.Types[b] = tUnit
}

func (c *checker) stmt(s syntax.Stmt, sc *Scope) {
	switch n := s.(type) {
	case *syntax.LetStmt:
		var want Type
		if n.Type != nil {
			want = c.resolveType(n.Type, c.typeParams)
		}
		t := c.infer(n.Value, sc, want)
		if want != nil {
			c.expectType(n.Value.Span(), t, want, fmt.Sprintf("`%s` is declared as %s", n.Name, newNamer().describe(want)))
			t = want
		}
		if n.Name != "_" {
			b := &Binding{Kind: BLocal, Name: n.Name, Type: t, Mutable: n.Mutable, Decl: n}
			sc.names[n.Name] = b
			c.g.info.Defs[n] = b
		}
	case *syntax.AssignStmt:
		b := sc.find(n.Name)
		if b == nil {
			c.unknownName(n.NameSp, n.Name, sc)
			c.infer(n.Value, sc, nil)
			return
		}
		if b.Kind != BLocal || !b.Mutable {
			msg := fmt.Sprintf("`%s` can't be changed.", n.Name)
			hint := ""
			switch {
			case b.Kind == BLocal:
				msg = fmt.Sprintf("`%s` was created with let, so it can't be changed.\n\nPlain keeps values fixed unless you ask otherwise, so nothing changes under your feet.", n.Name)
				hint = fmt.Sprintf("If it really needs to change, create it with var:\n\n    var %s = ...", n.Name)
			case b.Kind == BConst:
				msg = fmt.Sprintf("`%s` is a top-level constant, so it can't be changed.", n.Name)
			}
			c.err(n.NameSp, "CAN'T CHANGE THIS", msg, hint)
			c.infer(n.Value, sc, nil)
			return
		}
		c.g.info.Refs[n] = b
		vt := c.infer(n.Value, sc, b.Type)
		if n.Op == "=" {
			c.expectType(n.Value.Span(), vt, b.Type, fmt.Sprintf("`%s` holds %s", n.Name, newNamer().describe(b.Type)))
			return
		}
		op := n.Op[:1]
		if isCon(b.Type, "String") && op == "+" {
			c.err(n.Sp, "CAN'T ADD TEXT", "`+=` is for numbers.", fmt.Sprintf("To add to text, write %s = %s ++ ...", n.Name, n.Name))
			return
		}
		rt := c.binaryTypes(op, n.Sp, n.Sp, b.Type, vt)
		if !isErr(rt) {
			c.expectType(n.Value.Span(), rt, b.Type, fmt.Sprintf("`%s` holds %s", n.Name, newNamer().describe(b.Type)))
		}
	case *syntax.ExprStmt:
		c.stmtExpr(n.X, sc)
	case *syntax.ForStmt:
		it := c.infer(n.Iter, sc, nil)
		elem := c.fresh()
		var et Type = elem
		if !unify(it, listOf(elem)) {
			nm := newNamer()
			hint := ""
			switch {
			case isCon(it, "String"):
				hint = "To go over each character, use string.chars(s)."
			case isCon(it, "Dict"):
				hint = "Go over dict.keys(d), dict.values(d) or dict.entries(d)."
			case isCon(it, "Int"):
				hint = "To count, use list.range(1, n), which includes both ends."
			}
			c.err(n.Iter.Span(), "NOT A LIST", fmt.Sprintf("A for loop goes over a List, but this is %s.", nm.describe(it)), hint)
			et = tErr
		}
		bsc := newScope(sc)
		b := &Binding{Kind: BLocal, Name: n.Var, Type: et, Decl: n}
		bsc.names[n.Var] = b
		c.g.info.Defs[n] = b
		c.stmtBlock(n.Body, bsc)
	case *syntax.WhileStmt:
		c.expectBool(n.Cond, c.infer(n.Cond, sc, nil), "The condition of a while loop")
		c.stmtBlock(n.Body, sc)
	case *syntax.AssertStmt:
		c.expectBool(n.X, c.infer(n.X, sc, nil), "An assert")
	}
}

// stmtExpr checks an expression whose value is thrown away.
func (c *checker) stmtExpr(e syntax.Expr, sc *Scope) {
	switch n := e.(type) {
	case *syntax.If:
		c.expectBool(n.Cond, c.infer(n.Cond, sc, nil), "The condition of an if")
		c.stmtBlock(n.Then, sc)
		if n.Else != nil {
			c.stmtExpr(n.Else, sc)
		}
		c.g.info.Types[n] = tUnit
		return
	case *syntax.Match:
		c.inferMatch(n, sc, nil, true)
		c.g.info.Types[n] = tUnit
		return
	case *syntax.Block:
		c.stmtBlock(n, sc)
		return
	}
	t := c.infer(e, sc, nil)
	if isCon(t, "Result") {
		c.err(e.Span(), "UNUSED RESULT", fmt.Sprintf("This gives %s, but nothing looks at it. If it failed, nobody would know.", newNamer().describe(t)),
			"Pass the error up with ?, handle it with match, or ignore it on purpose with: let _ = ...")
	}
}

// ---- match and patterns ----

func (c *checker) inferMatch(n *syntax.Match, sc *Scope, want Type, stmt bool) Type {
	st := c.infer(n.Subject, sc, nil)
	var res Type = c.fresh()
	if want != nil && !stmt {
		unify(res, want)
	}
	before := c.errorCount()
	if len(n.Arms) == 0 {
		c.err(n.Sp, "EMPTY MATCH", "This match has no cases.", "")
		return tErr
	}
	var first Type
	for i, arm := range n.Arms {
		asc := newScope(sc)
		c.pattern(arm.Pat, st, asc, map[string]bool{})
		if arm.Guard != nil {
			c.expectBool(arm.Guard, c.infer(arm.Guard, asc, nil), "A match guard")
		}
		if stmt {
			c.stmtExpr(arm.Body, asc)
			continue
		}
		bt := c.infer(arm.Body, asc, res)
		if i == 0 {
			first = bt
		}
		if !unify(res, bt) {
			nm := newNamer()
			c.err(arm.Body.Span(), "MATCH CASES DIFFER", fmt.Sprintf("The first case of this match gives %s, but this case gives %s.", nm.describe(first), nm.describe(bt)),
				"Every case of a match that produces a value has to give the same type.")
		}
	}
	if c.errorCount() == before {
		c.checkExhaustive(n, st)
	}
	if stmt {
		return tUnit
	}
	return res
}

func (c *checker) pattern(p syntax.Pattern, t Type, sc *Scope, seen map[string]bool) {
	nm := newNamer()
	switch n := p.(type) {
	case *syntax.PWild:
	case *syntax.PBind:
		if seen[n.Name] {
			c.err(n.Sp, "DUPLICATE NAME IN PATTERN", fmt.Sprintf("`%s` is used twice in this pattern.", n.Name), "")
			return
		}
		seen[n.Name] = true
		b := &Binding{Kind: BLocal, Name: n.Name, Type: t, Decl: n}
		sc.names[n.Name] = b
		c.g.info.Defs[n] = b
	case *syntax.PLit:
		var lt Type = tInt
		switch n.Kind {
		case syntax.String:
			lt = tString
		case syntax.Keyword:
			lt = tBool
		}
		if !unify(t, lt) {
			c.err(n.Sp, "PATTERN TYPE MISMATCH", fmt.Sprintf("This pattern is %s, but the value being matched is %s.", nm.describe(lt), nm.describe(t)), c.mismatchHint(t, lt, nm))
		}
	case *syntax.PCtor:
		b := c.lookupCtor(n.Module, n.Name, n.Sp, sc)
		if b == nil {
			for _, a := range n.Args {
				c.pattern(a, tErr, sc, seen)
			}
			return
		}
		c.g.info.Refs[n] = b
		ut, fields := c.instantiateCtor(b.Ctor)
		if !unify(t, ut) {
			c.err(n.Sp, "PATTERN TYPE MISMATCH", fmt.Sprintf("`%s` is a pattern for %s, but the value being matched is %s.", n.Name, nm.describe(ut), nm.describe(t)), "")
			for _, a := range n.Args {
				c.pattern(a, tErr, sc, seen)
			}
			return
		}
		if len(n.Args) != len(fields) {
			if len(fields) > 0 && !n.Parens {
				blanks := strings.TrimSuffix(strings.Repeat("_, ", len(fields)), ", ")
				c.err(n.Sp, "MISSING PATTERN VALUES", fmt.Sprintf("`%s` holds %d value(s).", n.Name, len(fields)),
					fmt.Sprintf("Write %s(%s) to ignore them, or give them names to use them.", n.Name, blanks))
			} else {
				c.err(n.Sp, "WRONG NUMBER OF PATTERN VALUES", fmt.Sprintf("`%s` holds %d value(s), but this pattern has %d.", n.Name, len(fields), len(n.Args)), "")
			}
		}
		for i, a := range n.Args {
			ft := Type(tErr)
			if i < len(fields) {
				ft = fields[i]
			}
			c.pattern(a, ft, sc, seen)
		}
	case *syntax.PList:
		elem := c.fresh()
		var et Type = elem
		if !unify(t, listOf(elem)) {
			c.err(n.Sp, "PATTERN TYPE MISMATCH", fmt.Sprintf("This is a list pattern, but the value being matched is %s.", nm.describe(t)), "")
			et = tErr
		}
		for _, e := range n.Elems {
			c.pattern(e, et, sc, seen)
		}
		if n.Rest != "" {
			if seen[n.Rest] {
				c.err(n.RestSp, "DUPLICATE NAME IN PATTERN", fmt.Sprintf("`%s` is used twice in this pattern.", n.Rest), "")
				return
			}
			seen[n.Rest] = true
			var rt Type = tErr
			if !isErr(et) {
				rt = listOf(et)
			}
			b := &Binding{Kind: BLocal, Name: n.Rest, Type: rt, Decl: n}
			sc.names[n.Rest] = b
			c.g.info.Defs[n] = b
		}
	}
}
