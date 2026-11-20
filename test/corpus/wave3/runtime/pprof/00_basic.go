package main

// L11 (REVIEW-2026-11-18-05): runtime/pprof — zero corpus imports
// before this program. The profile registry: profile names and
// counts (presence only — profile bodies carry live stacks and are
// never printed), Lookup of known and unknown profiles.

import (
	"context"
	"fmt"
	"runtime/pprof"
	"sort"
)

func main() {
	profiles := pprof.Profiles()
	names := make([]string, 0, len(profiles))
	for _, p := range profiles {
		names = append(names, p.Name())
	}
	sort.Strings(names)
	fmt.Printf("profiles=%q\n", names)
	for _, p := range profiles {
		fmt.Printf("profile %s count-positive=%v\n", p.Name(), p.Count() > 0)
	}
	for _, name := range []string{"goroutine", "heap", "threadcreate", "block", "mutex", "nosuch"} {
		p := pprof.Lookup(name)
		fmt.Printf("lookup %s found=%v\n", name, p != nil)
	}
	// Labels round-trip through a context via pprof.Do.
	labels := pprof.Labels("test", "pprof-demo", "n", "1")
	pprof.Do(context.Background(), labels, func(ctx context.Context) {
		fmt.Printf("inside-do=true\n")
	})
}
