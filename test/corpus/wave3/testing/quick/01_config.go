// 01_config: Config.Rand hook (caller-supplied source is honored and
// deterministic), Values override, and -quickchecks flag registration.
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"testing/quick"
)

func runWith(seed int64, n int) []int {
	r := rand.New(rand.NewSource(seed))
	var xs []int
	_ = quick.Check(func(x int) bool {
		xs = append(xs, x)
		return true
	}, &quick.Config{Rand: r, MaxCount: n})
	return xs
}

func main() {
	// Same seed twice -> identical streams (the Rand hook is honored).
	a := runWith(12345, 20)
	b := runWith(12345, 20)
	equal := len(a) == len(b) && len(a) == 20
	for i := range a {
		if a[i] != b[i] {
			equal = false
		}
	}
	fmt.Println("custom-rand deterministic:", equal)

	// Different seeds -> (almost surely) different streams; just check
	// both runs execute.
	c := runWith(999, 20)
	fmt.Println("other-seed ran:", len(c) == 20)

	// Values override: only yields multiples of 7.
	bad := 0
	runs := 0
	_ = quick.Check(func(x int) bool {
		runs++
		if x%7 != 0 {
			bad++
		}
		return true
	}, &quick.Config{
		MaxCount: 10,
		Values: func(args []interface{}, r *rand.Rand) {
			args[0] = 7 * (int(r.Uint32()) % 100)
		},
	})
	fmt.Println("values-override runs:", runs, "bad:", bad)

	// -quickchecks is registered on flag.CommandLine.
	if f := flag.Lookup("quickchecks"); f != nil {
		fmt.Println("quickchecks default:", f.DefValue)
	} else {
		fmt.Println("BAD: quickchecks not registered")
	}
}
