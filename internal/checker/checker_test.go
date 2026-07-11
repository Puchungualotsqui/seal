package checker

import (
	"strings"
	"testing"

	"seal/internal/ast"
	"seal/internal/diag"
	"seal/internal/lexer"
	"seal/internal/parser"
	"seal/internal/resolver"
	"seal/internal/source"
)

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

func TestArrayLiteralElementTypes(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    arr: []int = [1, 2, 3]
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectArrayLiteralElementTypes(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    arr: []int = [1, true, 3]
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot assign bool to int") {
		t.Fatalf("expected array element diagnostic, got:\n%s", reporter.String())
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

func TestVariadicArrayOfAnyIsValid(t *testing.T) {
	_, reporter := check(t, `
TakeArrays :: task(args ...[10]any) uint {
    return len(args)
}

Main :: task() {
    a: [10]any = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    b: [10]any = ["a", "b", "c", "d", "e", "f", "g", "h", "i", "j"]

    count := TakeArrays(a, b)
    assert(count == 2)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestInferredArrayOfAnyIsValid(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    anyArr: []any = [2, 3, 4, 5, 6]
    n: uint = len(anyArr)
    assert(n == 5)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestInferredArrayOfMixedAnyIsValid(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    values: []any = [2, "hello", 3.14, true]
    n: uint = len(values)
    assert(n == 4)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestArrayOfInterfaceValuesIsValid(t *testing.T) {
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
    enemies: [1]Enemy = [e]
}
`)

	if reporter.HasErrors() {
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

func TestStringCStringAndCharTypes(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    c: char = 'ñ'
    s: string = "hola"
    cs: cstring = c"hola"

    n: uint = size(s)
    h: char = s[0]
    o: char = s[1]
    a: char = s[-1]

    assert(n == 4)
    assert(h == 'h')
    assert(o == 'o')
    assert(a == 'a')
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
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

func TestStringLenAndIndexing(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    c: char = 'ñ'
    s: string = "hola"
    cs: cstring = c"hola"

    n: uint = size(s)
    h: char = s[0]
    o: char = s[1]
    a: char = s[-1]

    assert(c == 'ñ')
    assert(n == 4)
    assert(h == 'h')
    assert(o == 'o')
    assert(a == 'a')
}
`)

	if reporter.HasErrors() {
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

	if !strings.Contains(reporter.String(), `string has no field "len"`) {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCStringIndexingIsInvalid(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    cs := c"hola"
    c := cs[0]
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cstring does not support character indexing") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestStringIndexAssignmentIsInvalid(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    s := "hola"
    s[0] = 'H'
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot assign to string index because strings are immutable") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestSpreadArrayIntoVariadicTask(t *testing.T) {
	_, reporter := check(t, `
Sum :: task(values ...int) int {
    total := 0

    for i := 0; i < len(values); i = i + 1 {
        total = total + values[i]
    }

    return total
}

Main :: task() {
    a: []int = [1, 2, 3]
    result := Sum(a...)
    assert(result == 6)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
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

func TestSpreadArrayIntoVariadicWithFixedParameter(t *testing.T) {
	_, reporter := check(t, `
Example :: task(prefix int, values ...int) int {
    total := prefix

    for i := 0; i < len(values); i = i + 1 {
        total = total + values[i]
    }

    return total
}

Main :: task() {
    a: []int = [1, 2, 3]
    result := Example(10, a...)
    assert(result == 16)
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

Main :: task() {
    a: []int = [1, 2, 3]
    Sum(a..., 4)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "spread argument must be the last argument") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectSpreadNonArrayNonVariadic(t *testing.T) {
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

	if !strings.Contains(reporter.String(), "cannot spread int; expected array or variadic value") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectSpreadIntoNonVariadicTask(t *testing.T) {
	_, reporter := check(t, `
Use :: task(a int, b int) int {
    return a + b
}

Main :: task() {
    values: []int = [1, 2]
    result := Use(values...)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot spread variadic argument into non-variadic task") &&
		!strings.Contains(reporter.String(), "task call argument count mismatch") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectSpreadMismatchedElementType(t *testing.T) {
	_, reporter := check(t, `
Sum :: task(values ...int) int {
    return 0
}

Main :: task() {
    values: []string = ["a", "b"]
    result := Sum(values...)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot assign string to int") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
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
    (10)[0] = 1
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "byte-index assignment requires an addressable value") {
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

func TestRejectLenString(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    s := "hello"
    n := len(s)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "len does not support string") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
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
    data [N]T
}

Main :: task() {
    b: Buffer<int, 32>
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
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
    data [N]T
}

Main :: task() {
    b: Buffer<int, 3> = Buffer<int, 3>{
        data = [1, 2, 3],
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
    data [N]T
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
    data [N]T
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
    data [N]T
}
`)

	reporter := checkWithPackages(t, `
Main :: task() {
    b: types.Buffer<int, 4>
    x := b.data[0]
    assert(x == 0)
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

func TestCheckImportedGenericStructValueParamRejectsWrongIndexUse(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "types", `
Buffer :: struct <T type, N int> {
    data [N]T
}
`)

	reporter := checkWithPackages(t, `
Main :: task() {
    b: types.Buffer<string, 4>
    x: int = b.data[0]
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

func TestCheckImportedGenericStructValueParamArrayLength(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "types", `
Buffer :: struct <T type, N int> {
    data [N]T
}
`)

	reporter := checkWithPackages(t, `
TakeFour :: task(values [4]int) {
}

Main :: task() {
    b: types.Buffer<int, 4>
    TakeFour(b.data)
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

func TestCheckImportedGenericStructValueParamRejectsArrayLengthMismatch(t *testing.T) {
	_, resolverPkg, checkerPkg := exportCheckerPackage(t, "types", `
Buffer :: struct <T type, N int> {
    data [N]T
}
`)

	reporter := checkWithPackages(t, `
TakeFive :: task(values [5]int) {
}

Main :: task() {
    b: types.Buffer<int, 4>
    TakeFive(b.data)
}
`, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*PackageInfo{
		"types": checkerPkg,
	})

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot assign [4]int to [5]int") {
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
