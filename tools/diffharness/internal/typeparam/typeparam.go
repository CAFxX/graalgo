// Package typeparam implements the test/typeparam differential adapter
// (M6/D2 acceptance, GG-0033/GG-0014).
//
// Go's test/typeparam tree (266 files at GOROOT/test/typeparam) is the
// pathological-generics corpus the D2 monomorphization design must survive.
// The adapter has two modes, driven by the Java acceptance test
// (TypeparamDifferentialTest):
//
//	scan  classify every file (run | compile | errorcheck | exclude:<reason>)
//	      and emit the scan JSON the exclusion-coverage test consumes.
//	run   differential gc-a vs graalgo over the included files:
//	      - run class:        full build+run parity (harness.Compare)
//	      - compile class:     build parity (both must build)
//	      - errorcheck class:  build-failure parity (both must fail;
//	                           messages are NOT compared — different compilers)
//	      and emit the report JSON the zero-divergence test consumes.
//
// Exclusions are authoritative in the versioned, reason-coded YAML file
// (typeparam-exclusions.yaml, GG-0033). The scanner additionally detects
// structural exclusions (multi-file .dir programs, cgo build constraints)
// and reports them as exclude:<reason>; any such file missing from the
// YAML is surfaced so the list can be completed — the coverage test then
// enforces scan/YAML consistency, valid reason codes, and no stale entries.
package typeparam

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"diffharness/internal/harness"
)

// File classes. Excluded files carry the reason: "exclude:<reason>".
const (
	ClassRun        = "run"
	ClassCompile    = "compile"
	ClassErrorCheck = "errorcheck"
)

// TFile is one classified test/typeparam file.
type TFile struct {
	Name   string   `json:"file"`
	Class  string   `json:"class"`
	Detail string   `json:"detail"`
	Srcs   []string `json:"-"`
}

// Exclusion is one entry of the versioned exclusion list (GG-0033).
type Exclusion struct {
	File   string
	Reason string
	Detail string
}

var directiveRe = regexp.MustCompile(`^//\s*(run|rundir|compile|compiledir|errorcheck|build)\b`)

// Scan classifies every *.go file directly under tpDir. yamlExcl are the
// exclusions from the versioned YAML file; files they name are reported as
// exclude:<reason> regardless of directive.
func Scan(tpDir string, yamlExcl map[string]Exclusion) ([]TFile, error) {
	entries, err := os.ReadDir(tpDir)
	if err != nil {
		return nil, fmt.Errorf("read typeparam dir: %w", err)
	}
	var out []TFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		tf := classify(tpDir, e.Name())
		tf.Srcs = []string{filepath.Join(tpDir, e.Name())}
		if x, ok := yamlExcl[tf.Name]; ok {
			tf.Class = "exclude:" + x.Reason
			tf.Detail = "excluded by " + ExclusionsFileName() + ": " + x.Detail
		}
		out = append(out, tf)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ExclusionsFileName is the conventional YAML file name.
func ExclusionsFileName() string { return "typeparam-exclusions.yaml" }

// classify determines a file's class from its directive comment, multi-file
// layout, and build constraints.
func classify(tpDir, name string) TFile {
	tf := TFile{Name: name}
	base := strings.TrimSuffix(name, ".go")

	// Structural exclusion: multi-file .dir programs. frontend/v1 is
	// single-file by design (package loading is WS G); neither backend
	// invocation in this adapter stages GOPATH packages.
	if st, err := os.Stat(filepath.Join(tpDir, base+".dir")); err == nil && st.IsDir() {
		tf.Class = "exclude:deferred-parity"
		tf.Detail = "multi-file program (" + base + ".dir): frontend/v1 is single-file; package loading is WS G"
		return tf
	}

	directive, constraint := readHeader(filepath.Join(tpDir, name))

	// Structural exclusion: cgo build constraint. graalgo has no cgo.
	if strings.Contains(constraint, "cgo") {
		tf.Class = "exclude:backend-specific"
		tf.Detail = "//go:build " + constraint + ": graalgo has no cgo support"
		return tf
	}

	switch directive {
	case "run", "rundir":
		tf.Class = ClassRun
	case "compile", "compiledir", "build":
		tf.Class = ClassCompile
	case "errorcheck":
		tf.Class = ClassErrorCheck
	default:
		tf.Class = "exclude:backend-specific"
		tf.Detail = "no recognized directive (found " + orNone(directive) + "): not runnable by the gc test driver either"
		return tf
	}
	tf.Detail = "directive // " + directive
	if constraint != "" {
		tf.Detail += " (//go:build " + constraint + ")"
	}
	return tf
}

func orNone(s string) string {
	if s == "" {
		return "<none>"
	}
	return s
}

var goBuildRe = regexp.MustCompile(`^//go:build\s+(.+)$`)

// readHeader returns the test directive and the //go:build constraint
// ("" when absent) from the file's leading comment block.
func readHeader(path string) (directive, constraint string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	for i := 0; i < 40 && sc.Scan(); i++ {
		line := sc.Text()
		if m := goBuildRe.FindStringSubmatch(line); m != nil {
			constraint = strings.TrimSpace(m[1])
			continue
		}
		if m := directiveRe.FindStringSubmatch(line); m != nil {
			directive = m[1]
			// Keep scanning for the //go:build line, which may follow.
			continue
		}
		if !strings.HasPrefix(line, "//") && strings.TrimSpace(line) != "" {
			break
		}
	}
	return directive, constraint
}

// LoadExclusions parses the versioned YAML exclusion file. The format is
// deliberately minimal (no YAML dependency):
//
//	# comments allowed
//	version: 1
//	exclusions:
//	- file: foo.go
//	  reason: deferred-parity
//	  detail: free text
func LoadExclusions(path string) (version string, excl map[string]Exclusion, err error) {
	excl = map[string]Exclusion{}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("read exclusions: %w", err)
	}
	var cur Exclusion
	inEntry := false
	flush := func() {
		if inEntry && cur.File != "" {
			excl[cur.File] = cur
		}
		cur = Exclusion{}
		inEntry = false
	}
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if strings.HasPrefix(t, "version:") {
			version = strings.TrimSpace(strings.TrimPrefix(t, "version:"))
			continue
		}
		if strings.HasPrefix(t, "- file:") {
			flush()
			cur.File = strings.TrimSpace(strings.TrimPrefix(t, "- file:"))
			inEntry = true
		} else if strings.HasPrefix(t, "reason:") && inEntry {
			cur.Reason = strings.TrimSpace(strings.TrimPrefix(t, "reason:"))
		} else if strings.HasPrefix(t, "detail:") && inEntry {
			cur.Detail = strings.TrimSpace(strings.TrimPrefix(t, "detail:"))
		}
	}
	flush()
	if version == "" {
		return "", nil, fmt.Errorf("exclusions file %s has no version: line", path)
	}
	return version, excl, nil
}

// WriteScanJSON writes {"scan": {"files": [...], "total": N}}.
func WriteScanJSON(path string, files []TFile) error {
	doc := map[string]any{
		"files": files, "total": len(files),
	}
	return writeJSON(path, doc)
}

// Comparison is one program's differential verdict.
type Comparison struct {
	Program string   `json:"program"`
	Same    bool     `json:"same"`
	Reasons []string `json:"reasons"`
}

// WriteReportJSON writes {"report": {"summary": {...}, "comparisons": [...]}}.
func WriteReportJSON(path string, comps []Comparison, skipped int) error {
	div := 0
	for _, c := range comps {
		if !c.Same {
			div++
		}
	}
	doc := map[string]any{
		"summary": map[string]int{
			"total":    len(comps),
			"diverged": div,
			"skipped":  skipped,
		},
		"comparisons": comps,
	}
	return writeJSON(path, doc)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Run executes the differential over included files with a worker pool.
// refName/altName select backends from cfg (e.g. "gc-a", "graalgo").
func Run(cfg *harness.Config, files []TFile, refName, altName string, jobs int) ([]Comparison, error) {
	var ref, alt *harness.Backend
	for i := range cfg.Backends {
		b := &cfg.Backends[i]
		switch b.Name {
		case refName:
			ref = b
		case altName:
			alt = b
		}
	}
	if ref == nil || alt == nil {
		return nil, fmt.Errorf("backends %q/%q not found in config", refName, altName)
	}
	workRoot, err := os.MkdirTemp("", "typeparam-work-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workRoot)

	var included []TFile
	for _, f := range files {
		if !strings.HasPrefix(f.Class, "exclude") {
			included = append(included, f)
		}
	}

	comps := make([]Comparison, len(included))
	sem := make(chan struct{}, jobs)
	var wg sync.WaitGroup
	for i, tf := range included {
		wg.Add(1)
		go func(i int, tf TFile) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			comps[i] = runOne(cfg, ref, alt, tf, workRoot)
		}(i, tf)
	}
	wg.Wait()
	return comps, nil
}

// runOne stages one program and runs its class-specific differential.
func runOne(cfg *harness.Config, ref, alt *harness.Backend, tf TFile, workRoot string) Comparison {
	c := Comparison{Program: tf.Name, Same: true}
	fail := func(format string, args ...any) {
		c.Same = false
		c.Reasons = append(c.Reasons, fmt.Sprintf(format, args...))
	}

	// Stage: single-file programs are copied to a scratch dir so the
	// backends' {SRC} token points at a stable path.
	dir := filepath.Join(workRoot, tf.Name)
	src, err := stageFile(tf, dir)
	if err != nil {
		fail("stage: %v", err)
		return c
	}
	prog := harness.Program{Name: tf.Name, Dir: dir, Main: src}

	bRef := harness.BuildOnly(cfg, ref, prog, workRoot)
	bAlt := harness.BuildOnly(cfg, alt, prog, workRoot)

	switch tf.Class {
	case ClassCompile:
		// Build parity: both must build.
		if !bRef.BuildOK {
			fail("build: ref failed: %s", firstLines(bRef.BuildLog, 4))
		}
		if !bAlt.BuildOK {
			fail("build: alt failed: %s", firstLines(bAlt.BuildLog, 4))
		}
		return c
	case ClassErrorCheck:
		// Build-failure parity: both must fail. Messages are NOT
		// compared — gc and graalgo are different compilers.
		if bRef.BuildOK {
			fail("errorcheck: ref built ok, expected failure")
		}
		if bAlt.BuildOK {
			fail("errorcheck: alt built ok, expected failure (gc rejects this program)")
		}
		return c
	}

	// ClassRun: full build+run parity.
	if !bRef.BuildOK || !bAlt.BuildOK {
		if !bRef.BuildOK {
			fail("build: ref failed: %s", firstLines(bRef.BuildLog, 4))
		}
		if !bAlt.BuildOK {
			fail("build: alt failed: %s", firstLines(bAlt.BuildLog, 4))
		}
		return c
	}
	oRef := harness.RunProgram(cfg, ref, prog, workRoot)
	oAlt := harness.RunProgram(cfg, alt, prog, workRoot)
	hc := harness.Compare(oRef, oAlt)
	c.Same = hc.Same
	c.Reasons = hc.Reasons
	return c
}

// stageFile copies the program's single source into dir/main.go.
func stageFile(tf TFile, dir string) (string, error) {
	if len(tf.Srcs) != 1 {
		return "", fmt.Errorf("expected 1 source, have %d", len(tf.Srcs))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "main.go")
	data, err := os.ReadFile(tf.Srcs[0])
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", err
	}
	return dst, nil
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = append(lines[:n], "...")
	}
	out := strings.Join(lines, "\\n")
	if len(out) > 400 {
		out = out[:400] + "…"
	}
	return out
}
