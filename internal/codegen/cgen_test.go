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

	if !strings.Contains(out, "return (((v).x * (v).x) + ((v).y * (v).y));") {
		t.Fatalf("expected return expression, got:\n%s", out)
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

	if !strings.Contains(out, "return Error_None;") {
		t.Fatalf("expected enum return, got:\n%s", out)
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
