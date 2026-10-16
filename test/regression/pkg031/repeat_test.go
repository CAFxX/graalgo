package pkg031

import (
	"strings"
	"testing"
)

// TestRepeatLargeCount is a regression test for PKG-031
// (graalgo test encoding/pem hangs).
//
// Root cause: the synthetic strings.Repeat used
//   b.writeString(b.String()[:chunk])
// in a loop. Builder.String() is O(n) in the synthetic package (it copies
// via string(buf); real Go uses unsafe for O(1)), making Repeat O(n^2).
// For the large count used in encoding/pem's TestCVE202224675
// (strings.Repeat("-----BEGIN \n", 10000000)), this never completes.
//
// The fix rewrites Repeat to use a direct O(n) doubling copy.
//
// NOTE: this test is deliberately synchronous and uses O(n) spot checks.
// The original version used a goroutine + time.After timeout guard to turn
// the pre-fix hang into a test failure, but timer channels do not fire
// under graalgo yet, so the guarded version hangs instead of failing (see
// the PKG-031 lane report). A full 100k-position verification loop is also
// too slow under graalgo (73s); spot checks are sufficient. Post-fix the
// 1.2MB Repeat completes in well under a second; pre-fix the O(n^2) work
// takes minutes and is killed by the suite's outer timeout.
func TestRepeatLargeCount(t *testing.T) {
	// 100000 repetitions of a 12-byte string = 1.2MB.
	// Pre-fix: O(n^2) work — effectively hangs.
	// Post-fix: O(n) — completes quickly.
	const unit = "-----BEGIN \n"
	const count = 100000
	s := strings.Repeat(unit, count)
	if len(s) != len(unit)*count {
		t.Fatalf("Repeat length = %d, want %d", len(s), len(unit)*count)
	}
	// Spot-check content: prefix, suffix, and an interior window.
	// (A full strings.Count verification is O(n*m) and too slow under
	// graalgo's interpreter for 1.2MB; the spot checks plus the length
	// assert the doubling-copy invariant.)
	if !strings.HasPrefix(s, unit) || !strings.HasSuffix(s, unit) {
		t.Fatal("Repeat content mismatch at prefix/suffix")
	}
	mid := len(s) / 2
	if s[mid:mid+len(unit)] != unit {
		t.Fatal("Repeat content mismatch at midpoint")
	}
}
