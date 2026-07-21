# Seal Multithreading Package Design

## 1. Goals

The multithreading packages should provide:

* portable thread creation and joining;
* compile-time selection of worker tasks;
* typed worker input;
* platform-specific ABI handling hidden from applications;
* no runtime task values;
* no task fields inside structs;
* no indirect task calls;
* explicit ownership of worker context and results.

Worker tasks are supplied as generic task parameters:

```seal
Worker task[(T)]
```

or:

```seal
Worker task[(T) R]
```

The thread package specializes an internal entry task for each worker and context type.

---

## 2. Package layout

```text
std/
├── thread/
│   ├── seal.toml
│   ├── thread.seal
│   └── context.seal
│
├── sync/
│   ├── seal.toml
│   ├── mutex.seal
│   ├── condition.seal
│   ├── semaphore.seal
│   └── once.seal
│
├── atomic/
│   ├── seal.toml
│   └── atomic.seal
│
└── platform/
    └── thread_native/
        ├── seal.toml
        ├── common.seal
        ├── windows.seal
        ├── posix.seal
        └── native.c
```

Suggested dependency direction:

```text
platform.thread_native
        ↓
      thread
        ↓
       sync

atomic is independent where possible
```

Applications should normally depend on:

```text
thread
sync
atomic
```

They should not use `platform.thread_native` directly.

---

## 3. Platform package

The platform package owns all foreign ABI directives.

Conceptually:

```seal
NativeThreadResult :: @foreign_type(
    SEAL_THREAD_RESULT
)

native_thread_success :: @foreign_value(
    NativeThreadResult,
    SEAL_THREAD_SUCCESS
)

ThreadEntryABI :: @foreign_task(
    declaration SEAL_THREAD_RESULT SEAL_THREAD_CALL {name}(void *{arg0_name}),
    address SEAL_THREAD_ADDRESS({name}),
)
```

It exposes ordinary Seal wrappers around native operations:

```seal
NativeThread :: struct {
    handle rawptr
}

Start :: task(
    thread *NativeThread,
    entry rawptr,
    context rawptr,
) bool

Join :: task(
    thread *NativeThread,
) bool

Detach :: task(
    thread *NativeThread,
) bool
```

The platform package may use different C implementations for:

```text
Windows threads
POSIX pthreads
other supported targets
```

The public `thread` package should not need to know the calling convention used by the host platform.

---

## 4. Public thread handle

The public package exposes a thread handle without storing the worker task:

```seal
Thread :: struct {
    native thread_native.NativeThread
    state rawptr
}
```

A `Thread` stores only runtime state:

* native handle;
* allocated context;
* optional result storage;
* completion state.

It does not store:

* a task value;
* a generic task argument;
* a callback field;
* a runtime function pointer used by Seal code.

The foreign task pointer is passed only when the thread is started.

---

## 5. Worker model without result

The simplest worker signature is:

```seal
Worker task[(T)]
```

The public API can be:

```seal
Start :: task<
    T type,
    Worker task[(T)],
>(
    value T,
) Thread
```

Internal context:

```seal
WorkerContext :: struct<
    T type,
> {
    value T
}
```

Internal foreign entry task:

```seal
WorkerEntry :: @foreign(
    thread_native.ThreadEntryABI
) task<
    T type,
    Worker task[(T)],
>(
    rawContext rawptr,
) thread_native.NativeThreadResult {
    context := cast<*WorkerContext<T>>(rawContext)

    Worker(context.value)

    thread_native.ReleaseContext(rawContext)

    return thread_native.native_thread_success
}
```

Thread creation:

```seal
Start :: task<
    T type,
    Worker task[(T)],
>(
    value T,
) Thread {
    context := AllocateWorkerContext<T>(value)

    entry := @task_pointer(
        WorkerEntry<T, Worker>
    )

    return StartNativeThread(
        entry,
        cast<rawptr>(context),
    )
}
```

The worker task is known at compile time. Only its specialized foreign entry address crosses into the native threading API.

---

## 6. Worker model with result

A result-producing worker can use:

```seal
Worker task[(T) R]
```

Public API:

```seal
StartWithResult :: task<
    T type,
    R type,
    Worker task[(T) R],
>(
    value T,
) JoinHandle<R>
```

The runtime context stores the input and result:

```seal
ResultContext :: struct<
    T type,
    R type,
> {
    value T
    result R
    completed bool
}
```

The task is still not stored:

```seal
ResultWorkerEntry :: @foreign(
    thread_native.ThreadEntryABI
) task<
    T type,
    R type,
    Worker task[(T) R],
>(
    rawContext rawptr,
) thread_native.NativeThreadResult {
    context := cast<*ResultContext<T, R>>(rawContext)

    context.result = Worker(context.value)
    context.completed = true

    return thread_native.native_thread_success
}
```

Public join handle:

```seal
JoinHandle :: struct<
    R type,
> {
    thread Thread
    resultContext rawptr
}
```

Join operation:

```seal
Join :: task<R type>(
    handle *JoinHandle<R>,
) R
```

`Join` waits for the native thread, reads the result, releases the context, and invalidates the handle.

---

## 7. Multiple worker parameters

The API should avoid requiring a distinct thread function shape for every parameter count.

Users can place arguments in a struct:

```seal
LoadArguments :: struct {
    path string
    retryCount int
}

LoadFile :: task(arguments LoadArguments) {
}
```

Then:

```seal
handle := thread.Start<
    LoadArguments,
    LoadFile,
>(
    LoadArguments{
        path = "data.bin",
        retryCount = 3,
    },
)
```

This keeps the generic worker signature simple:

```seal
task[(T)]
```

It also gives the context a stable, explicit lifetime.

---

## 8. No-input workers

A separate API can support workers with no runtime argument:

```seal
StartTask :: task<
    Worker task[()],
>() Thread
```

Internal entry:

```seal
TaskEntry :: @foreign(
    thread_native.ThreadEntryABI
) task<
    Worker task[()],
>(
    context rawptr,
) thread_native.NativeThreadResult {
    Worker()

    return thread_native.native_thread_success
}
```

Usage:

```seal
BackgroundCleanup :: task() {
}

worker := thread.StartTask<
    BackgroundCleanup,
>()
```

---

## 9. Thread lifecycle

Recommended public operations:

```seal
Join :: task(thread *Thread)

Detach :: task(thread *Thread)

IsJoinable :: pure task(thread *Thread) bool
```

For result handles:

```seal
Join :: task<R type>(
    handle *JoinHandle<R>,
) R
```

Rules:

* a thread may be joined once;
* a thread may be detached once;
* detached result-producing threads should be rejected or use a separate API;
* dropping a joinable thread without joining or detaching should be considered an error-prone operation;
* the first implementation may require explicit `Join` or `Detach`.

Typical usage:

```seal
worker := thread.Start<int, ProcessValue>(10)
defer thread.Join(&worker)
```

---

## 10. Error handling

Initial APIs may return `bool`:

```seal
TryStart :: task<
    T type,
    Worker task[(T)],
>(
    value T,
    output *Thread,
) bool
```

Once a standard `Result<T, E>` exists, prefer:

```seal
Start :: task<
    T type,
    Worker task[(T)],
>(
    value T,
) Result<Thread, ThreadError>
```

Possible errors:

```seal
ThreadError :: enum {
    CreationFailed
    JoinFailed
    DetachFailed
    InvalidState
    AllocationFailed
}
```

Avoid using native platform error codes as the primary portable API.

A lower-level function may expose them separately when required.

---

## 11. Synchronization package

The `sync` package should wrap runtime synchronization objects without compiler directives in its public API.

### Mutex

```seal
Mutex :: struct {
    native rawptr
}

InitMutex :: task() Mutex
Lock :: task(mutex *Mutex)
Unlock :: task(mutex *Mutex)
Destroy :: task(mutex *Mutex)
```

Usage:

```seal
SharedState :: struct {
    mutex sync.Mutex
    value int
}

Increment :: task(state *SharedState) {
    sync.Lock(&state.mutex)
    defer sync.Unlock(&state.mutex)

    state.value += 1
}
```

### Condition variable

```seal
Condition :: struct {
    native rawptr
}

Wait :: task(
    condition *Condition,
    mutex *Mutex,
)

Signal :: task(condition *Condition)
Broadcast :: task(condition *Condition)
```

### Semaphore

```seal
Semaphore :: struct {
    native rawptr
}

Acquire :: task(semaphore *Semaphore)
Release :: task(semaphore *Semaphore)
```

### Once

Because tasks cannot be stored, `Once` should receive the initialization task as a generic parameter:

```seal
RunOnce :: task<
    Init task[()],
>(
    once *Once,
)
```

Usage:

```seal
InitializeRuntime :: task() {
}

sync.RunOnce<
    InitializeRuntime,
>(
    &runtimeOnce,
)
```

The `Once` structure stores only state, not `InitializeRuntime`.

---

## 12. Atomics package

Atomics should use typed wrapper structures:

```seal
AtomicInt :: struct {
    storage int
}

AtomicBool :: struct {
    storage bool
}
```

Operations:

```seal
Load :: pure task(value *AtomicInt) int

Store :: task(
    value *AtomicInt,
    next int,
)

Exchange :: task(
    value *AtomicInt,
    next int,
) int

CompareExchange :: task(
    value *AtomicInt,
    expected *int,
    desired int,
) bool

FetchAdd :: task(
    value *AtomicInt,
    amount int,
) int
```

Memory ordering may begin with a safe default API and later add explicit variants:

```seal
LoadOrdered<Order>(...)
StoreOrdered<Order>(...)
```

where `Order` is a compile-time enum or value parameter.

---

## 13. Higher-level parallel helpers

Higher-level packages can also use generic task parameters.

Example parallel iteration:

```seal
ForEach :: task<
    T type,
    Worker task[(*T)],
>(
    values *Slice<T>,
)
```

Example parallel map:

```seal
Map :: task<
    T type,
    R type,
    Worker task[(T) R],
>(
    values *Slice<T>,
) Array<R>
```

Every specialized worker produces a specialized internal entry task.

The implementation must not place `Worker` inside:

* a queue item;
* a worker-pool structure;
* a thread handle;
* a closure-like runtime object.

A worker pool requiring runtime heterogeneous jobs cannot be represented with the current task model.

A homogeneous pool can be specialized by worker task:

```seal
Pool :: struct<
    T type,
> {
    // Queue and synchronization state only.
}

CreatePool :: task<
    T type,
    Worker task[(T)],
>(
    workerCount uint,
) Pool<T>
```

All jobs submitted to that pool use the same compile-time `Worker`.

---

## 14. Public usage examples

### Fire-and-join worker

```seal
PrintNumber :: task(value int) {
    // ...
}

Main :: task() {
    worker := thread.Start<
        int,
        PrintNumber,
    >(42)

    thread.Join(&worker)
}
```

### Result-producing worker

```seal
Square :: task(value int) int {
    return value * value
}

Main :: task() {
    worker := thread.StartWithResult<
        int,
        int,
        Square,
    >(12)

    result := thread.Join<int>(&worker)

    assert(result == 144)
}
```

### Structured arguments

```seal
CopyArguments :: struct {
    source string
    destination string
}

CopyFile :: task(arguments CopyArguments) bool {
    // ...
    return true
}

Main :: task() {
    worker := thread.StartWithResult<
        CopyArguments,
        bool,
        CopyFile,
    >(
        CopyArguments{
            source = "input.dat",
            destination = "output.dat",
        },
    )

    copied := thread.Join<bool>(&worker)
    assert(copied)
}
```

### Multiple concurrent workers using one task

```seal
Compute :: task(value int) int {
    return value * 2
}

Main :: task() {
    first := thread.StartWithResult<
        int,
        int,
        Compute,
    >(10)

    second := thread.StartWithResult<
        int,
        int,
        Compute,
    >(20)

    a := thread.Join<int>(&first)
    b := thread.Join<int>(&second)

    assert(a == 20)
    assert(b == 40)
}
```

---

## 15. Package-level directive boundary

The intended boundary is:

```text
application code
    ↓ ordinary generic thread APIs

thread package
    ↓ specialized entry tasks

platform.thread_native
    ↓ foreign ABI declarations and native wrappers

host threading API
```

Application users should not normally write:

```seal
@foreign_type
@foreign_value
@foreign_task
@foreign
@task_pointer
```

Those directives belong in the platform and low-level thread packages.

The public API exposes ordinary Seal declarations whose task behavior is selected using generic task parameters.

---

## 16. Initial implementation order

Recommended order:

1. `platform.thread_native`

   * native thread handle;
   * start;
   * join;
   * detach;
   * foreign entry ABI.

2. `thread`

   * `Thread`;
   * `Start<T, Worker>`;
   * `StartTask<Worker>`;
   * `Join`;
   * `Detach`.

3. Result support

   * `JoinHandle<R>`;
   * `StartWithResult<T, R, Worker>`;
   * result context ownership.

4. `sync`

   * mutex;
   * condition variable;
   * semaphore;
   * once.

5. `atomic`

   * integer and boolean atomics;
   * compare-exchange;
   * fetch operations.

6. Higher-level concurrency

   * specialized worker pools;
   * parallel iteration;
   * channels or queues after ownership rules are clearer.

---

## 17. Current limitations

The first multithreading API should explicitly document:

* tasks are compile-time values only;
* task values cannot be stored in fields or variables;
* thread workers are selected through generic task parameters;
* heterogeneous runtime job queues are not supported;
* worker arguments must remain valid for the required lifetime;
* mutable shared data requires synchronization;
* thread cancellation is not initially supported;
* thread-local storage may be added later;
* owned result and panic propagation require further runtime design.
