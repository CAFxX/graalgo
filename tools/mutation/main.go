// Command mutation validates the differential oracle of the new path by
// mutating graalgo itself (SPEC §5.7, GG-0034; m3-plan G6): deliberately
// break the node package, the runtime, the scheduler, graalgo.syscall, the
// -toolexec emitter or the standard-library fork and confirm the per-push
// differential gates go red. A gate that has never been seen failing is not
// evidence.
//
// Every mutant runs on a SCRATCH COPY of the repository (rsync; .git,
// scratch dirs and the lane worktrees under .worktrees/ excluded): the
// working tree is never modified. Per mutant:
//
//  1. apply the exact-string mutation (the 'old' text must occur exactly
//     once in the file, else the run stops before spending compute);
//  2. rebuild: `make build` (the Java modules; it also recompiles the file
//     the previous mutant restored), plus `go build ./...` for a mutant in
//     internal/toolexec (the gates rebuild graalgo-tool and the stdlib/ fork
//     overlay from source themselves); a compile failure is STILLBORN (not
//     scored);
//  3. run the mutant's `kill` programs, grouped per gate (snapshot-check
//     corpus names, test/m1a, m1r, m1c, m1d, m1w13 programs): the same
//     scripts and comparison the per-push gates run, narrowed to those
//     programs; red => KILLED;
//  4. a survivor of its own slice is re-run against every per-push
//     differential gate in full before it is declared SURVIVED;
//  5. restore the file.
//
// Gate verdicts: exit 0 green; exit 1 WITH a divergence line (DRIFT/STALE
// from the diffharness, "FAIL <program>" from the m1 runners) red; anything
// else is an infrastructure failure (=> ERROR, never counted as a kill).
// The unmutated copy must be green first on every gate slice the mutants
// use (baseline control), and on the full gates before the first
// confirmation, otherwise a mutant would be trivially "killed".
//
// Score = killed / (killed + survived). Exit status: 0 when every survivor
// carries a survivorJustification in the mutants file, 1 on an unjustified
// survivor, 3 on a red baseline or any ERROR.
//
// Resource discipline: nice -n 19, JVM caps via GRAALGO_JAVA_OPTS (default
// "-Xmx2g -XX:ActiveProcessorCount=4"); -jobs is the snapshot gate's
// program/JVM parallelism (1 on a shared host).
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Mutant struct {
	ID            string   `json:"id"`
	Class         string   `json:"class"`
	File          string   `json:"file"` // repo-relative
	Old           string   `json:"old"`
	New           string   `json:"new"`
	Description   string   `json:"description"`
	Kill          []string `json:"kill"` // gate:program, e.g. m1a:test/m1a/programs/x.go, snapshot:57_defer_order
	Justification string   `json:"survivorJustification,omitempty"`
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

// gates are the per-push differential gates a kill spec may name, in the
// order a slice runs them; "" programs = the whole gate.
var gates = []string{"snapshot", "m1a", "m1r", "m1c", "m1d", "m1w13"}

type config struct {
	repo, copyDir, work, javaOpts string
	jobs                          int
	fullBaseline                  string // "", "green" or the failure
}

func main() {
	var c config
	var mutantsPath, only, report string
	flag.StringVar(&c.repo, "repo", ".", "repository root to copy")
	flag.StringVar(&mutantsPath, "mutants", "tools/mutation/mutants.json", "mutant spec (relative to -repo)")
	flag.StringVar(&c.work, "work", "", "scratch dir for the repo copy (default: a new temp dir)")
	flag.StringVar(&only, "only", "", "comma-separated mutant ids to run (default: all)")
	flag.StringVar(&report, "report", "", "JSON report path (default: <work>/report.json)")
	flag.IntVar(&c.jobs, "jobs", 1, "programs the snapshot gate runs at a time (1 on a shared host; CI's 4-vCPU runner: 4)")
	flag.StringVar(&c.javaOpts, "java-opts", "-Xmx2g -XX:ActiveProcessorCount=4", "GRAALGO_JAVA_OPTS for gate runs")
	flag.Parse()

	repo, err := filepath.Abs(c.repo)
	if err != nil {
		fatal(3, err)
	}
	c.repo = repo
	mutants, err := load(filepath.Join(repo, mutantsPath))
	if err != nil {
		fatal(3, err)
	}
	if only != "" {
		want := map[string]bool{}
		for _, id := range strings.Split(only, ",") {
			want[strings.TrimSpace(id)] = true
		}
		var sel []Mutant
		for _, m := range mutants {
			if want[m.ID] {
				sel = append(sel, m)
			}
		}
		mutants = sel
	}
	if c.work == "" {
		if c.work, err = os.MkdirTemp("", "graalgo-mutation-*"); err != nil {
			fatal(3, err)
		}
	} else if err := os.MkdirAll(c.work, 0o755); err != nil {
		fatal(3, err)
	}
	if report == "" {
		report = filepath.Join(c.work, "report.json")
	}
	c.copyDir = filepath.Join(c.work, "repo")
	fmt.Printf("mutation: copying %s -> %s\n", repo, c.copyDir)
	if err := run(c.work, nil, "rsync", "-a", "--delete",
		"--exclude=.git", "--exclude=.tmp/", "--exclude=/.worktrees/", "--exclude=test-results/", "--exclude=out/test-tmp/",
		repo+"/", c.copyDir+"/"); err != nil {
		fatal(3, fmt.Errorf("rsync: %w", err))
	}

	// Validate every mutant applies exactly once BEFORE spending compute.
	for _, m := range mutants {
		src, err := os.ReadFile(filepath.Join(c.copyDir, m.File))
		if err != nil {
			fatal(3, fmt.Errorf("%s: %w", m.ID, err))
		}
		if n := strings.Count(string(src), m.Old); n != 1 {
			fatal(3, fmt.Errorf("%s: 'old' occurs %d times in %s (must be exactly 1: rot or ambiguity)", m.ID, n, m.File))
		}
	}

	if err := run(c.copyDir, nil, "nice", "-n", "19", "make", "-s", "build"); err != nil {
		fatal(3, fmt.Errorf("baseline build failed: %w", err))
	}
	fmt.Println("mutation: baseline control (the unmutated copy must be green on every slice)")
	union := map[string][]string{}
	for _, m := range mutants {
		for g, progs := range slices(m.Kill) {
			union[g] = append(union[g], progs...)
		}
	}
	for _, g := range gates {
		if progs, ok := union[g]; ok {
			if v, detail := c.gate(g, dedup(progs)); v != "green" {
				fatal(3, fmt.Errorf("baseline %s slice not green (%s): %s", g, v, detail))
			}
		}
	}
	fmt.Println("mutation: baseline GREEN")

	var results []Result
	for _, m := range mutants {
		start := time.Now()
		r := c.runMutant(m)
		r.DurationSec = int64(time.Since(start).Seconds())
		results = append(results, r)
		fmt.Printf("[%s] %-9s %3ds %s %s\n", m.ID, r.Verdict, r.DurationSec, m.Description, r.Detail)
		if err := writeReport(report, results); err != nil {
			fatal(3, err)
		}
	}
	os.Exit(summarize(results, mutants, report))
}

// load parses and validates the mutant spec: unique ids, a class, at least
// one kill spec per mutant, each naming a known gate.
func load(path string) ([]Mutant, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var spec struct {
		Mutants []Mutant `json:"mutants"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	seen := map[string]bool{}
	for _, m := range spec.Mutants {
		if m.ID == "" || seen[m.ID] {
			return nil, fmt.Errorf("mutant id %q empty or duplicated", m.ID)
		}
		seen[m.ID] = true
		if m.Class == "" || m.File == "" || m.Old == "" || m.Old == m.New {
			return nil, fmt.Errorf("%s: class, file and a changing old/new are required", m.ID)
		}
		if len(m.Kill) == 0 {
			return nil, fmt.Errorf("%s: no kill programs", m.ID)
		}
		for _, k := range m.Kill {
			g, prog, ok := strings.Cut(k, ":")
			if !ok || prog == "" || !known(g) {
				return nil, fmt.Errorf("%s: bad kill spec %q (want <gate>:<program>, gate one of %v)", m.ID, k, gates)
			}
		}
	}
	return spec.Mutants, nil
}

func known(g string) bool {
	for _, x := range gates {
		if x == g {
			return true
		}
	}
	return false
}

func slices(kill []string) map[string][]string {
	out := map[string][]string{}
	for _, k := range kill {
		g, prog, _ := strings.Cut(k, ":")
		out[g] = append(out[g], prog)
	}
	return out
}

func dedup(xs []string) []string {
	sort.Strings(xs)
	var out []string
	for i, x := range xs {
		if i == 0 || x != xs[i-1] {
			out = append(out, x)
		}
	}
	return out
}

func (c *config) runMutant(m Mutant) Result {
	r := Result{ID: m.ID, Class: m.Class, Description: m.Description}
	path := filepath.Join(c.copyDir, m.File)
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
	if err := c.build(m.File); err != nil {
		r.Verdict, r.Detail = "STILLBORN", "does not compile"
		return r
	}
	sl := slices(m.Kill)
	for _, g := range gates {
		progs, ok := sl[g]
		if !ok {
			continue
		}
		if v, detail := c.verdict(&r, "slice", g, progs); v != "green" {
			r.Detail = detail
			return r
		}
	}
	// Survived its slice: the whole per-push differential set decides.
	if c.fullBaseline == "" {
		c.fullBaseline = "green"
		if err := os.WriteFile(path, orig, 0o644); err != nil {
			fatal(3, err)
		}
		if err := c.build(m.File); err != nil {
			fatal(3, fmt.Errorf("rebuild of the unmutated copy failed: %w", err))
		}
		for _, g := range gates {
			if v, detail := c.gate(g, nil); v != "green" {
				fatal(3, fmt.Errorf("baseline full %s gate not green (%s): %s", g, v, detail))
			}
		}
		if err := os.WriteFile(path, []byte(mutated), 0o644); err != nil {
			fatal(3, err)
		}
		if err := c.build(m.File); err != nil {
			fatal(3, fmt.Errorf("rebuild of mutant %s failed: %w", m.ID, err))
		}
	}
	for _, g := range gates {
		if v, detail := c.verdict(&r, "full", g, nil); v != "green" {
			r.Detail = detail
			return r
		}
	}
	r.Verdict, r.Detail = "SURVIVED", "every per-push differential gate green"
	return r
}

// verdict runs one gate for a mutant and records KILLED or ERROR in r.
func (c *config) verdict(r *Result, stage, g string, progs []string) (string, string) {
	v, detail := c.gate(g, progs)
	switch v {
	case "red":
		r.Verdict, r.Stage = "KILLED", stage+" "+g
	case "infra":
		r.Verdict, r.Stage = "ERROR", stage+" "+g
		detail = "gate infrastructure failure: " + detail
	}
	return v, detail
}

// build brings the copy's build up to date with its sources: `make build`
// always (a no-op when nothing changed, and it recompiles the Java file the
// previous mutant restored: without it a Go mutant would run on the previous
// Java mutant's classes), plus the compile check of internal/toolexec for a
// mutant there. The gates build graalgo-tool, the stub and the stdlib/ fork
// overlay from source on every run.
func (c *config) build(file string) error {
	if err := run(c.copyDir, nil, "nice", "-n", "19", "make", "-s", "build"); err != nil {
		return err
	}
	if strings.HasPrefix(file, "internal/toolexec/") {
		return run(filepath.Join(c.copyDir, "internal/toolexec"), nil, "nice", "-n", "19", goBin(), "build", "./...")
	}
	return nil
}

// gate runs one per-push differential gate on the copy, narrowed to progs
// (nil: the whole gate). Returns green/red/infra and the first failing lines.
func (c *config) gate(g string, progs []string) (string, string) {
	tmp := filepath.Join(c.copyDir, ".tmp")
	os.MkdirAll(tmp, 0o755)
	env := append(os.Environ(),
		"GO="+goBin(),
		"GRAALGO_JAVA_OPTS="+c.javaOpts,
		"GC_ORACLE_CACHE="+filepath.Join(c.work, "gc-oracle"),
		"TMPDIR="+tmp, "GOTMPDIR="+tmp)
	var cmd *exec.Cmd
	if g == "snapshot" {
		snap, err := snapshotFile(c.copyDir)
		if err != nil {
			return "infra", err.Error()
		}
		args := []string{"-n", "19", "bash", "newpath.sh", "snapshot", "check",
			"-config", "backends-newpath.json", "-corpus", "corpus", "-backend", "graalgo",
			"-snapshot", snap, "-expected", "corpus-exclusions.yaml",
			"-jobs", strconv.Itoa(max(c.jobs, 1)), "-timeout", "120", "-fail-fast"}
		if len(progs) > 0 {
			args = append(args, "-programs", strings.Join(progs, ","))
		}
		cmd = exec.Command("nice", args...)
		cmd.Dir = filepath.Join(c.copyDir, "test/differential")
		env = append(env,
			"GRAALGO_BACKEND_DIR="+filepath.Join(c.copyDir, "internal/backend"),
			"DH_GO="+filepath.Dir(filepath.Dir(goBin())))
	} else {
		args := append([]string{"-n", "19", "test/" + g + "/run.sh"}, progs...)
		cmd = exec.Command("nice", args...)
		cmd.Dir = c.copyDir
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return "green", ""
	case errors.As(err, &ee) && ee.ExitCode() == 1 && reportsDivergence(string(out)):
		return "red", firstFailures(string(out))
	default:
		// Exit 1 without a divergence line is a harness error (bad flags,
		// corpus/snapshot inconsistency, ...), not a kill (global review G5).
		return "infra", firstFailures(string(out)) + " " + err.Error()
	}
}

func snapshotFile(copyDir string) (string, error) {
	snaps, _ := filepath.Glob(filepath.Join(copyDir, "test/snapshots/go*.json"))
	if len(snaps) != 1 {
		return "", fmt.Errorf("want exactly one gc snapshot in test/snapshots, found %d", len(snaps))
	}
	return snaps[0], nil
}

// reportsDivergence reports whether the gate output carries a divergence
// verdict: the only thing that may count as killing a mutant.
func reportsDivergence(out string) bool {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "DRIFT") || strings.HasPrefix(l, "STALE") || strings.HasPrefix(l, "FAIL ") {
			return true
		}
	}
	return false
}

func firstFailures(out string) string {
	var keep []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "DRIFT") || strings.HasPrefix(l, "STALE") || strings.HasPrefix(l, "FAIL ") ||
			strings.HasPrefix(l, "INFRA") || strings.HasPrefix(l, "RETRY") || strings.Contains(l, "error:") {
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
	if p := os.Getenv("GO"); p != "" {
		return p
	}
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	return filepath.Join(os.Getenv("HOME"), "tools/go/bin/go")
}

func fatal(code int, err error) {
	fmt.Fprintf(os.Stderr, "mutation: fatal: %v\n", err)
	os.Exit(code)
}
