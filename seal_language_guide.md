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
25. [`@inline_array`](#25-inline_array)
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
38. [Foreign ABI directives](#38-foreign-abi-directives)
39. [Build output and C compilation](#38-build-output-and-c-compilation)
40. [Cross-package generic specialization](#39-cross-package-generic-specialization)
41. [Current limitations and evolving areas](#40-current-limitations-and-evolving-areas)
42. [Complete multi-package example](#41-complete-multi-package-example)
43. [Syntax cheat sheet](#42-syntax-cheat-sheet)

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

The build system can be pointed at any file or directory inside the package:

```bash
sealc build ./
```

The path is optional and defaults to the current directory:

```bash
sealc build
```

To generate C without invoking the host C compiler:

```bash
sealc build ./ --emit-c
```

To choose an output path:

```bash
sealc build ./ -o hello
```

To temporarily override the compiler selected in `seal.toml`:

```bash
sealc build ./ -compiler gcc
```

The compiler override applies only to the current invocation and does not modify project configuration.

Internally, a normal build is equivalent to:

```go
BuildWorkspace(path, BuildOptions{})
```

Emit-only mode uses:

```go
BuildOptions{
    EmitOnly: true,
}
```

A temporary compiler selection uses:

```go
BuildOptions{
    Compiler: "gcc",
}
```

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

### 5.2 Build and compiler configuration

Seal separates common build configuration from compiler-specific profiles.

The selected compiler is normally declared under `[build]`:

```toml
[build]
compiler = "gcc"
```

Common settings may still be placed under `[build]`:

```toml
[build]
compiler = "gcc"
standard = "c11"
c_flags = [
    "-Wall",
    "-Wextra",
]
```

Multiline string arrays are supported for all array-valued configuration keys:

```toml
[build]
c_flags = [
    "-Wall",
    "-Wextra",
    "-Wpedantic",
]

include_dirs = [
    "include",
    "third_party/include",
]
```

#### Common build keys

| Key             |         Type |    Default | Meaning                                                                   |
| --------------- | -----------: | ---------: | ------------------------------------------------------------------------- |
| `compiler`      |       string |       `""` | Selected compiler or compiler profile                                     |
| `compiler_path` |       string |       `""` | Explicit compiler executable path                                         |
| `compiler_args` | string array |      empty | Arguments inserted immediately after the compiler executable              |
| `c_flags`       | string array |      empty | Additional compilation flags                                              |
| `link_flags`    | string array |      empty | Additional linker flags                                                   |
| `include_dirs`  | string array |      empty | Converted to `-I...`                                                      |
| `library_dirs`  | string array |      empty | Converted to `-L...`                                                      |
| `libraries`     | string array |      empty | Converted to `-l...`                                                      |
| `defines`       | string array |      empty | Converted to `-D...`                                                      |
| `target`        |       string |       `""` | Compilation target; handled automatically for supported compiler profiles |
| `standard`      |       string |    `"c11"` | C language standard                                                       |
| `linkage`       |       string | `"static"` | Requested linkage mode                                                    |

These common fields provide the base configuration before compiler profiles are applied.

#### Compiler profiles

Compiler-specific settings use sections of the form:

```toml
[compiler.<name>]
```

A default compiler profile may be declared as:

```toml
[compiler.default]
standard = "c11"
c_flags = [
    "-Wall",
    "-Wextra",
]
```

Named profiles can override individual settings:

```toml
[compiler.gcc]
path = "gcc"
c_flags = [
    "-Wall",
    "-Wextra",
    "-Wpedantic",
]

[compiler.clang]
path = "clang"
c_flags = [
    "-Wall",
    "-Wextra",
    "-Wconversion",
]

[compiler.zigcc]
path = "zig"
args = ["cc"]
target = "x86_64-windows-gnu"
```

Supported compiler-profile fields are:

| Key             |         Type | Meaning                                           |
| --------------- | -----------: | ------------------------------------------------- |
| `path`          |       string | Compiler executable path or command               |
| `compiler_path` |       string | Alias of `path`                                   |
| `args`          | string array | Arguments placed immediately after the executable |
| `compiler_args` | string array | Alias of `args`                                   |
| `c_flags`       | string array | Compilation flags                                 |
| `link_flags`    | string array | Linker flags                                      |
| `include_dirs`  | string array | Include directories                               |
| `library_dirs`  | string array | Library search directories                        |
| `libraries`     | string array | Libraries to link                                 |
| `defines`       | string array | Preprocessor definitions                          |
| `target`        |       string | Compiler target                                   |
| `standard`      |       string | C standard                                        |
| `linkage`       |       string | Requested linkage mode                            |

`compiler.default` cannot select an executable path. It supplies fallback settings shared by compiler profiles.

#### Configuration precedence

Compiler configuration is resolved in this order:

```text
built-in defaults
    ↓
[build] common settings
    ↓
[compiler.default]
    ↓
[compiler.<selected compiler>]
    ↓
temporary command-line compiler selection
```

A named compiler profile replaces any field it explicitly defines. Fields it omits continue to inherit from the preceding configuration layer.

For array fields, a profile replaces the inherited array rather than appending to it.

For example:

```toml
[compiler.default]
c_flags = [
    "-Wall",
]

[compiler.gcc]
c_flags = [
    "-Wall",
    "-Wextra",
]
```

selecting `gcc` produces:

```text
-Wall -Wextra
```

rather than duplicating `-Wall`.

An explicitly empty array clears an inherited array:

```toml
[compiler.clang]
c_flags = []
```

#### Compiler names

Recognized compiler names include:

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

Aliases are normalized internally:

```text
zig-cc → zigcc
zig cc → zigcc
cl     → msvc
```

Custom compiler names are accepted:

```toml
[build]
compiler = "x86_64-w64-mingw32-gcc"
```

When no matching named profile exists, the selected compiler still inherits `[compiler.default]`.

Example:

```toml
[build]
compiler = "tcc"

[compiler.default]
standard = "c11"
c_flags = [
    "-Wall",
]
```

This invokes `tcc` with the default profile settings.

#### Zig CC example

```toml
[build]
compiler = "zigcc"

[compiler.default]
standard = "c11"

[compiler.zigcc]
path = "zig"
args = ["cc"]
target = "x86_64-windows-gnu"
```

The resulting command begins with:

```text
zig cc -target x86_64-windows-gnu
```

#### Temporary command-line override

The compiler selected in `seal.toml` may be temporarily overridden for one build:

```bash
sealc build ./ -compiler gcc
```

Equivalent accepted forms include:

```bash
sealc build -compiler gcc ./
sealc build ./ --compiler clang
sealc build ./ --compiler=zigcc
sealc build ./ -compiler=gcc
```

The override selects another compiler and its matching profile. It does not modify `seal.toml`.

For example:

```toml
[build]
compiler = "clang"

[compiler.default]
standard = "c11"
c_flags = [
    "-Wall",
    "-Wextra",
]

[compiler.gcc]
c_flags = [
    "-Wall",
    "-Wextra",
    "-Wpedantic",
]
```

A normal build uses Clang:

```bash
sealc build ./
```

A temporary build can use GCC:

```bash
sealc build ./ -compiler gcc
```

The GCC build receives the `compiler.gcc` profile while the package configuration remains unchanged.


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

`char` stores one Unicode scalar value.

It currently lowers to:

```text
char → C uint32_t
```

Examples:

```seal
ascii: char = 'A'
unicode: char = 'ñ'
emoji: char = '😀'
```

A `char` is not a UTF-8 byte and is not equivalent to the C type `char`.

For example, `'ñ'` is represented as one Seal `char`, even though its UTF-8 encoding occupies two bytes.

A valid `char` literal must contain exactly one Unicode scalar value. The compiler rejects:

* empty character literals;
* literals containing multiple Unicode scalar values;
* invalid UTF-8;
* surrogate code points;
* values outside the Unicode scalar range.

`char` represents a Unicode scalar, not a user-perceived grapheme cluster. A displayed character composed from multiple Unicode scalars may therefore require more than one Seal `char`.

---

### 9.4 Pointer, text, and dynamic types

```text
rawptr  → void *
cstring → const char *
char    → uint32_t
```

`string` uses a compiler-generated runtime structure equivalent to:

```c
typedef struct sealString {
    const uint8_t *data;
    uintptr_t len;
} sealString;
```

The `len` field in this C structure is the number of UTF-8 bytes, not the number of Unicode scalar values.

The structure fields are backend details and are not directly accessible from Seal source.

`any` also uses a compiler-generated tagged runtime representation.

---

## 10. Literals and values

### 10.1 Booleans

```seal
enabled := true
finished := false
```

### 10.2 Integers and floats

Decimal and hexadecimal integer literals are supported:

```seal
decimal := 255
hexadecimal := 0xFF
uppercase := 0X10FFFF

mask: u8 = 0x80
maximumByte: u8 = 0xFF
```

Hexadecimal literals use the `0x` or `0X` prefix. Hexadecimal digits are case-insensitive:

```seal
0xABCD
0xabcd
```

Underscores may separate digits for readability:

```seal
decimal := 1_000_000
hexadecimal := 0x10_FFFF
```

Underscores do not affect the value. Equivalent spellings represent the same integer:

```seal
16
0x10
1_6
0x1_0
```

Integer literals begin as untyped compile-time values. They must fit the type required by their context:

```seal
valid: u8 = 0xFF

// Invalid: 256 is outside the range of u8.
invalid: u8 = 0x100
```

When no explicit type is supplied, an integer literal is inferred as `int` and must fit its range:

```seal
count := 10
```

Use an explicit type when the value does not fit `int`:

```seal
large: u64 = 0xFFFF_FFFF_FFFF_FFFF
```

Negative values use the unary `-` operator:

```seal
minimum: i8 = -128

// Invalid:
negativeUnsigned: u8 = -1
```

Floating-point literals use decimal notation:

```seal
ratio := 3.14
precise: f64 = 3.14159265
```

### 10.3 Characters

```seal
ascii: char = 'A'
unicode: char = 'ñ'
emoji: char = '😀'
```

A character literal contains exactly one Unicode scalar value.

The compiler validates that the literal:

* is valid UTF-8;
* decodes to exactly one scalar;
* is a valid Unicode scalar value.

A `char` is represented as a 32-bit unsigned value. It does not store the original UTF-8 byte sequence.

### 10.4 Seal strings

```seal
message: string = "hello"
unicode: string = "mañana"
```

A `string` literal contains immutable UTF-8 data and a byte length.

String literals may contain embedded null bytes because a Seal `string` is length-delimited rather than null-terminated:

```seal
value := "left\0right"
```

A string literal must contain valid UTF-8.

### 10.5 C strings

```seal
format: cstring = c"%d\n"
name: cstring = c"Seal"
```

The `c"..."` prefix creates an immutable, null-terminated UTF-8 C string literal.

A `cstring` literal cannot contain an embedded null byte:

```seal
// Invalid:
value := c"left\0right"
```

The compiler also requires a `cstring` literal to contain valid UTF-8.

The terminating null byte is part of the C storage but is not included in `size(cstring)` or `len(cstring)`.

---

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

break and continue only affect for loops. Internally are goto to avoid C collisions with switch statements.

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

### 23.7 Strings, C strings, and raw pointers

`string` and `cstring` are immutable text views.

They support read-only Unicode-scalar indexing:

```seal
text := "mañana"
first: char = text[0]

cText := c"mañana"
last: char = cText[-1]
```

They do not support indexed assignment:

```seal
// Invalid:
text[0] = 'M'

// Invalid:
cText[0] = 'M'
```

Indexing a `string` or `cstring` returns `char`, not `u8`.

Raw pointers retain byte-based indexing:

```seal
ptr: rawptr

firstByte: u8 = ptr[0]
ptr[0] = 255
```

Use an explicit cast when the underlying byte pointer is required:

```seal
text := "hello"
data := cast<rawptr>(text)

cText := c"hello"
cData := cast<rawptr>(cText)
```

These casts produce borrowed pointers. They do not allocate, copy, or transfer ownership.

Writing through a raw pointer obtained from `string` or `cstring` is invalid behavior because both text types represent immutable data.

---

## 24. Strings, C strings, and characters

Seal distinguishes three text-related primitive types:

```seal
string
cstring
char
```

They serve different purposes:

```text
string  → immutable UTF-8 pointer plus explicit byte length
cstring → immutable null-terminated UTF-8 C string
char    → one Unicode scalar value
```

### 24.1 `string`

A Seal `string` is an immutable, length-delimited UTF-8 view.

```seal
text := "hello"
```

Its generated C representation is equivalent to:

```c
typedef struct sealString {
    const uint8_t *data;
    uintptr_t len;
} sealString;
```

The stored length is measured in bytes.

The pointer and length are not accessible as Seal fields:

```seal
// Invalid:
bytes := text.len

// Invalid:
pointer := text.data
```

Use the language operations instead:

```seal
bytes := size(text)
characters := len(text)
pointer := cast<rawptr>(text)
```

A `string`:

* is immutable;
* may contain embedded null bytes;
* is not required to have a trailing null byte;
* does not own or free its storage by itself;
* may refer to static, stack, foreign, or manually allocated memory;
* must not outlive the storage it references.

### 24.2 `cstring`

A `cstring` is an immutable borrowed pointer to null-terminated UTF-8 data.

```seal
text := c"hello"
```

It lowers directly to:

```c
const char *
```

Unlike `string`, a `cstring` does not store a byte length. Operations that need its length scan until the first null byte.

A `cstring`:

* must be null-terminated;
* cannot represent embedded null bytes as part of its text;
* is immutable;
* is directly compatible with ordinary C APIs accepting `const char *`;
* does not own or free its storage by itself.

### 24.3 `char`

A `char` stores one Unicode scalar value:

```seal
letter := 'ñ'
```

It lowers to:

```c
uint32_t
```

`char` is not equivalent to:

* one UTF-8 byte;
* one C `char`;
* one grapheme cluster;
* one displayed glyph in every writing system.

For example, a combining sequence can contain multiple Unicode scalars even when it appears visually as one character.

### 24.4 Byte length with `size`

For `string`, `size` returns the stored number of UTF-8 bytes:

```seal
ascii := "hello"
assert(size(ascii) == 5)

unicode := "ñ"
assert(size(unicode) == 2)
```

For `cstring`, `size` scans until the null terminator and returns the number of bytes before it:

```seal
value := c"hello"
assert(size(value) == 5)
```

The terminating null byte is not included.

For both text types:

```text
size(text) → UTF-8 byte count
```

### 24.5 Unicode-scalar length with `len`

`len(string)` and `len(cstring)` return the number of Unicode scalar values:

```seal
ascii := "hello"
assert(len(ascii) == 5)

unicode := "mañana"
assert(len(unicode) == 6)

single := "ñ"
assert(size(single) == 2)
assert(len(single) == 1)
```

For `string`, the runtime decodes exactly the stored byte range.

For `cstring`, the runtime first finds the terminating null byte and then decodes that byte range.

Invalid UTF-8 encountered through a borrowed runtime view causes a runtime failure.

Because Unicode scalar counting requires decoding UTF-8, `len(string)` and `len(cstring)` are linear-time operations.

### 24.6 Unicode-scalar indexing

Both text types support read-only indexing:

```seal
text := "Seal"
first: char = text[0]

cText := c"Seal"
second: char = cText[1]
```

Indexing returns a `char`:

```seal
letter: char = "ñ"[0]
```

It does not return a UTF-8 byte:

```seal
// Invalid because indexing returns char:
byte: u8 = "hello"[0]
```

Indexes are measured in Unicode scalar values rather than bytes.

UTF-8 decoding is therefore performed while locating the requested scalar.

### 24.7 Negative indexes

String and C-string indexes may be negative:

```seal
text := "Seal"

last := text[-1]
beforeLast := text[-2]

assert(last == 'l')
assert(beforeLast == 'a')
```

A negative index is interpreted relative to the number of Unicode scalar values:

```text
-1 → final scalar
-2 → scalar before the final scalar
```

An index outside the valid range causes a runtime failure.

Because negative indexing needs the scalar count, it may require an additional scan.

### 24.8 Immutability

`string` and `cstring` indexing is read-only:

```seal
text := "hello"

// Invalid:
text[0] = 'H'
```

```seal
text := c"hello"

// Invalid:
text[0] = 'H'
```

There is no built-in `[]=` operation for either text type.

Mutable text must use an explicitly mutable buffer or a future standard-library owned string type.

### 24.9 Equality

Equality for two `string` values compares:

1. byte lengths;
2. UTF-8 bytes.

```seal
assert("hello" == "hello")
assert("hello" != "world")
```

Equality for two `cstring` values compares their null-terminated contents:

```seal
assert(c"hello" == c"hello")
assert(c"hello" != c"world")
```

Text equality is content-based rather than pointer-based.

`string` and `cstring` are different types and are not implicitly compared with each other:

```seal
sealText := "hello"
cText := c"hello"

// Invalid without an explicit conversion:
same := sealText == cText
```

Equality compares encoded text exactly. It does not perform Unicode normalization. Canonically equivalent but differently encoded strings may compare unequal.

### 24.10 Borrowed cast from `string` to `rawptr`

```seal
text := "hello"
data := cast<rawptr>(text)
```

This returns the string’s UTF-8 data pointer.

The cast:

* does not allocate;
* does not copy;
* does not append a null terminator;
* does not transfer ownership;
* does not extend the storage lifetime.

The resulting pointer must not be used after the original storage becomes invalid.

The pointer is not guaranteed to reference null-terminated data.

### 24.11 Borrowed cast from `cstring` to `rawptr`

```seal
text := c"hello"
data := cast<rawptr>(text)
```

This returns the underlying null-terminated C pointer.

The cast is borrowed and performs no allocation or copy.

Although `rawptr` itself permits low-level writes, writing through a pointer derived from a `cstring` is invalid because `cstring` data is immutable and may reside in read-only static storage.

### 24.12 Constructing a borrowed `string` view

A `string` view can be constructed from a raw pointer and a byte length:

```seal
view := cast<string>(data, byteLength)
```

The arguments are:

```text
data       rawptr
byteLength uint
```

The resulting string refers to exactly `byteLength` bytes beginning at `data`.

This cast:

* does not allocate;
* does not copy;
* does not take ownership;
* does not add a null terminator;
* allows embedded null bytes;
* requires the supplied byte range to remain valid for the view’s lifetime.

The bytes must contain valid UTF-8 before operations such as `len` or indexing are used.

Example:

```seal
source := "hello"
data := cast<rawptr>(source)
view := cast<string>(data, size(source))

assert(view == source)
```

### 24.13 Constructing a borrowed `cstring` view

A `cstring` view is constructed from a raw pointer and the expected byte length:

```seal
view := cast<cstring>(data, byteLength)
```

The arguments are:

```text
data       rawptr
byteLength uint
```

The runtime verifies that:

```text
data[byteLength] == 0
```

The byte length excludes the terminating null byte.

Example:

```seal
source := c"hello"
data := cast<rawptr>(source)
view := cast<cstring>(data, size(source))

assert(view == source)
```

This cast:

* does not allocate;
* does not copy;
* does not take ownership;
* returns the original pointer as a borrowed `cstring`;
* requires at least `byteLength + 1` accessible bytes;
* requires a null byte at exactly `data[byteLength]`.

Passing an invalid pointer, an incorrect length, or storage that cannot safely be read at `data[byteLength]` is invalid low-level behavior.

### 24.14 No direct `string` to `cstring` cast

A `string` cannot be cast directly to `cstring`:

```seal
text := "hello"

// Invalid:
cText := cast<cstring>(text)
```

A `string` is not guaranteed to:

* have a trailing null byte;
* contain no embedded null bytes;
* remain valid for the lifetime expected by a C API.

A standard-library conversion from runtime `string` to `cstring` must allocate or otherwise provide explicitly managed null-terminated storage.

Such a conversion should expose ownership clearly rather than pretending the result is a free borrowed cast.

### 24.15 Ownership and lifetime

Plain `string` and `cstring` values are views. They do not automatically free memory.

For manually allocated storage:

```seal
data := mem.Alloc(byteCount)

// Populate the bytes...

view := cast<string>(data, byteCount)

// Use view while data remains alive.

mem.Free(data)
```

After `mem.Free(data)`, every `string`, `cstring`, or raw pointer referring to that allocation is dangling and must not be used.

Only the owner of the allocation should free it.

A borrowed text view must never be freed independently from its backing allocation.

### 24.16 C interoperability

Use `cstring` for ordinary C APIs that accept `const char *`:

```seal
puts :: extern("puts") task(text cstring) int

Main :: task() {
    puts(c"Hello from Seal")
}
```

Use `string` when the API accepts a pointer and explicit byte count:

```seal
WriteBytes :: task(text string) {
    data := cast<rawptr>(text)
    count := size(text)

    // Pass data and count to an appropriate C API.
}
```

A C function returning a borrowed `const char *` can be declared as returning `cstring`:

```seal
GetName :: extern("get_name") task() cstring
```

The pointer’s lifetime and ownership are determined by the C API contract.

Do not free a returned `cstring` unless the C API explicitly transfers ownership and specifies the matching deallocation function.

### 24.17 Mutable C buffers

`cstring` represents `const char *`, not mutable `char *`.

A C function that writes into a character buffer should use `rawptr` or another explicitly mutable byte-buffer type:

```seal
ReadIntoBuffer :: extern("read_into_buffer") task(
    destination rawptr,
    capacity uint,
) int
```

After the C call writes a terminator, a borrowed view may be created with:

```seal
text := cast<cstring>(destination, writtenByteLength)
```

### 24.18 Performance characteristics

Current text operations have these general costs:

```text
size(string)       O(1)
size(cstring)      O(n bytes)

len(string)        O(n bytes)
len(cstring)       O(n bytes)

string[index]      O(n bytes)
cstring[index]     O(n bytes)

string equality    O(n bytes)
cstring equality   O(n bytes)
```

Negative indexing may require both scalar counting and scalar lookup.

The current implementation favors a small, C-compatible representation over cached Unicode metadata.

---

## 25. `@inline_array`

Seal has no array primitive. The old forms:

```seal
[N]T
[]T
[1, 2, 3]
```

remain obsolete.

For low-level fixed-size inline storage, Seal provides the compiler directive:

```seal
@inline_array<T, N>
```

Example:

```seal
values := @inline_array<int, 4>(
    10,
    20,
    30,
    40,
)
```

The length must be a non-negative compile-time integer. Generic compile-time lengths are supported:

```seal
Storage :: struct <
    T type,
    N int[N >= 0],
> {
    data @inline_array<T, N>
}
```

Inline arrays may be nested:

```seal
matrix := @inline_array<
    @inline_array<int, 3>,
    2
>(
    @inline_array<int, 3>(1, 2, 3),
    @inline_array<int, 3>(4, 5, 6),
)
```

They support direct indexing, indexed assignment, and `len`:

```seal
value := matrix[1][2]
matrix[0][1] = 99

rows := len(matrix)
columns := len(matrix[0])
```

`@inline_array` is intentionally a directive rather than a primitive type because it is not a general-purpose first-class array value. Important restrictions include:

* the length is fixed at compile time;
* the storage cannot grow or shrink;
* whole-array assignment is not supported;
* a raw inline array cannot be used directly as a task parameter or result;
* non-empty initialization requires exactly the declared number of values;
* ownership, allocation, resizing, slicing, and higher-level collection behavior are not provided.

Wrapping inline storage in a struct removes the task-signature restriction because the struct itself is an ordinary value:

```seal
Buffer :: struct <
    T type,
    N int[N >= 0],
> {
    data @inline_array<T, N>
}
```

The intended role of `@inline_array` is to provide exact inline storage for structs and other low-level abstractions, not to replace higher-level collection types.

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

The checker distinguishes:

* compiler-provided `@inline_array` indexing, assignment, and length;
* compiler-provided text indexing and length;
* compiler-provided variadic indexing and length where applicable;
* raw-pointer byte indexing;
* user-defined `[]` overloads;
* user-defined `[]=` overloads;
* user-defined `len` overloads;
* ordinary tasks that happen to be named `len`.

The checker records the selected meaning. The C backend consumes that decision rather than repeating overload resolution.

### 27.1 `@inline_array` indexing and length

An inline array has built-in indexed reads and writes:

```seal
values := @inline_array<int, 4>(
    10,
    20,
    30,
    40,
)

value := values[2]
values[1] = 99
```

Nested indexing follows the stored element types recursively:

```seal
value := matrix[1][2]
```

`len` returns the logical compile-time element count as `uint`:

```seal
count := len(values)
```

Indexed inline-array elements remain addressable when their containing storage is addressable. This allows mixed expressions where built-in inline-array indexing produces a struct receiver for an overloaded `[]`, `[]=`, or `len` operation.

### 27.2 Built-in text indexing

`string` and `cstring` have built-in read-only indexing:

```seal
stringValue := "mañana"
character: char = stringValue[2]

cstringValue := c"mañana"
last: char = cstringValue[-1]
```

The index type is `int`.

The result type is `char`.

Indexes refer to Unicode scalar values, not UTF-8 bytes.

Negative indexes are supported.

Neither type supports built-in index assignment.

### 27.3 Raw-pointer indexing

`rawptr` uses built-in byte indexing:

```seal
byte: u8 = pointer[index]
pointer[index] = 255
```

The index type is `int` and the indexed value type is `u8`.

### 27.4 `[]` read overload

A user-defined index reader must have a compatible shape:

```seal
Get :: pure task(
    receiver *Container,
    index int,
) Element
```

The receiver must be a pointer to a struct-backed type.

The task must be pure or trusted-pure.

Use:

```seal
value := container[index]
```

Built-in `string`, `cstring`, and `rawptr` indexing takes precedence over user-defined container behavior for those primitive types.

### 27.5 `[]=` write overload

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

`string` and `cstring` cannot use indexed assignment because they are immutable built-in text types.

### 27.6 Built-in text `len`

`len(string)` and `len(cstring)` are built-in operations returning `uint`:

```seal
sealLength: uint = len("mañana")
cLength: uint = len(c"mañana")
```

The result is the number of Unicode scalar values.

It is not the UTF-8 byte count. Use `size` for bytes.

### 27.7 Built-in variadic `len`

A variadic value also has built-in length:

```seal
Count :: task(values ...int) uint {
    return len(values)
}
```

The result is the number of variadic elements.

### 27.8 User-defined `len` overload

A user-defined `len` task must:

* receive a pointer to a struct-backed type;
* be pure or trusted-pure;
* return `uint`.

Example:

```seal
ContainerLen :: pure task(
    receiver *Container,
) uint {
    // ...
}
```

Use:

```seal
count := len(container)
```

### 27.9 Ordinary `len` task shadowing

An ordinary task named `len` shadows the primitive built-in name when it resolves normally in that scope.

Therefore:

```seal
len :: task(value Custom) uint {
    // ...
}
```

is treated as an ordinary task call when selected by normal task resolution.

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

For a type, `size` returns its generated C storage size:

```seal
bytes := size(int)
bytes := size(Player)
bytes := size(char)
```

For an ordinary value, it normally returns the generated C storage size of that value.

Text values have special behavior.

For `string`, `size` returns the stored UTF-8 byte length:

```seal
text := "ñ"
assert(size(text) == 2)
```

For `cstring`, `size` scans to the null terminator and returns the number of bytes before it:

```seal
text := c"ñ"
assert(size(text) == 2)
```

The terminating null byte is not included.

Use `len` when the number of Unicode scalar values is required.

### 36.3 `cast`

Ordinary explicit conversions use:

```seal
id := cast<UserId>(10)
pointer := cast<*Player>(raw)
```

Text-related casts have specific forms.

Borrow a string’s data pointer:

```seal
data := cast<rawptr>(text)
```

Borrow a C string’s pointer:

```seal
data := cast<rawptr>(cText)
```

Construct a borrowed length-delimited string view:

```seal
text := cast<string>(data, byteLength)
```

Construct a borrowed null-terminated C-string view:

```seal
cText := cast<cstring>(data, byteLength)
```

The `cstring` constructor verifies that the byte immediately following the supplied text range is null:

```text
data[byteLength] == 0
```

These casts do not allocate, copy, free, or transfer ownership.

A direct cast from `string` to `cstring` is invalid because a `string` is not guaranteed to be null-terminated.

### 36.4 `anyIs`

```seal
isNumber := anyIs<int>(value)
```

### 36.5 `anyAs`

```seal
number := anyAs<int>(value)
```

### 36.6 `len`

`len` may resolve to:

* built-in element count for `@inline_array`;
* built-in Unicode-scalar length for `string`;
* built-in Unicode-scalar length for `cstring`;
* built-in element count for variadic values;
* a user-defined `len` overload;
* an ordinary shadowing task named `len`.

Examples:

```seal
inlineCount := len(
    @inline_array<int, 3>(
        10,
        20,
        30,
    ),
)

characters := len("mañana")
cCharacters := len(c"mañana")
items := len(variadicValues)
containerLength := len(container)
```

For text:

```text
len  → Unicode scalar count
size → UTF-8 byte count
```

For `@inline_array<T, N>`:

```text
len → N
```

The checker records which meaning applies to each call.


### 36.7 `panic`

`panic` accepts either `string` or `cstring`:

```seal
panic("fatal error")
panic(c"fatal error")
```

A `string` message is written using its explicit byte length, so it does not require null termination.

A `cstring` message is written using its null terminator.

Calling `panic` prints the message through the generated runtime and terminates the program.

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

### 37.4 Text types in C declarations

Use `cstring` for C parameters and results declared as immutable null-terminated strings:

```c
const char *
```

Seal declaration:

```seal
GetWindowTitle :: extern("get_window_title") task() cstring

SetWindowTitle :: extern("set_window_title") task(
    title cstring,
)
```

C-string literals pass directly:

```seal
SetWindowTitle(c"Seal application")
```

Use `string` only for Seal-facing APIs or for C wrappers that explicitly receive both a pointer and byte count.

A `string` is not ABI-compatible with `const char *` because it is a two-field structure.

To pass a Seal string to a pointer-and-length C API:

```seal
text := "hello"
data := cast<rawptr>(text)
byteLength := size(text)
```

To pass a runtime Seal string to a C API requiring `const char *`, first create explicitly owned null-terminated storage through a standard-library conversion. Do not directly reinterpret a `string` as `cstring`.

### 37.5 Borrowed C results

An extern task returning `cstring` returns a borrowed `const char *` view:

```seal
GetError :: extern("library_get_error") task() cstring
```

The C library determines:

* whether the pointer may be null;
* how long it remains valid;
* whether it is static, thread-local, object-owned, or caller-owned;
* whether the caller must free it.

Seal does not automatically free a returned `cstring`.

### 37.6 Mutable character buffers

Do not use `cstring` for C parameters declared as mutable `char *`.

Use `rawptr` or a dedicated mutable buffer abstraction:

```seal
FillBuffer :: extern("fill_buffer") task(
    destination rawptr,
    capacity uint,
) int
```

After the function writes valid UTF-8 and a terminating null byte, construct a borrowed view with the known written length:

```seal
view := cast<cstring>(destination, writtenLength)
```

### 37.7 Native C source files

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

### 37.8 Include and link configuration

Common include and link settings may be placed under `[build]`:

```toml
[build]
include_dirs = [
    "include",
]

library_dirs = [
    "lib",
]

libraries = [
    "m",
]

defines = [
    "FEATURE_X=1",
]

c_flags = [
    "-Wall",
]

link_flags = [
    "-static",
]
```

These fields may also be supplied through compiler profiles:

```toml
[compiler.default]
include_dirs = [
    "include",
]

defines = [
    "FEATURE_X=1",
]

[compiler.gcc]
c_flags = [
    "-Wall",
    "-Wextra",
]

link_flags = [
    "-static",
]
```

Compiler-profile fields override matching common fields when their profile is selected.

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

### 38.1 Build command

The normal command is:

```bash
sealc build <path>
```

The path is optional:

```bash
sealc build
```

which builds the package containing the current directory.

The current build options include:

```text
--emit-c
-o <output>
--output <output>
-compiler <name>
--compiler <name>
```

Examples:

```bash
sealc build ./
sealc build ./ --emit-c
sealc build ./ -o my_program
sealc build ./ -compiler gcc
sealc build ./ --compiler=clang
```

Compiler options may appear before or after the package path:

```bash
sealc build -compiler gcc ./
sealc build ./ -compiler gcc
```

Unknown build options are rejected.

### 38.2 Emit-only mode

Emit-only mode generates package C files without invoking a C compiler:

```bash
sealc build ./ --emit-c
```

Conceptually:

```go
BuildWorkspace(path, BuildOptions{
    EmitOnly: true,
})
```

### 38.3 Temporary compiler override

The selected compiler can be overridden for one build:

```bash
sealc build ./ -compiler gcc
```

Conceptually:

```go
BuildWorkspace(path, BuildOptions{
    Compiler: "gcc",
})
```

The override changes the compiler profile selected for that invocation. It does not alter `seal.toml`.

When the selected compiler has a named profile, that profile is applied:

```toml
[compiler.gcc]
c_flags = [
    "-Wall",
    "-Wextra",
    "-Wpedantic",
]
```

When no named profile exists, the compiler still receives the common build settings and `[compiler.default]`.

### 38.4 Executable compilation

For an executable root package, the build command combines:

* generated C files for all dependency packages;
* native C files from all packages;
* compiler executable arguments;
* configured C standard;
* configured target arguments;
* include paths;
* preprocessor definitions;
* compilation flags;
* library search paths;
* libraries;
* linker flags;
* the requested output path.

The conceptual command order is:

```text
compiler
compiler_args
generated and native C files
standard and target settings
include directories
defines
c_flags
library directories
libraries
link_flags
-o output
```

The exact spelling may vary by compiler.

Library packages do not independently produce final executables.

### 38.5 Compiler command selection

Compiler selection follows this process:

```text
command-line override, when present
otherwise [build].compiler
otherwise cc
```

The selected configuration is then resolved through:

```text
[build]
    ↓
[compiler.default]
    ↓
[compiler.<selected compiler>]
```

An explicit compiler path takes precedence over the compiler preset executable.

Examples:

```toml
[compiler.gcc]
path = "/usr/bin/gcc"
```

```toml
[compiler.zigcc]
path = "C:/tools/zig/zig.exe"
args = ["cc"]
```

### 38.6 Package symbol names in C

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

## 38. Foreign ABI directives

Foreign ABI directives describe C types, constants, callback signatures, and task addresses that cannot be represented accurately by ordinary Seal declarations.

They are primarily intended for platform APIs such as threads, window systems, and operating-system callbacks.

### 38.1 `@foreign_type`

`@foreign_type` declares a nominal Seal type backed directly by a C type:

```seal
ThreadResult :: @foreign_type(
    SEAL_THREAD_RESULT
)
```

`ThreadResult` is a distinct type in Seal, while generated C uses `SEAL_THREAD_RESULT` directly.

The compiler does not emit a wrapper structure or typedef.

### 38.2 `@foreign_value`

`@foreign_value` declares a typed Seal value backed by a C expression:

```seal
thread_success :: @foreign_value(
    ThreadResult,
    SEAL_THREAD_SUCCESS
)
```

Use it like an ordinary value:

```seal
return thread_success
```

Generated C substitutes the configured expression directly.

### 38.3 `@foreign_task`

`@foreign_task` defines a reusable foreign task ABI:

```seal
ThreadEntryABI :: @foreign_task(
    declaration SEAL_THREAD_RESULT SEAL_THREAD_CALL {name}(void *{arg0_name}),
    address SEAL_THREAD_ADDRESS({name}),
)
```

The declaration template controls the generated C task declaration and definition.

The address template controls the expression produced by `@task_pointer`.

Supported placeholders include:

```text
{name}
{arg0_name}
{arg1_name}
...
```

Argument-name placeholders use the parameter names from the Seal task declaration.

### 38.4 `@foreign`

Apply a foreign ABI to a task with `@foreign`:

```seal
WorkerEntry :: @foreign(ThreadEntryABI) task(
    context rawptr,
) ThreadResult {
    return thread_success
}
```

The task remains a normal statically checked Seal task, but its generated C signature follows the selected foreign ABI template.

Foreign tasks may also be generic:

```seal
WorkerEntry :: @foreign(ThreadEntryABI) task<
    T type,
    Worker task[(T)],
>(
    context rawptr,
) ThreadResult {
    return thread_success
}
```

### 38.5 `@task_pointer`

`@task_pointer` returns the foreign address of a task as `rawptr`:

```seal
pointer := @task_pointer(WorkerEntry)
```

Generic tasks must be fully specialized:

```seal
pointer := @task_pointer(
    WorkerEntry<int, ProcessInt>
)
```

The referenced task must have a foreign ABI with an address template.

Seal does not currently provide general runtime task values or indirect task calls. `@task_pointer` exists specifically for passing statically selected task entry points to foreign APIs.

---

## 40. Cross-package generic specialization

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

## 41. Current limitations and evolving areas

The following points are important when using the language today.

### 41.1 Standard library is still being built

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

### 41.2 No array primitive

These old array forms remain removed:

```seal
[N]T
[]T
[1, 2, 3]
```

Seal provides `@inline_array<T, N>` only as a restricted compiler directive for fixed inline storage.

It is not a general-purpose array primitive and deliberately lacks features such as dynamic sizing, whole-array assignment, direct task parameter/result use, ownership, slicing, and allocation behavior.

Higher-level array and collection abstractions should wrap this storage or use allocator-backed representations as appropriate.

### 41.3 Text operations are scalar-based but not grapheme-aware

`string` and `cstring` currently provide built-in:

```seal
size(text)
len(text)
text[index]
```

Their meanings are:

```text
size  → UTF-8 byte count
len   → Unicode scalar count
[]    → Unicode scalar at an index
```

They do not perform grapheme-cluster segmentation or Unicode normalization.

Operations such as `len` and indexing decode UTF-8 linearly and may be relatively expensive for repeated random access.

Higher-level Unicode operations belong in the standard library.

### 41.4 Generic inference is not stable

Write:

```seal
Identity<int>(10)
```

rather than relying on:

```seal
Identity(10)
```

### 41.5 Generic unions are not yet complete

`Option<T>` and `Result<T, E>` remain planned foundational types.

### 41.6 Ownership remains explicit and low-level

Plain `string` and `cstring` are borrowed immutable views.

Seal has:

* typed pointers;
* `rawptr`;
* borrowed text views;
* C allocation access;
* `defer`.

It does not yet have a finalized general ownership, borrowing, destructor, or garbage-collection model.

The standard library still needs explicit owned text and mutable buffer abstractions for operations such as:

* runtime string construction;
* concatenation;
* null-terminated conversion;
* mutable UTF-8 editing;
* allocator-backed text storage.

### 41.7 Some `seal.toml` policy flags are only partially enforced

The configuration parser accepts several safety and style policies, but the compiler does not yet apply every policy consistently in every phase.

### 41.8 Overloaded index compound assignment is incomplete

This works:

```seal
values[index] = value
```

This is not yet guaranteed for an overloaded setter:

```seal
values[index] += 1
```

### 41.9 Test-task tooling is evolving

`test task` syntax exists, while discovery, execution, reporting, and package-level test commands still need a polished public workflow.

### 41.10 LSP and editor support are future work

The compiler retains semantic information that can support:

- diagnostics;
- hover;
- go to definition;
- completion;
- signature help.

A dedicated `seal-lsp` and Zed extension are logical next projects after the initial standard library.

---

## 42. Complete multi-package example

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

### 42.1 Workspace marker

`seal.workspace`:

```text
```

### 42.2 Library package

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

### 42.3 Executable package

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

[compiler.default]
standard = "c11"
c_flags = [
    "-Wall",
    "-Wextra",
]

[compiler.gcc]
c_flags = [
    "-Wall",
    "-Wextra",
    "-Wpedantic",
]

[compiler.clang]
c_flags = [
    "-Wall",
    "-Wextra",
    "-Wconversion",
]

[checker]
generic_constraint_max_depth = 32
```

The package normally builds using `cc`:

```bash
sealc build ./app
```

A compiler may be selected temporarily:

```bash
sealc build ./app -compiler gcc
```

or:

```bash
sealc build ./app --compiler clang
```

These commands select the corresponding compiler profile without changing `app/seal.toml`.

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

## 43. Syntax cheat sheet

### Constants

```seal
Pi :: 3.14159265
```

### Local variables and numeric literals

```seal
x := 10
x: int = 10
x: int

byte: u8 = 0xFF
scalar: u32 = 0x10_FFFF
large: u64 = 0xFFFF_FFFF_FFFF_FFFF
```

### Character literal

```seal
letter: char = 'ñ'
```

### Seal string

```seal
text: string = "mañana"
```

### C string

```seal
text: cstring = c"mañana"
```

### UTF-8 byte length

```seal
bytes := size(text)
```

### Unicode-scalar length

```seal
characters := len(text)
```

### Unicode-scalar indexing

```seal
first: char = text[0]
last: char = text[-1]
```

### Borrow text data as a raw pointer

```seal
data := cast<rawptr>(text)
```

### Construct a borrowed string view

```seal
view := cast<string>(data, byteLength)
```

### Construct a borrowed C-string view

```seal
view := cast<cstring>(data, byteLength)
```

### Borrowed cast example

```seal
source := "hello"
data := cast<rawptr>(source)
view := cast<string>(data, size(source))

assert(view == source)
```

### Borrowed C-string cast example

```seal
source := c"hello"
data := cast<rawptr>(source)
view := cast<cstring>(data, size(source))

assert(view == source)
```

### Text equality

```seal
assert("hello" == "hello")
assert(c"hello" == c"hello")
```

### C text parameter

```seal
puts :: extern("puts") task(text cstring) int
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

### Inline fixed-size storage

```seal
values := @inline_array<int, 4>(
    10,
    20,
    30,
    40,
)
```

### Nested inline storage

```seal
matrix := @inline_array<
    @inline_array<int, 3>,
    2
>(
    @inline_array<int, 3>(1, 2, 3),
    @inline_array<int, 3>(4, 5, 6),
)
```

### Inline-array indexing and length

```seal
value := matrix[1][2]
matrix[0][1] = 99

rows := len(matrix)
columns := len(matrix[0])
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

### Foreign C type

```seal
ThreadResult :: @foreign_type(
    SEAL_THREAD_RESULT
)
```

### Foreign C value

```seal
thread_success :: @foreign_value(
    ThreadResult,
    SEAL_THREAD_SUCCESS
)
```

### Foreign task ABI

```seal
ThreadEntryABI :: @foreign_task(
    declaration SEAL_THREAD_RESULT SEAL_THREAD_CALL {name}(void *{arg0_name}),
    address SEAL_THREAD_ADDRESS({name}),
)
```

### Task using a foreign ABI

```seal
WorkerEntry :: @foreign(ThreadEntryABI) task(
    context rawptr,
) ThreadResult {
    return thread_success
}
```

### Foreign task pointer

```seal
pointer := @task_pointer(WorkerEntry)
```

### Specialized foreign task pointer

```seal
pointer := @task_pointer(
    WorkerEntry<int, ProcessInt>
)
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

### Common build configuration

```toml
[build]
compiler = "gcc"
standard = "c11"
c_flags = [
    "-Wall",
    "-Wextra",
]
```

### Default compiler profile

```toml
[compiler.default]
standard = "c11"
c_flags = [
    "-Wall",
    "-Wextra",
]
```

### Named compiler profile

```toml
[compiler.gcc]
path = "gcc"
c_flags = [
    "-Wall",
    "-Wextra",
    "-Wpedantic",
]
```

### Zig CC profile

```toml
[compiler.zigcc]
path = "zig"
args = ["cc"]
target = "x86_64-windows-gnu"
```

### Build project

```bash
sealc build ./
```

### Emit generated C only

```bash
sealc build ./ --emit-c
```

### Choose output file

```bash
sealc build ./ -o program
```

### Temporarily select compiler

```bash
sealc build ./ -compiler gcc
```

```bash
sealc build ./ --compiler=clang
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
restricted compiler-directed inline storage with @inline_array<T, N>
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
higher-level collections built on suitable storage abstractions
string operations
memory and I/O packages
Option and Result
a test runner
a Tree-sitter grammar
seal-lsp
a Zed extension
```
