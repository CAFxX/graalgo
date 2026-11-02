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
	// -tags is forwarded, never dropped: it reaches the backend via
	// tagsEnv and filters the package-form file list below.
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
		// Explicit files are always included (go run semantics); the
		// tags still reach the backend for imported packages.
		return graalgoRunProgramEnv(root, runArgs, tagsEnv(tags))
	}

	// Package (directory or import path): find its main package files.
	// _test.go files are excluded, like `go run` (they're not part of
	// the package build).
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
	pkgFiles, err = filterFilesByBuildTags(pkgFiles, tags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo run: %v\n", err)
		return 1
	}
	// The backend compiles all package files together as one unit.
	if code := graalgoRunEnv(root, append([]string{"--check"}, pkgFiles...), tagsEnv(tags)); code != 0 {
		return code
	}
	runArgs := append(append([]string{}, pkgFiles...), "--")
	runArgs = append(runArgs, progArgs...)
	return graalgoRunProgramEnv(root, runArgs, tagsEnv(tags))
}

// tagsEnv carries build tags to a backend invocation via the
// GRAALGO_BUILD_TAGS environment variable — the channel the frontend's
// disk-import path already honors (internal/frontend/diskimport.go).
// Tags are not passed as backend argv: the backend rejects unknown
// flags, so an invented --tags argv flag would break every tagged
// build. The value is appended after the inherited environment, so a
// driver-supplied value wins over any user-exported GRAALGO_BUILD_TAGS
// (Go exec keeps the last duplicate key). Tags are not a
// privilege-conferring signal, so the env channel is appropriate here
// (unlike --test-package-dir, which deliberately stays on argv).
func tagsEnv(tags string) []string {
	if tags == "" {
		return nil
	}
	return []string{"GRAALGO_BUILD_TAGS=" + tags}
}

// filterFilesByBuildTags drops files whose build constraints are not
// satisfied with the given tags, using go/build — the same mechanism
// `go build` uses for file selection. With no tags the file list is
// returned unchanged, so untagged builds keep their exact current file
// set (including files constrained only by GOOS/GOARCH, which the
// driver historically passes through). An error reading a file's
// constraints, or a tag set that excludes every file, is a loud error,
// never a silent empty build.
func filterFilesByBuildTags(files []string, tags string) ([]string, error) {
	if tags == "" {
		return files, nil
	}
	ctxt := build.Default
	ctxt.BuildTags = strings.Fields(tags)
	var kept []string
	for _, f := range files {
		abs, err := filepath.Abs(f)
		if err != nil {
			return nil, fmt.Errorf("build tags: %s: %v", f, err)
		}
		ok, err := ctxt.MatchFile(filepath.Dir(abs), filepath.Base(abs))
		if err != nil {
			return nil, fmt.Errorf("build tags: %s: %v", f, err)
		}
		if ok {
			kept = append(kept, f)
		}
	}
	if len(kept) == 0 {
		return nil, fmt.Errorf("build tags %q exclude all files", tags)
	}
	return kept, nil
}

// groupFilesByPackage partitions files by package clause, so a directory
// holding both a package and its external test package (package foo
// plus package foo_test) is vetted as two packages, not one. Files
// sharing a clause (including internal _test.go files in the same
// package) stay together. A file whose package clause cannot be parsed
// is a loud error.
func groupFilesByPackage(files []string) ([][]string, error) {
	fset := token.NewFileSet()
	byName := map[string][]string{}
	var order []string
	for _, f := range files {
		parsed, err := parser.ParseFile(fset, f, nil, parser.PackageClauseOnly)
		if err != nil {
			return nil, fmt.Errorf("vet: parse %s: %v", f, err)
		}
		name := parsed.Name.Name
		if _, ok := byName[name]; !ok {
			order = append(order, name)
		}
		byName[name] = append(byName[name], f)
	}
	groups := make([][]string, 0, len(order))
	for _, name := range order {
		groups = append(groups, byName[name])
	}
	return groups, nil
}

// buildFlags holds the parsed `go build` flags that the driver recognizes.
// Unrecognized flags are rejected; recognized-but-unsupported flags produce
// a clear "not yet supported" error naming the flag.
type buildFlags struct {
	output  string // -o: output binary path
	ldflags string // -ldflags: linker flags (forwarded; -X needs backend)
	tags    string // -tags: build tags. Forwarded to the backend run
	// via GRAALGO_BUILD_TAGS (the channel the frontend's disk-import
	// path honors) and applied to the top-level package files by the
	// driver itself (filterFilesByBuildTags), since the backend
	// compiles exactly the files it is given. Never silently dropped.
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
	pkgFiles, err = filterFilesByBuildTags(pkgFiles, bf.tags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo build: %v\n", err)
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
	emitArgs, emitEnv := backendCheckArgs(bf.tags, wirePath, pkgFiles)
	if code := graalgoRunEnv(root, emitArgs, emitEnv); code != 0 {
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
// backendCheckArgs builds the backend invocation for a frontend gate
// run that captures wire JSON: the argv for the --emit-wire/--check run
// plus the environment carrying the build tags (see tagsEnv). Unit
// tests assert this invocation so a -tags regression (parsed but never
// forwarded, R01) cannot recur silently.
func backendCheckArgs(tags, wirePath string, files []string) (args, env []string) {
	args = append([]string{"--emit-wire=" + wirePath, "--check"}, files...)
	return args, tagsEnv(tags)
}

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
	// Run the analyzers per package clause, never over a mixed-clause
	// file list as one package: a directory holding package foo plus
	// its external test package (foo_test) previously failed the
	// single Check, and the analyzers then ran over degraded type
	// information while the driver printed a clean verdict (R02).
	groups, err := groupFilesByPackage(files)
	if err != nil {
		fmt.Fprintf(os.Stderr, "graalgo vet: %v\n", err)
		return 1
	}
	var allDs []vet.Diagnostic
	degraded := false
	for _, g := range groups {
		ds, err := vet.Run(g)
		if err != nil {
			// Loud, and never followed by "no issues found": a
			// type-checking failure means type-dependent analyzers
			// ran degraded, so a clean diagnostic list proves
			// nothing. Other groups still run so one bad package
			// does not hide the rest.
			fmt.Fprintf(os.Stderr, "graalgo vet: %v\n", err)
			degraded = true
		}
		allDs = append(allDs, ds...)
	}
	for _, d := range vet.SortDiagnostics(allDs) {
		fmt.Printf("%s: %s\n", d.Pos, d.Message)
	}
	if degraded || len(allDs) > 0 {
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
