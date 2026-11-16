// Package format prints Plain code in its one standard style.
//
// The formatter keeps the choices that carry meaning (where you broke a long call over
// lines, where you left a blank line, every comment) and fixes everything else:
// indentation, spacing, commas and braces.
package format

import (
	"strings"

	"github.com/Gitguy268/plain/internal/diag"
	"github.com/Gitguy268/plain/internal/syntax"
)

// Source formats a file. Code that doesn't parse is returned with its errors and left alone.
func Source(name, text string) (string, []diag.Diagnostic) {
	src := diag.NewSource(name, text)
	f, diags := syntax.ParseFile(src)
	if diag.HasErrors(diags) {
		return text, diags
	}
	p := &printer{src: src, text: text, comments: f.Comments}
	return p.file(f), nil
}

type printer struct {
	src      *diag.Source
	text     string
	comments []syntax.Comment
	ci       int
}

func pad(ind int) string { return strings.Repeat("  ", ind) }

// leading returns the comments that start before off, one per line at the given indent.
func (p *printer) leading(off, ind int) string {
	var b strings.Builder
	for p.ci < len(p.comments) && p.comments[p.ci].Sp.Start < off {
		c := p.comments[p.ci]
		if p.ci > 0 && b.Len() > 0 && p.blankBetween(p.comments[p.ci-1].Sp.End, c.Sp.Start) {
			b.WriteString("\n")
		}
		b.WriteString(pad(ind) + "//" + c.Text + "\n")
		p.ci++
	}
	return b.String()
}

// trailing returns a comment that sits on the same line right after end.
func (p *printer) trailing(end int) string {
	if p.ci >= len(p.comments) {
		return ""
	}
	c := p.comments[p.ci]
	if c.Sp.Start < end {
		return ""
	}
	if strings.Contains(p.text[end:c.Sp.Start], "\n") {
		return ""
	}
	p.ci++
	return " //" + c.Text
}

func (p *printer) blankBetween(a, b int) bool {
	if a >= b || b > len(p.text) {
		return false
	}
	lines := strings.Split(p.text[a:b], "\n")
	for i := 1; i < len(lines)-1; i++ {
		if strings.TrimSpace(lines[i]) == "" {
			return true
		}
	}
	return false
}

func (p *printer) multiline(sp diag.Span) bool {
	return strings.Contains(p.text[sp.Start:sp.End], "\n")
}

func (p *printer) file(f *syntax.File) string {
	var b strings.Builder
	var prev syntax.Item
	for _, it := range f.Items {
		if prev != nil {
			if p.blankBetween(prev.Span().End, it.Span().Start) || !sameKind(prev, it) {
				b.WriteString("\n")
			}
		}
		b.WriteString(p.leading(it.Span().Start, 0))
		b.WriteString(p.item(it))
		b.WriteString(p.trailing(it.Span().End))
		b.WriteString("\n")
		prev = it
	}
	rest := p.leading(len(p.text)+1, 0)
	if rest != "" {
		if prev != nil {
			b.WriteString("\n")
		}
		b.WriteString(rest)
	}
	return b.String()
}

func sameKind(a, b syntax.Item) bool {
	switch a.(type) {
	case *syntax.ImportDecl:
		_, ok := b.(*syntax.ImportDecl)
		return ok
	case *syntax.LetDecl:
		_, ok := b.(*syntax.LetDecl)
		return ok
	case *syntax.ExternDecl:
		_, ok := b.(*syntax.ExternDecl)
		return ok
	}
	return false
}

func pubPrefix(pub bool) string {
	if pub {
		return "pub "
	}
	return ""
}

func typeParams(ps []string) string {
	if len(ps) == 0 {
		return ""
	}
	return "[" + strings.Join(ps, ", ") + "]"
}

func (p *printer) params(ps []*syntax.Param, ind int) string {
	var out []string
	for _, prm := range ps {
		s := prm.Name
		if prm.Type != nil {
			s += ": " + p.typ(prm.Type)
		}
		if prm.Default != nil {
			s += " = " + p.expr(prm.Default, ind)
		}
		out = append(out, s)
	}
	return "(" + strings.Join(out, ", ") + ")"
}

func (p *printer) item(it syntax.Item) string {
	switch d := it.(type) {
	case *syntax.FnDecl:
		s := pubPrefix(d.Pub) + "fn " + d.Name + typeParams(d.TypeParams) + p.params(d.Params, 0)
		if d.Ret != nil {
			s += " -> " + p.typ(d.Ret)
		}
		return s + " " + p.block(d.Body, 0)
	case *syntax.ExternDecl:
		s := pubPrefix(d.Pub) + "extern fn " + d.Name + typeParams(d.TypeParams) + p.params(d.Params, 0)
		if d.Ret != nil {
			s += " -> " + p.typ(d.Ret)
		}
		return s + " = \"" + d.Target + "\""
	case *syntax.TypeDecl:
		s := pubPrefix(d.Pub) + "type " + d.Name + typeParams(d.Params) + " ="
		if d.Alias != nil {
			return s + " " + p.typ(d.Alias)
		}
		var vs []string
		for _, v := range d.Variants {
			vs = append(vs, p.variant(v))
		}
		one := s + " " + strings.Join(vs, " | ")
		if !p.multiline(d.Sp) && len(one) <= 90 {
			return one
		}
		var b strings.Builder
		b.WriteString(s)
		for i, v := range vs {
			b.WriteString("\n")
			b.WriteString(p.leading(d.Variants[i].Sp.Start, 1))
			b.WriteString("  | " + v)
		}
		return b.String()
	case *syntax.ImportDecl:
		return "import " + d.Name
	case *syntax.TestDecl:
		return "test " + p.text[d.Sp.Start:d.Body.Sp.Start][len("test "):] + p.block(d.Body, 0)
	case *syntax.LetDecl:
		s := pubPrefix(d.Pub) + "let " + d.Name
		if d.Type != nil {
			s += ": " + p.typ(d.Type)
		}
		return s + " = " + p.expr(d.Value, 0)
	}
	return ""
}

func (p *printer) variant(v *syntax.Variant) string {
	if len(v.Fields) == 0 {
		return v.Name
	}
	var fs []string
	for _, f := range v.Fields {
		s := p.typ(f.Type)
		if f.Name != "" {
			s = f.Name + ": " + s
		}
		fs = append(fs, s)
	}
	return v.Name + "(" + strings.Join(fs, ", ") + ")"
}

func (p *printer) typ(t syntax.TypeExpr) string {
	switch n := t.(type) {
	case *syntax.NamedType:
		s := n.Name
		if n.Module != "" {
			s = n.Module + "." + s
		}
		if len(n.Args) > 0 {
			var as []string
			for _, a := range n.Args {
				as = append(as, p.typ(a))
			}
			s += "[" + strings.Join(as, ", ") + "]"
		}
		return s
	case *syntax.FnType:
		var ps []string
		for _, a := range n.Params {
			ps = append(ps, p.typ(a))
		}
		s := "fn(" + strings.Join(ps, ", ") + ")"
		if n.Ret != nil {
			s += " -> " + p.typ(n.Ret)
		}
		return s
	case *syntax.RecordType:
		var fs []string
		for _, f := range n.Fields {
			fs = append(fs, f.Name+": "+p.typ(f.Type))
		}
		if p.multiline(n.Sp) {
			return "{\n  " + strings.Join(fs, ",\n  ") + ",\n}"
		}
		return "{" + strings.Join(fs, ", ") + "}"
	}
	return ""
}

// ---- blocks and statements ----

func (p *printer) block(b *syntax.Block, ind int) string {
	if len(b.Stmts) == 0 {
		inner := p.leading(b.Sp.End, ind+1)
		if inner == "" {
			return "{}"
		}
		return "{\n" + inner + pad(ind) + "}"
	}
	if len(b.Stmts) == 1 && !p.multiline(b.Sp) && !p.hasCommentIn(b.Sp) {
		return "{ " + p.stmt(b.Stmts[0], ind) + " }"
	}
	var sb strings.Builder
	sb.WriteString("{\n")
	var prev syntax.Stmt
	for _, s := range b.Stmts {
		if prev != nil && p.blankBetween(prev.Span().End, s.Span().Start) {
			sb.WriteString("\n")
		}
		sb.WriteString(p.leading(s.Span().Start, ind+1))
		sb.WriteString(pad(ind + 1))
		sb.WriteString(p.stmt(s, ind+1))
		sb.WriteString(p.trailing(s.Span().End))
		sb.WriteString("\n")
		prev = s
	}
	sb.WriteString(p.leading(b.Sp.End, ind+1))
	sb.WriteString(pad(ind) + "}")
	return sb.String()
}

func (p *printer) hasCommentIn(sp diag.Span) bool {
	for _, c := range p.comments[p.ci:] {
		if c.Sp.Start >= sp.Start && c.Sp.Start < sp.End {
			return true
		}
	}
	return false
}

func (p *printer) stmt(s syntax.Stmt, ind int) string {
	switch n := s.(type) {
	case *syntax.LetStmt:
		kw := "let "
		if n.Mutable {
			kw = "var "
		}
		out := kw + n.Name
		if n.Type != nil {
			out += ": " + p.typ(n.Type)
		}
		return out + " = " + p.expr(n.Value, ind)
	case *syntax.AssignStmt:
		return n.Name + " " + n.Op + " " + p.expr(n.Value, ind)
	case *syntax.ExprStmt:
		return p.expr(n.X, ind)
	case *syntax.ForStmt:
		return "for " + n.Var + " in " + p.expr(n.Iter, ind) + " " + p.block(n.Body, ind)
	case *syntax.WhileStmt:
		return "while " + p.expr(n.Cond, ind) + " " + p.block(n.Body, ind)
	case *syntax.AssertStmt:
		return "assert " + p.expr(n.X, ind)
	}
	return ""
}

// ---- expressions ----

func (p *printer) list(open, close string, items []string, starts []int, sp diag.Span, ml bool, ind int) string {
	if !ml || len(items) == 0 {
		return open + strings.Join(items, ", ") + close
	}
	var b strings.Builder
	b.WriteString(open + "\n")
	for i, it := range items {
		b.WriteString(p.leading(starts[i], ind+1))
		b.WriteString(pad(ind+1) + it + ",")
		if i+1 < len(starts) {
			b.WriteString(p.trailingBefore(starts[i+1]))
		} else {
			b.WriteString(p.trailingBefore(sp.End))
		}
		b.WriteString("\n")
	}
	b.WriteString(p.leading(sp.End-1, ind+1))
	b.WriteString(pad(ind) + close)
	return b.String()
}

// trailingBefore picks up a comment on the same line as the previous item, before the next one.
func (p *printer) trailingBefore(next int) string {
	if p.ci >= len(p.comments) {
		return ""
	}
	c := p.comments[p.ci]
	if c.Sp.Start >= next {
		return ""
	}
	start := strings.LastIndex(p.text[:c.Sp.Start], "\n") + 1
	if strings.TrimSpace(p.text[start:c.Sp.Start]) == "" {
		return "" // the comment has its own line; it leads the next item
	}
	p.ci++
	return " //" + c.Text
}

func continuation(op string) bool {
	switch op {
	case "|>", "&&", "||", "++":
		return true
	}
	return false
}

func (p *printer) expr(e syntax.Expr, ind int) string {
	switch n := e.(type) {
	case *syntax.IntLit:
		return n.Text
	case *syntax.FloatLit:
		return n.Text
	case *syntax.StringLit:
		return p.text[n.Sp.Start:n.Sp.End]
	case *syntax.BoolLit:
		if n.Value {
			return "true"
		}
		return "false"
	case *syntax.Ident:
		return n.Name
	case *syntax.CtorRef:
		if n.Module != "" {
			return n.Module + "." + n.Name
		}
		return n.Name
	case *syntax.Paren:
		return "(" + p.expr(n.X, ind) + ")"
	case *syntax.Field:
		return p.expr(n.X, ind) + "." + n.Name
	case *syntax.Try:
		return p.expr(n.X, ind) + "?"
	case *syntax.Unary:
		return n.Op + p.expr(n.X, ind)
	case *syntax.Binary:
		l := p.expr(n.L, ind)
		if continuation(n.Op) && strings.Contains(p.text[n.L.Span().End:n.OpSp.Start], "\n") {
			return l + "\n" + pad(ind+1) + n.Op + " " + p.expr(n.R, ind+1)
		}
		return l + " " + n.Op + " " + p.expr(n.R, ind)
	case *syntax.Call:
		var items []string
		var starts []int
		inner := ind
		if n.Multiline {
			inner = ind + 1
		}
		for _, a := range n.Args {
			items = append(items, p.expr(a, inner))
			starts = append(starts, a.Span().Start)
		}
		return p.expr(n.Fn, ind) + p.list("(", ")", items, starts, n.Sp, n.Multiline, ind)
	case *syntax.ListLit:
		var items []string
		var starts []int
		inner := ind
		if n.Multiline {
			inner = ind + 1
		}
		for _, x := range n.Elems {
			items = append(items, p.expr(x, inner))
			starts = append(starts, x.Span().Start)
		}
		return p.list("[", "]", items, starts, n.Sp, n.Multiline, ind)
	case *syntax.RecordLit:
		var items []string
		var starts []int
		inner := ind
		if n.Multiline {
			inner = ind + 1
		}
		if n.Base != nil {
			items = append(items, ".."+p.expr(n.Base, inner))
			starts = append(starts, n.Base.Span().Start)
		}
		for _, f := range n.Fields {
			items = append(items, f.Name+": "+p.expr(f.Value, inner))
			starts = append(starts, f.NameSp.Start)
		}
		return p.list("{", "}", items, starts, n.Sp, n.Multiline, ind)
	case *syntax.Block:
		return p.block(n, ind)
	case *syntax.If:
		s := "if " + p.expr(n.Cond, ind) + " " + p.block(n.Then, ind)
		if n.Else != nil {
			s += " else " + p.expr(n.Else, ind)
		}
		return s
	case *syntax.Match:
		var b strings.Builder
		b.WriteString("match " + p.expr(n.Subject, ind) + " {\n")
		for _, a := range n.Arms {
			b.WriteString(p.leading(a.Sp.Start, ind+1))
			b.WriteString(pad(ind+1) + p.pattern(a.Pat))
			if a.Guard != nil {
				b.WriteString(" if " + p.expr(a.Guard, ind+1))
			}
			b.WriteString(" -> " + p.expr(a.Body, ind+1))
			b.WriteString(p.trailing(a.Sp.End))
			b.WriteString("\n")
		}
		b.WriteString(p.leading(n.Sp.End, ind+1))
		b.WriteString(pad(ind) + "}")
		return b.String()
	case *syntax.Lambda:
		s := "fn" + p.params(n.Params, ind)
		if n.Ret != nil {
			s += " -> " + p.typ(n.Ret)
		}
		return s + " " + p.expr(n.Body, ind)
	}
	return ""
}

func (p *printer) pattern(pt syntax.Pattern) string {
	switch n := pt.(type) {
	case *syntax.PWild:
		return "_"
	case *syntax.PBind:
		return n.Name
	case *syntax.PLit:
		return n.Text
	case *syntax.PCtor:
		s := n.Name
		if n.Module != "" {
			s = n.Module + "." + s
		}
		if n.Parens {
			var as []string
			for _, a := range n.Args {
				as = append(as, p.pattern(a))
			}
			s += "(" + strings.Join(as, ", ") + ")"
		}
		return s
	case *syntax.PList:
		var as []string
		for _, a := range n.Elems {
			as = append(as, p.pattern(a))
		}
		if n.HasRest {
			as = append(as, ".."+n.Rest)
		}
		return "[" + strings.Join(as, ", ") + "]"
	}
	return ""
}
