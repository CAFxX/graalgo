// Regression tests for REVIEW-2026-11-04-11 NEW-P0-11: time.Now() and
// timer-channel values were stamped with System.currentTimeMillis()*1e6,
// so every UnixNano() was a multiple of 10^6, sub-millisecond durations
// measured 0, successive Now() calls within a millisecond compared equal,
// and Timer.C/Ticker.C values were delivery wall time rather than the
// heap fire instant.
//
// The sharp discriminator in every test below is the sub-millisecond
// residue: a millisecond-quantized clock scores 0 nonzero residues out
// of N, while a nanosecond clock scores ~N (a single sample lands exactly
// on a millisecond boundary with probability ~1e-6). Timing windows are
// deliberately generous: under load the scheduler overshoots sleeps by
// tens of milliseconds, so bounds only catch gross errors (wrong epoch,
// delivery-vs-fire confusion at the 100ms scale), never scheduling noise.
package timenanores

import (
	"testing"
	"time"
)

func TestNowHasSubMillisecondResolution(t *testing.T) {
	nonzero := 0
	prev := time.Now()
	distinct := false
	for i := 0; i < 200; i++ {
		cur := time.Now()
		if !cur.Equal(prev) {
			distinct = true
		}
		prev = cur
		if cur.UnixNano()%1000000 != 0 {
			nonzero++
		}
	}
	if !distinct {
		t.Fatal("time.Now() never advanced over 200 samples")
	}
	if nonzero < 100 {
		t.Fatalf("time.Now() lacks sub-millisecond resolution: nonzero residues=%d/200", nonzero)
	}
}

func TestSubMillisecondDuration(t *testing.T) {
	nonzero := 0
	for i := 0; i < 10; i++ {
		start := time.Now()
		time.Sleep(200 * time.Microsecond)
		d := time.Since(start)
		if d <= 0 {
			t.Fatalf("sub-millisecond sleep measured non-positive duration: %v", d)
		}
		if d > 100*time.Millisecond {
			t.Fatalf("sub-millisecond sleep measured implausibly long duration: %v", d)
		}
		if d.Nanoseconds()%1000000 != 0 {
			nonzero++
		}
	}
	if nonzero == 0 {
		t.Fatal("all sub-millisecond durations are whole milliseconds (quantized clock)")
	}
}

func TestTimerValueIsFireTime(t *testing.T) {
	start := time.Now()
	tm := time.NewTimer(50 * time.Millisecond)
	v := <-tm.C
	fire := v.Sub(start)
	if fire < 25*time.Millisecond || fire > 150*time.Millisecond {
		t.Fatalf("timer value is not the fire instant: fire-minus-start=%v (want ~50ms)", fire)
	}
	if v.UnixNano()%1000000 == 0 {
		t.Fatalf("timer value is millisecond-quantized: %v", v)
	}
}

func TestDelayedTimerReadStillFireTime(t *testing.T) {
	// The fire value sits buffered while the receiver is busy; reading
	// it later must still yield the fire instant, not the read time.
	// (This pins the semantics; the translator already delivered into
	// the buffer at fire time, so it does not by itself discriminate
	// the old delivery-stamp bug — the residue tests above do that.)
	start := time.Now()
	tm := time.NewTimer(20 * time.Millisecond)
	time.Sleep(120 * time.Millisecond)
	v := <-tm.C
	fire := v.Sub(start)
	if fire < 10*time.Millisecond || fire > 100*time.Millisecond {
		t.Fatalf("delayed timer read is not the fire instant: fire-minus-start=%v (want ~20ms)", fire)
	}
}

func TestTickerValuesHaveSubMillisecondResolution(t *testing.T) {
	tk := time.NewTicker(30 * time.Millisecond)
	defer tk.Stop()
	nonzero := 0
	for i := 0; i < 5; i++ {
		v := <-tk.C
		if v.UnixNano()%1000000 != 0 {
			nonzero++
		}
	}
	if nonzero == 0 {
		t.Fatal("all ticker values are whole milliseconds (quantized clock)")
	}
}
