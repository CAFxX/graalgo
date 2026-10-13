// 02_redefine: defining the same flag twice panics (fail-fast contract).
package main

import (
	"flag"
)

func main() {
	fs := flag.NewFlagSet("t2", flag.ContinueOnError)
	fs.Int("x", 0, "x")
	fs.Int("x", 1, "x again")
}
