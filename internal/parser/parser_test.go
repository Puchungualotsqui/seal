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
Buffer :: struct <T type, N int> {
    data [N]T
    len int
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	decl := file.Decls[0].(*ast.StructDecl)

	if len(decl.GenericParams) != 2 {
		t.Fatalf("expected 2 generic params, got %d", len(decl.GenericParams))
	}

	if decl.GenericParams[0].Category != ast.GenericParamType {
		t.Fatalf("expected first param to be type param")
	}

	if decl.GenericParams[1].Category != ast.GenericParamInt {
		t.Fatalf("expected second param to be int param")
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

func TestParseTypedVarDeclWithArray(t *testing.T) {
	file, reporter := parse(t, `
Main :: task() {
    arr: []int = [2, 3, 4]
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

Enemy :: interface <T type> {
    Damage :: task(e *T, damage int)
    Health :: task(e *T) int
}

Enemy<Soldier> :: impl {
    Damage :: DamageSoldier
    Health :: SoldierHealth
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

	impl, ok := file.Decls[3].(*ast.ImplDecl)
	if !ok {
		t.Fatalf("expected ImplDecl")
	}

	if len(impl.Entries) != 2 {
		t.Fatalf("expected 2 impl entries, got %d", len(impl.Entries))
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

func TestParseVariadicArrayOfAny(t *testing.T) {
	file, reporter := parse(t, `
TakeArrays :: task(args ...[10]any) {
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if len(file.Decls) != 1 {
		t.Fatalf("expected 1 decl, got %d", len(file.Decls))
	}

	taskDecl, ok := file.Decls[0].(*ast.TaskDecl)
	if !ok {
		t.Fatalf("expected TaskDecl")
	}

	if len(taskDecl.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(taskDecl.Params))
	}

	if !taskDecl.Params[0].IsVariadic {
		t.Fatalf("expected variadic parameter")
	}

	arrayType, ok := taskDecl.Params[0].Type.(*ast.ArrayType)
	if !ok {
		t.Fatalf("expected variadic element type to be array, got %T", taskDecl.Params[0].Type)
	}

	if arrayType.Inferred {
		t.Fatalf("expected fixed array length")
	}

	namedType, ok := arrayType.Elem.(*ast.NamedType)
	if !ok {
		t.Fatalf("expected array element type to be named type, got %T", arrayType.Elem)
	}

	if len(namedType.Parts) != 1 || namedType.Parts[0].Name != "any" {
		t.Fatalf("expected array element type any, got %+v", namedType.Parts)
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
Main :: task() {
    a: []int = [1, 2, 3]
    Sum(a...)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	mainDecl := file.Decls[0].(*ast.TaskDecl)
	stmt := mainDecl.Body.Stmts[1].(*ast.ExprStmt)
	call := stmt.Expr.(*ast.CallExpr)

	if len(call.Args) != 1 {
		t.Fatalf("expected 1 call arg, got %d", len(call.Args))
	}

	if _, ok := call.Args[0].(*ast.SpreadExpr); !ok {
		t.Fatalf("expected spread arg, got %T", call.Args[0])
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

func TestParseInferredArrayType(t *testing.T) {
	file, reporter := parse(t, `
Main :: task() {
    values: []int = [1, 2, 3]
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	mainDecl := file.Decls[0].(*ast.TaskDecl)
	stmt := mainDecl.Body.Stmts[0].(*ast.VarDeclStmt)

	arrType, ok := stmt.Type.(*ast.ArrayType)
	if !ok {
		t.Fatalf("expected array type, got %T", stmt.Type)
	}

	if !arrType.Inferred {
		t.Fatalf("expected inferred array type")
	}

	elem, ok := arrType.Elem.(*ast.NamedType)
	if !ok {
		t.Fatalf("expected named element type, got %T", arrType.Elem)
	}

	if elem.Parts[0].Name != "int" {
		t.Fatalf("expected int element type, got %q", elem.Parts[0].Name)
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
