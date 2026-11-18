package main

// runtime.Goexit on the node package (GG-0070, GG-0071): it runs every pending
// deferred call (open-coded and chained frames alternating), recover() returns
// nil during it, it aborts a panic in flight, a panic raised and recovered
// during it lets it resume, and on the main goroutine the program ends in gc's
// "no goroutines (main called runtime.Goexit) - deadlock!" once every other
// goroutine is done.
//
// The program imports runtime: the M1a gate reaches runtime.Goexit through the
// intrinsic registry until the facade runtime (toolexec W5) exists.

import "runtime"

func plain(done chan string) {
	defer func() { done <- "plain" }()
	defer func() { println("plain: recover during Goexit is nil:", recover() == nil) }()
	runtime.Goexit()
	println("unreachable")
}

// open-coded frame -> chained frame -> open-coded frame -> Goexit
func open1(done chan string) {
	defer func() { done <- "alternating" }()
	defer println("open1 defer")
	chained1()
	println("unreachable open1")
}

func chained1() {
	for i := 0; i < 3; i++ {
		defer println("chained1 defer", i)
	}
	open2()
}

func open2() {
	defer println("open2 defer")
	runtime.Goexit()
}

// Goexit called by a deferred call aborts the panic in flight: no crash.
func abortPanic(done chan string) {
	defer func() { done <- "abortPanic" }()
	defer func() { println("abortPanic: recover after Goexit is nil:", recover() == nil) }()
	defer runtime.Goexit()
	panic("aborted")
}

// A panic raised by a deferred call during Goexit, then recovered: Goexit resumes.
func resume(done chan string) {
	defer func() { done <- "resume" }()
	defer func() { println("resume: still exiting") }()
	defer func() { println("resume: recovered", recover().(string)) }()
	defer func() { panic("raised during Goexit") }()
	runtime.Goexit()
}

// A deferred func value of runtime.Goexit.
func value(done chan string) {
	defer func() { done <- "value" }()
	f := runtime.Goexit
	defer f()
	println("value: body done")
}

func main() {
	done := make(chan string)
	for _, f := range []func(chan string){plain, open1, abortPanic, resume, value} {
		go f(done)
		println("finished:", <-done)
	}
	release := make(chan bool)
	go func() {
		<-release
		println("worker runs after main's defers")
	}()
	defer close(release)
	defer println("main deferred")
	runtime.Goexit()
	println("unreachable main")
}
