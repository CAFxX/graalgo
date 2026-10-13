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
//	snapshot  behavior snapshots (M10 WS I): record graalgo's observable
//	          behavior on the corpus as the upgrade baseline, or check the
//	          current behavior against a recorded snapshot (fails on drift).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "diffharness %s: error: %v\n", os.Args[1], err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage: diffharness <run|mutate|validate|typeparam|srctest> [flags]

  run      -config PATH -corpus DIR -out PATH [-timeout SEC]
  mutate   -corpus DIR -class CLASS -target PROG [-out FILE]
  validate -config PATH -corpus DIR -mutations FILE -workdir DIR [-out PATH] [-timeout SEC]
  typeparam scan -dir DIR -exclusions FILE -out PATH
  typeparam run  -dir DIR -exclusions FILE -out PATH [-jobs N] [-ref NAME] [-alt NAME] [-config PATH]
  snapshot record -config PATH -corpus DIR -backend NAME -out FILE [-toolchain V] [-graalgo-commit SHA] [-runs N] [-jobs N]
  snapshot check  -config PATH -corpus DIR -backend NAME -snapshot FILE [-jobs N]
  srctest scan -dir DIR -exclusions FILE -out PATH
  srctest run  -dir DIR -exclusions FILE -out PATH [-jobs N] [-ref NAME] [-alt NAME] [-config PATH] [-timeout SEC]
`)
}

type runFlags struct {
	config  string
	corpus  string
	out     string
	timeout int
}

func baseFlags(fs *flag.FlagSet) *runFlags {
	rf := &runFlags{}
	fs.StringVar(&rf.config, "config", "backends.json", "backend config JSON")
	fs.StringVar(&rf.corpus, "corpus", "corpus", "corpus directory")
	fs.StringVar(&rf.out, "out", "results.json", "JSON report output path")
	fs.IntVar(&rf.timeout, "timeout", 0, "per-program timeout seconds (0 = config default)")
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
	diverged := 0
	for _, p := range progs {
		outs := runOne(cfg, p, workRoot)
		for i := 1; i < len(outs); i++ {
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
	if err := writeJSON(rf.out, rep); err != nil {
		return err
	}
	fmt.Printf("run: %d comparisons, %d diverged, report %s\n", len(rep.Comparisons), diverged, rf.out)
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *workdir == "" {
		return fmt.Errorf("-workdir is required")
	}
	cfg, err := loadCfg(rf)
	if err != nil {
		return err
	}
	if len(cfg.Backends) < 2 {
		return fmt.Errorf("validate needs at least 2 backends (reference + alternate)")
	}
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
	fmt.Println("== [1/3] green run: unmutated corpus ==")
	origOut := map[string]harness.Outcome{}
	for _, p := range progs {
		outs := runOne(cfg, p, workRoot)
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

	// (2) Rerun control: same program twice under the reference backend —
	// expects zero divergences; catches harness/corpus flakiness.
	fmt.Println("== [2/3] rerun control: ref backend twice ==")
	for _, p := range progs {
		a := harness.RunProgram(cfg, &cfg.Backends[0], p, workRoot)
		b := harness.RunProgram(cfg, &cfg.Backends[0], p, workRoot)
		c := harness.Compare(a, b)
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
	for _, m := range manifest {
		res := mutationResult{Class: m.class, Target: m.target}
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
		res.Description = mut.Description
		mdir := filepath.Join(*workdir, m.class+"__"+m.target)
		if err := os.MkdirAll(mdir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(mdir, "main.go"), mut.Src, 0o644); err != nil {
			return err
		}
		mp := harness.Program{Name: m.class + "__" + m.target, Dir: mdir, Main: filepath.Join(mdir, "main.go")}
		// Oracle question: does the comparator flag a behavioral change in
		// the program? Compare ORIGINAL vs MUTANT under the SAME backend.
		mutOut := harness.RunProgram(cfg, &cfg.Backends[0], mp, workRoot)
		c := harness.Compare(origOut[m.target], mutOut)
		res.Detected = !c.Same
		res.Reasons = c.Reasons
		if !mutOut.BuildOK {
			res.RefLog = mutOut.BuildLog
		}
		status := "DETECTED"
		if !res.Detected {
			status = "MISSED   "
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
	comps, err := typeparam.Run(cfg, files, *refName, *altName, *jobs)
	if err != nil {
		return err
	}
	div := 0
	for _, c := range comps {
		status := "SAME    "
		if !c.Same {
			status = "DIVERGE "
			div++
		}
		fmt.Printf("%s %s", status, c.Program)
		for _, r := range c.Reasons {
			fmt.Printf("\n           - %s", r)
		}
		fmt.Println()
	}
	skipped := 0
	for _, f := range files {
		if strings.HasPrefix(f.Class, "exclude") {
			skipped++
		}
	}
	if err := typeparam.WriteReportJSON(*out, comps, skipped); err != nil {
		return err
	}
	fmt.Printf("typeparam run: %d comparisons, %d diverged, %d excluded, report %s\n",
		len(comps), div, skipped, *out)
	if div > 0 {
		return fmt.Errorf("%d divergences", div)
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
	comps, err := typeparam.Run(cfg, files, *refName, *altName, *jobs)
	if err != nil {
		return err
	}
	div := 0
	for _, c := range comps {
		status := "SAME    "
		if !c.Same {
			status = "DIVERGE "
			div++
		}
		fmt.Printf("%s %s", status, c.Program)
		for _, r := range c.Reasons {
			fmt.Printf("\n           - %s", r)
		}
		fmt.Println()
	}
	skipped := 0
	for _, f := range files {
		if strings.HasPrefix(f.Class, "exclude") {
			skipped++
		}
	}
	if err := typeparam.WriteReportJSON(*out, comps, skipped); err != nil {
		return err
	}
	fmt.Printf("srctest run: %d comparisons, %d diverged, %d excluded, report %s\n",
		len(comps), div, skipped, *out)
	if div > 0 {
		return fmt.Errorf("%d divergences", div)
	}
	return nil
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
