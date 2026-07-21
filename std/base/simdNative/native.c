#include <stdint.h>

#if defined(__TINYC__) || \
    defined(SEAL_SIMD_FORCE_SCALAR)

#define SEAL_SIMD_USE_SIMDE 0

#else

#define SEAL_SIMD_USE_SIMDE 1

#include "vendor/simde/simde/x86/avx2.h"

#endif

int seal_simd_backend(
    void
) {
#if SEAL_SIMD_USE_SIMDE

    return 1;

#else

    return 0;

#endif
}

/*
f32 operations.
*/

void seal_simd_add_f32(
    float *destination,
    const float *left,
    const float *right,
    uintptr_t length
) {
    uintptr_t index;

    index = 0;

#if SEAL_SIMD_USE_SIMDE

    while (index + 8u <= length) {
        simde__m256 leftVector;
        simde__m256 rightVector;
        simde__m256 result;

        leftVector =
            simde_mm256_loadu_ps(
                left + index
            );

        rightVector =
            simde_mm256_loadu_ps(
                right + index
            );

        result =
            simde_mm256_add_ps(
                leftVector,
                rightVector
            );

        simde_mm256_storeu_ps(
            destination + index,
            result
        );

        index += 8u;
    }

#endif

    while (index < length) {
        destination[index] =
            left[index] +
            right[index];

        index += 1u;
    }
}

void seal_simd_sub_f32(
    float *destination,
    const float *left,
    const float *right,
    uintptr_t length
) {
    uintptr_t index;

    index = 0;

#if SEAL_SIMD_USE_SIMDE

    while (index + 8u <= length) {
        simde__m256 leftVector;
        simde__m256 rightVector;
        simde__m256 result;

        leftVector =
            simde_mm256_loadu_ps(
                left + index
            );

        rightVector =
            simde_mm256_loadu_ps(
                right + index
            );

        result =
            simde_mm256_sub_ps(
                leftVector,
                rightVector
            );

        simde_mm256_storeu_ps(
            destination + index,
            result
        );

        index += 8u;
    }

#endif

    while (index < length) {
        destination[index] =
            left[index] -
            right[index];

        index += 1u;
    }
}

void seal_simd_mul_f32(
    float *destination,
    const float *left,
    const float *right,
    uintptr_t length
) {
    uintptr_t index;

    index = 0;

#if SEAL_SIMD_USE_SIMDE

    while (index + 8u <= length) {
        simde__m256 leftVector;
        simde__m256 rightVector;
        simde__m256 result;

        leftVector =
            simde_mm256_loadu_ps(
                left + index
            );

        rightVector =
            simde_mm256_loadu_ps(
                right + index
            );

        result =
            simde_mm256_mul_ps(
                leftVector,
                rightVector
            );

        simde_mm256_storeu_ps(
            destination + index,
            result
        );

        index += 8u;
    }

#endif

    while (index < length) {
        destination[index] =
            left[index] *
            right[index];

        index += 1u;
    }
}

void seal_simd_div_f32(
    float *destination,
    const float *left,
    const float *right,
    uintptr_t length
) {
    uintptr_t index;

    index = 0;

#if SEAL_SIMD_USE_SIMDE

    while (index + 8u <= length) {
        simde__m256 leftVector;
        simde__m256 rightVector;
        simde__m256 result;

        leftVector =
            simde_mm256_loadu_ps(
                left + index
            );

        rightVector =
            simde_mm256_loadu_ps(
                right + index
            );

        result =
            simde_mm256_div_ps(
                leftVector,
                rightVector
            );

        simde_mm256_storeu_ps(
            destination + index,
            result
        );

        index += 8u;
    }

#endif

    while (index < length) {
        destination[index] =
            left[index] /
            right[index];

        index += 1u;
    }
}

void seal_simd_scale_f32(
    float *destination,
    const float *source,
    float scalar,
    uintptr_t length
) {
    uintptr_t index;

    index = 0;

#if SEAL_SIMD_USE_SIMDE

    {
        simde__m256 scalarVector;

        scalarVector =
            simde_mm256_set1_ps(
                scalar
            );

        while (index + 8u <= length) {
            simde__m256 sourceVector;
            simde__m256 result;

            sourceVector =
                simde_mm256_loadu_ps(
                    source + index
                );

            result =
                simde_mm256_mul_ps(
                    sourceVector,
                    scalarVector
                );

            simde_mm256_storeu_ps(
                destination + index,
                result
            );

            index += 8u;
        }
    }

#endif

    while (index < length) {
        destination[index] =
            source[index] *
            scalar;

        index += 1u;
    }
}

float seal_simd_sum_f32(
    const float *source,
    uintptr_t length
) {
    uintptr_t index;
    float result;

    index = 0;
    result = 0.0f;

#if SEAL_SIMD_USE_SIMDE

    {
        simde__m256 accumulator;
        float lanes[8];
        uintptr_t lane;

        accumulator =
            simde_mm256_setzero_ps();

        while (index + 8u <= length) {
            accumulator =
                simde_mm256_add_ps(
                    accumulator,
                    simde_mm256_loadu_ps(
                        source + index
                    )
                );

            index += 8u;
        }

        simde_mm256_storeu_ps(
            lanes,
            accumulator
        );

        lane = 0;

        while (lane < 8u) {
            result += lanes[lane];
            lane += 1u;
        }
    }

#endif

    while (index < length) {
        result += source[index];
        index += 1u;
    }

    return result;
}

float seal_simd_dot_f32(
    const float *left,
    const float *right,
    uintptr_t length
) {
    uintptr_t index;
    float result;

    index = 0;
    result = 0.0f;

#if SEAL_SIMD_USE_SIMDE

    {
        simde__m256 accumulator;
        float lanes[8];
        uintptr_t lane;

        accumulator =
            simde_mm256_setzero_ps();

        while (index + 8u <= length) {
            simde__m256 leftVector;
            simde__m256 rightVector;
            simde__m256 product;

            leftVector =
                simde_mm256_loadu_ps(
                    left + index
                );

            rightVector =
                simde_mm256_loadu_ps(
                    right + index
                );

            product =
                simde_mm256_mul_ps(
                    leftVector,
                    rightVector
                );

            accumulator =
                simde_mm256_add_ps(
                    accumulator,
                    product
                );

            index += 8u;
        }

        simde_mm256_storeu_ps(
            lanes,
            accumulator
        );

        lane = 0;

        while (lane < 8u) {
            result += lanes[lane];
            lane += 1u;
        }
    }

#endif

    while (index < length) {
        result +=
            left[index] *
            right[index];

        index += 1u;
    }

    return result;
}

/*
f64 operations.
*/

void seal_simd_add_f64(
    double *destination,
    const double *left,
    const double *right,
    uintptr_t length
) {
    uintptr_t index;

    index = 0;

#if SEAL_SIMD_USE_SIMDE

    while (index + 4u <= length) {
        simde__m256d leftVector;
        simde__m256d rightVector;
        simde__m256d result;

        leftVector =
            simde_mm256_loadu_pd(
                left + index
            );

        rightVector =
            simde_mm256_loadu_pd(
                right + index
            );

        result =
            simde_mm256_add_pd(
                leftVector,
                rightVector
            );

        simde_mm256_storeu_pd(
            destination + index,
            result
        );

        index += 4u;
    }

#endif

    while (index < length) {
        destination[index] =
            left[index] +
            right[index];

        index += 1u;
    }
}

void seal_simd_sub_f64(
    double *destination,
    const double *left,
    const double *right,
    uintptr_t length
) {
    uintptr_t index;

    index = 0;

#if SEAL_SIMD_USE_SIMDE

    while (index + 4u <= length) {
        simde__m256d leftVector;
        simde__m256d rightVector;
        simde__m256d result;

        leftVector =
            simde_mm256_loadu_pd(
                left + index
            );

        rightVector =
            simde_mm256_loadu_pd(
                right + index
            );

        result =
            simde_mm256_sub_pd(
                leftVector,
                rightVector
            );

        simde_mm256_storeu_pd(
            destination + index,
            result
        );

        index += 4u;
    }

#endif

    while (index < length) {
        destination[index] =
            left[index] -
            right[index];

        index += 1u;
    }
}

void seal_simd_mul_f64(
    double *destination,
    const double *left,
    const double *right,
    uintptr_t length
) {
    uintptr_t index;

    index = 0;

#if SEAL_SIMD_USE_SIMDE

    while (index + 4u <= length) {
        simde__m256d leftVector;
        simde__m256d rightVector;
        simde__m256d result;

        leftVector =
            simde_mm256_loadu_pd(
                left + index
            );

        rightVector =
            simde_mm256_loadu_pd(
                right + index
            );

        result =
            simde_mm256_mul_pd(
                leftVector,
                rightVector
            );

        simde_mm256_storeu_pd(
            destination + index,
            result
        );

        index += 4u;
    }

#endif

    while (index < length) {
        destination[index] =
            left[index] *
            right[index];

        index += 1u;
    }
}

void seal_simd_div_f64(
    double *destination,
    const double *left,
    const double *right,
    uintptr_t length
) {
    uintptr_t index;

    index = 0;

#if SEAL_SIMD_USE_SIMDE

    while (index + 4u <= length) {
        simde__m256d leftVector;
        simde__m256d rightVector;
        simde__m256d result;

        leftVector =
            simde_mm256_loadu_pd(
                left + index
            );

        rightVector =
            simde_mm256_loadu_pd(
                right + index
            );

        result =
            simde_mm256_div_pd(
                leftVector,
                rightVector
            );

        simde_mm256_storeu_pd(
            destination + index,
            result
        );

        index += 4u;
    }

#endif

    while (index < length) {
        destination[index] =
            left[index] /
            right[index];

        index += 1u;
    }
}

void seal_simd_scale_f64(
    double *destination,
    const double *source,
    double scalar,
    uintptr_t length
) {
    uintptr_t index;

    index = 0;

#if SEAL_SIMD_USE_SIMDE

    {
        simde__m256d scalarVector;

        scalarVector =
            simde_mm256_set1_pd(
                scalar
            );

        while (index + 4u <= length) {
            simde__m256d sourceVector;
            simde__m256d result;

            sourceVector =
                simde_mm256_loadu_pd(
                    source + index
                );

            result =
                simde_mm256_mul_pd(
                    sourceVector,
                    scalarVector
                );

            simde_mm256_storeu_pd(
                destination + index,
                result
            );

            index += 4u;
        }
    }

#endif

    while (index < length) {
        destination[index] =
            source[index] *
            scalar;

        index += 1u;
    }
}

double seal_simd_sum_f64(
    const double *source,
    uintptr_t length
) {
    uintptr_t index;
    double result;

    index = 0;
    result = 0.0;

#if SEAL_SIMD_USE_SIMDE

    {
        simde__m256d accumulator;
        double lanes[4];
        uintptr_t lane;

        accumulator =
            simde_mm256_setzero_pd();

        while (index + 4u <= length) {
            accumulator =
                simde_mm256_add_pd(
                    accumulator,
                    simde_mm256_loadu_pd(
                        source + index
                    )
                );

            index += 4u;
        }

        simde_mm256_storeu_pd(
            lanes,
            accumulator
        );

        lane = 0;

        while (lane < 4u) {
            result += lanes[lane];
            lane += 1u;
        }
    }

#endif

    while (index < length) {
        result += source[index];
        index += 1u;
    }

    return result;
}

double seal_simd_dot_f64(
    const double *left,
    const double *right,
    uintptr_t length
) {
    uintptr_t index;
    double result;

    index = 0;
    result = 0.0;

#if SEAL_SIMD_USE_SIMDE

    {
        simde__m256d accumulator;
        double lanes[4];
        uintptr_t lane;

        accumulator =
            simde_mm256_setzero_pd();

        while (index + 4u <= length) {
            simde__m256d leftVector;
            simde__m256d rightVector;
            simde__m256d product;

            leftVector =
                simde_mm256_loadu_pd(
                    left + index
                );

            rightVector =
                simde_mm256_loadu_pd(
                    right + index
                );

            product =
                simde_mm256_mul_pd(
                    leftVector,
                    rightVector
                );

            accumulator =
                simde_mm256_add_pd(
                    accumulator,
                    product
                );

            index += 4u;
        }

        simde_mm256_storeu_pd(
            lanes,
            accumulator
        );

        lane = 0;

        while (lane < 4u) {
            result += lanes[lane];
            lane += 1u;
        }
    }

#endif

    while (index < length) {
        result +=
            left[index] *
            right[index];

        index += 1u;
    }

    return result;
}

/*
u32 operations.
*/

void seal_simd_add_u32(
    uint32_t *destination,
    const uint32_t *left,
    const uint32_t *right,
    uintptr_t length
) {
    uintptr_t index;

    index = 0;

#if SEAL_SIMD_USE_SIMDE

    while (index + 8u <= length) {
        simde__m256i leftVector;
        simde__m256i rightVector;
        simde__m256i result;

        leftVector =
            simde_mm256_loadu_si256(
                (simde__m256i const *)
                    (left + index)
            );

        rightVector =
            simde_mm256_loadu_si256(
                (simde__m256i const *)
                    (right + index)
            );

        result =
            simde_mm256_add_epi32(
                leftVector,
                rightVector
            );

        simde_mm256_storeu_si256(
            (simde__m256i *)
                (destination + index),
            result
        );

        index += 8u;
    }

#endif

    while (index < length) {
        destination[index] =
            left[index] +
            right[index];

        index += 1u;
    }
}

void seal_simd_sub_u32(
    uint32_t *destination,
    const uint32_t *left,
    const uint32_t *right,
    uintptr_t length
) {
    uintptr_t index;

    index = 0;

#if SEAL_SIMD_USE_SIMDE

    while (index + 8u <= length) {
        simde__m256i leftVector;
        simde__m256i rightVector;
        simde__m256i result;

        leftVector =
            simde_mm256_loadu_si256(
                (simde__m256i const *)
                    (left + index)
            );

        rightVector =
            simde_mm256_loadu_si256(
                (simde__m256i const *)
                    (right + index)
            );

        result =
            simde_mm256_sub_epi32(
                leftVector,
                rightVector
            );

        simde_mm256_storeu_si256(
            (simde__m256i *)
                (destination + index),
            result
        );

        index += 8u;
    }

#endif

    while (index < length) {
        destination[index] =
            left[index] -
            right[index];

        index += 1u;
    }
}

void seal_simd_mul_u32(
    uint32_t *destination,
    const uint32_t *left,
    const uint32_t *right,
    uintptr_t length
) {
    uintptr_t index;

    index = 0;

#if SEAL_SIMD_USE_SIMDE

    while (index + 8u <= length) {
        simde__m256i leftVector;
        simde__m256i rightVector;
        simde__m256i result;

        leftVector =
            simde_mm256_loadu_si256(
                (simde__m256i const *)
                    (left + index)
            );

        rightVector =
            simde_mm256_loadu_si256(
                (simde__m256i const *)
                    (right + index)
            );

        result =
            simde_mm256_mullo_epi32(
                leftVector,
                rightVector
            );

        simde_mm256_storeu_si256(
            (simde__m256i *)
                (destination + index),
            result
        );

        index += 8u;
    }

#endif

    while (index < length) {
        destination[index] =
            left[index] *
            right[index];

        index += 1u;
    }
}

void seal_simd_and_u32(
    uint32_t *destination,
    const uint32_t *left,
    const uint32_t *right,
    uintptr_t length
) {
    uintptr_t index;

    index = 0;

#if SEAL_SIMD_USE_SIMDE

    while (index + 8u <= length) {
        simde__m256i leftVector;
        simde__m256i rightVector;
        simde__m256i result;

        leftVector =
            simde_mm256_loadu_si256(
                (simde__m256i const *)
                    (left + index)
            );

        rightVector =
            simde_mm256_loadu_si256(
                (simde__m256i const *)
                    (right + index)
            );

        result =
            simde_mm256_and_si256(
                leftVector,
                rightVector
            );

        simde_mm256_storeu_si256(
            (simde__m256i *)
                (destination + index),
            result
        );

        index += 8u;
    }

#endif

    while (index < length) {
        destination[index] =
            left[index] &
            right[index];

        index += 1u;
    }
}

void seal_simd_or_u32(
    uint32_t *destination,
    const uint32_t *left,
    const uint32_t *right,
    uintptr_t length
) {
    uintptr_t index;

    index = 0;

#if SEAL_SIMD_USE_SIMDE

    while (index + 8u <= length) {
        simde__m256i leftVector;
        simde__m256i rightVector;
        simde__m256i result;

        leftVector =
            simde_mm256_loadu_si256(
                (simde__m256i const *)
                    (left + index)
            );

        rightVector =
            simde_mm256_loadu_si256(
                (simde__m256i const *)
                    (right + index)
            );

        result =
            simde_mm256_or_si256(
                leftVector,
                rightVector
            );

        simde_mm256_storeu_si256(
            (simde__m256i *)
                (destination + index),
            result
        );

        index += 8u;
    }

#endif

    while (index < length) {
        destination[index] =
            left[index] |
            right[index];

        index += 1u;
    }
}

void seal_simd_xor_u32(
    uint32_t *destination,
    const uint32_t *left,
    const uint32_t *right,
    uintptr_t length
) {
    uintptr_t index;

    index = 0;

#if SEAL_SIMD_USE_SIMDE

    while (index + 8u <= length) {
        simde__m256i leftVector;
        simde__m256i rightVector;
        simde__m256i result;

        leftVector =
            simde_mm256_loadu_si256(
                (simde__m256i const *)
                    (left + index)
            );

        rightVector =
            simde_mm256_loadu_si256(
                (simde__m256i const *)
                    (right + index)
            );

        result =
            simde_mm256_xor_si256(
                leftVector,
                rightVector
            );

        simde_mm256_storeu_si256(
            (simde__m256i *)
                (destination + index),
            result
        );

        index += 8u;
    }

#endif

    while (index < length) {
        destination[index] =
            left[index] ^
            right[index];

        index += 1u;
    }
}
