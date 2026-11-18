// Command graalgo-tool is the -toolexec wrapper of the graalgo backend:
//
//	go build -toolexec /path/to/graalgo-tool ./...
//
// cmd/go then runs every toolchain tool as "graalgo-tool <tool> <args>".
// This walking skeleton implements the protocol of
// docs/arch/toolexec-design.md §2 for the two tools graalgo replaces:
//
//   - compile: run the stock compiler unchanged (it writes the archive with
//     gc export data and the build ID), then type-check the same files with
//     go/types against that export data and append the package's IR to the
//     archive.
//   - link: collect the IR of every package of the link into a bundle and
//     write launcher stub + bundle as the executable.
//
// Every other tool (asm, cgo, cover, vet, ...) is passed through.
// "<tool> -V=full" is answered with the stock line plus a graalgo suffix,
// which separates graalgo and gc entries in a shared build cache.
//
// Response files (cmd/go uses them because graalgo-tool is installed under
// the basename "compile", design §1) are expanded and rewritten for the stock
// tool; "-chain X" runs every stock tool under the user's own -toolexec X.
//
// The IR is GIR (internal/toolexec/ir), emitted by internal/toolexec/emit.
// Skeleton limits, each failing loudly rather than silently: packages
// compiled with -std get no IR (the stdlib fork of design §5 does not exist
// yet).
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"graalgo-toolexec/archive"
	"graalgo-toolexec/argv"
	"graalgo-toolexec/bundle"
	"graalgo-toolexec/emit"
	"graalgo-toolexec/ir"
	"graalgo-toolexec/release"
)

// irVersion is part of the -V=full answer: any change to the GIR schema
// (its version or its content hash) invalidates every cached archive.
var irVersion = fmt.Sprintf("gir%d.%x", ir.SchemaVersion, ir.SchemaHash[:4])

// goTools are the toolchain tools cmd/go runs from GOROOT/pkg/tool. Other
// tool names (the bare "gcc" of the C compiler probe) are host tools.
var goTools = map[string]bool{
	"compile": true, "link": true, "asm": true, "cgo": true, "cover": true,
	"vet": true, "preprofile": true, "pack": true, "buildid": true,
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go build -toolexec graalgo-tool ... (graalgo-tool [-chain CMD] <tool> <args>)")
		os.Exit(2)
	}
	inv, err := argv.ParseInvocation(os.Args[1:])
	if err != nil {
		fatalf("%v", err)
	}
	// cmd/go probes the host C compiler as a bare name; exec does no PATH
	// lookup for us.
	if !strings.ContainsRune(inv.Tool, filepath.Separator) {
		p, err := exec.LookPath(inv.Tool)
		if err != nil {
			fatalf("%v", err)
		}
		inv.Tool = p
	}
	if len(inv.Chain) > 0 && !strings.ContainsRune(inv.Chain[0], filepath.Separator) {
		p, err := exec.LookPath(inv.Chain[0])
		if err != nil {
			fatalf("-chain: %v", err)
		}
		inv.Chain[0] = p
	}
	logInvocation(inv.Tool, inv.Expanded)
	name := filepath.Base(inv.Tool)
	if goTools[name] {
		checkToolchain(inv.Tool)
	}
	switch {
	case len(inv.Expanded) == 1 && inv.Expanded[0] == "-V=full":
		versionProbe(inv, name)
	case name == "compile":
		compile(inv)
	case name == "link":
		link(inv.Expanded)
	default:
		os.Exit(run(inv.Command()))
	}
}

// logInvocation appends one line per tool run to $GRAALGO_TOOL_LOG, which
// the tests use to observe what cmd/go invoked.
func logInvocation(tool string, args []string) {
	path := os.Getenv("GRAALGO_TOOL_LOG")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fatalf("%v", err)
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", filepath.Base(tool), strings.Join(args, " "))
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "graalgo-tool: "+format+"\n", a...)
	os.Exit(2)
}

// run executes a command with stdio inherited and returns its exit status.
func run(argv []string) int {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ee):
		return ee.ExitCode()
	default:
		fatalf("%v", err)
		return 2
	}
}

// graalgoHome returns GRAALGO_HOME when graalgo-tool runs from an installed
// layout (GRAALGO_HOME/libexec/compile), else "".
func graalgoHome() string {
	dir := filepath.Dir(selfPath())
	if filepath.Base(dir) != "libexec" {
		return ""
	}
	return filepath.Dir(dir)
}

// checkToolchain refuses toolchain tools that do not belong to graalgo's
// GOROOT: under GOTOOLCHAIN switching cmd/go re-execs another release's go,
// whose GOROOT has no graalgo fork (design §1).
func checkToolchain(tool string) {
	home := graalgoHome()
	if home == "" {
		return // development layout: no GOROOT of graalgo's own
	}
	want, err := filepath.EvalSymlinks(filepath.Join(home, "goroot"))
	if err != nil {
		return
	}
	got, err := filepath.EvalSymlinks(tool)
	if err != nil {
		got = tool
	}
	if !strings.HasPrefix(got, want+string(filepath.Separator)) {
		fatalf("%s is not a tool of graalgo's GOROOT %s; this graalgo tracks %s (was the toolchain switched with GOTOOLCHAIN?)", tool, want, release.GoVersion)
	}
}

// versionProbe answers "<tool> -V=full" (design §2.1). cmd/go hashes the
// whole line into its action IDs for release toolchains, so the graalgo
// suffix separates graalgo and gc entries in a shared GOCACHE and any
// change to graalgo-tool (or, for link, to the runtime it links in)
// invalidates graalgo's entries.
func versionProbe(inv *argv.Invocation, name string) {
	cmd := exec.Command(inv.Command()[0], inv.Command()[1:]...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		fatalf("%s -V=full: %v", name, err)
	}
	line := strings.TrimSpace(string(out))
	if goTools[name] {
		// "<tool> version go1.27.1[ X:exp...]"
		if f := strings.Fields(line); len(f) < 3 || f[1] != "version" || f[2] != release.GoVersion {
			fatalf("%s reports %q; this graalgo tracks %s (was the toolchain switched with GOTOOLCHAIN?)", name, line, release.GoVersion)
		}
	}
	fmt.Printf("%s graalgo-%s-%s\n", line, irVersion, toolHash(name))
}

// toolHash is the first 16 hex digits of SHA-256 over graalgo-tool and,
// for link, over everything a linked program contains besides IR: the
// launcher stub and the runtime manifest of the installed layout.
func toolHash(name string) string {
	files := []string{selfPath()}
	if name == "link" {
		files = append(files, stubPath())
		if home := graalgoHome(); home != "" {
			m := filepath.Join(home, "lib", "MANIFEST.sha256")
			if _, err := os.Stat(m); err == nil {
				files = append(files, m)
			}
		}
	}
	h := sha256.New()
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			fatalf("%v", err)
		}
		h.Write(data)
	}
	return fmt.Sprintf("%x", h.Sum(nil)[:8])
}

func selfPath() string {
	p, err := os.Executable()
	if err != nil {
		fatalf("%v", err)
	}
	return p
}

// stubPath locates the launcher stub: $GRAALGO_STUB, else "graalgo-stub"
// next to this executable.
func stubPath() string {
	if p := os.Getenv("GRAALGO_STUB"); p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(selfPath()), "graalgo-stub")
}

// flags is the subset of a tool's command line the wrapper interprets.
type flags struct {
	values map[string]string // -name value / -name=value
	bools  map[string]bool   // -name
	rest   []string          // trailing non-flag arguments
}

// parseFlags extracts the named value flags and boolean flags from args.
// Other flags are skipped; because their arity is unknown here, the
// positional arguments are taken to be the trailing run of arguments that
// do not start with "-", which is how cmd/go lays out compile and link
// command lines.
func parseFlags(args []string, valueFlags, boolFlags []string) flags {
	fl := flags{values: map[string]string{}, bools: map[string]bool{}}
	isValue := map[string]bool{}
	for _, v := range valueFlags {
		isValue[v] = true
	}
	isBool := map[string]bool{}
	for _, b := range boolFlags {
		isBool[b] = true
	}
	end := len(args)
	for end > 0 && !strings.HasPrefix(args[end-1], "-") {
		end--
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			continue
		}
		name, val, hasVal := strings.Cut(strings.TrimLeft(a, "-"), "=")
		switch {
		case isValue[name] && hasVal:
			fl.values[name] = val
		case isValue[name] && i+1 < len(args):
			fl.values[name] = args[i+1]
			if i+1 >= end {
				end = i + 2
			}
			i++
		case isBool[name]:
			fl.bools[name] = true
		}
	}
	fl.rest = args[end:]
	return fl
}

// compileValueFlags are the cmd/compile flags that take a value as a
// separate argument when cmd/go passes them; parseFlags needs the full list
// to tell a flag's value from the first source file.
var compileValueFlags = []string{
	"o", "p", "importcfg", "lang", "trimpath", "embedcfg", "coveragecfg",
	"asmhdr", "symabis", "buildid", "goversion", "D", "I", "c", "d",
	"pgoprofile", "linkobj", "installsuffix", "json", "env", "spectre",
	"gendwarfinl", "bench", "blockprofile", "cpuprofile", "memprofile",
	"mutexprofile", "traceprofile", "goexperiment",
}

func compile(inv *argv.Invocation) {
	args := inv.Expanded
	fl := parseFlags(args, compileValueFlags, []string{"std", "race", "msan", "asan"})
	pkgPath := fl.values["p"]
	// Refusals that must happen before gc writes an archive cmd/go would
	// cache: these builds would otherwise succeed and behave wrongly.
	if v := fl.values["goversion"]; v != "" && v != release.GoVersion {
		fatalf("%s: compile -goversion %s; this graalgo tracks %s (was the toolchain switched with GOTOOLCHAIN?)", pkgPath, v, release.GoVersion)
	}
	if fl.values["coveragecfg"] != "" {
		fatalf("%s: coverage (-cover) not yet supported by the graalgo backend", pkgPath)
	}
	if strings.Contains(","+fl.values["d"]+",", ",libfuzzer") {
		fatalf("%s: fuzzing instrumentation (-d=libfuzzer) not supported by the graalgo backend", pkgPath)
	}
	if fl.bools["msan"] || fl.bools["asan"] {
		fatalf("%s: -msan/-asan not supported by the graalgo backend", pkgPath)
	}
	// Shadow compile: gc produces the archive, export data, diagnostics.
	if code := run(inv.Command()); code != 0 {
		os.Exit(code)
	}
	// -race needs no IR difference: link refuses it (design §2.6).
	if fl.bools["std"] {
		return // no IR for the unforked standard library in the skeleton
	}
	out := fl.values["o"]
	if out == "" || pkgPath == "" || len(fl.rest) == 0 {
		fatalf("compile: cannot interpret command line (need -o, -p and source files)")
	}
	for _, f := range fl.rest {
		if !strings.HasSuffix(f, ".go") {
			fatalf("%s: compile: unexpected positional argument %q", pkgPath, f)
		}
	}
	var embed *emit.EmbedConfig
	if cfg := fl.values["embedcfg"]; cfg != "" {
		data, err := os.ReadFile(cfg)
		if err != nil {
			fatalf("%s: %v", pkgPath, err)
		}
		embed = &emit.EmbedConfig{}
		if err := json.Unmarshal(data, embed); err != nil {
			fatalf("%s: -embedcfg: %v", pkgPath, err)
		}
	}
	gir, err := emitIR(pkgPath, fl.values["lang"], fl.values["importcfg"], fl.values["trimpath"], fl.bools["std"], embed, fl.rest)
	if err != nil {
		fatalf("%s: %v", pkgPath, err)
	}
	if err := archive.Append(out, archive.IRMember, gir); err != nil {
		fatalf("%s: %v", pkgPath, err)
	}
}

// readImportcfg parses an importcfg file into import path -> archive, with
// importmap entries resolved.
func readImportcfg(path string) (map[string]string, error) {
	pkgFile := map[string]string{}
	if path == "" {
		return pkgFile, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	importMap := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		verb, rest, _ := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		before, after, ok := strings.Cut(rest, "=")
		if !ok {
			continue // comments, blank lines, modinfo
		}
		switch verb {
		case "packagefile":
			pkgFile[before] = after
		case "importmap":
			importMap[before] = after
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for from, to := range importMap {
		if file, ok := pkgFile[to]; ok {
			pkgFile[from] = file
		}
	}
	return pkgFile, nil
}

// emitIR type-checks the package with go/types, importing dependencies
// from the gc export data named by importcfg, and returns its encoded GIR.
func emitIR(pkgPath, lang, importcfg, trimpath string, std bool, embed *emit.EmbedConfig, files []string) ([]byte, error) {
	pkgFile, err := readImportcfg(importcfg)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var astFiles []*ast.File
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("internal: go/parser disagrees with gc: %v", err)
		}
		astFiles = append(astFiles, f)
	}
	lookup := func(path string) (io.ReadCloser, error) {
		file, ok := pkgFile[path]
		if !ok {
			return nil, fmt.Errorf("no packagefile for %q in importcfg", path)
		}
		return os.Open(file)
	}
	info := emit.NewInfo()
	conf := types.Config{
		Importer:  importer.ForCompiler(fset, "gc", lookup),
		GoVersion: lang,
		Sizes:     types.SizesFor("gc", runtime.GOARCH),
	}
	tpkg, err := conf.Check(pkgPath, fset, astFiles, info)
	if err != nil {
		// gc accepted these files; a go/types rejection is a wrapper bug.
		return nil, fmt.Errorf("internal: go/types disagrees with gc: %v", err)
	}
	f, _, err := emit.Emit(emit.Config{
		Fset: fset, Pkg: tpkg, Info: info, Files: astFiles, Lang: lang, Std: std,
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		FileName: func(name string) string { return absFile(name, trimpath) },
		Embed:    embed,
	})
	if err != nil {
		return nil, err
	}
	return ir.Encode(f)
}

// absFile is the file name gc records for a source file (cmd/internal/
// objabi.AbsFile): made absolute against the working directory, then the
// -trimpath rewrites, else a $GOROOT prefix (design §6, -trimpath).
func absFile(name, trimpath string) string {
	if !filepath.IsAbs(name) {
		if wd, err := os.Getwd(); err == nil {
			name = filepath.Join(wd, name)
		}
	}
	if out := argv.ApplyRewrites(name, trimpath); out != name {
		return out
	}
	if root := os.Getenv("GOROOT"); root != "" && strings.HasPrefix(name, root+"/") {
		return "$GOROOT" + name[len(root):]
	}
	return name
}

func link(args []string) {
	fl := parseFlags(args, []string{"o", "importcfg", "buildid", "buildmode", "X", "extld", "extldflags", "linkmode", "installsuffix", "r", "E", "H", "I", "L", "T", "B", "k", "tmpdir", "pluginpath", "libgcc", "fipso", "randlayout", "compressdwarf"}, []string{"race", "msan", "asan"})
	out := fl.values["o"]
	if out == "" || len(fl.rest) != 1 {
		fatalf("link: cannot interpret command line (need -o and one main archive)")
	}
	if mode := fl.values["buildmode"]; mode != "" && mode != "exe" && mode != "pie" {
		fatalf("link: -buildmode=%s is not supported by the graalgo backend", mode)
	}
	if fl.bools["msan"] || fl.bools["asan"] {
		fatalf("link: -msan/-asan not supported by the graalgo backend")
	}
	if fl.bools["race"] && os.Getenv("GRAALGO_RACE") != "ignore" {
		// Building an uninstrumented binary by default would make
		// go test -race pass vacuously (design §2.6).
		fatalf("link: the graalgo backend has no race detector; set GRAALGO_RACE=ignore to build without one")
	}
	pkgFile, err := readImportcfg(fl.values["importcfg"])
	if err != nil {
		fatalf("link: %v", err)
	}
	mainArchive := fl.rest[0]
	paths := make([]string, 0, len(pkgFile))
	for p := range pkgFile {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	b := &bundle.Bundle{Runner: os.Getenv("GRAALGO_RUNNER")}
	seenMain := false
	for _, p := range paths {
		ir, err := archive.Read(pkgFile[p], archive.IRMember)
		if errors.Is(err, archive.ErrNoMember) {
			continue // -std package (skeleton) or a package built by plain gc
		}
		if err != nil {
			fatalf("link: %s: %v", p, err)
		}
		if pkgFile[p] == mainArchive {
			seenMain = true
			b.Main = p
		}
		b.Packages = append(b.Packages, bundle.Package{ImportPath: p, IR: ir})
	}
	if !seenMain {
		// cmd/go lists the main package in importcfg.link; be robust if
		// it does not.
		ir, err := archive.Read(mainArchive, archive.IRMember)
		if err != nil {
			fatalf("link: main package archive %s: %v (was it compiled through graalgo-tool?)", mainArchive, err)
		}
		b.Main = "main"
		b.Packages = append(b.Packages, bundle.Package{ImportPath: "main", IR: ir})
	}
	stub, err := os.ReadFile(stubPath())
	if err != nil {
		fatalf("link: launcher stub: %v", err)
	}
	if id := fl.values["buildid"]; id != "" {
		placeholder := []byte(bundle.BuildIDPlaceholder)
		if len(id) != len(placeholder) {
			fatalf("link: build ID is %d bytes, the stub reserves %d", len(id), len(placeholder))
		}
		if !bytes.Contains(stub, placeholder) {
			fatalf("link: launcher stub %s was not linked with the placeholder build ID", stubPath())
		}
		stub = bytes.ReplaceAll(stub, placeholder, []byte(id))
	}
	enc := bundle.Encode(b)
	file := append(append(stub, enc...), bundle.Trailer(int64(len(stub)), int64(len(enc)))...)
	if err := os.WriteFile(out, file, 0o755); err != nil {
		fatalf("link: %v", err)
	}
}
