package main

// P3-M9 (WS G): unresolvable imports are still out of scope. (A resolvable
// import like "net" now succeeds via the GOROOT disk-import fallback, so
// this fixture uses a path that resolves nowhere to pin the WS G error.)
import "example.com/nonexistent/pkg"

func main() {
	println("imports are out of v1 scope")
}
