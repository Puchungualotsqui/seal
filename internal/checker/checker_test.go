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
    Health :: task(e *$T) int
}

Goblin :: struct {
    hp int
}

Health :: task(g *Goblin) int {
    return g.hp
}

Goblin :: impl {
    Enemy,
}

Main :: task() {
    g := Goblin{hp = 10}
    e: Enemy = &g
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
    Health :: task(e *$T) int
}

Goblin :: struct {
    hp int
}

Health :: task(g *Goblin) int {
    return g.hp
}

Goblin :: impl {
    Enemy,
}

Main :: task() {
    g := Goblin{hp = 10}
    e: Enemy = &g
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
    Health :: task(e *$T) int
}

Main :: task() {
    e: Enemy = nil
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectInterfaceAssignmentWithoutImpl(t *testing.T) {
	_, reporter := check(t, `
Enemy :: interface {
    Health :: task(e *$T) int
}

Goblin :: struct {
    hp int
}

Health :: task(g *Goblin) int {
    return g.hp
}

Main :: task() {
    g := Goblin{hp = 10}
    e: Enemy = &g
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "cannot assign *Goblin to interface Enemy") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestRejectImplMissingRequirement(t *testing.T) {
	_, reporter := check(t, `
Enemy :: interface {
    Health :: task(e *$T) int
}

Goblin :: struct {
    hp int
}

Goblin :: impl {
    Enemy,
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
    Health :: task(e *$T) int
}

Goblin :: struct {
    hp int
}

Health :: task(g *Goblin) int {
    return g.hp
}

Goblin :: impl {
    Enemy,
}

Main :: task() {
    g := Goblin{hp = 10}
    e: Enemy = &g
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
