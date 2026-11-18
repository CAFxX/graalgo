package main

// After main calls runtime.Goexit the program keeps running other goroutines;
// one of them ends it with os.Exit: its status, no deadlock report.

import (
	"os"
	"runtime"
)

func main() {
	release := make(chan bool)
	go func() {
		<-release
		println("worker exits the process")
		os.Exit(3)
	}()
	defer close(release)
	defer println("main deferred")
	runtime.Goexit()
}
