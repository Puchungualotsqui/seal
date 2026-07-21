#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <limits.h>

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

#if (defined(__GNUC__) || defined(__clang__)) && \
    !defined(__TINYC__)
#define SEAL_HAS_GNU_ATOMICS 1
#else
#define SEAL_HAS_GNU_ATOMICS 0
#endif

typedef uintptr_t (*SealThreadEntry)(
    void *context
);

#if defined(_WIN32)

typedef struct SealMutex {
    CRITICAL_SECTION value;
} SealMutex;

typedef struct SealConditionWaiter
    SealConditionWaiter;

struct SealConditionWaiter {
    HANDLE event;
    SealConditionWaiter *next;
};

typedef struct SealCondition {
    CRITICAL_SECTION guard;
    SealConditionWaiter *head;
    SealConditionWaiter *tail;
} SealCondition;

#else

typedef struct SealMutex {
    pthread_mutex_t value;
} SealMutex;

typedef struct SealCondition {
    pthread_cond_t value;
} SealCondition;

#endif

typedef struct SealOnce {
    SealMutex guard;
    SealCondition completed;
    int state;
} SealOnce;

#if defined(_WIN32)

typedef struct SealSemaphore {
    HANDLE handle;
} SealSemaphore;

#else

typedef struct SealSemaphore {
    SealMutex guard;
    SealCondition available;
    uintptr_t count;
} SealSemaphore;

#endif

typedef struct SealRWMutex {
    SealMutex guard;
    SealCondition readers;
    SealCondition writers;

    uintptr_t activeReaders;
    uintptr_t waitingWriters;
    int writerActive;
} SealRWMutex;

typedef struct SealAtomic {
    SealMutex guard;
    volatile uintptr_t value;
} SealAtomic;

#if !defined(_WIN32)

typedef struct SealThreadCompletion {
    SealMutex guard;
    SealCondition condition;

    uintptr_t references;
    int completed;
} SealThreadCompletion;

#endif

typedef struct SealThreadStart {
    SealThreadEntry entry;
    void *context;

#if !defined(_WIN32)
    SealThreadCompletion *completion;
#endif
} SealThreadStart;

#if defined(_WIN32)

typedef struct SealThreadHandle {
    HANDLE handle;
} SealThreadHandle;

#else

typedef struct SealThreadHandle {
    pthread_t handle;
    SealThreadCompletion *completion;
} SealThreadHandle;

#endif

/*
Time helpers.
*/

static uint64_t seal_now_milliseconds(
    void
) {
#if defined(_WIN32)

    return (uint64_t)GetTickCount();

#else

    struct timespec now;

    if (clock_gettime(
        CLOCK_MONOTONIC,
        &now
    ) != 0) {
        return 0;
    }

    return
        ((uint64_t)now.tv_sec * 1000u) +
        ((uint64_t)now.tv_nsec / 1000000u);

#endif
}

static uintptr_t seal_remaining_milliseconds(
    uint64_t started,
    uintptr_t timeout
) {
    uint64_t now;
    uint64_t elapsed;

    now = seal_now_milliseconds();

    if (now < started) {
        return timeout;
    }

    elapsed = now - started;

    if (elapsed >= (uint64_t)timeout) {
        return 0;
    }

    return (uintptr_t)(
        (uint64_t)timeout -
        elapsed
    );
}

/*
Mutex implementation.
*/

static bool seal_mutex_init_value(
    SealMutex *mutex
) {
#if defined(_WIN32)

    InitializeCriticalSection(
        &mutex->value
    );

    return true;

#else

    return pthread_mutex_init(
        &mutex->value,
        NULL
    ) == 0;

#endif
}

static bool seal_mutex_destroy_value(
    SealMutex *mutex
) {
#if defined(_WIN32)

    DeleteCriticalSection(
        &mutex->value
    );

    return true;

#else

    return pthread_mutex_destroy(
        &mutex->value
    ) == 0;

#endif
}

static bool seal_mutex_lock_value(
    SealMutex *mutex
) {
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

static int seal_mutex_try_lock_value(
    SealMutex *mutex
) {
#if defined(_WIN32)

    return TryEnterCriticalSection(
        &mutex->value
    )
        ? 1
        : 0;

#else

    int status;

    status =
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

static bool seal_mutex_unlock_value(
    SealMutex *mutex
) {
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

/*
Condition implementation.
*/

#if defined(_WIN32)

static bool seal_condition_remove_waiter(
    SealCondition *condition,
    SealConditionWaiter *target
) {
    SealConditionWaiter *previous;
    SealConditionWaiter *current;

    previous = NULL;
    current = condition->head;

    while (current != NULL) {
        if (current == target) {
            if (previous == NULL) {
                condition->head =
                    current->next;
            } else {
                previous->next =
                    current->next;
            }

            if (condition->tail ==
                current) {
                condition->tail =
                    previous;
            }

            current->next = NULL;

            return true;
        }

        previous = current;
        current = current->next;
    }

    return false;
}

#endif

static bool seal_condition_init_value(
    SealCondition *condition
) {
#if defined(_WIN32)

    InitializeCriticalSection(
        &condition->guard
    );

    condition->head = NULL;
    condition->tail = NULL;

    return true;

#else

    return pthread_cond_init(
        &condition->value,
        NULL
    ) == 0;

#endif
}

static bool seal_condition_destroy_value(
    SealCondition *condition
) {
#if defined(_WIN32)

    EnterCriticalSection(
        &condition->guard
    );

    if (condition->head != NULL) {
        LeaveCriticalSection(
            &condition->guard
        );

        return false;
    }

    LeaveCriticalSection(
        &condition->guard
    );

    DeleteCriticalSection(
        &condition->guard
    );

    return true;

#else

    return pthread_cond_destroy(
        &condition->value
    ) == 0;

#endif
}

static int seal_condition_wait_value(
    SealCondition *condition,
    SealMutex *mutex,
    uintptr_t milliseconds,
    bool infinite
) {
#if defined(_WIN32)

    SealConditionWaiter waiter;
    DWORD waitStatus;
    DWORD timeout;
    bool removed;

    waiter.event = CreateEventA(
        NULL,
        FALSE,
        FALSE,
        NULL
    );

    if (waiter.event == NULL) {
        return -1;
    }

    waiter.next = NULL;

    EnterCriticalSection(
        &condition->guard
    );

    if (condition->tail == NULL) {
        condition->head = &waiter;
        condition->tail = &waiter;
    } else {
        condition->tail->next =
            &waiter;

        condition->tail =
            &waiter;
    }

    LeaveCriticalSection(
        &condition->guard
    );

    if (!seal_mutex_unlock_value(
        mutex
    )) {
        EnterCriticalSection(
            &condition->guard
        );

        (void)seal_condition_remove_waiter(
            condition,
            &waiter
        );

        LeaveCriticalSection(
            &condition->guard
        );

        CloseHandle(
            waiter.event
        );

        return -1;
    }

    if (infinite) {
        timeout = INFINITE;
    } else if (
        milliseconds >
        (uintptr_t)UINT32_MAX
    ) {
        timeout = UINT32_MAX;
    } else {
        timeout = (DWORD)milliseconds;
    }

    waitStatus =
        WaitForSingleObject(
            waiter.event,
            timeout
        );

    if (!seal_mutex_lock_value(
        mutex
    )) {
        CloseHandle(
            waiter.event
        );

        return -1;
    }

    if (waitStatus ==
        WAIT_OBJECT_0) {
        CloseHandle(
            waiter.event
        );

        return 1;
    }

    EnterCriticalSection(
        &condition->guard
    );

    removed =
        seal_condition_remove_waiter(
            condition,
            &waiter
        );

    LeaveCriticalSection(
        &condition->guard
    );

    CloseHandle(
        waiter.event
    );

    /*
    If it was already removed, Signal or Broadcast claimed it and called
    SetEvent before releasing condition->guard.
    */
    if (!removed) {
        return 1;
    }

    if (waitStatus == WAIT_TIMEOUT) {
        return 0;
    }

    return -1;

#else

    int status;

    if (infinite) {
        status =
            pthread_cond_wait(
                &condition->value,
                &mutex->value
            );

        return status == 0
            ? 1
            : -1;
    }

    {
        struct timespec deadline;
        uint64_t extraNanoseconds;

        if (clock_gettime(
            CLOCK_REALTIME,
            &deadline
        ) != 0) {
            return -1;
        }

        deadline.tv_sec +=
            (time_t)(
                milliseconds /
                1000u
            );

        extraNanoseconds =
            (uint64_t)(
                milliseconds %
                1000u
            ) * 1000000u;

        deadline.tv_nsec +=
            (long)extraNanoseconds;

        if (deadline.tv_nsec >=
            1000000000L) {
            deadline.tv_sec += 1;
            deadline.tv_nsec -=
                1000000000L;
        }

        status =
            pthread_cond_timedwait(
                &condition->value,
                &mutex->value,
                &deadline
            );
    }

    if (status == 0) {
        return 1;
    }

    if (status == ETIMEDOUT) {
        return 0;
    }

    return -1;

#endif
}

static bool seal_condition_signal_value(
    SealCondition *condition
) {
#if defined(_WIN32)

    SealConditionWaiter *waiter;

    EnterCriticalSection(
        &condition->guard
    );

    waiter = condition->head;

    if (waiter == NULL) {
        LeaveCriticalSection(
            &condition->guard
        );

        return true;
    }

    if (!SetEvent(
        waiter->event
    )) {
        LeaveCriticalSection(
            &condition->guard
        );

        return false;
    }

    condition->head =
        waiter->next;

    if (condition->head == NULL) {
        condition->tail = NULL;
    }

    waiter->next = NULL;

    LeaveCriticalSection(
        &condition->guard
    );

    return true;

#else

    return pthread_cond_signal(
        &condition->value
    ) == 0;

#endif
}

static bool seal_condition_broadcast_value(
    SealCondition *condition
) {
#if defined(_WIN32)

    SealConditionWaiter *waiter;

    EnterCriticalSection(
        &condition->guard
    );

    while (condition->head != NULL) {
        waiter = condition->head;

        if (!SetEvent(
            waiter->event
        )) {
            LeaveCriticalSection(
                &condition->guard
            );

            return false;
        }

        condition->head =
            waiter->next;

        waiter->next = NULL;
    }

    condition->tail = NULL;

    LeaveCriticalSection(
        &condition->guard
    );

    return true;

#else

    return pthread_cond_broadcast(
        &condition->value
    ) == 0;

#endif
}

/*
POSIX thread completion.
*/

#if !defined(_WIN32)

static SealThreadCompletion *
seal_thread_completion_create(
    void
) {
    SealThreadCompletion *completion;

    completion =
        (SealThreadCompletion *)malloc(
            sizeof(SealThreadCompletion)
        );

    if (completion == NULL) {
        return NULL;
    }

    if (!seal_mutex_init_value(
        &completion->guard
    )) {
        free(completion);

        return NULL;
    }

    if (!seal_condition_init_value(
        &completion->condition
    )) {
        seal_mutex_destroy_value(
            &completion->guard
        );

        free(completion);

        return NULL;
    }

    completion->references = 2;
    completion->completed = 0;

    return completion;
}

static void seal_thread_completion_release(
    SealThreadCompletion *completion
) {
    bool destroy;

    destroy = false;

    seal_mutex_lock_value(
        &completion->guard
    );

    if (completion->references > 0) {
        completion->references -= 1;
    }

    if (completion->references == 0) {
        destroy = true;
    }

    seal_mutex_unlock_value(
        &completion->guard
    );

    if (destroy) {
        seal_condition_destroy_value(
            &completion->condition
        );

        seal_mutex_destroy_value(
            &completion->guard
        );

        free(completion);
    }
}

static void seal_thread_completion_finish(
    SealThreadCompletion *completion
) {
    seal_mutex_lock_value(
        &completion->guard
    );

    completion->completed = 1;

    seal_condition_broadcast_value(
        &completion->condition
    );

    seal_mutex_unlock_value(
        &completion->guard
    );

    seal_thread_completion_release(
        completion
    );
}

#endif

/*
Thread trampoline.
*/

#if defined(_WIN32)

static DWORD WINAPI seal_thread_trampoline(
    LPVOID opaque
) {
    SealThreadStart *start;
    SealThreadEntry entry;
    void *context;
    uintptr_t result;

    start =
        (SealThreadStart *)opaque;

    entry = start->entry;
    context = start->context;

    free(start);

    result = entry(context);

    return (DWORD)result;
}

#else

static void *seal_thread_trampoline(
    void *opaque
) {
    SealThreadStart *start;
    SealThreadEntry entry;
    void *context;
    SealThreadCompletion *completion;
    uintptr_t result;

    start =
        (SealThreadStart *)opaque;

    entry = start->entry;
    context = start->context;
    completion = start->completion;

    free(start);

    result = entry(context);

    seal_thread_completion_finish(
        completion
    );

    return (void *)result;
}

#endif

/*
Thread API.
*/

void *seal_thread_start(
    void *entry,
    void *context
) {
    SealThreadEntry callback;
    SealThreadStart *start;
    SealThreadHandle *thread;

#if !defined(_WIN32)
    SealThreadCompletion *completion;
#endif

    if (entry == NULL) {
        return NULL;
    }

    callback =
        (SealThreadEntry)(uintptr_t)entry;

    start =
        (SealThreadStart *)malloc(
            sizeof(SealThreadStart)
        );

    if (start == NULL) {
        return NULL;
    }

    thread =
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

    thread->handle =
        CreateThread(
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

    completion =
        seal_thread_completion_create();

    if (completion == NULL) {
        free(start);
        free(thread);

        return NULL;
    }

    start->completion = completion;
    thread->completion = completion;

    if (pthread_create(
        &thread->handle,
        NULL,
        seal_thread_trampoline,
        start
    ) != 0) {
        completion->references = 0;

        seal_condition_destroy_value(
            &completion->condition
        );

        seal_mutex_destroy_value(
            &completion->guard
        );

        free(completion);
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
    SealThreadHandle *thread;

    if (opaque == NULL) {
        return false;
    }

    thread =
        (SealThreadHandle *)opaque;

#if defined(_WIN32)

    if (WaitForSingleObject(
        thread->handle,
        INFINITE
    ) != WAIT_OBJECT_0) {
        return false;
    }

    if (!CloseHandle(
        thread->handle
    )) {
        return false;
    }

#else

    if (pthread_join(
        thread->handle,
        NULL
    ) != 0) {
        return false;
    }

    seal_thread_completion_release(
        thread->completion
    );

#endif

    free(thread);

    return true;
}

int seal_thread_join_for(
    void *opaque,
    uintptr_t milliseconds
) {
    SealThreadHandle *thread;

    if (opaque == NULL) {
        return -1;
    }

    thread =
        (SealThreadHandle *)opaque;

#if defined(_WIN32)

    DWORD timeout;
    DWORD status;

    timeout =
        milliseconds >
            (uintptr_t)UINT32_MAX
        ? UINT32_MAX
        : (DWORD)milliseconds;

    status =
        WaitForSingleObject(
            thread->handle,
            timeout
        );

    if (status == WAIT_TIMEOUT) {
        return 0;
    }

    if (status != WAIT_OBJECT_0) {
        return -1;
    }

    if (!CloseHandle(
        thread->handle
    )) {
        return -1;
    }

#else

    int waitResult;
    uintptr_t remaining;
    uint64_t started;

    started =
        seal_now_milliseconds();

    if (!seal_mutex_lock_value(
        &thread->completion->guard
    )) {
        return -1;
    }

    while (!thread->completion->completed) {
        remaining =
            seal_remaining_milliseconds(
                started,
                milliseconds
            );

        if (remaining == 0) {
            seal_mutex_unlock_value(
                &thread->completion->guard
            );

            return 0;
        }

        waitResult =
            seal_condition_wait_value(
                &thread->completion->condition,
                &thread->completion->guard,
                remaining,
                false
            );

        if (waitResult < 0) {
            seal_mutex_unlock_value(
                &thread->completion->guard
            );

            return -1;
        }

        if (waitResult == 0 &&
            !thread->completion->completed) {
            seal_mutex_unlock_value(
                &thread->completion->guard
            );

            return 0;
        }
    }

    seal_mutex_unlock_value(
        &thread->completion->guard
    );

    if (pthread_join(
        thread->handle,
        NULL
    ) != 0) {
        return -1;
    }

    seal_thread_completion_release(
        thread->completion
    );

#endif

    free(thread);

    return 1;
}

bool seal_thread_detach(
    void *opaque
) {
    SealThreadHandle *thread;

    if (opaque == NULL) {
        return false;
    }

    thread =
        (SealThreadHandle *)opaque;

#if defined(_WIN32)

    if (!CloseHandle(
        thread->handle
    )) {
        return false;
    }

#else

    if (pthread_detach(
        thread->handle
    ) != 0) {
        return false;
    }

    seal_thread_completion_release(
        thread->completion
    );

#endif

    free(thread);

    return true;
}

void *seal_thread_allocate_context(
    uintptr_t byteSize
) {
    size_t allocationSize;

    allocationSize =
        byteSize == 0
            ? 1
            : (size_t)byteSize;

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

void seal_thread_yield(
    void
) {
#if defined(_WIN32)

    Sleep(0);

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

    Sleep(
        (DWORD)milliseconds
    );

#else

    struct timespec requested;

    requested.tv_sec =
        (time_t)(
            milliseconds /
            1000u
        );

    requested.tv_nsec =
        (long)(
            (milliseconds %
                1000u) *
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

    GetSystemInfo(
        &information
    );

    if (information.dwNumberOfProcessors ==
        0) {
        return 1;
    }

    return (uintptr_t)
        information.dwNumberOfProcessors;

#else

    long count;

    count =
        sysconf(
            _SC_NPROCESSORS_ONLN
        );

    if (count < 1) {
        return 1;
    }

    return (uintptr_t)count;

#endif
}

uintptr_t seal_thread_current_id(
    void
) {
#if defined(_WIN32)

    return (uintptr_t)
        GetCurrentThreadId();

#else

    pthread_t current;
    uintptr_t identifier;
    size_t copySize;

    current = pthread_self();
    identifier = 0;

    copySize = sizeof(current);

    if (copySize >
        sizeof(identifier)) {
        copySize =
            sizeof(identifier);
    }

    memcpy(
        &identifier,
        &current,
        copySize
    );

    return identifier;

#endif
}

/*
Public mutex API.
*/

void *seal_mutex_create(
    void
) {
    SealMutex *mutex;

    mutex =
        (SealMutex *)malloc(
            sizeof(SealMutex)
        );

    if (mutex == NULL) {
        return NULL;
    }

    if (!seal_mutex_init_value(
        mutex
    )) {
        free(mutex);

        return NULL;
    }

    return mutex;
}

bool seal_mutex_destroy(
    void *opaque
) {
    SealMutex *mutex;

    if (opaque == NULL) {
        return false;
    }

    mutex =
        (SealMutex *)opaque;

    if (!seal_mutex_destroy_value(
        mutex
    )) {
        return false;
    }

    free(mutex);

    return true;
}

bool seal_mutex_lock(
    void *opaque
) {
    if (opaque == NULL) {
        return false;
    }

    return seal_mutex_lock_value(
        (SealMutex *)opaque
    );
}

int seal_mutex_try_lock(
    void *opaque
) {
    if (opaque == NULL) {
        return -1;
    }

    return seal_mutex_try_lock_value(
        (SealMutex *)opaque
    );
}

bool seal_mutex_unlock(
    void *opaque
) {
    if (opaque == NULL) {
        return false;
    }

    return seal_mutex_unlock_value(
        (SealMutex *)opaque
    );
}

/*
Public condition API.
*/

void *seal_condition_create(
    void
) {
    SealCondition *condition;

    condition =
        (SealCondition *)malloc(
            sizeof(SealCondition)
        );

    if (condition == NULL) {
        return NULL;
    }

    if (!seal_condition_init_value(
        condition
    )) {
        free(condition);

        return NULL;
    }

    return condition;
}

bool seal_condition_destroy(
    void *opaque
) {
    SealCondition *condition;

    if (opaque == NULL) {
        return false;
    }

    condition =
        (SealCondition *)opaque;

    if (!seal_condition_destroy_value(
        condition
    )) {
        return false;
    }

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

    return seal_condition_wait_value(
        (SealCondition *)
            conditionOpaque,
        (SealMutex *)
            mutexOpaque,
        0,
        true
    ) > 0;
}

int seal_condition_wait_for(
    void *conditionOpaque,
    void *mutexOpaque,
    uintptr_t milliseconds
) {
    if (conditionOpaque == NULL ||
        mutexOpaque == NULL) {
        return -1;
    }

    return seal_condition_wait_value(
        (SealCondition *)
            conditionOpaque,
        (SealMutex *)
            mutexOpaque,
        milliseconds,
        false
    );
}

bool seal_condition_signal(
    void *opaque
) {
    if (opaque == NULL) {
        return false;
    }

    return seal_condition_signal_value(
        (SealCondition *)opaque
    );
}

bool seal_condition_broadcast(
    void *opaque
) {
    if (opaque == NULL) {
        return false;
    }

    return seal_condition_broadcast_value(
        (SealCondition *)opaque
    );
}

/*
Once API.
*/

void *seal_once_create(
    void
) {
    SealOnce *once;

    once =
        (SealOnce *)malloc(
            sizeof(SealOnce)
        );

    if (once == NULL) {
        return NULL;
    }

    if (!seal_mutex_init_value(
        &once->guard
    )) {
        free(once);

        return NULL;
    }

    if (!seal_condition_init_value(
        &once->completed
    )) {
        seal_mutex_destroy_value(
            &once->guard
        );

        free(once);

        return NULL;
    }

    once->state = 0;

    return once;
}

bool seal_once_destroy(
    void *opaque
) {
    SealOnce *once;

    if (opaque == NULL) {
        return false;
    }

    once =
        (SealOnce *)opaque;

    if (!seal_mutex_lock_value(
        &once->guard
    )) {
        return false;
    }

    if (once->state == 1) {
        seal_mutex_unlock_value(
            &once->guard
        );

        return false;
    }

    seal_mutex_unlock_value(
        &once->guard
    );

    if (!seal_condition_destroy_value(
        &once->completed
    )) {
        return false;
    }

    if (!seal_mutex_destroy_value(
        &once->guard
    )) {
        return false;
    }

    free(once);

    return true;
}

int seal_once_begin(
    void *opaque
) {
    SealOnce *once;
    int waitResult;

    if (opaque == NULL) {
        return -1;
    }

    once =
        (SealOnce *)opaque;

    if (!seal_mutex_lock_value(
        &once->guard
    )) {
        return -1;
    }

    while (once->state == 1) {
        waitResult =
            seal_condition_wait_value(
                &once->completed,
                &once->guard,
                0,
                true
            );

        if (waitResult < 0) {
            seal_mutex_unlock_value(
                &once->guard
            );

            return -1;
        }
    }

    if (once->state == 2) {
        seal_mutex_unlock_value(
            &once->guard
        );

        return 0;
    }

    once->state = 1;

    seal_mutex_unlock_value(
        &once->guard
    );

    return 1;
}

bool seal_once_complete(
    void *opaque
) {
    SealOnce *once;
    bool broadcasted;
    bool unlocked;

    if (opaque == NULL) {
        return false;
    }

    once =
        (SealOnce *)opaque;

    if (!seal_mutex_lock_value(
        &once->guard
    )) {
        return false;
    }

    if (once->state != 1) {
        seal_mutex_unlock_value(
            &once->guard
        );

        return false;
    }

    once->state = 2;

    broadcasted =
        seal_condition_broadcast_value(
            &once->completed
        );

    unlocked =
        seal_mutex_unlock_value(
            &once->guard
        );

    return broadcasted &&
        unlocked;
}

/*
Semaphore API.
*/

void *seal_semaphore_create(
    uintptr_t initialCount
) {
    SealSemaphore *semaphore;

#if defined(_WIN32)

    if (initialCount >
        (uintptr_t)LONG_MAX) {
        return NULL;
    }

#endif

    semaphore =
        (SealSemaphore *)malloc(
            sizeof(SealSemaphore)
        );

    if (semaphore == NULL) {
        return NULL;
    }

#if defined(_WIN32)

    semaphore->handle =
        CreateSemaphoreA(
            NULL,
            (LONG)initialCount,
            LONG_MAX,
            NULL
        );

    if (semaphore->handle == NULL) {
        free(semaphore);

        return NULL;
    }

#else

    if (!seal_mutex_init_value(
        &semaphore->guard
    )) {
        free(semaphore);

        return NULL;
    }

    if (!seal_condition_init_value(
        &semaphore->available
    )) {
        seal_mutex_destroy_value(
            &semaphore->guard
        );

        free(semaphore);

        return NULL;
    }

    semaphore->count =
        initialCount;

#endif

    return semaphore;
}

bool seal_semaphore_destroy(
    void *opaque
) {
    SealSemaphore *semaphore;

    if (opaque == NULL) {
        return false;
    }

    semaphore =
        (SealSemaphore *)opaque;

#if defined(_WIN32)

    if (!CloseHandle(
        semaphore->handle
    )) {
        return false;
    }

#else

    if (!seal_condition_destroy_value(
        &semaphore->available
    )) {
        return false;
    }

    if (!seal_mutex_destroy_value(
        &semaphore->guard
    )) {
        return false;
    }

#endif

    free(semaphore);

    return true;
}

bool seal_semaphore_acquire(
    void *opaque
) {
    SealSemaphore *semaphore;

    if (opaque == NULL) {
        return false;
    }

    semaphore =
        (SealSemaphore *)opaque;

#if defined(_WIN32)

    return WaitForSingleObject(
        semaphore->handle,
        INFINITE
    ) == WAIT_OBJECT_0;

#else

    if (!seal_mutex_lock_value(
        &semaphore->guard
    )) {
        return false;
    }

    while (semaphore->count == 0) {
        if (seal_condition_wait_value(
            &semaphore->available,
            &semaphore->guard,
            0,
            true
        ) < 0) {
            seal_mutex_unlock_value(
                &semaphore->guard
            );

            return false;
        }
    }

    semaphore->count -= 1;

    return seal_mutex_unlock_value(
        &semaphore->guard
    );

#endif
}

int seal_semaphore_try_acquire(
    void *opaque
) {
    SealSemaphore *semaphore;

    if (opaque == NULL) {
        return -1;
    }

    semaphore =
        (SealSemaphore *)opaque;

#if defined(_WIN32)

    {
        DWORD status;

        status =
            WaitForSingleObject(
                semaphore->handle,
                0
            );

        if (status == WAIT_OBJECT_0) {
            return 1;
        }

        if (status == WAIT_TIMEOUT) {
            return 0;
        }

        return -1;
    }

#else

    if (!seal_mutex_lock_value(
        &semaphore->guard
    )) {
        return -1;
    }

    if (semaphore->count == 0) {
        seal_mutex_unlock_value(
            &semaphore->guard
        );

        return 0;
    }

    semaphore->count -= 1;

    if (!seal_mutex_unlock_value(
        &semaphore->guard
    )) {
        return -1;
    }

    return 1;

#endif
}

int seal_semaphore_acquire_for(
    void *opaque,
    uintptr_t milliseconds
) {
    SealSemaphore *semaphore;

    if (opaque == NULL) {
        return -1;
    }

    semaphore =
        (SealSemaphore *)opaque;

#if defined(_WIN32)

    {
        DWORD timeout;
        DWORD status;

        timeout =
            milliseconds >
                (uintptr_t)UINT32_MAX
            ? UINT32_MAX
            : (DWORD)milliseconds;

        status =
            WaitForSingleObject(
                semaphore->handle,
                timeout
            );

        if (status == WAIT_OBJECT_0) {
            return 1;
        }

        if (status == WAIT_TIMEOUT) {
            return 0;
        }

        return -1;
    }

#else

    uint64_t started;
    uintptr_t remaining;
    int waitResult;

    started =
        seal_now_milliseconds();

    if (!seal_mutex_lock_value(
        &semaphore->guard
    )) {
        return -1;
    }

    while (semaphore->count == 0) {
        remaining =
            seal_remaining_milliseconds(
                started,
                milliseconds
            );

        if (remaining == 0) {
            seal_mutex_unlock_value(
                &semaphore->guard
            );

            return 0;
        }

        waitResult =
            seal_condition_wait_value(
                &semaphore->available,
                &semaphore->guard,
                remaining,
                false
            );

        if (waitResult < 0) {
            seal_mutex_unlock_value(
                &semaphore->guard
            );

            return -1;
        }

        if (waitResult == 0 &&
            semaphore->count == 0) {
            seal_mutex_unlock_value(
                &semaphore->guard
            );

            return 0;
        }
    }

    semaphore->count -= 1;

    if (!seal_mutex_unlock_value(
        &semaphore->guard
    )) {
        return -1;
    }

    return 1;

#endif
}

bool seal_semaphore_release(
    void *opaque,
    uintptr_t count
) {
    SealSemaphore *semaphore;

    if (opaque == NULL ||
        count == 0) {
        return false;
    }

    semaphore =
        (SealSemaphore *)opaque;

#if defined(_WIN32)

    if (count >
        (uintptr_t)LONG_MAX) {
        return false;
    }

    return ReleaseSemaphore(
        semaphore->handle,
        (LONG)count,
        NULL
    ) != 0;

#else

    uintptr_t index;

    if (!seal_mutex_lock_value(
        &semaphore->guard
    )) {
        return false;
    }

    if (UINTPTR_MAX -
        semaphore->count <
        count) {
        seal_mutex_unlock_value(
            &semaphore->guard
        );

        return false;
    }

    semaphore->count += count;

    index = 0;

    while (index < count) {
        if (!seal_condition_signal_value(
            &semaphore->available
        )) {
            seal_mutex_unlock_value(
                &semaphore->guard
            );

            return false;
        }

        index += 1;
    }

    return seal_mutex_unlock_value(
        &semaphore->guard
    );

#endif
}

/*
Read-write mutex API.

Waiting writers are preferred over new readers.
*/

void *seal_rw_mutex_create(
    void
) {
    SealRWMutex *mutex;

    mutex =
        (SealRWMutex *)malloc(
            sizeof(SealRWMutex)
        );

    if (mutex == NULL) {
        return NULL;
    }

    if (!seal_mutex_init_value(
        &mutex->guard
    )) {
        free(mutex);

        return NULL;
    }

    if (!seal_condition_init_value(
        &mutex->readers
    )) {
        seal_mutex_destroy_value(
            &mutex->guard
        );

        free(mutex);

        return NULL;
    }

    if (!seal_condition_init_value(
        &mutex->writers
    )) {
        seal_condition_destroy_value(
            &mutex->readers
        );

        seal_mutex_destroy_value(
            &mutex->guard
        );

        free(mutex);

        return NULL;
    }

    mutex->activeReaders = 0;
    mutex->waitingWriters = 0;
    mutex->writerActive = 0;

    return mutex;
}

bool seal_rw_mutex_destroy(
    void *opaque
) {
    SealRWMutex *mutex;

    if (opaque == NULL) {
        return false;
    }

    mutex =
        (SealRWMutex *)opaque;

    if (!seal_mutex_lock_value(
        &mutex->guard
    )) {
        return false;
    }

    if (mutex->activeReaders != 0 ||
        mutex->waitingWriters != 0 ||
        mutex->writerActive) {
        seal_mutex_unlock_value(
            &mutex->guard
        );

        return false;
    }

    seal_mutex_unlock_value(
        &mutex->guard
    );

    if (!seal_condition_destroy_value(
        &mutex->writers
    )) {
        return false;
    }

    if (!seal_condition_destroy_value(
        &mutex->readers
    )) {
        return false;
    }

    if (!seal_mutex_destroy_value(
        &mutex->guard
    )) {
        return false;
    }

    free(mutex);

    return true;
}

bool seal_rw_mutex_read_lock(
    void *opaque
) {
    SealRWMutex *mutex;

    if (opaque == NULL) {
        return false;
    }

    mutex =
        (SealRWMutex *)opaque;

    if (!seal_mutex_lock_value(
        &mutex->guard
    )) {
        return false;
    }

    while (mutex->writerActive ||
        mutex->waitingWriters > 0) {
        if (seal_condition_wait_value(
            &mutex->readers,
            &mutex->guard,
            0,
            true
        ) < 0) {
            seal_mutex_unlock_value(
                &mutex->guard
            );

            return false;
        }
    }

    mutex->activeReaders += 1;

    return seal_mutex_unlock_value(
        &mutex->guard
    );
}

int seal_rw_mutex_try_read_lock(
    void *opaque
) {
    SealRWMutex *mutex;

    if (opaque == NULL) {
        return -1;
    }

    mutex =
        (SealRWMutex *)opaque;

    if (!seal_mutex_lock_value(
        &mutex->guard
    )) {
        return -1;
    }

    if (mutex->writerActive ||
        mutex->waitingWriters > 0) {
        seal_mutex_unlock_value(
            &mutex->guard
        );

        return 0;
    }

    mutex->activeReaders += 1;

    if (!seal_mutex_unlock_value(
        &mutex->guard
    )) {
        return -1;
    }

    return 1;
}

int seal_rw_mutex_read_lock_for(
    void *opaque,
    uintptr_t milliseconds
) {
    SealRWMutex *mutex;
    uint64_t started;
    uintptr_t remaining;
    int waitResult;

    if (opaque == NULL) {
        return -1;
    }

    mutex =
        (SealRWMutex *)opaque;

    started =
        seal_now_milliseconds();

    if (!seal_mutex_lock_value(
        &mutex->guard
    )) {
        return -1;
    }

    while (mutex->writerActive ||
        mutex->waitingWriters > 0) {
        remaining =
            seal_remaining_milliseconds(
                started,
                milliseconds
            );

        if (remaining == 0) {
            seal_mutex_unlock_value(
                &mutex->guard
            );

            return 0;
        }

        waitResult =
            seal_condition_wait_value(
                &mutex->readers,
                &mutex->guard,
                remaining,
                false
            );

        if (waitResult < 0) {
            seal_mutex_unlock_value(
                &mutex->guard
            );

            return -1;
        }

        if (waitResult == 0 &&
            (mutex->writerActive ||
                mutex->waitingWriters >
                    0)) {
            seal_mutex_unlock_value(
                &mutex->guard
            );

            return 0;
        }
    }

    mutex->activeReaders += 1;

    if (!seal_mutex_unlock_value(
        &mutex->guard
    )) {
        return -1;
    }

    return 1;
}

bool seal_rw_mutex_read_unlock(
    void *opaque
) {
    SealRWMutex *mutex;
    bool signaled;

    if (opaque == NULL) {
        return false;
    }

    mutex =
        (SealRWMutex *)opaque;

    if (!seal_mutex_lock_value(
        &mutex->guard
    )) {
        return false;
    }

    if (mutex->activeReaders == 0) {
        seal_mutex_unlock_value(
            &mutex->guard
        );

        return false;
    }

    mutex->activeReaders -= 1;
    signaled = true;

    if (mutex->activeReaders == 0 &&
        mutex->waitingWriters > 0) {
        signaled =
            seal_condition_signal_value(
                &mutex->writers
            );
    }

    return
        seal_mutex_unlock_value(
            &mutex->guard
        ) &&
        signaled;
}

bool seal_rw_mutex_write_lock(
    void *opaque
) {
    SealRWMutex *mutex;

    if (opaque == NULL) {
        return false;
    }

    mutex =
        (SealRWMutex *)opaque;

    if (!seal_mutex_lock_value(
        &mutex->guard
    )) {
        return false;
    }

    mutex->waitingWriters += 1;

    while (mutex->writerActive ||
        mutex->activeReaders > 0) {
        if (seal_condition_wait_value(
            &mutex->writers,
            &mutex->guard,
            0,
            true
        ) < 0) {
            mutex->waitingWriters -= 1;

            seal_mutex_unlock_value(
                &mutex->guard
            );

            return false;
        }
    }

    mutex->waitingWriters -= 1;
    mutex->writerActive = 1;

    return seal_mutex_unlock_value(
        &mutex->guard
    );
}

int seal_rw_mutex_try_write_lock(
    void *opaque
) {
    SealRWMutex *mutex;

    if (opaque == NULL) {
        return -1;
    }

    mutex =
        (SealRWMutex *)opaque;

    if (!seal_mutex_lock_value(
        &mutex->guard
    )) {
        return -1;
    }

    if (mutex->writerActive ||
        mutex->activeReaders > 0) {
        seal_mutex_unlock_value(
            &mutex->guard
        );

        return 0;
    }

    mutex->writerActive = 1;

    if (!seal_mutex_unlock_value(
        &mutex->guard
    )) {
        return -1;
    }

    return 1;
}

int seal_rw_mutex_write_lock_for(
    void *opaque,
    uintptr_t milliseconds
) {
    SealRWMutex *mutex;
    uint64_t started;
    uintptr_t remaining;
    int waitResult;

    if (opaque == NULL) {
        return -1;
    }

    mutex =
        (SealRWMutex *)opaque;

    started =
        seal_now_milliseconds();

    if (!seal_mutex_lock_value(
        &mutex->guard
    )) {
        return -1;
    }

    mutex->waitingWriters += 1;

    while (mutex->writerActive ||
        mutex->activeReaders > 0) {
        remaining =
            seal_remaining_milliseconds(
                started,
                milliseconds
            );

        if (remaining == 0) {
            mutex->waitingWriters -= 1;

            seal_mutex_unlock_value(
                &mutex->guard
            );

            return 0;
        }

        waitResult =
            seal_condition_wait_value(
                &mutex->writers,
                &mutex->guard,
                remaining,
                false
            );

        if (waitResult < 0) {
            mutex->waitingWriters -= 1;

            seal_mutex_unlock_value(
                &mutex->guard
            );

            return -1;
        }

        if (waitResult == 0 &&
            (mutex->writerActive ||
                mutex->activeReaders >
                    0)) {
            mutex->waitingWriters -= 1;

            seal_mutex_unlock_value(
                &mutex->guard
            );

            return 0;
        }
    }

    mutex->waitingWriters -= 1;
    mutex->writerActive = 1;

    if (!seal_mutex_unlock_value(
        &mutex->guard
    )) {
        return -1;
    }

    return 1;
}

bool seal_rw_mutex_write_unlock(
    void *opaque
) {
    SealRWMutex *mutex;
    bool notified;

    if (opaque == NULL) {
        return false;
    }

    mutex =
        (SealRWMutex *)opaque;

    if (!seal_mutex_lock_value(
        &mutex->guard
    )) {
        return false;
    }

    if (!mutex->writerActive) {
        seal_mutex_unlock_value(
            &mutex->guard
        );

        return false;
    }

    mutex->writerActive = 0;

    if (mutex->waitingWriters > 0) {
        notified =
            seal_condition_signal_value(
                &mutex->writers
            );
    } else {
        notified =
            seal_condition_broadcast_value(
                &mutex->readers
            );
    }

    return
        seal_mutex_unlock_value(
            &mutex->guard
        ) &&
        notified;
}

/*
Atomic API.

GCC and Clang use sequentially consistent compiler atomics. TCC and
other compilers use the embedded mutex fallback.
*/

void *seal_atomic_create(
    uintptr_t initialValue
) {
    SealAtomic *atomic;

    atomic =
        (SealAtomic *)malloc(
            sizeof(SealAtomic)
        );

    if (atomic == NULL) {
        return NULL;
    }

    if (!seal_mutex_init_value(
        &atomic->guard
    )) {
        free(atomic);

        return NULL;
    }

    atomic->value = initialValue;

    return atomic;
}

bool seal_atomic_destroy(
    void *opaque
) {
    SealAtomic *atomic;

    if (opaque == NULL) {
        return false;
    }

    atomic =
        (SealAtomic *)opaque;

    if (!seal_mutex_destroy_value(
        &atomic->guard
    )) {
        return false;
    }

    free(atomic);

    return true;
}

uintptr_t seal_atomic_load(
    void *opaque
) {
    SealAtomic *atomic;
    uintptr_t result;

    if (opaque == NULL) {
        return 0;
    }

    atomic =
        (SealAtomic *)opaque;

#if SEAL_HAS_GNU_ATOMICS

    return __atomic_load_n(
        &atomic->value,
        __ATOMIC_SEQ_CST
    );

#else

    seal_mutex_lock_value(
        &atomic->guard
    );

    result = atomic->value;

    seal_mutex_unlock_value(
        &atomic->guard
    );

    return result;

#endif
}

bool seal_atomic_store(
    void *opaque,
    uintptr_t value
) {
    SealAtomic *atomic;

    if (opaque == NULL) {
        return false;
    }

    atomic =
        (SealAtomic *)opaque;

#if SEAL_HAS_GNU_ATOMICS

    __atomic_store_n(
        &atomic->value,
        value,
        __ATOMIC_SEQ_CST
    );

    return true;

#else

    if (!seal_mutex_lock_value(
        &atomic->guard
    )) {
        return false;
    }

    atomic->value = value;

    return seal_mutex_unlock_value(
        &atomic->guard
    );

#endif
}

uintptr_t seal_atomic_exchange(
    void *opaque,
    uintptr_t value
) {
    SealAtomic *atomic;
    uintptr_t previous;

    if (opaque == NULL) {
        return 0;
    }

    atomic =
        (SealAtomic *)opaque;

#if SEAL_HAS_GNU_ATOMICS

    return __atomic_exchange_n(
        &atomic->value,
        value,
        __ATOMIC_SEQ_CST
    );

#else

    seal_mutex_lock_value(
        &atomic->guard
    );

    previous = atomic->value;
    atomic->value = value;

    seal_mutex_unlock_value(
        &atomic->guard
    );

    return previous;

#endif
}

bool seal_atomic_compare_exchange(
    void *opaque,
    uintptr_t expected,
    uintptr_t desired
) {
    SealAtomic *atomic;

    if (opaque == NULL) {
        return false;
    }

    atomic =
        (SealAtomic *)opaque;

#if SEAL_HAS_GNU_ATOMICS

    return __atomic_compare_exchange_n(
        &atomic->value,
        &expected,
        desired,
        false,
        __ATOMIC_SEQ_CST,
        __ATOMIC_SEQ_CST
    );

#else

    bool exchanged;

    seal_mutex_lock_value(
        &atomic->guard
    );

    exchanged =
        atomic->value ==
        expected;

    if (exchanged) {
        atomic->value = desired;
    }

    seal_mutex_unlock_value(
        &atomic->guard
    );

    return exchanged;

#endif
}

uintptr_t seal_atomic_fetch_add(
    void *opaque,
    uintptr_t amount
) {
    SealAtomic *atomic;
    uintptr_t previous;

    if (opaque == NULL) {
        return 0;
    }

    atomic =
        (SealAtomic *)opaque;

#if SEAL_HAS_GNU_ATOMICS

    return __atomic_fetch_add(
        &atomic->value,
        amount,
        __ATOMIC_SEQ_CST
    );

#else

    seal_mutex_lock_value(
        &atomic->guard
    );

    previous = atomic->value;
    atomic->value += amount;

    seal_mutex_unlock_value(
        &atomic->guard
    );

    return previous;

#endif
}

uintptr_t seal_atomic_fetch_sub(
    void *opaque,
    uintptr_t amount
) {
    SealAtomic *atomic;
    uintptr_t previous;

    if (opaque == NULL) {
        return 0;
    }

    atomic =
        (SealAtomic *)opaque;

#if SEAL_HAS_GNU_ATOMICS

    return __atomic_fetch_sub(
        &atomic->value,
        amount,
        __ATOMIC_SEQ_CST
    );

#else

    seal_mutex_lock_value(
        &atomic->guard
    );

    previous = atomic->value;
    atomic->value -= amount;

    seal_mutex_unlock_value(
        &atomic->guard
    );

    return previous;

#endif
}

uintptr_t seal_atomic_fetch_and(
    void *opaque,
    uintptr_t value
) {
    SealAtomic *atomic;
    uintptr_t previous;

    if (opaque == NULL) {
        return 0;
    }

    atomic =
        (SealAtomic *)opaque;

#if SEAL_HAS_GNU_ATOMICS

    return __atomic_fetch_and(
        &atomic->value,
        value,
        __ATOMIC_SEQ_CST
    );

#else

    seal_mutex_lock_value(
        &atomic->guard
    );

    previous = atomic->value;
    atomic->value &= value;

    seal_mutex_unlock_value(
        &atomic->guard
    );

    return previous;

#endif
}

uintptr_t seal_atomic_fetch_or(
    void *opaque,
    uintptr_t value
) {
    SealAtomic *atomic;
    uintptr_t previous;

    if (opaque == NULL) {
        return 0;
    }

    atomic =
        (SealAtomic *)opaque;

#if SEAL_HAS_GNU_ATOMICS

    return __atomic_fetch_or(
        &atomic->value,
        value,
        __ATOMIC_SEQ_CST
    );

#else

    seal_mutex_lock_value(
        &atomic->guard
    );

    previous = atomic->value;
    atomic->value |= value;

    seal_mutex_unlock_value(
        &atomic->guard
    );

    return previous;

#endif
}

uintptr_t seal_atomic_fetch_xor(
    void *opaque,
    uintptr_t value
) {
    SealAtomic *atomic;
    uintptr_t previous;

    if (opaque == NULL) {
        return 0;
    }

    atomic =
        (SealAtomic *)opaque;

#if SEAL_HAS_GNU_ATOMICS

    return __atomic_fetch_xor(
        &atomic->value,
        value,
        __ATOMIC_SEQ_CST
    );

#else

    seal_mutex_lock_value(
        &atomic->guard
    );

    previous = atomic->value;
    atomic->value ^= value;

    seal_mutex_unlock_value(
        &atomic->guard
    );

    return previous;

#endif
}
