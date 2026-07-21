# atomic

The `atomic` package provides thread-safe integer, unsigned integer, and boolean values.

It supports:

* Atomic loads and stores
* Atomic exchange
* Compare-and-exchange
* Integer arithmetic
* Unsigned bitwise operations

Atomic values own native resources and must be destroyed when they are no longer needed.

## Error handling

Operations that may fail return an `Error`.

```seal
Error :: enum {
    None
    AllocationFailed
    InvalidState
    DestroyFailed
    NativeFailure
}
```

* `ErrorIsNone(error Error) bool` reports whether an operation succeeded.
* Creation tasks beginning with `TryNew` return allocation errors.
* `Store` and destruction tasks return errors.
* Load, exchange, and fetch operations panic when given an invalid atomic value.

## AtomicInt

`AtomicInt` stores a signed `int`.

```seal
AtomicInt :: struct {
    Handle rawptr
}
```

### Creation

* `TryNewInt(initialValue int) (AtomicInt, Error)` creates a signed atomic value.
* `NewInt(initialValue int) AtomicInt` creates one or panics.
* `IntIsValid(value *AtomicInt) bool` reports whether the value owns a native handle.

### Operations

* `IntLoad(value *AtomicInt) int` returns the current value.
* `IntStore(value *AtomicInt, next int) Error` replaces the current value.
* `IntExchange(value *AtomicInt, next int) int` replaces the value and returns the previous value.
* `IntCompareExchange(value *AtomicInt, expected int, desired int) bool` stores `desired` only when the current value equals `expected`.
* `IntFetchAdd(value *AtomicInt, amount int) int` adds `amount` and returns the previous value.
* `IntFetchSub(value *AtomicInt, amount int) int` subtracts `amount` and returns the previous value.
* `DestroyInt(value *AtomicInt) Error` destroys the value and clears its handle.

Fetch operations return the value from before the modification.

## AtomicUint

`AtomicUint` stores an unsigned `uint`.

```seal
AtomicUint :: struct {
    Handle rawptr
}
```

### Creation

* `TryNewUint(initialValue uint) (AtomicUint, Error)` creates an unsigned atomic value.
* `NewUint(initialValue uint) AtomicUint` creates one or panics.
* `UintIsValid(value *AtomicUint) bool` reports whether the value is valid.

### Basic operations

* `UintLoad(value *AtomicUint) uint`
* `UintStore(value *AtomicUint, next uint) Error`
* `UintExchange(value *AtomicUint, next uint) uint`
* `UintCompareExchange(value *AtomicUint, expected uint, desired uint) bool`
* `UintFetchAdd(value *AtomicUint, amount uint) uint`
* `UintFetchSub(value *AtomicUint, amount uint) uint`

### Bitwise operations

* `UintFetchAnd(value *AtomicUint, mask uint) uint`
* `UintFetchOr(value *AtomicUint, mask uint) uint`
* `UintFetchXor(value *AtomicUint, mask uint) uint`

Each bitwise fetch operation applies the operation and returns the previous value.

`DestroyUint(value *AtomicUint) Error` destroys the atomic and clears its handle.

Unsigned arithmetic follows normal `uint` wrapping behavior.

## AtomicBool

`AtomicBool` stores a boolean value.

```seal
AtomicBool :: struct {
    Handle rawptr
}
```

Internally, `false` is represented as zero and `true` as one.

### Creation

* `TryNewBool(initialValue bool) (AtomicBool, Error)` creates a boolean atomic.
* `NewBool(initialValue bool) AtomicBool` creates one or panics.
* `BoolIsValid(value *AtomicBool) bool` reports whether the value is valid.

### Operations

* `BoolLoad(value *AtomicBool) bool` returns the current value.
* `BoolStore(value *AtomicBool, next bool) Error` replaces the value.
* `BoolExchange(value *AtomicBool, next bool) bool` replaces the value and returns the previous value.
* `BoolCompareExchange(value *AtomicBool, expected bool, desired bool) bool` conditionally replaces the value.
* `DestroyBool(value *AtomicBool) Error` destroys the value and clears its handle.

A compare-and-exchange result of `false` can mean either that the current value did not match `expected` or that the atomic was invalid. Use `BoolIsValid` when that distinction matters.

## Compare-and-exchange

Compare-and-exchange performs this operation atomically:

```text
if current == expected:
    current = desired
    return true

return false
```

It is useful for state transitions, flags, and lock-free coordination.

The `expected` argument is passed by value and is not modified when the comparison fails.

## Memory ordering

Atomic operations use sequentially consistent ordering.

This means all atomic operations behave as though they participate in one global order observed consistently by all threads.

The underlying implementation may use compiler atomics or a mutex fallback depending on the selected C compiler.

## Example

This example increments one shared counter from multiple threads.

```seal
IncrementArguments :: struct {
    Counter *atomic.AtomicInt
    Count int
}

Increment :: task(
    arguments IncrementArguments,
) {
    index := 0

    for index < arguments.Count {
        atomic.IntFetchAdd(
            arguments.Counter,
            1,
        )

        index += 1
    }
}

Main :: task() {
    counter :=
        atomic.NewInt(0)

    arguments :=
        IncrementArguments{
            Counter = &counter,
            Count = 10000,
        }

    first :=
        thread.Start<
            IncrementArguments,
            Increment
        >(arguments)

    second :=
        thread.Start<
            IncrementArguments,
            Increment
        >(arguments)

    thread.JoinOrPanic(
        &first,
    )

    thread.JoinOrPanic(
        &second,
    )

    result :=
        atomic.IntLoad(
            &counter,
        )

    fmt.Println(
        "counter: %v",
        result,
    )

    if !atomic.ErrorIsNone(
        atomic.DestroyInt(
            &counter,
        ),
    ) {
        panic(
            "unable to destroy atomic counter",
        )
    }
}
```

## Lifetime rules

* Do not copy an initialized atomic value.
* Pass atomic values by pointer.
* Do not destroy an atomic while another thread may access it.
* Do not use an atomic after destruction.
* Successful destruction clears the handle.
* Loads, exchanges, and fetch operations panic on invalid values.
* Store and destruction operations report invalid state through `Error`.
* Atomics make individual operations safe; they do not automatically make multi-step algorithms atomic.
