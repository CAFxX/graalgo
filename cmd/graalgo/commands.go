package main

import (
	"errors"
	"fmt"
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/CAFxX/graalgo/internal/driver"
	"github.com/CAFxX/graalgo/internal/vet"
)

// cmdRun implements `graalgo run [build flags] <file.go|package> [args...]`.
//
// For a single .go file, invokes the backend directly. Program arguments
// after the file are passed through to the program.
func cmdRun(root string, args []string) int {
	// Split flags from the file/package and program args. The first
	// non-flag argument is the file or package; everything after it is
	// program arguments.
	var flagArgs []string
	var rest []string
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || len(rest) > 0 {
			rest = args[i:]
			break
		}
		flagArgs = append(flagArgs, a)
		// Consume the flag's value too, so `-ldflags -X=...` doesn't
		// leak the value into the program args.
		name, _, hasVal := strings.Cut(a, "=")
		if !hasVal && (name == "-ldflags" || name == "-o" || name == "-tags") && i+1 < len(args) {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}
	if i >= len(args) {
		rest = nil
	}
	// Validate build flags (reject unknown ones; -ldflags needs backend).
	// -tags is honored, not just parsed: the root package is filtered
	// with go/build (see resolveBuildFiles) and the tag set is passed
	// to the backend so imported packages select the same files.
	var bf buildFlags
	if len(flagArgs) > 0 {
		var err error
		bf, err = parseBuildFlags(flagArgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "graalgo run: %v\n", err)
			return 2
		}
		if bf.ldflags != "" {
			fmt.Fprintf(os.Stderr, "graalgo run: -ldflags: not yet supported\n")
			return 1
		}
		// -o is meaningless for `run` (no binary produced); reject
		// clearly rather than silently ignoring.
		if bf.output != "" {
			fmt.Fprintf(os.Stderr, "graalgo run: -o: not supported for run\n")
			return 1
		}
	}
	tags := effectiveBuildTags(bf.tags)
	backendEnv := backendTagsEnv(tags)

	if len(rest) == 0 {
		fmt.Fprintf(os.Stderr, "graalgo run: no Go files listed\n")
		return 2
	}

	target := rest[0]
	progArgs := rest[1:]

	// One or more explicit .go files: `graalgo run a.go [b.go...]`.
	// Leading .go arguments are program files (like `go run`); the rest
	// are program arguments.
	if strings.HasSuffix(target, ".go") {
		var progFiles []string
		i := 0
		for i < len(rest) && strings.HasSuffix(rest[i], ".go") {
			progFiles = append(progFiles, rest[i])
			i++
		}
		progArgs = rest[i:]
		// Program args are separated from the launcher's own flags by
		// "--" (the launcher treats every bare "-..." before "--" as
		// its own flag).
		runArgs := append(append([]string{}, progFiles...), "--")
		runArgs = append(runArgs, progArgs...)
		return graalgoRunProgramEnv(root, runArgs, backendEnv)
	}

	// Package (directory or import path): find its main package files.
	// _test.go files are excluded, like `go run` (they're not part of
	// the package build). Files are filtered by build constraints and
	// tags, so OS-gated alternatives never reach the backend together.
	files, err := resolveBuildFiles([]string{target}, tags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo run: %v\n", err)
		return 1
	}
	var pkgFiles []string
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			pkgFiles = append(pkgFiles, f)
		}
	}
	if len(pkgFiles) == 0 {
		fmt.Fprintf(os.Stderr, "graalgo run: no non-test Go files in %s\n", target)
		return 1
	}
	// The backend compiles all package files together as one unit.
	if code := graalgoRunEnv(root, append([]string{"--check"}, pkgFiles...), backendEnv); code != 0 {
		return code
	}
	runArgs := append(append([]string{}, pkgFiles...), "--")
	runArgs = append(runArgs, progArgs...)
	return graalgoRunProgramEnv(root, runArgs, backendEnv)
}

// buildFlags holds the parsed `go build` flags that the driver recognizes.
// Unrecognized flags are rejected; recognized-but-unsupported flags produce
// a clear "not yet supported" error naming the flag.
type buildFlags struct {
	output  string   // -o: output binary path
	ldflags string   // -ldflags: linker flags (forwarded; -X needs backend)
	tags    string   // -tags: build tags (forwarded to the backend; see effectiveBuildTags)
	pkgs    []string // remaining package/file args
}

// parseBuildFlags separates build flags from package args, following `go`
// flag conventions: -flag value, -flag=value, and boolean flags.
func parseBuildFlags(args []string) (buildFlags, error) {
	var bf buildFlags
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			bf.pkgs = append(bf.pkgs, a)
			continue
		}
		// Split -flag=value.
		name, val, hasVal := strings.Cut(a, "=")
		next := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag %s: missing argument", name)
			}
			i++
			return args[i], nil
		}
		switch name {
		case "-o":
			v, err := next()
			if err != nil {
				return bf, err
			}
			bf.output = v
		case "-ldflags":
			v, err := next()
			if err != nil {
				return bf, err
			}
			bf.ldflags = v
		case "-tags":
			v, err := next()
			if err != nil {
				return bf, err
			}
			bf.tags = v
		case "-a", "-x", "-work", "-n":
			// Accepted and ignored: -a (rebuild all) is a no-op without
			// a build cache; -x/-work/-n are go-command diagnostics that
			// have no meaning for the Truffle backend.
		default:
			return bf, fmt.Errorf("unknown flag %q", a)
		}
	}
	return bf, nil
}

// splitBuildTags splits a -tags value the way `go build` does: tags are
// separated by spaces and/or commas.
func splitBuildTags(s string) []string {
	return strings.Fields(strings.ReplaceAll(s, ",", " "))
}

// effectiveBuildTags is the tag set the backend must honor for this
// invocation: tags already in the environment (which the frontend reads
// for imported packages) plus the -tags flag value. The flag augments,
// never replaces, the environment, so a tag set in GRAALGO_BUILD_TAGS
// cannot be silently dropped by passing -tags. Duplicates are removed.
func effectiveBuildTags(flagTags string) []string {
	seen := make(map[string]bool)
	var tags []string
	for _, t := range append(splitBuildTags(os.Getenv("GRAALGO_BUILD_TAGS")), splitBuildTags(flagTags)...) {
		if !seen[t] {
			seen[t] = true
			tags = append(tags, t)
		}
	}
	return tags
}

// backendTagsEnv renders the effective tag set as extra environment for
// backend invocations, or nil when no tags are set (in which case the
// backend inherits the caller's environment unchanged).
func backendTagsEnv(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	return []string{"GRAALGO_BUILD_TAGS=" + strings.Join(tags, " ")}
}

// resolveBuildFiles is resolveFiles plus build-constraint filtering for
// package arguments (directories, and the no-argument current directory):
// only files go/build selects for the current platform and tag set are
// returned, so OS-gated alternatives and tag-gated files never reach the
// backend together. Explicitly listed files pass through unfiltered,
// matching `go build`/`go run` file mode, where named files are compiled
// as given regardless of their constraints. _test.go files are left for
// the callers to strip, as before.
func resolveBuildFiles(args []string, tags []string) ([]string, error) {
	files, err := resolveFiles(args)
	if err != nil {
		return nil, err
	}
	// Files named explicitly on the command line (anything that is not
	// a directory argument) pass through: resolveFiles kept them verbatim.
	explicit := make(map[string]bool, len(args))
	for _, a := range args {
		if fi, err := os.Stat(a); err != nil || !fi.IsDir() {
			explicit[a] = true
		}
	}
	// One ImportDir per directory: all files from one package share it.
	// A file reached both as a directory member and as an explicit
	// argument is emitted once, not twice.
	selected := make(map[string]map[string]bool)
	seen := make(map[string]bool)
	var out []string
	for _, f := range files {
		if seen[f] {
			continue
		}
		if explicit[f] {
			seen[f] = true
			out = append(out, f)
			continue
		}
		dir := filepath.Dir(f)
		sel, ok := selected[dir]
		if !ok {
			var err error
			sel, err = packageFiles(dir, tags)
			if err != nil {
				return nil, err
			}
			selected[dir] = sel
		}
		if sel[filepath.Base(f)] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out, nil
}

// packageFiles returns the set of base names go/build selects from dir as
// the package's non-test Go files for the current platform and tag set
// (filename GOOS/GOARCH suffixes and //go:build lines included). A
// directory with no buildable files yields an empty set, so callers
// report "no non-test Go files" instead of building the wrong variant.
// If the directory cannot be loaded for another reason (e.g. a malformed
// constraint), the unfiltered file set is returned when no tags were
// requested, letting the backend report the real error as before; when
// tags were requested, the load error is returned instead — falling back
// to the unfiltered set would silently build without honoring the tags.
func packageFiles(dir string, tags []string) (map[string]bool, error) {
	pkg, err := importDirWithTags(dir, tags)
	if err != nil {
		var noGo *build.NoGoError
		if errors.As(err, &noGo) {
			return map[string]bool{}, nil
		}
		if len(tags) > 0 {
			return nil, fmt.Errorf("cannot load package in %s: %w", dir, err)
		}
		matches, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		sel := make(map[string]bool, len(matches))
		for _, m := range matches {
			sel[filepath.Base(m)] = true
		}
		return sel, nil
	}
	sel := make(map[string]bool, len(pkg.GoFiles))
	for _, f := range pkg.GoFiles {
		sel[f] = true
	}
	return sel, nil
}

// cmdBuild implements `graalgo build [build flags] [packages]`.
//
// The named packages are validated through the frontend (the compile
// gate), then packaged into an executable artifact: a real binary that
// embeds the frontend output and runs it through the graalgo backend.
// Like `go build`, _test.go files are ignored and the artifact defaults
// to the current directory's base name (or the first .go file's base
// name) unless -o names the output.
func cmdBuild(root string, args []string) int {
	bf, err := parseBuildFlags(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo build: %v\n", err)
		return 2
	}
	if bf.output == "-" {
		fmt.Fprintf(os.Stderr, "graalgo build: -o - is not supported: the artifact is a binary\n")
		return 2
	}
	if bf.ldflags != "" {
		fmt.Fprintf(os.Stderr, "graalgo build: -ldflags: not yet supported\n")
		return 1
	}
	// -tags is honored, not just parsed: see cmdRun above.
	tags := effectiveBuildTags(bf.tags)
	backendEnv := backendTagsEnv(tags)
	files, err := resolveBuildFiles(bf.pkgs, tags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo build: %v\n", err)
		return 2
	}
	// _test.go files are not part of the package build, like `go build`.
	var pkgFiles []string
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			pkgFiles = append(pkgFiles, f)
		}
	}
	if len(pkgFiles) == 0 {
		fmt.Fprintf(os.Stderr, "graalgo build: no non-test Go files\n")
		return 1
	}
	// Frontend gate: run the frontend once, capturing its output for the
	// artifact. This both validates the program and produces the IR that
	// the artifact embeds, so the artifact never re-runs the frontend.
	wireFile, err := os.CreateTemp("", "graalgo-wire-*.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo build: cannot create temp file: %v\n", err)
		return 1
	}
	wirePath := wireFile.Name()
	wireFile.Close()
	defer os.Remove(wirePath)
	emitArgs := append([]string{"--emit-wire=" + wirePath, "--check"}, pkgFiles...)
	if code := graalgoRunEnv(root, emitArgs, backendEnv); code != 0 {
		fmt.Fprintf(os.Stderr, "graalgo build: build failed\n")
		return 1
	}
	wireJSON, err := os.ReadFile(wirePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo build: cannot read frontend output: %v\n", err)
		return 1
	}
	out, err := driver.DefaultOutputName(bf.output, bf.pkgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo build: %v\n", err)
		return 2
	}
	if err := driver.Emit(driver.Artifact{
		OutputPath:  out,
		GraalgoRoot: root,
		WireJSON:    wireJSON,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "graalgo build: %v\n", err)
		return 1
	}
	return 0
}

// cmdVet implements `graalgo vet [packages]`.
//
// Runs the frontend translation check (--check) over the named packages,
// then runs the static analyzers in internal/vet and reports diagnostics
// in the same style as `go vet`.
func cmdVet(root string, args []string) int {
	files, err := resolveFiles(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo vet: %v\n", err)
		return 1
	}
	failed := false
	for _, f := range files {
		if code := graalgoRun(root, []string{"--check", f}); code != 0 {
			failed = true
		}
	}
	if failed {
		return 1
	}
	// Run the analyzers over all files as one package.
	ds, err := vet.Run(files)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo vet: %v\n", err)
		return 1
	}
	for _, d := range ds {
		fmt.Printf("%s: %s\n", d.Pos, d.Message)
	}
	if len(ds) > 0 {
		return 1
	}
	fmt.Println("vet: no issues found")
	return 0
}

// cmdFmt implements `graalgo fmt [packages]`.
//
// Formats the Go source files using gofmt from the stock toolchain.
func cmdFmt(root string, args []string) int {
	files, err := resolveFiles(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo fmt: %v\n", err)
		return 1
	}
	gofmt := findGofmt()
	if gofmt == "" {
		fmt.Fprintf(os.Stderr, "graalgo fmt: cannot locate gofmt\n")
		return 1
	}
	cmdArgs := append([]string{"-w"}, files...)
	cmd := exec.Command(gofmt, cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "graalgo fmt: %v\n", err)
		return 1
	}
	return 0
}

// findGofmt locates the gofmt binary: GOROOT/bin/gofmt, beside the go
// binary on PATH, or ~/tools/go/bin/gofmt.
func findGofmt() string {
	if goroot := os.Getenv("GOROOT"); goroot != "" {
		if p := filepath.Join(goroot, "bin", "gofmt"); isExec(p) {
			return p
		}
	}
	if goBin, err := exec.LookPath("go"); err == nil {
		if p := filepath.Join(filepath.Dir(goBin), "gofmt"); isExec(p) {
			return p
		}
	}
	for _, home := range []string{os.Getenv("HOME"), "/root"} {
		if home == "" {
			continue
		}
		if p := filepath.Join(home, "tools", "go", "bin", "gofmt"); isExec(p) {
			return p
		}
	}
	if p, err := exec.LookPath("gofmt"); err == nil {
		return p
	}
	return ""
}

func isExec(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Mode()&0111 != 0
}
