# strconv

The `strconv` package converts booleans, integers, and floating-point values to and from strings.

Formatting functions return `strings.OwnedString` and therefore require an allocator and explicit destruction.

## Booleans

### `FormatBool`

```seal id="gsy90a"
text := strconv.FormatBool(
    allocator,
    true,
)
```

Produces `"true"` or `"false"`.

### `ParseBool`

```seal id="1u1xqf"
value, valid := strconv.ParseBool(
    "true",
)
```

Only lowercase `"true"` and `"false"` are accepted.

## Integers

Signed and unsigned integer formatting supports bases from 2 through 36.

```seal id="i2ygdq"
text := strconv.FormatInt(
    allocator,
    255,
    16,
)
```

This produces `"ff"`.

Available formatters include:

* `FormatInt`, `FormatUint`
* `FormatI8`, `FormatI16`, `FormatI32`, `FormatI64`
* `FormatU8`, `FormatU16`, `FormatU32`, `FormatU64`

Base 10 is used when the base argument is omitted.

### Parsing integers

```seal id="6v7x7u"
value, valid := strconv.ParseI32(
    "-7f",
    16,
)
```

Available parsers include:

* `ParseInt`, `ParseUint`
* `ParseI8`, `ParseI16`, `ParseI32`, `ParseI64`
* `ParseU8`, `ParseU16`, `ParseU32`, `ParseU64`

Parsing is strict:

* no whitespace;
* no `0x`, `0b`, or similar prefixes;
* no separators;
* no trailing characters;
* values outside the target type range are rejected.

A leading `+` is accepted. A leading `-` is accepted only for signed types.

## Floating-point values

### Formatting

```seal id="mtdwhq"
text := strconv.FormatF64(
    allocator,
    3.14159,
)
```

`FormatF32` and `FormatF64` produce round-trip-safe decimal text.

Special values are written as:

* `nan`
* `inf`
* `-inf`

Negative zero is preserved as `"-0"`.

### Parsing

```seal id="8q0jts"
value, valid := strconv.ParseF64(
    "-1.25e-4",
)
```

Accepted forms include:

```text id="s3mb7d"
12
-12
+12.5
.5
5.
1e3
-1.25E-4
nan
inf
-inf
```

Parsing rejects whitespace, hexadecimal floats, locale-specific decimal separators, trailing characters, overflow, and underflow.

Floating-point conversion uses the C numeric locale, so decimal parsing and formatting use `.` independently of the normal process locale.

## Ownership

Every formatting function returns an owned string:

```seal id="m5jjlw"
text := strconv.FormatUint(
    allocator,
    42,
)

value := strings.View(
    &text,
)

strings.Destroy(
    deallocator,
    &text,
)
```

Do not independently destroy shallow copies of the same `OwnedString`.

## Failure handling

Parsing functions return `(value, valid)`.

```seal id="qut0qz"
value, valid := strconv.ParseInt(
    input,
)

if !valid {
    // Do not use value.
}
```

When `valid` is `false`, the accompanying value is unspecified.
