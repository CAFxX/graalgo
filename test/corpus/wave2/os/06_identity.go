// 06_identity: pid/ppid/uid/gid/hostname. Only boolean relations are
// printed — raw pids differ run to run.
package main

import (
	"fmt"
	"os"
)

func main() {
	if os.Getpid() <= 0 {
		fmt.Println("FAIL: getpid")
		os.Exit(1)
	}
	if os.Getppid() < 0 {
		fmt.Println("FAIL: getppid")
		os.Exit(1)
	}
	if os.Geteuid() != os.Getuid() {
		fmt.Println("FAIL: euid != uid")
		os.Exit(1)
	}
	if os.Getegid() != os.Getgid() {
		fmt.Println("FAIL: egid != gid")
		os.Exit(1)
	}
	hn, err := os.Hostname()
	if err != nil {
		fmt.Println("FAIL: hostname:", err)
		os.Exit(1)
	}
	if hn == "" {
		fmt.Println("FAIL: empty hostname")
		os.Exit(1)
	}
	fmt.Println("hostname=" + hn)
	fmt.Println("ok")
}
