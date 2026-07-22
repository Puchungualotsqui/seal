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
#include <limits.h>

typedef struct sealString {
    const uint8_t *data;
    uintptr_t len;
} sealString;

#define SEAL_STRCONV_INVALID_F32_BITS \
    UINT32_C(0x7fc00001)

#define SEAL_STRCONV_INVALID_F64_BITS \
    UINT64_C(0x7ff8000000000001)

#define SEAL_STRCONV_POSITIVE_NAN_F32_BITS \
    UINT32_C(0x7fc00000)

#define SEAL_STRCONV_NEGATIVE_NAN_F32_BITS \
    UINT32_C(0xffc00000)

#define SEAL_STRCONV_POSITIVE_INFINITY_F32_BITS \
    UINT32_C(0x7f800000)

#define SEAL_STRCONV_NEGATIVE_INFINITY_F32_BITS \
    UINT32_C(0xff800000)

#define SEAL_STRCONV_POSITIVE_NAN_F64_BITS \
    UINT64_C(0x7ff8000000000000)

#define SEAL_STRCONV_NEGATIVE_NAN_F64_BITS \
    UINT64_C(0xfff8000000000000)

#define SEAL_STRCONV_POSITIVE_INFINITY_F64_BITS \
    UINT64_C(0x7ff0000000000000)

#define SEAL_STRCONV_NEGATIVE_INFINITY_F64_BITS \
    UINT64_C(0xfff0000000000000)

typedef enum sealStrconvSpecialFloat {
    SEAL_STRCONV_SPECIAL_NONE,
    SEAL_STRCONV_SPECIAL_POSITIVE_NAN,
    SEAL_STRCONV_SPECIAL_NEGATIVE_NAN,
    SEAL_STRCONV_SPECIAL_POSITIVE_INFINITY,
    SEAL_STRCONV_SPECIAL_NEGATIVE_INFINITY
} sealStrconvSpecialFloat;

static bool seal_strconv_equal_text(
    const char *value,
    size_t value_length,
    const char *expected
) {
    size_t expected_length =
        strlen(expected);

    if (value_length !=
        expected_length) {
        return false;
    }

    return memcmp(
        value,
        expected,
        expected_length
    ) == 0;
}

static sealStrconvSpecialFloat
seal_strconv_classify_special_float(
    const char *text,
    size_t length
) {
    if (text == NULL ||
        length == 0) {
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

static float seal_strconv_f32_from_raw_bits(
    uint32_t bits
) {
    float result = 0.0f;

    memcpy(
        &result,
        &bits,
        sizeof(result)
    );

    return result;
}

static double seal_strconv_f64_from_raw_bits(
    uint64_t bits
) {
    double result = 0.0;

    memcpy(
        &result,
        &bits,
        sizeof(result)
    );

    return result;
}

static uint32_t seal_strconv_f32_to_raw_bits(
    float value
) {
    uint32_t result = 0;

    memcpy(
        &result,
        &value,
        sizeof(result)
    );

    return result;
}

static uint64_t seal_strconv_f64_to_raw_bits(
    double value
) {
    uint64_t result = 0;

    memcpy(
        &result,
        &value,
        sizeof(result)
    );

    return result;
}

static float seal_strconv_special_f32(
    sealStrconvSpecialFloat special
) {
    switch (special) {
    case SEAL_STRCONV_SPECIAL_POSITIVE_NAN:
        return seal_strconv_f32_from_raw_bits(
            SEAL_STRCONV_POSITIVE_NAN_F32_BITS
        );

    case SEAL_STRCONV_SPECIAL_NEGATIVE_NAN:
        return seal_strconv_f32_from_raw_bits(
            SEAL_STRCONV_NEGATIVE_NAN_F32_BITS
        );

    case SEAL_STRCONV_SPECIAL_POSITIVE_INFINITY:
        return seal_strconv_f32_from_raw_bits(
            SEAL_STRCONV_POSITIVE_INFINITY_F32_BITS
        );

    case SEAL_STRCONV_SPECIAL_NEGATIVE_INFINITY:
        return seal_strconv_f32_from_raw_bits(
            SEAL_STRCONV_NEGATIVE_INFINITY_F32_BITS
        );

    default:
        return 0.0f;
    }
}

static double seal_strconv_special_f64(
    sealStrconvSpecialFloat special
) {
    switch (special) {
    case SEAL_STRCONV_SPECIAL_POSITIVE_NAN:
        return seal_strconv_f64_from_raw_bits(
            SEAL_STRCONV_POSITIVE_NAN_F64_BITS
        );

    case SEAL_STRCONV_SPECIAL_NEGATIVE_NAN:
        return seal_strconv_f64_from_raw_bits(
            SEAL_STRCONV_NEGATIVE_NAN_F64_BITS
        );

    case SEAL_STRCONV_SPECIAL_POSITIVE_INFINITY:
        return seal_strconv_f64_from_raw_bits(
            SEAL_STRCONV_POSITIVE_INFINITY_F64_BITS
        );

    case SEAL_STRCONV_SPECIAL_NEGATIVE_INFINITY:
        return seal_strconv_f64_from_raw_bits(
            SEAL_STRCONV_NEGATIVE_INFINITY_F64_BITS
        );

    default:
        return 0.0;
    }
}

/*
Validate the strict floating-point grammar accepted by strconv.

This intentionally rejects:

    whitespace
    hexadecimal floats
    infinity
    NaN payloads
    locale-specific decimal separators
    trailing characters
*/
static bool seal_strconv_float_syntax_is_valid(
    const char *value,
    size_t length
) {
    if (value == NULL ||
        length == 0) {
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

    bool has_integer_digits =
        false;

    while (index < length &&
           value[index] >= '0' &&
           value[index] <= '9') {
        has_integer_digits = true;
        index++;
    }

    bool has_fraction_digits =
        false;

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

        bool has_exponent_digits =
            false;

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

static bool seal_strconv_input_is_usable(
    sealString value,
    const char **text,
    size_t *length
) {
    if (text == NULL ||
        length == NULL ||
        value.len == 0 ||
        value.data == NULL) {
        return false;
    }

#if UINTPTR_MAX > SIZE_MAX
    if (value.len >
        (uintptr_t)SIZE_MAX) {
        return false;
    }
#endif

    size_t byte_length =
        (size_t)value.len;

    if (memchr(
            value.data,
            '\0',
            byte_length
        ) != NULL) {
        return false;
    }

    *text =
        (const char *)value.data;

    *length =
        byte_length;

    return true;
}

static char *seal_strconv_copy_text(
    const char *value,
    size_t length
) {
    if (value == NULL ||
        length == SIZE_MAX) {
        return NULL;
    }

    char *copy =
        (char *)malloc(
            length + 1
        );

    if (copy == NULL) {
        return NULL;
    }

    memcpy(
        copy,
        value,
        length
    );

    copy[length] = '\0';

    return copy;
}

static const char *
seal_strconv_special_format_text_f32(
    float value
) {
    if (isnan(value)) {
        return "nan";
    }

    if (isinf(value)) {
        return signbit(value)
            ? "-inf"
            : "inf";
    }

    return NULL;
}

static const char *
seal_strconv_special_format_text_f64(
    double value
) {
    if (isnan(value)) {
        return "nan";
    }

    if (isinf(value)) {
        return signbit(value)
            ? "-inf"
            : "inf";
    }

    return NULL;
}

static int seal_strconv_write_literal(
    char *destination,
    size_t capacity,
    const char *text
) {
    if (destination == NULL ||
        text == NULL) {
        return -1;
    }

    size_t length =
        strlen(text);

    if (length > (size_t)INT_MAX ||
        capacity <= length) {
        return -1;
    }

    memcpy(
        destination,
        text,
        length
    );

    destination[length] = '\0';

    return (int)length;
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

static int seal_strconv_count_f32_finite(
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

    _free_locale(
        locale
    );

    return length;
}

static int seal_strconv_count_f64_finite(
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

    _free_locale(
        locale
    );

    return length;
}

static int seal_strconv_write_f32_finite(
    char *destination,
    size_t capacity,
    float value
) {
    if (destination == NULL ||
        capacity == 0) {
        return -1;
    }

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

    _free_locale(
        locale
    );

    if (written < 0 ||
        (size_t)written >= capacity) {
        return -1;
    }

    return written;
}

static int seal_strconv_write_f64_finite(
    char *destination,
    size_t capacity,
    double value
) {
    if (destination == NULL ||
        capacity == 0) {
        return -1;
    }

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

    _free_locale(
        locale
    );

    if (written < 0 ||
        (size_t)written >= capacity) {
        return -1;
    }

    return written;
}

static bool seal_strconv_parse_f32_finite(
    const char *text,
    size_t length,
    float *output
) {
    if (text == NULL ||
        output == NULL) {
        return false;
    }

    _locale_t locale =
        seal_strconv_create_c_locale();

    if (locale == NULL) {
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

    bool valid =
        parse_errno != ERANGE &&
        end == text + length &&
        isfinite(parsed);

    _free_locale(
        locale
    );

    if (!valid) {
        return false;
    }

    *output = parsed;

    return true;
}

static bool seal_strconv_parse_f64_finite(
    const char *text,
    size_t length,
    double *output
) {
    if (text == NULL ||
        output == NULL) {
        return false;
    }

    _locale_t locale =
        seal_strconv_create_c_locale();

    if (locale == NULL) {
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

    bool valid =
        parse_errno != ERANGE &&
        end == text + length &&
        isfinite(parsed);

    _free_locale(
        locale
    );

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

The fallback temporarily changes the process-wide numeric locale to "C"
and restores it after each conversion.

Because setlocale changes process-global state, this branch is not safe
when multiple threads concurrently modify or depend on LC_NUMERIC.
*/

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
        (char *)malloc(
            length + 1
        );

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
        free(
            saved
        );

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

    free(
        saved
    );

    return restored;
}

static int seal_strconv_count_f32_finite(
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
        (size_t)length >=
            sizeof(buffer)) {
        return -1;
    }

    return length;
}

static int seal_strconv_count_f64_finite(
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
        (size_t)length >=
            sizeof(buffer)) {
        return -1;
    }

    return length;
}

static int seal_strconv_write_f32_finite(
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
        (size_t)written >=
            capacity) {
        return -1;
    }

    return written;
}

static int seal_strconv_write_f64_finite(
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
        (size_t)written >=
            capacity) {
        return -1;
    }

    return written;
}

static bool seal_strconv_parse_f32_finite(
    const char *text,
    size_t length,
    float *output
) {
    if (text == NULL ||
        output == NULL) {
        return false;
    }

    char *saved =
        seal_strconv_enter_c_numeric_locale();

    if (saved == NULL) {
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

    if (!valid) {
        return false;
    }

    *output = parsed;

    return true;
}

static bool seal_strconv_parse_f64_finite(
    const char *text,
    size_t length,
    double *output
) {
    if (text == NULL ||
        output == NULL) {
        return false;
    }

    char *saved =
        seal_strconv_enter_c_numeric_locale();

    if (saved == NULL) {
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

static int seal_strconv_count_f32_finite(
    float value
) {
    locale_t locale =
        seal_strconv_create_c_locale();

    if (locale ==
        (locale_t)0) {
        return -1;
    }

    locale_t previous =
        uselocale(
            locale
        );

    if (previous ==
        (locale_t)0) {
        freelocale(
            locale
        );

        return -1;
    }

    int length = snprintf(
        NULL,
        0,
        "%.9g",
        (double)value
    );

    bool restored =
        uselocale(
            previous
        ) != (locale_t)0;

    freelocale(
        locale
    );

    if (!restored) {
        return -1;
    }

    return length;
}

static int seal_strconv_count_f64_finite(
    double value
) {
    locale_t locale =
        seal_strconv_create_c_locale();

    if (locale ==
        (locale_t)0) {
        return -1;
    }

    locale_t previous =
        uselocale(
            locale
        );

    if (previous ==
        (locale_t)0) {
        freelocale(
            locale
        );

        return -1;
    }

    int length = snprintf(
        NULL,
        0,
        "%.17g",
        value
    );

    bool restored =
        uselocale(
            previous
        ) != (locale_t)0;

    freelocale(
        locale
    );

    if (!restored) {
        return -1;
    }

    return length;
}

static int seal_strconv_write_f32_finite(
    char *destination,
    size_t capacity,
    float value
) {
    if (destination == NULL ||
        capacity == 0) {
        return -1;
    }

    locale_t locale =
        seal_strconv_create_c_locale();

    if (locale ==
        (locale_t)0) {
        return -1;
    }

    locale_t previous =
        uselocale(
            locale
        );

    if (previous ==
        (locale_t)0) {
        freelocale(
            locale
        );

        return -1;
    }

    int written = snprintf(
        destination,
        capacity,
        "%.9g",
        (double)value
    );

    bool restored =
        uselocale(
            previous
        ) != (locale_t)0;

    freelocale(
        locale
    );

    if (!restored ||
        written < 0 ||
        (size_t)written >=
            capacity) {
        return -1;
    }

    return written;
}

static int seal_strconv_write_f64_finite(
    char *destination,
    size_t capacity,
    double value
) {
    if (destination == NULL ||
        capacity == 0) {
        return -1;
    }

    locale_t locale =
        seal_strconv_create_c_locale();

    if (locale ==
        (locale_t)0) {
        return -1;
    }

    locale_t previous =
        uselocale(
            locale
        );

    if (previous ==
        (locale_t)0) {
        freelocale(
            locale
        );

        return -1;
    }

    int written = snprintf(
        destination,
        capacity,
        "%.17g",
        value
    );

    bool restored =
        uselocale(
            previous
        ) != (locale_t)0;

    freelocale(
        locale
    );

    if (!restored ||
        written < 0 ||
        (size_t)written >=
            capacity) {
        return -1;
    }

    return written;
}

static bool seal_strconv_parse_f32_finite(
    const char *text,
    size_t length,
    float *output
) {
    if (text == NULL ||
        output == NULL) {
        return false;
    }

    locale_t locale =
        seal_strconv_create_c_locale();

    if (locale ==
        (locale_t)0) {
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

    bool valid =
        parse_errno != ERANGE &&
        end == text + length &&
        isfinite(parsed);

    freelocale(
        locale
    );

    if (!valid) {
        return false;
    }

    *output = parsed;

    return true;
}

static bool seal_strconv_parse_f64_finite(
    const char *text,
    size_t length,
    double *output
) {
    if (text == NULL ||
        output == NULL) {
        return false;
    }

    locale_t locale =
        seal_strconv_create_c_locale();

    if (locale ==
        (locale_t)0) {
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

    bool valid =
        parse_errno != ERANGE &&
        end == text + length &&
        isfinite(parsed);

    freelocale(
        locale
    );

    if (!valid) {
        return false;
    }

    *output = parsed;

    return true;
}

#endif

static int seal_strconv_count_f32(
    float value
) {
    const char *special =
        seal_strconv_special_format_text_f32(
            value
        );

    if (special != NULL) {
        size_t length =
            strlen(special);

        if (length >
            (size_t)INT_MAX) {
            return -1;
        }

        return (int)length;
    }

    return seal_strconv_count_f32_finite(
        value
    );
}

static int seal_strconv_count_f64(
    double value
) {
    const char *special =
        seal_strconv_special_format_text_f64(
            value
        );

    if (special != NULL) {
        size_t length =
            strlen(special);

        if (length >
            (size_t)INT_MAX) {
            return -1;
        }

        return (int)length;
    }

    return seal_strconv_count_f64_finite(
        value
    );
}

static int seal_strconv_write_f32(
    char *destination,
    size_t capacity,
    float value
) {
    const char *special =
        seal_strconv_special_format_text_f32(
            value
        );

    if (special != NULL) {
        return seal_strconv_write_literal(
            destination,
            capacity,
            special
        );
    }

    return seal_strconv_write_f32_finite(
        destination,
        capacity,
        value
    );
}

static int seal_strconv_write_f64(
    char *destination,
    size_t capacity,
    double value
) {
    const char *special =
        seal_strconv_special_format_text_f64(
            value
        );

    if (special != NULL) {
        return seal_strconv_write_literal(
            destination,
            capacity,
            special
        );
    }

    return seal_strconv_write_f64_finite(
        destination,
        capacity,
        value
    );
}

static bool seal_strconv_parse_f32_internal(
    sealString input,
    float *output
) {
    if (output == NULL) {
        return false;
    }

    const char *source = NULL;
    size_t length = 0;

    if (!seal_strconv_input_is_usable(
            input,
            &source,
            &length
        )) {
        return false;
    }

    if (!seal_strconv_float_syntax_is_valid(
            source,
            length
        )) {
        return false;
    }

    sealStrconvSpecialFloat special =
        seal_strconv_classify_special_float(
            source,
            length
        );

    if (special !=
        SEAL_STRCONV_SPECIAL_NONE) {
        *output =
            seal_strconv_special_f32(
                special
            );

        return true;
    }

    char *text =
        seal_strconv_copy_text(
            source,
            length
        );

    if (text == NULL) {
        return false;
    }

    bool valid =
        seal_strconv_parse_f32_finite(
            text,
            length,
            output
        );

    free(
        text
    );

    return valid;
}

static bool seal_strconv_parse_f64_internal(
    sealString input,
    double *output
) {
    if (output == NULL) {
        return false;
    }

    const char *source = NULL;
    size_t length = 0;

    if (!seal_strconv_input_is_usable(
            input,
            &source,
            &length
        )) {
        return false;
    }

    if (!seal_strconv_float_syntax_is_valid(
            source,
            length
        )) {
        return false;
    }

    sealStrconvSpecialFloat special =
        seal_strconv_classify_special_float(
            source,
            length
        );

    if (special !=
        SEAL_STRCONV_SPECIAL_NONE) {
        *output =
            seal_strconv_special_f64(
                special
            );

        return true;
    }

    char *text =
        seal_strconv_copy_text(
            source,
            length
        );

    if (text == NULL) {
        return false;
    }

    bool valid =
        seal_strconv_parse_f64_finite(
            text,
            length,
            output
        );

    free(
        text
    );

    return valid;
}

intptr_t seal_strconv_f32_text_length(
    float value
) {
    return (intptr_t)
        seal_strconv_count_f32(
            value
        );
}

intptr_t seal_strconv_f64_text_length(
    double value
) {
    return (intptr_t)
        seal_strconv_count_f64(
            value
        );
}

intptr_t seal_strconv_write_f32_text(
    void *destination,
    uintptr_t capacity,
    float value
) {
#if UINTPTR_MAX > SIZE_MAX
    if (capacity >
        (uintptr_t)SIZE_MAX) {
        return -1;
    }
#endif

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
#if UINTPTR_MAX > SIZE_MAX
    if (capacity >
        (uintptr_t)SIZE_MAX) {
        return -1;
    }
#endif

    return (intptr_t)
        seal_strconv_write_f64(
            (char *)destination,
            (size_t)capacity,
            value
        );
}

uint32_t seal_strconv_parse_f32_bits(
    sealString value
) {
    float parsed = 0.0f;

    if (!seal_strconv_parse_f32_internal(
            value,
            &parsed
        )) {
        return SEAL_STRCONV_INVALID_F32_BITS;
    }

    uint32_t bits =
        seal_strconv_f32_to_raw_bits(
            parsed
        );

    /*
    Valid special values are canonicalized before this point, and finite
    values cannot have the reserved NaN representation.
    */
    if (bits ==
        SEAL_STRCONV_INVALID_F32_BITS) {
        return SEAL_STRCONV_POSITIVE_NAN_F32_BITS;
    }

    return bits;
}

uint64_t seal_strconv_parse_f64_bits(
    sealString value
) {
    double parsed = 0.0;

    if (!seal_strconv_parse_f64_internal(
            value,
            &parsed
        )) {
        return SEAL_STRCONV_INVALID_F64_BITS;
    }

    uint64_t bits =
        seal_strconv_f64_to_raw_bits(
            parsed
        );

    /*
    Valid special values are canonicalized before this point, and finite
    values cannot have the reserved NaN representation.
    */
    if (bits ==
        SEAL_STRCONV_INVALID_F64_BITS) {
        return SEAL_STRCONV_POSITIVE_NAN_F64_BITS;
    }

    return bits;
}

float seal_strconv_f32_from_bits(
    uint32_t bits
) {
    return seal_strconv_f32_from_raw_bits(
        bits
    );
}

double seal_strconv_f64_from_bits(
    uint64_t bits
) {
    return seal_strconv_f64_from_raw_bits(
        bits
    );
}
