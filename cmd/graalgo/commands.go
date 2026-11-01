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
		// Program args are separated from the launcher's own flags by
		// "--" (the launcher treats every bare "-..." before "--" as
		// its own flag).
		runArgs := append(append([]string{}, progFiles...), "--")
		runArgs = append(runArgs, progArgs...)
		return graalgoRunProgram(root, runArgs)
	}

	// Package (directory or import path): find its main package files.
	// _test.go files are excluded, like `go run` (they're not part of
	// the package build).
	//
	// `./...` is only runnable when it matches exactly one package:
	// `go run` takes a single main package, so a multi-package match
	// is a loud error, not a merged build of unrelated packages.
	if target == "./..." {
		dirs, err := expandDotDotDot()
		if err != nil {
			fmt.Fprintf(os.Stderr, "graalgo run: %v\n", err)
			return 1
		}
		if len(dirs) != 1 {
			fmt.Fprintf(os.Stderr, "graalgo run: ./... matches %d packages; run a single package or file\n", len(dirs))
			return 1
		}
		target = dirs[0]
	}
	files, err := resolveFiles([]string{target})
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
	if code := graalgoRun(root, append([]string{"--check"}, pkgFiles...)); code != 0 {
		return code
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
	// A "./..." argument expands to many packages, which must each be
	// gated separately: merging files from different packages into one
	// frontend invocation would be a wrong build, not a build.
	for _, p := range bf.pkgs {
		if p == "./..." {
			return cmdBuildExpanded(root, bf)
		}
	}
	files, err := resolveFiles(bf.pkgs)
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
	wireJSON, err := gatePackage(root, pkgFiles)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo build: %v\n", err)
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

// gatePackage runs the frontend once over one package's files,
// capturing its wire output for the artifact. This both validates the
// package and produces the IR that the artifact embeds, so the artifact
// never re-runs the frontend.
func gatePackage(root string, pkgFiles []string) ([]byte, error) {
	wireFile, err := os.CreateTemp("", "graalgo-wire-*.json")
	if err != nil {
		return nil, fmt.Errorf("cannot create temp file: %v", err)
	}
	wirePath := wireFile.Name()
	wireFile.Close()
	defer os.Remove(wirePath)
	emitArgs := append([]string{"--emit-wire=" + wirePath, "--check"}, pkgFiles...)
	if code := graalgoRun(root, emitArgs); code != 0 {
		return nil, fmt.Errorf("build failed")
	}
	wireJSON, err := os.ReadFile(wirePath)
	if err != nil {
		return nil, fmt.Errorf("cannot read frontend output: %v", err)
	}
	return wireJSON, nil
}

// isMainPackage reports whether the package's files declare
// `package main`, from the package clause alone. A parse failure yields
// false; the frontend gate reports the real error for such files.
func isMainPackage(pkgFiles []string) bool {
	if len(pkgFiles) == 0 {
		return false
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, pkgFiles[0], nil, parser.PackageClauseOnly)
	if err != nil {
		return false
	}
	return f.Name.Name == "main"
}

// nonTestGoFiles returns the .go files in dir excluding _test.go files,
// like `go build` (test files are not part of the package build).
func nonTestGoFiles(dir string) []string {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	var out []string
	for _, f := range matches {
		if !strings.HasSuffix(f, "_test.go") {
			out = append(out, f)
		}
	}
	return out
}

// cmdBuildExpanded implements `graalgo build` when any argument is
// "./...", mirroring `go build ./...`:
//   - every matched package is gated separately through the frontend;
//   - packages with no non-test Go files (e.g. test-only packages) are
//     skipped, as `go build ./...` succeeds over them (verified vs gc);
//   - with multiple buildable packages, the results are discarded like
//     `go build` discards them: no artifact is emitted, exit 0;
//   - with exactly one buildable package and that package main, the
//     artifact is emitted exactly as for a single-package build;
//   - -o with multiple buildable packages is a loud error, like gc's
//     "cannot write multiple packages".
func cmdBuildExpanded(root string, bf buildFlags) int {
	dirs, err := resolveTestDirs(bf.pkgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo build: %v\n", err)
		return 2
	}
	dirs = dedupDirs(dirs)
	// Collect the buildable packages first: the -o check and the
	// single-main decision both count buildable packages, not raw
	// matched directories.
	type buildable struct {
		dir   string
		files []string
	}
	var pkgs []buildable
	for _, dir := range dirs {
		if files := nonTestGoFiles(dir); len(files) > 0 {
			pkgs = append(pkgs, buildable{dir: dir, files: files})
		}
	}
	if len(pkgs) == 0 {
		fmt.Fprintf(os.Stderr, "graalgo build: no non-test Go files\n")
		return 1
	}
	if bf.output != "" && len(pkgs) > 1 {
		fmt.Fprintf(os.Stderr, "graalgo build: -o cannot be used with multiple packages (./... matched %d packages)\n", len(pkgs))
		return 2
	}
	var mainWire []byte
	mainCount := 0
	var mainDir string
	for _, p := range pkgs {
		wireJSON, err := gatePackage(root, p.files)
		if err != nil {
			fmt.Fprintf(os.Stderr, "graalgo build: %v\n", err)
			return 1
		}
		if isMainPackage(p.files) {
			mainWire = wireJSON
			mainCount++
			mainDir = p.dir
		}
	}
	// Mirror `go build`: multiple packages (or one non-main package)
	// compile-checked, result discarded, no artifact.
	if len(pkgs) != 1 || mainCount != 1 {
		return 0
	}
	out, err := driver.DefaultOutputName(bf.output, []string{mainDir})
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo build: %v\n", err)
		return 2
	}
	if err := driver.Emit(driver.Artifact{
		OutputPath:  out,
		GraalgoRoot: root,
		WireJSON:    mainWire,
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
	// Run the analyzers per directory, not over all files as one
	// package: vet.Run type-checks its files as a single package, so
	// merging files from different packages (as `./...` expansion
	// produces) would analyze nonsense. Grouping is order-preserving
	// and a no-op for the single-package case.
	var dirOrder []string
	byDir := map[string][]string{}
	for _, f := range files {
		d := filepath.Dir(f)
		if _, ok := byDir[d]; !ok {
			dirOrder = append(dirOrder, d)
		}
		byDir[d] = append(byDir[d], f)
	}
	var all []vet.Diagnostic
	for _, d := range dirOrder {
		ds, err := vet.Run(byDir[d])
		if err != nil {
			fmt.Fprintf(os.Stderr, "graalgo vet: %v\n", err)
			return 1
		}
		all = append(all, ds...)
	}
	for _, d := range all {
		fmt.Printf("%s: %s\n", d.Pos, d.Message)
	}
	if len(all) > 0 {
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
