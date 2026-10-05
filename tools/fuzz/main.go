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

func loadState(workdir string) state {
	st := state{Counts: map[string]int64{}}
	data, err := os.ReadFile(filepath.Join(workdir, "state.json"))
	if err == nil {
		json.Unmarshal(data, &st)
	}
	if st.Counts == nil {
		st.Counts = map[string]int64{}
	}
	return st
}

func saveState(workdir string, st state) {
	data, _ := json.Marshal(st)
	os.WriteFile(filepath.Join(workdir, "state.json"), data, 0644)
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
	os.MkdirAll(scratch, 0755)
	defer os.RemoveAll(scratch)
	src := filepath.Join(scratch, "main.go")
	os.WriteFile(src, []byte(generate(seed)), 0644)

	gc := runGC(scratch, src)
	gr := runGraal(scratch, src)
	v := compare(gc, gr)
	note := ""
	if v == vDiverged {
		note = fmt.Sprintf("gc(exit=%d) vs graal(exit=%d) stdout_eq=%v",
			gc.exit, gr.exit, gc.stdout == gr.stdout)
		if doMin {
			minprog, probes := minimize(scratch, seed)
			name := saveRegression(workdir, seed, minprog)
			note += fmt.Sprintf(" minimized->%s probes=%d", name, probes)
		}
	} else if v == vScopeReject {
		note = "fail-fast scope error"
	} else if v == vGCInvalid {
		note = "gc rejected (generator bug)"
	}
	jmu.Lock()
	data, _ := json.Marshal(journalEntry{Seed: seed, Verdict: v.String(), Note: note})
	journal.Write(data)
	journal.WriteByte('\n')
	journal.Flush() // M8: flush per entry for crash safety and live monitoring
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

	os.MkdirAll(*workdir, 0755)
	os.MkdirAll(filepath.Join(*workdir, "scratch"), 0755)
	st := loadState(*workdir)
	next := *start
	if *resume && st.NextSeed > next {
		next = st.NextSeed
	}
	end := *start + *count
	st.NextSeed = next

	jf, err := os.OpenFile(filepath.Join(*workdir, "journal.jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "journal:", err)
		os.Exit(1)
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
					journal.Flush()
					saveState(*workdir, st)
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
	journal.Flush()
	saveState(*workdir, st)
	fmt.Fprintf(os.Stderr, "done: %v\n", st.Counts)
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
	os.MkdirAll(scratch, 0755)
	defer os.RemoveAll(scratch)
	for _, e := range ents {
		if filepath.Ext(e.Name()) != ".go" {
			continue
		}
		src := filepath.Join(dir, e.Name())
		data, _ := os.ReadFile(src)
		tmp := filepath.Join(scratch, "main.go")
		os.WriteFile(tmp, data, 0644)
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
