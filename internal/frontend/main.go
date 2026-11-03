// Command gofrontend: Go frontend service for graalgo.
//
// gofrontend type-checks a Go package (one or more files) with the vendored
// go/* packages (see VENDORING.md) and emits the frontend/v1 typed-AST JSON
// document (see ../docs/frontend-v1.md) to stdout.
//
// Usage: gofrontend [--test-package-dir=DIR] [--target=GOOS/GOARCH] <file.go> [file.go...]
// All files must declare the same package. They are type-checked together,
// so cross-file references resolve.
// --test-package-dir=DIR declares the package a synthesized `graalgo test`
// driver belongs to (passed only by `graalgo test` via the backend
// launcher); the import gate applies Go's internal-visibility rule with
// that package's privileges. Never read from the environment: a
// user-exported variable must not confer the privilege.
// --target=GOOS/GOARCH pins the compile target (see target.go). The
// pinned target must equal the host target; a mismatched target, and any
// compile on an unsupported host, fails loudly instead of emitting a
// document that claims the wrong GOARCH/GOOS or uses the wrong sizes.
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

// parseFrontendArgs splits argv into the test-package-dir flag, the
// explicit target flag, and the remaining arguments, in order. Flags may
// appear in any position; a repeated flag's last value wins, matching
// the previous loop's behaviour for --test-package-dir. Unknown flags
// are not consumed here: they stay in rest so the caller's usage check
// (or file handling) sees exactly what it saw before --target existed.
func parseFrontendArgs(args []string) (testPackageDir string, target string, rest []string) {
	for _, a := range args {
		if strings.HasPrefix(a, testPackageDirFlag) {
			testPackageDir = strings.TrimPrefix(a, testPackageDirFlag)
			continue
		}
		if strings.HasPrefix(a, targetFlag) {
			target = strings.TrimPrefix(a, targetFlag)
			continue
		}
		rest = append(rest, a)
	}
	return testPackageDir, target, rest
}

// resolveCompileTarget turns a --target value into the compile target
// for this invocation, distinguishing the two failure classes main()
// must report differently: a malformed value is a usage error (exit 1);
// a well-formed value that is unsupported or does not match the host is
// a frontend error (exit 2). With no --target, the host target is used
// and validated the same way (exit 2 on an unsupported host).
// The returned exit code is 0 on success.
func resolveCompileTarget(value string) (target Target, explicit bool, exitCode int, err error) {
	if value == "" {
		host := HostTarget()
		if verr := host.Validate(); verr != nil {
			return Target{}, false, 2, verr
		}
		return host, false, 0, nil
	}
	t, perr := ParseTarget(value)
	if perr != nil {
		return Target{}, true, 1, perr
	}
	if verr := t.validateForHost(HostTarget()); verr != nil {
		return Target{}, true, 2, verr
	}
	return t, true, 0, nil
}

func main() {
	// PKG-001: `graalgo test` passes --test-package-dir=DIR (via the
	// backend launcher) declaring which package a synthesized test
	// driver belongs to; the import gate then applies Go's
	// internal-visibility rule with that package's privileges. The flag
	// is extracted from argv here and threaded in-process to
	// ExportFiles — it is never read from the environment, so a
	// user-exported variable cannot confer the privilege.
	// --target (target.go) is extracted the same way: an argv flag,
	// never an environment variable, for the same reason.
	testPackageDir, targetValue, rest := parseFrontendArgs(os.Args[1:])
	if len(rest) < 1 {
		fmt.Fprintf(os.Stderr, "usage: gofrontend [--test-package-dir=DIR] [--target=GOOS/GOARCH] <file.go> [file.go...] | gofrontend --check-synthetic <import-path>\n")
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
		// The compile target gates this path too (target.go): without
		// the check, an unsupported host could still build synthetic
		// packages that do not import runtime and never hit the
		// importer's target validation.
		if _, _, code, err := resolveCompileTarget(targetValue); err != nil {
			fmt.Fprintf(os.Stderr, "gofrontend: %v\n", err)
			os.Exit(code)
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
	target, explicit, code, terr := resolveCompileTarget(targetValue)
	if terr != nil {
		fmt.Fprintf(os.Stderr, "gofrontend: %v\n", terr)
		os.Exit(code)
	}
	var out string
	var err error
	if explicit {
		out, err = ExportFilesWithTarget(rest, testPackageDir, target)
	} else {
		out, err = ExportFiles(rest, testPackageDir)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "gofrontend: %v\n", err)
		os.Exit(2)
	}
	fmt.Println(out)
}
