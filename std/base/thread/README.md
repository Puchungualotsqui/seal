# thread

The `thread` package creates and manages native operating-system threads.

It supports:

* Workers with one argument
* Workers without arguments
* Workers that return a result
* Blocking and timed joins
* Detached threads
* Basic thread utilities

A created thread owns native resources. `Thread` and `JoinHandle` values must not be copied after creation.

## Error handling

Operations that may fail return an `Error`.

```seal
Error :: enum {
    None
    AllocationFailed
    CreationFailed
    JoinFailed
    DetachFailed
    InvalidState
}
```

* `ErrorIsNone(error Error) bool` reports whether an operation succeeded.
* `ErrorString(error Error) string` returns a readable error description.

Tasks such as `Start`, `JoinOrPanic`, and `DetachOrPanic` panic instead of returning creation or lifecycle errors.

## Thread

`Thread` owns one native thread handle.

```seal
Thread :: struct {
    Handle rawptr
    State ThreadState
}
```

Its state is represented by:

```seal
ThreadState :: enum {
    Empty
    Joinable
    Consumed
}
```

* `Empty` means no native thread is owned.
* `Joinable` means the thread can be joined or detached.
* `Consumed` means ownership was released by a successful join or detach.

`Empty() Thread` returns a thread with no native handle.

`IsJoinable(value *Thread) bool` reports whether a thread currently owns a joinable native handle.

## Starting threads

### Worker with one argument

A worker passed to `Start` must accept one value and return nothing.

```seal
Worker :: task(
    value T,
)
```

* `TryStart<T, Worker>(value T) (Thread, Error)` starts the worker and reports allocation or creation errors.
* `Start<T, Worker>(value T) Thread` starts the worker or panics.

The argument is copied into internal thread-owned storage before the thread starts.

Avoid passing a pointer to data that may be destroyed while the worker is still running.

### Worker without arguments

* `TryStartTask<Worker>() (Thread, Error)` starts a task with no parameters.
* `StartTask<Worker>() Thread` starts it or panics.

The worker must have this shape:

```seal
Worker :: task()
```

### Worker with a result

`JoinHandle<R>` owns both the native thread and its result storage.

```seal
JoinHandle :: struct<R type> {
    Thread Thread
    Result rawptr
    State JoinHandleState
}
```

Its lifecycle is represented by:

```seal
JoinHandleState :: enum {
    Empty
    Joinable
    Consumed
}
```

* `TryStartWithResult<T, R, Worker>(value T) (JoinHandle<R>, Error)` starts a result-producing worker.
* `StartWithResult<T, R, Worker>(value T) JoinHandle<R>` starts it or panics.
* `JoinHandleIsJoinable<R>(value *JoinHandle<R>) bool` reports whether the handle still owns a thread and result storage.

The worker must accept one argument and return one result:

```seal
Worker :: task(
    value T,
) R
```

A result handle cannot be detached because its result storage must be reclaimed through one of the result-join operations.

## Joining threads

`Join(value *Thread) Error` waits until a thread finishes.

After a successful join:

* The native handle is released.
* `Handle` becomes `nil`.
* The state becomes `Consumed`.

If the native join fails, the `Thread` retains ownership and may still be used for another join or detach attempt.

`JoinOrPanic(value *Thread)` joins or panics.

### Timed join

`TryJoinFor(value *Thread, milliseconds uint) (bool, Error)` waits for at most the requested duration.

Its results are:

```text
true,  None     the thread completed and was consumed
false, None     the timeout expired
false, error    the operation failed
```

A timeout does not consume the thread. It remains joinable.

## Joining result workers

`JoinResult<R>(value *JoinHandle<R>) R` waits for the worker, retrieves its result, releases the result storage, and consumes the handle.

It panics when the handle is invalid or the native join fails.

`TryJoinResultFor<R>(value *JoinHandle<R>, milliseconds uint) (R, bool, Error)` performs a timed result join.

Its results are:

```text
result, true,  None     the worker completed
zero,   false, None     the timeout expired
zero,   false, error    the operation failed
```

The handle retains ownership after a timeout or native failure.

The returned `zero` value is the default value of `R` and should not be interpreted unless `completed` is `true`.

`JoinDiscardResult<R>(value *JoinHandle<R>) Error` joins the worker and releases its result storage without returning the result.

## Detaching

`Detach(value *Thread) Error` releases ownership of a running thread without waiting for it.

After a successful detach:

* The thread continues independently.
* The native handle is released from the `Thread`.
* The state becomes `Consumed`.
* The thread can no longer be joined.

`DetachOrPanic(value *Thread)` detaches or panics.

A detached worker must not access local data that may go out of scope before the worker finishes.

Use another synchronization mechanism, such as a semaphore, when the program needs to observe detached-worker completion.

## Thread utilities

* `Yield()` gives other runnable threads an opportunity to execute.
* `Sleep(milliseconds uint)` pauses the current thread for approximately the requested duration.
* `HardwareThreadCount() uint` returns the number of hardware threads reported by the platform.
* `CurrentThreadID() uint` returns the native identifier of the current thread.

Thread identifiers are intended for diagnostics and logging. Do not assume that they are sequential or permanently unique.

## Example

This example starts a worker, performs a short timed join, and then retrieves its result.

```seal
Square :: task(
    value int,
) int {
    thread.Sleep(20)

    return value * value
}

Main :: task() {
    worker :=
        thread.StartWithResult<
            int,
            int,
            Square
        >(12)

    earlyResult,
        completed,
        error :=
        thread.TryJoinResultFor<int>(
            &worker,
            1,
        )

    if !thread.ErrorIsNone(error) {
        panic(
            thread.ErrorString(error),
        )
    }

    if completed {
        fmt.Println(
            "completed early: %v",
            earlyResult,
        )

        return
    }

    result :=
        thread.JoinResult<int>(
            &worker,
        )

    fmt.Println(
        "result: %v",
        result,
    )
}
```

## Ownership rules

* Do not copy a `Thread` after it becomes joinable.
* Do not copy a `JoinHandle` after it becomes joinable.
* Every successful start must eventually be joined or detached.
* A `JoinHandle` must be completed with `JoinResult`, `TryJoinResultFor`, or `JoinDiscardResult`.
* Do not join or detach the same thread more than once.
* Do not use a consumed thread or result handle.
* A timed-out join retains ownership.
* A failed native join or detach retains ownership.
* Ensure referenced data remains alive until the worker no longer uses it.
