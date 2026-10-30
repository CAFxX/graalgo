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

Flags:
	-tags=tags	build tags (top-level package file selection)
	-ldflags=flags	linker flags (not yet supported)
	-o file		not supported for run (no binary produced)
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
	-tags=tags	build tags (top-level package file selection)
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
	-tags=tags	build tags (top-level package file selection)
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

// buildContextWithTags returns a go/build context honouring the given
// -tags value (space- and/or comma-separated, like `go build -tags`).
// An empty tags string returns build.Default unchanged.
func buildContextWithTags(tags string) build.Context {
	ctxt := build.Default
	if tags != "" {
		ctxt.BuildTags = strings.FieldsFunc(tags, func(r rune) bool {
			return r == ' ' || r == ',' || r == '\t'
		})
	}
	return ctxt
}

// expandDirFiles returns the .go files of the package in dir that satisfy
// the build constraints for the given tags: the package's own files plus
// its internal and external test files, mirroring `go build`'s file set.
// When the directory cannot be imported by go/build (no package clause,
// mixed packages, no module context), it falls back to every *.go file
// in the directory and lets the backend report the real error, preserving
// the previous behaviour for such directories.
func expandDirFiles(dir, tags string) ([]string, error) {
	ctxt := buildContextWithTags(tags)
	if pkg, err := ctxt.ImportDir(dir, 0); err == nil {
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
	matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	return matches, nil
}

// resolveFiles turns package/file arguments into a list of .go files.
// With no args, uses *.go in the current directory. Directories are
// expanded to their *.go files (non-recursive, like a simple package).
// Arguments that are neither files nor directories are resolved as
// import paths — same-module packages, the standard library, the module
// cache, and GOPATH/src — via importDirForPath, so `graalgo build fmt`
// and `graalgo build example.com/x` work instead of failing with
// "no such file or directory".
func resolveFiles(args []string) ([]string, error) {
	return resolveFilesTags(args, "")
}

// resolveFilesTags is resolveFiles with -tags applied to directory
// expansion: files excluded by the tags' build constraints are not
// returned, so a tagged build compiles the tagged file set rather than
// silently compiling every *.go file. Explicit .go file arguments are
// passed through unchanged (as with `go build` file arguments, which
// ignore build constraints).
func resolveFilesTags(args []string, tags string) ([]string, error) {
	if len(args) == 0 {
		matches, err := expandDirFiles(".", tags)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no Go files in current directory")
		}
		return matches, nil
	}
	var files []string
	for _, a := range args {
		if a == "./..." {
			return nil, fmt.Errorf("./... not yet supported")
		}
		fi, err := os.Stat(a)
		if err != nil {
			// Maybe a .go file that doesn't exist yet (typo) — let
			// the backend report the error.
			if filepath.Ext(a) == ".go" {
				files = append(files, a)
				continue
			}
			// Maybe an import path: resolve it to a directory and
			// expand that directory like any other.
			if dir, err := importDirForPath(".", a); err == nil {
				matches, err := expandDirFiles(dir, tags)
				if err != nil {
					return nil, err
				}
				files = append(files, matches...)
				continue
			}
			return nil, fmt.Errorf("no such file or directory: %s", a)
		}
		if fi.IsDir() {
			matches, err := expandDirFiles(a, tags)
			if err != nil {
				return nil, err
			}
			files = append(files, matches...)
			continue
		}
		files = append(files, a)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no Go files matched")
	}
	return files, nil
}
