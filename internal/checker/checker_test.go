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
