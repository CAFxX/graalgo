package main

import (
	"context"
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
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
	// Separate test flags from package args. We accept -run, -v, -count,
	// -cover, and -timeout (-count and -cover for compatibility;
	// see below).
	var runFilter string
	var verbose bool
	var count int = 1
	var cover bool
	// Like `go test`, the default timeout is 10 minutes; 0 disables it.
	var timeout time.Duration = 10 * time.Minute
	var pkgs []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-v":
			verbose = true
		case a == "-h" || a == "-help" || a == "--help":
			fmt.Printf(`usage: graalgo test [build/test flags] [packages]

Test the named packages. Runs each package's tests through the
graalgo backend and reports pass/fail.

Flags:
	-v		verbose output
	-run=regexp	run only tests matching regexp
	-count=n		run tests n times (n=1 disables caching; graalgo
			has no test cache, so every run is fresh)
	-cover		enable coverage (not yet supported)
	-timeout d	fail the package if the backend runs longer than d
			(default 10m, like go test; 0 disables the timeout)
`)
			return 0
		case a == "-run" && i+1 < len(args):
			i++
			runFilter = args[i]
		case strings.HasPrefix(a, "-run="):
			runFilter = strings.TrimPrefix(a, "-run=")
		case a == "-count" && i+1 < len(args):
			i++
			n, err := parseCount(args[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "graalgo test: %v\n", err)
				return 2
			}
			count = n
		case strings.HasPrefix(a, "-count="):
			n, err := parseCount(strings.TrimPrefix(a, "-count="))
			if err != nil {
				fmt.Fprintf(os.Stderr, "graalgo test: %v\n", err)
				return 2
			}
			count = n
		case a == "-timeout" && i+1 < len(args):
			i++
			d, err := parseTimeout(args[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "graalgo test: %v\n", err)
				return 2
			}
			timeout = d
		case strings.HasPrefix(a, "-timeout="):
			d, err := parseTimeout(strings.TrimPrefix(a, "-timeout="))
			if err != nil {
				fmt.Fprintf(os.Stderr, "graalgo test: %v\n", err)
				return 2
			}
			timeout = d
		case a == "-cover":
			cover = true
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "graalgo test: unknown flag %q\n", a)
			return 2
		default:
			pkgs = append(pkgs, a)
		}
	}
	if cover {
		fmt.Fprintf(os.Stderr, "graalgo test: -cover: coverage not yet supported\n")
		return 1
	}

	// Resolve package dirs.
	dirs, err := resolveTestDirs(pkgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo test: %v\n", err)
		return 1
	}

	// Compile the -run filter as a regex, matching `go test` semantics.
	// An invalid regex is a hard error, like `go test`.
	var runRe *regexp.Regexp
	if runFilter != "" {
		runRe, err = regexp.Compile(runFilter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "graalgo test: invalid -run regexp %q: %v\n", runFilter, err)
			return 1
		}
	}

	runner := newTestRunner(root, verbose, runRe, count, timeout)
	failed := false
	for _, dir := range dirs {
		// -count=n: run the package's tests n times, like `go test`.
		// A failure in any iteration fails the package.
		for iter := 0; iter < count; iter++ {
			if !runner.runPackage(dir) {
				failed = true
				break
			}
		}
	}
	if failed {
		return 1
	}
	return 0
}

// parseCount parses a -count=n value. Like `go test`, n must be >= 1.
func parseCount(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid -count %q: must be a positive integer", s)
	}
	return n, nil
}

// parseTimeout parses a -timeout value. Like `go test`, the default is
// 10m and 0 disables the timeout; a negative value also disables it
// (time.ParseDuration accepts it, and the runner treats timeout <= 0
// as "no deadline").
func parseTimeout(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid -timeout %q: %v", s, err)
	}
	return d, nil
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
	root    string
	verbose bool
	runRe   *regexp.Regexp
	count   int
	// timeout bounds each package's backend run (one backend invocation
	// per package per -count iteration, like `go test`); <= 0 disables
	// the deadline.
	timeout time.Duration
	// runBackend invokes the backend for one package. It is a field
	// (default graalgoRunCtx) so tests can substitute a fake backend
	// without the Truffle runtime; it reports whether the backend was
	// killed by the context deadline.
	runBackend func(ctx context.Context, root string, args []string) (code int, timedOut bool)
}

func newTestRunner(root string, verbose bool, runRe *regexp.Regexp, count int, timeout time.Duration) *testRunner {
	return &testRunner{root: root, verbose: verbose, runRe: runRe, count: count, timeout: timeout, runBackend: graalgoRunCtx}
}

// runPackage discovers and runs the tests in one directory.
// Returns false if any test failed.
//
// A single driver is synthesized per package (not per file) so the
// output matches `go test`: per-test === RUN / --- PASS lines and one
// PASS/FAIL summary, then the `ok`/`FAIL` package line with the import
// path and timing.
func (r *testRunner) runPackage(dir string) bool {
	importPath := importPathForDir(dir)
	testFiles, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil || len(testFiles) == 0 {
		fmt.Printf("?   \t%s\t[no test files]\n", importPath)
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

	// Discover the selected tests of every file, applying the -run
	// filter (regex, like `go test`), and the TestMain entry point
	// (A-2). A file defining TestMain joins ftList even when no test
	// is selected: gc runs TestMain with -run matching nothing, and
	// its m.Run() then reports zero tests.
	var ftList []fileTests
	totalSelected := 0
	var testMainFile string
	testMainAsTest := false
	testMainAsTestFiles := 0
	for _, tf := range runnableTests {
		tests, err := discoverTests(tf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "graalgo test: %s: %v\n", tf, err)
			return false
		}
		hasMain, err := discoverTestMain(tf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "graalgo test: %s: %v\n", tf, err)
			return false
		}
		if hasMain {
			if testMainFile != "" {
				fmt.Fprintf(os.Stderr, "graalgo test: multiple definitions of TestMain\n")
				return false
			}
			testMainFile = tf
		}
		var selected []string
		asTestInFile := false
		for _, t := range tests {
			if t == "TestMain" && tf != testMainFile {
				// A TestMain-as-test in another file than the entry
				// point is a redeclaration (gc: "TestMain redeclared
				// in this block" [build failed]).
				testMainAsTest = true
				asTestInFile = true
			}
			if r.runRe == nil || r.runRe.MatchString(t) {
				selected = append(selected, t)
			}
		}
		if asTestInFile {
			testMainAsTestFiles++
		}
		totalSelected += len(selected)
		if len(selected) > 0 || hasMain {
			ftList = append(ftList, fileTests{file: tf, tests: selected, testMain: hasMain})
		}
	}
	if testMainFile != "" && testMainAsTest {
		fmt.Fprintf(os.Stderr, "graalgo test: TestMain redeclared in this block\n")
		return false
	}
	if testMainAsTestFiles > 1 {
		// Two files each defining TestMain as an ordinary test
		// (func TestMain(m *testing.T)) with no entry point: gc
		// rejects the package ("TestMain redeclared in this block").
		// Without this check the duplicate slips through to the
		// driver, which fails to compile with a confusing error.
		fmt.Fprintf(os.Stderr, "graalgo test: TestMain redeclared in this block\n")
		return false
	}

	// The synthesized driver prints go-test-format output itself
	// (=== RUN / --- PASS/FAIL lines, PASS/FAIL summary); a non-zero
	// exit means the package failed.
	t0 := time.Now()
	ok := true
	if len(ftList) > 0 {
		driver, err := synthesizeTestDriver(dir, ftList, srcFiles, r.verbose)
		if err != nil {
			fmt.Fprintf(os.Stderr, "graalgo test: %v\n", err)
			return false
		}
		defer os.Remove(driver)
		// The driver is the package's own test binary (white-box, like
		// `go test`): pass the package dir as an explicit argv flag so
		// the frontend applies Go's internal-visibility rule with the
		// package's own privileges (e.g. stdlib internal/* imports). A
		// flag is per-invocation and non-inheritable: unlike an
		// environment variable, a user-exported value cannot confer
		// privilege on `graalgo run`/`build`/`vet`.
		absDir, err := filepath.Abs(dir)
		if err != nil {
			absDir = dir
		}
		// Bound the backend run: a hung guest test must fail the
		// package with a timeout line (like `go test -timeout`)
		// instead of hanging the driver forever. Each -count
		// iteration gets a fresh deadline, as in `go test`.
		ctx := context.Background()
		var cancel context.CancelFunc = func() {}
		if r.timeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, r.timeout)
		}
		defer cancel()
		code, timedOut := r.runBackend(ctx, r.root, []string{"--test-package-dir=" + absDir, driver})
		switch {
		case timedOut:
			fmt.Printf("panic: test timed out after %s\n", r.timeout)
			ok = false
		case code != 0:
			ok = false
		}
	} else if r.verbose {
		// `go test -v` with no tests selected prints a warning and
		// a PASS summary before the ok line.
		fmt.Println("testing: warning: no tests to run")
		fmt.Println("PASS")
	}
	elapsed := time.Since(t0).Seconds()
	okSuffix := ""
	if totalSelected == 0 {
		okSuffix = " [no tests to run]"
	}
	if ok {
		fmt.Printf("ok  \t%s\t%.3fs%s\n", importPath, elapsed, okSuffix)
	} else {
		fmt.Printf("FAIL\t%s\t%.3fs\nFAIL\n", importPath, elapsed)
	}
	return ok
}

// importPathForDir resolves a directory to the import path `go test`
// prints, using the enclosing go.mod module path.
func importPathForDir(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	if modRoot, module := moduleForDir(dir); module != "" {
		rel, err := filepath.Rel(modRoot, abs)
		if err != nil || rel == "." {
			return module
		}
		return module + "/" + filepath.ToSlash(rel)
	}
	return abs
}

// parseModulePath extracts the module path from go.mod data.
func parseModulePath(modData []byte) string {
	for _, line := range strings.Split(string(modData), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
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
