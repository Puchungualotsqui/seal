#ifndef _WIN32
#define _GNU_SOURCE
#endif

#include <stdio.h>
#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include <stdlib.h>
#include <string.h>
#include <locale.h>
#include <errno.h>
#include <math.h>

typedef struct sealString {
    const uint8_t *data;
    uintptr_t len;
} sealString;

static bool seal_strconv_equal_text(
    const char *value,
    size_t value_length,
    const char *expected
) {
    size_t expected_length =
        strlen(expected);

    if (value_length != expected_length) {
        return false;
    }

    return memcmp(
        value,
        expected,
        expected_length
    ) == 0;
}

/*
Validate the strict floating-point grammar accepted by strconv.

This intentionally rejects:

    whitespace
    hexadecimal floats
    infinity
    NaN payloads
    locale-specific decimal separators
*/
static bool seal_strconv_float_syntax_is_valid(
    const char *value,
    size_t length
) {
    if (length == 0) {
        return false;
    }

    size_t index = 0;

    if (value[index] == '+' ||
        value[index] == '-') {
        index++;

        if (index == length) {
            return false;
        }
    }

    const char *unsigned_value =
        value + index;

    size_t unsigned_length =
        length - index;

    if (seal_strconv_equal_text(
            unsigned_value,
            unsigned_length,
            "nan"
        ) ||
        seal_strconv_equal_text(
            unsigned_value,
            unsigned_length,
            "inf"
        )) {
        return true;
    }

    bool has_integer_digits = false;

    while (index < length &&
           value[index] >= '0' &&
           value[index] <= '9') {
        has_integer_digits = true;
        index++;
    }

    bool has_fraction_digits = false;

    if (index < length &&
        value[index] == '.') {
        index++;

        while (index < length &&
               value[index] >= '0' &&
               value[index] <= '9') {
            has_fraction_digits = true;
            index++;
        }
    }

    if (!has_integer_digits &&
        !has_fraction_digits) {
        return false;
    }

    if (index < length &&
        (value[index] == 'e' ||
         value[index] == 'E')) {
        index++;

        if (index < length &&
            (value[index] == '+' ||
             value[index] == '-')) {
            index++;
        }

        bool has_exponent_digits = false;

        while (index < length &&
               value[index] >= '0' &&
               value[index] <= '9') {
            has_exponent_digits = true;
            index++;
        }

        if (!has_exponent_digits) {
            return false;
        }
    }

    return index == length;
}

static char *seal_strconv_copy_string(
    sealString value
) {
    if (value.len > SIZE_MAX - 1) {
        return NULL;
    }

    size_t length =
        (size_t)value.len;

    char *copy = malloc(
        length + 1
    );

    if (copy == NULL) {
        return NULL;
    }

    if (length > 0) {
        memcpy(
            copy,
            value.data,
            length
        );
    }

    copy[length] = '\0';

    return copy;
}

#ifdef _WIN32

static _locale_t seal_strconv_create_c_locale(
    void
) {
    return _create_locale(
        LC_NUMERIC,
        "C"
    );
}

static int seal_strconv_count_f32(
    float value
) {
    _locale_t locale =
        seal_strconv_create_c_locale();

    if (locale == NULL) {
        return -1;
    }

    int length = _scprintf_l(
        "%.9g",
        locale,
        (double)value
    );

    _free_locale(locale);

    return length;
}

static int seal_strconv_count_f64(
    double value
) {
    _locale_t locale =
        seal_strconv_create_c_locale();

    if (locale == NULL) {
        return -1;
    }

    int length = _scprintf_l(
        "%.17g",
        locale,
        value
    );

    _free_locale(locale);

    return length;
}

static int seal_strconv_write_f32(
    char *destination,
    size_t capacity,
    float value
) {
    _locale_t locale =
        seal_strconv_create_c_locale();

    if (locale == NULL) {
        return -1;
    }

    int written = _snprintf_l(
        destination,
        capacity,
        "%.9g",
        locale,
        (double)value
    );

    _free_locale(locale);

    return written;
}

static int seal_strconv_write_f64(
    char *destination,
    size_t capacity,
    double value
) {
    _locale_t locale =
        seal_strconv_create_c_locale();

    if (locale == NULL) {
        return -1;
    }

    int written = _snprintf_l(
        destination,
        capacity,
        "%.17g",
        locale,
        value
    );

    _free_locale(locale);

    return written;
}

static bool seal_strconv_parse_f32_internal(
    sealString input,
    float *output
) {
    size_t length =
        (size_t)input.len;

    if (length == 0 ||
        memchr(
            input.data,
            '\0',
            length
        ) != NULL) {
        return false;
    }

    char *text =
        seal_strconv_copy_string(input);

    if (text == NULL) {
        return false;
    }

    if (!seal_strconv_float_syntax_is_valid(
            text,
            length
        )) {
        free(text);
        return false;
    }

    _locale_t locale =
        seal_strconv_create_c_locale();

    if (locale == NULL) {
        free(text);
        return false;
    }

    errno = 0;

    char *end = NULL;

    float parsed = _strtof_l(
        text,
        &end,
        locale
    );

    bool special =
        seal_strconv_equal_text(
            text +
                (text[0] == '+' ||
                 text[0] == '-'),
            length -
                (text[0] == '+' ||
                 text[0] == '-'),
            "nan"
        ) ||
        seal_strconv_equal_text(
            text +
                (text[0] == '+' ||
                 text[0] == '-'),
            length -
                (text[0] == '+' ||
                 text[0] == '-'),
            "inf"
        );

    bool valid =
        errno != ERANGE &&
        end == text + length &&
        (special || isfinite(parsed));

    _free_locale(locale);
    free(text);

    if (!valid) {
        return false;
    }

    *output = parsed;

    return true;
}

static bool seal_strconv_parse_f64_internal(
    sealString input,
    double *output
) {
    size_t length =
        (size_t)input.len;

    if (length == 0 ||
        memchr(
            input.data,
            '\0',
            length
        ) != NULL) {
        return false;
    }

    char *text =
        seal_strconv_copy_string(input);

    if (text == NULL) {
        return false;
    }

    if (!seal_strconv_float_syntax_is_valid(
            text,
            length
        )) {
        free(text);
        return false;
    }

    _locale_t locale =
        seal_strconv_create_c_locale();

    if (locale == NULL) {
        free(text);
        return false;
    }

    errno = 0;

    char *end = NULL;

    double parsed = _strtod_l(
        text,
        &end,
        locale
    );

    bool special =
        seal_strconv_equal_text(
            text +
                (text[0] == '+' ||
                 text[0] == '-'),
            length -
                (text[0] == '+' ||
                 text[0] == '-'),
            "nan"
        ) ||
        seal_strconv_equal_text(
            text +
                (text[0] == '+' ||
                 text[0] == '-'),
            length -
                (text[0] == '+' ||
                 text[0] == '-'),
            "inf"
        );

    bool valid =
        errno != ERANGE &&
        end == text + length &&
        (special || isfinite(parsed));

    _free_locale(locale);
    free(text);

    if (!valid) {
        return false;
    }

    *output = parsed;

    return true;
}

#else

static locale_t seal_strconv_create_c_locale(
    void
) {
    return newlocale(
        LC_NUMERIC_MASK,
        "C",
        (locale_t)0
    );
}

static int seal_strconv_count_f32(
    float value
) {
    locale_t locale =
        seal_strconv_create_c_locale();

    if (locale == (locale_t)0) {
        return -1;
    }

    locale_t previous =
        uselocale(locale);

    int length = snprintf(
        NULL,
        0,
        "%.9g",
        (double)value
    );

    uselocale(previous);
    freelocale(locale);

    return length;
}

static int seal_strconv_count_f64(
    double value
) {
    locale_t locale =
        seal_strconv_create_c_locale();

    if (locale == (locale_t)0) {
        return -1;
    }

    locale_t previous =
        uselocale(locale);

    int length = snprintf(
        NULL,
        0,
        "%.17g",
        value
    );

    uselocale(previous);
    freelocale(locale);

    return length;
}

static int seal_strconv_write_f32(
    char *destination,
    size_t capacity,
    float value
) {
    locale_t locale =
        seal_strconv_create_c_locale();

    if (locale == (locale_t)0) {
        return -1;
    }

    locale_t previous =
        uselocale(locale);

    int written = snprintf(
        destination,
        capacity,
        "%.9g",
        (double)value
    );

    uselocale(previous);
    freelocale(locale);

    return written;
}

static int seal_strconv_write_f64(
    char *destination,
    size_t capacity,
    double value
) {
    locale_t locale =
        seal_strconv_create_c_locale();

    if (locale == (locale_t)0) {
        return -1;
    }

    locale_t previous =
        uselocale(locale);

    int written = snprintf(
        destination,
        capacity,
        "%.17g",
        value
    );

    uselocale(previous);
    freelocale(locale);

    return written;
}

static bool seal_strconv_parse_f32_internal(
    sealString input,
    float *output
) {
    size_t length =
        (size_t)input.len;

    if (length == 0 ||
        memchr(
            input.data,
            '\0',
            length
        ) != NULL) {
        return false;
    }

    char *text =
        seal_strconv_copy_string(input);

    if (text == NULL) {
        return false;
    }

    if (!seal_strconv_float_syntax_is_valid(
            text,
            length
        )) {
        free(text);
        return false;
    }

    locale_t locale =
        seal_strconv_create_c_locale();

    if (locale == (locale_t)0) {
        free(text);
        return false;
    }

    errno = 0;

    char *end = NULL;

    float parsed = strtof_l(
        text,
        &end,
        locale
    );

    bool special =
        seal_strconv_equal_text(
            text +
                (text[0] == '+' ||
                 text[0] == '-'),
            length -
                (text[0] == '+' ||
                 text[0] == '-'),
            "nan"
        ) ||
        seal_strconv_equal_text(
            text +
                (text[0] == '+' ||
                 text[0] == '-'),
            length -
                (text[0] == '+' ||
                 text[0] == '-'),
            "inf"
        );

    bool valid =
        errno != ERANGE &&
        end == text + length &&
        (special || isfinite(parsed));

    freelocale(locale);
    free(text);

    if (!valid) {
        return false;
    }

    *output = parsed;

    return true;
}

static bool seal_strconv_parse_f64_internal(
    sealString input,
    double *output
) {
    size_t length =
        (size_t)input.len;

    if (length == 0 ||
        memchr(
            input.data,
            '\0',
            length
        ) != NULL) {
        return false;
    }

    char *text =
        seal_strconv_copy_string(input);

    if (text == NULL) {
        return false;
    }

    if (!seal_strconv_float_syntax_is_valid(
            text,
            length
        )) {
        free(text);
        return false;
    }

    locale_t locale =
        seal_strconv_create_c_locale();

    if (locale == (locale_t)0) {
        free(text);
        return false;
    }

    errno = 0;

    char *end = NULL;

    double parsed = strtod_l(
        text,
        &end,
        locale
    );

    bool special =
        seal_strconv_equal_text(
            text +
                (text[0] == '+' ||
                 text[0] == '-'),
            length -
                (text[0] == '+' ||
                 text[0] == '-'),
            "nan"
        ) ||
        seal_strconv_equal_text(
            text +
                (text[0] == '+' ||
                 text[0] == '-'),
            length -
                (text[0] == '+' ||
                 text[0] == '-'),
            "inf"
        );

    bool valid =
        errno != ERANGE &&
        end == text + length &&
        (special || isfinite(parsed));

    freelocale(locale);
    free(text);

    if (!valid) {
        return false;
    }

    *output = parsed;

    return true;
}

#endif

intptr_t seal_strconv_f32_text_length(
    float value
) {
    return (intptr_t)
        seal_strconv_count_f32(value);
}

intptr_t seal_strconv_f64_text_length(
    double value
) {
    return (intptr_t)
        seal_strconv_count_f64(value);
}

intptr_t seal_strconv_write_f32_text(
    void *destination,
    uintptr_t capacity,
    float value
) {
    return (intptr_t)
        seal_strconv_write_f32(
            (char *)destination,
            (size_t)capacity,
            value
        );
}

intptr_t seal_strconv_write_f64_text(
    void *destination,
    uintptr_t capacity,
    double value
) {
    return (intptr_t)
        seal_strconv_write_f64(
            (char *)destination,
            (size_t)capacity,
            value
        );
}

bool seal_strconv_f32_text_is_valid(
    sealString value
) {
    float parsed = 0.0f;

    return seal_strconv_parse_f32_internal(
        value,
        &parsed
    );
}

bool seal_strconv_f64_text_is_valid(
    sealString value
) {
    double parsed = 0.0;

    return seal_strconv_parse_f64_internal(
        value,
        &parsed
    );
}

float seal_strconv_parse_f32_value(
    sealString value
) {
    float parsed = 0.0f;

    if (!seal_strconv_parse_f32_internal(
            value,
            &parsed
        )) {
        return 0.0f;
    }

    return parsed;
}

double seal_strconv_parse_f64_value(
    sealString value
) {
    double parsed = 0.0;

    if (!seal_strconv_parse_f64_internal(
            value,
            &parsed
        )) {
        return 0.0;
    }

    return parsed;
}
