package resolver

import (
	"strings"
	"testing"

	"seal/internal/ast"
	"seal/internal/diag"
	"seal/internal/lexer"
	"seal/internal/parser"
	"seal/internal/source"
)

func resolve(t *testing.T, input string) (*Scope, *diag.Reporter) {
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

	r := New(reporter)
	scope := r.ResolveFile(parsed)

	return scope, reporter
}

func TestResolveGlobalSymbols(t *testing.T) {
	scope, reporter := resolve(t, `
MAX_COUNT :: 1024

Vec2 :: struct {
    x f32
    y f32
}

Main :: task() {
    return
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if scope.LookupLocal("MAX_COUNT") == nil {
		t.Fatalf("expected MAX_COUNT")
	}

	if scope.LookupLocal("Vec2") == nil {
		t.Fatalf("expected Vec2")
	}

	if scope.LookupLocal("Main") == nil {
		t.Fatalf("expected Main")
	}
}

func TestLocalTaskNotVisibleBeforeDeclaration(t *testing.T) {
	_, reporter := resolve(t, `
OuterTask :: task() {
    InnerTask()

    InnerTask :: task() {
    }
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `undefined symbol "InnerTask"`) {
		t.Fatalf("expected undefined InnerTask diagnostic, got:\n%s", reporter.String())
	}
}

func TestLocalTaskVisibleAfterDeclarationInChildBlock(t *testing.T) {
	_, reporter := resolve(t, `
OuterTask :: task() {
    InnerTask :: task() {
    }

    {
        InnerTask()
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestLocalTaskNotVisibleOutsideParentScope(t *testing.T) {
	_, reporter := resolve(t, `
OuterTask :: task() {
    InnerTask :: task() {
    }
}

SecondTask :: task() {
    InnerTask()
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `undefined symbol "InnerTask"`) {
		t.Fatalf("expected undefined InnerTask diagnostic, got:\n%s", reporter.String())
	}
}

func TestNestedTaskCannotCaptureRuntimeVariable(t *testing.T) {
	_, reporter := resolve(t, `
SomeThing :: task(x int) {
}

OuterTask :: task() {
    outerVar := 20

    InnerTask :: task() {
        SomeThing(outerVar)
    }
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `nested task cannot capture runtime symbol "outerVar"`) {
		t.Fatalf("expected runtime capture diagnostic, got:\n%s", reporter.String())
	}
}

func TestNestedTaskCanUseOuterConstant(t *testing.T) {
	_, reporter := resolve(t, `
SomeThing :: task(x int) {
}

OuterTask :: task() {
    OUTER_CONST :: 10

    InnerTask :: task() {
        SomeThing(OUTER_CONST)
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestParameterCannotShadowOuterVariable(t *testing.T) {
	_, reporter := resolve(t, `
OuterTask :: task() {
    outerVar := 20

    SomeTask :: task(outerVar int) {
    }
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `declaration of "outerVar" shadows visible variable`) {
		t.Fatalf("expected shadowing diagnostic, got:\n%s", reporter.String())
	}
}

func TestInnerBlockCannotShadowOuterConst(t *testing.T) {
	_, reporter := resolve(t, `
OuterTask :: task() {
    OUTER_CONST :: 10

    {
        OUTER_CONST :: 20
    }
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `declaration of "OUTER_CONST" shadows visible constant`) {
		t.Fatalf("expected const shadowing diagnostic, got:\n%s", reporter.String())
	}
}

func TestSiblingScopesCanReuseName(t *testing.T) {
	_, reporter := resolve(t, `
FirstTask :: task() {
    LOCAL_CONST :: 10
}

SecondTask :: task() {
    LOCAL_CONST :: 20
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestCannotShadowBuiltinType(t *testing.T) {
	_, reporter := resolve(t, `
int :: struct {
    value int
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `declaration of "int" shadows visible builtin type`) {
		t.Fatalf("expected builtin shadowing diagnostic, got:\n%s", reporter.String())
	}
}

func TestCImportDirectiveDoesNotDeclareVisibleSymbol(t *testing.T) {
	_, reporter := resolve(t, `
c :: @c_import {
    include "stdlib.h"
}

Main :: task() {
    c
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `undefined symbol "c"`) {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestImplMustBeInTypeDefiningScope(t *testing.T) {
	_, reporter := resolve(t, `
Enemy :: interface {
    Damage :: task(e *$T, amount int)
}

Soldier :: struct {
    health int
}

Main :: task() {
    Soldier :: impl {
        Enemy
    }
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `impl for "Soldier" must be declared in the type's defining scope`) {
		t.Fatalf("expected impl scope diagnostic, got:\n%s", reporter.String())
	}
}

func TestLocalInterfaceAndLocalTypeImplIsValid(t *testing.T) {
	_, reporter := resolve(t, `
Main :: task() {
    Enemy :: interface {
        Damage :: task(e *$T, amount int)
    }

    Soldier :: struct {
        health int
    }

    Damage :: task(e *Soldier, amount int) {
        e.health = e.health - amount
    }

    Soldier :: impl {
        Enemy
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestDuplicateEnumVariant(t *testing.T) {
	_, reporter := resolve(t, `
Direction :: enum {
    North
    South
    North
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `duplicate enum variant "North"`) {
		t.Fatalf("expected duplicate enum variant diagnostic, got:\n%s", reporter.String())
	}
}

func TestResolverDebugSummary(t *testing.T) {
	parsed := &ast.File{}
	reporter := diag.NewReporter()
	r := New(reporter)

	scope := r.ResolveFile(parsed)
	out := DebugSummary(scope)

	if !strings.Contains(out, "global_symbols=0") {
		t.Fatalf("unexpected summary: %s", out)
	}
}

func TestResolveSpreadCallArgument(t *testing.T) {
	_, reporter := resolve(t, `
Sum :: task(values ...int) int {
    return 0
}

Main :: task() {
    a: []int = [1, 2, 3]
    Sum(a...)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestResolveSpreadReportsUndefinedInnerSymbol(t *testing.T) {
	_, reporter := resolve(t, `
Sum :: task(values ...int) int {
    return 0
}

Main :: task() {
    Sum(missing...)
}
`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), `undefined symbol "missing"`) {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}
