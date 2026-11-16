package types

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Gitguy268/plain/internal/diag"
	"github.com/Gitguy268/plain/internal/syntax"
)

// Exhaustiveness and reachability use the usefulness algorithm from
// Maranget, "Warnings for pattern matching" (2007).

const (
	pWild = iota
	pCtor
	pLit
)

type spat struct {
	kind int
	name string // constructor name, or the literal's canonical text
	args []*spat
}

var wild = &spat{kind: pWild}

type csig struct {
	name string
	args []Type
}

func toSpat(p syntax.Pattern) *spat {
	switch n := p.(type) {
	case *syntax.PCtor:
		s := &spat{kind: pCtor, name: n.Name}
		for _, a := range n.Args {
			s.args = append(s.args, toSpat(a))
		}
		return s
	case *syntax.PLit:
		switch n.Kind {
		case syntax.Keyword:
			return &spat{kind: pCtor, name: n.Text}
		case syntax.String:
			return &spat{kind: pLit, name: "s:" + n.Str}
		default:
			v, _ := strconv.ParseInt(strings.ReplaceAll(n.Text, "_", ""), 10, 64)
			return &spat{kind: pLit, name: "i:" + strconv.FormatInt(v, 10)}
		}
	case *syntax.PList:
		var tail *spat
		if n.HasRest {
			tail = wild
		} else {
			tail = &spat{kind: pCtor, name: "[]"}
		}
		for i := len(n.Elems) - 1; i >= 0; i-- {
			tail = &spat{kind: pCtor, name: "::", args: []*spat{toSpat(n.Elems[i]), tail}}
		}
		return tail
	}
	return wild
}

func (c *checker) ctorsOf(t Type) ([]csig, bool) {
	tc, ok := Prune(t).(*TCon)
	if !ok || tc.Rigid {
		return nil, false
	}
	switch tc.Key {
	case "Bool":
		return []csig{{name: "true"}, {name: "false"}}, true
	case "List":
		return []csig{{name: "[]"}, {name: "::", args: []Type{tc.Args[0], tc}}}, true
	}
	u, ok := c.g.unions[tc.Key]
	if !ok {
		return nil, false
	}
	m := map[string]Type{}
	for i, p := range u.Params {
		if i < len(tc.Args) {
			m[p] = tc.Args[i]
		}
	}
	var out []csig
	for _, ct := range u.Ctors {
		s := csig{name: ct.Name}
		for _, f := range ct.Fields {
			s.args = append(s.args, subst(f, m))
		}
		out = append(out, s)
	}
	return out, true
}

func wilds(n int) []*spat {
	out := make([]*spat, n)
	for i := range out {
		out[i] = wild
	}
	return out
}

func specialize(rows [][]*spat, cs csig) [][]*spat {
	var out [][]*spat
	for _, r := range rows {
		h := r[0]
		switch {
		case h.kind == pCtor && h.name == cs.name:
			args := h.args
			if len(args) != len(cs.args) {
				args = wilds(len(cs.args))
			}
			out = append(out, append(append([]*spat{}, args...), r[1:]...))
		case h.kind == pWild:
			out = append(out, append(wilds(len(cs.args)), r[1:]...))
		}
	}
	return out
}

func specializeLit(rows [][]*spat, lit string) [][]*spat {
	var out [][]*spat
	for _, r := range rows {
		h := r[0]
		if h.kind == pWild || (h.kind == pLit && h.name == lit) {
			out = append(out, r[1:])
		}
	}
	return out
}

func defaultRows(rows [][]*spat) [][]*spat {
	var out [][]*spat
	for _, r := range rows {
		if r[0].kind == pWild {
			out = append(out, r[1:])
		}
	}
	return out
}

func headCtors(rows [][]*spat) map[string]bool {
	m := map[string]bool{}
	for _, r := range rows {
		if r[0].kind == pCtor {
			m[r[0].name] = true
		}
	}
	return m
}

func covers(ctors []csig, heads map[string]bool) bool {
	if len(heads) == 0 {
		return false
	}
	for _, cs := range ctors {
		if !heads[cs.name] {
			return false
		}
	}
	return true
}

// missing returns values not matched by any row, or nil if the rows cover everything.
func (c *checker) missing(rows [][]*spat, tys []Type) []*spat {
	if len(tys) == 0 {
		if len(rows) == 0 {
			return []*spat{}
		}
		return nil
	}
	ctors, complete := c.ctorsOf(tys[0])
	heads := headCtors(rows)
	if complete && covers(ctors, heads) {
		for _, cs := range ctors {
			w := c.missing(specialize(rows, cs), append(append([]Type{}, cs.args...), tys[1:]...))
			if w != nil {
				n := len(cs.args)
				return append([]*spat{{kind: pCtor, name: cs.name, args: w[:n]}}, w[n:]...)
			}
		}
		return nil
	}
	w := c.missing(defaultRows(rows), tys[1:])
	if w == nil {
		return nil
	}
	head := wild
	if complete && len(heads) > 0 {
		for _, cs := range ctors {
			if !heads[cs.name] {
				head = &spat{kind: pCtor, name: cs.name, args: wilds(len(cs.args))}
				break
			}
		}
	}
	return append([]*spat{head}, w...)
}

func (c *checker) useful(rows [][]*spat, vec []*spat, tys []Type) bool {
	if len(vec) == 0 {
		return len(rows) == 0
	}
	h := vec[0]
	switch h.kind {
	case pCtor:
		ctors, _ := c.ctorsOf(tys[0])
		for _, cs := range ctors {
			if cs.name == h.name {
				args := h.args
				if len(args) != len(cs.args) {
					args = wilds(len(cs.args))
				}
				return c.useful(specialize(rows, cs), append(append([]*spat{}, args...), vec[1:]...), append(append([]Type{}, cs.args...), tys[1:]...))
			}
		}
		return true
	case pLit:
		return c.useful(specializeLit(rows, h.name), vec[1:], tys[1:])
	}
	ctors, complete := c.ctorsOf(tys[0])
	if complete && covers(ctors, headCtors(rows)) {
		for _, cs := range ctors {
			if c.useful(specialize(rows, cs), append(wilds(len(cs.args)), vec[1:]...), append(append([]Type{}, cs.args...), tys[1:]...)) {
				return true
			}
		}
		return false
	}
	return c.useful(defaultRows(rows), vec[1:], tys[1:])
}

func (s *spat) String() string {
	switch s.kind {
	case pWild:
		return "_"
	case pLit:
		if strings.HasPrefix(s.name, "s:") {
			return strconv.Quote(s.name[2:])
		}
		return s.name[2:]
	}
	switch s.name {
	case "[]":
		return "[]"
	case "::":
		var items []string
		cur := s
		for cur.kind == pCtor && cur.name == "::" {
			items = append(items, cur.args[0].String())
			cur = cur.args[1]
		}
		if cur.kind == pWild {
			items = append(items, "..")
		}
		return "[" + strings.Join(items, ", ") + "]"
	}
	if len(s.args) == 0 {
		return s.name
	}
	parts := make([]string, len(s.args))
	for i, a := range s.args {
		parts[i] = a.String()
	}
	return s.name + "(" + strings.Join(parts, ", ") + ")"
}

func (c *checker) checkExhaustive(n *syntax.Match, st Type) {
	tys := []Type{st}
	var rows [][]*spat
	for _, arm := range n.Arms {
		row := []*spat{toSpat(arm.Pat)}
		if !c.useful(rows, row, tys) {
			c.report(diag.Warning, arm.Pat.Span(), "UNREACHABLE CASE",
				"This case can never match, because the cases above it already cover every value it would.", "Remove it, or move it above the case that catches it.")
		}
		if arm.Guard == nil {
			rows = append(rows, row)
		}
	}
	var gaps []string
	for len(gaps) < 4 {
		w := c.missing(rows, tys)
		if w == nil {
			break
		}
		gaps = append(gaps, "    "+w[0].String())
		rows = append(rows, w)
		if w[0].kind == pWild {
			break
		}
	}
	if len(gaps) == 0 {
		return
	}
	sp := diag.Span{Start: n.Sp.Start, End: n.Subject.Span().End}
	c.err(sp, "MISSING CASES", fmt.Sprintf("This match doesn't handle every possible value of %s. It's missing:\n\n%s", TypeString(st), strings.Join(gaps, "\n")),
		"Add a case for each one. A final `_ -> ...` case catches everything else, but then Plain can't warn you when a new variant is added later.")
}
