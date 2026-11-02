package main

import (
	"fmt"
	"go/parser"
	"go/token"
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
	var tags []string
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
		// -tags is honored, not merely accepted: the driver selects
		// the package's files with it and forwards it to the backend
		// for imported packages (graalgoRunWithTags).
		tags = splitTags(bf.tags)
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
		progArgs = stripProgSeparator(rest[i:])
		// Program args are separated from the launcher's own flags by
		// "--" (the launcher treats every bare "-..." before "--" as
		// its own flag).
		runArgs := append(append([]string{}, progFiles...), "--")
		runArgs = append(runArgs, progArgs...)
		return graalgoRunProgramWithTags(root, runArgs, tags)
	}

	// Package (directory or import path): find its main package files.
	// _test.go files are excluded, like `go run` (they're not part of
	// the package build).
	files, err := resolveFilesTags([]string{target}, tags)
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
	if code := graalgoRunWithTags(root, append([]string{"--check"}, pkgFiles...), tags); code != 0 {
		return code
	}
	progArgs = stripProgSeparator(progArgs)
	runArgs := append(append([]string{}, pkgFiles...), "--")
	runArgs = append(runArgs, progArgs...)
	return graalgoRunProgramWithTags(root, runArgs, tags)
}

// stripProgSeparator consumes one leading "--" from the program
// arguments, as `go run` does: `graalgo run main.go -- -x` passes
// "-x" (not "--" then "-x") to the program. The driver adds its own
// "--" separator when invoking the backend, so a forwarded "--" would
// otherwise reach the program as a spurious extra argument.
func stripProgSeparator(args []string) []string {
	if len(args) > 0 && args[0] == "--" {
		return args[1:]
	}
	return args
}

// buildFlags holds the parsed `go build` flags that the driver recognizes.
// Unrecognized flags are rejected; recognized-but-unsupported flags produce
// a clear "not yet supported" error naming the flag.
type buildFlags struct {
	output  string   // -o: output binary path
	ldflags string   // -ldflags: linker flags (forwarded; -X needs backend)
	tags    string   // -tags: build tags (driver selects files with them; forwarded to the backend for imports via GRAALGO_BUILD_TAGS)
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
	tags := splitTags(bf.tags)
	files, err := resolveFilesTags(bf.pkgs, tags)
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
	if code := graalgoRunWithTags(root, emitArgs, tags); code != 0 {
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

// cmdVet implements `graalgo vet [build flags] [packages]`.
//
// Runs the frontend translation check (--check) over the named packages,
// then runs the static analyzers in internal/vet and reports diagnostics
// in the same style as `go vet`.
//
// Each package is checked as a whole, not file by file: a multi-file
// package checked one file at a time spuriously fails on symbols
// declared in sibling files. And files are grouped by their declared
// package before both --check and vet.Run, so a package's internal
// test files (package foo) are checked with it while its external
// test files (package foo_test) are checked as a separate package —
// go/types cannot check foo and foo_test files as one package, and
// feeding them together yields spurious diagnostics from the broken
// type information.
func cmdVet(root string, args []string) int {
	bf, err := parseBuildFlags(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo vet: %v\n", err)
		return 2
	}
	tags := splitTags(bf.tags)
	files, err := resolveFilesTags(bf.pkgs, tags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo vet: %v\n", err)
		return 1
	}
	groups := partitionByPackage(files)
	failed := false
	for _, group := range groups {
		if code := graalgoRunWithTags(root, append([]string{"--check"}, group...), tags); code != 0 {
			failed = true
		}
	}
	if failed {
		return 1
	}
	// Run the analyzers over each package group separately.
	var all []vet.Diagnostic
	for _, group := range groups {
		ds, err := vet.Run(group)
		if err != nil {
			fmt.Fprintf(os.Stderr, "graalgo vet: %v\n", err)
			return 1
		}
		all = append(all, ds...)
	}
	all = vet.SortDiagnostics(all)
	for _, d := range all {
		fmt.Printf("%s: %s\n", d.Pos, d.Message)
	}
	if len(all) > 0 {
		return 1
	}
	fmt.Println("vet: no issues found")
	return 0
}

// partitionByPackage groups files by (directory, declared package
// name), preserving first-appearance order of groups and files within
// a group. Files whose package clause cannot be parsed (missing file,
// syntax error) group under the empty package name for their
// directory; the --check pass then reports the real error.
func partitionByPackage(files []string) [][]string {
	fset := token.NewFileSet()
	type key struct {
		dir string
		pkg string
	}
	var order []key
	groups := map[key][]string{}
	for _, f := range files {
		pkgName := ""
		if parsed, err := parser.ParseFile(fset, f, nil, parser.PackageClauseOnly); err == nil {
			pkgName = parsed.Name.Name
		}
		k := key{dir: filepath.Dir(f), pkg: pkgName}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], f)
	}
	var out [][]string
	for _, k := range order {
		out = append(out, groups[k])
	}
	return out
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
