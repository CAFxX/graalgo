// Command mutation validates the differential oracle by mutating graalgo
// itself (SPEC §5.7, GG-0034): deliberately break the lowering/runtime and
// confirm the per-push differential gate (`make snapshot-check`, i.e.
// `diffharness snapshot check` against gc's snapshot) goes red. A gate that
// has never been seen failing is not evidence.
//
// Every mutant runs on a SCRATCH COPY of the repository (rsync, .git and
// scratch dirs excluded) — the working tree is never modified. Per mutant:
//
//  1. apply the exact-string mutation (the 'old' text must occur exactly
//     once in the file, else the mutant is ERROR: ambiguous);
//  2. `make build` in the copy: a compile failure is STILLBORN (not scored);
//  3. run the gate on the -slice programs; red => KILLED;
//  4. a slice survivor is re-run against the FULL corpus (the whole
//     per-push gate) before it is declared SURVIVED;
//  5. restore the file.
//
// Gate exit codes: 0 green, 1 red, 3 infrastructure failure (=> ERROR, never
// counted as a kill). The unmutated copy must be green first (baseline
// control), otherwise every mutant would be trivially "killed".
//
// Score = killed / (killed + survived). Exit status: 0 when every survivor
// carries a survivorJustification in the mutants file, 1 on an unjustified
// survivor, 3 on a red baseline or any ERROR.
//
// Resource discipline: one JVM at a time (-jobs 1), nice -n 19, JVM caps via
// GRAALGO_JAVA_OPTS (default "-Xmx2g -XX:ActiveProcessorCount=4").
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Mutant struct {
	ID            string `json:"id"`
	Class         string `json:"class"`
	File          string `json:"file"` // repo-relative
	Old           string `json:"old"`
	New           string `json:"new"`
	Description   string `json:"description"`
	Justification string `json:"survivorJustification,omitempty"`
}

type Result struct {
	ID          string `json:"id"`
	Class       string `json:"class"`
	Description string `json:"description"`
	Verdict     string `json:"verdict"` // KILLED, SURVIVED, STILLBORN, ERROR
	Stage       string `json:"stage,omitempty"`
	Detail      string `json:"detail"`
	DurationSec int64  `json:"durationSec"`
}

type config struct {
	repo, work, snapshot, expected, slice, javaOpts string
	only                                            map[string]bool
	confirm                                         bool
}

func main() {
	var c config
	var mutantsPath, only, report string
	flag.StringVar(&c.repo, "repo", ".", "repository root to copy")
	flag.StringVar(&mutantsPath, "mutants", "tools/mutation/mutants.json", "mutant spec (relative to -repo)")
	flag.StringVar(&c.work, "work", "", "scratch dir for the repo copy (default: a new temp dir)")
	flag.StringVar(&c.slice, "slice", "", "comma-separated corpus programs for the fast first stage (default: full corpus only)")
	flag.BoolVar(&c.confirm, "confirm", true, "re-run slice survivors against the full corpus")
	flag.StringVar(&only, "only", "", "comma-separated mutant ids to run (default: all)")
	flag.StringVar(&report, "report", "", "JSON report path (default: <work>/report.json)")
	flag.StringVar(&c.javaOpts, "java-opts", "-Xmx2g -XX:ActiveProcessorCount=4", "GRAALGO_JAVA_OPTS for gate runs")
	flag.Parse()

	repo, err := filepath.Abs(c.repo)
	if err != nil {
		fatal(3, err)
	}
	c.repo = repo
	data, err := os.ReadFile(filepath.Join(repo, mutantsPath))
	if err != nil {
		fatal(3, err)
	}
	var spec struct {
		Mutants []Mutant `json:"mutants"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		fatal(3, fmt.Errorf("parse %s: %w", mutantsPath, err))
	}
	if only != "" {
		c.only = map[string]bool{}
		for _, id := range strings.Split(only, ",") {
			c.only[strings.TrimSpace(id)] = true
		}
	}
	if c.work == "" {
		if c.work, err = os.MkdirTemp("", "graalgo-mutation-*"); err != nil {
			fatal(3, err)
		}
	} else if err := os.MkdirAll(c.work, 0o755); err != nil {
		// -work names a scratch dir that may not exist yet (rsync's
		// working directory is c.work, so it must exist before the copy).
		fatal(3, err)
	}
	if report == "" {
		report = filepath.Join(c.work, "report.json")
	}
	copyDir := filepath.Join(c.work, "repo")
	fmt.Printf("mutation: copying %s -> %s\n", repo, copyDir)
	if err := run(c.work, nil, "rsync", "-a", "--delete",
		"--exclude=.git", "--exclude=.tmp/", "--exclude=test-results/", "--exclude=out/test-tmp/",
		repo+"/", copyDir+"/"); err != nil {
		fatal(3, fmt.Errorf("rsync: %w", err))
	}
	c.snapshot = filepath.Join(copyDir, "test/snapshots/go1.27.1.json")
	if snaps, _ := filepath.Glob(filepath.Join(copyDir, "test/snapshots/go*.json")); len(snaps) == 1 {
		c.snapshot = snaps[0]
	}
	c.expected = filepath.Join(copyDir, "test/differential/corpus-exclusions.yaml")

	// Validate every mutant applies exactly once BEFORE spending compute.
	for _, m := range spec.Mutants {
		if c.only != nil && !c.only[m.ID] {
			continue
		}
		src, err := os.ReadFile(filepath.Join(copyDir, m.File))
		if err != nil {
			fatal(3, fmt.Errorf("%s: %w", m.ID, err))
		}
		if n := strings.Count(string(src), m.Old); n != 1 {
			fatal(3, fmt.Errorf("%s: 'old' occurs %d times in %s (must be exactly 1: rot or ambiguity)", m.ID, n, m.File))
		}
	}

	if err := build(copyDir); err != nil {
		fatal(3, fmt.Errorf("baseline build failed: %w", err))
	}
	dh := filepath.Join(c.work, "diffharness")
	if err := run(filepath.Join(copyDir, "test/differential"), nil, goBin(), "build", "-o", dh, "./cmd/diffharness"); err != nil {
		fatal(3, fmt.Errorf("build diffharness: %w", err))
	}
	fmt.Println("mutation: baseline control (unmutated copy must be green)")
	for _, stage := range c.stages() {
		if v, detail := c.gate(copyDir, dh, stage); v != "green" {
			fatal(3, fmt.Errorf("baseline %s gate not green (%s): %s", stageName(stage), v, detail))
		}
	}
	fmt.Println("mutation: baseline GREEN")

	var results []Result
	for _, m := range spec.Mutants {
		if c.only != nil && !c.only[m.ID] {
			continue
		}
		start := time.Now()
		r := c.runMutant(copyDir, dh, m)
		r.DurationSec = int64(time.Since(start).Seconds())
		results = append(results, r)
		fmt.Printf("[%s] %-9s %s %s\n", m.ID, r.Verdict, m.Description, r.Detail)
		if err := writeReport(report, results); err != nil {
			fatal(3, err)
		}
	}
	os.Exit(summarize(results, spec.Mutants, report))
}

func (c *config) stages() []string {
	if c.slice == "" {
		return []string{""}
	}
	if c.confirm {
		return []string{c.slice, ""}
	}
	return []string{c.slice}
}

func stageName(s string) string {
	if s == "" {
		return "full-corpus"
	}
	return "slice"
}

func (c *config) runMutant(copyDir, dh string, m Mutant) Result {
	r := Result{ID: m.ID, Class: m.Class, Description: m.Description}
	path := filepath.Join(copyDir, m.File)
	orig, err := os.ReadFile(path)
	if err != nil {
		r.Verdict, r.Detail = "ERROR", err.Error()
		return r
	}
	mutated := strings.Replace(string(orig), m.Old, m.New, 1)
	if err := os.WriteFile(path, []byte(mutated), 0o644); err != nil {
		r.Verdict, r.Detail = "ERROR", err.Error()
		return r
	}
	defer func() {
		// Restore; the next build sees the newer mtime and recompiles.
		if err := os.WriteFile(path, orig, 0o644); err != nil {
			fatal(3, fmt.Errorf("restore %s: %w", m.File, err))
		}
	}()
	if err := build(copyDir); err != nil {
		r.Verdict, r.Detail = "STILLBORN", "does not compile"
		return r
	}
	stages := []string{""}
	if c.slice != "" {
		stages = []string{c.slice}
		if c.confirm {
			stages = append(stages, "")
		}
	}
	for _, stage := range stages {
		v, detail := c.gate(copyDir, dh, stage)
		switch v {
		case "red":
			r.Verdict, r.Stage, r.Detail = "KILLED", stageName(stage), detail
			return r
		case "infra":
			r.Verdict, r.Stage, r.Detail = "ERROR", stageName(stage), "gate infrastructure failure: "+detail
			return r
		}
	}
	r.Verdict, r.Detail = "SURVIVED", "gate green"
	return r
}

// gate runs the differential gate against the copy; programs=="" is the
// full corpus. Returns green/red/infra and the first failing lines.
func (c *config) gate(copyDir, dh, programs string) (string, string) {
	dhDir := filepath.Join(copyDir, "test/differential")
	tmp := filepath.Join(dhDir, ".tmp")
	os.MkdirAll(tmp, 0o755)
	args := []string{"-n", "19", dh, "snapshot", "check",
		"-config", "backends-p3.json", "-corpus", "corpus", "-backend", "graalgo",
		"-snapshot", c.snapshot, "-expected", c.expected, "-jobs", "1", "-timeout", "120", "-fail-fast"}
	if programs != "" {
		args = append(args, "-programs", programs)
	}
	cmd := exec.Command("nice", args...)
	cmd.Dir = dhDir
	cmd.Env = append(os.Environ(),
		"GRAALGO_BACKEND_DIR="+filepath.Join(copyDir, "internal/backend"),
		"TMPDIR="+tmp, "GOTMPDIR="+tmp,
		"GRAALGO_JAVA_OPTS="+c.javaOpts,
		"GRAALGO_GOFRONTEND="+filepath.Join(copyDir, "internal/frontend/gofrontend"))
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return "green", ""
	case errors.As(err, &ee) && ee.ExitCode() == 1 && reportsDivergence(string(out)):
		return "red", firstFailures(string(out))
	default:
		// Exit 1 without a DRIFT/STALE verdict line is a harness error
		// (bad flags, corpus/snapshot inconsistency, ...), not a kill
		// (global review G5).
		return "infra", firstFailures(string(out)) + " " + err.Error()
	}
}

// reportsDivergence reports whether the gate output carries a divergence
// verdict: the only thing that may count as killing a mutant.
func reportsDivergence(out string) bool {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "DRIFT") || strings.HasPrefix(l, "STALE") {
			return true
		}
	}
	return false
}

func firstFailures(out string) string {
	var keep []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "DRIFT") || strings.HasPrefix(l, "STALE") || strings.HasPrefix(l, "INFRA") || strings.Contains(l, "error:") {
			keep = append(keep, strings.Join(strings.Fields(l), " "))
		}
		if len(keep) == 3 {
			break
		}
	}
	s := strings.Join(keep, " | ")
	if len(s) > 400 {
		s = s[:400] + "…"
	}
	return s
}

func build(copyDir string) error {
	return run(copyDir, nil, "nice", "-n", "19", "make", "-s", "build")
}

func run(dir string, env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		tail := string(out)
		if len(tail) > 2000 {
			tail = tail[len(tail)-2000:]
		}
		return fmt.Errorf("%s %v: %w\n%s", name, args, err, tail)
	}
	return nil
}

func writeReport(path string, results []Result) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func summarize(results []Result, mutants []Mutant, report string) int {
	just := map[string]string{}
	for _, m := range mutants {
		just[m.ID] = m.Justification
	}
	killed, survived, stillborn, errs, unjustified := 0, 0, 0, 0, 0
	for _, r := range results {
		switch r.Verdict {
		case "KILLED":
			killed++
		case "SURVIVED":
			survived++
			if just[r.ID] == "" {
				unjustified++
				fmt.Printf("UNJUSTIFIED SURVIVOR %s: %s\n", r.ID, r.Description)
			} else {
				fmt.Printf("justified survivor %s: %s\n", r.ID, just[r.ID])
			}
		case "STILLBORN":
			stillborn++
		default:
			errs++
		}
	}
	score := 0.0
	if killed+survived > 0 {
		score = float64(killed) / float64(killed+survived)
	}
	fmt.Printf("mutation: killed=%d survived=%d (unjustified=%d) stillborn=%d error=%d score=%.1f%% report=%s\n",
		killed, survived, unjustified, stillborn, errs, 100*score, report)
	switch {
	case errs > 0:
		return 3
	case unjustified > 0:
		return 1
	}
	return 0
}

func goBin() string {
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	return filepath.Join(os.Getenv("HOME"), "tools/go/bin/go")
}

func fatal(code int, err error) {
	fmt.Fprintf(os.Stderr, "mutation: fatal: %v\n", err)
	os.Exit(code)
}
