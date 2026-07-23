# vector

The `vector` package provides element-wise vector operations and reductions over `lists.Slice` values.

Its implementation delegates low-level arithmetic to the `simd` package while exposing a slice-based API with argument validation.

Supported element types are:

* `f32`
* `f64`
* `u32`

## Error handling

Vector operations return a `vector.Error`.

```seal
Error :: enum {
    None
    InvalidArgument
    LengthMismatch
}
```

Possible errors are:

* `None`: the operation completed successfully;
* `InvalidArgument`: a slice pointer or its data is invalid;
* `LengthMismatch`: the input and destination slices do not have equal lengths.

Check an error with `ErrorIsNone`:

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
    fmt.Println(
        "vector operation failed: %v",
        vector.ErrorString(
            error,
        ),
    )
}
```

`ErrorString` returns a human-readable description:

```seal
message :=
    vector.ErrorString(
        error,
    )
```

## Slice requirements

All operations use `lists.Slice` values.

A slice is valid when:

* the slice itself exists;
* an empty slice has any data pointer, including `nil`;
* a non-empty slice has a non-`nil` data pointer.

Operations with multiple slices require equal lengths.

The destination may be exactly the same slice as one of the sources:

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

For every valid index:

```text
destination[index] = left[index] - right[index]
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

For every valid index:

```text
destination[index] = left[index] * right[index]
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

For every valid index:

```text
destination[index] = left[index] / right[index]
```

### `ScaleF32`

Multiplies every source element by one scalar.

```seal
error :=
    vector.ScaleF32(
        destination,
        source,
        cast<f32>(2.0),
    )
```

For every valid index:

```text
destination[index] = source[index] * scalar
```

### `SumF32`

Returns the sum of all vector elements.

```seal
sum, error :=
    vector.SumF32(
        source,
    )
```

An empty vector returns zero and `Error.None`.

### `DotF32`

Returns the dot product of two equally sized vectors.

```seal
dot, error :=
    vector.DotF32(
        left,
        right,
    )
```

The result is equivalent to:

```text
left[0] * right[0] +
left[1] * right[1] +
...
```

An empty pair of vectors returns zero and `Error.None`.

## `f64` operations

The `f64` API has the same behavior as the `f32` API.

### `AddF64`

```seal
error :=
    vector.AddF64(
        destination,
        left,
        right,
    )
```

### `SubF64`

```seal
error :=
    vector.SubF64(
        destination,
        left,
        right,
    )
```

### `MulF64`

```seal
error :=
    vector.MulF64(
        destination,
        left,
        right,
    )
```

### `DivF64`

```seal
error :=
    vector.DivF64(
        destination,
        left,
        right,
    )
```

### `ScaleF64`

```seal
error :=
    vector.ScaleF64(
        destination,
        source,
        cast<f64>(2.0),
    )
```

### `SumF64`

```seal
sum, error :=
    vector.SumF64(
        source,
    )
```

### `DotF64`

```seal
dot, error :=
    vector.DotF64(
        left,
        right,
    )
```

## `u32` operations

Unsigned operations use normal wrapping arithmetic.

### `AddU32`

Adds two vectors element by element.

```seal
error :=
    vector.AddU32(
        destination,
        left,
        right,
    )
```

### `SubU32`

Subtracts the right vector from the left vector element by element.

```seal
error :=
    vector.SubU32(
        destination,
        left,
        right,
    )
```

Unsigned underflow wraps normally.

### `MulU32`

Multiplies two vectors element by element.

```seal
error :=
    vector.MulU32(
        destination,
        left,
        right,
    )
```

Unsigned overflow wraps normally.

### `BitAndU32`

Applies bitwise AND element by element.

```seal
error :=
    vector.BitAndU32(
        destination,
        left,
        right,
    )
```

For every valid index:

```text
destination[index] = left[index] & right[index]
```

### `BitOrU32`

Applies bitwise OR element by element.

```seal
error :=
    vector.BitOrU32(
        destination,
        left,
        right,
    )
```

For every valid index:

```text
destination[index] = left[index] | right[index]
```

### `BitXorU32`

Applies bitwise XOR element by element.

```seal
error :=
    vector.BitXorU32(
        destination,
        left,
        right,
    )
```

For every valid index:

```text
destination[index] = left[index] ^ right[index]
```

## Complete example

```seal
leftValues :=
    @inline_array<f32, 4>(
        cast<f32>(1.0),
        cast<f32>(2.0),
        cast<f32>(3.0),
        cast<f32>(4.0),
    )

rightValues :=
    @inline_array<f32, 4>(
        cast<f32>(5.0),
        cast<f32>(6.0),
        cast<f32>(7.0),
        cast<f32>(8.0),
    )

resultValues :=
    @inline_array<f32, 4>(
        cast<f32>(0.0),
        cast<f32>(0.0),
        cast<f32>(0.0),
        cast<f32>(0.0),
    )

left :=
    lists.SliceFromInlineArray<f32, 4>(
        &leftValues,
    )

right :=
    lists.SliceFromInlineArray<f32, 4>(
        &rightValues,
    )

result :=
    lists.SliceFromInlineArray<f32, 4>(
        &resultValues,
    )

error :=
    vector.AddF32(
        result,
        left,
        right,
    )

if !vector.ErrorIsNone(
    error,
) {
    fmt.Println(
        "vector addition failed: %v",
        vector.ErrorString(
            error,
        ),
    )

    return
}

sum, sumError :=
    vector.SumF32(
        result,
    )

if !vector.ErrorIsNone(
    sumError,
) {
    fmt.Println(
        "vector sum failed: %v",
        vector.ErrorString(
            sumError,
        ),
    )

    return
}

fmt.Println(
    "sum: %v",
    sum,
)
```

## Performance

The package forwards arithmetic operations to `simd`.

The concrete implementation may use platform-specific SIMD instructions when supported by the selected backend. Callers should depend on the behavior of the `vector` API rather than a particular instruction set.

The package does not allocate memory for operation results. Callers provide destination slices for element-wise operations.
