# Seal Programming Language

## Language Specification (Version 1.0 Design)

---

# Philosophy

Seal is a compiled, statically typed systems programming language designed for:

* High performance
* Simple syntax
* Strong static checking
* Zero-cost abstractions
* Excellent tooling
* Predictable compilation
* Cross-platform native binaries

Seal attempts to provide the productivity of modern languages while remaining as close as possible to C-level performance.

The language intentionally avoids hidden allocations, runtime reflection, implicit conversions, exceptions, and garbage collection.

Everything should be understandable simply by reading the source code.

---

# Design Goals

Seal is designed around the following principles:

* Explicit is better than implicit.
* Simplicity over cleverness.
* Fast compilation.
* Excellent diagnostics.
* Safe by default.
* Zero-cost abstractions.
* Generic programming through specialization.
* No hidden runtime.
* Interoperability with C.

---

# Project Layout

```
Game/
│
├── seal.toml
├── src/
│   └── main.seal
│
├── assets/
│
├── libs/
│
└── build/
```

Libraries have the same structure.

---

# seal.toml

Every package contains a `seal.toml`.

Example executable:

```toml
name = "game"
kind = "executable"

dependencies = [
    "fmt",
    "math",
    "os"
]

optimization = "debug"

target = "native"

bounds_check = true
overflow_check = false
null_check = true

panic = "abort"

debug_symbols = true

warnings_as_errors = false

lto = true
```

---

Example library:

```toml
name = "math"
kind = "library"

dependencies = []
```

---

Example shared library:

```toml
name = "physics"

kind = "shared"

dependencies = [
    "math"
]
```

---

# Package Kinds

Supported package kinds:

* executable
* library
* shared
* static

---

# Compiler Settings

Supported settings include:

```toml
optimization = "debug"
optimization = "0"
optimization = "1"
optimization = "2"

target = "native"
target = "windows"
target = "linux"
target = "macos"

bounds_check = true
overflow_check = true
null_check = true

panic = "abort"
panic = "unwind"

debug_symbols = true

warnings_as_errors = false

lto = true
```

---

# Dependencies

```
dependencies = [
    "fmt",
    "math",
    "os",
    "net"
]
```

Dependencies are resolved by package name. Cannot be there duplicated names in a project.

---

# Source Files

```
src/
    main.seal
    player.seal
    world.seal
```

No header files exist.

---

# Comments

```seal
// Single line

/*
Multi-line
comment
*/
```

---

# Variables

Immutable:

```seal
x := 10
```

Mutable:

```seal
count: int = 0
```

Type inference:

```seal
name := "Seal"
```

Explicit type:

```seal
age: int = 20
```

---

# Constants

```seal
Pi :: 3.14159265

MaxPlayers :: 16
```

---

# Primitive Types

Signed integers

```
i8
i16
i32
i64
int
```

Unsigned integers

```
u8
u16
u32
u64
uint
```

Floating point

```
f32
f64
```

Boolean

```
bool
```

Characters

```
char
```

Strings

```
string
```

Raw pointers

```
rawptr
```

Void

```
void
```

---

# Arrays

```seal
numbers: [5]int
```

Initialization

```seal
numbers := [5]int{
    10,
    20,
    30,
    40,
    50,
}
```

Indexing

```seal
x := numbers[2]

numbers[3] = 100
```

---

# Slices

```seal
values: []int
```

---

# Structs

```seal
Player :: struct {
    name string
    hp int
}
```

Construction

```seal
player := Player{
    name = "Alice",
    hp = 100,
}
```

---

# Enums

```seal
Color :: enum {
    Red
    Green
    Blue
}
```

---

# Unions

```seal
Value :: union {
    int
    f64
    string
}
```

---

# Distinct Types

```seal
Id :: distinct int

PlayerId :: distinct int
```

---

# Type Aliases

```seal
Meters :: alias f32
```

---

# Pointers

```seal
ptr: *Player
```

Address

```seal
p := &player
```

Dereference

```seal
value := ptr.*
```

---

# Tasks

Seal uses **tasks** instead of functions.

```seal
Add :: task(a int, b int) int {
    return a + b
}
```

Pure task

```seal
Length :: pure task(v Vec2) f32
```

---

# Entry Point

```seal
Main :: task() {
}
```

---

# Return Values

```seal
return value
```

Multiple returns

```seal
Split :: task() (int, int)
```

---

# Control Flow

If

```seal
if value > 10 {

}
else {

}
```

While

```seal
while running {

}
```

For

```seal
for i := 0; i < 10; i += 1 {

}
```

For-each

```seal
for value in array {

}
```

---

# Switch

```seal
switch value {
case 0:
case 1:
default:
}
```

---

# Defer

```seal
defer Close(file)
```

---

# Struct Methods

Implemented as normal tasks.

```seal
Length :: task(v *Vec2) f32
```

---

# Interfaces

```seal
Drawable :: interface {
    Draw :: task(*Self)
}
```

---

# Generic Types

```seal
Box :: struct <T type> {
    value T
}
```

Instantiation

```seal
box: Box<int>
```

Nested

```seal
Box<Pair<int, string>>
```

---

# Generic Tasks

```seal
Swap :: task <T type>(a *T, b *T)
```

---

# Value Generics

```seal
Buffer :: struct <T type, N int> {
    data [N]T
}
```

Instantiation

```seal
Buffer<int, 64>
```

---

# Type Constraints

```seal
T comparable
```

Future constraints may include:

```
number
integer
floating
signed
unsigned
```

---

# Operator Overloading

```seal
+ :: overload {
    VecAdd
}
```

Future overloadable operators include:

```
+
-
*
/
%
==
!=
<
<=
>
>=
&
|
^
<<
>>
[]
[]=
```

Example

```seal
VecAdd :: task(a Vec2, b Vec2) Vec2
```

---

# Built-in Intrinsics

Compiler-provided intrinsics include:

```
len
cap
sizeof
alignof
typeof
panic
assert
cast
bitcast
```

---

# Compile-Time Evaluation

```seal
const_value := sizeof(Player)
```

---

# Memory

Seal has no garbage collector.

Memory is managed through:

* stack allocation
* explicit allocators
* arenas
* pools

---

# Error Handling

No exceptions.

Preferred approaches:

```
Option<T>

Result<T, E>
```

---

# Imports

```seal
import "fmt"
import "math"
```

---

# Visibility

Public

```seal
Player :: struct
```

Private

```seal
playerHelper :: task
```

(Module visibility rules apply.)

---

# Standard Library

Core packages:

```
fmt
math
os
io
fs
net
time
random
mem
sync
json
strings
unicode
```

---

# Testing

```
test "Vector Length" {

}
```

---

# Documentation Comments

```seal
/// Returns vector length.
Length :: pure task(v Vec2) f32
```

---

# Attributes

Examples

```seal
@inline
@noinline
@deprecated
@cold
@hot
@test
```

---

# C Interoperability

```seal
foreign "C"

printf :: foreign task(...)
```

---

# Build Commands

```
seal build

seal run

seal test

seal fmt

seal check

seal clean
```

---

# Compilation Pipeline

```
Lexer

↓

Parser

↓

AST

↓

Resolver

↓

Type Checker

↓

Generic Specialization

↓

Optimization

↓

C Code Generation

↓

Native Compiler
```

---

# Optimization Goals

The compiler performs:

* constant folding
* dead code elimination
* inlining
* escape analysis
* specialization
* unreachable code removal
* loop simplification

without changing observable program behavior.

---

# Runtime Philosophy

The runtime should remain extremely small.

There is no:

* garbage collector
* virtual machine
* bytecode interpreter
* hidden scheduler

The language should compile directly to efficient native code through generated C (or a future native backend), while preserving predictable performance and debuggability.

---

# Long-Term Goals

Seal aims to provide:

* A modern replacement for C in systems programming.
* Simple syntax with strong static guarantees.
* Generic programming through compile-time specialization.
* Zero-cost abstractions.
* High-quality diagnostics and tooling.
* Fast incremental compilation.
* A comprehensive standard library.
* Seamless interoperability with existing C ecosystems.
* A language that remains understandable and maintainable even for very large codebases.
