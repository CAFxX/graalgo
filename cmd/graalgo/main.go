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
)

const version = "0.4.0"

func main() {
	os.Exit(runMain(os.Args[1:]))
}

// runMain implements the command dispatch, returning the exit code
// instead of exiting so dispatch of missing/unknown commands is unit
// testable. Help and version are handled before requiring an
// installation, exactly as before.
func runMain(args []string) int {
	if len(args) < 1 {
		usage()
		return 2
	}
	cmd, rest := args[0], args[1:]

	// Handle help and version before requiring installation
	switch cmd {
	case "help", "-h", "--help":
		if len(rest) > 0 {
			helpFor(rest[0])
		} else {
			usage()
		}
		return 0
	case "version", "-v", "--version":
		fmt.Printf("graalgo version %s\n", version)
		return 0
	}

	root, err := findGraalgoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo: %v\n", err)
		return 1
	}

	return dispatchCommand(root, cmd, rest)
}

// dispatchCommand routes one command (after help/version and root
// discovery) to its implementation. Unknown commands exit 2, like the
// go command's usage error.
func dispatchCommand(root, cmd string, args []string) int {
	switch cmd {
	case "run":
		return cmdRun(root, args)
	case "build":
		return cmdBuild(root, args)
	case "test":
		return cmdTest(root, args)
	case "vet":
		return cmdVet(root, args)
	case "fmt":
		return cmdFmt(root, args)
	default:
		fmt.Fprintf(os.Stderr, "graalgo: unknown command %q\n", cmd)
		fmt.Fprintf(os.Stderr, "Run 'graalgo help' for usage.\n")
		return 2
	}
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

// resolveFiles turns package/file arguments into a list of .go files,
// honoring build constraints: directories are expanded with
// buildableGoFiles (go/build constraint evaluation — GOOS/GOARCH
// filename suffixes and //go:build lines), the same file set `go build`
// would use. A bare *.go glob here used to inline build-tag-gated
// alternatives together (e.g. foo_linux.go + foo_darwin.go), which
// redeclare each other's symbols. Explicitly listed files are passed
// through unfiltered, like `go run file.go`: naming a file opts into it
// regardless of constraints. _test.go files are included in the list;
// run/build filter them out, vet groups them by package clause, and fmt
// uses resolveAllFiles instead (see below).
func resolveFiles(args []string) ([]string, error) {
	return expandFileArgs(args, buildableGoFiles)
}

// resolveAllFiles is resolveFiles without constraint filtering: every
// *.go file in a directory, including build-excluded ones. Formatting
// is not compilation — `gofmt` formats files for every platform — so
// cmdFmt uses this instead of resolveFiles; using the constrained list
// there would silently skip formatting files for other platforms.
func resolveAllFiles(args []string) ([]string, error) {
	return expandFileArgs(args, func(dir string) []string {
		matches, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		return matches
	})
}

// expandFileArgs is the shared argument expansion for resolveFiles and
// resolveAllFiles: with no args, the current directory; directories are
// expanded by expandDir (non-recursive, like a simple package).
func expandFileArgs(args []string, expandDir func(string) []string) ([]string, error) {
	if len(args) == 0 {
		matches := expandDir(".")
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
			return nil, fmt.Errorf("no such file or directory: %s", a)
		}
		if fi.IsDir() {
			files = append(files, expandDir(a)...)
			continue
		}
		files = append(files, a)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no Go files matched")
	}
	return files, nil
}
