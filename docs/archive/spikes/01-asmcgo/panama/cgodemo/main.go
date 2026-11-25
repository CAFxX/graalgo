package main

/*
#include <string.h>
#include <stdlib.h>

// A small piece of real C logic, representative of what cgo packages wrap:
// checksum over a buffer plus a stateful counter via an upcall-like callback.
static unsigned long c_checksum(const char *data, unsigned long len) {
    unsigned long h = 5381;
    for (unsigned long i = 0; i < len; i++) {
        h = h * 33 + (unsigned char)data[i];
    }
    return h;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func main() {
	msg := "graalgo-panama-demo"
	cstr := C.CString(msg)
	defer C.free(unsafe.Pointer(cstr))

	// downcall: Go -> C
	h := C.c_checksum(cstr, C.ulong(len(msg)))
	fmt.Printf("cgo  : checksum(%q) = %d\n", msg, uint64(h))

	// also exercise a libc call through cgo
	fmt.Printf("cgo  : strlen(%q) = %d\n", msg, uint64(C.strlen(cstr)))
}
