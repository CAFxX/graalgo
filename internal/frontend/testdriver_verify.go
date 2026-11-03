package main

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// L-L F3 — test-driver verification for --test-package-dir.
//
// The flag confers internal-visibility privileges, so gofrontend must
// not take it on bare say-so: the inputs must look like the driver
// `graalgo test` synthesizes for that package dir. verifyTestDriver
// checks, from the package dir's own files on disk (ground truth, not
// the driver's claims):
//   - the dir holds at least one _test.go file (graalgo test only runs
//     a driver when it selected tests, so a genuine driver implies one);
//   - every input file declares `package main` and the set contains the
//     generated test shim ("// --- graalgo test shim (generated) ---")
//     and a `func main` — the shape synthesizeTestDriver always emits;
//   - every import in the inputs is either one the package dir's own
//     source or test files import (the driver's imports are a subset
//     of that union: testsynth collects them from exactly those files),
//     or "time"/"os" (added by the driver's own main runner).
//     "testing" is never allowed: testsynth rewrites it to the graalgoT
//     shim, so a driver importing "testing" is not genuine.
//
// What this does NOT do (stated residual, see the L-L F3 design note):
// it cannot authenticate the driver against a local invoker who owns
// the invoking uid — any argv, file-content, pipe, or token signal
// passes through user-controlled processes, so a determined direct
// invocation can still forge a driver-shaped input. What it closes is
// the casual/parity hole: an arbitrary program can no longer claim a
// package's internal privileges merely by passing the flag; a forgery
// must now restrict itself to imports the package's own files already
// use, i.e. to privileges that package's own tests already hold. Full
// parity would require byte-exact re-synthesis comparison against
// synthesizeTestDriver's output for the dir (shared-code refactor,
// designed but not implemented in this lane). The public `graalgo`
// CLI additionally never forwards the flag: graalgo run/build reject
// it as an unknown flag (parseBuildFlags), so the only path that can
// confer the privilege is `graalgo test` or a direct internal-binary
// invocation, and the latter is now verified here.

const testDriverShimMarker = "// --- graalgo test shim (generated) ---"

// verifyTestDriver validates that paths are a synthesized `graalgo test`
// driver for testPackageDir before its internal-visibility privileges
// are honored. It returns an error (fail closed) on any mismatch.
func verifyTestDriver(paths []string, testPackageDir string) error {
	if testPackageDir == "" {
		return fmt.Errorf("gofrontend: --test-package-dir must not be empty")
	}
	info, err := os.Stat(testPackageDir)
	if err != nil {
		return fmt.Errorf("gofrontend: --test-package-dir %q: %w", testPackageDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("gofrontend: --test-package-dir %q is not a directory", testPackageDir)
	}
	allowed, hasTests, err := testDriverAllowedImports(testPackageDir)
	if err != nil {
		return err
	}
	if !hasTests {
		return fmt.Errorf("gofrontend: --test-package-dir %q holds no _test.go files; refusing test-driver privileges", testPackageDir)
	}
	// The driver's own main runner imports these directly.
	allowed["time"] = true
	allowed["os"] = true
	delete(allowed, "testing")

	var sawShim, sawMain bool
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("gofrontend: cannot read driver %s: %w", p, err)
		}
		if strings.Contains(string(src), testDriverShimMarker) {
			sawShim = true
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, src, 0)
		if err != nil {
			return fmt.Errorf("gofrontend: cannot parse driver %s: %w", p, err)
		}
		if f.Name.Name != "main" {
			return fmt.Errorf("gofrontend: test driver %s declares package %q, want main", p, f.Name.Name)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, "\"`")
			if !allowed[path] {
				return fmt.Errorf("gofrontend: test driver %s imports %q, which no source or test file in %q imports; refusing test-driver privileges", p, path, testPackageDir)
			}
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "main" {
				sawMain = true
			}
		}
	}
	if !sawShim {
		return fmt.Errorf("gofrontend: inputs lack the generated test shim; refusing test-driver privileges")
	}
	if !sawMain {
		return fmt.Errorf("gofrontend: inputs lack func main; refusing test-driver privileges")
	}
	return nil
}

// testDriverAllowedImports returns the import paths used by the package
// dir's own Go files (buildable sources plus test files, per go/build's
// file selection — the same file set `graalgo test` synthesizes from),
// and whether the dir holds any _test.go file. Files that fail to
// parse contribute nothing (mirroring testsynth, which skips
// unparseable sources); if go/build cannot classify the dir at all,
// every *.go file in it is scanned instead (a superset, still grounded
// in the dir's own files).
func testDriverAllowedImports(dir string) (map[string]bool, bool, error) {
	allowed := make(map[string]bool)
	var files []string
	hasTests := false
	if pkg, err := build.Default.ImportDir(dir, 0); err == nil {
		files = append(files, pkg.GoFiles...)
		files = append(files, pkg.TestGoFiles...)
		files = append(files, pkg.XTestGoFiles...)
		files = append(files, pkg.CgoFiles...)
		if len(pkg.TestGoFiles) > 0 || len(pkg.XTestGoFiles) > 0 {
			hasTests = true
		}
	} else {
		entries, gerr := filepath.Glob(filepath.Join(dir, "*.go"))
		if gerr != nil {
			return nil, false, fmt.Errorf("gofrontend: cannot list %q: %w", dir, gerr)
		}
		files = entries
		for _, f := range entries {
			if strings.HasSuffix(f, "_test.go") {
				hasTests = true
			}
		}
	}
	// ImportDir returns base names; glob returns full paths. Normalize.
	for _, f := range files {
		full := f
		if !filepath.IsAbs(full) {
			full = filepath.Join(dir, f)
		}
		if strings.HasSuffix(full, "_test.go") {
			hasTests = true
		}
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, full, nil, parser.ImportsOnly)
		if err != nil {
			continue
		}
		for _, imp := range parsed.Imports {
			allowed[strings.Trim(imp.Path.Value, "\"`")] = true
		}
	}
	return allowed, hasTests, nil
}
