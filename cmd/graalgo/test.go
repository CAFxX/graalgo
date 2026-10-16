package main

import (
	"errors"
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// cmdTest implements `graalgo test [build/test flags] [packages]`.
//
// Discovers *_test.go files in the named packages and runs each test
// function through the graalgo backend. Test functions are run by
// synthesizing a single-file driver per test file that invokes each
// TestXxx function with a minimal testing shim.
//
// -tags is honored in file selection via go/build constraints
// (buildableGoFiles), like `go test -tags`.
//
// This is the driver's built-in test runner. The full differential
// conformance suite (test/differential/) remains the arbiter for
// graalgo-vs-gc behavior parity.

// testFlags holds the parsed `graalgo test` flags.
type testFlags struct {
	runFilter string // -run: test-name filter regexp
	verbose   bool   // -v: verbose output
	count     int    // -count: run tests n times
	cover     bool   // -cover: coverage (recognized; not yet supported)
	tags      string // -tags: build tags, honored in file selection
}

// flagError is a flag-parsing failure carrying the exit code, mirroring
// cmdBuild's convention: 2 for usage errors (unknown flag, bad value),
// 1 for recognized-but-unsupported flags.
type flagError struct {
	code int
	msg  string
}

func (e *flagError) Error() string { return e.msg }

// errHelp is returned by parseTestFlags when -h/-help/--help is seen.
var errHelp = errors.New("help requested")

// testUsage is printed for `graalgo test -h`.
const testUsage = `usage: graalgo test [build/test flags] [packages]

Test the named packages. Runs each package's tests through the
graalgo backend and reports pass/fail.

Flags:
	-v		verbose output
	-run=regexp	run only tests matching regexp
	-count=n		run tests n times (n=1 disables caching; graalgo
			has no test cache, so every run is fresh)
	-cover		enable coverage (not yet supported)
	-tags=list	build tags (affects which files are built, like go)
`

// parseTestFlags separates test flags from package args, following `go`
// flag conventions: -flag value and -flag=value. The build flags that
// parseBuildFlags accepts (-tags, -o, -ldflags, -a/-x/-work/-n) are
// recognized here too, so `graalgo test` accepts the same flag surface
// as `graalgo build`; -tags is honored in file selection (via
// buildableGoFiles), the rest behave exactly as they do for build.
func parseTestFlags(args []string) (testFlags, []string, error) {
	tf := testFlags{count: 1}
	var pkgs []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, val, hasVal := strings.Cut(a, "=")
		next := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", &flagError{code: 2, msg: fmt.Sprintf("graalgo test: flag %s: missing argument", name)}
			}
			i++
			return args[i], nil
		}
		switch {
		case a == "-v":
			tf.verbose = true
		case a == "-h" || a == "-help" || a == "--help":
			return tf, nil, errHelp
		case name == "-run":
			v, err := next()
			if err != nil {
				return tf, nil, err
			}
			tf.runFilter = v
		case name == "-count":
			v, err := next()
			if err != nil {
				return tf, nil, err
			}
			n, err := parseCount(v)
			if err != nil {
				return tf, nil, &flagError{code: 2, msg: fmt.Sprintf("graalgo test: %v", err)}
			}
			tf.count = n
		case a == "-cover":
			tf.cover = true
		case name == "-tags":
			v, err := next()
			if err != nil {
				return tf, nil, err
			}
			tf.tags = v
		case name == "-o":
			// -o is meaningless without -c (unsupported); reject
			// clearly rather than silently ignoring.
			return tf, nil, &flagError{code: 2, msg: "graalgo test: -o: not supported (no -c flag)"}
		case name == "-ldflags":
			return tf, nil, &flagError{code: 1, msg: "graalgo test: -ldflags: not yet supported"}
		case a == "-a" || a == "-x" || a == "-work" || a == "-n":
			// Accepted and ignored, like `graalgo build`.
		case strings.HasPrefix(a, "-"):
			return tf, nil, &flagError{code: 2, msg: fmt.Sprintf("graalgo test: unknown flag %q", a)}
		default:
			pkgs = append(pkgs, a)
		}
	}
	return tf, pkgs, nil
}

func cmdTest(root string, args []string) int {
	tf, pkgs, err := parseTestFlags(args)
	if err != nil {
		if errors.Is(err, errHelp) {
			fmt.Print(testUsage)
			return 0
		}
		var fe *flagError
		if errors.As(err, &fe) {
			fmt.Fprintln(os.Stderr, fe.msg)
			return fe.code
		}
		fmt.Fprintf(os.Stderr, "graalgo test: %v\n", err)
		return 2
	}
	if tf.cover {
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
	if tf.runFilter != "" {
		runRe, err = regexp.Compile(tf.runFilter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "graalgo test: invalid -run regexp %q: %v\n", tf.runFilter, err)
			return 1
		}
	}

	// Build tags for file selection, like `go test -tags`.
	// tf.tags is the raw flag value (comma/space separated); splitTags
	// applies `go`'s splitting rules.
	tags := splitTags(tf.tags)

	runner := newTestRunner(root, tf.verbose, runRe, tf.count, tags)
	failed := false
	for _, dir := range dirs {
		// -count=n: run the package's tests n times, like `go test`.
		// A failure in any iteration fails the package.
		for iter := 0; iter < tf.count; iter++ {
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
	tags    []string
}

func newTestRunner(root string, verbose bool, runRe *regexp.Regexp, count int, tags []string) *testRunner {
	return &testRunner{root: root, verbose: verbose, runRe: runRe, count: count, tags: tags}
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
	buildable := buildableGoFiles(dir, r.tags)
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
	// filter (regex, like `go test`).
	var ftList []fileTests
	for _, tf := range runnableTests {
		tests, err := discoverTests(tf)
		if err != nil {
			fmt.Fprintf(os.Stderr, "graalgo test: %s: %v\n", tf, err)
			return false
		}
		var selected []string
		for _, t := range tests {
			if r.runRe == nil || r.runRe.MatchString(t) {
				selected = append(selected, t)
			}
		}
		if len(selected) > 0 {
			ftList = append(ftList, fileTests{file: tf, tests: selected})
		}
	}

	// The synthesized driver prints go-test-format output itself
	// (=== RUN / --- PASS/FAIL lines, PASS/FAIL summary); a non-zero
	// exit means the package failed.
	t0 := time.Now()
	ok := true
	if len(ftList) > 0 {
		driver, err := synthesizeTestDriver(dir, ftList, srcFiles, r.verbose, r.tags)
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
		if code := graalgoRun(r.root, []string{"--test-package-dir=" + absDir, driver}); code != 0 {
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
	if len(ftList) == 0 {
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
// platform's build constraints and the given build tags, using the same
// rules as `go build -tags`. A nil tags slice selects the platform
// defaults.
func buildableGoFiles(dir string, tags []string) []string {
	ctxt := build.Default
	ctxt.BuildTags = tags
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
