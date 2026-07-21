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

/*
Each Windows condition waiter receives its own auto-reset event.

This avoids depending on CONDITION_VARIABLE and prevents a notification
from being consumed by a waiter that arrived after Signal was called.
*/
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

/*
state values:

0: initialization has not started
1: initialization is running
2: initialization completed
*/
typedef struct SealOnce {
    CRITICAL_SECTION guard;
    HANDLE completed;
    int state;
} SealOnce;

/*
seal_condition_remove_waiter removes target from condition.

The caller must own condition->guard.
*/
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

    return (void *)result;
}

#endif

void *seal_thread_start(
    void *entry,
    void *context
) {
    SealThreadEntry callback;
    SealThreadStart *start;
    SealThreadHandle *thread;

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

    if (pthread_create(
        &thread->handle,
        NULL,
        seal_thread_trampoline,
        start
    ) != 0) {
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

#endif

    free(thread);

    return true;
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

#endif

    free(thread);

    return true;
}

void *seal_thread_allocate_context(
    uintptr_t size
) {
    size_t allocationSize;

    allocationSize =
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

    /*
    Sleep(0) yields the remainder of the current time slice to another
    ready thread.
    */
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

void *seal_mutex_create(void) {
    SealMutex *mutex;

    mutex =
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
    SealMutex *mutex;

    if (opaque == NULL) {
        return false;
    }

    mutex =
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
    SealMutex *mutex;

    if (opaque == NULL) {
        return false;
    }

    mutex =
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
    SealMutex *mutex;

    if (opaque == NULL) {
        return -1;
    }

    mutex =
        (SealMutex *)opaque;

#if defined(_WIN32)

    if (TryEnterCriticalSection(
        &mutex->value
    )) {
        return 1;
    }

    return 0;

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

bool seal_mutex_unlock(
    void *opaque
) {
    SealMutex *mutex;

    if (opaque == NULL) {
        return false;
    }

    mutex =
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
    SealCondition *condition;

    condition =
        (SealCondition *)malloc(
            sizeof(SealCondition)
        );

    if (condition == NULL) {
        return NULL;
    }

#if defined(_WIN32)

    InitializeCriticalSection(
        &condition->guard
    );

    condition->head = NULL;
    condition->tail = NULL;

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
    SealCondition *condition;

    if (opaque == NULL) {
        return false;
    }

    condition =
        (SealCondition *)opaque;

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

#else

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
    SealCondition *condition;
    SealMutex *mutex;

    if (conditionOpaque == NULL ||
        mutexOpaque == NULL) {
        return false;
    }

    condition =
        (SealCondition *)
            conditionOpaque;

    mutex =
        (SealMutex *)
            mutexOpaque;

#if defined(_WIN32)

    SealConditionWaiter waiter;
    DWORD waitStatus;

    /*
    An auto-reset event represents exactly one waiter notification.
    */
    waiter.event = CreateEventA(
        NULL,
        FALSE,
        FALSE,
        NULL
    );

    if (waiter.event == NULL) {
        return false;
    }

    waiter.next = NULL;

    /*
    Publish the waiter while the caller still owns the external mutex.

    A signal that occurs after publication cannot be lost because the
    event remains signaled until this waiter consumes it.
    */
    EnterCriticalSection(
        &condition->guard
    );

    if (condition->tail == NULL) {
        condition->head =
            &waiter;

        condition->tail =
            &waiter;
    } else {
        condition->tail->next =
            &waiter;

        condition->tail =
            &waiter;
    }

    LeaveCriticalSection(
        &condition->guard
    );

    /*
    Release the caller's mutex only after registration.
    */
    LeaveCriticalSection(
        &mutex->value
    );

    waitStatus =
        WaitForSingleObject(
            waiter.event,
            INFINITE
        );

    /*
    Condition waits always return with the caller's mutex reacquired.
    */
    EnterCriticalSection(
        &mutex->value
    );

    if (waitStatus !=
        WAIT_OBJECT_0) {
        /*
        The waiter might still be queued if the wait operation itself
        failed. Remove it before destroying its event.
        */
        EnterCriticalSection(
            &condition->guard
        );

        (void)
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

        return false;
    }

    CloseHandle(
        waiter.event
    );

    return true;

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
    SealCondition *condition;

    if (opaque == NULL) {
        return false;
    }

    condition =
        (SealCondition *)opaque;

#if defined(_WIN32)

    SealConditionWaiter *waiter;

    EnterCriticalSection(
        &condition->guard
    );

    waiter =
        condition->head;

    if (waiter == NULL) {
        LeaveCriticalSection(
            &condition->guard
        );

        return true;
    }

    /*
    Do not remove the waiter unless its event was successfully signaled.
    */
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

bool seal_condition_broadcast(
    void *opaque
) {
    SealCondition *condition;

    if (opaque == NULL) {
        return false;
    }

    condition =
        (SealCondition *)opaque;

#if defined(_WIN32)

    SealConditionWaiter *waiter;

    EnterCriticalSection(
        &condition->guard
    );

    while (condition->head != NULL) {
        waiter =
            condition->head;

        /*
        Keep this waiter and all later waiters queued if signaling fails.
        Successfully signaled waiters have already been removed.
        */
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

void *seal_once_create(void) {
    SealOnce *once;

    once =
        (SealOnce *)malloc(
            sizeof(SealOnce)
        );

    if (once == NULL) {
        return NULL;
    }

    once->state = 0;

#if defined(_WIN32)

    InitializeCriticalSection(
        &once->guard
    );

    /*
    A manual-reset event wakes all current waiters and remains signaled
    for callers that arrive after initialization completes.
    */
    once->completed = CreateEventA(
        NULL,
        TRUE,
        FALSE,
        NULL
    );

    if (once->completed == NULL) {
        DeleteCriticalSection(
            &once->guard
        );

        free(once);

        return NULL;
    }

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
    SealOnce *once;

    if (opaque == NULL) {
        return false;
    }

    once =
        (SealOnce *)opaque;

#if defined(_WIN32)

    EnterCriticalSection(
        &once->guard
    );

    /*
    Destroying while initialization is running could invalidate the event
    while another thread is waiting on it.
    */
    if (once->state == 1) {
        LeaveCriticalSection(
            &once->guard
        );

        return false;
    }

    LeaveCriticalSection(
        &once->guard
    );

    if (!CloseHandle(
        once->completed
    )) {
        return false;
    }

    DeleteCriticalSection(
        &once->guard
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
    SealOnce *once;

    if (opaque == NULL) {
        return -1;
    }

    once =
        (SealOnce *)opaque;

#if defined(_WIN32)

    int state;
    DWORD waitStatus;

    EnterCriticalSection(
        &once->guard
    );

    state = once->state;

    if (state == 0) {
        once->state = 1;

        LeaveCriticalSection(
            &once->guard
        );

        return 1;
    }

    if (state == 2) {
        LeaveCriticalSection(
            &once->guard
        );

        return 0;
    }

    LeaveCriticalSection(
        &once->guard
    );

    waitStatus =
        WaitForSingleObject(
            once->completed,
            INFINITE
        );

    if (waitStatus !=
        WAIT_OBJECT_0) {
        return -1;
    }

    return 0;

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
    SealOnce *once;

    if (opaque == NULL) {
        return false;
    }

    once =
        (SealOnce *)opaque;

#if defined(_WIN32)

    EnterCriticalSection(
        &once->guard
    );

    if (once->state != 1) {
        LeaveCriticalSection(
            &once->guard
        );

        return false;
    }

    /*
    Signal the event before publishing state 2. A caller observing state 2
    can therefore rely on the event already being signaled.
    */
    if (!SetEvent(
        once->completed
    )) {
        LeaveCriticalSection(
            &once->guard
        );

        return false;
    }

    once->state = 2;

    LeaveCriticalSection(
        &once->guard
    );

    return true;

#else

    int broadcastStatus;
    int unlockStatus;

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

    broadcastStatus =
        pthread_cond_broadcast(
            &once->condition
        );

    unlockStatus =
        pthread_mutex_unlock(
            &once->mutex
        );

    return broadcastStatus == 0 &&
        unlockStatus == 0;

#endif
}
