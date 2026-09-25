package kry

type TypeSpec struct {
	Name   string
	Params []*TypeSpec
	Tok    Token
}

type TypeParam struct {
	Name       string
	Constraint string
	Tok        Token
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
	NameToken             Token
	Definition            Token
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
	FieldTokens           []Token
	Values                []*Expr
	EnumType, EnumVariant string
	VariantToken          Token
	VariantDefinition     Token
	Type                  *Type
	Function              *Function
	ConstValue            *Value
	Tail                  bool
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
	StConst
)

type Stmt struct {
	Kind          StmtKind
	Tok           Token
	NameToken     Token
	Name          string
	Type          *Type
	Mutable       bool
	Const         bool
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
	BindingTok                 Token
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
	// Default is the expression used when the caller omits this argument. It is
	// nil for required parameters. Defaults must be trailing.
	Default *Expr
}
type Function struct {
	Name            string
	NameToken       Token
	Public          bool
	Worker          bool
	TypeParams      []TypeParam
	Params          []Param
	Return          *TypeSpec
	Receiver        *TypeSpec
	Unsafe          bool
	Body            []*Stmt
	Tok             Token
	Module          string
	VisibilityScope string
}
type FieldDecl struct {
	Name   string
	Public bool
	Spec   *TypeSpec
	Tok    Token
	Type   *Type
}
type StructDecl struct {
	Name            string
	NameToken       Token
	Public          bool
	Fields          []FieldDecl
	Tok             Token
	Module          string
	VisibilityScope string
	Type            *Type
}
type EnumDecl struct {
	Name            string
	NameToken       Token
	Public          bool
	Variants        []string
	VariantTokens   []Token
	Tok             Token
	Module          string
	VisibilityScope string
	Type            *Type
}
type ImportDecl struct {
	Path string
	Tok  Token
}
type Program struct {
	Source          *Source
	Statements      []*Stmt
	Functions       []*Function
	Structs         []*StructDecl
	Enums           []*EnumDecl
	Imports         []ImportDecl
	Sources         []*Source
	Module          string
	VisibilityScope string
}
