package checker

import (
	"strings"
	"testing"

	"seal/internal/diag"
	"seal/internal/lexer"
	"seal/internal/parser"
	"seal/internal/resolver"
	"seal/internal/source"
)

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
        Assert(true)
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
    arr: [?]int = [1, 2, 3]
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectArrayLiteralElementTypes(t *testing.T) {
	_, reporter := check(t, `
Main :: task() {
    arr: [?]int = [1, true, 3]
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
