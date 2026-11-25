package harness

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Batched runs: a backend with BatchRun builds every program as usual and
// then runs the built ones in a few long-lived processes instead of one
// process each — graalgo-bundle-run --batch (graalgo.launcher.BundleBatchMain,
// test/lib/batchrun.sh's protocol): each program in a fresh polyglot
// Context, with its stdin/stdout/stderr files on descriptors 0-2, its
// working directory, os.Args and its timeout (status 124). The manifest has
// one tab-separated line per program, `id cwd argv0 stdin stdout stderr
// timeout-seconds [arg...]`; the results file gets `START id` and
// `END id status`. A program without its END line (the batch process
// died, or a cancel hung and the batch halted) runs in its own process,
// with every program after it, so a crash is never silent and never
// decides a verdict. What a batch cannot give a program — its own
// environment, or a process-global mutator that outlives its context —
// is decided by the BatchIsolated pattern (test/lib/batchrun.sh's
// BATCH_ISOLATED_RE, the one definition): a program whose sources match it
// runs in its own process.

// batchOff is the environment switch that runs every program in its own
// process (the per-process path, for equivalence checks and debugging),
// shared with test/lib/batchrun.sh.
const batchOff = "GRAALGO_BATCH"

// batchIsolatedVar is the line of test/lib/batchrun.sh that defines the
// pattern (single-quoted).
var batchIsolatedVar = regexp.MustCompile(`(?m)^BATCH_ISOLATED_RE='([^']*)'$`)

// LoadBatchIsolated reads BATCH_ISOLATED_RE from a batchrun.sh-style file.
func LoadBatchIsolated(path string) (*regexp.Regexp, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("batch isolation pattern: %w", err)
	}
	m := batchIsolatedVar.FindSubmatch(data)
	if m == nil {
		return nil, fmt.Errorf("batch isolation pattern: no BATCH_ISOLATED_RE='...' line in %s", path)
	}
	re, err := regexp.Compile(string(m[1]))
	if err != nil {
		return nil, fmt.Errorf("batch isolation pattern of %s: %w", path, err)
	}
	return re, nil
}

// isolated reports whether prog must run in its own process: a source file
// of its directory matches the pattern.
func isolated(re *regexp.Regexp, prog Program) bool {
	files, _ := filepath.Glob(filepath.Join(prog.Dir, "*.go"))
	if len(files) == 0 {
		files = []string{prog.Main}
	}
	for _, f := range files {
		if data, err := os.ReadFile(f); err == nil && re.Match(data) {
			return true
		}
	}
	return false
}

// Batched reports whether runs of b go through RunBatch: the backend
// declares a batch command and GRAALGO_BATCH is not "off".
func (b *Backend) Batched() bool {
	return len(b.BatchRun) > 0 && os.Getenv(batchOff) != "off"
}

// batchEntry is one queued program.
type batchEntry struct {
	idx                 int
	runDir, out, errOut string
}

// RunBatch builds every program under b (jobs at a time), runs the built
// ones in at most jobs batch processes (contiguous slices of the list) and
// returns the outcomes in program order, as RunProgram would give them.
// Programs the batches did not finish, and isolated ones, run in their own
// process. Progress notes go to log.
func RunBatch(cfg *Config, b *Backend, progs []Program, workRoot string, jobs int, log io.Writer) []Outcome {
	if jobs < 1 {
		jobs = 1
	}
	outs := make([]Outcome, len(progs))
	forEach(len(progs), jobs, func(i int) { outs[i] = BuildOnly(cfg, b, progs[i], workRoot) })

	var queue []batchEntry
	var solo []int
	for i, p := range progs {
		switch {
		case !outs[i].BuildOK:
		case b.batchIsolated != nil && isolated(b.batchIsolated, p) || !batchable(p, workRoot):
			solo = append(solo, i)
		default:
			dir := filepath.Join(workRoot, p.Name, b.Name)
			queue = append(queue, batchEntry{idx: i, out: filepath.Join(dir, "batch.out"), errOut: filepath.Join(dir, "batch.err")})
		}
	}
	n := min(jobs, len(queue))
	var unfinished []int
	var mu sync.Mutex
	forEach(n, n, func(k int) {
		part := queue[k*len(queue)/n : (k+1)*len(queue)/n]
		left := runOneBatch(cfg, b, progs, part, workRoot, k, outs, log)
		mu.Lock()
		unfinished = append(unfinished, left...)
		mu.Unlock()
	})
	if len(queue) > 0 && len(unfinished) == 0 {
		fmt.Fprintf(log, "batch: %d programs in %d JVM(s); %d in their own process\n", len(queue), n, len(solo))
	}
	solo = append(solo, unfinished...)
	forEach(len(solo), jobs, func(k int) {
		i := solo[k]
		outs[i] = runBuilt(cfg, b, progs[i], workRoot, outs[i])
	})
	return outs
}

// batchable: no manifest field carries a tab or newline (LoadConfig
// already requires the run command to be the built binary, argv0).
func batchable(p Program, workRoot string) bool {
	for _, f := range append([]string{p.RunDir, workRoot, p.Name}, p.Args...) {
		if strings.ContainsAny(f, "\t\n") {
			return false
		}
	}
	return true
}

// runOneBatch runs part in one batch process, fills outs for every program
// it finished and returns the indices of those it did not.
func runOneBatch(cfg *Config, b *Backend, progs []Program, part []batchEntry, workRoot string, k int,
	outs []Outcome, log io.Writer) []int {
	dir := filepath.Join(workRoot, fmt.Sprintf(".batch-%d", k))
	manifest, results := filepath.Join(dir, "manifest"), filepath.Join(dir, "results")
	rest := func(from int) []int {
		var ids []int
		for _, e := range part[from:] {
			ids = append(ids, e.idx)
		}
		return ids
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(log, "batch: %v; running %d programs in their own processes\n", err, len(part))
		return rest(0)
	}
	var lines []string
	var total time.Duration
	for j := range part {
		e := &part[j]
		p := progs[e.idx]
		e.runDir = p.RunDir
		if e.runDir == "" {
			d, err := os.MkdirTemp("", "diffharness-run-*")
			if err != nil {
				fmt.Fprintf(log, "batch: %v; running %d programs in their own processes\n", err, len(part))
				return rest(0)
			}
			defer os.RemoveAll(d)
			e.runDir = d
		}
		to := p.timeout(cfg)
		total += to
		secs := int(math.Ceil(to.Seconds()))
		f := append([]string{strconv.Itoa(e.idx), e.runDir, filepath.Join(workRoot, p.Name, b.Name, "prog"),
			os.DevNull, e.out, e.errOut, strconv.Itoa(max(secs, 1))}, p.Args...)
		lines = append(lines, strings.Join(f, "\t"))
	}
	if err := os.WriteFile(manifest, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		fmt.Fprintf(log, "batch: %v; running %d programs in their own processes\n", err, len(part))
		return rest(0)
	}
	cmdline := substitute(b.render(b.BatchRun), map[string]string{"MANIFEST": manifest, "RESULTS": results})
	// The outer bound only guards the watchdog: every program has its own
	// timeout inside, and a hung cancel halts the batch.
	ctx, cancel := context.WithTimeout(context.Background(), total+60*time.Second)
	defer cancel()
	jvmErr, _ := os.Create(filepath.Join(dir, "batch.stderr"))
	cmd := exec.CommandContext(ctx, cmdline[0], cmdline[1:]...)
	killGroupOnCancel(cmd)
	cmd.Dir = dir
	cmd.Env = runEnv(b.Env)
	if jvmErr != nil {
		cmd.Stdout, cmd.Stderr = jvmErr, jvmErr
		defer jvmErr.Close()
	}
	runErr := cmd.Run()

	status := map[int]int{}
	if f, err := os.Open(results); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fs := strings.Fields(sc.Text())
			if len(fs) == 3 && fs[0] == "END" {
				id, err1 := strconv.Atoi(fs[1])
				st, err2 := strconv.Atoi(fs[2])
				if err1 == nil && err2 == nil {
					status[id] = st
				}
			}
		}
		f.Close()
	}
	for j, e := range part {
		st, ok := status[e.idx]
		if !ok {
			left := rest(j)
			fmt.Fprintf(log, "batch: the JVM stopped (%v) after %d of %d programs; running the other %d in their own processes\n",
				runErr, j, len(part), len(left))
			if data, err := os.ReadFile(filepath.Join(dir, "batch.stderr")); err == nil {
				var tail []string
				for _, l := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
					if !strings.HasPrefix(l, "WARNING: ") {
						tail = append(tail, "    "+l)
					}
				}
				fmt.Fprintln(log, strings.Join(tail[max(0, len(tail)-5):], "\n"))
			}
			return left
		}
		outs[e.idx] = batchOutcome(b, progs[e.idx], outs[e.idx], e, st)
	}
	return nil
}

// batchOutcome is the Outcome of a finished batched program, normalized as
// runBuilt normalizes a per-process run.
func batchOutcome(b *Backend, p Program, oc Outcome, e batchEntry, st int) Outcome {
	stdout, _ := os.ReadFile(e.out)
	stderr, _ := os.ReadFile(e.errOut)
	oc.Stdout = NormalizeStdout(string(stdout), p.StdoutRules)
	oc.Stderr = b.FilterStderr(Normalize(string(stderr)))
	if st == batchTimedOut {
		oc.TimedOut = true
		oc.ExitCode = -1
		return oc
	}
	oc.ExitCode = st
	return oc
}

// batchTimedOut is the batch's status for a program its timeout cancelled
// (coreutils timeout's).
const batchTimedOut = 124

// forEach calls f(0..n-1), at most jobs at a time.
func forEach(n, jobs int, f func(int)) {
	if n == 0 {
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(jobs, 1))
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			f(i)
		}(i)
	}
	wg.Wait()
}
