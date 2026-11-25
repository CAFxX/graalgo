// A panic in main after other programs ran in the same JVM
// (test/m1r/batch.sh): goroutine ids are per program run, so its
// traceback names goroutine 1, as gc's does.
package main

func main() {
	panic("boom")
}
