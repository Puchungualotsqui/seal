# Seal Language Guide

> **Current implementation reference**
>
> Seal is still under active development. This guide documents the language and build system as they currently exist after the removal of compiler-native arrays. Features that are incomplete, experimental, or planned are explicitly marked.

## Table of contents

1. [Overview](#1-overview)
2. [A minimal program](#2-a-minimal-program)
3. [Projects, workspaces, and packages](#3-projects-workspaces-and-packages)
4. [`seal.workspace`](#4-sealworkspace)
5. [`seal.toml`](#5-sealtoml)
6. [Dependencies and package-qualified access](#6-dependencies-and-package-qualified-access)
7. [Standard-library package discovery](#7-standard-library-package-discovery)
8. [Source files and declaration style](#8-source-files-and-declaration-style)
9. [Primitive types](#9-primitive-types)
10. [Literals and values](#10-literals-and-values)
11. [Variables and assignments](#11-variables-and-assignments)
12. [Tasks](#12-tasks)
13. [Purity](#13-purity)
14. [Default and variadic parameters](#14-default-and-variadic-parameters)
15. [Multiple return values](#15-multiple-return-values)
16. [Control flow](#16-control-flow)
17. [`defer`](#17-defer)
18. [Structs](#18-structs)
19. [Distinct types](#19-distinct-types)
20. [Enums](#20-enums)
21. [Unions](#21-unions)
22. [`any`](#22-any)
23. [Pointers and low-level memory access](#23-pointers-and-low-level-memory-access)
24. [Strings and C strings](#24-strings-and-c-strings)
25. [Arrays after native-array removal](#25-arrays-after-native-array-removal)
26. [Operator and task overloads](#26-operator-and-task-overloads)
27. [Indexing and `len` resolution](#27-indexing-and-len-resolution)
28. [Interfaces](#28-interfaces)
29. [Implementations](#29-implementations)
30. [Delegated implementations](#30-delegated-implementations)
31. [Dynamic interfaces](#31-dynamic-interfaces)
32. [Generics](#32-generics)
33. [Generic task parameters](#33-generic-task-parameters)
34. [Generic constraints](#34-generic-constraints)
35. [Compile-time constraint evaluation](#35-compile-time-constraint-evaluation)
36. [Built-in and intrinsic tasks](#36-built-in-and-intrinsic-tasks)
37. [C interoperability](#37-c-interoperability)
38. [Build output and C compilation](#38-build-output-and-c-compilation)
39. [Cross-package generic specialization](#39-cross-package-generic-specialization)
40. [Current limitations and evolving areas](#40-current-limitations-and-evolving-areas)
41. [Complete multi-package example](#41-complete-multi-package-example)
42. [Syntax cheat sheet](#42-syntax-cheat-sheet)

---

## 1. Overview

Seal is a statically typed compiled programming language with a C backend. The compiler is written in Go and currently follows this broad pipeline:

```text
Seal source
    ↓
lexer
    ↓
parser
    ↓
resolver
    ↓
type checker
    ↓
semantic side tables
    ↓
C generator
    ↓
host C compiler
```

The language emphasizes:

- explicit static types;
- predictable C-compatible code generation;
- monomorphized generics;
- package-oriented compilation;
- low-level access when needed;
- interfaces with static or dynamic dispatch;
- compile-time generic constraints;
- no mandatory garbage collector;
- a standard library built from normal Seal packages.

Seal source files use the `.seal` extension.

---

## 2. A minimal program

```seal
Main :: task() {
    message := "Hello from Seal"
    assert(size(message) > 0)
}
```

`Main` is the executable entry task. The C backend lowers it to a C entry point similar to:

```c
int main(void)
```

An executable package must contain Seal source files and normally defines `Main`.

A minimal project layout is:

```text
hello/
├── seal.workspace
└── app/
    ├── seal.toml
    └── main.seal
```

`seal.workspace` may be empty.

`app/seal.toml`:

```toml
name = "app"
version = "0.1.0"
kind = "executable"
```

`app/main.seal`:

```seal
Main :: task() {
    x := 10
    y := x + 20
    assert(y == 30)
}
```

The build system can be pointed at any file or directory inside the package. Internally, the build entry is equivalent to:

```go
BuildWorkspace(path, BuildOptions{})
```

To generate C without invoking the host C compiler, the build API uses:

```go
BuildOptions{
    EmitOnly: true,
}
```

The exact command-line spelling depends on the current `sealc` command frontend.

---

## 3. Projects, workspaces, and packages

Seal distinguishes:

- a **workspace**, identified by `seal.workspace`;
- a **package**, identified by `seal.toml`;
- a **source file**, identified by its `.seal` extension.

A workspace can contain multiple packages:

```text
game-workspace/
├── seal.workspace
├── game/
│   ├── seal.toml
│   └── main.seal
├── math/
│   ├── seal.toml
│   └── math.seal
└── types/
    ├── seal.toml
    └── types.seal
```

Packages are discovered recursively under the workspace root. Directories named `.git`, `.seal`, and `build` are skipped during package discovery.

Every package must have a unique package name. Duplicate names are errors even when the packages are in different directories.

The build system computes a dependency-first order. For example:

```text
mem → fmt → game
```

Dependencies are checked for:

- missing package names;
- exact version mismatches when both sides specify a version;
- dependency cycles.

Seal currently discovers dependencies locally. A dependency declaration does not download a package from a registry.

---

## 4. `seal.workspace`

`seal.workspace` is currently a workspace marker.

It may be empty:

```text
```

When the compiler searches upward from a source path:

1. an ancestor containing `seal.workspace` is treated as the workspace root;
2. otherwise, the nearest ancestor containing `seal.toml` is treated as the root;
3. if neither exists, workspace discovery fails.

A workspace-local `std/` directory is also significant because it may be used as the standard-library package root.

Example:

```text
project/
├── seal.workspace
├── app/
│   └── seal.toml
└── std/
    ├── mem/
    │   └── seal.toml
    └── array/
        └── seal.toml
```

---

## 5. `seal.toml`

Each package is configured through `seal.toml`.

Seal accepts package keys either at the top level:

```toml
name = "app"
version = "0.1.0"
kind = "executable"
```

or under sections:

```toml
[package]
name = "app"
version = "0.1.0"
kind = "executable"
```

### 5.1 Package keys

| Key | Type | Default | Meaning |
|---|---:|---:|---|
| `name` | string | required | Unique package name |
| `version` | string | `"0.1.0"` | Local package version |
| `kind` | string | `"library"` | `"library"` or `"executable"` |
| `dependencies` | array | empty | Other package names and optional versions |

Dependencies may be short strings:

```toml
dependencies = [
    "mem",
    "fmt",
]
```

or objects:

```toml
dependencies = [
    { name = "mem", version = "0.1.0" },
    { name = "fmt", version = "0.1.0" },
]
```

Both forms may also appear under `[package]`.

### 5.2 Build keys

Build keys may be top-level or placed under `[build]`.

```toml
[build]
compiler = "clang"
standard = "c11"
c_flags = ["-Wall", "-Wextra"]
link_flags = ["-static"]
include_dirs = ["include"]
library_dirs = ["lib"]
libraries = ["m"]
defines = ["SEAL_DEBUG=1"]
```

| Key | Type | Default | Meaning |
|---|---:|---:|---|
| `compiler` | string | `""` | Compiler preset or executable name |
| `compiler_path` | string | `""` | Explicit compiler executable path |
| `compiler_args` | string array | empty | Arguments inserted immediately after compiler executable |
| `c_flags` | string array | empty | Additional compile flags |
| `link_flags` | string array | empty | Additional linker flags |
| `include_dirs` | string array | empty | Converted to `-I...` |
| `library_dirs` | string array | empty | Converted to `-L...` |
| `libraries` | string array | empty | Converted to `-l...` |
| `defines` | string array | empty | Converted to `-D...` |
| `target` | string | `""` | Target triple; automatically handled for Zig CC |
| `standard` | string | `"c11"` | C language standard |
| `linkage` | string | `"static"` | Requested linkage mode |

Supported compiler presets include:

```text
cc
gcc
clang
zigcc
zig-cc
zig cc
msvc
cl
```

For Zig CC:

```toml
[build]
compiler = "zigcc"
target = "x86_64-windows-gnu"
```

The resulting command begins with:

```text
zig cc -target x86_64-windows-gnu
```

`compiler_path` overrides the preset executable:

```toml
[build]
compiler = "zigcc"
compiler_path = "C:/tools/zig/zig.exe"
compiler_args = ["cc"]
```

Custom compiler names are also accepted:

```toml
[build]
compiler = "x86_64-w64-mingw32-gcc"
```

### 5.3 Checker configuration

```toml
[checker]
generic_constraint_max_depth = 32
```

| Key | Type | Default | Meaning |
|---|---:|---:|---|
| `generic_constraint_max_depth` | integer | `0` | Maximum nested compile-time generic-constraint task evaluation depth |

A value less than or equal to zero disables the depth guard:

```toml
[checker]
generic_constraint_max_depth = -1
```

A positive value also enables recursion detection during generic constraint evaluation.

### 5.4 General check-policy keys

```toml
[checks]
auto_initialize_variables = true
allow_uninitialized_variables = false
allow_partial_initialized_structs = false
allow_partial_switches = false
integer_overflow = "trap"
bounds_checking = "trap"
fail_bad_style = false
allow_unused_variables = true
allow_unused_parameters = true
allow_run_directives = true
```

Current configuration fields are:

| Key | Type | Default |
|---|---:|---:|
| `auto_initialize_variables` | bool | `true` |
| `allow_uninitialized_variables` | bool | `false` |
| `allow_partial_initialized_structs` | bool | `false` |
| `allow_partial_switches` | bool | `false` |
| `integer_overflow` | string | `"trap"` |
| `bounds_checking` | string | `"trap"` |
| `fail_bad_style` | bool | `false` |
| `allow_unused_variables` | bool | `true` |
| `allow_unused_parameters` | bool | `true` |
| `allow_run_directives` | bool | `true` |

These settings are parsed and retained by the build configuration. At the current stage of the compiler, not every policy is fully enforced throughout the checker and C backend.

The removed key:

```toml
allow_partial_initialized_arrays = true
```

is obsolete and should not be used.

---

## 6. Dependencies and package-qualified access

Seal does not currently require source-level `import` declarations for normal Seal packages.

A package declares dependencies in `seal.toml`:

```toml
[package]
name = "app"
kind = "executable"
dependencies = ["types", "math"]
```

The dependency names become package namespaces in source:

```seal
Main :: task() {
    p := types.Player{health = 10}
    doubled := math.Double(p.health)
}
```

Package-qualified forms include:

```seal
types.Player
types.Player{health = 10}
types.MakeBox<int>(10)
types.SomeConstant
```

A package dependency must exist in the discovered package graph. Merely writing:

```seal
types.Player
```

does not add the dependency automatically.

There is currently no dependency alias syntax. The package name from its `seal.toml` is the source namespace.

---

## 7. Standard-library package discovery

Standard-library modules are normal Seal packages.

The compiler searches for standard packages in this order:

1. paths from `SEAL_STD_PATH`, when set;
2. a workspace-local `std/` directory;
3. installed layouts near the compiler executable;
4. development `std/` directories found while walking upward from the current working directory.

`SEAL_STD_PATH` uses the operating system’s path-list separator.

Linux/macOS example:

```bash
export SEAL_STD_PATH="/opt/seal/std:/home/user/seal-extra-std"
```

PowerShell example:

```powershell
$env:SEAL_STD_PATH = "C:\seal\std;D:\seal-extra-std"
```

When `SEAL_STD_PATH` is set, its explicit paths take priority.

A standard package still needs a `seal.toml`:

```text
std/
└── mem/
    ├── seal.toml
    └── mem.seal
```

`std/mem/seal.toml`:

```toml
name = "mem"
version = "0.1.0"
kind = "library"
```

A consuming package declares it normally:

```toml
dependencies = ["mem"]
```

and uses it normally:

```seal
ptr := mem.Alloc(64)
mem.Free(ptr)
```

---

## 8. Source files and declaration style

All `.seal` files beneath a package root are collected, sorted, parsed, and combined into one package AST.

The scanner skips:

```text
.git
.seal
build
vendor
```

A declaration generally begins with a name and `::`:

```seal
Name :: declaration-or-value
```

Examples:

```seal
Pi :: 3.14159265

Player :: struct {
    health int
}

Add :: task(a int, b int) int {
    return a + b
}
```

Source examples normally omit semicolons. Newlines terminate ordinary statements.

Comments use familiar forms:

```seal
// line comment

/*
block comment
*/
```

Naming is currently not restricted by a mandatory style policy, although `fail_bad_style` exists as a configuration direction.

---

## 9. Primitive types

Seal currently defines these primitive source types:

```seal
bool

int
uint

i8
i16
i32
i64

u8
u16
u32
u64

f32
f64

char
rawptr
any
string
cstring
```

### 9.1 Integer types

`int` and `uint` are machine-word-sized:

```text
int  → C intptr_t
uint → C uintptr_t
```

Fixed-width types map directly to C integer types:

```text
i8  → int8_t
i16 → int16_t
i32 → int32_t
i64 → int64_t

u8  → uint8_t
u16 → uint16_t
u32 → uint32_t
u64 → uint64_t
```

### 9.2 Floating-point types

```text
f32 → C float
f64 → C double
```

### 9.3 Character type

`char` stores a Unicode scalar value and currently lowers to:

```text
uint32_t
```

Example:

```seal
letter: char = 'ñ'
```

### 9.4 Pointer and dynamic types

```text
rawptr  → void *
cstring → const char *
```

`string` and `any` use compiler-generated runtime structs.

---

## 10. Literals and values

### 10.1 Booleans

```seal
enabled := true
finished := false
```

### 10.2 Integers and floats

```seal
count := 10
ratio := 3.14

small: i8 = 12
large: u64 = 1000
precise: f64 = 3.14159265
```

Unsuffixed numeric literals begin as untyped numeric values and are checked against their context.

### 10.3 Characters

```seal
ascii: char = 'A'
unicode: char = 'ñ'
```

### 10.4 Seal strings

```seal
message: string = "hello"
```

A Seal string is UTF-8 data plus a byte length.

### 10.5 C strings

```seal
format: cstring = c"%d\n"
```

The `c"..."` prefix creates a null-terminated C string literal.

### 10.6 `nil`

`nil` is used with supported nullable/tagged representations, including unions and pointers where permitted:

```seal
value = nil
```

---

## 11. Variables and assignments

### 11.1 Inferred local variable

```seal
x := 10
name := "Seal"
```

### 11.2 Explicit type and initializer

```seal
x: int = 10
name: string = "Seal"
```

### 11.3 Explicit declaration without initializer

```seal
x: int
```

The intended initialization policy is controlled by package checks, though some policies remain only partially wired through the compiler.

### 11.4 Assignment

```seal
x = 20
```

### 11.5 Compound assignment

Ordinary assignable expressions support forms such as:

```seal
x += 1
x -= 1
x *= 2
x /= 2
```

Pointer-selected struct fields are automatically dereferenced:

```seal
Damage :: task(player *Player, amount int) {
    player.health -= amount
}
```

### 11.6 Multiple declaration

Multi-result tasks can be destructured:

```seal
left, right := Split()
```

The blank identifier discards a result:

```seal
_, value := Split()
```

---

## 12. Tasks

Tasks are Seal’s function-like declarations.

### 12.1 No result

```seal
PrintValue :: task(value int) {
    // ...
}
```

### 12.2 One result

```seal
Add :: task(a int, b int) int {
    return a + b
}
```

### 12.3 Multiple results

```seal
Divide :: task(a int, b int) int, bool {
    if b == 0 {
        return 0, false
    }

    return a / b, true
}
```

### 12.4 Calling tasks

```seal
sum := Add(10, 20)
```

### 12.5 Entry task

```seal
Main :: task() {
}
```

For an executable package, `Main` becomes the C program entry point.

### 12.6 Test tasks

The parser supports test-task declarations:

```seal
AdditionWorks :: test task() {
    assert(Add(2, 3) == 5)
}
```

The syntax exists, but the complete test-runner workflow is still evolving.

---

## 13. Purity

A task can be declared pure:

```seal
Square :: pure task(value int) int {
    return value * value
}
```

Purity is used by:

- generic compile-time constraint evaluation;
- operator requirements that must be side-effect free;
- interface requirements marked pure;
- `[]` read overloads;
- `len` overloads.

A pure task must not perform operations the checker classifies as impure.

The compiler also tracks **trusted-pure** tasks for intrinsics or library integration. The exact user-facing spelling for trusted purity is not yet considered stable; `pure task` is the normal source form.

---

## 14. Default and variadic parameters

### 14.1 Default parameters

```seal
Increment :: task(value int, amount int = 1) int {
    return value + amount
}

Main :: task() {
    a := Increment(10)
    b := Increment(10, 5)
}
```

Rules include:

- required parameters must precede defaulted parameters;
- a variadic parameter cannot have a default;
- defaults are type checked;
- generic defaults are substituted in specialized task instances.

### 14.2 Variadic parameters

```seal
Sum :: task(values ...int) int {
    total := 0

    for i := 0; i < len(values); i = i + 1 {
        total += values[i]
    }

    return total
}
```

Call it with any number of trailing arguments:

```seal
result := Sum(1, 2, 3)
```

The final variadic parameter is represented by a compiler-generated runtime value containing:

```text
data pointer
length
```

### 14.3 Variadic forwarding

A variadic value can be forwarded with `...`:

```seal
Forward :: task(values ...int) int {
    return Sum(values...)
}
```

The spread argument must be last.

After native-array removal, variadic spread is the stable spread use. Standard-library collections do not automatically spread unless a dedicated language/library rule is added later.

---

## 15. Multiple return values

```seal
Swap :: task(a int, b int) int, int {
    return b, a
}

Main :: task() {
    x, y := Swap(1, 2)
}
```

The C backend creates a result struct:

```c
typedef struct Swap_Result {
    intptr_t _0;
    intptr_t _1;
} Swap_Result;
```

Multiple results can be forwarded:

```seal
ForwardSwap :: task(a int, b int) int, int {
    return Swap(a, b)
}
```

Multi-return tasks cannot be used as a normal single expression. They must be:

- destructured;
- returned directly from a compatible multi-result task;
- otherwise handled in a context expecting all results.

---

## 16. Control flow

### 16.1 `if`

```seal
if score > 100 {
    score = 100
} else {
    score += 1
}
```

Conditions must be `bool`.

### 16.2 C-style `for`

```seal
for i := 0; i < 10; i = i + 1 {
    // ...
}
```

A loop may also use the forms accepted by the current parser for omitted initializer, condition, or post expression.

### 16.3 `break` and `continue`

```seal
for i := 0; i < 100; i += 1 {
    if i == 10 {
        break
    }

    if i % 2 == 0 {
        continue
    }
}
```

### 16.4 `switch`

Seal has specialized switch forms for unions and `any`, covered later.

---

## 17. `defer`

`defer` schedules a task call for the current task’s exit:

```seal
Close :: task(handle int) {
}

UseHandle :: task() {
    handle := 1
    defer Close(handle)

    handle = 2
}
```

The current C backend captures deferred arguments into temporaries at the defer site, then emits the deferred call when leaving the task.

This means the deferred call uses the captured value, not necessarily the variable’s later value.

---

## 18. Structs

### 18.1 Declaration

```seal
Player :: struct {
    health int
    name string
}
```

### 18.2 Compound literal

```seal
player := Player{
    health = 100,
    name = "Ada",
}
```

### 18.3 Field access

```seal
health := player.health
player.health = 80
```

### 18.4 Pointer field access

Seal automatically chooses the appropriate C field operation:

```seal
Heal :: task(player *Player, amount int) {
    player.health += amount
}
```

You write:

```seal
player.health
```

rather than a separate arrow operator.

### 18.5 Generic structs

```seal
Box :: struct <T type> {
    value T
}

Main :: task() {
    box := Box<int>{value = 10}
}
```

Generic structs are monomorphized into concrete C structs.

---

## 19. Distinct types

A distinct type creates a type that is separate from its underlying primitive:

```seal
UserId :: distinct uint
```

Use an explicit cast to create or convert it:

```seal
id := cast<UserId>(10)
```

A distinct type is not a struct and cannot use a compound literal:

```seal
// Invalid:
id := UserId{10}
```

The underlying type must currently be a concrete primitive type.

Distinct types are useful for preventing accidental interchange:

```seal
UserId :: distinct uint
OrderId :: distinct uint
```

Although both lower to compatible C primitives, Seal treats them as separate source types.

---

## 20. Enums

### 20.1 Declaration

```seal
Status :: enum {
    Ready
    Running
    Done
}
```

### 20.2 Dot literal

```seal
status: Status = .Ready
```

The expected enum type supplies the enum name.

### 20.3 Comparison

```seal
if status == .Done {
    // ...
}
```

The C backend emits names similar to:

```text
Status_Ready
Status_Running
Status_Done
```

### 20.4 Generic enum constraints

A generic enum parameter can require a variant:

```seal
UseReady :: task <E enum[Ready]>() {
}
```

This is covered further under generic constraints.

---

## 21. Unions

A normal Seal union is a tagged union.

### 21.1 Declaration

```seal
Circle :: struct {
    radius f32
}

Rectangle :: struct {
    width f32
    height f32
}

Shape :: union {
    Circle
    Rectangle
}
```

### 21.2 Assignment

```seal
shape: Shape = Circle{radius = 5.0}
```

A supported union can be assigned `nil`:

```seal
shape = nil
```

### 21.3 Union switch and narrowing

```seal
Area :: task(shape Shape) f32 {
    switch value in shape {
    case Circle:
        return value.radius * value.radius

    case Rectangle:
        return value.width * value.height

    case nil:
        return 0
    }

    return 0
}
```

The binding after `switch` receives the narrowed member value.

### 21.4 Raw unions

The parser supports an explicitly raw union form:

```seal
Value :: @rawUnion union {
    i32
    f32
}
```

A raw union is intended for low-level representation and C interoperability. It does not provide the same tagged safety model as a normal union. Its exact ABI guarantees remain an advanced and evolving area.

### 21.5 Generic unions

Generic union support is not yet considered a completed, stable feature. Types such as:

```seal
Option<T>
Result<T, E>
```

remain an important standard-library and compiler milestone.

---

## 22. `any`

`any` is a tagged runtime container for supported primitive values.

```seal
value: any = 10
```

Primitive values are boxed automatically when assigned or passed to `any`.

### 22.1 Type test

```seal
if anyIs<int>(value) {
    // ...
}
```

### 22.2 Extraction

```seal
number := anyAs<int>(value)
```

The program should normally check the type before extraction.

### 22.3 Partial type switch

```seal
@partial switch value type {
case int:
    number := anyAs<int>(value)

case string:
    text := anyAs<string>(value)
}
```

### 22.4 Non-partial type switch

A non-partial type switch should cover its required cases or include a default branch:

```seal
switch value type {
case int:
    number := anyAs<int>(value)

default:
    fallback := 0
}
```

The C backend represents `any` using a type tag and a value union.

---

## 23. Pointers and low-level memory access

### 23.1 Typed pointers

```seal
player: *Player
```

### 23.2 Address-of

```seal
player := Player{health = 100}
pointer := &player
```

### 23.3 Automatic pointer field access

```seal
health := pointer.health
```

### 23.4 Raw pointers

```seal
ptr: rawptr
```

`rawptr` is intended for:

- allocation APIs;
- C interoperability;
- untyped low-level memory;
- explicit casts.

### 23.5 Raw-pointer byte indexing

Raw pointers retain compiler-native byte indexing:

```seal
ptr[0] = 255
byte := ptr[0]
```

The index must be an `int`. Reads and writes operate on bytes.

### 23.6 Primitive scalar byte indexing

Primitive scalar storage may also be inspected or modified byte-by-byte:

```seal
value := 300
low := value[0]
value[0] = low
```

This is low-level, representation-dependent behavior.

### 23.7 C-string indexing

A `cstring` can be read as bytes:

```seal
first: u8 = text[0]
```

Writing through a `cstring` is invalid because it is represented as `const char *`.

---

## 24. Strings and C strings

Seal distinguishes:

```seal
string
cstring
```

### 24.1 Seal string

```seal
text := "hello"
```

The runtime representation contains:

```text
UTF-8 byte pointer
byte length
```

### 24.2 C string

```seal
text := c"hello"
```

This is a null-terminated C string suitable for C APIs.

### 24.3 Character values

```seal
letter := 'ñ'
```

`char` is a Unicode scalar value, not a one-byte C `char`.

### 24.4 String size

```seal
bytes: uint = size(text)
```

For `string`, `size` returns the stored UTF-8 byte length.

### 24.5 String indexing and length

Compiler-native string indexing and compiler-native `len(string)` have been removed.

These operations should be provided by the standard library through overloads:

```seal
character := text[index]
count := len(text)
```

The checker selects the string overload, and CGen emits the selected task call.

This allows the standard library to define whether indexing means:

- byte indexing;
- Unicode scalar indexing;
- another documented string model.

The compiler no longer hardcodes `sealString_at` or `sealString_len`.

---

## 25. Arrays after native-array removal

The following old language forms are obsolete:

```seal
[N]T
[]T
[1, 2, 3]
```

Compiler-native fixed arrays, inferred arrays, and array literals were removed from the AST, parser, checker, and C backend.

Arrays are now intended to be implemented by the standard library as a generic type:

```seal
Array<T, N>
```

Example use:

```seal
values: array.Array<int, 4>
```

The exact constructor API belongs to the standard library. It could use tasks such as:

```seal
values := array.New<int, 4>()
```

or another explicit initialization model.

### 25.1 Expected array overloads

A standard-library array package can define tasks conceptually like:

```seal
ArrayAt :: pure task <T type, N int>(
    values *Array<T, N>,
    index int,
) T {
    // ...
}

ArraySet :: task <T type, N int>(
    values *Array<T, N>,
    index int,
    value T,
) {
    // ...
}

ArrayLen :: pure task <T type, N int>(
    values *Array<T, N>,
) uint {
    return cast<uint>(N)
}
```

and bind them through overload declarations:

```seal
[] :: overload {
    ArrayAt
}

[]= :: overload {
    ArraySet
}

len :: overload {
    ArrayLen
}
```

The storage representation of `Array<T, N>` is a standard-library design choice rather than a compiler-native array representation.

---

## 26. Operator and task overloads

Seal supports named overload sets and operator overload sets.

### 26.1 Named overload

```seal
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
```

Use:

```seal
a := Sum(1, 2)
b := Sum(1.0, 2.0)
```

The checker selects the candidate based on argument types. And also compiler time arguments.

```seal
SumInt :: task(a int, b int) int {
    return a + b
}

SumGen :: task<T type>(a T, b f64) f64 {
    return a + b
}

Sum :: overload {
    SumInt
    SumGen
}
```

### 26.2 Operator overload

```seal
Vec2 :: struct {
    x f32
    y f32
}

Vec2Add :: pure task(a Vec2, b Vec2) Vec2 {
    return Vec2{
        x = a.x + b.x,
        y = a.y + b.y,
    }
}

+ :: overload {
    Vec2Add
}
```

Use:

```seal
c := a + b
```

### 26.3 Package-owned overloads

An overload declared in the package that owns a type may participate when that imported type is used:

```seal
result := importedValue + other
```

The checker considers overloads associated with the operand types’ packages.

### 26.4 Candidate selection

Overload resolution belongs to the checker.

The checker records the exact selected task. The C backend consumes that decision rather than attempting a second independent overload search.

This rule is especially important for:

```text
[]
[]=
len
```

---

## 27. Indexing and `len` resolution

Indexing and `len` use exact semantic resolutions produced by the checker.

### 27.1 `[]` read overload

A user-defined index reader must have a compatible shape:

```seal
Get :: pure task(receiver *Container, index int) Element
```

or use a trusted-pure equivalent.

The receiver is normally:

- a pointer to a struct-backed container;
- a temporary pointer materialization for `string`.

Use:

```seal
value := container[index]
```

### 27.2 `[]=` write overload

A user-defined index setter has a compatible shape:

```seal
Set :: task(
    receiver *Container,
    index int,
    value Element,
) {
}
```

Use:

```seal
container[index] = value
```

The setter may be impure.

At present, overloaded index setters support plain `=`. Compound forms such as:

```seal
container[index] += 1
```

are not yet lowered as read-modify-write overload sequences.

### 27.3 `len` overload

A user-defined `len` task must be pure or trusted-pure and return `uint`:

```seal
ContainerLen :: pure task(receiver *Container) uint {
    // ...
}
```

Use:

```seal
count := len(container)
```

### 27.4 Built-in `len`

The remaining built-in length case is a variadic value:

```seal
Count :: task(values ...int) uint {
    return len(values)
}
```

### 27.5 Ordinary `len` task shadowing

An ordinary task named `len` shadows the primitive built-in name in its scope.

Therefore:

```seal
len :: task(value Custom) uint {
    // ...
}
```

is treated as an ordinary task call when it resolves normally.

---

## 28. Interfaces

An interface declares required tasks.

### 28.1 Static interface

```seal
Positioned :: interface {
    Position :: task(value *self) int
}
```

`self` refers to the implementing type.

A static interface is represented using a known-implementor tag and data pointer.

### 28.2 Interface with multiple requirements

```seal
Spatial :: interface {
    X :: task(value *self) int
    SetX :: task(value *self, x int)
}
```

### 28.3 Pure requirement

```seal
Readable :: interface {
    Read :: pure task(value *self) int
}
```

A pure requirement must be implemented by a pure or trusted-pure task.

### 28.4 Generic interface

```seal
Readable :: interface <T type> {
    Read :: task(value *self) T
}
```

Generic interface instances are specialized similarly to generic structs and tasks.

---

## 29. Implementations

### 29.1 Alias implementation

```seal
Transform :: struct {
    x int
}

ReadPosition :: task(transform *Transform) int {
    return transform.x
}

Positioned :: impl Transform {
    Position :: ReadPosition
}
```

### 29.2 Inline implementation task

```seal
Readable<T> :: impl <T type> Box<T> {
    Read :: task(box *Box<T>) T {
        return box.value
    }
}
```

### 29.3 Casting to an interface

```seal
transform := Transform{x = 42}
positioned := cast<Positioned>(&transform)
```

### 29.4 Calling a requirement

Interface requirements are invoked by requirement name:

```seal
position := Position(positioned)
```

The checker resolves the call to the appropriate interface dispatcher.

### 29.5 Signature validation

An implementation is checked for:

- missing requirements;
- extra entries;
- wrong parameter types;
- wrong result types;
- incompatible variadic parameters;
- purity violations.

---

## 30. Delegated implementations

An implementation can delegate through a field path:

```seal
Entity :: struct {
    transform Transform
}

Positioned :: impl Entity using transform
```

Now an `Entity` implements `Positioned` through its `transform` field.

Nested paths are supported:

```seal
Entity :: struct {
    components Components
}

Positioned :: impl Entity using components.transform
```

Pointer fields and pointer intermediates are also supported:

```seal
Entity :: struct {
    components *Components
}

Positioned :: impl Entity using components.transform
```

The compiler records each projection step so the C backend can generate the correct field and pointer traversal.

---

## 31. Dynamic interfaces

A dynamic interface uses a vtable:

```seal
Positioned :: dyn interface {
    Position :: task(value *self) int
    SetPosition :: task(value *self, position int)
}
```

Its runtime representation is conceptually:

```text
data pointer
vtable pointer
```

Cast and use it in the same source-level way:

```seal
positioned := cast<Positioned>(&transform)
before := Position(positioned)
SetPosition(positioned, before + 1)
```

The difference between static and dynamic interfaces comes from the interface declaration, not from a special cast syntax.

---

## 32. Generics

Seal uses explicit generic parameter lists:

```seal
Box :: struct <T type> {
    value T
}
```

```seal
Identity :: task <T type>(value T) T {
    return value
}
```

Use explicit generic arguments:

```seal
box := Box<int>{value = 10}
value := Identity<int>(10)
```

Generic type inference is not yet a stable feature. Prefer explicit arguments.

### 32.1 Type parameters

```seal
T type
```

### 32.2 Integer value parameters

```seal
N int
```

### 32.3 Boolean value parameters

```seal
Enabled bool
```

### 32.4 String value parameters

```seal
Name string
```

### 32.5 Task parameters

```seal
F task[(int) int]
```

### 32.6 Enum and union categories

The generic system also models enum and union categories for category-specific constraints.

### 32.7 Monomorphization

Each concrete instance receives a concrete C definition.

Example:

```seal
Identity<int>(10)
Identity<string>("hello")
```

produces separate task instances conceptually equivalent to:

```text
Identity_int
Identity_string
```

### 32.8 Package-qualified generics

```seal
box := types.MakeBox<int>(10)
```

Imported generic tasks and types are specialized through the cross-package request mechanism.

---

## 33. Generic task parameters

Tasks can be passed as compile-time generic arguments.

```seal
Double :: task(value int) int {
    return value * 2
}

Apply :: task <F task[(int) int]>(value int) int {
    return F(value)
}

Main :: task() {
    result := Apply<Double>(10)
}
```

A generic task argument must match the required signature.

A generic task can itself be specialized before being passed:

```seal
Identity :: task <T type>(value T) T {
    return value
}

result := Apply<Identity<int>>(10)
```

### 33.1 Type-dependent task signature

```seal
ApplyTyped :: task <
    T type,
    F task[(T) T],
>(value T) T {
    return F(value)
}
```

Use:

```seal
value := ApplyTyped<int, Identity<int>>(10)
```

### 33.2 Multi-result task parameters

```seal
Swap :: task <T type>(a T, b T) T, T {
    return b, a
}

ApplySwap :: task <
    T type,
    F task[(T, T) T, T],
>(a T, b T) T, T {
    return F(a, b)
}
```

Use:

```seal
x, y := ApplySwap<int, Swap<int>>(1, 2)
```

---

## 34. Generic constraints

Generic parameters can include constraints inside `[...]`.

### 34.1 Structural field constraint

```seal
HealthOf :: task <T type[health int]>(target T) int {
    return target.health
}
```

A valid type must contain:

```seal
health int
```

Example:

```seal
Player :: struct {
    health int
}
```

Use:

```seal
health := HealthOf<Player>(player)
```

### 34.2 Compile-time expression constraint

```seal
UseAge :: task <Age int[Age > 18]>() {
}
```

Valid:

```seal
UseAge<21>()
```

Invalid:

```seal
UseAge<18>()
```

### 34.3 Constraint calling a pure task

```seal
Over :: pure task(age int) bool {
    return age > 18
}

UseAge :: task <Age int[Over(Age)]>() {
}
```

### 34.4 Imported pure constraint task

```seal
UseAge :: task <Age int[rules.Over(Age)]>() {
}
```

The dependency package must export the task body so the checker can evaluate it.

### 34.5 Task-signature constraint

```seal
Apply :: task <
    T type,
    F task[(T) T],
>(value T) T {
    return F(value)
}
```

### 34.6 Enum variant constraint

```seal
UseReady :: task <E enum[Ready]>() {
}
```

The enum argument must contain `.Ready`.

### 34.7 Union member constraint

```seal
HandleNumber :: task <U union[int]>() {
}
```

The union argument must contain the required member type.

### 34.8 Interface implementation constraint

The checker supports constraints requiring a type to implement an interface. The syntax is still being stabilized, but the intended form is based on an interface type inside the type parameter’s constraint list, for example:

```seal
ReadValue :: task <
    T type[Readable<int>],
>(value *T) int {
    // ...
}
```

Treat this exact spelling as evolving until the standard library and public grammar are finalized.

---

## 35. Compile-time constraint evaluation

Generic value constraints are evaluated by the checker.

The compile-time evaluator currently supports important categories such as:

- booleans;
- integers;
- strings;
- unary operations;
- binary operations;
- pure task calls;
- imported pure task calls;
- pure operator overloads.

Example:

```seal
Matrix :: struct {
    years int
}

IsVampireAge :: pure task(age Matrix, tag string) bool {
    return age.years == 999 && tag == "vampire"
}

== :: overload {
    IsVampireAge
}
```

A generic constraint can use that overload when it is compile-time evaluable.

### 35.1 Depth and recursion guard

Configure the evaluator with:

```toml
[checker]
generic_constraint_max_depth = 32
```

A positive limit detects:

- direct or indirect recursive evaluation;
- evaluation exceeding the configured depth.

A value less than or equal to zero disables the guard.

---

## 36. Built-in and intrinsic tasks

Seal currently recognizes these compiler-level tasks:

```seal
assert(...)
len(...)
size(...)
anyAs<T>(...)
anyIs<T>(...)
panic(...)
trap()
unreachable()
cast<T>(...)
```

### 36.1 `assert`

```seal
assert(value > 0)
```

The condition must be `bool`.

### 36.2 `size`

For a type:

```seal
bytes := size(int)
bytes := size(Player)
```

For an ordinary value:

```seal
bytes := size(value)
```

For `string`, `size` returns UTF-8 byte length:

```seal
bytes := size(text)
```

### 36.3 `cast`

```seal
id := cast<UserId>(10)
pointer := cast<*Player>(raw)
```

Casts are explicit and lower to C casts where supported.

### 36.4 `anyIs`

```seal
isNumber := anyIs<int>(value)
```

### 36.5 `anyAs`

```seal
number := anyAs<int>(value)
```

### 36.6 `len`

`len` is special because it may be:

- a variadic built-in;
- a user-defined overload;
- an ordinary shadowing task.

The checker records which meaning applies to each call.

### 36.7 `panic`

```seal
panic(c"fatal error")
```

The exact accepted message forms depend on the current intrinsic signature and runtime support.

### 36.8 `trap`

```seal
trap()
```

Terminates through the generated runtime trap mechanism.

### 36.9 `unreachable`

```seal
unreachable()
```

Marks a path that must not be reached.

---

## 37. C interoperability

### 37.1 C include metadata

Use a named `@c_import` declaration:

```seal
c :: @c_import {
    include "stdlib.h"
    include "stdio.h"
}
```

The declaration name, such as `c`, is metadata and does not become a normal Seal symbol.

### 37.2 Extern task

```seal
malloc :: extern("malloc") task(size uint) rawptr
free :: extern("free") task(ptr rawptr)
```

Use:

```seal
ptr := malloc(64)
free(ptr)
```

The quoted name is the C link name.

### 37.3 Variadic C function

```seal
printf :: extern("printf") task(
    format cstring,
    args ...any,
) int
```

Use C strings for C format strings:

```seal
printf(c"%d %s\n", 10, c"hello")
```

### 37.4 Native C source files

Every `.c` file found under a package root is included in the final executable compilation.

Directories skipped during native C discovery include:

```text
.git
.seal
build
vendor
```

This permits a package layout such as:

```text
native/
├── seal.toml
├── bindings.seal
└── implementation.c
```

### 37.5 Include and link configuration

`seal.toml` can add:

```toml
[build]
include_dirs = ["include"]
library_dirs = ["lib"]
libraries = ["m"]
defines = ["FEATURE_X=1"]
c_flags = ["-Wall"]
link_flags = ["-static"]
```

---

## 38. Build output and C compilation

The default output directory is:

```text
<root-package>/.seal/build
```

The build creates one generated C file per package:

```text
.seal/build/app.c
.seal/build/types.c
.seal/build/mem.c
```

The executable output defaults to:

```text
.seal/build/<root-package-name>
```

On Windows, `.exe` is added when the output has no extension.

### 38.1 Emit-only mode

Emit-only mode generates package C files without invoking a C compiler.

Conceptually:

```go
BuildWorkspace(path, BuildOptions{
    EmitOnly: true,
})
```

### 38.2 Executable compilation

For an executable root package, the build command combines:

- generated C files for all dependency packages;
- native C files from all packages;
- configured compiler arguments;
- configured include paths;
- defines;
- libraries and linker flags.

Library packages do not independently produce final executables.

### 38.3 Package symbol names in C

Imported names are prefixed to avoid collisions:

```seal
types.Player
types.MakeBox<int>
```

may lower to names similar to:

```text
types_Player
types_MakeBox_int
```

These generated names are backend details and are not a stable external C ABI unless explicitly documented later.

---

## 39. Cross-package generic specialization

Seal uses a fixed-point process to generate imported generic instances.

Suppose package `types` contains:

```seal
Box :: struct <T type> {
    value T
}

MakeBox :: task <T type>(value T) Box<T> {
    return Box<T>{value = value}
}
```

and package `app` calls:

```seal
box := types.MakeBox<int>(10)
```

The process is:

```text
1. parse, resolve, and check every package;
2. export package signatures and semantic information;
3. generate app C;
4. app requests types.MakeBox<int>;
5. the request also identifies types.Box<int>;
6. regenerate the owning package with the requested instances;
7. repeat until no new requests appear;
8. write the final C files.
```

Requests are deduplicated and sorted deterministically.

The current maximum number of fixed-point iterations is:

```text
64
```

If generation does not converge within that limit, the build fails.

This mechanism supports nested cases where one generic specialization discovers another.

---

## 40. Current limitations and evolving areas

The following points are important when using the language today.

### 40.1 Standard library is still being built

Core packages such as:

```text
array
string utilities
memory
I/O
formatting
option/result
collections
testing
```

are still under development.

### 40.2 No compiler-native arrays

These forms have been removed:

```seal
[N]T
[]T
[1, 2, 3]
```

Use standard-library generic containers instead.

### 40.3 No builtin string indexing or `len(string)`

String indexing and logical length belong to standard-library overloads.

`size(string)` remains the byte-length operation.

### 40.4 Generic inference is not stable

Write:

```seal
Identity<int>(10)
```

rather than relying on:

```seal
Identity(10)
```

### 40.5 Generic unions are not yet complete

`Option<T>` and `Result<T, E>` remain planned foundational types.

### 40.6 Memory model is not finalized

Seal has:

- typed pointers;
- `rawptr`;
- C allocation access;
- `defer`.

It does not yet have a finalized ownership, borrowing, destructor, or garbage-collection model.

### 40.7 Some `seal.toml` policy flags are only partially enforced

The configuration parser accepts several safety and style policies, but the compiler does not yet apply every policy consistently in every phase.

### 40.8 Overloaded index compound assignment is incomplete

This works:

```seal
values[index] = value
```

This is not yet guaranteed for an overloaded setter:

```seal
values[index] += 1
```

### 40.9 Test-task tooling is evolving

`test task` syntax exists, while discovery, execution, reporting, and package-level test commands still need a polished public workflow.

### 40.10 LSP and editor support are future work

The compiler retains semantic information that can support:

- diagnostics;
- hover;
- go to definition;
- completion;
- signature help.

A dedicated `seal-lsp` and Zed extension are logical next projects after the initial standard library.

---

## 41. Complete multi-package example

Project layout:

```text
example/
├── seal.workspace
├── types/
│   ├── seal.toml
│   └── types.seal
└── app/
    ├── seal.toml
    └── main.seal
```

### 41.1 Workspace marker

`seal.workspace`:

```text
```

### 41.2 Library package

`types/seal.toml`:

```toml
[package]
name = "types"
version = "0.1.0"
kind = "library"
```

`types/types.seal`:

```seal
Box :: struct <T type> {
    value T
}

MakeBox :: task <T type>(value T) Box<T> {
    return Box<T>{value = value}
}

Player :: struct {
    health int
}

ReadHealth :: pure task(player *Player) int {
    return player.health
}

ReadableHealth :: interface {
    Health :: pure task(value *self) int
}

ReadableHealth :: impl Player {
    Health :: ReadHealth
}
```

### 41.3 Executable package

`app/seal.toml`:

```toml
[package]
name = "app"
version = "0.1.0"
kind = "executable"
dependencies = [
    { name = "types", version = "0.1.0" },
]

[build]
compiler = "cc"
standard = "c11"

[checker]
generic_constraint_max_depth = 32
```

`app/main.seal`:

```seal
Identity :: task <T type>(value T) T {
    return value
}

Apply :: task <T type, F task[(T) T]>(value T) T {
    return F(value)
}

Main :: task() {
    box := types.MakeBox<int>(10)
    assert(box.value == 10)

    player := types.Player{health = 42}
    readable := cast<types.ReadableHealth>(&player)
    assert(Health(readable) == 42)

    result := Apply<int, Identity<int>>(5)
    assert(result == 5)
}
```

This example demonstrates:

- a workspace;
- a library package;
- an executable package;
- package dependencies;
- package-qualified types and tasks;
- imported generic specialization;
- a static interface;
- an implementation;
- interface conversion;
- interface dispatch;
- a generic task parameter;
- build and checker configuration.

---

## 42. Syntax cheat sheet

### Constants

```seal
Pi :: 3.14159265
```

### Local variables

```seal
x := 10
x: int = 10
x: int
```

### Task

```seal
Add :: task(a int, b int) int {
    return a + b
}
```

### Pure task

```seal
Square :: pure task(value int) int {
    return value * value
}
```

### Default parameter

```seal
Increment :: task(value int, amount int = 1) int {
    return value + amount
}
```

### Variadic task

```seal
Count :: task(values ...int) uint {
    return len(values)
}
```

### Spread forwarding

```seal
Forward :: task(values ...int) uint {
    return Count(values...)
}
```

### Multi-return

```seal
Swap :: task(a int, b int) int, int {
    return b, a
}

x, y := Swap(1, 2)
```

### Struct

```seal
Player :: struct {
    health int
}

player := Player{health = 100}
```

### Distinct type

```seal
UserId :: distinct uint
id := cast<UserId>(10)
```

### Enum

```seal
Status :: enum {
    Ready
    Done
}

status: Status = .Ready
```

### Union

```seal
Value :: union {
    int
    string
}
```

### Pointer

```seal
pointer := &value
```

### Generic struct

```seal
Box :: struct <T type> {
    value T
}

box := Box<int>{value = 10}
```

### Generic task

```seal
Identity :: task <T type>(value T) T {
    return value
}

value := Identity<int>(10)
```

### Generic value constraint

```seal
UsePositive :: task <N int[N > 0]>() {
}
```

### Generic field constraint

```seal
HealthOf :: task <T type[health int]>(value T) int {
    return value.health
}
```

### Generic task parameter

```seal
Apply :: task <F task[(int) int]>(value int) int {
    return F(value)
}
```

### Named overload

```seal
Sum :: overload {
    SumInt
    SumF64
}
```

### Operator overload

```seal
+ :: overload {
    Vec2Add
}
```

### Index overloads

```seal
[] :: overload {
    ArrayAt
}

[]= :: overload {
    ArraySet
}

len :: overload {
    ArrayLen
}
```

### Static interface

```seal
Positioned :: interface {
    Position :: task(value *self) int
}
```

### Dynamic interface

```seal
Positioned :: dyn interface {
    Position :: task(value *self) int
}
```

### Implementation

```seal
Positioned :: impl Transform {
    Position :: ReadPosition
}
```

### Delegated implementation

```seal
Positioned :: impl Entity using components.transform
```

### Interface conversion

```seal
positioned := cast<Positioned>(&entity)
```

### C include

```seal
c :: @c_import {
    include "stdlib.h"
}
```

### Extern task

```seal
malloc :: extern("malloc") task(size uint) rawptr
```

### Package dependency

```toml
dependencies = ["mem", "types"]
```

### Package-qualified use

```seal
ptr := mem.Alloc(64)
box := types.MakeBox<int>(10)
```

---

## Final characterization

Seal currently has the core architecture of a serious compiled language:

```text
static typing
C backend
multi-package workspaces
local dependency graph
monomorphized generic structs and tasks
generic compile-time values
generic task parameters
generic constraints
single and multiple return values
static and dynamic interfaces
delegated implementations
named and operator overloads
tagged unions
distinct types
any boxing
variadic tasks
C interoperability
cross-package generic fixed-point generation
semantic side tables for exact lowering decisions
```

The next major stage is to make the language comfortable to use by building:

```text
a coherent standard library
Array<T, N>
string operations
memory and I/O packages
Option and Result
a test runner
a Tree-sitter grammar
seal-lsp
a Zed extension
```
