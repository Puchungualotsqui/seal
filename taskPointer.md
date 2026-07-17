# Native Task ABI and Task Pointers

Seal supports exposing task addresses to native code without making tasks first-class runtime values.

## `@native_abi(modelDefinedByCompiler)`

```seal
@native_abi(ThreadEntry)
Entry :: task(context rawptr) rawptr {
    // ...
}
```

`@native_abi(...)` instructs the compiler to emit a task using a compiler-defined native ABI model.

The model determines details such as:

* parameter and return representation;
* C calling convention;
* generated C declaration;
* platform-specific ABI differences.

For example, `ThreadEntry` may map to the appropriate thread-entry ABI on Windows and POSIX.

The checker must verify that:

* the ABI model exists;
* the task signature matches the model;
* the model is valid for the current target;
* the annotation is not applied to an unsupported declaration.

Invalid ABI models or incompatible task signatures are compile-time errors.

`@native_abi(...)` does not generate a wrapper or trampoline. It changes how the annotated task itself is emitted.

---

## `@task_pointer(task)`

```seal
pointer :=
    @task_pointer(Entry)
```

`@task_pointer(...)` returns the native address of a statically resolved task as a `rawptr`.

The operand must resolve to exactly one concrete task. For generic or overloaded tasks, all compile-time arguments and overload resolution must already be determined.

```seal
pointer :=
    @task_pointer(
        WorkerEntry<Job, ProcessJob>
    )
```

The compiler must ensure that the referenced task is emitted even when it has no ordinary Seal caller.

The resulting `rawptr` may be:

* assigned to variables;
* stored in structs;
* passed to tasks;
* returned from tasks;
* compared;
* passed to native C code.

It is still only an opaque address in Seal.

Seal code cannot call it:

```seal
pointer :=
    @task_pointer(Entry)

pointer() // Invalid: rawptr is not callable.
```

`@task_pointer(...)` does not change the task ABI. When native code intends to call the address, the task should normally be annotated with the appropriate `@native_abi(...)` model.

```seal
@native_abi(ThreadEntry)
Entry :: task(context rawptr) rawptr {
    return null
}

nativeStart(
    @task_pointer(Entry),
    context,
)
```

Passing a task pointer to native code using an incompatible ABI is unsafe.
