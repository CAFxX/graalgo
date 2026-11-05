package main

import (
	"fmt"
	"go/build"
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
	// -tags is honored: it filters the package's files via go/build and
	// is forwarded to the frontend for import resolution.
	var tags string
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
		tags = bf.tags
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
		// Program args are separated from the launcher's own flags by
		// "--" (the launcher treats every bare "-..." before "--" as
		// its own flag).
		runArgs := append(append([]string{}, progFiles...), "--")
		runArgs = append(runArgs, progArgs...)
		return graalgoRunProgram(root, runArgs, tags)
	}

	// Package (directory or import path): find its main package files.
	// _test.go files are excluded, like `go run` (they're not part of
	// the package build). File selection honors GOOS/GOARCH and -tags
	// via go/build, so platform-alternative files are never compiled
	// together.
	files, err := resolveFiles([]string{target}, buildContext(tags))
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
	if code := graalgoRunTags(root, append([]string{"--check"}, pkgFiles...), tags); code != 0 {
		return code
	}
	runArgs := append(append([]string{}, pkgFiles...), "--")
	runArgs = append(runArgs, progArgs...)
	return graalgoRunProgram(root, runArgs, tags)
}

// buildFlags holds the parsed `go build` flags that the driver recognizes.
// Unrecognized flags are rejected; recognized-but-unsupported flags produce
// a clear "not yet supported" error naming the flag.
type buildFlags struct {
	output  string // -o: output binary path
	ldflags string // -ldflags: linker flags (forwarded; -X needs backend)
	tags    string // -tags: build tags (honored: go/build file selection
	// in the driver, forwarded to the frontend for import resolution)
	pkgs []string // remaining package/file args
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
	files, err := resolveFiles(bf.pkgs, buildContext(bf.tags))
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
	if code := graalgoRunTags(root, emitArgs, bf.tags); code != 0 {
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
// Runs the frontend translation check (--check) once over the package's
// files together (cross-file references only resolve when the package is
// checked as one unit), then runs the static analyzers in internal/vet
// and reports diagnostics in the same style as `go vet`.
//
// _test.go files in the package itself (white-box tests) are checked
// with the package; external test files (package foo_test) are excluded:
// they declare a different package, so neither the frontend nor the
// type-checker can take them in the same unit. Like `go vet`, -tags is
// accepted and filters the package's files.
func cmdVet(root string, args []string) int {
	bf, err := parseBuildFlags(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo vet: %v\n", err)
		return 2
	}
	if bf.output != "" {
		fmt.Fprintf(os.Stderr, "graalgo vet: -o: not supported for vet\n")
		return 2
	}
	if bf.ldflags != "" {
		fmt.Fprintf(os.Stderr, "graalgo vet: -ldflags: not yet supported\n")
		return 1
	}
	files, err := resolveFiles(bf.pkgs, buildContext(bf.tags))
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo vet: %v\n", err)
		return 1
	}
	checkFiles, _ := splitVetFiles(files)
	if len(checkFiles) == 0 {
		fmt.Fprintf(os.Stderr, "graalgo vet: no buildable non-test Go files\n")
		return 1
	}
	// One frontend invocation over the whole package: checking files
	// one at a time fails on any cross-file reference.
	if code := graalgoRunTags(root, append([]string{"--check"}, checkFiles...), bf.tags); code != 0 {
		return 1
	}
	// Run the analyzers over the same file set as one package.
	ds, err := vet.Run(checkFiles)
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

// splitVetFiles partitions files into the set vet can check as one
// package and the external test files it cannot: non-test files plus
// white-box _test.go files (same package name) go in the first set;
// _test.go files declaring another package (package foo_test) go in the
// second. A file whose package clause cannot be parsed is kept in the
// check set so the frontend/vet reports the error instead of silently
// dropping the file.
func splitVetFiles(files []string) (checkFiles, externalTestFiles []string) {
	var pkgName string
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			if pkgName == "" {
				pkgName, _ = packageClause(f)
			}
		}
	}
	if pkgName == "" && len(files) > 0 {
		pkgName, _ = packageClause(files[0])
	}
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			checkFiles = append(checkFiles, f)
			continue
		}
		name, err := packageClause(f)
		if err == nil && name != pkgName {
			externalTestFiles = append(externalTestFiles, f)
			continue
		}
		checkFiles = append(checkFiles, f)
	}
	return checkFiles, externalTestFiles
}

// packageClause returns the package name declared by a Go source file,
// parsing only the package clause.
func packageClause(path string) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
	if err != nil {
		return "", err
	}
	return f.Name.Name, nil
}

// fmtOptions holds the `go fmt` flags cmdFmt accepts.
type fmtOptions struct {
	list     bool // -l: list files whose formatting differs, do not rewrite
	diff     bool // -d: print the diff, do not rewrite
	simplify bool // -s: simplify the code
	showErrs bool // -e: print all errors (including parse errors)
}

// cmdFmt implements `graalgo fmt [-l] [-d] [-s] [-e] [packages]`.
//
// Formats the Go source files using gofmt from the stock toolchain.
// Like `go fmt`: with no flags it rewrites files in place and prints the
// names of the files it changed; -l lists files whose formatting differs
// without rewriting them; -d prints the diff. `./...` formats all
// packages under the current directory (recursively). Formatting ignores
// build constraints — every .go file is formatted, like `go fmt`.
func cmdFmt(root string, args []string) int {
	opts, rest, err := parseFmtFlags(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo fmt: %v\n", err)
		return 2
	}
	files, err := expandFmtArgs(rest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo fmt: %v\n", err)
		return 1
	}
	gofmt := findGofmt()
	if gofmt == "" {
		fmt.Fprintf(os.Stderr, "graalgo fmt: cannot locate gofmt\n")
		return 1
	}
	// gofmt flags shared by every invocation.
	var gofmtFlags []string
	if opts.simplify {
		gofmtFlags = append(gofmtFlags, "-s")
	}
	if opts.showErrs {
		gofmtFlags = append(gofmtFlags, "-e")
	}
	if opts.diff {
		// -d prints the diff to stdout and rewrites nothing.
		// gofmt -d exits 1 when a diff exists (like diff) and 2 on a
		// real error: a diff is a successful display, so only other
		// exit codes propagate.
		cmd := exec.Command(gofmt, append(append([]string{"-d"}, gofmtFlags...), files...)...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				if ee.ExitCode() == 1 {
					return 0
				}
				return ee.ExitCode()
			}
			fmt.Fprintf(os.Stderr, "graalgo fmt: %v\n", err)
			return 1
		}
		return 0
	}
	// Find the files whose formatting differs, then either list them
	// (-l) or rewrite them and print their names (like `go fmt`).
	out, err := runGofmtList(gofmt, append(gofmtFlags, files...))
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "graalgo fmt: %v\n", err)
		return 1
	}
	needsFmt := strings.Fields(out)
	if opts.list {
		for _, f := range needsFmt {
			fmt.Println(f)
		}
		return 0
	}
	if len(needsFmt) == 0 {
		return 0
	}
	cmd := exec.Command(gofmt, append(append([]string{"-w"}, gofmtFlags...), needsFmt...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "graalgo fmt: %v\n", err)
		return 1
	}
	for _, f := range needsFmt {
		fmt.Println(f)
	}
	return 0
}

// parseFmtFlags splits leading -l/-d/-s/-e/-w flags from package args.
// -w is accepted and ignored: writing in place is the default. Unknown
// flags are an error, like `go fmt`.
func parseFmtFlags(args []string) (fmtOptions, []string, error) {
	var opts fmtOptions
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			i++
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			break
		}
		name, val, hasVal := strings.Cut(a, "=")
		set := true
		if hasVal {
			switch val {
			case "true":
				set = true
			case "false":
				set = false
			default:
				return opts, nil, fmt.Errorf("invalid value %q for flag %s", val, name)
			}
		}
		switch name {
		case "-l":
			opts.list = set
		case "-d":
			opts.diff = set
		case "-s":
			opts.simplify = set
		case "-e":
			opts.showErrs = set
		case "-w":
			// Accepted: writing in place is the default.
		default:
			return opts, nil, fmt.Errorf("unknown flag %q", a)
		}
	}
	return opts, args[i:], nil
}

// expandFmtArgs turns fmt package arguments into a file list. `./...`
// (and dir/... patterns) expand recursively to every .go file under the
// directory, skipping vendor and hidden/_-prefixed directories like
// `go fmt`; anything else goes through resolveFiles with a context that
// matches all files (formatting ignores build constraints).
func expandFmtArgs(args []string) ([]string, error) {
	var files []string
	// Formatting ignores build constraints: every .go file is a
	// candidate, including platform-specific ones.
	ctxt := build.Default
	ctxt.UseAllFiles = true
	for _, a := range args {
		if strings.HasSuffix(a, "/...") {
			root := strings.TrimSuffix(a, "/...")
			if root == "." || root == "" {
				root = "."
			}
			matches, err := walkGoFiles(root)
			if err != nil {
				return nil, err
			}
			files = append(files, matches...)
			continue
		}
		matches, err := resolveFiles([]string{a}, ctxt)
		if err != nil {
			return nil, err
		}
		files = append(files, matches...)
	}
	if len(args) == 0 {
		// No operands: the package in the current directory.
		matches, err := resolveFiles(nil, ctxt)
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

// walkGoFiles collects every .go file under root (recursively), skipping
// vendor and hidden/_-prefixed directories, like `go fmt ./...`.
func walkGoFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// runGofmtList runs `gofmt -l` over files and returns its stdout: the
// newline-separated names of files whose formatting differs. Parse
// errors go to the caller's stderr (gofmt reports them there).
func runGofmtList(gofmt string, args []string) (string, error) {
	cmdArgs := append([]string{"-l"}, args...)
	cmd := exec.Command(gofmt, cmdArgs...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
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
