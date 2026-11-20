package main

// L11 (REVIEW-2026-11-18-05): go/version — zero corpus imports before
// this program. Version-string validity, comparison, and language
// version extraction: IsValid, Compare, Lang, and the invalid-input
// paths.

import (
	"fmt"
	"go/version"
)

func main() {
	valid := []string{"go1", "go1.21", "go1.21.0", "go1.21rc1", "go1.21.0rc1", "go1.22.3"}
	invalid := []string{"", "go", "1.21", "go1.", "go1.x", "go1.21.0.1", "go2.0.0.0"}
	for _, v := range valid {
		fmt.Printf("valid %s=%v\n", v, version.IsValid(v))
	}
	for _, v := range invalid {
		fmt.Printf("valid %q=%v\n", v, version.IsValid(v))
	}
	pairs := [][2]string{
		{"go1.21", "go1.21.0"},
		{"go1.21", "go1.22"},
		{"go1.22", "go1.21"},
		{"go1.21rc1", "go1.21"},
		{"go1.21.0", "go1.21.0"},
		{"go1", "go1.0"},
	}
	for _, p := range pairs {
		fmt.Printf("cmp %s vs %s=%d\n", p[0], p[1], version.Compare(p[0], p[1]))
	}
	for _, v := range []string{"go1.21.0", "go1.21rc1", "go1.22.3", "go1"} {
		fmt.Printf("lang %s=%s\n", v, version.Lang(v))
	}
	fmt.Printf("lang invalid=%q\n", version.Lang("notaversion"))
}
