// Command gofrontend: Go frontend service for graalgo.
//
// gofrontend type-checks a Go package (one or more files) with the vendored
// go/* packages (see VENDORING.md) and emits the frontend/v1 typed-AST JSON
// document (see ../docs/frontend-v1.md) to stdout.
//
// Usage: gofrontend <file.go> [file.go...]
// All files must declare the same package. They are type-checked together,
// so cross-file references resolve.
// Exit codes: 0 success, 1 usage error, 2 frontend error (details on stderr).
//
// Validation mode: gofrontend --check-synthetic <import-path>
// Builds the synthetic package for <import-path> in isolation (via the
// same getSyntheticPackage path used by the import overlay) and exits 0
// iff it builds without error. This validates internal synthetic packages
// (e.g. crypto/internal/boring) that cannot be imported directly by user
// code per Go's internal visibility rule, but must still be well-formed
// as dependencies of other packages. General mechanism: works for any
// path in syntheticSources, no per-package cases.
package main

import (
	"fmt"
	"os"
)

const toolVersion = "0.7.0"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: gofrontend <file.go> [file.go...] | gofrontend --check-synthetic <import-path>\n")
		os.Exit(1)
	}
	if os.Args[1] == "--check-synthetic" {
		if len(os.Args) != 3 {
			fmt.Fprintf(os.Stderr, "usage: gofrontend --check-synthetic <import-path>\n")
			os.Exit(1)
		}
		path := os.Args[2]
		if _, ok := syntheticSources[path]; !ok {
			fmt.Fprintf(os.Stderr, "gofrontend: --check-synthetic: no synthetic source for %q\n", path)
			os.Exit(2)
		}
		// Build the synthetic package. getSyntheticPackage panics on
		// parse/type errors; recover and report as a clean failure.
		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "gofrontend: --check-synthetic %q: %v\n", path, r)
					os.Exit(2)
				}
			}()
			sp := getSyntheticPackage(path)
			fmt.Printf("ok %s (%d decls)\n", path, sp.pkg.Scope().Len())
		}()
		return
	}
	out, err := ExportFiles(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "gofrontend: %v\n", err)
		os.Exit(2)
	}
	fmt.Println(out)
}
