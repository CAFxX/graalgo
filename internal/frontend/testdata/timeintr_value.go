package main

import "time"

// A time intrinsic in value position (here, package scope) lowers to a
// synthesized wrapper closure: func() time.Time { return time.Now() }.
// General mechanism: any time intrinsic, any arity — parameters are
// forwarded, evaluation deferred to call time.
var timeNowFn = time.Now

func main() {
	t := timeNowFn()
	println("year =", t.Year())
}
