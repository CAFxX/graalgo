package pkg031

import (
	"strings"
	"testing"
	"time"
)

// TestRepeatLargeCount is a regression test for PKG-031
// (graalgo test encoding/pem hangs).
//
// Root cause: the synthetic strings.Repeat used
//   b.writeString(b.String()[:chunk])
// in a loop. Builder.String() is O(n) in the synthetic package (it copies
// via string(buf); real Go uses unsafe for O(1)), making Repeat O(n^2).
// For the large count used in encoding/pem's TestFuzz
// (strings.Repeat("-----BEGIN \n", 10000000)), this never completes.
//
// The fix rewrites Repeat to use a direct O(n) doubling copy.
//
// This test uses a 30-second timeout: pre-fix it hangs (times out),
// post-fix it completes in well under a second.
func TestRepeatLargeCount(t *testing.T) {
	done := make(chan string, 1)
	go func() {
		// 100000 repetitions of a 12-byte string = 1.2MB.
		// Pre-fix: O(n^2) work — effectively hangs.
		// Post-fix: O(n) — completes quickly.
		done <- strings.Repeat("-----BEGIN \n", 100000)
	}()

	select {
	case s := <-done:
		wantLen := 12 * 100000
		if len(s) != wantLen {
			t.Fatalf("Repeat length = %d, want %d", len(s), wantLen)
		}
		for i := 0; i < 100000; i++ {
			if s[i*12:(i+1)*12] != "-----BEGIN \n" {
				t.Fatalf("Repeat content mismatch at repetition %d", i)
			}
		}
	case <-time.After(30 * time.Second):
		t.Fatal("TestRepeatLargeCount timed out after 30s (PKG-031: strings.Repeat hangs)")
	}
}
