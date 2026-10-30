package main

import (
	"strings"
	"testing"
)

// TestSyntheticMaphashValueDivergencePinned pins the L13 (review
// 2026-10-30, R1-20) scoped divergence as EXPLICITLY DIVERGENT, not as
// correct: the synthetic hash/maphash values are deterministic per fixed
// seed but NOT gc-identical (gc funnels through runtime.memhash, which
// no pure-Go synthetic reproduces bit-exactly).
//
// Numeric values cannot be pinned from user code — Seed is opaque and
// MakeSeed is random, so no program can name a fixed seed — and the
// wave-3 corpus therefore pins only structural properties. This test
// pins what CAN be pinned: the algorithm identity (the splitmix64
// finalizer constants in mix64) and the explicit divergence note in the
// synthetic source header. A silent algorithm swap, or a softened
// divergence note that would let a reader mistake graalgo values for
// gc values, fails this test instead of drifting silently.
func TestSyntheticMaphashValueDivergencePinned(t *testing.T) {
	for _, want := range []string{
		// splitmix64 finalizer constants (mix64 in synth_hash_maphash.go).
		"0xbf58476d1ce4e5b9",
		"0x94d049bb133111eb",
		"func mix64(z uint64) uint64",
		// The divergence must stay stated, not softened (wording as
		// it appears inside the synthetic source itself).
		"VALUE DIVERGENCE",
		"NOT gc-identical",
		"runtime memhash",
	} {
		if !strings.Contains(syntheticMaphashSource, want) {
			t.Errorf("syntheticMaphashSource missing %q (algorithm identity / divergence note changed?)", want)
		}
	}
}
