// Shared library for the Panama demo: the same C logic the cgo demo calls.
#include <stddef.h>

unsigned long c_checksum(const char *data, unsigned long len) {
    unsigned long h = 5381;
    for (unsigned long i = 0; i < len; i++) {
        h = h * 33 + (unsigned char)data[i];
    }
    return h;
}

// Calls back into the caller's function pointer once per byte, summing results.
// Mirrors cgo's "C calls back into Go" direction (upcall).
typedef long (*byte_fn)(unsigned char b, void *ctx);

unsigned long c_fold(const char *data, unsigned long len, byte_fn f, void *ctx) {
    unsigned long acc = 0;
    for (unsigned long i = 0; i < len; i++) {
        acc += (unsigned long)f((unsigned char)data[i], ctx);
    }
    return acc;
}
