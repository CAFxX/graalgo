package main

import "syscall"

// sc08: RawSyscall6 fail-fast pins the same message shape (name +
// number + trigger) on the 6-arg variant.
func main() {
	r1, r2, err := syscall.RawSyscall6(syscall.SYS_OPEN, 0, 0, 0, 0, 0, 0)
	_, _, _ = r1, r2, err
}
