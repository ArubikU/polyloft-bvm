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
	Name token.Token
}

type Annotation struct {
	Name token.Token
}

type Parameter struct {
	Name token.Token
	Type *TypeRef
}

type VariableKind string

const (
	VariableLet   VariableKind = "let"
	VariableVar   VariableKind = "var"
	VariableConst VariableKind = "const"
	VariableFinal VariableKind = "final"
)

type LetStmt struct {
	Kind  VariableKind
	Name  token.Token
	Type  *TypeRef
	Value Expr
}

func (LetStmt) stmtNode() {}

type DestructureLetStmt struct {
	Kind    VariableKind
	Targets []token.Token
	Value   Expr
}

func (DestructureLetStmt) stmtNode() {}

type AssignStmt struct {
	Name  token.Token
	Value Expr
}

func (AssignStmt) stmtNode() {}

type SetStmt struct {
	Object Expr
	Name   token.Token
	Value  Expr
}

func (SetStmt) stmtNode() {}

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

type ReturnStmt struct {
	Keyword token.Token
	Value   Expr
}

func (ReturnStmt) stmtNode() {}

type FunctionStmt struct {
	Name       token.Token
	Params     []Parameter
	ReturnType *TypeRef
	Body       *BlockStmt
}

func (FunctionStmt) stmtNode() {}

type FieldDecl struct {
	Kind   VariableKind
	Name   token.Token
	Type   *TypeRef
	Value  Expr
	Static bool
}

type MethodDecl struct {
	Name          token.Token
	Annotations   []Annotation
	Params        []Parameter
	ReturnType    *TypeRef
	Body          *BlockStmt
	IsConstructor bool
	Static        bool
}

type ClassStmt struct {
	Name       token.Token
	Superclass *TypeRef
	Implements []*TypeRef
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

type ThisExpr struct {
	Keyword token.Token
}

func (ThisExpr) exprNode() {}

type SuperExpr struct {
	Keyword token.Token
}

func (SuperExpr) exprNode() {}

type GroupingExpr struct {
	Expr Expr
}

func (GroupingExpr) exprNode() {}

type TupleExpr struct {
	Elements []Expr
}

func (TupleExpr) exprNode() {}

type LiteralExpr struct {
	Value any
}

func (LiteralExpr) exprNode() {}

type VariableExpr struct {
	Name token.Token
}

func (VariableExpr) exprNode() {}
