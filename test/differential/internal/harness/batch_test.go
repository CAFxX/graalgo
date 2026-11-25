package harness

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The batch tests run shell stand-ins (no JVM, no go build): the "build"
// writes a shell script as the program; fakeBatch is a batch launcher that
// speaks test/lib/batchrun.sh's protocol and, with FAKE_DIE_AFTER=N, dies
// after N programs (the JVM crash a batch must survive).
const fakeBatch = `#!/bin/sh
n=0
while IFS='	' read -r id cwd argv0 stdin out err to rest; do
  [ -n "$FAKE_DIE_AFTER" ] && [ "$n" -ge "$FAKE_DIE_AFTER" ] && { echo "fake: dying" >&2; exit 137; }
  echo "START $id" >> "$2"
  (cd "$cwd" && FAKE_BATCHED=1 timeout "$to" "$argv0" $rest < "$stdin" > "$out" 2> "$err"); st=$?
  echo "END $id $st" >> "$2"
  n=$((n + 1))
done < "$1"
`

func batchFixture(t *testing.T, progs map[string]string) (*Backend, []Program, string) {
	t.Helper()
	dir := t.TempDir()
	runner := filepath.Join(dir, "fake-batch")
	if err := os.WriteFile(runner, []byte(fakeBatch), 0o755); err != nil {
		t.Fatal(err)
	}
	var ps []Program
	for _, name := range []string{"a", "b", "c", "d"} {
		body, ok := progs[name]
		if !ok {
			continue
		}
		pd := filepath.Join(dir, "corpus", name)
		os.MkdirAll(pd, 0o755)
		if err := os.WriteFile(filepath.Join(pd, "main.go"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		ps = append(ps, Program{Name: name, Dir: pd, Main: filepath.Join(pd, "main.go")})
	}
	b := &Backend{
		Name: "g",
		// the "program" is the shell after the source's `//sh ` line
		Build:         []string{"sh", "-c", `{ echo '#!/bin/sh'; sed -n 's,^//sh ,,p' "$1"; } > "$2" && chmod +x "$2"`, "build", "{SRC}", "{BIN}"},
		Run:           []string{"{BIN}"},
		BatchRun:      []string{runner, "{MANIFEST}", "{RESULTS}"},
		batchIsolated: regexp.MustCompile(`Setrlimit`),
		NoiseSets:     []string{"jvm"},
	}
	return b, ps, t.TempDir()
}

func TestRunBatch_SameOutcomesAsPerProcess(t *testing.T) {
	b, ps, work := batchFixture(t, map[string]string{
		"a": "//sh echo out-a; echo 'WARNING: sun.misc.Unsafe::x' >&2; echo err-a >&2; exit 3\n",
		"b": "//sh echo \"batched=$FAKE_BATCHED\"\n",
		"c": "//sh sleep 5\n",
		"d": "//sh echo \"Setrlimit batched=$FAKE_BATCHED\"\n",
	})
	cfg := &Config{TimeoutSec: 1, TimeoutUnit: 200 * time.Millisecond} // the batch rounds up to 1 s
	var log strings.Builder
	outs := RunBatch(cfg, b, ps, work, 2, &log)
	if got := outs[0]; got.Stdout != "out-a\n" || got.Stderr != "err-a\n" || got.ExitCode != 3 || !got.BuildOK {
		t.Fatalf("a: %+v", got)
	}
	if outs[1].Stdout != "batched=1\n" {
		t.Fatalf("b did not run in the batch: %q", outs[1].Stdout)
	}
	if !outs[2].TimedOut || outs[2].ExitCode != -1 {
		t.Fatalf("c: a batch timeout (124) must be TimedOut: %+v", outs[2])
	}
	// an isolated program runs in its own process, never in the batch
	if outs[3].Stdout != "Setrlimit batched=\n" {
		t.Fatalf("d (isolated) ran batched: %q", outs[3].Stdout)
	}
	// the same programs one process each: same outcomes but the marker
	t.Setenv("GRAALGO_BATCH", "off")
	if b.Batched() {
		t.Fatal("GRAALGO_BATCH=off must turn batching off")
	}
	for i, p := range ps {
		oc := RunProgram(cfg, b, p, work)
		if i == 1 {
			oc.Stdout = strings.Replace(oc.Stdout, "batched=", "batched=1", 1)
		}
		if oc.Stdout != outs[i].Stdout || oc.Stderr != outs[i].Stderr || oc.ExitCode != outs[i].ExitCode || oc.TimedOut != outs[i].TimedOut {
			t.Fatalf("%s: batch %+v vs per-process %+v", p.Name, outs[i], oc)
		}
	}
}

func TestRunBatch_DeadBatchRerunsTheRest(t *testing.T) {
	b, ps, work := batchFixture(t, map[string]string{
		"a": "//sh echo \"a batched=$FAKE_BATCHED\"\n",
		"b": "//sh echo \"b batched=$FAKE_BATCHED\"\n",
		"c": "//sh echo \"c batched=$FAKE_BATCHED\"\n",
	})
	b.Env = map[string]string{"FAKE_DIE_AFTER": "1"} // the batch runs in the minimal run environment
	var log strings.Builder
	outs := RunBatch(&Config{TimeoutSec: 5}, b, ps, work, 1, &log)
	want := []string{"a batched=1\n", "b batched=\n", "c batched=\n"}
	for i := range want {
		if outs[i].Stdout != want[i] || outs[i].ExitCode != 0 {
			t.Fatalf("%d: %+v (want stdout %q)", i, outs[i], want[i])
		}
	}
	if !strings.Contains(log.String(), "after 1 of 3 programs") || !strings.Contains(log.String(), "fake: dying") {
		t.Fatalf("a dead batch must be reported with its stderr: %q", log.String())
	}
}

func TestRunBatch_BuildFailureNeverRuns(t *testing.T) {
	b, ps, work := batchFixture(t, map[string]string{"a": "//sh echo hi\n"})
	b.Build = []string{"sh", "-c", "echo broken >&2; exit 1"}
	outs := RunBatch(&Config{TimeoutSec: 5}, b, ps, work, 1, &strings.Builder{})
	if outs[0].BuildOK || !strings.Contains(outs[0].BuildLog, "broken") || outs[0].Stdout != "" {
		t.Fatalf("%+v", outs[0])
	}
}

func TestLoadConfig_Batch(t *testing.T) {
	dir := t.TempDir()
	write := func(name, s string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("lib.sh", "x=1\nBATCH_ISOLATED_RE='Setrlimit|Umask'\n")
	ok := write("ok.json", `{"backends":[{"name":"g","build":["b"],"run":["{BIN}"],"batchRun":["r","{MANIFEST}","{RESULTS}"],"batchIsolatedFrom":"lib.sh"}]}`)
	c, err := LoadConfig(ok)
	if err != nil {
		t.Fatal(err)
	}
	if re := c.Backends[0].batchIsolated; re == nil || !re.MatchString("syscall.Umask(0)") || re.MatchString("os.Exit(1)") {
		t.Fatalf("pattern not loaded: %v", re)
	}
	for name, s := range map[string]string{
		"argv.json":    `{"backends":[{"name":"g","build":["b"],"run":["launcher","{SRC}"],"batchRun":["r"],"batchIsolatedFrom":"lib.sh"}]}`,
		"nopat.json":   `{"backends":[{"name":"g","build":["b"],"run":["{BIN}"],"batchRun":["r"]}]}`,
		"missing.json": `{"backends":[{"name":"g","build":["b"],"run":["{BIN}"],"batchRun":["r"],"batchIsolatedFrom":"nope.sh"}]}`,
	} {
		if _, err := LoadConfig(write(name, s)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// The real definition parses and matches what batchrun.sh isolates.
func TestLoadBatchIsolated_BatchrunSh(t *testing.T) {
	re, err := LoadBatchIsolated("../../../lib/batchrun.sh")
	if err != nil {
		t.Fatal(err)
	}
	for s, want := range map[string]bool{"syscall.Setrlimit(": true, "signal.Notify(c)": true, "os.Exit(3)": false, `fmt.Println("hi")`: false} {
		if re.MatchString(s) != want {
			t.Errorf("%q: isolated=%v, want %v", s, !want, want)
		}
	}
}
