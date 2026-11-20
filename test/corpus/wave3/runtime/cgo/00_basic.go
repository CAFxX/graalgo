package main

// L11 (REVIEW-2026-11-18-05): runtime/cgo — zero corpus imports
// before this program. The cgo package has no public API: importing
// it is the whole program. The build info records whether the
// binary was built with cgo enabled, which is the observable pin.
// (Marked .scopelimited: the graalgo frontend has no cgo support;
// retest when it lands.)

import (
	"fmt"
	_ "runtime/cgo"
	"runtime/debug"
	"sort"
)

func main() {
	fmt.Printf("cgo-imported=true\n")
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		fmt.Printf("buildinfo-ok=false\n")
		return
	}
	var cgo string
	var keys []string
	for _, s := range bi.Settings {
		keys = append(keys, s.Key)
		if s.Key == "CGO_ENABLED" {
			cgo = s.Value
		}
	}
	sort.Strings(keys)
	fmt.Printf("cgo-enabled=%s settings-has-cgo=%v\n", cgo, cgo != "")
}
