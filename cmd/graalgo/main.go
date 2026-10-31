// Command graalgo is the Phase-4 driver: a drop-in replacement for the go
// command that routes through graalgo's Truffle backend.
//
// Usage:
//
//	graalgo <command> [arguments]
//
// Commands:
//
//	run       compile and run a Go program
//	build     compile packages and dependencies
//	test      test packages
//	vet       vet packages
//	fmt       format packages
//	version   print version information
//
// The driver locates the graalgo installation via the GRAALGO_HOME
// environment variable, or by walking up from its own executable path to
// find internal/backend/graalgo-sync-run.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const version = "0.4.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]

	// Handle help and version before requiring installation
	switch cmd {
	case "help", "-h", "--help":
		if len(args) > 0 {
			helpFor(args[0])
		} else {
			usage()
		}
		return
	case "version", "-v", "--version":
		fmt.Printf("graalgo version %s\n", version)
		return
	}

	root, err := findGraalgoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo: %v\n", err)
		os.Exit(1)
	}

	var exitCode int
	switch cmd {
	case "run":
		exitCode = cmdRun(root, args)
	case "build":
		exitCode = cmdBuild(root, args)
	case "test":
		exitCode = cmdTest(root, args)
	case "vet":
		exitCode = cmdVet(root, args)
	case "fmt":
		exitCode = cmdFmt(root, args)
	default:
		fmt.Fprintf(os.Stderr, "graalgo: unknown command %q\n", cmd)
		fmt.Fprintf(os.Stderr, "Run 'graalgo help' for usage.\n")
		exitCode = 2
	}
	os.Exit(exitCode)
}

func usage() {
	fmt.Printf(`graalgo is a Go toolchain on GraalVM Truffle.

Usage:

	graalgo <command> [arguments]

The commands are:

	run         compile and run a Go program
	build       compile packages and dependencies
	test        test packages
	vet         vet packages
	fmt         format source files
	version     print version information

Use "graalgo help <command>" for more information about a command.
`)
}

func helpFor(cmd string) {
	switch cmd {
	case "run":
		fmt.Printf(`usage: graalgo run [build flags] [package] [arguments...]

Compile and run the Go program in package, or the single file if given.

The program arguments follow the package or file.
`)
	case "build":
		fmt.Printf(`usage: graalgo build [-o output] [packages]

Compile the named packages and emit an executable artifact: a
self-contained launcher that runs the program through the graalgo
backend. With no package arguments, builds the package in the current
directory. The default artifact name mirrors go build (the directory or
first file base name); -o names the output explicitly.

Package patterns ("./...", "<dir>/...") are supported: every matched
package is validated, and when several packages match, the results are
discarded like "go build" (no artifact is emitted); -o with several
matched packages is an error.

Flags:
	-o file		output artifact path
	-ldflags=flags	linker flags (not yet supported)
	-tags=tags	build tags
`)
	case "test":
		fmt.Printf(`usage: graalgo test [build/test flags] [packages]

Test the named packages. Runs each package's tests through the
graalgo backend and reports pass/fail.
`)
	case "vet":
		fmt.Printf(`usage: graalgo vet [build flags] [packages]

Vet the named packages: run the frontend's static checks and
report likely mistakes.
`)
	case "fmt":
		fmt.Printf(`usage: graalgo fmt [packages]

Format the Go source files in the named packages using gofmt.
`)
	default:
		fmt.Printf("graalgo: unknown command %q\n", cmd)
	}
}

// findGraalgoRoot locates the graalgo installation root: the directory
// containing internal/backend/graalgo-sync-run (the product launcher).
func findGraalgoRoot() (string, error) {
	// 1. GRAALGO_HOME env var
	if home := os.Getenv("GRAALGO_HOME"); home != "" {
		if isGraalgoRoot(home) {
			return home, nil
		}
		return "", fmt.Errorf("GRAALGO_HOME=%q is not a graalgo installation", home)
	}

	// 2. Walk up from the executable's directory
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		for i := 0; i < 6; i++ {
			if isGraalgoRoot(dir) {
				return dir, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	// 3. Current working directory and parents
	if cwd, err := os.Getwd(); err == nil {
		dir := cwd
		for i := 0; i < 6; i++ {
			if isGraalgoRoot(dir) {
				return dir, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	return "", fmt.Errorf("cannot locate graalgo installation (set GRAALGO_HOME)")
}

func isGraalgoRoot(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "internal", "backend", "graalgo-sync-run"))
	return err == nil && !info.IsDir()
}

// graalgoRun invokes internal/backend/graalgo-sync-run with the given args,
// wiring stdio through. Returns the process exit code.
//
// NOTE: privilege-conferring signals (e.g. PKG-001's test-package-dir)
// travel as explicit argv flags, never environment variables: env is
// inherited by every child process, so a user-exported variable would
// confer privilege on commands that never asked for it (`graalgo run`).
func graalgoRun(root string, args []string) int {
	runner := filepath.Join(root, "internal", "backend", "graalgo-sync-run")
	cmd := exec.Command(runner, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "graalgo: %v\n", err)
		return 1
	}
	return 0
}

// graalgoRunProgram invokes the backend for `graalgo run` and emulates
// `go run`'s exit contract: when the program exits non-zero, `go run`
// prints "exit status N" to stderr and exits 1 (rather than propagating
// the raw code). This keeps `graalgo run` byte-identical to `go run` on
// stderr and exit code for programs that exit non-zero.
//
// Known wart: the backend's own failure codes (1 = input error, 2 =
// build failure) share the exit-code space with program exits, so a
// backend failure also gets the "exit status N" treatment; `go run`
// prints no such line for its own build failures. Disambiguating needs
// a backend exit-code convention change.
func graalgoRunProgram(root string, args []string) int {
	code := graalgoRun(root, args)
	if code != 0 {
		fmt.Fprintf(os.Stderr, "exit status %d\n", code)
		return 1
	}
	return 0
}

// fileGroup is one unit the backend compiles together: either the files
// of one package directory (from a directory or pattern argument) or one
// flat set of explicitly named files. Keeping pattern matches grouped per
// package matters: compiling files from several packages as one unit
// would merge their package clauses, so a pattern must never flatten.
type fileGroup struct {
	dir     string // package directory for directory/pattern groups; "" for explicit files
	pattern string // pattern argument this group came from; "" otherwise
	files   []string
}

// splitPattern parses a package-pattern argument of the form "<dir>/...",
// mirroring `go help packages`: "./..." matches the packages under the
// current directory, "./pkg/..." those under pkg. It reports whether
// the argument is a pattern at all. A bare "..." is a different gc
// pattern (all packages, including the standard library) and is
// rejected explicitly rather than silently treated as "./..."; any
// other "..." placement is invalid.
func splitPattern(arg string) (prefix string, isPattern bool, err error) {
	if arg == "..." {
		return "", false, fmt.Errorf(`"..." matches all packages including the standard library and is not yet supported; use "./..." to match packages under the current directory`)
	}
	if !strings.Contains(arg, "...") {
		return "", false, nil
	}
	if !strings.HasSuffix(arg, "/...") {
		return "", false, fmt.Errorf("invalid package pattern %q", arg)
	}
	prefix = strings.TrimSuffix(arg, "/...")
	if prefix == "" {
		return "", false, fmt.Errorf("invalid package pattern %q", arg)
	}
	return prefix, true, nil
}

// expandPatternDirs returns the package directories matched by a
// "<dir>/..." pattern, in deterministic (lexical, parent-first) order.
// A directory counts as a package directory when it holds at least one
// .go file (test files included; callers filter _test.go as their
// command requires, exactly like `go build`/`go test` do). Following
// `go help packages`, the walk skips directories whose name starts with
// "." or "_", directories named "testdata" or "vendor", symlinked
// directories, and nested modules (a subdirectory holding its own
// go.mod belongs to another module, so the walk does not descend into
// it). The root directory itself is always a candidate.
func expandPatternDirs(prefix, arg string) ([]string, error) {
	fi, err := os.Stat(prefix)
	if err != nil {
		return nil, fmt.Errorf("pattern %s: %v", arg, err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("pattern %s: %s is not a directory", arg, prefix)
	}
	// The ignore rules apply to the pattern root itself, not just to
	// directories reached while descending: gc reports "matched no
	// packages" for ./testdata/... and ./_hidden/... (verified against
	// gc 1.27). A vendor root is a different gc error (it requires
	// vendor/modules.txt), so it fails loudly instead of matching
	// nothing silently.
	if base := filepath.Base(filepath.Clean(prefix)); base != "." {
		switch {
		case base == "vendor":
			return nil, fmt.Errorf("pattern %s: vendor directories are not matched by ... patterns (gc lists them only via vendor/modules.txt)", arg)
		case strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") || base == "testdata":
			return []string{}, nil
		}
	}
	var dirs []string
	var walk func(dir string, isRoot bool) error
	walk = func(dir string, isRoot bool) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if isRoot {
				return err
			}
			return nil // unreadable subdirectory: skip, like a non-package
		}
		hasGo := false
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
				hasGo = true
				break
			}
		}
		if hasGo {
			dirs = append(dirs, dir)
		}
		for _, e := range entries {
			if e.Type()&os.ModeSymlink != 0 || !e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
				name == "testdata" || name == "vendor" {
				continue
			}
			child := filepath.Join(dir, name)
			if _, err := os.Stat(filepath.Join(child, "go.mod")); err == nil {
				continue // nested module: a different module's tree
			}
			if err := walk(child, false); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(prefix, true); err != nil {
		return nil, err
	}
	return dirs, nil
}

// resolveFileGroups turns package/file arguments into per-package file
// groups. With no args, uses *.go in the current directory. Directories
// are expanded to their *.go files (non-recursive, like a simple
// package). Pattern arguments ("./...", "<dir>/...") expand to one
// group per matched package directory, deduplicated across overlapping
// patterns. Non-pattern arguments keep the historical flat semantics:
// they accumulate into a single group in argument order.
func resolveFileGroups(args []string) ([]fileGroup, error) {
	if len(args) == 0 {
		matches, err := filepath.Glob("*.go")
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no Go files in current directory")
		}
		return []fileGroup{{files: matches}}, nil
	}
	var groups []fileGroup
	var pending []string // explicitly named files/dirs, one flat group
	flush := func() {
		if len(pending) > 0 {
			groups = append(groups, fileGroup{files: pending})
			pending = nil
		}
	}
	seenPatternDirs := map[string]bool{}
	for _, a := range args {
		prefix, isPattern, err := splitPattern(a)
		if err != nil {
			return nil, err
		}
		if isPattern {
			flush()
			dirs, err := expandPatternDirs(prefix, a)
			if err != nil {
				return nil, err
			}
			if len(dirs) == 0 {
				return nil, fmt.Errorf("pattern %s: matched no packages", a)
			}
			for _, d := range dirs {
				key := filepath.Clean(d)
				if seenPatternDirs[key] {
					continue
				}
				seenPatternDirs[key] = true
				matches, err := filepath.Glob(filepath.Join(d, "*.go"))
				if err != nil {
					return nil, err
				}
				groups = append(groups, fileGroup{dir: d, pattern: a, files: matches})
			}
			continue
		}
		fi, err := os.Stat(a)
		if err != nil {
			// Maybe a .go file that doesn't exist yet (typo) — let
			// the backend report the error.
			if filepath.Ext(a) == ".go" {
				pending = append(pending, a)
				continue
			}
			return nil, fmt.Errorf("no such file or directory: %s", a)
		}
		if fi.IsDir() {
			matches, err := filepath.Glob(filepath.Join(a, "*.go"))
			if err != nil {
				return nil, err
			}
			pending = append(pending, matches...)
			continue
		}
		pending = append(pending, a)
	}
	flush()
	if len(groups) == 0 {
		return nil, fmt.Errorf("no Go files matched")
	}
	return groups, nil
}

// resolveFiles turns package/file arguments into a flat list of .go
// files. It flattens resolveFileGroups, so it is only correct for
// callers that treat files independently of package boundaries (fmt);
// build/run/vet use resolveFileGroups directly.
func resolveFiles(args []string) ([]string, error) {
	groups, err := resolveFileGroups(args)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, g := range groups {
		files = append(files, g.files...)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no Go files matched")
	}
	return files, nil
}
