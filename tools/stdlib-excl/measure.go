package main

import (
	"encoding/json"
	"fmt"
	"go/build"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// measure.go — the strict pure-Go check (P3-M6 workstream 5; SPEC §2.8,
// GG-0009 re-measurement on go1.27.1).
//
// Definition. A package dir D (population: see population.go) is evaluated
// for GOOS=linux GOARCH=amd64 via go/build. Let SEL(D) be the Go files
// selected for that target. D is:
//
//   - "not-linux-amd64": SEL(D) is empty — the dir contributes no files to
//     a linux/amd64 build (e.g. windows-only files). Not counted as
//     pure-Go nor coupled.
//   - "pure": SEL(D) is non-empty and none of the coupled reasons below
//     fires.
//   - "coupled": SEL(D) is non-empty and at least one reason fires.
//
// Coupled reasons (a dir may carry several):
//   - ASM:        a .s file is selected for the linux/amd64 build.
//   - CGO:        some .go file in D has `import "C"` (parsed imports).
//   - LINKNAME:   some .go file in D carries a //go:linkname directive
//                 (parsed comments; // line comments, matching gc).
//   - BUILD_TAGS: some source file in D (.go or .s) is excluded from the
//                 linux/amd64 build by a //go:build (or // +build) line.
//
// Scope notes (deliberate, documented):
//   - .go files are scanned whole-dir INCLUDING _test.go: a linkname or
//     cgo use in a test file still binds the package's test binary to a
//     runtime symbol, which the intrinsic surface (GG-0020) must provide
//     before the M7–M9 waves can run it. The JSON also carries the
//     non-test-only counts as a secondary metric for the narrower
//     "compiles unchanged" reading.
//   - "Compile unchanged against the frontend + intrinsic surface" is a
//     strictly stronger claim than this check; M6 delivers the pure-Go
//     check + per-dir reason. The compile claim is re-verified per wave
//     in M7–M9 (see coupled-packages-go1.27.1.md §5).
//   - Only linux/amd64 is evaluated (GG-0023: linux first, darwin
//     deferred, Windows out of v1 scope).

// Coupled reasons, stable strings used in the JSON output.
const (
	ReasonASM       = "asm"
	ReasonCGO       = "cgo"
	ReasonLinkname  = "linkname"
	ReasonBuildTags = "build-tags"
)

// DirClass is the measurement verdict for one package dir.
type DirClass string

const (
	ClassPure          DirClass = "pure"
	ClassCoupled       DirClass = "coupled"
	ClassNotLinuxAmd64 DirClass = "not-linux-amd64"
)

// DirResult is the per-dir measurement record.
type DirResult struct {
	Path    string   `json:"path"`
	Class   DirClass `json:"class"`
	Reasons []string `json:"reasons"`
	// NonTestReasons recomputes the verdict ignoring _test.go files
	// (secondary metric; see scope notes above).
	NonTestReasons []string `json:"non_test_reasons"`
}

// MeasureReport is the versioned machine-readable measurement.
type MeasureReport struct {
	Version           int            `json:"version"`
	GoVersion         string         `json:"go_version"`
	Generated         string         `json:"generated_utc"`
	Src               string         `json:"src"`
	Target            string         `json:"target"`
	Population        int            `json:"population_dirs"`
	Counts            map[string]int `json:"counts"`
	CountsNonTestOnly map[string]int `json:"counts_non_test_only"`
	ReasonCounts      map[string]int `json:"reason_counts"`
	TestBinaries      int            `json:"test_binaries"`
	Dirs              []DirResult    `json:"dirs"`
}

var goBuildLineRe = regexp.MustCompile(`(?m)^[ \t]*//[ \t]*go:build[ \t]`)
var plusBuildLineRe = regexp.MustCompile(`(?m)^[ \t]*//[ \t]*\+build[ \t]`)

// hasBuildConstraint reports whether the file's leading comment block
// carries a //go:build or // +build line.
func hasBuildConstraint(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	// Only the leading comment block matters; cap the scan.
	head := data
	if len(head) > 8192 {
		head = head[:8192]
	}
	// Stop at the first non-comment, non-blank line.
	var lines []string
	for _, ln := range strings.Split(string(head), "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "//") {
			lines = append(lines, ln)
			continue
		}
		break
	}
	joined := strings.Join(lines, "\n")
	return goBuildLineRe.MatchString(joined) || plusBuildLineRe.MatchString(joined)
}

// fileHasImportC reports whether the Go file imports "C".
func fileHasImportC(path string) bool {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return false
	}
	for _, imp := range f.Imports {
		if strings.Trim(imp.Path.Value, `"`) == "C" {
			return true
		}
	}
	return false
}

// fileHasLinkname reports whether the Go file carries a //go:linkname
// directive (// line comments only, matching gc's pragma scan).
func fileHasLinkname(path string) bool {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return false
	}
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			t := strings.TrimSpace(c.Text)
			if !strings.HasPrefix(t, "//") {
				continue // block comments: not honored for linkname
			}
			rest := strings.TrimSpace(strings.TrimPrefix(t, "//"))
			if strings.HasPrefix(rest, "go:linkname") {
				if len(rest) == len("go:linkname") || rest[len("go:linkname")] == ' ' || rest[len("go:linkname")] == '\t' {
					return true
				}
			}
		}
	}
	return false
}

// linuxAmd64Context is the go/build context for the evaluated target.
// CgoEnabled is true so that files with `import "C"` are still selected
// for the linux/amd64 build (cgo is available on the target); the CGO
// coupled reason is detected separately by classifyDir. Selection here
// answers "what does the gc toolchain build on linux/amd64", not "what
// does graalgo support".
func linuxAmd64Context() *build.Context {
	c := build.Default
	c.GOOS = "linux"
	c.GOARCH = "amd64"
	c.CgoEnabled = true
	return &c
}

// classifyDir applies the strict pure-Go check to one package dir.
// goFiles/testGoFiles are the .go file names directly inside abs.
func classifyDir(ctx *build.Context, abs string, goFiles, testGoFiles []string) DirResult {
	all := append(append([]string{}, goFiles...), testGoFiles...)
	pkg, err := ctx.ImportDir(abs, 0)
	selected := map[string]bool{}
	var sFiles []string
	if err == nil {
		// CgoFiles are selected too: with CgoEnabled the toolchain puts
		// `import "C"` files there instead of GoFiles.
		for _, f := range pkg.GoFiles {
			selected[f] = true
		}
		for _, f := range pkg.CgoFiles {
			selected[f] = true
		}
		sFiles = pkg.SFiles
	}
	// CGO + LINKNAME over the whole dir (including tests; see scope notes).
	cgo, linkname := false, false
	for _, name := range all {
		p := filepath.Join(abs, name)
		if !cgo && fileHasImportC(p) {
			cgo = true
		}
		if !linkname && fileHasLinkname(p) {
			linkname = true
		}
	}
	// BUILD_TAGS: any source file excluded from the linux/amd64 build by a
	// build-constraint line. (.s files: list dir, subtract selected.)
	asmSelected := len(sFiles) > 0
	buildTags := false
	for _, name := range all {
		if !selected[name] && hasBuildConstraint(filepath.Join(abs, name)) {
			buildTags = true
			break
		}
	}
	if !buildTags {
		if sEntries, err := filepath.Glob(filepath.Join(abs, "*.s")); err == nil {
			sel := map[string]bool{}
			for _, f := range sFiles {
				sel[f] = true
			}
			for _, p := range sEntries {
				base := filepath.Base(p)
				if !sel[base] && hasBuildConstraint(p) {
					buildTags = true
					break
				}
			}
		}
	}
	var reasons []string
	if asmSelected {
		reasons = append(reasons, ReasonASM)
	}
	if cgo {
		reasons = append(reasons, ReasonCGO)
	}
	if linkname {
		reasons = append(reasons, ReasonLinkname)
	}
	if buildTags {
		reasons = append(reasons, ReasonBuildTags)
	}
	// Secondary metric: reasons ignoring _test.go files.
	var ntReasons []string
	if asmSelected {
		ntReasons = append(ntReasons, ReasonASM)
	}
	ntCgo, ntLinkname, ntTags := false, false, false
	for _, name := range goFiles {
		p := filepath.Join(abs, name)
		if !ntCgo && fileHasImportC(p) {
			ntCgo = true
		}
		if !ntLinkname && fileHasLinkname(p) {
			ntLinkname = true
		}
		if !ntTags && !selected[name] && hasBuildConstraint(p) {
			ntTags = true
		}
	}
	if ntCgo {
		ntReasons = append(ntReasons, ReasonCGO)
	}
	if ntLinkname {
		ntReasons = append(ntReasons, ReasonLinkname)
	}
	if ntTags {
		ntReasons = append(ntReasons, ReasonBuildTags)
	}

	res := DirResult{Reasons: reasons, NonTestReasons: ntReasons}
	if len(selected) == 0 {
		res.Class = ClassNotLinuxAmd64
		return res
	}
	if len(reasons) == 0 {
		res.Class = ClassPure
	} else {
		res.Class = ClassCoupled
	}
	return res
}

func runMeasure(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("measure")
	srcRoot := fs.String("src", "", "stdlib source root (default: GOROOT/src)")
	outPath := fs.String("out", "", "output JSON path (required)")
	goVersion := fs.String("go-version", "", "Go version label (default: from `go version`)")
	if err := fs.Parse(args); err != nil {
		return &usageError{fmt.Sprintf("measure: %v", err)}
	}
	if *outPath == "" {
		return &usageError{"measure: --out is required"}
	}
	if *srcRoot == "" {
		out, err := runGoCmd("env", "GOROOT")
		if err != nil {
			return fmt.Errorf("measure: %w", err)
		}
		*srcRoot = filepath.Join(strings.TrimSpace(out), "src")
	}
	if *goVersion == "" {
		out, err := runGoCmd("version")
		if err != nil {
			return fmt.Errorf("measure: %w", err)
		}
		*goVersion = strings.TrimSpace(out)
		*goVersion = strings.TrimPrefix(*goVersion, "go version ")
	}

	ctx := linuxAmd64Context()
	rep := MeasureReport{
		Version:           1,
		GoVersion:         *goVersion,
		Generated:         time.Now().UTC().Format(time.RFC3339),
		Src:               *srcRoot,
		Target:            "linux/amd64",
		Counts:            map[string]int{},
		CountsNonTestOnly: map[string]int{},
		ReasonCounts:      map[string]int{},
	}
	err := walkPopulation(*srcRoot, func(rel, abs string, nonTest, test []string) error {
		r := classifyDir(ctx, abs, nonTest, test)
		r.Path = rel
		rep.Dirs = append(rep.Dirs, r)
		rep.Counts[string(r.Class)]++
		ntClass := ClassPure
		if len(r.NonTestReasons) > 0 {
			ntClass = ClassCoupled
		}
		if r.Class == ClassNotLinuxAmd64 {
			ntClass = ClassNotLinuxAmd64
		}
		rep.CountsNonTestOnly[string(ntClass)]++
		for _, reason := range r.Reasons {
			rep.ReasonCounts[reason]++
		}
		if len(test) > 0 {
			rep.TestBinaries++
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("measure: %w", err)
	}
	rep.Population = len(rep.Dirs)
	sort.Slice(rep.Dirs, func(i, j int) bool { return rep.Dirs[i].Path < rep.Dirs[j].Path })

	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("measure: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(*outPath, data, 0644); err != nil {
		return fmt.Errorf("measure: write %s: %w", *outPath, err)
	}
	fmt.Fprintf(stdout, "measure: %s: population=%d pure=%d coupled=%d not-linux-amd64=%d test_binaries=%d reasons=%v\n",
		*outPath, rep.Population, rep.Counts["pure"], rep.Counts["coupled"],
		rep.Counts["not-linux-amd64"], rep.TestBinaries, rep.ReasonCounts)
	fmt.Fprintf(stdout, "measure: non-test-only: pure=%d coupled=%d not-linux-amd64=%d\n",
		rep.CountsNonTestOnly["pure"], rep.CountsNonTestOnly["coupled"],
		rep.CountsNonTestOnly["not-linux-amd64"])
	return nil
}

func runGoCmd(args ...string) (string, error) {
	out, err := exec.Command(goBinary(), args...).Output()
	if err != nil {
		return "", fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// DriftPct returns the percentage drift of a measured count against the
// SPEC baseline: |measured-spec|/spec*100.
func DriftPct(spec, measured int) float64 {
	if spec == 0 {
		return 0
	}
	d := measured - spec
	if d < 0 {
		d = -d
	}
	return 100 * float64(d) / float64(spec)
}

// DriftReopensBoundary reports whether the coupled-count drift exceeds the
// 15% re-test trigger on P3-D1 (SPEC §2.8 re-measurement; P3-M6 workstream
// 5). The trigger is strict: exactly 15% does not reopen.
func DriftReopensBoundary(specCoupled, measuredCoupled int) bool {
	return DriftPct(specCoupled, measuredCoupled) > 15
}
