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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"diffharness/internal/harness"
)

// File classes. Excluded files carry the reason: "exclude:<reason>".
// ClassNotRun ("notrun:<why>") marks files that are not standalone test
// programs for this target at all (helper packages, no func main, build
// constraints unsatisfiable on linux/amd64): nothing to compare, so not an
// exclusion.
const (
	ClassRun        = "run"
	ClassCompile    = "compile"
	ClassErrorCheck = "errorcheck"
	ClassNotRun     = "notrun"
)

// TFile is one classified test/typeparam file.
type TFile struct {
	Name   string `json:"file"`
	Class  string `json:"class"`
	Detail string `json:"detail"`
	// Expected is the reason code of a reviewed expected-divergence entry
	// (srctest): the file still RUNS, and matching gc makes it STALE.
	Expected string `json:"expected,omitempty"`
	// ExpectedSig is the entry's pinned DiffSignature, if any (G2).
	ExpectedSig string   `json:"expectedSig,omitempty"`
	Srcs        []string `json:"-"`
}

// Exclusion is one entry of the versioned exclusion list (GG-0033).
type Exclusion struct {
	File      string
	Reason    string
	Detail    string
	Signature string
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
			// Reviewed expected divergence: the file still RUNS (global
			// review G4: it used to be dropped as exclude:<reason>, so a
			// stale entry was never reported). EXPECTED while it diverges,
			// STALE once it matches gc. Files that are not programs
			// (notrun) stay notrun.
			tf.Expected = x.Reason
			tf.ExpectedSig = x.Signature
			tf.Detail = "expected divergence (" + ExclusionsFileName() + "): " + x.Detail
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

// LoadExclusions parses the versioned, reason-coded exclusion file via
// the shared harness loader (one vocabulary for every layer, SPEC §5.6):
// an unknown reason code, an empty detail or a duplicate entry is an error.
func LoadExclusions(path string) (version string, excl map[string]Exclusion, err error) {
	v, rx, err := harness.LoadReviewedExclusions(path)
	if err != nil {
		return "", nil, err
	}
	excl = make(map[string]Exclusion, len(rx))
	for k, e := range rx {
		excl[k] = Exclusion{File: e.File, Reason: e.Reason, Detail: e.Detail, Signature: e.Signature}
	}
	return v, excl, nil
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
	Program  string   `json:"program"`
	Same     bool     `json:"same"`
	Reasons  []string `json:"reasons"`
	Expected string   `json:"expected,omitempty"`
	// ExpectedSig is the pinned DiffSignature of the expected divergence.
	ExpectedSig string `json:"expectedSig,omitempty"`
	// Infra marks a comparison that could not produce a verdict (the
	// graalgo batch JVM failed, staging failed): never a pass.
	Infra bool `json:"infra,omitempty"`
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
//
// SPEEDUP (2026-10-14): the graalgo backend runs in a JVM (~17-25s startup
// per invocation). Instead of one JVM per file, each worker writes a batch
// file with all its graalgo invocations and runs them in a single JVM via
// `graalgo-run --batch`. Coverage is identical — the same files run through
// the same code paths, only the JVM startup is amortized.
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
		if !strings.HasPrefix(f.Class, "exclude") && !strings.HasPrefix(f.Class, ClassNotRun) {
			included = append(included, f)
		}
	}

	// Partition files among jobs; each job processes its share with one
	// graalgo JVM via batch mode.
	comps := make([]Comparison, len(included))
	var wg sync.WaitGroup
	for j := 0; j < jobs; j++ {
		wg.Add(1)
		go func(job int) {
			defer wg.Done()
			// Collect this job's files (stride partitioning for balance).
			var mine []int
			for i := job; i < len(included); i += jobs {
				mine = append(mine, i)
			}
			if len(mine) == 0 {
				return
			}
			var results []Comparison
			if alt.Batch {
				results = runBatched(cfg, ref, alt, included, mine, workRoot, job)
			} else {
				results = runPerFile(cfg, ref, alt, included, mine, workRoot, job)
			}
			for k, idx := range mine {
				results[k].Expected = included[idx].Expected
				results[k].ExpectedSig = included[idx].ExpectedSig
				comps[idx] = results[k]
			}
		}(j)
	}
	wg.Wait()
	return comps, nil
}

// batchEntry is one graalgo invocation in a batch file.
type batchEntry struct {
	idx  int    // index into the job's file list
	mode string // "check" or "run"
	src  string // source file path
}

// runBatched processes one job's files: gc backend runs normally (fast),
// graalgo backend runs via a single --batch JVM invocation.
func runBatched(cfg *harness.Config, ref, alt *harness.Backend, included []TFile, mine []int, workRoot string, job int) []Comparison {
	n := len(mine)
	comps := make([]Comparison, n)
	// Stage all files first.
	srcs := make([]string, n)
	dirs := make([]string, n)
	for k, idx := range mine {
		tf := included[idx]
		comps[k] = Comparison{Program: tf.Name, Same: true}
		dir := filepath.Join(workRoot, fmt.Sprintf("job%d", job), tf.Name)
		src, err := stageFile(tf, dir)
		if err != nil {
			comps[k].Same, comps[k].Infra = false, true
			comps[k].Reasons = append(comps[k].Reasons, fmt.Sprintf("infra: stage: %v", err))
			continue
		}
		srcs[k] = src
		dirs[k] = dir
	}

	// Build the batch file for graalgo invocations.
	// Each file needs: check (for build parity) and optionally run.
	var entries []batchEntry
	for k, idx := range mine {
		if srcs[k] == "" {
			continue // stage failed
		}
		tf := included[idx]
		// All classes need the build check.
		entries = append(entries, batchEntry{idx: k, mode: "check", src: srcs[k]})
		// Run class also needs execution.
		if tf.Class == ClassRun {
			entries = append(entries, batchEntry{idx: k, mode: "run", src: srcs[k]})
		}
	}

	// Write batch file.
	batchPath := filepath.Join(workRoot, fmt.Sprintf("job%d-batch.txt", job))
	var batchLines []string
	for _, e := range entries {
		batchLines = append(batchLines, e.mode+":"+e.src)
	}
	if err := os.WriteFile(batchPath, []byte(strings.Join(batchLines, "\n")+"\n"), 0o644); err != nil {
		for k := range comps {
			comps[k].Same = false
			comps[k].Reasons = append(comps[k].Reasons, fmt.Sprintf("batch write: %v", err))
		}
		return comps
	}

	// Run the batch: one JVM for all graalgo invocations in this job.
	batchOut := runGraalgoBatch(cfg, alt, batchPath, workRoot)

	// Now do the gc backend work and combine with batch results.
	for k, idx := range mine {
		if srcs[k] == "" {
			continue
		}
		tf := included[idx]
		prog := harness.Program{Name: tf.Name, Dir: dirs[k], Main: srcs[k]}
		c := &comps[k]
		fail := func(format string, args ...any) {
			c.Same = false
			c.Reasons = append(c.Reasons, fmt.Sprintf(format, args...))
		}

		// gc backend (fast, per-case).
		bRef := harness.BuildOnly(cfg, ref, prog, workRoot)

		// graalgo build result from batch.
		bAlt, ok := batchOut[batchKey("check", srcs[k])]
		if !ok {
			c.Same, c.Infra = false, true
			c.Reasons = append(c.Reasons, "infra: no graalgo batch result (batch JVM failed or output unparsable)")
			continue
		}

		switch tf.Class {
		case ClassCompile:
			if !bRef.BuildOK {
				fail("build: ref failed: %s", firstLines(bRef.BuildLog, 4))
			}
			if !bAlt.BuildOK {
				fail("build: alt failed: %s", firstLines(bAlt.BuildLog, 4))
			}
			continue
		case ClassErrorCheck:
			checkErrorcheck(bRef, bAlt, fail)
			continue
		}

		// ClassRun: full build+run parity.
		if !bRef.BuildOK || !bAlt.BuildOK {
			if !bRef.BuildOK {
				fail("build: ref failed: %s", firstLines(bRef.BuildLog, 4))
			}
			if !bAlt.BuildOK {
				fail("build: alt failed: %s", firstLines(bAlt.BuildLog, 4))
			}
			continue
		}
		oAlt, ok := batchOut[batchKey("run", srcs[k])]
		if !ok {
			// G3: a missing run result used to read as a zero Outcome
			// (exit 0, empty output), which "matched" silent programs.
			c.Same, c.Infra = false, true
			c.Reasons = append(c.Reasons, "infra: no graalgo batch run result (batch JVM failed or output unparsable)")
			continue
		}
		oRef := harness.RunProgram(cfg, ref, prog, workRoot)
		// RunProgram does BuildOnly first; our batch already did the build.
		// For the run outcome, use the batch's run result directly.
		// But we need to construct an Outcome for Compare.
		oAltFull := harness.Outcome{
			Program:  tf.Name,
			Backend:  alt.Name,
			BuildOK:  bAlt.BuildOK,
			BuildLog: bAlt.BuildLog,
			Stdout:   oAlt.Stdout,
			Stderr:   oAlt.Stderr,
			ExitCode: oAlt.ExitCode,
			TimedOut: oAlt.TimedOut,
			Duration: oAlt.Duration,
		}
		hc := harness.Compare(oRef, oAltFull)
		c.Same = hc.Same
		c.Reasons = hc.Reasons
	}
	return comps
}

// runPerFile is runBatched for backends without --batch support (the
// product launcher): one graalgo JVM per build and per run.
func runPerFile(cfg *harness.Config, ref, alt *harness.Backend, included []TFile, mine []int, workRoot string, job int) []Comparison {
	comps := make([]Comparison, len(mine))
	for k, idx := range mine {
		tf := included[idx]
		c := &comps[k]
		*c = Comparison{Program: tf.Name, Same: true}
		fail := func(format string, args ...any) {
			c.Same = false
			c.Reasons = append(c.Reasons, fmt.Sprintf(format, args...))
		}
		dir := filepath.Join(workRoot, fmt.Sprintf("job%d", job), tf.Name)
		src, err := stageFile(tf, dir)
		if err != nil {
			c.Same, c.Infra = false, true
			fail("infra: stage: %v", err)
			continue
		}
		prog := harness.Program{Name: tf.Name, Dir: dir, Main: src}
		switch tf.Class {
		case ClassCompile, ClassErrorCheck:
			bRef := harness.BuildOnly(cfg, ref, prog, workRoot)
			bAlt := harness.BuildOnly(cfg, alt, prog, workRoot)
			if tf.Class == ClassCompile {
				if !bRef.BuildOK {
					fail("build: ref failed: %s", firstLines(bRef.BuildLog, 4))
				}
				if !bAlt.BuildOK {
					fail("build: alt failed: %s", firstLines(bAlt.BuildLog, 4))
				}
			} else {
				checkErrorcheck(bRef, bAlt, fail)
			}
			continue
		}
		hc := harness.Compare(harness.RunProgram(cfg, ref, prog, workRoot), harness.RunProgram(cfg, alt, prog, workRoot))
		c.Same, c.Reasons = hc.Same, hc.Reasons
	}
	return comps
}

// crashRe matches a build log that reports a crash of the tool rather than
// a compile diagnostic: a negative test only passes when graalgo REJECTS
// the program the way a compiler does (G4: a JVM/frontend crash used to
// count as "both builds fail").
var crashRe = regexp.MustCompile(`(?i)exception|internal error|fatal error|panic:|stack overflow|\tat |signal [A-Z]+`)

// checkErrorcheck enforces errorcheck parity: gc rejects the program, and
// graalgo rejects it with a diagnostic — not by crashing.
func checkErrorcheck(bRef, bAlt harness.Outcome, fail func(string, ...any)) {
	if bRef.BuildOK {
		fail("errorcheck: ref built ok, expected failure")
	}
	switch {
	case bAlt.BuildOK:
		fail("errorcheck: alt built ok, expected failure (gc rejects this program)")
	case strings.TrimSpace(bAlt.BuildLog) == "":
		fail("errorcheck: alt failed without a diagnostic")
	case crashRe.MatchString(bAlt.BuildLog):
		fail("errorcheck: alt crashed instead of rejecting: %s", firstLines(bAlt.BuildLog, 4))
	}
}

// overrideEnv applies KEY=VAL overrides onto a base environment.
// (Mirrors harness.overrideEnv, which is package-private.)
func overrideEnv(base []string, overrides map[string]string) []string {
	out := append([]string{}, base...)
	seen := map[string]int{}
	for i, kv := range out {
		if idx := strings.IndexByte(kv, '='); idx > 0 {
			seen[kv[:idx]] = i
		}
	}
	for k, v := range overrides {
		kv := k + "=" + v
		if i, ok := seen[k]; ok {
			out[i] = kv
		} else {
			out = append(out, kv)
		}
	}
	return out
}

// batchKey identifies a batch entry's result.
func batchKey(mode, src string) string {
	return mode + "\x00" + src
}

// batchResult is one entry's outcome from a --batch run.
type batchResult struct {
	Exit   int    `json:"exit"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// runGraalgoBatch invokes graalgo-run --batch and returns outcomes keyed by
// batchKey(mode, src). The graalgo backend's build command is
// {GRAALGO_RUN} --check {SRC}; we replace it with --batch for the whole job.
func runGraalgoBatch(cfg *harness.Config, alt *harness.Backend, batchPath, workRoot string) map[string]harness.Outcome {
	out := make(map[string]harness.Outcome)
	// Render the batch command from the backend's build template, replacing
	// the per-file invocation with --batch.
	// The build template is [{GRAALGO_RUN} --check {SRC}]; for batch we want
	// [{GRAALGO_RUN} --batch <batchfile>].
	var graalgoRun string
	for _, b := range cfg.Backends {
		if b.Name == alt.Name {
			// Extract the launcher path from vars.
			if v, ok := b.Vars["GRAALGO_RUN"]; ok {
				graalgoRun = v
			}
			break
		}
	}
	if graalgoRun == "" {
		// Fallback: try to derive from the build command template.
		cmd := alt.RenderBuild("SRC_PLACEHOLDER", "BIN_PLACEHOLDER")
		if len(cmd) > 0 {
			graalgoRun = cmd[0]
		}
	}
	// Expand vars in graalgoRun (it may contain ${GRAALGO_BACKEND_DIR} etc.).
	// For now, use the rendered build command's first token and replace
	// --check {SRC} with --batch <file>.
	buildCmd := alt.RenderBuild("/tmp/placeholder-src", "/tmp/placeholder-bin")
	if len(buildCmd) == 0 {
		return out
	}
	// Replace the args: [launcher --check src] -> [launcher --batch=batchfile]
	batchCmd := []string{buildCmd[0], "--batch=" + batchPath}
	// Copy env from backend.
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, batchCmd[0], batchCmd[1:]...)
	cmd.Env = overrideEnv(os.Environ(), alt.Env)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Batch failed entirely: every entry is an infrastructure failure
		// (absent from the map -> runBatched marks it Infra).
		return out
	}
	// Parse JSON array. Results are in the same order as the batch file
	// lines (the Java batch processor outputs sequentially).
	var results []struct {
		File   string `json:"file"`
		Exit   int    `json:"exit"`
		Stdout string `json:"stdout"`
		Stderr string `json:"stderr"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		return out
	}
	// Map results by (mode, src) using positional matching: the Nth
	// non-empty batch line corresponds to the Nth result.
	data, _ := os.ReadFile(batchPath)
	resultIdx := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if resultIdx >= len(results) {
			break
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			resultIdx++
			continue
		}
		mode := line[:colon]
		src := line[colon+1:]
		r := results[resultIdx]
		resultIdx++
		// Sanity: the result's file should match the batch line's src.
		// If not, the batch processor reordered output; fall back to
		// file-path matching (may conflate check/run for the same file).
		if r.File != src {
			for _, rr := range results {
				if rr.File == src {
					r = rr
					break
				}
			}
		}
		oc := harness.Outcome{
			Program:  filepath.Base(filepath.Dir(src)),
			Backend:  alt.Name,
			Stdout:   r.Stdout, // exact: stdout normalization is per-program opt-in only
			Stderr:   alt.FilterStderr(harness.Normalize(r.Stderr)),
			ExitCode: r.Exit,
		}
		if mode == "check" {
			oc.BuildOK = r.Exit == 0
			// For build, stdout+stderr combined is the build log.
			// The single-file path puts build errors on stderr.
			oc.BuildLog = oc.Stderr
			if oc.BuildLog == "" {
				oc.BuildLog = oc.Stdout
			}
		}
		out[batchKey(mode, src)] = oc
	}
	return out
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
