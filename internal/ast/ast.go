package ast

import "github.com/ArubikU/polyloft-bvm/internal/token"

type Program struct {
	Statements []Stmt
}

type Stmt interface {
	stmtNode()
}

type Expr interface {
	exprNode()
}

type TypeRef struct {
	Name      token.Token
	Args      []*TypeRef
	Union     []*TypeRef
	Wildcard  bool
	BoundKind token.Type
	Bound     *TypeRef
}

type TypeParam struct {
	Name   token.Token
	Bounds []*TypeRef
}

type Annotation struct {
	Name token.Token
}

type Parameter struct {
	Name token.Token
	Type *TypeRef
}

type VariableKind string

type Visibility string

const (
	VariableLet   VariableKind = "let"
	VariableVar   VariableKind = "var"
	VariableConst VariableKind = "const"
	VariableFinal VariableKind = "final"

	VisibilityPublic    Visibility = "public"
	VisibilityPrivate   Visibility = "private"
	VisibilityProtected Visibility = "protected"
)

type LetStmt struct {
	Kind       VariableKind
	Visibility Visibility
	Name       token.Token
	Type       *TypeRef
	Value      Expr
}

func (LetStmt) stmtNode() {}

type DestructureLetStmt struct {
	Kind       VariableKind
	Visibility Visibility
	Targets    []token.Token
	Value      Expr
}

func (DestructureLetStmt) stmtNode() {}

type AssignStmt struct {
	Name     token.Token
	Operator token.Token
	Value    Expr
}

func (AssignStmt) stmtNode() {}

type SetStmt struct {
	Object   Expr
	Name     token.Token
	Operator token.Token
	Value    Expr
}

func (SetStmt) stmtNode() {}

type SetIndexStmt struct {
	Object   Expr
	Index    Expr
	Operator token.Token
	Value    Expr
}

func (SetIndexStmt) stmtNode() {}

type ExprStmt struct {
	Expr Expr
}

func (ExprStmt) stmtNode() {}

type BlockStmt struct {
	Statements []Stmt
}

func (BlockStmt) stmtNode() {}

type IfStmt struct {
	Condition Expr
	Then      *BlockStmt
	Else      *BlockStmt
}

func (IfStmt) stmtNode() {}

type TypePattern struct {
	Binding token.Token
	Type    *TypeRef
}

type SwitchPattern struct {
	Value Expr
	Type  *TypePattern
}

type SwitchArm struct {
	Patterns []SwitchPattern
	Body     *BlockStmt
}

type SwitchStmt struct {
	Value   Expr
	Arms    []SwitchArm
	Default *BlockStmt
}

func (SwitchStmt) stmtNode() {}

type ReturnStmt struct {
	Keyword token.Token
	Value   Expr
}

func (ReturnStmt) stmtNode() {}

type ImportStmt struct {
	Path  []token.Token
	Names []token.Token
}

func (ImportStmt) stmtNode() {}

type TypeAliasStmt struct {
	Visibility Visibility
	Name       token.Token
	Target     *TypeRef
}

func (TypeAliasStmt) stmtNode() {}

type FunctionStmt struct {
	Visibility Visibility
	Name       token.Token
	TypeParams []TypeParam
	Params     []Parameter
	ReturnType *TypeRef
	Body       *BlockStmt
}

func (FunctionStmt) stmtNode() {}

type InterfaceMethod struct {
	Name       token.Token
	Params     []Parameter
	ReturnType *TypeRef
}

type InterfaceStmt struct {
	Visibility Visibility
	Name       token.Token
	TypeParams []TypeParam
	Extends    []*TypeRef
	Permits    []*TypeRef
	IsSealed   bool
	Methods    []InterfaceMethod
}

func (InterfaceStmt) stmtNode() {}

type FieldDecl struct {
	Kind       VariableKind
	Name       token.Token
	Type       *TypeRef
	Value      Expr
	Static     bool
	Visibility Visibility
}

type MethodDecl struct {
	Name          token.Token
	Annotations   []Annotation
	TypeParams    []TypeParam
	Params        []Parameter
	ReturnType    *TypeRef
	Body          *BlockStmt
	IsConstructor bool
	IsAbstract    bool
	Static        bool
	Visibility    Visibility
}

type EnumValueDecl struct {
	Name      token.Token
	Arguments []Expr
}

type ClassStmt struct {
	Visibility Visibility
	Name       token.Token
	TypeParams []TypeParam
	Superclass *TypeRef
	Implements []*TypeRef
	Permits    []*TypeRef
	IsAbstract bool
	IsFinal    bool
	IsSealed   bool
	IsEnum     bool
	IsRecord   bool
	EnumValues []EnumValueDecl
	Fields     []FieldDecl
	Methods    []MethodDecl
}

func (ClassStmt) stmtNode() {}

type ForStmt struct {
	Targets   []token.Token
	Iterable  Expr
	Condition Expr
	Body      *BlockStmt
}

func (ForStmt) stmtNode() {}

type BinaryExpr struct {
	Left     Expr
	Operator token.Token
	Right    Expr
}

func (BinaryExpr) exprNode() {}

type UnaryExpr struct {
	Operator token.Token
	Right    Expr
}

func (UnaryExpr) exprNode() {}

type CastExpr struct {
	Target *TypeRef
	Expr   Expr
}

func (CastExpr) exprNode() {}

type CallExpr struct {
	Callee    Expr
	Paren     token.Token
	Arguments []Expr
}

func (CallExpr) exprNode() {}

type NewExpr struct {
	Class     token.Token
	Paren     token.Token
	Arguments []Expr
}

func (NewExpr) exprNode() {}

type GetExpr struct {
	Object Expr
	Name   token.Token
}

func (GetExpr) exprNode() {}

type IndexExpr struct {
	Object  Expr
	Index   Expr
	Bracket token.Token
}

func (IndexExpr) exprNode() {}

type SliceExpr struct {
	Object  Expr
	Start   Expr
	End     Expr
	Bracket token.Token
}

func (SliceExpr) exprNode() {}

type ThisExpr struct {
	Keyword token.Token
}

func (ThisExpr) exprNode() {}

type SuperExpr struct {
	Keyword token.Token
}

func (SuperExpr) exprNode() {}

type LambdaExpr struct {
	Params []Parameter
	Body   Expr
	Block  *BlockStmt
}

func (LambdaExpr) exprNode() {}

type GroupingExpr struct {
	Expr Expr
}

func (GroupingExpr) exprNode() {}

type TupleExpr struct {
	Elements []Expr
}

func (TupleExpr) exprNode() {}

type ArrayExpr struct {
	Elements []Expr
}

func (ArrayExpr) exprNode() {}

type MapEntry struct {
	Key   string
	Value Expr
}

type MapExpr struct {
	Entries []MapEntry
}

func (MapExpr) exprNode() {}

type LiteralExpr struct {
	Value any
}

func (LiteralExpr) exprNode() {}

type VariableExpr struct {
	Name token.Token
}

func (VariableExpr) exprNode() {}
