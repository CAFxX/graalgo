package main

import "testing"

// G5: only a divergence verdict kills a mutant; a harness error that also
// exits 1 (snapshot/corpus inconsistency, bad flags) must not.
func TestReportsDivergence(t *testing.T) {
	for out, want := range map[string]bool{
		"DRIFT     50_shifts   stdout differs at line 3\ncheck: ...\n":                            true,
		"STALE     58_x  listed but matches gc\n":                                                 true,
		"diffharness snapshot: error: snapshot/corpus inconsistency:\n  a: not in the snapshot\n": false,
		"flag provided but not defined: -bogus\n":                                                 false,
		"": false,
	} {
		if got := reportsDivergence(out); got != want {
			t.Errorf("reportsDivergence(%q) = %v, want %v", out, got, want)
		}
	}
}
