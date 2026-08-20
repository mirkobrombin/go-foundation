#include "../abi/foundation_plugin.h"

#include <stddef.h>
#include <stdint.h>

extern unsigned char __heap_base;

static uint32_t heap_pointer;
static char last_error[256] = "no error";
static const char metadata[] =
    "{\"name\":\"fixture\",\"version\":\"1.0.0\","
    "\"description\":\"Foundation test plugin\","
    "\"methods\":[\"echo\",\"capability\",\"fail\",\"hang\"],"
    "\"capabilities\":[\"test.echo\"],"
    "\"properties\":{\"language\":\"c\"}}";

static uint32_t allocate(uint32_t size) {
    if (heap_pointer == 0) {
        heap_pointer = (uint32_t)(uintptr_t)&__heap_base;
    }
    uint32_t pointer = heap_pointer;
    heap_pointer = (heap_pointer + size + 7u) & ~7u;
    return pointer;
}

static void copy_bytes(char *destination, const char *source, uint32_t length) {
    for (uint32_t index = 0; index < length; index++) {
        destination[index] = source[index];
    }
}

static int equal(const char *left, uint32_t left_length, const char *right, uint32_t right_length) {
    if (left_length != right_length) {
        return 0;
    }
    for (uint32_t index = 0; index < left_length; index++) {
        if (left[index] != right[index]) {
            return 0;
        }
    }
    return 1;
}

static uint64_t fail_with(const char *message, uint32_t length) {
    if (length > sizeof(last_error)) {
        length = sizeof(last_error);
    }
    copy_bytes(last_error, message, length);
    if (length < sizeof(last_error)) {
        last_error[length] = '\0';
    }
    return FOUNDATION_ERROR_RESULT;
}

FOUNDATION_EXPORT("foundation_abi_version")
uint64_t foundation_abi_version(void) {
    return foundation_abi_version_value();
}

FOUNDATION_EXPORT("foundation_alloc")
uint32_t foundation_alloc(uint32_t size) {
    return allocate(size);
}

FOUNDATION_EXPORT("foundation_free")
void foundation_free(uint32_t pointer, uint32_t size) {
    (void)pointer;
    (void)size;
}

FOUNDATION_EXPORT("foundation_metadata")
uint64_t foundation_metadata(void) {
    return foundation_buffer((uint32_t)(uintptr_t)metadata, sizeof(metadata) - 1u);
}

FOUNDATION_EXPORT("foundation_start")
uint32_t foundation_start(void) {
    return 0;
}

FOUNDATION_EXPORT("foundation_stop")
uint32_t foundation_stop(void) {
    return 0;
}

FOUNDATION_EXPORT("foundation_last_error")
uint64_t foundation_last_error(void) {
    uint32_t length = 0;
    while (length < sizeof(last_error) && last_error[length] != '\0') {
        length++;
    }
    return foundation_buffer((uint32_t)(uintptr_t)last_error, length);
}

FOUNDATION_EXPORT("foundation_call")
uint64_t foundation_call(
    uint32_t method_pointer,
    uint32_t method_length,
    uint32_t input_pointer,
    uint32_t input_length
) {
    const char *method = (const char *)(uintptr_t)method_pointer;
    const char *input = (const char *)(uintptr_t)input_pointer;
    if (equal(method, method_length, "echo", 4)) {
        uint32_t output = allocate(input_length);
        copy_bytes((char *)(uintptr_t)output, input, input_length);
        return foundation_buffer(output, input_length);
    }
    if (equal(method, method_length, "capability", 10)) {
        uint32_t status = foundation_host_call(
            (uint32_t)(uintptr_t)"test.echo",
            9,
            (uint32_t)(uintptr_t)"invoke",
            6,
            input_pointer,
            input_length
        );
        if (status != FOUNDATION_HOST_OK) {
            uint32_t length = foundation_host_error_len();
            if (length > sizeof(last_error)) {
                length = sizeof(last_error);
            }
            foundation_host_error_read((uint32_t)(uintptr_t)last_error, length);
            return FOUNDATION_ERROR_RESULT;
        }
        uint32_t length = foundation_host_response_len();
        uint32_t output = allocate(length);
        if (foundation_host_response_read(output, length) < 0) {
            return fail_with("host response read failed", 25);
        }
        return foundation_buffer(output, length);
    }
    if (equal(method, method_length, "hang", 4)) {
        for (;;) {
        }
    }
    return fail_with("fixture failure", 15);
}
