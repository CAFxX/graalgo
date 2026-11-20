package main

// L11 (REVIEW-2026-11-18-05): go/build — zero corpus imports before
// this program. The default build context and package discovery:
// Default fields, ImportDir on a generated temp package, Import with
// FindOnly, IsLocalImport, and MatchFile.

import (
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"sort"
)

const pkgSrc = `package demo

// Add adds two ints.
func Add(a, b int) int { return a + b }

const Answer = 42
`

func main() {
	ctxt := build.Default
	fmt.Printf("goos=%s goarch=%s cgo=%v\n", ctxt.GOOS, ctxt.GOARCH, ctxt.CgoEnabled)
	fmt.Printf("goroot-set=%v\n", ctxt.GOROOT != "")
	fmt.Printf("islocal ./x=%v /abs=%v fmt=%v\n",
		build.IsLocalImport("./x"), build.IsLocalImport("/abs"), build.IsLocalImport("fmt"))
	// ImportDir on a generated package (paths never printed: temp dir).
	dir, err := os.MkdirTemp("", "gobuild-demo")
	if err != nil {
		fmt.Printf("mktemp-err=%v\n", err != nil)
		return
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "demo.go"), []byte(pkgSrc), 0644); err != nil {
		fmt.Printf("write-err=%v\n", err != nil)
		return
	}
	pkg, err := ctxt.ImportDir(dir, 0)
	if err != nil {
		fmt.Printf("importdir-err=%v\n", err != nil)
		return
	}
	fmt.Printf("pkg-name=%s gofiles=%d imports=%d\n", pkg.Name, len(pkg.GoFiles), len(pkg.Imports))
	fmt.Printf("pkg-has-doc=%v\n", pkg.Doc != "")
	// Import of a std package, metadata only (no paths printed).
	p, err := ctxt.Import("go/token", "", build.FindOnly)
	if err != nil {
		fmt.Printf("import-findonly-err=%v\n", err != nil)
		return
	}
	fmt.Printf("findonly-name=%s\n", p.Name)
	_, err = ctxt.Import("nosuch/package", "", build.FindOnly)
	fmt.Printf("import-missing-err=%v\n", err != nil)
	// MatchFile: build-tag filtering on file names.
	fmt.Printf("matchfile demo.go=%v\n", func() bool {
		ok, _ := ctxt.MatchFile(dir, "demo.go")
		return ok
	}())
	fmt.Printf("matchfile demo_test.go=%v\n", func() bool {
		ok, _ := ctxt.MatchFile(dir, "demo_test.go")
		return ok
	}())
	// Sorted build tags in effect.
	tags := append([]string(nil), ctxt.BuildTags...)
	sort.Strings(tags)
	fmt.Printf("buildtags=%q\n", tags)
}
