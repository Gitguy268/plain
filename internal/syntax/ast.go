package syntax

import "github.com/Gitguy268/plain/internal/diag"

// Node is anything with a source span.
type Node interface{ Span() diag.Span }

// File is one parsed module.
type File struct {
	Src      *diag.Source
	Items    []Item
	Comments []Comment
}

// ---- items ----

type Item interface {
	Node
	item()
}

type Param struct {
	Name    string
	Sp      diag.Span
	Type    TypeExpr // may be nil in lambdas
	Default Expr     // may be nil
}

type FnDecl struct {
	Sp         diag.Span
	Pub        bool
	Name       string
	NameSp     diag.Span
	TypeParams []string
	Params     []*Param
	Ret        TypeExpr // nil means Unit
	Body       *Block
}

type ExternDecl struct {
	Sp         diag.Span
	Pub        bool
	Name       string
	NameSp     diag.Span
	TypeParams []string
	Params     []*Param
	Ret        TypeExpr
	Target     string
}

type VField struct {
	Name string // may be empty
	Type TypeExpr
}

type Variant struct {
	Sp     diag.Span
	Name   string
	Fields []VField
}

type TypeDecl struct {
	Sp       diag.Span
	Pub      bool
	Name     string
	NameSp   diag.Span
	Params   []string
	Variants []*Variant
	Alias    TypeExpr // a record type, when this is an alias
}

type ImportDecl struct {
	Sp     diag.Span
	Name   string
	NameSp diag.Span
}

type TestDecl struct {
	Sp   diag.Span
	Name string
	Body *Block
}

type LetDecl struct {
	Sp     diag.Span
	Pub    bool
	Name   string
	NameSp diag.Span
	Type   TypeExpr
	Value  Expr
}

func (n *FnDecl) Span() diag.Span     { return n.Sp }
func (n *ExternDecl) Span() diag.Span { return n.Sp }
func (n *TypeDecl) Span() diag.Span   { return n.Sp }
func (n *ImportDecl) Span() diag.Span { return n.Sp }
func (n *TestDecl) Span() diag.Span   { return n.Sp }
func (n *LetDecl) Span() diag.Span    { return n.Sp }
func (*FnDecl) item()                 {}
func (*ExternDecl) item()             {}
func (*TypeDecl) item()               {}
func (*ImportDecl) item()             {}
func (*TestDecl) item()               {}
func (*LetDecl) item()                {}

// ---- types ----

type TypeExpr interface {
	Node
	typeExpr()
}

type NamedType struct {
	Sp     diag.Span
	Module string
	Name   string
	Args   []TypeExpr
}

type FnType struct {
	Sp     diag.Span
	Params []TypeExpr
	Ret    TypeExpr // nil means Unit
}

type RField struct {
	Name string
	Type TypeExpr
}

type RecordType struct {
	Sp     diag.Span
	Fields []RField
}

func (n *NamedType) Span() diag.Span  { return n.Sp }
func (n *FnType) Span() diag.Span     { return n.Sp }
func (n *RecordType) Span() diag.Span { return n.Sp }
func (*NamedType) typeExpr()          {}
func (*FnType) typeExpr()             {}
func (*RecordType) typeExpr()         {}

// ---- statements ----

type Stmt interface {
	Node
	stmt()
}

type LetStmt struct {
	Sp      diag.Span
	Mutable bool
	Name    string // "_" discards the value
	NameSp  diag.Span
	Type    TypeExpr
	Value   Expr
}

type AssignStmt struct {
	Sp     diag.Span
	Name   string
	NameSp diag.Span
	Op     string // "=", "+=" or "-="
	Value  Expr
}

type ExprStmt struct{ X Expr }

type ForStmt struct {
	Sp    diag.Span
	Var   string
	VarSp diag.Span
	Iter  Expr
	Body  *Block
}

type WhileStmt struct {
	Sp   diag.Span
	Cond Expr
	Body *Block
}

type AssertStmt struct {
	Sp diag.Span
	X  Expr
}

func (n *LetStmt) Span() diag.Span    { return n.Sp }
func (n *AssignStmt) Span() diag.Span { return n.Sp }
func (n *ExprStmt) Span() diag.Span   { return n.X.Span() }
func (n *ForStmt) Span() diag.Span    { return n.Sp }
func (n *WhileStmt) Span() diag.Span  { return n.Sp }
func (n *AssertStmt) Span() diag.Span { return n.Sp }
func (*LetStmt) stmt()                {}
func (*AssignStmt) stmt()             {}
func (*ExprStmt) stmt()               {}
func (*ForStmt) stmt()                {}
func (*WhileStmt) stmt()              {}
func (*AssertStmt) stmt()             {}

// ---- expressions ----

type Expr interface {
	Node
	expr()
}

type IntLit struct {
	Sp   diag.Span
	Text string
}

type FloatLit struct {
	Sp   diag.Span
	Text string
}

// StrPiece is literal text or an interpolated expression.
type StrPiece struct {
	Lit string
	X   Expr
}

type StringLit struct {
	Sp    diag.Span
	Parts []StrPiece
}

type BoolLit struct {
	Sp    diag.Span
	Value bool
}

type Ident struct {
	Sp   diag.Span
	Name string
}

// CtorRef names a variant constructor, optionally from a module: Some, shapes.Circle.
type CtorRef struct {
	Sp     diag.Span
	Module string
	Name   string
}

type Call struct {
	Sp        diag.Span
	Fn        Expr
	Args      []Expr
	Multiline bool
}

type Binary struct {
	Sp   diag.Span
	Op   string
	OpSp diag.Span
	L, R Expr
}

type Unary struct {
	Sp diag.Span
	Op string
	X  Expr
}

type If struct {
	Sp   diag.Span
	Cond Expr
	Then *Block
	Else Expr // nil, *Block or *If
}

type Arm struct {
	Sp    diag.Span
	Pat   Pattern
	Guard Expr
	Body  Expr
}

type Match struct {
	Sp      diag.Span
	Subject Expr
	Arms    []*Arm
}

// Block's value is its last statement when that is an expression.
type Block struct {
	Sp    diag.Span
	Stmts []Stmt
}

type Lambda struct {
	Sp     diag.Span
	Params []*Param
	Ret    TypeExpr
	Body   Expr
}

type Field struct {
	Sp     diag.Span
	X      Expr
	Name   string
	NameSp diag.Span
}

type RecField struct {
	Name   string
	NameSp diag.Span
	Value  Expr
}

type RecordLit struct {
	Sp        diag.Span
	Base      Expr // {..base, x: 1}
	Fields    []RecField
	Multiline bool
}

type ListLit struct {
	Sp        diag.Span
	Elems     []Expr
	Multiline bool
}

type Try struct {
	Sp diag.Span
	X  Expr
}

type Paren struct {
	Sp diag.Span
	X  Expr
}

func (n *IntLit) Span() diag.Span    { return n.Sp }
func (n *FloatLit) Span() diag.Span  { return n.Sp }
func (n *StringLit) Span() diag.Span { return n.Sp }
func (n *BoolLit) Span() diag.Span   { return n.Sp }
func (n *Ident) Span() diag.Span     { return n.Sp }
func (n *CtorRef) Span() diag.Span   { return n.Sp }
func (n *Call) Span() diag.Span      { return n.Sp }
func (n *Binary) Span() diag.Span    { return n.Sp }
func (n *Unary) Span() diag.Span     { return n.Sp }
func (n *If) Span() diag.Span        { return n.Sp }
func (n *Match) Span() diag.Span     { return n.Sp }
func (n *Block) Span() diag.Span     { return n.Sp }
func (n *Lambda) Span() diag.Span    { return n.Sp }
func (n *Field) Span() diag.Span     { return n.Sp }
func (n *RecordLit) Span() diag.Span { return n.Sp }
func (n *ListLit) Span() diag.Span   { return n.Sp }
func (n *Try) Span() diag.Span       { return n.Sp }
func (n *Paren) Span() diag.Span     { return n.Sp }
func (*IntLit) expr()                {}
func (*FloatLit) expr()              {}
func (*StringLit) expr()             {}
func (*BoolLit) expr()               {}
func (*Ident) expr()                 {}
func (*CtorRef) expr()               {}
func (*Call) expr()                  {}
func (*Binary) expr()                {}
func (*Unary) expr()                 {}
func (*If) expr()                    {}
func (*Match) expr()                 {}
func (*Block) expr()                 {}
func (*Lambda) expr()                {}
func (*Field) expr()                 {}
func (*RecordLit) expr()             {}
func (*ListLit) expr()               {}
func (*Try) expr()                   {}
func (*Paren) expr()                 {}

// ---- patterns ----

type Pattern interface {
	Node
	pattern()
}

type PWild struct{ Sp diag.Span }

type PBind struct {
	Sp   diag.Span
	Name string
}

type PCtor struct {
	Sp     diag.Span
	Module string
	Name   string
	Args   []Pattern
	Parens bool
}

// PLit is an Int, String or Bool literal pattern. Text is the literal as written (Int may start with -).
type PLit struct {
	Sp   diag.Span
	Kind Kind // Int, String or Keyword (true/false)
	Text string
	Str  string // decoded value for strings
}

type PList struct {
	Sp      diag.Span
	Elems   []Pattern
	HasRest bool
	Rest    string // name bound to the rest, may be empty
	RestSp  diag.Span
}

func (n *PWild) Span() diag.Span { return n.Sp }
func (n *PBind) Span() diag.Span { return n.Sp }
func (n *PCtor) Span() diag.Span { return n.Sp }
func (n *PLit) Span() diag.Span  { return n.Sp }
func (n *PList) Span() diag.Span { return n.Sp }
func (*PWild) pattern()          {}
func (*PBind) pattern()          {}
func (*PCtor) pattern()          {}
func (*PLit) pattern()           {}
func (*PList) pattern()          {}

// BlockValue returns the block's final expression, or nil if it ends with a statement.
func BlockValue(b *Block) Expr {
	if len(b.Stmts) == 0 {
		return nil
	}
	if es, ok := b.Stmts[len(b.Stmts)-1].(*ExprStmt); ok {
		return es.X
	}
	return nil
}

func (n *Variant) Span() diag.Span { return n.Sp }
func (n *Param) Span() diag.Span   { return n.Sp }
