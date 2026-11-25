// Never ends (test/m1r/batch.sh): its timeout cancels its context, the
// status is timeout(1)'s 124, and the batch goes on.
package main

func main() {
	println("spinning")
	n := 0
	for {
		n++
	}
}
