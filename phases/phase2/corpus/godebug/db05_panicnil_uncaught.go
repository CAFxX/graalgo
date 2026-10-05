// db05: panicnil uncaught rendering. With GODEBUG=panicnil=1, an
// uncaught panic(nil) must print exactly "panic: nil" (like gc).
package main

func main() {
	panic(nil)
}
