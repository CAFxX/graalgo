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
	"go/build"
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
			os.Exit(helpFor(args[0]))
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

// helpFor prints usage for a command. It returns the process exit code:
// 0 for a known command, 2 for an unknown one (like `go help <unknown>`).
func helpFor(cmd string) int {
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

Flags:
	-tags=tags	build tags
`)
	case "fmt":
		fmt.Printf(`usage: graalgo fmt [-l] [-d] [-s] [-e] [packages]

Format the Go source files in the named packages using gofmt.
With no flags, rewrites the files in place and prints the names
of the files it changed, like go fmt. -l lists files whose
formatting differs without rewriting them; -d prints the diff;
-s simplifies the code; -e prints all errors.
`)
	default:
		fmt.Fprintf(os.Stderr, "graalgo: unknown command %q\n", cmd)
		return 2
	}
	return 0
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
	return graalgoRunEnv(root, args, nil)
}

// graalgoRunTags invokes the backend like graalgoRun, additionally passing
// the -tags build tags to the frontend: the frontend reads them from the
// GRAALGO_BUILD_TAGS environment variable when resolving imports
// (internal/frontend/diskimport.go), and the driver applies the same tags
// to its own file selection via buildContext. Tags select files; they
// confer no privilege (unlike --test-package-dir), so the established
// environment channel is safe here. When tags is empty the environment
// is left untouched, so a user-exported GRAALGO_BUILD_TAGS still applies.
func graalgoRunTags(root string, args []string, tags string) int {
	if tags == "" {
		return graalgoRun(root, args)
	}
	return graalgoRunEnv(root, args, []string{"GRAALGO_BUILD_TAGS=" + tags})
}

// graalgoRunEnv is graalgoRun with extra environment variables set on the
// launcher child only (later entries override earlier ones).
func graalgoRunEnv(root string, args []string, extraEnv []string) int {
	runner := filepath.Join(root, "internal", "backend", "graalgo-sync-run")
	cmd := exec.Command(runner, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
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
func graalgoRunProgram(root string, args []string, tags string) int {
	code := graalgoRunTags(root, args, tags)
	if code != 0 {
		fmt.Fprintf(os.Stderr, "exit status %d\n", code)
		return 1
	}
	return 0
}

// buildContext returns the go/build context for file selection: the
// platform defaults (GOOS/GOARCH from the environment) plus the -tags
// build tags, if any. Tags are space- or comma-separated, like `go build`.
func buildContext(tags string) build.Context {
	ctxt := build.Default
	if tags != "" {
		ctxt.BuildTags = append(ctxt.BuildTags, splitTags(tags)...)
	}
	return ctxt
}

// splitTags splits a -tags value the way the go command does: on spaces,
// tabs, and commas.
func splitTags(tags string) []string {
	return strings.FieldsFunc(tags, func(r rune) bool {
		return r == ' ' || r == '\t' || r == ','
	})
}

// packageFiles returns the buildable .go files of dir under ctxt: only the
// files satisfying the platform and tag constraints, like `go build`.
// Returns an error (never a silent fallback) when the directory cannot be
// imported — including *build.NoGoError when no file is buildable, e.g.
// every file is excluded by constraints.
func packageFiles(dir string, ctxt build.Context) ([]string, error) {
	pkg, err := ctxt.ImportDir(dir, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot load package in %s: %w", dir, err)
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

// resolveFiles turns package/file arguments into a list of .go files.
// With no args, uses the buildable .go files in the current directory.
// Directories and import paths expand to their buildable .go files
// (honoring GOOS/GOARCH and //go:build constraints via ctxt, like
// `go build`); explicit .go files are used exactly as listed, like
// `go run file.go` (file lists ignore build constraints).
func resolveFiles(args []string, ctxt build.Context) ([]string, error) {
	if len(args) == 0 {
		files, err := packageFiles(".", ctxt)
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			return nil, fmt.Errorf("no buildable Go files in current directory")
		}
		return files, nil
	}
	var files []string
	for _, a := range args {
		if a == "./..." {
			return nil, fmt.Errorf("./... not yet supported")
		}
		if fi, err := os.Stat(a); err == nil {
			if fi.IsDir() {
				matches, err := packageFiles(a, ctxt)
				if err != nil {
					return nil, err
				}
				files = append(files, matches...)
				continue
			}
			files = append(files, a)
			continue
		}
		// Not a path on disk: a .go file that doesn't exist yet (typo)
		// is passed through so the backend reports the error, while
		// anything else is resolved as an import path (same module,
		// stdlib, module cache, or GOPATH) via importDirForPath.
		if filepath.Ext(a) == ".go" {
			files = append(files, a)
			continue
		}
		dir, err := resolvePackageDir(a)
		if err != nil {
			return nil, fmt.Errorf("no such file or directory: %s", a)
		}
		matches, err := packageFiles(dir, ctxt)
		if err != nil {
			return nil, err
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no Go files matched")
	}
	return files, nil
}

// resolvePackageDir maps a package argument to its source directory:
// a directory on disk as-is, otherwise an import path resolved via
// importDirForPath (same module, stdlib, module cache, GOPATH).
func resolvePackageDir(arg string) (string, error) {
	if fi, err := os.Stat(arg); err == nil && fi.IsDir() {
		return arg, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return importDirForPath(cwd, arg)
}
