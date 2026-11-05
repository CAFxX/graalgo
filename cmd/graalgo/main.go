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
	"context"
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

// runBackend invokes internal/backend/graalgo-sync-run with the given args,
// wiring stdio through, plus extraEnv appended to the inherited
// environment for this child process only. Returns the process exit code.
//
// NOTE: privilege-conferring signals (e.g. PKG-001's test-package-dir)
// travel as explicit argv flags, never environment variables: env is
// inherited by every child process, so a user-exported variable would
// confer privilege on commands that never asked for it (`graalgo run`).
// runBackendCtx invokes internal/backend/graalgo-sync-run with the given
// args, wiring stdio through, plus extraEnv appended to the inherited
// environment for this child process only. Bound to ctx: when ctx expires
// the backend process is killed and timedOut reports whether the context
// expired before the backend exited on its own. The launcher execs the
// JVM directly (see internal/backend/graalgo-sync-run), so killing the
// child kills the backend itself — no orphaned grandchildren. A backend
// that exits on its own always yields a real exit code, while a
// context-killed process surfaces as exit code -1, so a -1 together with
// an expired context is a timeout, never a backend failure.
//
// NOTE: privilege-conferring signals (e.g. PKG-001's test-package-dir)
// travel as explicit argv flags, never environment variables: env is
// inherited by every child process, so a user-exported variable would
// confer privilege on commands that never asked for it (`graalgo run`).
// Build tags are not such a signal: they grant no access, they are
// ordinary build input the user supplied on argv (-tags), and the
// backend's only existing channel for them is the GRAALGO_BUILD_TAGS
// environment variable read by the frontend's disk importer (see
// internal/frontend/diskimport.go). graalgoRunTags therefore translates
// the driver's own -tags argv into that sanctioned channel, scoped to
// this one backend child via cmd.Env — never exported to the driver's
// own environment, and never a new third channel. When -tags is absent,
// the ambient environment is passed through untouched, preserving the
// existing GRAALGO_BUILD_TAGS behavior for callers that set it
// deliberately; when -tags is present, argv wins over the ambient value
// for this invocation.
func runBackendCtx(ctx context.Context, root string, args []string, extraEnv []string) (code int, timedOut bool) {
	runner := filepath.Join(root, "internal", "backend", "graalgo-sync-run")
	cmd := exec.CommandContext(ctx, runner, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if extraEnv != nil {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), ctx.Err() != nil && ee.ExitCode() == -1
		}
		fmt.Fprintf(os.Stderr, "graalgo: %v\n", err)
		return 1, ctx.Err() != nil
	}
	return 0, false
}

func runBackend(root string, args []string, extraEnv []string) int {
	code, _ := runBackendCtx(context.Background(), root, args, extraEnv)
	return code
}

// graalgoRunCtx invokes the backend like graalgoRun but bound to ctx:
// when ctx expires the backend process is killed and timedOut reports
// whether the context expired before the backend exited on its own.
func graalgoRunCtx(ctx context.Context, root string, args []string) (code int, timedOut bool) {
	return runBackendCtx(ctx, root, args, nil)
}
// graalgoRun invokes internal/backend/graalgo-sync-run with the given args,
// wiring stdio through. Returns the process exit code.
func graalgoRun(root string, args []string) int {
	return runBackend(root, args, nil)
}

// backendTagsEnv translates a -tags value into the GRAALGO_BUILD_TAGS
// environment entry for one backend invocation, or nil when no tags
// were given. The -tags syntax accepts comma- and/or space-separated
// tags (like `go build -tags`); the frontend reads GRAALGO_BUILD_TAGS
// as space-separated, so commas are normalized to spaces here.
func backendTagsEnv(tags string) []string {
	fields := splitBuildTags(tags)
	if len(fields) == 0 {
		return nil
	}
	return []string{"GRAALGO_BUILD_TAGS=" + strings.Join(fields, " ")}
}

// graalgoRunTags is graalgoRun for a build carrying -tags: the main
// package was already selected with those tags by the caller, and this
// forwards them to the backend so dependency packages are selected
// with the same tags.
func graalgoRunTags(root, tags string, args []string) int {
	return runBackend(root, args, backendTagsEnv(tags))
}

// splitBuildTags splits a -tags value on commas and whitespace.
func splitBuildTags(tags string) []string {
	return strings.Fields(strings.ReplaceAll(tags, ",", " "))
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
	return runProgram(root, args, nil)
}

// graalgoRunProgramTags is graalgoRunProgram for a build carrying -tags.
func graalgoRunProgramTags(root, tags string, args []string) int {
	return runProgram(root, args, backendTagsEnv(tags))
}

func runProgram(root string, args []string, extraEnv []string) int {
	code := runBackend(root, args, extraEnv)
	if code != 0 {
		fmt.Fprintf(os.Stderr, "exit status %d\n", code)
		return 1
	}
	return 0
}

// resolveFilesWithTags turns package/file arguments into a list of .go
// files, applying build constraints like `go build` does for a package
// argument:
//
//   - No args, or a directory argument: the files in that directory
//     that satisfy the platform constraints and the given tags, via
//     buildableGoFilesWithTags (the same selection `graalgo test`
//     already uses). A directory whose files are all excluded by
//     constraints contributes nothing, so an all-excluded package
//     errors instead of being compiled from excluded files.
//   - An explicitly named .go file: passed through unchanged, even
//     if its own constraints would exclude it. This matches `go run`
//     / `go build` with explicit files, where listed files bypass
//     constraints (verified against gc: `go run main.go foo.go`
//     compiles foo.go without the foo tag, while `go run .` does not).
//
// _test.go files are NOT filtered here; callers that build (not test)
// drop them, like `go build`.
func resolveFilesWithTags(args []string, tags []string) ([]string, error) {
	if len(args) == 0 {
		files := buildableGoFilesWithTags(".", tags)
		if len(files) == 0 {
			return nil, fmt.Errorf("no Go files in current directory")
		}
		return files, nil
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
			files = append(files, buildableGoFilesWithTags(a, tags)...)
			continue
		}
		files = append(files, a)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no Go files matched")
	}
	return files, nil
}

// globFiles is the pre-constraint-selection file expansion, kept only
// for `graalgo fmt`: `go fmt` formats every .go file in the package
// directory regardless of build constraints (verified against gc —
// a foo-tagged file is reformatted by `go fmt` with no tags set, and
// `go fmt` rejects -tags outright), so fmt deliberately does not
// filter. Every other command uses resolveFilesWithTags.
func globFiles(args []string) ([]string, error) {
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
