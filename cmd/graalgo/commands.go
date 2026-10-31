package main

import (
	"fmt"
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
	if len(flagArgs) > 0 {
		bf, err := parseBuildFlags(flagArgs)
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
		// Check phase first: a backend failure here (exit 1/2) is a
		// driver failure (exit 1, no "exit status" line), which is
		// what lets graalgoRunProgram treat a run-phase non-zero
		// code as the program's own exit. See the convention comment
		// on graalgoRunProgram.
		if code := graalgoRun(root, append([]string{"--check"}, progFiles...)); code != 0 {
			return 1
		}
		// Program args are separated from the launcher's own flags by
		// "--" (the launcher treats every bare "-..." before "--" as
		// its own flag).
		runArgs := append(append([]string{}, progFiles...), "--")
		runArgs = append(runArgs, progArgs...)
		return graalgoRunProgram(root, runArgs)
	}

	// Package (directory or import path): find its main package files.
	// _test.go files are excluded, like `go run` (they're not part of
	// the package build). The target must resolve to exactly one
	// package: `run` takes a single program, so a ./... pattern that
	// expands to several packages is rejected, not merged.
	pkgs, err := resolvePackages([]string{target})
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo run: %v\n", err)
		return 1
	}
	if len(pkgs) != 1 {
		fmt.Fprintf(os.Stderr, "graalgo run: %s resolves to %d packages; run takes a single package\n", target, len(pkgs))
		return 1
	}
	var pkgFiles []string
	for _, f := range pkgs[0].files {
		if !strings.HasSuffix(f, "_test.go") {
			pkgFiles = append(pkgFiles, f)
		}
	}
	if len(pkgFiles) == 0 {
		fmt.Fprintf(os.Stderr, "graalgo run: no non-test Go files in %s\n", target)
		return 1
	}
	// The backend compiles all package files together as one unit.
	// Check-phase failure maps to driver exit 1 (see above).
	if code := graalgoRun(root, append([]string{"--check"}, pkgFiles...)); code != 0 {
		return 1
	}
	runArgs := append(append([]string{}, pkgFiles...), "--")
	runArgs = append(runArgs, progArgs...)
	return graalgoRunProgram(root, runArgs)
}

// buildFlags holds the parsed `go build` flags that the driver recognizes.
// Unrecognized flags are rejected; recognized-but-unsupported flags produce
// a clear "not yet supported" error naming the flag.
type buildFlags struct {
	output  string   // -o: output binary path
	ldflags string   // -ldflags: linker flags (forwarded; -X needs backend)
	tags    string   // -tags: build tags (accepted; backend honors via go/build)
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

// cmdBuild implements `graalgo build [build flags] [packages]`.
//
// Each named package is validated through the frontend (the compile
// gate) and packaged into its own executable artifact: a real binary
// that embeds the frontend output and runs it through the graalgo
// backend. Packages are never merged into one compile unit —
// `graalgo build ./a ./b` builds a and b separately, each with its own
// artifact, because merging them would compile a Franken-package and
// leave -o ambiguous. Like `go build`, _test.go files are ignored and
// the artifact defaults to the package directory's base name (or the
// first .go file's base name) unless -o names the output. With
// multiple packages, -o must name an existing directory, and each
// package's artifact lands in it under its default name, like
// `go build`; -o naming anything else with multiple packages is
// rejected with exit 1.
//
// INTENTIONAL DIVERGENCE from `go build`: `go build ./a ./b` with
// no -o writes nothing (multiple packages are compiled only as a
// check), while graalgo emits one artifact per package. As a
// consequence, without -o artifacts land in the current directory,
// and two packages sharing a base name silently overwrite each other
// (last wins) instead of producing nothing; the same last-wins
// collision applies inside an -o directory. This is the price of
// per-package emission, documented here rather than hidden.
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
	pkgs, err := resolvePackages(bf.pkgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo build: %v\n", err)
		return 2
	}
	if len(pkgs) > 1 && bf.output != "" {
		// Like `go build`: -o with multiple packages must name an
		// existing directory; anything else is exit 1, not 2 (it is
		// a build error, not a usage error), before any backend call.
		if fi, err := os.Stat(bf.output); err != nil || !fi.IsDir() {
			fmt.Fprintf(os.Stderr, "graalgo build: cannot write multiple packages to non-directory %s\n", bf.output)
			return 1
		}
	}
	for _, pkg := range pkgs {
		// _test.go files are not part of the package build, like
		// `go build`.
		var pkgFiles []string
		for _, f := range pkg.files {
			if !strings.HasSuffix(f, "_test.go") {
				pkgFiles = append(pkgFiles, f)
			}
		}
		if len(pkgFiles) == 0 {
			// Only a pattern-expanded package with test files only
			// is skipped: an explicitly named test-only package
			// errors, like `go build`, even in a multi-build.
			if pkg.expanded {
				continue
			}
			fmt.Fprintf(os.Stderr, "graalgo build: no non-test Go files\n")
			return 1
		}
		out := bf.output
		if len(pkgs) > 1 && out != "" {
			// -o names an existing directory (checked above): each
			// package's artifact lands in it under its default name.
			name, err := driver.DefaultOutputName("", []string{pkg.label})
			if err != nil {
				fmt.Fprintf(os.Stderr, "graalgo build: %v\n", err)
				return 2
			}
			out = filepath.Join(out, name)
		}
		if code := buildOnePackage(root, out, pkg, pkgFiles); code != 0 {
			return code
		}
	}
	return 0
}

// buildOnePackage runs the frontend gate for one package and emits its
// artifact. The gate runs once per package over that package's files
// only, capturing its output for the artifact, so the artifact never
// re-runs the frontend.
func buildOnePackage(root, output string, pkg resolvedPackage, pkgFiles []string) int {
	out, err := driver.DefaultOutputName(output, []string{pkg.label})
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo build: %v\n", err)
		return 2
	}
	// Emit compiles the launcher in its own temp dir, so a relative
	// output path must be anchored to the caller's directory first —
	// otherwise the artifact lands in the temp dir and is deleted with
	// it, and `graalgo build` silently produces nothing.
	if abs, err := filepath.Abs(out); err == nil {
		out = abs
	} else {
		fmt.Fprintf(os.Stderr, "graalgo build: %v\n", err)
		return 2
	}
	// Fail fast when the output path is an existing directory, like
	// `go build` ("build output %q already exists and is a
	// directory"): without this, the launcher's `go build -o` treats
	// the directory as an output directory and silently writes a
	// binary of an unrelated name inside it.
	if fi, err := os.Stat(out); err == nil && fi.IsDir() {
		fmt.Fprintf(os.Stderr, "graalgo build: build output %q already exists and is a directory\n", out)
		return 1
	}
	wireFile, err := os.CreateTemp("", "graalgo-wire-*.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo build: cannot create temp file: %v\n", err)
		return 1
	}
	wirePath := wireFile.Name()
	wireFile.Close()
	defer os.Remove(wirePath)
	emitArgs := append([]string{"--emit-wire=" + wirePath, "--check"}, pkgFiles...)
	if code := graalgoRun(root, emitArgs); code != 0 {
		fmt.Fprintf(os.Stderr, "graalgo build: build failed: %s\n", pkg.label)
		return 1
	}
	wireJSON, err := os.ReadFile(wirePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo build: cannot read frontend output: %v\n", err)
		return 1
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
// For each named package, runs the frontend translation check (--check)
// once over the package's whole file set — cross-file references are
// checked as a unit, not file by file — and then runs the static
// analyzers in internal/vet over the same set regardless of the check
// outcome (a failing check must not suppress analyzer diagnostics).
// Diagnostics go to stderr, like `go vet`; only the "no issues found"
// summary goes to stdout.
func cmdVet(root string, args []string) int {
	pkgs, err := resolvePackages(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo vet: %v\n", err)
		return 1
	}
	failed := false
	for _, pkg := range pkgs {
		if code := graalgoRun(root, append([]string{"--check"}, pkg.files...)); code != 0 {
			failed = true
		}
		// Analyzers run regardless of the check outcome, over the
		// whole package file set as one unit.
		ds, err := vet.Run(pkg.files)
		if err != nil {
			fmt.Fprintf(os.Stderr, "graalgo vet: %v\n", err)
			failed = true
			continue
		}
		for _, d := range ds {
			fmt.Fprintf(os.Stderr, "%s: %s\n", d.Pos, d.Message)
		}
		if len(ds) > 0 {
			failed = true
		}
	}
	if failed {
		return 1
	}
	fmt.Println("vet: no issues found")
	return 0
}

// cmdFmt implements `graalgo fmt [packages]`.
//
// Formats the Go source files using gofmt from the stock toolchain.
// The file list is unfiltered by build constraints (see
// resolveFmtFiles): gofmt formats every .go file, gated or not.
func cmdFmt(root string, args []string) int {
	files, err := resolveFmtFiles(args)
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
