package ast

import (
	"testing"

	"seal/internal/source"
)

func TestIdentSpan(t *testing.T) {
	file := source.NewFile("main.seal", "hello")
	span := source.NewSpan(file, 0, 5)

	id := Ident{
		Name: "hello",
		Loc:  span,
	}

	if id.Span().Text() != "hello" {
		t.Fatalf("expected ident span text hello, got %q", id.Span().Text())
	}
}

func TestConstDeclImplementsDecl(t *testing.T) {
	var _ Decl = (*ConstDecl)(nil)
}

func TestReturnStmtImplementsStmt(t *testing.T) {
	var _ Stmt = (*ReturnStmt)(nil)
}

func TestIdentExprImplementsExpr(t *testing.T) {
	var _ Expr = (*IdentExpr)(nil)
}

func TestNamedTypeImplementsType(t *testing.T) {
	var _ Type = (*NamedType)(nil)
}

func TestDistinctDeclImplementsDecl(t *testing.T) {
	var _ Decl = (*DistinctDecl)(nil)
}

func TestGenericTypeImplementsType(t *testing.T) {
	var _ Type = (*GenericType)(nil)
}

func TestGenericExprImplementsExpr(t *testing.T) {
	var _ Expr = (*GenericExpr)(nil)
}

func TestSpreadExprImplementsExpr(t *testing.T) {
	var _ Expr = (*SpreadExpr)(nil)
}

func TestIndexExprImplementsExpr(t *testing.T) {
	var _ Expr = (*IndexExpr)(nil)
}

func TestAssignStmtImplementsStmt(t *testing.T) {
	var _ Stmt = (*AssignStmt)(nil)
}

func TestOverloadDeclImplementsDecl(t *testing.T) {
	var _ Decl = (*OverloadDecl)(nil)
}
