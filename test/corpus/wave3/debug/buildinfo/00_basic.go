package main

// L11 (REVIEW-2026-11-18-05): debug/buildinfo — zero corpus imports
// before this program. Read build info from the running binary itself
// (/proc/self/exe): Go version, main module path, and build settings.
// Also the error path: a non-Go binary has no build info.

import (
	"debug/buildinfo"
	"fmt"
	"sort"
)

func main() {
	bi, err := buildinfo.ReadFile("/proc/self/exe")
	if err != nil {
		fmt.Printf("self-err=%v\n", err)
		return
	}
	fmt.Printf("go-version=%s path=%s\n", bi.GoVersion, bi.Path)
	fmt.Printf("main-path=%s main-version=%s\n", bi.Main.Path, bi.Main.Version)
	keys := make([]string, 0, len(bi.Settings))
	for _, s := range bi.Settings {
		keys = append(keys, s.Key+"="+s.Value)
	}
	sort.Strings(keys)
	fmt.Printf("settings=%q\n", keys)
	fmt.Printf("deps=%d\n", len(bi.Deps))

	// Error path: /bin/true is not a Go binary.
	_, err = buildinfo.ReadFile("/bin/true")
	fmt.Printf("non-go-err=%v\n", err != nil)
	// Missing file.
	_, err = buildinfo.ReadFile("/nonexistent/binary")
	fmt.Printf("missing-err=%v\n", err != nil)
}
