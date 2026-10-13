package main

import (
	"fmt"
	"syscall"
)

// sc03: identity scalars. Only shapes are printed (pids/uids vary);
// the differential property is that every call succeeds and the
// values are sane.
func main() {
	fmt.Println(syscall.Getpid() > 0)
	fmt.Println(syscall.Getppid() >= 0)
	fmt.Println(syscall.Getuid() >= 0)
	fmt.Println(syscall.Geteuid() >= 0)
	fmt.Println(syscall.Getgid() >= 0)
	fmt.Println(syscall.Getegid() >= 0)
	gids, err := syscall.Getgroups()
	// getgroups(2) returns supplementary groups only (empty for root
	// in a container); the count is stable per machine and must agree
	// between the legs.
	fmt.Println(err == nil, len(gids))
}
