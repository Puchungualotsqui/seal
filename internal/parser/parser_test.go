package parser

import (
	"testing"

	"seal/internal/ast"
	"seal/internal/diag"
	"seal/internal/lexer"
	"seal/internal/source"
)

func parse(t *testing.T, input string) (*ast.File, *diag.Reporter) {
	t.Helper()

	file := source.NewFile("test.seal", input)
	reporter := diag.NewReporter()

	lex := lexer.New(file, reporter)
	tokens := lex.LexAll()

	if reporter.HasErrors() {
		t.Fatalf("lexer diagnostics:\n%s", reporter.String())
	}

	p := New(tokens, reporter)
	parsed := p.ParseFile()

	return parsed, reporter
}

func TestParseConstDecl(t *testing.T) {
	file, reporter := parse(t, `MAX_COUNT :: 1024`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if len(file.Decls) != 1 {
		t.Fatalf("expected 1 decl, got %d", len(file.Decls))
	}

	decl, ok := file.Decls[0].(*ast.ConstDecl)
	if !ok {
		t.Fatalf("expected ConstDecl, got %T", file.Decls[0])
	}

	if decl.Name.Name != "MAX_COUNT" {
		t.Fatalf("expected MAX_COUNT, got %q", decl.Name.Name)
	}
}

func TestParseStructDecl(t *testing.T) {
	file, reporter := parse(t, `
Vec2 :: struct {
    x f32
    y f32
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	decl, ok := file.Decls[0].(*ast.StructDecl)
	if !ok {
		t.Fatalf("expected StructDecl, got %T", file.Decls[0])
	}

	if decl.Name.Name != "Vec2" {
		t.Fatalf("expected Vec2")
	}

	if len(decl.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(decl.Fields))
	}

	if decl.Fields[0].Name.Name != "x" {
		t.Fatalf("expected first field x")
	}
}

func TestParseGenericStructDecl(t *testing.T) {
	file, reporter := parse(t, `
Buffer :: struct($T, #N) {
    data [N]T
    len int
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	decl := file.Decls[0].(*ast.StructDecl)

	if len(decl.Params) != 2 {
		t.Fatalf("expected 2 generic params, got %d", len(decl.Params))
	}

	if decl.Params[0].Kind != ast.GenericTypeParam {
		t.Fatalf("expected first param to be type param")
	}

	if decl.Params[1].Kind != ast.GenericValueParam {
		t.Fatalf("expected second param to be value param")
	}
}

func TestParseTaskDecl(t *testing.T) {
	file, reporter := parse(t, `
LengthSq :: pure task(v Vec2) f32 {
    return v.x * v.x + v.y * v.y
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	decl, ok := file.Decls[0].(*ast.TaskDecl)
	if !ok {
		t.Fatalf("expected TaskDecl, got %T", file.Decls[0])
	}

	if !decl.IsPure {
		t.Fatalf("expected pure task")
	}

	if len(decl.Params) != 1 {
		t.Fatalf("expected 1 param")
	}

	if len(decl.Results) != 1 {
		t.Fatalf("expected 1 result")
	}

	if len(decl.Body.Stmts) != 1 {
		t.Fatalf("expected 1 statement")
	}
}

func TestParseTestTaskDecl(t *testing.T) {
	file, reporter := parse(t, `
SwitchTest :: test task() {
    Assert(true)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	decl := file.Decls[0].(*ast.TaskDecl)

	if !decl.IsTest {
		t.Fatalf("expected test task")
	}
}

func TestParseVarDeclAndAssign(t *testing.T) {
	file, reporter := parse(t, `
Foo :: task(number int) int {
    n := number
    n = n + 5
    return n
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	task := file.Decls[0].(*ast.TaskDecl)

	if len(task.Body.Stmts) != 3 {
		t.Fatalf("expected 3 statements, got %d", len(task.Body.Stmts))
	}

	if _, ok := task.Body.Stmts[0].(*ast.VarDeclStmt); !ok {
		t.Fatalf("expected first stmt VarDeclStmt, got %T", task.Body.Stmts[0])
	}

	if _, ok := task.Body.Stmts[1].(*ast.AssignStmt); !ok {
		t.Fatalf("expected second stmt AssignStmt, got %T", task.Body.Stmts[1])
	}
}

func TestParseTypedVarDeclWithArray(t *testing.T) {
	file, reporter := parse(t, `
Main :: task() {
    arr: [?]int = [2, 3, 4]
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	task := file.Decls[0].(*ast.TaskDecl)
	stmt := task.Body.Stmts[0].(*ast.VarDeclStmt)

	arrayType, ok := stmt.Type.(*ast.ArrayType)
	if !ok {
		t.Fatalf("expected ArrayType, got %T", stmt.Type)
	}

	if !arrayType.Inferred {
		t.Fatalf("expected inferred array length")
	}
}

func TestParseCStyleFor(t *testing.T) {
	file, reporter := parse(t, `
Main :: task() {
    for i := 0; i < 10; i = i + 1 {
        Print(i)
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	task := file.Decls[0].(*ast.TaskDecl)

	stmt, ok := task.Body.Stmts[0].(*ast.ForStmt)
	if !ok {
		t.Fatalf("expected ForStmt, got %T", task.Body.Stmts[0])
	}

	if stmt.Init == nil {
		t.Fatalf("expected for init")
	}

	if stmt.Cond == nil {
		t.Fatalf("expected for cond")
	}

	if stmt.Post == nil {
		t.Fatalf("expected for post")
	}
}

func TestParseNoInKeyword(t *testing.T) {
	file, reporter := parse(t, `
Main :: task() {
    in := 10
    return in
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	task := file.Decls[0].(*ast.TaskDecl)

	if len(task.Body.Stmts) != 2 {
		t.Fatalf("expected 2 statements")
	}
}

func TestParseEnumUnionInterfaceImpl(t *testing.T) {
	file, reporter := parse(t, `
Error :: enum {
    None
    ErrorReading
}

Shape :: union {
    Circle,
    Rectangle,
}

Enemy :: interface {
    Damage :: task(e *$T, damage int)
    Health :: task(e *$T) int
}

Soldier :: impl {
    Enemy
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if len(file.Decls) != 4 {
		t.Fatalf("expected 4 decls, got %d", len(file.Decls))
	}

	if _, ok := file.Decls[0].(*ast.EnumDecl); !ok {
		t.Fatalf("expected EnumDecl")
	}

	if _, ok := file.Decls[1].(*ast.UnionDecl); !ok {
		t.Fatalf("expected UnionDecl")
	}

	if _, ok := file.Decls[2].(*ast.InterfaceDecl); !ok {
		t.Fatalf("expected InterfaceDecl")
	}

	if _, ok := file.Decls[3].(*ast.ImplDecl); !ok {
		t.Fatalf("expected ImplDecl")
	}
}

func TestParseCImportDirective(t *testing.T) {
	file, reporter := parse(t, `
c :: @c_import {
    include "stdio.h"
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if _, ok := file.Decls[0].(*ast.DirectiveDecl); !ok {
		t.Fatalf("expected DirectiveDecl, got %T", file.Decls[0])
	}
}

func TestParseRawUnion(t *testing.T) {
	file, reporter := parse(t, `
CValue :: @rawUnion union {
    i int
    f f32
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	decl, ok := file.Decls[0].(*ast.UnionDecl)
	if !ok {
		t.Fatalf("expected UnionDecl, got %T", file.Decls[0])
	}

	if !decl.Raw {
		t.Fatalf("expected raw union")
	}
}
