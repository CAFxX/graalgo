package main

// After main calls runtime.Goexit the program keeps running other goroutines;
// one of them ends it as os.Exit does: its status, no deadlock report.

import (
	"runtime"
	_ "unsafe"
)

// os.Exit ends in syscall.Exit, which the runtime provides. Package os
// itself does not link through the -toolexec path yet (toolexec-design §13:
// 62 undefined symbols, milestone M1c), so the program names the runtime's
// function directly.
//
//go:linkname exit syscall.Exit
func exit(code int)

func main() {
	release := make(chan bool)
	go func() {
		<-release
		println("worker exits the process")
		exit(3)
	}()
	defer close(release)
	defer println("main deferred")
	runtime.Goexit()
}
