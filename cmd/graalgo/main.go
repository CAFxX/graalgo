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
	"syscall"
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

// backendResult is the outcome of one backend invocation. The backend's
// exit code is not the whole story: a signal death (ExitCode() == -1)
// and a launch failure are distinguished, because `go run` renders a
// signal death as "signal: <name>" rather than "exit status -1".
type backendResult struct {
	code     int       // process exit code; -1 if it didn't exit
	sig      os.Signal // non-nil if the process died from a signal
	startErr error     // non-nil if the backend couldn't be launched
}

// backendRunnerOverride replaces the resolved graalgo-sync-run path when
// set. Test seam only: the driver unit tests point it at a fake backend
// script that implements the launcher contract (sentinel iff instructed,
// arbitrary exit codes, signal death on demand).
var backendRunnerOverride string

// runBackend is the single exec choke point for backend invocations: it
// invokes internal/backend/graalgo-sync-run with the given args, wiring
// stdio through.
func runBackend(root string, args []string) backendResult {
	runner := filepath.Join(root, "internal", "backend", "graalgo-sync-run")
	if backendRunnerOverride != "" {
		runner = backendRunnerOverride
	}
	cmd := exec.Command(runner, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	var r backendResult
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			r.code = ee.ExitCode()
			// syscall.WaitStatus is unix-only; graalgo targets
			// Linux/GraalVM (consistent with the rest of the backend).
			if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				r.sig = ws.Signal()
			}
			return r
		}
		r.startErr = err
		return r
	}
	r.code = 0
	return r
}

// graalgoRun invokes internal/backend/graalgo-sync-run with the given args,
// wiring stdio through. Returns the process exit code.
//
// A signal death or a start failure returns 1 rather than leaking Go's
// -1/err conventions: downstream callers only test `!= 0`.
//
// NOTE: privilege-conferring signals (e.g. PKG-001's test-package-dir)
// travel as explicit argv flags, never environment variables: env is
// inherited by every child process, so a user-exported variable would
// confer privilege on commands that never asked for it (`graalgo run`).
func graalgoRun(root string, args []string) int {
	r := runBackend(root, args)
	if r.startErr != nil {
		fmt.Fprintf(os.Stderr, "graalgo: %v\n", r.startErr)
		return 1
	}
	if r.sig != nil {
		return 1
	}
	return r.code
}

// graalgoRunProgram invokes the backend for `graalgo run` and emulates
// `go run`'s exit contract: when the program exits non-zero, `go run`
// prints "exit status N" to stderr and exits 1 (rather than propagating
// the raw code).
//
// The backend's own failures are disambiguated from program outcomes via
// an out-of-band sentinel file (fix-p1-run-exitcode): the driver creates
// a private temp dir and passes --status-file=<path> to the backend,
// which writes a status document only on its own failure paths. File
// presence (with a non-zero exit) means "the backend itself failed" —
// the backend's stderr already carries the message, so the driver stays
// silent and returns 1, exactly like `go run` on a build failure.
// Absence means the exit code/signal is the program's outcome.
//
// A backend killed by a signal is rendered as "signal: <name>"
// (syscall.SIGKILL.String() == "killed", byte-identical to `go run`);
// the old code printed "exit status -1".
//
// Degraded-but-safe fallbacks: if the temp dir cannot be created, the
// driver warns and runs without the flag (legacy behavior); if the
// sentinel write fails inside the backend, the driver falls back to
// "exit status N". Behavior is never worse than before the change.
func graalgoRunProgram(root string, args []string) int {
	// Step 1: private temp dir for the status sentinel.
	statusDir, err := os.MkdirTemp("", "graalgo-run-*")
	statusFile := ""
	if err != nil {
		// E4: degraded but safe — legacy behavior (no flag).
		fmt.Fprintf(os.Stderr, "graalgo: warning: cannot create status dir: %v\n", err)
	} else {
		defer os.RemoveAll(statusDir)
		statusFile = filepath.Join(statusDir, "backend-status.json")
		// Step 2: the flag travels as explicit argv (PKG-001 precedent),
		// prepended so it precedes every launcher flag and, crucially,
		// precedes "--" — the launcher strips it before "--", so it never
		// reaches the guest's os.Args.
		args = append([]string{"--status-file=" + statusFile}, args...)
	}

	r := runBackend(root, args)

	// Step 3: start failure (unchanged from the old behavior).
	if r.startErr != nil {
		fmt.Fprintf(os.Stderr, "graalgo: %v\n", r.startErr)
		return 1
	}
	// Step 4: signal death — `go run` prints "signal: <name>", no
	// "exit status" line, rc 1.
	if r.sig != nil {
		fmt.Fprintf(os.Stderr, "signal: %s\n", r.sig)
		return 1
	}
	// Step 5: the backend's own failure (sentinel present). Honored only
	// when the exit code is non-zero: a sentinel with code 0 is
	// impossible by construction and is ignored defensively.
	if r.code != 0 && statusFile != "" {
		if _, serr := os.Stat(statusFile); serr == nil {
			return 1
		}
	}
	// Step 6: program outcome — gc-exact for program exits, incl.
	// os.Exit(N), uncaught panics, and deadlocks.
	if r.code != 0 {
		fmt.Fprintf(os.Stderr, "exit status %d\n", r.code)
		return 1
	}
	// Step 7.
	return 0
}

// resolveFiles turns package/file arguments into a list of .go files.
// With no args, uses *.go in the current directory. Directories are
// expanded to their *.go files (non-recursive, like a simple package).
func resolveFiles(args []string) ([]string, error) {
	if len(args) == 0 {
		matches, err := filepath.Glob("*.go")
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
			return nil, fmt.Errorf("no such file or directory: %s", a)
		}
		if fi.IsDir() {
			matches, err := filepath.Glob(filepath.Join(a, "*.go"))
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
