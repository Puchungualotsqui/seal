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

static bool seal_strconv_is_special_float_text(
    const char *text,
    size_t length
) {
    if (length == 0) {
        return false;
    }

    size_t offset =
        text[0] == '+' ||
        text[0] == '-';

    const char *unsigned_text =
        text + offset;

    size_t unsigned_length =
        length - offset;

    return seal_strconv_equal_text(
        unsigned_text,
        unsigned_length,
        "nan"
    ) ||
    seal_strconv_equal_text(
        unsigned_text,
        unsigned_length,
        "inf"
    );
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

    char *copy =
        malloc(length + 1);

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

#if defined(_WIN32) && !defined(__TINYC__)

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

    if (output == NULL ||
        length == 0 ||
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

    int parse_errno =
        errno;

    bool special =
        seal_strconv_is_special_float_text(
            text,
            length
        );

    bool valid =
        parse_errno != ERANGE &&
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

    if (output == NULL ||
        length == 0 ||
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

    int parse_errno =
        errno;

    bool special =
        seal_strconv_is_special_float_text(
            text,
            length
        );

    bool valid =
        parse_errno != ERANGE &&
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

#elif defined(_WIN32) && defined(__TINYC__)

/*
TinyCC's Windows runtime does not provide Microsoft's locale-specific
floating-point functions such as _strtof_l and _strtod_l.

For ordinary numbers, this fallback temporarily changes the process-wide
numeric locale to "C" and uses standard strtof and strtod.

TinyCC's Windows runtime may also reject "nan" and "inf", so those values
are constructed directly from their IEEE-754 representations.

Because setlocale changes process-global state, ordinary numeric parsing
and formatting in this branch are not safe when multiple threads
concurrently modify or depend on LC_NUMERIC.
*/

typedef enum sealStrconvSpecialFloat {
    SEAL_STRCONV_SPECIAL_NONE,
    SEAL_STRCONV_SPECIAL_POSITIVE_NAN,
    SEAL_STRCONV_SPECIAL_NEGATIVE_NAN,
    SEAL_STRCONV_SPECIAL_POSITIVE_INFINITY,
    SEAL_STRCONV_SPECIAL_NEGATIVE_INFINITY
} sealStrconvSpecialFloat;

static sealStrconvSpecialFloat
seal_strconv_classify_special_float(
    const char *text,
    size_t length
) {
    if (length == 0) {
        return SEAL_STRCONV_SPECIAL_NONE;
    }

    bool negative =
        text[0] == '-';

    size_t offset =
        text[0] == '+' ||
        text[0] == '-';

    const char *unsigned_text =
        text + offset;

    size_t unsigned_length =
        length - offset;

    if (seal_strconv_equal_text(
            unsigned_text,
            unsigned_length,
            "nan"
        )) {
        return negative
            ? SEAL_STRCONV_SPECIAL_NEGATIVE_NAN
            : SEAL_STRCONV_SPECIAL_POSITIVE_NAN;
    }

    if (seal_strconv_equal_text(
            unsigned_text,
            unsigned_length,
            "inf"
        )) {
        return negative
            ? SEAL_STRCONV_SPECIAL_NEGATIVE_INFINITY
            : SEAL_STRCONV_SPECIAL_POSITIVE_INFINITY;
    }

    return SEAL_STRCONV_SPECIAL_NONE;
}

static float seal_strconv_special_f32(
    sealStrconvSpecialFloat special
) {
    union {
        uint32_t bits;
        float value;
    } result;

    switch (special) {
    case SEAL_STRCONV_SPECIAL_POSITIVE_NAN:
        result.bits =
            UINT32_C(0x7fc00000);
        break;

    case SEAL_STRCONV_SPECIAL_NEGATIVE_NAN:
        result.bits =
            UINT32_C(0xffc00000);
        break;

    case SEAL_STRCONV_SPECIAL_POSITIVE_INFINITY:
        result.bits =
            UINT32_C(0x7f800000);
        break;

    case SEAL_STRCONV_SPECIAL_NEGATIVE_INFINITY:
        result.bits =
            UINT32_C(0xff800000);
        break;

    default:
        result.bits = 0;
        break;
    }

    return result.value;
}

static double seal_strconv_special_f64(
    sealStrconvSpecialFloat special
) {
    union {
        uint64_t bits;
        double value;
    } result;

    switch (special) {
    case SEAL_STRCONV_SPECIAL_POSITIVE_NAN:
        result.bits =
            UINT64_C(0x7ff8000000000000);
        break;

    case SEAL_STRCONV_SPECIAL_NEGATIVE_NAN:
        result.bits =
            UINT64_C(0xfff8000000000000);
        break;

    case SEAL_STRCONV_SPECIAL_POSITIVE_INFINITY:
        result.bits =
            UINT64_C(0x7ff0000000000000);
        break;

    case SEAL_STRCONV_SPECIAL_NEGATIVE_INFINITY:
        result.bits =
            UINT64_C(0xfff0000000000000);
        break;

    default:
        result.bits = 0;
        break;
    }

    return result.value;
}

static char *seal_strconv_copy_cstring(
    const char *value
) {
    if (value == NULL) {
        return NULL;
    }

    size_t length =
        strlen(value);

    if (length == SIZE_MAX) {
        return NULL;
    }

    char *copy =
        malloc(length + 1);

    if (copy == NULL) {
        return NULL;
    }

    memcpy(
        copy,
        value,
        length + 1
    );

    return copy;
}

static char *seal_strconv_enter_c_numeric_locale(
    void
) {
    const char *current =
        setlocale(
            LC_NUMERIC,
            NULL
        );

    char *saved =
        seal_strconv_copy_cstring(
            current
        );

    if (saved == NULL) {
        return NULL;
    }

    if (setlocale(
            LC_NUMERIC,
            "C"
        ) == NULL) {
        free(saved);
        return NULL;
    }

    return saved;
}

static bool seal_strconv_leave_numeric_locale(
    char *saved
) {
    if (saved == NULL) {
        return false;
    }

    bool restored =
        setlocale(
            LC_NUMERIC,
            saved
        ) != NULL;

    free(saved);

    return restored;
}

static int seal_strconv_count_f32(
    float value
) {
    char buffer[64];

    char *saved =
        seal_strconv_enter_c_numeric_locale();

    if (saved == NULL) {
        return -1;
    }

    int length = snprintf(
        buffer,
        sizeof(buffer),
        "%.9g",
        (double)value
    );

    bool restored =
        seal_strconv_leave_numeric_locale(
            saved
        );

    if (!restored ||
        length < 0 ||
        (size_t)length >= sizeof(buffer)) {
        return -1;
    }

    return length;
}

static int seal_strconv_count_f64(
    double value
) {
    char buffer[64];

    char *saved =
        seal_strconv_enter_c_numeric_locale();

    if (saved == NULL) {
        return -1;
    }

    int length = snprintf(
        buffer,
        sizeof(buffer),
        "%.17g",
        value
    );

    bool restored =
        seal_strconv_leave_numeric_locale(
            saved
        );

    if (!restored ||
        length < 0 ||
        (size_t)length >= sizeof(buffer)) {
        return -1;
    }

    return length;
}

static int seal_strconv_write_f32(
    char *destination,
    size_t capacity,
    float value
) {
    if (destination == NULL ||
        capacity == 0) {
        return -1;
    }

    char *saved =
        seal_strconv_enter_c_numeric_locale();

    if (saved == NULL) {
        return -1;
    }

    int written = snprintf(
        destination,
        capacity,
        "%.9g",
        (double)value
    );

    bool restored =
        seal_strconv_leave_numeric_locale(
            saved
        );

    if (!restored ||
        written < 0 ||
        (size_t)written >= capacity) {
        return -1;
    }

    return written;
}

static int seal_strconv_write_f64(
    char *destination,
    size_t capacity,
    double value
) {
    if (destination == NULL ||
        capacity == 0) {
        return -1;
    }

    char *saved =
        seal_strconv_enter_c_numeric_locale();

    if (saved == NULL) {
        return -1;
    }

    int written = snprintf(
        destination,
        capacity,
        "%.17g",
        value
    );

    bool restored =
        seal_strconv_leave_numeric_locale(
            saved
        );

    if (!restored ||
        written < 0 ||
        (size_t)written >= capacity) {
        return -1;
    }

    return written;
}

static bool seal_strconv_parse_f32_internal(
    sealString input,
    float *output
) {
    size_t length =
        (size_t)input.len;

    if (output == NULL ||
        length == 0 ||
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

    sealStrconvSpecialFloat special =
        seal_strconv_classify_special_float(
            text,
            length
        );

    if (special !=
        SEAL_STRCONV_SPECIAL_NONE) {
        *output =
            seal_strconv_special_f32(
                special
            );

        free(text);

        return true;
    }

    char *saved =
        seal_strconv_enter_c_numeric_locale();

    if (saved == NULL) {
        free(text);
        return false;
    }

    errno = 0;

    char *end = NULL;

    float parsed = strtof(
        text,
        &end
    );

    int parse_errno =
        errno;

    bool restored =
        seal_strconv_leave_numeric_locale(
            saved
        );

    bool valid =
        restored &&
        parse_errno != ERANGE &&
        end == text + length &&
        isfinite(parsed);

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

    if (output == NULL ||
        length == 0 ||
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

    sealStrconvSpecialFloat special =
        seal_strconv_classify_special_float(
            text,
            length
        );

    if (special !=
        SEAL_STRCONV_SPECIAL_NONE) {
        *output =
            seal_strconv_special_f64(
                special
            );

        free(text);

        return true;
    }

    char *saved =
        seal_strconv_enter_c_numeric_locale();

    if (saved == NULL) {
        free(text);
        return false;
    }

    errno = 0;

    char *end = NULL;

    double parsed = strtod(
        text,
        &end
    );

    int parse_errno =
        errno;

    bool restored =
        seal_strconv_leave_numeric_locale(
            saved
        );

    bool valid =
        restored &&
        parse_errno != ERANGE &&
        end == text + length &&
        isfinite(parsed);

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

    if (previous == (locale_t)0) {
        freelocale(locale);
        return -1;
    }

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

    if (previous == (locale_t)0) {
        freelocale(locale);
        return -1;
    }

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

    if (previous == (locale_t)0) {
        freelocale(locale);
        return -1;
    }

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

    if (previous == (locale_t)0) {
        freelocale(locale);
        return -1;
    }

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

    if (output == NULL ||
        length == 0 ||
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

    int parse_errno =
        errno;

    bool special =
        seal_strconv_is_special_float_text(
            text,
            length
        );

    bool valid =
        parse_errno != ERANGE &&
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

    if (output == NULL ||
        length == 0 ||
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

    int parse_errno =
        errno;

    bool special =
        seal_strconv_is_special_float_text(
            text,
            length
        );

    bool valid =
        parse_errno != ERANGE &&
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
