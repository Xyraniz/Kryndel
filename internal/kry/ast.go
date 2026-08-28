package kry

type TypeSpec struct {
	Name   string
	Params []*TypeSpec
	Tok    Token
}
type ExprKind int

const (
	ExInt ExprKind = iota
	ExFloat
	ExBool
	ExNil
	ExString
	ExVar
	ExUnary
	ExBinary
	ExCall
	ExArray
	ExIndex
	ExField
	ExStruct
	ExEnum
	ExMap
	ExSet
	ExPropagate
)

type Expr struct {
	Kind                  ExprKind
	Tok                   Token
	Int                   int64
	Float                 float64
	Bool                  bool
	Str                   string
	Name                  string
	Op                    TokenKind
	Left, Right, Operand  *Expr
	Args                  []*Expr
	Items                 []*Expr
	Base                  *Expr
	Field                 string
	Receiver              *Expr
	MapKeys               []*Expr
	StructName            string
	Fields                []string
	Values                []*Expr
	EnumType, EnumVariant string
	Type                  *Type
}
type StmtKind int

const (
	StLet StmtKind = iota
	StExpr
	StAssign
	StIf
	StWhile
	StReturn
	StBreak
	StContinue
	StMatch
	StFor
	StDefer
	StUnsafe
)

type Stmt struct {
	Kind          StmtKind
	Tok           Token
	Name          string
	Mutable       bool
	Annotation    *TypeSpec
	Init          *Expr
	Expr          *Expr
	Target, Value *Expr
	Cond          *Expr
	Then, Else    []*Stmt
	Body          []*Stmt
	Iter          *Expr
	Return        *Expr
	Scrutinee     *Expr
	Arms          []MatchArm
}
type PatternKind int

const (
	PatWildcard PatternKind = iota
	PatNil
	PatBool
	PatInt
	PatString
	PatEnum
	PatOption
	PatResult
)

type Pattern struct {
	Kind                       PatternKind
	Tok                        Token
	Bool                       bool
	Int                        int64
	Str                        string
	TypeName, Variant, Binding string
	Present                    bool
	OK                         bool
}
type MatchArm struct {
	Pattern Pattern
	Body    []*Stmt
}
type Param struct {
	Name string
	Type *TypeSpec
	Tok  Token
}
type Function struct {
	Name     string
	Public   bool
	Worker   bool
	Params   []Param
	Return   *TypeSpec
	Receiver *TypeSpec
	Unsafe   bool
	Body     []*Stmt
	Tok      Token
	Module   string
}
type FieldDecl struct {
	Name string
	Spec *TypeSpec
	Tok  Token
	Type *Type
}
type StructDecl struct {
	Name   string
	Public bool
	Fields []FieldDecl
	Tok    Token
	Module string
	Type   *Type
}
type EnumDecl struct {
	Name     string
	Public   bool
	Variants []string
	Tok      Token
	Module   string
	Type     *Type
}
type ImportDecl struct {
	Path string
	Tok  Token
}
type Program struct {
	Source     *Source
	Statements []*Stmt
	Functions  []*Function
	Structs    []*StructDecl
	Enums      []*EnumDecl
	Imports    []ImportDecl
	Sources    []*Source
	Module     string
}
