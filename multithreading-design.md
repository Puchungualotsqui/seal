# Seal Multithreading and Concurrency Package Design

## 1. Overview

Seal provides multithreading through a small set of standard-library packages rather than through new language primitives.

The design is divided into low-level native concurrency packages and higher-level structured concurrency packages.

```text
// Low level
thread
atomic
sync

// Higher level
channel
concurrent

// Data parallelism
parallel
```

The packages have separate responsibilities:

| Package      | Responsibility                                            |
| ------------ | --------------------------------------------------------- |
| `thread`     | Native operating-system thread creation and lifecycle     |
| `atomic`     | Explicit typed atomic values and atomic memory operations |
| `sync`       | Blocking synchronization primitives                       |
| `channel`    | Typed communication between concurrent tasks              |
| `concurrent` | Structured task spawning, groups, handles, and futures    |
| `parallel`   | Data-parallel loops, mapping, reduction, and worker pools |

The design follows one central rule:

> Tasks remain compile-time identities. Only their arguments, results, handles, and synchronization state exist at runtime.

Seal does not initially introduce:

* first-class task values;
* closures;
* runtime function objects;
* task fields in structs;
* arrays or channels of tasks;
* a goroutine scheduler;
* a new concurrency syntax;
* implicit shared-state protection;
* a general Go-style `select` primitive.

Concurrency is built using existing Seal generics, task-signature constraints, ordinary structs, native C interoperability, and generated task-specific wrappers.

---

# 2. Language constraints

## 2.1 Tasks are not runtime values

Seal tasks cannot be stored as ordinary runtime data.

The following patterns are not supported:

```seal
Invalid :: struct {
    callback task(int)
}
```

```seal
callbacks [8]task(int)
```

```seal
channel.Channel<task(int)>
```

A task cannot be passed as a normal struct field inside another task argument.

This means a native thread API cannot receive an arbitrary task value stored in a struct.

Instead, a task must be passed as a compile-time generic argument:

```seal
Run :: task <
    F task[()],
>() {
    F()
}
```

Or with typed input and output:

```seal
Apply :: task <
    T type,
    R type,
    F task[(T) R],
>(value T) R {
    return F(value)
}
```

The exact task is therefore known during specialization and C generation.

---

## 2.2 Task-signature constraints

Concurrency APIs use task-signature generic constraints to guarantee that a supplied task has the required shape.

Example:

```seal
Spawn :: task <
    F task[()],
>() Handle
```

The task must accept no arguments and return no value.

For a task with an argument:

```seal
SpawnWith :: task <
    T type,
    F task[(T)],
>(argument T) Handle
```

The supplied task must have this shape:

```seal
task(T)
```

For a result-producing task:

```seal
SpawnResult :: task <
    R type,
    F task[() R],
>() Future<R>
```

For a task with input and output:

```seal
SpawnResultWith :: task <
    T type,
    R type,
    F task[(T) R],
>(argument T) Future<R>
```

These constraints are mandatory. The checker must reject task arguments whose parameter or return types do not match.

---

## 2.3 Static behavior and runtime data

Concurrency types may use task parameters as compile-time generic arguments, but tasks are not stored inside their runtime representation.

Example:

```seal
Pool :: struct <
    T type,
    Worker task[(T)],
> {
    jobs channel.Channel<T>
}
```

`Worker` affects specialization and generated code, but it does not occupy a runtime struct field.

This pattern is called:

> Static behavior with runtime data.

It is the main mechanism used by Seal’s concurrency design.

---

## 2.4 Native task trampolines

Native operating-system thread APIs require a function pointer with a fixed C-compatible signature.

Seal must generate a native trampoline for every concrete task specialization.

Conceptually:

```seal
PrintNumber :: task(value int) {
    fmt.PrintLine(value)
}

thread.StartWith<
    int,
    PrintNumber
>(42)
```

May generate C similar to:

```c
typedef struct {
    intptr_t value;
} seal_thread_PrintNumber_int_argument;

static unsigned long seal_thread_PrintNumber_int_entry(
    void *opaque
) {
    seal_thread_PrintNumber_int_argument *argument =
        opaque;

    intptr_t value =
        argument->value;

    free(argument);

    PrintNumber(value);

    return 0;
}
```

The native thread receives the generated wrapper, not a runtime Seal task object.

The wrapper is responsible for:

1. decoding the stored argument;
2. releasing temporary thread argument storage when appropriate;
3. invoking the statically known Seal task;
4. storing a result if the operation produces one;
5. marking completion;
6. notifying waiters;
7. returning through the platform thread ABI.

---

# 3. Package dependency hierarchy

The dependency direction must remain acyclic.

```text
atomic
thread

sync
├── atomic
└── thread

channel
└── sync

concurrent
├── thread
├── sync
└── channel

parallel
├── concurrent
├── sync
└── atomic
```

Important restrictions:

* `thread` must not depend on `sync`.
* `atomic` should not depend on the other concurrency packages.
* `sync` may use `atomic` and native waiting primitives.
* `channel` is implemented using `sync`.
* `concurrent` builds structured APIs over `thread`, `sync`, and `channel`.
* `parallel` builds data-parallel operations over `concurrent`.

Platform-specific C implementations may use shared internal native helpers, but public Seal package dependencies must remain acyclic.

---

# 4. Package naming

The selected package names are:

```text
thread
atomic
sync
channel
concurrent
parallel
```

`concurrent` is preferred over `concurrency`.

Examples read naturally:

```seal
concurrent.Spawn<Work>()
concurrent.Wait(&handle)
concurrent.Group{}
```

`parallel` is kept separate because concurrent execution and parallel data processing are related but not identical.

* `concurrent` manages independently executing operations.
* `parallel` divides computational work over multiple workers.
* `channel` transfers typed values between concurrent operations.
* `thread` exposes native operating-system threads.

---

# 5. `thread` package

## 5.1 Purpose

The `thread` package exposes low-level native thread functionality.

It is intentionally small.

It does not provide:

* channels;
* mutexes;
* futures;
* task groups;
* worker pools;
* cancellation;
* scheduling;
* automatic work stealing;
* coroutine stacks;
* panic propagation.

Those features belong in higher-level packages.

---

## 5.2 Public types

```seal
Id :: distinct uint
```

```seal
Thread :: struct {
    // Opaque native state.
}
```

The exact fields of `Thread` must not be public.

Its native representation may contain:

* a Windows thread handle;
* a POSIX `pthread_t`;
* join state;
* detach state;
* completion state;
* internal ownership flags.

---

## 5.3 Thread creation

### No-argument task

```seal
Start :: task <
    F task[()],
>() Thread
```

Example:

```seal
PrintHello :: task() {
    fmt.PrintLine("hello")
}

worker :=
    thread.Start<PrintHello>()

thread.Join(&worker)
```

### Task with argument

```seal
StartWith :: task <
    T type,
    F task[(T)],
>(argument T) Thread
```

Example:

```seal
PrintNumber :: task(value int) {
    fmt.PrintLine(value)
}

worker :=
    thread.StartWith<
        int,
        PrintNumber
    >(42)

thread.Join(&worker)
```

The task argument is stored in thread-owned memory before the native thread begins.

---

## 5.4 Thread lifecycle

```seal
Join :: task(
    thread *Thread,
)
```

`Join` blocks until the thread exits.

Calling `Join` on an already joined thread is invalid unless the implementation explicitly defines it as harmless.

```seal
Detach :: task(
    thread *Thread,
)
```

`Detach` releases the requirement to join the native thread.

After detachment:

* the thread may continue executing;
* its native resources are automatically released after completion;
* it cannot later be joined;
* the `Thread` value must not be reused as an active handle.

---

## 5.5 Thread information

```seal
CurrentId :: task() Id
```

Returns the identifier of the calling native thread.

Thread identifiers are useful for diagnostics and debugging. They must not be treated as stable globally unique application identifiers.

```seal
Yield :: task()
```

Hints that the current native thread may allow another runnable thread to execute.

```seal
ProcessorCount :: task() uint
```

Returns the logical processor count visible to the current process.

This value may be used as a default worker count, but it is not a guarantee of the ideal parallelism level.

---

## 5.6 Thread ownership rules

A `Thread` handle must not be copied after initialization.

The intended usage is:

```seal
worker := thread.Start<Work>()
thread.Join(&worker)
```

Not:

```seal
first := thread.Start<Work>()
second := first
```

Copying native handles can cause:

* double join;
* double close;
* use-after-close;
* inconsistent detach state;
* native resource leaks.

Seal does not initially add a non-copyable type primitive solely for this package. The restriction must be documented and may later be enforced by the checker if move-only types are introduced.

---

## 5.7 Thread argument semantics

For:

```seal
thread.StartWith<T, F>(argument)
```

The initial design uses copy semantics.

The implementation copies the argument into thread-owned storage before starting the native thread.

Consequences:

* scalar values are copied;
* structs are copied;
* pointers are copied as pointer values;
* pointed-to data remains shared;
* the caller must ensure shared pointed-to data remains alive;
* shared mutable data must be synchronized;
* synchronization types and atomics must normally be passed by pointer.

The API does not imply deep copying.

---

# 6. `atomic` package

## 6.1 Purpose

The `atomic` package provides indivisible memory operations for a fixed set of supported scalar types.

Atomics are specialized operations with platform-sensitive requirements involving:

* size;
* alignment;
* native instruction support;
* lock-free guarantees;
* memory ordering;
* signedness.

For that reason, the first design avoids generic atomic types.

---

## 6.2 Explicit atomic types

The initial package should provide:

```seal
AtomicBool
AtomicInt
AtomicUint
AtomicInt32
AtomicUint32
AtomicInt64
AtomicUint64
AtomicUintptr
```

Possible later additions:

```seal
AtomicInt8
AtomicUint8
AtomicInt16
AtomicUint16
AtomicPointer
```

`AtomicPointer` should not be added until pointer typing, nullability, ownership, and safe conversion rules are clear.

---

## 6.3 Why atomics are not generic

The following generic design is intentionally avoided:

```seal
Atomic<T>
```

A generic atomic would imply that arbitrary types can be made atomic, which is false.

For example, these are generally not valid single-operation atomics:

```seal
Atomic<MyStruct>
Atomic<[16]byte>
Atomic<string>
```

Explicit types make supported operations and storage widths unambiguous.

---

## 6.4 Initialization

Atomics should support zero initialization:

```seal
counter := atomic.AtomicUint{}
ready := atomic.AtomicBool{}
```

Optional constructor tasks may also exist:

```seal
MakeBool :: task(value bool) AtomicBool
MakeUint :: task(value uint) AtomicUint
MakeInt64 :: task(value int64) AtomicInt64
```

Example:

```seal
counter :=
    atomic.MakeUint(10)
```

An atomic value must not be copied after concurrent use begins.

---

## 6.5 Basic operations

Each atomic type should provide operations appropriate for its underlying value.

For `AtomicUint`:

```seal
Load :: task(
    value *AtomicUint,
) uint
```

```seal
Store :: task(
    value *AtomicUint,
    replacement uint,
)
```

```seal
Swap :: task(
    value *AtomicUint,
    replacement uint,
) uint
```

```seal
CompareExchange :: task(
    value *AtomicUint,
    expected *uint,
    replacement uint,
) bool
```

```seal
Add :: task(
    value *AtomicUint,
    amount uint,
) uint
```

```seal
Subtract :: task(
    value *AtomicUint,
    amount uint,
) uint
```

Equivalent type-specific tasks are provided for the other atomic types.

---

## 6.6 Compare-exchange semantics

`CompareExchange` behaves as follows:

```seal
expected := old_value

changed :=
    atomic.CompareExchange(
        &value,
        &expected,
        replacement,
    )
```

If the current value equals `expected`:

* the atomic value is replaced;
* the task returns `true`;
* `expected` remains unchanged.

If the current value does not equal `expected`:

* the atomic value is not replaced;
* the task returns `false`;
* `expected` is updated with the observed current value.

This convention supports retry loops:

```seal
expected :=
    atomic.Load(&counter)

loop {
    replacement :=
        expected + 1

    when atomic.CompareExchange(
        &counter,
        &expected,
        replacement,
    ) {
        break
    }
}
```

---

## 6.7 Arithmetic return semantics

Atomic arithmetic operations return the new value.

```seal
new_count :=
    atomic.Add(
        &counter,
        1,
    )
```

This means:

```text
new_count = previous value + amount
```

The implementation may use a native fetch-add operation internally and then adjust the returned value.

The selected behavior must be consistent across all numeric atomic types.

---

## 6.8 Memory ordering

A future advanced API may expose:

```seal
Order :: enum {
    Relaxed
    Acquire
    Release
    AcquireRelease
    Sequential
}
```

The initial public operations should use sequentially consistent ordering.

Examples:

```seal
atomic.Load(&value)
atomic.Store(&value, replacement)
atomic.Add(&value, 1)
```

Ordered variants may later be added:

```seal
LoadOrdered
StoreOrdered
SwapOrdered
CompareExchangeOrdered
AddOrdered
SubtractOrdered
```

Not all memory orders are valid for all operations.

For example:

* `Release` is not valid for a pure load;
* `Acquire` is not valid for a pure store;
* compare-exchange may require separate success and failure orders.

The checker or runtime wrapper must validate invalid combinations.

The simple API remains the recommended default.

---

## 6.9 Atomic implementation

Possible platform implementations include:

* C11 `_Atomic`;
* GCC or Clang `__atomic` built-ins;
* Windows `Interlocked*` operations;
* compiler-specific intrinsics;
* a lock-backed fallback for unsupported widths.

The public behavior must remain consistent across compilers.

The package must not claim that every atomic type is lock-free unless that is guaranteed by the target.

A future query may expose:

```seal
IsLockFree :: task(
    value *AtomicUint64,
) bool
```

This is not required for the initial package.

---

# 7. `sync` package

## 7.1 Purpose

The `sync` package provides blocking synchronization primitives.

The initial set is:

```text
Mutex
RwMutex
Condition
WaitGroup
Once
```

A semaphore may be added later.

---

## 7.2 `Mutex`

```seal
Mutex :: struct {
    // Opaque synchronization state.
}
```

Operations:

```seal
Lock :: task(
    mutex *Mutex,
)
```

```seal
TryLock :: task(
    mutex *Mutex,
) bool
```

```seal
Unlock :: task(
    mutex *Mutex,
)
```

Example:

```seal
mutex := sync.Mutex{}

sync.Lock(&mutex)

shared_value += 1

sync.Unlock(&mutex)
```

The same thread must not unlock a mutex it does not own unless the native mutex design explicitly supports that behavior.

Recursive locking is not guaranteed.

The first `Mutex` should be a non-recursive mutex.

---

## 7.3 Scoped locking helper

Seal may provide:

```seal
WithLock :: task <
    F task[()],
>(
    mutex *Mutex,
) {
    Lock(mutex)
    F()
    Unlock(mutex)
}
```

Example:

```seal
UpdateSharedValue :: task() {
    shared_value += 1
}

sync.WithLock<
    UpdateSharedValue
>(&mutex)
```

This helper is limited because `F` cannot capture arbitrary local variables.

It does not replace explicit `Lock` and `Unlock`.

If Seal later gains guaranteed cleanup or defer-like semantics, those features may provide safer lock release.

---

## 7.4 `RwMutex`

```seal
RwMutex :: struct {
    // Opaque reader/writer synchronization state.
}
```

Operations:

```seal
ReadLock :: task(
    mutex *RwMutex,
)
```

```seal
TryReadLock :: task(
    mutex *RwMutex,
) bool
```

```seal
ReadUnlock :: task(
    mutex *RwMutex,
)
```

```seal
WriteLock :: task(
    mutex *RwMutex,
)
```

```seal
TryWriteLock :: task(
    mutex *RwMutex,
) bool
```

```seal
WriteUnlock :: task(
    mutex *RwMutex,
)
```

The fairness policy is implementation-defined initially.

Implementations may prefer readers or writers depending on native platform behavior.

Programs must not rely on strict fairness unless a future API guarantees it.

---

## 7.5 `Condition`

```seal
Condition :: struct {
    // Opaque condition-variable state.
}
```

Operations:

```seal
Wait :: task(
    condition *Condition,
    mutex *Mutex,
)
```

```seal
Signal :: task(
    condition *Condition,
)
```

```seal
Broadcast :: task(
    condition *Condition,
)
```

`Wait` must:

1. require the caller to hold the mutex;
2. atomically release the mutex while waiting;
3. block until signaled or spuriously awakened;
4. reacquire the mutex before returning.

Condition waits must always be placed in a loop:

```seal
sync.Lock(&mutex)

loop {
    when ready {
        break
    }

    sync.Wait(
        &condition,
        &mutex,
    )
}

sync.Unlock(&mutex)
```

The loop is mandatory because:

* condition variables may wake spuriously;
* another thread may consume the condition first;
* the protected state may change before the awakened thread reacquires the mutex.

---

## 7.6 `WaitGroup`

```seal
WaitGroup :: struct {
    // Counter and wait state.
}
```

Operations:

```seal
Add :: task(
    group *WaitGroup,
    count int,
)
```

```seal
Done :: task(
    group *WaitGroup,
)
```

```seal
Wait :: task(
    group *WaitGroup,
)
```

`Done` is equivalent to:

```seal
Add(group, -1)
```

Example:

```seal
Worker :: task(
    group *sync.WaitGroup,
) {
    DoWork()
    sync.Done(group)
}

group := sync.WaitGroup{}

sync.Add(&group, 2)

first :=
    thread.StartWith<
        *sync.WaitGroup,
        Worker
    >(&group)

second :=
    thread.StartWith<
        *sync.WaitGroup,
        Worker
    >(&group)

sync.Wait(&group)

thread.Join(&first)
thread.Join(&second)
```

The counter must never become negative.

Adding new work while another task is already waiting requires carefully defined semantics. The initial recommendation is:

* positive `Add` calls should occur before the corresponding work is exposed;
* reusing a completed `WaitGroup` is permitted only after all previous waiters have returned;
* concurrent reuse across generations is invalid.

---

## 7.7 `Once`

`Once` guarantees that one initialization task runs at most once.

A simple non-generic design would be:

```seal
Once :: struct {
    // State only.
}
```

```seal
Do :: task <
    F task[()],
>(
    once *Once,
)
```

Example:

```seal
Initialize :: task() {
    LoadConfiguration()
}

once := sync.Once{}

sync.Do<Initialize>(&once)
sync.Do<Initialize>(&once)
```

However, this permits misuse:

```seal
sync.Do<InitializeA>(&once)
sync.Do<InitializeB>(&once)
```

The same runtime `Once` value would be associated with different compile-time tasks.

The preferred design is therefore:

```seal
Once :: struct <
    F task[()],
> {
    // Runtime state only.
}
```

Usage:

```seal
Initialize :: task() {
    LoadConfiguration()
}

once :=
    sync.Once<Initialize>{}

sync.Do<Initialize>(&once)
```

Or, if task arguments can be inferred from the `Once` type:

```seal
sync.Do(&once)
```

`F` is not a struct field. It is a compile-time type parameter.

This design binds the one-time state to exactly one initialization task.

---

## 7.8 Synchronization object restrictions

The following objects must not be copied after initialization:

```text
Mutex
RwMutex
Condition
WaitGroup
Once
```

They should normally be:

* created in stable storage;
* passed by pointer;
* destroyed only when no task can still access them.

A future Seal ownership system may enforce these rules statically.

---

# 8. `channel` package

## 8.1 Purpose

The `channel` package provides typed communication between concurrent tasks.

Channels transfer runtime data, not tasks.

Valid:

```seal
channel.Channel<int>
channel.Channel<Message>
channel.Channel<*Request>
```

Invalid:

```seal
channel.Channel<task(int)>
```

---

## 8.2 Channel type

```seal
Channel :: struct <
    T type,
> {
    // Opaque buffer and synchronization state.
}
```

The runtime representation may contain:

* a circular buffer;
* capacity;
* length;
* read index;
* write index;
* closed state;
* a mutex;
* sender condition variable;
* receiver condition variable;
* waiting sender count;
* waiting receiver count.

---

## 8.3 Initial channel model

The first implementation should support bounded buffered channels.

Construction:

```seal
Make :: task <
    T type,
>(
    capacity uint,
) Channel<T>
```

Example:

```seal
messages :=
    channel.Make<Message>(32)
```

A capacity of zero should initially be rejected.

This avoids implementing synchronous rendezvous channels in the first version.

Possible behavior:

```seal
when capacity == 0 {
    return invalid argument
}
```

A future version may support unbuffered channels.

---

## 8.4 Send operations

```seal
Send :: task <
    T type,
>(
    channel *Channel<T>,
    value T,
) bool
```

Behavior:

* blocks while the buffer is full;
* wakes when capacity becomes available;
* stores a copy of `value`;
* returns `true` if the value was sent;
* returns `false` if the channel was already closed.

Sending to a closed channel must not silently succeed.

A future stricter design may return an error rather than `false`.

Non-blocking send:

```seal
TrySend :: task <
    T type,
>(
    channel *Channel<T>,
    value T,
) bool
```

`TrySend` returns:

* `true` when the value was sent;
* `false` when the channel is full or closed.

If distinguishing full from closed is necessary, use a status enum:

```seal
SendStatus :: enum {
    Sent
    Full
    Closed
}
```

Then:

```seal
TrySend :: task <
    T type,
>(
    channel *Channel<T>,
    value T,
) SendStatus
```

The enum form is more expressive and is preferred if it fits the standard-library style.

---

## 8.5 Receive operations

Blocking receive:

```seal
Receive :: task <
    T type,
>(
    channel *Channel<T>,
    output *T,
) bool
```

Behavior:

* blocks while the channel is empty and open;
* receives the oldest buffered value;
* returns `true` when a value is written to `output`;
* returns `false` when the channel is closed and drained.

Example:

```seal
value := Message{}

received :=
    channel.Receive<Message>(
        &messages,
        &value,
    )

when received {
    Process(value)
}
```

Non-blocking receive should use a status:

```seal
ReceiveStatus :: enum {
    Received
    Empty
    Closed
}
```

```seal
TryReceive :: task <
    T type,
>(
    channel *Channel<T>,
    output *T,
) ReceiveStatus
```

This distinguishes:

* an open but currently empty channel;
* a closed and drained channel;
* a successful receive.

---

## 8.6 Closing channels

```seal
Close :: task <
    T type,
>(
    channel *Channel<T>,
)
```

Closing a channel means:

* no new values may be sent;
* existing buffered values remain available;
* blocked receivers wake;
* blocked senders wake and fail;
* receivers continue until the buffer is drained;
* after draining, receive reports closed.

Closing an already closed channel should either:

* be harmless; or
* report an explicit error.

The initial recommendation is to make repeated close an error because it often indicates broken ownership.

Only the producer side responsible for channel lifetime should close the channel.

Receivers should not normally close channels they do not own.

---

## 8.7 Channel value semantics

`Send<T>` copies the supplied value into channel storage.

For pointer values:

```seal
channel.Channel<*Message>
```

Only the pointer is copied.

The pointed-to object remains shared.

The user must manage:

* lifetime;
* mutation synchronization;
* ownership transfer conventions.

Channels do not automatically deep-copy data.

---

## 8.8 Channel destruction

A channel should provide explicit cleanup if it owns heap storage:

```seal
Destroy :: task <
    T type,
>(
    channel *Channel<T>,
)
```

`Destroy` is valid only when:

* no sender is active;
* no receiver is active;
* no task can access the channel;
* owned buffered values require no special user-defined destruction.

Until Seal has deterministic destructors, channel cleanup must be explicit.

If the project accepts process-lifetime allocation for standard-library handles, `Destroy` may be postponed, but reusable libraries should eventually support it.

---

## 8.9 Unbuffered channels

A capacity-zero channel requires rendezvous behavior.

A sender must wait until a receiver is ready, and a receiver must wait until a sender is ready.

Correct implementation requires:

* pending sender state;
* pending receiver state;
* temporary value ownership;
* close wakeups;
* cancellation behavior;
* careful handling of multiple waiters.

Unbuffered channels should be added only after buffered channels are stable.

---

## 8.10 No general `select` initially

Go-style `select` is intentionally excluded from the first design.

A general `select` requires:

* registering one operation with multiple channels;
* atomically choosing one ready case;
* removing losing registrations;
* representing heterogeneous send and receive operations;
* managing runtime continuations;
* supporting default and timeout cases;
* avoiding missed wakeups;
* fair case selection.

Seal also cannot store arbitrary task continuations as runtime values.

The initial alternatives are:

```seal
TrySend
TryReceive
```

Fixed-arity selection helpers may later be considered:

```seal
SelectReceive2
SelectReceive3
```

But these should not define the core API.

---

# 9. `concurrent` package

## 9.1 Purpose

The `concurrent` package provides structured helpers over native threads.

Its API resembles some aspects of Go concurrency, but the initial implementation does not claim goroutine semantics.

A spawned concurrent task may initially map to one native thread.

The implementation may later change to a worker scheduler without changing the public API.

---

## 9.2 Why it is not a goroutine runtime

A true goroutine system normally includes:

* lightweight user-space tasks;
* dynamically growing stacks;
* a runtime scheduler;
* multiplexing many tasks over fewer operating-system threads;
* work stealing;
* task parking;
* runtime-aware channel waits;
* runtime-aware blocking I/O;
* garbage collector coordination.

The first Seal concurrency implementation does not require these mechanisms.

The package offers a Go-like static task model, not a Go runtime clone.

---

## 9.3 Handles

```seal
Handle :: struct {
    // Opaque task execution state.
}
```

A handle may internally wrap:

* a native thread;
* completion state;
* detach state;
* scheduler task state in a future implementation.

Operations:

```seal
Wait :: task(
    handle *Handle,
)
```

```seal
Detach :: task(
    handle *Handle,
)
```

A `Handle` must not be copied after initialization.

---

## 9.4 Spawning no-argument tasks

```seal
Spawn :: task <
    F task[()],
>() Handle
```

Example:

```seal
Work :: task() {
    Compute()
}

handle :=
    concurrent.Spawn<Work>()

concurrent.Wait(&handle)
```

The implementation uses a generated wrapper for `F`.

---

## 9.5 Spawning tasks with arguments

```seal
SpawnWith :: task <
    T type,
    F task[(T)],
>(
    argument T,
) Handle
```

Example:

```seal
Process :: task(value int) {
    fmt.PrintLine(value)
}

handle :=
    concurrent.SpawnWith<
        int,
        Process
    >(100)

concurrent.Wait(&handle)
```

The argument is copied into execution-owned storage.

---

## 9.6 Task groups

A group tracks multiple concurrent operations.

```seal
Group :: struct {
    // Internal wait state.
}
```

Operations:

```seal
Start :: task <
    F task[()],
>(
    group *Group,
)
```

```seal
StartWith :: task <
    T type,
    F task[(T)],
>(
    group *Group,
    argument T,
)
```

```seal
Wait :: task(
    group *Group,
)
```

Example:

```seal
Process :: task(value int) {
    Compute(value)
}

group := concurrent.Group{}

concurrent.StartWith<
    int,
    Process
>(&group, 10)

concurrent.StartWith<
    int,
    Process
>(&group, 20)

concurrent.Wait(&group)
```

The generated thread wrapper must call the group completion operation even when the task returns early.

If Seal has no panic or exception model, ordinary return paths are sufficient.

If failure can bypass normal return, failure cleanup rules must be defined before relying on groups.

---

## 9.7 Futures

A future stores the eventual result of a statically known task.

```seal
Future :: struct <
    T type,
> {
    // Completion state and result storage.
}
```

Spawn without input:

```seal
SpawnResult :: task <
    R type,
    F task[() R],
>() Future<R>
```

Spawn with input:

```seal
SpawnResultWith :: task <
    T type,
    R type,
    F task[(T) R],
>(
    argument T,
) Future<R>
```

Wait for the result:

```seal
WaitResult :: task <
    R type,
>(
    future *Future<R>,
) R
```

Example:

```seal
Square :: task(value int) int {
    return value * value
}

future :=
    concurrent.SpawnResultWith<
        int,
        int,
        Square
    >(12)

result :=
    concurrent.WaitResult<int>(
        &future,
    )
```

The task itself is not stored in `Future<R>`.

Only the result and completion state are stored.

---

## 9.8 Future result storage

A future must guarantee that:

* the result is written before completion becomes visible;
* waiting tasks observe the fully initialized result;
* only one task writes the result;
* multiple waiters may read the result if the result type permits shared reads;
* result destruction occurs only after all users are finished.

Initially, `WaitResult` may move or copy the result out.

The exact rule must be documented.

A simple first rule is:

> `WaitResult` copies the completed result to the caller.

This has the same limitations as other Seal copies.

For large or non-copyable future values, a later API may provide:

```seal
ResultPointer :: task <
    R type,
>(
    future *Future<R>,
) *R
```

That introduces lifetime complexity and should not be in the initial version.

---

## 9.9 Failure propagation

The concurrency package must not invent an exception system.

Result-producing tasks should use Seal’s existing values, enums, or unions.

Example:

```seal
LoadResult :: union {
    Data
    Error
}
```

```seal
Load :: task(path string) LoadResult {
    // ...
}
```

Then:

```seal
future :=
    concurrent.SpawnResultWith<
        string,
        LoadResult,
        Load
    >(path)
```

If Seal later introduces panic-like failures, the behavior of failures inside spawned tasks must be specified separately.

Possible future policies include:

* terminate the process;
* store failure in a future;
* mark a group failed;
* propagate through an explicit result type.

The first design should prefer explicit result types.

---

## 9.10 Cancellation

General cancellation is not included initially.

Cancellation requires a cooperative protocol.

A later package may provide:

```seal
CancelToken :: struct {
    cancelled atomic.AtomicBool
}
```

```seal
Cancel :: task(
    token *CancelToken,
)
```

```seal
IsCancelled :: task(
    token *CancelToken,
) bool
```

Tasks would explicitly receive the token:

```seal
Work :: task(token *concurrent.CancelToken) {
    loop {
        when concurrent.IsCancelled(token) {
            return
        }

        DoPart()
    }
}
```

Cancellation is not forced into every initial concurrency API.

---

# 10. `parallel` package

## 10.1 Purpose

The `parallel` package provides data-parallel operations.

It is distinct from `concurrent`.

Use `concurrent` for independently executing work.

Use `parallel` when one computation is divided over a collection or index range.

---

## 10.2 Parallel index loop

```seal
For :: task <
    F task[(uint)],
>(
    start uint,
    end uint,
)
```

Example:

```seal
ProcessIndex :: task(index uint) {
    Process(values[index])
}

parallel.For<
    ProcessIndex
>(0, values.len)
```

This simple form cannot capture `values`.

Because Seal tasks do not support closures, shared data must be available through:

* a global;
* an explicit context pointer;
* a generic context argument;
* a specialized struct containing runtime data.

A more useful API is:

```seal
ForWith :: task <
    Context type,
    F task[(*Context, uint)],
>(
    context *Context,
    start uint,
    end uint,
)
```

Example:

```seal
WorkContext :: struct {
    values *int
    length uint
}

ProcessIndex :: task(
    context *WorkContext,
    index uint,
) {
    Process(
        context.values[index]
    )
}

context := WorkContext{
    values = values.data,
    length = values.len,
}

parallel.ForWith<
    WorkContext,
    ProcessIndex
>(
    &context,
    0,
    context.length,
)
```

The task remains static while the context is runtime data.

---

## 10.3 Parallel map

Conceptual API:

```seal
Map :: task <
    Input type,
    Output type,
    F task[(Input) Output],
>(
    source []Input,
) []Output
```

Example:

```seal
Square :: task(value int) int {
    return value * value
}

result :=
    parallel.Map<
        int,
        int,
        Square
    >(values)
```

The implementation divides the input range into chunks.

Each output index is written by exactly one worker, avoiding synchronization for distinct elements.

Allocation and slice ownership must follow Seal’s normal collection rules.

---

## 10.4 Parallel map with context

Because tasks cannot capture runtime state:

```seal
MapWith :: task <
    Context type,
    Input type,
    Output type,
    F task[(*Context, Input) Output],
>(
    context *Context,
    source []Input,
) []Output
```

This supports runtime configuration without closures.

---

## 10.5 Parallel reduction

```seal
Reduce :: task <
    T type,
    F task[(T, T) T],
>(
    source []T,
    initial T,
) T
```

Example:

```seal
Add :: task(
    left int,
    right int,
) int {
    return left + right
}

sum :=
    parallel.Reduce<
        int,
        Add
    >(
        values,
        0,
    )
```

Parallel reduction changes grouping order.

Therefore, the result may differ for non-associative operations.

For example, floating-point addition can produce slightly different results because:

```text
(a + b) + c
```

May not equal:

```text
a + (b + c)
```

The documentation must require that the reduction task be associative for deterministic mathematical behavior.

A commutative operation is preferable but not always mandatory if chunk order is defined.

---

## 10.6 Worker pools

A statically specialized worker pool can be represented as:

```seal
Pool :: struct <
    T type,
    Worker task[(T)],
> {
    jobs channel.Channel<T>
    // Worker handles and lifecycle state.
}
```

Creation:

```seal
MakePool :: task <
    T type,
    Worker task[(T)],
>(
    worker_count uint,
    queue_capacity uint,
) Pool<T, Worker>
```

Submission:

```seal
Submit :: task <
    T type,
    Worker task[(T)],
>(
    pool *Pool<T, Worker>,
    job T,
) bool
```

Shutdown:

```seal
Close :: task <
    T type,
    Worker task[(T)],
>(
    pool *Pool<T, Worker>,
)
```

Wait:

```seal
Wait :: task <
    T type,
    Worker task[(T)],
>(
    pool *Pool<T, Worker>,
)
```

Example:

```seal
ProcessJob :: task(job Job) {
    Process(job)
}

pool :=
    parallel.MakePool<
        Job,
        ProcessJob
    >(
        thread.ProcessorCount(),
        256,
    )

parallel.Submit<
    Job,
    ProcessJob
>(&pool, first_job)

parallel.Submit<
    Job,
    ProcessJob
>(&pool, second_job)

parallel.Close(&pool)
parallel.Wait(&pool)
```

`Worker` is a compile-time parameter, not a runtime field.

---

## 10.7 Work partitioning

The first parallel implementation may use static chunking.

For a range:

```text
[start, end)
```

And `N` workers, divide the range into approximately equal contiguous chunks.

Later implementations may add:

* dynamic scheduling;
* guided scheduling;
* work stealing;
* configurable grain size;
* per-thread local queues.

The initial API should avoid exposing the scheduling implementation unless necessary.

---

## 10.8 Vectorization relationship

The `parallel` package provides multithreaded data parallelism.

It does not by itself guarantee SIMD vectorization.

Compiler vectorization and multithreading are separate optimizations:

* SIMD executes multiple data elements in one CPU instruction.
* Multithreading executes work across multiple CPU cores.

A loop may use both.

Example conceptual pipeline:

```text
parallel.For
    -> divides range into chunks
    -> each worker executes a normal loop
    -> C compiler may auto-vectorize the inner loop
```

A future vector package may expose explicit vector operations, but it should remain separate from thread management.

---

# 11. Native platform support

## 11.1 Windows

Possible native mechanisms:

* `CreateThread` or `_beginthreadex`;
* `WaitForSingleObject`;
* `CloseHandle`;
* `GetCurrentThreadId`;
* `SwitchToThread`;
* `GetActiveProcessorCount`;
* `Interlocked*` atomic operations;
* `SRWLOCK`;
* `CONDITION_VARIABLE`;
* `CRITICAL_SECTION`;
* `WakeConditionVariable`;
* `WakeAllConditionVariable`.

Using `_beginthreadex` may be preferable when threads execute C runtime functions.

The implementation must work with supported Seal C compilers, including GCC and TCC where possible.

Compiler and SDK differences must be hidden behind the package’s native C layer.

---

## 11.2 POSIX

Possible native mechanisms:

* `pthread_create`;
* `pthread_join`;
* `pthread_detach`;
* `pthread_self`;
* `sched_yield`;
* `sysconf(_SC_NPROCESSORS_ONLN)`;
* `pthread_mutex_t`;
* `pthread_rwlock_t`;
* `pthread_cond_t`;
* C11 atomics or compiler built-ins.

The build system may need to link with the platform thread library.

---

## 11.3 Cross-platform behavior

Public Seal semantics should remain consistent.

Platform differences may remain in:

* exact thread identifier format;
* scheduling fairness;
* lock implementation;
* processor count reporting;
* lock-free atomic availability;
* native error codes;
* performance characteristics.

Public APIs should not expose native handles by default.

---

# 12. Data races

The concurrency packages do not make ordinary memory access thread-safe.

A data race occurs when:

* two tasks access the same memory concurrently;
* at least one access writes;
* the accesses are not properly synchronized.

Invalid:

```seal
counter := 0

Increment :: task() {
    counter += 1
}
```

Running `Increment` on multiple threads creates a data race.

Correct using a mutex:

```seal
counter := 0
counter_mutex := sync.Mutex{}

Increment :: task() {
    sync.Lock(&counter_mutex)
    counter += 1
    sync.Unlock(&counter_mutex)
}
```

Correct using an atomic:

```seal
counter := atomic.AtomicUint{}

Increment :: task() {
    atomic.Add(
        &counter,
        1,
    )
}
```

The compiler does not initially attempt to prove race freedom.

---

# 13. Memory visibility

Synchronization operations establish memory visibility.

Examples:

* unlocking a mutex publishes prior writes to a later successful locker;
* a release store may publish prior writes;
* an acquire load may observe writes published by a release operation;
* thread completion followed by `Join` makes completed writes visible;
* sending through a channel publishes the sent value before receive returns;
* future completion publishes the result before `WaitResult` returns.

These rules must be implemented consistently even when the native platform APIs differ.

---

# 14. Lifetime and ownership

## 14.1 Shared pointer lifetime

Passing a pointer to a spawned task does not extend the pointed-to object’s lifetime automatically.

Invalid:

```seal
StartWork :: task() {
    local := Data{}

    concurrent.SpawnWith<
        *Data,
        UseData
    >(&local)

    // local may cease to exist before UseData finishes.
}
```

Correct:

```seal
StartWork :: task() {
    local := Data{}

    handle :=
        concurrent.SpawnWith<
            *Data,
            UseData
        >(&local)

    concurrent.Wait(&handle)
}
```

Or allocate data in storage whose lifetime exceeds the concurrent operation.

---

## 14.2 Ownership transfer

The initial APIs use copy semantics for task arguments and channel elements.

The standard library does not automatically enforce exclusive ownership transfer.

Users must define conventions for pointer-containing values.

A future move system may permit APIs such as:

```seal
SendMove
SpawnMove
```

These are not part of the initial design.

---

## 14.3 Opaque object destruction

Objects owning native or heap resources may require explicit destruction:

```seal
thread.Join
thread.Detach
channel.Destroy
concurrent.Wait
parallel.Close
parallel.Wait
```

Until Seal has deterministic destructors, resource lifetime must be explicit.

---

# 15. Minimal primitive policy

The concurrency design should add as few language primitives as possible.

The first implementation should rely on:

* generic type arguments;
* generic compile-time value arguments;
* task-signature constraints;
* ordinary structs;
* pointers;
* arrays and heap allocations;
* native C functions;
* generated specialization wrappers.

The following should not be added solely for concurrency:

* first-class tasks;
* closures;
* implicit captures;
* coroutine syntax;
* a `go` keyword;
* a `select` keyword;
* a built-in channel type;
* built-in mutex syntax;
* automatic actor objects;
* implicit thread-local storage;
* hidden exception propagation.

If a future feature has broad value outside concurrency, it may be considered independently.

---

# 16. Generic constraints used by the packages

## 16.1 Task shape constraints

```seal
Spawn :: task <
    F task[()],
>() Handle
```

```seal
SpawnWith :: task <
    T type,
    F task[(T)],
>(argument T) Handle
```

```seal
SpawnResultWith :: task <
    T type,
    R type,
    F task[(T) R],
>(argument T) Future<R>
```

---

## 16.2 Compile-time value constraints

A statically sized queue or pool may use:

```seal
StaticChannel :: struct <
    T type,
    Capacity uint[Capacity > 0],
> {
    values [Capacity]T
}
```

A worker collection may use:

```seal
StaticPool :: struct <
    T type,
    Worker task[(T)],
    Workers uint[Workers > 0],
> {
}
```

These designs create one specialization for each capacity or worker count.

They should only be used where fixed-size inline storage is valuable.

General channels and pools should prefer runtime capacities to reduce code generation.

---

## 16.3 Structural constraints

Structural constraints may later be useful for optimized parallel operations.

Example:

```seal
LengthOf :: task <
    T type[len uint],
>(value *T) uint {
    return value.len
}
```

However, standard concurrency APIs should not require structural constraints unless they provide a clear benefit.

---

## 16.4 Pure task constraints

Compile-time pure tasks may validate parameters.

Example:

```seal
ValidWorkerCount :: pure task(
    count uint,
) bool {
    return count > 0 &&
        count <= 1024
}
```

```seal
StaticPool :: struct <
    Workers uint[
        ValidWorkerCount(Workers)
    ],
> {
}
```

Runtime worker counts still require runtime validation.

---

# 17. Error handling

The packages should follow Seal’s existing error conventions.

Low-level operations can fail because of:

* native thread creation failure;
* memory allocation failure;
* invalid state;
* unsupported operation;
* resource exhaustion;
* invalid capacity;
* joining a detached thread;
* sending to a closed channel;
* destroying an active object.

Possible API styles include:

```seal
StartResult :: union {
    Thread
    Error
}
```

Or out-parameter/result conventions used by the rest of the standard library.

The concurrency design should not introduce a unique error model.

Error types should classify portable categories while preserving native codes where useful.

---

# 18. Initialization and zero values

Where practical, synchronization types should have useful zero values:

```seal
mutex := sync.Mutex{}
group := sync.WaitGroup{}
counter := atomic.AtomicUint{}
```

This avoids mandatory constructor calls.

However, some native implementations require explicit initialization.

The native bridge may lazily initialize an opaque state on first use.

Lazy initialization must itself be thread-safe.

If reliable zero initialization is difficult, explicit constructors should be used instead:

```seal
mutex := sync.MakeMutex()
channel := channel.Make<int>(16)
```

Channels and worker pools require constructors because they need runtime allocation and capacity.

---

# 19. API evolution strategy

## Phase 1: native foundations

```text
atomic
    AtomicBool
    AtomicInt
    AtomicUint
    AtomicInt64
    AtomicUint64
    Load
    Store
    Swap
    CompareExchange
    Add
    Subtract

thread
    Thread
    Start
    StartWith
    Join
    Detach
    CurrentId
    Yield
    ProcessorCount
```

---

## Phase 2: synchronization

```text
sync
    Mutex
    RwMutex
    Condition
    WaitGroup
    Once
```

---

## Phase 3: typed communication

```text
channel
    Channel<T>
    Make
    Send
    Receive
    TrySend
    TryReceive
    Close
    Destroy
```

Only buffered channels are required initially.

---

## Phase 4: structured concurrency

```text
concurrent
    Handle
    Spawn
    SpawnWith
    Wait
    Detach
    Group
    Future<T>
    SpawnResult
    SpawnResultWith
    WaitResult
```

---

## Phase 5: data parallelism

```text
parallel
    For
    ForWith
    Map
    MapWith
    Reduce
    Pool<T, Worker>
```

---

## Phase 6: optional advanced features

Possible future additions:

```text
concurrent
    CancelToken
    Timeout
    Race
    JoinAll

channel
    Unbuffered channels
    Timed send and receive
    Limited selection helpers

parallel
    Dynamic scheduling
    Work stealing
    Grain-size configuration
    Parallel scan
    Parallel sort

atomic
    Ordered operations
    Atomic pointer support
    Lock-free capability queries

sync
    Semaphore
    Barrier
    Latch
```

These should be added only after the core model is stable.

---

# 20. Non-goals

The initial design does not attempt to provide:

* full Go goroutines;
* transparent asynchronous I/O;
* automatic task migration;
* arbitrary runtime callbacks;
* dynamic function dispatch inside worker queues;
* lock-free implementations for every primitive;
* guaranteed fairness;
* automatic deadlock detection;
* compile-time race detection;
* distributed concurrency;
* actor isolation;
* implicit cancellation;
* preemptive task interruption;
* heterogeneous Go-style `select`;
* automatic SIMD generation.

These may be explored later, but they are not required for useful multithreading.

---

# 21. Example architecture

A typical program may combine the packages as follows:

```seal
Job :: struct {
    value int
}

Result :: struct {
    value int
}

Process :: task(job Job) Result {
    return Result{
        value = job.value * job.value,
    }
}
```

Using futures:

```seal
first :=
    concurrent.SpawnResultWith<
        Job,
        Result,
        Process
    >(
        Job{
            value = 10,
        },
    )

second :=
    concurrent.SpawnResultWith<
        Job,
        Result,
        Process
    >(
        Job{
            value = 20,
        },
    )

first_result :=
    concurrent.WaitResult<Result>(
        &first,
    )

second_result :=
    concurrent.WaitResult<Result>(
        &second,
    )
```

Using a channel:

```seal
results :=
    channel.Make<Result>(16)
```

Using a worker pool:

```seal
HandleJob :: task(job Job) {
    result :=
        Process(job)

    channel.Send<Result>(
        &results,
        result,
    )
}

pool :=
    parallel.MakePool<
        Job,
        HandleJob
    >(
        thread.ProcessorCount(),
        64,
    )
```

The worker task is static.

The jobs, results, channels, and synchronization state are runtime data.

---

# 22. Final design principles

The Seal multithreading design follows these principles:

1. **Use packages before language primitives.**

2. **Keep tasks compile-time-only.**

3. **Use task-signature constraints to validate callable shape.**

4. **Generate native wrappers for concrete task specializations.**

5. **Store arguments, results, and state at runtime, but never task values.**

6. **Keep low-level native threads separate from structured concurrency.**

7. **Keep concurrency separate from data parallelism.**

8. **Use explicit non-generic atomic types.**

9. **Prefer buffered channels before unbuffered channels.**

10. **Do not implement general `select` until the runtime model supports it cleanly.**

11. **Do not claim goroutine semantics while using native threads.**

12. **Allow the implementation of `concurrent` to evolve from native threads to a scheduler without changing user code.**

13. **Require explicit synchronization for shared mutable state.**

14. **Document copy, lifetime, and ownership behavior precisely.**

15. **Avoid adding primitives that only solve one standard-library implementation problem.**

The central architectural rule remains:

> Seal concurrency uses statically known behavior and explicitly represented runtime data.

This gives Seal useful multithreading, typed communication, structured execution, and data parallelism while preserving the language’s current model and avoiding first-class tasks or a large mandatory runtime.
