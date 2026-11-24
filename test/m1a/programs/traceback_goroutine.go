// Traceback of a panic in a goroutine other than main (WP-TR-AUDIT-6): the
// goroutine's own frames, ending at its entry function literal; gc's
// "created by" trailer is not compared (tbnorm.py).
// m1a:traceback
package main

func work(c chan int) {
	v := <-c
	var m map[int]int
	m[v] = 1
}

func main() {
	c := make(chan int)
	done := make(chan bool)
	go func() {
		work(c)
		done <- true
	}()
	c <- 4
	<-done
}
