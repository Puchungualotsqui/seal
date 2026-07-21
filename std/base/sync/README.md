# sync

The `sync` package provides synchronization primitives for coordinating multiple threads:

* Mutexes
* Condition variables
* One-time initialization
* Semaphores
* Read-write mutexes

Each primitive owns a native operating-system handle and must be destroyed when it is no longer needed.

## Error handling

Most operations return an `Error` value instead of panicking.

```seal
Error :: enum {
    None
    AllocationFailed
    LockFailed
    UnlockFailed
    WaitFailed
    SignalFailed
    ReleaseFailed
    DestroyFailed
    InvalidState
    NativeFailure
}
```

`ErrorIsNone` returns whether an operation succeeded.

```seal
sync.ErrorIsNone(error)
```

`ErrorString` returns a readable description of an error.

```seal
sync.ErrorString(error)
```

Tasks ending in `OrPanic` call the corresponding error-returning task and panic when it fails.

## Mutex

A `Mutex` provides exclusive access to shared state.

```seal
Mutex :: struct {
    Handle rawptr
}
```

### Creation

* `TryNewMutex() (Mutex, Error)` creates a mutex and reports allocation failure.
* `NewMutex() Mutex` creates a mutex or panics.

### Operations

* `MutexIsValid(value *Mutex) bool` reports whether the mutex has a native handle.
* `Lock(value *Mutex) Error` waits until the mutex is locked.
* `LockOrPanic(value *Mutex)` locks or panics.
* `TryLock(value *Mutex) (bool, Error)` attempts to lock without waiting.
* `Unlock(value *Mutex) Error` unlocks the mutex.
* `UnlockOrPanic(value *Mutex)` unlocks or panics.
* `DestroyMutex(value *Mutex) Error` destroys the mutex and clears its handle.
* `DestroyMutexOrPanic(value *Mutex)` destroys or panics.

A mutex must only be unlocked by code that currently owns its lock.

## Condition variable

A `Condition` lets threads sleep until another thread signals that shared state may have changed.

```seal
Condition :: struct {
    Handle rawptr
}
```

Condition variables must be used together with a `Mutex`. Always check the shared-state condition in a loop because waking does not guarantee that the condition is now true.

### Creation

* `TryNewCondition() (Condition, Error)` creates a condition variable.
* `NewCondition() Condition` creates one or panics.

### Waiting

* `ConditionIsValid(value *Condition) bool` reports whether the condition is valid.
* `Wait(condition *Condition, mutex *Mutex) Error` releases the mutex, waits, and reacquires the mutex before returning.
* `WaitFor(condition *Condition, mutex *Mutex, milliseconds uint) (bool, Error)` performs a timed wait. It returns `false` on timeout.
* `WaitOrPanic(condition *Condition, mutex *Mutex)` waits or panics.

### Notification

* `Signal(value *Condition) Error` wakes one waiting thread.
* `SignalOrPanic(value *Condition)` signals or panics.
* `Broadcast(value *Condition) Error` wakes all waiting threads.
* `BroadcastOrPanic(value *Condition)` broadcasts or panics.

### Destruction

* `DestroyCondition(value *Condition) Error` destroys the condition and clears its handle.
* `DestroyConditionOrPanic(value *Condition)` destroys or panics.

## Once

`Once` ensures that an initialization task is selected for execution only once.

```seal
Once :: struct {
    Handle rawptr
}
```

### Creation

* `TryNewOnce() (Once, Error)` creates a once state.
* `NewOnce() Once` creates one or panics.
* `OnceIsValid(value *Once) bool` reports whether it is valid.

### Execution

* `RunOnce<Init>(value *Once) Error` runs a no-argument initialization task once.
* `RunOnceWith<T, Init>(value *Once, context *T) Error` passes context to the thread selected to initialize.

Other callers wait for or observe completion and do not run the initialization task again.

### Destruction

* `DestroyOnce(value *Once) Error` destroys the once state.
* `DestroyOnceOrPanic(value *Once)` destroys or panics.

Do not destroy a `Once` while another thread may still be using it.

## Semaphore

A `Semaphore` stores a count of available permits.

```seal
Semaphore :: struct {
    Handle rawptr
}
```

Acquiring consumes one permit. Releasing adds one or more permits.

### Creation

* `TryNewSemaphore(initialCount uint) (Semaphore, Error)` creates a semaphore.
* `NewSemaphore(initialCount uint) Semaphore` creates one or panics.
* `SemaphoreIsValid(value *Semaphore) bool` reports whether it is valid.

### Acquiring

* `Acquire(value *Semaphore) Error` waits for and consumes one permit.
* `AcquireOrPanic(value *Semaphore)` acquires or panics.
* `TryAcquire(value *Semaphore) (bool, Error)` attempts to acquire without waiting.
* `AcquireFor(value *Semaphore, milliseconds uint) (bool, Error)` waits up to the given duration and returns `false` on timeout.

### Releasing

* `Release(value *Semaphore, count uint) Error` adds `count` permits. `count` must be greater than zero.
* `ReleaseOne(value *Semaphore) Error` adds one permit.
* `ReleaseOrPanic(value *Semaphore, count uint)` releases or panics.

### Destruction

* `DestroySemaphore(value *Semaphore) Error` destroys the semaphore.
* `DestroySemaphoreOrPanic(value *Semaphore)` destroys or panics.

## Read-write mutex

An `RWMutex` supports two lock modes:

* Multiple threads may hold read locks simultaneously.
* Only one thread may hold the write lock, with no active readers.

```seal
RWMutex :: struct {
    Handle rawptr
}
```

### Creation

* `TryNewRWMutex() (RWMutex, Error)` creates a read-write mutex.
* `NewRWMutex() RWMutex` creates one or panics.
* `RWMutexIsValid(value *RWMutex) bool` reports whether it is valid.

### Read locking

* `ReadLock(value *RWMutex) Error` waits for a read lock.
* `TryReadLock(value *RWMutex) (bool, Error)` attempts a read lock without waiting.
* `ReadLockFor(value *RWMutex, milliseconds uint) (bool, Error)` performs a timed read-lock attempt.
* `ReadUnlock(value *RWMutex) Error` releases a read lock.

### Write locking

* `WriteLock(value *RWMutex) Error` waits for exclusive access.
* `TryWriteLock(value *RWMutex) (bool, Error)` attempts a write lock without waiting.
* `WriteLockFor(value *RWMutex, milliseconds uint) (bool, Error)` performs a timed write-lock attempt.
* `WriteUnlock(value *RWMutex) Error` releases the write lock.

### Destruction

* `DestroyRWMutex(value *RWMutex) Error` destroys the read-write mutex.
* `DestroyRWMutexOrPanic(value *RWMutex)` destroys or panics.

## Example

The following example protects a shared counter with a mutex:

```seal
SharedCounter :: struct {
    Mutex sync.Mutex
    Value int
}

Increment :: task(
    counter *SharedCounter,
) {
    sync.LockOrPanic(
        &counter.Mutex,
    )

    counter.Value += 1

    sync.UnlockOrPanic(
        &counter.Mutex,
    )
}

Main :: task() {
    counter := SharedCounter{
        Mutex = sync.NewMutex(),
        Value = 0,
    }

    Increment(&counter)

    sync.DestroyMutexOrPanic(
        &counter.Mutex,
    )
}
```

## Lifetime rules

Synchronization values contain native handles and are not ordinary copyable values.

* Initialize each value before use.
* Pass pointers to synchronization operations.
* Do not copy an initialized synchronization value.
* Do not destroy a value while another thread may use it.
* Do not use a value after destruction.
* Successful destruction clears its handle, causing later operations to return `InvalidState`.
