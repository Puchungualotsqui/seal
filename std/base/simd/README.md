# simd

The `simd` package provides checked array-oriented SIMD operations without
exposing machine vector types.

It currently supports:

- `f32` and `f64` add, subtract, multiply, divide, and scale
- `f32` and `f64` sum and dot product
- `u32` add, subtract, multiply, AND, OR, and XOR
- automatic SIMDe or scalar backend selection

Every operation receives pointers to contiguous values and an element count.
No special alignment is required.

A zero-length operation accepts `nil` pointers. For nonzero lengths, every
required pointer must be non-`nil`.

The destination may be exactly the same pointer as either input. Partially
overlapping ranges are not supported.

## Backend

`CurrentBackend()` returns either:

- `Scalar`
- `SIMDe`

TinyCC uses the scalar backend. Other supported C compilers use SIMDe unless
`SEAL_SIMD_FORCE_SCALAR` is defined.

## Floating reductions

`SumF32`, `DotF32`, `SumF64`, and `DotF64` may produce small rounding
differences compared with a strictly sequential scalar loop because values are
accumulated in a different order.
