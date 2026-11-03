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
	"unicode"
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
	return graalgoRunEnv(root, args, nil)
}

// graalgoRunEnv is graalgoRun with extra environment variables set on
// the child only (appended to the inherited environment, so a later
// entry overrides an inherited one). The driver's own environment is
// never mutated: a variable set here is visible to this one backend
// invocation and, through it, to the frontend subprocess it spawns —
// never to sibling commands or to the user's shell.
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

// tagsEnv builds the child-only environment carrying a -tags flag value
// to the frontend: internal/frontend/diskimport.go reads
// GRAALGO_BUILD_TAGS (space-separated) into its go/build context, so
// the frontend's disk imports select files under exactly the tags the
// driver was given. The value is space-joined (diskimport splits on
// whitespace, so a comma-joined value would arrive as one bogus tag).
// A user-exported GRAALGO_BUILD_TAGS is preserved and the flag's tags
// are added to it, not substituted for it. Empty tags yield no entry,
// so the inherited environment passes through untouched.
func tagsEnv(tags string) []string {
	tagList := splitTags(tags)
	if len(tagList) == 0 {
		return nil
	}
	if existing := os.Getenv("GRAALGO_BUILD_TAGS"); existing != "" {
		return []string{"GRAALGO_BUILD_TAGS=" + existing + " " + strings.Join(tagList, " ")}
	}
	return []string{"GRAALGO_BUILD_TAGS=" + strings.Join(tagList, " ")}
}

// graalgoRunWithTags runs the backend with a -tags flag value forwarded
// to the frontend via tagsEnv, so disk imports honor the tags.
func graalgoRunWithTags(root string, args []string, tags string) int {
	return graalgoRunEnv(root, args, tagsEnv(tags))
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
	return graalgoRunProgramEnv(root, args, nil)
}

// graalgoRunProgramWithTags is graalgoRunProgram with a -tags flag value
// forwarded to the frontend (see tagsEnv). The program invocation
// re-runs the frontend over its files and imports, so it needs the
// tags exactly like the --check gate does.
func graalgoRunProgramWithTags(root string, args []string, tags string) int {
	return graalgoRunProgramEnv(root, args, tagsEnv(tags))
}

func graalgoRunProgramEnv(root string, args []string, extraEnv []string) int {
	code := graalgoRunEnv(root, args, extraEnv)
	if code != 0 {
		fmt.Fprintf(os.Stderr, "exit status %d\n", code)
		return 1
	}
	return 0
}

// splitTags splits a -tags flag value the way `go` does: tags are
// separated by commas and/or whitespace ("-tags tag,list").
func splitTags(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
}

// filterBuildableFiles drops files whose build constraints are not
// satisfied under the given -tags value, like `go build -tags`. It is
// the mechanism by which `graalgo build -tags` and `graalgo run -tags`
// honor build tags on the main-package path: file selection is the only
// place the driver interprets constraints for the files it names
// explicitly (the backend compiles exactly the file list it is given,
// so excluding a file here excludes it from the build). The frontend's
// disk imports are covered separately, by tagsEnv.
//
// Even with empty tags this is not the identity: platform-default
// constraints (GOOS/GOARCH, filename suffixes, //go:build lines) still
// apply, exactly as in `go build` with no -tags — a directory build
// that globbed every *.go file would otherwise compile a foo-gated
// file and its !foo sibling together. Files are grouped by directory
// and each directory is resolved through buildableGoFilesWithTags
// (go/build constraint evaluation). A file the build package cannot
// account for (e.g. an explicitly named file that does not exist yet)
// is kept, so the backend reports the real error — exactly as the
// no-tags path does. Callers that share resolveFiles (notably
// `graalgo vet`) consume this same helper, or resolveFilesWithTags,
// rather than re-implementing filtering.
//
// Ordering and duplicates of the input list are preserved.
func filterBuildableFiles(files []string, tags string) []string {
	tagList := splitTags(tags)
	dirs := make([]string, 0, len(files))
	seen := make(map[string]bool)
	for _, f := range files {
		d := filepath.Dir(f)
		if !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	keep := make(map[string]bool)
	for _, d := range dirs {
		for _, b := range buildableGoFilesWithTags(d, tagList) {
			if abs, err := filepath.Abs(b); err == nil {
				keep[abs] = true
			}
		}
	}
	var out []string
	for _, f := range files {
		abs, err := filepath.Abs(f)
		if err != nil {
			out = append(out, f) // unreachable in practice; keep defensively
			continue
		}
		if keep[abs] {
			out = append(out, f)
			continue
		}
		if _, serr := os.Stat(abs); os.IsNotExist(serr) {
			// Not on disk (e.g. a typo'd explicit file): keep it so
			// the backend reports the real error, like the no-tags path.
			out = append(out, f)
		}
		// Otherwise the file is excluded by build constraints: drop it.
	}
	return out
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

// resolveFilesWithTags is resolveFiles with go/build constraint
// filtering applied to directory expansion (including the no-argument
// current-directory case), like `go build -tags` / `go run -tags`.
// It is the shared helper the tags-aware commands (build, run, and —
// once it parses tags — vet) build on, so the filtering lives in one
// place instead of being re-implemented per command.
//
// Semantics follow gc exactly, and the two cases differ deliberately:
//   - Directories expand to their buildable files only: a file whose
//     constraints fail (under the platform defaults plus -tags) is not
//     part of the package and is dropped.
//   - Explicitly named .go files pass through unfiltered, even when
//     their constraints fail: gc compiles named files regardless of
//     constraints (`go run win.go` builds on linux), and a nonexistent
//     named file is likewise passed through so the backend reports
//     the real error.
//
// Empty-result contract differs from resolveFiles on purpose: when
// directories contained files but filtering removed all of them, the
// result is an empty list with a nil error, so the caller can report
// the tags-aware diagnostic ("-tags=%q excluded all files") instead
// of a generic "no Go files matched". Truly empty directories still
// yield the "no Go files matched" error, as in resolveFiles.
func resolveFilesWithTags(args []string, tags string) ([]string, error) {
	if len(args) == 0 {
		matches, err := filepath.Glob("*.go")
		if err != nil {
			return nil, err
		}
		filtered := filterBuildableFiles(matches, tags)
		if len(filtered) == 0 && len(matches) > 0 {
			return nil, nil
		}
		if len(filtered) == 0 {
			return nil, fmt.Errorf("no Go files in current directory")
		}
		return filtered, nil
	}
	var files []string
	sawRaw := false
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
			if len(matches) > 0 {
				sawRaw = true
			}
			files = append(files, filterBuildableFiles(matches, tags)...)
			continue
		}
		// An explicitly named file: pass through unfiltered (see above).
		files = append(files, a)
	}
	if len(files) == 0 && sawRaw {
		return nil, nil
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no Go files matched")
	}
	return files, nil
}
