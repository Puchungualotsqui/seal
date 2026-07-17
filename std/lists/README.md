# lists

The `lists` package provides generic collections backed by the `mem` allocator interfaces.

It includes borrowed slices, fixed and dynamic arrays, stacks, queues, deques, hash maps, and hash sets.

## Memory and ownership

Most collections own allocator-backed storage and must be destroyed explicitly.

```seal
allocatorValue := mem.NewCAllocator()

allocator := cast<mem.Allocator>(
    &allocatorValue,
)

deallocator := cast<mem.Deallocator>(
    &allocatorValue,
)
```

`Array`, `DynamicArray`, `Deque`, `Stack`, `Queue`, `HashMap`, and `HashSet` perform shallow ownership copies. Avoid copying them after creation.

Elements are also copied shallowly. Destroying a collection does not recursively destroy owning elements.

## Indexing

Several collections support:

```seal
value := collection[index]
collection[index] = value
length := len(collection)
```

Negative indexes are interpreted relative to the end:

```seal
last := values[-1]
```

Checked access is available through:

* `Get`
* `TrySet`
* `SliceGet`, `ArrayGet`, `DynamicArrayGet`, `StaticArrayGet`, `DequeGet`

Unsafe access functions skip bounds checking and must only receive valid indexes.

## `Slice<T>`

A `Slice` is a borrowed mutable view over contiguous storage. It does not allocate or free memory.

```seal
view := lists.SliceRange<int>(
    &source,
    1,
    4,
)
```

Main operations:

* `SliceFromRaw`
* `SliceData`
* `SliceLength`
* `SliceAt`
* `SliceSet`
* `SliceRange`

A slice must not outlive its backing storage.

## `StaticArray<T, N>`

A `StaticArray` stores exactly `N` elements inline. It requires no allocator or destructor.

```seal
values :=
    lists.NewStaticArrayFilled<
        int,
        4
    >(10)
```

Main operations:

* `NewStaticArrayZeroed`
* `NewStaticArrayFilled`
* `StaticArrayLength`
* `StaticArrayAt`
* `StaticArraySet`
* `StaticArraySlice`

Copying a `StaticArray` copies its complete inline storage.

## `Array<T>`

An `Array` owns fixed-length allocator-backed storage.

```seal
values := lists.ArrayWithLength<int>(
    zeroAllocator,
    deallocator,
    8,
)

lists.DestroyArray<int>(
    &values,
)
```

Constructors include:

* `NewArrayEmpty`
* `ArrayWithLength`
* `ArrayFilled`
* `ArrayFromSlice`

Main operations:

* `ArrayLength`
* `ArrayAt`
* `ArraySet`
* `ArraySlice`
* `DestroyArray`

Its length cannot change after creation.

## `DynamicArray<T>`

A `DynamicArray` owns growable contiguous storage.

```seal
values :=
    lists.NewDynamicArrayEmpty<int>(
        allocator,
        deallocator,
    )

lists.Append<int>(
    &values,
    42,
)

lists.DestroyDynamicArray<int>(
    &values,
)
```

Construction:

* `NewDynamicArrayEmpty`
* `DynamicArrayWithCapacity`
* `DynamicArrayWithLength`
* `DynamicArrayFilled`
* `DynamicArrayFromSlice`
* static-capacity and static-length variants

Mutation:

* `Append`
* `AppendSlice`
* `Insert`
* `Remove`
* `Pop`
* `Clear`
* `Resize`
* `ReserveDynamicArray`
* `ShrinkDynamicArrayToFit`

Inspection:

* `DynamicArrayLength`
* `DynamicArrayCapacity`
* `DynamicArrayAt`
* `DynamicArraySlice`

## `Stack<T>`

A stack provides last-in, first-out storage.

```seal
stack := lists.NewStack<int>(
    allocator,
    deallocator,
)

lists.Push<int>(&stack, 10)

value, available :=
    lists.StackPop<int>(&stack)

lists.DestroyStack<int>(&stack)
```

Main operations:

* `Push`
* `StackPop`
* `Peek`
* `StackTryPeek`
* `StackLength`
* `StackIsEmpty`
* `DestroyStack`

`Peek` panics when the stack is empty. `StackTryPeek` returns `false`.

## `Queue<T>`

A queue provides first-in, first-out storage.

```seal
queue := lists.NewQueue<int>(
    allocator,
    deallocator,
)

lists.Enqueue<int>(&queue, 10)

value, available :=
    lists.Dequeue<int>(&queue)

lists.DestroyQueue<int>(&queue)
```

Main operations:

* `Enqueue`
* `Dequeue`
* `QueueFront`
* `QueueTryFront`
* `QueueLength`
* `QueueIsEmpty`
* `DestroyQueue`

## `Deque<T>`

A deque is a growable circular buffer supporting insertion and removal at both ends.

```seal
deque := lists.NewDeque<int>(
    allocator,
    deallocator,
)

lists.PushFront<int>(&deque, 10)
lists.PushBack<int>(&deque, 20)

front, available :=
    lists.PopFront<int>(&deque)

lists.DestroyDeque<int>(&deque)
```

Main operations:

* `PushFront`
* `PushBack`
* `PopFront`
* `PopBack`
* `Front`
* `Back`
* `DequeAt`
* `DequeSet`
* `ClearDeque`
* `DestroyDeque`

Checked front and back access is available through `DequeTryFront` and `DequeTryBack`.

## `HashMap<K, V, Hash, Equal>`

A hash map stores key-value pairs using open addressing and linear probing.

```seal
values :=
    lists.NewHashMapEmpty<
        string,
        int,
        lists.HashString,
        lists.EqualString
    >(
        allocator,
        deallocator,
    )

lists.HashMapPut<
    string,
    int,
    lists.HashString,
    lists.EqualString
>(
    &values,
    "answer",
    42,
)

answer, found :=
    lists.HashMapGet<
        string,
        int,
        lists.HashString,
        lists.EqualString
    >(
        &values,
        "answer",
    )

lists.DestroyHashMap<
    string,
    int,
    lists.HashString,
    lists.EqualString
>(&values)
```

Mutation:

* `HashMapPut`
* `HashMapInsert`
* `HashMapReplace`
* `HashMapRemove`
* `ClearHashMap`
* `ReserveHashMap`
* `ShrinkHashMapToFit`

Lookup:

* `HashMapGet`
* `HashMapAt`
* `HashMapGetUnsafe`
* `HashMapContainsKey`

Extraction:

* `HashMapKeys`
* `HashMapValues`
* `HashMapItems`

The hash and equality tasks must be pure and compatible:

```text
Equal(a, b) implies Hash(a) == Hash(b)
```

## `KeyValue<K, V>`

`KeyValue` represents one map entry returned by `HashMapItems`.

```seal
item := lists.KeyValue<string, int>{
    Key = "answer",
    Value = 42,
}
```

## `HashSet<T, Hash, Equal>`

A hash set stores unique values using a hash map internally.

```seal
values :=
    lists.NewHashSetEmpty<
        string,
        lists.HashString,
        lists.EqualString
    >(
        allocator,
        deallocator,
    )

inserted :=
    lists.HashSetAdd<
        string,
        lists.HashString,
        lists.EqualString
    >(
        &values,
        "seal",
    )

lists.DestroyHashSet<
    string,
    lists.HashString,
    lists.EqualString
>(&values)
```

Main operations:

* `HashSetAdd`
* `HashSetContains`
* `HashSetRemove`
* `HashSetValues`
* `ClearHashSet`
* `ReserveHashSet`
* `ShrinkHashSetToFit`
* `DestroyHashSet`

## Hash and equality helpers

Built-in helpers are provided for common key types:

* `HashString`, `EqualString`
* `HashCString`, `EqualCString`
* `HashBool`, `EqualBool`
* `HashInt`, `EqualInt`
* `HashUint`, `EqualUint`
* integer-width hash functions
* `HashChar`, `EqualChar`

```seal
hash := lists.HashString(
    "seal",
)
```

## Panicking and checked operations

Functions such as `At`, `Set`, `Front`, `Back`, `Peek`, `Insert`, and `Remove` panic when their preconditions are violated.

Use checked alternatives when failure is expected:

```seal
value, found :=
    lists.Get<int>(
        &values,
        index,
    )
```

Functions ending in `Unsafe` perform no safety validation and require the caller to guarantee valid access.
