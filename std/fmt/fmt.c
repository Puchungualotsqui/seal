#include <stdio.h>
#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include <inttypes.h>

/*
This must remain ABI-compatible with Seal's generated string
representation:

    typedef struct sealString {
        const uint8_t *data;
        uintptr_t len;
    } sealString;

The field `len` is the UTF-8 byte count.
*/
typedef struct sealString {
    const uint8_t *data;
    uintptr_t len;
} sealString;

void seal_fmt_write_cstring(const char *value) {
    fputs(value, stdout);
}

void seal_fmt_write_string(sealString value) {
    if (value.len == 0) {
        return;
    }

    fwrite(
        value.data,
        1,
        (size_t)value.len,
        stdout
    );
}

void seal_fmt_write_char(uint32_t value) {
    if (value <= 0x7F) {
        fputc(
            (int)value,
            stdout
        );

        return;
    }

    if (value <= 0x7FF) {
        fputc(
            0xC0 |
                ((value >> 6) & 0x1F),
            stdout
        );

        fputc(
            0x80 |
                (value & 0x3F),
            stdout
        );

        return;
    }

    if (value <= 0xFFFF) {
        fputc(
            0xE0 |
                ((value >> 12) & 0x0F),
            stdout
        );

        fputc(
            0x80 |
                ((value >> 6) & 0x3F),
            stdout
        );

        fputc(
            0x80 |
                (value & 0x3F),
            stdout
        );

        return;
    }

    fputc(
        0xF0 |
            ((value >> 18) & 0x07),
        stdout
    );

    fputc(
        0x80 |
            ((value >> 12) & 0x3F),
        stdout
    );

    fputc(
        0x80 |
            ((value >> 6) & 0x3F),
        stdout
    );

    fputc(
        0x80 |
            (value & 0x3F),
        stdout
    );
}

void seal_fmt_write_int(intptr_t value) {
    printf(
        "%" PRIdPTR,
        value
    );
}

void seal_fmt_write_uint(uintptr_t value) {
    printf(
        "%" PRIuPTR,
        value
    );
}

void seal_fmt_write_f32(float value) {
    printf(
        "%g",
        (double)value
    );
}

void seal_fmt_write_f64(double value) {
    printf(
        "%g",
        value
    );
}

void seal_fmt_write_bool(bool value) {
    fputs(
        value
            ? "true"
            : "false",
        stdout
    );
}

void seal_fmt_write_rawptr(void *value) {
    printf(
        "%p",
        value
    );
}
