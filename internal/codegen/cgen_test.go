package cgen

import (
	"strings"
	"testing"

	"seal/internal/checker"
	"seal/internal/diag"
	"seal/internal/lexer"
	"seal/internal/parser"
	"seal/internal/resolver"
	"seal/internal/source"
)

func generate(t *testing.T, input string) (string, *diag.Reporter) {
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

	c := checker.New(reporter)
	c.CheckFile(parsed)

	if reporter.HasErrors() {
		t.Fatalf("checker diagnostics:\n%s", reporter.String())
	}

	g := New(reporter)
	out := g.Generate(parsed)

	return out, reporter
}

func TestGenerateSimpleMain(t *testing.T) {
	out, reporter := generate(t, `
Main :: task() {
    x := 10
    y := x + 20
    Print(y)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "int main(void)") {
		t.Fatalf("expected main function, got:\n%s", out)
	}

	if !strings.Contains(out, "int x = 10;") {
		t.Fatalf("expected x variable, got:\n%s", out)
	}

	if !strings.Contains(out, "int y = (x + 20);") {
		t.Fatalf("expected y variable, got:\n%s", out)
	}

	if !strings.Contains(out, `printf("%d", y);`) {
		t.Fatalf("expected printf, got:\n%s", out)
	}

	if !strings.Contains(out, "return 0;") {
		t.Fatalf("expected return 0, got:\n%s", out)
	}
}

func TestGenerateStructAndFunction(t *testing.T) {
	out, reporter := generate(t, `
Vec2 :: struct {
    x f32
    y f32
}

LengthSq :: task(v Vec2) f32 {
    return v.x * v.x + v.y * v.y
}

Main :: task() {
    v := Vec2{x = 2.0, y = 3.0}
    result := LengthSq(v)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "typedef struct Vec2") {
		t.Fatalf("expected Vec2 struct, got:\n%s", out)
	}

	if !strings.Contains(out, "float x;") {
		t.Fatalf("expected float field, got:\n%s", out)
	}

	if !strings.Contains(out, "float LengthSq(Vec2 v);") {
		t.Fatalf("expected function prototype, got:\n%s", out)
	}

	if !strings.Contains(out, "float __seal_return_value_") ||
		!strings.Contains(out, "= (((v).x * (v).x) + ((v).y * (v).y));") ||
		!strings.Contains(out, "return __seal_return_value_") {
		t.Fatalf("expected return temp expression, got:\n%s", out)
	}
}

func TestGeneratePointerFieldAutoDeref(t *testing.T) {
	out, reporter := generate(t, `
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

	if !strings.Contains(out, "void Damage(Soldier * e, int amount)") {
		t.Fatalf("expected Damage signature, got:\n%s", out)
	}

	if !strings.Contains(out, "(e)->health = ((e)->health - amount);") {
		t.Fatalf("expected pointer field assignment, got:\n%s", out)
	}
}

func TestGenerateEnum(t *testing.T) {
	out, reporter := generate(t, `
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

	if !strings.Contains(out, "typedef enum Error") {
		t.Fatalf("expected enum, got:\n%s", out)
	}

	if !strings.Contains(out, "Error_None") {
		t.Fatalf("expected enum variant, got:\n%s", out)
	}

	if !strings.Contains(out, "Error __seal_return_value_") ||
		!strings.Contains(out, "= Error_None;") ||
		!strings.Contains(out, "return __seal_return_value_") {
		t.Fatalf("expected enum return temp, got:\n%s", out)
	}
}

func TestGenerateIfForAndArray(t *testing.T) {
	out, reporter := generate(t, `
Main :: task() {
    values: [?]int = [1, 2, 3]

    sum := 0

    for i := 0; i < 3; i = i + 1 {
        sum = sum + values[i]
    }

    if sum > 0 {
        Print(sum)
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "int values[3] = {1, 2, 3};") {
		t.Fatalf("expected array init, got:\n%s", out)
	}

	if !strings.Contains(out, "for (int i = 0; (i < 3); i = (i + 1))") {
		t.Fatalf("expected C-like for, got:\n%s", out)
	}

	if !strings.Contains(out, "if ((sum > 0))") {
		t.Fatalf("expected if statement, got:\n%s", out)
	}
}

func TestGenerateDefer(t *testing.T) {
	out, reporter := generate(t, `
Close :: task(x int) {
    Print(x)
}

Main :: task() {
    x := 1
    defer Close(x)
    x = 2
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "int __seal_defer_arg_") {
		t.Fatalf("expected defer temp arg, got:\n%s", out)
	}

	if !strings.Contains(out, "Close(__seal_defer_arg_") {
		t.Fatalf("expected deferred Close call, got:\n%s", out)
	}

	if strings.Index(out, "x = 2;") > strings.Index(out, "Close(__seal_defer_arg_") {
		t.Fatalf("expected defer to run after assignment, got:\n%s", out)
	}
}

func TestGenerateOverloadCall(t *testing.T) {
	out, reporter := generate(t, `
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

	if !strings.Contains(out, "int a = SumInt(1, 2);") {
		t.Fatalf("expected SumInt call, got:\n%s", out)
	}

	if !strings.Contains(out, "double b = SumF64(1.0, 2.0);") {
		t.Fatalf("expected SumF64 call, got:\n%s", out)
	}
}

func TestGenerateOperatorOverload(t *testing.T) {
	out, reporter := generate(t, `
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

	if !strings.Contains(out, "Vec2 c = Vec2Add(a, b);") {
		t.Fatalf("expected operator overload call, got:\n%s", out)
	}
}

func TestGenerateUnionAssignment(t *testing.T) {
	out, reporter := generate(t, `
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
    s = nil
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "typedef struct Shape") {
		t.Fatalf("expected Shape struct, got:\n%s", out)
	}

	if !strings.Contains(out, ".tag = Shape_Tag_Circle") {
		t.Fatalf("expected Circle tagged union literal, got:\n%s", out)
	}

	if !strings.Contains(out, "s = (Shape){.tag = Shape_Tag_nil};") {
		t.Fatalf("expected nil union assignment, got:\n%s", out)
	}
}

func TestGenerateUnionSwitch(t *testing.T) {
	out, reporter := generate(t, `
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

	if !strings.Contains(out, "switch (__seal_union_switch_") {
		t.Fatalf("expected union switch temp, got:\n%s", out)
	}

	if !strings.Contains(out, "case Shape_Tag_Circle:") {
		t.Fatalf("expected Circle case, got:\n%s", out)
	}

	if !strings.Contains(out, "Circle shape = __seal_union_switch_") {
		t.Fatalf("expected narrowed Circle binding, got:\n%s", out)
	}
}
