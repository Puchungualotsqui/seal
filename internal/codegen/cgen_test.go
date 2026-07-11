package cgen

import (
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

func TestGenerateIfForAndArray(t *testing.T) {
	out, reporter := generate(t, `
Main :: task() {
    values: []int = [1, 2, 3]

    sum := 0

    for i := 0; i < 3; i = i + 1 {
        sum = sum + values[i]
    }

    if sum > 0 {
        sum = sum + 1
    }
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "intptr_t values[3] = {1, 2, 3};") {
		t.Fatalf("expected array init, got:\n%s", out)
	}

	if !strings.Contains(out, "for (intptr_t i = 0; (i < 3); i = (i + 1))") {
		t.Fatalf("expected C-like for, got:\n%s", out)
	}

	if !strings.Contains(out, "if ((sum > 0))") {
		t.Fatalf("expected if statement, got:\n%s", out)
	}

	if !strings.Contains(out, "sum = (sum + 1);") {
		t.Fatalf("expected if body assignment, got:\n%s", out)
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
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, `#include "stdlib.h"`) {
		t.Fatalf("expected stdlib include, got:\n%s", out)
	}

	if !strings.Contains(out, "void * malloc(uintptr_t size);") {
		t.Fatalf("expected malloc prototype, got:\n%s", out)
	}

	if !strings.Contains(out, "void free(void * ptr);") {
		t.Fatalf("expected free prototype, got:\n%s", out)
	}

	if strings.Contains(out, "void * malloc(uintptr_t size) {") {
		t.Fatalf("extern malloc should not emit body, got:\n%s", out)
	}

	if !strings.Contains(out, "void * ptr = malloc(64);") {
		t.Fatalf("expected malloc call, got:\n%s", out)
	}

	if !strings.Contains(out, "free(ptr);") {
		t.Fatalf("expected free call, got:\n%s", out)
	}
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
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, `#include "stdio.h"`) {
		t.Fatalf("expected stdio include, got:\n%s", out)
	}

	if !strings.Contains(out, "intptr_t printf(const char * format, ...);") {
		t.Fatalf("expected printf variadic prototype, got:\n%s", out)
	}

	if !strings.Contains(out, `printf("%d %s", 10, "hello");`) {
		t.Fatalf("expected printf call, got:\n%s", out)
	}
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
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "typedef struct sealAny") {
		t.Fatalf("expected sealAny runtime, got:\n%s", out)
	}

	if !strings.Contains(out, "sealAny x = sealAny_int(10);") {
		t.Fatalf("expected any boxing, got:\n%s", out)
	}

	if !strings.Contains(out, "uintptr_t CountAny(sealVariadic_any args)") {
		t.Fatalf("expected CountAny variadic any signature, got:\n%s", out)
	}

	if !strings.Contains(out, `sealAny_string((sealString){.data = (const unsigned char *)"hello", .byte_len = 5})`) {
		t.Fatalf("expected string boxing, got:\n%s", out)
	}

	if !strings.Contains(out, "sealAny_f64(3.14)") {
		t.Fatalf("expected f64 boxing, got:\n%s", out)
	}
}

func TestGenerateInferredArrayOfAny(t *testing.T) {
	out, reporter := generate(t, `
Main :: task() {
    values: []any = [2, "hello", 3.14, true]
    n: uint = len(values)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "sealAny values[4]") {
		t.Fatalf("expected sealAny inferred array, got:\n%s", out)
	}

	if !strings.Contains(out, "sealAny_int(2)") {
		t.Fatalf("expected int boxing, got:\n%s", out)
	}

	if !strings.Contains(out, `sealAny_string((sealString){.data = (const unsigned char *)"hello", .byte_len = 5})`) {
		t.Fatalf("expected string boxing, got:\n%s", out)
	}

	if !strings.Contains(out, "sealAny_f64(3.14)") {
		t.Fatalf("expected f64 boxing, got:\n%s", out)
	}

	if !strings.Contains(out, "sealAny_bool(true)") {
		t.Fatalf("expected bool boxing, got:\n%s", out)
	}

	if !strings.Contains(out, "uintptr_t n = (uintptr_t)4;") {
		t.Fatalf("expected len(values) lowering, got:\n%s", out)
	}
}

func TestGenerateVariadicArrayOfAny(t *testing.T) {
	out, reporter := generate(t, `
TakeArrays :: task(args ...[10]any) uint {
    return len(args)
}

Main :: task() {
    a: [10]any = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    b: [10]any = ["a", "b", "c", "d", "e", "f", "g", "h", "i", "j"]

    result := TakeArrays(a, b)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "TakeArrays(") {
		t.Fatalf("expected TakeArrays call, got:\n%s", out)
	}

	if !strings.Contains(out, ".len = 2") {
		t.Fatalf("expected packed variadic length 2, got:\n%s", out)
	}

	if !strings.Contains(out, "uintptr_t TakeArrays(") {
		t.Fatalf("expected TakeArrays uint return, got:\n%s", out)
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
printf :: extern("printf") task(format cstring, args ...any) int

Main :: task() {
    c: char = 'ñ'
    s: string = "hola"
    cs: cstring = c"world"

    n: uint = size(s)
    h: char = s[0]
    o: char = s[1]
    a: char = s[-1]

    printf(c"%zu %u %u %u %u %s", n, c, h, o, a, cs)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "typedef struct sealString") {
		t.Fatalf("expected sealString runtime, got:\n%s", out)
	}

	if !strings.Contains(out, "static inline size_t sealString_len(sealString s)") {
		t.Fatalf("expected sealString_len runtime helper, got:\n%s", out)
	}

	if !strings.Contains(out, "static inline uint32_t sealString_at(sealString s, ptrdiff_t index)") {
		t.Fatalf("expected sealString_at runtime helper, got:\n%s", out)
	}

	if !strings.Contains(out, `sealString s = (sealString){.data = (const unsigned char *)"hola", .byte_len = 4};`) {
		t.Fatalf("expected string literal lowering, got:\n%s", out)
	}

	if !strings.Contains(out, `const char * cs = "world";`) {
		t.Fatalf("expected cstring literal lowering, got:\n%s", out)
	}

	if !strings.Contains(out, "uint32_t c = 241;") {
		t.Fatalf("expected char lowering for ñ, got:\n%s", out)
	}

	if !strings.Contains(out, "uintptr_t n = (uintptr_t)(s).byte_len;") {
		t.Fatalf("expected size(s) lowering, got:\n%s", out)
	}

	if !strings.Contains(out, "uint32_t h = sealString_at(s, (ptrdiff_t)(0));") {
		t.Fatalf("expected s[0] lowering, got:\n%s", out)
	}

	if !strings.Contains(out, "uint32_t o = sealString_at(s, (ptrdiff_t)(1));") {
		t.Fatalf("expected s[1] lowering, got:\n%s", out)
	}

	if !strings.Contains(out, "uint32_t a = sealString_at(s, (ptrdiff_t)((-1)));") {
		t.Fatalf("expected s[-1] lowering, got:\n%s", out)
	}
}

func TestGenerateSpreadArrayIntoVariadicTask(t *testing.T) {
	out, reporter := generate(t, `
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
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "intptr_t a[3] = {1, 2, 3};") {
		t.Fatalf("expected array declaration, got:\n%s", out)
	}

	if !strings.Contains(out, "Sum((sealVariadic_int){.data = a, .len = 3})") {
		t.Fatalf("expected array spread lowering, got:\n%s", out)
	}
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

func TestGenerateSpreadArrayIntoVariadicWithFixedParameter(t *testing.T) {
	out, reporter := generate(t, `
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
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "Example(10, (sealVariadic_int){.data = a, .len = 3})") {
		t.Fatalf("expected spread after fixed argument, got:\n%s", out)
	}
}

func TestGenerateSpreadArrayLiteralIntoVariadicTask(t *testing.T) {
	out, reporter := generate(t, `
Sum :: task(values ...int) uint {
    return len(values)
}

Main :: task() {
    result := Sum([1, 2, 3]...)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "Sum((sealVariadic_int){.data = (intptr_t[]){1, 2, 3}, .len = 3})") {
		t.Fatalf("expected array literal spread lowering, got:\n%s", out)
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

    a: uint = size(int)
    b: uint = size(Goblin)
    c: uint = size(x)
    d: uint = size(s)
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "uintptr_t a = (uintptr_t)sizeof(intptr_t);") {
		t.Fatalf("expected size(int)")
	}

	if !strings.Contains(out, "uintptr_t b = (uintptr_t)sizeof(Goblin);") {
		t.Fatalf("expected size(Goblin)")
	}

	if !strings.Contains(out, "uintptr_t c = (uintptr_t)sizeof(x);") {
		t.Fatalf("expected size(x)")
	}

	if !strings.Contains(out, "uintptr_t d = (uintptr_t)(s).byte_len;") {
		t.Fatalf("expected size(string)")
	}
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

func TestGenerateGenericStructValueArgumentSpecialization(t *testing.T) {
	out, reporter := generate(t, `
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

	if !strings.Contains(out, "typedef struct Buffer_int_32") {
		t.Fatalf("expected Buffer_int_32 struct, got:\n%s", out)
	}

	if !strings.Contains(out, "intptr_t data[32];") {
		t.Fatalf("expected [32]int field, got:\n%s", out)
	}

	if !strings.Contains(out, "Buffer_int_32 b;") {
		t.Fatalf("expected Buffer_int_32 variable, got:\n%s", out)
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
		"Enemy_int_vtable *vtable;",
		"Enemy_int_Goblin_Health",
		"Enemy_int_Goblin_vtable",
		".vtable = &Enemy_int_Goblin_vtable",
		"(e).vtable->Health((e).data)",
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
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "intptr_t Identity_int(intptr_t value);") {
		t.Fatalf("expected Identity_int prototype, got:\n%s", out)
	}

	if !strings.Contains(out, "sealString Identity_string(sealString value);") {
		t.Fatalf("expected Identity_string prototype, got:\n%s", out)
	}

	if !strings.Contains(out, "intptr_t Identity_int(intptr_t value) {") {
		t.Fatalf("expected Identity_int body, got:\n%s", out)
	}

	if !strings.Contains(out, "sealString Identity_string(sealString value) {") {
		t.Fatalf("expected Identity_string body, got:\n%s", out)
	}

	if !strings.Contains(out, "intptr_t a = Identity_int(10);") {
		t.Fatalf("expected Identity_int call, got:\n%s", out)
	}

	if !strings.Contains(out, "sealString b = Identity_string((sealString){.data = (const unsigned char *)\"hello\", .byte_len = 5});") {
		t.Fatalf("expected Identity_string call, got:\n%s", out)
	}
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

func TestGenerateGenericTaskWithValueGenericArg(t *testing.T) {
	out, reporter := generate(t, `
Buffer :: struct <T type, N int> {
    data [N]T
}

MakeBuffer :: task <T type, N int>(value T) Buffer<T, N> {
    return Buffer<T, N>{data = [value, value, value]}
}

Main :: task() {
    b := MakeBuffer<int, 3>(9)
    x := b.data[0]
}
`)

	if reporter.HasErrors() {
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	if !strings.Contains(out, "typedef struct Buffer_int_3") {
		t.Fatalf("expected Buffer_int_3 struct, got:\n%s", out)
	}

	if !strings.Contains(out, "Buffer_int_3 MakeBuffer_int_3(intptr_t value);") {
		t.Fatalf("expected MakeBuffer_int_3 prototype, got:\n%s", out)
	}

	if !strings.Contains(out, "Buffer_int_3 b = MakeBuffer_int_3(9);") {
		t.Fatalf("expected MakeBuffer_int_3 call, got:\n%s", out)
	}

	if !strings.Contains(out, "intptr_t x = (b).data[0];") {
		t.Fatalf("expected generic buffer index access, got:\n%s", out)
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

func TestGenerateGenericTaskCallsGenericTaskWithTypeParam(t *testing.T) {
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
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
	}

	checks := []string{
		"intptr_t Identity_int(intptr_t value);",
		"sealString Identity_string(sealString value);",
		"intptr_t Forward_int(intptr_t value);",
		"sealString Forward_string(sealString value);",
		"return __seal_return_value_",
		"Identity_int(value)",
		"Identity_string(value)",
		"intptr_t a = Forward_int(10);",
		"sealString b = Forward_string((sealString){.data = (const unsigned char *)\"hello\", .byte_len = 5});",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated output to contain %q\n\n%s", want, out)
		}
	}
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

func TestGenerateGenericTaskNestedGenericStructInReturn(t *testing.T) {
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
		t.Fatalf("unexpected diagnostics:\n%s", reporter.String())
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
		"Box_Pair_int_string nested = MakeNested_string((sealString){.data = (const unsigned char *)\"hello\", .byte_len = 5});",
		"Pair_int_string pair = (nested).value;",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated output to contain %q\n\n%s", want, out)
		}
	}
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

	g := NewWithPackages(reporter, "", packages)
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

	return ExportPackageInfo(packageName, parsed, reporter),
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

func TestImportedGenericStructValueParamCodegen(t *testing.T) {
	typesPkg, resolverPkg, checkerPkg := exportCGenPackage(t, "types", `
Buffer :: struct <T type, N int> {
    data [N]T
}
`)

	out, reporter := generateWithPackages(t, `
Main :: task() {
    b: types.Buffer<int, 4>
    x := b.data[0]
    assert(x == 0)
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
		"typedef struct types_Buffer_int_4 {",
		"intptr_t data[4];",
		"} types_Buffer_int_4;",
		"types_Buffer_int_4 b;",
		"intptr_t x = (b).data[0];",
		"assert((x == 0));",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated C to contain %q, got:\n%s", want, out)
		}
	}
}

func TestImportedNestedGenericStructValueParamCodegen(t *testing.T) {
	typesPkg, resolverPkg, checkerPkg := exportCGenPackage(t, "types", `
Buffer :: struct <T type, N int> {
    data [N]T
}

Box :: struct <T type> {
    value T
}
`)

	out, reporter := generateWithPackages(t, `
Main :: task() {
    b: types.Box<types.Buffer<int, 4>>
    x := b.value.data[0]
    assert(x == 0)
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
		"typedef struct types_Buffer_int_4 {",
		"intptr_t data[4];",
		"} types_Buffer_int_4;",
		"typedef struct types_Box_types_Buffer_int_4 {",
		"types_Buffer_int_4 value;",
		"} types_Box_types_Buffer_int_4;",
		"types_Box_types_Buffer_int_4 b;",
		"intptr_t x = ((b).value).data[0];",
		"assert((x == 0));",
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

	g := NewWithPackages(reporter, "", map[string]*PackageInfo{
		"types": typesPkg,
	})
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

	g := NewWithPackages(reporter, "types", nil)
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

func TestRequestedGenericStructEmitsPackagePrefixedDefinition(t *testing.T) {
	file := source.NewFile("types.seal", `
Buffer :: struct <T type, N int> {
    data [N]T
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

	g := NewWithPackages(reporter, "types", nil)
	g.AddRequestedInstances([]GenericInstanceRequest{
		{
			Kind:        GenericInstanceStruct,
			PackageName: "types",
			SymbolName:  "Buffer",
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
				{
					Kind: ast.GenericArgExpr,
					Expr: &ast.IntLitExpr{
						Value: "4",
						Loc:   source.NewSpan(file, 0, 0),
					},
					Loc: source.NewSpan(file, 0, 0),
				},
			},
		},
	})

	out := g.Generate(parsed)

	checks := []string{
		"typedef struct types_Buffer_int_4 {",
		"intptr_t data[4];",
		"} types_Buffer_int_4;",
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
		"Positioned_Transform_Position(void *data)",
		"Positioned_Entity_Position(void *data)",
		"return Positioned_Transform_Position(",
		"static Positioned_vtable Positioned_Entity_vtable",
		".Position = Positioned_Entity_Position",
		".vtable = &Positioned_Entity_vtable",
		"(positioned).vtable->Position((positioned).data)",
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
		"typedef struct Positioned_vtable {",
		"Positioned_vtable *vtable;",
		"static Positioned_vtable Positioned_Transform_vtable",
		".vtable = &Positioned_Transform_vtable",
		"(positioned).vtable->X((positioned).data)",
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

		"typedef struct DynamicPositioned_vtable {",
		"DynamicPositioned_vtable *vtable;",
		".vtable = &DynamicPositioned_Transform_vtable",
		"(dynamicPositioned).vtable->X((dynamicPositioned).data)",
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
