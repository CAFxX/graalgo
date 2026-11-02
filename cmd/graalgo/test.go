package main

import (
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
	// and -cover (the last two for compatibility; see below).
	var runFilter string
	var verbose bool
	var count int = 1
	var cover bool
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
	-run=regexp	run only tests matching regexp (subtest selection
			with / is not yet supported and is rejected)
	-count=n		run tests n times (n=1 disables caching; graalgo
			has no test cache, so every run is fresh)
	-cover		enable coverage (not yet supported)
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

	// Resolve test operands (directories, or file-mode targets).
	targets, err := resolveTestTargets(pkgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo test: %v\n", err)
		return 1
	}

	// Parse the -run filter. Like `go test`, an invalid regex is a hard
	// error; a slash-separated subtest selector is also a hard error
	// (see parseRunFilter): the driver has no way to filter subtests
	// today, so rejecting loudly replaces the previous silent false
	// pass (`ok [no tests to run]`, exit 0).
	var runRes []*regexp.Regexp
	if runFilter != "" {
		runRes, err = parseRunFilter(runFilter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "graalgo test: %v\n", err)
			return 1
		}
	}

	runner := newTestRunner(root, verbose, runRes, count)
	failed := false
	for _, tgt := range targets {
		// -count=n: run the package's tests n times, like `go test`.
		// A failure in any iteration fails the package.
		for iter := 0; iter < count; iter++ {
			if !runner.runPackage(tgt) {
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

// testTarget is one resolved test operand: a directory, optionally
// restricted to explicitly listed files (go test file mode).
//
// files == nil means "the whole directory" (a directory operand, or no
// operand). files != nil lists .go files in dir, in argument order:
// the package under test is built from exactly those files — listed
// test files are the only test files run, and listed non-test files are
// the only package sources inlined for white-box tests, matching `go
// test` file mode. (External test files import the directory package
// through the module, as `go test` file mode does, so they are
// unaffected by the source restriction.)
//
// Known leniencies vs `go test` (deliberate, not silent mis-runs):
//   - file operands in several directories are each run for their own
//     directory; `go test` rejects such lists ("named files must all
//     be in one directory").
//   - a non-.go file operand still tests its whole directory (the old
//     behaviour); `go test` rejects non-.go operands.
//   - a directory operand mixed with file operands runs both; `go test`
//     rejects the mix.
type testTarget struct {
	dir   string
	files []string // nil = whole directory; otherwise absolute .go paths in dir
}

// resolveTestTargets turns test operands into targets. Directory
// operands become whole-directory targets. .go file operands coalesce
// per directory into one file-mode target per directory, so `graalgo
// test a_test.go b_test.go` runs one driver for the package built from
// exactly those files. Previously a file operand was reduced to its
// directory and the filename discarded, so `graalgo test a_test.go`
// ran every test in the directory — including failing tests from
// other files — and could fail (or pass) for tests never named.
func resolveTestTargets(pkgs []string) ([]testTarget, error) {
	if len(pkgs) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		return []testTarget{{dir: cwd}}, nil
	}
	var targets []testTarget
	fileTargetByDir := make(map[string]int)
	for _, p := range pkgs {
		if p == "./..." {
			return nil, fmt.Errorf("./... not yet supported")
		}
		fi, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("no such file or directory: %s", p)
		}
		if fi.IsDir() {
			targets = append(targets, testTarget{dir: p})
			continue
		}
		dir := filepath.Dir(p)
		if filepath.Ext(p) != ".go" {
			// Not a Go file: keep the old behaviour (its directory).
			targets = append(targets, testTarget{dir: dir})
			continue
		}
		absFile, err := filepath.Abs(p)
		if err != nil {
			absFile = p
		}
		absDir, err := filepath.Abs(dir)
		if err != nil {
			absDir = dir
		}
		if i, ok := fileTargetByDir[absDir]; ok {
			// Coalesce, deduplicating a file listed twice.
			dup := false
			for _, f := range targets[i].files {
				if f == absFile {
					dup = true
					break
				}
			}
			if !dup {
				targets[i].files = append(targets[i].files, absFile)
			}
			continue
		}
		fileTargetByDir[absDir] = len(targets)
		targets = append(targets, testTarget{dir: dir, files: []string{absFile}})
	}
	return targets, nil
}

// parseRunFilter parses a -run pattern into the top-level regexes that
// select tests, following `go test`'s split rules (see splitRunPattern):
// the pattern is split on unbracketed, unparenthesized slashes into
// per-level elements, and on top-level '|' into alternatives. A
// top-level test matches when any alternative's first element matches
// it — exactly the selection runPackage applies.
//
// Slash-separated subtest selection is NOT representable here: after
// stripping trailing empty elements (so `TestX/` is just `TestX`), any
// alternative with more than one element needs a filter inside the
// test driver, applied in graalgoT.Run as subtests execute. That filter
// would need a regex engine in the synthesized driver, and `regexp`
// does not compile under graalgo (the frontend rejects the package:
// `identifier "Regexp" denotes *types.TypeName`, even for a bare
// regexp.MatchString call). Until the driver can filter subtests, a
// multi-element pattern is a hard error rather than a silent
// `ok [no tests to run]`: a filter that matches nothing must never
// read as a pass.
//
// An invalid element regex is likewise a hard error, like `go test`.
func parseRunFilter(pattern string) ([]*regexp.Regexp, error) {
	// Strip trailing empty elements from each alternative: they match
	// every deeper level, so they impose no subtest filter (`TestX/`
	// selects TestX whole). An alternative left empty (pattern was all
	// slashes) matches everything.
	var stripped [][]string
	for _, alt := range splitRunPattern(pattern) {
		for len(alt) > 0 && alt[len(alt)-1] == "" {
			alt = alt[:len(alt)-1]
		}
		if len(alt) == 0 {
			alt = []string{""}
		}
		stripped = append(stripped, alt)
	}
	for _, alt := range stripped {
		if len(alt) > 1 {
			return nil, fmt.Errorf("-run pattern %q selects subtests (slash-separated), which is not yet supported: run the top-level test without the /suffix (e.g. -run %s)", pattern, alt[0])
		}
	}
	var res []*regexp.Regexp
	for ai, alt := range stripped {
		elem := rewriteRunElement(alt[0])
		re, err := regexp.Compile(elem)
		if err != nil {
			return nil, fmt.Errorf("invalid -run regexp %q (alternative %d element %q): %v", pattern, ai, alt[0], err)
		}
		res = append(res, re)
	}
	return res, nil
}

// matchRunFilter reports whether name is selected by the parsed -run
// filter: any alternative's top-level regex matching is enough. A nil
// filter selects everything.
func matchRunFilter(res []*regexp.Regexp, name string) bool {
	if res == nil {
		return true
	}
	for _, re := range res {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

// splitRunPattern splits a -run pattern the way `go test` does
// (testing.splitRegexp): '/' separates levels and '|' separates
// alternatives, but only when outside [...] character classes and
// (...) groups; a backslash escapes the next character (so `\/` is not
// a separator). A '/' inside brackets, parens, or after a backslash
// stays part of its element — such a pattern is a single-level regex,
// not subtest selection.
func splitRunPattern(s string) [][]string {
	var alts [][]string
	var cur []string
	cs, cp := 0, 0 // bracket depth, paren depth (outside brackets)
	rest := s
	for i := 0; i < len(rest); {
		switch rest[i] {
		case '[':
			cs++
		case ']':
			if cs--; cs < 0 { // An unmatched ']' is legal.
				cs = 0
			}
		case '(':
			if cs == 0 {
				cp++
			}
		case ')':
			if cs == 0 {
				cp--
			}
		case '\\':
			i++ // Skip the escaped character.
		case '/':
			if cs == 0 && cp == 0 {
				cur = append(cur, rest[:i])
				rest = rest[i+1:]
				i = 0
				continue
			}
		case '|':
			if cs == 0 && cp == 0 {
				cur = append(cur, rest[:i])
				alts = append(alts, cur)
				cur = nil
				rest = rest[i+1:]
				i = 0
				continue
			}
		}
		i++
	}
	cur = append(cur, rest)
	alts = append(alts, cur)
	return alts
}

// rewriteRunElement rewrites one -run pattern element the way `go test`
// rewrites subtest names for matching (testing.rewrite): whitespace
// becomes '_'. Without this, a pattern naming a subtest with spaces
// via its underscore form (e.g. -run 'TestX/my_sub' for t.Run("my
// sub", ...)) would never match. Non-printable characters, which `go
// test` rewrites to quoted forms, are left as-is: subtest names
// containing them are vanishingly rare, and a wrong rewrite there
// would be worse than none.
func rewriteRunElement(s string) string {
	var b strings.Builder
	for _, r := range s {
		if isRunSpace(r) {
			b.WriteByte('_')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isRunSpace reports whether r is a space character in `go test`'s
// sense (testing.isSpace).
func isRunSpace(r rune) bool {
	if r < 0x2000 {
		switch r {
		case '\t', '\n', '\v', '\f', '\r', ' ', 0x85, 0xA0, 0x1680:
			return true
		}
		return false
	}
	if r <= 0x200a {
		return true
	}
	switch r {
	case 0x2028, 0x2029, 0x202f, 0x205f, 0x3000:
		return true
	}
	return false
}

type testRunner struct {
	root    string
	verbose bool
	runRes  []*regexp.Regexp
	count   int
}

func newTestRunner(root string, verbose bool, runRes []*regexp.Regexp, count int) *testRunner {
	return &testRunner{root: root, verbose: verbose, runRes: runRes, count: count}
}

// runPackage discovers and runs the tests of one target.
// Returns false if any test failed.
//
// A single driver is synthesized per package (not per file) so the
// output matches `go test`: per-test === RUN / --- PASS lines and one
// PASS/FAIL summary, then the `ok`/`FAIL` package line with the import
// path and timing.
//
// In file mode (tgt.files != nil) only the listed test files run, and
// only the listed non-test files are inlined as package sources —
// listed files are used as given, without the build-constraint filter,
// matching `go test` file mode. In directory mode the current behaviour
// is kept: every *_test.go in the directory, intersected with the
// buildable file set.
func (r *testRunner) runPackage(tgt testTarget) bool {
	dir := tgt.dir
	importPath := importPathForDir(dir)
	var runnableTests []string
	var srcFiles []string
	fileMode := tgt.files != nil
	if fileMode {
		for _, f := range tgt.files {
			if strings.HasSuffix(f, "_test.go") {
				runnableTests = append(runnableTests, f)
			} else {
				srcFiles = append(srcFiles, f)
			}
		}
	} else {
		testFiles, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
		if err != nil || len(testFiles) == 0 {
			fmt.Printf("?   \t%s\t[no test files]\n", importPath)
			return true
		}
		// Collect non-test Go files in the package (the code under
		// test), honoring build constraints (GOOS/GOARCH/filename
		// suffixes and //go:build lines) — the same file set `go test`
		// would use. Without this, build-tag-gated alternatives (e.g.
		// node_js.go vs node_net.go) are inlined together and
		// redeclare each other's symbols.
		buildable := buildableGoFiles(dir)
		for _, f := range buildable {
			if !strings.HasSuffix(f, "_test.go") {
				srcFiles = append(srcFiles, f)
			}
		}
		// The test files themselves must also satisfy build constraints.
		for _, tf := range testFiles {
			base := filepath.Base(tf)
			for _, b := range buildable {
				if filepath.Base(b) == base {
					runnableTests = append(runnableTests, tf)
					break
				}
			}
		}
	}
	if len(runnableTests) == 0 {
		fmt.Printf("?   \t%s\t[no test files]\n", importPath)
		return true
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
			if matchRunFilter(r.runRes, t) {
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
		driver, err := synthesizeTestDriver(dir, ftList, srcFiles, r.verbose, fileMode)
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
