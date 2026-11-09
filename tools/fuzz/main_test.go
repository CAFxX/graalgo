package main

// Regression tests for R03-T5: NextSeed must be monotonic across lanes.
// Lanes finish out of seed order, so the last finisher — not the highest
// seed — used to win the NextSeed write, dragging resume backwards and
// re-running already-covered seeds. The full runner (cmdRun) shells out to
// gc/graalgo per seed and is not unit-testable in isolation, so these tests
// drive noteCompleted, the exact helper cmdRun calls under its mutex, plus
// the loadState/saveState round-trip the resume path reads.

import (
	"math/rand"
	"sync"
	"testing"
)

// TestNoteCompletedMonotonic is the minimal R03-T5 repro: seed 100
// completes first (NextSeed 101), then seed 50 completes late. NextSeed
// must stay 101. With the old `st.NextSeed = s + 1` assignment it
// regresses to 51.
func TestNoteCompletedMonotonic(t *testing.T) {
	st := state{Counts: map[string]int64{}}
	st.noteCompleted(100)
	if st.NextSeed != 101 {
		t.Fatalf("after seed 100: NextSeed = %d, want 101", st.NextSeed)
	}
	st.noteCompleted(50)
	if st.NextSeed != 101 {
		t.Fatalf("after late seed 50: NextSeed = %d, want 101 (regressed)", st.NextSeed)
	}
	// In-order completion still advances normally.
	st2 := state{Counts: map[string]int64{}}
	for s := int64(0); s < 10; s++ {
		st2.noteCompleted(s)
	}
	if st2.NextSeed != 10 {
		t.Fatalf("after seeds 0..9 in order: NextSeed = %d, want 10", st2.NextSeed)
	}
}

// TestNoteCompletedConcurrentOutOfOrder mirrors cmdRun: several lanes
// completing seeds in shuffled order under one mutex. The final NextSeed
// must be one past the highest seed, regardless of finish order.
func TestNoteCompletedConcurrentOutOfOrder(t *testing.T) {
	const n = 200
	seeds := rand.Perm(n)
	st := state{Counts: map[string]int64{}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for l := 0; l < 4; l++ {
		wg.Add(1)
		go func(lane int) {
			defer wg.Done()
			for i := lane; i < n; i += 4 {
				s := int64(seeds[i])
				mu.Lock()
				st.noteCompleted(s)
				mu.Unlock()
			}
		}(l)
	}
	wg.Wait()
	if st.NextSeed != n {
		t.Fatalf("after concurrent shuffled completion of 0..%d: NextSeed = %d, want %d", n-1, st.NextSeed, n)
	}
}

// TestResumePicksPersistedNextSeed pins the resume contract: the NextSeed
// persisted by saveState is the seed resume restarts from (cmdRun: `if
// *resume && st.NextSeed > next { next = st.NextSeed }`), so a regressed
// persisted value would re-run covered seeds. Round-trips through the real
// saveState/loadState.
func TestResumePicksPersistedNextSeed(t *testing.T) {
	dir := t.TempDir()
	st := state{NextSeed: 101, Counts: map[string]int64{"match": 100}}
	saveState(dir, st)
	loaded := loadState(dir)
	if loaded.NextSeed != 101 {
		t.Fatalf("loadState NextSeed = %d, want 101", loaded.NextSeed)
	}
	// Mirror the cmdRun resume branch.
	var next int64
	if loaded.NextSeed > next {
		next = loaded.NextSeed
	}
	if next != 101 {
		t.Fatalf("resume start = %d, want 101", next)
	}
}
