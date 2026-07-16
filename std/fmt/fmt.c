#include <stdio.h>
#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include <inttypes.h>

#ifdef _WIN32
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#endif

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

/*
Configure the Windows console to interpret stdout bytes as UTF-8.

Redirected output remains ordinary UTF-8 byte output. On non-Windows
platforms no preparation is required.
*/
static void seal_fmt_prepare_stdout(void) {
#ifdef _WIN32
    static bool prepared = false;

    if (prepared) {
        return;
    }

    prepared = true;

    /*
    CP_UTF8 is 65001.

    Use the numeric value to avoid depending on how a particular Windows
    SDK exposes the constant.
    */
    SetConsoleOutputCP(65001);
#endif
}

void seal_fmt_write_cstring(const char *value) {
    seal_fmt_prepare_stdout();

    fputs(
        value,
        stdout
    );
}

void seal_fmt_write_string(sealString value) {
    seal_fmt_prepare_stdout();

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
    seal_fmt_prepare_stdout();

    /*
    A Seal char should already contain a valid Unicode scalar. Replace an
    invalid value defensively rather than emitting invalid UTF-8.
    */
    if (value > 0x10FFFF ||
        (value >= 0xD800 &&
         value <= 0xDFFF)) {
        value = 0xFFFD;
    }

    unsigned char encoded[4];
    size_t encoded_length = 0;

    if (value <= 0x7F) {
        encoded[0] =
            (unsigned char)value;

        encoded_length = 1;
    } else if (value <= 0x7FF) {
        encoded[0] =
            (unsigned char)(
                0xC0 |
                ((value >> 6) & 0x1F)
            );

        encoded[1] =
            (unsigned char)(
                0x80 |
                (value & 0x3F)
            );

        encoded_length = 2;
    } else if (value <= 0xFFFF) {
        encoded[0] =
            (unsigned char)(
                0xE0 |
                ((value >> 12) & 0x0F)
            );

        encoded[1] =
            (unsigned char)(
                0x80 |
                ((value >> 6) & 0x3F)
            );

        encoded[2] =
            (unsigned char)(
                0x80 |
                (value & 0x3F)
            );

        encoded_length = 3;
    } else {
        encoded[0] =
            (unsigned char)(
                0xF0 |
                ((value >> 18) & 0x07)
            );

        encoded[1] =
            (unsigned char)(
                0x80 |
                ((value >> 12) & 0x3F)
            );

        encoded[2] =
            (unsigned char)(
                0x80 |
                ((value >> 6) & 0x3F)
            );

        encoded[3] =
            (unsigned char)(
                0x80 |
                (value & 0x3F)
            );

        encoded_length = 4;
    }

    fwrite(
        encoded,
        1,
        encoded_length,
        stdout
    );
}

void seal_fmt_write_int(intptr_t value) {
    seal_fmt_prepare_stdout();

    printf(
        "%" PRIdPTR,
        value
    );
}

void seal_fmt_write_uint(uintptr_t value) {
    seal_fmt_prepare_stdout();

    printf(
        "%" PRIuPTR,
        value
    );
}

void seal_fmt_write_f32(float value) {
    seal_fmt_prepare_stdout();

    printf(
        "%g",
        (double)value
    );
}

void seal_fmt_write_f64(double value) {
    seal_fmt_prepare_stdout();

    printf(
        "%g",
        value
    );
}

void seal_fmt_write_bool(bool value) {
    seal_fmt_prepare_stdout();

    fputs(
        value
            ? "true"
            : "false",
        stdout
    );
}

void seal_fmt_write_rawptr(void *value) {
    seal_fmt_prepare_stdout();

    printf(
        "%p",
        value
    );
}
