# fmt

The `fmt` package writes formatted values to standard output.

It supports UTF-8 text and a small `%v`-based formatting syntax.

## `Print`

`Print` replaces each `%v` directive with the next argument.

```seal
fmt.Print(
    "Name: %v, age: %v",
    name,
    age,
)
```

Use `%%` to write a literal percent sign:

```seal
fmt.Print(
    "Progress: %v%%",
    progress,
)
```

## `Println`

`Println` behaves like `Print` and adds a newline.

```seal
fmt.Println(
    "Result: %v",
    result,
)
```

Calling it without arguments writes an empty line:

```seal
fmt.Println()
```

## Aliases

`Printf` is an alias of `Print`.

`Printfl` is an alias of `Println`.

## Supported values

`%v` supports these runtime types:

* `int`
* `uint`
* `f32`
* `f64`
* `bool`
* `char`
* `string`
* `cstring`
* `rawptr`

```seal
fmt.Println(
    "%v %v %v",
    42,
    true,
    "seal",
)
```

Unsupported `any` values cause a panic.

## `WriteStringable`

`WriteStringable` writes the borrowed text returned by a `strings.Stringable` implementation.

```seal
fmt.WriteStringable(
    cast<strings.Stringable>(
        &value,
    ),
)
```

No allocation is performed by `fmt`.

## Format errors

Formatting panics when:

* a `%` directive is incomplete;
* a directive other than `%v` or `%%` is used;
* a `%v` has no matching argument;
* extra arguments remain unused;
* an argument has an unsupported runtime type.

The format string is processed by Unicode-scalar index, so non-ASCII text is handled correctly.
