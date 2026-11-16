// Package types checks Plain programs: inference, exhaustiveness and the no-surprises rules.
package types

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Gitguy268/plain/internal/syntax"
)

// Type is a Plain type.
type Type interface{ isType() }

// TVar is a type not known yet. Ref is set once it is.
type TVar struct {
	ID  int
	Ref Type
}

// TCon is a named type: Int, List[T], a union, or a type parameter (Rigid) inside a generic function.
type TCon struct {
	Name  string // shown to people
	Key   string // unique identity
	Args  []Type
	Rigid bool
}

// TFn is a function type. Required is how many leading parameters have no default.
type TFn struct {
	Params   []Type
	Ret      Type
	Required int
}

// TRecord is a structural record with fields sorted by name.
type TRecord struct {
	Names []string
	Types []Type
}

// TErr stands in after an error so one mistake doesn't cause many reports.
type TErr struct{}

func (*TVar) isType()    {}
func (*TCon) isType()    {}
func (*TFn) isType()     {}
func (*TRecord) isType() {}
func (*TErr) isType()    {}

func con(name string, args ...Type) *TCon { return &TCon{Name: name, Key: name, Args: args} }

var (
	tInt    = con("Int")
	tFloat  = con("Float")
	tBool   = con("Bool")
	tString = con("String")
	tUnit   = con("Unit")
	tErr    = &TErr{}
)

func listOf(t Type) *TCon      { return con("List", t) }
func dictOf(k, v Type) *TCon   { return con("Dict", k, v) }
func optionOf(t Type) *TCon    { return con("Option", t) }
func resultOf(t, e Type) *TCon { return con("Result", t, e) }
func isCon(t Type, key string) bool {
	c, ok := Prune(t).(*TCon)
	return ok && c.Key == key && !c.Rigid
}

// Prune follows bound type variables.
func Prune(t Type) Type {
	for {
		v, ok := t.(*TVar)
		if !ok || v.Ref == nil {
			return t
		}
		t = v.Ref
	}
}

func occurs(v *TVar, t Type) bool {
	switch t := Prune(t).(type) {
	case *TVar:
		return t == v
	case *TCon:
		for _, a := range t.Args {
			if occurs(v, a) {
				return true
			}
		}
	case *TFn:
		for _, p := range t.Params {
			if occurs(v, p) {
				return true
			}
		}
		return occurs(v, t.Ret)
	case *TRecord:
		for _, f := range t.Types {
			if occurs(v, f) {
				return true
			}
		}
	}
	return false
}

func unify(a, b Type) bool {
	a, b = Prune(a), Prune(b)
	if _, ok := a.(*TErr); ok {
		return true
	}
	if _, ok := b.(*TErr); ok {
		return true
	}
	if va, ok := a.(*TVar); ok {
		if vb, ok := b.(*TVar); ok && va == vb {
			return true
		}
		if occurs(va, b) {
			return false
		}
		va.Ref = b
		return true
	}
	if _, ok := b.(*TVar); ok {
		return unify(b, a)
	}
	switch a := a.(type) {
	case *TCon:
		b, ok := b.(*TCon)
		if !ok || a.Key != b.Key || a.Rigid != b.Rigid || len(a.Args) != len(b.Args) {
			return false
		}
		okAll := true
		for i := range a.Args {
			if !unify(a.Args[i], b.Args[i]) {
				okAll = false
			}
		}
		return okAll
	case *TFn:
		b, ok := b.(*TFn)
		if !ok || len(a.Params) != len(b.Params) {
			return false
		}
		okAll := true
		for i := range a.Params {
			if !unify(a.Params[i], b.Params[i]) {
				okAll = false
			}
		}
		return unify(a.Ret, b.Ret) && okAll
	case *TRecord:
		b, ok := b.(*TRecord)
		if !ok || len(a.Names) != len(b.Names) {
			return false
		}
		for i := range a.Names {
			if a.Names[i] != b.Names[i] {
				return false
			}
		}
		okAll := true
		for i := range a.Types {
			if !unify(a.Types[i], b.Types[i]) {
				okAll = false
			}
		}
		return okAll
	}
	return false
}

// subst replaces rigid type parameters by name.
func subst(t Type, m map[string]Type) Type {
	if len(m) == 0 {
		return t
	}
	switch t := Prune(t).(type) {
	case *TCon:
		if t.Rigid {
			if r, ok := m[t.Name]; ok {
				return r
			}
			return t
		}
		if len(t.Args) == 0 {
			return t
		}
		args := make([]Type, len(t.Args))
		for i, a := range t.Args {
			args[i] = subst(a, m)
		}
		return &TCon{Name: t.Name, Key: t.Key, Args: args}
	case *TFn:
		ps := make([]Type, len(t.Params))
		for i, p := range t.Params {
			ps[i] = subst(p, m)
		}
		return &TFn{Params: ps, Ret: subst(t.Ret, m), Required: t.Required}
	case *TRecord:
		ts := make([]Type, len(t.Types))
		for i, f := range t.Types {
			ts[i] = subst(f, m)
		}
		return &TRecord{Names: t.Names, Types: ts}
	default:
		return t
	}
}

func newRecord(names []string, ts []Type) *TRecord {
	idx := make([]int, len(names))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(i, j int) bool { return names[idx[i]] < names[idx[j]] })
	r := &TRecord{}
	for _, i := range idx {
		r.Names = append(r.Names, names[i])
		r.Types = append(r.Types, ts[i])
	}
	return r
}

func (r *TRecord) field(name string) (Type, bool) {
	for i, n := range r.Names {
		if n == name {
			return r.Types[i], true
		}
	}
	return nil, false
}

// ---- printing ----

type namer struct{ names map[*TVar]string }

func newNamer() *namer { return &namer{names: map[*TVar]string{}} }

// TypeString prints a type the way people write it.
func TypeString(t Type) string { return newNamer().str(t) }

func (n *namer) str(t Type) string {
	switch t := Prune(t).(type) {
	case *TVar:
		if s, ok := n.names[t]; ok {
			return s
		}
		s := string(rune('a' + len(n.names)%26))
		if len(n.names) >= 26 {
			s += fmt.Sprint(len(n.names) / 26)
		}
		n.names[t] = s
		return s
	case *TCon:
		if len(t.Args) == 0 {
			return t.Name
		}
		parts := make([]string, len(t.Args))
		for i, a := range t.Args {
			parts[i] = n.str(a)
		}
		return t.Name + "[" + strings.Join(parts, ", ") + "]"
	case *TFn:
		parts := make([]string, len(t.Params))
		for i, p := range t.Params {
			parts[i] = n.str(p)
		}
		s := "fn(" + strings.Join(parts, ", ") + ")"
		if r, ok := Prune(t.Ret).(*TCon); !ok || r.Key != "Unit" {
			s += " -> " + n.str(t.Ret)
		}
		return s
	case *TRecord:
		parts := make([]string, len(t.Names))
		for i := range t.Names {
			parts[i] = t.Names[i] + ": " + n.str(t.Types[i])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case *TErr:
		return "?"
	}
	return "?"
}

// describe gives "an Int", "a List[String]", "a record {x: Int}".
func (n *namer) describe(t Type) string {
	s := n.str(t)
	switch Prune(t).(type) {
	case *TRecord:
		return "a record " + s
	case *TFn:
		return "a function " + s
	case *TVar:
		return "a value of unknown type"
	}
	if strings.ContainsRune("AEIOU", rune(s[0])) {
		return "an " + s
	}
	return "a " + s
}

// ---- modules and bindings ----

// Union is a type declared with variants.
type Union struct {
	Key    string
	Name   string
	Params []string
	Ctors  []*Ctor
	Module *Module
}

// Ctor is one variant of a union.
type Ctor struct {
	Union      *Union
	Name       string
	Fields     []Type // may mention the union's rigid params
	FieldNames []string
}

type alias struct {
	decl      *syntax.TypeDecl
	params    []string
	body      Type
	resolving bool
}

// BindKind says what a name refers to.
type BindKind int

const (
	BLocal BindKind = iota
	BFn
	BConst
	BExtern
	BCtor
	BModule
)

// Scheme is a possibly generic type: Params are the names of rigid parameters to replace.
type Scheme struct {
	Params []string
	Type   Type
}

// Binding is what a name means.
type Binding struct {
	Kind    BindKind
	Name    string
	Type    Type
	Scheme  *Scheme
	Mutable bool
	Module  *Module // owner; for BModule, the module itself
	Extern  *syntax.ExternDecl
	Ctor    *Ctor
	Decl    syntax.Node
	Pub     bool
}

// Scope is a chain of name tables.
type Scope struct {
	parent *Scope
	names  map[string]*Binding
	lookup func(string) *Binding // fallback, used by the outermost scope
}

func newScope(parent *Scope) *Scope { return &Scope{parent: parent, names: map[string]*Binding{}} }

func (s *Scope) find(name string) *Binding {
	for sc := s; sc != nil; sc = sc.parent {
		if b, ok := sc.names[name]; ok {
			return b
		}
		if sc.lookup != nil {
			if b := sc.lookup(name); b != nil {
				return b
			}
		}
	}
	return nil
}

func (s *Scope) all() []string {
	seen := map[string]bool{}
	var out []string
	for sc := s; sc != nil; sc = sc.parent {
		for n := range sc.names {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	return out
}

// Module is one checked file.
type Module struct {
	Name    string
	Std     bool
	File    *syntax.File
	JSName  string
	Scope   *Scope
	Exports map[string]*Binding
	Types   map[string]any // *Union or *alias
	Imports []*Module
	state   int // 0 new, 1 checking, 2 done
}

// NewModule wraps a parsed file.
func NewModule(name string, std bool, f *syntax.File) *Module {
	js := "$m_" + name
	if std {
		js = "$std_" + name
	}
	return &Module{Name: name, Std: std, File: f, JSName: js, Exports: map[string]*Binding{}, Types: map[string]any{}}
}

// Info is what the code generator needs to know from checking.
type Info struct {
	Types   map[syntax.Expr]Type
	Refs    map[syntax.Node]*Binding // identifiers, constructors and module members
	Defs    map[syntax.Node]*Binding // declarations of locals
	Pipes   map[*syntax.Binary]*syntax.Call
	Modules []*Module // dependencies first
	Unions  map[string]*Union
	Core    *Module
}

// Lookup finds a name defined directly in this scope.
func (s *Scope) Lookup(name string) (*Binding, bool) {
	b, ok := s.names[name]
	return b, ok
}

// MustLookup is Lookup for names the checker has already verified.
func (s *Scope) MustLookup(name string) *Binding { return s.names[name] }

// IsCon reports whether t is the named, non-parameter type key (Int, List, Option, ...).
func IsCon(t Type, key string) bool { return isCon(t, key) }

// CtorFields returns a constructor's field types for a union applied to args.
func CtorFields(ct *Ctor, args []Type) []Type {
	m := map[string]Type{}
	for i, p := range ct.Union.Params {
		if i < len(args) {
			m[p] = args[i]
		}
	}
	out := make([]Type, len(ct.Fields))
	for i, f := range ct.Fields {
		out[i] = subst(f, m)
	}
	return out
}
