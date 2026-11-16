package syntax

import (
	"fmt"
	"strings"

	"github.com/Gitguy268/plain/internal/diag"
)

type bailout struct{}

type parser struct {
	src   *diag.Source
	toks  []Token
	p     int
	diags []diag.Diagnostic
}

const maxErrors = 10

// ParseFile parses a whole module.
func ParseFile(src *diag.Source) (*File, []diag.Diagnostic) {
	toks, comments, lexDiags := Lex(src, 0, len(src.Text))
	ps := &parser{src: src, toks: toks, diags: lexDiags}
	f := &File{Src: src, Comments: comments}
	ps.skipNewlines()
	for !ps.at(EOF) && len(ps.diags) < maxErrors {
		item := ps.recoverItem()
		if item != nil {
			f.Items = append(f.Items, item)
		}
		ps.skipNewlines()
	}
	return f, ps.diags
}

// ParseExprAt parses an expression from a byte range (used for text interpolation).
func parseExprAt(src *diag.Source, start, end int, outer *parser) Expr {
	toks, _, lexDiags := Lex(src, start, end)
	ps := &parser{src: src, toks: toks, diags: lexDiags}
	var e Expr
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(bailout); !ok {
					panic(r)
				}
			}
		}()
		if ps.at(EOF) {
			ps.fail(diag.Span{Start: start - 1, End: end + 1}, "EMPTY VALUE IN TEXT",
				"There is nothing between these braces.", "Put a value inside, like \"{name}\", or write \\{ for a literal brace.")
		}
		e = ps.expr()
		if !ps.at(EOF) {
			ps.fail(ps.cur().Sp, "UNEXPECTED CODE IN TEXT", "I expected the } that ends this value here.", "")
		}
	}()
	outer.diags = append(outer.diags, ps.diags...)
	if e == nil {
		e = &StringLit{Sp: diag.Span{Start: start, End: end}}
	}
	return e
}

func (ps *parser) cur() Token { return ps.toks[ps.p] }
func (ps *parser) peek(n int) Token {
	if ps.p+n < len(ps.toks) {
		return ps.toks[ps.p+n]
	}
	return ps.toks[len(ps.toks)-1]
}
func (ps *parser) at(k Kind) bool { return ps.cur().Kind == k }
func (ps *parser) isP(text string) bool {
	t := ps.cur()
	return t.Kind == Punct && t.Text == text
}
func (ps *parser) isK(text string) bool {
	t := ps.cur()
	return t.Kind == Keyword && t.Text == text
}
func (ps *parser) next() Token {
	t := ps.toks[ps.p]
	if ps.p < len(ps.toks)-1 {
		ps.p++
	}
	return t
}
func (ps *parser) prevEnd() int {
	if ps.p == 0 {
		return 0
	}
	return ps.toks[ps.p-1].Sp.End
}
func (ps *parser) skipNewlines() {
	for ps.at(Newline) {
		ps.next()
	}
}

func describe(t Token) string {
	switch t.Kind {
	case EOF:
		return "the end of the file"
	case Newline:
		return "the end of the line"
	case Lower, Upper:
		return "the name `" + t.Text + "`"
	case Int, Float:
		return "the number " + t.Text
	case String:
		return "some text"
	case Keyword:
		return "the keyword `" + t.Text + "`"
	}
	return "`" + t.Text + "`"
}

func (ps *parser) fail(sp diag.Span, title, msg, hint string) {
	ps.diags = append(ps.diags, diag.Diagnostic{Title: title, Src: ps.src, Span: sp, Message: msg, Hint: hint})
	panic(bailout{})
}

func (ps *parser) unexpected(want string, hint string) {
	t := ps.cur()
	ps.fail(t.Sp, "UNEXPECTED CODE", fmt.Sprintf("I was expecting %s, but I found %s.", want, describe(t)), hint)
}

func (ps *parser) expectP(text string, hint string) Token {
	if !ps.isP(text) {
		ps.unexpected("`"+text+"`", hint)
	}
	return ps.next()
}

func (ps *parser) expectIdent(what string) Token {
	if !ps.at(Lower) {
		hint := ""
		if ps.at(Upper) {
			hint = "Names of values and functions start with a lowercase letter. Capitalised names are for types and constructors."
		} else if ps.at(Keyword) {
			hint = "`" + ps.cur().Text + "` is a keyword in Plain, so it can't be used as a name."
		}
		ps.unexpected(what, hint)
	}
	return ps.next()
}

func (ps *parser) expectUpper(what string) Token {
	if !ps.at(Upper) {
		hint := ""
		if ps.at(Lower) {
			hint = "Type and constructor names start with a capital letter, like `Shape` or `Circle`."
		}
		ps.unexpected(what, hint)
	}
	return ps.next()
}

// ---- items ----

func (ps *parser) recoverItem() (item Item) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(bailout); !ok {
				panic(r)
			}
			item = nil
			ps.syncTop()
		}
	}()
	return ps.item()
}

// syncTop skips to the next line that starts a top-level item.
func (ps *parser) syncTop() {
	depth := 0
	for !ps.at(EOF) {
		t := ps.cur()
		if t.Kind == Punct {
			switch t.Text {
			case "{", "(", "[":
				depth++
			case "}", ")", "]":
				depth--
			}
		}
		ps.next()
		if t.Kind == Newline && depth <= 0 {
			n := ps.cur()
			if n.Kind == Keyword {
				switch n.Text {
				case "fn", "type", "pub", "import", "extern", "test", "let":
					return
				}
			}
		}
	}
}

func (ps *parser) item() Item {
	start := ps.cur().Sp.Start
	pub := false
	if ps.isK("pub") {
		ps.next()
		pub = true
	}
	t := ps.cur()
	if t.Kind == Keyword {
		switch t.Text {
		case "fn":
			return ps.fnDecl(start, pub)
		case "type":
			return ps.typeDecl(start, pub)
		case "extern":
			return ps.externDecl(start, pub)
		case "let":
			ps.next()
			name := ps.expectIdent("a name for this constant")
			d := &LetDecl{Pub: pub, Name: name.Text, NameSp: name.Sp}
			if ps.isP(":") {
				ps.next()
				d.Type = ps.typeExpr()
			}
			ps.expectP("=", "")
			d.Value = ps.expr()
			d.Sp = diag.Span{Start: start, End: ps.prevEnd()}
			ps.endLine()
			return d
		case "import":
			if pub {
				ps.fail(t.Sp, "UNEXPECTED CODE", "Imports can't be `pub`.", "")
			}
			ps.next()
			name := ps.expectIdent("the name of a module to import")
			d := &ImportDecl{Name: name.Text, NameSp: name.Sp, Sp: diag.Span{Start: start, End: name.Sp.End}}
			ps.endLine()
			return d
		case "test":
			ps.next()
			if !ps.at(String) {
				ps.unexpected("a name for the test in quotes", "For example: test \"adds numbers\" { assert 1 + 1 == 2 }")
			}
			name := ps.next()
			body := ps.block()
			return &TestDecl{Sp: diag.Span{Start: start, End: body.Sp.End}, Name: litText(name), Body: body}
		}
	}
	hint := "A Plain file is made of `fn`, `type`, `let`, `import`, `extern` and `test` declarations."
	if t.Kind == Lower && (t.Text == "def" || t.Text == "function" || t.Text == "func" || t.Text == "fun") {
		hint = "Functions in Plain start with `fn`, like: fn add(a: Int, b: Int) -> Int { a + b }"
	} else if t.Kind == Lower && (t.Text == "const" || t.Text == "val") {
		hint = "Constants in Plain use `let`, like: let limit = 10"
	} else if t.Kind == Lower && (t.Text == "class" || t.Text == "struct" || t.Text == "interface" || t.Text == "enum") {
		hint = "Plain uses `type` for records and choices, like: type Point = {x: Float, y: Float}"
	}
	ps.unexpected("a declaration", hint)
	return nil
}

func litText(t Token) string {
	var b strings.Builder
	for _, p := range t.Parts {
		if !p.IsExpr {
			b.WriteString(p.Lit)
		}
	}
	return b.String()
}

func (ps *parser) endLine() {
	if ps.at(Newline) || ps.at(EOF) || ps.isP("}") {
		return
	}
	ps.unexpected("the end of the line", "Put each declaration or statement on its own line.")
}

func (ps *parser) typeParams() []string {
	if !ps.isP("[") {
		return nil
	}
	ps.next()
	var out []string
	for {
		t := ps.expectUpper("a type parameter name like T")
		out = append(out, t.Text)
		if ps.isP(",") {
			ps.next()
			continue
		}
		break
	}
	ps.expectP("]", "")
	return out
}

func (ps *parser) params(requireTypes bool) []*Param {
	ps.expectP("(", "")
	var out []*Param
	for !ps.isP(")") {
		name := ps.cur()
		var p *Param
		if name.Kind == Underscore {
			ps.next()
			p = &Param{Name: "_", Sp: name.Sp}
		} else {
			name = ps.expectIdent("a parameter name")
			p = &Param{Name: name.Text, Sp: name.Sp}
		}
		if ps.isP(":") {
			ps.next()
			p.Type = ps.typeExpr()
		} else if requireTypes {
			ps.fail(diag.Span{Start: name.Sp.Start, End: ps.cur().Sp.Start}, "MISSING TYPE",
				fmt.Sprintf("The parameter `%s` needs a type.", name.Text),
				fmt.Sprintf("Top-level functions spell out their parameter types, so errors stay close to where they happen. For example: %s: Int", name.Text))
		}
		if ps.isP("=") {
			ps.next()
			p.Default = ps.expr()
		}
		out = append(out, p)
		if ps.isP(",") {
			ps.next()
			continue
		}
		if !ps.isP(")") {
			ps.unexpected("`,` or `)`", "")
		}
	}
	ps.next()
	return out
}

func (ps *parser) fnDecl(start int, pub bool) *FnDecl {
	ps.next() // fn
	name := ps.expectIdent("a function name")
	d := &FnDecl{Pub: pub, Name: name.Text, NameSp: name.Sp}
	d.TypeParams = ps.typeParams()
	d.Params = ps.params(true)
	if ps.isP("->") {
		ps.next()
		d.Ret = ps.typeExpr()
	} else if ps.isP(":") {
		ps.fail(ps.cur().Sp, "UNEXPECTED CODE", "The return type comes after an arrow `->`, not a colon.", "For example: fn double(n: Int) -> Int { n * 2 }")
	}
	if !ps.isP("{") {
		ps.unexpected("`{` to start the function body", "")
	}
	d.Body = ps.block()
	d.Sp = diag.Span{Start: start, End: d.Body.Sp.End}
	return d
}

func (ps *parser) externDecl(start int, pub bool) *ExternDecl {
	ps.next() // extern
	if !ps.isK("fn") {
		ps.unexpected("`fn`", "Externs look like: extern fn now() -> Float = \"Date.now\"")
	}
	ps.next()
	name := ps.expectIdent("a function name")
	d := &ExternDecl{Pub: pub, Name: name.Text, NameSp: name.Sp}
	d.TypeParams = ps.typeParams()
	d.Params = ps.params(true)
	if ps.isP("->") {
		ps.next()
		d.Ret = ps.typeExpr()
	}
	ps.expectP("=", "An extern says which JavaScript function it calls, like: = \"Math.random\"")
	if !ps.at(String) {
		ps.unexpected("the JavaScript name in quotes", "")
	}
	d.Target = litText(ps.next())
	d.Sp = diag.Span{Start: start, End: ps.prevEnd()}
	ps.endLine()
	return d
}

func (ps *parser) typeDecl(start int, pub bool) *TypeDecl {
	ps.next() // type
	name := ps.expectUpper("a type name")
	d := &TypeDecl{Pub: pub, Name: name.Text, NameSp: name.Sp}
	d.Params = ps.typeParams()
	ps.expectP("=", "")
	ps.skipNewlines()
	if ps.isP("{") {
		d.Alias = ps.typeExpr()
	} else {
		if ps.isP("|") {
			ps.next()
		}
		for {
			ps.skipNewlines()
			vt := ps.expectUpper("a variant name like `Circle`")
			v := &Variant{Name: vt.Text, Sp: vt.Sp}
			if ps.isP("(") {
				ps.next()
				for !ps.isP(")") {
					var f VField
					if ps.at(Lower) && ps.peek(1).Kind == Punct && ps.peek(1).Text == ":" {
						f.Name = ps.next().Text
						ps.next()
					}
					f.Type = ps.typeExpr()
					v.Fields = append(v.Fields, f)
					if ps.isP(",") {
						ps.next()
						continue
					}
					if !ps.isP(")") {
						ps.unexpected("`,` or `)`", "")
					}
				}
				ps.next()
				v.Sp.End = ps.prevEnd()
			}
			d.Variants = append(d.Variants, v)
			// a | may start the next line
			save := ps.p
			ps.skipNewlines()
			if ps.isP("|") {
				ps.next()
				continue
			}
			ps.p = save
			break
		}
	}
	d.Sp = diag.Span{Start: start, End: ps.prevEnd()}
	ps.endLine()
	return d
}

// ---- types ----

func (ps *parser) typeExpr() TypeExpr {
	t := ps.cur()
	start := t.Sp.Start
	switch {
	case t.Kind == Keyword && t.Text == "fn":
		ps.next()
		ps.expectP("(", "Function types look like fn(Int, Int) -> Int")
		ft := &FnType{}
		for !ps.isP(")") {
			ft.Params = append(ft.Params, ps.typeExpr())
			if ps.isP(",") {
				ps.next()
				continue
			}
			if !ps.isP(")") {
				ps.unexpected("`,` or `)`", "")
			}
		}
		ps.next()
		if ps.isP("->") {
			ps.next()
			ft.Ret = ps.typeExpr()
		}
		ft.Sp = diag.Span{Start: start, End: ps.prevEnd()}
		return ft
	case t.Kind == Punct && t.Text == "{":
		ps.next()
		rt := &RecordType{}
		ps.skipNewlines()
		for !ps.isP("}") {
			name := ps.expectIdent("a field name")
			ps.expectP(":", "Record fields look like {name: String, age: Int}")
			rt.Fields = append(rt.Fields, RField{Name: name.Text, Type: ps.typeExpr()})
			ps.skipNewlines()
			if ps.isP(",") {
				ps.next()
				ps.skipNewlines()
				continue
			}
			if !ps.isP("}") {
				ps.unexpected("`,` or `}`", "")
			}
		}
		ps.next()
		rt.Sp = diag.Span{Start: start, End: ps.prevEnd()}
		return rt
	case t.Kind == Upper || t.Kind == Lower:
		nt := &NamedType{}
		if t.Kind == Lower && ps.peek(1).Kind == Punct && ps.peek(1).Text == "." {
			nt.Module = ps.next().Text
			ps.next()
			t = ps.expectUpper("a type name")
		} else {
			ps.next()
		}
		nt.Name = t.Text
		if ps.isP("[") {
			ps.next()
			for !ps.isP("]") {
				nt.Args = append(nt.Args, ps.typeExpr())
				if ps.isP(",") {
					ps.next()
					continue
				}
				if !ps.isP("]") {
					ps.unexpected("`,` or `]`", "")
				}
			}
			ps.next()
		} else if ps.isP("<") {
			ps.fail(ps.cur().Sp, "UNEXPECTED CODE", "Type arguments go in square brackets in Plain.", "For example: List[Int] or Option[String]")
		}
		nt.Sp = diag.Span{Start: start, End: ps.prevEnd()}
		return nt
	}
	ps.unexpected("a type", "Types look like Int, String, List[Int], {x: Float} or fn(Int) -> Bool.")
	return nil
}

// ---- blocks and statements ----

func (ps *parser) block() *Block {
	start := ps.expectP("{", "").Sp.Start
	b := &Block{}
	ps.skipNewlines()
	for !ps.isP("}") {
		if ps.at(EOF) {
			ps.fail(diag.Span{Start: start, End: start + 1}, "UNFINISHED BLOCK", "This { is never closed.", "Add a matching } at the end of the block.")
		}
		if s := ps.recoverStmt(); s != nil {
			b.Stmts = append(b.Stmts, s)
		}
		if len(ps.diags) >= maxErrors {
			panic(bailout{})
		}
		ps.skipNewlines()
	}
	ps.next()
	b.Sp = diag.Span{Start: start, End: ps.prevEnd()}
	return b
}

func (ps *parser) recoverStmt() (s Stmt) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(bailout); !ok {
				panic(r)
			}
			s = nil
			// skip to the end of this line at the current depth
			depth := 0
			for !ps.at(EOF) {
				t := ps.cur()
				if depth == 0 && (t.Kind == Newline || (t.Kind == Punct && t.Text == "}")) {
					return
				}
				if t.Kind == Punct {
					switch t.Text {
					case "{", "(", "[":
						depth++
					case "}", ")", "]":
						depth--
					}
				}
				ps.next()
			}
		}
	}()
	s = ps.stmt()
	if !ps.at(Newline) && !ps.isP("}") {
		ps.unexpected("the end of the line", "Put each statement on its own line.")
	}
	return s
}

func (ps *parser) stmt() Stmt {
	t := ps.cur()
	start := t.Sp.Start
	if t.Kind == Keyword {
		switch t.Text {
		case "let", "var":
			ps.next()
			s := &LetStmt{Mutable: t.Text == "var"}
			if ps.at(Underscore) {
				u := ps.next()
				s.Name, s.NameSp = "_", u.Sp
			} else {
				name := ps.expectIdent("a name")
				s.Name, s.NameSp = name.Text, name.Sp
			}
			if ps.isP(":") {
				ps.next()
				s.Type = ps.typeExpr()
			}
			if !ps.isP("=") {
				ps.unexpected("`=`", fmt.Sprintf("Every %s needs a starting value, like: %s %s = 0", t.Text, t.Text, s.Name))
			}
			ps.next()
			s.Value = ps.expr()
			s.Sp = diag.Span{Start: start, End: ps.prevEnd()}
			return s
		case "for":
			ps.next()
			v := ps.expectIdent("a name for each item")
			if !ps.isK("in") {
				ps.unexpected("`in`", "Loops look like: for item in items { ... }")
			}
			ps.next()
			iter := ps.expr()
			body := ps.block()
			return &ForStmt{Sp: diag.Span{Start: start, End: body.Sp.End}, Var: v.Text, VarSp: v.Sp, Iter: iter, Body: body}
		case "while":
			ps.next()
			cond := ps.expr()
			body := ps.block()
			return &WhileStmt{Sp: diag.Span{Start: start, End: body.Sp.End}, Cond: cond, Body: body}
		case "assert":
			ps.next()
			x := ps.expr()
			return &AssertStmt{Sp: diag.Span{Start: start, End: ps.prevEnd()}, X: x}
		}
	}
	if t.Kind == Lower {
		n := ps.peek(1)
		if n.Kind == Punct && (n.Text == "=" || n.Text == "+=" || n.Text == "-=") {
			ps.next()
			ps.next()
			v := ps.expr()
			return &AssignStmt{Sp: diag.Span{Start: start, End: ps.prevEnd()}, Name: t.Text, NameSp: t.Sp, Op: n.Text, Value: v}
		}
	}
	if t.Kind == Lower && (t.Text == "return" || t.Text == "break" || t.Text == "continue") {
		hint := "A function's value is its last expression. To stop early on an error, use `?` on a Result."
		if t.Text != "return" {
			hint = "Plain loops always run to the end. Use list.find, list.any or a while loop with a condition instead."
		}
		ps.fail(t.Sp, "UNKNOWN WORD", fmt.Sprintf("Plain doesn't have `%s`.", t.Text), hint)
	}
	return &ExprStmt{X: ps.expr()}
}

// ---- expressions ----

var precedence = map[string]int{
	"|>": 1,
	"||": 2,
	"&&": 3,
	"==": 4, "!=": 4, "<": 4, "<=": 4, ">": 4, ">=": 4,
	"++": 5,
	"+":  6, "-": 6,
	"*": 7, "/": 7, "%": 7,
}

func (ps *parser) expr() Expr { return ps.binary(1) }

func (ps *parser) binary(minPrec int) Expr {
	left := ps.unary()
	for {
		t := ps.cur()
		if t.Kind != Punct {
			if t.Kind == Lower && (t.Text == "and" || t.Text == "or" || t.Text == "not") {
				op := map[string]string{"and": "&&", "or": "||", "not": "!"}[t.Text]
				ps.fail(t.Sp, "UNKNOWN WORD", fmt.Sprintf("Plain doesn't use `%s` as an operator.", t.Text), fmt.Sprintf("Write %s instead.", op))
			}
			return left
		}
		prec, ok := precedence[t.Text]
		if t.Text == "=" && minPrec <= 4 {
			ps.fail(t.Sp, "UNEXPECTED CODE", "A single = gives a variable a new value; it can't be used inside an expression.", "To compare two values, use ==.")
		}
		if t.Text == "===" || t.Text == "!==" {
			ps.fail(t.Sp, "UNKNOWN OPERATOR", "Plain has only one equality operator.", "Use == (or !=). It always compares contents and both sides must have the same type.")
		}
		if !ok || prec < minPrec {
			return left
		}
		ps.next()
		ps.skipNewlines()
		right := ps.binary(prec + 1)
		left = &Binary{Sp: diag.Join(left.Span(), right.Span()), Op: t.Text, OpSp: t.Sp, L: left, R: right}
		if prec == 4 {
			if n := ps.cur(); n.Kind == Punct && precedence[n.Text] == 4 {
				ps.fail(n.Sp, "CHAINED COMPARISON", "Comparisons can't be chained like this.",
					"Use && to combine them, for example: 0 < x && x < 10")
			}
		}
	}
}

func (ps *parser) unary() Expr {
	t := ps.cur()
	if t.Kind == Punct && (t.Text == "-" || t.Text == "!") {
		ps.next()
		x := ps.unary()
		return &Unary{Sp: diag.Join(t.Sp, x.Span()), Op: t.Text, X: x}
	}
	return ps.postfix(ps.primary())
}

func (ps *parser) callArgs() ([]Expr, bool) {
	open := ps.next() // (
	var args []Expr
	for !ps.isP(")") {
		args = append(args, ps.expr())
		if ps.isP(",") {
			ps.next()
			continue
		}
		if !ps.isP(")") {
			ps.unexpected("`,` or `)`", "")
		}
	}
	close := ps.next()
	return args, strings.Contains(ps.src.Text[open.Sp.Start:close.Sp.End], "\n")
}

func (ps *parser) postfix(x Expr) Expr {
	for {
		t := ps.cur()
		if t.Kind != Punct {
			return x
		}
		switch t.Text {
		case "(":
			args, ml := ps.callArgs()
			x = &Call{Sp: diag.Span{Start: x.Span().Start, End: ps.prevEnd()}, Fn: x, Args: args, Multiline: ml}
		case ".":
			ps.next()
			n := ps.cur()
			if n.Kind == Upper {
				id, ok := x.(*Ident)
				if !ok {
					ps.unexpected("a field name", "")
				}
				ps.next()
				x = &CtorRef{Sp: diag.Join(id.Sp, n.Sp), Module: id.Name, Name: n.Text}
				continue
			}
			if n.Kind == Int {
				ps.fail(n.Sp, "UNEXPECTED CODE", "Plain has no tuples, so values can't be read by position.", "Use a record with named fields, like {first: 1, second: 2}, and read p.first.")
			}
			name := ps.expectIdent("a field name")
			x = &Field{Sp: diag.Join(x.Span(), name.Sp), X: x, Name: name.Text, NameSp: name.Sp}
		case "?":
			ps.next()
			x = &Try{Sp: diag.Join(x.Span(), t.Sp), X: x}
		case "[":
			if ps.toks[ps.p-1].Sp.End == t.Sp.Start {
				ps.fail(t.Sp, "NO INDEXING", "Plain doesn't read list items with xs[i], because the index might not exist.",
					"Use list.get(xs, i). It gives an Option, so you decide what happens when the index is out of range.")
			}
			return x
		default:
			return x
		}
	}
}

func (ps *parser) primary() Expr {
	t := ps.cur()
	switch t.Kind {
	case Int:
		ps.next()
		return &IntLit{Sp: t.Sp, Text: t.Text}
	case Float:
		ps.next()
		return &FloatLit{Sp: t.Sp, Text: t.Text}
	case String:
		ps.next()
		s := &StringLit{Sp: t.Sp}
		for _, p := range t.Parts {
			if p.IsExpr {
				s.Parts = append(s.Parts, StrPiece{X: parseExprAt(ps.src, p.Start, p.End, ps)})
			} else {
				s.Parts = append(s.Parts, StrPiece{Lit: p.Lit})
			}
		}
		return s
	case Lower:
		ps.next()
		switch t.Text {
		case "null", "nil", "undefined", "None_":
			ps.fail(t.Sp, "NO NULL", fmt.Sprintf("Plain has no `%s`.", t.Text),
				"A value that might be missing has the type Option[T]. Write None for \"no value\" and Some(x) for a value, and use match to handle both.")
		}
		return &Ident{Sp: t.Sp, Name: t.Text}
	case Upper:
		ps.next()
		return &CtorRef{Sp: t.Sp, Name: t.Text}
	case Underscore:
		ps.fail(t.Sp, "UNEXPECTED CODE", "_ can only be used in patterns and to ignore a value with let _ = ...", "")
	case Keyword:
		switch t.Text {
		case "true", "false":
			ps.next()
			return &BoolLit{Sp: t.Sp, Value: t.Text == "true"}
		case "if":
			return ps.ifExpr()
		case "match":
			return ps.matchExpr()
		case "fn":
			return ps.lambda()
		}
	case Punct:
		switch t.Text {
		case "(":
			ps.next()
			if ps.isP(")") {
				ps.fail(diag.Join(t.Sp, ps.cur().Sp), "UNEXPECTED CODE", "Plain has no () value.", "A function that returns nothing just leaves out the -> part. An empty block {} also means \"nothing\".")
			}
			x := ps.expr()
			if ps.isP(",") {
				ps.fail(ps.cur().Sp, "UNEXPECTED CODE", "Plain has no tuples.", "Use a record instead, like {x: 1, y: 2}.")
			}
			ps.expectP(")", "")
			return &Paren{Sp: diag.Span{Start: t.Sp.Start, End: ps.prevEnd()}, X: x}
		case "[":
			ps.next()
			l := &ListLit{}
			for !ps.isP("]") {
				l.Elems = append(l.Elems, ps.expr())
				if ps.isP(",") {
					ps.next()
					continue
				}
				if !ps.isP("]") {
					ps.unexpected("`,` or `]`", "")
				}
			}
			close := ps.next()
			l.Sp = diag.Span{Start: t.Sp.Start, End: close.Sp.End}
			l.Multiline = strings.Contains(ps.src.Text[l.Sp.Start:l.Sp.End], "\n")
			return l
		case "{":
			if ps.looksLikeRecord() {
				return ps.recordLit()
			}
			return ps.block()
		}
	}
	ps.unexpected("a value", "")
	return nil
}

func (ps *parser) looksLikeRecord() bool {
	i := 1
	for ps.peek(i).Kind == Newline {
		i++
	}
	a, b := ps.peek(i), ps.peek(i+1)
	if a.Kind == Punct && a.Text == ".." {
		return true
	}
	return a.Kind == Lower && b.Kind == Punct && b.Text == ":"
}

func (ps *parser) recordLit() Expr {
	open := ps.next()
	r := &RecordLit{}
	ps.skipNewlines()
	if ps.isP("..") {
		ps.next()
		r.Base = ps.expr()
		ps.skipNewlines()
		if ps.isP(",") {
			ps.next()
			ps.skipNewlines()
		}
	}
	for !ps.isP("}") {
		name := ps.expectIdent("a field name")
		ps.expectP(":", "Record fields look like {name: \"Ann\", age: 30}")
		v := ps.expr()
		r.Fields = append(r.Fields, RecField{Name: name.Text, NameSp: name.Sp, Value: v})
		ps.skipNewlines()
		if ps.isP(",") {
			ps.next()
			ps.skipNewlines()
			continue
		}
		if !ps.isP("}") {
			ps.unexpected("`,` or `}`", "")
		}
	}
	close := ps.next()
	r.Sp = diag.Span{Start: open.Sp.Start, End: close.Sp.End}
	r.Multiline = strings.Contains(ps.src.Text[r.Sp.Start:r.Sp.End], "\n")
	return r
}

func (ps *parser) ifExpr() Expr {
	start := ps.next().Sp.Start
	cond := ps.expr()
	if !ps.isP("{") {
		ps.unexpected("`{` after the condition", "")
	}
	then := ps.block()
	n := &If{Cond: cond, Then: then}
	end := then.Sp.End
	if ps.isK("else") {
		ps.next()
		if ps.isK("if") {
			n.Else = ps.ifExpr()
		} else if ps.isP("{") {
			n.Else = ps.block()
		} else {
			ps.unexpected("`{` or `if` after else", "")
		}
		end = n.Else.Span().End
	}
	n.Sp = diag.Span{Start: start, End: end}
	return n
}

func (ps *parser) matchExpr() Expr {
	start := ps.next().Sp.Start
	subj := ps.expr()
	ps.expectP("{", "")
	m := &Match{Subject: subj}
	ps.skipNewlines()
	for !ps.isP("}") {
		if ps.at(EOF) {
			ps.fail(diag.Span{Start: start, End: start + 5}, "UNFINISHED MATCH", "This match is never closed with }.", "")
		}
		pat := ps.pattern()
		arm := &Arm{Pat: pat}
		if ps.isK("if") {
			ps.next()
			arm.Guard = ps.expr()
		}
		if !ps.isP("->") {
			hint := ""
			if ps.isP("=") || (ps.isP("=") && ps.peek(1).Text == ">") {
				hint = "Plain uses -> in match arms, like: Some(x) -> x"
			}
			ps.unexpected("`->`", hint)
		}
		ps.next()
		ps.skipNewlines()
		arm.Body = ps.expr()
		arm.Sp = diag.Join(pat.Span(), arm.Body.Span())
		m.Arms = append(m.Arms, arm)
		if ps.isP(",") {
			ps.next()
		}
		if !ps.at(Newline) && !ps.isP("}") {
			ps.unexpected("a new line before the next case", "")
		}
		ps.skipNewlines()
	}
	close := ps.next()
	m.Sp = diag.Span{Start: start, End: close.Sp.End}
	return m
}

func (ps *parser) lambda() Expr {
	start := ps.next().Sp.Start // fn
	if ps.at(Lower) {
		ps.fail(ps.cur().Sp, "NAMED FUNCTION IN EXPRESSION", "Functions inside other code have no name.",
			"Write fn(x) x + 1, and give it a name with let if you need one: let add_one = fn(x) x + 1")
	}
	l := &Lambda{Params: ps.params(false)}
	if ps.isP("->") {
		ps.next()
		l.Ret = ps.typeExpr()
		if !ps.isP("{") {
			ps.unexpected("`{`", "When a function value has a return type, its body goes in braces.")
		}
	}
	if ps.isP("{") {
		l.Body = ps.block()
	} else {
		l.Body = ps.expr()
	}
	l.Sp = diag.Span{Start: start, End: l.Body.Span().End}
	return l
}

// ---- patterns ----

func (ps *parser) pattern() Pattern {
	t := ps.cur()
	switch t.Kind {
	case Underscore:
		ps.next()
		return &PWild{Sp: t.Sp}
	case Lower:
		ps.next()
		if ps.isP(".") && ps.peek(1).Kind == Upper {
			ps.next()
			return ps.ctorPattern(t.Sp.Start, t.Text)
		}
		return &PBind{Sp: t.Sp, Name: t.Text}
	case Upper:
		return ps.ctorPattern(t.Sp.Start, "")
	case Int:
		ps.next()
		return &PLit{Sp: t.Sp, Kind: Int, Text: t.Text}
	case Float:
		ps.fail(t.Sp, "FLOAT PATTERN", "Decimal numbers can't be used as patterns, because tiny rounding differences would make them miss.",
			"Use a guard instead, like: x if x > 0.5 -> ...")
	case String:
		ps.next()
		for _, p := range t.Parts {
			if p.IsExpr {
				ps.fail(t.Sp, "TEXT PATTERN", "Text in a pattern can't contain {values}.", "")
			}
		}
		return &PLit{Sp: t.Sp, Kind: String, Text: t.Text, Str: litText(t)}
	case Keyword:
		if t.Text == "true" || t.Text == "false" {
			ps.next()
			return &PLit{Sp: t.Sp, Kind: Keyword, Text: t.Text}
		}
	case Punct:
		if t.Text == "-" && ps.peek(1).Kind == Int {
			ps.next()
			n := ps.next()
			return &PLit{Sp: diag.Join(t.Sp, n.Sp), Kind: Int, Text: "-" + n.Text}
		}
		if t.Text == "[" {
			ps.next()
			l := &PList{}
			for !ps.isP("]") {
				if ps.isP("..") {
					d := ps.next()
					l.HasRest = true
					l.RestSp = d.Sp
					if ps.at(Lower) {
						r := ps.next()
						l.Rest = r.Text
						l.RestSp = diag.Join(d.Sp, r.Sp)
					}
					if !ps.isP("]") {
						ps.unexpected("`]`", "The ..rest part must come last in a list pattern.")
					}
					break
				}
				l.Elems = append(l.Elems, ps.pattern())
				if ps.isP(",") {
					ps.next()
					continue
				}
				if !ps.isP("]") {
					ps.unexpected("`,` or `]`", "")
				}
			}
			close := ps.next()
			l.Sp = diag.Span{Start: t.Sp.Start, End: close.Sp.End}
			return l
		}
	}
	ps.unexpected("a pattern", "Patterns look like Some(x), None, [first, ..rest], 0, \"text\" or _.")
	return nil
}

func (ps *parser) ctorPattern(start int, module string) Pattern {
	name := ps.expectUpper("a constructor name")
	p := &PCtor{Module: module, Name: name.Text}
	if ps.isP("(") {
		ps.next()
		p.Parens = true
		for !ps.isP(")") {
			p.Args = append(p.Args, ps.pattern())
			if ps.isP(",") {
				ps.next()
				continue
			}
			if !ps.isP(")") {
				ps.unexpected("`,` or `)`", "")
			}
		}
		ps.next()
	}
	p.Sp = diag.Span{Start: start, End: ps.prevEnd()}
	return p
}
