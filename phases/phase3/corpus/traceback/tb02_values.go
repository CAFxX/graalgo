package main

import "fmt"

func fail(code int, msg string) {
	panic(fmt.Sprintf("code=%d msg=%s", code, msg))
}

func main() {
	fail(42, "broken")
}
