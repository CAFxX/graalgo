// Command gofrontend: Go frontend service for graalgo.
//
// gofrontend type-checks a Go package (one or more files) with the vendored
// go/* packages (see VENDORING.md) and emits the frontend/v1 typed-AST JSON
// document (see ../docs/frontend-v1.md) to stdout.
//
// Usage: gofrontend [--test-package-dir=DIR] <file.go> [file.go...]
// All files must declare the same package. They are type-checked together,
// so cross-file references resolve.
// --test-package-dir=DIR declares the package a synthesized `graalgo test`
// driver belongs to (passed only by `graalgo test` via the backend
// launcher); the import gate applies Go's internal-visibility rule with
// that package's privileges. Never read from the environment: a
// user-exported variable must not confer the privilege.
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
	"strings"
)

const toolVersion = "0.7.0"

func main() {
	// PKG-001: `graalgo test` passes --test-package-dir=DIR (via the
	// backend launcher) declaring which package a synthesized test
	// driver belongs to; the import gate then applies Go's
	// internal-visibility rule with that package's privileges. The flag
	// is extracted from argv here and threaded in-process to
	// ExportFiles — it is never read from the environment, so a
	// user-exported variable cannot confer the privilege.
	var testPackageDir string
	var rest []string
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, testPackageDirFlag) {
			testPackageDir = strings.TrimPrefix(a, testPackageDirFlag)
			continue
		}
		rest = append(rest, a)
	}
	if len(rest) < 1 {
		fmt.Fprintf(os.Stderr, "usage: gofrontend [--test-package-dir=DIR] <file.go> [file.go...] | gofrontend --check-synthetic <import-path>\n")
		os.Exit(1)
	}
	if rest[0] == "--check-synthetic" {
		if len(rest) != 2 {
			fmt.Fprintf(os.Stderr, "usage: gofrontend --check-synthetic <import-path>\n")
			os.Exit(1)
		}
		path := rest[1]
		if _, ok := syntheticSources[path]; !ok {
			fmt.Fprintf(os.Stderr, "gofrontend: --check-synthetic: no synthetic source for %q\n", path)
			os.Exit(2)
		}
		// Build the synthetic package. Parse/type failures come back as
		// a clean error (P1-13: getSyntheticPackage returns errors, no
		// longer panics on broken committed sources). The recover below
		// stays as a last-resort guard for genuinely unexpected
		// internal panics (e.g. the "no synthetic source" panic, which
		// is unreachable here because of the map check above but is
		// retained by design) so the tool still exits 2 with a message
		// instead of a stack trace. The error path provably covers the
		// parse/type failures; the recover covers the unprovable rest.
		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "gofrontend: --check-synthetic %q: internal error: %v\n", path, r)
					os.Exit(2)
				}
			}()
			sp, err := getSyntheticPackage(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gofrontend: --check-synthetic %q: %v\n", path, err)
				os.Exit(2)
			}
			fmt.Printf("ok %s (%d decls)\n", path, sp.pkg.Scope().Len())
		}()
		return
	}
	out, err := ExportFiles(rest, testPackageDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gofrontend: %v\n", err)
		os.Exit(2)
	}
	fmt.Println(out)
}
