package ast

import (
	"seal/internal/source"
	"seal/internal/token"
)

type Node interface {
	Span() source.Span
}

type File struct {
	Decls []Decl
}

type Ident struct {
	Name string
	Loc  source.Span
}

func (i Ident) Span() source.Span {
	return i.Loc
}

type Decl interface {
	Node
	declNode()
}

type Stmt interface {
	Node
	stmtNode()
}

type Expr interface {
	Node
	exprNode()
}

type Type interface {
	Node
	typeNode()
}

// --------------------
// Declarations
// --------------------

type ConstDecl struct {
	Name  Ident
	Value Expr
	Loc   source.Span
}

func (*ConstDecl) declNode() {}
func (d *ConstDecl) Span() source.Span {
	return d.Loc
}

type GenericParamKind int

const (
	GenericTypeParam GenericParamKind = iota
	GenericValueParam
)

type GenericParam struct {
	Kind GenericParamKind
	Name Ident
}

type StructDecl struct {
	Name   Ident
	Params []GenericParam
	Fields []Field
	Loc    source.Span
}

type DeclStmt struct {
	Decl Decl
	Loc  source.Span
}

func (*DeclStmt) stmtNode() {}

func (s *DeclStmt) Span() source.Span {
	return s.Loc
}

func (*StructDecl) declNode() {}
func (d *StructDecl) Span() source.Span {
	return d.Loc
}

type Field struct {
	Name Ident
	Type Type
}

type Param struct {
	Name       Ident
	Type       Type
	HasDefault bool
	Default    Expr
}

type TaskDecl struct {
	Name    Ident
	IsPure  bool
	IsTest  bool
	Params  []Param
	Results []Type
	Body    *BlockStmt
	Loc     source.Span
}

func (*TaskDecl) declNode() {}
func (d *TaskDecl) Span() source.Span {
	return d.Loc
}

type EnumDecl struct {
	Name     Ident
	Variants []Ident
	Loc      source.Span
}

func (*EnumDecl) declNode() {}
func (d *EnumDecl) Span() source.Span {
	return d.Loc
}

type UnionDecl struct {
	Name    Ident
	Members []Type
	Loc     source.Span
	Raw     bool
}

func (*UnionDecl) declNode() {}
func (d *UnionDecl) Span() source.Span {
	return d.Loc
}

type InterfaceDecl struct {
	Name         Ident
	Requirements []*TaskSignature
	Loc          source.Span
}

func (*InterfaceDecl) declNode() {}
func (d *InterfaceDecl) Span() source.Span {
	return d.Loc
}

type TaskSignature struct {
	Name    Ident
	Params  []Param
	Results []Type
	Loc     source.Span
}

type ImplDecl struct {
	TypeName   Ident
	Interfaces []Type
	Loc        source.Span
}

func (*ImplDecl) declNode() {}
func (d *ImplDecl) Span() source.Span {
	return d.Loc
}

type OverloadDecl struct {
	Name  string
	Names []Ident
	Loc   source.Span
}

func (*OverloadDecl) declNode() {}
func (d *OverloadDecl) Span() source.Span {
	return d.Loc
}

type DirectiveDecl struct {
	Name      Ident
	Directive Ident
	Body      []token.Token
	Loc       source.Span
}

func (*DirectiveDecl) declNode() {}
func (d *DirectiveDecl) Span() source.Span {
	return d.Loc
}

// --------------------
// Types
// --------------------

type NamedType struct {
	Parts []Ident
	Loc   source.Span
}

func (*NamedType) typeNode() {}
func (t *NamedType) Span() source.Span {
	return t.Loc
}

type PointerType struct {
	Elem Type
	Loc  source.Span
}

func (*PointerType) typeNode() {}
func (t *PointerType) Span() source.Span {
	return t.Loc
}

type ArrayType struct {
	Len      Expr
	Inferred bool
	Elem     Type
	Loc      source.Span
}

func (*ArrayType) typeNode() {}
func (t *ArrayType) Span() source.Span {
	return t.Loc
}

type GenericType struct {
	Base Type
	Args []Expr
	Loc  source.Span
}

func (*GenericType) typeNode() {}
func (t *GenericType) Span() source.Span {
	return t.Loc
}

// --------------------
// Statements
// --------------------

type BlockStmt struct {
	Stmts []Stmt
	Loc   source.Span
}

func (*BlockStmt) stmtNode() {}
func (s *BlockStmt) Span() source.Span {
	return s.Loc
}

type ReturnStmt struct {
	Values []Expr
	Loc    source.Span
}

func (*ReturnStmt) stmtNode() {}
func (s *ReturnStmt) Span() source.Span {
	return s.Loc
}

type DeferStmt struct {
	Call Expr
	Loc  source.Span
}

func (*DeferStmt) stmtNode() {}
func (s *DeferStmt) Span() source.Span {
	return s.Loc
}

type SealStmt struct {
	Target Expr
	Loc    source.Span
}

func (*SealStmt) stmtNode() {}
func (s *SealStmt) Span() source.Span {
	return s.Loc
}

type ExprStmt struct {
	Expr Expr
	Loc  source.Span
}

func (*ExprStmt) stmtNode() {}
func (s *ExprStmt) Span() source.Span {
	return s.Loc
}

type AssignStmt struct {
	Left  Expr
	Op    token.Kind
	Right Expr
	Loc   source.Span
}

func (*AssignStmt) stmtNode() {}
func (s *AssignStmt) Span() source.Span {
	return s.Loc
}

type VarDeclStmt struct {
	Name     Ident
	Type     Type
	Value    Expr
	HasType  bool
	HasValue bool
	Loc      source.Span
}

func (*VarDeclStmt) stmtNode() {}
func (s *VarDeclStmt) Span() source.Span {
	return s.Loc
}

type IfStmt struct {
	Cond Expr
	Then *BlockStmt
	Else Stmt
	Loc  source.Span
}

func (*IfStmt) stmtNode() {}
func (s *IfStmt) Span() source.Span {
	return s.Loc
}

type ForStmt struct {
	Init Stmt
	Cond Expr
	Post Stmt
	Body *BlockStmt
	Loc  source.Span
}

func (*ForStmt) stmtNode() {}
func (s *ForStmt) Span() source.Span {
	return s.Loc
}

// --------------------
// Expressions
// --------------------

type IdentExpr struct {
	Name Ident
}

func (*IdentExpr) exprNode() {}
func (e *IdentExpr) Span() source.Span {
	return e.Name.Span()
}

type DotIdentExpr struct {
	Name Ident
	Loc  source.Span
}

func (*DotIdentExpr) exprNode() {}
func (e *DotIdentExpr) Span() source.Span {
	return e.Loc
}

type IntLitExpr struct {
	Value string
	Loc   source.Span
}

func (*IntLitExpr) exprNode() {}
func (e *IntLitExpr) Span() source.Span {
	return e.Loc
}

type FloatLitExpr struct {
	Value string
	Loc   source.Span
}

func (*FloatLitExpr) exprNode() {}
func (e *FloatLitExpr) Span() source.Span {
	return e.Loc
}

type StringLitExpr struct {
	Value string
	Loc   source.Span
}

func (*StringLitExpr) exprNode() {}
func (e *StringLitExpr) Span() source.Span {
	return e.Loc
}

type BoolLitExpr struct {
	Value bool
	Loc   source.Span
}

func (*BoolLitExpr) exprNode() {}
func (e *BoolLitExpr) Span() source.Span {
	return e.Loc
}

type NilLitExpr struct {
	Loc source.Span
}

func (*NilLitExpr) exprNode() {}
func (e *NilLitExpr) Span() source.Span {
	return e.Loc
}

type UnaryExpr struct {
	Op   token.Kind
	Expr Expr
	Loc  source.Span
}

func (*UnaryExpr) exprNode() {}
func (e *UnaryExpr) Span() source.Span {
	return e.Loc
}

type BinaryExpr struct {
	Left  Expr
	Op    token.Kind
	Right Expr
	Loc   source.Span
}

func (*BinaryExpr) exprNode() {}
func (e *BinaryExpr) Span() source.Span {
	return e.Loc
}

type CallExpr struct {
	Callee Expr
	Args   []Expr
	Loc    source.Span
}

func (*CallExpr) exprNode() {}
func (e *CallExpr) Span() source.Span {
	return e.Loc
}

type SelectorExpr struct {
	Left Expr
	Name Ident
	Loc  source.Span
}

func (*SelectorExpr) exprNode() {}
func (e *SelectorExpr) Span() source.Span {
	return e.Loc
}

type IndexExpr struct {
	Left  Expr
	Index Expr
	Loc   source.Span
}

func (*IndexExpr) exprNode() {}
func (e *IndexExpr) Span() source.Span {
	return e.Loc
}

type ArrayLiteralExpr struct {
	Values []Expr
	Loc    source.Span
}

func (*ArrayLiteralExpr) exprNode() {}
func (e *ArrayLiteralExpr) Span() source.Span {
	return e.Loc
}

type LiteralField struct {
	Name  Ident
	Value Expr
}

type CompoundLiteralExpr struct {
	Type   Type
	Fields []LiteralField
	Values []Expr
	Loc    source.Span
}

func (*CompoundLiteralExpr) exprNode() {}
func (e *CompoundLiteralExpr) Span() source.Span {
	return e.Loc
}
