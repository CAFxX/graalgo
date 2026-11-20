// Command diffharness snapshot — behavior snapshots (SPEC §5.9, GG-0036).
//
// A behavior snapshot records the REFERENCE toolchain's (gc's) observable
// behavior on the differential corpus: per program, the normalized
// stdout/stderr, exit code, build outcome, timeout flag, plus the SHA-256
// of the program source (content addressing: an edited program without a
// re-record fails loudly). graalgo is then checked against gc's snapshot —
// graalgo's own output is never the baseline.
//
// Modes:
//
//	snapshot record -config PATH -corpus DIR -backend gc-a -out FILE
//	    Run every corpus program under the reference backend -runs times
//	    (self-consistency gate: any nondeterminism fails the recording)
//	    and write the snapshot JSON.
//
//	snapshot check -config PATH -corpus DIR -backend graalgo -snapshot FILE
//	              -expected corpus-exclusions.yaml [-programs a,b] [-verify-ref gc-a]
//	    Run the corpus (or the -programs slice) under the backend and
//	    compare against the gc snapshot. Verdicts per program:
//	      MATCH     same as gc
//	      DRIFT     differs from gc, not listed as an expected divergence  -> fail
//	      EXPECTED  differs from gc, listed (reason-coded, reviewed)       -> ok
//	      STALE     listed as expected divergence but now matches gc       -> fail
//	                (prune the entry: a shrinking list is the health metric)
//	      TIMING    listed `timing: true` (outcome depends on host speed)
//	                and matches gc: a pass, not STALE
//	      INFRA     the run failed for an infrastructure reason (e.g. a
//	                corrupted frontend export) on every retry               -> fail, exit 3
//	    With -verify-ref, the reference backend is re-run too and any
//	    difference from the snapshot is REF-DRIFT (upstream behavior
//	    change or an unrecorded corpus edit) -> fail.
//
// Normalization: harness.Normalize (goroutine ids, trace addresses) and
// per-program opt-in stdout rules, plus backend noise filters; on top of
// that machine-specific paths (the corpus root, launcher path) are
// scrubbed to {CORPUS}/{GRAALGO_RUN}.
package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	"unicode/utf8"

	"diffharness/internal/harness"
)

// snapshotFormat identifies the on-disk format. v2: recorded under the
// reference toolchain, content-addressed by source hash, no baked-in
// exclusions (those live in the reviewed expected-divergence file).
const snapshotFormat = "graalgo-behavior-snapshot/v2"

// snapshotNormalizer identifies the normalization rule set applied at
// record time, derived from the rules' content (harness.NormalizerID): any
// rule change makes an old snapshot fail loudly until re-recorded.
var snapshotNormalizer = harness.NormalizerID() + "+NORMALIZE-opt-in"

// SnapshotEntry is the recorded observable behavior of one program.
type SnapshotEntry struct {
	Name         string `json:"name"`
	SourceSHA256 string `json:"sourceSha256"`
	BuildOK      bool   `json:"buildOk"`
	BuildLog     string `json:"buildLog"`
	ExitCode     int    `json:"exitCode"`
	TimedOut     bool   `json:"timedOut"`
	Stdout       string `json:"stdout"`
	Stderr       string `json:"stderr"`
	// Output that is not valid UTF-8 cannot round-trip through a JSON
	// string (encoding/json substitutes U+FFFD, silently changing the
	// recorded bytes); it is stored base64-encoded here instead and the
	// text field is left empty. See encodeBytes/decodeBytes.
	StdoutBase64 string `json:"stdoutBase64,omitempty"`
	StderrBase64 string `json:"stderrBase64,omitempty"`
}

// encodeBytes moves non-UTF-8 output into the base64 fields (on write).
func (e *SnapshotEntry) encodeBytes() {
	if !utf8.ValidString(e.Stdout) {
		e.StdoutBase64, e.Stdout = base64.StdEncoding.EncodeToString([]byte(e.Stdout)), ""
	}
	if !utf8.ValidString(e.Stderr) {
		e.StderrBase64, e.Stderr = base64.StdEncoding.EncodeToString([]byte(e.Stderr)), ""
	}
}

// decodeBytes restores base64-stored output (on read).
func (e *SnapshotEntry) decodeBytes() error {
	for _, p := range []struct{ b64, dst *string }{{&e.StdoutBase64, &e.Stdout}, {&e.StderrBase64, &e.Stderr}} {
		if *p.b64 == "" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(*p.b64)
		if err != nil {
			return fmt.Errorf("program %s: bad base64 output: %w", e.Name, err)
		}
		*p.dst, *p.b64 = string(raw), ""
	}
	return nil
}

// SnapshotFile is the on-disk behavior snapshot.
type SnapshotFile struct {
	Format     string          `json:"format"`
	Toolchain  string          `json:"toolchain"`
	Reference  string          `json:"reference"`
	RecordedAt string          `json:"recordedAt"`
	Corpus     string          `json:"corpus"`
	Normalizer string          `json:"normalizer"`
	Programs   []SnapshotEntry `json:"programs"`
}

// errInfra marks an infrastructure failure (exit status 3 from main).
var errInfra = errors.New("infrastructure failure")

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
	out          string
	snapshotPath string
	expected     string
	programs     string
	verifyRef    string
	runs         int
	jobs         int
	timeout      int
	retries      int
	retryBudget  int
	failFast     bool
}

func baseSnapshotFlags(fs *flag.FlagSet, mode string) *snapshotFlags {
	sf := &snapshotFlags{}
	fs.StringVar(&sf.config, "config", "backends.json", "backend config JSON")
	fs.StringVar(&sf.corpus, "corpus", "corpus", "corpus directory")
	fs.IntVar(&sf.jobs, "jobs", 1, "parallel program runs")
	fs.IntVar(&sf.timeout, "timeout", 0, "per-program timeout seconds (0 = config default)")
	if mode == "record" {
		fs.StringVar(&sf.backend, "backend", "gc-a", "REFERENCE backend to record (gc)")
		fs.StringVar(&sf.corpusLabel, "corpus-label", "", "repo-relative corpus label recorded in the snapshot (default: -corpus value)")
		fs.StringVar(&sf.out, "out", "", "snapshot JSON output path (required)")
		fs.StringVar(&sf.toolchain, "toolchain", "", "toolchain label, e.g. go1.27.1 (default: from `go version`)")
		fs.IntVar(&sf.runs, "runs", 3, "runs per program; all must agree (self-consistency gate)")
	} else {
		fs.StringVar(&sf.backend, "backend", "graalgo", "backend to check against the gc snapshot")
		fs.StringVar(&sf.snapshotPath, "snapshot", "", "gc snapshot JSON to check against (required)")
		fs.StringVar(&sf.expected, "expected", "", "reviewed expected-divergence file (reason-coded); empty = none")
		fs.StringVar(&sf.programs, "programs", "", "comma-separated program names to check (default: whole corpus)")
		fs.StringVar(&sf.verifyRef, "verify-ref", "", "also re-run this reference backend and fail on drift from the snapshot")
		fs.IntVar(&sf.retries, "infra-retries", 2, "re-runs of a program whose failure has an infrastructure signature")
		fs.IntVar(&sf.retryBudget, "retry-budget", 0, "max programs that may pass only after an infra retry; above it the check FAILS (a retried-away failure is usually a product flake, e.g. the gofrontend NUL-byte JSON bug)")
		fs.BoolVar(&sf.failFast, "fail-fast", false, "run programs one at a time and stop at the first DRIFT/STALE (mutation testing)")
	}
	return sf
}

// reGraalgoRun scrubs the launcher path out of recorded text: the absolute
// checkout path is environmental, not behavioral.
var reGraalgoRun = regexp.MustCompile(`\S*/graalgo(-sync)?-run`)

// scrubEnvPaths replaces machine-specific paths with stable tokens so a
// snapshot recorded on one checkout verifies on another.
func scrubEnvPaths(s, corpusRoot string) string {
	if corpusRoot != "" {
		s = strings.ReplaceAll(s, corpusRoot, "{CORPUS}")
	}
	s = reGraalgoRun.ReplaceAllString(s, "{GRAALGO_RUN}")
	return s
}

// reInfra matches failures that are about the test infrastructure, not the
// program: JVM class-loading/launch failures and a corrupted frontend
// export (observed: gofrontend emitting a stray NUL byte under heavy host
// load, then the launcher's JSON parse fails).
var reInfra = regexp.MustCompile(`JSON parse error|JSON encoding failed|ClassNotFoundException|NoClassDefFoundError|Could not find or load main class|Could not create the Java Virtual Machine|Could not reserve enough space|insufficient memory for the Java Runtime`)

func isInfra(e SnapshotEntry) bool {
	return reInfra.MatchString(e.BuildLog) || reInfra.MatchString(e.Stderr)
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

func sourceHash(p harness.Program) (string, error) {
	data, err := os.ReadFile(p.Main)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}

// entryFromOutcome converts a harness outcome into a snapshot entry,
// applying the machine-path scrubbing on top of the harness normalization.
func entryFromOutcome(prog harness.Program, oc harness.Outcome, corpusRoot string) SnapshotEntry {
	return SnapshotEntry{
		Name:     prog.Name,
		BuildOK:  oc.BuildOK,
		BuildLog: scrubEnvPaths(oc.BuildLog, corpusRoot),
		ExitCode: oc.ExitCode,
		TimedOut: oc.TimedOut,
		Stdout:   scrubEnvPaths(oc.Stdout, corpusRoot),
		Stderr:   scrubEnvPaths(oc.Stderr, corpusRoot),
	}
}

// diffEntries compares two recordings of the SAME backend field by field
// (build logs included), returning the first difference ("" when equal).
func diffEntries(want, got SnapshotEntry) string {
	if d := diffBehavior(want, got); d != "" {
		return d
	}
	return diffText("buildLog", want.BuildLog, got.BuildLog)
}

// diffBehavior compares the observable behavior of two entries that may
// come from different toolchains: build outcome, timeout, exit code,
// stdout, stderr. Build logs are compiler-specific and are not compared.
func diffBehavior(want, got SnapshotEntry) string {
	if want.BuildOK != got.BuildOK {
		return fmt.Sprintf("buildOk: want=%v got=%v %s", want.BuildOK, got.BuildOK,
			trunc(strings.ReplaceAll(strings.TrimSpace(got.BuildLog+want.BuildLog), "\n", "\\n"), 300))
	}
	if want.TimedOut != got.TimedOut {
		return fmt.Sprintf("timedOut: want=%v got=%v", want.TimedOut, got.TimedOut)
	}
	if want.ExitCode != got.ExitCode {
		return fmt.Sprintf("exitCode: want=%d got=%d (stderr: %s)", want.ExitCode, got.ExitCode,
			trunc(strings.ReplaceAll(strings.TrimSpace(got.Stderr), "\n", "\\n"), 300))
	}
	if d := diffText("stdout", want.Stdout, got.Stdout); d != "" {
		return d
	}
	return diffText("stderr", want.Stderr, got.Stderr)
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
	f := strings.Fields(string(out))
	if len(f) >= 3 {
		return f[2]
	}
	return "unknown"
}

func loadSnapshotDeps(sf *snapshotFlags) (*harness.Config, *harness.Backend, []harness.Program, string, error) {
	cfg, err := harness.LoadConfig(sf.config)
	if err != nil {
		return nil, nil, nil, "", err
	}
	if sf.timeout > 0 {
		cfg.TimeoutSec = sf.timeout
	}
	b, err := findBackend(cfg, sf.backend)
	if err != nil {
		return nil, nil, nil, "", err
	}
	progs, err := harness.DiscoverCorpus(sf.corpus)
	if err != nil {
		return nil, nil, nil, "", err
	}
	corpusRoot, err := filepath.Abs(sf.corpus)
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("resolve corpus dir: %w", err)
	}
	return cfg, b, progs, corpusRoot, nil
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
	cfg, b, progs, corpusRoot, err := loadSnapshotDeps(sf)
	if err != nil {
		return err
	}
	workRoot, err := os.MkdirTemp("", "diffharness-snapshot-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workRoot)

	snap := SnapshotFile{
		Format:     snapshotFormat,
		Toolchain:  sf.toolchain,
		Reference:  b.Name,
		RecordedAt: time.Now().UTC().Format(time.RFC3339),
		Corpus:     label,
		Normalizer: snapshotNormalizer,
	}
	for r := 0; r < sf.runs; r++ {
		outs := runAll(cfg, b, progs, workRoot, sf.jobs)
		for i, p := range progs {
			e := entryFromOutcome(p, outs[i], corpusRoot)
			if e.SourceSHA256, err = sourceHash(p); err != nil {
				return err
			}
			if r == 0 {
				snap.Programs = append(snap.Programs, e)
				continue
			}
			if d := diffEntries(snap.Programs[i], e); d != "" {
				return fmt.Errorf("self-consistency gate: program %s nondeterministic under %s between run 1 and run %d: %s",
					p.Name, b.Name, r+1, d)
			}
		}
		fmt.Printf("record: run %d/%d complete (%d programs)\n", r+1, sf.runs, len(progs))
	}
	if err := os.MkdirAll(filepath.Dir(sf.out), 0o755); err != nil {
		return err
	}
	for i := range snap.Programs {
		snap.Programs[i].encodeBytes()
	}
	if err := writeJSON(sf.out, snap); err != nil {
		return err
	}
	fmt.Printf("record: wrote %d programs to %s [toolchain=%s reference=%s]\n",
		len(snap.Programs), sf.out, snap.Toolchain, snap.Reference)
	return nil
}

// verdict is one program's check result.
type verdict struct {
	name, kind, detail string
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
	snap, err := readSnapshot(sf.snapshotPath)
	if err != nil {
		return err
	}
	cfg, b, progs, corpusRoot, err := loadSnapshotDeps(sf)
	if err != nil {
		return err
	}
	expected := map[string]harness.ReviewedExclusion{}
	if sf.expected != "" {
		if _, expected, err = harness.LoadReviewedExclusions(sf.expected); err != nil {
			return err
		}
	}
	live, err := selectChecked(snap, progs, expected, sf.programs)
	if err != nil {
		return err
	}
	want := map[string]SnapshotEntry{}
	for _, e := range snap.Programs {
		want[e.Name] = e
	}
	workRoot, err := os.MkdirTemp("", "diffharness-snapshot-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workRoot)

	var verdicts []verdict
	var retriedAway []string
	if sf.verifyRef != "" {
		rb, err := findBackend(cfg, sf.verifyRef)
		if err != nil {
			return err
		}
		outs := runAll(cfg, rb, live, workRoot, sf.jobs)
		for i, p := range live {
			if d := diffEntries(want[p.Name], entryFromOutcome(p, outs[i], corpusRoot)); d != "" {
				verdicts = append(verdicts, verdict{p.Name, "REF-DRIFT", rb.Name + ": " + d})
			}
		}
	}
	var outs []harness.Outcome
	if !sf.failFast {
		outs = runAll(cfg, b, live, workRoot, sf.jobs)
	}
	for i, p := range live {
		var oc harness.Outcome
		if sf.failFast {
			oc = harness.RunProgram(cfg, b, p, workRoot)
		} else {
			oc = outs[i]
		}
		got := entryFromOutcome(p, oc, corpusRoot)
		d := diffBehavior(want[p.Name], got)
		retried := false
		for try := 0; d != "" && isInfra(got) && try < sf.retries; try++ {
			fmt.Printf("RETRY %-28s infrastructure signature: %s\n", p.Name, d)
			retried = true
			got = entryFromOutcome(p, harness.RunProgram(cfg, b, p, workRoot), corpusRoot)
			d = diffBehavior(want[p.Name], got)
		}
		if retried && !isInfra(got) {
			retriedAway = append(retriedAway, p.Name)
		}
		v := classify(p.Name, d, isInfra(got), expected)
		verdicts = append(verdicts, v)
		if sf.failFast && (v.kind == "DRIFT" || v.kind == "STALE") {
			fmt.Printf("fail-fast: stopping after %s %s (%d/%d programs run)\n", v.kind, p.Name, i+1, len(live))
			break
		}
	}
	if err := report(verdicts, len(live), snap, b.Name); err != nil {
		return err
	}
	return checkRetryBudget(retriedAway, sf.retryBudget)
}

// checkRetryBudget fails the check when more programs than budget passed
// only after an infrastructure retry (global review G6): a failure that a
// retry makes disappear is a flake, and in practice a product flake (the
// gofrontend's intermittent NUL byte in its JSON output) — retrying it away
// silently hid the bug.
func checkRetryBudget(retriedAway []string, budget int) error {
	if len(retriedAway) == 0 {
		return nil
	}
	fmt.Printf("RETRIED %d program(s) passed only after an infra retry: %s\n",
		len(retriedAway), strings.Join(retriedAway, ", "))
	if len(retriedAway) > budget {
		return fmt.Errorf("%d program(s) needed an infra retry (budget %d): a flake is a bug, not noise", len(retriedAway), budget)
	}
	return nil
}

// classify turns one comparison into a verdict. An expected divergence must
// be the PINNED one (global review G2): an entry without a signature is
// UNPINNED (a failure that prints the signature to pin), and a divergence
// whose signature differs from the pinned one is DRIFT — otherwise a new
// crash of a listed program would pass as EXPECTED.
func classify(name, diff string, infra bool, expected map[string]harness.ReviewedExclusion) verdict {
	x, isExpected := expected[name]
	switch {
	case diff != "" && infra:
		return verdict{name, "INFRA", diff}
	case diff == "" && isExpected && x.Timing:
		return verdict{name, "TIMING", fmt.Sprintf("passes (timing-dependent): listed `timing: true` (%s: %s), matches gc on this host", x.Reason, x.Detail)}
	case diff == "" && isExpected:
		return verdict{name, "STALE", fmt.Sprintf("listed as expected divergence (%s: %s) but matches gc — prune the entry", x.Reason, x.Detail)}
	case diff == "":
		return verdict{name, "MATCH", ""}
	case isExpected && x.Signature == "":
		return verdict{name, "UNPINNED", fmt.Sprintf("expected divergence has no signature: pin `signature: %s` (%s)", harness.DiffSignature(diff), diff)}
	case isExpected && x.Signature != harness.DiffSignature(diff):
		return verdict{name, "DRIFT", fmt.Sprintf("expected divergence changed (pinned %s, got %s): %s", x.Signature, harness.DiffSignature(diff), diff)}
	case isExpected:
		return verdict{name, "EXPECTED", x.Reason + ": " + diff}
	default:
		return verdict{name, "DRIFT", diff}
	}
}

func report(vs []verdict, n int, snap *SnapshotFile, backend string) error {
	counts := map[string]int{}
	for _, v := range vs {
		counts[v.kind]++
		if v.kind != "MATCH" {
			fmt.Printf("%-9s %-28s %s\n", v.kind, v.name, v.detail)
		}
	}
	fmt.Printf("check: %d programs under %s vs %s snapshot (%s): match=%d expected=%d drift=%d stale=%d unpinned=%d infra=%d ref-drift=%d timing-pass=%d\n",
		n, backend, snap.Reference, snap.Toolchain, counts["MATCH"], counts["EXPECTED"],
		counts["DRIFT"], counts["STALE"], counts["UNPINNED"], counts["INFRA"], counts["REF-DRIFT"], counts["TIMING"])
	if bad := counts["DRIFT"] + counts["STALE"] + counts["UNPINNED"] + counts["REF-DRIFT"]; bad > 0 {
		return fmt.Errorf("%d unexpected results (drift/stale/unpinned/ref-drift)", bad)
	}
	if counts["INFRA"] > 0 {
		return fmt.Errorf("%w: %d programs could not be checked", errInfra, counts["INFRA"])
	}
	return nil
}

func readSnapshot(path string) (*SnapshotFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}
	var snap SnapshotFile
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("parse snapshot: %w", err)
	}
	if snap.Format != snapshotFormat {
		return nil, fmt.Errorf("snapshot format %q != this tool's %q: re-record with `make snapshot`", snap.Format, snapshotFormat)
	}
	if snap.Normalizer != snapshotNormalizer {
		return nil, fmt.Errorf("snapshot normalizer %q != this tool's %q: re-record with `make snapshot`", snap.Normalizer, snapshotNormalizer)
	}
	for i := range snap.Programs {
		if err := snap.Programs[i].decodeBytes(); err != nil {
			return nil, err
		}
	}
	return &snap, nil
}

// selectChecked validates corpus/snapshot/expected-list consistency and
// returns the programs to run. Every inconsistency is an error: a corpus
// program edited, added or removed without a re-record; an expected
// divergence naming no corpus program; an unknown -programs name.
func selectChecked(snap *SnapshotFile, progs []harness.Program, expected map[string]harness.ReviewedExclusion, only string) ([]harness.Program, error) {
	want := map[string]SnapshotEntry{}
	for _, e := range snap.Programs {
		want[e.Name] = e
	}
	inCorpus := map[string]bool{}
	var problems []string
	for _, p := range progs {
		inCorpus[p.Name] = true
		e, ok := want[p.Name]
		if !ok {
			problems = append(problems, p.Name+": not in the snapshot (new program: run `make snapshot`)")
			continue
		}
		h, err := sourceHash(p)
		if err != nil {
			return nil, err
		}
		if h != e.SourceSHA256 {
			problems = append(problems, p.Name+": source changed since recording (run `make snapshot`)")
		}
	}
	for _, e := range snap.Programs {
		if !inCorpus[e.Name] {
			problems = append(problems, e.Name+": recorded but missing from the corpus")
		}
	}
	for _, name := range harness.SortedExclusionNames(expected) {
		if !inCorpus[name] {
			problems = append(problems, name+": expected-divergence entry names no corpus program")
		}
	}
	var sel map[string]bool
	if only != "" {
		sel = map[string]bool{}
		for _, n := range strings.Split(only, ",") {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			if !inCorpus[n] {
				problems = append(problems, n+": -programs names no corpus program")
			}
			sel[n] = true
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("snapshot/corpus inconsistency:\n  %s", strings.Join(problems, "\n  "))
	}
	if sel != nil && len(sel) == 0 {
		return nil, fmt.Errorf("-programs %q names no program (zero cases is never a pass)", only)
	}
	var live []harness.Program
	for _, p := range progs {
		if sel == nil || sel[p.Name] {
			live = append(live, p)
		}
	}
	if len(live) == 0 {
		return nil, fmt.Errorf("no programs to check (empty corpus?): zero cases is never a pass")
	}
	return live, nil
}
