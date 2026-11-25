// Traceback of a panic raised by a deferred call that runs at a normal
// return (WP-TR-AUDIT-6): the deferred literal's frame at the panic, then the
// deferring function at its exit — gc's deferreturn sits at the closing
// brace of a function that falls off its end — then its caller. The other
// deferred call (open-coded and chained paths alike) runs first.
// Compared with frames by tbnorm.py.
// m1a:traceback
package main

func work(n int) {
	defer println("first deferred, runs last")
	defer func() {
		if n > 0 {
			panic("raised by a deferred call")
		}
	}()
	println("work", n)
}

func chained(n int) {
	for i := 0; i < 2; i++ {
		defer println("chained", i)
	}
	work(n)
}

func main() {
	chained(0)
	chained(1)
}
