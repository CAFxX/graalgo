package main

import (
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"strings"
)

// cmdTest implements `graalgo test [packages]`.
//
// Discovers *_test.go files in the named packages and runs each test
// function through the graalgo backend. Test functions are run by
// synthesizing a single-file driver per test file that invokes each
// TestXxx function with a minimal testing shim.
//
// This is the driver's built-in test runner. The full differential
// conformance suite (test/differential/) remains the arbiter for
// graalgo-vs-gc behavior parity.
func cmdTest(root string, args []string) int {
	// Separate test flags from package args. We accept -run and -v.
	var runFilter string
	var verbose bool
	var pkgs []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-v":
			verbose = true
		case a == "-run" && i+1 < len(args):
			i++
			runFilter = args[i]
		case strings.HasPrefix(a, "-run="):
			runFilter = strings.TrimPrefix(a, "-run=")
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "graalgo test: unknown flag %q\n", a)
			return 2
		default:
			pkgs = append(pkgs, a)
		}
	}

	// Resolve package dirs.
	dirs, err := resolveTestDirs(pkgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo test: %v\n", err)
		return 1
	}

	runner := newTestRunner(root, verbose, runFilter)
	failed := false
	for _, dir := range dirs {
		if !runner.runPackage(dir) {
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}

// resolveTestDirs turns package arguments into directories to test.
func resolveTestDirs(pkgs []string) ([]string, error) {
	if len(pkgs) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		return []string{cwd}, nil
	}
	var dirs []string
	for _, p := range pkgs {
		if p == "./..." {
			return nil, fmt.Errorf("./... not yet supported")
		}
		fi, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("no such file or directory: %s", p)
		}
		if !fi.IsDir() {
			// A single file: test its directory.
			dirs = append(dirs, filepath.Dir(p))
			continue
		}
		dirs = append(dirs, p)
	}
	return dirs, nil
}

type testRunner struct {
	root      string
	verbose   bool
	runFilter string
}

func newTestRunner(root string, verbose bool, runFilter string) *testRunner {
	return &testRunner{root: root, verbose: verbose, runFilter: runFilter}
}

// runPackage discovers and runs the tests in one directory.
// Returns false if any test failed.
func (r *testRunner) runPackage(dir string) bool {
	testFiles, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil || len(testFiles) == 0 {
		fmt.Printf("?   %s [no test files]\n", dir)
		return true
	}

	// Collect non-test Go files in the package (the code under test),
	// honoring build constraints (GOOS/GOARCH/filename suffixes and
	// //go:build lines) — the same file set `go test` would use. Without
	// this, build-tag-gated alternatives (e.g. node_js.go vs node_net.go)
	// are inlined together and redeclare each other's symbols.
	buildable := buildableGoFiles(dir)
	var srcFiles []string
	for _, f := range buildable {
		if !strings.HasSuffix(f, "_test.go") {
			srcFiles = append(srcFiles, f)
		}
	}
	// The test files themselves must also satisfy build constraints.
	var runnableTests []string
	for _, tf := range testFiles {
		base := filepath.Base(tf)
		for _, b := range buildable {
			if filepath.Base(b) == base {
				runnableTests = append(runnableTests, tf)
				break
			}
		}
	}

	ok := true
	for _, tf := range runnableTests {
		if !r.runTestFile(dir, tf, srcFiles) {
			ok = false
		}
	}
	if ok {
		fmt.Printf("ok  %s\n", dir)
	} else {
		fmt.Printf("FAIL %s\n", dir)
	}
	return ok
}

// buildableGoFiles returns the .go files in dir that satisfy the current
// platform's build constraints, using the same rules as `go build`.
func buildableGoFiles(dir string) []string {
	ctxt := build.Default
	pkg, err := ctxt.ImportDir(dir, 0)
	if err != nil {
		// Not importable (no go.mod context, etc.): fall back to all
		// files and let the backend report real errors.
		all, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		return all
	}
	var out []string
	for _, f := range pkg.GoFiles {
		out = append(out, filepath.Join(dir, f))
	}
	for _, f := range pkg.TestGoFiles {
		out = append(out, filepath.Join(dir, f))
	}
	for _, f := range pkg.XTestGoFiles {
		out = append(out, filepath.Join(dir, f))
	}
	return out
}

// runTestFile synthesizes a driver for one _test.go file and runs it.
// Returns false if any test in the file failed.
func (r *testRunner) runTestFile(dir, testFile string, srcFiles []string) bool {
	tests, err := discoverTests(testFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo test: %s: %v\n", testFile, err)
		return false
	}
	if len(tests) == 0 {
		return true
	}

	// Apply -run filter.
	var selected []string
	for _, t := range tests {
		if r.runFilter == "" || strings.Contains(t, r.runFilter) {
			selected = append(selected, t)
		}
	}
	if len(selected) == 0 {
		return true
	}

	driver, err := synthesizeTestDriver(dir, testFile, srcFiles, selected)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo test: %s: %v\n", testFile, err)
		return false
	}
	defer os.Remove(driver)

	if r.verbose {
		fmt.Printf("=== RUN %s\n", testFile)
	}
	code := graalgoRun(r.root, []string{driver})
	if code != 0 {
		fmt.Fprintf(os.Stderr, "--- FAIL: %s (driver exit %d)\n", testFile, code)
		return false
	}
	if r.verbose {
		for _, t := range selected {
			fmt.Printf("--- PASS: %s\n", t)
		}
	}
	return true
}
