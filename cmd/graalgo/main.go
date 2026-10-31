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
	"errors"
	"fmt"
	"go/build"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const version = "0.4.0"

// versionString renders `graalgo version` in `go version` format, so
// tooling that parses `go version go1.27.1 ...` keeps working under
// `alias go=graalgo`. runtime.Version is the toolchain that built the
// driver (the toolchain graalgo tracks); the trailing graalgo/<ver>
// field identifies the drop-in replacement.
func versionString() string {
	return fmt.Sprintf("go version %s graalgo/%s", runtime.Version(), version)
}

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
		fmt.Println(versionString())
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
The package may be a directory or an import path.

The program arguments follow the package or file.
`)
	case "build":
		fmt.Printf(`usage: graalgo build [-o output] [packages]

Compile the named packages and emit an executable artifact: a
self-contained launcher that runs the program through the graalgo
backend. With no package arguments, builds the package in the current
directory. The default artifact name mirrors go build (the directory or
first file base name); -o names the output explicitly. Each named
package is built separately; -o cannot be used with multiple packages.

Package arguments may be directories, import paths, .go files, or
./... patterns (which expand to the packages under a directory,
excluding vendor, testdata, and hidden or underscore-prefixed trees).

Flags:
	-o file		output artifact path
	-ldflags=flags	linker flags (not yet supported)
	-tags=tags	build tags
`)
	case "test":
		fmt.Printf(`usage: graalgo test [build/test flags] [packages]

Test the named packages. Runs each package's tests through the
graalgo backend and reports pass/fail. Package arguments may be
directories, import paths, or ./... patterns.
`)
	case "vet":
		fmt.Printf(`usage: graalgo vet [build flags] [packages]

Vet the named packages: run the frontend's static checks over each
whole package and report likely mistakes. Package arguments may be
directories, import paths, .go files, or ./... patterns.
`)
	case "fmt":
		fmt.Printf(`usage: graalgo fmt [packages]

Format the Go source files in the named packages using gofmt.
Package arguments may be directories, import paths, .go files, or
./... patterns.
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

// Backend exit-code convention (the launchers' Main/P3SyncMain: 1 =
// input/usage/launcher error, 2 = build failure; a program panic also
// exits 2, as in gc). The driver never interprets a run-phase code as
// a backend failure: every `graalgo run` first passes the program
// through a separate --check phase, and any non-zero check result is a
// driver failure (exit 1, no "exit status" line, like `go run` on a
// build failure). A non-zero run-phase code is therefore the program's
// own exit — including a panic's 2 — and gets `go run`'s treatment:
// "exit status N" on stderr, driver exit 1.
//
// Residual ambiguity, accepted deliberately: a backend/launcher failure
// striking only in the run phase, after a passing check, is
// indistinguishable from a program exit. That window is a launcher
// crash between two phases, not a build failure, and the check phase
// already proves the program compiles and the launcher starts.
// Separating the phases, rather than inventing a new backend code,
// keeps the convention inside the driver (the backend's codes are
// unchanged) while giving `run` the same observable contract as
// `go run` in both the build-failure and program-exit cases.
func graalgoRunProgram(root string, args []string) int {
	code := graalgoRun(root, args)
	if code != 0 {
		fmt.Fprintf(os.Stderr, "exit status %d\n", code)
		return 1
	}
	return 0
}

// resolvedPackage is one package argument resolved to its buildable
// files. Keeping packages separate (rather than flattening everything
// into one file list) is what stops `graalgo build ./a ./b` from
// compiling a Franken-package: each package is checked, vetted, and
// built on its own file set.
type resolvedPackage struct {
	// label is the argument (or directory, for pattern expansion) used
	// for output naming and diagnostics.
	label string
	// dir is the package directory, or "" for an explicit .go file list.
	dir string
	// files are the package's buildable .go files, including _test.go
	// files; callers that build or run filter those out.
	files []string
}

// resolveFiles turns package/file arguments into a flat list of .go
// files, for callers that genuinely operate on files (fmt). It is
// resolvePackages flattened: the same per-package, constraint-filtered
// file sets, in argument order.
func resolveFiles(args []string) ([]string, error) {
	pkgs, err := resolvePackages(args)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, p := range pkgs {
		files = append(files, p.files...)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no Go files matched")
	}
	return files, nil
}

// resolvePackages turns package/file arguments into one resolvedPackage
// per package. With no args, resolves the package in the current
// directory. Each argument resolves independently:
//
//   - a directory expands to its buildable .go files (never a raw
//     *.go glob: build constraints — GOOS/GOARCH filename suffixes
//     and //go:build lines — are honored, so gated variants are never
//     compiled together);
//   - an import path resolves via the enclosing module, the module
//     cache, or GOPATH (see importDirForPath in testsynth.go);
//   - a pattern ending in "/..." (including "./...") expands to the
//     packages under that directory, excluding vendor, testdata, and
//     hidden or underscore-prefixed trees, like `go`'s ./...;
//   - .go file arguments form a single file-list package, as in
//     `go build a.go b.go`. Explicit files bypass constraint filtering
//     (the user named them); they must all live in one directory and
//     cannot be mixed with package arguments.
//
// NOTE on -tags: constraint filtering uses build.Default, the same
// context test.go's buildableGoFiles uses. Wiring the driver's -tags
// flag into the context is separate queued work (fix-p1-tags-flag);
// this function deliberately does not touch it.
func resolvePackages(args []string) ([]resolvedPackage, error) {
	if len(args) == 0 {
		files, err := buildableFiles(".")
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			return nil, fmt.Errorf("no Go files in current directory")
		}
		return []resolvedPackage{{label: ".", dir: ".", files: files}}, nil
	}
	var pkgs []resolvedPackage
	var fileArgs []string
	addDirPackage := func(label, dir string) error {
		files, err := buildableFiles(dir)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return fmt.Errorf("no Go files in %s", label)
		}
		pkgs = append(pkgs, resolvedPackage{label: label, dir: dir, files: files})
		return nil
	}
	for _, a := range args {
		if strings.HasSuffix(a, "/...") {
			base := strings.TrimSuffix(a, "/...")
			if base == "" {
				base = "."
			}
			expanded, err := expandDotDotDot(base)
			if err != nil {
				return nil, err
			}
			pkgs = append(pkgs, expanded...)
			continue
		}
		if fi, err := os.Stat(a); err == nil {
			if fi.IsDir() {
				if err := addDirPackage(a, a); err != nil {
					return nil, err
				}
				continue
			}
			// An existing non-directory file: a file-list entry (a
			// non-Go file reaches the backend, which reports it).
			fileArgs = append(fileArgs, a)
			continue
		}
		// Not on disk: a .go file that doesn't exist yet (typo) is
		// left for the backend to report; anything else is tried as
		// an import path before giving up.
		if filepath.Ext(a) == ".go" {
			fileArgs = append(fileArgs, a)
			continue
		}
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		dir, err := importDirForPath(cwd, a)
		if err != nil {
			return nil, fmt.Errorf("no such file, directory, or import path: %s", a)
		}
		if err := addDirPackage(a, dir); err != nil {
			return nil, err
		}
	}
	if len(fileArgs) > 0 {
		if len(pkgs) > 0 {
			return nil, fmt.Errorf("cannot mix .go file arguments with package arguments")
		}
		fileDir := ""
		for _, f := range fileArgs {
			d := filepath.Dir(f)
			if fileDir == "" {
				fileDir = d
			} else if d != fileDir {
				return nil, fmt.Errorf("named files must all be in one directory")
			}
		}
		pkgs = append(pkgs, resolvedPackage{label: fileArgs[0], dir: fileDir, files: fileArgs})
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no Go files matched")
	}
	return pkgs, nil
}

// buildableFiles returns the .go files in dir that satisfy the current
// platform's build constraints, using the same rules as `go build`
// (go/build's ImportDir: filename suffixes and //go:build lines, plus
// the test file sets). Unlike test.go's buildableGoFiles, a directory
// that ImportDir rejects (e.g. two packages in one directory) is an
// error, not a silent fallback to a raw glob — the glob is exactly
// how gated or foreign files ended up compiled together. A directory
// with no buildable files yields an empty list, not an error, so
// pattern expansion can skip it and explicit callers can report it.
func buildableFiles(dir string) ([]string, error) {
	pkg, err := build.Default.ImportDir(dir, 0)
	if err != nil {
		var noGo *build.NoGoError
		if errors.As(err, &noGo) {
			return nil, nil
		}
		return nil, err
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
	return out, nil
}

// expandDotDotDot expands a ./...-style pattern root into one
// resolvedPackage per directory under root that contains buildable
// files, in lexical walk order. vendor, testdata, and hidden or
// underscore-prefixed directories are pruned, matching `go`'s ./...
// semantics. A directory ImportDir rejects aborts the expansion with
// an error rather than being silently skipped: a broken package in
// the tree is information, not something ./... should hide.
func expandDotDotDot(root string) ([]resolvedPackage, error) {
	var pkgs []resolvedPackage
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != root {
			name := d.Name()
			if name == "vendor" || name == "testdata" ||
				strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return fs.SkipDir
			}
		}
		files, err := buildableFiles(path)
		if err != nil {
			return err
		}
		if len(files) > 0 {
			pkgs = append(pkgs, resolvedPackage{label: path, dir: path, files: files})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return pkgs, nil
}
