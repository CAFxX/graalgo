//m1c:ldflags -checklinkname=0

// The syscall executor (graalgo.syscall, W8a) through the raw entry every
// syscall.Syscall ends in, internal/runtime/syscall/linux.Syscall6, pulled by
// //go:linkname (package syscall imports errors, which waits for W9a's
// descriptor images): openat/write/lseek/read/close on a file, pointers
// passed as uintptr(unsafe.Pointer(...)) words exactly as package syscall
// passes them, buffers in the middle of a larger array, a partially filled
// read with its tail left intact, and the errno paths (ENOENT, EBADF,
// EFAULT-free nil reads of zero bytes).
package main

import "unsafe"

//go:linkname syscall6 internal/runtime/syscall/linux.Syscall6
func syscall6(num, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2, errno uintptr)

const (
	sysRead   = 0
	sysWrite  = 1
	sysClose  = 3
	sysLseek  = 8
	sysGetpid = 39
	sysOpenat = 257

	oRDONLY  = 0x0
	oWRONLY  = 0x1
	oRDWR    = 0x2
	oCREAT   = 0x40
	oTRUNC   = 0x200
	oCLOEXEC = 0x80000
)

var atFDCWD = -100

func cstr(s string) []byte { return append([]byte(s), 0) }

// Every buffer whose address becomes a uintptr word is a package variable:
// gc keeps a pointer converted to uintptr alive and unmoved only in the
// argument list of a call to an assembly or //go:uintptrkeepalive function,
// and the helper below is neither, so a stack buffer could move.
var (
	path    = cstr("m1c_syscall_raw.tmp")
	data    = []byte("..hello, kernel; this is graalgo..")
	arr     [40]byte
	r       rec
	missing = cstr("no/such/dir/file")
	big     = make([]byte, 100)
)

// A buffer inside a struct, between other fields.
type rec struct {
	before int64
	buf    [8]byte
	after  int64
}

func call(nr uintptr, a1, a2, a3 uintptr) (int, uintptr) {
	r1, _, e := syscall6(nr, a1, a2, a3, 0, 0, 0)
	return int(r1), e
}

func main() {
	r1, _, e := syscall6(sysOpenat, uintptr(atFDCWD), uintptr(unsafe.Pointer(&path[0])), oRDWR|oCREAT|oTRUNC|oCLOEXEC, 0o644, 0, 0)
	fd := int(r1)
	println("openat", fd >= 0, e)

	n, e := call(sysWrite, uintptr(fd), uintptr(unsafe.Pointer(&data[2])), uintptr(len(data)-4))
	println("write", n, e)

	off, e := call(sysLseek, uintptr(fd), 6, 0)
	println("lseek", off, e)

	for i := range arr {
		arr[i] = '#'
	}
	n, e = call(sysRead, uintptr(fd), uintptr(unsafe.Pointer(&arr[3])), 30)
	println("read", n, e, string(arr[:]))

	n, e = call(sysRead, uintptr(fd), uintptr(unsafe.Pointer(&arr[0])), 10)
	println("read at eof", n, e)

	r = rec{before: -1, after: -2}
	call(sysLseek, uintptr(fd), 0, 0)
	n, e = call(sysRead, uintptr(fd), uintptr(unsafe.Pointer(&r.buf)), uintptr(len(r.buf)))
	println("read into struct", n, e, string(r.buf[:]), r.before, r.after)

	println("close", fst(call(sysClose, uintptr(fd), 0, 0)))
	_, e = call(sysClose, uintptr(fd), 0, 0)
	println("close again: EBADF", e == 9)
	_, e = call(sysRead, uintptr(fd), 0, 0)
	println("read closed: EBADF", e == 9)

	n, e = call(sysOpenat, uintptr(atFDCWD), uintptr(unsafe.Pointer(&missing[0])), oRDONLY)
	println("openat missing: ENOENT", n, e == 2)

	fd, _ = call(sysOpenat, uintptr(atFDCWD), uintptr(unsafe.Pointer(&path[0])), oRDONLY|oCLOEXEC)
	n, e = call(sysRead, uintptr(fd), uintptr(unsafe.Pointer(&big[0])), uintptr(len(big)))
	println("whole file", n, e, string(big[:n]))
	call(sysClose, uintptr(fd), 0, 0)

	pid, e := call(sysGetpid, 0, 0, 0)
	println("getpid", pid > 0, e)

	_, _, e = syscall6(263, uintptr(atFDCWD), uintptr(unsafe.Pointer(&path[0])), 0, 0, 0, 0) // unlinkat
	println("unlinkat", e)
}

func fst(a int, _ uintptr) int { return a }
