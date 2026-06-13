package lexer

import (
	"reflect"
	"testing"

	"seal/internal/diag"
	"seal/internal/source"
	"seal/internal/token"
)

func lexKinds(t *testing.T, input string) ([]token.Kind, *diag.Reporter) {
	t.Helper()

	file := source.NewFile("test.seal", input)
	reporter := diag.NewReporter()
	lex := New(file, reporter)

	tokens := lex.LexAll()

	kinds := make([]token.Kind, 0, len(tokens))
	for _, tok := range tokens {
		kinds = append(kinds, tok.Kind)
	}

	return kinds, reporter
}

func TestLexTaskDeclaration(t *testing.T) {
	kinds, reporter := lexKinds(t, `
Main :: task() int {
    return 0
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	want := []token.Kind{
		token.Ident,
		token.ColonColon,
		token.KeywordTask,
		token.LParen,
		token.RParen,
		token.Ident,
		token.LBrace,
		token.KeywordReturn,
		token.IntLit,
		token.RBrace,
		token.EOF,
	}

	assertKinds(t, kinds, want)
}

func TestLexPureTestTask(t *testing.T) {
	kinds, reporter := lexKinds(t, `
SwitchTest :: test task() {
    assert(true)
}

Length :: pure task(x f32) f32 {
    return x * x
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	want := []token.Kind{
		token.Ident,
		token.ColonColon,
		token.KeywordTest,
		token.KeywordTask,
		token.LParen,
		token.RParen,
		token.LBrace,
		token.Ident,
		token.LParen,
		token.KeywordTrue,
		token.RParen,
		token.RBrace,

		token.Ident,
		token.ColonColon,
		token.KeywordPure,
		token.KeywordTask,
		token.LParen,
		token.Ident,
		token.Ident,
		token.RParen,
		token.Ident,
		token.LBrace,
		token.KeywordReturn,
		token.Ident,
		token.Star,
		token.Ident,
		token.RBrace,
		token.EOF,
	}

	assertKinds(t, kinds, want)
}

func TestLexStructAndArray(t *testing.T) {
	kinds, reporter := lexKinds(t, `
Buffer :: struct($T, #N) {
    data [N]T
    len int
}

arr: []int = [2, 3, 4]
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	want := []token.Kind{
		token.Ident,
		token.ColonColon,
		token.KeywordStruct,
		token.LParen,
		token.Dollar,
		token.Ident,
		token.Comma,
		token.Hash,
		token.Ident,
		token.RParen,
		token.LBrace,
		token.Ident,
		token.LBracket,
		token.Ident,
		token.RBracket,
		token.Ident,
		token.Ident,
		token.Ident,
		token.RBrace,

		token.Ident,
		token.Colon,
		token.LBracket,
		token.RBracket,
		token.Ident,
		token.Assign,
		token.LBracket,
		token.IntLit,
		token.Comma,
		token.IntLit,
		token.Comma,
		token.IntLit,
		token.RBracket,
		token.EOF,
	}

	assertKinds(t, kinds, want)
}

func TestLexInterfaceAndImpl(t *testing.T) {
	kinds, reporter := lexKinds(t, `
Enemy :: interface {
    Damage :: task(e *$T, damage int)
}

Soldier :: impl {
    Enemy
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	want := []token.Kind{
		token.Ident,
		token.ColonColon,
		token.KeywordInterface,
		token.LBrace,
		token.Ident,
		token.ColonColon,
		token.KeywordTask,
		token.LParen,
		token.Ident,
		token.Star,
		token.Dollar,
		token.Ident,
		token.Comma,
		token.Ident,
		token.Ident,
		token.RParen,
		token.RBrace,

		token.Ident,
		token.ColonColon,
		token.KeywordImpl,
		token.LBrace,
		token.Ident,
		token.RBrace,
		token.EOF,
	}

	assertKinds(t, kinds, want)
}

func TestLexNumbers(t *testing.T) {
	kinds, reporter := lexKinds(t, `
a := 10
b := 10.5
c := 1.5e10
d := 1_000
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	want := []token.Kind{
		token.Ident, token.ColonEq, token.IntLit,
		token.Ident, token.ColonEq, token.FloatLit,
		token.Ident, token.ColonEq, token.FloatLit,
		token.Ident, token.ColonEq, token.IntLit,
		token.EOF,
	}

	assertKinds(t, kinds, want)
}

func TestLexString(t *testing.T) {
	kinds, reporter := lexKinds(t, `
name := "soldier\n"
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	want := []token.Kind{
		token.Ident,
		token.ColonEq,
		token.StringLit,
		token.EOF,
	}

	assertKinds(t, kinds, want)
}

func TestLexComments(t *testing.T) {
	kinds, reporter := lexKinds(t, `
// line comment
x := 1

/* block
   comment */

/* nested /* block */ comment */
y := 2
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	want := []token.Kind{
		token.Ident, token.ColonEq, token.IntLit,
		token.Ident, token.ColonEq, token.IntLit,
		token.EOF,
	}

	assertKinds(t, kinds, want)
}

func TestUnterminatedStringReportsDiagnostic(t *testing.T) {
	kinds, reporter := lexKinds(t, `x := "hello`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostic")
	}

	if kinds[2] != token.Invalid {
		t.Fatalf("expected invalid string token")
	}
}

func TestUnterminatedBlockCommentReportsDiagnostic(t *testing.T) {
	_, reporter := lexKinds(t, `x := 1 /* nope`)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostic")
	}
}

func TestLexOperators(t *testing.T) {
	kinds, reporter := lexKinds(t, `
a == b
a != b
a <= b
a >= b
a += b
a -= b
a *= b
a /= b
a %= b
a && b
a || b
...
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	want := []token.Kind{
		token.Ident, token.EqEq, token.Ident,
		token.Ident, token.NotEq, token.Ident,
		token.Ident, token.LtEq, token.Ident,
		token.Ident, token.GtEq, token.Ident,
		token.Ident, token.PlusEq, token.Ident,
		token.Ident, token.MinusEq, token.Ident,
		token.Ident, token.StarEq, token.Ident,
		token.Ident, token.SlashEq, token.Ident,
		token.Ident, token.PercentEq, token.Ident,
		token.Ident, token.AndAnd, token.Ident,
		token.Ident, token.OrOr, token.Ident,
		token.Ellipsis,
		token.EOF,
	}

	assertKinds(t, kinds, want)
}

func assertKinds(t *testing.T, got []token.Kind, want []token.Kind) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("token count mismatch\nwant %d: %v\ngot  %d: %v", len(want), want, len(got), got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("token %d: expected %s, got %s", i, want[i], got[i])
		}
	}
}

func TestStringCStringAndCharLiterals(t *testing.T) {
	tokens, reporter := lex(t, `
s := "hello"
cs := c"hello"
ch := 'ñ'
nl := '\n'
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	kinds := tokenKinds(tokens)

	want := []token.Kind{
		token.Ident, token.ColonEq, token.StringLit,
		token.Ident, token.ColonEq, token.CStringLit,
		token.Ident, token.ColonEq, token.CharLit,
		token.Ident, token.ColonEq, token.CharLit,
		token.EOF,
	}

	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("unexpected token kinds:\nwant=%v\ngot=%v", want, kinds)
	}
}

func lex(t *testing.T, input string) ([]token.Token, *diag.Reporter) {
	t.Helper()

	file := source.NewFile("test.seal", input)
	reporter := diag.NewReporter()

	l := New(file, reporter)
	tokens := l.LexAll()

	return tokens, reporter
}

func tokenKinds(tokens []token.Token) []token.Kind {
	kinds := make([]token.Kind, 0, len(tokens))
	for _, tok := range tokens {
		kinds = append(kinds, tok.Kind)
	}
	return kinds
}
