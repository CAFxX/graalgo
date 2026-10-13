package main

import (
	"fmt"
	"syscall"
)

// sc05: pure-Go time conversions + WaitStatus vocabulary. No
// intrinsics; must be bit-identical to gc.
func main() {
	ts := syscall.NsecToTimespec(1500000001)
	fmt.Println(ts.Sec, ts.Nsec)
	fmt.Println(syscall.TimespecToNsec(ts))
	tv := syscall.NsecToTimeval(1500000001)
	fmt.Println(tv.Sec, tv.Usec)
	fmt.Println(syscall.TimevalToNsec(tv))
	// Negative normalization: sec--, nsec += 1e9.
	ts = syscall.NsecToTimespec(-1)
	fmt.Println(ts.Sec, ts.Nsec)
	fmt.Println(syscall.TimespecToNsec(ts))
	// WaitStatus bit vocabulary.
	var w syscall.WaitStatus = 0x0000
	fmt.Println(w.Exited(), w.ExitStatus(), w.Signaled())
	w = 0x0100
	fmt.Println(w.Exited(), w.ExitStatus())
	w = 0x0009 // signaled by SIGKILL
	fmt.Println(w.Signaled(), int(w.Signal()) == int(syscall.SIGKILL))
	fmt.Println(w.CoreDump(), w.Stopped(), w.Continued())
	w = 0xFFFF
	fmt.Println(w.Continued())
}
