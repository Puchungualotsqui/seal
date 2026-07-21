# concurrent

The `concurrent` package provides higher-level concurrency utilities built on top of `thread` and `sync`.

It currently includes:

* A bounded blocking queue
* A fixed-size worker pool

These types own synchronization objects, native allocations, and thread handles. They must not be copied after initialization.

## Error handling

Most operations return an `Error`.

```seal
Error :: enum {
    None
    AllocationFailed
    Closed
    Empty
    Full
    InvalidState
    SyncFailure
    ThreadFailure
    NotDrained
}
```

* `ErrorIsNone(error Error) bool` reports whether an operation succeeded.
* `ErrorString(error Error) string` returns a readable error description.

Important queue states are reported as regular errors:

* `Closed` means no more values may be submitted.
* `Empty` means a non-blocking pop found no value.
* `Full` means a non-blocking push found no capacity.
* `NotDrained` means destruction was attempted before the queue was closed and emptied.

## BlockingQueue

`BlockingQueue<T>` is a bounded first-in, first-out queue.

```seal
BlockingQueue :: struct<T type> {
    Head *_QueueNode<T>
    Tail *_QueueNode<T>

    Capacity uint
    Count uint

    Closed bool
    Initialized bool

    Mutex sync.Mutex
    NotEmpty sync.Condition
    NotFull sync.Condition
}
```

The queue stores copies of submitted values in individually allocated nodes.

A queue must not be copied after initialization.

### Creation

* `TryNewBlockingQueue<T>(capacity uint) (BlockingQueue<T>, Error)` creates an empty queue.
* `NewBlockingQueue<T>(capacity uint) BlockingQueue<T>` creates one or panics.
* `QueueIsValid<T>(queue *BlockingQueue<T>) bool` reports whether the queue owns all required resources.

The capacity must be greater than zero.

### Push operations

`Push<T>(queue *BlockingQueue<T>, value T) Error` waits until space is available and appends the value.

It returns `Closed` when the queue is closed before the value can be inserted.

`TryPush<T>(queue *BlockingQueue<T>, value T) (bool, Error)` never waits.

Its results are:

```text
true,  None     value was inserted
false, Full     queue has no available capacity
false, Closed   queue no longer accepts values
false, error    operation failed
```

A successful push transfers ownership of the stored value copy to the queue.

### Pop operations

`Pop<T>(queue *BlockingQueue<T>) (T, Error)` waits until a value is available.

Its results are:

```text
value, None     one value was removed
zero,  Closed   queue is closed and drained
zero,  error    operation failed
```

Closing a queue does not discard values already stored in it. Consumers may continue popping until it is drained.

`TryPop<T>(queue *BlockingQueue<T>) (T, bool, Error)` never waits.

Its results are:

```text
value, true,  None     one value was removed
zero,  false, Empty    queue is currently empty
zero,  false, Closed   queue is closed and drained
zero,  false, error    operation failed
```

The returned zero value must only be interpreted when the success flag is `true`.

### Closing

`CloseQueue<T>(queue *BlockingQueue<T>) Error` prevents future pushes and wakes blocked producers and consumers.

Closing is idempotent. Calling it more than once succeeds.

Values already in the queue remain available.

### Inspection

`QueueLength<T>(queue *BlockingQueue<T>) (uint, Error)` returns the current number of queued values.

The result is only a snapshot. Other threads may change the queue immediately after the task returns.

### Destruction

`DestroyQueue<T>(queue *BlockingQueue<T>) Error` releases the queue’s synchronization resources.

The queue must first be:

* Closed
* Fully drained
* No longer accessed by any thread

Otherwise, destruction returns `NotDrained`.

Successful destruction marks the queue as uninitialized.

## WorkerPool

`WorkerPool<T>` owns a bounded job queue and a fixed number of worker threads.

```seal
WorkerPool :: struct<T type> {
    Queue BlockingQueue<T>
    Threads *_PoolThreadNode

    ThreadCount uint
    StartedThreadCount uint

    State PoolState
}
```

Its lifecycle is represented by:

```seal
PoolState :: enum {
    Empty
    Running
    Closing
    Joined
}
```

* `Empty` means the pool is not active.
* `Running` means jobs may be submitted.
* `Closing` means submissions are closed while workers drain queued jobs.
* `Joined` means all workers and resources have been released.

The pool must remain at the same memory address after initialization because worker threads borrow a pointer to its internal queue.

Do not return, move, or copy an initialized pool.

### Initialization

`InitWorkerPool<T, Worker>(pool *WorkerPool<T>, threadCount uint, queueCapacity uint) Error` initializes a pool in its final memory location.

The worker must have this shape:

```seal
Worker :: task(
    value T,
)
```

`Worker` is selected at compile time and is not stored in the pool.

Both `threadCount` and `queueCapacity` must be greater than zero.

If initialization fails after some worker threads have started, the package attempts to close the queue, join the started workers, and release all allocated resources.

### Submitting jobs

`Submit<T>(pool *WorkerPool<T>, value T) Error` waits for queue capacity and submits one job.

It may block while the queue is full.

`TrySubmit<T>(pool *WorkerPool<T>, value T) (bool, Error)` submits without waiting.

Its results follow the same convention as `TryPush`:

```text
true,  None
false, Full
false, Closed
false, error
```

Jobs may only be submitted while the pool is in the `Running` state.

### Closing

`CloseWorkerPool<T>(pool *WorkerPool<T>) Error` prevents new submissions.

Workers continue processing every value already in the queue.

Calling it again while the pool is `Closing` or `Joined` succeeds without performing additional work.

### Joining

`JoinWorkerPool<T>(pool *WorkerPool<T>) Error` performs the complete pool shutdown:

1. Closes the queue if necessary.
2. Lets workers process all submitted jobs.
3. Joins every worker thread.
4. Destroys the queue.
5. Releases all worker metadata.
6. Changes the pool state to `Joined`.

A failed thread join leaves the failed thread and all remaining threads owned by the pool so the operation may be retried.

## Example

This example creates a worker pool, submits several integer jobs, and waits for all work to finish.

```seal
PrintJob :: task(
    value int,
) {
    fmt.Println(
        "processing: %v",
        value,
    )
}

Main :: task() {
    pool: concurrent.WorkerPool<int>

    initError :=
        concurrent.InitWorkerPool<
            int,
            PrintJob
        >(
            &pool,
            4,
            16,
        )

    if !concurrent.ErrorIsNone(
        initError,
    ) {
        panic(
            concurrent.ErrorString(
                initError,
            ),
        )
    }

    value := 0

    for value < 100 {
        submitError :=
            concurrent.Submit<int>(
                &pool,
                value,
            )

        if !concurrent.ErrorIsNone(
            submitError,
        ) {
            panic(
                concurrent.ErrorString(
                    submitError,
                ),
            )
        }

        value += 1
    }

    joinError :=
        concurrent.JoinWorkerPool<int>(
            &pool,
        )

    if !concurrent.ErrorIsNone(
        joinError,
    ) {
        panic(
            concurrent.ErrorString(
                joinError,
            ),
        )
    }
}
```

`JoinWorkerPool` closes the pool automatically, so a separate call to `CloseWorkerPool` is optional.

## Ownership and lifetime rules

* Do not copy an initialized `BlockingQueue`.
* Do not copy or move an initialized `WorkerPool`.
* Initialize a worker pool only in its final memory location.
* Do not access a queue after successful destruction.
* Do not submit jobs after a pool begins closing.
* Close and drain a standalone queue before destroying it.
* Join every worker pool before its storage goes out of scope.
* Ensure job values remain valid according to their own ownership rules.
* If jobs contain pointers, the referenced data must remain alive until all submitted jobs finish.
* Queue and pool operations are thread-safe, but the values stored inside them are not automatically made thread-safe.
