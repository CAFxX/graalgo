package harness

import (
	"testing"
	"time"
)

// A reference (oracle) timeout is retried once with an extended timeout; a
// pass on the retry is the program's outcome, a persistent timeout is INFRA.
func TestRunReference_RetryPolicy(t *testing.T) {
	timedOut := Outcome{TimedOut: true, ExitCode: -1}
	ok := Outcome{ExitCode: 0, Stdout: "x"}
	for _, tc := range []struct {
		name      string
		outs      []Outcome // successive run results
		wantCalls int
		wantInfra bool
		wantRetry bool
		wantScale []int
	}{
		{"no timeout: single run", []Outcome{ok}, 1, false, false, []int{4}},
		{"timeout then ok: retried, not infra", []Outcome{timedOut, ok}, 2, false, true, []int{4, 8}},
		{"timeout twice: infra", []Outcome{timedOut, timedOut}, 2, true, false, []int{4, 8}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var scales []int
			i := 0
			oc := runReference(func(p Program) Outcome {
				scales = append(scales, p.timeoutScale())
				o := tc.outs[i]
				i++
				return o
			}, Program{Name: "p", TimeoutScale: 4})
			if i != tc.wantCalls {
				t.Fatalf("runs = %d, want %d", i, tc.wantCalls)
			}
			for k, w := range tc.wantScale[:i] {
				if scales[k] != w {
					t.Fatalf("run %d scale = %d, want %d (all %v)", k, scales[k], w, scales)
				}
			}
			if RefInfra(oc) != tc.wantInfra || oc.RetriedTimeout != tc.wantRetry {
				t.Fatalf("infra=%v retried=%v, want %v %v", RefInfra(oc), oc.RetriedTimeout, tc.wantInfra, tc.wantRetry)
			}
		})
	}
}

func TestProgramTimeout_Scale(t *testing.T) {
	cfg := &Config{TimeoutSec: 30}
	if got := (Program{}).timeout(cfg); got != 30*time.Second {
		t.Fatalf("default = %v", got)
	}
	if got := (Program{TimeoutScale: 4}).timeout(cfg); got != 120*time.Second {
		t.Fatalf("scaled = %v", got)
	}
	cfg.TimeoutUnit = time.Millisecond
	if got := (Program{TimeoutScale: 4}).Extended().timeout(cfg); got != 240*time.Millisecond {
		t.Fatalf("extended = %v", got)
	}
}

// A build that exceeds buildTimeout is flagged, so a reference build timeout
// is INFRA too.
func TestBuildOnly_TimeoutFlagged(t *testing.T) {
	old := buildTimeout
	buildTimeout = 100 * time.Millisecond
	defer func() { buildTimeout = old }()
	b := &Backend{Name: "slowbuild", Build: []string{"sleep", "30"}, Run: []string{"{BIN}"}}
	oc := BuildOnly(&Config{TimeoutSec: 1}, b, testProg("hello"), t.TempDir())
	if oc.BuildOK || !oc.BuildTimedOut || !RefInfra(oc) {
		t.Fatalf("want a flagged build timeout, got %+v", oc)
	}
	// An ordinary build failure is not a timeout.
	bad := &Backend{Name: "bad", Build: []string{"false"}, Run: []string{"{BIN}"}}
	if oc := BuildOnly(&Config{TimeoutSec: 1}, bad, testProg("hello"), t.TempDir()); oc.BuildTimedOut {
		t.Fatal("plain build failure flagged as timeout")
	}
}
