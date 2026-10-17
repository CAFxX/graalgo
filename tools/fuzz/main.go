package main

// main.go — M8 fuzz driver.
//
// Usage:
//
//	fuzz gen -seed N                    # print the program for seed N
//	fuzz run -start S -count C -lanes L # differential soak over seeds [S, S+C)
//	fuzz regress                        # re-run saved regressions, expect SAME
//
// State: <workdir>/state.json (resume), <workdir>/journal.jsonl (verdicts),
// <workdir>/regressions/fuzz_<seed>.go (minimized divergences).
// Per-seed scratch lives under <workdir>/scratch/ and is removed after each
// program (only divergences are kept).
//
// Error policy: this tool's job is durable evidence. A corrupt state file,
// a lost journal write, or an unwritable regression is never silently
// swallowed — such failures are fatal and loud. A broken backend binary
// yields HARNESS_ERROR verdicts and fails the run at the end, so a dead
// harness can never masquerade as a clean or divergent soak.

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type state struct {
	NextSeed int64            `json:"nextSeed"`
	Counts   map[string]int64 `json:"counts"`
}

type journalEntry struct {
	Seed    int64  `json:"seed"`
	Verdict string `json:"verdict"`
	Note    string `json:"note,omitempty"`
}

var stopFlag atomic.Bool

// fatalf reports an unrecoverable tool failure loudly and exits non-zero.
// Used for evidence-integrity failures (state/journal/regression writes),
// never for per-seed program verdicts.
func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "fuzz: fatal: "+format+"\n", args...)
	os.Exit(1)
}

func loadState(workdir string) (state, error) {
	st := state{Counts: map[string]int64{}}
	data, err := os.ReadFile(filepath.Join(workdir, "state.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return st, fmt.Errorf("read state.json: %w", err)
	}
	// A corrupt state file must never silently reset the counters —
	// resuming from zero would rerun seeds and double-count verdicts.
	if err := json.Unmarshal(data, &st); err != nil {
		return st, fmt.Errorf("corrupt state.json: %w", err)
	}
	if st.Counts == nil {
		st.Counts = map[string]int64{}
	}
	return st, nil
}

func saveState(workdir string, st state) error {
	data, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "state.json"), data, 0644); err != nil {
		return fmt.Errorf("write state.json: %w", err)
	}
	return nil
}

func cmdGen() {
	fs := flag.NewFlagSet("gen", flag.ExitOnError)
	seed := fs.Int64("seed", 0, "seed")
	fs.Parse(os.Args[2:])
	fmt.Print(generate(*seed))
}

// oneSeed runs a single seed end-to-end and returns the verdict + note.
func oneSeed(workdir string, seed int64, doMin bool, journal *bufio.Writer, jmu *sync.Mutex) verdict {
	scratch := filepath.Join(workdir, "scratch", fmt.Sprintf("s%d", seed))
	if err := os.MkdirAll(scratch, 0755); err != nil {
		fatalf("scratch mkdir for seed %d: %v", seed, err)
	}
	defer os.RemoveAll(scratch)
	src := filepath.Join(scratch, "main.go")
	if err := os.WriteFile(src, []byte(generate(seed)), 0644); err != nil {
		fatalf("write seed %d: %v", seed, err)
	}

	gc := runGC(scratch, src)
	gr := runGraal(scratch, src)
	v := compare(gc, gr)
	note := ""
	if v == vDiverged {
		note = fmt.Sprintf("gc(exit=%d) vs graal(exit=%d) stdout_eq=%v",
			gc.exit, gr.exit, gc.stdout == gr.stdout)
		if doMin {
			minprog, probes, err := minimize(scratch, seed)
			if err != nil {
				fatalf("minimize seed %d: %v", seed, err)
			}
			name, err := saveRegression(workdir, seed, minprog)
			if err != nil {
				fatalf("save regression seed %d: %v", seed, err)
			}
			note += fmt.Sprintf(" minimized->%s probes=%d", name, probes)
		}
	} else if v == vScopeReject {
		note = "fail-fast scope error"
	} else if v == vGCInvalid {
		note = "gc rejected (generator bug)"
	} else if v == vHarnessError {
		if gr.harnessErr {
			note = fmt.Sprintf("harness error: graalgo backend binary failed to start (%s)", graalgoRun)
		} else {
			note = fmt.Sprintf("harness error: gc binary failed to start (%s)", goBin)
		}
	}
	data, err := json.Marshal(journalEntry{Seed: seed, Verdict: v.String(), Note: note})
	if err != nil {
		fatalf("journal marshal seed %d: %v", seed, err)
	}
	jmu.Lock()
	if _, err := journal.Write(data); err != nil {
		jmu.Unlock()
		fatalf("journal write seed %d: %v", seed, err)
	}
	if err := journal.WriteByte('\n'); err != nil {
		jmu.Unlock()
		fatalf("journal write seed %d: %v", seed, err)
	}
	if err := journal.Flush(); err != nil {
		jmu.Unlock()
		fatalf("journal flush seed %d: %v", seed, err)
	}
	jmu.Unlock()
	return v
}

func cmdRun() {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	start := fs.Int64("start", 0, "first seed")
	count := fs.Int64("count", 1000, "number of seeds")
	lanes := fs.Int("lanes", 2, "parallel lanes")
	workdir := fs.String("workdir", "work", "working directory")
	resume := fs.Bool("resume", false, "resume from state.json")
	doMin := fs.Bool("min", true, "minimize divergences")
	fs.Parse(os.Args[2:])

	if err := os.MkdirAll(*workdir, 0755); err != nil {
		fatalf("workdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(*workdir, "scratch"), 0755); err != nil {
		fatalf("scratch dir: %v", err)
	}
	st, err := loadState(*workdir)
	if err != nil {
		fatalf("%v", err)
	}
	next := *start
	if *resume && st.NextSeed > next {
		next = st.NextSeed
	}
	end := *start + *count
	st.NextSeed = next

	jf, err := os.OpenFile(filepath.Join(*workdir, "journal.jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fatalf("journal open: %v", err)
	}
	defer jf.Close()
	journal := bufio.NewWriter(jf)
	defer journal.Flush()
	var jmu sync.Mutex

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigc
		fmt.Fprintln(os.Stderr, "\nstopping after current seeds...")
		stopFlag.Store(true)
	}()

	seeds := make(chan int64, *lanes*2)
	var wg sync.WaitGroup
	var mu sync.Mutex
	done := int64(0)
	t0 := time.Now()
	for l := 0; l < *lanes; l++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range seeds {
				if stopFlag.Load() {
					continue
				}
				v := oneSeed(*workdir, s, *doMin, journal, &jmu)
				mu.Lock()
				st.Counts[v.String()]++
				st.NextSeed = s + 1
				done++
				if done%25 == 0 {
					if err := journal.Flush(); err != nil {
						fatalf("journal flush: %v", err)
					}
					if err := saveState(*workdir, st); err != nil {
						fatalf("%v", err)
					}
					el := time.Since(t0).Round(time.Second)
					fmt.Fprintf(os.Stderr, "[%s] %d/%d %v\n", el, done, end-next, st.Counts)
				}
				mu.Unlock()
			}
		}()
	}
feed:
	for s := next; s < end; s++ {
		if stopFlag.Load() {
			break feed
		}
		seeds <- s
	}
	close(seeds)
	wg.Wait()
	if err := journal.Flush(); err != nil {
		fatalf("journal flush: %v", err)
	}
	if err := saveState(*workdir, st); err != nil {
		fatalf("%v", err)
	}
	fmt.Fprintf(os.Stderr, "done: %v\n", st.Counts)
	if n := st.Counts[vHarnessError.String()]; n > 0 {
		fmt.Fprintf(os.Stderr, "fuzz: %d seed(s) hit HARNESS_ERROR — the backend binary failed to start (%s); this run's results are invalid\n", n, graalgoRun)
		os.Exit(2)
	}
}

// cmdRegress re-runs every saved regression and reports any that no longer
// diverge (fixed) or newly fail to build (scope change).
func cmdRegress() {
	fs := flag.NewFlagSet("regress", flag.ExitOnError)
	workdir := fs.String("workdir", "work", "working directory")
	fs.Parse(os.Args[2:])
	dir := filepath.Join(*workdir, "regressions")
	ents, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "no regressions dir:", err)
		return
	}
	scratch := filepath.Join(*workdir, "scratch", "regress")
	if err := os.MkdirAll(scratch, 0755); err != nil {
		fatalf("scratch mkdir: %v", err)
	}
	defer os.RemoveAll(scratch)
	for _, e := range ents {
		if filepath.Ext(e.Name()) != ".go" {
			continue
		}
		src := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "regress: read %s: %v\n", e.Name(), err)
			continue
		}
		tmp := filepath.Join(scratch, "main.go")
		if err := os.WriteFile(tmp, data, 0644); err != nil {
			fatalf("regress: write scratch for %s: %v", e.Name(), err)
		}
		gc := runGC(scratch, tmp)
		gr := runGraal(scratch, tmp)
		v := compare(gc, gr)
		fmt.Printf("%s: %s\n", e.Name(), v)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: fuzz <gen|run|regress> [flags]")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "gen":
		cmdGen()
	case "run":
		cmdRun()
	case "regress":
		cmdRegress()
	default:
		fmt.Fprintln(os.Stderr, "unknown subcommand:", os.Args[1])
		os.Exit(1)
	}
}
