// prlimit64 through package syscall: the executor's struct role (a pointer
// to one pointer-free value, syscall.Rlimit, copied in for the new limit and
// back for the old one; nil reaches the kernel as NULL). syscall's own init
// already ran it (it raises RLIMIT_NOFILE's soft limit). Limits are host
// facts, so the program prints relations and values it set itself.
package main

import "syscall"

func main() {
	var lim syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim)
	println("get:", err == nil, lim.Max > 0, lim.Cur <= lim.Max)

	// Lower the soft limit, read it back, restore it.
	orig := lim
	set := syscall.Rlimit{Cur: 64, Max: lim.Max}
	println("set:", syscall.Setrlimit(syscall.RLIMIT_NOFILE, &set) == nil)
	var back syscall.Rlimit
	println("back:", syscall.Getrlimit(syscall.RLIMIT_NOFILE, &back) == nil, back.Cur, back.Max == orig.Max)
	println("restore:", syscall.Setrlimit(syscall.RLIMIT_NOFILE, &orig) == nil)
	syscall.Getrlimit(syscall.RLIMIT_NOFILE, &back)
	println("restored:", back == orig)

	// A struct inside a larger object: the neighbours stay untouched.
	var arr [3]syscall.Rlimit
	arr[0] = syscall.Rlimit{Cur: 1, Max: 2}
	arr[2] = syscall.Rlimit{Cur: 5, Max: 6}
	println("mid:", syscall.Getrlimit(syscall.RLIMIT_NOFILE, &arr[1]) == nil, arr[1] == orig, arr[0].Cur, arr[0].Max, arr[2].Cur, arr[2].Max)

	// Errors: a soft limit above the hard one, an unknown resource.
	bad := syscall.Rlimit{Cur: orig.Max + 1, Max: orig.Max}
	if orig.Max == ^uint64(0) {
		bad = syscall.Rlimit{Cur: 2, Max: 1}
	}
	println("einval:", syscall.Setrlimit(syscall.RLIMIT_NOFILE, &bad) == syscall.EINVAL)
	println("unknown:", syscall.Getrlimit(1000, &back) == syscall.EINVAL)
}
