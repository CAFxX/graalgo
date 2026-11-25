// Command diffharness is the differential-vs-gc smoke harness (FEASIBILITY.md
// §7, smoke layer).
//
// Subcommands:
//
//	run       run the corpus under every configured backend, compare each
//	          backend against the first (reference), write a JSON report.
//	          Exit status is non-zero if any program diverges.
//
//	mutate    apply one mutation class to one corpus program and print the
//	          mutated source location/description (oracle-validation helper).
//
//	validate  full oracle validation: (1) green run of the unmutated corpus
//	          (expects zero divergences), (2) rerun control under the
//	          reference backend (expects zero divergences — flakiness check),
//	          (3) per-class mutation validation (expects a divergence for
//	          every mutation class). Exit status is non-zero on any violated
//	          expectation. This is the red-green evidence for the oracles.
//
//	snapshot  behavior snapshots (SPEC 5.9): record gc's observable
//	          behavior on the corpus, or check a backend (graalgo) against
//	          the recorded gc snapshot (fails on unexpected divergence, on a
//	          stale expected-divergence entry, exits 3 on infra failure).
//
//	registers static gate over the exclusion registers (no programs run):
//	          every file parses, every deferred-parity entry cites a
//	          docs/dev/bugs.md id (and every cited id exists), and every
//	          deferred-parity entry of a differential register pins a
//	          signature. `-unsigned FILE` names a register whose entries are
//	          never run (no divergence text to pin).
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"diffharness/internal/harness"
	"diffharness/internal/mutate"
	"diffharness/internal/srctest"
	"diffharness/internal/typeparam"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = cmdRun(os.Args[2:])
	case "mutate":
		err = cmdMutate(os.Args[2:])
	case "validate":
		err = cmdValidate(os.Args[2:])
	case "typeparam":
		err = cmdTypeparam(os.Args[2:])
	case "snapshot":
		err = cmdSnapshot(os.Args[2:])
	case "srctest":
		err = cmdSrctest(os.Args[2:])
	case "registers":
		err = cmdRegisters(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "diffharness %s: error: %v\n", os.Args[1], err)
		if errors.Is(err, errInfra) {
			os.Exit(3) // infrastructure failure: no verdict
		}
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage: diffharness <run|mutate|validate|typeparam|srctest|registers> [flags]

  run      -config PATH -corpus DIR -out PATH [-timeout SEC]
  mutate   -corpus DIR -class CLASS -target PROG [-out FILE]
  validate -config PATH -corpus DIR -mutations FILE -workdir DIR [-out PATH] [-timeout SEC]
  typeparam scan -dir DIR -exclusions FILE -out PATH
  typeparam run  -dir DIR -exclusions FILE -out PATH [-jobs N] [-ref NAME] [-alt NAME] [-config PATH]
  snapshot record -config PATH -corpus DIR -backend gc-a -out FILE [-toolchain V] [-runs N] [-jobs N]
  snapshot check  -config PATH -corpus DIR -backend graalgo -snapshot FILE [-expected FILE] [-programs a,b] [-verify-ref gc-a] [-jobs N]
  srctest scan -dir DIR -exclusions FILE -out PATH
  srctest run  -dir DIR -exclusions FILE -out PATH [-files a.go,b.go] [-jobs N] [-ref NAME] [-alt NAME] [-config PATH] [-timeout SEC]
`)
}

type runFlags struct {
	config   string
	corpus   string
	out      string
	timeout  int
	backends string
}

func baseFlags(fs *flag.FlagSet) *runFlags {
	rf := &runFlags{}
	fs.StringVar(&rf.config, "config", "backends-newpath.json", "backend config JSON")
	fs.StringVar(&rf.corpus, "corpus", "corpus", "corpus directory")
	fs.StringVar(&rf.out, "out", "results.json", "JSON report output path")
	fs.IntVar(&rf.timeout, "timeout", 0, "per-program timeout seconds (0 = config default)")
	fs.StringVar(&rf.backends, "backends", "", "comma-separated backend names to use, in order (default: all in the config)")
	return rf
}

func loadCfg(rf *runFlags) (*harness.Config, error) {
	cfg, err := harness.LoadConfig(rf.config)
	if err != nil {
		return nil, err
	}
	if rf.timeout > 0 {
		cfg.TimeoutSec = rf.timeout
	}
	if rf.backends != "" {
		var sel []harness.Backend
		for _, name := range strings.Split(rf.backends, ",") {
			b, err := findBackend(cfg, strings.TrimSpace(name))
			if err != nil {
				return nil, err
			}
			sel = append(sel, *b)
		}
		cfg.Backends = sel
	}
	return cfg, nil
}

func loadCorpus(rf *runFlags, cfg *harness.Config) ([]harness.Program, []harness.Exclusion, error) {
	progs, err := harness.DiscoverCorpus(rf.corpus)
	if err != nil {
		return nil, nil, err
	}
	kept, skipped := harness.ApplyExclusions(progs, cfg)
	return kept, skipped, nil
}

// runOne executes prog under every backend and returns the outcomes in
// backend order.
func runOne(cfg *harness.Config, prog harness.Program, workRoot string) []harness.Outcome {
	outs := make([]harness.Outcome, len(cfg.Backends))
	for i := range cfg.Backends {
		if i == 0 { // the reference (oracle) backend
			outs[i] = harness.RunReference(cfg, &cfg.Backends[i], prog, workRoot)
			continue
		}
		outs[i] = harness.RunProgram(cfg, &cfg.Backends[i], prog, workRoot)
	}
	return outs
}

type runReport struct {
	GeneratedAt string               `json:"generatedAt"`
	Config      string               `json:"config"`
	Backends    []string             `json:"backends"`
	Skipped     []harness.Exclusion  `json:"skipped"`
	Comparisons []harness.Comparison `json:"comparisons"`
	Summary     map[string]int       `json:"summary"`
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	rf := baseFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadCfg(rf)
	if err != nil {
		return err
	}
	progs, skipped, err := loadCorpus(rf, cfg)
	if err != nil {
		return err
	}
	workRoot, err := os.MkdirTemp("", "diffharness-work-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workRoot)

	backendNames := make([]string, len(cfg.Backends))
	for i, b := range cfg.Backends {
		backendNames[i] = b.Name
	}
	rep := runReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Config:      rf.config,
		Backends:    backendNames,
		Skipped:     skipped,
		Summary:     map[string]int{},
	}
	diverged, infra := 0, 0
	for _, p := range progs {
		outs := runOne(cfg, p, workRoot)
		if harness.RefInfra(outs[0]) {
			infra++
			fmt.Printf("INFRA    %-28s %s: reference timed out (also on the extended retry): no verdict\n", p.Name, outs[0].Backend)
			continue
		}
		for i := 1; i < len(outs); i++ {
			if reInfra.MatchString(outs[i].BuildLog) || reInfra.MatchString(outs[i].Stderr) {
				infra++
				fmt.Printf("INFRA    %-28s %s: no verdict (launcher/frontend failure): %s\n", p.Name, outs[i].Backend,
					trunc(strings.ReplaceAll(outs[i].BuildLog+outs[i].Stderr, "\n", "\\n"), 200))
				continue
			}
			c := harness.Compare(outs[0], outs[i])
			rep.Comparisons = append(rep.Comparisons, c)
			status := "SAME    "
			if !c.Same {
				status = "DIVERGE "
				diverged++
			}
			fmt.Printf("%s %-28s %s vs %s", status, p.Name, outs[0].Backend, outs[i].Backend)
			for _, r := range c.Reasons {
				fmt.Printf("\n           - %s", r)
			}
			fmt.Println()
		}
	}
	rep.Summary["total"] = len(rep.Comparisons)
	rep.Summary["same"] = len(rep.Comparisons) - diverged
	rep.Summary["diverged"] = diverged
	rep.Summary["skipped"] = len(skipped)
	rep.Summary["infra"] = infra
	if err := writeJSON(rf.out, rep); err != nil {
		return err
	}
	fmt.Printf("run: %d comparisons, %d diverged, %d infra, report %s\n", len(rep.Comparisons), diverged, infra, rf.out)
	if infra > 0 {
		return fmt.Errorf("%w: %d comparisons without a verdict", errInfra, infra)
	}
	if diverged > 0 {
		return fmt.Errorf("%d divergences", diverged)
	}
	return nil
}

func cmdMutate(args []string) error {
	fs := flag.NewFlagSet("mutate", flag.ExitOnError)
	corpus := fs.String("corpus", "corpus", "corpus directory")
	class := fs.String("class", "", "mutation class")
	target := fs.String("target", "", "target program name")
	out := fs.String("out", "", "write mutated source to FILE (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *class == "" || *target == "" {
		return fmt.Errorf("-class and -target are required")
	}
	main := filepath.Join(*corpus, *target, "main.go")
	src, err := os.ReadFile(main)
	if err != nil {
		return fmt.Errorf("read target: %w", err)
	}
	mut, err := mutate.Apply(*class, src, main)
	if err != nil {
		return err
	}
	fmt.Printf("mutation: %s\n  class: %s\n  target: %s\n", mut.Description, mut.Class, *target)
	if *out == "" {
		fmt.Println("--- mutated source ---")
		fmt.Print(string(mut.Src))
		return nil
	}
	if err := os.WriteFile(*out, mut.Src, 0o644); err != nil {
		return err
	}
	fmt.Printf("  wrote: %s\n", *out)
	return nil
}

type mutationResult struct {
	Class       string   `json:"class"`
	Target      string   `json:"target"`
	Description string   `json:"description"`
	Detected    bool     `json:"detected"`
	Reasons     []string `json:"reasons"`
	RefLog      string   `json:"refLog,omitempty"`
}

type validateReport struct {
	GeneratedAt     string               `json:"generatedAt"`
	Config          string               `json:"config"`
	Backends        []string             `json:"backends"`
	GreenRun        []harness.Comparison `json:"greenRun"`
	RerunControl    []harness.Comparison `json:"rerunControl"`
	Mutations       []mutationResult     `json:"mutations"`
	GreenDiverged   int                  `json:"greenDiverged"`
	RerunDiverged   int                  `json:"rerunDiverged"`
	MutationsMissed int                  `json:"mutationsMissed"`
	Pass            bool                 `json:"pass"`
}

func cmdValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	rf := baseFlags(fs)
	mutationsFile := fs.String("mutations", "mutations.txt", "mutation manifest (lines: CLASS TARGET)")
	workdir := fs.String("workdir", "", "working directory for mutants (required)")
	jobs := fs.Int("jobs", 1, "parallel programs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *workdir == "" {
		return fmt.Errorf("-workdir is required")
	}
	if *jobs < 1 {
		return fmt.Errorf("-jobs must be >= 1, got %d", *jobs)
	}
	cfg, err := loadCfg(rf)
	if err != nil {
		return err
	}
	if len(cfg.Backends) < 2 {
		return fmt.Errorf("validate needs at least 2 backends (reference + alternate)")
	}
	// Only the reference and the alternate are compared: running a third
	// configured backend (backends-p3.json's graalgo: two JVMs a program)
	// would cost minutes for outcomes nothing reads.
	cfg.Backends = cfg.Backends[:2]
	progs, _, err := loadCorpus(rf, cfg)
	if err != nil {
		return err
	}
	byName := map[string]harness.Program{}
	for _, p := range progs {
		byName[p.Name] = p
	}
	if err := os.MkdirAll(*workdir, 0o755); err != nil {
		return err
	}
	workRoot, err := os.MkdirTemp("", "diffharness-validate-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workRoot)

	backendNames := []string{cfg.Backends[0].Name, cfg.Backends[1].Name}
	rep := validateReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Config:      rf.config,
		Backends:    backendNames,
	}

	// (1) Green run: unmutated corpus, ref vs alt — expect zero divergences.
	// Outcomes under the reference backend are kept: step (3) compares each
	// mutant against the ORIGINAL program's outcome under the SAME backend.
	// (Comparing mutant-vs-mutant across backends would be vacuous: two
	// identical backends running the same mutant must agree.)
	// (2) Rerun control: the reference backend once more on the same
	// program, compared with its green-run outcome — catches harness/corpus
	// flakiness. Programs run -jobs at a time (each worker owns its
	// program's work dirs); verdicts print in corpus order.
	type progRuns struct {
		outs  []harness.Outcome
		rerun harness.Outcome
	}
	runs := make([]progRuns, len(progs))
	forEach(len(progs), *jobs, func(i int) {
		runs[i].outs = runOne(cfg, progs[i], workRoot)
		runs[i].rerun = harness.RunProgram(cfg, &cfg.Backends[0], progs[i], workRoot)
	})
	fmt.Println("== [1/3] green run: unmutated corpus ==")
	origOut := map[string]harness.Outcome{}
	for i, p := range progs {
		outs := runs[i].outs
		origOut[p.Name] = outs[0]
		c := harness.Compare(outs[0], outs[1])
		rep.GreenRun = append(rep.GreenRun, c)
		if !c.Same {
			rep.GreenDiverged++
			fmt.Printf("DIVERGE (unexpected!) %s\n", p.Name)
			for _, r := range c.Reasons {
				fmt.Printf("    - %s\n", r)
			}
		}
	}
	fmt.Printf("green: %d programs, %d divergences (want 0)\n", len(progs), rep.GreenDiverged)

	fmt.Println("== [2/3] rerun control: ref backend twice ==")
	for i, p := range progs {
		c := harness.Compare(runs[i].outs[0], runs[i].rerun)
		rep.RerunControl = append(rep.RerunControl, c)
		if !c.Same {
			rep.RerunDiverged++
			fmt.Printf("DIVERGE (unexpected!) %s\n", p.Name)
			for _, r := range c.Reasons {
				fmt.Printf("    - %s\n", r)
			}
		}
	}
	fmt.Printf("rerun: %d programs, %d divergences (want 0)\n", len(progs), rep.RerunDiverged)

	// (3) Mutation validation: every class must produce a detected divergence.
	fmt.Println("== [3/3] mutation validation ==")
	manifest, err := readManifest(*mutationsFile)
	if err != nil {
		return err
	}
	type mutRun struct {
		mut    *mutate.Mutation
		mutOut harness.Outcome
	}
	mruns := make([]mutRun, len(manifest))
	for i, m := range manifest {
		p, ok := byName[m.target]
		if !ok {
			return fmt.Errorf("manifest: unknown target program %q", m.target)
		}
		src, err := os.ReadFile(p.Main)
		if err != nil {
			return err
		}
		mut, err := mutate.Apply(m.class, src, p.Main)
		if err != nil {
			return fmt.Errorf("manifest %s %s: %w", m.class, m.target, err)
		}
		mruns[i].mut = mut
	}
	forEach(len(manifest), *jobs, func(i int) {
		m := manifest[i]
		mdir := filepath.Join(*workdir, m.class+"__"+m.target)
		mp := harness.Program{Name: m.class + "__" + m.target, Dir: mdir, Main: filepath.Join(mdir, "main.go")}
		if err := os.MkdirAll(mdir, 0o755); err != nil {
			mruns[i].mutOut = harness.Outcome{Program: mp.Name, BuildLog: err.Error()}
			return
		}
		if err := os.WriteFile(mp.Main, mruns[i].mut.Src, 0o644); err != nil {
			mruns[i].mutOut = harness.Outcome{Program: mp.Name, BuildLog: err.Error()}
			return
		}
		// Oracle question: does the comparator flag a behavioral change in
		// the program? Compare ORIGINAL vs MUTANT under the SAME backend.
		mruns[i].mutOut = harness.RunProgram(cfg, &cfg.Backends[0], mp, workRoot)
	})
	for i, m := range manifest {
		mut, mutOut := mruns[i].mut, mruns[i].mutOut
		res := mutationResult{Class: m.class, Target: m.target, Description: mut.Description}
		c := harness.Compare(origOut[m.target], mutOut)
		res.Detected = !c.Same
		res.Reasons = c.Reasons
		status, ok := mutantStatus(mutOut.BuildOK, res.Detected)
		if !mutOut.BuildOK {
			res.Detected = false
			res.RefLog = mutOut.BuildLog
		}
		if !ok {
			rep.MutationsMissed++
		}
		fmt.Printf("%s %-12s %-24s %s\n", status, m.class, m.target, mut.Description)
		for _, r := range c.Reasons {
			fmt.Printf("             - %s\n", r)
		}
		rep.Mutations = append(rep.Mutations, res)
	}
	fmt.Printf("mutations: %d tested, %d missed (want 0)\n", len(rep.Mutations), rep.MutationsMissed)

	rep.Pass = rep.GreenDiverged == 0 && rep.RerunDiverged == 0 && rep.MutationsMissed == 0 && len(rep.Mutations) > 0
	if err := writeJSON(rf.out, rep); err != nil {
		return err
	}
	fmt.Printf("validate: pass=%v, report %s\n", rep.Pass, rf.out)
	if !rep.Pass {
		return fmt.Errorf("validation failed: green=%d rerun=%d missed=%d",
			rep.GreenDiverged, rep.RerunDiverged, rep.MutationsMissed)
	}
	return nil
}

type manifestEntry struct{ class, target string }

// forEach calls f(0..n-1), at most jobs at a time, and returns when all
// calls have returned.
func forEach(n, jobs int, f func(i int)) {
	sem := make(chan struct{}, jobs)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer func() { <-sem; wg.Done() }()
			f(i)
		}(i)
	}
	wg.Wait()
}

// cmdTypeparam implements the test/typeparam differential adapter (M6/D2).
func cmdTypeparam(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("typeparam needs a mode: scan|run")
	}
	switch args[0] {
	case "scan":
		return cmdTypeparamScan(args[1:])
	case "run":
		return cmdTypeparamRun(args[1:])
	default:
		return fmt.Errorf("unknown typeparam mode %q (want scan|run)", args[0])
	}
}

func cmdTypeparamScan(args []string) error {
	fs := flag.NewFlagSet("typeparam scan", flag.ExitOnError)
	dir := fs.String("dir", "", "test/typeparam directory")
	exclusions := fs.String("exclusions", "", "versioned exclusion YAML (GG-0033)")
	out := fs.String("out", "", "scan JSON output path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" || *out == "" {
		return fmt.Errorf("-dir and -out are required")
	}
	var excl map[string]typeparam.Exclusion
	if *exclusions != "" {
		_, excl2, err := typeparam.LoadExclusions(*exclusions)
		if err != nil {
			return err
		}
		excl = excl2
	}
	files, err := typeparam.Scan(*dir, excl)
	if err != nil {
		return err
	}
	byClass := map[string]int{}
	for _, f := range files {
		byClass[f.Class]++
	}
	fmt.Printf("typeparam scan: %d files, classes=%v\n", len(files), byClass)
	return typeparam.WriteScanJSON(*out, files)
}

func cmdTypeparamRun(args []string) error {
	fs := flag.NewFlagSet("typeparam run", flag.ExitOnError)
	dir := fs.String("dir", "", "test/typeparam directory")
	exclusions := fs.String("exclusions", "", "versioned exclusion YAML (GG-0033)")
	out := fs.String("out", "", "report JSON output path")
	jobs := fs.Int("jobs", 4, "parallel programs")
	refName := fs.String("ref", "gc-a", "reference backend name")
	altName := fs.String("alt", "graalgo", "alternate backend name")
	config := fs.String("config", "", "backend config JSON (default: backends.json next to the exclusions file)")
	only := fs.String("files", "", "comma-separated file names: compare only these (a partial run for triage/pinning; stale entries are only judged for the files compared)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" || *exclusions == "" || *out == "" {
		return fmt.Errorf("-dir, -exclusions and -out are required")
	}
	if *jobs < 1 {
		// P2-9: -jobs 0 deadlocks Run (unbuffered semaphore + wg.Wait).
		return fmt.Errorf("-jobs must be >= 1, got %d", *jobs)
	}
	_, excl, err := typeparam.LoadExclusions(*exclusions)
	if err != nil {
		return err
	}
	cfgPath := *config
	if cfgPath == "" {
		cfgPath = filepath.Join(filepath.Dir(*exclusions), "backends.json")
	}
	cfg, err := harness.LoadConfig(cfgPath)
	if err != nil {
		return err
	}
	files, err := typeparam.Scan(*dir, excl)
	if err != nil {
		return err
	}
	if *only != "" {
		files, err = srctest.Select(files, strings.Split(*only, ","))
		if err != nil {
			return err
		}
	}
	comps, err := typeparam.Run(cfg, files, *refName, *altName, *jobs)
	if err != nil {
		return err
	}
	// Same verdicts as srctest: listed files run (G4), EXPECTED while they
	// diverge as pinned, STALE once they match gc; INFRA is never a pass.
	counts := map[string]int{}
	for _, c := range comps {
		v := srctestVerdict(c)
		counts[v]++
		if v == "MATCH" {
			continue
		}
		fmt.Printf("%-9s %s", v, c.Program)
		if v == "STALE" {
			fmt.Printf("\n           - listed in %s (%s) but matches gc: prune the entry", filepath.Base(*exclusions), c.Expected)
		}
		if v == "TIMING" {
			fmt.Printf("\n           - passes (timing-dependent): listed `timing: true` in %s (%s), matches gc on this host", filepath.Base(*exclusions), c.Expected)
		}
		for _, r := range c.Reasons {
			fmt.Printf("\n           - %s", r)
		}
		if v != "STALE" && v != "INFRA" && v != "TIMING" {
			fmt.Printf("\n           = signature %s", harness.DiffSignature(strings.Join(c.Reasons, "\n")))
		}
		fmt.Println()
	}
	notrun := 0
	for _, f := range files {
		if strings.HasPrefix(f.Class, typeparam.ClassNotRun) {
			notrun++
		}
	}
	if err := typeparam.WriteReportJSON(*out, comps, notrun); err != nil {
		return err
	}
	fmt.Printf("typeparam run: %d comparisons: match=%d expected=%d diverge=%d stale=%d infra=%d timing-pass=%d; %d not programs; report %s\n",
		len(comps), counts["MATCH"], counts["EXPECTED"], counts["DIVERGE"], counts["STALE"], counts["INFRA"], counts["TIMING"], notrun, *out)
	if bad := counts["DIVERGE"] + counts["STALE"]; bad > 0 {
		return fmt.Errorf("%d divergences (unlisted divergences / stale entries)", bad)
	}
	if counts["INFRA"] > 0 {
		return fmt.Errorf("%w: %d files could not be compared", errInfra, counts["INFRA"])
	}
	if len(comps) == 0 {
		return fmt.Errorf("%w: zero comparisons (wrong -dir?)", errInfra)
	}
	return nil
}

// cmdSrctest implements the GOROOT/test differential adapter (conformance
// layer: PLAN.md §5 layer 2, GG-0029/GG-0032). Modes mirror typeparam.
func cmdSrctest(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("srctest needs a mode: scan|run")
	}
	switch args[0] {
	case "scan":
		return cmdSrctestScan(args[1:])
	case "run":
		return cmdSrctestRun(args[1:])
	default:
		return fmt.Errorf("unknown srctest mode %q (want scan|run)", args[0])
	}
}

func cmdSrctestScan(args []string) error {
	fs := flag.NewFlagSet("srctest scan", flag.ExitOnError)
	dir := fs.String("dir", "", "GOROOT/test directory")
	exclusions := fs.String("exclusions", "", "versioned exclusion YAML (GG-0033)")
	out := fs.String("out", "", "scan JSON output path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" || *out == "" {
		return fmt.Errorf("-dir and -out are required")
	}
	var excl map[string]typeparam.Exclusion
	if *exclusions != "" {
		_, excl2, err := typeparam.LoadExclusions(*exclusions)
		if err != nil {
			return err
		}
		excl = excl2
	}
	files, err := srctest.Scan(*dir, excl)
	if err != nil {
		return err
	}
	byClass := map[string]int{}
	for _, f := range files {
		byClass[f.Class]++
	}
	fmt.Printf("srctest scan: %d files, classes=%v\n", len(files), byClass)
	return typeparam.WriteScanJSON(*out, files)
}

func cmdSrctestRun(args []string) error {
	fs := flag.NewFlagSet("srctest run", flag.ExitOnError)
	dir := fs.String("dir", "", "GOROOT/test directory")
	exclusions := fs.String("exclusions", "", "versioned exclusion YAML (GG-0033)")
	out := fs.String("out", "", "report JSON output path")
	jobs := fs.Int("jobs", 4, "parallel programs")
	refName := fs.String("ref", "gc-a", "reference backend name")
	altName := fs.String("alt", "graalgo", "alternate backend name")
	config := fs.String("config", "", "backend config JSON (default: backends.json next to the exclusions file)")
	timeout := fs.Int("timeout", 0, "per-program timeout seconds (0 = config default)")
	only := fs.String("files", "", "comma-separated file names: compare only these (a partial run for batching/triage; stale entries are only judged for the files compared)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" || *exclusions == "" || *out == "" {
		return fmt.Errorf("-dir, -exclusions and -out are required")
	}
	if *jobs < 1 {
		return fmt.Errorf("-jobs must be >= 1, got %d", *jobs)
	}
	_, excl, err := typeparam.LoadExclusions(*exclusions)
	if err != nil {
		return err
	}
	cfgPath := *config
	if cfgPath == "" {
		cfgPath = filepath.Join(filepath.Dir(*exclusions), "backends.json")
	}
	cfg, err := harness.LoadConfig(cfgPath)
	if err != nil {
		return err
	}
	if *timeout > 0 {
		cfg.TimeoutSec = *timeout
	}
	files, err := srctest.Scan(*dir, excl)
	if err != nil {
		return err
	}
	if *only != "" {
		files, err = srctest.Select(files, strings.Split(*only, ","))
		if err != nil {
			return err
		}
	}
	comps, err := typeparam.Run(cfg, files, *refName, *altName, *jobs)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, c := range comps {
		v := srctestVerdict(c)
		counts[v]++
		if v == "MATCH" {
			continue
		}
		fmt.Printf("%-9s %s", v, c.Program)
		if v == "STALE" {
			fmt.Printf("\n           - listed in %s (%s) but matches gc: prune the entry", filepath.Base(*exclusions), c.Expected)
		}
		if v == "TIMING" {
			fmt.Printf("\n           - passes (timing-dependent): listed `timing: true` in %s (%s), matches gc on this host", filepath.Base(*exclusions), c.Expected)
		}
		for _, r := range c.Reasons {
			fmt.Printf("\n           - %s", r)
		}
		if v != "STALE" && v != "INFRA" && v != "TIMING" {
			fmt.Printf("\n           = signature %s", harness.DiffSignature(strings.Join(c.Reasons, "\n")))
		}
		fmt.Println()
	}
	notrun := 0
	for _, f := range files {
		if strings.HasPrefix(f.Class, typeparam.ClassNotRun) {
			notrun++
		}
	}
	if err := typeparam.WriteReportJSON(*out, comps, notrun); err != nil {
		return err
	}
	fmt.Printf("srctest run: %d compared: match=%d expected=%d diverge=%d stale=%d infra=%d timing-pass=%d; %d not programs; report %s\n",
		len(comps), counts["MATCH"], counts["EXPECTED"], counts["DIVERGE"], counts["STALE"], counts["INFRA"], counts["TIMING"], notrun, *out)
	if bad := counts["DIVERGE"] + counts["STALE"]; bad > 0 {
		return fmt.Errorf("%d unexpected results (unlisted divergences / stale entries)", bad)
	}
	if counts["INFRA"] > 0 {
		return fmt.Errorf("%w: %d files could not be compared", errInfra, counts["INFRA"])
	}
	return nil
}

// srctestVerdict classifies one srctest/typeparam comparison: MATCH,
// EXPECTED (listed and diverging as pinned), DIVERGE (unlisted divergence,
// or a listed one whose pinned signature changed — G2), STALE (listed but
// matching gc), TIMING (listed `timing: true` and matching gc: a pass, the
// outcome depends on host speed), INFRA (no verdict).
func srctestVerdict(c typeparam.Comparison) string {
	switch {
	case c.Infra:
		return "INFRA"
	case c.Same && c.Expected != "" && c.Timing:
		return "TIMING"
	case c.Same && c.Expected != "":
		return "STALE"
	case c.Same:
		return "MATCH"
	case c.Expected != "" && harness.IsPinned(c.ExpectedSig) && c.ExpectedSig != harness.DiffSignature(strings.Join(c.Reasons, "\n")):
		return "DIVERGE"
	case c.Expected != "":
		return "EXPECTED"
	default:
		return "DIVERGE"
	}
}

// mutantStatus classifies one comparator-self-test mutant. A mutant that
// does not BUILD tests nothing about the comparator (any build failure
// "differs"): it is STILLBORN, a failure of the self-test, never a
// detection (global review G5).
func mutantStatus(buildOK, differs bool) (status string, ok bool) {
	switch {
	case !buildOK:
		return "STILLBORN", false
	case !differs:
		return "MISSED   ", false
	default:
		return "DETECTED", true
	}
}

func readManifest(path string) ([]manifestEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []manifestEntry
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 {
			return nil, fmt.Errorf("manifest line %d: want 'CLASS TARGET', got %q", i+1, line)
		}
		out = append(out, manifestEntry{f[0], f[1]})
	}
	return out, nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// cmdRegisters is the exclusion-register gate (see the package comment).
func cmdRegisters(args []string) error {
	fs := flag.NewFlagSet("registers", flag.ExitOnError)
	bugsPath := fs.String("bugs", "", "bug register (docs/dev/bugs.md)")
	var unsigned []string
	fs.Func("unsigned", "register whose entries are never run: no signature required (repeatable)", func(v string) error {
		unsigned = append(unsigned, v)
		return nil
	})
	if err := fs.Parse(args); err != nil {
		return err
	}
	files := append(fs.Args(), unsigned...)
	if *bugsPath == "" || len(files) == 0 {
		return fmt.Errorf("usage: registers -bugs docs/dev/bugs.md [-unsigned FILE]... FILE...")
	}
	bugs, err := harness.LoadBugIDs(*bugsPath)
	if err != nil {
		return err
	}
	var problems []string
	for i, f := range files {
		_, excl, err := harness.LoadReviewedExclusions(f)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		problems = append(problems, harness.RegisterProblems(f, excl, bugs, i < len(fs.Args()))...)
	}
	for _, p := range problems {
		fmt.Println(p)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d register problem(s)", len(problems))
	}
	fmt.Printf("registers: %d file(s) clean\n", len(files))
	return nil
}
