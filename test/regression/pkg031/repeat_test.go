package pkg031

import (
	"strings"
	"testing"
)

// TestRepeatLargeCount is a regression test for PKG-031
// (graalgo test encoding/pem hangs).
//
// Root cause: the synthetic strings.Repeat used
//
//	b.writeString(b.String()[:chunk])
//
// in a loop. Builder.String() is O(n) in the synthetic package (it copies
// via string(buf); real Go uses unsafe for O(1)), and once the buffer
// exceeds the 8KB chunk limit every iteration re-copies the whole buffer,
// making Repeat O(n^2) in the output length.
//
// The upstream trigger is encoding/pem's TestCVE202224675
// (~/tools/go/src/encoding/pem/pem_test.go:163-165), which does
// strings.Repeat("-----BEGIN \n", 10000000) (120MB): pre-fix that performs
// hundreds of GB of buffer copying and never completes.
//
// The fix rewrites Repeat as a direct O(n) doubling copy within a single
// backing array, avoiding Builder.String() in the hot loop.
//
// This test is deliberately synchronous: an earlier version used a
// goroutine + time.After watchdog to turn the pre-fix hang into a test
// failure, but timer channels do not fire under graalgo (PKG-048: select
// on time.After hangs forever), so the guarded version hangs instead of
// failing. Failability rests on the outer timeout (`timeout 600` in the
// test-regression Makefile target).
//
// Sizing and failability: 5,000,000 repetitions of the 12-byte unit = 60MB.
// Pre-fix, the O(n^2) loop performs ~220GB of redundant full-buffer copies
// (n^2/(2*8KB)); under graalgo's interpreter this takes many hours —
// effectively a hang — and the run is killed by the outer timeout. (At
// 1.2MB the pre-fix run still completes in ~97s, so the 60MB size is
// load-bearing for the hang property.) Post-fix the Repeat call is a
// single O(n) pass and the package passes well under the gate timeout
// (measured 75-156s across runs, JVM-startup dominated).
func TestRepeatLargeCount(t *testing.T) {
	const (
		unit  = "-----BEGIN \n" // 12 bytes: the CVE-2022-24675 trigger unit
		count = 5000000         // 60MB total: pre-fix ~220GB of redundant copies
	)

	s := strings.Repeat(unit, count)
	wantLen := len(unit) * count
	if len(s) != wantLen {
		t.Fatalf("Repeat length = %d, want %d", len(s), wantLen)
	}
	// Spot-check the first, middle, and last units. A full per-unit scan
	// at 5M units would dominate the test's own runtime under graalgo's
	// interpreter; the head/middle/tail checks assert the doubling-copy
	// invariant and catch the systematic corruption modes of a broken
	// Repeat. Unit-indexed, so every checked offset is unit-aligned by
	// construction.
	for _, u := range []int{0, count / 2, count - 1} {
		off := u * len(unit)
		if s[off:off+len(unit)] != unit {
			t.Fatalf("Repeat content mismatch at unit %d (offset %d)", u, off)
		}
	}
}
