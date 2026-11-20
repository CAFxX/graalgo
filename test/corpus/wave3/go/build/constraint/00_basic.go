package main

// L11 (REVIEW-2026-11-18-05): go/build/constraint — zero corpus imports
// before this program. Parsing //go:build lines (and legacy // +build
// lines) and evaluating them against tag sets: Parse, String
// round-trip, Eval, IsGoBuild/IsPlusBuild, and the error paths.

import (
	"fmt"
	"go/build/constraint"
	"sort"
	"strings"
)

func show(line string) {
	e, err := constraint.Parse(line)
	if err != nil {
		fmt.Printf("parse %q err=%v\n", line, err != nil)
		return
	}
	fmt.Printf("parse %q => %s\n", line, e.String())
}

func main() {
	for _, line := range []string{
		"//go:build linux",
		"//go:build linux && amd64",
		"//go:build linux || darwin",
		"//go:build !windows",
		"//go:build (linux || darwin) && amd64",
		"//go:build !cgo",
		"//go:build go1.21",
		"//go:build unix && !openbsd",
		"// +build linux,amd64",
		"// +build !windows",
	} {
		show(line)
	}
	for _, line := range []string{"", "//go:build", "//go:build linux &&", "//go:build ()", "linux", "//go:build linux ||", " //go:build linux"} {
		show(line)
	}
	fmt.Printf("isgobuild //go:build linux=%v\n", constraint.IsGoBuild("//go:build linux"))
	fmt.Printf("isgobuild // +build linux=%v\n", constraint.IsGoBuild("// +build linux"))
	fmt.Printf("isplusbuild // +build linux=%v\n", constraint.IsPlusBuild("// +build linux"))
	fmt.Printf("isplusbuild //go:build linux=%v\n", constraint.IsPlusBuild("//go:build linux"))
	// Eval against tag sets.
	e, _ := constraint.Parse("//go:build linux && amd64")
	sets := []map[string]bool{
		{"linux": true, "amd64": true},
		{"linux": true, "arm64": true},
		{"darwin": true, "amd64": true},
	}
	for _, tags := range sets {
		keys := make([]string, 0, len(tags))
		for k := range tags {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Printf("eval %s vs %s=%v\n", e.String(), strings.Join(keys, ","), e.Eval(func(tag string) bool { return tags[tag] }))
	}
	e2, _ := constraint.Parse("//go:build !windows && (go1.21 || go1.22)")
	fmt.Printf("eval %s vs go1.21=%v\n", e2.String(), e2.Eval(func(tag string) bool { return tag == "go1.21" }))
	fmt.Printf("eval %s vs go1.20=%v\n", e2.String(), e2.Eval(func(tag string) bool { return tag == "go1.20" }))
	// PlusBuild round-trip: a // +build line parses to the same expr.
	e3, _ := constraint.Parse("// +build linux,amd64")
	fmt.Printf("plusbuild-expr=%s equal=%v\n", e3.String(), e3.String() == e.String())
}
