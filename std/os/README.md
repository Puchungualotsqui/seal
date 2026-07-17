# os

The `os` package provides portable access to process information, environment variables, files, directories, standard streams, and common filesystem operations.

Expected operating-system failures are returned as `os.Error`. They do not normally cause a panic.

## Package overview

The package includes:

* process arguments and process information;
* environment-variable access;
* working-directory management;
* file opening, reading, writing, flushing, seeking, and closing;
* standard input, output, and error streams;
* whole-file reading and writing;
* filesystem metadata;
* directory creation, removal, renaming, and enumeration.

Paths and textual results use UTF-8.

## Allocators

Functions that return owned strings, byte buffers, or directory entries require allocator interfaces from the `mem` package.

```seal
allocatorValue := mem.NewCAllocator()

allocator := cast<mem.Allocator>(
    &allocatorValue,
)

deallocator := cast<mem.Deallocator>(
    &allocatorValue,
)
```

The allocator and deallocator supplied to the same owning value must refer to compatible allocator state.

## Errors

Operating-system operations return `os.Error`.

```seal
file, err := os.OpenRead(
    "settings.txt",
)

if !os.ErrorIsNone(err) {
    fmt.Println(
        "open failed: %v",
        os.ErrorString(err),
    )
}
```

`Error.Kind` provides a portable classification:

* `None`
* `NotFound`
* `AlreadyExists`
* `PermissionDenied`
* `InvalidArgument`
* `InvalidPath`
* `NotDirectory`
* `IsDirectory`
* `DirectoryNotEmpty`
* `Interrupted`
* `OutOfMemory`
* `InputOutput`
* `Overflow`
* `Unsupported`
* `Unknown`

`Error.NativeCode` preserves the platform-specific error code when one is available.

```seal
if err.Kind == .NotFound {
    fmt.Println("the file does not exist")
}
```

Use `NoError` to construct a successful error value and `NewError` to construct an explicit error.

## Process arguments

`ArgumentCount` returns the number of command-line arguments.

The executable path normally occupies index zero.

```seal
count, err :=
    os.ArgumentCount()

if os.ErrorIsNone(err) {
    fmt.Println(
        "argument count: %v",
        count,
    )
}
```

`Argument` returns an owned copy of one argument.

```seal
argument, found, err :=
    os.Argument(
        allocator,
        1,
    )

if os.ErrorIsNone(err) &&
    found {
    fmt.Println(
        "first argument: %v",
        strings.View(&argument),
    )

    strings.Destroy(
        deallocator,
        &argument,
    )
}
```

`found` is false when the requested index is outside the argument list.

## Environment variables

### Reading

`GetEnv` returns an owned copy of an environment variable.

```seal
value, found, err :=
    os.GetEnv(
        allocator,
        "HOME",
    )

if os.ErrorIsNone(err) &&
    found {
    fmt.Println(
        "HOME: %v",
        strings.View(&value),
    )

    strings.Destroy(
        deallocator,
        &value,
    )
}
```

The `found` result distinguishes a missing variable from an existing variable with an empty value.

### Writing

```seal
err := os.SetEnv(
    "SEAL_MODE",
    "development",
)
```

### Removing

```seal
err := os.UnsetEnv(
    "SEAL_MODE",
)
```

Removing a variable that does not exist succeeds.

## Working directory

`GetWorkingDirectory` returns an owned path.

```seal
directory, err :=
    os.GetWorkingDirectory(
        allocator,
    )

if os.ErrorIsNone(err) {
    fmt.Println(
        "working directory: %v",
        strings.View(&directory),
    )

    strings.Destroy(
        deallocator,
        &directory,
    )
}
```

Use `SetWorkingDirectory` to change it:

```seal
err := os.SetWorkingDirectory(
    "build",
)
```

Changing the working directory affects subsequent relative-path operations for the entire process.

## Files

`File` wraps a native file stream.

An owning `File` performs manual resource management. Copying it creates a shallow ownership copy and must be avoided.

A file may implement:

* `io.Reader`
* `io.Writer`
* `io.Flusher`
* `io.Closer`

### Opening for reading

```seal
file, err := os.OpenRead(
    "input.bin",
)

if os.ErrorIsNone(err) {
    defer {
        os.CloseFile(&file)
    }

    reader := cast<io.Reader>(
        &file,
    )
}
```

### Creating or truncating

```seal
file, err := os.CreateFile(
    "output.bin",
)
```

`CreateFile` creates the file when necessary and truncates any existing contents.

### Exclusive creation

```seal
file, err :=
    os.CreateFileExclusive(
        "lockfile",
    )
```

This fails with `AlreadyExists` when the path already exists.

### Appending

```seal
file, err := os.OpenAppend(
    "application.log",
)
```

Writes are added at the end of the file.

### Custom opening options

`OpenFile` accepts `OpenOptions`.

```seal
file, err := os.OpenFile(
    "data.bin",
    os.OpenOptions{
        Read = true,
        Write = true,
        Append = false,
        Create = true,
        Truncate = false,
        Exclusive = false,
    },
)
```

At least one of `Read` or `Write` must be enabled.

`Append`, `Create`, `Truncate`, and `Exclusive` require write access. `Exclusive` also requires `Create`.

Use `OpenOptionsIsValid` to validate options before opening.

## Reading and writing files

A `File` can be passed through the `io` interfaces.

```seal
file, err := os.OpenRead(
    "input.bin",
)

if os.ErrorIsNone(err) {
    bufferData := mem.AllocWith(
        allocator,
        4096,
    )

    buffer := io.ByteSpan{
        Data = bufferData,
        Length = 4096,
    }

    count, status := Read(
        cast<io.Reader>(&file),
        buffer,
    )

    mem.FreeWith(
        deallocator,
        bufferData,
    )

    os.CloseFile(&file)
}
```

Writing:

```seal
file, err := os.CreateFile(
    "output.txt",
)

if os.ErrorIsNone(err) {
    text := "hello from Seal"

    source := io.ByteSpan{
        Data = cast<rawptr>(text),
        Length = size(text),
    }

    count, status := io.WriteAll(
        cast<io.Writer>(&file),
        source,
    )

    os.CloseFile(&file)
}
```

`Read` and `Write` may transfer fewer bytes than requested. Use `io.ReadExact` and `io.WriteAll` when complete transfer is required.

## Flushing and closing

`FlushFile` commits buffered output associated with a file.

```seal
err := os.FlushFile(
    &file,
)
```

`CloseFile` releases the native file handle.

```seal
err := os.CloseFile(
    &file,
)
```

Calling `CloseFile` more than once on the same value succeeds.

After closing, the file must not be used for reading, writing, flushing, or seeking.

## Seeking

`FileSeek` changes the current byte position.

```seal
position, err := os.FileSeek(
    &file,
    0,
    .End,
)
```

Available origins are:

* `Start`
* `Current`
* `End`

`FilePosition` returns the current absolute position:

```seal
position, err :=
    os.FilePosition(
        &file,
    )
```

Seeking is not supported by every kind of stream.

## Standard streams

The package exposes borrowed wrappers around the process standard streams.

```seal
input := os.StandardInput()
output := os.StandardOutput()
errors := os.StandardError()
```

They can be used through the `io` interfaces:

```seal
text := "hello\n"

source := io.ByteSpan{
    Data = cast<rawptr>(text),
    Length = size(text),
}

io.WriteAll(
    cast<io.Writer>(&output),
    source,
)
```

Closing one of these wrappers does not close the process-global standard stream. It only invalidates that wrapper value.

## Whole-file operations

### Reading

`ReadFile` reads an entire file into `OwnedBytes`.

```seal
contents, err := os.ReadFile(
    allocator,
    deallocator,
    "input.bin",
)

if os.ErrorIsNone(err) {
    span := os.OwnedBytesSpan(
        &contents,
    )

    fmt.Println(
        "read %v bytes",
        span.Length,
    )

    os.DestroyOwnedBytes(
        &contents,
    )
}
```

`OwnedBytes` owns its allocation and must not be shallow-copied and independently destroyed.

### Writing

```seal
text := "complete contents"

source := io.ByteSpan{
    Data = cast<rawptr>(text),
    Length = size(text),
}

err := os.WriteFile(
    "output.txt",
    source,
)
```

`WriteFile` creates or truncates the destination.

### Appending

```seal
text := "new log entry\n"

source := io.ByteSpan{
    Data = cast<rawptr>(text),
    Length = size(text),
}

err := os.AppendFile(
    "application.log",
    source,
)
```

`AppendFile` creates the file when necessary.

## File metadata

`Stat` returns metadata and follows symbolic links.

```seal
info, err := os.Stat(
    "data.bin",
)

if os.ErrorIsNone(err) {
    fmt.Println(
        "size: %v",
        info.Size,
    )
}
```

`Lstat` describes the final symbolic link itself instead of following it.

```seal
info, err := os.Lstat(
    "current",
)
```

`FileInfo` contains:

* `Type`
* `Size`
* `ModifiedUnixSeconds`
* `ReadOnly`

`FileType` values are:

* `Regular`
* `Directory`
* `Symlink`
* `Other`

The modification time is expressed as seconds since the Unix epoch.

## Existence checks

`Exists` reports whether a path can be successfully inspected.

```seal
if os.Exists("settings.txt") {
    fmt.Println("settings found")
}
```

It returns false for missing paths, permission failures, and other metadata errors. Use `Stat` when the reason matters.

`PathIsDirectory` reports whether a path exists and is a directory.

```seal
if os.PathIsDirectory("assets") {
    fmt.Println("assets is a directory")
}
```

## Directory creation

`CreateDirectory` creates one directory.

```seal
err := os.CreateDirectory(
    "build",
)
```

It fails when a parent directory is missing.

`CreateDirectories` creates missing parent directories as well.

```seal
err := os.CreateDirectories(
    "build/generated/c",
)
```

Existing directories are accepted by `CreateDirectories`.

## Removing and renaming entries

Remove a file:

```seal
err := os.RemoveFile(
    "temporary.bin",
)
```

Remove an empty directory:

```seal
err := os.RemoveDirectory(
    "empty-directory",
)
```

Rename or move an entry:

```seal
err := os.Rename(
    "old-name.txt",
    "new-name.txt",
)
```

Replacement behavior for an existing destination follows the host operating system.

The package does not currently provide recursive removal.

## Reading directories

`ReadDirectory` reads the immediate entries of a directory.

```seal
entries, err := os.ReadDirectory(
    allocator,
    deallocator,
    ".",
)

if os.ErrorIsNone(err) {
    count := os.DirectoryEntriesLength(
        &entries,
    )

    index: int = 0

    for index < cast<int>(count) {
        entry, found :=
            os.DirectoryEntriesGet(
                &entries,
                index,
            )

        if found {
            fmt.Println(
                "%v",
                strings.View(
                    &entry.Name,
                ),
            )
        }

        index += 1
    }

    os.DestroyDirectoryEntries(
        &entries,
    )
}
```

The special entries `.` and `..` are omitted.

`DirectoryEntries` owns:

* the dynamic-array slot allocation;
* every `DirectoryEntry.Name` allocation.

The value returned by `DirectoryEntriesGet` is a shallow borrowed copy. Do not destroy its `Name` separately.

Release the complete result with `DestroyDirectoryEntries`.

Directory iteration order is platform-dependent and is not guaranteed to be sorted.

## Process information

`ProcessID` returns the current process identifier.

```seal
identifier := os.ProcessID()
```

`ExecutablePath` returns an owned path for the running executable.

```seal
path, err := os.ExecutablePath(
    allocator,
)

if os.ErrorIsNone(err) {
    fmt.Println(
        "executable: %v",
        strings.View(&path),
    )

    strings.Destroy(
        deallocator,
        &path,
    )
}
```

The returned path representation follows the host operating system.

## Exiting the process

`Exit` terminates the current process immediately.

```seal
os.Exit(1)
```

Normal task returns and deferred actions do not run after `Exit`.

Use normal returns when cleanup must execute.

## Ownership summary

These values own resources and must not be shallow-copied and independently destroyed:

* `File`, when returned by file-opening functions;
* `OwnedBytes`;
* `DirectoryEntries`;
* `strings.OwnedString` values returned by this package.

Release them with:

* `CloseFile`
* `DestroyOwnedBytes`
* `DestroyDirectoryEntries`
* `strings.Destroy`

Standard-stream `File` wrappers are borrowed and do not own the process-global stream.

## Path behavior

The package accepts native filesystem paths.

Examples:

```text
assets/images
```

```text
C:\projects\seal
```

Path separators, drive prefixes, symbolic links, case sensitivity, and replacement behavior follow the host operating system.

The package currently performs operating-system operations but does not provide lexical path manipulation such as joining, cleaning, extracting extensions, or finding base names. Those operations belong in a separate `filepath` package.

## Current limitations

The initial package does not include:

* recursive directory removal;
* file permission modification;
* symbolic-link creation;
* hard links;
* temporary files and directories;
* memory mapping;
* file locking;
* subprocess creation;
* signals;
* asynchronous I/O;
* filesystem watching.

These features can be added without changing the current error and ownership model.
