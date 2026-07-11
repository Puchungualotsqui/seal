package parser

import (
	"strings"
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
Buffer :: struct <T type, N int> {
    data Array<T, N>
    len int
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if len(file.Decls) != 1 {
		t.Fatalf("expected 1 declaration, got %d", len(file.Decls))
	}

	decl, ok := file.Decls[0].(*ast.StructDecl)
	if !ok {
		t.Fatalf(
			"expected StructDecl, got %T",
			file.Decls[0],
		)
	}

	if len(decl.GenericParams) != 2 {
		t.Fatalf(
			"expected 2 generic params, got %d",
			len(decl.GenericParams),
		)
	}

	if decl.GenericParams[0].Category != ast.GenericParamType {
		t.Fatalf("expected first param to be type param")
	}

	if decl.GenericParams[1].Category != ast.GenericParamInt {
		t.Fatalf("expected second param to be int param")
	}

	if len(decl.Fields) != 2 {
		t.Fatalf(
			"expected 2 fields, got %d",
			len(decl.Fields),
		)
	}

	arrayType, ok := decl.Fields[0].Type.(*ast.GenericType)
	if !ok {
		t.Fatalf(
			"data field type is %T, expected *ast.GenericType",
			decl.Fields[0].Type,
		)
	}

	base, ok := arrayType.Base.(*ast.NamedType)
	if !ok {
		t.Fatalf(
			"array base is %T, expected *ast.NamedType",
			arrayType.Base,
		)
	}

	if len(base.Parts) != 1 ||
		base.Parts[0].Name != "Array" {
		t.Fatalf(
			"expected Array generic base, got %#v",
			base.Parts,
		)
	}

	if len(arrayType.Args) != 2 {
		t.Fatalf(
			"expected 2 Array arguments, got %d",
			len(arrayType.Args),
		)
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
    assert(true)
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

Soldier :: struct {
    health int
}

Enemy :: interface {
    Damage :: task(self *self, damage int)
    Health :: task(self *self) int
}

Enemy :: impl Soldier {
    Damage :: DamageSoldier
    Health :: SoldierHealth
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if len(file.Decls) != 5 {
		t.Fatalf("expected 5 decls, got %d", len(file.Decls))
	}

	if _, ok := file.Decls[0].(*ast.EnumDecl); !ok {
		t.Fatalf("expected EnumDecl")
	}

	if _, ok := file.Decls[1].(*ast.UnionDecl); !ok {
		t.Fatalf("expected UnionDecl")
	}

	if _, ok := file.Decls[2].(*ast.StructDecl); !ok {
		t.Fatalf("expected StructDecl")
	}

	if _, ok := file.Decls[3].(*ast.InterfaceDecl); !ok {
		t.Fatalf("expected InterfaceDecl")
	}

	impl, ok := file.Decls[4].(*ast.ImplDecl)
	if !ok {
		t.Fatalf("expected ImplDecl")
	}

	if len(impl.Entries) != 2 {
		t.Fatalf("expected 2 impl entries, got %d", len(impl.Entries))
	}

	target, ok := impl.Target.(*ast.NamedType)
	if !ok {
		t.Fatalf("impl target is %T, want *ast.NamedType", impl.Target)
	}

	if len(target.Parts) != 1 || target.Parts[0].Name != "Soldier" {
		t.Fatalf("unexpected impl target: %#v", target.Parts)
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

func TestParseLocalConstAndTaskDecl(t *testing.T) {
	file, reporter := parse(t, `
Outer :: task() {
    OUTER_CONST :: 10

    Inner :: task() {
        SomeThing(OUTER_CONST)
    }

    Inner()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	task := file.Decls[0].(*ast.TaskDecl)

	if len(task.Body.Stmts) != 3 {
		t.Fatalf("expected 3 statements, got %d", len(task.Body.Stmts))
	}

	if _, ok := task.Body.Stmts[0].(*ast.DeclStmt); !ok {
		t.Fatalf("expected local const DeclStmt, got %T", task.Body.Stmts[0])
	}

	if _, ok := task.Body.Stmts[1].(*ast.DeclStmt); !ok {
		t.Fatalf("expected local task DeclStmt, got %T", task.Body.Stmts[1])
	}
}

func TestParseStandaloneBlock(t *testing.T) {
	file, reporter := parse(t, `
Main :: task() {
    {
        x := 10
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	task := file.Decls[0].(*ast.TaskDecl)

	if len(task.Body.Stmts) != 1 {
		t.Fatalf("expected 1 statement")
	}

	if _, ok := task.Body.Stmts[0].(*ast.BlockStmt); !ok {
		t.Fatalf("expected BlockStmt, got %T", task.Body.Stmts[0])
	}
}

func TestParseEnumSwitch(t *testing.T) {
	file, reporter := parse(t, `
Main :: task(err Error) int {
    switch err {
    case .None:
        return 0

    case .ErrorReading:
        return 1

    default:
        return 2
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	task := file.Decls[0].(*ast.TaskDecl)

	if len(task.Body.Stmts) != 1 {
		t.Fatalf("expected 1 statement")
	}

	sw, ok := task.Body.Stmts[0].(*ast.SwitchStmt)
	if !ok {
		t.Fatalf("expected SwitchStmt, got %T", task.Body.Stmts[0])
	}

	if sw.IsUnionSwitch {
		t.Fatalf("expected normal switch")
	}

	if len(sw.Cases) != 3 {
		t.Fatalf("expected 3 cases, got %d", len(sw.Cases))
	}
}

func TestParseUnionSwitch(t *testing.T) {
	file, reporter := parse(t, `
Area :: task(s Shape) f32 {
    switch shape in s {
    case Circle:
        return shape.radius * shape.radius

    case Rectangle:
        return shape.width * shape.height

    case nil:
        return 0
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	task := file.Decls[0].(*ast.TaskDecl)
	sw := task.Body.Stmts[0].(*ast.SwitchStmt)

	if !sw.IsUnionSwitch {
		t.Fatalf("expected union switch")
	}

	if sw.BindName.Name != "shape" {
		t.Fatalf("expected bind name shape, got %q", sw.BindName.Name)
	}

	if len(sw.Cases) != 3 {
		t.Fatalf("expected 3 cases")
	}
}

func TestInIsStillNormalIdentifier(t *testing.T) {
	file, reporter := parse(t, `
Main :: task() {
    in := 10
    value := in
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

func TestParseExternTaskDecl(t *testing.T) {
	file, reporter := parse(t, `
malloc :: extern("malloc") task(size uint) rawptr
printf :: extern("printf") task(format string, args ...any) int
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if len(file.Decls) != 2 {
		t.Fatalf("expected 2 decls, got %d", len(file.Decls))
	}

	mallocDecl, ok := file.Decls[0].(*ast.TaskDecl)
	if !ok {
		t.Fatalf("expected TaskDecl")
	}

	if !mallocDecl.IsExtern || mallocDecl.ExternName != "malloc" {
		t.Fatalf("expected extern malloc, got %+v", mallocDecl)
	}

	printfDecl, ok := file.Decls[1].(*ast.TaskDecl)
	if !ok {
		t.Fatalf("expected TaskDecl")
	}

	if !printfDecl.IsExtern || printfDecl.ExternName != "printf" {
		t.Fatalf("expected extern printf, got %+v", printfDecl)
	}

	if len(printfDecl.Params) != 2 {
		t.Fatalf("expected 2 params")
	}

	if !printfDecl.Params[1].IsVariadic {
		t.Fatalf("expected variadic args")
	}
}

func TestParseGenericAnyIntrinsics(t *testing.T) {
	file, reporter := parse(t, `
Main :: task() {
    value: any = 10
    isInt := anyIs<int>(value)
    x := anyAs<int>(value)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if len(file.Decls) != 1 {
		t.Fatalf("expected 1 decl, got %d", len(file.Decls))
	}
}

func TestParsePartialAnyTypeSwitch(t *testing.T) {
	file, reporter := parse(t, `
Main :: task() {
    value: any = 10

    @partial switch value type {
    case int:
        x := anyAs<int>(value)
    case string:
        s := anyAs<string>(value)
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	taskDecl := file.Decls[0].(*ast.TaskDecl)
	switchStmt := taskDecl.Body.Stmts[1].(*ast.SwitchStmt)

	if !switchStmt.IsPartial {
		t.Fatalf("expected partial switch")
	}

	if !switchStmt.IsTypeSwitch {
		t.Fatalf("expected type switch")
	}
}

func TestParseStringCStringAndCharLiterals(t *testing.T) {
	file, reporter := parse(t, `
Main :: task() {
    s := "hello"
    cs := c"hello"
    ch := 'ñ'
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	taskDecl := file.Decls[0].(*ast.TaskDecl)

	if _, ok := taskDecl.Body.Stmts[0].(*ast.VarDeclStmt).Value.(*ast.StringLitExpr); !ok {
		t.Fatalf("expected string literal")
	}

	if _, ok := taskDecl.Body.Stmts[1].(*ast.VarDeclStmt).Value.(*ast.CStringLitExpr); !ok {
		t.Fatalf("expected cstring literal")
	}

	if _, ok := taskDecl.Body.Stmts[2].(*ast.VarDeclStmt).Value.(*ast.CharLitExpr); !ok {
		t.Fatalf("expected char literal")
	}
}

func TestParseSpreadCallArgument(t *testing.T) {
	file, reporter := parse(t, `
Main :: task(values ...int) {
    Sum(values...)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	mainDecl, ok := file.Decls[0].(*ast.TaskDecl)
	if !ok {
		t.Fatalf(
			"expected TaskDecl, got %T",
			file.Decls[0],
		)
	}

	if len(mainDecl.Body.Stmts) != 1 {
		t.Fatalf(
			"expected 1 statement, got %d",
			len(mainDecl.Body.Stmts),
		)
	}

	stmt, ok := mainDecl.Body.Stmts[0].(*ast.ExprStmt)
	if !ok {
		t.Fatalf(
			"expected ExprStmt, got %T",
			mainDecl.Body.Stmts[0],
		)
	}

	call, ok := stmt.Expr.(*ast.CallExpr)
	if !ok {
		t.Fatalf(
			"expected CallExpr, got %T",
			stmt.Expr,
		)
	}

	if len(call.Args) != 1 {
		t.Fatalf(
			"expected 1 call argument, got %d",
			len(call.Args),
		)
	}

	spread, ok := call.Args[0].(*ast.SpreadExpr)
	if !ok {
		t.Fatalf(
			"expected SpreadExpr, got %T",
			call.Args[0],
		)
	}

	inner, ok := spread.Expr.(*ast.IdentExpr)
	if !ok {
		t.Fatalf(
			"spread operand is %T, expected *ast.IdentExpr",
			spread.Expr,
		)
	}

	if inner.Name.Name != "values" {
		t.Fatalf(
			"spread operand is %q, expected values",
			inner.Name.Name,
		)
	}
}

func TestParsePackageQualifiedSpreadCallArgument(t *testing.T) {
	file, reporter := parse(t, `
Main :: task() {
    fmt.Print("x = %\n", args...)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	mainDecl := file.Decls[0].(*ast.TaskDecl)
	stmt := mainDecl.Body.Stmts[0].(*ast.ExprStmt)
	call := stmt.Expr.(*ast.CallExpr)

	if len(call.Args) != 2 {
		t.Fatalf("expected 2 call args, got %d", len(call.Args))
	}

	if _, ok := call.Args[1].(*ast.SpreadExpr); !ok {
		t.Fatalf("expected second arg to be spread, got %T", call.Args[1])
	}
}

func TestParseGroupedVariadicParameterAppliesOnlyToLastName(t *testing.T) {
	file, reporter := parse(t, `
Example :: task(a, b ...int) {
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	decl := file.Decls[0].(*ast.TaskDecl)

	if len(decl.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(decl.Params))
	}

	if decl.Params[0].IsVariadic {
		t.Fatalf("expected first grouped param to not be variadic")
	}

	if !decl.Params[1].IsVariadic {
		t.Fatalf("expected second grouped param to be variadic")
	}
}

func TestParseMultiVarDeclStmt(t *testing.T) {
	file, reporter := parse(t, `
Foo :: task() rawptr, rawptr {
    return nil, nil
}

Main :: task() {
    a, b := Foo()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	mainDecl := file.Decls[1].(*ast.TaskDecl)
	stmt := mainDecl.Body.Stmts[0].(*ast.MultiVarDeclStmt)

	if len(stmt.Names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(stmt.Names))
	}

	if stmt.Names[0].Name != "a" || stmt.Names[1].Name != "b" {
		t.Fatalf("unexpected names: %#v", stmt.Names)
	}
}

func TestParseMultiVarDeclWithBlankIdentifier(t *testing.T) {
	file, reporter := parse(t, `
Foo :: task() rawptr, rawptr {
    return nil, nil
}

Main :: task() {
    _, b := Foo()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	mainDecl := file.Decls[1].(*ast.TaskDecl)
	stmt := mainDecl.Body.Stmts[0].(*ast.MultiVarDeclStmt)

	if stmt.Names[0].Name != "_" || stmt.Names[1].Name != "b" {
		t.Fatalf("unexpected names: %#v", stmt.Names)
	}
}

func TestParseDistinctDecl(t *testing.T) {
	file, reporter := parse(t, `
EnemyId :: distinct uint
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if len(file.Decls) != 1 {
		t.Fatalf("expected 1 decl, got %d", len(file.Decls))
	}

	decl, ok := file.Decls[0].(*ast.DistinctDecl)
	if !ok {
		t.Fatalf("expected DistinctDecl, got %T", file.Decls[0])
	}

	if decl.Name.Name != "EnemyId" {
		t.Fatalf("expected EnemyId, got %q", decl.Name.Name)
	}
}

func TestParseDynInterfaceDecl(t *testing.T) {
	file, reporter := parse(t, `
Enemy :: dyn interface <T type> {
    Health :: task(e *T) int
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	decl := file.Decls[0].(*ast.InterfaceDecl)

	if !decl.IsDyn {
		t.Fatalf("expected dyn interface")
	}

	if len(decl.GenericParams) != 1 {
		t.Fatalf("expected 1 generic param")
	}
}

func TestParseGenericTaskDeclWithConstraints(t *testing.T) {
	file, reporter := parse(t, `
T :: task <
    defaultZombie Zombie[defaultZombie.id >= cast<Id>(0)],
    player Id
>() bool {
    return defaultZombie.id == player
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	decl := file.Decls[0].(*ast.TaskDecl)

	if len(decl.GenericParams) != 2 {
		t.Fatalf("expected 2 generic params, got %d", len(decl.GenericParams))
	}

	if decl.GenericParams[0].Category != ast.GenericParamValue {
		t.Fatalf("expected first param to be typed comptime value")
	}

	if len(decl.GenericParams[0].Constraints) != 1 {
		t.Fatalf("expected first param to have 1 constraint")
	}

	if decl.GenericParams[1].Category != ast.GenericParamValue {
		t.Fatalf("expected second param to be typed comptime value")
	}
}

func TestParseGenericCategoriesAndConstraints(t *testing.T) {
	file, reporter := parse(t, `
S :: struct <
    T type[health int, Enemy()],
    E enum[North, East],
    U union[Circle, Rectangle],
    F task[(int, bool) f32, f64],
    N int[N > 10],
    B bool,
    Name string
> {
    value T
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	decl := file.Decls[0].(*ast.StructDecl)

	if len(decl.GenericParams) != 7 {
		t.Fatalf("expected 7 generic params, got %d", len(decl.GenericParams))
	}

	want := []ast.GenericParamCategory{
		ast.GenericParamType,
		ast.GenericParamEnum,
		ast.GenericParamUnion,
		ast.GenericParamTask,
		ast.GenericParamInt,
		ast.GenericParamBool,
		ast.GenericParamString,
	}

	for i, category := range want {
		if decl.GenericParams[i].Category != category {
			t.Fatalf("param %d: expected %s, got %s", i, category, decl.GenericParams[i].Category)
		}
	}
}

func TestParseGenericCallWithValueArguments(t *testing.T) {
	file, reporter := parse(t, `
Main :: task() {
    b := T<ZombieDefault, Id{5}>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	mainDecl := file.Decls[0].(*ast.TaskDecl)
	stmt := mainDecl.Body.Stmts[0].(*ast.VarDeclStmt)
	call := stmt.Value.(*ast.CallExpr)

	gen, ok := call.Callee.(*ast.GenericExpr)
	if !ok {
		t.Fatalf("expected generic callee, got %T", call.Callee)
	}

	if len(gen.Args) != 2 {
		t.Fatalf("expected 2 generic args, got %d", len(gen.Args))
	}
}

func TestParseGenericTaskArgumentSpecialization(t *testing.T) {
	file, reporter := parse(t, `
Identity :: task <T type>(value T) T {
    return value
}

Apply :: task <F task[(int) int]>(value int) int {
    return F(value)
}

Main :: task() {
    x := Apply<Identity<int>>(10)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected parser diagnostics:\n%s", reporter.String())
	}

	if file == nil {
		t.Fatalf("expected file")
	}
}

func TestParseNestedGenericTypeArgumentStillWorks(t *testing.T) {
	file, reporter := parse(t, `
Pair :: struct <A type, B type> {
    first A
    second B
}

Box :: struct <T type> {
    value T
}

Main :: task() {
    b: Box<Pair<int, string>> = Box<Pair<int, string>>{
        value = Pair<int, string>{
            first = 1,
            second = "x",
        },
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected parser diagnostics:\n%s", reporter.String())
	}

	if file == nil {
		t.Fatalf("expected file")
	}
}

func TestParseGenericValueConstraintExpression(t *testing.T) {
	file, reporter := parse(t, `
Buffer :: struct <T type, N int[N > 0]> {
    data Array<T, N>
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected parser diagnostics:\n%s",
			reporter.String(),
		)
	}

	if len(file.Decls) != 1 {
		t.Fatalf(
			"expected 1 declaration, got %d",
			len(file.Decls),
		)
	}

	decl, ok := file.Decls[0].(*ast.StructDecl)
	if !ok {
		t.Fatalf(
			"expected StructDecl, got %T",
			file.Decls[0],
		)
	}

	if len(decl.GenericParams) != 2 {
		t.Fatalf(
			"expected 2 generic parameters, got %d",
			len(decl.GenericParams),
		)
	}

	nParam := decl.GenericParams[1]

	if nParam.Category != ast.GenericParamInt {
		t.Fatalf(
			"expected int generic parameter, got %s",
			nParam.Category,
		)
	}

	if len(nParam.Constraints) != 1 {
		t.Fatalf(
			"expected 1 constraint, got %d",
			len(nParam.Constraints),
		)
	}

	constraint, ok :=
		nParam.Constraints[0].(*ast.GenericExprConstraint)
	if !ok {
		t.Fatalf(
			"constraint is %T, expected *ast.GenericExprConstraint",
			nParam.Constraints[0],
		)
	}

	if _, ok := constraint.Expr.(*ast.BinaryExpr); !ok {
		t.Fatalf(
			"constraint expression is %T, expected *ast.BinaryExpr",
			constraint.Expr,
		)
	}

	if len(decl.Fields) != 1 {
		t.Fatalf(
			"expected 1 field, got %d",
			len(decl.Fields),
		)
	}

	if _, ok := decl.Fields[0].Type.(*ast.GenericType); !ok {
		t.Fatalf(
			"data field type is %T, expected *ast.GenericType",
			decl.Fields[0].Type,
		)
	}
}

func TestParseGenericFieldConstraint(t *testing.T) {
	file, reporter := parse(t, `
GetHealth :: task <T type[health int]>(value T) int {
    return value.health
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected parser diagnostics:\n%s", reporter.String())
	}

	if file == nil {
		t.Fatalf("expected file")
	}
}

func TestParseGenericTaskParamConstraintDependingOnTypeParam(t *testing.T) {
	file, reporter := parse(t, `
Apply :: task <T type, F task[(T) T]>(value T) T {
    return F(value)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected parser diagnostics:\n%s", reporter.String())
	}

	if file == nil {
		t.Fatalf("expected file")
	}
}

func parseInterfaceTestSource(
	t *testing.T,
	text string,
) (*ast.File, *diag.Reporter) {
	t.Helper()

	file := source.NewFile("test.seal", text)
	reporter := diag.NewReporter()

	tokens := lexer.New(file, reporter).LexAll()
	parsed := New(tokens, reporter).ParseFile()

	return parsed, reporter
}

func requireNoParserDiagnostics(
	t *testing.T,
	reporter *diag.Reporter,
) {
	t.Helper()

	if reporter.HasErrors() {
		t.Fatalf("unexpected parser diagnostics:\n%s", reporter.String())
	}
}

func TestParseGenericInterfaceAndManualImpl(t *testing.T) {
	file, reporter := parseInterfaceTestSource(t, `
Reader :: interface <Out type> {
	Read :: task(self *self) Out
}

Box :: struct <T type> {
	value T
}

Reader<T> :: impl <T type> Box<T> {
	Read :: task(self *Box<T>) T {
		return self.value
	}
}
`)

	requireNoParserDiagnostics(t, reporter)

	if len(file.Decls) != 3 {
		t.Fatalf("got %d declarations, want 3", len(file.Decls))
	}

	iface, ok := file.Decls[0].(*ast.InterfaceDecl)
	if !ok {
		t.Fatalf("declaration 0 is %T, want *ast.InterfaceDecl", file.Decls[0])
	}

	if iface.IsDyn {
		t.Fatalf("Reader should be a static/default interface")
	}

	if len(iface.GenericParams) != 1 ||
		iface.GenericParams[0].Name.Name != "Out" {
		t.Fatalf("unexpected interface generic parameters: %#v", iface.GenericParams)
	}

	if len(iface.Requirements) != 1 {
		t.Fatalf(
			"got %d requirements, want 1",
			len(iface.Requirements),
		)
	}

	requirement := iface.Requirements[0]
	if requirement.Name.Name != "Read" {
		t.Fatalf(
			"requirement name = %q, want Read",
			requirement.Name.Name,
		)
	}

	if len(requirement.Params) != 1 {
		t.Fatalf(
			"got %d requirement parameters, want 1",
			len(requirement.Params),
		)
	}

	if requirement.Params[0].Name.Name != "self" {
		t.Fatalf(
			"receiver name = %q, want self",
			requirement.Params[0].Name.Name,
		)
	}

	ptr, ok := requirement.Params[0].Type.(*ast.PointerType)
	if !ok {
		t.Fatalf(
			"receiver type is %T, want *ast.PointerType",
			requirement.Params[0].Type,
		)
	}

	if _, ok := ptr.Elem.(*ast.InterfaceSelfType); !ok {
		t.Fatalf(
			"pointer element is %T, want *ast.InterfaceSelfType",
			ptr.Elem,
		)
	}

	impl, ok := file.Decls[2].(*ast.ImplDecl)
	if !ok {
		t.Fatalf("declaration 2 is %T, want *ast.ImplDecl", file.Decls[2])
	}

	if impl.IsDelegated() {
		t.Fatalf("manual impl was parsed as delegated")
	}

	if len(impl.GenericParams) != 1 ||
		impl.GenericParams[0].Name.Name != "T" {
		t.Fatalf("unexpected impl generic parameters: %#v", impl.GenericParams)
	}

	if _, ok := impl.Interface.(*ast.GenericType); !ok {
		t.Fatalf(
			"impl interface is %T, want *ast.GenericType",
			impl.Interface,
		)
	}

	if _, ok := impl.Target.(*ast.GenericType); !ok {
		t.Fatalf(
			"impl target is %T, want *ast.GenericType",
			impl.Target,
		)
	}

	if len(impl.Entries) != 1 || impl.Entries[0].Task == nil {
		t.Fatalf("unexpected impl entries: %#v", impl.Entries)
	}

	body := impl.Entries[0].Task.Body
	if body == nil || len(body.Stmts) != 1 {
		t.Fatalf("unexpected Read body: %#v", body)
	}

	ret, ok := body.Stmts[0].(*ast.ReturnStmt)
	if !ok || len(ret.Values) != 1 {
		t.Fatalf("Read body statement is %#v", body.Stmts[0])
	}

	selector, ok := ret.Values[0].(*ast.SelectorExpr)
	if !ok {
		t.Fatalf(
			"return expression is %T, want *ast.SelectorExpr",
			ret.Values[0],
		)
	}

	selfExpr, ok := selector.Left.(*ast.IdentExpr)
	if !ok || selfExpr.Name.Name != "self" {
		t.Fatalf("selector left side is %#v, want self", selector.Left)
	}

	if selector.Name.Name != "value" {
		t.Fatalf("selected field = %q, want value", selector.Name.Name)
	}
}

func TestParseNestedDelegatedImpl(t *testing.T) {
	file, reporter := parseInterfaceTestSource(t, `
Positioned :: impl Entity using components.transform
`)

	requireNoParserDiagnostics(t, reporter)

	if len(file.Decls) != 1 {
		t.Fatalf("got %d declarations, want 1", len(file.Decls))
	}

	impl, ok := file.Decls[0].(*ast.ImplDecl)
	if !ok {
		t.Fatalf("declaration is %T, want *ast.ImplDecl", file.Decls[0])
	}

	if !impl.IsDelegated() {
		t.Fatalf("expected delegated impl")
	}

	if len(impl.Entries) != 0 {
		t.Fatalf("delegated impl has %d manual entries", len(impl.Entries))
	}

	if len(impl.UsingPath) != 2 {
		t.Fatalf(
			"using path has %d parts, want 2",
			len(impl.UsingPath),
		)
	}

	if impl.UsingPath[0].Name != "components" ||
		impl.UsingPath[1].Name != "transform" {
		t.Fatalf("unexpected using path: %#v", impl.UsingPath)
	}
}

func TestParseImportedGenericInterfaceImpl(t *testing.T) {
	file, reporter := parseInterfaceTestSource(t, `
io.Reader<T> :: impl <T type> Box<T> {
	Read :: task(self *Box<T>) T {
		return self.value
	}
}
`)

	requireNoParserDiagnostics(t, reporter)

	impl, ok := file.Decls[0].(*ast.ImplDecl)
	if !ok {
		t.Fatalf("declaration is %T, want *ast.ImplDecl", file.Decls[0])
	}

	genericInterface, ok := impl.Interface.(*ast.GenericType)
	if !ok {
		t.Fatalf(
			"interface is %T, want *ast.GenericType",
			impl.Interface,
		)
	}

	named, ok := genericInterface.Base.(*ast.NamedType)
	if !ok {
		t.Fatalf(
			"interface base is %T, want *ast.NamedType",
			genericInterface.Base,
		)
	}

	if len(named.Parts) != 2 ||
		named.Parts[0].Name != "io" ||
		named.Parts[1].Name != "Reader" {
		t.Fatalf("unexpected imported interface name: %#v", named.Parts)
	}

	if len(impl.GenericParams) != 1 ||
		impl.GenericParams[0].Name.Name != "T" {
		t.Fatalf("unexpected impl generic parameters: %#v", impl.GenericParams)
	}
}

func TestParseDynamicInterfaceMultireturnRequirement(t *testing.T) {
	file, reporter := parseInterfaceTestSource(t, `
Reader :: dyn interface <Out type> {
	Read :: task(self *self) (Out, bool)
}
`)

	requireNoParserDiagnostics(t, reporter)

	iface, ok := file.Decls[0].(*ast.InterfaceDecl)
	if !ok {
		t.Fatalf("declaration is %T, want *ast.InterfaceDecl", file.Decls[0])
	}

	if !iface.IsDyn {
		t.Fatalf("expected dynamic interface")
	}

	requirement := iface.Requirements[0]
	if len(requirement.Results) != 2 {
		t.Fatalf(
			"got %d results, want 2",
			len(requirement.Results),
		)
	}
}

func TestParseDelegatedImplRejectsBlock(t *testing.T) {
	_, reporter := parseInterfaceTestSource(t, `
Positioned :: impl Entity using transform {
	Position :: task(self *Entity) Vec3 {
		return self.transform.position
	}
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected parser diagnostic")
	}

	if !strings.Contains(
		reporter.String(),
		"delegated impl cannot contain an impl block",
	) {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestRejectDynInterfaceTypeInCast(t *testing.T) {
	_, reporter := parse(t, `
Positioned :: interface {
    X :: task(value *self) int
}

Transform :: struct {
    x int
}

Main :: task() {
    transform := Transform{x = 42}
    positioned := cast<dyn Positioned>(&transform)
}
`)

	if !reporter.HasErrors() {
		t.Fatal("expected dyn in cast type to be rejected")
	}

	got := reporter.String()

	if !strings.Contains(
		got,
		"'dyn' is only valid in an interface declaration",
	) {
		t.Fatalf(
			"expected dyn diagnostic, got:\n%s",
			got,
		)
	}
}

func TestParseReceiverOwnedOverloadDecls(t *testing.T) {
	file, reporter := parse(t, `
[] :: overload {
    GetAt
}

[]= :: overload {
    SetAt
}

len :: overload {
    Length
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected parser diagnostics:\n%s",
			reporter.String(),
		)
	}

	if len(file.Decls) != 3 {
		t.Fatalf(
			"expected 3 declarations, got %d",
			len(file.Decls),
		)
	}

	getOverload, ok := file.Decls[0].(*ast.OverloadDecl)
	if !ok {
		t.Fatalf(
			"declaration 0 is %T, expected *ast.OverloadDecl",
			file.Decls[0],
		)
	}

	if getOverload.Name != "[]" {
		t.Fatalf(
			"overload name is %q, expected []",
			getOverload.Name,
		)
	}

	setOverload, ok := file.Decls[1].(*ast.OverloadDecl)
	if !ok {
		t.Fatalf(
			"declaration 1 is %T, expected *ast.OverloadDecl",
			file.Decls[1],
		)
	}

	if setOverload.Name != "[]=" {
		t.Fatalf(
			"overload name is %q, expected []=",
			setOverload.Name,
		)
	}

	lenOverload, ok := file.Decls[2].(*ast.OverloadDecl)
	if !ok {
		t.Fatalf(
			"declaration 2 is %T, expected *ast.OverloadDecl",
			file.Decls[2],
		)
	}

	if lenOverload.Name != "len" {
		t.Fatalf(
			"overload name is %q, expected len",
			lenOverload.Name,
		)
	}
}

func TestParseIndexReadAndWrite(t *testing.T) {
	file, reporter := parse(t, `
Main :: task(values Buffer) {
    first := values[0]
    values[1] = 42
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected parser diagnostics:\n%s",
			reporter.String(),
		)
	}

	mainDecl := file.Decls[0].(*ast.TaskDecl)

	if len(mainDecl.Body.Stmts) != 2 {
		t.Fatalf(
			"expected 2 statements, got %d",
			len(mainDecl.Body.Stmts),
		)
	}

	readDecl, ok :=
		mainDecl.Body.Stmts[0].(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf(
			"statement 0 is %T, expected *ast.VarDeclStmt",
			mainDecl.Body.Stmts[0],
		)
	}

	if _, ok := readDecl.Value.(*ast.IndexExpr); !ok {
		t.Fatalf(
			"read value is %T, expected *ast.IndexExpr",
			readDecl.Value,
		)
	}

	writeStmt, ok :=
		mainDecl.Body.Stmts[1].(*ast.AssignStmt)
	if !ok {
		t.Fatalf(
			"statement 1 is %T, expected *ast.AssignStmt",
			mainDecl.Body.Stmts[1],
		)
	}

	if _, ok := writeStmt.Left.(*ast.IndexExpr); !ok {
		t.Fatalf(
			"assignment left side is %T, expected *ast.IndexExpr",
			writeStmt.Left,
		)
	}
}
