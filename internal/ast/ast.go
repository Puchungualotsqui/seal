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

type DistinctDecl struct {
	Name       Ident
	Underlying Type
	Loc        source.Span
}

func (*DistinctDecl) declNode() {}

func (d *DistinctDecl) Span() source.Span {
	return d.Loc
}

type GenericParamCategory int

const (
	GenericParamInvalid GenericParamCategory = iota

	// Type-level parameters.
	GenericParamType
	GenericParamEnum
	GenericParamUnion
	GenericParamTask

	// Builtin comptime value parameters.
	GenericParamInt
	GenericParamBool
	GenericParamString

	// Typed comptime value parameter:
	//
	//     player Id
	//     defaultZombie Zombie
	//
	GenericParamValue
)

func (c GenericParamCategory) String() string {
	switch c {
	case GenericParamType:
		return "type"
	case GenericParamEnum:
		return "enum"
	case GenericParamUnion:
		return "union"
	case GenericParamTask:
		return "task"
	case GenericParamInt:
		return "int"
	case GenericParamBool:
		return "bool"
	case GenericParamString:
		return "string"
	case GenericParamValue:
		return "value"
	default:
		return "<invalid>"
	}
}

type GenericParam struct {
	Name Ident

	// Category is one of:
	//
	//     T type
	//     E enum
	//     U union
	//     F task
	//     N int
	//     B bool
	//     Name string
	//     player Id
	//
	Category GenericParamCategory

	// Type is used only for typed comptime value parameters:
	//
	//     player Id
	//     defaultZombie Zombie
	//
	Type Type

	Constraints []GenericConstraint
	Loc         source.Span
}

func (p GenericParam) Span() source.Span {
	return p.Loc
}

type GenericConstraint interface {
	Node
	genericConstraintNode()
}

// Value constraint:
//
//	N int[N > 0]
//	Name string[len(Name) > 0]
//	defaultZombie Zombie[defaultZombie.id >= cast<Id>(0)]
type GenericExprConstraint struct {
	Expr Expr
	Loc  source.Span
}

func (*GenericExprConstraint) genericConstraintNode() {}

func (c *GenericExprConstraint) Span() source.Span {
	return c.Loc
}

// Field requirement:
//
//	T type[health]
//	T type[health int]
type GenericFieldConstraint struct {
	Name    Ident
	Type    Type
	HasType bool
	Loc     source.Span
}

func (*GenericFieldConstraint) genericConstraintNode() {}

func (c *GenericFieldConstraint) Span() source.Span {
	return c.Loc
}

// Static interface implementation requirement:
//
//	T type[Enemy()]
type GenericImplConstraint struct {
	Interface Type
	Loc       source.Span
}

func (*GenericImplConstraint) genericConstraintNode() {}

func (c *GenericImplConstraint) Span() source.Span {
	return c.Loc
}

// Enum variant requirement:
//
//	E enum[North, East]
type GenericEnumVariantConstraint struct {
	Name Ident
	Loc  source.Span
}

func (*GenericEnumVariantConstraint) genericConstraintNode() {}

func (c *GenericEnumVariantConstraint) Span() source.Span {
	return c.Loc
}

// Union member requirement:
//
//	U union[Circle, Rectangle]
type GenericUnionMemberConstraint struct {
	Member Type
	Loc    source.Span
}

func (*GenericUnionMemberConstraint) genericConstraintNode() {}

func (c *GenericUnionMemberConstraint) Span() source.Span {
	return c.Loc
}

// Task signature requirement:
//
//	F task[(int, bool) f32, f64]
type GenericTaskConstraint struct {
	Params  []Type
	Results []Type
	Loc     source.Span
}

func (*GenericTaskConstraint) genericConstraintNode() {}

func (c *GenericTaskConstraint) Span() source.Span {
	return c.Loc
}

type StructDecl struct {
	Name          Ident
	GenericParams []GenericParam
	Fields        []Field
	IsIntrinsic   bool
	Loc           source.Span
}

func (*StructDecl) declNode() {}

func (d *StructDecl) Span() source.Span {
	return d.Loc
}

type DeclStmt struct {
	Decl Decl
	Loc  source.Span
}

func (*DeclStmt) stmtNode() {}

func (s *DeclStmt) Span() source.Span {
	return s.Loc
}

type Field struct {
	Name Ident
	Type Type
}

type Param struct {
	Name       Ident
	Type       Type
	IsVariadic bool
	HasDefault bool
	Default    Expr
}

type TaskDecl struct {
	Name          Ident
	GenericParams []GenericParam

	IsPure        bool
	IsTest        bool
	IsExtern      bool
	IsIntrinsic   bool
	IsTrustedPure bool
	ExternName    string

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

// InterfaceDecl describes either a static/default interface or a dynamic
// interface.
//
// Static/default interface:
//
//	Reader :: interface <Out type> {
//		Read :: task(self *self) Out
//	}
//
// Dynamic interface:
//
//	Reader :: dyn interface <Out type> {
//		Read :: task(self *self) Out
//	}
//
// The implementing type is represented inside requirements by the builtin
// InterfaceSelfType. It is deliberately not included in GenericParams.
type InterfaceDecl struct {
	Name          Ident
	GenericParams []GenericParam

	// false:
	//
	//     Reader :: interface <Out type> { ... }
	//
	// true:
	//
	//     Reader :: dyn interface <Out type> { ... }
	IsDyn bool

	Requirements []*TaskSignature
	Loc          source.Span
}

func (*InterfaceDecl) declNode() {}

func (d *InterfaceDecl) Span() source.Span {
	return d.Loc
}

// TaskSignature describes an interface requirement.
//
// Requirements have no body. They may have multiple result types and may
// reference InterfaceSelfType anywhere in their parameter or result types.
//
// Example:
//
//	Read :: task(self *self) (Out, bool)
type TaskSignature struct {
	Name Ident

	IsPure        bool
	IsTrustedPure bool

	Params  []Param
	Results []Type
	Loc     source.Span
}

func (s *TaskSignature) Span() source.Span {
	if s == nil {
		return source.Span{}
	}
	return s.Loc
}

// ImplDecl describes either a manual interface implementation or an explicit
// delegated implementation.
//
// Manual implementation:
//
//	Reader<T> :: impl <T type> Box<T> {
//		Read :: task(self *Box<T>) T {
//			return self.value
//		}
//	}
//
// Delegated implementation:
//
//	Positioned :: impl Entity using transform
//
// Nested delegated implementation:
//
//	Positioned :: impl Entity using components.transform
//
// Interface identifies the interface specialization pattern:
//
//	Reader<T>
//
// GenericParams belong to the impl declaration:
//
//	<T type>
//
// Target identifies the implementing type pattern:
//
//	Box<T>
//
// A manual impl contains Entries and has an empty UsingPath.
// A delegated impl contains a UsingPath and should have no Entries.
type ImplDecl struct {
	Interface Type

	GenericParams []GenericParam
	Target        Type

	Entries []ImplEntry

	// UsingPath is an explicit field path used for interface delegation.
	//
	//     using transform
	//     using components.transform
	//
	// Each path component is stored separately. An empty path means this is a
	// manual implementation.
	UsingPath []Ident

	Loc source.Span
}

func (*ImplDecl) declNode() {}

func (d *ImplDecl) Span() source.Span {
	return d.Loc
}

func (d *ImplDecl) IsDelegated() bool {
	return d != nil && len(d.UsingPath) > 0
}

func (d *ImplDecl) IsManual() bool {
	return d != nil && len(d.UsingPath) == 0
}

// ImplEntry describes one manually implemented interface requirement.
//
// Inline implementation:
//
//	Read :: task(self *Box<T>) T {
//		return self.value
//	}
//
// Alias implementation:
//
//	Read :: ReadBox
//
// Alias implementations remain part of the AST so they may continue to be
// supported independently from using-based whole-interface delegation.
type ImplEntry struct {
	Name Ident

	Task  *TaskDecl
	Alias Expr

	Loc source.Span
}

func (e *ImplEntry) Span() source.Span {
	if e == nil {
		return source.Span{}
	}
	return e.Loc
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

type GenericArgKind int

const (
	GenericArgInvalid GenericArgKind = iota
	GenericArgType
	GenericArgExpr
)

type GenericArg struct {
	Kind GenericArgKind
	Type Type
	Expr Expr
	Loc  source.Span
}

func (a GenericArg) Span() source.Span {
	return a.Loc
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

// InterfaceSelfType represents the builtin `self` type available inside
// interface requirement signatures.
//
// Example:
//
//	Reader :: interface <Out type> {
//		Read :: task(self *self) Out
//	}
//
// The parser may construct this node anywhere `self` appears in a type
// position. The checker is responsible for rejecting it outside an interface
// requirement scope.
type InterfaceSelfType struct {
	Loc source.Span
}

func (*InterfaceSelfType) typeNode() {}

func (t *InterfaceSelfType) Span() source.Span {
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

type GenericType struct {
	Base Type
	Args []GenericArg
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

type MultiVarDeclStmt struct {
	Names []Ident
	Value Expr
	Loc   source.Span
}

func (*MultiVarDeclStmt) stmtNode() {}

func (s *MultiVarDeclStmt) Span() source.Span {
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

type SwitchStmt struct {
	// For normal enum switch:
	//
	//     switch err { ... }
	//
	// For union switch:
	//
	//     switch shape in s { ... }
	//
	// For any type switch:
	//
	//     switch value type { ... }
	BindName      Ident
	Target        Expr
	IsUnionSwitch bool
	IsTypeSwitch  bool
	IsPartial     bool
	Cases         []SwitchCase
	Loc           source.Span
}

func (*SwitchStmt) stmtNode() {}

func (s *SwitchStmt) Span() source.Span {
	return s.Loc
}

type SwitchCaseKind int

const (
	SwitchCaseExpr SwitchCaseKind = iota
	SwitchCaseEnumVariant
	SwitchCaseUnionMember
	SwitchCaseNil
	SwitchCaseDefault
)

type SwitchCase struct {
	Kind SwitchCaseKind

	// case .None:
	EnumVariant Ident

	// case Circle:
	UnionMember Type

	// case 10:
	Expr Expr

	Body []Stmt
	Loc  source.Span
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

type CStringLitExpr struct {
	Value string
	Loc   source.Span
}

func (*CStringLitExpr) exprNode() {}

func (e *CStringLitExpr) Span() source.Span {
	return e.Loc
}

type CharLitExpr struct {
	Value string
	Loc   source.Span
}

func (*CharLitExpr) exprNode() {}

func (e *CharLitExpr) Span() source.Span {
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

type SpreadExpr struct {
	Expr Expr
	Loc  source.Span
}

func (*SpreadExpr) exprNode() {}

func (e *SpreadExpr) Span() source.Span {
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

type GenericExpr struct {
	Base Expr
	Args []GenericArg
	Loc  source.Span
}

func (*GenericExpr) exprNode() {}

func (e *GenericExpr) Span() source.Span {
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
