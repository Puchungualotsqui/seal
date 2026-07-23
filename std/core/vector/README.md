# vector

The `vector` package provides element-wise vector operations over borrowed `lists.Slice` values.

It currently supports:

* `f32`
* `f64`
* `u32`

The implementation delegates low-level arithmetic to the `simd` package while exposing a stable public API based on `lists.Slice`.

## Errors

Vector operations return `vector.Error`.

```seal
Error :: enum {
    None
    InvalidArgument
    LengthMismatch
}
```

Use `ErrorIsNone` to test whether an operation succeeded:

```seal
error :=
    vector.AddF32(
        destination,
        left,
        right,
    )

if !vector.ErrorIsNone(
    error,
) {
    panic(
        vector.ErrorString(
            error,
        ),
    )
}
```

`ErrorString` returns a human-readable description:

* `None`: no error
* `InvalidArgument`: one of the supplied slices is invalid
* `LengthMismatch`: the supplied slices do not have compatible lengths

## Slice requirements

All operations accept borrowed `lists.Slice` values.

A slice is valid when:

* its slice pointer is not `nil`;
* and either its length is zero or its data pointer is not `nil`.

Operations that write into a destination require the destination and source lengths to match.

Binary operations require all three slices to have the same length:

```seal
vector.AddF32(
    destination,
    left,
    right,
)
```

Reduction operations such as `DotF32` require both input slices to have the same length.

## Aliasing

The destination may be exactly the same slice as one of the sources.

This allows in-place operations:

```seal
error :=
    vector.AddF32(
        values,
        values,
        other,
    )
```

Partially overlapping slices are not supported.

## `f32` operations

### `AddF32`

Adds two vectors element by element.

```seal
error :=
    vector.AddF32(
        destination,
        left,
        right,
    )
```

For every valid index:

```text
destination[index] = left[index] + right[index]
```

### `SubF32`

Subtracts the right vector from the left vector.

```seal
error :=
    vector.SubF32(
        destination,
        left,
        right,
    )
```

### `MulF32`

Multiplies two vectors element by element.

```seal
error :=
    vector.MulF32(
        destination,
        left,
        right,
    )
```

### `DivF32`

Divides the left vector by the right vector element by element.

```seal
error :=
    vector.DivF32(
        destination,
        left,
        right,
    )
```

### `ScaleF32`

Multiplies every source element by one scalar.

```seal
error :=
    vector.ScaleF32(
        destination,
        source,
        cast<f32>(2),
    )
```

### `SumF32`

Returns the sum of all elements.

```seal
sum, error :=
    vector.SumF32(
        values,
    )
```

### `DotF32`

Returns the dot product of two vectors.

```seal
dot, error :=
    vector.DotF32(
        left,
        right,
    )
```

The vectors must have the same length.

## `f64` operations

The `f64` API mirrors the `f32` API:

```seal
vector.AddF64(
    destination,
    left,
    right,
)

vector.SubF64(
    destination,
    left,
    right,
)

vector.MulF64(
    destination,
    left,
    right,
)

vector.DivF64(
    destination,
    left,
    right,
)

vector.ScaleF64(
    destination,
    source,
    scalar,
)

sum, error :=
    vector.SumF64(
        source,
    )

dot, error :=
    vector.DotF64(
        left,
        right,
    )
```

## `u32` operations

Unsigned arithmetic uses normal wrapping behavior.

### Arithmetic

```seal
vector.AddU32(
    destination,
    left,
    right,
)

vector.SubU32(
    destination,
    left,
    right,
)

vector.MulU32(
    destination,
    left,
    right,
)
```

### Bitwise operations

```seal
vector.BitAndU32(
    destination,
    left,
    right,
)

vector.BitOrU32(
    destination,
    left,
    right,
)

vector.BitXorU32(
    destination,
    left,
    right,
)
```

Each operation applies the corresponding operator element by element.

## Complete example

```seal
allocatorValue :=
    mem.NewCAllocator()

zeroAllocator :=
    cast<mem.ZeroAllocator>(
        &allocatorValue,
    )

deallocator :=
    cast<mem.Deallocator>(
        &allocatorValue,
    )

left :=
    lists.ArrayWithLength<f32>(
        zeroAllocator,
        deallocator,
        3,
    )

right :=
    lists.ArrayWithLength<f32>(
        zeroAllocator,
        deallocator,
        3,
    )

result :=
    lists.ArrayWithLength<f32>(
        zeroAllocator,
        deallocator,
        3,
    )

defer lists.DestroyArray<f32>(
    &result,
)

defer lists.DestroyArray<f32>(
    &right,
)

defer lists.DestroyArray<f32>(
    &left,
)

left[0] = cast<f32>(1)
left[1] = cast<f32>(2)
left[2] = cast<f32>(3)

right[0] = cast<f32>(4)
right[1] = cast<f32>(5)
right[2] = cast<f32>(6)

leftValues :=
    lists.ArraySlice<f32>(
        &left,
    )

rightValues :=
    lists.ArraySlice<f32>(
        &right,
    )

resultValues :=
    lists.ArraySlice<f32>(
        &result,
    )

error :=
    vector.AddF32(
        resultValues,
        leftValues,
        rightValues,
    )

if !vector.ErrorIsNone(
    error,
) {
    panic(
        vector.ErrorString(
            error,
        ),
    )
}

dot, dotError :=
    vector.DotF32(
        leftValues,
        rightValues,
    )

if !vector.ErrorIsNone(
    dotError,
) {
    panic(
        vector.ErrorString(
            dotError,
        ),
    )
}

fmt.Println(
    "sum: %v %v %v",
    result[0],
    result[1],
    result[2],
)

fmt.Println(
    "dot: %v",
    dot,
)
```

This produces an element-wise sum of:

```text
5 7 9
```

and a dot product of:

```text
32
```

## Allocation

The `vector` package does not allocate memory.

Callers own all input and destination arrays or slices and remain responsible for their lifetime.

Operations borrow the supplied slices only for the duration of the call.
