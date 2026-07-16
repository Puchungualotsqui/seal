package cgen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"seal/internal/ast"
	"seal/internal/checker"
	"seal/internal/diag"
	"seal/internal/lexer"
	"seal/internal/parser"
	"seal/internal/resolver"
	"seal/internal/source"
)

func compileGeneratedC(t *testing.T, generated string) {
	t.Helper()

	var compiler string

	for _, candidate := range []string{"cc", "clang", "gcc"} {
		path, err := exec.LookPath(candidate)
		if err == nil {
			compiler = path
			break
		}
	}

	if compiler == "" {
		t.Skip("no C compiler found; install cc, clang, or gcc to run generated-C compilation tests")
	}

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "generated.c")
	objectPath := filepath.Join(tempDir, "generated.o")

	if err := os.WriteFile(sourcePath, []byte(generated), 0o600); err != nil {
		t.Fatalf("failed to write generated C source: %v", err)
	}

	cmd := exec.Command(
		compiler,
		"-std=c11",
		"-c",
		sourcePath,
		"-o",
		objectPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf(
			"generated C failed to compile with %s:\n%s\n\ngenerated C:\n%s",
			compiler,
			string(output),
			generated,
		)
	}

	if _, err := os.Stat(objectPath); err != nil {
		t.Fatalf("C compiler reported success but object file was not created: %v", err)
	}
}

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

	g := NewWithPackagesAndSemanticInfo(
		reporter,
		"",
		nil,
		c.SemanticInfo(),
	)
	out := g.Generate(parsed)

	return out, reporter
}

func runGeneratedC(t *testing.T, generated string) {
	t.Helper()

	var compiler string

	for _, candidate := range []string{"cc", "clang", "gcc"} {
		path, err := exec.LookPath(candidate)
		if err == nil {
			compiler = path
			break
		}
	}

	if compiler == "" {
		t.Skip(
			"no C compiler found; install cc, clang, or gcc to run generated-C execution tests",
		)
	}

	tempDir := t.TempDir()
	sourcePath := filepath.Join(
		tempDir,
		"generated.c",
	)

	executableName := "generated"
	if runtime.GOOS == "windows" {
		executableName += ".exe"
	}

	executablePath := filepath.Join(
		tempDir,
		executableName,
	)

	if err := os.WriteFile(
		sourcePath,
		[]byte(generated),
		0o600,
	); err != nil {
		t.Fatalf(
			"failed to write generated C source: %v",
			err,
		)
	}

	compile := exec.Command(
		compiler,
		"-std=c11",
		sourcePath,
		"-o",
		executablePath,
	)

	output, err := compile.CombinedOutput()
	if err != nil {
		t.Fatalf(
			"generated C failed to compile with %s:\n%s\n\ngenerated C:\n%s",
			compiler,
			string(output),
			generated,
		)
	}

	run := exec.Command(executablePath)

	output, err = run.CombinedOutput()
	if err != nil {
		t.Fatalf(
			"generated executable failed:\n%s\n\ngenerated C:\n%s",
			string(output),
			generated,
		)
	}
}

func TestGenerateSimpleMain(t *testing.T) {
	out, reporter := generate(t, `
Main :: task() {
    x := 10
    y := x + 20
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "int main(void)") {
		t.Fatalf("expected main function, got:\n%s", out)
	}

	if !strings.Contains(out, "intptr_t x = 10;") {
		t.Fatalf("expected x variable, got:\n%s", out)
	}

	if !strings.Contains(out, "intptr_t y = (x + 20);") {
		t.Fatalf("expected y variable, got:\n%s", out)
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

	if !strings.Contains(out, "void Damage(Soldier * e, intptr_t amount)") {
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

func TestGenerateDefer(t *testing.T) {
	out, reporter := generate(t, `
Close :: task(x int) {
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

	if !strings.Contains(out, "intptr_t __seal_defer_arg_") {
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

	if !strings.Contains(out, "intptr_t a = SumInt(1, 2);") {
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

func TestGenerateExternMallocFree(t *testing.T) {
	out, reporter := generate(t, `
c :: @c_import {
    include "stdlib.h"
}

malloc :: extern("malloc") task(size uint) rawptr
free :: extern("free") task(ptr rawptr)

Main :: task() {
    ptr := malloc(64)
    free(ptr)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		`#include "stdlib.h"`,
	) {
		t.Fatalf(
			"expected stdlib include, got:\n%s",
			out,
		)
	}

	// stdlib.h is authoritative for malloc and free. CGen must not emit
	// Seal-derived declarations that could conflict with the platform ABI.
	if strings.Contains(
		out,
		"void * malloc(uintptr_t size);",
	) {
		t.Fatalf(
			"header-backed extern malloc must not be redeclared, got:\n%s",
			out,
		)
	}

	if strings.Contains(
		out,
		"void free(void * ptr);",
	) {
		t.Fatalf(
			"header-backed extern free must not be redeclared, got:\n%s",
			out,
		)
	}

	if strings.Contains(
		out,
		"void * malloc(uintptr_t size) {",
	) {
		t.Fatalf(
			"extern malloc must not emit a function body, got:\n%s",
			out,
		)
	}

	if strings.Contains(
		out,
		"void free(void * ptr) {",
	) {
		t.Fatalf(
			"extern free must not emit a function body, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"void * ptr = malloc(64);",
	) {
		t.Fatalf(
			"expected malloc call, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"free(ptr);",
	) {
		t.Fatalf(
			"expected free call, got:\n%s",
			out,
		)
	}

	compileGeneratedC(t, out)
}

func TestGenerateExternVariadicPrintf(t *testing.T) {
	out, reporter := generate(t, `
c :: @c_import {
    include "stdio.h"
}

printf :: extern("printf") task(format cstring, args ...any) int

Main :: task() {
    printf(c"%d %s", 10, c"hello")
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		`#include "stdio.h"`,
	) {
		t.Fatalf(
			"expected stdio include, got:\n%s",
			out,
		)
	}

	// stdio.h declares printf using the platform C ABI. Seal's `int` lowers
	// to intptr_t, which is not necessarily C's int, so emitting this
	// redeclaration would be incorrect.
	if strings.Contains(
		out,
		"intptr_t printf(const char * format, ...);",
	) {
		t.Fatalf(
			"header-backed extern printf must not be redeclared, got:\n%s",
			out,
		)
	}

	if strings.Contains(
		out,
		"intptr_t printf(const char * format, ...) {",
	) {
		t.Fatalf(
			"extern printf must not emit a function body, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		`printf("%d %s", 10, "hello");`,
	) {
		t.Fatalf(
			"expected printf call, got:\n%s",
			out,
		)
	}

	compileGeneratedC(t, out)
}

func TestGenerateSealVariadicInt(t *testing.T) {
	out, reporter := generate(t, `
Sum :: task(args ...int) int {
    total := 0

    for i := 0; i < len(args); i = i + 1 {
        total = total + args[i]
    }

    return total
}

Main :: task() {
    result := Sum(1, 2, 3)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "typedef struct sealVariadic_int") {
		t.Fatalf("expected variadic int runtime type, got:\n%s", out)
	}

	if !strings.Contains(out, "intptr_t Sum(sealVariadic_int args)") {
		t.Fatalf("expected Sum variadic signature, got:\n%s", out)
	}

	if !strings.Contains(out, "Sum((sealVariadic_int){.data = (intptr_t[]){1, 2, 3}, .len = 3})") {
		t.Fatalf("expected packed Sum call, got:\n%s", out)
	}

	if !strings.Contains(out, "(args).data[i]") {
		t.Fatalf("expected args index lowering, got:\n%s", out)
	}

	if !strings.Contains(out, "((uintptr_t)(args).len)") {
		t.Fatalf("expected len(args) lowering, got:\n%s", out)
	}
}

func TestGenerateSealVariadicAny(t *testing.T) {
	out, reporter := generate(t, `
CountAny :: task(args ...any) uint {
    return len(args)
}

Main :: task() {
    x: any = 10
    result := CountAny(x, "hello", 3.14)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"typedef struct sealAny",
	) {
		t.Fatalf(
			"expected sealAny runtime, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"sealAny x = sealAny_int(10);",
	) {
		t.Fatalf(
			"expected any boxing, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"uintptr_t CountAny(sealVariadic_any args)",
	) {
		t.Fatalf(
			"expected CountAny variadic any signature, got:\n%s",
			out,
		)
	}

	expectedString := `sealAny_string((sealString){.data = (const uint8_t *)"hello", .len = (uintptr_t)5})`

	if !strings.Contains(out, expectedString) {
		t.Fatalf(
			"expected string boxing %q, got:\n%s",
			expectedString,
			out,
		)
	}

	if !strings.Contains(
		out,
		"sealAny_f64(3.14)",
	) {
		t.Fatalf(
			"expected f64 boxing, got:\n%s",
			out,
		)
	}
}

func TestGenerateAnyAsAnyIsIntrinsics(t *testing.T) {
	out, reporter := generate(t, `
Main :: task() {
    value: any = 10

    if anyIs<int>(value) {
        x := anyAs<int>(value)
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "sealAny value = sealAny_int(10);") {
		t.Fatalf("expected any boxing, got:\n%s", out)
	}

	if !strings.Contains(out, "if (((value).type == sealType_int))") {
		t.Fatalf("expected anyIs<int> lowering, got:\n%s", out)
	}

	if !strings.Contains(out, "intptr_t x = ((value).value.as_int);") {
		t.Fatalf("expected anyAs<int> lowering, got:\n%s", out)
	}
}

func TestGeneratePartialAnyTypeSwitch(t *testing.T) {
	out, reporter := generate(t, `
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

	if !strings.Contains(out, "switch ((value).type)") {
		t.Fatalf("expected any type switch, got:\n%s", out)
	}

	if !strings.Contains(out, "case sealType_int:") {
		t.Fatalf("expected int case, got:\n%s", out)
	}

	if !strings.Contains(out, "case sealType_string:") {
		t.Fatalf("expected string case, got:\n%s", out)
	}

	if !strings.Contains(out, "intptr_t x = ((value).value.as_int);") {
		t.Fatalf("expected anyAs<int>, got:\n%s", out)
	}

	if !strings.Contains(out, "sealString s = ((value).value.as_string);") {
		t.Fatalf("expected anyAs<string>, got:\n%s", out)
	}
}

func TestGenerateNonPartialAnyTypeSwitchWithDefault(t *testing.T) {
	out, reporter := generate(t, `
Main :: task() {
    value: any = "hello"

    switch value type {
    case int:
        x := anyAs<int>(value)

    default:
        y := 0
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "default:") {
		t.Fatalf("expected default case, got:\n%s", out)
	}
}

func TestGenerateStringCStringAndChar(t *testing.T) {
	out, reporter := generate(t, `
Main :: task() {
    c: char = 'ñ'
    s: string = "hola"
    cs: cstring = c"world"

    stringBytes: uint = size(s)
    stringChars: uint = len(s)

    cstringBytes: uint = size(cs)
    cstringChars: uint = len(cs)

    stringFirst: char = s[0]
    cstringLast: char = cs[-1]

    sameString := s == "hola"
    sameCString := cs == c"world"
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	checks := []string{
		"typedef struct sealString {",
		"const uint8_t *data;",
		"uintptr_t len;",
		"} sealString;",

		"uint32_t c = ((uint32_t)241);",

		`sealString s = (sealString){.data = (const uint8_t *)"hola", .len = (uintptr_t)4};`,
		`const char * cs = "world";`,

		"uintptr_t stringBytes = (uintptr_t)(s).len;",
		"uintptr_t stringChars = seal_string_scalar_len(s);",

		"uintptr_t cstringBytes = seal_cstring_byte_len(cs);",
		"uintptr_t cstringChars = seal_cstring_scalar_len(cs);",

		"uint32_t stringFirst = seal_string_index(s, 0);",
		"uint32_t cstringLast = seal_cstring_index(cs, (-1));",

		`bool sameString = seal_string_equal(s, (sealString){.data = (const uint8_t *)"hola", .len = (uintptr_t)4});`,
		`bool sameCString = seal_cstring_equal(cs, "world");`,
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}

	if strings.Contains(out, ".byte_len") {
		t.Fatalf(
			"generated C still uses obsolete byte_len field:\n%s",
			out,
		)
	}

	compileGeneratedC(t, out)
}

func TestGenerateUTF8LiteralByteEncoding(t *testing.T) {
	out, reporter := generate(t, `
Main :: task() {
    s: string = "ñ🙂"
    cs: cstring = c"ñ🙂"
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	expectedString :=
		`sealString s = (sealString){.data = (const uint8_t *)"\303\261\360\237\231\202", .len = (uintptr_t)6};`

	if !strings.Contains(out, expectedString) {
		t.Fatalf(
			"expected UTF-8 string bytes %q, got:\n%s",
			expectedString,
			out,
		)
	}

	expectedCString :=
		`const char * cs = "\303\261\360\237\231\202";`

	if !strings.Contains(out, expectedCString) {
		t.Fatalf(
			"expected UTF-8 cstring bytes %q, got:\n%s",
			expectedCString,
			out,
		)
	}

	compileGeneratedC(t, out)
}

func TestGenerateBorrowedStringAndCStringCasts(
	t *testing.T,
) {
	out, reporter := generate(t, `
Main :: task() {
    source := "hello"
    data := cast<rawptr>(source)
    view := cast<string>(data, size(source))

    csource := c"world"
    cdata := cast<rawptr>(csource)
    cview := cast<cstring>(cdata, size(csource))

    assert(view == source)
    assert(cview == csource)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	checks := []string{
		"void * data = ((void *)((source).data));",

		"sealString view = (sealString){.data = (const uint8_t *)(data),",

		".len = (uintptr_t)((uintptr_t)(source).len)",

		"void * cdata = ((void *)(csource));",

		"const char * cview = seal_cstring_from_parts(",

		"(const char *)(cdata)",

		"(uintptr_t)(seal_cstring_byte_len(csource))",

		"seal_string_equal(view, source)",

		"seal_cstring_equal(cview, csource)",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}

	if strings.Contains(out, "malloc(") {
		t.Fatalf(
			"borrowed casts must not allocate:\n%s",
			out,
		)
	}

	if strings.Contains(out, "free(") {
		t.Fatalf(
			"borrowed casts must not free memory:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGenerateCStringExternABI(t *testing.T) {
	out, reporter := generate(t, `
SetWindowTitle :: extern("seal_test_set_window_title") task(
    title cstring,
)

GetError :: extern("seal_test_get_error") task() cstring

Main :: task() {
    SetWindowTitle(c"Seal")
    message := GetError()
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	checks := []string{
		"void seal_test_set_window_title(const char * title);",

		"const char * seal_test_get_error(void);",

		`seal_test_set_window_title("Seal");`,

		"const char * message = seal_test_get_error();",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}

	compileGeneratedC(t, out)
}

func TestRunGeneratedUnicodeStringOperations(
	t *testing.T,
) {
	out, reporter := generate(t, `
Main :: task() {
    text := "Año🙂"
    ctext := c"Año🙂"

    assert(size(text) == 8)
    assert(len(text) == 4)

    assert(size(ctext) == 8)
    assert(len(ctext) == 4)

    assert(text[0] == 'A')
    assert(text[1] == 'ñ')
    assert(text[-1] == '🙂')

    assert(ctext[0] == 'A')
    assert(ctext[1] == 'ñ')
    assert(ctext[-1] == '🙂')

    assert(text == "Año🙂")
    assert(text != "Otro")

    assert(ctext == c"Año🙂")
    assert(ctext != c"Otro")
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	checks := []string{
		"seal_string_scalar_len(text)",
		"seal_cstring_scalar_len(ctext)",

		"seal_string_index(text, 0)",
		"seal_string_index(text, 1)",
		"seal_string_index(text, (-1))",

		"seal_cstring_index(ctext, 0)",
		"seal_cstring_index(ctext, 1)",
		"seal_cstring_index(ctext, (-1))",

		"seal_string_equal(",
		"seal_cstring_equal(",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}

	runGeneratedC(t, out)
}

func TestGenerateForwardVariadicIntoVariadicTask(t *testing.T) {
	out, reporter := generate(t, `
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
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "intptr_t Forward(sealVariadic_int values)") {
		t.Fatalf("expected Forward variadic signature, got:\n%s", out)
	}

	if !strings.Contains(out, "Sum(values)") {
		t.Fatalf("expected variadic forwarding without repacking, got:\n%s", out)
	}
}

func TestGeneratePackageQualifiedSpreadCall(t *testing.T) {
	out, reporter := generate(t, `
Forward :: task(values ...any) {
    PrintLike("values = %", values...)
}

PrintLike :: task(format string, args ...any) {
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "PrintLike(") {
		t.Fatalf("expected PrintLike call, got:\n%s", out)
	}

	if !strings.Contains(out, "values)") {
		t.Fatalf("expected variadic forwarding to pass values directly, got:\n%s", out)
	}
}

func TestGenerateMultipleReturnTask(t *testing.T) {
	out, reporter := generate(t, `
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

	if !strings.Contains(out, "typedef struct Foo_Result") {
		t.Fatalf("expected Foo_Result struct, got:\n%s", out)
	}

	if !strings.Contains(out, "Foo_Result Foo(void)") {
		t.Fatalf("expected Foo result signature, got:\n%s", out)
	}

	if !strings.Contains(out, "Foo_Result __seal_multi_result") {
		t.Fatalf("expected multi-result temporary, got:\n%s", out)
	}

	if !strings.Contains(out, "void * a = __seal_multi_result") {
		t.Fatalf("expected first result extraction, got:\n%s", out)
	}

	if !strings.Contains(out, "void * b = __seal_multi_result") {
		t.Fatalf("expected second result extraction, got:\n%s", out)
	}
}

func TestGenerateMultipleReturnWithBlankIdentifier(t *testing.T) {
	out, reporter := generate(t, `
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

	if strings.Contains(out, "void * _ =") {
		t.Fatalf("blank identifier should not emit a variable, got:\n%s", out)
	}

	if !strings.Contains(out, "void * b = __seal_multi_result") {
		t.Fatalf("expected second result extraction, got:\n%s", out)
	}
}

func TestGenerateInterfaceAssignmentAndDispatch(t *testing.T) {
	out, reporter := generate(t, `
Enemy :: interface <Value type> {
    Health :: task(e *self) Value
}

Goblin :: struct {
    hp int
}


    GoblinHealth :: task(g *Goblin) int {

    return g.hp
}

Enemy<int> :: impl Goblin {

    Health :: GoblinHealth

}

Main :: task() {
    g := Goblin{hp = 10}


    e := cast<Enemy<int>>(&g)


    hp := Health(e)
    assert(hp == 10)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct Enemy_int {",
		"uintptr_t tag;",
		"void *data;",
		"Enemy_int_Tag_Goblin",
		"Enemy_int_Goblin_Health",
		".tag = Enemy_int_Tag_Goblin",
		".data = (void *)",
		"Enemy_int_Health(e)",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}
}

func TestGeneratePositionedDelegation(t *testing.T) {
	out, reporter := generate(t, `
Positioned :: interface {
    Position :: task(value *self) int
}

Transform :: struct {
    x int
}

ReadPosition :: task(transform *Transform) int {
    return transform.x
}

Positioned :: impl Transform {
    Position :: ReadPosition
}

Entity :: struct {
    transform Transform
}

Positioned :: impl Entity using transform

Main :: task() {
    entity := Entity{
        transform = Transform{x = 42},
    }

    positioned := cast<Positioned>(&entity)
    position := Position(positioned)

    assert(position == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct Positioned {",
		"Positioned_Transform_Position(void *data)",
		"Positioned_Entity_Position(void *data)",
		"return Positioned_Transform_Position(",
		"->transform",
		"(void *)&",
		".tag = Positioned_Tag_Entity",
		"Positioned_Position(positioned)",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}
}

func TestGeneratePositionedNestedDelegation(t *testing.T) {
	out, reporter := generate(t, `
Positioned :: interface {
    Position :: task(value *self) int
}

Transform :: struct {
    x int
}

ReadPosition :: task(transform *Transform) int {
    return transform.x
}

Positioned :: impl Transform {
    Position :: ReadPosition
}

Components :: struct {
    transform Transform
}

Entity :: struct {
    components Components
}

Positioned :: impl Entity using components.transform

Main :: task() {
    entity := Entity{
        components = Components{
            transform = Transform{x = 64},
        },
    }

    positioned := cast<Positioned>(&entity)
    position := Position(positioned)

    assert(position == 64)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"Positioned_Transform_Position(void *data)",
		"Positioned_Entity_Position(void *data)",
		"return Positioned_Transform_Position(",
		"->components",
		").transform",
		"Positioned_Position(positioned)",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}
}

func TestGeneratePositionedPointerDelegation(t *testing.T) {
	out, reporter := generate(t, `
Positioned :: interface {
    Position :: task(value *self) int
}

Transform :: struct {
    x int
}

ReadPosition :: task(transform *Transform) int {
    return transform.x
}

Positioned :: impl Transform {
    Position :: ReadPosition
}

Entity :: struct {
    transform *Transform
}

Positioned :: impl Entity using transform

Main :: task() {
    transform := Transform{x = 128}
    entity := Entity{transform = &transform}

    positioned := cast<Positioned>(&entity)
    position := Position(positioned)

    assert(position == 128)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"Positioned_Transform_Position(void *data)",
		"Positioned_Entity_Position(void *data)",
		"return Positioned_Transform_Position((void *)",
		"->transform",
		"Positioned_Position(positioned)",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}

	if strings.Contains(
		out,
		"return Positioned_Transform_Position((void *)&",
	) {
		t.Fatalf(
			"pointer-field delegation must pass the pointer value, not its address:\n%s",
			out,
		)
	}
}

func TestGenerateRawptrByteIndexReadWrite(t *testing.T) {
	out, reporter := generate(t, `
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

	if !strings.Contains(out, "((unsigned char *)(ptr))[0] = 255;") {
		t.Fatalf("expected rawptr byte write")
	}

	if !strings.Contains(out, "uint8_t b = ((unsigned char *)(ptr))[0];") {
		t.Fatalf("expected rawptr byte read")
	}
}

func TestGenerateValueByteIndexReadWrite(t *testing.T) {
	out, reporter := generate(t, `
Main :: task() {
    x := 300
    b := x[0]
    x[0] = b
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "uint8_t b = ((unsigned char *)&(x))[0];") {
		t.Fatalf("expected value byte read")
	}

	if !strings.Contains(out, "((unsigned char *)&(x))[0] = b;") {
		t.Fatalf("expected value byte write")
	}
}

func TestGenerateSizePrimitiveTypeAndValue(t *testing.T) {
	out, reporter := generate(t, `
Goblin :: struct {
    hp int
}

Main :: task() {
    x := 10
    s := "ñ"
    cs := c"ñ"

    a: uint = size(int)
    b: uint = size(Goblin)
    c: uint = size(x)
    d: uint = size(s)
    e: uint = size(cs)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	checks := []string{
		"uintptr_t a = (uintptr_t)sizeof(intptr_t);",
		"uintptr_t b = (uintptr_t)sizeof(Goblin);",
		"uintptr_t c = (uintptr_t)sizeof(x);",
		"uintptr_t d = (uintptr_t)(s).len;",
		"uintptr_t e = seal_cstring_byte_len(cs);",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}

	compileGeneratedC(t, out)
}

func TestGenerateAssertPrimitive(t *testing.T) {
	out, reporter := generate(t, `
Main :: task() {
    x := 10
    assert(x == 10)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "#include <assert.h>") {
		t.Fatalf("expected assert include, got:\n%s", out)
	}

	if !strings.Contains(out, "assert((x == 10));") {
		t.Fatalf("expected assert lowering, got:\n%s", out)
	}
}

func TestGenerateDistinctType(t *testing.T) {
	out, reporter := generate(t, `
EnemyId :: distinct uint

Main :: task() {
    id: EnemyId = 10
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "typedef uintptr_t EnemyId;") {
		t.Fatalf("expected distinct typedef, got:\n%s", out)
	}

	if !strings.Contains(out, "EnemyId id = 10;") {
		t.Fatalf("expected distinct variable, got:\n%s", out)
	}
}

func TestGenerateDistinctCast(t *testing.T) {
	out, reporter := generate(t, `
Id :: distinct int

Main :: task() {
    x := cast<Id>(5)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "typedef intptr_t Id;") {
		t.Fatalf("expected distinct typedef, got:\n%s", out)
	}

	if !strings.Contains(out, "Id x = ((Id)(5));") {
		t.Fatalf("expected distinct cast lowering, got:\n%s", out)
	}
}

func TestRejectDistinctCompoundLiteralInCodegen(t *testing.T) {
	file := source.NewFile("test.seal", `
Id :: distinct int

Main :: task() {
    x := Id{5}
}
`)
	reporter := diag.NewReporter()

	lex := lexer.New(file, reporter)
	tokens := lex.LexAll()

	p := parser.New(tokens, reporter)
	parsed := p.ParseFile()

	r := resolver.New(reporter)
	r.ResolveFile(parsed)

	g := New(reporter)
	_ = g.Generate(parsed)

	if !reporter.HasErrors() {
		t.Fatalf("expected diagnostics")
	}

	if !strings.Contains(reporter.String(), "distinct type Id cannot be constructed with a literal") {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}
}

func TestGenerateGenericStructSpecialization(t *testing.T) {
	out, reporter := generate(t, `
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

	if !strings.Contains(out, "typedef struct Box_int") {
		t.Fatalf("expected specialized Box_int struct, got:\n%s", out)
	}

	if !strings.Contains(out, "intptr_t value;") {
		t.Fatalf("expected int field in Box_int, got:\n%s", out)
	}

	if !strings.Contains(out, "Box_int b = (Box_int){.value = 10};") {
		t.Fatalf("expected Box_int variable initialization, got:\n%s", out)
	}
}

func TestGenerateNestedGenericStructSpecialization(t *testing.T) {
	out, reporter := generate(t, `
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

	if !strings.Contains(out, "typedef struct Pair_int_string") {
		t.Fatalf("expected Pair_int_string struct, got:\n%s", out)
	}

	if !strings.Contains(out, "typedef struct Box_Pair_int_string") {
		t.Fatalf("expected Box_Pair_int_string struct, got:\n%s", out)
	}

	if !strings.Contains(out, "Pair_int_string value;") {
		t.Fatalf("expected nested Pair_int_string field, got:\n%s", out)
	}
}

func TestGenerateGenericStructFieldAccess(t *testing.T) {
	out, reporter := generate(t, `
Box :: struct <T type> {
    value T
}

Main :: task() {
    b: Box<int> = Box<int>{value = 10}
    x := b.value
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "typedef struct Box_int") {
		t.Fatalf("expected Box_int struct, got:\n%s", out)
	}

	if !strings.Contains(out, "Box_int b = (Box_int){.value = 10};") {
		t.Fatalf("expected Box_int initialization, got:\n%s", out)
	}

	if !strings.Contains(out, "intptr_t x = (b).value;") {
		t.Fatalf("expected generic struct field access, got:\n%s", out)
	}
}

func TestGenerateNestedGenericStructFieldAccess(t *testing.T) {
	out, reporter := generate(t, `
Pair :: struct <A type, B type> {
    a A
    b B
}

Box :: struct <T type> {
    value T
}

Main :: task() {
    b: Box<Pair<int, string>>
    p := b.value
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "typedef struct Pair_int_string") {
		t.Fatalf("expected Pair_int_string struct, got:\n%s", out)
	}

	if !strings.Contains(out, "typedef struct Box_Pair_int_string") {
		t.Fatalf("expected Box_Pair_int_string struct, got:\n%s", out)
	}

	if !strings.Contains(out, "Pair_int_string p = (b).value;") {
		t.Fatalf("expected nested generic field access, got:\n%s", out)
	}
}

func TestGenerateDynInterfaceAssignmentAndDispatch(t *testing.T) {
	out, reporter := generate(t, `
Enemy :: dyn interface <Value type> {
    Health :: task(e *self) Value
}

Goblin :: struct {
    hp int
}


    GoblinHealth :: task(g *Goblin) int {

    return g.hp
}

Enemy<int> :: impl Goblin {

    Health :: GoblinHealth

}

Main :: task() {
    g := Goblin{hp = 10}


    e := cast<Enemy<int>>(&g)


    hp := Health(e)
    assert(hp == 10)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct Enemy_int_vtable",
		"typedef struct Enemy_int {",
		"void *data;",
		"const Enemy_int_vtable *vtable;",
		"Enemy_int_Goblin_Health",
		"static const Enemy_int_vtable Enemy_int_Goblin_vtable",
		".vtable = &Enemy_int_Goblin_vtable",
		"Enemy_int_Health(e)",
		"return receiver.vtable->Health(receiver.data);",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}
}

func TestGenerateGenericTaskIdentity(t *testing.T) {
	out, reporter := generate(t, `
Identity :: task <T type>(value T) T {
    return value
}

Main :: task() {
    a := Identity<int>(10)
    b := Identity<string>("hello")
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	checks := []string{
		"intptr_t Identity_int(intptr_t value);",
		"sealString Identity_string(sealString value);",

		"intptr_t Identity_int(intptr_t value) {",
		"sealString Identity_string(sealString value) {",

		"intptr_t a = Identity_int(10);",

		`sealString b = Identity_string((sealString){.data = (const uint8_t *)"hello", .len = (uintptr_t)5});`,
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}

	compileGeneratedC(t, out)
}

func TestGenerateGenericTaskReturnsGenericStruct(t *testing.T) {
	out, reporter := generate(t, `
Box :: struct <T type> {
    value T
}

MakeBox :: task <T type>(value T) Box<T> {
    return Box<T>{value = value}
}

Main :: task() {
    b := MakeBox<int>(10)
    x := b.value
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "typedef struct Box_int") {
		t.Fatalf("expected Box_int struct, got:\n%s", out)
	}

	if !strings.Contains(out, "Box_int MakeBox_int(intptr_t value);") {
		t.Fatalf("expected MakeBox_int prototype, got:\n%s", out)
	}

	if !strings.Contains(out, "Box_int MakeBox_int(intptr_t value) {") {
		t.Fatalf("expected MakeBox_int body, got:\n%s", out)
	}

	if !strings.Contains(out, "Box_int b = MakeBox_int(10);") {
		t.Fatalf("expected MakeBox_int call, got:\n%s", out)
	}

	if !strings.Contains(out, "intptr_t x = (b).value;") {
		t.Fatalf("expected Box_int field access, got:\n%s", out)
	}
}

func TestGenerateGenericTaskUsesCastTypeParam(t *testing.T) {
	out, reporter := generate(t, `
Id :: distinct int

Wrap :: task <T type>(value int) T {
	return cast<T>(value)
}

Main :: task() {
	id: Id = Wrap<Id>(10)
	assert(id == cast<Id>(10))
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"Id Wrap_Id(intptr_t value);",
		"Id Wrap_Id(intptr_t value) {",
		"Id __seal_return_value_",
		"return __seal_return_value_",
		"Id id = Wrap_Id(10);",
		"assert((id == ((Id)(10))));",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated output to contain %q\n\n%s", want, out)
		}
	}
}

func TestGenerateGenericTaskCallsGenericTaskWithTypeParam(
	t *testing.T,
) {
	out, reporter := generate(t, `
Identity :: task <T type>(value T) T {
    return value
}

Forward :: task <T type>(value T) T {
    return Identity<T>(value)
}

Main :: task() {
    a := Forward<int>(10)
    b := Forward<string>("hello")

    assert(a == 10)
    assert(size(b) > 0)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	checks := []string{
		"intptr_t Identity_int(intptr_t value);",
		"sealString Identity_string(sealString value);",

		"intptr_t Forward_int(intptr_t value);",
		"sealString Forward_string(sealString value);",

		"Identity_int(value)",
		"Identity_string(value)",

		"intptr_t a = Forward_int(10);",

		`sealString b = Forward_string((sealString){.data = (const uint8_t *)"hello", .len = (uintptr_t)5});`,
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated output to contain %q\n\n%s",
				want,
				out,
			)
		}
	}

	compileGeneratedC(t, out)
}

func TestGenerateGenericTaskUsesGenericStructOnlyInBody(t *testing.T) {
	out, reporter := generate(t, `
Box :: struct <T type> {
	value T
}

MakeAndRead :: task <T type>(value T) T {
	box: Box<T> = Box<T>{value = value}
	return box.value
}

Main :: task() {
	x := MakeAndRead<int>(42)
	assert(x == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct Box_int {",
		"intptr_t value;",
		"} Box_int;",
		"intptr_t MakeAndRead_int(intptr_t value);",
		"intptr_t MakeAndRead_int(intptr_t value) {",
		"Box_int box = (Box_int){.value = value};",
		"return __seal_return_value_",
		"intptr_t x = MakeAndRead_int(42);",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated output to contain %q\n\n%s", want, out)
		}
	}
}

func TestGenerateGenericTaskDefaultParameterUsesGenericType(t *testing.T) {
	out, reporter := generate(t, `
Id :: distinct int

WithDefault :: task <T type>(value T = cast<T>(7)) T {
	return value
}

Main :: task() {
	id: Id = WithDefault<Id>()
	assert(id == cast<Id>(7))
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"Id WithDefault_Id(Id value);",
		"Id WithDefault_Id(Id value) {",
		"Id id = WithDefault_Id(((Id)(7)));",
		"assert((id == ((Id)(7))));",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated output to contain %q\n\n%s", want, out)
		}
	}
}

func TestGenerateGenericTaskNestedGenericStructInReturn(
	t *testing.T,
) {
	out, reporter := generate(t, `
Box :: struct <T type> {
    value T
}

Pair :: struct <A type, B type> {
    first A
    second B
}

MakeNested :: task <T type>(value T) Box<Pair<int, T>> {
    return Box<Pair<int, T>>{
        value = Pair<int, T>{
            first = 1,
            second = value,
        },
    }
}

Main :: task() {
    nested := MakeNested<string>("hello")
    pair := nested.value
    assert(pair.first == 1)
    assert(size(pair.second) > 0)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	checks := []string{
		"typedef struct Pair_int_string {",
		"intptr_t first;",
		"sealString second;",
		"} Pair_int_string;",

		"typedef struct Box_Pair_int_string {",
		"Pair_int_string value;",
		"} Box_Pair_int_string;",

		"Box_Pair_int_string MakeNested_string(sealString value);",
		"Box_Pair_int_string MakeNested_string(sealString value) {",

		"return __seal_return_value_",

		`Box_Pair_int_string nested = MakeNested_string((sealString){.data = (const uint8_t *)"hello", .len = (uintptr_t)5});`,

		"Pair_int_string pair = (nested).value;",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated output to contain %q\n\n%s",
				want,
				out,
			)
		}
	}

	compileGeneratedC(t, out)
}

func TestGenerateGenericTaskParameterValueCall(t *testing.T) {
	out, reporter := generate(t, `
Double :: task(value int) int {
	return value * 2
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

	checks := []string{
		"intptr_t Double(intptr_t value);",
		"intptr_t Apply_Double(intptr_t value);",
		"intptr_t Apply_Double(intptr_t value) {",
		"Double(value)",
		"intptr_t x = Apply_Double(10);",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated output to contain %q\n\n%s", want, out)
		}
	}
}

func TestGenerateGenericTaskParameterSpecializedGenericTaskValueCall(t *testing.T) {
	out, reporter := generate(t, `
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

	checks := []string{
		"intptr_t Identity_int(intptr_t value);",
		"intptr_t Apply_Identity_int(intptr_t value);",
		"intptr_t Apply_Identity_int(intptr_t value) {",
		"Identity_int(value)",
		"intptr_t x = Apply_Identity_int(10);",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated output to contain %q\n\n%s", want, out)
		}
	}
}

func TestGenerateGenericMultiReturnTask(t *testing.T) {
	out, reporter := generate(t, `
Swap :: task <T type>(a T, b T) T, T {
	return b, a
}

Main :: task() {
	x, y := Swap<int>(1, 2)

	assert(x == 2)
	assert(y == 1)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct Swap_int_Result {",
		"intptr_t _0;",
		"intptr_t _1;",
		"} Swap_int_Result;",
		"Swap_int_Result Swap_int(intptr_t a, intptr_t b);",
		"Swap_int_Result Swap_int(intptr_t a, intptr_t b) {",
		"Swap_int_Result __seal_return_value_",
		"__seal_return_value_",
		"._0 = b;",
		"._1 = a;",
		"return __seal_return_value_",
		"Swap_int_Result __seal_multi_result_",
		"= Swap_int(1, 2);",
		"intptr_t x = __seal_multi_result_",
		"._0;",
		"intptr_t y = __seal_multi_result_",
		"._1;",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated output to contain %q\n\n%s", want, out)
		}
	}
}

func TestGenerateGenericMultiReturnTaskWithGenericStruct(t *testing.T) {
	out, reporter := generate(t, `
Box :: struct <T type> {
	value T
}

PairBoxes :: task <T type>(a T, b T) Box<T>, Box<T> {
	return Box<T>{value = a}, Box<T>{value = b}
}

Main :: task() {
	left, right := PairBoxes<int>(1, 2)

	x := left.value
	y := right.value

	assert(x == 1)
	assert(y == 2)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct Box_int {",
		"intptr_t value;",
		"} Box_int;",
		"typedef struct PairBoxes_int_Result {",
		"Box_int _0;",
		"Box_int _1;",
		"} PairBoxes_int_Result;",
		"PairBoxes_int_Result PairBoxes_int(intptr_t a, intptr_t b);",
		"PairBoxes_int_Result PairBoxes_int(intptr_t a, intptr_t b) {",
		"Box_int left = __seal_multi_result_",
		"._0;",
		"Box_int right = __seal_multi_result_",
		"._1;",
		"intptr_t x = (left).value;",
		"intptr_t y = (right).value;",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated output to contain %q\n\n%s", want, out)
		}
	}
}

func TestGenericMultiReturnTaskCodegen(t *testing.T) {
	out, reporter := generate(t, `
Swap :: task <T type>(a T, b T) T, T {
    return b, a
}

Main :: task() {
    x, y := Swap<int>(1, 2)

    assert(x == 2)
    assert(y == 1)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct Swap_int_Result {",
		"intptr_t _0;",
		"intptr_t _1;",
		"} Swap_int_Result;",
		"Swap_int_Result Swap_int(intptr_t a, intptr_t b);",
		"Swap_int_Result Swap_int(intptr_t a, intptr_t b) {",
		"Swap_int_Result __seal_return_value_",
		"__seal_return_value_",
		"._0 = b;",
		"._1 = a;",
		"return __seal_return_value_",
		"Swap_int_Result __seal_multi_result_",
		"= Swap_int(1, 2);",
		"intptr_t x = __seal_multi_result_",
		"intptr_t y = __seal_multi_result_",
		"._0;",
		"._1;",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestGenericTaskParameterAsValueCodegen(t *testing.T) {
	out, reporter := generate(t, `
Double :: task(x int) int {
    return x * 2
}

Apply :: task <F task[(int) int]>(x int) int {
    return F(x)
}

Main :: task() {
    y := Apply<Double>(10)
    assert(y == 20)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"intptr_t Double(intptr_t x);",
		"intptr_t Apply_Double(intptr_t x);",
		"intptr_t Apply_Double(intptr_t x) {",
		"intptr_t __seal_return_value_",
		"= Double(x);",
		"return __seal_return_value_",
		"intptr_t y = Apply_Double(10);",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestGenericTaskParameterMultiReturnCodegen(t *testing.T) {
	out, reporter := generate(t, `
SwapInt :: task(a int, b int) int, int {
    return b, a
}

UseSwap :: task <F task[(int, int) int, int]>(a int, b int) int {
    x, y := F(a, b)
    return x - y
}

Main :: task() {
    result := UseSwap<SwapInt>(1, 5)
    assert(result == 4)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct SwapInt_Result {",
		"intptr_t _0;",
		"intptr_t _1;",
		"} SwapInt_Result;",
		"intptr_t UseSwap_SwapInt(intptr_t a, intptr_t b);",
		"intptr_t UseSwap_SwapInt(intptr_t a, intptr_t b) {",
		"SwapInt_Result __seal_multi_result_",
		"= SwapInt(a, b);",
		"intptr_t x = __seal_multi_result_",
		"._0;",
		"intptr_t y = __seal_multi_result_",
		"._1;",
		"intptr_t __seal_return_value_",
		"= (x - y);",
		"return __seal_return_value_",
		"intptr_t result = UseSwap_SwapInt(1, 5);",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestGenericTaskParameterSpecializedGenericMultiReturnCodegen(t *testing.T) {
	out, reporter := generate(t, `
Swap :: task <T type>(a T, b T) T, T {
    return b, a
}

UseSwap :: task <F task[(int, int) int, int]>(a int, b int) int {
    x, y := F(a, b)
    return x - y
}

Main :: task() {
    result := UseSwap<Swap<int>>(1, 5)
    assert(result == 4)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct Swap_int_Result {",
		"intptr_t _0;",
		"intptr_t _1;",
		"} Swap_int_Result;",
		"Swap_int_Result Swap_int(intptr_t a, intptr_t b);",
		"intptr_t UseSwap_Swap_int(intptr_t a, intptr_t b);",
		"Swap_int_Result Swap_int(intptr_t a, intptr_t b) {",
		"intptr_t UseSwap_Swap_int(intptr_t a, intptr_t b) {",
		"Swap_int_Result __seal_multi_result_",
		"= Swap_int(a, b);",
		"intptr_t x = __seal_multi_result_",
		"._0;",
		"intptr_t y = __seal_multi_result_",
		"._1;",
		"intptr_t result = UseSwap_Swap_int(1, 5);",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestGenericDirectMultiReturnForwardingCodegen(t *testing.T) {
	out, reporter := generate(t, `
Swap :: task <T type>(a T, b T) T, T {
    return b, a
}

ForwardSwap :: task <T type>(a T, b T) T, T {
    return Swap<T>(a, b)
}

Main :: task() {
    x, y := ForwardSwap<int>(1, 5)

    assert(x == 5)
    assert(y == 1)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct Swap_int_Result {",
		"typedef struct ForwardSwap_int_Result {",
		"Swap_int_Result Swap_int(intptr_t a, intptr_t b);",
		"ForwardSwap_int_Result ForwardSwap_int(intptr_t a, intptr_t b);",
		"ForwardSwap_int_Result ForwardSwap_int(intptr_t a, intptr_t b) {",
		"Swap_int_Result __seal_forward_result_",
		"= Swap_int(a, b);",
		"ForwardSwap_int_Result __seal_return_value_",
		"._0 = __seal_forward_result_",
		"._1 = __seal_forward_result_",
		"return __seal_return_value_",
		"ForwardSwap_int_Result __seal_multi_result_",
		"= ForwardSwap_int(1, 5);",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestGenericTaskParamDirectMultiReturnForwardingCodegen(t *testing.T) {
	out, reporter := generate(t, `
SwapInt :: task(a int, b int) int, int {
    return b, a
}

ForwardWith :: task <F task[(int, int) int, int]>(a int, b int) int, int {
    return F(a, b)
}

Main :: task() {
    x, y := ForwardWith<SwapInt>(1, 5)

    assert(x == 5)
    assert(y == 1)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct SwapInt_Result {",
		"typedef struct ForwardWith_SwapInt_Result {",
		"SwapInt_Result SwapInt(intptr_t a, intptr_t b);",
		"ForwardWith_SwapInt_Result ForwardWith_SwapInt(intptr_t a, intptr_t b);",
		"ForwardWith_SwapInt_Result ForwardWith_SwapInt(intptr_t a, intptr_t b) {",
		"SwapInt_Result __seal_forward_result_",
		"= SwapInt(a, b);",
		"ForwardWith_SwapInt_Result __seal_return_value_",
		"._0 = __seal_forward_result_",
		"._1 = __seal_forward_result_",
		"return __seal_return_value_",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestGenericTaskParamSpecializedGenericDirectMultiReturnForwardingCodegen(t *testing.T) {
	out, reporter := generate(t, `
Swap :: task <T type>(a T, b T) T, T {
    return b, a
}

ForwardWith :: task <F task[(int, int) int, int]>(a int, b int) int, int {
    return F(a, b)
}

Main :: task() {
    x, y := ForwardWith<Swap<int>>(1, 5)

    assert(x == 5)
    assert(y == 1)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct Swap_int_Result {",
		"typedef struct ForwardWith_Swap_int_Result {",
		"Swap_int_Result Swap_int(intptr_t a, intptr_t b);",
		"ForwardWith_Swap_int_Result ForwardWith_Swap_int(intptr_t a, intptr_t b);",
		"ForwardWith_Swap_int_Result ForwardWith_Swap_int(intptr_t a, intptr_t b) {",
		"Swap_int_Result __seal_forward_result_",
		"= Swap_int(a, b);",
		"ForwardWith_Swap_int_Result __seal_return_value_",
		"._0 = __seal_forward_result_",
		"._1 = __seal_forward_result_",
		"return __seal_return_value_",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func generateWithPackages(
	t *testing.T,
	input string,
	packages map[string]*PackageInfo,
	resolverPackages map[string]*resolver.PackageInfo,
	checkerPackages map[string]*checker.PackageInfo,
) (string, *diag.Reporter) {
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

	c := checker.NewWithPackages(reporter, checkerPackages)
	c.CheckFile(parsed)

	if reporter.HasErrors() {
		t.Fatalf("checker diagnostics:\n%s", reporter.String())
	}

	g := NewWithPackagesAndSemanticInfo(
		reporter,
		"",
		packages,
		c.SemanticInfo(),
	)
	out := g.Generate(parsed)

	return out, reporter
}

func exportCGenPackage(t *testing.T, packageName string, input string) (*PackageInfo, *resolver.PackageInfo, *checker.PackageInfo) {
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

	c := checker.New(reporter)
	checkerScope := c.CheckFile(parsed)

	if reporter.HasErrors() {
		t.Fatalf("checker diagnostics:\n%s", reporter.String())
	}

	return ExportPackageInfoWithSemanticInfo(
			packageName,
			parsed,
			reporter,
			nil,
			c.SemanticInfo(),
		),

		resolver.ExportPackage(packageName, resolverScope),
		checker.ExportPackage(packageName, checkerScope)
}

func TestImportedGenericTaskSpecializationCodegen(t *testing.T) {
	mathPkg, mathResolverPkg, mathCheckerPkg := exportCGenPackage(t, "math", `
Identity :: task <T type>(value T) T {
    return value
}
`)

	out, reporter := generateWithPackages(t, `
Main :: task() {
    x := math.Identity<int>(10)
    assert(x == 10)
}
`, map[string]*PackageInfo{
		"math": mathPkg,
	}, map[string]*resolver.PackageInfo{
		"math": mathResolverPkg,
	}, map[string]*checker.PackageInfo{
		"math": mathCheckerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"intptr_t math_Identity_int(intptr_t value);",
		"intptr_t x = math_Identity_int(10);",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestImportedGenericMultiReturnTaskSpecializationCodegen(t *testing.T) {
	mathPkg, mathResolverPkg, mathCheckerPkg := exportCGenPackage(t, "math", `
Swap :: task <T type>(a T, b T) T, T {
    return b, a
}
`)

	out, reporter := generateWithPackages(t, `
Main :: task() {
    x, y := math.Swap<int>(1, 5)

    assert(x == 5)
    assert(y == 1)
}
`, map[string]*PackageInfo{
		"math": mathPkg,
	}, map[string]*resolver.PackageInfo{
		"math": mathResolverPkg,
	}, map[string]*checker.PackageInfo{
		"math": mathCheckerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct math_Swap_int_Result {",
		"intptr_t _0;",
		"intptr_t _1;",
		"math_Swap_int_Result math_Swap_int(intptr_t a, intptr_t b);",
		"math_Swap_int_Result __seal_multi_result_",
		"= math_Swap_int(1, 5);",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestImportedGenericTaskArgumentCodegen(t *testing.T) {
	mathPkg, mathResolverPkg, mathCheckerPkg := exportCGenPackage(t, "math", `
Identity :: task <T type>(value T) T {
    return value
}
`)

	out, reporter := generateWithPackages(t, `
Apply :: task <F task[(int) int]>(value int) int {
    return F(value)
}

Main :: task() {
    x := Apply<math.Identity<int>>(10)
    assert(x == 10)
}
`, map[string]*PackageInfo{
		"math": mathPkg,
	}, map[string]*resolver.PackageInfo{
		"math": mathResolverPkg,
	}, map[string]*checker.PackageInfo{
		"math": mathCheckerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"intptr_t math_Identity_int(intptr_t value);",
		"intptr_t Apply_math_Identity_int(intptr_t value);",
		"intptr_t Apply_math_Identity_int(intptr_t value) {",
		"math_Identity_int(value)",
		"intptr_t x = Apply_math_Identity_int(10);",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestImportedGenericStructSpecializationCodegen(t *testing.T) {
	typesPkg, resolverPkg, checkerPkg := exportCGenPackage(t, "types", `
Box :: struct <T type> {
    value T
}
`)

	out, reporter := generateWithPackages(t, `
Main :: task() {
    b: types.Box<int> = types.Box<int>{value = 10}
    x := b.value
    assert(x == 10)
}
`, map[string]*PackageInfo{
		"types": typesPkg,
	}, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*checker.PackageInfo{
		"types": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct types_Box_int {",
		"intptr_t value;",
		"} types_Box_int;",
		"types_Box_int b = (types_Box_int){.value = 10};",
		"intptr_t x = (b).value;",
		"assert((x == 10));",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestImportedNestedGenericStructSpecializationCodegen(t *testing.T) {
	typesPkg, resolverPkg, checkerPkg := exportCGenPackage(t, "types", `
Pair :: struct <A type, B type> {
    first A
    second B
}

Box :: struct <T type> {
    value T
}
`)

	out, reporter := generateWithPackages(t, `
Main :: task() {
    b: types.Box<types.Pair<int, string>>
    pair := b.value
    x := pair.first
    s := pair.second

    assert(x == 0)
    assert(size(s) >= 0)
}
`, map[string]*PackageInfo{
		"types": typesPkg,
	}, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*checker.PackageInfo{
		"types": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct types_Pair_int_string {",
		"intptr_t first;",
		"sealString second;",
		"} types_Pair_int_string;",
		"typedef struct types_Box_types_Pair_int_string {",
		"types_Pair_int_string value;",
		"} types_Box_types_Pair_int_string;",
		"types_Box_types_Pair_int_string b;",
		"types_Pair_int_string pair = (b).value;",
		"intptr_t x = (pair).first;",
		"sealString s = (pair).second;",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestImportedGenericTaskReturnsImportedGenericStructCodegen(t *testing.T) {
	typesPkg, resolverPkg, checkerPkg := exportCGenPackage(t, "types", `
Box :: struct <T type> {
    value T
}

MakeBox :: task <T type>(value T) Box<T> {
    return Box<T>{value = value}
}
`)

	out, reporter := generateWithPackages(t, `
Main :: task() {
    b := types.MakeBox<int>(10)
    x := b.value
    assert(x == 10)
}
`, map[string]*PackageInfo{
		"types": typesPkg,
	}, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*checker.PackageInfo{
		"types": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct types_Box_int {",
		"intptr_t value;",
		"} types_Box_int;",
		"types_Box_int types_MakeBox_int(intptr_t value);",
		"types_Box_int b = types_MakeBox_int(10);",
		"intptr_t x = (b).value;",
		"assert((x == 10));",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestImportedGenericTaskRecordsInstanceRequest(t *testing.T) {
	typesPkg, resolverPkg, checkerPkg := exportCGenPackage(t, "types", `
Box :: struct <T type> {
    value T
}

MakeBox :: task <T type>(value T) Box<T> {
    return Box<T>{value = value}
}
`)

	file := source.NewFile("test.seal", `
Main :: task() {
    b := types.MakeBox<int>(10)
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

	r := resolver.NewWithPackages(reporter, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	})
	r.ResolveFile(parsed)
	if reporter.HasErrors() {
		t.Fatalf("resolver diagnostics:\n%s", reporter.String())
	}

	c := checker.NewWithPackages(reporter, map[string]*checker.PackageInfo{
		"types": checkerPkg,
	})
	c.CheckFile(parsed)
	if reporter.HasErrors() {
		t.Fatalf("checker diagnostics:\n%s", reporter.String())
	}

	g := NewWithPackagesAndSemanticInfo(
		reporter,
		"",
		map[string]*PackageInfo{
			"types": typesPkg,
		},
		c.SemanticInfo(),
	)

	_ = g.Generate(parsed)

	reqs := g.RequestedGenericInstances()

	var sawTask bool
	var sawStruct bool

	for _, req := range reqs {
		if req.PackageName == "types" && req.SymbolName == "MakeBox" && req.Kind == GenericInstanceTask {
			sawTask = true
		}

		if req.PackageName == "types" && req.SymbolName == "Box" && req.Kind == GenericInstanceStruct {
			sawStruct = true
		}
	}

	if !sawTask {
		t.Fatalf("expected request for task types.MakeBox<int>, got %#v", reqs)
	}

	if !sawStruct {
		t.Fatalf("expected request for struct types.Box<int>, got %#v", reqs)
	}
}

func TestRequestedGenericTaskEmitsPackagePrefixedDefinition(t *testing.T) {
	file := source.NewFile("types.seal", `
Box :: struct <T type> {
    value T
}

MakeBox :: task <T type>(value T) Box<T> {
    return Box<T>{value = value}
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
	if reporter.HasErrors() {
		t.Fatalf("resolver diagnostics:\n%s", reporter.String())
	}

	c := checker.New(reporter)
	c.CheckFile(parsed)
	if reporter.HasErrors() {
		t.Fatalf("checker diagnostics:\n%s", reporter.String())
	}

	g := NewWithPackagesAndSemanticInfo(
		reporter,
		"types",
		nil,
		c.SemanticInfo(),
	)

	g.AddRequestedInstances([]GenericInstanceRequest{
		{
			Kind:        GenericInstanceTask,
			PackageName: "types",
			SymbolName:  "MakeBox",
			Args: []ast.GenericArg{
				{
					Kind: ast.GenericArgExpr,
					Expr: &ast.IdentExpr{
						Name: ast.Ident{
							Name: "int",
							Loc:  source.NewSpan(file, 0, 0),
						},
					},
					Loc: source.NewSpan(file, 0, 0),
				},
			},
		},
	})

	out := g.Generate(parsed)

	checks := []string{
		"typedef struct types_Box_int {",
		"intptr_t value;",
		"} types_Box_int;",
		"types_Box_int types_MakeBox_int(intptr_t value);",
		"types_Box_int types_MakeBox_int(intptr_t value) {",
		"return __seal_return_value_",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestGenericFieldConstraintCodegen(t *testing.T) {
	out, reporter := generate(t, `
Player :: struct {
    health int
}

HealthOf :: task <T type[health int]>(target T) int {
    return target.health
}

Main :: task() {
    p := Player{health = 10}
    h := HealthOf<Player>(p)
    assert(h == 10)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct Player {",
		"intptr_t health;",
		"} Player;",
		"intptr_t HealthOf_Player(Player target);",
		"Player p = (Player){.health = 10};",
		"intptr_t h = HealthOf_Player(p);",
		"intptr_t HealthOf_Player(Player target) {",
		"return __seal_return_value_",
		"(target).health",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestGenericFieldConstraintPointerCodegen(t *testing.T) {
	out, reporter := generate(t, `
Enemy :: struct {
    health int
}

ReadHealth :: task <T type[health int]>(target *T) int {
    return target.health
}

Main :: task() {
    e := Enemy{health = 25}
    h := ReadHealth<Enemy>(&e)
    assert(h == 25)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct Enemy {",
		"intptr_t health;",
		"} Enemy;",
		"intptr_t ReadHealth_Enemy(Enemy * target);",
		"Enemy e = (Enemy){.health = 25};",
		"intptr_t h = ReadHealth_Enemy((&e));",
		"intptr_t ReadHealth_Enemy(Enemy * target) {",
		"(target)->health",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestGenericFieldConstraintMutationCodegen(t *testing.T) {
	out, reporter := generate(t, `
Player :: struct {
    health int
}

Damage :: task <T type[health int]>(target *T, amount int) {
    target.health -= amount
}

Main :: task() {
    p := Player{health = 10}
    Damage<Player>(&p, 3)
    assert(p.health == 7)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"void Damage_Player(Player * target, intptr_t amount);",
		"Player p = (Player){.health = 10};",
		"Damage_Player((&p), 3);",
		"assert(((p).health == 7));",
		"void Damage_Player(Player * target, intptr_t amount) {",
		"(target)->health -= amount;",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestGenericTaskConstraintCodegen(t *testing.T) {
	out, reporter := generate(t, `
Identity :: task <T type>(value T) T {
    return value
}

Apply :: task <T type, F task[(T) T]>(value T) T {
    return F(value)
}

Main :: task() {
    x := Apply<int, Identity<int>>(10)
    assert(x == 10)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"intptr_t Identity_int(intptr_t value);",
		"intptr_t Apply_int_Identity_int(intptr_t value);",
		"intptr_t x = Apply_int_Identity_int(10);",
		"intptr_t Apply_int_Identity_int(intptr_t value) {",
		"Identity_int(value)",
		"intptr_t Identity_int(intptr_t value) {",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestGenericTaskConstraintWithStructTypeCodegen(t *testing.T) {
	out, reporter := generate(t, `
Box :: struct <T type> {
    value T
}

Wrap :: task <T type>(value T) Box<T> {
    return Box<T>{value = value}
}

ApplyWrap :: task <T type, F task[(T) Box<T>]>(value T) Box<T> {
    return F(value)
}

Main :: task() {
    b := ApplyWrap<int, Wrap<int>>(10)
    x := b.value
    assert(x == 10)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct Box_int {",
		"intptr_t value;",
		"} Box_int;",
		"Box_int Wrap_int(intptr_t value);",
		"Box_int ApplyWrap_int_Wrap_int(intptr_t value);",
		"Box_int b = ApplyWrap_int_Wrap_int(10);",
		"intptr_t x = (b).value;",
		"Box_int ApplyWrap_int_Wrap_int(intptr_t value) {",
		"Wrap_int(value)",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestGenericTaskConstraintMultiReturnCodegen(t *testing.T) {
	out, reporter := generate(t, `
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

	checks := []string{
		"typedef struct Swap_int_Result {",
		"intptr_t _0;",
		"intptr_t _1;",
		"} Swap_int_Result;",
		"typedef struct ApplySwap_int_Swap_int_Result {",
		"intptr_t _0;",
		"intptr_t _1;",
		"} ApplySwap_int_Swap_int_Result;",
		"ApplySwap_int_Swap_int_Result ApplySwap_int_Swap_int(intptr_t a, intptr_t b);",
		"Swap_int(a, b)",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}

	if strings.Contains(out, "typedef struct /*invalid*/ int") {
		t.Fatalf("generated invalid generic struct typedef:\n%s", out)
	}
}

func TestImportedStructSatisfiesGenericFieldConstraintCodegen(t *testing.T) {
	typesPkg, resolverPkg, checkerPkg := exportCGenPackage(t, "types", `
Player :: struct {
    health int
}
`)

	out, reporter := generateWithPackages(t, `
HealthOf :: task <T type[health int]>(target T) int {
    return target.health
}

Main :: task() {
    p := types.Player{health = 10}
    h := HealthOf<types.Player>(p)
    assert(h == 10)
}
`, map[string]*PackageInfo{
		"types": typesPkg,
	}, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*checker.PackageInfo{
		"types": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct types_Player {",
		"intptr_t health;",
		"} types_Player;",
		"intptr_t HealthOf_types_Player(types_Player target);",
		"types_Player p = (types_Player){.health = 10};",
		"intptr_t h = HealthOf_types_Player(p);",
		"(target).health",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestImportedGenericStructSatisfiesGenericFieldConstraintCodegen(t *testing.T) {
	typesPkg, resolverPkg, checkerPkg := exportCGenPackage(t, "types", `
Cell :: struct <T type> {
    health T
}
`)

	out, reporter := generateWithPackages(t, `
HealthOf :: task <T type[health int]>(target T) int {
    return target.health
}

Main :: task() {
    c := types.Cell<int>{health = 15}
    h := HealthOf<types.Cell<int>>(c)
    assert(h == 15)
}
`, map[string]*PackageInfo{
		"types": typesPkg,
	}, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*checker.PackageInfo{
		"types": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct types_Cell_int {",
		"intptr_t health;",
		"} types_Cell_int;",
		"intptr_t HealthOf_types_Cell_int(types_Cell_int target);",
		"types_Cell_int c = (types_Cell_int){.health = 15};",
		"intptr_t h = HealthOf_types_Cell_int(c);",
		"(target).health",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestImportedGenericTaskSatisfiesGenericTaskConstraintCodegen(t *testing.T) {
	typesPkg, resolverPkg, checkerPkg := exportCGenPackage(t, "types", `
Identity :: task <T type>(value T) T {
    return value
}
`)

	out, reporter := generateWithPackages(t, `
Apply :: task <T type, F task[(T) T]>(value T) T {
    return F(value)
}

Main :: task() {
    x := Apply<int, types.Identity<int>>(10)
    assert(x == 10)
}
`, map[string]*PackageInfo{
		"types": typesPkg,
	}, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*checker.PackageInfo{
		"types": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"intptr_t types_Identity_int(intptr_t value);",
		"intptr_t Apply_int_types_Identity_int(intptr_t value);",
		"intptr_t x = Apply_int_types_Identity_int(10);",
		"intptr_t Apply_int_types_Identity_int(intptr_t value) {",
		"types_Identity_int(value)",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestImportedGenericTaskReturningImportedGenericStructConstraintCodegen(t *testing.T) {
	typesPkg, resolverPkg, checkerPkg := exportCGenPackage(t, "types", `
Box :: struct <T type> {
    value T
}

Wrap :: task <T type>(value T) Box<T> {
    return Box<T>{value = value}
}
`)

	out, reporter := generateWithPackages(t, `
ApplyWrap :: task <T type, F task[(T) types.Box<T>]>(value T) types.Box<T> {
    return F(value)
}

Main :: task() {
    b := ApplyWrap<int, types.Wrap<int>>(10)
    x := b.value
    assert(x == 10)
}
`, map[string]*PackageInfo{
		"types": typesPkg,
	}, map[string]*resolver.PackageInfo{
		"types": resolverPkg,
	}, map[string]*checker.PackageInfo{
		"types": checkerPkg,
	})

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct types_Box_int {",
		"intptr_t value;",
		"} types_Box_int;",
		"types_Box_int types_Wrap_int(intptr_t value);",
		"types_Box_int ApplyWrap_int_types_Wrap_int(intptr_t value);",
		"types_Box_int b = ApplyWrap_int_types_Wrap_int(10);",
		"intptr_t x = (b).value;",
		"types_Wrap_int(value)",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestGenericTaskTemplateMultiReturnDoesNotEmitUnspecializedResultStruct(t *testing.T) {
	out, reporter := generate(t, `
Pair :: task <T type>(a T, b T) T, T {
    return a, b
}

Main :: task() {
    x, y := Pair<int>(1, 2)
    assert(x == 1)
    assert(y == 2)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if strings.Contains(out, "Pair_Result") {
		t.Fatalf("generic task template emitted unspecialized result struct:\n%s", out)
	}

	if strings.Contains(out, "typedef struct /*invalid*/ int") {
		t.Fatalf("generic task template emitted invalid result struct:\n%s", out)
	}

	checks := []string{
		"typedef struct Pair_int_Result {",
		"intptr_t _0;",
		"intptr_t _1;",
		"} Pair_int_Result;",
		"Pair_int_Result Pair_int(intptr_t a, intptr_t b);",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestNormalizeGenericTypeArgExprToTypeForRequestKey(t *testing.T) {
	params := []ast.GenericParam{
		{
			Name:     ast.Ident{Name: "T"},
			Category: ast.GenericParamType,
		},
	}

	exprArgs := []ast.GenericArg{
		{
			Kind: ast.GenericArgExpr,
			Expr: &ast.IdentExpr{
				Name: ast.Ident{Name: "int"},
			},
		},
	}

	typeArgs := []ast.GenericArg{
		{
			Kind: ast.GenericArgType,
			Type: &ast.NamedType{
				Parts: []ast.Ident{{Name: "int"}},
			},
		},
	}

	normalizedExprArgs := normalizeGenericArgsForCGenParams(params, exprArgs)
	normalizedTypeArgs := normalizeGenericArgsForCGenParams(params, typeArgs)

	reqA := GenericInstanceRequest{
		Kind:        GenericInstanceTask,
		PackageName: "rules",
		SymbolName:  "Box",
		Args:        normalizedExprArgs,
	}

	reqB := GenericInstanceRequest{
		Kind:        GenericInstanceTask,
		PackageName: "rules",
		SymbolName:  "Box",
		Args:        normalizedTypeArgs,
	}

	if reqA.Key() != reqB.Key() {
		t.Fatalf("expected normalized request keys to match:\n%s\n%s", reqA.Key(), reqB.Key())
	}
}

func TestNormalizeGenericTaskArgKeepsExpressionForRequestKey(t *testing.T) {
	params := []ast.GenericParam{
		{
			Name:     ast.Ident{Name: "F"},
			Category: ast.GenericParamTask,
		},
	}

	args := []ast.GenericArg{
		{
			Kind: ast.GenericArgExpr,
			Expr: &ast.GenericExpr{
				Base: &ast.IdentExpr{
					Name: ast.Ident{Name: "Identity"},
				},
				Args: []ast.GenericArg{
					{
						Kind: ast.GenericArgType,
						Type: &ast.NamedType{
							Parts: []ast.Ident{{Name: "int"}},
						},
					},
				},
			},
		},
	}

	normalized := normalizeGenericArgsForCGenParams(params, args)

	if normalized[0].Kind != ast.GenericArgExpr {
		t.Fatalf("expected task generic argument to remain expression, got kind %v", normalized[0].Kind)
	}
}

func TestGenerateDelegationForwardsRequirementArguments(t *testing.T) {
	out, reporter := generate(t, `
Offsettable :: interface {
    Offset :: task(value *self, amount int) int
}

Transform :: struct {
    x int
}

ReadOffset :: task(transform *Transform, amount int) int {
    return transform.x + amount
}

Offsettable :: impl Transform {
    Offset :: ReadOffset
}

Entity :: struct {
    transform Transform
}

Offsettable :: impl Entity using transform

Main :: task() {
    entity := Entity{
        transform = Transform{x = 40},
    }

    offsettable := cast<Offsettable>(&entity)
    result := Offset(offsettable, 2)

    assert(result == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"Offsettable_Transform_Offset(void *data, intptr_t arg1)",
		"Offsettable_Entity_Offset(void *data, intptr_t arg1)",
		"return Offsettable_Transform_Offset(",
		", arg1);",
		"Offsettable_Offset(offsettable, 2)",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}
}

func TestGenerateDelegatedVoidRequirement(t *testing.T) {
	out, reporter := generate(t, `
Movable :: interface {
    Move :: task(value *self, amount int)
}

Transform :: struct {
    x int
}

MoveTransform :: task(transform *Transform, amount int) {
    transform.x += amount
}

Movable :: impl Transform {
    Move :: MoveTransform
}

Entity :: struct {
    transform Transform
}

Movable :: impl Entity using transform

Main :: task() {
    entity := Entity{
        transform = Transform{x = 10},
    }

    movable := cast<Movable>(&entity)
    Move(movable, 5)

    assert(entity.transform.x == 15)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"static void Movable_Transform_Move(void *data, intptr_t arg1)",
		"static void Movable_Entity_Move(void *data, intptr_t arg1)",
		"Movable_Transform_Move(",
		", arg1);",
		"return;",
		"Movable_Move(movable, 5)",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}
}

func TestGenerateDynamicInterfaceDelegation(t *testing.T) {
	out, reporter := generate(t, `
Positioned :: dyn interface {
    Position :: task(value *self) int
}

Transform :: struct {
    x int
}

ReadPosition :: task(transform *Transform) int {
    return transform.x
}

Positioned :: impl Transform {
    Position :: ReadPosition
}

Entity :: struct {
    transform Transform
}

Positioned :: impl Entity using transform

Main :: task() {
    entity := Entity{
        transform = Transform{x = 73},
    }

    positioned := cast<Positioned>(&entity)
    result := Position(positioned)

    assert(result == 73)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct Positioned_vtable",
		"const Positioned_vtable *vtable;",
		"Positioned_Transform_Position(void *data)",
		"Positioned_Entity_Position(void *data)",
		"return Positioned_Transform_Position(",
		"static const Positioned_vtable Positioned_Entity_vtable",
		".Position = Positioned_Entity_Position",
		".vtable = &Positioned_Entity_vtable",
		"Positioned_Position(positioned)",
		"return receiver.vtable->Position(receiver.data);",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}
}

func TestGenerateGenericInterfaceDelegation(t *testing.T) {
	out, reporter := generate(t, `
Readable :: interface <T type> {
    Read :: task(value *self) T
}

Box :: struct <T type> {
    value T
}

Readable<T> :: impl <T type> Box<T> {
    Read :: task(box *Box<T>) T {
        return box.value
    }
}

Holder :: struct <T type> {
    box Box<T>
}

Readable<int> :: impl Holder<int> using box

Main :: task() {
    holder := Holder<int>{
        box = Box<int>{value = 42},
    }

    readable := cast<Readable<int>>(&holder)
    result := Read(readable)

    assert(result == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct Box_int {",
		"intptr_t value;",
		"} Box_int;",
		"typedef struct Holder_int {",
		"Box_int box;",
		"} Holder_int;",
		"typedef struct Readable_int {",
		"Readable_int_Box_int_Read(void *data)",
		"Readable_int_Holder_int_Read(void *data)",
		"return Readable_int_Box_int_Read(",
		"->box",
		".tag = Readable_int_Tag_Holder_int",
		"Readable_int_Read(readable)",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}
}

func TestGenerateDelegationWithMultipleRequirements(t *testing.T) {
	out, reporter := generate(t, `
Spatial :: interface {
    X :: task(value *self) int
    SetX :: task(value *self, x int)
}

Transform :: struct {
    x int
}

ReadX :: task(transform *Transform) int {
    return transform.x
}

WriteX :: task(transform *Transform, x int) {
    transform.x = x
}

Spatial :: impl Transform {
    X :: ReadX
    SetX :: WriteX
}

Entity :: struct {
    transform Transform
}

Spatial :: impl Entity using transform

Main :: task() {
    entity := Entity{
        transform = Transform{x = 10},
    }

    spatial := cast<Spatial>(&entity)

    SetX(spatial, 42)
    result := X(spatial)

    assert(result == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"Spatial_Transform_X(void *data)",
		"Spatial_Transform_SetX(void *data, intptr_t arg1)",
		"Spatial_Entity_X(void *data)",
		"Spatial_Entity_SetX(void *data, intptr_t arg1)",
		"return Spatial_Transform_X(",
		"Spatial_Transform_SetX(",
		", arg1);",
		"Spatial_SetX(spatial, 42)",
		"Spatial_X(spatial)",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}
}

func TestGenerateDelegatedMultipleReturnRequirement(t *testing.T) {
	out, reporter := generate(t, `
Coordinates :: interface {
    Read :: task(value *self) int, int
}

Transform :: struct {
    x int
    y int
}

ReadTransform :: task(transform *Transform) int, int {
    return transform.x, transform.y
}

Coordinates :: impl Transform {
    Read :: ReadTransform
}

Entity :: struct {
    transform Transform
}

Coordinates :: impl Entity using transform

Main :: task() {
    entity := Entity{
        transform = Transform{x = 20, y = 22},
    }

    coordinates := cast<Coordinates>(&entity)
    x, y := Read(coordinates)

    assert(x + y == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct Coordinates_Read_Result {",
		"intptr_t _0;",
		"intptr_t _1;",
		"Coordinates_Read_Result Coordinates_Transform_Read(void *data)",
		"Coordinates_Read_Result Coordinates_Entity_Read(void *data)",
		"Coordinates_Read_Result __seal_",
		"._0 = ",
		"._1 = ",
		"Coordinates_Read(coordinates)",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}

	if strings.Contains(
		out,
		"return ReadTransform((Transform *)data);",
	) {
		t.Fatalf(
			"multiple-return alias wrapper cannot directly return a differently named C result struct:\n%s",
			out,
		)
	}
}

func TestCastToDeclaredDynamicInterfaceWithoutDynTypeModifier(t *testing.T) {
	out, reporter := generate(t, `
Positioned :: dyn interface {
    X :: task(value *self) int
}

Transform :: struct {
    x int
}

XTransform :: task(value *Transform) int {
    return value.x
}

Positioned :: impl Transform {
    X :: XTransform
}

Main :: task() {
    transform := Transform{x = 42}
    positioned := cast<Positioned>(&transform)
    x := X(positioned)
    assert(x == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct Positioned_vtable Positioned_vtable;",
		"struct Positioned_vtable {",
		"const Positioned_vtable *vtable;",
		"static const Positioned_vtable Positioned_Transform_vtable",
		".vtable = &Positioned_Transform_vtable",
		"Positioned_X(positioned)",
		"return receiver.vtable->X(receiver.data);",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}
}

func TestInterfaceDispatchModeComesFromDeclaration(t *testing.T) {
	out, reporter := generate(t, `
StaticPositioned :: interface {
    X :: task(value *self) int
}

DynamicPositioned :: dyn interface {
    X :: task(value *self) int
}

Transform :: struct {
    x int
}

XTransform :: task(value *Transform) int {
    return value.x
}

StaticPositioned :: impl Transform {
    X :: XTransform
}

DynamicPositioned :: impl Transform {
    X :: XTransform
}

Main :: task() {
    transform := Transform{x = 42}

    staticPositioned := cast<StaticPositioned>(&transform)
    dynamicPositioned := cast<DynamicPositioned>(&transform)

    assert(X(staticPositioned) == 42)
    assert(X(dynamicPositioned) == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct StaticPositioned {",
		"uintptr_t tag;",
		"#define StaticPositioned_Tag_Transform",
		"switch (receiver.tag)",

		"typedef struct DynamicPositioned_vtable DynamicPositioned_vtable;",
		"struct DynamicPositioned_vtable {",
		"const DynamicPositioned_vtable *vtable;",
		"static const DynamicPositioned_vtable DynamicPositioned_Transform_vtable",
		".vtable = &DynamicPositioned_Transform_vtable",
		"DynamicPositioned_X(dynamicPositioned)",
		"return receiver.vtable->X(receiver.data);",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}
}

func TestGenerateChainedInterfaceDelegation(t *testing.T) {
	t.Skip("chained delegated implementation code generation is not supported yet")

	out, reporter := generate(t, `
Positioned :: interface {
	Position :: task(value *self) int
}

Transform :: struct {
	x int
}

ReadPosition :: task(transform *Transform) int {
	return transform.x
}

Positioned :: impl Transform {
	Position :: ReadPosition
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
			transform = Transform{x = 42},
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

	checks := []string{
		"Positioned_Transform_Position(void *data)",
		"Positioned_Components_Position(void *data)",
		"Positioned_Entity_Position(void *data)",

		"return Positioned_Transform_Position(",
		"->transform",

		"return Positioned_Components_Position(",
		"->components",

		".tag = Positioned_Tag_Entity",
		"Positioned_Position(positioned)",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}
}

func TestCompileGeneratedStaticDelegatedInterface(t *testing.T) {
	out, reporter := generate(t, `
Positioned :: interface {
	Position :: task(value *self) int
}

Transform :: struct {
	x int
}

ReadPosition :: task(transform *Transform) int {
	return transform.x
}

Positioned :: impl Transform {
	Position :: ReadPosition
}

Entity :: struct {
	transform Transform
}

Positioned :: impl Entity using transform

Main :: task() {
	entity := Entity{
		transform = Transform{x = 42},
	}

	positioned := cast<Positioned>(&entity)
	position := Position(positioned)

	assert(position == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	compileGeneratedC(t, out)
}

func TestCompileGeneratedDynamicDelegatedInterface(t *testing.T) {
	out, reporter := generate(t, `
Positioned :: dyn interface {
	Position :: task(value *self) int
	SetPosition :: task(value *self, position int)
}

Transform :: struct {
	x int
}

ReadPosition :: task(transform *Transform) int {
	return transform.x
}

WritePosition :: task(transform *Transform, position int) {
	transform.x = position
}

Positioned :: impl Transform {
	Position :: ReadPosition
	SetPosition :: WritePosition
}

Entity :: struct {
	transform Transform
}

Positioned :: impl Entity using transform

ReadAndUpdate :: task(positioned Positioned) int {
	before := Position(positioned)
	SetPosition(positioned, before + 1)
	return Position(positioned)
}

Main :: task() {
	entity := Entity{
		transform = Transform{x = 41},
	}

	positioned := cast<Positioned>(&entity)
	result := ReadAndUpdate(positioned)

	assert(result == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	compileGeneratedC(t, out)
}

func TestCompileGeneratedDelegatedMultipleReturnInterface(t *testing.T) {
	out, reporter := generate(t, `
Coordinates :: interface {
	Read :: task(value *self) int, int
}

Transform :: struct {
	x int
	y int
}

ReadTransform :: task(transform *Transform) int, int {
	return transform.x, transform.y
}

Coordinates :: impl Transform {
	Read :: ReadTransform
}

Entity :: struct {
	transform Transform
}

Coordinates :: impl Entity using transform

Main :: task() {
	entity := Entity{
		transform = Transform{
			x = 20,
			y = 22,
		},
	}

	coordinates := cast<Coordinates>(&entity)
	x, y := Read(coordinates)

	assert(x + y == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	compileGeneratedC(t, out)
}

func TestCompileGeneratedPointerIntermediateDelegation(t *testing.T) {
	out, reporter := generate(t, `
Positioned :: interface {
	Position :: task(self *self) int
}

Transform :: struct {
	position int
}

ReadPosition :: task(transform *Transform) int {
	return transform.position
}

Positioned :: impl Transform {
	Position :: ReadPosition
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

	compileGeneratedC(t, out)
}

func TestGenerateInterfaceAssignmentPassingAndReturn(t *testing.T) {
	out, reporter := generate(t, `
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
	copy := value
	return copy
}

ReadPosition :: task(value Positioned) int {
	return X(value)
}

Main :: task() {
	transform := Transform{x = 42}

	first := cast<Positioned>(&transform)
	second: Positioned = first
	third := IdentityPositioned(second)

	result := ReadPosition(third)

	assert(result == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct Positioned {",
		"uintptr_t tag;",
		"void *data;",
		"} Positioned;",

		"Positioned IdentityPositioned(Positioned value);",
		"intptr_t ReadPosition(Positioned value);",

		"Positioned first =",
		"Positioned second = first;",
		"Positioned third = IdentityPositioned(second);",
		"intptr_t result = ReadPosition(third);",

		"Positioned copy = value;",
		"Positioned_X(value)",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}

	compileGeneratedC(t, out)
}

func TestGenerateInterfaceStoredAndPassedFromStruct(t *testing.T) {
	out, reporter := generate(t, `
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

ReadHolder :: task(holder PositionHolder) int {
	return X(holder.value)
}

Main :: task() {
	transform := Transform{x = 42}
	positioned := cast<Positioned>(&transform)

	holder := PositionHolder{
		value = positioned,
	}

	result := ReadHolder(holder)

	assert(result == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"typedef struct PositionHolder {",
		"Positioned value;",
		"} PositionHolder;",

		"intptr_t ReadHolder(PositionHolder holder);",
		"PositionHolder holder = (PositionHolder){.value = positioned};",
		"Positioned_X((holder).value)",
		"intptr_t result = ReadHolder(holder);",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}

	compileGeneratedC(t, out)
}

func TestGenerateMultipleInterfaceParameters(t *testing.T) {
	out, reporter := generate(t, `
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

SumPositions :: task(left Positioned, right Positioned) int {
	return X(left) + X(right)
}

Main :: task() {
	leftTransform := Transform{x = 20}
	rightTransform := Transform{x = 22}

	left := cast<Positioned>(&leftTransform)
	right := cast<Positioned>(&rightTransform)

	result := SumPositions(left, right)

	assert(result == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"intptr_t SumPositions(Positioned left, Positioned right);",
		"Positioned_X(left)",
		"Positioned_X(right)",
		"SumPositions(left, right)",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}

	compileGeneratedC(t, out)
}

func TestGenerateEnumWithU32UnderlyingType(t *testing.T) {
	out, reporter := generate(t, `
ErrorCode :: enum u32 {
	Success
	NotFound
	Invalid
}

GetError :: task() ErrorCode {
	return .NotFound
}

Main :: task() {
	code: ErrorCode = .Invalid
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"typedef uint32_t ErrorCode;",
	) {
		t.Fatalf(
			"expected enum to use uint32_t storage, got:\n%s",
			out,
		)
	}

	if strings.Contains(
		out,
		"typedef enum ErrorCode",
	) {
		t.Fatalf(
			"fixed-underlying enum must not use a C enum as its storage type, got:\n%s",
			out,
		)
	}

	expectedVariants := []string{
		"ErrorCode_Success = 0",
		"ErrorCode_NotFound = 1",
		"ErrorCode_Invalid = 2",
	}

	for _, expected := range expectedVariants {
		if !strings.Contains(out, expected) {
			t.Fatalf(
				"expected generated enum variant %q, got:\n%s",
				expected,
				out,
			)
		}
	}

	if !strings.Contains(
		out,
		"ErrorCode GetError(void)",
	) {
		t.Fatalf(
			"expected enum return type to use uint32_t, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"= ErrorCode_NotFound;",
	) {
		t.Fatalf(
			"expected contextual enum return literal, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"ErrorCode code = ErrorCode_Invalid;",
	) {
		t.Fatalf(
			"expected contextual enum variable initialization, got:\n%s",
			out,
		)
	}
}

func TestGenerateEnumUnderlyingIntegerTypes(t *testing.T) {
	tests := []struct {
		name     string
		sealType string
		cType    string
		enumName string
		variant  string
	}{
		{
			name:     "u8",
			sealType: "u8",
			cType:    "uint8_t",
			enumName: "EnumU8",
			variant:  "ValueU8",
		},
		{
			name:     "u16",
			sealType: "u16",
			cType:    "uint16_t",
			enumName: "EnumU16",
			variant:  "ValueU16",
		},
		{
			name:     "u32",
			sealType: "u32",
			cType:    "uint32_t",
			enumName: "EnumU32",
			variant:  "ValueU32",
		},
		{
			name:     "u64",
			sealType: "u64",
			cType:    "uint64_t",
			enumName: "EnumU64",
			variant:  "ValueU64",
		},
		{
			name:     "i8",
			sealType: "i8",
			cType:    "int8_t",
			enumName: "EnumI8",
			variant:  "ValueI8",
		},
		{
			name:     "i16",
			sealType: "i16",
			cType:    "int16_t",
			enumName: "EnumI16",
			variant:  "ValueI16",
		},
		{
			name:     "i32",
			sealType: "i32",
			cType:    "int32_t",
			enumName: "EnumI32",
			variant:  "ValueI32",
		},
		{
			name:     "i64",
			sealType: "i64",
			cType:    "int64_t",
			enumName: "EnumI64",
			variant:  "ValueI64",
		},
		{
			name:     "int",
			sealType: "int",
			cType:    "intptr_t",
			enumName: "EnumInt",
			variant:  "ValueInt",
		},
		{
			name:     "uint",
			sealType: "uint",
			cType:    "uintptr_t",
			enumName: "EnumUint",
			variant:  "ValueUint",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := fmt.Sprintf(`
%s :: enum %s {
	%s
}

Main :: task() {
	value: %s = .%s
}
`,
				test.enumName,
				test.sealType,
				test.variant,
				test.enumName,
				test.variant,
			)

			out, reporter := generate(t, input)

			if reporter.HasErrors() {
				t.Fatalf(
					"unexpected diagnostics:\n%s",
					reporter.String(),
				)
			}

			expectedTypedef := fmt.Sprintf(
				"typedef %s %s;",
				test.cType,
				test.enumName,
			)

			if !strings.Contains(out, expectedTypedef) {
				t.Fatalf(
					"expected %q, got:\n%s",
					expectedTypedef,
					out,
				)
			}

			expectedVariable := fmt.Sprintf(
				"%s value = %s_%s;",
				test.enumName,
				test.enumName,
				test.variant,
			)

			if !strings.Contains(out, expectedVariable) {
				t.Fatalf(
					"expected %q, got:\n%s",
					expectedVariable,
					out,
				)
			}
		})
	}
}

func TestGenerateEnumWithoutUnderlyingTypeStillUsesCEnum(
	t *testing.T,
) {
	out, reporter := generate(t, `
Direction :: enum {
	North
	South
}

Main :: task() {
	direction: Direction = .South
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"typedef enum Direction {",
	) {
		t.Fatalf(
			"expected ordinary enum to retain C enum lowering, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"Direction_North",
	) ||
		!strings.Contains(
			out,
			"Direction_South",
		) {
		t.Fatalf(
			"expected ordinary enum variants, got:\n%s",
			out,
		)
	}

	if strings.Contains(
		out,
		"typedef uint32_t Direction;",
	) {
		t.Fatalf(
			"ordinary enum unexpectedly received fixed uint32 storage, got:\n%s",
			out,
		)
	}
}

func TestGenerateEnumUnderlyingTypeInSwitch(t *testing.T) {
	out, reporter := generate(t, `
State :: enum u8 {
	Waiting
	Running
	Stopped
}

Value :: task(state State) int {
	switch state {
	case .Waiting:
		return 1
	case .Running:
		return 2
	case .Stopped:
		return 3
	}

	return 0
}

Main :: task() {
	state: State = .Running
	value := Value(state)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"typedef uint8_t State;",
	) {
		t.Fatalf(
			"expected u8 enum storage, got:\n%s",
			out,
		)
	}

	expectedCases := []string{
		"case State_Waiting:",
		"case State_Running:",
		"case State_Stopped:",
	}

	for _, expected := range expectedCases {
		if !strings.Contains(out, expected) {
			t.Fatalf(
				"expected switch case %q, got:\n%s",
				expected,
				out,
			)
		}
	}
}

func TestGenerateDeferBlockRunsAtScopeExit(t *testing.T) {
	out, reporter := generate(t, `
Main :: task() {
	value := 1

	defer {
		value = value + 10
	}

	value = 2
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	ordinaryAssignment := "value = 2;"
	deferredAssignment := "value = (value + 10);"

	ordinaryIndex := strings.Index(
		out,
		ordinaryAssignment,
	)

	deferredIndex := strings.Index(
		out,
		deferredAssignment,
	)

	if ordinaryIndex < 0 {
		t.Fatalf(
			"expected ordinary assignment %q, got:\n%s",
			ordinaryAssignment,
			out,
		)
	}

	if deferredIndex < 0 {
		t.Fatalf(
			"expected deferred block assignment %q, got:\n%s",
			deferredAssignment,
			out,
		)
	}

	if deferredIndex < ordinaryIndex {
		t.Fatalf(
			"deferred block ran before the remaining scope statements, got:\n%s",
			out,
		)
	}

	if strings.Contains(
		out,
		"__seal_defer_arg_",
	) {
		t.Fatalf(
			"block-form defer must not capture call arguments, got:\n%s",
			out,
		)
	}
}

func TestGenerateDeferBlockUsesExitTimeValues(t *testing.T) {
	out, reporter := generate(t, `
Observe :: task(value int) {
}

Main :: task() {
	value := 1

	defer {
		Observe(value)
	}

	value = 2
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	assignmentIndex := strings.Index(
		out,
		"value = 2;",
	)

	deferredCallIndex := strings.LastIndex(
		out,
		"Observe(value);",
	)

	if assignmentIndex < 0 {
		t.Fatalf(
			"expected value assignment, got:\n%s",
			out,
		)
	}

	if deferredCallIndex < 0 {
		t.Fatalf(
			"expected deferred Observe(value) call, got:\n%s",
			out,
		)
	}

	if deferredCallIndex < assignmentIndex {
		t.Fatalf(
			"deferred block call must be emitted after value mutation, got:\n%s",
			out,
		)
	}

	if strings.Contains(
		out,
		"__seal_defer_arg_",
	) {
		t.Fatalf(
			"block-form defer unexpectedly captured its argument, got:\n%s",
			out,
		)
	}
}

func TestGenerateDeferBlocksRunInLIFOOrder(t *testing.T) {
	out, reporter := generate(t, `
Main :: task() {
	value := 1

	defer {
		value = value + 10
	}

	defer {
		value = value * 2
	}

	value = 3
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	bodyAssignment := strings.Index(
		out,
		"value = 3;",
	)

	secondDefer := strings.Index(
		out,
		"value = (value * 2);",
	)

	firstDefer := strings.Index(
		out,
		"value = (value + 10);",
	)

	if bodyAssignment < 0 ||
		secondDefer < 0 ||
		firstDefer < 0 {
		t.Fatalf(
			"expected body and deferred assignments, got:\n%s",
			out,
		)
	}

	if !(bodyAssignment < secondDefer &&
		secondDefer < firstDefer) {
		t.Fatalf(
			"expected deferred blocks in LIFO order, got:\n%s",
			out,
		)
	}
}

func TestGenerateMixedCallAndBlockDefersInLIFOOrder(
	t *testing.T,
) {
	out, reporter := generate(t, `
Record :: task(value int) {
}

Main :: task() {
	value := 1

	defer Record(value)

	defer {
		value = value + 10
	}

	value = 2
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	captureIndex := strings.Index(
		out,
		"__seal_defer_arg_",
	)

	assignmentIndex := strings.Index(
		out,
		"value = 2;",
	)

	blockDeferIndex := strings.Index(
		out,
		"value = (value + 10);",
	)

	callDeferIndex := strings.LastIndex(
		out,
		"Record(__seal_defer_arg_",
	)

	if captureIndex < 0 ||
		assignmentIndex < 0 ||
		blockDeferIndex < 0 ||
		callDeferIndex < 0 {
		t.Fatalf(
			"expected captured call defer and deferred block, got:\n%s",
			out,
		)
	}

	if !(captureIndex < assignmentIndex &&
		assignmentIndex < blockDeferIndex &&
		blockDeferIndex < callDeferIndex) {
		t.Fatalf(
			"expected mixed defers to execute in LIFO order, got:\n%s",
			out,
		)
	}
}

func TestGenerateDeferBlockAtNestedScopeExit(t *testing.T) {
	out, reporter := generate(t, `
Main :: task() {
	value := 1

	{
		defer {
			value = value + 10
		}

		value = value + 2
	}

	value = value + 4
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	innerBody := strings.Index(
		out,
		"value = (value + 2);",
	)

	innerDefer := strings.Index(
		out,
		"value = (value + 10);",
	)

	outerBody := strings.Index(
		out,
		"value = (value + 4);",
	)

	if innerBody < 0 ||
		innerDefer < 0 ||
		outerBody < 0 {
		t.Fatalf(
			"expected nested-scope statements, got:\n%s",
			out,
		)
	}

	if !(innerBody < innerDefer &&
		innerDefer < outerBody) {
		t.Fatalf(
			"expected inner defer before continuing the outer scope, got:\n%s",
			out,
		)
	}
}

func TestGenerateDeferBlockBeforeReturn(t *testing.T) {
	out, reporter := generate(t, `
Compute :: task() int {
	value := 1

	defer {
		value = value + 10
	}

	return value
}

Main :: task() {
	result := Compute()
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	returnValueIndex := strings.Index(
		out,
		"= value;",
	)

	deferredIndex := strings.Index(
		out,
		"value = (value + 10);",
	)

	returnIndex := strings.Index(
		out,
		"return __seal_return_value_",
	)

	if returnValueIndex < 0 ||
		deferredIndex < 0 ||
		returnIndex < 0 {
		t.Fatalf(
			"expected return capture, deferred block, and return, got:\n%s",
			out,
		)
	}

	if !(returnValueIndex < deferredIndex &&
		deferredIndex < returnIndex) {
		t.Fatalf(
			"expected return expression to be captured before defer and returned afterward, got:\n%s",
			out,
		)
	}
}

func TestGenerateGenericOverloadByArgumentCategory(t *testing.T) {
	out, reporter := generate(t, `
SelectType :: task <T type>() int {
	return 1
}

SelectInt :: task <N int>() int {
	return N
}

Select :: overload {
	SelectType
	SelectInt
}

Main :: task() {
	fromType := Select<int>()
	fromInt := Select<42>()

	assert(fromType == 1)
	assert(fromInt == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	checks := []string{
		"intptr_t SelectType_int(void);",
		"intptr_t SelectInt_42(void);",

		"intptr_t fromType = SelectType_int();",
		"intptr_t fromInt = SelectInt_42();",

		"intptr_t SelectType_int(void) {",
		"intptr_t SelectInt_42(void) {",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}

	if strings.Contains(out, "Select_int(") ||
		strings.Contains(out, "Select_42(") {
		t.Fatalf(
			"generic overload name itself must not be specialized:\n%s",
			out,
		)
	}

	compileGeneratedC(t, out)
}

func TestGenerateMixedOrdinaryAndGenericOverloadCandidates(
	t *testing.T,
) {
	out, reporter := generate(t, `
ProcessBool :: task(value bool) bool {
	return value
}

ProcessType :: task <T type>(value T) T {
	return value
}

ProcessCount :: task <N int>(value int) int {
	return value + N
}

Process :: overload {
	ProcessType
	ProcessCount
	ProcessBool
}

Main :: task() {
	boolean := Process(true)
	typed := Process<int>(10)
	counted := Process<32>(10)

	assert(boolean)
	assert(typed == 10)
	assert(counted == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	checks := []string{
		"bool ProcessBool(bool value);",
		"intptr_t ProcessType_int(intptr_t value);",
		"intptr_t ProcessCount_32(intptr_t value);",

		"bool boolean = ProcessBool(true);",
		"intptr_t typed = ProcessType_int(10);",
		"intptr_t counted = ProcessCount_32(10);",

		"intptr_t ProcessType_int(intptr_t value) {",
		"intptr_t ProcessCount_32(intptr_t value) {",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}

	compileGeneratedC(t, out)
}

func TestGenerateGenericOverloadInsideGenericTaskSpecialization(
	t *testing.T,
) {
	out, reporter := generate(t, `
SelectType :: task <T type>(value T) T {
    return value
}

SelectCount :: task <N int>(value int) int {
    return value + N
}

Select :: overload {
    SelectType
    SelectCount
}

Forward :: task <T type>(value T) T {
    return Select<T>(value)
}

Main :: task() {
    integer := Forward<int>(42)
    text := Forward<string>("hello")

    assert(integer == 42)
    assert(size(text) > 0)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	checks := []string{
		"intptr_t SelectType_int(intptr_t value);",
		"sealString SelectType_string(sealString value);",

		"intptr_t Forward_int(intptr_t value);",
		"sealString Forward_string(sealString value);",

		"SelectType_int(value)",
		"SelectType_string(value)",

		"intptr_t integer = Forward_int(42);",

		`sealString text = Forward_string((sealString){.data = (const uint8_t *)"hello", .len = (uintptr_t)5});`,
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}

	if strings.Contains(out, "Select_T(") {
		t.Fatalf(
			"generic overload call retained an unspecialized type parameter:\n%s",
			out,
		)
	}

	compileGeneratedC(t, out)
}

func TestGenerateGenericOverloadDefaultParameters(t *testing.T) {
	out, reporter := generate(t, `
DefaultType :: task <T type>(
	value T = cast<T>(7),
) T {
	return value
}

DefaultCount :: task <N int>(
	value int = N,
) int {
	return value
}

Default :: overload {
	DefaultType
	DefaultCount
}

Main :: task() {
	typed := Default<int>()
	counted := Default<42>()

	assert(typed == 7)
	assert(counted == 42)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	checks := []string{
		"intptr_t DefaultType_int(intptr_t value);",
		"intptr_t DefaultCount_42(intptr_t value);",

		"intptr_t typed = DefaultType_int(((intptr_t)(7)));",
		"intptr_t counted = DefaultCount_42(42);",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}

	compileGeneratedC(t, out)
}

func TestGenerateGenericOverloadMultipleReturns(t *testing.T) {
	out, reporter := generate(t, `
PairType :: task <T type>(left T, right T) T, T {
	return left, right
}

PairCount :: task <N int>(left int, right int) int, int {
	return left + N, right + N
}

Pair :: overload {
	PairType
	PairCount
}

Main :: task() {
	typeLeft, typeRight := Pair<int>(1, 2)
	countLeft, countRight := Pair<10>(1, 2)

	assert(typeLeft == 1)
	assert(typeRight == 2)
	assert(countLeft == 11)
	assert(countRight == 12)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	checks := []string{
		"typedef struct PairType_int_Result {",
		"typedef struct PairCount_10_Result {",

		"PairType_int_Result PairType_int(intptr_t left, intptr_t right);",
		"PairCount_10_Result PairCount_10(intptr_t left, intptr_t right);",

		"= PairType_int(1, 2);",
		"= PairCount_10(1, 2);",

		"intptr_t typeLeft = __seal_multi_result_",
		"intptr_t typeRight = __seal_multi_result_",
		"intptr_t countLeft = __seal_multi_result_",
		"intptr_t countRight = __seal_multi_result_",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}

	if strings.Contains(out, "Pair_Result") {
		t.Fatalf(
			"overload declaration must not emit a result structure:\n%s",
			out,
		)
	}

	compileGeneratedC(t, out)
}

func TestGenerateGenericOverloadTaskArgumentCategory(t *testing.T) {
	out, reporter := generate(t, `
Double :: task(value int) int {
	return value * 2
}

ApplyTask :: task <F task[(int) int]>(value int) int {
	return F(value)
}

ApplyType :: task <T type>(value T) T {
	return value
}

Apply :: overload {
	ApplyTask
	ApplyType
}

Main :: task() {
	fromTask := Apply<Double>(21)
	fromType := Apply<int>(21)

	assert(fromTask == 42)
	assert(fromType == 21)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	checks := []string{
		"intptr_t Double(intptr_t value);",
		"intptr_t ApplyTask_Double(intptr_t value);",
		"intptr_t ApplyType_int(intptr_t value);",

		"intptr_t fromTask = ApplyTask_Double(21);",
		"intptr_t fromType = ApplyType_int(21);",

		"intptr_t ApplyTask_Double(intptr_t value) {",
		"= Double(value);",

		"intptr_t ApplyType_int(intptr_t value) {",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}

	if strings.Contains(out, "Apply_Double(") ||
		strings.Contains(out, "Apply_int(") {
		t.Fatalf(
			"overload name itself must not be specialized:\n%s",
			out,
		)
	}

	compileGeneratedC(t, out)
}

func TestImportedGenericOverloadSpecializationCodegen(
	t *testing.T,
) {
	rulesPkg,
		rulesResolverPkg,
		rulesCheckerPkg := exportCGenPackage(
		t,
		"rules",
		`
ChooseType :: task <T type>(value T) T {
	return value
}

ChooseCount :: task <N int>(value int) int {
	return value + N
}

Choose :: overload {
	ChooseType
	ChooseCount
}
`,
	)

	out, reporter := generateWithPackages(
		t,
		`
Main :: task() {
	typed := rules.Choose<int>(10)
	counted := rules.Choose<32>(10)

	assert(typed == 10)
	assert(counted == 42)
}
`,
		map[string]*PackageInfo{
			"rules": rulesPkg,
		},
		map[string]*resolver.PackageInfo{
			"rules": rulesResolverPkg,
		},
		map[string]*checker.PackageInfo{
			"rules": rulesCheckerPkg,
		},
	)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	checks := []string{
		"intptr_t rules_ChooseType_int(intptr_t value);",
		"intptr_t rules_ChooseCount_32(intptr_t value);",

		"intptr_t typed = rules_ChooseType_int(10);",
		"intptr_t counted = rules_ChooseCount_32(10);",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}

	if strings.Contains(out, "rules_Choose_int(") ||
		strings.Contains(out, "rules_Choose_32(") {
		t.Fatalf(
			"imported overload name itself must not be specialized:\n%s",
			out,
		)
	}

	compileGeneratedC(t, out)
}

func TestImportedGenericOverloadRecordsSelectedInstanceRequest(
	t *testing.T,
) {
	rulesPkg,
		rulesResolverPkg,
		rulesCheckerPkg := exportCGenPackage(
		t,
		"rules",
		`
ChooseType :: task <T type>(value T) T {
	return value
}

ChooseCount :: task <N int>(value int) int {
	return value + N
}

Choose :: overload {
	ChooseType
	ChooseCount
}
`,
	)

	file := source.NewFile(
		"test.seal",
		`
Main :: task() {
	value := rules.Choose<int>(10)
}
`,
	)

	reporter := diag.NewReporter()

	lex := lexer.New(file, reporter)
	tokens := lex.LexAll()

	if reporter.HasErrors() {
		t.Fatalf(
			"lexer diagnostics:\n%s",
			reporter.String(),
		)
	}

	p := parser.New(tokens, reporter)
	parsed := p.ParseFile()

	if reporter.HasErrors() {
		t.Fatalf(
			"parser diagnostics:\n%s",
			reporter.String(),
		)
	}

	r := resolver.NewWithPackages(
		reporter,
		map[string]*resolver.PackageInfo{
			"rules": rulesResolverPkg,
		},
	)
	r.ResolveFile(parsed)

	if reporter.HasErrors() {
		t.Fatalf(
			"resolver diagnostics:\n%s",
			reporter.String(),
		)
	}

	c := checker.NewWithPackages(
		reporter,
		map[string]*checker.PackageInfo{
			"rules": rulesCheckerPkg,
		},
	)
	c.CheckFile(parsed)

	if reporter.HasErrors() {
		t.Fatalf(
			"checker diagnostics:\n%s",
			reporter.String(),
		)
	}

	g := NewWithPackagesAndSemanticInfo(
		reporter,
		"",
		map[string]*PackageInfo{
			"rules": rulesPkg,
		},
		c.SemanticInfo(),
	)

	_ = g.Generate(parsed)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected CGen diagnostics:\n%s",
			reporter.String(),
		)
	}

	requests := g.RequestedGenericInstances()

	var selectedCandidate bool
	var overloadName bool
	var wrongCandidate bool

	for _, request := range requests {
		if request.Kind != GenericInstanceTask ||
			request.PackageName != "rules" {
			continue
		}

		switch request.SymbolName {
		case "ChooseType":
			selectedCandidate = true

		case "Choose":
			overloadName = true

		case "ChooseCount":
			wrongCandidate = true
		}
	}

	if !selectedCandidate {
		t.Fatalf(
			"expected request for selected task rules.ChooseType<int>, got %#v",
			requests,
		)
	}

	if overloadName {
		t.Fatalf(
			"must not request specialization of overload rules.Choose, got %#v",
			requests,
		)
	}

	if wrongCandidate {
		t.Fatalf(
			"must not request unselected candidate rules.ChooseCount, got %#v",
			requests,
		)
	}
}

func TestGenerateBreakAndContinueLoopControl(t *testing.T) {
	out, reporter := generate(t, `
Main :: task() {
	sum := 0

	for i := 0; i < 10; i += 1 {
		if i == 2 {
			continue
		}

		if i == 5 {
			break
		}

		sum += i
	}

	assert(sum == 8)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	checks := []string{
		"for (intptr_t i = 0; (i < 10); i += 1) {",
		"goto __seal_loop_continue_",
		"goto __seal_loop_break_",
		"__seal_loop_continue_",
		"__seal_loop_break_",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf(
				"expected generated C to contain %q, got:\n%s",
				want,
				out,
			)
		}
	}

	runGeneratedC(t, out)
}

func TestGeneratedContinueExecutesForPostStatement(
	t *testing.T,
) {
	out, reporter := generate(t, `
Main :: task() {
	i := 0
	iterations := 0

	for i = 0; i < 5; i += 1 {
		if i < 4 {
			iterations += 1
			continue
		}

		iterations += 1
	}

	assert(i == 5)
	assert(iterations == 5)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	continueJump := strings.Index(
		out,
		"goto __seal_loop_continue_",
	)

	if continueJump < 0 {
		t.Fatalf(
			"expected continue to use a generated loop label, got:\n%s",
			out,
		)
	}

	continueLabel := strings.Index(
		out[continueJump:],
		": ;",
	)

	if continueLabel < 0 {
		t.Fatalf(
			"expected generated continue label, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"for (i = 0; (i < 5); i += 1) {",
	) {
		t.Fatalf(
			"expected loop post expression to remain in the C for statement, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGeneratedBreakInsideSwitchExitsForLoop(
	t *testing.T,
) {
	out, reporter := generate(t, `
LoopAction :: enum {
	KeepGoing
	Stop
}

Main :: task() {
	result := 0

	for i := 0; i < 10; i += 1 {
		action: LoopAction = .KeepGoing

		if i == 3 {
			action = .Stop
		}

		switch action {
		case .Stop:
			break

		case .KeepGoing:
			result += 1
		}
	}

	assert(result == 3)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	switchIndex := strings.Index(
		out,
		"switch (action)",
	)

	breakJumpIndex := strings.Index(
		out,
		"goto __seal_loop_break_",
	)

	breakLabelIndex := strings.LastIndex(
		out,
		"__seal_loop_break_",
	)

	if switchIndex < 0 {
		t.Fatalf(
			"expected generated enum switch, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"case LoopAction_Stop:",
	) {
		t.Fatalf(
			"expected Stop enum case, got:\n%s",
			out,
		)
	}

	if breakJumpIndex < 0 {
		t.Fatalf(
			"expected source-level break to jump to the for-loop break label, got:\n%s",
			out,
		)
	}

	if breakJumpIndex < switchIndex {
		t.Fatalf(
			"expected loop-break jump inside the generated switch, got:\n%s",
			out,
		)
	}

	if breakLabelIndex <= breakJumpIndex {
		t.Fatalf(
			"expected loop-break label after the generated switch and loop, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGeneratedNestedLoopControlTargetsInnermostLoop(
	t *testing.T,
) {
	out, reporter := generate(t, `
Main :: task() {
	total := 0

	for outer := 0; outer < 3; outer += 1 {
		for inner := 0; inner < 5; inner += 1 {
			if inner == 1 {
				continue
			}

			if inner == 3 {
				break
			}

			total += 1
		}
	}

	assert(total == 6)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if strings.Count(
		out,
		"goto __seal_loop_continue_",
	) != 1 {
		t.Fatalf(
			"expected one source-level continue jump, got:\n%s",
			out,
		)
	}

	if strings.Count(
		out,
		"goto __seal_loop_break_",
	) != 1 {
		t.Fatalf(
			"expected one source-level break jump, got:\n%s",
			out,
		)
	}

	if strings.Count(
		out,
		": ;",
	) < 4 {
		t.Fatalf(
			"expected break and continue labels for both nested loops, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGeneratedLoopControlRunsDefersBeforeExit(
	t *testing.T,
) {
	out, reporter := generate(t, `
Main :: task() {
	total := 0

	for i := 0; i < 5; i += 1 {
		defer {
			total += 100
		}

		if i == 1 {
			continue
		}

		total += 1

		if i == 2 {
			break
		}
	}

	assert(total == 302)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	continueJump := strings.Index(
		out,
		"goto __seal_loop_continue_",
	)

	breakJump := strings.Index(
		out,
		"goto __seal_loop_break_",
	)

	if continueJump < 0 {
		t.Fatalf(
			"expected generated continue jump, got:\n%s",
			out,
		)
	}

	if breakJump < 0 {
		t.Fatalf(
			"expected generated break jump, got:\n%s",
			out,
		)
	}

	deferBeforeContinue := strings.LastIndex(
		out[:continueJump],
		"total += 100;",
	)

	if deferBeforeContinue < 0 {
		t.Fatalf(
			"expected loop defer before continue jump, got:\n%s",
			out,
		)
	}

	deferBeforeBreak := strings.LastIndex(
		out[:breakJump],
		"total += 100;",
	)

	if deferBeforeBreak < 0 {
		t.Fatalf(
			"expected loop defer before break jump, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGeneratedBreakFromNestedBlockRunsAllExitedDefers(
	t *testing.T,
) {
	out, reporter := generate(t, `
Main :: task() {
	value := 0

	for {
		defer {
			value += 10
		}

		{
			defer {
				value += 1
			}

			break
		}
	}

	assert(value == 11)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	breakJump := strings.Index(
		out,
		"goto __seal_loop_break_",
	)

	if breakJump < 0 {
		t.Fatalf(
			"expected generated break jump, got:\n%s",
			out,
		)
	}

	beforeBreak := out[:breakJump]

	innerDefer := strings.LastIndex(
		beforeBreak,
		"value += 1;",
	)

	loopDefer := strings.LastIndex(
		beforeBreak,
		"value += 10;",
	)

	if innerDefer < 0 {
		t.Fatalf(
			"expected nested block defer before break, got:\n%s",
			out,
		)
	}

	if loopDefer < 0 {
		t.Fatalf(
			"expected loop-scope defer before break, got:\n%s",
			out,
		)
	}

	if innerDefer >= loopDefer {
		t.Fatalf(
			"expected inner defer to execute before loop defer during break, got:\n%s",
			out,
		)
	}

	if loopDefer >= breakJump {
		t.Fatalf(
			"expected both exited-scope defers before the break jump, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGenerateInlineArrayVariable(t *testing.T) {
	out, reporter := generate(t, `
Main :: task() {
    values := @inline_array<int, 4>(
        10,
        20,
        30,
        40,
    )
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"intptr_t values[4] = {10, 20, 30, 40};",
	) {
		t.Fatalf(
			"expected inline C array declaration, got:\n%s",
			out,
		)
	}

	compileGeneratedC(t, out)
}

func TestGenerateNestedInlineArrayVariable(t *testing.T) {
	out, reporter := generate(t, `
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

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"intptr_t matrix[2][3] = {{1, 2, 3}, {4, 5, 6}};",
	) {
		t.Fatalf(
			"expected nested inline C array declaration, got:\n%s",
			out,
		)
	}

	compileGeneratedC(t, out)
}

func TestGenerateThreeDimensionalInlineArray(t *testing.T) {
	out, reporter := generate(t, `
Main :: task() {
    values := @inline_array<
        @inline_array<
            @inline_array<int, 2>,
            3
        >,
        2
    >(
        @inline_array<
            @inline_array<int, 2>,
            3
        >(
            @inline_array<int, 2>(1, 2),
            @inline_array<int, 2>(3, 4),
            @inline_array<int, 2>(5, 6),
        ),
        @inline_array<
            @inline_array<int, 2>,
            3
        >(
            @inline_array<int, 2>(7, 8),
            @inline_array<int, 2>(9, 10),
            @inline_array<int, 2>(11, 12),
        ),
    )
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"intptr_t values[2][3][2]",
	) {
		t.Fatalf(
			"expected recursive three-dimensional declarator, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"{{{1, 2}, {3, 4}, {5, 6}}, {{7, 8}, {9, 10}, {11, 12}}}",
	) {
		t.Fatalf(
			"expected recursive three-dimensional initializer, got:\n%s",
			out,
		)
	}

	compileGeneratedC(t, out)
}

func TestGenerateInlineArrayIndexRead(t *testing.T) {
	out, reporter := generate(t, `
Main :: task() {
    values := @inline_array<int, 4>(
        10,
        20,
        30,
        40,
    )

    value := values[2]

    assert(value == 30)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"intptr_t value = (values)[2];",
	) {
		t.Fatalf(
			"expected inline-array index read, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGenerateNestedInlineArrayIndexRead(t *testing.T) {
	out, reporter := generate(t, `
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

    value := matrix[1][2]

    assert(value == 6)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"intptr_t value = ((matrix)[1])[2];",
	) {
		t.Fatalf(
			"expected nested inline-array index read, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGenerateNestedInlineArrayIndexWrite(t *testing.T) {
	out, reporter := generate(t, `
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

    matrix[0][1] = 99

    assert(matrix[0][1] == 99)
    assert(matrix[1][2] == 6)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"((matrix)[0])[1] = 99;",
	) {
		t.Fatalf(
			"expected nested inline-array index assignment, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGenerateInlineArrayLen(t *testing.T) {
	out, reporter := generate(t, `
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

    rows := len(matrix)
    columns := len(matrix[0])

    assert(rows == 2)
    assert(columns == 3)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"uintptr_t rows = ((uintptr_t)2);",
	) {
		t.Fatalf(
			"expected compile-time outer inline-array length, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"uintptr_t columns = ((uintptr_t)3);",
	) {
		t.Fatalf(
			"expected compile-time inner inline-array length, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGenerateInlineArrayNamedConstantLength(t *testing.T) {
	out, reporter := generate(t, `
ColumnCount :: 3

Main :: task() {
    values := @inline_array<int, ColumnCount>(
        10,
        20,
        30,
    )

    assert(len(values) == 3)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"intptr_t values[3] = {10, 20, 30};",
	) {
		t.Fatalf(
			"expected constant inline-array length to lower to 3, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGenerateInlineArrayConstantExpressionLength(t *testing.T) {
	out, reporter := generate(t, `
Base :: 2
Extra :: 1

Main :: task() {
    values := @inline_array<int, Base + Extra>(
        10,
        20,
        30,
    )

    assert(len(values) == 3)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"intptr_t values[3] = {10, 20, 30};",
	) {
		t.Fatalf(
			"expected evaluated inline-array constant length, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGenerateZeroLengthInlineArray(t *testing.T) {
	out, reporter := generate(t, `
Main :: task() {
    values := @inline_array<int, 0>()

    assert(len(values) == 0)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"intptr_t values[1] = {0};",
	) {
		t.Fatalf(
			"expected zero-length inline array to use physical C storage of one element, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"((uintptr_t)0)",
	) {
		t.Fatalf(
			"expected logical inline-array length to remain zero, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGenerateStructContainingInlineArray(t *testing.T) {
	out, reporter := generate(t, `
Buffer :: struct {
    data @inline_array<int, 4>
}

Main :: task() {
    buffer := Buffer{
        data = @inline_array<int, 4>(
            10,
            20,
            30,
            40,
        ),
    }

    assert(buffer.data[0] == 10)
    assert(buffer.data[3] == 40)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"intptr_t data[4];",
	) {
		t.Fatalf(
			"expected inline array inside struct declaration, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		".data = {10, 20, 30, 40}",
	) {
		t.Fatalf(
			"expected brace initializer for inline-array struct field, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGenerateNestedInlineArrayStructField(t *testing.T) {
	out, reporter := generate(t, `
Matrix :: struct {
    data @inline_array<
        @inline_array<int, 3>,
        2
    >
}

Main :: task() {
    matrix := Matrix{
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

    assert(matrix.data[1][2] == 6)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"intptr_t data[2][3];",
	) {
		t.Fatalf(
			"expected nested inline-array struct field declaration, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		".data = {{1, 2, 3}, {4, 5, 6}}",
	) {
		t.Fatalf(
			"expected nested brace initializer for struct field, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGenerateGenericStructContainingNestedInlineArray(
	t *testing.T,
) {
	out, reporter := generate(t, `
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

    assert(matrix.data[0][0] == 1)
    assert(matrix.data[1][2] == 6)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"intptr_t data[2][3];",
	) {
		t.Fatalf(
			"expected specialized generic nested inline-array field, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGenerateInlineArrayInsideReturnedStruct(t *testing.T) {
	out, reporter := generate(t, `
StackArray :: struct<
    T type,
    N int[N >= 0],
> {
    data @inline_array<T, N>
}

Create :: task() StackArray<int, 4> {
    return StackArray<int, 4>{
        data = @inline_array<int, 4>(
            10,
            20,
            30,
            40,
        ),
    }
}

Main :: task() {
    values := Create()

    assert(values.data[0] == 10)
    assert(values.data[3] == 40)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"intptr_t data[4];",
	) {
		t.Fatalf(
			"expected inline storage in returned wrapper struct, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGenerateNestedInlineArrayEndToEnd(t *testing.T) {
	out, reporter := generate(t, `
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

    matrix.data[0][1] = 99

    rows := len(matrix.data)
    columns := len(matrix.data[0])

    assert(matrix.data[0][0] == 1)
    assert(matrix.data[0][1] == 99)
    assert(matrix.data[1][2] == 6)
    assert(rows == 2)
    assert(columns == 3)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"intptr_t data[2][3];",
	) {
		t.Fatalf(
			"expected nested inline storage declaration, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		".data = {{1, 2, 3}, {4, 5, 6}}",
	) {
		t.Fatalf(
			"expected recursive brace initializer, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"(((matrix).data)[0])[1] = 99;",
	) {
		t.Fatalf(
			"expected nested inline-array write, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGenerateInlineArrayOfOverloadedIndexRows(
	t *testing.T,
) {
	out, reporter := generate(t, `
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

    value := matrix[1][2]

    matrix[0][1] = 99

    rows := len(matrix)
    columns := len(matrix[0])

    assert(value == 6)
    assert(matrix[0][0] == 1)
    assert(matrix[0][1] == 99)
    assert(matrix[1][2] == 6)
    assert(rows == 2)
    assert(columns == 3)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"intptr_t data[3];",
	) {
		t.Fatalf(
			"expected inline-array storage inside Row, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"Row matrix[2] = {first, second};",
	) {
		t.Fatalf(
			"expected outer inline-array matrix declaration, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"RowAt(&((matrix)[1]), 2)",
	) {
		t.Fatalf(
			"expected built-in outer indexing followed by overloaded inner read, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"RowSet(&((matrix)[0]), 1, 99)",
	) {
		t.Fatalf(
			"expected built-in outer indexing followed by overloaded inner write, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"uintptr_t rows = ((uintptr_t)2);",
	) {
		t.Fatalf(
			"expected built-in outer inline-array len, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"RowLen(&((matrix)[0]))",
	) {
		t.Fatalf(
			"expected overloaded len for indexed Row value, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGenerateMixedBuiltinAndOverloadedDoubleIndex(
	t *testing.T,
) {
	out, reporter := generate(t, `
DynamicRow :: struct {
    values @inline_array<int, 4>
    length uint
}

DynamicRowAt :: pure task(
    row *DynamicRow,
    index int,
) int {
    return row.values[index]
}

DynamicRowSet :: task(
    row *DynamicRow,
    index int,
    value int,
) {
    row.values[index] = value
}

[] :: overload {
    DynamicRowAt
}

[]= :: overload {
    DynamicRowSet
}

Main :: task() {
    row0 := DynamicRow{
        values = @inline_array<int, 4>(
            10,
            20,
            30,
            40,
        ),
        length = 4,
    }

    row1 := DynamicRow{
        values = @inline_array<int, 4>(
            50,
            60,
            70,
            80,
        ),
        length = 4,
    }

    matrix := @inline_array<DynamicRow, 2>(
        row0,
        row1,
    )

    before := matrix[1][2]

    matrix[1][2] = 777

    after := matrix[1][2]

    assert(before == 70)
    assert(after == 777)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"DynamicRow matrix[2] = {row0, row1};",
	) {
		t.Fatalf(
			"expected inline-array matrix storage, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"DynamicRowAt(&((matrix)[1]), 2)",
	) {
		t.Fatalf(
			"expected chained builtin-plus-overloaded read, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"DynamicRowSet(&((matrix)[1]), 2, 777)",
	) {
		t.Fatalf(
			"expected chained builtin-plus-overloaded write, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGenerateShiftOperators(t *testing.T) {
	out, reporter := generate(t, `
Main :: task() {
    left: u64 = 8 << 2
    right: u64 = left >> 1
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"uint64_t left = ((uint64_t)(((uint64_t)(8)) << (2)));",
	) {
		t.Fatalf(
			"expected primitive left-shift expression, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"uint64_t right = ((uint64_t)(((uint64_t)(left)) >> (1)));",
	) {
		t.Fatalf(
			"expected primitive right-shift expression, got:\n%s",
			out,
		)
	}

	compileGeneratedC(t, out)
}

func TestGenerateNarrowShiftTruncatesIntermediateResult(
	t *testing.T,
) {
	out, reporter := generate(t, `
Main :: task() {
    value: u8 = 255
    shifted := value << 1
    restored := shifted >> 1

    assert(shifted == 254)
    assert(restored == 127)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"uint8_t shifted = ((uint8_t)(((uint8_t)(value)) << (1)));",
	) {
		t.Fatalf(
			"expected u8 left shift to cast its result back to u8, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"uint8_t restored = ((uint8_t)(((uint8_t)(shifted)) >> (1)));",
	) {
		t.Fatalf(
			"expected u8 right shift to preserve the left operand type, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGenerateUntypedShiftUsesContextualUnsignedType(
	t *testing.T,
) {
	out, reporter := generate(t, `
Main :: task() {
    highBit: u64 = 1 << 63
    restored := highBit >> 63

    assert(restored == 1)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"uint64_t highBit = ((uint64_t)(((uint64_t)(1)) << (63)));",
	) {
		t.Fatalf(
			"expected untyped shift to use contextual u64 representation, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"uint64_t restored = ((uint64_t)(((uint64_t)(highBit)) >> (63)));",
	) {
		t.Fatalf(
			"expected u64 right shift, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGenerateShiftWithRuntimeCount(t *testing.T) {
	out, reporter := generate(t, `
ShiftLeft :: task(value u32, count uint) u32 {
    return value << count
}

ShiftRight :: task(value u32, count uint) u32 {
    return value >> count
}

Main :: task() {
    count: uint = 4

    left := ShiftLeft(3, count)
    right := ShiftRight(left, count)

    assert(left == 48)
    assert(right == 3)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"((uint32_t)(((uint32_t)(value)) << (count)))",
	) {
		t.Fatalf(
			"expected runtime left shift to lower directly to C, got:\n%s",
			out,
		)
	}

	if !strings.Contains(
		out,
		"((uint32_t)(((uint32_t)(value)) >> (count)))",
	) {
		t.Fatalf(
			"expected runtime right shift to lower directly to C, got:\n%s",
			out,
		)
	}

	if strings.Contains(out, "count &") ||
		strings.Contains(out, "count %") {
		t.Fatalf(
			"expected unsafe runtime shift without count masking, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGenerateShiftResultUsesLeftOperandType(
	t *testing.T,
) {
	out, reporter := generate(t, `
Main :: task() {
    value: u16 = 3
    count: u64 = 5

    shifted := value << count

    assert(shifted == 96)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"uint16_t shifted = ((uint16_t)(((uint16_t)(value)) << (count)));",
	) {
		t.Fatalf(
			"expected shift result to retain the u16 left operand type, got:\n%s",
			out,
		)
	}

	if strings.Contains(
		out,
		"uint64_t shifted",
	) {
		t.Fatalf(
			"shift count type must not determine the result type, got:\n%s",
			out,
		)
	}

	runGeneratedC(t, out)
}

func TestGenerateCastProvidesContextToUntypedShift(
	t *testing.T,
) {
	out, reporter := generate(t, `
Main :: task() {
    highBit := cast<u64>(1 << 63)
    restored := highBit >> 63

    assert(restored == 1)
}
`)

	if reporter.HasErrors() {
		t.Fatalf(
			"unexpected diagnostics:\n%s",
			reporter.String(),
		)
	}

	if !strings.Contains(
		out,
		"((uint64_t)(((uint64_t)(1)) << (63)))",
	) {
		t.Fatalf(
			"expected cast target to contextualize the untyped shift as u64, got:\n%s",
			out,
		)
	}

	compileGeneratedC(t, out)
	runGeneratedC(t, out)
}
