// Command diffharness snapshot — behavior snapshots (M10 WS I).
//
// A behavior snapshot records graalgo's observable behavior on the
// differential corpus against a pinned Go toolchain (the upgrade baseline):
// per program, the normalized stdout/stderr bytes, exit code, build outcome
// and timeout flag. Future toolchain upgrades or refactors diff against the
// recorded file to detect behavior drift.
//
// Modes:
//
//	snapshot record -config PATH -corpus DIR -backend NAME -out FILE
//	    Run every corpus program under the named backend, normalize the
//	    observables with the same rules the differential comparator uses,
//	    self-consistency-gate them (each program runs -runs times; any
//	    nondeterminism fails the recording instead of entering the
//	    baseline), and write the snapshot JSON to -out.
//
//	snapshot check -config PATH -corpus DIR -backend NAME -snapshot FILE
//	    Re-run every recorded program once and fail on any drift from the
//	    snapshot file. Also fails if the config's exclusion list no longer
//	    matches the snapshot's (coverage changed: re-record deliberately).
//
// Normalization (what is NOT recorded):
//   - run durations (timings are not semantic observables);
//   - machine-specific paths: the corpus root and the launcher path are
//     scrubbed to {CORPUS}/{GRAALGO_RUN}; the harness already scrubs its
//     work root to {WORK};
//   - code addresses, goroutine ids, .go line numbers (harness.Normalize),
//     backend stderr noise (backend stderrFilters, e.g. the Truffle Unsafe
//     warnings).
//
// Deliberately recorded exactly: message text, exit codes, timeout flags,
// build-failure logs (a program that fails to build under graalgo has
// observable behavior too — the failure is the baseline).
package main

import (
	"encoding/json"
	"flag"
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

// snapshotFormatV1 identifies the on-disk format. Bump when the recorded
// fields or the normalization rules change; old snapshots then fail the
// check loudly instead of drifting silently.
const snapshotFormatV1 = "graalgo-behavior-snapshot/v1"

// snapshotNormalizerV1 identifies the normalization rule set applied at
// record time: harness.Normalize plus the backend's stderrFilters from the
// config used for the recording. If either changes, re-record under a new
// normalizer id.
const snapshotNormalizerV1 = "harness.Normalize+backend.stderrFilters/v1"

// SnapshotEntry is the recorded observable behavior of one program.
type SnapshotEntry struct {
	Name     string   `json:"name"`
	Args     []string `json:"args"`
	Stdin    string   `json:"stdin"`
	BuildOK  bool     `json:"buildOk"`
	BuildLog string   `json:"buildLog"`
	ExitCode int      `json:"exitCode"`
	TimedOut bool     `json:"timedOut"`
	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr"`
}

// SnapshotFile is the on-disk behavior snapshot.
type SnapshotFile struct {
	Format        string              `json:"format"`
	Toolchain     string              `json:"toolchain"`
	Backend       string              `json:"backend"`
	GraalgoCommit string              `json:"graalgoCommit"`
	RecordedAt    string              `json:"recordedAt"`
	Corpus        string              `json:"corpus"`
	Normalizer    string              `json:"normalizer"`
	Excluded      []harness.Exclusion `json:"excluded"`
	Programs      []SnapshotEntry     `json:"programs"`
}

func cmdSnapshot(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("snapshot needs a mode: record|check")
	}
	switch args[0] {
	case "record":
		return cmdSnapshotRecord(args[1:])
	case "check":
		return cmdSnapshotCheck(args[1:])
	default:
		return fmt.Errorf("unknown snapshot mode %q (want record|check)", args[0])
	}
}

type snapshotFlags struct {
	config       string
	corpus       string
	corpusLabel  string
	backend      string
	toolchain    string
	commit       string
	out          string
	snapshotPath string
	runs         int
	jobs         int
	timeout      int
}

func baseSnapshotFlags(fs *flag.FlagSet, mode string) *snapshotFlags {
	sf := &snapshotFlags{}
	fs.StringVar(&sf.config, "config", "backends.json", "backend config JSON")
	fs.StringVar(&sf.corpus, "corpus", "corpus", "corpus directory")
	fs.StringVar(&sf.corpusLabel, "corpus-label", "", "repo-relative corpus label recorded in the snapshot (default: -corpus value)")
	fs.StringVar(&sf.backend, "backend", "graalgo", "backend name to snapshot")
	fs.IntVar(&sf.jobs, "jobs", 4, "parallel program runs")
	fs.IntVar(&sf.timeout, "timeout", 0, "per-program timeout seconds (0 = config default)")
	if mode == "record" {
		fs.StringVar(&sf.out, "out", "", "snapshot JSON output path (required)")
		fs.StringVar(&sf.toolchain, "toolchain", "", "toolchain label, e.g. go1.27.1 (default: from `go version`)")
		fs.StringVar(&sf.commit, "graalgo-commit", "unknown", "graalgo git commit the snapshot was recorded from")
		fs.IntVar(&sf.runs, "runs", 2, "runs per program; all must agree (self-consistency gate)")
	} else {
		fs.StringVar(&sf.snapshotPath, "snapshot", "", "snapshot JSON to check against (required)")
	}
	return sf
}

// reGraalgoRun scrubs the launcher path out of recorded text: the absolute
// checkout path is environmental, not behavioral.
var reGraalgoRun = regexp.MustCompile(`\S*/graalgo-run`)

// scrubEnvPaths replaces machine-specific paths with stable tokens so a
// snapshot recorded on one checkout verifies on another.
func scrubEnvPaths(s, corpusRoot string) string {
	if corpusRoot != "" {
		s = strings.ReplaceAll(s, corpusRoot, "{CORPUS}")
	}
	s = reGraalgoRun.ReplaceAllString(s, "{GRAALGO_RUN}")
	return s
}

func findBackend(cfg *harness.Config, name string) (*harness.Backend, error) {
	for i := range cfg.Backends {
		if cfg.Backends[i].Name == name {
			return &cfg.Backends[i], nil
		}
	}
	names := make([]string, len(cfg.Backends))
	for i, b := range cfg.Backends {
		names[i] = b.Name
	}
	return nil, fmt.Errorf("backend %q not in config (have: %s)", name, strings.Join(names, ", "))
}

// entryFromOutcome converts a harness outcome into a snapshot entry,
// applying the machine-path scrubbing on top of the harness normalization.
func entryFromOutcome(prog harness.Program, oc harness.Outcome, corpusRoot string) SnapshotEntry {
	return SnapshotEntry{
		Name:     prog.Name,
		Args:     []string{},
		Stdin:    "",
		BuildOK:  oc.BuildOK,
		BuildLog: scrubEnvPaths(oc.BuildLog, corpusRoot),
		ExitCode: oc.ExitCode,
		TimedOut: oc.TimedOut,
		Stdout:   scrubEnvPaths(oc.Stdout, corpusRoot),
		Stderr:   scrubEnvPaths(oc.Stderr, corpusRoot),
	}
}

// diffEntries compares two entries field by field, returning a compact
// human-readable description of the first difference ("" when equal).
func diffEntries(want, got SnapshotEntry) string {
	if want.BuildOK != got.BuildOK {
		return fmt.Sprintf("buildOk: want=%v got=%v", want.BuildOK, got.BuildOK)
	}
	if want.TimedOut != got.TimedOut {
		return fmt.Sprintf("timedOut: want=%v got=%v", want.TimedOut, got.TimedOut)
	}
	if want.ExitCode != got.ExitCode {
		return fmt.Sprintf("exitCode: want=%d got=%d", want.ExitCode, got.ExitCode)
	}
	if d := diffText("stdout", want.Stdout, got.Stdout); d != "" {
		return d
	}
	if d := diffText("stderr", want.Stderr, got.Stderr); d != "" {
		return d
	}
	if d := diffText("buildLog", want.BuildLog, got.BuildLog); d != "" {
		return d
	}
	return ""
}

func diffText(field, a, b string) string {
	if a == b {
		return ""
	}
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) && i < len(bl); i++ {
		if al[i] != bl[i] {
			return fmt.Sprintf("%s differs at line %d: want=%q got=%q",
				field, i+1, trunc(al[i], 160), trunc(bl[i], 160))
		}
	}
	n := min(len(al), len(bl))
	var extra string
	if len(al) > len(bl) {
		extra = trunc(al[n], 160)
	} else {
		extra = trunc(bl[n], 160)
	}
	return fmt.Sprintf("%s differs at line %d: length differs, extra=%q", field, n+1, extra)
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// runAll executes every program under the backend, jobs-at-a-time, and
// returns the outcomes in program order.
func runAll(cfg *harness.Config, b *harness.Backend, progs []harness.Program, workRoot string, jobs int) []harness.Outcome {
	outs := make([]harness.Outcome, len(progs))
	if jobs < 1 {
		jobs = 1
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, jobs)
	for i, p := range progs {
		wg.Add(1)
		go func(i int, p harness.Program) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			outs[i] = harness.RunProgram(cfg, b, p, workRoot)
		}(i, p)
	}
	wg.Wait()
	return outs
}

func goToolchainVersion() string {
	out, err := exec.Command("go", "version").Output()
	if err != nil {
		return "unknown"
	}
	// "go version go1.27.1 linux/amd64" -> "go1.27.1"
	f := strings.Fields(string(out))
	if len(f) >= 3 {
		return f[2]
	}
	return "unknown"
}

func loadSnapshotDeps(sf *snapshotFlags) (*harness.Config, *harness.Backend, []harness.Program, []harness.Exclusion, error) {
	cfg, err := harness.LoadConfig(sf.config)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if sf.timeout > 0 {
		cfg.TimeoutSec = sf.timeout
	}
	b, err := findBackend(cfg, sf.backend)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	progs, err := harness.DiscoverCorpus(sf.corpus)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	kept, skipped := harness.ApplyExclusions(progs, cfg)
	return cfg, b, kept, skipped, nil
}

func cmdSnapshotRecord(args []string) error {
	fs := flag.NewFlagSet("snapshot record", flag.ExitOnError)
	sf := baseSnapshotFlags(fs, "record")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if sf.out == "" {
		return fmt.Errorf("-out is required")
	}
	if sf.runs < 1 {
		return fmt.Errorf("-runs must be >= 1")
	}
	if sf.toolchain == "" {
		sf.toolchain = goToolchainVersion()
	}
	label := sf.corpusLabel
	if label == "" {
		label = sf.corpus
	}
	cfg, b, progs, skipped, err := loadSnapshotDeps(sf)
	if err != nil {
		return err
	}
	corpusRoot, err := filepath.Abs(sf.corpus)
	if err != nil {
		return fmt.Errorf("resolve corpus dir: %w", err)
	}
	workRoot, err := os.MkdirTemp("", "diffharness-snapshot-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workRoot)

	snap := SnapshotFile{
		Format:        snapshotFormatV1,
		Toolchain:     sf.toolchain,
		Backend:       b.Name,
		GraalgoCommit: sf.commit,
		RecordedAt:    time.Now().UTC().Format(time.RFC3339),
		Corpus:        label,
		Normalizer:    snapshotNormalizerV1,
		Excluded:      skipped,
	}
	if snap.Excluded == nil {
		snap.Excluded = []harness.Exclusion{}
	}
	// Self-consistency gate: run every program -runs times; any
	// nondeterminism fails the recording instead of entering the baseline.
	for r := 0; r < sf.runs; r++ {
		outs := runAll(cfg, b, progs, workRoot, sf.jobs)
		for i, p := range progs {
			e := entryFromOutcome(p, outs[i], corpusRoot)
			if r == 0 {
				snap.Programs = append(snap.Programs, e)
				continue
			}
			if d := diffEntries(snap.Programs[i], e); d != "" {
				return fmt.Errorf("self-consistency gate: program %s nondeterministic between run 1 and run %d: %s",
					p.Name, r+1, d)
			}
		}
		fmt.Printf("record: run %d/%d complete (%d programs)\n", r+1, sf.runs, len(progs))
	}
	if err := os.MkdirAll(filepath.Dir(sf.out), 0o755); err != nil {
		return err
	}
	if err := writeJSON(sf.out, snap); err != nil {
		return err
	}
	fmt.Printf("record: wrote %d programs (+%d excluded) to %s [toolchain=%s backend=%s commit=%s]\n",
		len(snap.Programs), len(snap.Excluded), sf.out, snap.Toolchain, snap.Backend, snap.GraalgoCommit)
	return nil
}

func cmdSnapshotCheck(args []string) error {
	fs := flag.NewFlagSet("snapshot check", flag.ExitOnError)
	sf := baseSnapshotFlags(fs, "check")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if sf.snapshotPath == "" {
		return fmt.Errorf("-snapshot is required")
	}
	data, err := os.ReadFile(sf.snapshotPath)
	if err != nil {
		return fmt.Errorf("read snapshot: %w", err)
	}
	var snap SnapshotFile
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("parse snapshot: %w", err)
	}
	if snap.Format != snapshotFormatV1 {
		return fmt.Errorf("snapshot format %q != this tool's %q: re-record the snapshot",
			snap.Format, snapshotFormatV1)
	}
	cfg, b, progs, skipped, err := loadSnapshotDeps(sf)
	if err != nil {
		return err
	}
	// Coverage gate: the exclusion list is part of the baseline. A program
	// silently leaving (or joining) the snapshot is drift.
	if d := diffExclusions(snap.Excluded, skipped); d != "" {
		return fmt.Errorf("exclusion list drift: %s (re-record the snapshot if deliberate)", d)
	}
	want := map[string]SnapshotEntry{}
	for _, e := range snap.Programs {
		want[e.Name] = e
	}
	var missing []string
	var live []harness.Program
	for _, p := range progs {
		if _, ok := want[p.Name]; !ok {
			missing = append(missing, p.Name)
			continue
		}
		live = append(live, p)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("corpus programs with no recorded snapshot: %s", strings.Join(missing, ", "))
	}
	corpusRoot, err := filepath.Abs(sf.corpus)
	if err != nil {
		return fmt.Errorf("resolve corpus dir: %w", err)
	}
	workRoot, err := os.MkdirTemp("", "diffharness-snapshot-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workRoot)

	outs := runAll(cfg, b, live, workRoot, sf.jobs)
	drifted := 0
	for i, p := range live {
		got := entryFromOutcome(p, outs[i], corpusRoot)
		if d := diffEntries(want[p.Name], got); d != "" {
			drifted++
			fmt.Printf("DRIFT %-28s %s\n", p.Name, d)
		}
	}
	// Programs recorded but no longer in the corpus are drift too.
	for _, e := range snap.Programs {
		found := false
		for _, p := range progs {
			if p.Name == e.Name {
				found = true
				break
			}
		}
		if !found {
			drifted++
			fmt.Printf("DRIFT %-28s recorded program missing from corpus\n", e.Name)
		}
	}
	fmt.Printf("check: %d programs, %d drifted [snapshot toolchain=%s backend=%s commit=%s recorded=%s]\n",
		len(live), drifted, snap.Toolchain, snap.Backend, snap.GraalgoCommit, snap.RecordedAt)
	if drifted > 0 {
		return fmt.Errorf("%d programs drifted from the snapshot", drifted)
	}
	return nil
}

// diffExclusions reports a difference between the snapshot's recorded
// exclusion list and the config's current one, order-insensitively.
func diffExclusions(want, got []harness.Exclusion) string {
	key := func(e harness.Exclusion) string { return e.Program + "\x00" + e.Reason }
	wm, gm := map[string]bool{}, map[string]bool{}
	for _, e := range want {
		wm[key(e)] = true
	}
	for _, e := range got {
		gm[key(e)] = true
	}
	var onlyWant, onlyGot []string
	for k := range wm {
		if !gm[k] {
			onlyWant = append(onlyWant, k)
		}
	}
	for k := range gm {
		if !wm[k] {
			onlyGot = append(onlyGot, k)
		}
	}
	if len(onlyWant) == 0 && len(onlyGot) == 0 {
		return ""
	}
	sort.Strings(onlyWant)
	sort.Strings(onlyGot)
	return fmt.Sprintf("snapshot-only=%v current-only=%v", onlyWant, onlyGot)
}
