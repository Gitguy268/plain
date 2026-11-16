// Package diag holds source files, spans and the friendly error renderer.
package diag

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// Source is one file of Plain code.
type Source struct {
	Name  string
	Text  string
	lines []int // byte offset where each line starts
}

// NewSource wraps a file's text so offsets can be turned into lines.
func NewSource(name, text string) *Source {
	s := &Source{Name: name, Text: text, lines: []int{0}}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			s.lines = append(s.lines, i+1)
		}
	}
	return s
}

// LineCol returns the 1-based line and column (in characters) of a byte offset.
func (s *Source) LineCol(off int) (int, int) {
	if off > len(s.Text) {
		off = len(s.Text)
	}
	i := sort.Search(len(s.lines), func(i int) bool { return s.lines[i] > off }) - 1
	if i < 0 {
		i = 0
	}
	return i + 1, utf8.RuneCountInString(s.Text[s.lines[i]:off]) + 1
}

// Line returns the text of a 1-based line without its newline.
func (s *Source) Line(n int) string {
	if n < 1 || n > len(s.lines) {
		return ""
	}
	start := s.lines[n-1]
	end := len(s.Text)
	if n < len(s.lines) {
		end = s.lines[n] - 1
	}
	return strings.TrimRight(s.Text[start:end], "\r")
}

// Loc formats an offset as file:line:col.
func (s *Source) Loc(off int) string {
	l, c := s.LineCol(off)
	return fmt.Sprintf("%s:%d:%d", s.Name, l, c)
}

// Span is a byte range inside one source.
type Span struct{ Start, End int }

// Join returns a span covering both.
func Join(a, b Span) Span {
	if b.Start < a.Start {
		a.Start = b.Start
	}
	if b.End > a.End {
		a.End = b.End
	}
	return a
}

// Severity says whether a diagnostic stops compilation.
type Severity int

const (
	Error Severity = iota
	Warning
)

// Diagnostic is one problem found in the code.
type Diagnostic struct {
	Severity Severity
	Title    string // short heading in capitals, e.g. "TYPE MISMATCH"
	Src      *Source
	Span     Span
	Message  string // paragraphs separated by blank lines
	Hint     string
}

// Pos returns file:line:col for the diagnostic.
func (d Diagnostic) Pos() string {
	if d.Src == nil {
		return ""
	}
	return d.Src.Loc(d.Span.Start)
}

const width = 72

type palette struct{ title, mark, hint, dim, reset string }

func colors(on bool, sev Severity) palette {
	if !on {
		return palette{}
	}
	mark := "\x1b[31m"
	if sev == Warning {
		mark = "\x1b[33m"
	}
	return palette{title: "\x1b[36m", mark: mark, hint: "\x1b[32m", dim: "\x1b[2m", reset: "\x1b[0m"}
}

// Render turns a diagnostic into Elm-style text: heading, code with carets, message, hint.
func Render(d Diagnostic, color bool) string {
	p := colors(color, d.Severity)
	var b strings.Builder
	title := d.Title
	if d.Severity == Warning {
		title = "WARNING: " + title
	}
	pos := d.Pos()
	fill := width - len(title) - len(pos) - 5
	if fill < 3 {
		fill = 3
	}
	fmt.Fprintf(&b, "%s-- %s %s %s%s\n\n", p.title, title, strings.Repeat("-", fill), pos, p.reset)
	if d.Src != nil {
		b.WriteString(snippet(d, p))
		b.WriteString("\n")
	}
	b.WriteString(wrap(d.Message))
	b.WriteString("\n")
	if d.Hint != "" {
		b.WriteString("\n")
		hint := wrap(d.Hint)
		lines := strings.Split(hint, "\n")
		for i, l := range lines {
			if i == 0 {
				fmt.Fprintf(&b, "%sHint:%s %s\n", p.hint, p.reset, l)
			} else if l == "" {
				b.WriteString("\n")
			} else {
				fmt.Fprintf(&b, "      %s\n", l)
			}
		}
	}
	return b.String()
}

func snippet(d Diagnostic, p palette) string {
	src := d.Src
	l1, _ := src.LineCol(d.Span.Start)
	end := d.Span.End
	if end > d.Span.Start {
		end--
	}
	l2, _ := src.LineCol(end)
	if l2 > l1+2 {
		l2 = l1 + 2
	}
	gutter := len(fmt.Sprint(l2))
	var b strings.Builder
	for ln := l1; ln <= l2; ln++ {
		raw := src.Line(ln)
		text := strings.ReplaceAll(raw, "\t", "    ")
		fmt.Fprintf(&b, "%s%*d |%s %s\n", p.dim, gutter, ln, p.reset, text)
		// columns of the underline on this line, in display cells
		lineStart := src.lines[ln-1]
		from, to := 0, len(raw)
		if ln == l1 {
			from = d.Span.Start - lineStart
		}
		if ln == l2 && d.Span.End-lineStart <= len(raw) {
			to = d.Span.End - lineStart
		}
		if from > len(raw) {
			from = len(raw)
		}
		if to < from {
			to = from
		}
		c1 := cells(raw[:from])
		c2 := cells(raw[:to])
		if c2 <= c1 {
			c2 = c1 + 1
		}
		fmt.Fprintf(&b, "%s%s |%s %s%s%s%s\n", p.dim, strings.Repeat(" ", gutter), p.reset,
			strings.Repeat(" ", c1), p.mark, strings.Repeat("^", c2-c1), p.reset)
	}
	return b.String()
}

func cells(s string) int {
	n := 0
	for _, r := range s {
		if r == '\t' {
			n += 4
		} else {
			n++
		}
	}
	return n
}

// wrap breaks long paragraphs at the page width. Lines starting with spaces are kept as they are.
func wrap(text string) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, " ") || len(line) <= width {
			out = append(out, line)
			continue
		}
		cur := ""
		for _, w := range strings.Fields(line) {
			if cur != "" && len(cur)+1+len(w) > width {
				out = append(out, cur)
				cur = w
			} else if cur == "" {
				cur = w
			} else {
				cur += " " + w
			}
		}
		out = append(out, cur)
	}
	return strings.Join(out, "\n")
}

// Sort orders diagnostics by file and position.
func Sort(ds []Diagnostic) {
	sort.SliceStable(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		an, bn := "", ""
		if a.Src != nil {
			an = a.Src.Name
		}
		if b.Src != nil {
			bn = b.Src.Name
		}
		if an != bn {
			return an < bn
		}
		return a.Span.Start < b.Span.Start
	})
}

// RenderAll renders every diagnostic, errors first in source order, with a count at the end.
func RenderAll(ds []Diagnostic, color bool) string {
	Sort(ds)
	var b strings.Builder
	errs := 0
	for i, d := range ds {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(Render(d, color))
		if d.Severity == Error {
			errs++
		}
	}
	if errs > 1 {
		fmt.Fprintf(&b, "\nFound %d problems.\n", errs)
	}
	return b.String()
}

// HasErrors reports whether any diagnostic is an error.
func HasErrors(ds []Diagnostic) bool {
	for _, d := range ds {
		if d.Severity == Error {
			return true
		}
	}
	return false
}
