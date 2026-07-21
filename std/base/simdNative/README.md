# simdNative

`simdNative` is the private C bridge used by the public `simd` package.

It provides unaligned array kernels for:

- `f32` arithmetic, scaling, sum, and dot product
- `f64` arithmetic, scaling, sum, and dot product
- `u32` arithmetic and bitwise operations

The package uses SIMDe when compiled by GCC, Clang, or another compatible
compiler. TinyCC and builds defining `SEAL_SIMD_FORCE_SCALAR` use the
explicit scalar implementation.

Applications should normally import `simd`, not `simdNative`.

## Vendored dependency

Place SIMDe under:

```text
simdNative/vendor/simde/
├── COPYING
├── README.md
└── simde/
    ├── x86/
    └── ...
```

`native.c` includes:

```c
#include "vendor/simde/simde/x86/avx2.h"
```

Vendor a fixed upstream release and keep its `COPYING` file.

## Backend result

`Backend()` returns:

```text
0  explicit scalar implementation
1  SIMDe implementation
```

A SIMDe build may use native instructions, compiler vector extensions, or
SIMDe's portable implementation according to the compiler and target flags.
