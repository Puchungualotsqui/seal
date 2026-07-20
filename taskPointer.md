# Seal Update: Foreign Task Pointers and C ABI Declarations

Seal will support passing task addresses to native C code without introducing first-class task values, callable 
function pointers, compiler-generated trampolines, or compiler-defined calling conventions.

## Task pointers

The new `@task_pointer` directive returns the address of a concrete task as an opaque `rawptr`.

```seal
entry: rawptr = @task_pointer(WorkerEntry)
```

Generic tasks must include all compile-time arguments:

```seal
entry := @task_pointer(WorkerEntry<int, Process>)
```

Seal code cannot invoke this pointer. It is intended only for passing task addresses to foreign C code.

## Foreign task declarations

Libraries can define how a task is emitted as a C function:

```seal
ThreadEntryABI :: @foreign_task(
    declaration SEAL_THREAD_RESULT SEAL_THREAD_CALL {name}(void *{arg0_name}),
    address     SEAL_THREAD_ADDRESS({name}),
)
```

A task can then use that declaration:

```seal
@foreign(ThreadEntryABI)
WorkerEntry :: task(context: rawptr) ThreadResult {
    return thread_success
}
```

The `declaration` field controls the generated C function declaration, while `address` controls how 
`@task_pointer` produces its address.

## Foreign types and values

C-backed types and constants must be declared explicitly in Seal:

```seal
ThreadResult :: @foreign_type(SEAL_THREAD_RESULT)

thread_success :: @foreign_value(
    ThreadResult,
    SEAL_THREAD_SUCCESS,
)
```

The corresponding identifiers are defined by the package's C header:

```c
#define SEAL_THREAD_RESULT unsigned
#define SEAL_THREAD_CALL
#define SEAL_THREAD_SUCCESS 0
#define SEAL_THREAD_ADDRESS(name) ((void *)(name))
```

None of these values are compiler magic.

## Compiler responsibility

The Seal compiler only:

* resolves the referenced task
* substitutes placeholders such as `{name}` and `{arg0_name}`
* emits the provided C token sequences
* returns the resulting address as `rawptr`

Seal does not verify whether the foreign declaration matches the target C ABI. ABI correctness remains the 
responsibility of the library author and the C compiler.

