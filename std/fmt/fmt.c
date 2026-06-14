#include <stdio.h>
#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>

typedef struct sealString {
    const unsigned char *data;
    size_t byte_len;
} sealString;

void seal_fmt_write_cstring(const char *s) {
    fputs(s, stdout);
}

void seal_fmt_write_string(sealString s) {
    fwrite(s.data, 1, s.byte_len, stdout);
}

void seal_fmt_write_char(uint32_t c) {
    if (c <= 0x7F) {
        fputc((int)c, stdout);
    } else if (c <= 0x7FF) {
        fputc(0xC0 | ((c >> 6) & 0x1F), stdout);
        fputc(0x80 | (c & 0x3F), stdout);
    } else if (c <= 0xFFFF) {
        fputc(0xE0 | ((c >> 12) & 0x0F), stdout);
        fputc(0x80 | ((c >> 6) & 0x3F), stdout);
        fputc(0x80 | (c & 0x3F), stdout);
    } else {
        fputc(0xF0 | ((c >> 18) & 0x07), stdout);
        fputc(0x80 | ((c >> 12) & 0x3F), stdout);
        fputc(0x80 | ((c >> 6) & 0x3F), stdout);
        fputc(0x80 | (c & 0x3F), stdout);
    }
}

void seal_fmt_write_int(int v) {
    printf("%d", v);
}

void seal_fmt_write_uint(uintptr_t v) {
    printf("%zu", (size_t)v);
}

void seal_fmt_write_f32(float v) {
    printf("%g", v);
}

void seal_fmt_write_f64(double v) {
    printf("%g", v);
}

void seal_fmt_write_bool(bool v) {
    fputs(v ? "true" : "false", stdout);
}

void seal_fmt_write_rawptr(void *v) {
    printf("%p", v);
}
