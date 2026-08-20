#ifndef FOUNDATION_PLUGIN_H
#define FOUNDATION_PLUGIN_H

#include <stdint.h>

#define FOUNDATION_ABI_MAJOR 1u
#define FOUNDATION_ABI_MINOR 0u
#define FOUNDATION_ERROR_RESULT UINT64_MAX

#if defined(__wasm__)
#define FOUNDATION_EXPORT(name) __attribute__((export_name(name)))
#define FOUNDATION_IMPORT(name) \
    __attribute__((import_module("foundation"), import_name(name)))
#else
#define FOUNDATION_EXPORT(name)
#define FOUNDATION_IMPORT(name)
#endif

enum foundation_host_status {
    FOUNDATION_HOST_OK = 0,
    FOUNDATION_HOST_DENIED = 1,
    FOUNDATION_HOST_INVALID_REQUEST = 2,
    FOUNDATION_HOST_HANDLER_ERROR = 3,
    FOUNDATION_HOST_PAYLOAD_TOO_LARGE = 4,
};

FOUNDATION_IMPORT("host_call")
uint32_t foundation_host_call(
    uint32_t capability_pointer,
    uint32_t capability_length,
    uint32_t operation_pointer,
    uint32_t operation_length,
    uint32_t input_pointer,
    uint32_t input_length
);

FOUNDATION_IMPORT("host_response_len")
uint32_t foundation_host_response_len(void);

FOUNDATION_IMPORT("host_response_read")
int32_t foundation_host_response_read(uint32_t pointer, uint32_t capacity);

FOUNDATION_IMPORT("host_error_len")
uint32_t foundation_host_error_len(void);

FOUNDATION_IMPORT("host_error_read")
int32_t foundation_host_error_read(uint32_t pointer, uint32_t capacity);

static inline uint64_t foundation_abi_version_value(void) {
    return ((uint64_t)FOUNDATION_ABI_MAJOR << 32u) | FOUNDATION_ABI_MINOR;
}

static inline uint64_t foundation_buffer(uint32_t pointer, uint32_t length) {
    return ((uint64_t)pointer << 32u) | length;
}

#endif
