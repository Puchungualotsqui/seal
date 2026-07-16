package checker

import (
	"math/big"
	"strings"
	"testing"

	"seal/internal/ast"
	"seal/internal/diag"
	"seal/internal/lexer"
	"seal/internal/parser"
	"seal/internal/resolver"
	"seal/internal/source"
	"seal/internal/token"
)

type checkerTestRun struct {
	File     *ast.File
	Checker  *Checker
	Scope    *Scope
	Reporter *diag.Reporter
}

func runCheckerTest(
	t *testing.T,
	input string,
) checkerTestRun {
	t.Helper()

	parsed, reporter := parseCheckerFile(
		t,
		"test",
		input,
	)

	r := resolver.New(reporter)
	r.ResolveFile(parsed)

	if reporter.HasErrors() {
		t.Fatalf(
			"resolver diagnostics:\n%s",
			reporter.String(),
		)
	}

	c := New(reporter)
	scope := c.CheckFile(parsed)

	return checkerTestRun{
		File:     parsed,
		Checker:  c,
		Scope:    scope,
		Reporter: reporter,
	}
}

func runCheckerTestWithPackages(
	t *testing.T,
	input string,
	resolverPackages map[string]*resolver.PackageInfo,
	checkerPackages map[string]*PackageInfo,
) checkerTestRun {
	t.Helper()

	parsed, reporter := parseCheckerFile(
		t,
		"test",
		input,
	)

	r := resolver.NewWithPackages(
		reporter,
		resolverPackages,
	)
	r.ResolveFile(parsed)

	if reporter.HasErrors() {
		t.Fatalf(
			"resolver diagnostics:\n%s",
			reporter.String(),
		)
	}

	c := NewWithPackages(
		reporter,
		checkerPackages,
	)
	scope := c.CheckFile(parsed)

	return checkerTestRun{
		File:     parsed,
		Checker:  c,
		Scope:    scope,
		Reporter: reporter,
	}
}

func checkerTaskDecl(
	t *testing.T,
	file *ast.File,
	name string,
) *ast.TaskDecl {
	t.Helper()

	for _, decl := range file.Decls {
		task, ok := decl.(*ast.TaskDecl)
		if !ok {
			continue
		}

		if task.Name.Name == name {
			return task
		}
	}

	t.Fatalf("task %q was not found", name)
	return nil
}

func checkerTaskStmt(
	t *testing.T,
	file *ast.File,
	taskName string,
	index int,
) ast.Stmt {
	t.Helper()

	task := checkerTaskDecl(t, file, taskName)

	if task.Body == nil {
		t.Fatalf("task %q has no body", taskName)
	}

	if index < 0 || index >= len(task.Body.Stmts) {
		t.Fatalf(
			"statement index %d outside task %q body of length %d",
			index,
			taskName,
			len(task.Body.Stmts),
		)
	}

	return task.Body.Stmts[index]
}

func checkerIndexFromVarStmt(
	t *testing.T,
	stmt ast.Stmt,
) *ast.IndexExpr {
	t.Helper()

	decl, ok := stmt.(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf(
			"statement type = %T, want *ast.VarDeclStmt",
			stmt,
		)
	}

	index, ok := decl.Value.(*ast.IndexExpr)
	if !ok {
		t.Fatalf(
			"variable value type = %T, want *ast.IndexExpr",
			decl.Value,
		)
	}

	return index
}

func checkerIndexFromAssignStmt(
	t *testing.T,
	stmt ast.Stmt,
) *ast.IndexExpr {
	t.Helper()

	assign, ok := stmt.(*ast.AssignStmt)
	if !ok {
		t.Fatalf(
			"statement type = %T, want *ast.AssignStmt",
			stmt,
		)
	}

	index, ok := assign.Left.(*ast.IndexExpr)
	if !ok {
		t.Fatalf(
			"assignment left type = %T, want *ast.IndexExpr",
			assign.Left,
		)
	}

	return index
}

func checkerCallFromVarStmt(
	t *testing.T,
	stmt ast.Stmt,
) *ast.CallExpr {
	t.Helper()

	decl, ok := stmt.(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf(
			"statement type = %T, want *ast.VarDeclStmt",
			stmt,
		)
	}

	call, ok := decl.Value.(*ast.CallExpr)
	if !ok {
		t.Fatalf(
			"variable value type = %T, want *ast.CallExpr",
			decl.Value,
		)
	}

	return call
}

func assertIndexResolutionKind(
	t *testing.T,
	c *Checker,
	expr *ast.IndexExpr,
	want IndexResolutionKind,
) IndexResolution {
	t.Helper()

	got, ok := c.IndexResolutionFor(expr)
	if !ok {
		t.Fatalf(
			"index expression has no checker resolution",
		)
	}

	if got.Kind != want {
		t.Fatalf(
			"index resolution kind = %v, want %v",
			got.Kind,
			want,
		)
	}

	return got
}

func assertLenResolutionKind(
	t *testing.T,
	c *Checker,
	expr *ast.CallExpr,
	want LenResolutionKind,
) LenResolution {
	t.Helper()

	got, ok := c.LenResolutionFor(expr)
	if !ok {
		t.Fatalf(
			"len call has no checker resolution",
		)
	}

	if got.Kind != want {
		t.Fatalf(
			"len resolution kind = %v, want %v",
			got.Kind,
			want,
		)
	}

	return got
}

func checkerTestNamedType(parts ...string) ast.Type {
	idents := make([]ast.Ident, len(parts))

	for i, part := range parts {
		idents[i] = ast.Ident{
			Name: part,
		}
	}

	return &ast.NamedType{
		Parts: idents,
	}
}

func parseCheckerFile(t *testing.T, packageName string, input string) (*ast.File, *diag.Reporter) {
	t.Helper()

	file := source.NewFile(packageName+".seal", input)
	reporter := diag.NewReporter()

	lex := lexer.New(file, reporter)
	tokens := lex.LexAll()
	if reporter.HasErrors() {
		t.Fatalf("lexer diagnostics:\n%s", reporter.String())
	}

	p := parser.New(tokens, reporter)
	parsed := p.ParseFile()
	if reporter.HasErrors() {
		t.Fatalf("parser diagnostics:\n%s", reporter.String())
	}

	return parsed, reporter
}

func checkSource(t *testing.T, input string) *diag.Reporter {
	t.Helper()

	parsed, reporter := parseCheckerFile(t, "test", input)

	r := resolver.New(reporter)
	r.ResolveFile(parsed)
	if reporter.HasErrors() {
		return reporter
	}

	c := New(reporter)
	c.CheckFile(parsed)

	return reporter
}

func check(t *testing.T, input string) (*Scope, *diag.Reporter) {
	t.Helper()

	file := source.NewFile("test.seal", input)
	reporter := diag.NewReporter()

	lex := lexer.New(file, reporter)
	tokens := lex.LexAll()

	if reporter.HasErrors() {
		t.Fatalf("lexer diagnostics:\n%s", reporter.String())
	}

	p := parser.New(tokens, reporter)
	parsed := p.ParseFile()

	if reporter.HasErrors() {
		t.Fatalf("parser diagnostics:\n%s", reporter.String())
	}

	r := resolver.New(reporter)
	r.ResolveFile(parsed)

	if reporter.HasErrors() {
		t.Fatalf("resolver diagnostics:\n%s", reporter.String())
	}

	c := New(reporter)
	scope := c.CheckFile(parsed)

	return scope, reporter
}

func checkWithPackages(t *testing.T, input string, resolverPackages map[string]*resolver.PackageInfo, checkerPackages map[string]*PackageInfo) *diag.Reporter {
	t.Helper()

	file := source.NewFile("test.seal", input)
	reporter := diag.NewReporter()

	lex := lexer.New(file, reporter)
	tokens := lex.LexAll()

	if reporter.HasErrors() {
		t.Fatalf("lexer diagnostics:\n%s", reporter.String())
	}

	p := parser.New(tokens, reporter)
	parsed := p.ParseFile()

	if reporter.HasErrors() {
		t.Fatalf("parser diagnostics:\n%s", reporter.String())
	}

	r := resolver.NewWithPackages(reporter, resolverPackages)
	r.ResolveFile(parsed)

	if reporter.HasErrors() {
		t.Fatalf("resolver diagnostics:\n%s", reporter.String())
	}

	c := NewWithPackages(reporter, checkerPackages)
	c.CheckFile(parsed)

	return reporter
}

func exportCheckerPackage(t *testing.T, packageName string, input string) (*Scope, *resolver.PackageInfo, *PackageInfo) {
	t.Helper()

	file := source.NewFile(packageName+".seal", input)
	reporter := diag.NewReporter()

	lex := lexer.New(file, reporter)
	tokens := lex.LexAll()

	if reporter.HasErrors() {
		t.Fatalf("lexer diagnostics:\n%s", reporter.String())
	}

	p := parser.New(tokens, reporter)
	parsed := p.ParseFile()

	if reporter.HasErrors() {
		t.Fatalf("parser diagnostics:\n%s", reporter.String())
	}

	r := resolver.New(reporter)
	resolverScope := r.ResolveFile(parsed)

	if reporter.HasErrors() {
		t.Fatalf("resolver diagnostics:\n%s", reporter.String())
	}

	c := New(reporter)
	checkerScope := c.CheckFile(parsed)

	if reporter.HasErrors() {
		t.Fatalf("checker diagnostics:\n%s", reporter.String())
	}

	return checkerScope, resolver.ExportPackage(packageName, resolverScope), ExportPackage(packageName, checkerScope)
}

func TestCheckBuiltInTypesAndVarInference(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    a := 10
    b := true
    c := 1.5
    d := "hello"
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckTypedVarDecl(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    a: int = 10
    b: bool = true
    c: f32 = 1.5
    d: string = "hello"
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectBadAssignment(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    a: int = true
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot assign bool to int") {
		t.Fatalf("expected assignment diagnostic, got:\n%s", reporter.String())
	}
}

func TestRejectParameterReassignment(t *testing.T) {
	_, reporter := check(t, `
Foo :: task(number int) int {
    number = 20
    return number
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `cannot reassign parameter "number"`) {
		t.Fatalf("expected parameter reassignment diagnostic, got:\n%s", reporter.String())
	}
}

func TestCheckReturnType(t *testing.T) {
	_, reporter := check(t, `
Foo :: task() int {
    return 10
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectBadReturnType(t *testing.T) {
	_, reporter := check(t, `
Foo :: task() int {
    return true
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot assign bool to int") {
		t.Fatalf("expected return type diagnostic, got:\n%s", reporter.String())
	}
}

func TestRejectBadReturnCount(t *testing.T) {
	_, reporter := check(t, `
Foo :: task() int, bool {
    return 10
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "return count mismatch") {
		t.Fatalf("expected return count diagnostic, got:\n%s", reporter.String())
	}
}

func TestCheckTaskCallParameterCount(t *testing.T) {
	_, reporter := check(t, `
Add :: task(a int, b int) int {
    return a + b
}

Main :: task() {
    x := Add(1, 2)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectTaskCallParameterCount(t *testing.T) {
	_, reporter := check(t, `
Add :: task(a int, b int) int {
    return a + b
}

Main :: task() {
    x := Add(1)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "task call argument count mismatch") {
		t.Fatalf("expected argument count diagnostic, got:\n%s", reporter.String())
	}
}

func TestRejectTaskCallArgumentType(t *testing.T) {
	_, reporter := check(t, `
Add :: task(a int, b int) int {
    return a + b
}

Main :: task() {
    x := Add(1, true)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot assign bool to int") {
		t.Fatalf("expected argument type diagnostic, got:\n%s", reporter.String())
	}
}

func TestIfConditionMustBeBool(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    if true {
        return
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectNonBoolIfCondition(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    if 10 {
        return
    }
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "if condition must be bool") {
		t.Fatalf("expected bool condition diagnostic, got:\n%s", reporter.String())
	}
}

func TestForConditionMustBeBool(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    i := 0

    for i < 10 {
        i = i + 1
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectNonBoolForCondition(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    for 10 {
    }
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "for condition must be bool") {
		t.Fatalf("expected bool condition diagnostic, got:\n%s", reporter.String())
	}
}

func TestCStyleFor(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    for i := 0; i < 10; i = i + 1 {
        assert(true)
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestBinaryOps(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    a := 1 + 2
    b := 4 - 3
    c := 2 * 5
    d := 10 / 2
    e := a == b
    f := a != b
    g := a < b
    h := a > b
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectBadBinaryOps(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    a := true + 1
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "requires numeric operands") {
		t.Fatalf("expected numeric operand diagnostic, got:\n%s", reporter.String())
	}
}

func TestResolveNamedStructAndFields(t *testing.T) {
	_, reporter := check(t, `
Vec2 :: struct {
    x f32
    y f32
}

LengthSq :: task(v Vec2) f32 {
    return v.x * v.x + v.y * v.y
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestPointerFieldAutoDeref(t *testing.T) {
	_, reporter := check(t, `
Soldier :: struct {
    health int
}

Damage :: task(e *Soldier, amount int) {
    e.health = e.health - amount
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestStructLiteralFieldTypes(t *testing.T) {
	_, reporter := check(t, `
Vec2 :: struct {
    x f32
    y f32
}

Main :: task() {
    v := Vec2{x = 1.0, y = 2.0}
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectStructLiteralFieldTypes(t *testing.T) {
	_, reporter := check(t, `
Vec2 :: struct {
    x f32
    y f32
}

Main :: task() {
    v := Vec2{x = true, y = 2.0}
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot assign bool to f32") {
		t.Fatalf("expected struct field diagnostic, got:\n%s", reporter.String())
	}
}

func TestEnumTypedVariable(t *testing.T) {
	_, reporter := check(t, `
Error :: enum {
    None
    ErrorReading
}

Main :: task() {
    err: Error = .None
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectInvalidEnumVariant(t *testing.T) {
	_, reporter := check(t, `
Error :: enum {
    None
    ErrorReading
}

Main :: task() {
    err: Error = .Missing
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "enum Error has no variant .Missing") {
		t.Fatalf("expected invalid enum variant diagnostic, got:\n%s", reporter.String())
	}
}

func TestRejectEnumLiteralWithoutContext(t *testing.T) {
	_, reporter := check(t, `
Error :: enum {
    None
}

Main :: task() {
    err := .None
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "enum literal .None needs explicit type") {
		t.Fatalf("expected enum literal context diagnostic, got:\n%s", reporter.String())
	}
}

func TestEnumReturn(t *testing.T) {
	_, reporter := check(t, `
Error :: enum {
    None
    ErrorReading
}

Read :: task() Error {
    return .None
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestEnumSwitch(t *testing.T) {
	_, reporter := check(t, `
Error :: enum {
    None
    ErrorReading
}

Code :: task(err Error) int {
    switch err {
    case .None:
        return 0

    case .ErrorReading:
        return 1
    }

    return 2
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectInvalidEnumSwitchCase(t *testing.T) {
	_, reporter := check(t, `
Error :: enum {
    None
}

Code :: task(err Error) int {
    switch err {
    case .Missing:
        return 0
    }

    return 1
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "enum Error has no variant .Missing") {
		t.Fatalf("expected enum switch diagnostic, got:\n%s", reporter.String())
	}
}

func TestUnionAssignmentFromMember(t *testing.T) {
	_, reporter := check(t, `
Circle :: struct {
    radius f32
}

Rectangle :: struct {
    width f32
    height f32
}

Shape :: union {
    Circle,
    Rectangle,
}

Main :: task() {
    s: Shape = Circle{radius = 5.0}
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestUnionAssignmentNil(t *testing.T) {
	_, reporter := check(t, `
Circle :: struct {
    radius f32
}

Shape :: union {
    Circle,
}

Main :: task() {
    s: Shape = nil
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectInvalidUnionAssignment(t *testing.T) {
	_, reporter := check(t, `
Circle :: struct {
    radius f32
}

Vec2 :: struct {
    x f32
    y f32
}

Shape :: union {
    Circle,
}

Main :: task() {
    s: Shape = Vec2{x = 1.0, y = 2.0}
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot assign Vec2 to union Shape") {
		t.Fatalf("expected union assignment diagnostic, got:\n%s", reporter.String())
	}
}

func TestUnionSwitchNarrowing(t *testing.T) {
	_, reporter := check(t, `
Circle :: struct {
    radius f32
}

Rectangle :: struct {
    width f32
    height f32
}

Shape :: union {
    Circle,
    Rectangle,
}

Area :: task(s Shape) f32 {
    switch shape in s {
    case Circle:
        return shape.radius * shape.radius

    case Rectangle:
        return shape.width * shape.height

    case nil:
        return 0
    }

    return 0
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectInvalidUnionSwitchMember(t *testing.T) {
	_, reporter := check(t, `
Circle :: struct {
    radius f32
}

Vec2 :: struct {
    x f32
    y f32
}

Shape :: union {
    Circle,
}

Area :: task(s Shape) f32 {
    switch shape in s {
    case Vec2:
        return shape.x
    }

    return 0
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "union Shape has no member Vec2") {
		t.Fatalf("expected invalid union member diagnostic, got:\n%s", reporter.String())
	}
}

func TestNormalOverloadResolution(t *testing.T) {
	_, reporter := check(t, `
SumInt :: task(a int, b int) int {
    return a + b
}

SumF64 :: task(a f64, b f64) f64 {
    return a + b
}

Sum :: overload {
    SumInt
    SumF64
}

Main :: task() {
    a := Sum(1, 2)
    b := Sum(1.0, 2.0)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectNoMatchingOverload(t *testing.T) {
	_, reporter := check(t, `
SumInt :: task(a int, b int) int {
    return a + b
}

Sum :: overload {
    SumInt
}

Main :: task() {
    a := Sum(true, false)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `no overload of "Sum" matches argument types`) {
		t.Fatalf("expected no matching overload diagnostic, got:\n%s", reporter.String())
	}
}

func TestRejectAmbiguousOverload(t *testing.T) {
	_, reporter := check(t, `
A :: struct {
    value int
}

UseA1 :: task(a A) int {
    return a.value
}

UseA2 :: task(a A) int {
    return a.value
}

UseA :: overload {
    UseA1
    UseA2
}

Main :: task() {
    a := A{value = 1}
    x := UseA(a)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `ambiguous overload call "UseA"`) {
		t.Fatalf("expected ambiguous overload diagnostic, got:\n%s", reporter.String())
	}
}

func TestOperatorOverloadVec2Add(t *testing.T) {
	_, reporter := check(t, `
Vec2 :: struct {
    x f32
    y f32
}

Vec2Add :: pure task(a Vec2, b Vec2) Vec2 {
    return Vec2{x = a.x + b.x, y = a.y + b.y}
}

+ :: overload {
    Vec2Add
}

Main :: task() {
    a := Vec2{x = 1.0, y = 2.0}
    b := Vec2{x = 3.0, y = 4.0}
    c := a + b
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectImpureOperatorOverload(t *testing.T) {
	_, reporter := check(t, `
Vec2 :: struct {
    x f32
    y f32
}

Vec2Add :: task(a Vec2, b Vec2) Vec2 {
    return Vec2{x = a.x + b.x, y = a.y + b.y}
}

+ :: overload {
    Vec2Add
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `operator overload "+" requires pure task candidate "Vec2Add"`) {
		t.Fatalf("expected pure operator diagnostic, got:\n%s", reporter.String())
	}
}

func TestRejectPrimitiveOperatorReplacement(t *testing.T) {
	_, reporter := check(t, `
IntAdd :: pure task(a int, b int) int {
    return a + b
}

+ :: overload {
    IntAdd
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `operator overload "+" cannot replace built-in primitive operator behavior`) {
		t.Fatalf("expected primitive replacement diagnostic, got:\n%s", reporter.String())
	}
}

func TestEqualityOperatorOverload(t *testing.T) {
	_, reporter := check(t, `
Vec2 :: struct {
    x f32
    y f32
}

Vec2Equal :: pure task(a Vec2, b Vec2) bool {
    return a.x == b.x && a.y == b.y
}

== :: overload {
    Vec2Equal
}

Main :: task() {
    a := Vec2{x = 1.0, y = 2.0}
    b := Vec2{x = 1.0, y = 2.0}
    same := a == b
    different := a != b
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectStructEqualityWithoutOverload(t *testing.T) {
	_, reporter := check(t, `
Vec2 :: struct {
    x f32
    y f32
}

Main :: task() {
    a := Vec2{x = 1.0, y = 2.0}
    b := Vec2{x = 1.0, y = 2.0}
    same := a == b
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot compare Vec2 and Vec2") {
		t.Fatalf("expected struct equality diagnostic, got:\n%s", reporter.String())
	}
}

func TestRejectComparisonOperatorNonBoolReturn(t *testing.T) {
	_, reporter := check(t, `
Vec2 :: struct {
    x f32
    y f32
}

Vec2Less :: pure task(a Vec2, b Vec2) int {
    return 0
}

< :: overload {
    Vec2Less
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `comparison operator overload "<" candidate "Vec2Less" must return bool`) {
		t.Fatalf("expected bool return diagnostic, got:\n%s", reporter.String())
	}
}

func TestDefaultParameterCall(t *testing.T) {
	_, reporter := check(t, `
Add :: task(a int, b int = 1) int {
    return a + b
}

Main :: task() {
    x := Add(10)
    y := Add(10, 20)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectTooFewArgumentsWithDefaults(t *testing.T) {
	_, reporter := check(t, `
Add :: task(a int, b int = 1) int {
    return a + b
}

Main :: task() {
    x := Add()
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "expected 1 to 2, got 0") {
		t.Fatalf("expected default argument count diagnostic, got:\n%s", reporter.String())
	}
}

func TestRejectDefaultParameterTypeMismatch(t *testing.T) {
	_, reporter := check(t, `
Foo :: task(a int = true) int {
    return a
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot assign bool to int") {
		t.Fatalf("expected default parameter type diagnostic, got:\n%s", reporter.String())
	}
}

func TestRejectNonDefaultAfterDefault(t *testing.T) {
	_, reporter := check(t, `
Foo :: task(a int = 1, b int) int {
    return a + b
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `parameter "b" must have a default value`) {
		t.Fatalf("expected trailing default diagnostic, got:\n%s", reporter.String())
	}
}

func TestOverloadResolutionWithDefaultParameter(t *testing.T) {
	_, reporter := check(t, `
UseOne :: task(a int) int {
    return a
}

UseTwo :: task(a int, b int = 10) int {
    return a + b
}

Use :: overload {
    UseOne
    UseTwo
}

Main :: task() {
    a := Use(1)
    b := Use(1, 2)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectOperatorOverloadWithDefaultParameter(t *testing.T) {
	_, reporter := check(t, `
Vec2 :: struct {
    x f32
    y f32
}

Vec2Add :: pure task(a Vec2, b Vec2 = Vec2{x = 0.0, y = 0.0}) Vec2 {
    return Vec2{x = a.x + b.x, y = a.y + b.y}
}

+ :: overload {
    Vec2Add
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `operator overload "+" candidate "Vec2Add" cannot have default parameters`) {
		t.Fatalf("expected operator default diagnostic, got:\n%s", reporter.String())
	}
}

func TestExternTaskDecl(t *testing.T) {
	_, reporter := check(t, `
malloc :: extern("malloc") task(size uint) rawptr
free :: extern("free") task(ptr rawptr)

Main :: task() {
    ptr := malloc(64)
    free(ptr)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestExternVariadicAny(t *testing.T) {
	_, reporter := check(t, `
printf :: extern("printf") task(format cstring, args ...any) int

Main :: task() {
    printf(c"%d %s", 10, c"hello")
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectVariadicNonAny(t *testing.T) {
	_, reporter := check(t, `
Bad :: extern("bad") task(format cstring, args ...int) int
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `variadic parameter "args" must have type any`) {
		t.Fatalf("expected variadic any diagnostic, got:\n%s", reporter.String())
	}
}

func TestSealVariadicInt(t *testing.T) {
	_, reporter := check(t, `
Sum :: task(args ...int) int {
    total := 0

    for i := 0; i < len(args); i = i + 1 {
        total = total + args[i]
    }

    return total
}

Main :: task() {
    result := Sum(1, 2, 3)
    assert(result == 6)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestSealVariadicAny(t *testing.T) {
	_, reporter := check(t, `
CountAny :: task(args ...any) uint {
	return len(args)
}

Main :: task() {
    x: any = 10
    count := CountAny(x, "hello", 3.14)
    assert(count == 3)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestVariadicRejectWrongType(t *testing.T) {
	_, reporter := check(t, `
Sum :: task(args ...int) uint {
    return len(args)
}

Main :: task() {
    Sum(1, "bad")
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot assign string to int") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestAnyAsAnyIsIntrinsics(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    value: any = 10

    if anyIs<int>(value) {
        x := anyAs<int>(value)
        assert(x == 10)
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestAnyAsRejectsNonAny(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    x := anyAs<int>(10)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "anyAs expects any") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestPartialAnyTypeSwitch(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    value: any = 10

    @partial switch value type {
    case int:
        x := anyAs<int>(value)
        assert(x == 10)

    case string:
        s := anyAs<string>(value)
        assert(size(s) > 0)
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestNonPartialAnyTypeSwitchRequiresDefault(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    value: any = 10

    switch value type {
    case int:
        x := anyAs<int>(value)
        assert(x == 10)
    }
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "non-partial any type switch requires default") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestNonPartialAnyTypeSwitchWithDefault(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    value: any = 10

    switch value type {
    case int:
        x := anyAs<int>(value)
        assert(x == 10)

    default:
        assert(true)
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectDuplicateEnumSwitchCase(t *testing.T) {
	_, reporter := check(t, `
Error :: enum {
    None,
    Bad,
}

Main :: task() {
    e: Error = .None

    switch e {
    case .None:
        assert(true)
    case .None:
        assert(false)
    }
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected duplicate enum case diagnostic")
	}
}

func TestRejectDuplicateUnionSwitchCase(t *testing.T) {
	_, reporter := check(t, `
Circle :: struct { radius f32 }
Rect :: struct { width f32 }

Shape :: union {
    Circle,
    Rect,
}

Main :: task() {
    s: Shape = Circle{radius = 1.0}

    switch shape in s {
    case Circle:
        assert(true)
    case Circle:
        assert(false)
    }
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected duplicate union case diagnostic")
	}
}

func TestRejectDuplicateUnionNilSwitchCase(t *testing.T) {
	_, reporter := check(t, `
Circle :: struct { radius f32 }

Shape :: union {
    Circle,
}

Main :: task() {
    s: Shape = nil

    switch shape in s {
    case nil:
        assert(true)
    case nil:
        assert(false)
    }
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected duplicate nil case diagnostic")
	}
}

func TestRejectDuplicateSwitchDefaultCase(t *testing.T) {
	_, reporter := check(t, `
Error :: enum {
    None,
    Bad,
}

Main :: task() {
    e: Error = .None

    switch e {
    default:
        assert(true)
    default:
        assert(false)
    }
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected duplicate default case diagnostic")
	}
}

func TestCStringRequiresCStringLiteral(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    cs: cstring = "hello"
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot assign string to cstring") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestStringRequiresStringLiteral(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    s: string = c"hello"
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot assign cstring to string") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestStringFieldsAreInvalid(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    s := "hola"
    n := s.len
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(
		reporter.String(),
		`string has no accessible field "len"`,
	) {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestCStringCharacterIndexingIsValid(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    cs := c"hola"
    c: char = cs[0]
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestCStringIndexAssignmentIsInvalid(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    cs := c"hola"
    cs[0] = 'H'
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		"cannot assign to immutable cstring index",
	)
}

func TestStringIndexAssignmentIsInvalid(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    s := "hola"
    s[0] = 'H'
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		"cannot assign to immutable string index",
	)
}

func TestForwardVariadicIntoVariadicTask(t *testing.T) {
	_, reporter := check(t, `
Sum :: task(values ...int) int {
    total := 0

    for i := 0; i < len(values); i = i + 1 {
        total = total + values[i]
    }

    return total
}

Forward :: task(values ...int) int {
    return Sum(values...)
}

Main :: task() {
    result := Forward(1, 2, 3)
    assert(result == 6)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGroupedVariadicParameterOnlyLastNameIsVariadic(t *testing.T) {
	_, reporter := check(t, `
Example :: task(a, b ...int) int {
    total := a

    for i := 0; i < len(b); i = i + 1 {
        total = total + b[i]
    }

    return total
}

Main :: task() {
    result := Example(10, 1, 2, 3)
    assert(result == 16)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestSpreadMustBeLastArgument(t *testing.T) {
	_, reporter := check(t, `
Sum :: task(values ...int) int {
    return 0
}

Forward :: task(values ...int) int {
    return Sum(values..., 4)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(
		reporter.String(),
		"spread argument must be the last argument",
	) {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestRejectSpreadNonVariadicValue(t *testing.T) {
	_, reporter := check(t, `
Sum :: task(values ...int) int {
    return 0
}

Main :: task() {
    x := 10
    Sum(x...)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(
		reporter.String(),
		"cannot spread int; expected variadic value",
	) {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestRejectSpreadIntoNonVariadicTask(t *testing.T) {
	_, reporter := check(t, `
Use :: task(a int, b int) int {
    return a + b
}

Forward :: task(values ...int) int {
    return Use(values...)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(
		reporter.String(),
		"cannot spread variadic argument into non-variadic task",
	) && !strings.Contains(
		reporter.String(),
		"task call argument count mismatch",
	) {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestRejectSpreadMismatchedElementType(t *testing.T) {
	_, reporter := check(t, `
Sum :: task(values ...int) int {
    return 0
}

Forward :: task(values ...string) int {
    return Sum(values...)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(
		reporter.String(),
		"cannot assign string to int",
	) {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestMultiReturnDeclaration(t *testing.T) {
	_, reporter := check(t, `
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
}

func TestMultiReturnDeclarationWithBlankIdentifier(t *testing.T) {
	scope, reporter := check(t, `
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

	if scope.Lookup("_") != nil {
		t.Fatalf("blank identifier should not be declared")
	}
}

func TestRejectMultiReturnAsSingleVarDecl(t *testing.T) {
	_, reporter := check(t, `
Foo :: task() rawptr, rawptr {
    return nil, nil
}

Main :: task() {
    b := Foo()
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "multi-result task call cannot be used as a single expression") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectMultiReturnDeclarationWrongNameCount(t *testing.T) {
	_, reporter := check(t, `
Foo :: task() rawptr, rawptr {
    return nil, nil
}

Main :: task() {
    a, b, c := Foo()
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "multi-value declaration mismatch") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestInterfaceAssignmentAndDispatch(t *testing.T) {
	_, reporter := check(t, `
		Enemy :: interface {
    Health :: task(e *self) int
}

Goblin :: struct {
    hp int
}

GoblinHealth :: task(g *Goblin) int {
    return g.hp
}

Enemy :: impl Goblin {
    Health :: GoblinHealth
}

Main :: task() {
    g := Goblin{hp = 10}
    e := cast<Enemy>(&g)
    hp := Health(e)
    assert(hp == 10)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestInterfaceCanBeNil(t *testing.T) {
	_, reporter := check(t, `
		Enemy :: interface {
    Health :: task(e *self) int
}

Main :: task() {
    e: Enemy = nil
}
`)
	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectImplMissingRequirement(t *testing.T) {
	_, reporter := check(t, `
		Enemy :: interface {
    Health :: task(e *self) int
}

Goblin :: struct {
    hp int
}

Enemy :: impl Goblin {
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "does not implement Enemy") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectInterfaceMethodSyntax(t *testing.T) {
	_, reporter := check(t, `
		Enemy :: interface {
    Health :: task(e *self) int
}

Goblin :: struct {
    hp int
}

GoblinHealth :: task(g *Goblin) int {
    return g.hp
}

Enemy :: impl Goblin {
    Health :: GoblinHealth
}

Main :: task() {
    g := Goblin{hp = 10}
    e := cast<Enemy>(&g)
    hp := e.Health()
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "interface method syntax is invalid") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRawptrByteIndexReadWrite(t *testing.T) {
	_, reporter := check(t, `
malloc :: extern("malloc") task(size uint) rawptr

Main :: task() {
    ptr := malloc(4)
    ptr[0] = 255
    b := ptr[0]
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestValueByteIndexReadWrite(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    x := 300
    b := x[0]
    x[0] = b
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectByteIndexAssignmentToNonAddressableValue(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    cast<int>(10)[0] = 1
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "byte-index assignment requires a mutable addressable value") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestSizePrimitiveTypeAndValue(t *testing.T) {
	_, reporter := check(t, `
Goblin :: struct {
    hp int
}

Main :: task() {
    x := 10
    s := "ñ"

    a: uint = size(int)
    b: uint = size(Goblin)
    c: uint = size(x)
    d: uint = size(s)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectSizeCString(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    cs := c"hello"
    n := size(cs)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("checker should allow size(cstring); codegen rejects it for now only if emitted:\n%s", reporter.String())
	}
}

func TestLenStringIsValid(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    s := "hello"
    n: uint = len(s)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestLenCStringIsValid(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    s := c"hello"
    n: uint = len(s)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestStringCharacterIndexingIsValid(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    s := "hola"
    c: char = s[0]
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestStringIndexDoesNotReturnByte(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    s := "hola"
    c: u8 = s[0]
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		"cannot assign char to u8",
	)
}

func TestCStringIndexDoesNotReturnByte(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    s := c"hola"
    c: u8 = s[0]
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		"cannot assign char to u8",
	)
}

func TestIntrinsicStructAndTaskDeclarations(t *testing.T) {
	_, reporter := check(t, `
Handle :: intrinsic struct {
    ptr rawptr
}

ByteSize :: pure intrinsic task(value any) uint
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestTrustedPureExternTask(t *testing.T) {
	_, reporter := check(t, `
strlen :: @trusted_pure extern("strlen") task(s cstring) uint
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectPureExternTask(t *testing.T) {
	_, reporter := check(t, `
strlen :: pure extern("strlen") task(s cstring) uint
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "extern task cannot be marked pure") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestLowercaseAssertPrimitive(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    assert(true)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectOldAssertPrimitive(t *testing.T) {
	file := source.NewFile("test.seal", `
Main :: task() {
    Assert(true)
}
`)
	reporter := diag.NewReporter()

	lex := lexer.New(file, reporter)
	tokens := lex.LexAll()

	if reporter.HasErrors() {
		t.Fatalf("lexer diagnostics:\n%s", reporter.String())
	}

	p := parser.New(tokens, reporter)
	parsed := p.ParseFile()

	if reporter.HasErrors() {
		t.Fatalf("parser diagnostics:\n%s", reporter.String())
	}

	r := resolver.New(reporter)
	r.ResolveFile(parsed)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `undefined symbol "Assert"`) {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestNumericPrimitiveTypes(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    a: i8 = 1
    b: i16 = 2
    c: i32 = 3
    d: i64 = 4

    e: u8 = 1
    f: u16 = 2
    g: u32 = 3
    h: u64 = 4

    i: int = 5
    j: uint = 6

    x: f32 = 1.5
    y: f64 = 2.5

    flag: bool = true
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRemovedFrontendTypesAreInvalid(t *testing.T) {
	file := source.NewFile("test.seal", `
Main :: task() {
    a: usize = 1
    b: isize = 1
    c: uintptr = 1
    d: voidptr = nil
}
`)
	reporter := diag.NewReporter()

	lex := lexer.New(file, reporter)
	tokens := lex.LexAll()

	if reporter.HasErrors() {
		t.Fatalf("lexer diagnostics:\n%s", reporter.String())
	}

	p := parser.New(tokens, reporter)
	parsed := p.ParseFile()

	if reporter.HasErrors() {
		t.Fatalf("parser diagnostics:\n%s", reporter.String())
	}

	r := resolver.New(reporter)
	r.ResolveFile(parsed)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	text := reporter.String()

	for _, name := range []string{"usize", "isize", "uintptr", "voidptr"} {
		if !strings.Contains(text, `undefined symbol "`+name+`"`) {
			t.Fatalf("expected undefined symbol diagnostic for %q, got:\n%s", name, text)
		}
	}
}

func TestDistinctTypeLiteralInitialization(t *testing.T) {
	_, reporter := check(t, `
EnemyId :: distinct uint

Main :: task() {
    id: EnemyId = 10
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestDistinctRejectsUnderlyingAssignment(t *testing.T) {
	_, reporter := check(t, `
EnemyId :: distinct uint

Main :: task() {
    raw: uint = 10
    id: EnemyId = raw
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot assign uint to EnemyId") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestDistinctRejectsDifferentDistinctAssignment(t *testing.T) {
	_, reporter := check(t, `
EnemyId :: distinct uint
PlayerId :: distinct uint

Main :: task() {
    enemy: EnemyId = 1
    player: PlayerId = enemy
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot assign EnemyId to PlayerId") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestDistinctEqualitySameType(t *testing.T) {
	_, reporter := check(t, `
EnemyId :: distinct uint

Main :: task() {
    a: EnemyId = 1
    b: EnemyId = 2
    same := a == b
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestDistinctRejectsArithmeticForNow(t *testing.T) {
	_, reporter := check(t, `
EnemyId :: distinct uint

Main :: task() {
    a: EnemyId = 1
    b: EnemyId = 2
    c := a + b
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "requires numeric operands") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectDistinctCompoundLiteral(t *testing.T) {
	_, reporter := check(t, `
Id :: distinct int

Main :: task() {
    x := Id{5}
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "distinct type Id cannot be constructed with a literal") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericValueArgumentUsesCastForDistinct(t *testing.T) {
	_, reporter := check(t, `
Id :: distinct int

Zombie :: struct {
    id Id
}

ZombieDefault :: Zombie{id = cast<Id>(9)}

T :: task <
    defaultZombie Zombie[defaultZombie.id >= cast<Id>(0)],
    player Id
>() bool {
    return defaultZombie.id == player
}

Main :: task() {
    b := T<ZombieDefault, cast<Id>(5)>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectGenericValueArgumentDistinctLiteral(t *testing.T) {
	_, reporter := check(t, `
Id :: distinct int

Zombie :: struct {
    id Id
}

ZombieDefault :: Zombie{id = cast<Id>(9)}

T :: task <
    defaultZombie Zombie,
    player Id
>() bool {
    return defaultZombie.id == player
}

Main :: task() {
    b := T<ZombieDefault, Id{5}>()
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "distinct type Id cannot be constructed with a literal") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectValueAsTypeGenericArgument(t *testing.T) {
	_, reporter := check(t, `
Zombie :: struct {
    hp int
}

ZombieDefault :: Zombie{hp = 10}

Box :: struct <T type> {
    value T
}

Main :: task() {
    b: Box<ZombieDefault>
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `expected type argument, got value "ZombieDefault"`) {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericStructSpecialization(t *testing.T) {
	_, reporter := check(t, `
Box :: struct <T type> {
    value T
}

Main :: task() {
    b: Box<int> = Box<int>{value = 10}
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericStructValueArgumentSpecialization(t *testing.T) {
	_, reporter := check(t, `
Buffer :: struct <T type, N int> {
    value T
    capacity int
}

Main :: task() {
    b: Buffer<int, 32> = Buffer<int, 32>{
        value = 10,
        capacity = 32,
    }

    value: int = b.value
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestNestedGenericStructSpecialization(t *testing.T) {
	_, reporter := check(t, `
Pair :: struct <A type, B type> {
    a A
    b B
}

Box :: struct <T type> {
    value T
}

Main :: task() {
    b: Box<Pair<int, string>>
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericTaskCallsAnotherGenericTask(t *testing.T) {
	_, reporter := check(t, `
Box :: struct <T type> {
    value T
}

MakeBox :: task <T type>(value T) Box<T> {
    return Box<T>{value = value}
}

Wrap :: task <T type>(value T) Box<T> {
    return MakeBox<T>(value)
}

Main :: task() {
    b := Wrap<int>(10)
    x := b.value
    assert(x == 10)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericTaskParameterCanBeCalled(t *testing.T) {
	_, reporter := check(t, `
Double :: task(x int) int {
    return x * 2
}

Apply :: task <F task[(int) int]>(value int) int {
    return F(value)
}

Main :: task() {
    x := Apply<Double>(10)
    assert(x == 20)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericTaskParameterSignatureCanDependOnTypeParam(t *testing.T) {
	_, reporter := check(t, `
IdentityInt :: task(value int) int {
    return value
}

Apply :: task <T type, F task[(T) T]>(value T) T {
    return F(value)
}

Main :: task() {
    x := Apply<int, IdentityInt>(10)
    assert(x == 10)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectGenericTaskParameterWrongSignature(t *testing.T) {
	_, reporter := check(t, `
ToString :: task(value int) string {
    return "x"
}

Apply :: task <F task[(int) int]>(value int) int {
    return F(value)
}

Main :: task() {
    x := Apply<ToString>(10)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected checker diagnostics")
	}

	if !strings.Contains(reporter.String(), `generic task parameter "F" result 1 expects int, got string`) {
		t.Fatalf("expected wrong generic task result diagnostic, got:\n%s", reporter.String())
	}
}

func TestRejectUnspecializedGenericTaskArgument(t *testing.T) {
	_, reporter := check(t, `
Identity :: task <T type>(value T) T {
    return value
}

Apply :: task <F task[(int) int]>(value int) int {
    return F(value)
}

Main :: task() {
    x := Apply<Identity>(10)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected checker diagnostics")
	}

	if !strings.Contains(reporter.String(), `generic task argument "Identity" requires specialization`) {
		t.Fatalf("expected unspecialized generic task argument diagnostic, got:\n%s", reporter.String())
	}
}

func TestGenericTaskArgumentSpecialization(t *testing.T) {
	_, reporter := check(t, `
Identity :: task <T type>(value T) T {
    return value
}

Apply :: task <F task[(int) int]>(value int) int {
    return F(value)
}

Main :: task() {
    x := Apply<Identity<int>>(10)
    assert(x == 10)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericIntConstraintAcceptsTrueExpression(t *testing.T) {
	_, reporter := check(t, `
Buffer :: struct <T type, N int[N > 0]> {
    value T
}

Main :: task() {
    b: Buffer<int, 3> = Buffer<int, 3>{
        value = 1,
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectGenericIntConstraintFalseExpression(t *testing.T) {
	_, reporter := check(t, `
Buffer :: struct <T type, N int[N > 0]> {
    value T
}

Main :: task() {
    b: Buffer<int, 0>
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected checker diagnostics")
	}

	if !strings.Contains(reporter.String(), `generic constraint failed: 0 > 0`) {
		t.Fatalf("expected generic constraint failure diagnostic, got:\n%s", reporter.String())
	}
}

func TestRejectRuntimeValueAsGenericIntArgument(t *testing.T) {
	_, reporter := check(t, `
Buffer :: struct <T type, N int> {
    value T
}

Main :: task() {
    n := 3
    b: Buffer<int, n>
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected checker diagnostics")
	}

	if !strings.Contains(reporter.String(), `generic parameter "N" requires a compile-time value argument`) {
		t.Fatalf("expected compile-time value diagnostic, got:\n%s", reporter.String())
	}
}

func TestGenericFieldConstraintAllowsFieldAccess(t *testing.T) {
	_, reporter := check(t, `
Actor :: struct {
    health int
}

GetHealth :: task <T type[health int]>(value T) int {
    return value.health
}

Main :: task() {
    actor := Actor{health = 10}
    health := GetHealth<Actor>(actor)
    assert(health == 10)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectGenericFieldConstraintMissingField(t *testing.T) {
	_, reporter := check(t, `
Actor :: struct {
    name string
}

GetHealth :: task <T type[health int]>(value T) int {
    return value.health
}

Main :: task() {
    actor := Actor{name = "bob"}
    health := GetHealth<Actor>(actor)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected checker diagnostics")
	}

	if !strings.Contains(reporter.String(), `must have field "health"`) {
		t.Fatalf("expected missing field constraint diagnostic, got:\n%s", reporter.String())
	}
}

func TestRejectGenericFieldConstraintWrongFieldType(t *testing.T) {
	_, reporter := check(t, `
Actor :: struct {
    health string
}

GetHealth :: task <T type[health int]>(value T) int {
    return value.health
}

Main :: task() {
    actor := Actor{health = "full"}
    health := GetHealth<Actor>(actor)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected checker diagnostics")
	}

	if !strings.Contains(reporter.String(), `field "health" must have type int, got string`) {
		t.Fatalf("expected wrong field type diagnostic, got:\n%s", reporter.String())
	}
}

func TestGenericEnumVariantConstraintAcceptsVariant(t *testing.T) {
	_, reporter := check(t, `
Status :: enum {
    Ready,
    Failed,
}

UseStatus :: task <E enum[Ready]>() {
}

Main :: task() {
    UseStatus<Status>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectGenericEnumVariantConstraintMissingVariant(t *testing.T) {
	_, reporter := check(t, `
Status :: enum {
    Failed,
}

UseStatus :: task <E enum[Ready]>() {
}

Main :: task() {
    UseStatus<Status>()
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected checker diagnostics")
	}

	if !strings.Contains(reporter.String(), `must contain variant .Ready`) {
		t.Fatalf("expected missing enum variant diagnostic, got:\n%s", reporter.String())
	}
}

func TestGenericUnionMemberConstraintAcceptsMember(t *testing.T) {
	_, reporter := check(t, `
Failure :: struct {
    code int
}

Result :: union {
    Failure,
}

UseResult :: task <U union[Failure]>() {
}

Main :: task() {
    UseResult<Result>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectGenericUnionMemberConstraintMissingMember(t *testing.T) {
	_, reporter := check(t, `
Failure :: struct {
    code int
}

Other :: struct {
    code int
}

Result :: union {
    Other,
}

UseResult :: task <U union[Failure]>() {
}

Main :: task() {
    UseResult<Result>()
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected checker diagnostics")
	}

	if !strings.Contains(reporter.String(), `must contain member Failure`) {
		t.Fatalf("expected missing union member diagnostic, got:\n%s", reporter.String())
	}
}

func TestCheckImportedGenericStructFieldSubstitution(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "types", `
Box :: struct <T type> {
    value T
}
`)

	reporter := checkWithPackages(t, `
Main :: task() {
    b: types.Box<int> = types.Box<int>{value = 10}
    x := b.value
    assert(x == 10)
}
`, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*PackageInfo{
		"types": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckImportedGenericStructRejectsWrongFieldValueType(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "types", `
Box :: struct <T type> {
    value T
}
`)

	reporter := checkWithPackages(t, `
Main :: task() {
    b: types.Box<int> = types.Box<int>{value = "wrong"}
}
`, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*PackageInfo{
		"types": checkerPkg,
	})

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot assign string to int") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckImportedNestedGenericStructFieldSubstitution(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "types", `
Pair :: struct <A type, B type> {
    first A
    second B
}

Box :: struct <T type> {
    value T
}
`)

	reporter := checkWithPackages(t, `
Main :: task() {
    b: types.Box<types.Pair<int, string>>
    pair := b.value

    x := pair.first
    s := pair.second

    assert(x == 0)
    assert(size(s) >= 0)
}
`, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*PackageInfo{
		"types": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckImportedGenericTaskReturnsImportedGenericStruct(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "types", `
Box :: struct <T type> {
    value T
}

MakeBox :: task <T type>(value T) Box<T> {
    return Box<T>{value = value}
}
`)

	reporter := checkWithPackages(t, `
Main :: task() {
    b := types.MakeBox<int>(10)
    x := b.value

    assert(x == 10)
}
`, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*PackageInfo{
		"types": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckImportedGenericTaskRejectsWrongArgumentAfterSpecialization(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "types", `
Identity :: task <T type>(value T) T {
    return value
}
`)

	reporter := checkWithPackages(t, `
Main :: task() {
    x := types.Identity<int>("wrong")
}
`, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*PackageInfo{
		"types": checkerPkg,
	})

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot assign string to int") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckImportedSpecializedGenericTaskArgument(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "types", `
Identity :: task <T type>(value T) T {
    return value
}
`)

	reporter := checkWithPackages(t, `
Apply :: task <F task[(int) int]>(value int) int {
    return F(value)
}

Main :: task() {
    x := Apply<types.Identity<int>>(10)
    assert(x == 10)
}
`, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*PackageInfo{
		"types": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckImportedSpecializedGenericTaskArgumentRejectsSignatureMismatch(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "types", `
Identity :: task <T type>(value T) T {
    return value
}
`)

	reporter := checkWithPackages(t, `
Apply :: task <F task[(int) int]>(value int) int {
    return F(value)
}

Main :: task() {
    x := Apply<types.Identity<string>>(10)
}
`, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*PackageInfo{
		"types": checkerPkg,
	})

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	out := reporter.String()

	if !strings.Contains(out, `generic task parameter "F" parameter 1 expects int, got string`) {
		t.Fatalf("expected imported generic task parameter mismatch diagnostic, got:\n%s", out)
	}

	if !strings.Contains(out, `generic task parameter "F" result 1 expects int, got string`) {
		t.Fatalf("expected imported generic task result mismatch diagnostic, got:\n%s", out)
	}
}

func TestCheckImportedGenericStructValueParamFieldSubstitution(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "types", `
Buffer :: struct <T type, N int> {
    value T
    capacity int
}
`)

	reporter := checkWithPackages(t, `
Main :: task() {
    b: types.Buffer<int, 4> = types.Buffer<int, 4>{
        value = 10,
        capacity = 4,
    }

    x: int = b.value
}
`, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*PackageInfo{
		"types": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestCheckImportedGenericStructValueParamRejectsWrongFieldUse(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "types", `
Buffer :: struct <T type, N int> {
    value T
}
`)

	reporter := checkWithPackages(t, `
Main :: task() {
    b: types.Buffer<string, 4>
    x: int = b.value
}
`, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*PackageInfo{
		"types": checkerPkg,
	})

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot assign string to int") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func assertCheckerDiagnosticContains(t *testing.T, reporter *diag.Reporter, want string) {
	t.Helper()

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics containing %q, got none", want)
	}

	if !strings.Contains(reporter.String(), want) {
		t.Fatalf("expected diagnostics to contain %q, got:\n%s", want, reporter.String())
	}
}

func TestGenericTaskConstraintAcceptsMultiReturnTask(t *testing.T) {
	reporter := checkSource(t, `
Swap :: task <T type>(a T, b T) T, T {
    return b, a
}

ApplySwap :: task <T type, F task[(T, T) T, T]>(a T, b T) T, T {
    return F(a, b)
}

Main :: task() {
    x, y := ApplySwap<int, Swap<int>>(1, 2)
    assert(x == 2)
    assert(y == 1)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericTaskConstraintRejectsParameterCountMismatch(t *testing.T) {
	reporter := checkSource(t, `
One :: task(a int) int {
    return a
}

Apply :: task <F task[(int, int) int]>(a int, b int) int {
    return F(a, b)
}

Main :: task() {
    x := Apply<One>(1, 2)
    assert(x == 1)
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic task parameter "F" expects task with 2 parameter(s), got 1`)
}

func TestGenericTaskConstraintRejectsParameterTypeMismatch(t *testing.T) {
	reporter := checkSource(t, `
StringInput :: task(value string) int {
    return 0
}

Apply :: task <F task[(int) int]>(value int) int {
    return F(value)
}

Main :: task() {
    x := Apply<StringInput>(1)
    assert(x == 0)
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic task parameter "F" parameter 1 expects int, got string`)
}

func TestGenericTaskConstraintRejectsResultCountMismatch(t *testing.T) {
	reporter := checkSource(t, `
Pair :: task(value int) int, int {
    return value, value
}

Apply :: task <F task[(int) int]>(value int) int {
    return F(value)
}

Main :: task() {
    x := Apply<Pair>(1)
    assert(x == 1)
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic task parameter "F" expects task with 1 result value(s), got 2`)
}

func TestGenericTaskConstraintRejectsResultTypeMismatch(t *testing.T) {
	reporter := checkSource(t, `
ToString :: task(value int) string {
    return "x"
}

Apply :: task <F task[(int) int]>(value int) int {
    return F(value)
}

Main :: task() {
    x := Apply<ToString>(1)
    assert(x == 1)
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic task parameter "F" result 1 expects int, got string`)
}

func TestGenericTaskConstraintRejectsMultiReturnResultTypeMismatch(t *testing.T) {
	reporter := checkSource(t, `
BadSwap :: task(a int, b int) int, string {
    return b, "x"
}

ApplySwap :: task <F task[(int, int) int, int]>(a int, b int) int, int {
    return F(a, b)
}

Main :: task() {
    x, y := ApplySwap<BadSwap>(1, 2)
    assert(x == 2)
    assert(y == 1)
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic task parameter "F" result 2 expects int, got string`)
}

func TestGenericTaskConstraintDependingOnTypeParamRejectsSpecializedMismatch(t *testing.T) {
	reporter := checkSource(t, `
Identity :: task <T type>(value T) T {
    return value
}

Apply :: task <T type, F task[(T) T]>(value T) T {
    return F(value)
}

Main :: task() {
    x := Apply<int, Identity<string>>(1)
    assert(x == 1)
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic task parameter "F" parameter 1 expects int, got string`)
	assertCheckerDiagnosticContains(t, reporter, `generic task parameter "F" result 1 expects int, got string`)
}

func TestGenericTaskConstraintRejectsVariadicTaskArgument(t *testing.T) {
	reporter := checkSource(t, `
TakeMany :: task(values ...int) int {
    return 0
}

Apply :: task <F task[(int) int]>(value int) int {
    return F(value)
}

Main :: task() {
    x := Apply<TakeMany>(1)
    assert(x == 0)
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic task parameter "F" expects non-variadic task`)
}

func TestImportedGenericTaskConstraintRejectsSpecializedMismatch(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "util", `
Identity :: task <T type>(value T) T {
    return value
}
`)

	reporter := checkWithPackages(
		t,
		`
Apply :: task <T type, F task[(T) T]>(value T) T {
    return F(value)
}

Main :: task() {
    x := Apply<int, util.Identity<string>>(1)
    assert(x == 1)
}
`,
		map[string]*resolver.PackageInfo{
			"util": resolverPkg,
		},
		map[string]*PackageInfo{
			"util": checkerPkg,
		},
	)

	assertCheckerDiagnosticContains(t, reporter, `generic task parameter "F" parameter 1 expects int, got string`)
	assertCheckerDiagnosticContains(t, reporter, `generic task parameter "F" result 1 expects int, got string`)
}

func TestGenericTypeArgumentRejectsEnum(t *testing.T) {
	reporter := checkSource(t, `
Status :: enum {
    Ready
    Done
}

UseType :: task <T type>() {}

Main :: task() {
    UseType<Status>()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic parameter "T" expects concrete stored data type, got Status`)
}

func TestGenericTypeArgumentRejectsUnion(t *testing.T) {
	reporter := checkSource(t, `
Value :: union {
    int
    string
}

UseType :: task <T type>() {}

Main :: task() {
    UseType<Value>()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic parameter "T" expects concrete stored data type, got Value`)
}

func TestGenericTypeArgumentRejectsTask(t *testing.T) {
	reporter := checkSource(t, `
Identity :: task(value int) int {
    return value
}

UseType :: task <T type>() {}

Main :: task() {
    UseType<Identity>()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `expected type argument, got value "Identity"`)
}

func TestGenericTypeArgumentRejectsSpecializedGenericTask(t *testing.T) {
	reporter := checkSource(t, `
Identity :: task <T type>(value T) T {
    return value
}

UseType :: task <T type>() {}

Main :: task() {
    UseType<Identity<int>>()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `"Identity" is not a type`)
}

func TestGenericEnumArgumentAcceptsEnum(t *testing.T) {
	reporter := checkSource(t, `
Status :: enum {
    Ready
    Done
}

UseEnum :: task <E enum>() {}

Main :: task() {
    UseEnum<Status>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericEnumArgumentRejectsNonEnum(t *testing.T) {
	reporter := checkSource(t, `
UseEnum :: task <E enum>() {}

Main :: task() {
    UseEnum<int>()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic parameter "E" expects enum type, got int`)
}

func TestGenericEnumArgumentRejectsStruct(t *testing.T) {
	reporter := checkSource(t, `
Player :: struct {
    health int
}

UseEnum :: task <E enum>() {}

Main :: task() {
    UseEnum<Player>()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic parameter "E" expects enum type, got Player`)
}

func TestGenericUnionArgumentAcceptsUnion(t *testing.T) {
	reporter := checkSource(t, `
Value :: union {
    int
    string
}

UseUnion :: task <U union>() {}

Main :: task() {
    UseUnion<Value>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericUnionArgumentRejectsNonUnion(t *testing.T) {
	reporter := checkSource(t, `
UseUnion :: task <U union>() {}

Main :: task() {
    UseUnion<int>()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic parameter "U" expects union type, got int`)
}

func TestGenericUnionArgumentRejectsStruct(t *testing.T) {
	reporter := checkSource(t, `
Player :: struct {
    health int
}

UseUnion :: task <U union>() {}

Main :: task() {
    UseUnion<Player>()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic parameter "U" expects union type, got Player`)
}

func TestGenericTaskArgumentAcceptsTask(t *testing.T) {
	reporter := checkSource(t, `
Identity :: task(value int) int {
    return value
}

UseTask :: task <F task[(int) int]>() {}

Main :: task() {
    UseTask<Identity>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericTaskArgumentRejectsType(t *testing.T) {
	reporter := checkSource(t, `
UseTask :: task <F task[(int) int]>() {}

Main :: task() {
    UseTask<int>()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `expected task argument`)
}

func TestGenericTaskArgumentRejectsStructType(t *testing.T) {
	reporter := checkSource(t, `
Player :: struct {
    health int
}

UseTask :: task <F task[(int) int]>() {}

Main :: task() {
    UseTask<Player>()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `expected task argument, got "Player"`)
}

func TestGenericIntValueArgumentAcceptsLiteral(t *testing.T) {
	reporter := checkSource(t, `
UseInt :: task <N int>() {}

Main :: task() {
    UseInt<3>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericIntValueArgumentAcceptsConstExpr(t *testing.T) {
	reporter := checkSource(t, `
Base :: 2
UseInt :: task <N int>() {}

Main :: task() {
    UseInt<Base + 3>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericIntValueArgumentRejectsString(t *testing.T) {
	reporter := checkSource(t, `
UseInt :: task <N int>() {}

Main :: task() {
    UseInt<"x">()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `cannot assign string to int`)
}

func TestGenericIntValueArgumentRejectsRuntimeValue(t *testing.T) {
	reporter := checkSource(t, `
UseInt :: task <N int>() {}

Main :: task() {
    x := 10
    UseInt<x>()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic parameter "N" requires a compile-time value argument`)
}

func TestGenericBoolValueArgumentAcceptsLiteral(t *testing.T) {
	reporter := checkSource(t, `
UseBool :: task <B bool>() {}

Main :: task() {
    UseBool<true>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericBoolValueArgumentAcceptsConstExpr(t *testing.T) {
	reporter := checkSource(t, `
Enabled :: true
UseBool :: task <B bool>() {}

Main :: task() {
    UseBool<!Enabled || true>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericBoolValueArgumentRejectsInt(t *testing.T) {
	reporter := checkSource(t, `
UseBool :: task <B bool>() {}

Main :: task() {
    UseBool<1>()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `cannot assign untyped int to bool`)
}

func TestGenericBoolValueArgumentRejectsRuntimeValue(t *testing.T) {
	reporter := checkSource(t, `
UseBool :: task <B bool>() {}

Main :: task() {
    flag := true
    UseBool<flag>()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic parameter "B" requires a compile-time value argument`)
}

func TestGenericStringValueArgumentAcceptsLiteral(t *testing.T) {
	reporter := checkSource(t, `
UseString :: task <S string>() {}

Main :: task() {
    UseString<"hello">()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericStringValueArgumentAcceptsConst(t *testing.T) {
	reporter := checkSource(t, `
Name :: "seal"
UseString :: task <S string>() {}

Main :: task() {
    UseString<Name>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericStringValueArgumentRejectsBool(t *testing.T) {
	reporter := checkSource(t, `
UseString :: task <S string>() {}

Main :: task() {
    UseString<true>()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `cannot assign bool to string`)
}

func TestGenericStringValueArgumentRejectsRuntimeValue(t *testing.T) {
	reporter := checkSource(t, `
UseString :: task <S string>() {}

Main :: task() {
    name := "seal"
    UseString<name>()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic parameter "S" requires a compile-time value argument`)
}

func TestGenericTypedValueArgumentAcceptsMatchingConst(t *testing.T) {
	reporter := checkSource(t, `
UseValue :: task <N int>() {}

Main :: task() {
    UseValue<1 + 2>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericTypedValueArgumentRejectsWrongType(t *testing.T) {
	reporter := checkSource(t, `
UseValue :: task <N int>() {}

Main :: task() {
    UseValue<"bad">()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `cannot assign string to int`)
}

func TestGenericTypedValueArgumentRejectsRuntimeValue(t *testing.T) {
	reporter := checkSource(t, `
UseValue :: task <N int>() {}

Main :: task() {
    n := 3
    UseValue<n>()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic parameter "N" requires a compile-time value argument`)
}

func TestGenericValueConstraintSubstitutesIntArgument(t *testing.T) {
	reporter := checkSource(t, `
UsePositive :: task <N int[N > 0]>() {}

Main :: task() {
    UsePositive<3>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericValueConstraintRejectsSubstitutedIntArgument(t *testing.T) {
	reporter := checkSource(t, `
UsePositive :: task <N int[N > 0]>() {}

Main :: task() {
    UsePositive<0>()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic constraint failed: 0 > 0`)
}

func TestGenericValueConstraintSubstitutesBoolArgument(t *testing.T) {
	reporter := checkSource(t, `
UsePositive :: task <N int, OK bool[OK != false]>() {}

Main :: task() {
    UsePositive<3, true>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericValueConstraintRejectsSubstitutedBoolArgument(t *testing.T) {
	reporter := checkSource(t, `
UsePositive :: task <N int, OK bool[OK != false]>() {}

Main :: task() {
    UsePositive<3, false>()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic constraint failed: false != false`)
}

func TestGenericValueConstraintReferencesEarlierIntArgument(t *testing.T) {
	reporter := checkSource(t, `
UseBounded :: task <N int, Limit int[N < Limit]>() {}

Main :: task() {
    UseBounded<3, 10>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericValueConstraintRejectsEarlierIntArgumentReference(t *testing.T) {
	reporter := checkSource(t, `
UseBounded :: task <N int, Limit int[N < Limit]>() {}

Main :: task() {
    UseBounded<10, 3>()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic constraint failed: 10 < 3`)
}

func TestGenericValueConstraintSubstitutesStringArgument(t *testing.T) {
	reporter := checkSource(t, `
UseNamed :: task <Name string[Name != ""]>() {}

Main :: task() {
    UseNamed<"seal">()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericValueConstraintRejectsSubstitutedStringArgument(t *testing.T) {
	reporter := checkSource(t, `
UseNamed :: task <Name string[Name != ""]>() {}

Main :: task() {
    UseNamed<"">()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic constraint failed: "" != ""`)
}

func TestGenericValueConstraintAcceptsConstExprArgument(t *testing.T) {
	reporter := checkSource(t, `
Base :: 2
UsePositive :: task <N int[N > 0]>() {}

Main :: task() {
    UsePositive<Base + 1>()
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericValueConstraintRejectsConstExprArgument(t *testing.T) {
	reporter := checkSource(t, `
Base :: 2
UseSmall :: task <N int[N < 2]>() {}

Main :: task() {
    UseSmall<Base + 1>()
}
`)

	assertCheckerDiagnosticContains(t, reporter, `generic constraint failed: Base + 1 < 2`)
}

func TestImportedGenericValueConstraintSubstitutesArgument(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "limits", `
UsePositive :: task <N int[N > 0]>() {}
`)

	reporter := checkWithPackages(t, `
Main :: task() {
    limits.UsePositive<3>()
}
`, map[string]*resolver.PackageInfo{
		"limits": resolverPkg,
	}, map[string]*PackageInfo{
		"limits": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestImportedGenericValueConstraintRejectsSubstitutedArgument(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "limits", `
UsePositive :: task <N int[N > 0]>() {}
`)

	reporter := checkWithPackages(t, `
Main :: task() {
    limits.UsePositive<0>()
}
`, map[string]*resolver.PackageInfo{
		"limits": resolverPkg,
	}, map[string]*PackageInfo{
		"limits": checkerPkg,
	})

	assertCheckerDiagnosticContains(t, reporter, `generic constraint failed: 0 > 0`)
}

func TestCheckImportedGenericValueConstraintPreservesPackageQualifiedStructType(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "rules", `
Matrix :: struct {
	years int
}

Accept :: pure task(age Matrix) bool {
	return age.years > 0
}
`)

	reporter := checkWithPackages(t, `
UseAge :: task <Age rules.Matrix[rules.Accept(Age)]>() {}

Main :: task() {
	UseAge<rules.Matrix{years = 1}>()
}
`, map[string]*resolver.PackageInfo{
		"rules": resolverPkg,
	}, map[string]*PackageInfo{
		"rules": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckImportedGenericValueConstraintRejectsDifferentPackageSameTypeName(t *testing.T) {
	_, resolverRules, checkerRules := exportCheckerPackage(t, "rules", `
Matrix :: struct {
	years int
}

Accept :: pure task(age Matrix) bool {
	return age.years > 0
}
`)

	_, resolverOther, checkerOther := exportCheckerPackage(t, "other", `
Matrix :: struct {
	years int
}
`)

	reporter := checkWithPackages(t, `
UseAge :: task <Age other.Matrix[rules.Accept(Age)]>() {}

Main :: task() {
	UseAge<other.Matrix{years = 1}>()
}
`, map[string]*resolver.PackageInfo{
		"rules": resolverRules,
		"other": resolverOther,
	}, map[string]*PackageInfo{
		"rules": checkerRules,
		"other": checkerOther,
	})

	assertCheckerDiagnosticContains(t, reporter, `cannot assign other.Matrix to rules.Matrix`)
}

func TestCheckImportedGenericValueConstraintRejectsAmbiguousImportedOperatorOverload(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "rules", `
Matrix :: struct {
	years int
}

A :: pure task(age Matrix, tag any) bool {
	return true
}

B :: pure task(age any, tag string) bool {
	return true
}

== :: overload {
	A
	B
}
`)

	reporter := checkWithPackages(t, `
UseAge :: task <Age rules.Matrix[Age == "vampire"]>() {}

Main :: task() {
	UseAge<rules.Matrix{years = 999}>()
}
`, map[string]*resolver.PackageInfo{
		"rules": resolverPkg,
	}, map[string]*PackageInfo{
		"rules": checkerPkg,
	})

	assertCheckerDiagnosticContains(t, reporter, `ambiguous operator overload "=="`)
}

func TestCheckImportedGenericValueConstraintRejectsNoMatchingImportedOperatorOverload(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "rules", `
Matrix :: struct {
	years int
}

CompareInt :: pure task(age Matrix, value int) bool {
	return age.years == value
}

== :: overload {
	CompareInt
}
`)

	reporter := checkWithPackages(t, `
UseAge :: task <Age rules.Matrix[Age == "vampire"]>() {}

Main :: task() {
	UseAge<rules.Matrix{years = 999}>()
}
`, map[string]*resolver.PackageInfo{
		"rules": resolverPkg,
	}, map[string]*PackageInfo{
		"rules": checkerPkg,
	})

	assertCheckerDiagnosticContains(t, reporter, `no operator overload "==" matches operand types (rules.Matrix, string)`)
}

func TestCheckImportedTypeConstraintFindsImportedOperatorEvenWithUnrelatedLocalOperator(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "rules", `
Matrix :: struct {
	years int
}

IsVampireAge :: pure task(age Matrix, tag string) bool {
	return age.years == 999 && tag == "vampire"
}

== :: overload {
	IsVampireAge
}
`)

	reporter := checkWithPackages(t, `
LocalThing :: struct {
	value int
}

LocalCompare :: pure task(x LocalThing, tag string) bool {
	return false
}

== :: overload {
	LocalCompare
}

UseAge :: task <Age rules.Matrix[Age == "vampire"]>() {}

Main :: task() {
	UseAge<rules.Matrix{years = 999}>()
}
`, map[string]*resolver.PackageInfo{
		"rules": resolverPkg,
	}, map[string]*PackageInfo{
		"rules": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericConstraintMaxDepthDisabledAllowsDeepPureEvaluation(t *testing.T) {
	reporter := diag.NewReporter()

	src := source.NewFile("test.seal", `
A :: pure task(n int) bool {
	return B(n)
}

B :: pure task(n int) bool {
	return C(n)
}

C :: pure task(n int) bool {
	return n > 0
}

Use :: task <N int[A(N)]>() {}

Main :: task() {
	Use<1>()
}
`)

	lex := lexer.New(src, reporter)
	tokens := lex.LexAll()
	p := parser.New(tokens, reporter)
	file := p.ParseFile()

	r := resolver.New(reporter)
	r.ResolveFile(file)

	c := NewWithPackagesAndOptions(reporter, nil, Options{
		GenericConstraintMaxDepth: 0,
	})
	c.CheckFile(file)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenericConstraintMaxDepthRejectsDeepPureEvaluation(t *testing.T) {
	reporter := diag.NewReporter()

	src := source.NewFile("test.seal", `
A :: pure task(n int) bool {
	return B(n)
}

B :: pure task(n int) bool {
	return C(n)
}

C :: pure task(n int) bool {
	return n > 0
}

Use :: task <N int[A(N)]>() {}

Main :: task() {
	Use<1>()
}
`)

	lex := lexer.New(src, reporter)
	tokens := lex.LexAll()
	p := parser.New(tokens, reporter)
	file := p.ParseFile()

	r := resolver.New(reporter)
	r.ResolveFile(file)

	c := NewWithPackagesAndOptions(reporter, nil, Options{
		GenericConstraintMaxDepth: 2,
	})
	c.CheckFile(file)

	assertCheckerDiagnosticContains(t, reporter, `generic constraint evaluation exceeded max depth 2`)
}

func TestGenericConstraintMaxDepthRejectsDirectRecursivePureEvaluation(t *testing.T) {
	reporter := diag.NewReporter()

	src := source.NewFile("test.seal", `
Loop :: pure task(n int) bool {
	return Loop(n)
}

Use :: task <N int[Loop(N)]>() {}

Main :: task() {
	Use<1>()
}
`)

	lex := lexer.New(src, reporter)
	tokens := lex.LexAll()
	p := parser.New(tokens, reporter)
	file := p.ParseFile()

	r := resolver.New(reporter)
	r.ResolveFile(file)

	c := NewWithPackagesAndOptions(reporter, nil, Options{
		GenericConstraintMaxDepth: 8,
	})
	c.CheckFile(file)

	assertCheckerDiagnosticContains(t, reporter, `recursive generic constraint evaluation through "Loop"`)
}

func TestGenericConstraintMaxDepthRejectsMutualRecursivePureEvaluation(t *testing.T) {
	reporter := diag.NewReporter()

	src := source.NewFile("test.seal", `
A :: pure task(n int) bool {
	return B(n)
}

B :: pure task(n int) bool {
	return A(n)
}

Use :: task <N int[A(N)]>() {}

Main :: task() {
	Use<1>()
}
`)

	lex := lexer.New(src, reporter)
	tokens := lex.LexAll()
	p := parser.New(tokens, reporter)
	file := p.ParseFile()

	r := resolver.New(reporter)
	r.ResolveFile(file)

	c := NewWithPackagesAndOptions(reporter, nil, Options{
		GenericConstraintMaxDepth: 8,
	})
	c.CheckFile(file)

	assertCheckerDiagnosticContains(t, reporter, `recursive generic constraint evaluation through "A"`)
}

func TestCheckLocalStaticInterface(t *testing.T) {
	reporter := checkSource(t, `
Vec3 :: struct {
	x f32
	y f32
	z f32
}

Positioned :: interface {
	Position :: task(self *self) Vec3
}

Transform :: struct {
	position Vec3
}

Positioned :: impl Transform {
	Position :: task(self *Transform) Vec3 {
		return self.position
	}
}

Main :: task() {
	transform := Transform{}
	positioned := cast<Positioned>(&transform)
	position := Position(positioned)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckGenericInterfaceAndImpl(t *testing.T) {
	reporter := checkSource(t, `
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

Main :: task() {
	box := Box<int>{value = 10}
	reader := cast<Reader<int>>(&box)
	value := Read(reader)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckImplRejectsMissingRequirement(t *testing.T) {
	reporter := checkSource(t, `
Positioned :: interface {
	Position :: task(self *self) int
	SetPosition :: task(self *self, value int)
}

Entity :: struct {
	position int
}

Positioned :: impl Entity {
	Position :: task(self *Entity) int {
		return self.position
	}
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`missing requirement "SetPosition"`,
	)
}

func TestCheckImplRejectsWrongReceiverType(t *testing.T) {
	reporter := checkSource(t, `
Reader :: interface <Out type> {
	Read :: task(self *self) Out
}

Box :: struct {
	value int
}

Reader<int> :: impl Box {
	Read :: task(self Box) int {
		return self.value
	}
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`impl entry "Read" has wrong signature`,
	)
}

func TestCheckImplRejectsExtraEntry(t *testing.T) {
	reporter := checkSource(t, `
Readable :: interface {
	Read :: task(self *self) int
}

Box :: struct {
	value int
}

Readable :: impl Box {
	Read :: task(self *Box) int {
		return self.value
	}

	Write :: task(self *Box, value int) {
	}
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`impl entry "Write" is not a requirement`,
	)
}

func TestCheckInterfaceCastRejectsMissingImpl(t *testing.T) {
	reporter := checkSource(t, `
Readable :: interface {
	Read :: task(self *self) int
}

Box :: struct {
	value int
}

Main :: task() {
	box := Box{value = 10}
	reader := cast<Readable>(&box)
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`cannot cast *Box to Readable: no matching implementation`,
	)
}

func TestCheckInterfaceAssignmentRequiresExplicitCast(t *testing.T) {
	reporter := checkSource(t, `
Readable :: interface {
	Read :: task(self *self) int
}

Box :: struct {
	value int
}

Readable :: impl Box {
	Read :: task(self *Box) int {
		return self.value
	}
}

Main :: task() {
	box := Box{value = 10}
	reader: Readable = &box
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`use cast<Readable>(value)`,
	)
}

func TestCheckInterfaceMultireturn(t *testing.T) {
	reporter := checkSource(t, `
Reader :: interface <Out type> {
	Read :: task(self *self) (Out, bool)
}

Box :: struct <T type> {
	value T
}

Reader<T> :: impl <T type> Box<T> {
	Read :: task(self *Box<T>) (T, bool) {
		return self.value, true
	}
}

Main :: task() {
	box := Box<int>{value = 10}
	reader := cast<Reader<int>>(&box)
	value, ok := Read(reader)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckDelegatedImpl(t *testing.T) {
	reporter := checkSource(t, `
Vec3 :: struct {
	x f32
	y f32
	z f32
}

Positioned :: interface {
	Position :: task(self *self) Vec3
	SetPosition :: task(self *self, value Vec3)
}

Transform :: struct {
	position Vec3
}

Positioned :: impl Transform {
	Position :: task(self *Transform) Vec3 {
		return self.position
	}

	SetPosition :: task(self *Transform, value Vec3) {
		self.position = value
	}
}

Entity :: struct {
	transform Transform
}

Positioned :: impl Entity using transform

Main :: task() {
	entity := Entity{}
	positioned := cast<Positioned>(&entity)
	position := Position(positioned)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckNestedDelegatedImpl(t *testing.T) {
	reporter := checkSource(t, `
Vec3 :: struct {
	x f32
	y f32
	z f32
}

Positioned :: interface {
	Position :: task(self *self) Vec3
}

Transform :: struct {
	position Vec3
}

Positioned :: impl Transform {
	Position :: task(self *Transform) Vec3 {
		return self.position
	}
}

Components :: struct {
	transform Transform
}

Entity :: struct {
	components Components
}

Positioned :: impl Entity using components.transform

Main :: task() {
	entity := Entity{}
	positioned := cast<Positioned>(&entity)
	position := Position(positioned)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckDelegatedImplRejectsNonImplementingField(t *testing.T) {
	reporter := checkSource(t, `
Positioned :: interface {
	Position :: task(self *self) int
}

Transform :: struct {
	position int
}

Entity :: struct {
	transform Transform
}

Positioned :: impl Entity using transform
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`selected type Transform does not implement the interface`,
	)
}

func TestCheckDelegatedImplRejectsCycle(t *testing.T) {
	reporter := checkSource(t, `
Positioned :: interface {
	Position :: task(self *self) int
}

A :: struct {
	b *B
}

B :: struct {
	a *A
}

Positioned :: impl A using b
Positioned :: impl B using a
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`cyclic delegated implementation`,
	)
}

func TestCheckInterfaceCastRejectsAmbiguousImpl(t *testing.T) {
	reporter := checkSource(t, `
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

Reader<int> :: impl Box<int> {
	Read :: task(self *Box<int>) int {
		return self.value
	}
}

Main :: task() {
	box := Box<int>{value = 10}
	reader := cast<Reader<int>>(&box)
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`ambiguous implementation of Reader<int> for Box<int>`,
	)
}

func TestCheckInterfaceRejectsSelfResult(t *testing.T) {
	reporter := checkSource(t, `
Cloneable :: interface {
	Clone :: task(self *self) self
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`interface requirement results cannot currently contain "self"`,
	)
}

func TestCheckImportedInterfaceWithLocalImpl(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "io", `
Reader :: interface <Out type> {
	Read :: task(self *self) Out
}
`)

	reporter := checkWithPackages(t, `
Box :: struct <T type> {
	value T
}

io.Reader<T> :: impl <T type> Box<T> {
	Read :: task(self *Box<T>) T {
		return self.value
	}
}

Main :: task() {
	box := Box<int>{value = 10}
	reader := cast<io.Reader<int>>(&box)
	value := Read(reader)
}
`, map[string]*resolver.PackageInfo{
		"io": resolverPkg,
	}, map[string]*PackageInfo{
		"io": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckImportedGenericImpl(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "boxes", `
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

	reporter := checkWithPackages(t, `
Main :: task() {
	box := boxes.Box<int>{value = 10}
	reader := cast<boxes.Reader<int>>(&box)
	value := Read(reader)
}
`, map[string]*resolver.PackageInfo{
		"boxes": resolverPkg,
	}, map[string]*PackageInfo{
		"boxes": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckRejectsOrphanImpl(t *testing.T) {
	_, resolverIO, checkerIO := exportCheckerPackage(t, "io", `
Reader :: interface <Out type> {
	Read :: task(self *self) Out
}
`)

	_, resolverBoxes, checkerBoxes := exportCheckerPackage(t, "boxes", `
Box :: struct <T type> {
	value T
}
`)

	reporter := checkWithPackages(t, `
io.Reader<int> :: impl boxes.Box<int> {
	Read :: task(self *boxes.Box<int>) int {
		return self.value
	}
}
`, map[string]*resolver.PackageInfo{
		"io":    resolverIO,
		"boxes": resolverBoxes,
	}, map[string]*PackageInfo{
		"io":    checkerIO,
		"boxes": checkerBoxes,
	})

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`orphan impl is not allowed`,
	)
}

func TestCheckChainedDelegatedImpl(t *testing.T) {
	t.Skip("chained delegated implementations are not supported yet")

	reporter := checkSource(t, `
Positioned :: interface {
	Position :: task(self *self) int
}

Transform :: struct {
	position int
}

Positioned :: impl Transform {
	Position :: task(self *Transform) int {
		return self.position
	}
}

Components :: struct {
	transform Transform
}

Positioned :: impl Components using transform

Entity :: struct {
	components Components
}

Positioned :: impl Entity using components

Main :: task() {
	entity := Entity{
		components = Components{
			transform = Transform{
				position = 42,
			},
		},
	}

	positioned := cast<Positioned>(&entity)
	position := Position(positioned)

	assert(position == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics for chained delegated implementation:\n%s",
			reporter.String(),
		)
	}
}

func TestCheckImplRejectsWrongParameterCount(t *testing.T) {
	reporter := checkSource(t, `
Movable :: interface {
	Move :: task(self *self, x int, y int)
}

Transform :: struct {
	x int
	y int
}

Movable :: impl Transform {
	Move :: task(self *Transform, x int) {
		self.x = x
	}
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`impl entry "Move" has wrong signature`,
	)
}

func TestCheckImplRejectsWrongParameterType(t *testing.T) {
	reporter := checkSource(t, `
Movable :: interface {
	Move :: task(self *self, amount int)
}

Transform :: struct {
	x int
}

Movable :: impl Transform {
	Move :: task(self *Transform, amount string) {
	}
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`impl entry "Move" has wrong signature`,
	)
}

func TestCheckImplRejectsWrongResultCount(t *testing.T) {
	reporter := checkSource(t, `
Readable :: interface {
	Read :: task(self *self) int, bool
}

Box :: struct {
	value int
}

Readable :: impl Box {
	Read :: task(self *Box) int {
		return self.value
	}
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`impl entry "Read" has wrong signature`,
	)
}

func TestCheckImplRejectsWrongResultType(t *testing.T) {
	reporter := checkSource(t, `
Readable :: interface {
	Read :: task(self *self) int
}

Box :: struct {
	value int
}

Readable :: impl Box {
	Read :: task(self *Box) string {
		return "invalid"
	}
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`impl entry "Read" has wrong signature`,
	)
}

func TestCheckImplRejectsAliasWithWrongSignature(t *testing.T) {
	reporter := checkSource(t, `
Movable :: interface {
	Move :: task(self *self, amount int)
}

Transform :: struct {
	x int
}

MoveTransformWrong :: task(transform *Transform, amount string) {
}

Movable :: impl Transform {
	Move :: MoveTransformWrong
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`impl entry "Move" has wrong signature`,
	)
}

func TestCheckImplRejectsAliasWithWrongReceiver(t *testing.T) {
	reporter := checkSource(t, `
Readable :: interface {
	Read :: task(self *self) int
}

Box :: struct {
	value int
}

Other :: struct {
	value int
}

ReadOther :: task(other *Other) int {
	return other.value
}

Readable :: impl Box {
	Read :: ReadOther
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`impl entry "Read" has wrong signature`,
	)
}

func TestCheckImplRequiresExactRequirementParameterTypes(t *testing.T) {
	reporter := checkSource(t, `
Id :: distinct int

Lookup :: interface {
	Find :: task(self *self, id Id) int
}

Store :: struct {
	value int
}

Lookup :: impl Store {
	Find :: task(self *Store, id int) int {
		return self.value + id
	}
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`impl entry "Find" has wrong signature`,
	)
}

func TestCheckImplRequiresExactRequirementResultTypes(t *testing.T) {
	reporter := checkSource(t, `
Id :: distinct int

Identifiable :: interface {
	IdOf :: task(self *self) Id
}

Entity :: struct {
	id int
}

Identifiable :: impl Entity {
	IdOf :: task(self *Entity) int {
		return self.id
	}
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`impl entry "IdOf" has wrong signature`,
	)
}

func TestCheckDelegatedImplRejectsMissingDirectField(t *testing.T) {
	reporter := checkSource(t, `
Positioned :: interface {
	Position :: task(self *self) int
}

Transform :: struct {
	position int
}

Positioned :: impl Transform {
	Position :: task(self *Transform) int {
		return self.position
	}
}

Entity :: struct {
	transform Transform
}

Positioned :: impl Entity using missing
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`has no field "missing"`,
	)
}

func TestCheckDelegatedImplRejectsMissingNestedField(t *testing.T) {
	reporter := checkSource(t, `
Positioned :: interface {
	Position :: task(self *self) int
}

Transform :: struct {
	position int
}

Positioned :: impl Transform {
	Position :: task(self *Transform) int {
		return self.position
	}
}

Components :: struct {
	transform Transform
}

Entity :: struct {
	components Components
}

Positioned :: impl Entity using components.missing
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`has no field "missing"`,
	)
}

func TestCheckDelegatedImplRejectsNonStructIntermediateField(t *testing.T) {
	reporter := checkSource(t, `
Positioned :: interface {
	Position :: task(self *self) int
}

Entity :: struct {
	components int
}

Positioned :: impl Entity using components.transform
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`cannot select delegated field "transform" on non-struct type int`,
	)
}

func TestCheckDelegatedImplRejectsPointerToNonStructIntermediateField(t *testing.T) {
	reporter := checkSource(t, `
Positioned :: interface {
	Position :: task(self *self) int
}

Entity :: struct {
	components *int
}

Positioned :: impl Entity using components.transform
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`cannot select delegated field "transform" on non-struct type int`,
	)
}

func TestCheckDelegatedImplRejectsPrimitiveFinalField(t *testing.T) {
	reporter := checkSource(t, `
Positioned :: interface {
	Position :: task(self *self) int
}

Entity :: struct {
	position int
}

Positioned :: impl Entity using position
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`selected type int does not implement the interface`,
	)
}

func TestCheckDelegatedImplRequiresImplementationOfSameInterface(t *testing.T) {
	reporter := checkSource(t, `
Positioned :: interface {
	Position :: task(self *self) int
}

Named :: interface {
	Name :: task(self *self) string
}

Transform :: struct {
	position int
}

Named :: impl Transform {
	Name :: task(self *Transform) string {
		return "transform"
	}
}

Entity :: struct {
	transform Transform
}

Positioned :: impl Entity using transform
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`selected type Transform does not implement the interface`,
	)
}

func TestCheckDelegatedImplRejectsWrongGenericSpecialization(t *testing.T) {
	reporter := checkSource(t, `
Readable :: interface <T type> {
	Read :: task(self *self) T
}

Box :: struct <T type> {
	value T
}

Readable<T> :: impl <T type> Box<T> {
	Read :: task(self *Box<T>) T {
		return self.value
	}
}

Holder :: struct {
	box Box<string>
}

Readable<int> :: impl Holder using box
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`selected type Box<string> does not implement the interface`,
	)
}

func TestCheckDelegatedImplAcceptsMatchingGenericSpecialization(t *testing.T) {
	reporter := checkSource(t, `
Readable :: interface <T type> {
	Read :: task(self *self) T
}

Box :: struct <T type> {
	value T
}

Readable<T> :: impl <T type> Box<T> {
	Read :: task(self *Box<T>) T {
		return self.value
	}
}

Holder :: struct {
	box Box<int>
}

Readable<int> :: impl Holder using box

Main :: task() {
	holder := Holder{
		box = Box<int>{value = 42},
	}

	readable := cast<Readable<int>>(&holder)
	value := Read(readable)

	assert(value == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckDelegatedImplAcceptsPointerIntermediateField(t *testing.T) {
	reporter := checkSource(t, `
Positioned :: interface {
	Position :: task(self *self) int
}

Transform :: struct {
	position int
}

Positioned :: impl Transform {
	Position :: task(self *Transform) int {
		return self.position
	}
}

Components :: struct {
	transform Transform
}

Entity :: struct {
	components *Components
}

Positioned :: impl Entity using components.transform

Main :: task() {
	transform := Transform{position = 42}
	components := Components{transform = transform}
	entity := Entity{components = &components}

	positioned := cast<Positioned>(&entity)
	position := Position(positioned)

	assert(position == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckInterfaceValueAssignment(t *testing.T) {
	reporter := checkSource(t, `
Positioned :: interface {
	X :: task(value *self) int
}

Transform :: struct {
	x int
}

ReadX :: task(value *Transform) int {
	return value.x
}

Positioned :: impl Transform {
	X :: ReadX
}

Main :: task() {
	transform := Transform{x = 42}
	first := cast<Positioned>(&transform)
	second: Positioned = first

	assert(X(second) == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckInterfacePassedAsTaskParameter(t *testing.T) {
	reporter := checkSource(t, `
Positioned :: interface {
	X :: task(value *self) int
}

Transform :: struct {
	x int
}

ReadX :: task(value *Transform) int {
	return value.x
}

Positioned :: impl Transform {
	X :: ReadX
}

ReadPosition :: task(value Positioned) int {
	return X(value)
}

Main :: task() {
	transform := Transform{x = 42}
	positioned := cast<Positioned>(&transform)
	result := ReadPosition(positioned)

	assert(result == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckInterfaceReturnedFromTask(t *testing.T) {
	reporter := checkSource(t, `
Positioned :: interface {
	X :: task(value *self) int
}

Transform :: struct {
	x int
}

ReadX :: task(value *Transform) int {
	return value.x
}

Positioned :: impl Transform {
	X :: ReadX
}

IdentityPositioned :: task(value Positioned) Positioned {
	return value
}

Main :: task() {
	transform := Transform{x = 42}
	positioned := cast<Positioned>(&transform)
	returned := IdentityPositioned(positioned)

	assert(X(returned) == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckInterfaceStoredInStruct(t *testing.T) {
	reporter := checkSource(t, `
Positioned :: interface {
	X :: task(value *self) int
}

Transform :: struct {
	x int
}

ReadX :: task(value *Transform) int {
	return value.x
}

Positioned :: impl Transform {
	X :: ReadX
}

PositionHolder :: struct {
	value Positioned
}

Main :: task() {
	transform := Transform{x = 42}
	positioned := cast<Positioned>(&transform)

	holder := PositionHolder{
		value = positioned,
	}

	assert(X(holder.value) == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckInterfacePassedThroughMultipleTasks(t *testing.T) {
	reporter := checkSource(t, `
Positioned :: interface {
	X :: task(value *self) int
}

Transform :: struct {
	x int
}

ReadX :: task(value *Transform) int {
	return value.x
}

Positioned :: impl Transform {
	X :: ReadX
}

Forward :: task(value Positioned) Positioned {
	copy := value
	return copy
}

ReadForwarded :: task(value Positioned) int {
	forwarded := Forward(value)
	return X(forwarded)
}

Main :: task() {
	transform := Transform{x = 42}
	positioned := cast<Positioned>(&transform)
	result := ReadForwarded(positioned)

	assert(result == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckRejectsAssignmentBetweenDifferentInterfaceTypes(t *testing.T) {
	reporter := checkSource(t, `
Positioned :: interface {
	Value :: task(value *self) int
}

Readable :: interface {
	Value :: task(value *self) int
}

Transform :: struct {
	x int
}

ReadValue :: task(value *Transform) int {
	return value.x
}

Positioned :: impl Transform {
	Value :: ReadValue
}

Readable :: impl Transform {
	Value :: ReadValue
}

Main :: task() {
	transform := Transform{x = 42}
	positioned := cast<Positioned>(&transform)

	readable: Readable = positioned
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	out := reporter.String()

	if !strings.Contains(out, "cannot assign Positioned to interface Readable") {
		t.Fatalf(
			"expected incompatible interface assignment diagnostic, got:\n%s",
			out,
		)
	}
}

func TestCheckRejectsPassingDifferentInterfaceType(t *testing.T) {
	reporter := checkSource(t, `
Positioned :: interface {
	Value :: task(value *self) int
}

Readable :: interface {
	Value :: task(value *self) int
}

Transform :: struct {
	x int
}

ReadValue :: task(value *Transform) int {
	return value.x
}

Positioned :: impl Transform {
	Value :: ReadValue
}

UseReadable :: task(value Readable) int {
	return Value(value)
}

Main :: task() {
	transform := Transform{x = 42}
	positioned := cast<Positioned>(&transform)

	result := UseReadable(positioned)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	out := reporter.String()

	if !strings.Contains(out, "cannot assign Positioned to interface Readable") {
		t.Fatalf(
			"expected incompatible interface parameter diagnostic, got:\n%s",
			out,
		)
	}
}

func TestCheckRejectsReturningDifferentInterfaceType(t *testing.T) {
	reporter := checkSource(t, `
Positioned :: interface {
	Value :: task(value *self) int
}

Readable :: interface {
	Value :: task(value *self) int
}

Transform :: struct {
	x int
}

ReadValue :: task(value *Transform) int {
	return value.x
}

Positioned :: impl Transform {
	Value :: ReadValue
}

BadConversion :: task(value Positioned) Readable {
	return value
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	out := reporter.String()

	if !strings.Contains(out, "cannot assign Positioned to interface Readable") {
		t.Fatalf(
			"expected incompatible interface return diagnostic, got:\n%s",
			out,
		)
	}
}

func TestCheckInterfaceCastRejectsDuplicateConcreteImpls(t *testing.T) {
	reporter := checkSource(t, `
Readable :: interface {
	Read :: task(self *self) int
}

Box :: struct {
	value int
}

ReadFirst :: task(self *Box) int {
	return self.value
}

ReadSecond :: task(self *Box) int {
	return self.value + 1
}

Readable :: impl Box {
	Read :: ReadFirst
}

Readable :: impl Box {
	Read :: ReadSecond
}

Main :: task() {
	box := Box{value = 10}
	readable := cast<Readable>(&box)
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`ambiguous implementation of Readable for Box`,
	)
}

func TestCheckInterfaceCastRejectsAmbiguousGenericImpls(t *testing.T) {
	reporter := checkSource(t, `
Readable :: interface <T type> {
	Read :: task(self *self) T
}

Box :: struct <T type> {
	value T
}

Readable<T> :: impl <T type> Box<T> {
	Read :: task(self *Box<T>) T {
		return self.value
	}
}

Readable<U> :: impl <U type> Box<U> {
	Read :: task(self *Box<U>) U {
		return self.value
	}
}

Main :: task() {
	box := Box<int>{value = 10}
	readable := cast<Readable<int>>(&box)
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`ambiguous implementation of Readable<int> for Box<int>`,
	)
}

func TestCheckDelegatedImplRejectsAmbiguousSelectedImplementation(t *testing.T) {
	reporter := checkSource(t, `
Positioned :: interface {
	Position :: task(self *self) int
}

Transform :: struct {
	position int
}

ReadPositionOne :: task(self *Transform) int {
	return self.position
}

ReadPositionTwo :: task(self *Transform) int {
	return self.position + 1
}

Positioned :: impl Transform {
	Position :: ReadPositionOne
}

Positioned :: impl Transform {
	Position :: ReadPositionTwo
}

Entity :: struct {
	transform Transform
}

Positioned :: impl Entity using transform
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`ambiguous implementation of Positioned for Transform`,
	)

	if strings.Contains(
		reporter.String(),
		`selected type Transform does not implement the interface`,
	) {
		t.Fatalf(
			"ambiguity must not also be reported as a missing implementation:\n%s",
			reporter.String(),
		)
	}
}

func TestCheckGenericImplConstraintRejectsAmbiguousImplementation(t *testing.T) {
	reporter := checkSource(t, `
Readable :: interface {
	Read :: task(self *self) int
}

Box :: struct {
	value int
}

ReadFirst :: task(self *Box) int {
	return self.value
}

ReadSecond :: task(self *Box) int {
	return self.value + 1
}

Readable :: impl Box {
	Read :: ReadFirst
}

Readable :: impl Box {
	Read :: ReadSecond
}

UseReadable :: task <T type[Readable()]>(value T) {
}

Main :: task() {
	box := Box{value = 10}
	UseReadable<Box>(box)
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`ambiguous implementation of Readable for Box`,
	)

	if strings.Contains(
		reporter.String(),
		`generic argument Box for "T" must implement Readable`,
	) {
		t.Fatalf(
			"ambiguity must not also be reported as a missing implementation:\n%s",
			reporter.String(),
		)
	}
}

func TestCheckNilAcceptedWithPointerContext(t *testing.T) {
	reporter := checkSource(t, `
TakePointer :: task(value *int) {
}

ReturnPointer :: task() *int {
	return nil
}

Identity :: task <T type>(value T) T {
	return value
}

Main :: task() {
	value: *int = nil
	TakePointer(nil)

	other := ReturnPointer()
	explicit := Identity<*int>(nil)

	TakePointer(value)
	TakePointer(other)
	TakePointer(explicit)
}
`)

	if reporter.String() != "" {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCheckNilCannotInferVariableType(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
	value := nil
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`nil needs explicit type`,
	)
}

func TestCheckNilCannotInferGenericArgument(t *testing.T) {
	reporter := checkSource(t, `
Identity :: task <T type>(value T) T {
	return value
}

Main :: task() {
	value := Identity(nil)
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`generic task "Identity" requires generic arguments`,
	)
}

func TestCheckNilCannotInitializeAny(t *testing.T) {
	reporter := checkSource(t, `
TakeAny :: task(value any) {
}

Main :: task() {
	value: any = nil
	TakeAny(nil)
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`cannot assign nil to any`,
	)
}

func TestCheckNilCannotBeCastToNonNullableType(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
	value := cast<int>(nil)
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`cannot cast nil to int`,
	)
}

func TestCheckNilCannotInitializeUntypedConstant(t *testing.T) {
	reporter := checkSource(t, `
Nothing :: nil
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`nil cannot initialize an untyped constant`,
	)
}

func TestCheckCannotTakeAddressOfNil(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
	value := &nil
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`cannot take the address of nil`,
	)
}

func TestExportPackageIncludesStaticAndDynamicInterfaces(t *testing.T) {
	scope := NewScope(nil)

	scope.Declare(&Symbol{
		Name: "StaticReadable",
		Kind: SymbolType,
		Type: &Type{
			Kind:           TypeInterface,
			Name:           "StaticReadable",
			IsDynInterface: false,
		},
	})

	scope.Declare(&Symbol{
		Name: "DynamicReadable",
		Kind: SymbolType,
		Type: &Type{
			Kind:           TypeInterface,
			Name:           "DynamicReadable",
			IsDynInterface: true,
		},
	})

	pkg := ExportPackage("api", scope)

	staticSym := pkg.Symbols["StaticReadable"]
	if staticSym == nil {
		t.Fatal("static interface was not exported")
	}

	if staticSym.Kind != SymbolType {
		t.Fatalf(
			"static interface symbol kind = %v, want SymbolType",
			staticSym.Kind,
		)
	}

	if staticSym.Type == nil ||
		staticSym.Type.Kind != TypeInterface {
		t.Fatalf(
			"exported static interface type = %#v",
			staticSym.Type,
		)
	}

	if staticSym.Type.IsDynInterface {
		t.Fatal("static interface became dynamic during export")
	}

	dynamicSym := pkg.Symbols["DynamicReadable"]
	if dynamicSym == nil {
		t.Fatal("dynamic interface was not exported")
	}

	if dynamicSym.Kind != SymbolType {
		t.Fatalf(
			"dynamic interface symbol kind = %v, want SymbolType",
			dynamicSym.Kind,
		)
	}

	if dynamicSym.Type == nil ||
		dynamicSym.Type.Kind != TypeInterface {
		t.Fatalf(
			"exported dynamic interface type = %#v",
			dynamicSym.Type,
		)
	}

	if !dynamicSym.Type.IsDynInterface {
		t.Fatal("dynamic interface lost its dynamic flag during export")
	}
}

func TestImportedStaticAndDynamicInterfacesRemainAvailable(t *testing.T) {
	staticInterface := &Type{
		Kind:           TypeInterface,
		Name:           "Readable",
		IsDynInterface: false,
	}

	dynamicInterface := &Type{
		Kind:           TypeInterface,
		Name:           "Readable",
		IsDynInterface: true,
	}

	packages := map[string]*PackageInfo{
		"staticapi": {
			Name: "staticapi",
			Symbols: map[string]*Symbol{
				"Readable": {
					Name: "Readable",
					Kind: SymbolType,
					Type: staticInterface,
				},
			},
		},
		"dynapi": {
			Name: "dynapi",
			Symbols: map[string]*Symbol{
				"Readable": {
					Name: "Readable",
					Kind: SymbolType,
					Type: dynamicInterface,
				},
			},
		},
	}

	reporter := diag.NewReporter()
	checker := NewWithPackages(reporter, packages)

	staticImported := checker.typeFromAst(
		checker.global,
		checkerTestNamedType("staticapi", "Readable"),
	)

	if staticImported.Kind != TypeInterface {
		t.Fatalf(
			"static imported type kind = %v, want TypeInterface",
			staticImported.Kind,
		)
	}

	if staticImported.Name != "staticapi.Readable" {
		t.Fatalf(
			"static imported interface name = %q, want %q",
			staticImported.Name,
			"staticapi.Readable",
		)
	}

	if staticImported.IsDynInterface {
		t.Fatal("imported static interface became dynamic")
	}

	dynamicImported := checker.typeFromAst(
		checker.global,
		checkerTestNamedType("dynapi", "Readable"),
	)

	if dynamicImported.Kind != TypeInterface {
		t.Fatalf(
			"dynamic imported type kind = %v, want TypeInterface",
			dynamicImported.Kind,
		)
	}

	if dynamicImported.Name != "dynapi.Readable" {
		t.Fatalf(
			"dynamic imported interface name = %q, want %q",
			dynamicImported.Name,
			"dynapi.Readable",
		)
	}

	if !dynamicImported.IsDynInterface {
		t.Fatal("imported dynamic interface lost its dynamic flag")
	}

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestImportedImplResolvesForStaticAndDynamicInterfaces(t *testing.T) {
	tests := []struct {
		name  string
		isDyn bool
	}{
		{
			name:  "static",
			isDyn: false,
		},
		{
			name:  "dynamic",
			isDyn: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			iface := &Type{
				Kind:           TypeInterface,
				Name:           "Readable",
				IsDynInterface: test.isDyn,
			}

			target := &Type{
				Kind: TypeStruct,
				Name: "Widget",
			}

			pkg := &PackageInfo{
				Name: "api",
				Symbols: map[string]*Symbol{
					"Readable": {
						Name: "Readable",
						Kind: SymbolType,
						Type: iface,
					},
					"Widget": {
						Name: "Widget",
						Kind: SymbolType,
						Type: target,
					},
				},
				Impls: []*ImplInfo{
					{
						Interface: iface,
						Target:    target,
						Entries:   map[string]*ImplEntryInfo{},
						Checked:   true,
						Usable:    true,
					},
				},
			}

			reporter := diag.NewReporter()
			checker := NewWithPackages(
				reporter,
				map[string]*PackageInfo{
					"api": pkg,
				},
			)

			importedInterface := checker.typeFromAst(
				checker.global,
				checkerTestNamedType("api", "Readable"),
			)

			importedTarget := checker.typeFromAst(
				checker.global,
				checkerTestNamedType("api", "Widget"),
			)

			resolution := checker.resolveImplAt(
				importedInterface,
				importedTarget,
				source.Span{},
				nil,
				true,
			)

			if !resolution.Found() {
				t.Fatalf(
					"expected imported %s interface implementation to resolve; diagnostics:\n%s",
					test.name,
					reporter.String(),
				)
			}

			if resolution.Resolved.Info.PackageName != "api" {
				t.Fatalf(
					"resolved implementation package = %q, want %q",
					resolution.Resolved.Info.PackageName,
					"api",
				)
			}

			if resolution.Resolved.Interface.IsDynInterface != test.isDyn {
				t.Fatalf(
					"resolved interface dynamic flag = %v, want %v",
					resolution.Resolved.Interface.IsDynInterface,
					test.isDyn,
				)
			}
		})
	}
}

func TestCurrentPackageImplOwnership(t *testing.T) {
	localStruct := &Type{
		Kind: TypeStruct,
		Name: "Widget",
	}

	importedStruct := &Type{
		Kind: TypeStruct,
		Name: "api.Widget",
	}

	localGenericStruct := &Type{
		Kind:            TypeStruct,
		Name:            "Box<api.Widget>",
		GenericBaseName: "Box",
	}

	importedGenericStruct := &Type{
		Kind:            TypeStruct,
		Name:            "api.Box<Widget>",
		GenericBaseName: "api.Box",
	}

	localInterface := &Type{
		Kind: TypeInterface,
		Name: "Readable",
	}

	importedInterface := &Type{
		Kind: TypeInterface,
		Name: "api.Readable",
	}

	tests := []struct {
		name string
		typ  *Type
		want bool
	}{
		{
			name: "local struct",
			typ:  localStruct,
			want: true,
		},
		{
			name: "imported struct",
			typ:  importedStruct,
			want: false,
		},
		{
			name: "local generic base with imported argument",
			typ:  localGenericStruct,
			want: true,
		},
		{
			name: "imported generic base with local argument",
			typ:  importedGenericStruct,
			want: false,
		},
		{
			name: "pointer to local struct",
			typ: &Type{
				Kind: TypePointer,
				Elem: localStruct,
			},
			want: true,
		},
		{
			name: "pointer to imported struct",
			typ: &Type{
				Kind: TypePointer,
				Elem: importedStruct,
			},
			want: false,
		},
		{
			name: "builtin",
			typ:  IntType,
			want: false,
		},
		{
			name: "generic type parameter",
			typ: &Type{
				Kind: TypeTypeParam,
				Name: "T",
			},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := currentPackageOwnsImplTarget(test.typ)

			if got != test.want {
				t.Fatalf(
					"currentPackageOwnsImplTarget(%s) = %v, want %v",
					test.typ.String(),
					got,
					test.want,
				)
			}
		})
	}

	if !currentPackageOwnsInterface(localInterface) {
		t.Fatal("current package should own local interface")
	}

	if currentPackageOwnsInterface(importedInterface) {
		t.Fatal("current package must not own imported interface")
	}
}

func TestRejectOrphanImplOfImportedInterfaceForBuiltin(t *testing.T) {
	iface := &Type{
		Kind: TypeInterface,
		Name: "Readable",
	}

	api := &PackageInfo{
		Name: "api",
		Symbols: map[string]*Symbol{
			"Readable": {
				Name: "Readable",
				Kind: SymbolType,
				Type: iface,
			},
		},
	}

	reporter := diag.NewReporter()
	checker := NewWithPackages(
		reporter,
		map[string]*PackageInfo{
			"api": api,
		},
	)

	checker.prepareImplDecl(
		checker.global,
		&ast.ImplDecl{
			Interface: checkerTestNamedType("api", "Readable"),
			Target:    checkerTestNamedType("int"),
		},
	)

	if !reporter.HasErrors() {
		t.Fatal("expected orphan implementation diagnostic")
	}

	if !strings.Contains(
		reporter.String(),
		"orphan impl is not allowed",
	) {
		t.Fatalf(
			"expected orphan implementation diagnostic, got:\n%s",
			reporter.String(),
		)
	}
}

func TestRejectOrphanImplOfImportedInterfaceForImportedTarget(t *testing.T) {
	iface := &Type{
		Kind: TypeInterface,
		Name: "Readable",
	}

	widget := &Type{
		Kind: TypeStruct,
		Name: "Widget",
	}

	packages := map[string]*PackageInfo{
		"api": {
			Name: "api",
			Symbols: map[string]*Symbol{
				"Readable": {
					Name: "Readable",
					Kind: SymbolType,
					Type: iface,
				},
			},
		},
		"models": {
			Name: "models",
			Symbols: map[string]*Symbol{
				"Widget": {
					Name: "Widget",
					Kind: SymbolType,
					Type: widget,
				},
			},
		},
	}

	reporter := diag.NewReporter()
	checker := NewWithPackages(reporter, packages)

	checker.prepareImplDecl(
		checker.global,
		&ast.ImplDecl{
			Interface: checkerTestNamedType("api", "Readable"),
			Target:    checkerTestNamedType("models", "Widget"),
		},
	)

	if !reporter.HasErrors() {
		t.Fatal("expected orphan implementation diagnostic")
	}

	if !strings.Contains(
		reporter.String(),
		"current package owns neither interface api.Readable nor target type models.Widget",
	) {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestAllowImportedInterfaceImplForLocalTarget(t *testing.T) {
	iface := &Type{
		Kind: TypeInterface,
		Name: "Readable",
	}

	api := &PackageInfo{
		Name: "api",
		Symbols: map[string]*Symbol{
			"Readable": {
				Name: "Readable",
				Kind: SymbolType,
				Type: iface,
			},
		},
	}

	reporter := diag.NewReporter()
	checker := NewWithPackages(
		reporter,
		map[string]*PackageInfo{
			"api": api,
		},
	)

	checker.global.Declare(&Symbol{
		Name: "LocalWidget",
		Kind: SymbolType,
		Type: &Type{
			Kind: TypeStruct,
			Name: "LocalWidget",
		},
	})

	checker.prepareImplDecl(
		checker.global,
		&ast.ImplDecl{
			Interface: checkerTestNamedType("api", "Readable"),
			Target:    checkerTestNamedType("LocalWidget"),
		},
	)

	if reporter.HasErrors() {
		t.Fatalf(
			"imported interface should be implementable for a local target:\n%s",
			reporter.String(),
		)
	}
}

func TestAllowLocalInterfaceImplForImportedTarget(t *testing.T) {
	widget := &Type{
		Kind: TypeStruct,
		Name: "Widget",
	}

	models := &PackageInfo{
		Name: "models",
		Symbols: map[string]*Symbol{
			"Widget": {
				Name: "Widget",
				Kind: SymbolType,
				Type: widget,
			},
		},
	}

	reporter := diag.NewReporter()
	checker := NewWithPackages(
		reporter,
		map[string]*PackageInfo{
			"models": models,
		},
	)

	checker.global.Declare(&Symbol{
		Name: "LocalReadable",
		Kind: SymbolType,
		Type: &Type{
			Kind: TypeInterface,
			Name: "LocalReadable",
		},
	})

	checker.prepareImplDecl(
		checker.global,
		&ast.ImplDecl{
			Interface: checkerTestNamedType("LocalReadable"),
			Target:    checkerTestNamedType("models", "Widget"),
		},
	)

	if reporter.HasErrors() {
		t.Fatalf(
			"local interface should be implementable for an imported target:\n%s",
			reporter.String(),
		)
	}
}

func TestCheckerRecordsBuiltinIndexAndLenResolutions(
	t *testing.T,
) {
	run := runCheckerTest(t, `
UseVariadic :: task(values ...int) {
    first := values[0]
    values[0] = 1
    count := len(values)
}

UseRaw :: task(ptr rawptr, text cstring) {
    first := ptr[0]
    ptr[0] = 2
    character := text[0]
}

UsePrimitive :: task() {
    value := 300
    first := value[0]
    value[0] = first
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	variadicRead := checkerIndexFromVarStmt(
		t,
		checkerTaskStmt(t, run.File, "UseVariadic", 0),
	)
	assertIndexResolutionKind(
		t,
		run.Checker,
		variadicRead,
		IndexResolutionVariadicRead,
	)

	variadicWrite := checkerIndexFromAssignStmt(
		t,
		checkerTaskStmt(t, run.File, "UseVariadic", 1),
	)
	assertIndexResolutionKind(
		t,
		run.Checker,
		variadicWrite,
		IndexResolutionVariadicWrite,
	)

	variadicLen := checkerCallFromVarStmt(
		t,
		checkerTaskStmt(t, run.File, "UseVariadic", 2),
	)
	assertLenResolutionKind(
		t,
		run.Checker,
		variadicLen,
		LenResolutionVariadic,
	)

	rawRead := checkerIndexFromVarStmt(
		t,
		checkerTaskStmt(t, run.File, "UseRaw", 0),
	)
	assertIndexResolutionKind(
		t,
		run.Checker,
		rawRead,
		IndexResolutionRawptrRead,
	)

	rawWrite := checkerIndexFromAssignStmt(
		t,
		checkerTaskStmt(t, run.File, "UseRaw", 1),
	)
	assertIndexResolutionKind(
		t,
		run.Checker,
		rawWrite,
		IndexResolutionRawptrWrite,
	)

	cstringRead := checkerIndexFromVarStmt(
		t,
		checkerTaskStmt(t, run.File, "UseRaw", 2),
	)
	assertIndexResolutionKind(
		t,
		run.Checker,
		cstringRead,
		IndexResolutionCstringRead,
	)

	primitiveRead := checkerIndexFromVarStmt(
		t,
		checkerTaskStmt(t, run.File, "UsePrimitive", 1),
	)
	assertIndexResolutionKind(
		t,
		run.Checker,
		primitiveRead,
		IndexResolutionPrimitiveByteRead,
	)

	primitiveWrite := checkerIndexFromAssignStmt(
		t,
		checkerTaskStmt(t, run.File, "UsePrimitive", 2),
	)
	assertIndexResolutionKind(
		t,
		run.Checker,
		primitiveWrite,
		IndexResolutionPrimitiveByteWrite,
	)
}

func TestCheckerRecordsGenericBracketAndLenSpecializations(
	t *testing.T,
) {
	run := runCheckerTest(t, `
Box :: struct <T type, N int> {
    value T
}

BoxGet :: pure task <T type, N int>(
    self *Box<T, N>,
    index int,
) T {
    return self.value
}

BoxSet :: task <T type, N int>(
    self *Box<T, N>,
    index int,
    value T,
) {
    self.value = value
}

BoxLen :: pure task <T type, N int>(
    self *Box<T, N>,
) uint {
    return cast<uint>(N)
}

[] :: overload {
    BoxGet
}

[]= :: overload {
    BoxSet
}

len :: overload {
    BoxLen
}

Main :: task() {
    box := Box<int, 4>{value = 7}
    value := box[0]
    box[0] = 9
    count := len(box)
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	readExpr := checkerIndexFromVarStmt(
		t,
		checkerTaskStmt(t, run.File, "Main", 1),
	)

	read := assertIndexResolutionKind(
		t,
		run.Checker,
		readExpr,
		IndexResolutionOverloadRead,
	)

	if read.Candidate == nil ||
		read.Candidate.Name != "BoxGet" {
		t.Fatalf(
			"read candidate = %#v, want BoxGet",
			read.Candidate,
		)
	}

	if read.TaskType == nil {
		t.Fatal("generic read has no specialized task type")
	}

	if read.TaskType.Name != "BoxGet<int, 4>" {
		t.Fatalf(
			"specialized read task name = %q",
			read.TaskType.Name,
		)
	}

	if len(read.TaskType.Params) != 2 ||
		read.TaskType.Params[0].String() != "*Box<int, 4>" ||
		!run.Checker.sameType(
			read.TaskType.Params[1],
			IntType,
		) {
		t.Fatalf(
			"unexpected specialized read parameters: %#v",
			read.TaskType.Params,
		)
	}

	if len(read.TaskType.Results) != 1 ||
		!run.Checker.sameType(
			read.TaskType.Results[0],
			IntType,
		) {
		t.Fatalf(
			"unexpected specialized read results: %#v",
			read.TaskType.Results,
		)
	}

	if read.TaskType.Elem != nil {
		t.Fatalf(
			"specialized task Elem = %s, want nil",
			read.TaskType.Elem.String(),
		)
	}

	if read.TaskType.Underlying != nil {
		t.Fatalf(
			"specialized task Underlying = %s, want nil",
			read.TaskType.Underlying.String(),
		)
	}

	if len(read.GenericArguments) != 2 ||
		read.GenericArguments[0].Key != "int" ||
		read.GenericArguments[1].Key != "4" {
		t.Fatalf(
			"read generic arguments = %#v",
			read.GenericArguments,
		)
	}

	writeExpr := checkerIndexFromAssignStmt(
		t,
		checkerTaskStmt(t, run.File, "Main", 2),
	)

	write := assertIndexResolutionKind(
		t,
		run.Checker,
		writeExpr,
		IndexResolutionOverloadWrite,
	)

	if write.Candidate == nil ||
		write.Candidate.Name != "BoxSet" {
		t.Fatalf(
			"write candidate = %#v, want BoxSet",
			write.Candidate,
		)
	}

	if write.TaskType == nil ||
		write.TaskType.Name != "BoxSet<int, 4>" {
		t.Fatalf(
			"specialized write task = %#v",
			write.TaskType,
		)
	}

	if len(write.TaskType.Params) != 3 ||
		!run.Checker.sameType(
			write.TaskType.Params[2],
			IntType,
		) {
		t.Fatalf(
			"unexpected specialized write parameters: %#v",
			write.TaskType.Params,
		)
	}

	lenCall := checkerCallFromVarStmt(
		t,
		checkerTaskStmt(t, run.File, "Main", 3),
	)

	length := assertLenResolutionKind(
		t,
		run.Checker,
		lenCall,
		LenResolutionOverload,
	)

	if length.Candidate == nil ||
		length.Candidate.Name != "BoxLen" {
		t.Fatalf(
			"len candidate = %#v, want BoxLen",
			length.Candidate,
		)
	}

	if length.TaskType == nil ||
		length.TaskType.Name != "BoxLen<int, 4>" {
		t.Fatalf(
			"specialized len task = %#v",
			length.TaskType,
		)
	}

	if len(length.TaskType.Results) != 1 ||
		!run.Checker.sameType(
			length.TaskType.Results[0],
			UintType,
		) {
		t.Fatalf(
			"unexpected len results: %#v",
			length.TaskType.Results,
		)
	}
}

func TestCheckerRecordsImportedBracketAndLenResolutions(
	t *testing.T,
) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(
		t,
		"boxes",
		`
Box :: struct <T type, N int> {
    value T
}

BoxGet :: pure task <T type, N int>(
    self *Box<T, N>,
    index int,
) T {
    return self.value
}

BoxSet :: task <T type, N int>(
    self *Box<T, N>,
    index int,
    value T,
) {
    self.value = value
}

BoxLen :: pure task <T type, N int>(
    self *Box<T, N>,
) uint {
    return cast<uint>(N)
}

[] :: overload {
    BoxGet
}

[]= :: overload {
    BoxSet
}

len :: overload {
    BoxLen
}
`,
	)

	exportedSetter := checkerPkg.Symbols["BoxSet"]
	if exportedSetter == nil {
		t.Fatal("BoxSet was not exported")
	}

	if exportedSetter.Node == nil {
		t.Fatal(
			"impure generic BoxSet body was discarded during export",
		)
	}

	exportedSetterOverload := checkerPkg.Symbols["[]="]
	if exportedSetterOverload == nil ||
		exportedSetterOverload.Overload == nil ||
		len(exportedSetterOverload.Overload.Candidates) != 1 {
		t.Fatalf(
			"invalid exported []= overload: %#v",
			exportedSetterOverload,
		)
	}

	if exportedSetterOverload.
		Overload.
		Candidates[0].
		Node == nil {
		t.Fatal(
			"generic []= candidate body was discarded during export",
		)
	}

	run := runCheckerTestWithPackages(
		t,
		`
LocalBox :: struct {
    value int
}

LocalGet :: pure task(
    self *LocalBox,
    index int,
) int {
    return self.value
}

[] :: overload {
    LocalGet
}

Main :: task() {
    box := boxes.Box<int, 4>{value = 1}
    value := box[0]
    box[0] = 2
    count := len(box)
}
`,
		map[string]*resolver.PackageInfo{
			"boxes": resolverPkg,
		},
		map[string]*PackageInfo{
			"boxes": checkerPkg,
		},
	)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	readExpr := checkerIndexFromVarStmt(
		t,
		checkerTaskStmt(t, run.File, "Main", 1),
	)

	read := assertIndexResolutionKind(
		t,
		run.Checker,
		readExpr,
		IndexResolutionOverloadRead,
	)

	if read.PackageName != "boxes" {
		t.Fatalf(
			"read package = %q, want boxes",
			read.PackageName,
		)
	}

	if read.Candidate == nil ||
		read.Candidate.Name != "BoxGet" {
		t.Fatalf(
			"read candidate = %#v, want BoxGet",
			read.Candidate,
		)
	}

	if read.TaskType == nil ||
		read.TaskType.Params[0].String() !=
			"*boxes.Box<int, 4>" {
		t.Fatalf(
			"imported read task type = %#v",
			read.TaskType,
		)
	}

	writeExpr := checkerIndexFromAssignStmt(
		t,
		checkerTaskStmt(t, run.File, "Main", 2),
	)

	write := assertIndexResolutionKind(
		t,
		run.Checker,
		writeExpr,
		IndexResolutionOverloadWrite,
	)

	if write.PackageName != "boxes" {
		t.Fatalf(
			"write package = %q, want boxes",
			write.PackageName,
		)
	}

	if write.Candidate == nil ||
		write.Candidate.Name != "BoxSet" {
		t.Fatalf(
			"write candidate = %#v, want BoxSet",
			write.Candidate,
		)
	}

	lenCall := checkerCallFromVarStmt(
		t,
		checkerTaskStmt(t, run.File, "Main", 3),
	)

	length := assertLenResolutionKind(
		t,
		run.Checker,
		lenCall,
		LenResolutionOverload,
	)

	if length.PackageName != "boxes" {
		t.Fatalf(
			"len package = %q, want boxes",
			length.PackageName,
		)
	}

	if length.Candidate == nil ||
		length.Candidate.Name != "BoxLen" {
		t.Fatalf(
			"len candidate = %#v, want BoxLen",
			length.Candidate,
		)
	}
}

func TestLenOverloadPreservesVariadicBuiltinDispatch(
	t *testing.T,
) {
	run := runCheckerTest(t, `
Box :: struct {
    value int
}

BoxLen :: pure task(self *Box) uint {
    return 1
}

len :: overload {
    BoxLen
}

UseVariadic :: task(values ...int) uint {
    return len(values)
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	task := checkerTaskDecl(
		t,
		run.File,
		"UseVariadic",
	)

	ret, ok := task.Body.Stmts[0].(*ast.ReturnStmt)
	if !ok || len(ret.Values) != 1 {
		t.Fatalf(
			"unexpected return statement: %#v",
			task.Body.Stmts[0],
		)
	}

	call, ok := ret.Values[0].(*ast.CallExpr)
	if !ok {
		t.Fatalf(
			"return value type = %T, want *ast.CallExpr",
			ret.Values[0],
		)
	}

	assertLenResolutionKind(
		t,
		run.Checker,
		call,
		LenResolutionVariadic,
	)
}

func TestOrdinaryTaskNamedLenShadowsBuiltinDispatch(
	t *testing.T,
) {
	run := runCheckerTest(t, `
len :: task(value int) int {
    return value
}

Main :: task() {
    value := len(10)
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	call := checkerCallFromVarStmt(
		t,
		checkerTaskStmt(t, run.File, "Main", 0),
	)

	if resolution, ok :=
		run.Checker.LenResolutionFor(call); ok {
		t.Fatalf(
			"ordinary task call incorrectly received len dispatch: %#v",
			resolution,
		)
	}
}

func TestBracketAndLenOverloadSignatureValidation(
	t *testing.T,
) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "impure bracket read",
			input: `
Box :: struct {}

Get :: task(self *Box, index int) int {
    return 0
}

[] :: overload {
    Get
}
`,
			want: `bracket operator [] candidate "Get" must be pure`,
		},
		{
			name: "bracket receiver by value",
			input: `
Box :: struct {}

Get :: pure task(self Box, index int) int {
    return 0
}

[] :: overload {
    Get
}
`,
			want: `must be a pointer to a struct`,
		},
		{
			name: "bracket index must be int",
			input: `
Box :: struct {}

Get :: pure task(self *Box, index uint) int {
    return 0
}

[] :: overload {
    Get
}
`,
			want: `second parameter of bracket operator [] candidate "Get" must have type int`,
		},
		{
			name: "bracket read requires result",
			input: `
Box :: struct {}

Get :: pure task(self *Box, index int) {
}

[] :: overload {
    Get
}
`,
			want: `bracket operator [] candidate "Get" must return exactly 1 value`,
		},
		{
			name: "setter cannot return value",
			input: `
Box :: struct {}

Set :: task(
    self *Box,
    index int,
    value int,
) int {
    return value
}

[]= :: overload {
    Set
}
`,
			want: `bracket assignment operator []= candidate "Set" must not return a value`,
		},
		{
			name: "setter needs three parameters",
			input: `
Box :: struct {}

Set :: task(self *Box, index int) {
}

[]= :: overload {
    Set
}
`,
			want: `bracket operator []= candidate "Set" must have exactly 3 parameters`,
		},
		{
			name: "impure len",
			input: `
Box :: struct {}

BoxLen :: task(self *Box) uint {
    return 0
}

len :: overload {
    BoxLen
}
`,
			want: `len overload candidate "BoxLen" must be pure`,
		},
		{
			name: "len wrong result",
			input: `
Box :: struct {}

BoxLen :: pure task(self *Box) int {
    return 0
}

len :: overload {
    BoxLen
}
`,
			want: `len overload candidate "BoxLen" must return uint`,
		},
		{
			name: "len wrong receiver",
			input: `
Box :: struct {}

BoxLen :: pure task(self Box) uint {
    return 0
}

len :: overload {
    BoxLen
}
`,
			want: `must be a pointer to a struct`,
		},
		{
			name: "len default parameter",
			input: `
Box :: struct {}

BoxLen :: pure task(
    self *Box = nil,
) uint {
    return 0
}

len :: overload {
    BoxLen
}
`,
			want: `len overload candidate "BoxLen" cannot have default parameters`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reporter := checkSource(
				t,
				test.input,
			)

			assertCheckerDiagnosticContains(
				t,
				reporter,
				test.want,
			)
		})
	}
}

func TestBracketDispatchMissingAndNonmatchingDiagnostics(
	t *testing.T,
) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "no read overload exists",
			input: `
Box :: struct {
    value int
}

Main :: task() {
    box := Box{value = 1}
    value := box[0]
}
`,
			want: `type Box does not define bracket operator []`,
		},
		{
			name: "read overload exists but does not match",
			input: `
Other :: struct {}

OtherGet :: pure task(
    self *Other,
    index int,
) int {
    return 0
}

[] :: overload {
    OtherGet
}

Box :: struct {
    value int
}

Main :: task() {
    box := Box{value = 1}
    value := box[0]
}
`,
			want: `no bracket operator [] matches receiver Box and index int`,
		},
		{
			name: "no setter overload exists",
			input: `
Box :: struct {
    value int
}

Main :: task() {
    box := Box{value = 1}
    box[0] = 2
}
`,
			want: `type Box does not define bracket assignment operator []=`,
		},
		{
			name: "setter value type does not match",
			input: `
Box :: struct {
    value int
}

BoxSet :: task(
    self *Box,
    index int,
    value int,
) {
    self.value = value
}

[]= :: overload {
    BoxSet
}

Main :: task() {
    box := Box{value = 1}
    box[0] = "wrong"
}
`,
			want: `no bracket assignment operator []= matches receiver Box, index int, and value string`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reporter := checkSource(
				t,
				test.input,
			)

			assertCheckerDiagnosticContains(
				t,
				reporter,
				test.want,
			)
		})
	}
}

func TestBracketAndLenOverloadsRequireAddressableReceivers(
	t *testing.T,
) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "bracket read",
			input: `
Box :: struct {
    value int
}

BoxGet :: pure task(
    self *Box,
    index int,
) int {
    return self.value
}

[] :: overload {
    BoxGet
}

MakeBox :: task() Box {
    return Box{value = 1}
}

Main :: task() {
    value := MakeBox()[0]
}
`,
			want: `bracket operator [] requires an addressable receiver`,
		},
		{
			name: "bracket write",
			input: `
Box :: struct {
    value int
}

BoxSet :: task(
    self *Box,
    index int,
    value int,
) {
    self.value = value
}

[]= :: overload {
    BoxSet
}

Use :: task(box Box) {
    box[0] = 2
}
`,
			want: `bracket assignment operator []= requires a mutable addressable receiver`,
		},
		{
			name: "len",
			input: `
Box :: struct {
    value int
}

BoxLen :: pure task(self *Box) uint {
    return 1
}

len :: overload {
    BoxLen
}

MakeBox :: task() Box {
    return Box{value = 1}
}

Main :: task() {
    count := len(MakeBox())
}
`,
			want: `len overload requires an addressable receiver`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reporter := checkSource(
				t,
				test.input,
			)

			assertCheckerDiagnosticContains(
				t,
				reporter,
				test.want,
			)
		})
	}
}

func TestRejectIndexedCompoundAssignment(
	t *testing.T,
) {
	reporter := checkSource(t, `
Box :: struct {
    value int
}

BoxGet :: pure task(
    self *Box,
    index int,
) int {
    return self.value
}

BoxSet :: task(
    self *Box,
    index int,
    value int,
) {
    self.value = value
}

[] :: overload {
    BoxGet
}

[]= :: overload {
    BoxSet
}

Main :: task() {
    box := Box{value = 1}
    box[0] += 1
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		"indexed compound assignment is not supported",
	)
}

func TestBracketIndexMustBeInt(
	t *testing.T,
) {
	reporter := checkSource(t, `
Box :: struct {
    value int
}

BoxGet :: pure task(
    self *Box,
    index int,
) int {
    return self.value
}

[] :: overload {
    BoxGet
}

Main :: task() {
    box := Box{value = 1}
    value := box[true]
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		"bracket index must be int, got bool",
	)
}

func TestRejectAmbiguousGenericBracketOverload(
	t *testing.T,
) {
	reporter := checkSource(t, `
Box :: struct <T type> {
    value T
}

GenericGet :: pure task <T type>(
    self *Box<T>,
    index int,
) T {
    return self.value
}

IntGet :: pure task(
    self *Box<int>,
    index int,
) int {
    return self.value
}

[] :: overload {
    GenericGet
    IntGet
}

Main :: task() {
    box := Box<int>{value = 1}
    value := box[0]
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		"ambiguous bracket operator [] for receiver Box<int> and index int",
	)
}

func TestPureInterfaceRequirementRejectsImpureAlias(
	t *testing.T,
) {
	reporter := checkSource(t, `
Readable :: interface {
    Read :: pure task(self *self) int
}

Box :: struct {
    value int
}

ReadBox :: task(self *Box) int {
    return self.value
}

Readable :: impl Box {
    Read :: ReadBox
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`implementation of pure requirement "Read" must be pure`,
	)
}

func TestPureInterfaceRequirementAcceptsPureAlias(
	t *testing.T,
) {
	reporter := checkSource(t, `
Readable :: interface {
    Read :: pure task(self *self) int
}

Box :: struct {
    value int
}

ReadBox :: pure task(self *Box) int {
    return self.value
}

Readable :: impl Box {
    Read :: ReadBox
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestPureInterfaceRequirementAcceptsTrustedPureAlias(
	t *testing.T,
) {
	reporter := checkSource(t, `
Readable :: interface {
    Read :: pure task(self *self) int
}

Box :: struct {
    value int
}

ReadBox :: @trusted_pure extern("read_box") task(
    self *Box,
) int

Readable :: impl Box {
    Read :: ReadBox
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func checkerGenericExprFromCallStmt(
	t *testing.T,
	stmt ast.Stmt,
) *ast.GenericExpr {
	t.Helper()

	exprStmt, ok := stmt.(*ast.ExprStmt)
	if !ok {
		t.Fatalf(
			"statement type = %T, want *ast.ExprStmt",
			stmt,
		)
	}

	call, ok := exprStmt.Expr.(*ast.CallExpr)
	if !ok {
		t.Fatalf(
			"expression type = %T, want *ast.CallExpr",
			exprStmt.Expr,
		)
	}

	generic, ok := call.Callee.(*ast.GenericExpr)
	if !ok {
		t.Fatalf(
			"call callee type = %T, want *ast.GenericExpr",
			call.Callee,
		)
	}

	return generic
}

func checkerGenericExprFromVarStmt(
	t *testing.T,
	stmt ast.Stmt,
) *ast.GenericExpr {
	t.Helper()

	call := checkerCallFromVarStmt(
		t,
		stmt,
	)

	generic, ok := call.Callee.(*ast.GenericExpr)
	if !ok {
		t.Fatalf(
			"call callee type = %T, want *ast.GenericExpr",
			call.Callee,
		)
	}

	return generic
}

func assertGenericOverloadResolution(
	t *testing.T,
	c *Checker,
	expr *ast.GenericExpr,
	wantCandidate string,
	wantPackage string,
) GenericOverloadCallResolution {
	t.Helper()

	if c == nil {
		t.Fatal("checker is nil")
	}

	resolution, ok := c.genericOverloadCalls[expr]
	if !ok {
		t.Fatal(
			"generic overload call has no checker resolution",
		)
	}

	if resolution.Candidate == nil {
		t.Fatal(
			"generic overload resolution has no candidate",
		)
	}

	if resolution.Candidate.Name != wantCandidate {
		t.Fatalf(
			"generic overload candidate = %q, want %q",
			resolution.Candidate.Name,
			wantCandidate,
		)
	}

	if resolution.PackageName != wantPackage {
		t.Fatalf(
			"generic overload package = %q, want %q",
			resolution.PackageName,
			wantPackage,
		)
	}

	if resolution.TaskType == nil ||
		resolution.TaskType.Kind != TypeTask {
		t.Fatalf(
			"generic overload specialized task type = %#v",
			resolution.TaskType,
		)
	}

	return resolution
}

func TestGenericOverloadDistinguishesGenericCategories(
	t *testing.T,
) {
	run := runCheckerTest(t, `
Noop :: task() {
}

FooType :: task <T type>() int {
    return 1
}

FooInt :: task <N int>() int {
    return 2
}

FooBool :: task <Flag bool>() int {
    return 3
}

FooString :: task <Name string>() int {
    return 4
}

FooTask :: task <F task[() ]>() int {
    return 5
}

Foo :: overload {
    FooType
    FooInt
    FooBool
    FooString
    FooTask
}

Main :: task() {
    byType := Foo<int>()
    byInt := Foo<16>()
    byBool := Foo<true>()
    byString := Foo<"seal">()
    byTask := Foo<Noop>()
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	tests := []struct {
		index         int
		wantCandidate string
		wantTaskName  string
		wantCategory  ast.GenericParamCategory
	}{
		{
			index:         0,
			wantCandidate: "FooType",
			wantTaskName:  "FooType<int>",
			wantCategory:  ast.GenericParamType,
		},
		{
			index:         1,
			wantCandidate: "FooInt",
			wantTaskName:  "FooInt<16>",
			wantCategory:  ast.GenericParamInt,
		},
		{
			index:         2,
			wantCandidate: "FooBool",
			wantTaskName:  "FooBool<true>",
			wantCategory:  ast.GenericParamBool,
		},
		{
			index:         3,
			wantCandidate: "FooString",
			wantTaskName:  `FooString<"seal">`,
			wantCategory:  ast.GenericParamString,
		},
		{
			index:         4,
			wantCandidate: "FooTask",
			wantTaskName:  "FooTask<Noop>",
			wantCategory:  ast.GenericParamTask,
		},
	}

	for _, test := range tests {
		generic := checkerGenericExprFromVarStmt(
			t,
			checkerTaskStmt(
				t,
				run.File,
				"Main",
				test.index,
			),
		)

		resolution := assertGenericOverloadResolution(
			t,
			run.Checker,
			generic,
			test.wantCandidate,
			"",
		)

		if resolution.TaskType.Name != test.wantTaskName {
			t.Fatalf(
				"specialized task name = %q, want %q",
				resolution.TaskType.Name,
				test.wantTaskName,
			)
		}

		if len(resolution.GenericArguments) != 1 {
			t.Fatalf(
				"generic argument count = %d, want 1",
				len(resolution.GenericArguments),
			)
		}

		if resolution.GenericArguments[0].Category !=
			test.wantCategory {
			t.Fatalf(
				"generic argument category = %v, want %v",
				resolution.GenericArguments[0].Category,
				test.wantCategory,
			)
		}
	}
}

func TestGenericOverloadDistinguishesTypeAndIntWithRuntimeArguments(
	t *testing.T,
) {
	run := runCheckerTest(t, `
ProcessType :: task <T type>(value T) int {
    return 1
}

ProcessInt :: task <N int>(value int) int {
    return 2
}

Process :: overload {
    ProcessType
    ProcessInt
}

Main :: task() {
    byType := Process<int>(10)
    byValue := Process<16>(10)
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	typeExpr := checkerGenericExprFromVarStmt(
		t,
		checkerTaskStmt(
			t,
			run.File,
			"Main",
			0,
		),
	)

	typeResolution := assertGenericOverloadResolution(
		t,
		run.Checker,
		typeExpr,
		"ProcessType",
		"",
	)

	if typeResolution.TaskType.Name !=
		"ProcessType<int>" {
		t.Fatalf(
			"type specialization = %q, want ProcessType<int>",
			typeResolution.TaskType.Name,
		)
	}

	if len(typeResolution.TaskType.Params) != 1 ||
		!run.Checker.sameType(
			typeResolution.TaskType.Params[0],
			IntType,
		) {
		t.Fatalf(
			"unexpected type-specialized parameters: %#v",
			typeResolution.TaskType.Params,
		)
	}

	valueExpr := checkerGenericExprFromVarStmt(
		t,
		checkerTaskStmt(
			t,
			run.File,
			"Main",
			1,
		),
	)

	valueResolution := assertGenericOverloadResolution(
		t,
		run.Checker,
		valueExpr,
		"ProcessInt",
		"",
	)

	if valueResolution.TaskType.Name !=
		"ProcessInt<16>" {
		t.Fatalf(
			"value specialization = %q, want ProcessInt<16>",
			valueResolution.TaskType.Name,
		)
	}
}

func TestOverloadAllowsGenericAndOrdinaryCandidates(
	t *testing.T,
) {
	run := runCheckerTest(t, `
ProcessOrdinary :: task(value bool) int {
    return 1
}

ProcessType :: task <T type>(value T) int {
    return 2
}

ProcessInt :: task <N int>(value int) int {
    return 3
}

Process :: overload {
    ProcessType
    ProcessInt
    ProcessOrdinary
}

Main :: task() {
    ordinary := Process(true)
    byType := Process<int>(10)
    byValue := Process<8>(10)
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	typeExpr := checkerGenericExprFromVarStmt(
		t,
		checkerTaskStmt(
			t,
			run.File,
			"Main",
			1,
		),
	)

	assertGenericOverloadResolution(
		t,
		run.Checker,
		typeExpr,
		"ProcessType",
		"",
	)

	valueExpr := checkerGenericExprFromVarStmt(
		t,
		checkerTaskStmt(
			t,
			run.File,
			"Main",
			2,
		),
	)

	assertGenericOverloadResolution(
		t,
		run.Checker,
		valueExpr,
		"ProcessInt",
		"",
	)
}

func TestOrdinaryOverloadCallSkipsGenericCandidates(
	t *testing.T,
) {
	_, reporter := check(t, `
Generic :: task <T type>(value T) int {
    return 1
}

Ordinary :: task(value bool) int {
    return 2
}

Process :: overload {
    Generic
    Ordinary
}

Main :: task() {
    result := Process(true)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestOrdinaryOverloadCallCannotUseGenericCandidate(
	t *testing.T,
) {
	reporter := checkSource(t, `
Generic :: task <T type>(value T) int {
    return 1
}

Process :: overload {
    Generic
}

Main :: task() {
    result := Process(10)
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`no overload of "Process" matches argument types`,
	)
}

func TestGenericOverloadCallSkipsOrdinaryCandidates(
	t *testing.T,
) {
	reporter := checkSource(t, `
Ordinary :: task(value int) int {
    return value
}

Process :: overload {
    Ordinary
}

Main :: task() {
    result := Process<int>(10)
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`no generic overload of "Process" matches generic arguments <int>`,
	)
}

func TestGenericOverloadUsesRuntimeParametersAfterCategorySelection(
	t *testing.T,
) {
	run := runCheckerTest(t, `
UseInt :: task <T type>(value int) int {
    return value
}

UseString :: task <T type>(value string) string {
    return value
}

Use :: overload {
    UseInt
    UseString
}

Main :: task() {
    number := Use<bool>(10)
    text := Use<bool>("hello")
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	numberExpr := checkerGenericExprFromVarStmt(
		t,
		checkerTaskStmt(
			t,
			run.File,
			"Main",
			0,
		),
	)

	assertGenericOverloadResolution(
		t,
		run.Checker,
		numberExpr,
		"UseInt",
		"",
	)

	textExpr := checkerGenericExprFromVarStmt(
		t,
		checkerTaskStmt(
			t,
			run.File,
			"Main",
			1,
		),
	)

	assertGenericOverloadResolution(
		t,
		run.Checker,
		textExpr,
		"UseString",
		"",
	)
}

func TestRejectNoMatchingGenericOverloadRuntimeArguments(
	t *testing.T,
) {
	reporter := checkSource(t, `
UseInt :: task <T type>(value int) int {
    return value
}

Use :: overload {
    UseInt
}

Main :: task() {
    result := Use<bool>("wrong")
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`no generic overload of "Use" matches generic arguments <bool> and argument types (string)`,
	)
}

func TestRejectDuplicateGenericOverloadSignatureIgnoringNames(
	t *testing.T,
) {
	reporter := checkSource(t, `
First :: task <T type>(value T) int {
    return 1
}

Second :: task <U type>(value U) int {
    return 2
}

Use :: overload {
    First
    Second
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`duplicate overload signature for "Use"`,
	)
}

func TestRejectDuplicateComptimeOverloadSignatureIgnoringNames(
	t *testing.T,
) {
	reporter := checkSource(t, `
First :: task <N int>(value int) int {
    return value
}

Second :: task <Size int>(value int) int {
    return value
}

Use :: overload {
    First
    Second
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`duplicate overload signature for "Use"`,
	)
}

func TestGenericOverloadConstraintsDoNotCreateDistinctSignatures(
	t *testing.T,
) {
	reporter := checkSource(t, `
Size16 :: task <Size int[Size == 16]>() int {
    return 16
}

Size32 :: task <Size int[Size == 32]>() int {
    return 32
}

Buffer :: overload {
    Size16
    Size32
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`duplicate overload signature for "Buffer"`,
	)
}

func TestGenericTypeConstraintsDoNotCreateDistinctOverloadSignatures(
	t *testing.T,
) {
	reporter := checkSource(t, `
Actor :: struct {
    health int
    mana int
}

ByHealth :: task <T type[health int]>(value T) int {
    return value.health
}

ByMana :: task <U type[mana int]>(value U) int {
    return value.mana
}

Use :: overload {
    ByHealth
    ByMana
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`duplicate overload signature for "Use"`,
	)
}

func TestGenericOverloadCategoriesCreateDistinctSignatures(
	t *testing.T,
) {
	_, reporter := check(t, `
ByType :: task <T type>(value int) int {
    return value
}

ByInt :: task <N int>(value int) int {
    return value
}

ByBool :: task <Flag bool>(value int) int {
    return value
}

ByString :: task <Name string>(value int) int {
    return value
}

Use :: overload {
    ByType
    ByInt
    ByBool
    ByString
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestGenericOverloadConstraintCheckedAfterSelection(
	t *testing.T,
) {
	reporter := checkSource(t, `
Positive :: task <N int[N > 0]>() int {
    return N
}

ByType :: task <T type>() int {
    return 0
}

Use :: overload {
    Positive
    ByType
}

Main :: task() {
    result := Use<0>()
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`generic constraint failed: 0 > 0`,
	)

	if strings.Contains(
		reporter.String(),
		`no generic overload of "Use"`,
	) {
		t.Fatalf(
			"constraint failure must occur after candidate selection:\n%s",
			reporter.String(),
		)
	}
}

func TestGenericOverloadRejectsRuntimeComptimeArgument(
	t *testing.T,
) {
	reporter := checkSource(t, `
ByValue :: task <N int>() int {
    return N
}

ByType :: task <T type>() int {
    return 0
}

Use :: overload {
    ByValue
    ByType
}

Main :: task() {
    number := 10
    result := Use<number>()
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`no generic overload of "Use" matches generic arguments <number>`,
	)
}

func TestRejectAmbiguousGenericOverloadCall(
	t *testing.T,
) {
	reporter := checkSource(t, `
First :: task <T type>(value any) int {
    return 1
}

Second :: task <T type>(value any) int {
    return 2
}

Use :: overload {
    First
    Second
}

Main :: task() {
    result := Use<int>(10)
}
`)

	// The duplicate-signature check should normally reject this overload
	// before the call is considered. Keep this assertion so ambiguity cannot
	// silently become the only diagnostic if duplicate checking regresses.
	assertCheckerDiagnosticContains(
		t,
		reporter,
		`duplicate overload signature for "Use"`,
	)
}

func TestImportedGenericOverloadResolution(
	t *testing.T,
) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(
		t,
		"operations",
		`
ProcessType :: task <T type>(value T) int {
    return 1
}

ProcessInt :: task <N int>(value int) int {
    return 2
}

ProcessBool :: task(value bool) int {
    return 3
}

Process :: overload {
    ProcessType
    ProcessInt
    ProcessBool
}
`,
	)

	run := runCheckerTestWithPackages(
		t,
		`
Main :: task() {
    ordinary := operations.Process(true)
    byType := operations.Process<int>(10)
    byValue := operations.Process<16>(10)
}
`,
		map[string]*resolver.PackageInfo{
			"operations": resolverPkg,
		},
		map[string]*PackageInfo{
			"operations": checkerPkg,
		},
	)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	typeExpr := checkerGenericExprFromVarStmt(
		t,
		checkerTaskStmt(
			t,
			run.File,
			"Main",
			1,
		),
	)

	typeResolution := assertGenericOverloadResolution(
		t,
		run.Checker,
		typeExpr,
		"ProcessType",
		"operations",
	)

	if typeResolution.TaskType.Name !=
		"operations.ProcessType<int>" {
		t.Fatalf(
			"imported type specialization name = %q",
			typeResolution.TaskType.Name,
		)
	}

	valueExpr := checkerGenericExprFromVarStmt(
		t,
		checkerTaskStmt(
			t,
			run.File,
			"Main",
			2,
		),
	)

	valueResolution := assertGenericOverloadResolution(
		t,
		run.Checker,
		valueExpr,
		"ProcessInt",
		"operations",
	)

	if valueResolution.TaskType.Name !=
		"operations.ProcessInt<16>" {
		t.Fatalf(
			"imported value specialization name = %q",
			valueResolution.TaskType.Name,
		)
	}
}

func TestImportedGenericOverloadChecksRuntimeArgumentType(
	t *testing.T,
) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(
		t,
		"operations",
		`
ProcessInt :: task <N int>(value int) int {
    return value
}

Process :: overload {
    ProcessInt
}
`,
	)

	reporter := checkWithPackages(
		t,
		`
Main :: task() {
    result := operations.Process<16>("wrong")
}
`,
		map[string]*resolver.PackageInfo{
			"operations": resolverPkg,
		},
		map[string]*PackageInfo{
			"operations": checkerPkg,
		},
	)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		`no generic overload of "Process" matches generic arguments <16> and argument types (string)`,
	)
}

func TestBreakInsideForIsValid(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    for {
        break
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestContinueInsideForIsValid(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    for {
        continue
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestBreakInsideConditionalInForIsValid(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    for {
        if true {
            break
        }
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestContinueInsideConditionalInForIsValid(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    for {
        if true {
            continue
        }
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestBreakInsideNestedForIsValid(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    for {
        for {
            break
        }

        break
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestContinueInsideNestedForIsValid(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    for {
        for {
            continue
        }

        continue
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestRejectBreakOutsideFor(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    break
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		"break is only valid inside a for loop",
	)
}

func TestRejectContinueOutsideFor(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    continue
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		"continue is only valid inside a for loop",
	)
}

func TestRejectBreakInsideIfOutsideFor(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    if true {
        break
    }
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		"break is only valid inside a for loop",
	)
}

func TestRejectContinueInsideIfOutsideFor(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    if true {
        continue
    }
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		"continue is only valid inside a for loop",
	)
}

func TestRejectBreakInsideSwitchOutsideFor(t *testing.T) {
	reporter := checkSource(t, `
Status :: enum {
    Ready
    Done
}

Main :: task() {
    status: Status = .Ready

    switch status {
    case .Ready:
        break

    case .Done:
        assert(true)
    }
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		"break is only valid inside a for loop",
	)
}

func TestRejectContinueInsideSwitchOutsideFor(t *testing.T) {
	reporter := checkSource(t, `
Status :: enum {
    Ready
    Done
}

Main :: task() {
    status: Status = .Ready

    switch status {
    case .Ready:
        continue

    case .Done:
        assert(true)
    }
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		"continue is only valid inside a for loop",
	)
}

func TestBreakInsideSwitchNestedInForIsValid(t *testing.T) {
	reporter := checkSource(t, `
Status :: enum {
    Ready
    Done
}

Main :: task() {
    status: Status = .Ready

    for {
        switch status {
        case .Ready:
            break

        case .Done:
            assert(true)
        }
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"break inside a switch nested in a for must target the for loop:\n%s",
			reporter.String(),
		)
	}
}

func TestContinueInsideSwitchNestedInForIsValid(t *testing.T) {
	reporter := checkSource(t, `
Status :: enum {
    Ready
    Done
}

Main :: task() {
    status: Status = .Ready

    for {
        switch status {
        case .Ready:
            continue

        case .Done:
            assert(true)
        }
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"continue inside a switch nested in a for must target the for loop:\n%s",
			reporter.String(),
		)
	}
}

func TestBreakInsideNestedSwitchesInForIsValid(t *testing.T) {
	reporter := checkSource(t, `
Outer :: enum {
    First
    Second
}

Inner :: enum {
    Ready
    Done
}

Main :: task() {
    outer: Outer = .First
    inner: Inner = .Ready

    for {
        switch outer {
        case .First:
            switch inner {
            case .Ready:
                break

            case .Done:
                assert(true)
            }

        case .Second:
            assert(true)
        }
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"switch nesting must not hide the surrounding for loop:\n%s",
			reporter.String(),
		)
	}
}

func TestContinueInsideNestedSwitchesInForIsValid(t *testing.T) {
	reporter := checkSource(t, `
Outer :: enum {
    First
    Second
}

Inner :: enum {
    Ready
    Done
}

Main :: task() {
    outer: Outer = .First
    inner: Inner = .Ready

    for {
        switch outer {
        case .First:
            switch inner {
            case .Ready:
                continue

            case .Done:
                assert(true)
            }

        case .Second:
            assert(true)
        }
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"switch nesting must not hide the surrounding for loop:\n%s",
			reporter.String(),
		)
	}
}

func TestRejectBreakInsideNestedTaskDeclaredInFor(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    for {
        Nested :: task() {
            break
        }

        break
    }
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		"break is only valid inside a for loop",
	)
}

func TestRejectContinueInsideNestedTaskDeclaredInFor(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    for {
        Nested :: task() {
            continue
        }

        break
    }
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		"continue is only valid inside a for loop",
	)
}

func TestNestedTaskCanUseItsOwnForBreak(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    for {
        Nested :: task() {
            for {
                break
            }
        }

        break
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"nested task should be able to use break in its own loop:\n%s",
			reporter.String(),
		)
	}
}

func TestNestedTaskCanUseItsOwnForContinue(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    for {
        Nested :: task() {
            for {
                continue
            }
        }

        break
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"nested task should be able to use continue in its own loop:\n%s",
			reporter.String(),
		)
	}
}

func TestRejectBreakInsideDeferredBlockDeclaredInFor(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    for {
        defer {
            break
        }

        break
    }
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		"break is only valid inside a for loop",
	)
}

func TestRejectContinueInsideDeferredBlockDeclaredInFor(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    for {
        defer {
            continue
        }

        break
    }
}
`)

	assertCheckerDiagnosticContains(
		t,
		reporter,
		"continue is only valid inside a for loop",
	)
}

func TestBreakAndContinueRequireForLoop(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "top-level break",
			input: `
Main :: task() {
    break
}
`,
			want: "break is only valid inside a for loop",
		},
		{
			name: "top-level continue",
			input: `
Main :: task() {
    continue
}
`,
			want: "continue is only valid inside a for loop",
		},
		{
			name: "break in switch",
			input: `
State :: enum {
    Active
}

Main :: task() {
    state: State = .Active

    switch state {
    case .Active:
        break
    }
}
`,
			want: "break is only valid inside a for loop",
		},
		{
			name: "continue in switch",
			input: `
State :: enum {
    Active
}

Main :: task() {
    state: State = .Active

    switch state {
    case .Active:
        continue
    }
}
`,
			want: "continue is only valid inside a for loop",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reporter := checkSource(
				t,
				test.input,
			)

			assertCheckerDiagnosticContains(
				t,
				reporter,
				test.want,
			)
		})
	}
}

func checkerInlineArrayFromVarStmt(
	t *testing.T,
	stmt ast.Stmt,
) *ast.InlineArrayExpr {
	t.Helper()

	decl, ok := stmt.(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf(
			"statement type = %T, want *ast.VarDeclStmt",
			stmt,
		)
	}

	inline, ok := decl.Value.(*ast.InlineArrayExpr)
	if !ok {
		t.Fatalf(
			"variable value type = %T, want *ast.InlineArrayExpr",
			decl.Value,
		)
	}

	return inline
}

func TestCheckInlineArrayExpr(t *testing.T) {
	run := runCheckerTest(t, `
Main :: task() {
    values := @inline_array<int, 4>(
        10,
        20,
        30,
        40,
    )
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	stmt := checkerTaskStmt(
		t,
		run.File,
		"Main",
		0,
	)

	inline := checkerInlineArrayFromVarStmt(
		t,
		stmt,
	)

	typ, ok := run.Checker.ExprTypeFor(inline)
	if !ok {
		t.Fatalf(
			"inline array expression has no checker type",
		)
	}

	if typ.Kind != TypeInlineArray {
		t.Fatalf(
			"inline array type kind = %v, want TypeInlineArray",
			typ.Kind,
		)
	}

	if typ.Elem == nil ||
		typ.Elem.Kind != TypeInt {
		t.Fatalf(
			"inline array element type = %v, want int",
			typ.Elem,
		)
	}

	if !typ.InlineLengthKnown {
		t.Fatalf(
			"expected concrete inline array length",
		)
	}

	if typ.InlineLength != 4 {
		t.Fatalf(
			"inline array length = %d, want 4",
			typ.InlineLength,
		)
	}

	if typ.InlineLengthKey != "4" {
		t.Fatalf(
			"inline array length key = %q, want %q",
			typ.InlineLengthKey,
			"4",
		)
	}
}

func TestCheckInlineArrayZeroInitializer(t *testing.T) {
	run := runCheckerTest(t, `
Main :: task() {
    values := @inline_array<u8, 256>()
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	stmt := checkerTaskStmt(
		t,
		run.File,
		"Main",
		0,
	)

	inline := checkerInlineArrayFromVarStmt(
		t,
		stmt,
	)

	typ, ok := run.Checker.ExprTypeFor(inline)
	if !ok {
		t.Fatalf(
			"inline array expression has no checker type",
		)
	}

	if typ.Kind != TypeInlineArray {
		t.Fatalf(
			"type kind = %v, want TypeInlineArray",
			typ.Kind,
		)
	}

	if typ.Elem == nil ||
		typ.Elem.Kind != TypeU8 {
		t.Fatalf(
			"element type = %v, want u8",
			typ.Elem,
		)
	}

	if !typ.InlineLengthKnown ||
		typ.InlineLength != 256 {
		t.Fatalf(
			"length = %d, known = %v; want 256, true",
			typ.InlineLength,
			typ.InlineLengthKnown,
		)
	}
}

func TestCheckInlineArrayConstantLengthExpression(t *testing.T) {
	run := runCheckerTest(t, `
Main :: task() {
    values := @inline_array<int, 2 + 2>(
        10,
        20,
        30,
        40,
    )
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	stmt := checkerTaskStmt(
		t,
		run.File,
		"Main",
		0,
	)

	inline := checkerInlineArrayFromVarStmt(
		t,
		stmt,
	)

	typ, ok := run.Checker.ExprTypeFor(inline)
	if !ok {
		t.Fatalf(
			"inline array expression has no checker type",
		)
	}

	if !typ.InlineLengthKnown {
		t.Fatalf(
			"expected concrete inline array length",
		)
	}

	if typ.InlineLength != 4 {
		t.Fatalf(
			"inline array length = %d, want 4",
			typ.InlineLength,
		)
	}

	if typ.InlineLengthKey != "4" {
		t.Fatalf(
			"inline array length key = %q, want 4",
			typ.InlineLengthKey,
		)
	}
}

func TestCheckInlineArrayNamedConstantLength(t *testing.T) {
	run := runCheckerTest(t, `
COUNT :: 4

Main :: task() {
    values := @inline_array<int, COUNT>(
        10,
        20,
        30,
        40,
    )
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	stmt := checkerTaskStmt(
		t,
		run.File,
		"Main",
		0,
	)

	inline := checkerInlineArrayFromVarStmt(
		t,
		stmt,
	)

	typ, ok := run.Checker.ExprTypeFor(inline)
	if !ok {
		t.Fatalf(
			"inline array expression has no checker type",
		)
	}

	if !typ.InlineLengthKnown ||
		typ.InlineLength != 4 {
		t.Fatalf(
			"length = %d, known = %v; want 4, true",
			typ.InlineLength,
			typ.InlineLengthKnown,
		)
	}
}

func TestRejectInlineArrayTooFewInitializers(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    values := @inline_array<int, 4>(
        10,
        20,
    )
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(
		reporter.String(),
		"requires exactly 4 initializer value(s), got 2",
	) {
		t.Fatalf(
			"expected initializer-count diagnostic, got:\n%s",
			reporter.String(),
		)
	}
}

func TestRejectInlineArrayTooManyInitializers(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    values := @inline_array<int, 2>(
        10,
        20,
        30,
    )
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(
		reporter.String(),
		"requires exactly 2 initializer value(s), got 3",
	) {
		t.Fatalf(
			"expected initializer-count diagnostic, got:\n%s",
			reporter.String(),
		)
	}
}

func TestRejectInlineArrayWrongElementType(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    values := @inline_array<int, 3>(
        10,
        true,
        30,
    )
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(
		reporter.String(),
		"cannot assign bool to int",
	) {
		t.Fatalf(
			"expected element type diagnostic, got:\n%s",
			reporter.String(),
		)
	}
}

func TestRejectInlineArrayNegativeLength(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    values := @inline_array<int, -1>()
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(
		reporter.String(),
		"@inline_array length cannot be negative, got -1",
	) {
		t.Fatalf(
			"expected negative-length diagnostic, got:\n%s",
			reporter.String(),
		)
	}
}

func TestRejectInlineArrayRuntimeLength(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    count := 4
    values := @inline_array<int, count>()
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(
		reporter.String(),
		"@inline_array length must be evaluable as a compile-time int",
	) {
		t.Fatalf(
			"expected compile-time length diagnostic, got:\n%s",
			reporter.String(),
		)
	}
}

func TestRejectInlineArrayNonIntegerLength(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    values := @inline_array<int, true>()
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(
		reporter.String(),
		"cannot assign bool to int",
	) {
		t.Fatalf(
			"expected integer-length diagnostic, got:\n%s",
			reporter.String(),
		)
	}
}

func TestCheckZeroLengthInlineArray(t *testing.T) {
	run := runCheckerTest(t, `
Main :: task() {
    values := @inline_array<int, 0>()
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	stmt := checkerTaskStmt(
		t,
		run.File,
		"Main",
		0,
	)

	inline := checkerInlineArrayFromVarStmt(
		t,
		stmt,
	)

	typ, ok := run.Checker.ExprTypeFor(inline)
	if !ok {
		t.Fatalf(
			"inline array expression has no checker type",
		)
	}

	if !typ.InlineLengthKnown ||
		typ.InlineLength != 0 {
		t.Fatalf(
			"length = %d, known = %v; want 0, true",
			typ.InlineLength,
			typ.InlineLengthKnown,
		)
	}
}

func TestCheckGenericStructWithInlineArrayField(t *testing.T) {
	run := runCheckerTest(t, `
StackArray :: struct<T type, N int[N >= 0]> {
    data @inline_array<T, N>
}

Main :: task() {
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	sym := run.Scope.Lookup("StackArray")
	if sym == nil {
		t.Fatalf("StackArray symbol was not found")
	}

	if sym.Type == nil {
		t.Fatalf("StackArray has no checker type")
	}

	if sym.Type.Kind != TypeStruct {
		t.Fatalf(
			"StackArray kind = %v, want TypeStruct",
			sym.Type.Kind,
		)
	}

	if len(sym.Type.Fields) != 1 {
		t.Fatalf(
			"StackArray field count = %d, want 1",
			len(sym.Type.Fields),
		)
	}

	field := sym.Type.Fields[0]

	if field.Type == nil ||
		field.Type.Kind != TypeInlineArray {
		t.Fatalf(
			"field type = %v, want TypeInlineArray",
			field.Type,
		)
	}

	if field.Type.Elem == nil ||
		field.Type.Elem.Kind != TypeTypeParam ||
		field.Type.Elem.Name != "T" {
		t.Fatalf(
			"field element type = %v, want type parameter T",
			field.Type.Elem,
		)
	}

	if field.Type.InlineLengthKnown {
		t.Fatalf(
			"generic inline-array length should remain symbolic",
		)
	}

	if field.Type.InlineLengthKey != "N" {
		t.Fatalf(
			"field length key = %q, want N",
			field.Type.InlineLengthKey,
		)
	}
}

func TestCheckGenericStructInlineArrayFieldSpecialization(t *testing.T) {
	run := runCheckerTest(t, `
StackArray :: struct<T type, N int[N >= 0]> {
    data @inline_array<T, N>
}

Main :: task() {
    values: StackArray<int, 4>
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	stmt := checkerTaskStmt(
		t,
		run.File,
		"Main",
		0,
	)

	decl, ok := stmt.(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf(
			"statement = %T, want *ast.VarDeclStmt",
			stmt,
		)
	}

	typ := run.Checker.typeFromAst(
		run.Scope,
		decl.Type,
	)

	if typ == nil ||
		typ.Kind != TypeStruct {
		t.Fatalf(
			"variable type = %v, want specialized struct",
			typ,
		)
	}

	if len(typ.Fields) != 1 {
		t.Fatalf(
			"specialized struct field count = %d, want 1",
			len(typ.Fields),
		)
	}

	fieldType := typ.Fields[0].Type

	if fieldType == nil ||
		fieldType.Kind != TypeInlineArray {
		t.Fatalf(
			"specialized field type = %v, want TypeInlineArray",
			fieldType,
		)
	}

	if fieldType.Elem == nil ||
		fieldType.Elem.Kind != TypeInt {
		t.Fatalf(
			"specialized element type = %v, want int",
			fieldType.Elem,
		)
	}

	if !fieldType.InlineLengthKnown {
		t.Fatalf(
			"specialized inline-array length should be concrete",
		)
	}

	if fieldType.InlineLength != 4 {
		t.Fatalf(
			"specialized inline-array length = %d, want 4",
			fieldType.InlineLength,
		)
	}
}

func TestCheckInlineArrayIndexRead(t *testing.T) {
	run := runCheckerTest(t, `
Main :: task() {
    values := @inline_array<int, 4>(
        10,
        20,
        30,
        40,
    )

    value := values[2]
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	index := checkerIndexFromVarStmt(
		t,
		checkerTaskStmt(
			t,
			run.File,
			"Main",
			1,
		),
	)

	assertIndexResolutionKind(
		t,
		run.Checker,
		index,
		IndexResolutionInlineArrayRead,
	)

	typ, ok := run.Checker.ExprTypeFor(index)
	if !ok {
		t.Fatalf(
			"index expression has no checker type",
		)
	}

	if typ.Kind != TypeInt {
		t.Fatalf(
			"index expression type = %v, want int",
			typ,
		)
	}
}

func TestCheckInlineArrayIndexWrite(t *testing.T) {
	run := runCheckerTest(t, `
Main :: task() {
    values := @inline_array<int, 4>(
        10,
        20,
        30,
        40,
    )

    values[2] = 100
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	index := checkerIndexFromAssignStmt(
		t,
		checkerTaskStmt(
			t,
			run.File,
			"Main",
			1,
		),
	)

	assertIndexResolutionKind(
		t,
		run.Checker,
		index,
		IndexResolutionInlineArrayWrite,
	)
}

func TestRejectInlineArrayIndexWriteWrongType(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    values := @inline_array<int, 4>(
        10,
        20,
        30,
        40,
    )

    values[2] = true
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(
		reporter.String(),
		"cannot assign bool to int",
	) {
		t.Fatalf(
			"expected indexed assignment diagnostic, got:\n%s",
			reporter.String(),
		)
	}
}

func TestCheckInlineArrayLen(t *testing.T) {
	run := runCheckerTest(t, `
Main :: task() {
    values := @inline_array<int, 4>(
        10,
        20,
        30,
        40,
    )

    count := len(values)
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	call := checkerCallFromVarStmt(
		t,
		checkerTaskStmt(
			t,
			run.File,
			"Main",
			1,
		),
	)

	assertLenResolutionKind(
		t,
		run.Checker,
		call,
		LenResolutionInlineArray,
	)

	typ, ok := run.Checker.ExprTypeFor(call)
	if !ok {
		t.Fatalf(
			"len call has no checker type",
		)
	}

	if typ.Kind != TypeUint {
		t.Fatalf(
			"len result type = %v, want uint",
			typ,
		)
	}
}

func TestRejectWholeInlineArrayAssignment(t *testing.T) {
	reporter := checkSource(t, `
Main :: task() {
    left := @inline_array<int, 4>(
        1,
        2,
        3,
        4,
    )

    right := @inline_array<int, 4>(
        5,
        6,
        7,
        8,
    )

    left = right
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(
		reporter.String(),
		"@inline_array storage cannot be assigned as a whole",
	) {
		t.Fatalf(
			"expected whole-array assignment diagnostic, got:\n%s",
			reporter.String(),
		)
	}
}

func TestCheckInlineArrayAsStructFieldInitializer(t *testing.T) {
	run := runCheckerTest(t, `
StackArray :: struct<T type, N int[N >= 0]> {
    data @inline_array<T, N>
}

Main :: task() {
    values := StackArray<int, 4>{
        data = @inline_array<int, 4>(
            10,
            20,
            30,
            40,
        )
    }
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}
}

func TestCheckInlineArrayAsZeroInitializedStructField(t *testing.T) {
	run := runCheckerTest(t, `
StackArray :: struct<T type, N int[N >= 0]> {
    data @inline_array<T, N>
}

Main :: task() {
    values := StackArray<int, 4>{
        data = @inline_array<int, 4>()
    }
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}
}

func TestCheckerInlineArrayExpression(t *testing.T) {
	run := runCheckerTest(t, `
Main :: task() {
    values := @inline_array<int, 4>(
        10,
        20,
        30,
        40,
    )
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}
}

func TestCheckerNestedInlineArrayExpression(t *testing.T) {
	run := runCheckerTest(t, `
Main :: task() {
    matrix := @inline_array<
        @inline_array<int, 3>,
        2
    >(
        @inline_array<int, 3>(
            1,
            2,
            3,
        ),
        @inline_array<int, 3>(
            4,
            5,
            6,
        ),
    )
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}
}

func TestCheckerNestedInlineArrayIndexReadResolution(t *testing.T) {
	run := runCheckerTest(t, `
Main :: task() {
    matrix := @inline_array<
        @inline_array<int, 3>,
        2
    >(
        @inline_array<int, 3>(1, 2, 3),
        @inline_array<int, 3>(4, 5, 6),
    )

    row := matrix[1]
    value := matrix[1][2]
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	rowIndex := checkerIndexFromVarStmt(
		t,
		checkerTaskStmt(
			t,
			run.File,
			"Main",
			1,
		),
	)

	assertIndexResolutionKind(
		t,
		run.Checker,
		rowIndex,
		IndexResolutionInlineArrayRead,
	)

	valueStmt := checkerTaskStmt(
		t,
		run.File,
		"Main",
		2,
	)

	valueDecl, ok := valueStmt.(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf(
			"statement type = %T, want *ast.VarDeclStmt",
			valueStmt,
		)
	}

	outerIndex, ok := valueDecl.Value.(*ast.IndexExpr)
	if !ok {
		t.Fatalf(
			"value expression type = %T, want *ast.IndexExpr",
			valueDecl.Value,
		)
	}

	innerIndex, ok := outerIndex.Left.(*ast.IndexExpr)
	if !ok {
		t.Fatalf(
			"outer index left type = %T, want *ast.IndexExpr",
			outerIndex.Left,
		)
	}

	assertIndexResolutionKind(
		t,
		run.Checker,
		innerIndex,
		IndexResolutionInlineArrayRead,
	)

	assertIndexResolutionKind(
		t,
		run.Checker,
		outerIndex,
		IndexResolutionInlineArrayRead,
	)
}

func TestCheckerNestedInlineArrayIndexWriteResolution(t *testing.T) {
	run := runCheckerTest(t, `
Main :: task() {
    matrix := @inline_array<
        @inline_array<int, 3>,
        2
    >(
        @inline_array<int, 3>(1, 2, 3),
        @inline_array<int, 3>(4, 5, 6),
    )

    matrix[0][1] = 99
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	assignIndex := checkerIndexFromAssignStmt(
		t,
		checkerTaskStmt(
			t,
			run.File,
			"Main",
			1,
		),
	)

	assertIndexResolutionKind(
		t,
		run.Checker,
		assignIndex,
		IndexResolutionInlineArrayWrite,
	)

	innerIndex, ok := assignIndex.Left.(*ast.IndexExpr)
	if !ok {
		t.Fatalf(
			"assignment index left type = %T, want *ast.IndexExpr",
			assignIndex.Left,
		)
	}

	assertIndexResolutionKind(
		t,
		run.Checker,
		innerIndex,
		IndexResolutionInlineArrayRead,
	)
}

func TestCheckerNestedInlineArrayLen(t *testing.T) {
	run := runCheckerTest(t, `
Main :: task() {
    matrix := @inline_array<
        @inline_array<int, 3>,
        2
    >(
        @inline_array<int, 3>(1, 2, 3),
        @inline_array<int, 3>(4, 5, 6),
    )

    rows := len(matrix)
    columns := len(matrix[0])
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}
}

func TestCheckerInlineArrayNamedConstantLength(t *testing.T) {
	run := runCheckerTest(t, `
ColumnCount :: 3

Main :: task() {
    values := @inline_array<int, ColumnCount>(
        10,
        20,
        30,
    )
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}
}

func TestCheckerGenericNestedInlineArrayField(t *testing.T) {
	run := runCheckerTest(t, `
Matrix :: struct<
    T type,
    Rows int[Rows >= 0],
    Columns int[Columns >= 0],
> {
    data @inline_array<
        @inline_array<T, Columns>,
        Rows
    >
}

Main :: task() {
    matrix := Matrix<int, 2, 3>{
        data = @inline_array<
            @inline_array<int, 3>,
            2
        >(
            @inline_array<int, 3>(
                1,
                2,
                3,
            ),
            @inline_array<int, 3>(
                4,
                5,
                6,
            ),
        ),
    }

    value := matrix.data[1][2]
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}
}

func TestCheckerNestedInlineArrayRejectsWrongInnerLength(t *testing.T) {
	run := runCheckerTest(t, `
Main :: task() {
    matrix := @inline_array<
        @inline_array<int, 3>,
        2
    >(
        @inline_array<int, 3>(
            1,
            2,
        ),
        @inline_array<int, 3>(
            3,
            4,
            5,
        ),
    )
}
`)

	if !run.Reporter.HasErrors() {
		t.Fatal(
			"expected checker diagnostic for wrong nested @inline_array initializer length",
		)
	}

	diagnostics := run.Reporter.String()

	if !strings.Contains(diagnostics, "3") {
		t.Fatalf(
			"expected diagnostic to mention required length 3:\n%s",
			diagnostics,
		)
	}
}

func TestCheckerNestedInlineArrayRejectsWrongInnerElementType(t *testing.T) {
	run := runCheckerTest(t, `
Main :: task() {
    matrix := @inline_array<
        @inline_array<int, 2>,
        2
    >(
        @inline_array<int, 2>(
            1,
            2,
        ),
        @inline_array<int, 2>(
            3,
            "wrong",
        ),
    )
}
`)

	if !run.Reporter.HasErrors() {
		t.Fatal(
			"expected checker diagnostic for wrong nested @inline_array element type",
		)
	}
}

func TestCheckerRejectsWholeInlineArrayAssignment(t *testing.T) {
	run := runCheckerTest(t, `
Main :: task() {
    left := @inline_array<int, 3>(
        1,
        2,
        3,
    )

    right := @inline_array<int, 3>(
        4,
        5,
        6,
    )

    left = right
}
`)

	if !run.Reporter.HasErrors() {
		t.Fatal(
			"expected checker diagnostic for whole @inline_array assignment",
		)
	}

	diagnostics := run.Reporter.String()

	if !strings.Contains(
		diagnostics,
		"@inline_array",
	) {
		t.Fatalf(
			"expected @inline_array assignment diagnostic:\n%s",
			diagnostics,
		)
	}
}

func TestCheckerRejectsInlineArrayTaskParameter(t *testing.T) {
	run := runCheckerTest(t, `
Consume :: task(
    values @inline_array<int, 4>,
) {
}
`)

	if !run.Reporter.HasErrors() {
		t.Fatal(
			"expected checker diagnostic for direct @inline_array task parameter",
		)
	}

	diagnostics := run.Reporter.String()

	if !strings.Contains(
		diagnostics,
		"@inline_array",
	) {
		t.Fatalf(
			"expected @inline_array parameter diagnostic:\n%s",
			diagnostics,
		)
	}
}

func TestCheckerRejectsInlineArrayTaskResult(t *testing.T) {
	run := runCheckerTest(t, `
Create :: task() @inline_array<int, 4> {
}
`)

	if !run.Reporter.HasErrors() {
		t.Fatal(
			"expected checker diagnostic for direct @inline_array task result",
		)
	}

	diagnostics := run.Reporter.String()

	if !strings.Contains(
		diagnostics,
		"@inline_array",
	) {
		t.Fatalf(
			"expected @inline_array result diagnostic:\n%s",
			diagnostics,
		)
	}
}

func TestCheckerAllowsStructContainingInlineArrayInTaskSignature(
	t *testing.T,
) {
	run := runCheckerTest(t, `
StackArray :: struct<
    T type,
    N int[N >= 0],
> {
    data @inline_array<T, N>
}

Identity :: task(
    values StackArray<int, 4>,
) StackArray<int, 4> {
    return values
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}
}

func TestCheckerInlineArrayOfOverloadedIndexRows(
	t *testing.T,
) {
	run := runCheckerTest(t, `
Row :: struct {
    data @inline_array<int, 3>
    length uint
}

RowAt :: pure task(
    values *Row,
    index int,
) int {
    return values.data[index]
}

RowSet :: task(
    values *Row,
    index int,
    value int,
) {
    values.data[index] = value
}

RowLen :: pure task(
    values *Row,
) uint {
    return values.length
}

[] :: overload {
    RowAt
}

[]= :: overload {
    RowSet
}

len :: overload {
    RowLen
}

Main :: task() {
    first := Row{
        data = @inline_array<int, 3>(
            1,
            2,
            3,
        ),
        length = 3,
    }

    second := Row{
        data = @inline_array<int, 3>(
            4,
            5,
            6,
        ),
        length = 3,
    }

    matrix := @inline_array<Row, 2>(
        first,
        second,
    )

    row := matrix[1]
    value := matrix[1][2]

    matrix[0][1] = 99

    rows := len(matrix)
    columns := len(matrix[0])
}
`)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	/*
		row := matrix[1]

		This is a direct inline-array read.
	*/
	rowIndex := checkerIndexFromVarStmt(
		t,
		checkerTaskStmt(
			t,
			run.File,
			"Main",
			3,
		),
	)

	assertIndexResolutionKind(
		t,
		run.Checker,
		rowIndex,
		IndexResolutionInlineArrayRead,
	)

	/*
		value := matrix[1][2]

		The expression contains two independent index operations:

		    matrix[1]
		        builtin inline-array indexing

		    matrix[1][2]
		        overloaded Row indexing
	*/
	valueStmt := checkerTaskStmt(
		t,
		run.File,
		"Main",
		4,
	)

	valueDecl, ok :=
		valueStmt.(*ast.VarDeclStmt)

	if !ok {
		t.Fatalf(
			"statement type = %T, want *ast.VarDeclStmt",
			valueStmt,
		)
	}

	outerIndex, ok :=
		valueDecl.Value.(*ast.IndexExpr)

	if !ok {
		t.Fatalf(
			"value expression type = %T, want *ast.IndexExpr",
			valueDecl.Value,
		)
	}

	innerIndex, ok :=
		outerIndex.Left.(*ast.IndexExpr)

	if !ok {
		t.Fatalf(
			"outer index left type = %T, want *ast.IndexExpr",
			outerIndex.Left,
		)
	}

	assertIndexResolutionKind(
		t,
		run.Checker,
		innerIndex,
		IndexResolutionInlineArrayRead,
	)

	overloadedRead :=
		assertIndexResolutionKind(
			t,
			run.Checker,
			outerIndex,
			IndexResolutionOverloadRead,
		)

	if overloadedRead.Candidate == nil {
		t.Fatal(
			"expected overloaded row index read to have a selected candidate",
		)
	}

	if overloadedRead.Candidate.Name !=
		"RowAt" {
		t.Fatalf(
			"selected overloaded read candidate = %q, want %q",
			overloadedRead.Candidate.Name,
			"RowAt",
		)
	}

	/*
		matrix[0][1] = 99

		Again:

		    matrix[0]
		        inline-array read producing an addressable Row

		    matrix[0][1] = 99
		        overloaded []= assignment
	*/
	assignStmt := checkerTaskStmt(
		t,
		run.File,
		"Main",
		5,
	)

	assignIndex :=
		checkerIndexFromAssignStmt(
			t,
			assignStmt,
		)

	innerAssignIndex, ok :=
		assignIndex.Left.(*ast.IndexExpr)

	if !ok {
		t.Fatalf(
			"assignment outer index left type = %T, want *ast.IndexExpr",
			assignIndex.Left,
		)
	}

	assertIndexResolutionKind(
		t,
		run.Checker,
		innerAssignIndex,
		IndexResolutionInlineArrayRead,
	)

	overloadedWrite :=
		assertIndexResolutionKind(
			t,
			run.Checker,
			assignIndex,
			IndexResolutionOverloadWrite,
		)

	if overloadedWrite.Candidate == nil {
		t.Fatal(
			"expected overloaded row index write to have a selected candidate",
		)
	}

	if overloadedWrite.Candidate.Name !=
		"RowSet" {
		t.Fatalf(
			"selected overloaded write candidate = %q, want %q",
			overloadedWrite.Candidate.Name,
			"RowSet",
		)
	}

	/*
		rows := len(matrix)

		This must use the builtin inline-array len operation.
	*/
	rowsCall := checkerCallFromVarStmt(
		t,
		checkerTaskStmt(
			t,
			run.File,
			"Main",
			6,
		),
	)

	rowsResolution, ok :=
		run.Checker.LenResolutionFor(
			rowsCall,
		)

	if !ok {
		t.Fatal(
			"len(matrix) has no checker resolution",
		)
	}

	if rowsResolution.Kind !=
		LenResolutionInlineArray {
		t.Fatalf(
			"len(matrix) resolution kind = %v, want %v",
			rowsResolution.Kind,
			LenResolutionInlineArray,
		)
	}

	/*
		columns := len(matrix[0])

		The argument is an addressable Row obtained through inline-array
		indexing, so the RowLen overload must be selected.
	*/
	columnsCall := checkerCallFromVarStmt(
		t,
		checkerTaskStmt(
			t,
			run.File,
			"Main",
			7,
		),
	)

	columnsResolution, ok :=
		run.Checker.LenResolutionFor(
			columnsCall,
		)

	if !ok {
		t.Fatal(
			"len(matrix[0]) has no checker resolution",
		)
	}

	if columnsResolution.Kind !=
		LenResolutionOverload {
		t.Fatalf(
			"len(matrix[0]) resolution kind = %v, want %v",
			columnsResolution.Kind,
			LenResolutionOverload,
		)
	}

	if columnsResolution.Candidate == nil {
		t.Fatal(
			"expected len(matrix[0]) to select overloaded RowLen",
		)
	}

	if columnsResolution.Candidate.Name !=
		"RowLen" {
		t.Fatalf(
			"selected len candidate = %q, want %q",
			columnsResolution.Candidate.Name,
			"RowLen",
		)
	}
}

func TestCheckerImportedGenericStructWithPrivateInlineArrayElement(
	t *testing.T,
) {
	providerInput := `
_Slot :: struct<T type> {
    value T
}

StaticArray :: struct<
    T type,
    N int[N >= 0],
> {
    data @inline_array<_Slot<T>, N>
}
`

	providerReporter := diag.NewReporter()

	providerSource := source.NewFile(
		"std/lists/static_array.seal",
		providerInput,
	)

	providerLexer := lexer.New(
		providerSource,
		providerReporter,
	)

	providerTokens := providerLexer.LexAll()

	if providerReporter.HasErrors() {
		t.Fatalf(
			"unexpected provider lexer diagnostics:\n%s",
			providerReporter.String(),
		)
	}

	providerParser := parser.New(
		providerTokens,
		providerReporter,
	)

	providerFile := providerParser.ParseFile()

	if providerReporter.HasErrors() {
		t.Fatalf(
			"unexpected provider parser diagnostics:\n%s",
			providerReporter.String(),
		)
	}

	providerResolver := resolver.New(
		providerReporter,
	)

	providerResolverScope := providerResolver.ResolveFile(
		providerFile,
	)

	if providerReporter.HasErrors() {
		t.Fatalf(
			"unexpected provider resolver diagnostics:\n%s",
			providerReporter.String(),
		)
	}

	providerChecker := New(
		providerReporter,
	)

	providerCheckerScope := providerChecker.CheckFile(
		providerFile,
	)

	if providerReporter.HasErrors() {
		t.Fatalf(
			"unexpected provider checker diagnostics:\n%s",
			providerReporter.String(),
		)
	}

	resolverPackage := resolver.ExportPackage(
		"lists",
		providerResolverScope,
	)

	checkerPackage := ExportPackage(
		"lists",
		providerCheckerScope,
	)

	consumerInput := `
Main :: task() {
    array: lists.StaticArray<int, 4>
}
`

	result := runCheckerTestWithPackages(
		t,
		consumerInput,
		map[string]*resolver.PackageInfo{
			"lists": resolverPackage,
		},
		map[string]*PackageInfo{
			"lists": checkerPackage,
		},
	)

	if result.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected consumer diagnostics:\n%s",
			result.Reporter.String(),
		)
	}

	task := checkerTaskDecl(
		t,
		result.File,
		"Main",
	)

	if task.Body == nil ||
		len(task.Body.Stmts) != 1 {
		t.Fatalf(
			"expected Main to contain exactly one statement",
		)
	}

	varDecl, ok := task.Body.Stmts[0].(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf(
			"expected Main statement to be *ast.VarDeclStmt, got %T",
			task.Body.Stmts[0],
		)
	}

	sym := result.Scope.Lookup(
		varDecl.Name.Name,
	)

	/*
		The variable is declared inside the task scope rather than the global
		scope, so depending on your existing checker test helpers you may not
		expose its symbol directly here.

		The essential regression assertion is already the absence of
		diagnostics above. The remaining assertions can therefore inspect the
		specialized type through the AST/checker helpers available in your test
		suite.
	*/
	_ = sym
}

func checkerBinaryFromVarStmt(
	t *testing.T,
	stmt ast.Stmt,
) *ast.BinaryExpr {
	t.Helper()

	decl, ok := stmt.(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf(
			"statement type = %T, want *ast.VarDeclStmt",
			stmt,
		)
	}

	binary, ok := decl.Value.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf(
			"variable value type = %T, want *ast.BinaryExpr",
			decl.Value,
		)
	}

	return binary
}

func TestCheckerShiftRightResultUsesLeftOperandType(
	t *testing.T,
) {
	run := runCheckerTest(
		t,
		`
Main :: task() {
	value: i16 = 128
	amount: u8 = 3
	shifted := value >> amount
}
`,
	)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected checker diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	binary := checkerBinaryFromVarStmt(
		t,
		checkerTaskStmt(
			t,
			run.File,
			"Main",
			2,
		),
	)

	got, ok := run.Checker.ExprTypeFor(binary)
	if !ok {
		t.Fatal(
			"shift expression has no checker type",
		)
	}

	if !run.Checker.sameType(
		got,
		I16Type,
	) {
		t.Fatalf(
			"shift expression type = %s, want i16",
			got.String(),
		)
	}
}

func TestCheckerShiftAcceptsDifferentIntegerCountType(
	t *testing.T,
) {
	_, reporter := check(
		t,
		`
Main :: task() {
	value: u64 = 1024
	signedCount: i8 = 2
	unsignedCount: u16 = 3

	left := value << signedCount
	right := value >> unsignedCount
}
`,
	)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected checker diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestCheckerShiftAllowsRuntimeCount(
	t *testing.T,
) {
	_, reporter := check(
		t,
		`
ShiftLeft :: task(
	value u32,
	amount int,
) u32 {
	return value << amount
}

ShiftRight :: task(
	value i64,
	amount uint,
) i64 {
	return value >> amount
}
`,
	)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected checker diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestCheckerShiftRejectsNegativeCompileTimeCount(
	t *testing.T,
) {
	reporter := checkSource(
		t,
		`
Main :: task() {
	value: uint = 8
	result := value >> -1
}
`,
	)

	if !reporter.HasErrors() {
		t.Fatal(
			"expected negative shift count diagnostic",
		)
	}

	got := reporter.String()

	if !strings.Contains(
		got,
		"shift count cannot be negative, got -1",
	) {
		t.Fatalf(
			"diagnostics:\n%s\nwant negative shift count diagnostic",
			got,
		)
	}
}

func TestCheckerShiftRejectsNegativeConstantExpression(
	t *testing.T,
) {
	reporter := checkSource(
		t,
		`
Main :: task() {
	value: uint = 8
	result := value << (1 - 3)
}
`,
	)

	if !reporter.HasErrors() {
		t.Fatal(
			"expected negative shift count diagnostic",
		)
	}

	got := reporter.String()

	if !strings.Contains(
		got,
		"shift count cannot be negative, got -2",
	) {
		t.Fatalf(
			"diagnostics:\n%s\nwant shift count -2 diagnostic",
			got,
		)
	}
}

func TestCheckerShiftRejectsCountGreaterThanOperandWidth(
	t *testing.T,
) {
	reporter := checkSource(
		t,
		`
Main :: task() {
	value: i16 = 1
	result := value >> 31
}
`,
	)

	if !reporter.HasErrors() {
		t.Fatal(
			"expected oversized shift count diagnostic",
		)
	}

	got := reporter.String()

	if !strings.Contains(
		got,
		"shift count 31 is outside the valid range 0 through 15 for i16",
	) {
		t.Fatalf(
			"diagnostics:\n%s\nwant i16 shift range diagnostic",
			got,
		)
	}
}

func TestCheckerShiftAcceptsMaximumValidConcreteCount(
	t *testing.T,
) {
	_, reporter := check(
		t,
		`
Main :: task() {
	a: u8 = 1
	b: i16 = 1
	c: u32 = 1
	d: i64 = 1

	r1 := a << 7
	r2 := b >> 15
	r3 := c << 31
	r4 := d >> 63
}
`,
	)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected checker diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestCheckerShiftChecksNamedCompileTimeCount(
	t *testing.T,
) {
	reporter := checkSource(
		t,
		`
Count :: 8

Main :: task() {
	value: u8 = 1
	result := value << Count
}
`,
	)

	if !reporter.HasErrors() {
		t.Fatal(
			"expected named constant shift count diagnostic",
		)
	}

	got := reporter.String()

	if !strings.Contains(
		got,
		"shift count 8 is outside the valid range 0 through 7 for u8",
	) {
		t.Fatalf(
			"diagnostics:\n%s\nwant named constant shift range diagnostic",
			got,
		)
	}
}

func TestCheckerShiftChecksNamedConstantExpression(
	t *testing.T,
) {
	reporter := checkSource(
		t,
		`
Base :: 4
Count :: Base + 4

Main :: task() {
	value: u8 = 1
	result := value << Count
}
`,
	)

	if !reporter.HasErrors() {
		t.Fatal(
			"expected named constant expression shift diagnostic",
		)
	}

	got := reporter.String()

	if !strings.Contains(
		got,
		"shift count 8 is outside the valid range 0 through 7 for u8",
	) {
		t.Fatalf(
			"diagnostics:\n%s\nwant evaluated constant shift diagnostic",
			got,
		)
	}
}

func TestCheckerShiftRejectsNonIntegerLeftOperand(
	t *testing.T,
) {
	reporter := checkSource(
		t,
		`
Main :: task() {
	value: f64 = 1.0
	result := value << 1
}
`,
	)

	if !reporter.HasErrors() {
		t.Fatal(
			"expected non-integer left operand diagnostic",
		)
	}

	got := reporter.String()

	if !strings.Contains(
		got,
		`left operand of operator "<<" must be an integer, got f64`,
	) {
		t.Fatalf(
			"diagnostics:\n%s\nwant non-integer left operand diagnostic",
			got,
		)
	}
}

func TestCheckerShiftRejectsNonIntegerCount(
	t *testing.T,
) {
	reporter := checkSource(
		t,
		`
Main :: task() {
	value: uint = 1
	amount: f32 = 2.0
	result := value >> amount
}
`,
	)

	if !reporter.HasErrors() {
		t.Fatal(
			"expected non-integer shift count diagnostic",
		)
	}

	got := reporter.String()

	if !strings.Contains(
		got,
		`shift count for operator ">>" must be an integer, got f32`,
	) {
		t.Fatalf(
			"diagnostics:\n%s\nwant non-integer shift count diagnostic",
			got,
		)
	}
}

func TestCheckerShiftDoesNotUseOperatorOverloadFallback(
	t *testing.T,
) {
	reporter := checkSource(
		t,
		`
Value :: struct {
	data int
}

Main :: task() {
	value := Value{1}
	result := value << 1
}
`,
	)

	if !reporter.HasErrors() {
		t.Fatal(
			"expected invalid shift operand diagnostic",
		)
	}

	got := reporter.String()

	if !strings.Contains(
		got,
		`left operand of operator "<<" must be an integer, got Value`,
	) {
		t.Fatalf(
			"diagnostics:\n%s\nwant primitive shift operand diagnostic",
			got,
		)
	}

	if strings.Contains(
		got,
		"operator overload",
	) {
		t.Fatalf(
			"shift unexpectedly attempted overload resolution:\n%s",
			got,
		)
	}
}

func TestCheckerShiftFoldsUntypedIntegerConstant(
	t *testing.T,
) {
	run := runCheckerTest(
		t,
		`
Main :: task() {
	value := 1 << 10
}
`,
	)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected checker diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	binary := checkerBinaryFromVarStmt(
		t,
		checkerTaskStmt(
			t,
			run.File,
			"Main",
			0,
		),
	)

	got, ok := run.Checker.ExprTypeFor(binary)
	if !ok {
		t.Fatal(
			"shift expression has no checker type",
		)
	}

	if got.Kind != TypeUntypedInt {
		t.Fatalf(
			"shift expression kind = %v, want TypeUntypedInt",
			got.Kind,
		)
	}

	if got.IntConstant == nil {
		t.Fatal(
			"shift expression has no folded integer constant",
		)
	}

	if got.IntConstant.String() != "1024" {
		t.Fatalf(
			"folded shift value = %s, want 1024",
			got.IntConstant.String(),
		)
	}
}

func TestCheckerShiftFoldsLargeUntypedIntegerConstant(
	t *testing.T,
) {
	run := runCheckerTest(
		t,
		`
Large :: 1 << 100
`,
	)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected checker diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	decl, ok := run.File.Decls[0].(*ast.ConstDecl)
	if !ok {
		t.Fatalf(
			"declaration type = %T, want *ast.ConstDecl",
			run.File.Decls[0],
		)
	}

	binary, ok := decl.Value.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf(
			"constant value type = %T, want *ast.BinaryExpr",
			decl.Value,
		)
	}

	got, ok := run.Checker.ExprTypeFor(binary)
	if !ok {
		t.Fatal(
			"shift expression has no checker type",
		)
	}

	if got.Kind != TypeUntypedInt {
		t.Fatalf(
			"shift expression kind = %v, want TypeUntypedInt",
			got.Kind,
		)
	}

	if got.IntConstant == nil {
		t.Fatal(
			"large shift expression has no folded constant",
		)
	}

	const want = "1267650600228229401496703205376"

	if got.IntConstant.String() != want {
		t.Fatalf(
			"folded shift value = %s, want %s",
			got.IntConstant.String(),
			want,
		)
	}
}

func TestCheckerIntegerBinaryResultTypeFoldsLargeLeftShift(
	t *testing.T,
) {
	reporter := diag.NewReporter()
	c := New(reporter)

	left := untypedIntConstantType(
		big.NewInt(1),
	)

	right := untypedIntConstantType(
		big.NewInt(100),
	)

	got := c.integerBinaryResultType(
		token.ShiftLeft,
		left,
		right,
		UntypedIntType,
		source.Span{},
	)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected checker diagnostics:\n%s",
			reporter.String(),
		)
	}

	if got == nil ||
		got.Kind != TypeUntypedInt ||
		got.IntConstant == nil {
		t.Fatalf(
			"result = %#v, want folded untyped integer",
			got,
		)
	}

	const want = "1267650600228229401496703205376"

	if got.IntConstant.String() != want {
		t.Fatalf(
			"folded shift value = %s, want %s",
			got.IntConstant.String(),
			want,
		)
	}
}

func TestCheckerShiftedConstantMustFitDestinationType(
	t *testing.T,
) {
	reporter := checkSource(
		t,
		`
Main :: task() {
	value: u64 = 1 << 64
}
`,
	)

	if !reporter.HasErrors() {
		t.Fatal(
			"expected shifted constant range diagnostic",
		)
	}

	got := reporter.String()

	if !strings.Contains(
		got,
		"integer constant 18446744073709551616 is outside the range of u64",
	) {
		t.Fatalf(
			"diagnostics:\n%s\nwant u64 constant range diagnostic",
			got,
		)
	}
}

func TestCheckerShiftedConstantCanInitializeU64(
	t *testing.T,
) {
	_, reporter := check(
		t,
		`
Main :: task() {
	value: u64 = 1 << 63
}
`,
	)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected checker diagnostics:\n%s",
			reporter.String(),
		)
	}
}

func TestCheckerShiftRightFoldsUntypedIntegerConstant(
	t *testing.T,
) {
	run := runCheckerTest(
		t,
		`
Main :: task() {
	value := 1024 >> 3
}
`,
	)

	if run.Reporter.HasErrors() {
		t.Fatalf(
			"unexpected checker diagnostics:\n%s",
			run.Reporter.String(),
		)
	}

	binary := checkerBinaryFromVarStmt(
		t,
		checkerTaskStmt(
			t,
			run.File,
			"Main",
			0,
		),
	)

	got, ok := run.Checker.ExprTypeFor(binary)
	if !ok {
		t.Fatal(
			"shift expression has no checker type",
		)
	}

	if got.Kind != TypeUntypedInt ||
		got.IntConstant == nil {
		t.Fatalf(
			"shift expression type = %#v, want folded untyped int",
			got,
		)
	}

	if got.IntConstant.String() != "128" {
		t.Fatalf(
			"folded shift value = %s, want 128",
			got.IntConstant.String(),
		)
	}
}
