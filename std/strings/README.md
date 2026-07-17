# strings

The `strings` package provides UTF-8-aware string operations, owned string types, C-string conversion, searching, slicing, splitting, joining, replacement, and repetition.

Seal `string` values are borrowed views. Functions that create new storage return `OwnedString` or `OwnedCString`.

## Ownership

### `OwnedString`

Owns a UTF-8 byte allocation.

```seal
owner := strings.Clone(
    allocator,
    "hello",
)

value := strings.View(
    &owner,
)

strings.Destroy(
    deallocator,
    &owner,
)
```

### `OwnedCString`

Owns a null-terminated UTF-8 sequence.

```seal
owner := strings.ToCString(
    allocator,
    "hello",
)

value := strings.CView(
    &owner,
)

strings.DestroyC(
    deallocator,
    &owner,
)
```

Owned values must not be copied and destroyed independently.

## Copying and conversion

* `Clone` creates an owned copy of a `string`.
* `CloneC` creates an owned copy of a `cstring`.
* `BorrowString` creates a borrowed `string` view over a `cstring`.
* `FromCString` creates an owned Seal string.
* `TryToCString` converts safely and returns a success flag.
* `ToCString` panics when conversion fails.
* `HasNullByte` detects embedded null bytes.

```seal
converted, valid :=
    strings.TryToCString(
        allocator,
        value,
    )
```

## Concatenation and repetition

### `Concat`

```seal
result := strings.Concat(
    allocator,
    "hello ",
    "world",
)
```

### `Concat3`

Joins three values into one owned string.

### `Repeat`

```seal
result := strings.Repeat(
    allocator,
    "ab",
    3,
)
```

## Joining and splitting

### `Join`

Joins a borrowed slice of strings using a separator.

```seal
result := strings.Join(
    allocator,
    values,
    ", ",
)
```

### `Split`

Returns a `DynamicArray<string>` containing borrowed views into the original string.

```seal
parts := strings.Split(
    allocator,
    deallocator,
    "a,b,c",
    ",",
)

lists.DestroyDynamicArray<string>(
    &parts,
)
```

The returned fields must not outlive the source string.

An empty separator splits by Unicode scalar.

## Searching

* `IndexOf` returns the first Unicode-scalar index.
* `LastIndexOf` returns the final Unicode-scalar index.
* `Count` counts non-overlapping matches.
* `Contains` reports whether a match exists.

```seal
index, found := strings.IndexOf(
    "héllo",
    "é",
)
```

Search results use Unicode-scalar indexes, not byte offsets.

## Predicates

* `IsEmpty`
* `IsEmptyC`
* `StartsWith`
* `EndsWith`
* `Contains`

```seal
valid := strings.StartsWith(
    "seal compiler",
    "seal",
)
```

## Slicing

### `BorrowSlice`

Returns a borrowed Unicode-scalar range.

```seal
view := strings.BorrowSlice(
    "héllo",
    1,
    4,
)
```

### `CloneSlice`

Creates an owned copy of a Unicode-scalar range.

Negative indexes are interpreted relative to the end.

## Replacement

### `ReplaceAll`

Replaces every non-overlapping occurrence and returns an owned string.

```seal
result := strings.ReplaceAll(
    allocator,
    "a-b-c",
    "-",
    "/",
)
```

An empty needle returns an unchanged owned copy.

## UTF-8 indexing

### `ByteOffset`

Converts a Unicode-scalar index to a UTF-8 byte offset.

```seal
offset := strings.ByteOffset(
    "héllo",
    2,
)
```

Invalid UTF-8 or an invalid index causes a panic.

## `Stringable`

`Stringable` represents values that expose a borrowed textual representation.

```seal
Stringable :: impl Item {
    String :: pure task(
        value *Item,
    ) string {
        return value.Name
    }
}
```

The returned string must remain valid while the caller uses it.

## Safety

Borrowed views returned by `View`, `CView`, `BorrowString`, `BorrowSlice`, and `Split` must not outlive their backing storage.

Destroy every owned result exactly once using `Destroy` or `DestroyC`.
