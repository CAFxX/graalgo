package main

// Delete-during-range over a map with primitive (int) carriers.
//
// RV18-P1-14 (review remediation): RangeMapNode snapshotted the key set at
// range entry and produced every snapshotted key; a not-yet-reached entry
// deleted during iteration made map.get(key) return null, and
// Boxing.setSlot unboxed it into a primitive frame slot — a raw Java
// NullPointerException (gc never produces a Java NPE). The fix restores the
// production-time containsKey liveness check: a not-yet-reached entry
// removed during iteration is not produced (Go spec; verified against gc).
//
// Output goes to stderr via the println builtin (bi04/bi07 precedent);
// stdout stays empty.
func main() {
	// Case 1: delete the CURRENT key inside the loop. The entry was already
	// reached and produced, so every entry is still produced exactly once.
	m1 := map[int]int{1: 10, 2: 20, 3: 30, 4: 40, 5: 50, 6: 60, 7: 70, 8: 80}
	sum1 := 0
	cnt1 := 0
	for k, v := range m1 {
		delete(m1, k)
		sum1 += v
		cnt1++
	}
	println("delcur:", sum1, cnt1)

	// Case 2: on the first iteration, delete every not-yet-reached key.
	// All values are equal so the observed output is order-independent.
	m2 := map[int]int{1: 10, 2: 10, 3: 10, 4: 10, 5: 10, 6: 10, 7: 10, 8: 10}
	sum2 := 0
	cnt2 := 0
	first := true
	for k, v := range m2 {
		if first {
			first = false
			for d := 1; d <= 8; d++ {
				if d != k {
					delete(m2, d)
				}
			}
		}
		sum2 += v
		cnt2++
	}
	println("delnotreached:", sum2, cnt2)
}
