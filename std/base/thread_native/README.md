# threadNative

The `threadNative` package provides Seal bindings to the platform’s native threading and synchronization APIs.

It implements the low-level operations used by:

* `thread`
* `sync`
* `atomic`
* Higher-level concurrent containers

Applications should normally use those packages instead of calling `threadNative` directly.

The native implementation supports Windows and POSIX-compatible platforms.

## Return conventions

Many native operations return `bool`:

* `true` means the operation succeeded.
* `false` means the handle was invalid or the native operation failed.

Non-blocking and timed operations commonly return an `int`:

```text
1   operation completed
0   unavailable or timed out
-1  native failure
```

Native handles are represented as `rawptr`. A handle returned by a creation task must eventually be passed to its matching destruction or ownership-consuming task.

## Native thread entry ABI

`ThreadResult` is the physical result type returned by native thread entry tasks.

```seal
ThreadResult :: @foreign_type(
    uintptr_t
)
```

`thread_success` is the normal successful thread-entry result.

`ThreadEntryABI` describes the callback signature expected by the native thread bridge:

```c
uintptr_t entry(void *context)
```

Task addresses using this ABI are intended only for `Start`.

## Threads

### Starting and ownership

* `Start(entry rawptr, context rawptr) rawptr` starts a native thread and returns its handle.
* A `nil` result means creation failed.
* The entry callback receives `context`.
* The returned handle must be joined or detached exactly once.

### Completion

* `Join(handle rawptr) bool` waits indefinitely, releases the native handle, and consumes ownership.
* `JoinFor(handle rawptr, milliseconds uint) int` performs a timed join.
* `Detach(handle rawptr) bool` releases the handle without waiting for the thread.

A timeout from `JoinFor` does not consume the handle. A successful join does.

### Context allocation

* `AllocateContext(byteSize uint) rawptr` allocates zero-initialized native memory.
* `FreeContext(context rawptr)` releases memory allocated by `AllocateContext`.

A zero-byte allocation still returns storage suitable for later release.

### Utilities

* `Yield()` yields execution to another runnable thread.
* `Sleep(milliseconds uint)` pauses the current thread.
* `HardwareThreadCount() uint` reports the number of online hardware threads, with a minimum result of one.
* `CurrentThreadID() uint` returns a platform-derived identifier for the current thread.

Thread identifiers are intended for diagnostics. Their exact representation is platform-dependent.

## Mutexes

* `CreateMutex() rawptr` creates a mutex.
* `DestroyMutex(handle rawptr) bool` destroys it.
* `LockMutex(handle rawptr) bool` waits for exclusive ownership.
* `TryLockMutex(handle rawptr) int` attempts to lock without waiting.
* `UnlockMutex(handle rawptr) bool` releases ownership.

`TryLockMutex` returns:

```text
1   acquired
0   currently unavailable
-1  native failure
```

A mutex must not be destroyed while locked or while another thread may still use it.

## Condition variables

* `CreateCondition() rawptr` creates a condition variable.
* `DestroyCondition(handle rawptr) bool` destroys it.
* `WaitCondition(condition rawptr, mutex rawptr) bool` waits indefinitely.
* `WaitConditionFor(condition rawptr, mutex rawptr, milliseconds uint) int` performs a timed wait.
* `SignalCondition(condition rawptr) bool` wakes one waiter.
* `BroadcastCondition(condition rawptr) bool` wakes all waiters.

Waiting releases the supplied mutex and reacquires it before returning.

`WaitConditionFor` returns:

```text
1   signaled
0   timeout
-1  native failure
```

Condition predicates must still be checked in a loop after waking.

## One-time initialization

* `CreateOnce() rawptr` creates one-time initialization state.
* `DestroyOnce(handle rawptr) bool` destroys it.
* `BeginOnce(handle rawptr) int` determines whether the caller should initialize.
* `CompleteOnce(handle rawptr) bool` marks initialization as complete and wakes waiting callers.

`BeginOnce` returns:

```text
1   this caller must initialize
0   initialization has already completed
-1  native failure
```

When `BeginOnce` returns `1`, the caller must complete initialization with `CompleteOnce`. Abandoning the operation leaves other callers waiting.

The object cannot be destroyed while initialization is in progress.

## Semaphores

* `CreateSemaphore(initialCount uint) rawptr` creates a counting semaphore.
* `DestroySemaphore(handle rawptr) bool` destroys it.
* `AcquireSemaphore(handle rawptr) bool` waits for and consumes one permit.
* `TryAcquireSemaphore(handle rawptr) int` attempts to consume a permit immediately.
* `AcquireSemaphoreFor(handle rawptr, milliseconds uint) int` performs a timed acquire.
* `ReleaseSemaphore(handle rawptr, count uint) bool` adds permits.

The try and timed operations return:

```text
1   acquired
0   unavailable or timed out
-1  native failure
```

`ReleaseSemaphore` rejects a count of zero and fails if the native permit count cannot represent the requested result.

## Read-write mutexes

A read-write mutex allows concurrent readers or one exclusive writer.

Waiting writers are preferred over new readers.

### Creation

* `CreateRWMutex() rawptr`
* `DestroyRWMutex(handle rawptr) bool`

Destruction fails while readers, writers, or waiting writers are present.

### Read locking

* `ReadLockRWMutex(handle rawptr) bool`
* `TryReadLockRWMutex(handle rawptr) int`
* `ReadLockRWMutexFor(handle rawptr, milliseconds uint) int`
* `ReadUnlockRWMutex(handle rawptr) bool`

### Write locking

* `WriteLockRWMutex(handle rawptr) bool`
* `TryWriteLockRWMutex(handle rawptr) int`
* `WriteLockRWMutexFor(handle rawptr, milliseconds uint) int`
* `WriteUnlockRWMutex(handle rawptr) bool`

Try and timed lock operations use the standard integer convention:

```text
1   acquired
0   unavailable or timed out
-1  native failure
```

Unlocking without a matching active lock fails.

## Atomics

The native atomic representation stores `uintptr_t` bits.

GCC and Clang use sequentially consistent compiler atomics. TCC and other supported compilers use an internal mutex fallback with equivalent serialized behavior.

### Lifetime

* `CreateAtomic(initialValue uint) rawptr` creates an atomic value.
* `DestroyAtomic(handle rawptr) bool` destroys it.

The value must not be destroyed while another thread may still access it.

### Operations

* `LoadAtomic(handle rawptr) uint` reads the current value.
* `StoreAtomic(handle rawptr, value uint) bool` replaces the value.
* `ExchangeAtomic(handle rawptr, value uint) uint` replaces the value and returns the previous value.
* `CompareExchangeAtomic(handle rawptr, expected uint, desired uint) bool` stores `desired` only when the current value equals `expected`.
* `FetchAddAtomic(handle rawptr, amount uint) uint` adds and returns the previous value.
* `FetchSubAtomic(handle rawptr, amount uint) uint` subtracts and returns the previous value.
* `FetchAndAtomic(handle rawptr, value uint) uint` applies bitwise AND and returns the previous value.
* `FetchOrAtomic(handle rawptr, value uint) uint` applies bitwise OR and returns the previous value.
* `FetchXorAtomic(handle rawptr, value uint) uint` applies bitwise XOR and returns the previous value.

A `nil` atomic handle causes value-returning operations to return zero and status-returning operations to return `false`.

Signed integers and booleans are represented by wrappers in the higher-level `atomic` package.

## Example

Direct use is mainly appropriate when implementing another runtime package. This example creates and operates a native mutex:

```seal
Main :: task() {
    mutex :=
        threadNative.CreateMutex()

    if mutex == nil {
        panic(
            "unable to create native mutex",
        )
    }

    if !threadNative.LockMutex(
        mutex,
    ) {
        panic(
            "unable to lock native mutex",
        )
    }

    /*
    Access shared native state here.
    */

    if !threadNative.UnlockMutex(
        mutex,
    ) {
        panic(
            "unable to unlock native mutex",
        )
    }

    if !threadNative.DestroyMutex(
        mutex,
    ) {
        panic(
            "unable to destroy native mutex",
        )
    }
}
```

## Safety and lifetime rules

* Prefer `thread`, `sync`, and `atomic` in application code.
* Treat every non-`nil` `rawptr` handle as an owned native resource.
* Do not copy ownership of handles without an explicit ownership protocol.
* Match every creation task with its destruction or consuming operation.
* Do not use a handle after successful destruction, join, or detach.
* Do not destroy synchronization objects while another thread may use them.
* Do not free a thread context before the entry task has finished using it.
* Check integer return values before interpreting timeout as failure.
* Platform behavior is normalized where practical, but identifiers and low-level failure conditions remain platform-dependent.
