#ifndef KRYNDEL_QT6_BRIDGE_H
#define KRYNDEL_QT6_BRIDGE_H

#include <stdint.h>

#if defined(_WIN32)
#define KRY_QT6_EXPORT __declspec(dllexport)
#else
#define KRY_QT6_EXPORT __attribute__((visibility("default")))
#endif

#ifdef __cplusplus
extern "C" {
#endif

/*
 * Reads one UTF-8 JSON request and writes a base64-encoded UTF-8 JSON response.
 * Returns the number of response bytes, or the required buffer size when the
 * supplied output buffer is too small. The caller provides `capacity` bytes.
 */
KRY_QT6_EXPORT int32_t kry_qt6_request(const char *request, int32_t request_length,
                                       char *response, int32_t capacity);

KRY_QT6_EXPORT int32_t kry_qt6_abi_version(void);

#ifdef __cplusplus
}
#endif

#endif
