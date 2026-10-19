package main

import "time"

// tc17 (P1-7): timer channels must deliver the fire instant — with
// nanosecond granularity (not a millis-quantized re-sample of the
// delivery time) and within a tolerance band of start+d.
//
// Self-checking for the differential harness: prints the fixed string
// "firetime-ok" on success (builtin println, stderr — the timectx corpus
// convention, cf. tc03); any failed assertion panics, which the harness
// flags via the exit-code mismatch (gc exits 0).
//
// NOTE: fmt.Println is unusable in corpus tests on this master
// (frontend "assign.fields: lhs/rhs length mismatch" — pre-existing,
// breaks test/corpus/sync/sy11_once_panic_done.go too), hence the
// builtin println + panic scheme.
//
// Reference (gc 1.27.1, measured): 20/20 After(100ms) fires report
// fire%1e6 != 0, elapsed 100.006-100.008ms. The band below absorbs
// scheduler jitter (graalgo's heap fires on wakeup) while an epoch
// error in the mono->wall conversion would shift the reading by
// decades and fail loudly.
func main() {
	const d = 100 * time.Millisecond
	const n = 20
	granular := 0
	var minE, maxE int64
	for i := 0; i < n; i++ {
		start := time.Now()
		t := <-time.After(d)
		fire := t.UnixNano()
		e := fire - start.UnixNano()
		if fire%1_000_000 != 0 {
			granular++
		}
		if i == 0 || e < minE {
			minE = e
		}
		if i == 0 || e > maxE {
			maxE = e
		}
	}
	// (a) nanosecond granularity: a millis-quantized clock reports
	// fire%1e6 == 0 on every sample (P(all 20 non-zero by chance) = 1e-120).
	if granular != n {
		println("FAIL granularity:", granular, "of", n, "non-millis-quantized")
		panic("tc17: timer fire time is millis-quantized")
	}
	// (b) fire-time accuracy band around the expected fire instant.
	lo := int64(d) - int64(2*time.Millisecond)
	hi := int64(d) + int64(500*time.Millisecond)
	if minE < lo || maxE > hi {
		println("FAIL elapsed out of band")
		panic("tc17: timer fire time outside tolerance band")
	}
	println("firetime-ok")
}
