// Package syntax turns Plain source into an AST.
package syntax

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Gitguy268/plain/internal/diag"
)

// Kind is a token kind.
type Kind int

const (
	EOF Kind = iota
	Newline
	Lower
	Upper
	Int
	Float
	String
	Underscore
	Punct
	Keyword
)

// Token is one lexed token.
type Token struct {
	Kind  Kind
	Text  string // identifier, keyword or punctuation text
	Sp    diag.Span
	Parts []StrPart // for strings
}

// StrPart is a piece of a string literal: plain text or an interpolated expression.
type StrPart struct {
	Lit    string
	IsExpr bool
	Start  int // byte range of the expression source
	End    int
}

// Comment is a // comment kept for the formatter.
type Comment struct {
	Text string // without the leading //
	Sp   diag.Span
}

var keywords = map[string]bool{
	"fn": true, "let": true, "var": true, "if": true, "else": true, "match": true,
	"type": true, "pub": true, "import": true, "extern": true, "test": true,
	"assert": true, "true": true, "false": true, "for": true, "in": true, "while": true,
}

var puncts2 = []string{"|>", "->", "==", "!=", "<=", ">=", "&&", "||", "++", "..", "+=", "-="}

const puncts1 = "+-*/%<>=!(){}[],:.?|;"

type lexer struct {
	src      *diag.Source
	text     string
	pos, end int
	toks     []Token
	comments []Comment
	diags    []diag.Diagnostic
}

// Lex splits source[start:end] into tokens. Newlines that can't end a statement are dropped.
func Lex(src *diag.Source, start, end int) ([]Token, []Comment, []diag.Diagnostic) {
	lx := &lexer{src: src, text: src.Text, pos: start, end: end}
	lx.run()
	return filterNewlines(lx.toks), lx.comments, lx.diags
}

func (lx *lexer) errorf(sp diag.Span, title, msg, hint string) {
	lx.diags = append(lx.diags, diag.Diagnostic{Title: title, Src: lx.src, Span: sp, Message: msg, Hint: hint})
}

func (lx *lexer) emit(k Kind, text string, start int) {
	lx.toks = append(lx.toks, Token{Kind: k, Text: text, Sp: diag.Span{Start: start, End: lx.pos}})
}

func isLetter(c byte) bool { return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isDigit(c byte) bool  { return c >= '0' && c <= '9' }

func (lx *lexer) run() {
	for lx.pos < lx.end {
		c := lx.text[lx.pos]
		start := lx.pos
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			lx.pos++
		case c == '\n':
			lx.pos++
			lx.emit(Newline, "\n", start)
		case c == '/' && lx.pos+1 < lx.end && lx.text[lx.pos+1] == '/':
			for lx.pos < lx.end && lx.text[lx.pos] != '\n' {
				lx.pos++
			}
			lx.comments = append(lx.comments, Comment{Text: strings.TrimRight(lx.text[start+2:lx.pos], " \r"), Sp: diag.Span{Start: start, End: lx.pos}})
		case isDigit(c):
			lx.number()
		case isLetter(c):
			for lx.pos < lx.end && (isLetter(lx.text[lx.pos]) || isDigit(lx.text[lx.pos])) {
				lx.pos++
			}
			word := lx.text[start:lx.pos]
			switch {
			case word == "_":
				lx.emit(Underscore, word, start)
			case keywords[word]:
				lx.emit(Keyword, word, start)
			case c >= 'A' && c <= 'Z':
				lx.emit(Upper, word, start)
			default:
				lx.emit(Lower, word, start)
			}
		case c == '"':
			lx.str()
		default:
			matched := false
			for _, p := range puncts2 {
				if strings.HasPrefix(lx.text[lx.pos:lx.end], p) {
					lx.pos += 2
					lx.emit(Punct, p, start)
					matched = true
					break
				}
			}
			if matched {
				continue
			}
			if strings.IndexByte(puncts1, c) >= 0 {
				lx.pos++
				if c == ';' {
					lx.emit(Newline, ";", start)
				} else {
					lx.emit(Punct, string(c), start)
				}
				continue
			}
			r, size := utf8.DecodeRuneInString(lx.text[lx.pos:])
			lx.pos += size
			hint := ""
			switch r {
			case '\'':
				hint = "Text in Plain always uses double quotes: \"like this\"."
			case '#':
				hint = "Comments in Plain start with //."
			case '&':
				hint = "For \"and\" use &&."
			}
			lx.errorf(diag.Span{Start: start, End: lx.pos}, "UNEXPECTED CHARACTER",
				fmt.Sprintf("I don't know what to do with the character %q here.", r), hint)
		}
	}
	lx.toks = append(lx.toks, Token{Kind: EOF, Sp: diag.Span{Start: lx.end, End: lx.end}})
}

func (lx *lexer) number() {
	start := lx.pos
	digits := func() {
		for lx.pos < lx.end && (isDigit(lx.text[lx.pos]) || lx.text[lx.pos] == '_') {
			lx.pos++
		}
	}
	digits()
	kind := Int
	if lx.pos+1 < lx.end && lx.text[lx.pos] == '.' && isDigit(lx.text[lx.pos+1]) {
		kind = Float
		lx.pos++
		digits()
	}
	if lx.pos < lx.end && (lx.text[lx.pos] == 'e' || lx.text[lx.pos] == 'E') {
		save := lx.pos
		lx.pos++
		if lx.pos < lx.end && (lx.text[lx.pos] == '+' || lx.text[lx.pos] == '-') {
			lx.pos++
		}
		if lx.pos < lx.end && isDigit(lx.text[lx.pos]) {
			kind = Float
			digits()
		} else {
			lx.pos = save
		}
	}
	lx.emit(kind, lx.text[start:lx.pos], start)
}

func (lx *lexer) str() {
	start := lx.pos
	lx.pos++ // opening quote
	var parts []StrPart
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			parts = append(parts, StrPart{Lit: lit.String()})
			lit.Reset()
		}
	}
	for {
		if lx.pos >= lx.end || lx.text[lx.pos] == '\n' {
			lx.errorf(diag.Span{Start: start, End: lx.pos}, "UNFINISHED TEXT",
				"This text starts with \" but never ends.", "Add a closing \" on the same line.")
			break
		}
		c := lx.text[lx.pos]
		if c == '"' {
			lx.pos++
			break
		}
		if c == '\\' && lx.pos+1 < lx.end {
			e := lx.text[lx.pos+1]
			lx.pos += 2
			switch e {
			case 'n':
				lit.WriteByte('\n')
			case 't':
				lit.WriteByte('\t')
			case 'r':
				lit.WriteByte('\r')
			case '\\', '"', '{', '}':
				lit.WriteByte(e)
			case 'u':
				if lx.pos < lx.end && lx.text[lx.pos] == '{' {
					close := strings.IndexByte(lx.text[lx.pos:lx.end], '}')
					var r rune
					if close > 0 {
						if _, err := fmt.Sscanf(lx.text[lx.pos+1:lx.pos+close], "%x", &r); err == nil {
							lit.WriteRune(r)
							lx.pos += close + 1
							continue
						}
					}
				}
				lx.errorf(diag.Span{Start: lx.pos - 2, End: lx.pos}, "BAD ESCAPE", "A \\u escape needs hex digits in braces.", "For example \\u{1F600}.")
			default:
				lx.errorf(diag.Span{Start: lx.pos - 2, End: lx.pos}, "BAD ESCAPE",
					fmt.Sprintf("I don't know the escape \\%c.", e), "The escapes are \\n \\t \\r \\\\ \\\" \\{ \\} and \\u{...}.")
			}
			continue
		}
		if c == '{' {
			flush()
			exprStart := lx.pos + 1
			depth := 0
			i := exprStart
			for ; i < lx.end && lx.text[i] != '\n'; i++ {
				ch := lx.text[i]
				if ch == '"' { // skip a nested string
					i++
					for i < lx.end && lx.text[i] != '"' && lx.text[i] != '\n' {
						if lx.text[i] == '\\' {
							i++
						}
						i++
					}
					continue
				}
				if ch == '{' {
					depth++
				} else if ch == '}' {
					if depth == 0 {
						break
					}
					depth--
				}
			}
			if i >= lx.end || lx.text[i] != '}' {
				lx.errorf(diag.Span{Start: lx.pos, End: lx.pos + 1}, "UNFINISHED TEXT",
					"This { starts a value inside the text, but I can't find the } that ends it.",
					"Write \\{ if you want a literal { in your text.")
				lx.pos = i
				continue
			}
			parts = append(parts, StrPart{IsExpr: true, Start: exprStart, End: i})
			lx.pos = i + 1
			continue
		}
		lit.WriteByte(c)
		lx.pos++
	}
	flush()
	lx.toks = append(lx.toks, Token{Kind: String, Text: lx.text[start:lx.pos], Sp: diag.Span{Start: start, End: lx.pos}, Parts: parts})
}

func canEnd(t Token) bool {
	switch t.Kind {
	case Lower, Upper, Int, Float, String, Underscore:
		return true
	case Keyword:
		return t.Text == "true" || t.Text == "false"
	case Punct:
		return t.Text == ")" || t.Text == "]" || t.Text == "}" || t.Text == "?"
	}
	return false
}

func continues(t Token) bool {
	if t.Kind == Keyword {
		return t.Text == "else"
	}
	if t.Kind == Punct {
		switch t.Text {
		case "|>", ".", "&&", "||", "++", ")", "]":
			return true
		}
	}
	return false
}

// filterNewlines keeps a newline only where it can end a statement: not inside ( or [,
// not after an operator, and not before a line that continues the expression.
func filterNewlines(in []Token) []Token {
	out := make([]Token, 0, len(in))
	var stack []string
	for i, t := range in {
		if t.Kind == Punct {
			switch t.Text {
			case "(", "[", "{":
				stack = append(stack, t.Text)
			case ")", "]", "}":
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
			}
		}
		if t.Kind != Newline {
			out = append(out, t)
			continue
		}
		if len(stack) > 0 && stack[len(stack)-1] != "{" {
			continue
		}
		if len(out) == 0 || out[len(out)-1].Kind == Newline || !canEnd(out[len(out)-1]) {
			if t.Text == ";" && len(out) > 0 && out[len(out)-1].Kind != Newline {
				out = append(out, t)
			}
			continue
		}
		j := i + 1
		for j < len(in) && in[j].Kind == Newline {
			j++
		}
		if j < len(in) && continues(in[j]) {
			continue
		}
		out = append(out, t)
	}
	return out
}
