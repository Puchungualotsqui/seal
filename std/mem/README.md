# mem

The `mem` package provides low-level memory allocation, deallocation, zero-initialization, arena allocation, and byte-memory operations.

It includes configurable allocator interfaces as well as direct wrappers around the C standard library.

## Allocator interfaces

### `Allocator`

Reserves an uninitialized block of memory.

```seal
ptr := Alloc(
    allocator,
    64,
)
```

Returns `nil` when allocation fails.

### `ZeroAllocator`

Reserves zero-initialized memory for a number of elements.

```seal
ptr := AllocZeroed(
    allocator,
    10,
    size(int),
)
```

### `Deallocator`

Releases an individual allocation.

```seal
Free(
    deallocator,
    ptr,
)
```

### `ResetAllocator`

Discards all allocations owned by a resettable allocator.

```seal
Reset(allocator)
```

## Allocators

### `CAllocator`

Uses the C standard library functions `malloc`, `calloc`, and `free`.

```seal
allocator := NewCAllocator()

ptr := Alloc(
    cast<Allocator>(&allocator),
    64,
)

Free(
    cast<Deallocator>(&allocator),
    ptr,
)
```

### `ZeroingAllocator`

Adds zero-initialized allocation support to an `Allocator` and compatible `Deallocator`.

```seal
allocator := NewZeroingAllocator(
    backingAllocator,
    backingDeallocator,
)

ptr := AllocZeroed(
    cast<ZeroAllocator>(&allocator),
    4,
    size(int),
)
```

### `ArenaAllocator`

Owns all allocations until it is reset. Individual allocations cannot be freed separately.

```seal
arena := NewArenaAllocator(
    backingAllocator,
    backingDeallocator,
)

value := AllocTypeWith<int>(
    cast<Allocator>(&arena),
)

ResetArenaAllocator(&arena)
```

All pointers returned by the arena become invalid after reset.

### `ArenaAllocation`

Internal linked-list metadata used by `ArenaAllocator` to track owned allocations.

Applications normally do not create this structure directly.

## Configurable allocation helpers

### `AllocWith`

Allocates an uninitialized byte block.

```seal
ptr := AllocWith(
    allocator,
    128,
)
```

### `AllocTypeWith`

Allocates uninitialized memory for one value of `T`.

```seal
value := AllocTypeWith<MyStruct>(
    allocator,
)
```

### `AllocZeroedWith`

Allocates zero-initialized memory for multiple elements.

```seal
values := AllocZeroedWith(
    zeroAllocator,
    16,
    size(int),
)
```

### `NewWith`

Allocates one zero-initialized value of `T`.

```seal
value := NewWith<MyStruct>(
    zeroAllocator,
)
```

### `FreeWith`

Releases an untyped allocation.

```seal
FreeWith(
    deallocator,
    ptr,
)
```

### `FreeTypeWith`

Releases a typed allocation.

```seal
FreeTypeWith<MyStruct>(
    deallocator,
    value,
)
```

### `ResetWith`

Resets an allocator through the `ResetAllocator` interface.

```seal
ResetWith(
    resetAllocator,
)
```

## Direct C allocation helpers

These functions use the C standard library directly.

### `AllocC`

```seal
ptr := AllocC(64)
```

### `CallocC`

```seal
ptr := CallocC(
    10,
    size(int),
)
```

### `AllocTypeC`

```seal
value := AllocTypeC<MyStruct>()
```

### `NewC`

Allocates one zero-initialized value.

```seal
value := NewC<MyStruct>()
```

### `FreeC`

```seal
FreeC(ptr)
```

### `FreeTypeC`

```seal
FreeTypeC<MyStruct>(value)
```

## Byte-memory operations

### `MemcpyC`

Copies non-overlapping byte ranges.

```seal
MemcpyC(
    destination,
    source,
    byteCount,
)
```

### `MemmoveC`

Copies byte ranges that may overlap.

```seal
MemmoveC(
    destination,
    source,
    byteCount,
)
```

### `MemcmpC`

Compares two byte ranges.

```seal
comparison := MemcmpC(
    left,
    right,
    byteCount,
)
```

The result is negative, zero, or positive.

### `MemsetC`

Fills a byte range with one byte value.

```seal
MemsetC(
    destination,
    0,
    byteCount,
)
```

## Safety

This package exposes raw pointers and manual memory management.

The caller must ensure that:

* allocation and deallocation use compatible allocator state;
* freed or reset memory is not accessed again;
* memory-copy ranges are valid;
* `MemcpyC` ranges do not overlap;
* allocation-size calculations do not exceed the available address space.
