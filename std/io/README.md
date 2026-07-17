# io

The `io` package provides byte-oriented readers, writers, buffers, and transfer helpers.

All operations use `ByteSpan`, a borrowed view over contiguous memory.

## `ByteSpan`

`ByteSpan` stores a pointer and byte length without owning the memory.

```seal
span := io.NewByteSpan(
    data,
    length,
)
```

Useful operations:

* `ByteSpanFrom`
* `ByteSpanPrefix`
* `ByteSpanRange`
* `CopyByteSpan`
* `ByteSpanIsValid`
* `EmptyByteSpan`

Returned sub-spans borrow the original storage.

## Stream interfaces

### `Reader`

Reads bytes into a destination span.

```seal
count, status := Read(
    reader,
    destination,
)
```

### `Writer`

Writes bytes from a source span.

```seal
count, status := Write(
    writer,
    source,
)
```

### `Flusher`

Commits buffered output.

```seal
success := Flush(flusher)
```

### `Closer`

Releases a stream or resource.

```seal
success := Close(closer)
```

## Status values

`ReadStatus`:

* `Data`
* `End`
* `Failed`

`WriteStatus`:

* `Data`
* `Failed`

Reader and writer implementations may transfer fewer bytes than requested.

## `BytesReader`

Reads sequentially from an existing `ByteSpan`.

```seal
readerValue :=
    io.NewBytesReader(source)

reader := cast<io.Reader>(
    &readerValue,
)
```

`BytesReader` does not own the source memory.

Use `NewChunkedBytesReader` to limit the number of bytes returned by each read.

## `FixedBufferWriter`

Writes sequentially into caller-owned memory.

```seal
writerValue :=
    io.NewFixedBufferWriter(
        destination,
    )

writer := cast<io.Writer>(
    &writerValue,
)
```

Use:

* `FixedBufferWriterWritten`
* `FixedBufferWriterRemaining`
* `ResetFixedBufferWriter`

It does not own or release the destination.

## `BufferWriter`

A growable in-memory writer that owns its byte allocation.

```seal
writerValue :=
    io.NewBufferWriter(
        allocator,
        deallocator,
    )

writer := cast<io.Writer>(
    &writerValue,
)

bytes := io.BufferWriterBytes(
    &writerValue,
)

Close(
    cast<io.Closer>(
        &writerValue,
    ),
)
```

The span returned by `BufferWriterBytes` becomes invalid when the writer grows or closes.

Use `ResetBufferWriter` to clear contents while retaining capacity.

## `DiscardWriter`

Accepts and discards every valid byte sequence.

```seal
writerValue :=
    io.NewDiscardWriter()

writer := cast<io.Writer>(
    &writerValue,
)
```

This is useful when output must be consumed without being stored.

## Transfer helpers

### `ReadExact`

Attempts to completely fill a destination.

```seal
count, status :=
    io.ReadExact(
        reader,
        destination,
    )
```

Possible results include `Complete`, `End`, `Failed`, `NoProgress`, and `InvalidBuffer`.

### `WriteAll`

Attempts to write every source byte.

```seal
count, status :=
    io.WriteAll(
        writer,
        source,
    )
```

### `CopyWithBuffer`

Copies from a reader to a writer using caller-provided scratch memory.

```seal
count, status :=
    io.CopyWithBuffer(
        reader,
        writer,
        buffer,
    )
```

The buffer must be valid and non-empty.

## Ownership and safety

`ByteSpan`, `BytesReader`, and `FixedBufferWriter` borrow memory.

`BufferWriter` owns its allocation and should be closed exactly once, although repeated closes are accepted.

The caller must ensure that borrowed storage remains valid and that aliased memory obeys each operation's requirements.
