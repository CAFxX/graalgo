// Command gofrontend: Go frontend service for graalgo.
//
// gofrontend type-checks a Go package (one or more files) with the vendored
// go/* packages (see VENDORING.md) and emits the frontend/v1 typed-AST JSON
// document (see ../docs/frontend-v1.md) to stdout.
//
// Usage: gofrontend [flags] <file.go> [file.go...]
// Flags: --test-package-dir=DIR, --modcache=DIR, --goroot=DIR,
// --build-tags="TAG ...".
//
// All files must declare the same package. They are type-checked together,
// so cross-file references resolve.
// --test-package-dir=DIR declares the package a synthesized `graalgo test`
// driver belongs to (passed only by `graalgo test` via the backend
// launcher); the import gate applies Go's internal-visibility rule with
// that package's privileges. Never read from the environment: a
// user-exported variable must not confer the privilege.
// --modcache=DIR pins the module-cache location for disk imports
// (default: GOMODCACHE, else the GOPATH default). The cache location
// never selects a version: under a module context the version comes
// from the importing module's go.mod (see diskmodule.go).
// --goroot=DIR pins the toolchain GOROOT for stdlib disk imports. When
// absent, the ambient GOROOT variable is cleared at startup so
// runtime.GOROOT() returns the toolchain that built this binary —
// ambient env must not redirect stdlib resolution.
// --build-tags="TAG ..." adds build tags for disk-import file selection
// (default: GRAALGO_BUILD_TAGS). Build tags are a build input: a build
// with different tags is a different build.
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
	var modCacheFlag, gorootFlag, buildTagsFlag string
	var buildTagsSet bool
	var rest []string
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, testPackageDirFlag) {
			testPackageDir = strings.TrimPrefix(a, testPackageDirFlag)
			continue
		}
		if strings.HasPrefix(a, "--modcache=") {
			modCacheFlag = strings.TrimPrefix(a, "--modcache=")
			continue
		}
		if strings.HasPrefix(a, "--goroot=") {
			gorootFlag = strings.TrimPrefix(a, "--goroot=")
			continue
		}
		if strings.HasPrefix(a, "--build-tags=") {
			buildTagsFlag = strings.TrimPrefix(a, "--build-tags=")
			buildTagsSet = true
			continue
		}
		rest = append(rest, a)
	}
	// Disk-resolution inputs (see diskimport.go): explicit flags win.
	setDiskFlags(modCacheFlag, gorootFlag, strings.Fields(buildTagsFlag), buildTagsSet)
	// Pin GOROOT to the toolchain that built this binary unless the
	// caller explicitly passed --goroot: runtime.GOROOT() consults the
	// GOROOT environment variable first, so ambient env would otherwise
	// redirect stdlib disk resolution (review 2026-11-04, P1-05).
	if gorootFlag == "" {
		os.Unsetenv("GOROOT")
	}
	if len(rest) < 1 {
		fmt.Fprintf(os.Stderr, "usage: gofrontend [--test-package-dir=DIR] [--modcache=DIR] [--goroot=DIR] [--build-tags=\"TAG ...\"] <file.go> [file.go...] | gofrontend --check-synthetic <import-path>\n")
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
	out, err := ExportFiles(rest, testPackageDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gofrontend: %v\n", err)
		os.Exit(2)
	}
	fmt.Println(out)
}
