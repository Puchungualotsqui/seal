#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>

#if defined(_WIN32)

#define WIN32_LEAN_AND_MEAN
#include <windows.h>

#else

#include <errno.h>
#include <pthread.h>
#include <sched.h>
#include <time.h>
#include <unistd.h>

#endif

typedef uintptr_t (*SealThreadEntry)(
    void *context
);

typedef struct SealThreadStart {
    SealThreadEntry entry;
    void *context;
} SealThreadStart;

#if defined(_WIN32)

typedef struct SealThreadHandle {
    HANDLE handle;
} SealThreadHandle;

typedef struct SealMutex {
    CRITICAL_SECTION value;
} SealMutex;

typedef struct SealCondition {
    CONDITION_VARIABLE value;
} SealCondition;

typedef struct SealOnce {
    CRITICAL_SECTION mutex;
    CONDITION_VARIABLE condition;
    int state;
} SealOnce;

static DWORD WINAPI seal_thread_trampoline(
    LPVOID opaque
) {
    SealThreadStart *start =
        (SealThreadStart *)opaque;

    SealThreadEntry entry =
        start->entry;

    void *context =
        start->context;

    free(start);

    uintptr_t result =
        entry(context);

    return (DWORD)result;
}

#else

typedef struct SealThreadHandle {
    pthread_t handle;
} SealThreadHandle;

typedef struct SealMutex {
    pthread_mutex_t value;
} SealMutex;

typedef struct SealCondition {
    pthread_cond_t value;
} SealCondition;

typedef struct SealOnce {
    pthread_mutex_t mutex;
    pthread_cond_t condition;
    int state;
} SealOnce;

static void *seal_thread_trampoline(
    void *opaque
) {
    SealThreadStart *start =
        (SealThreadStart *)opaque;

    SealThreadEntry entry =
        start->entry;

    void *context =
        start->context;

    free(start);

    uintptr_t result =
        entry(context);

    return (void *)result;
}

#endif

void *seal_thread_start(
    void *entry,
    void *context
) {
    if (entry == NULL) {
        return NULL;
    }

    SealThreadEntry callback =
        (SealThreadEntry)(uintptr_t)entry;

    SealThreadStart *start =
        (SealThreadStart *)malloc(
            sizeof(SealThreadStart)
        );

    if (start == NULL) {
        return NULL;
    }

    SealThreadHandle *thread =
        (SealThreadHandle *)malloc(
            sizeof(SealThreadHandle)
        );

    if (thread == NULL) {
        free(start);
        return NULL;
    }

    start->entry = callback;
    start->context = context;

#if defined(_WIN32)

    thread->handle = CreateThread(
        NULL,
        0,
        seal_thread_trampoline,
        start,
        0,
        NULL
    );

    if (thread->handle == NULL) {
        free(start);
        free(thread);
        return NULL;
    }

#else

    int status = pthread_create(
        &thread->handle,
        NULL,
        seal_thread_trampoline,
        start
    );

    if (status != 0) {
        free(start);
        free(thread);
        return NULL;
    }

#endif

    return thread;
}

bool seal_thread_join(
    void *opaque
) {
    if (opaque == NULL) {
        return false;
    }

    SealThreadHandle *thread =
        (SealThreadHandle *)opaque;

#if defined(_WIN32)

    DWORD waitStatus =
        WaitForSingleObject(
            thread->handle,
            INFINITE
        );

    if (waitStatus != WAIT_OBJECT_0) {
        return false;
    }

    if (!CloseHandle(thread->handle)) {
        return false;
    }

#else

    int status = pthread_join(
        thread->handle,
        NULL
    );

    if (status != 0) {
        return false;
    }

#endif

    free(thread);

    return true;
}

bool seal_thread_detach(
    void *opaque
) {
    if (opaque == NULL) {
        return false;
    }

    SealThreadHandle *thread =
        (SealThreadHandle *)opaque;

#if defined(_WIN32)

    if (!CloseHandle(thread->handle)) {
        return false;
    }

#else

    int status = pthread_detach(
        thread->handle
    );

    if (status != 0) {
        return false;
    }

#endif

    free(thread);

    return true;
}

void *seal_thread_allocate_context(
    uintptr_t size
) {
    size_t allocationSize =
        size == 0
            ? 1
            : (size_t)size;

    return calloc(
        1,
        allocationSize
    );
}

void seal_thread_free_context(
    void *context
) {
    free(context);
}

void seal_thread_yield(void) {
#if defined(_WIN32)

    (void)SwitchToThread();

#else

    (void)sched_yield();

#endif
}

void seal_thread_sleep(
    uintptr_t milliseconds
) {
#if defined(_WIN32)

    while (milliseconds >
        (uintptr_t)UINT32_MAX) {
        Sleep(UINT32_MAX);
        milliseconds -=
            (uintptr_t)UINT32_MAX;
    }

    Sleep((DWORD)milliseconds);

#else

    struct timespec requested;

    requested.tv_sec =
        (time_t)(
            milliseconds / 1000u
        );

    requested.tv_nsec =
        (long)(
            (milliseconds % 1000u) *
            1000000u
        );

    while (nanosleep(
        &requested,
        &requested
    ) != 0) {
        if (errno != EINTR) {
            break;
        }
    }

#endif
}

uintptr_t seal_thread_hardware_thread_count(
    void
) {
#if defined(_WIN32)

    SYSTEM_INFO information;

    GetSystemInfo(&information);

    if (information.dwNumberOfProcessors == 0) {
        return 1;
    }

    return (uintptr_t)
        information.dwNumberOfProcessors;

#else

    long count =
        sysconf(
            _SC_NPROCESSORS_ONLN
        );

    if (count < 1) {
        return 1;
    }

    return (uintptr_t)count;

#endif
}

void *seal_mutex_create(void) {
    SealMutex *mutex =
        (SealMutex *)malloc(
            sizeof(SealMutex)
        );

    if (mutex == NULL) {
        return NULL;
    }

#if defined(_WIN32)

    InitializeCriticalSection(
        &mutex->value
    );

#else

    if (pthread_mutex_init(
        &mutex->value,
        NULL
    ) != 0) {
        free(mutex);
        return NULL;
    }

#endif

    return mutex;
}

bool seal_mutex_destroy(
    void *opaque
) {
    if (opaque == NULL) {
        return false;
    }

    SealMutex *mutex =
        (SealMutex *)opaque;

#if defined(_WIN32)

    DeleteCriticalSection(
        &mutex->value
    );

#else

    if (pthread_mutex_destroy(
        &mutex->value
    ) != 0) {
        return false;
    }

#endif

    free(mutex);

    return true;
}

bool seal_mutex_lock(
    void *opaque
) {
    if (opaque == NULL) {
        return false;
    }

    SealMutex *mutex =
        (SealMutex *)opaque;

#if defined(_WIN32)

    EnterCriticalSection(
        &mutex->value
    );

    return true;

#else

    return pthread_mutex_lock(
        &mutex->value
    ) == 0;

#endif
}

int seal_mutex_try_lock(
    void *opaque
) {
    if (opaque == NULL) {
        return -1;
    }

    SealMutex *mutex =
        (SealMutex *)opaque;

#if defined(_WIN32)

    return TryEnterCriticalSection(
        &mutex->value
    )
        ? 1
        : 0;

#else

    int status =
        pthread_mutex_trylock(
            &mutex->value
        );

    if (status == 0) {
        return 1;
    }

    if (status == EBUSY) {
        return 0;
    }

    return -1;

#endif
}

bool seal_mutex_unlock(
    void *opaque
) {
    if (opaque == NULL) {
        return false;
    }

    SealMutex *mutex =
        (SealMutex *)opaque;

#if defined(_WIN32)

    LeaveCriticalSection(
        &mutex->value
    );

    return true;

#else

    return pthread_mutex_unlock(
        &mutex->value
    ) == 0;

#endif
}

void *seal_condition_create(void) {
    SealCondition *condition =
        (SealCondition *)malloc(
            sizeof(SealCondition)
        );

    if (condition == NULL) {
        return NULL;
    }

#if defined(_WIN32)

    InitializeConditionVariable(
        &condition->value
    );

#else

    if (pthread_cond_init(
        &condition->value,
        NULL
    ) != 0) {
        free(condition);
        return NULL;
    }

#endif

    return condition;
}

bool seal_condition_destroy(
    void *opaque
) {
    if (opaque == NULL) {
        return false;
    }

    SealCondition *condition =
        (SealCondition *)opaque;

#if !defined(_WIN32)

    if (pthread_cond_destroy(
        &condition->value
    ) != 0) {
        return false;
    }

#endif

    free(condition);

    return true;
}

bool seal_condition_wait(
    void *conditionOpaque,
    void *mutexOpaque
) {
    if (conditionOpaque == NULL ||
        mutexOpaque == NULL) {
        return false;
    }

    SealCondition *condition =
        (SealCondition *)
            conditionOpaque;

    SealMutex *mutex =
        (SealMutex *)mutexOpaque;

#if defined(_WIN32)

    return SleepConditionVariableCS(
        &condition->value,
        &mutex->value,
        INFINITE
    ) != 0;

#else

    return pthread_cond_wait(
        &condition->value,
        &mutex->value
    ) == 0;

#endif
}

bool seal_condition_signal(
    void *opaque
) {
    if (opaque == NULL) {
        return false;
    }

    SealCondition *condition =
        (SealCondition *)opaque;

#if defined(_WIN32)

    WakeConditionVariable(
        &condition->value
    );

    return true;

#else

    return pthread_cond_signal(
        &condition->value
    ) == 0;

#endif
}

bool seal_condition_broadcast(
    void *opaque
) {
    if (opaque == NULL) {
        return false;
    }

    SealCondition *condition =
        (SealCondition *)opaque;

#if defined(_WIN32)

    WakeAllConditionVariable(
        &condition->value
    );

    return true;

#else

    return pthread_cond_broadcast(
        &condition->value
    ) == 0;

#endif
}

void *seal_once_create(void) {
    SealOnce *once =
        (SealOnce *)malloc(
            sizeof(SealOnce)
        );

    if (once == NULL) {
        return NULL;
    }

    once->state = 0;

#if defined(_WIN32)

    InitializeCriticalSection(
        &once->mutex
    );

    InitializeConditionVariable(
        &once->condition
    );

#else

    if (pthread_mutex_init(
        &once->mutex,
        NULL
    ) != 0) {
        free(once);
        return NULL;
    }

    if (pthread_cond_init(
        &once->condition,
        NULL
    ) != 0) {
        pthread_mutex_destroy(
            &once->mutex
        );

        free(once);

        return NULL;
    }

#endif

    return once;
}

bool seal_once_destroy(
    void *opaque
) {
    if (opaque == NULL) {
        return false;
    }

    SealOnce *once =
        (SealOnce *)opaque;

#if defined(_WIN32)

    DeleteCriticalSection(
        &once->mutex
    );

#else

    if (pthread_cond_destroy(
        &once->condition
    ) != 0) {
        return false;
    }

    if (pthread_mutex_destroy(
        &once->mutex
    ) != 0) {
        return false;
    }

#endif

    free(once);

    return true;
}

int seal_once_begin(
    void *opaque
) {
    if (opaque == NULL) {
        return -1;
    }

    SealOnce *once =
        (SealOnce *)opaque;

#if defined(_WIN32)

    EnterCriticalSection(
        &once->mutex
    );

    while (once->state == 1) {
        if (!SleepConditionVariableCS(
            &once->condition,
            &once->mutex,
            INFINITE
        )) {
            LeaveCriticalSection(
                &once->mutex
            );

            return -1;
        }
    }

    if (once->state == 2) {
        LeaveCriticalSection(
            &once->mutex
        );

        return 0;
    }

    once->state = 1;

    LeaveCriticalSection(
        &once->mutex
    );

    return 1;

#else

    if (pthread_mutex_lock(
        &once->mutex
    ) != 0) {
        return -1;
    }

    while (once->state == 1) {
        if (pthread_cond_wait(
            &once->condition,
            &once->mutex
        ) != 0) {
            pthread_mutex_unlock(
                &once->mutex
            );

            return -1;
        }
    }

    if (once->state == 2) {
        pthread_mutex_unlock(
            &once->mutex
        );

        return 0;
    }

    once->state = 1;

    pthread_mutex_unlock(
        &once->mutex
    );

    return 1;

#endif
}

bool seal_once_complete(
    void *opaque
) {
    if (opaque == NULL) {
        return false;
    }

    SealOnce *once =
        (SealOnce *)opaque;

#if defined(_WIN32)

    EnterCriticalSection(
        &once->mutex
    );

    if (once->state != 1) {
        LeaveCriticalSection(
            &once->mutex
        );

        return false;
    }

    once->state = 2;

    WakeAllConditionVariable(
        &once->condition
    );

    LeaveCriticalSection(
        &once->mutex
    );

    return true;

#else

    if (pthread_mutex_lock(
        &once->mutex
    ) != 0) {
        return false;
    }

    if (once->state != 1) {
        pthread_mutex_unlock(
            &once->mutex
        );

        return false;
    }

    once->state = 2;

    int broadcastStatus =
        pthread_cond_broadcast(
            &once->condition
        );

    int unlockStatus =
        pthread_mutex_unlock(
            &once->mutex
        );

    return broadcastStatus == 0 &&
        unlockStatus == 0;

#endif
}
