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
	"strconv"
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

// exitKind is the backend's self-reported outcome category, delivered
// out-of-band via --exit-report. The backend's exit codes 1 and 2 are
// ambiguous at this level (input/build failure vs the program's own
// os.Exit(1)/panic), and any 0-255 code is a legitimate program exit, so
// no exit-code range can be reserved for backend failures. Only the
// backend knows which side of the ambiguity a run fell on.
type exitKind int

const (
	// exitUnknown: no (or unreadable) report — the backend died before
	// reporting (crash, signal). Treated as a backend failure: exit 1
	// with no "exit status" wrapper, the safe default.
	exitUnknown exitKind = iota
	// exitProgram: the Go program ran and exited (any code, including
	// panic, which surfaces as 2 like gc).
	exitProgram
	// exitBackendFailure: the backend failed on its own (usage/input
	// error, build failure, internal error); its diagnostics are already
	// on stderr.
	exitBackendFailure
)

// runOutcome is the fully-observed result of one backend invocation.
type runOutcome struct {
	waitCode int      // process exit code; -1 if killed by a signal or never started
	signaled bool     // the backend died from a signal
	sigText  string   // e.g. "signal: terminated" (only when signaled)
	kind     exitKind // the backend's --exit-report verdict
	startErr error    // non-nil when the backend process never started
}

// runBackend invokes the backend launcher like graalgoRun, but captures the
// full outcome. When reportPath != "", it is passed as --exit-report= (as a
// launcher flag, ahead of any "--" program-argv separator) and the verdict
// file is parsed after the process exits.
func runBackend(root string, args []string, reportPath string) runOutcome {
	var o runOutcome
	fullArgs := args
	if reportPath != "" {
		fullArgs = append([]string{"--exit-report=" + reportPath}, args...)
	}
	runner := filepath.Join(root, "internal", "backend", "graalgo-sync-run")
	cmd := exec.Command(runner, fullArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			o.waitCode = ee.ExitCode()
			// Go reports -1 when the process was killed by a signal.
			if o.waitCode == -1 {
				o.signaled = true
				o.sigText = ee.Error() // "signal: terminated"
			}
		} else {
			o.startErr = err
			o.waitCode = -1
		}
	}
	if reportPath != "" {
		o.kind = parseExitReport(reportPath)
	}
	return o
}

// parseExitReport reads the backend's --exit-report verdict file, one line
// of the form "program-exit <code>" or "backend-failure <code>". Anything
// missing or malformed yields exitUnknown.
func parseExitReport(path string) exitKind {
	data, err := os.ReadFile(path)
	if err != nil {
		return exitUnknown
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		return exitUnknown
	}
	if _, err := strconv.Atoi(fields[1]); err != nil {
		return exitUnknown
	}
	switch fields[0] {
	case "program-exit":
		return exitProgram
	case "backend-failure":
		return exitBackendFailure
	default:
		return exitUnknown
	}
}

// decideRunExit maps a finished backend invocation to `graalgo run`'s
// stderr trailer and exit code, mirroring `go run` byte-for-byte:
//
//   - program exit 0 → silent, exit 0
//   - program exit n≠0 (including panic, which is 2 like gc) →
//     "exit status n" on stderr, exit 1
//   - backend killed by a signal → "signal: <name>" on stderr, exit 1
//     (exactly what `go run` prints for its child)
//   - the backend's own failure, or an unreported outcome (crash before
//     the verdict was written) → the backend's diagnostics are already on
//     stderr; exit 1 with no wrapper line
func decideRunExit(o runOutcome) (stderrTrailer string, exitCode int) {
	if o.startErr != nil {
		return fmt.Sprintf("graalgo: %v\n", o.startErr), 1
	}
	switch {
	case o.signaled:
		return o.sigText + "\n", 1
	case o.waitCode == 0:
		return "", 0
	case o.kind == exitProgram:
		return fmt.Sprintf("exit status %d\n", o.waitCode), 1
	default:
		return "", 1
	}
}

// graalgoRunProgram invokes the backend for `graalgo run` and applies
// `go run`'s exit contract (see decideRunExit). The backend reports whether
// the Go program actually ran via --exit-report, so a backend build failure
// (exit 2) is never mistaken for a program exit the way the old
// exit-code-only check did.
func graalgoRunProgram(root string, args []string) int {
	// Like `go run`: interrupt/quit go to the backend, whose signal
	// death is then reported as "signal: <name>" with exit 1.
	catchDriverSignals()

	report, err := os.CreateTemp("", "graalgo-exit-report-*.txt")
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo run: cannot create exit-report file: %v\n", err)
		return 1
	}
	reportPath := report.Name()
	report.Close()
	defer os.Remove(reportPath)

	trailer, code := decideRunExit(runBackend(root, args, reportPath))
	if trailer != "" {
		fmt.Fprint(os.Stderr, trailer)
	}
	return code
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
