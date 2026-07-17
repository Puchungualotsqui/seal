#ifndef _WIN32
#define _GNU_SOURCE
#define _FILE_OFFSET_BITS 64
#define _POSIX_C_SOURCE 200809L
#endif

#include <stdio.h>
#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>
#include <limits.h>
#include <sys/stat.h>

#ifdef _WIN32

#define WIN32_LEAN_AND_MEAN

#include <windows.h>
#include <io.h>
#include <fcntl.h>
#include <direct.h>
#include <process.h>

#ifndef _O_BINARY
#define _O_BINARY 0
#endif

/*
Older Windows SDK headers, including those bundled with some TCC
versions, may not expose these conversion flags.

Passing zero keeps the conversions available, but invalid input may be
replaced by the Windows default replacement character instead of making
the conversion fail.
*/
#ifndef MB_ERR_INVALID_CHARS
#define MB_ERR_INVALID_CHARS 0
#endif

#ifndef WC_ERR_INVALID_CHARS
#define WC_ERR_INVALID_CHARS 0
#endif

/*
TCC's Windows errno.h may omit EOVERFLOW. This value only needs to be
distinct within the native bridge so it can map to ErrorKind.Overflow.
*/
#ifndef EOVERFLOW
#define EOVERFLOW (INT_MAX - 1)
#endif

#else

#include <unistd.h>
#include <fcntl.h>
#include <dirent.h>
#include <sys/types.h>

#ifdef __APPLE__
#include <mach-o/dyld.h>
#include <crt_externs.h>
#endif

#endif

/*
Some C runtimes, including TCC's Windows runtime, do not define
ENOTSUP. Select the closest available errno value while keeping the
portable Seal classification as Unsupported.
*/
#if defined(ENOTSUP)
#define SEAL_OS_NATIVE_UNSUPPORTED ENOTSUP
#elif defined(EOPNOTSUPP)
#define SEAL_OS_NATIVE_UNSUPPORTED EOPNOTSUPP
#elif defined(ENOSYS)
#define SEAL_OS_NATIVE_UNSUPPORTED ENOSYS
#else
#define SEAL_OS_NATIVE_UNSUPPORTED INT_MAX
#endif

typedef struct sealString {
    const uint8_t *data;
    uintptr_t len;
} sealString;

enum {
    SEAL_OS_ERROR_NONE = 0,
    SEAL_OS_ERROR_NOT_FOUND = 1,
    SEAL_OS_ERROR_ALREADY_EXISTS = 2,
    SEAL_OS_ERROR_PERMISSION_DENIED = 3,
    SEAL_OS_ERROR_INVALID_ARGUMENT = 4,
    SEAL_OS_ERROR_INVALID_PATH = 5,
    SEAL_OS_ERROR_NOT_DIRECTORY = 6,
    SEAL_OS_ERROR_IS_DIRECTORY = 7,
    SEAL_OS_ERROR_DIRECTORY_NOT_EMPTY = 8,
    SEAL_OS_ERROR_INTERRUPTED = 9,
    SEAL_OS_ERROR_OUT_OF_MEMORY = 10,
    SEAL_OS_ERROR_INPUT_OUTPUT = 11,
    SEAL_OS_ERROR_OVERFLOW = 12,
    SEAL_OS_ERROR_UNSUPPORTED = 13,
    SEAL_OS_ERROR_UNKNOWN = 14
};

enum {
    SEAL_OS_FILE_REGULAR = 0,
    SEAL_OS_FILE_DIRECTORY = 1,
    SEAL_OS_FILE_SYMLINK = 2,
    SEAL_OS_FILE_OTHER = 3
};

static void seal_os_set_error(
    intptr_t *output,
    int value
) {
    if (output != NULL) {
        *output = (intptr_t)value;
    }
}

static void seal_os_clear_error(
    intptr_t *output
) {
    seal_os_set_error(
        output,
        0
    );
}

static bool seal_os_contains_null(
    sealString value
) {
    if (value.len == 0) {
        return false;
    }

    if (value.data == NULL) {
        return true;
    }

    return memchr(
        value.data,
        '\0',
        (size_t)value.len
    ) != NULL;
}

static void *seal_os_copy_bytes(
    const void *source,
    size_t length
) {
    size_t allocation_size =
        length == 0
            ? 1
            : length;

    void *result =
        malloc(allocation_size);

    if (result == NULL) {
        return NULL;
    }

    if (length > 0) {
        memcpy(
            result,
            source,
            length
        );
    }

    return result;
}

#ifndef _WIN32

static char *seal_os_copy_string(
    sealString value,
    int *error
) {
    if (error != NULL) {
        *error = 0;
    }

    if (seal_os_contains_null(value)) {
        if (error != NULL) {
            *error = EINVAL;
        }

        return NULL;
    }

    if (value.len > SIZE_MAX - 1) {
        if (error != NULL) {
            *error = ENAMETOOLONG;
        }

        return NULL;
    }

    size_t length =
        (size_t)value.len;

    char *result =
        malloc(length + 1);

    if (result == NULL) {
        if (error != NULL) {
            *error = ENOMEM;
        }

        return NULL;
    }

    if (length > 0) {
        memcpy(
            result,
            value.data,
            length
        );
    }

    result[length] = '\0';

    return result;
}

#else

static int seal_os_errno_from_win32(
    DWORD code
) {
    switch (code) {
    case ERROR_SUCCESS:
        return 0;

    case ERROR_FILE_NOT_FOUND:
    case ERROR_PATH_NOT_FOUND:
    case ERROR_ENVVAR_NOT_FOUND:
        return ENOENT;

    case ERROR_FILE_EXISTS:
    case ERROR_ALREADY_EXISTS:
        return EEXIST;

    case ERROR_ACCESS_DENIED:
    case ERROR_PRIVILEGE_NOT_HELD:
    case ERROR_SHARING_VIOLATION:
    case ERROR_LOCK_VIOLATION:
        return EACCES;

    case ERROR_INVALID_PARAMETER:
        return EINVAL;

    case ERROR_INVALID_NAME:
    case ERROR_BAD_PATHNAME:
    case ERROR_FILENAME_EXCED_RANGE:
        return ENAMETOOLONG;

    case ERROR_DIRECTORY:
        return ENOTDIR;

    case ERROR_DIR_NOT_EMPTY:
        return ENOTEMPTY;

    case ERROR_NOT_ENOUGH_MEMORY:
    case ERROR_OUTOFMEMORY:
        return ENOMEM;

    case ERROR_OPERATION_ABORTED:
        return EINTR;

    case ERROR_NOT_SUPPORTED:
    case ERROR_CALL_NOT_IMPLEMENTED:
        return SEAL_OS_NATIVE_UNSUPPORTED;

    default:
        return EIO;
    }
}

static wchar_t *seal_os_utf8_to_wide(
    sealString value,
    int *error
) {
    if (error != NULL) {
        *error = 0;
    }

    if (seal_os_contains_null(value)) {
        if (error != NULL) {
            *error = EINVAL;
        }

        return NULL;
    }

    if (value.len > INT_MAX) {
        if (error != NULL) {
            *error = ENAMETOOLONG;
        }

        return NULL;
    }

    if (value.len == 0) {
        wchar_t *empty =
            malloc(sizeof(wchar_t));

        if (empty == NULL) {
            if (error != NULL) {
                *error = ENOMEM;
            }

            return NULL;
        }

        empty[0] = L'\0';

        return empty;
    }

    int required =
        MultiByteToWideChar(
            CP_UTF8,
            MB_ERR_INVALID_CHARS,
            (const char *)value.data,
            (int)value.len,
            NULL,
            0
        );

    if (required <= 0) {
        if (error != NULL) {
            *error =
                seal_os_errno_from_win32(
                    GetLastError()
                );
        }

        return NULL;
    }

    if ((size_t)required >
        SIZE_MAX / sizeof(wchar_t) - 1) {
        if (error != NULL) {
            *error = ENAMETOOLONG;
        }

        return NULL;
    }

    wchar_t *result =
        malloc(
            ((size_t)required + 1) *
            sizeof(wchar_t)
        );

    if (result == NULL) {
        if (error != NULL) {
            *error = ENOMEM;
        }

        return NULL;
    }

    int written =
        MultiByteToWideChar(
            CP_UTF8,
            MB_ERR_INVALID_CHARS,
            (const char *)value.data,
            (int)value.len,
            result,
            required
        );

    if (written != required) {
        int conversion_error =
            seal_os_errno_from_win32(
                GetLastError()
            );

        free(result);

        if (error != NULL) {
            *error = conversion_error;
        }

        return NULL;
    }

    result[required] = L'\0';

    return result;
}

static bool seal_os_wide_to_utf8(
    const wchar_t *value,
    size_t length,
    void **output_data,
    uintptr_t *output_length,
    int *error
) {
    if (output_data != NULL) {
        *output_data = NULL;
    }

    if (output_length != NULL) {
        *output_length = 0;
    }

    if (error != NULL) {
        *error = 0;
    }

    if (length > INT_MAX) {
        if (error != NULL) {
            *error = ENAMETOOLONG;
        }

        return false;
    }

    if (length == 0) {
        void *empty =
            seal_os_copy_bytes(
                NULL,
                0
            );

        if (empty == NULL) {
            if (error != NULL) {
                *error = ENOMEM;
            }

            return false;
        }

        if (output_data != NULL) {
            *output_data = empty;
        }

        return true;
    }

    int required =
        WideCharToMultiByte(
            CP_UTF8,
            WC_ERR_INVALID_CHARS,
            value,
            (int)length,
            NULL,
            0,
            NULL,
            NULL
        );

    if (required <= 0) {
        if (error != NULL) {
            *error =
                seal_os_errno_from_win32(
                    GetLastError()
                );
        }

        return false;
    }

    void *data =
        malloc((size_t)required);

    if (data == NULL) {
        if (error != NULL) {
            *error = ENOMEM;
        }

        return false;
    }

    int written =
        WideCharToMultiByte(
            CP_UTF8,
            WC_ERR_INVALID_CHARS,
            value,
            (int)length,
            (char *)data,
            required,
            NULL,
            NULL
        );

    if (written != required) {
        int conversion_error =
            seal_os_errno_from_win32(
                GetLastError()
            );

        free(data);

        if (error != NULL) {
            *error = conversion_error;
        }

        return false;
    }

    if (output_data != NULL) {
        *output_data = data;
    }

    if (output_length != NULL) {
        *output_length =
            (uintptr_t)required;
    }

    return true;
}

/*
Windows command-line parsing compatible with CommandLineToArgvW rules.

The returned array and all argument strings are stored in one allocation.
Release it with free().
*/
static wchar_t **seal_os_windows_arguments(
    int *output_count,
    int *error
) {
    if (output_count != NULL) {
        *output_count = 0;
    }

    if (error != NULL) {
        *error = 0;
    }

    const wchar_t *command_line =
        GetCommandLineW();

    if (command_line == NULL) {
        if (error != NULL) {
            *error =
                seal_os_errno_from_win32(
                    GetLastError()
                );
        }

        return NULL;
    }

    size_t command_length =
        wcslen(command_line);

    if (command_length >
        (SIZE_MAX / sizeof(wchar_t *)) - 1) {
        if (error != NULL) {
            *error = EOVERFLOW;
        }

        return NULL;
    }

    /*
    There cannot be more arguments than UTF-16 code units plus one.
    Store the pointer table and copied argument text in one allocation.
    */
    size_t pointer_capacity =
        command_length + 1;

    if (pointer_capacity >
        SIZE_MAX / sizeof(wchar_t *)) {
        if (error != NULL) {
            *error = EOVERFLOW;
        }

        return NULL;
    }

    size_t pointer_bytes =
        pointer_capacity *
        sizeof(wchar_t *);

    if (command_length >
        (SIZE_MAX / sizeof(wchar_t)) - 1) {
        if (error != NULL) {
            *error = EOVERFLOW;
        }

        return NULL;
    }

    size_t text_bytes =
        (command_length + 1) *
        sizeof(wchar_t);

    if (pointer_bytes >
        SIZE_MAX - text_bytes) {
        if (error != NULL) {
            *error = EOVERFLOW;
        }

        return NULL;
    }

    unsigned char *allocation =
        malloc(
            pointer_bytes +
            text_bytes
        );

    if (allocation == NULL) {
        if (error != NULL) {
            *error = ENOMEM;
        }

        return NULL;
    }

    wchar_t **arguments =
        (wchar_t **)allocation;

    wchar_t *text =
        (wchar_t *)(
            allocation +
            pointer_bytes
        );

    const wchar_t *read =
        command_line;

    wchar_t *write =
        text;

    int count = 0;

    while (*read != L'\0') {
        while (*read == L' ' ||
               *read == L'\t') {
            read++;
        }

        if (*read == L'\0') {
            break;
        }

        arguments[count++] =
            write;

        bool quoted = false;

        for (;;) {
            size_t slash_count = 0;

            while (*read == L'\\') {
                slash_count++;
                read++;
            }

            if (*read == L'"') {
                size_t literal_slashes =
                    slash_count / 2;

                for (size_t index = 0;
                     index < literal_slashes;
                     index++) {
                    *write++ = L'\\';
                }

                if ((slash_count % 2) != 0) {
                    *write++ = L'"';
                    read++;
                    continue;
                }

                if (quoted &&
                    read[1] == L'"') {
                    *write++ = L'"';
                    read += 2;
                    continue;
                }

                quoted = !quoted;
                read++;
                continue;
            }

            for (size_t index = 0;
                 index < slash_count;
                 index++) {
                *write++ = L'\\';
            }

            if (*read == L'\0') {
                break;
            }

            if (!quoted &&
                (*read == L' ' ||
                 *read == L'\t')) {
                break;
            }

            *write++ =
                *read++;
        }

        *write++ = L'\0';

        while (*read == L' ' ||
               *read == L'\t') {
            read++;
        }
    }

    arguments[count] = NULL;

    if (output_count != NULL) {
        *output_count = count;
    }

    return arguments;
}

#endif

void seal_os_free_buffer(
    void *data
) {
    free(data);
}

intptr_t seal_os_classify_error(
    intptr_t native_code
) {
    int code =
        (int)native_code;

    if (code == 0) {
        return SEAL_OS_ERROR_NONE;
    }

    /*
    Test unsupported codes before the switch because ENOTSUP,
    EOPNOTSUPP, and ENOSYS may share the same integer value.
    */
    if (code ==
        SEAL_OS_NATIVE_UNSUPPORTED) {
        return SEAL_OS_ERROR_UNSUPPORTED;
    }

#ifdef ENOSYS
    if (code == ENOSYS) {
        return SEAL_OS_ERROR_UNSUPPORTED;
    }
#endif

#ifdef ENOTSUP
    if (code == ENOTSUP) {
        return SEAL_OS_ERROR_UNSUPPORTED;
    }
#endif

#ifdef EOPNOTSUPP
    if (code == EOPNOTSUPP) {
        return SEAL_OS_ERROR_UNSUPPORTED;
    }
#endif

    switch (code) {
    case ENOENT:
        return SEAL_OS_ERROR_NOT_FOUND;

    case EEXIST:
        return SEAL_OS_ERROR_ALREADY_EXISTS;

    case EACCES:
#ifdef EPERM
    case EPERM:
#endif
        return SEAL_OS_ERROR_PERMISSION_DENIED;

    case EINVAL:
        return SEAL_OS_ERROR_INVALID_ARGUMENT;

#ifdef ENAMETOOLONG
    case ENAMETOOLONG:
        return SEAL_OS_ERROR_INVALID_PATH;
#endif

#ifdef ENOTDIR
    case ENOTDIR:
        return SEAL_OS_ERROR_NOT_DIRECTORY;
#endif

#ifdef EISDIR
    case EISDIR:
        return SEAL_OS_ERROR_IS_DIRECTORY;
#endif

#ifdef ENOTEMPTY
    case ENOTEMPTY:
        return SEAL_OS_ERROR_DIRECTORY_NOT_EMPTY;
#endif

    case EINTR:
        return SEAL_OS_ERROR_INTERRUPTED;

    case ENOMEM:
        return SEAL_OS_ERROR_OUT_OF_MEMORY;

#ifdef EIO
    case EIO:
        return SEAL_OS_ERROR_INPUT_OUTPUT;
#endif

#ifdef EOVERFLOW
    case EOVERFLOW:
        return SEAL_OS_ERROR_OVERFLOW;
#endif

    default:
        return SEAL_OS_ERROR_UNKNOWN;
    }
}

/*
Process arguments
*/

#ifdef __linux__

static bool seal_os_linux_command_line(
    char **output_data,
    size_t *output_length,
    size_t *output_count,
    int *error
) {
    if (output_data != NULL) {
        *output_data = NULL;
    }

    if (output_length != NULL) {
        *output_length = 0;
    }

    if (output_count != NULL) {
        *output_count = 0;
    }

    FILE *file =
        fopen(
            "/proc/self/cmdline",
            "rb"
        );

    if (file == NULL) {
        if (error != NULL) {
            *error = errno;
        }

        return false;
    }

    size_t capacity = 256;
    size_t length = 0;

    char *data =
        malloc(capacity);

    if (data == NULL) {
        fclose(file);

        if (error != NULL) {
            *error = ENOMEM;
        }

        return false;
    }

    for (;;) {
        if (length == capacity) {
            if (capacity > SIZE_MAX / 2) {
                free(data);
                fclose(file);

                if (error != NULL) {
                    *error = EOVERFLOW;
                }

                return false;
            }

            size_t new_capacity =
                capacity * 2;

            char *grown =
                realloc(
                    data,
                    new_capacity
                );

            if (grown == NULL) {
                free(data);
                fclose(file);

                if (error != NULL) {
                    *error = ENOMEM;
                }

                return false;
            }

            data = grown;
            capacity = new_capacity;
        }

        size_t read_count =
            fread(
                data + length,
                1,
                capacity - length,
                file
            );

        length += read_count;

        if (read_count == 0) {
            if (ferror(file)) {
                int read_error =
                    errno != 0
                        ? errno
                        : EIO;

                free(data);
                fclose(file);

                if (error != NULL) {
                    *error = read_error;
                }

                return false;
            }

            break;
        }
    }

    fclose(file);

    if (length > 0 &&
        data[length - 1] != '\0') {
        if (length == capacity) {
            char *grown =
                realloc(
                    data,
                    capacity + 1
                );

            if (grown == NULL) {
                free(data);

                if (error != NULL) {
                    *error = ENOMEM;
                }

                return false;
            }

            data = grown;
        }

        data[length++] = '\0';
    }

    size_t count = 0;

    for (size_t index = 0;
         index < length;
         index++) {
        if (data[index] == '\0') {
            count++;
        }
    }

    if (output_data != NULL) {
        *output_data = data;
    }

    if (output_length != NULL) {
        *output_length = length;
    }

    if (output_count != NULL) {
        *output_count = count;
    }

    if (error != NULL) {
        *error = 0;
    }

    return true;
}

#endif

bool seal_os_argument_count(
    uintptr_t *output_count,
    intptr_t *output_error
) {
    if (output_count != NULL) {
        *output_count = 0;
    }

    seal_os_clear_error(
        output_error
    );

#ifdef _WIN32

    int count = 0;
    int native_error = 0;

    wchar_t **arguments =
        seal_os_windows_arguments(
            &count,
            &native_error
        );

    if (arguments == NULL) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    free(arguments);

    if (output_count != NULL) {
        *output_count =
            (uintptr_t)count;
    }

    return true;

#elif defined(__linux__)

    char *data = NULL;
    size_t length = 0;
    size_t count = 0;
    int error = 0;

    bool success =
        seal_os_linux_command_line(
            &data,
            &length,
            &count,
            &error
        );

    free(data);

    if (!success) {
        seal_os_set_error(
            output_error,
            error
        );

        return false;
    }

    if (output_count != NULL) {
        *output_count =
            (uintptr_t)count;
    }

    return true;

#elif defined(__APPLE__)

    int count =
        *_NSGetArgc();

    if (count < 0) {
        seal_os_set_error(
            output_error,
            EIO
        );

        return false;
    }

    if (output_count != NULL) {
        *output_count =
            (uintptr_t)count;
    }

    return true;

#else

    seal_os_set_error(
        output_error,
        SEAL_OS_NATIVE_UNSUPPORTED
    );

    return false;

#endif
}

bool seal_os_argument(
    uintptr_t index,
    void **output_data,
    uintptr_t *output_length,
    bool *output_found,
    intptr_t *output_error
) {
    if (output_data != NULL) {
        *output_data = NULL;
    }

    if (output_length != NULL) {
        *output_length = 0;
    }

    if (output_found != NULL) {
        *output_found = false;
    }

    seal_os_clear_error(
        output_error
    );

#ifdef _WIN32

    int count = 0;
    int native_error = 0;

    wchar_t **arguments =
        seal_os_windows_arguments(
            &count,
            &native_error
        );

    if (arguments == NULL) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    if (index >= (uintptr_t)count) {
        free(arguments);
        return true;
    }

    const wchar_t *argument =
        arguments[index];

    size_t length =
        wcslen(argument);

    int conversion_error = 0;

    bool converted =
        seal_os_wide_to_utf8(
            argument,
            length,
            output_data,
            output_length,
            &conversion_error
        );

    free(arguments);

    if (!converted) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    if (output_found != NULL) {
        *output_found = true;
    }

    return true;

#elif defined(__linux__)

    char *data = NULL;
    size_t length = 0;
    size_t count = 0;
    int error = 0;

    if (!seal_os_linux_command_line(
            &data,
            &length,
            &count,
            &error
        )) {
        seal_os_set_error(
            output_error,
            error
        );

        return false;
    }

    if (index >= (uintptr_t)count) {
        free(data);
        return true;
    }

    size_t current = 0;
    size_t offset = 0;

    while (current < (size_t)index &&
           offset < length) {
        while (offset < length &&
               data[offset] != '\0') {
            offset++;
        }

        if (offset < length) {
            offset++;
        }

        current++;
    }

    size_t argument_length = 0;

    while (offset + argument_length < length &&
           data[offset + argument_length] != '\0') {
        argument_length++;
    }

    void *copy =
        seal_os_copy_bytes(
            data + offset,
            argument_length
        );

    free(data);

    if (copy == NULL) {
        seal_os_set_error(
            output_error,
            ENOMEM
        );

        return false;
    }

    if (output_data != NULL) {
        *output_data = copy;
    }

    if (output_length != NULL) {
        *output_length =
            (uintptr_t)argument_length;
    }

    if (output_found != NULL) {
        *output_found = true;
    }

    return true;

#elif defined(__APPLE__)

    int count =
        *_NSGetArgc();

    char **arguments =
        *_NSGetArgv();

    if (count < 0 ||
        arguments == NULL) {
        seal_os_set_error(
            output_error,
            EIO
        );

        return false;
    }

    if (index >= (uintptr_t)count) {
        return true;
    }

    const char *argument =
        arguments[index];

    size_t length =
        strlen(argument);

    void *copy =
        seal_os_copy_bytes(
            argument,
            length
        );

    if (copy == NULL) {
        seal_os_set_error(
            output_error,
            ENOMEM
        );

        return false;
    }

    if (output_data != NULL) {
        *output_data = copy;
    }

    if (output_length != NULL) {
        *output_length =
            (uintptr_t)length;
    }

    if (output_found != NULL) {
        *output_found = true;
    }

    return true;

#else

    seal_os_set_error(
        output_error,
        SEAL_OS_NATIVE_UNSUPPORTED
    );

    return false;

#endif
}

/*
Environment
*/

bool seal_os_get_environment(
    sealString name,
    void **output_data,
    uintptr_t *output_length,
    bool *output_found,
    intptr_t *output_error
) {
    if (output_data != NULL) {
        *output_data = NULL;
    }

    if (output_length != NULL) {
        *output_length = 0;
    }

    if (output_found != NULL) {
        *output_found = false;
    }

    seal_os_clear_error(
        output_error
    );

#ifdef _WIN32

    int conversion_error = 0;

    wchar_t *wide_name =
        seal_os_utf8_to_wide(
            name,
            &conversion_error
        );

    if (wide_name == NULL) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    SetLastError(ERROR_SUCCESS);

    DWORD required =
        GetEnvironmentVariableW(
            wide_name,
            NULL,
            0
        );

    if (required == 0) {
        DWORD native_error =
            GetLastError();

        free(wide_name);

        if (native_error ==
            ERROR_ENVVAR_NOT_FOUND) {
            return true;
        }

        if (native_error ==
            ERROR_SUCCESS) {
            void *empty =
                seal_os_copy_bytes(
                    NULL,
                    0
                );

            if (empty == NULL) {
                seal_os_set_error(
                    output_error,
                    ENOMEM
                );

                return false;
            }

            if (output_data != NULL) {
                *output_data = empty;
            }

            if (output_found != NULL) {
                *output_found = true;
            }

            return true;
        }

        seal_os_set_error(
            output_error,
            seal_os_errno_from_win32(
                native_error
            )
        );

        return false;
    }

    wchar_t *wide_value =
        malloc(
            (size_t)required *
            sizeof(wchar_t)
        );

    if (wide_value == NULL) {
        free(wide_name);

        seal_os_set_error(
            output_error,
            ENOMEM
        );

        return false;
    }

    DWORD written =
        GetEnvironmentVariableW(
            wide_name,
            wide_value,
            required
        );

    free(wide_name);

    if (written >= required) {
        int native_error =
            seal_os_errno_from_win32(
                GetLastError()
            );

        free(wide_value);

        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    bool converted =
        seal_os_wide_to_utf8(
            wide_value,
            (size_t)written,
            output_data,
            output_length,
            &conversion_error
        );

    free(wide_value);

    if (!converted) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    if (output_found != NULL) {
        *output_found = true;
    }

    return true;

#else

    int conversion_error = 0;

    char *native_name =
        seal_os_copy_string(
            name,
            &conversion_error
        );

    if (native_name == NULL) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    const char *value =
        getenv(native_name);

    free(native_name);

    if (value == NULL) {
        return true;
    }

    size_t length =
        strlen(value);

    void *copy =
        seal_os_copy_bytes(
            value,
            length
        );

    if (copy == NULL) {
        seal_os_set_error(
            output_error,
            ENOMEM
        );

        return false;
    }

    if (output_data != NULL) {
        *output_data = copy;
    }

    if (output_length != NULL) {
        *output_length =
            (uintptr_t)length;
    }

    if (output_found != NULL) {
        *output_found = true;
    }

    return true;

#endif
}

bool seal_os_set_environment(
    sealString name,
    sealString value,
    intptr_t *output_error
) {
    seal_os_clear_error(
        output_error
    );

#ifdef _WIN32

    int name_error = 0;
    int value_error = 0;

    wchar_t *wide_name =
        seal_os_utf8_to_wide(
            name,
            &name_error
        );

    if (wide_name == NULL) {
        seal_os_set_error(
            output_error,
            name_error
        );

        return false;
    }

    wchar_t *wide_value =
        seal_os_utf8_to_wide(
            value,
            &value_error
        );

    if (wide_value == NULL) {
        free(wide_name);

        seal_os_set_error(
            output_error,
            value_error
        );

        return false;
    }

    BOOL success =
        SetEnvironmentVariableW(
            wide_name,
            wide_value
        );

    int native_error =
        success
            ? 0
            : seal_os_errno_from_win32(
                GetLastError()
            );

    free(wide_name);
    free(wide_value);

    if (!success) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    return true;

#else

    int name_error = 0;
    int value_error = 0;

    char *native_name =
        seal_os_copy_string(
            name,
            &name_error
        );

    if (native_name == NULL) {
        seal_os_set_error(
            output_error,
            name_error
        );

        return false;
    }

    char *native_value =
        seal_os_copy_string(
            value,
            &value_error
        );

    if (native_value == NULL) {
        free(native_name);

        seal_os_set_error(
            output_error,
            value_error
        );

        return false;
    }

    int result =
        setenv(
            native_name,
            native_value,
            1
        );

    int native_error =
        result == 0
            ? 0
            : errno;

    free(native_name);
    free(native_value);

    if (result != 0) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    return true;

#endif
}

bool seal_os_unset_environment(
    sealString name,
    intptr_t *output_error
) {
    seal_os_clear_error(
        output_error
    );

#ifdef _WIN32

    int conversion_error = 0;

    wchar_t *wide_name =
        seal_os_utf8_to_wide(
            name,
            &conversion_error
        );

    if (wide_name == NULL) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    BOOL success =
        SetEnvironmentVariableW(
            wide_name,
            NULL
        );

    int native_error =
        success
            ? 0
            : seal_os_errno_from_win32(
                GetLastError()
            );

    free(wide_name);

    if (!success &&
        native_error != ENOENT) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    return true;

#else

    int conversion_error = 0;

    char *native_name =
        seal_os_copy_string(
            name,
            &conversion_error
        );

    if (native_name == NULL) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    int result =
        unsetenv(native_name);

    int native_error =
        result == 0
            ? 0
            : errno;

    free(native_name);

    if (result != 0) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    return true;

#endif
}

/*
Working directory
*/

bool seal_os_get_working_directory(
    void **output_data,
    uintptr_t *output_length,
    intptr_t *output_error
) {
    if (output_data != NULL) {
        *output_data = NULL;
    }

    if (output_length != NULL) {
        *output_length = 0;
    }

    seal_os_clear_error(
        output_error
    );

#ifdef _WIN32

    DWORD required =
        GetCurrentDirectoryW(
            0,
            NULL
        );

    if (required == 0) {
        seal_os_set_error(
            output_error,
            seal_os_errno_from_win32(
                GetLastError()
            )
        );

        return false;
    }

    wchar_t *buffer =
        malloc(
            (size_t)required *
            sizeof(wchar_t)
        );

    if (buffer == NULL) {
        seal_os_set_error(
            output_error,
            ENOMEM
        );

        return false;
    }

    DWORD written =
        GetCurrentDirectoryW(
            required,
            buffer
        );

    if (written == 0 ||
        written >= required) {
        int native_error =
            seal_os_errno_from_win32(
                GetLastError()
            );

        free(buffer);

        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    int conversion_error = 0;

    bool converted =
        seal_os_wide_to_utf8(
            buffer,
            (size_t)written,
            output_data,
            output_length,
            &conversion_error
        );

    free(buffer);

    if (!converted) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    return true;

#else

    size_t capacity = 256;

    for (;;) {
        char *buffer =
            malloc(capacity);

        if (buffer == NULL) {
            seal_os_set_error(
                output_error,
                ENOMEM
            );

            return false;
        }

        errno = 0;

        if (getcwd(
                buffer,
                capacity
            ) != NULL) {
            size_t length =
                strlen(buffer);

            void *copy =
                seal_os_copy_bytes(
                    buffer,
                    length
                );

            free(buffer);

            if (copy == NULL) {
                seal_os_set_error(
                    output_error,
                    ENOMEM
                );

                return false;
            }

            if (output_data != NULL) {
                *output_data = copy;
            }

            if (output_length != NULL) {
                *output_length =
                    (uintptr_t)length;
            }

            return true;
        }

        int native_error =
            errno;

        free(buffer);

        if (native_error != ERANGE) {
            seal_os_set_error(
                output_error,
                native_error
            );

            return false;
        }

        if (capacity > SIZE_MAX / 2) {
            seal_os_set_error(
                output_error,
                EOVERFLOW
            );

            return false;
        }

        capacity *= 2;
    }

#endif
}

bool seal_os_set_working_directory(
    sealString path,
    intptr_t *output_error
) {
    seal_os_clear_error(
        output_error
    );

#ifdef _WIN32

    int conversion_error = 0;

    wchar_t *native_path =
        seal_os_utf8_to_wide(
            path,
            &conversion_error
        );

    if (native_path == NULL) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    BOOL success =
        SetCurrentDirectoryW(
            native_path
        );

    int native_error =
        success
            ? 0
            : seal_os_errno_from_win32(
                GetLastError()
            );

    free(native_path);

    if (!success) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    return true;

#else

    int conversion_error = 0;

    char *native_path =
        seal_os_copy_string(
            path,
            &conversion_error
        );

    if (native_path == NULL) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    int result =
        chdir(native_path);

    int native_error =
        result == 0
            ? 0
            : errno;

    free(native_path);

    if (result != 0) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    return true;

#endif
}

/*
Files
*/

static const char *seal_os_stream_mode(
    bool readable,
    bool writable,
    bool append
) {
    if (readable && writable) {
        return append
            ? "a+b"
            : "r+b";
    }

    if (writable) {
        return append
            ? "ab"
            : "wb";
    }

    return "rb";
}

bool seal_os_open_file(
    sealString path,
    bool readable,
    bool writable,
    bool append,
    bool create,
    bool truncate,
    bool exclusive,
    void **output_handle,
    intptr_t *output_error
) {
    if (output_handle != NULL) {
        *output_handle = NULL;
    }

    seal_os_clear_error(
        output_error
    );

    if ((!readable && !writable) ||
        ((append ||
          create ||
          truncate ||
          exclusive) &&
         !writable) ||
        (exclusive && !create)) {
        seal_os_set_error(
            output_error,
            EINVAL
        );

        return false;
    }

    int flags = 0;

    if (readable && writable) {
        flags |= O_RDWR;
    } else if (writable) {
        flags |= O_WRONLY;
    } else {
        flags |= O_RDONLY;
    }

    if (append) {
        flags |= O_APPEND;
    }

    if (create) {
        flags |= O_CREAT;
    }

    if (truncate) {
        flags |= O_TRUNC;
    }

    if (exclusive) {
        flags |= O_EXCL;
    }

#ifdef _WIN32

    flags |= _O_BINARY;

    int conversion_error = 0;

    wchar_t *native_path =
        seal_os_utf8_to_wide(
            path,
            &conversion_error
        );

    if (native_path == NULL) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    int descriptor =
        _wopen(
            native_path,
            flags,
            _S_IREAD |
            _S_IWRITE
        );

    int open_error =
        descriptor >= 0
            ? 0
            : errno;

    free(native_path);

    if (descriptor < 0) {
        seal_os_set_error(
            output_error,
            open_error
        );

        return false;
    }

    FILE *stream =
        _fdopen(
            descriptor,
            seal_os_stream_mode(
                readable,
                writable,
                append
            )
        );

    if (stream == NULL) {
        int stream_error =
            errno;

        _close(descriptor);

        seal_os_set_error(
            output_error,
            stream_error
        );

        return false;
    }

#else

    int conversion_error = 0;

    char *native_path =
        seal_os_copy_string(
            path,
            &conversion_error
        );

    if (native_path == NULL) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    int descriptor =
        open(
            native_path,
            flags,
            0666
        );

    int open_error =
        descriptor >= 0
            ? 0
            : errno;

    free(native_path);

    if (descriptor < 0) {
        seal_os_set_error(
            output_error,
            open_error
        );

        return false;
    }

    FILE *stream =
        fdopen(
            descriptor,
            seal_os_stream_mode(
                readable,
                writable,
                append
            )
        );

    if (stream == NULL) {
        int stream_error =
            errno;

        close(descriptor);

        seal_os_set_error(
            output_error,
            stream_error
        );

        return false;
    }

#endif

    if (output_handle != NULL) {
        *output_handle = stream;
    }

    return true;
}

bool seal_os_read_file_stream(
    void *handle,
    void *destination,
    uintptr_t byte_length,
    uintptr_t *output_count,
    bool *output_ended,
    intptr_t *output_error
) {
    if (output_count != NULL) {
        *output_count = 0;
    }

    if (output_ended != NULL) {
        *output_ended = false;
    }

    seal_os_clear_error(
        output_error
    );

    if (handle == NULL ||
        (byte_length > 0 &&
         destination == NULL)) {
        seal_os_set_error(
            output_error,
            EINVAL
        );

        return false;
    }

    if (byte_length == 0) {
        return true;
    }

    FILE *stream =
        (FILE *)handle;

    clearerr(stream);

    size_t count =
        fread(
            destination,
            1,
            (size_t)byte_length,
            stream
        );

    if (count > 0) {
        if (output_count != NULL) {
            *output_count =
                (uintptr_t)count;
        }

        return true;
    }

    if (feof(stream)) {
        if (output_ended != NULL) {
            *output_ended = true;
        }

        return true;
    }

    int native_error =
        errno != 0
            ? errno
            : EIO;

    seal_os_set_error(
        output_error,
        native_error
    );

    return false;
}

bool seal_os_write_file_stream(
    void *handle,
    void *source,
    uintptr_t byte_length,
    uintptr_t *output_count,
    intptr_t *output_error
) {
    if (output_count != NULL) {
        *output_count = 0;
    }

    seal_os_clear_error(
        output_error
    );

    if (handle == NULL ||
        (byte_length > 0 &&
         source == NULL)) {
        seal_os_set_error(
            output_error,
            EINVAL
        );

        return false;
    }

    if (byte_length == 0) {
        return true;
    }

    FILE *stream =
        (FILE *)handle;

    clearerr(stream);

    size_t count =
        fwrite(
            source,
            1,
            (size_t)byte_length,
            stream
        );

    if (count > 0) {
        if (output_count != NULL) {
            *output_count =
                (uintptr_t)count;
        }

        return true;
    }

    int native_error =
        errno != 0
            ? errno
            : EIO;

    seal_os_set_error(
        output_error,
        native_error
    );

    return false;
}

bool seal_os_flush_file_stream(
    void *handle,
    intptr_t *output_error
) {
    seal_os_clear_error(
        output_error
    );

    if (handle == NULL) {
        seal_os_set_error(
            output_error,
            EINVAL
        );

        return false;
    }

    if (fflush((FILE *)handle) != 0) {
        seal_os_set_error(
            output_error,
            errno != 0
                ? errno
                : EIO
        );

        return false;
    }

    return true;
}

bool seal_os_close_file_stream(
    void *handle,
    intptr_t *output_error
) {
    seal_os_clear_error(
        output_error
    );

    if (handle == NULL) {
        return true;
    }

    if (fclose((FILE *)handle) != 0) {
        seal_os_set_error(
            output_error,
            errno != 0
                ? errno
                : EIO
        );

        return false;
    }

    return true;
}

bool seal_os_seek_file_stream(
    void *handle,
    int64_t offset,
    intptr_t origin,
    uint64_t *output_position,
    intptr_t *output_error
) {
    if (output_position != NULL) {
        *output_position = 0;
    }

    seal_os_clear_error(
        output_error
    );

    if (handle == NULL ||
        origin < 0 ||
        origin > 2) {
        seal_os_set_error(
            output_error,
            EINVAL
        );

        return false;
    }

    int native_origin =
        origin == 0
            ? SEEK_SET
            : origin == 1
                ? SEEK_CUR
                : SEEK_END;

    FILE *stream =
        (FILE *)handle;

    #ifdef _WIN32

        /*
        TCC's runtime may not export _ftelli64. Use the Windows file handle
        directly after synchronizing the C stream.
        */
        if (fflush(stream) != 0) {
            seal_os_set_error(
                output_error,
                errno != 0
                    ? errno
                    : EIO
            );

            return false;
        }

        int descriptor =
            _fileno(stream);

        if (descriptor < 0) {
            seal_os_set_error(
                output_error,
                errno != 0
                    ? errno
                    : EIO
            );

            return false;
        }

        intptr_t native_handle_value =
            _get_osfhandle(
                descriptor
            );

        if (native_handle_value == -1) {
            seal_os_set_error(
                output_error,
                errno != 0
                    ? errno
                    : EIO
            );

            return false;
        }

        HANDLE native_handle =
            (HANDLE)native_handle_value;

        LARGE_INTEGER distance;
        LARGE_INTEGER position;

        distance.QuadPart =
            offset;

        DWORD move_method =
            origin == 0
                ? FILE_BEGIN
                : origin == 1
                    ? FILE_CURRENT
                    : FILE_END;

        if (!SetFilePointerEx(
                native_handle,
                distance,
                &position,
                move_method
            )) {
            seal_os_set_error(
                output_error,
                seal_os_errno_from_win32(
                    GetLastError()
                )
            );

            return false;
        }

        if (position.QuadPart < 0) {
            seal_os_set_error(
                output_error,
                EINVAL
            );

            return false;
        }

        /*
        Reset the C stream state after moving its underlying native handle.
        */
        clearerr(stream);

        if (output_position != NULL) {
            *output_position =
                (uint64_t)
                    position.QuadPart;
        }

    #else

    if (fseeko(
            stream,
            (off_t)offset,
            native_origin
        ) != 0) {
        seal_os_set_error(
            output_error,
            errno != 0
                ? errno
                : EIO
        );

        return false;
    }

    off_t position =
        ftello(stream);

    if (position < 0) {
        seal_os_set_error(
            output_error,
            errno != 0
                ? errno
                : EIO
        );

        return false;
    }

    if (output_position != NULL) {
        *output_position =
            (uint64_t)position;
    }

#endif

    return true;
}

void *seal_os_standard_input(void) {
    return stdin;
}

void *seal_os_standard_output(void) {
    return stdout;
}

void *seal_os_standard_error(void) {
    return stderr;
}

/*
Metadata
*/

#ifdef _WIN32

static int64_t seal_os_filetime_to_unix_seconds(
    FILETIME value
) {
    ULARGE_INTEGER ticks;

    ticks.LowPart =
        value.dwLowDateTime;

    ticks.HighPart =
        value.dwHighDateTime;

    const uint64_t epoch_difference =
        UINT64_C(116444736000000000);

    if (ticks.QuadPart <
        epoch_difference) {
        return 0;
    }

    return (int64_t)(
        (ticks.QuadPart -
         epoch_difference) /
        UINT64_C(10000000)
    );
}

#endif

bool seal_os_stat(
    sealString path,
    bool follow_symlinks,
    intptr_t *output_type,
    uint64_t *output_size,
    int64_t *output_modified,
    bool *output_read_only,
    intptr_t *output_error
) {
    if (output_type != NULL) {
        *output_type =
            SEAL_OS_FILE_OTHER;
    }

    if (output_size != NULL) {
        *output_size = 0;
    }

    if (output_modified != NULL) {
        *output_modified = 0;
    }

    if (output_read_only != NULL) {
        *output_read_only = false;
    }

    seal_os_clear_error(
        output_error
    );

#ifdef _WIN32

    int conversion_error = 0;

    wchar_t *native_path =
        seal_os_utf8_to_wide(
            path,
            &conversion_error
        );

    if (native_path == NULL) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    DWORD flags =
        FILE_FLAG_BACKUP_SEMANTICS;

    if (!follow_symlinks) {
        flags |=
            FILE_FLAG_OPEN_REPARSE_POINT;
    }

    HANDLE handle =
        CreateFileW(
            native_path,
            FILE_READ_ATTRIBUTES,
            FILE_SHARE_READ |
            FILE_SHARE_WRITE |
            FILE_SHARE_DELETE,
            NULL,
            OPEN_EXISTING,
            flags,
            NULL
        );

    free(native_path);

    if (handle ==
        INVALID_HANDLE_VALUE) {
        seal_os_set_error(
            output_error,
            seal_os_errno_from_win32(
                GetLastError()
            )
        );

        return false;
    }

    BY_HANDLE_FILE_INFORMATION info;

    BOOL success =
        GetFileInformationByHandle(
            handle,
            &info
        );

    int native_error =
        success
            ? 0
            : seal_os_errno_from_win32(
                GetLastError()
            );

    CloseHandle(handle);

    if (!success) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    intptr_t file_type =
        SEAL_OS_FILE_OTHER;

    if (!follow_symlinks &&
        (info.dwFileAttributes &
         FILE_ATTRIBUTE_REPARSE_POINT) != 0) {
        file_type =
            SEAL_OS_FILE_SYMLINK;
    } else if (
        (info.dwFileAttributes &
         FILE_ATTRIBUTE_DIRECTORY) != 0
    ) {
        file_type =
            SEAL_OS_FILE_DIRECTORY;
    } else if (
        (info.dwFileAttributes &
         FILE_ATTRIBUTE_DEVICE) == 0
    ) {
        file_type =
            SEAL_OS_FILE_REGULAR;
    }

    uint64_t size =
        ((uint64_t)
            info.nFileSizeHigh << 32) |
        (uint64_t)
            info.nFileSizeLow;

    if (output_type != NULL) {
        *output_type =
            file_type;
    }

    if (output_size != NULL) {
        *output_size =
            size;
    }

    if (output_modified != NULL) {
        *output_modified =
            seal_os_filetime_to_unix_seconds(
                info.ftLastWriteTime
            );
    }

    if (output_read_only != NULL) {
        *output_read_only =
            (info.dwFileAttributes &
             FILE_ATTRIBUTE_READONLY) != 0;
    }

    return true;

#else

    int conversion_error = 0;

    char *native_path =
        seal_os_copy_string(
            path,
            &conversion_error
        );

    if (native_path == NULL) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    struct stat info;

    int result =
        follow_symlinks
            ? stat(native_path, &info)
            : lstat(native_path, &info);

    int native_error =
        result == 0
            ? 0
            : errno;

    free(native_path);

    if (result != 0) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    intptr_t file_type =
        SEAL_OS_FILE_OTHER;

    if (S_ISREG(info.st_mode)) {
        file_type =
            SEAL_OS_FILE_REGULAR;
    } else if (S_ISDIR(info.st_mode)) {
        file_type =
            SEAL_OS_FILE_DIRECTORY;
    } else if (S_ISLNK(info.st_mode)) {
        file_type =
            SEAL_OS_FILE_SYMLINK;
    }

    if (output_type != NULL) {
        *output_type =
            file_type;
    }

    if (output_size != NULL) {
        *output_size =
            info.st_size < 0
                ? 0
                : (uint64_t)info.st_size;
    }

    if (output_modified != NULL) {
        *output_modified =
            (int64_t)info.st_mtime;
    }

    if (output_read_only != NULL) {
        *output_read_only =
            (info.st_mode &
             S_IWUSR) == 0;
    }

    return true;

#endif
}

/*
Filesystem mutation
*/

#ifdef _WIN32

static bool seal_os_windows_is_directory(
    const wchar_t *path
) {
    DWORD attributes =
        GetFileAttributesW(path);

    return attributes !=
            INVALID_FILE_ATTRIBUTES &&
        (attributes &
         FILE_ATTRIBUTE_DIRECTORY) != 0;
}

static size_t seal_os_windows_root_length(
    const wchar_t *path,
    size_t length
) {
    if (length >= 3 &&
        path[1] == L':' &&
        (path[2] == L'\\' ||
         path[2] == L'/')) {
        return 3;
    }

    if (length >= 2 &&
        (path[0] == L'\\' ||
         path[0] == L'/') &&
        (path[1] == L'\\' ||
         path[1] == L'/')) {
        size_t index = 2;

        while (index < length &&
               path[index] != L'\\' &&
               path[index] != L'/') {
            index++;
        }

        if (index < length) {
            index++;
        }

        while (index < length &&
               path[index] != L'\\' &&
               path[index] != L'/') {
            index++;
        }

        if (index < length) {
            index++;
        }

        return index;
    }

    if (length > 0 &&
        (path[0] == L'\\' ||
         path[0] == L'/')) {
        return 1;
    }

    return 0;
}

#endif

bool seal_os_create_directory(
    sealString path,
    intptr_t *output_error
) {
    seal_os_clear_error(
        output_error
    );

#ifdef _WIN32

    int conversion_error = 0;

    wchar_t *native_path =
        seal_os_utf8_to_wide(
            path,
            &conversion_error
        );

    if (native_path == NULL) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    BOOL success =
        CreateDirectoryW(
            native_path,
            NULL
        );

    int native_error =
        success
            ? 0
            : seal_os_errno_from_win32(
                GetLastError()
            );

    free(native_path);

    if (!success) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    return true;

#else

    int conversion_error = 0;

    char *native_path =
        seal_os_copy_string(
            path,
            &conversion_error
        );

    if (native_path == NULL) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    int result =
        mkdir(
            native_path,
            0777
        );

    int native_error =
        result == 0
            ? 0
            : errno;

    free(native_path);

    if (result != 0) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    return true;

#endif
}

bool seal_os_create_directories(
    sealString path,
    intptr_t *output_error
) {
    seal_os_clear_error(
        output_error
    );

#ifdef _WIN32

    int conversion_error = 0;

    wchar_t *native_path =
        seal_os_utf8_to_wide(
            path,
            &conversion_error
        );

    if (native_path == NULL) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    size_t length =
        wcslen(native_path);

    if (length == 0) {
        free(native_path);

        seal_os_set_error(
            output_error,
            EINVAL
        );

        return false;
    }

    for (size_t index = 0;
         index < length;
         index++) {
        if (native_path[index] == L'/') {
            native_path[index] = L'\\';
        }
    }

    size_t root_length =
        seal_os_windows_root_length(
            native_path,
            length
        );

    for (size_t index = root_length;
         index < length;
         index++) {
        if (native_path[index] != L'\\') {
            continue;
        }

        if (index == 0 ||
            native_path[index - 1] ==
                L'\\') {
            continue;
        }

        wchar_t saved =
            native_path[index];

        native_path[index] = L'\0';

        if (!CreateDirectoryW(
                native_path,
                NULL
            )) {
            DWORD code =
                GetLastError();

            if (code != ERROR_ALREADY_EXISTS ||
                !seal_os_windows_is_directory(
                    native_path
                )) {
                int native_error =
                    seal_os_errno_from_win32(
                        code
                    );

                native_path[index] =
                    saved;

                free(native_path);

                seal_os_set_error(
                    output_error,
                    native_error
                );

                return false;
            }
        }

        native_path[index] =
            saved;
    }

    if (!CreateDirectoryW(
            native_path,
            NULL
        )) {
        DWORD code =
            GetLastError();

        if (code != ERROR_ALREADY_EXISTS ||
            !seal_os_windows_is_directory(
                native_path
            )) {
            int native_error =
                seal_os_errno_from_win32(
                    code
                );

            free(native_path);

            seal_os_set_error(
                output_error,
                native_error
            );

            return false;
        }
    }

    free(native_path);

    return true;

#else

    int conversion_error = 0;

    char *native_path =
        seal_os_copy_string(
            path,
            &conversion_error
        );

    if (native_path == NULL) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    size_t length =
        strlen(native_path);

    if (length == 0) {
        free(native_path);

        seal_os_set_error(
            output_error,
            EINVAL
        );

        return false;
    }

    for (size_t index = 1;
         index < length;
         index++) {
        if (native_path[index] != '/') {
            continue;
        }

        if (native_path[index - 1] ==
            '/') {
            continue;
        }

        char saved =
            native_path[index];

        native_path[index] = '\0';

        if (mkdir(
                native_path,
                0777
            ) != 0 &&
            errno != EEXIST) {
            int native_error =
                errno;

            native_path[index] =
                saved;

            free(native_path);

            seal_os_set_error(
                output_error,
                native_error
            );

            return false;
        }

        native_path[index] =
            saved;
    }

    if (mkdir(
            native_path,
            0777
        ) != 0 &&
        errno != EEXIST) {
        int native_error =
            errno;

        free(native_path);

        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    struct stat info;

    if (stat(
            native_path,
            &info
        ) != 0 ||
        !S_ISDIR(info.st_mode)) {
        int native_error =
            errno != 0
                ? errno
                : ENOTDIR;

        free(native_path);

        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    free(native_path);

    return true;

#endif
}

bool seal_os_remove_file(
    sealString path,
    intptr_t *output_error
) {
    seal_os_clear_error(
        output_error
    );

#ifdef _WIN32

    int conversion_error = 0;

    wchar_t *native_path =
        seal_os_utf8_to_wide(
            path,
            &conversion_error
        );

    if (native_path == NULL) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    BOOL success =
        DeleteFileW(native_path);

    int native_error =
        success
            ? 0
            : seal_os_errno_from_win32(
                GetLastError()
            );

    free(native_path);

    if (!success) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    return true;

#else

    int conversion_error = 0;

    char *native_path =
        seal_os_copy_string(
            path,
            &conversion_error
        );

    if (native_path == NULL) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    int result =
        unlink(native_path);

    int native_error =
        result == 0
            ? 0
            : errno;

    free(native_path);

    if (result != 0) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    return true;

#endif
}

bool seal_os_remove_directory(
    sealString path,
    intptr_t *output_error
) {
    seal_os_clear_error(
        output_error
    );

#ifdef _WIN32

    int conversion_error = 0;

    wchar_t *native_path =
        seal_os_utf8_to_wide(
            path,
            &conversion_error
        );

    if (native_path == NULL) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    BOOL success =
        RemoveDirectoryW(
            native_path
        );

    int native_error =
        success
            ? 0
            : seal_os_errno_from_win32(
                GetLastError()
            );

    free(native_path);

    if (!success) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    return true;

#else

    int conversion_error = 0;

    char *native_path =
        seal_os_copy_string(
            path,
            &conversion_error
        );

    if (native_path == NULL) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    int result =
        rmdir(native_path);

    int native_error =
        result == 0
            ? 0
            : errno;

    free(native_path);

    if (result != 0) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    return true;

#endif
}

bool seal_os_rename(
    sealString old_path,
    sealString new_path,
    intptr_t *output_error
) {
    seal_os_clear_error(
        output_error
    );

#ifdef _WIN32

    int old_error = 0;
    int new_error = 0;

    wchar_t *native_old =
        seal_os_utf8_to_wide(
            old_path,
            &old_error
        );

    if (native_old == NULL) {
        seal_os_set_error(
            output_error,
            old_error
        );

        return false;
    }

    wchar_t *native_new =
        seal_os_utf8_to_wide(
            new_path,
            &new_error
        );

    if (native_new == NULL) {
        free(native_old);

        seal_os_set_error(
            output_error,
            new_error
        );

        return false;
    }

    BOOL success =
        MoveFileExW(
            native_old,
            native_new,
            MOVEFILE_REPLACE_EXISTING
        );

    int native_error =
        success
            ? 0
            : seal_os_errno_from_win32(
                GetLastError()
            );

    free(native_old);
    free(native_new);

    if (!success) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    return true;

#else

    int old_error = 0;
    int new_error = 0;

    char *native_old =
        seal_os_copy_string(
            old_path,
            &old_error
        );

    if (native_old == NULL) {
        seal_os_set_error(
            output_error,
            old_error
        );

        return false;
    }

    char *native_new =
        seal_os_copy_string(
            new_path,
            &new_error
        );

    if (native_new == NULL) {
        free(native_old);

        seal_os_set_error(
            output_error,
            new_error
        );

        return false;
    }

    int result =
        rename(
            native_old,
            native_new
        );

    int native_error =
        result == 0
            ? 0
            : errno;

    free(native_old);
    free(native_new);

    if (result != 0) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    return true;

#endif
}

/*
Directory iteration
*/

#ifdef _WIN32

typedef struct sealOsDirectory {
    HANDLE handle;
    WIN32_FIND_DATAW current;
    bool current_pending;
    bool finished;
} sealOsDirectory;

#else

typedef struct sealOsDirectory {
    DIR *directory;
} sealOsDirectory;

#endif

bool seal_os_open_directory(
    sealString path,
    void **output_handle,
    intptr_t *output_error
) {
    if (output_handle != NULL) {
        *output_handle = NULL;
    }

    seal_os_clear_error(
        output_error
    );

#ifdef _WIN32

    int conversion_error = 0;

    wchar_t *native_path =
        seal_os_utf8_to_wide(
            path,
            &conversion_error
        );

    if (native_path == NULL) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    DWORD attributes =
        GetFileAttributesW(
            native_path
        );

    if (attributes ==
        INVALID_FILE_ATTRIBUTES) {
        int native_error =
            seal_os_errno_from_win32(
                GetLastError()
            );

        free(native_path);

        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    if ((attributes &
         FILE_ATTRIBUTE_DIRECTORY) == 0) {
        free(native_path);

        seal_os_set_error(
            output_error,
            ENOTDIR
        );

        return false;
    }

    size_t length =
        wcslen(native_path);

    bool needs_separator =
        length > 0 &&
        native_path[length - 1] != L'\\' &&
        native_path[length - 1] != L'/';

    size_t pattern_length =
        length +
        (needs_separator ? 1 : 0) +
        1;

    wchar_t *pattern =
        malloc(
            (pattern_length + 1) *
            sizeof(wchar_t)
        );

    if (pattern == NULL) {
        free(native_path);

        seal_os_set_error(
            output_error,
            ENOMEM
        );

        return false;
    }

    memcpy(
        pattern,
        native_path,
        length *
        sizeof(wchar_t)
    );

    size_t write_index =
        length;

    if (needs_separator) {
        pattern[write_index++] =
            L'\\';
    }

    pattern[write_index++] =
        L'*';

    pattern[write_index] =
        L'\0';

    free(native_path);

    sealOsDirectory *iterator =
        calloc(
            1,
            sizeof(
                sealOsDirectory
            )
        );

    if (iterator == NULL) {
        free(pattern);

        seal_os_set_error(
            output_error,
            ENOMEM
        );

        return false;
    }

    iterator->handle =
        FindFirstFileW(
            pattern,
            &iterator->current
        );

    free(pattern);

    if (iterator->handle ==
        INVALID_HANDLE_VALUE) {
        DWORD code =
            GetLastError();

        if (code ==
            ERROR_FILE_NOT_FOUND) {
            iterator->finished =
                true;

            iterator->current_pending =
                false;
        } else {
            int native_error =
                seal_os_errno_from_win32(
                    code
                );

            free(iterator);

            seal_os_set_error(
                output_error,
                native_error
            );

            return false;
        }
    } else {
        iterator->current_pending =
            true;

        iterator->finished =
            false;
    }

#else

    int conversion_error = 0;

    char *native_path =
        seal_os_copy_string(
            path,
            &conversion_error
        );

    if (native_path == NULL) {
        seal_os_set_error(
            output_error,
            conversion_error
        );

        return false;
    }

    DIR *directory =
        opendir(native_path);

    int native_error =
        directory != NULL
            ? 0
            : errno;

    free(native_path);

    if (directory == NULL) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

    sealOsDirectory *iterator =
        malloc(
            sizeof(
                sealOsDirectory
            )
        );

    if (iterator == NULL) {
        closedir(directory);

        seal_os_set_error(
            output_error,
            ENOMEM
        );

        return false;
    }

    iterator->directory =
        directory;

#endif

    if (output_handle != NULL) {
        *output_handle =
            iterator;
    }

    return true;
}

bool seal_os_next_directory_entry(
    void *handle,
    void **output_name_data,
    uintptr_t *output_name_length,
    intptr_t *output_type,
    bool *output_ended,
    intptr_t *output_error
) {
    if (output_name_data != NULL) {
        *output_name_data = NULL;
    }

    if (output_name_length != NULL) {
        *output_name_length = 0;
    }

    if (output_type != NULL) {
        *output_type =
            SEAL_OS_FILE_OTHER;
    }

    if (output_ended != NULL) {
        *output_ended = false;
    }

    seal_os_clear_error(
        output_error
    );

    if (handle == NULL) {
        seal_os_set_error(
            output_error,
            EINVAL
        );

        return false;
    }

    sealOsDirectory *iterator =
        (sealOsDirectory *)handle;

#ifdef _WIN32

    for (;;) {
        if (iterator->finished) {
            if (output_ended != NULL) {
                *output_ended = true;
            }

            return true;
        }

        WIN32_FIND_DATAW *entry =
            &iterator->current;

        if (iterator->current_pending) {
            iterator->current_pending =
                false;
        } else {
            if (!FindNextFileW(
                    iterator->handle,
                    entry
                )) {
                DWORD code =
                    GetLastError();

                if (code ==
                    ERROR_NO_MORE_FILES) {
                    iterator->finished =
                        true;

                    if (output_ended != NULL) {
                        *output_ended = true;
                    }

                    return true;
                }

                seal_os_set_error(
                    output_error,
                    seal_os_errno_from_win32(
                        code
                    )
                );

                return false;
            }
        }

        const wchar_t *name =
            entry->cFileName;

        if ((name[0] == L'.' &&
             name[1] == L'\0') ||
            (name[0] == L'.' &&
             name[1] == L'.' &&
             name[2] == L'\0')) {
            continue;
        }

        size_t name_length =
            wcslen(name);

        int conversion_error = 0;

        if (!seal_os_wide_to_utf8(
                name,
                name_length,
                output_name_data,
                output_name_length,
                &conversion_error
            )) {
            seal_os_set_error(
                output_error,
                conversion_error
            );

            return false;
        }

        intptr_t file_type =
            SEAL_OS_FILE_OTHER;

        if ((entry->dwFileAttributes &
             FILE_ATTRIBUTE_REPARSE_POINT) != 0) {
            file_type =
                SEAL_OS_FILE_SYMLINK;
        } else if (
            (entry->dwFileAttributes &
             FILE_ATTRIBUTE_DIRECTORY) != 0
        ) {
            file_type =
                SEAL_OS_FILE_DIRECTORY;
        } else if (
            (entry->dwFileAttributes &
             FILE_ATTRIBUTE_DEVICE) == 0
        ) {
            file_type =
                SEAL_OS_FILE_REGULAR;
        }

        if (output_type != NULL) {
            *output_type =
                file_type;
        }

        return true;
    }

#else

    errno = 0;

    for (;;) {
        struct dirent *entry =
            readdir(
                iterator->directory
            );

        if (entry == NULL) {
            if (errno != 0) {
                seal_os_set_error(
                    output_error,
                    errno
                );

                return false;
            }

            if (output_ended != NULL) {
                *output_ended = true;
            }

            return true;
        }

        if ((entry->d_name[0] == '.' &&
             entry->d_name[1] == '\0') ||
            (entry->d_name[0] == '.' &&
             entry->d_name[1] == '.' &&
             entry->d_name[2] == '\0')) {
            continue;
        }

        size_t name_length =
            strlen(
                entry->d_name
            );

        void *copy =
            seal_os_copy_bytes(
                entry->d_name,
                name_length
            );

        if (copy == NULL) {
            seal_os_set_error(
                output_error,
                ENOMEM
            );

            return false;
        }

        intptr_t file_type =
            SEAL_OS_FILE_OTHER;

#ifdef DT_REG
        if (entry->d_type == DT_REG) {
            file_type =
                SEAL_OS_FILE_REGULAR;
        } else if (
            entry->d_type == DT_DIR
        ) {
            file_type =
                SEAL_OS_FILE_DIRECTORY;
        } else if (
            entry->d_type == DT_LNK
        ) {
            file_type =
                SEAL_OS_FILE_SYMLINK;
        }
#endif

        if (output_name_data != NULL) {
            *output_name_data =
                copy;
        }

        if (output_name_length != NULL) {
            *output_name_length =
                (uintptr_t)name_length;
        }

        if (output_type != NULL) {
            *output_type =
                file_type;
        }

        return true;
    }

#endif
}

bool seal_os_close_directory(
    void *handle,
    intptr_t *output_error
) {
    seal_os_clear_error(
        output_error
    );

    if (handle == NULL) {
        return true;
    }

    sealOsDirectory *iterator =
        (sealOsDirectory *)handle;

#ifdef _WIN32

    bool success = true;
    int native_error = 0;

    if (iterator->handle !=
        INVALID_HANDLE_VALUE) {
        if (!FindClose(
                iterator->handle
            )) {
            success = false;

            native_error =
                seal_os_errno_from_win32(
                    GetLastError()
                );
        }
    }

    free(iterator);

    if (!success) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

#else

    int result =
        closedir(
            iterator->directory
        );

    int native_error =
        result == 0
            ? 0
            : errno;

    free(iterator);

    if (result != 0) {
        seal_os_set_error(
            output_error,
            native_error
        );

        return false;
    }

#endif

    return true;
}

/*
Process information
*/

uintptr_t seal_os_process_id(void) {
#ifdef _WIN32
    return (uintptr_t)
        GetCurrentProcessId();
#else
    return (uintptr_t)
        getpid();
#endif
}

bool seal_os_executable_path(
    void **output_data,
    uintptr_t *output_length,
    intptr_t *output_error
) {
    if (output_data != NULL) {
        *output_data = NULL;
    }

    if (output_length != NULL) {
        *output_length = 0;
    }

    seal_os_clear_error(
        output_error
    );

#ifdef _WIN32

    DWORD capacity = 256;

    for (;;) {
        wchar_t *buffer =
            malloc(
                (size_t)capacity *
                sizeof(wchar_t)
            );

        if (buffer == NULL) {
            seal_os_set_error(
                output_error,
                ENOMEM
            );

            return false;
        }

        DWORD written =
            GetModuleFileNameW(
                NULL,
                buffer,
                capacity
            );

        if (written == 0) {
            int native_error =
                seal_os_errno_from_win32(
                    GetLastError()
                );

            free(buffer);

            seal_os_set_error(
                output_error,
                native_error
            );

            return false;
        }

        if (written < capacity - 1) {
            int conversion_error = 0;

            bool converted =
                seal_os_wide_to_utf8(
                    buffer,
                    (size_t)written,
                    output_data,
                    output_length,
                    &conversion_error
                );

            free(buffer);

            if (!converted) {
                seal_os_set_error(
                    output_error,
                    conversion_error
                );

                return false;
            }

            return true;
        }

        free(buffer);

        if (capacity >
            UINT32_MAX / 2) {
            seal_os_set_error(
                output_error,
                EOVERFLOW
            );

            return false;
        }

        capacity *= 2;
    }

#elif defined(__linux__)

    size_t capacity = 256;

    for (;;) {
        char *buffer =
            malloc(capacity);

        if (buffer == NULL) {
            seal_os_set_error(
                output_error,
                ENOMEM
            );

            return false;
        }

        ssize_t written =
            readlink(
                "/proc/self/exe",
                buffer,
                capacity
            );

        if (written < 0) {
            int native_error =
                errno;

            free(buffer);

            seal_os_set_error(
                output_error,
                native_error
            );

            return false;
        }

        if ((size_t)written <
            capacity) {
            void *copy =
                seal_os_copy_bytes(
                    buffer,
                    (size_t)written
                );

            free(buffer);

            if (copy == NULL) {
                seal_os_set_error(
                    output_error,
                    ENOMEM
                );

                return false;
            }

            if (output_data != NULL) {
                *output_data = copy;
            }

            if (output_length != NULL) {
                *output_length =
                    (uintptr_t)written;
            }

            return true;
        }

        free(buffer);

        if (capacity > SIZE_MAX / 2) {
            seal_os_set_error(
                output_error,
                EOVERFLOW
            );

            return false;
        }

        capacity *= 2;
    }

#elif defined(__APPLE__)

    uint32_t required = 0;

    _NSGetExecutablePath(
        NULL,
        &required
    );

    if (required == 0) {
        seal_os_set_error(
            output_error,
            EIO
        );

        return false;
    }

    char *buffer =
        malloc(required);

    if (buffer == NULL) {
        seal_os_set_error(
            output_error,
            ENOMEM
        );

        return false;
    }

    if (_NSGetExecutablePath(
            buffer,
            &required
        ) != 0) {
        free(buffer);

        seal_os_set_error(
            output_error,
            EIO
        );

        return false;
    }

    size_t length =
        strlen(buffer);

    void *copy =
        seal_os_copy_bytes(
            buffer,
            length
        );

    free(buffer);

    if (copy == NULL) {
        seal_os_set_error(
            output_error,
            ENOMEM
        );

        return false;
    }

    if (output_data != NULL) {
        *output_data = copy;
    }

    if (output_length != NULL) {
        *output_length =
            (uintptr_t)length;
    }

    return true;

#else

    seal_os_set_error(
        output_error,
        SEAL_OS_NATIVE_UNSUPPORTED
    );

    return false;

#endif
}

void seal_os_exit(
    intptr_t code
) {
    exit((int)code);
}
