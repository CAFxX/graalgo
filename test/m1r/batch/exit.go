// os.Exit from a goroutine other than main (test/m1r/batch.sh): the
// context's hard exit, status 7, nothing after it.
package main

import "os"

func main() {
	done := make(chan bool)
	go func() {
		println("exiting")
		os.Exit(7)
	}()
	<-done
	println("unreachable")
}
