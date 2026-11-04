package graalgo.p3time;

/**
 * Unit tests for the P3TimeRuntime wall-clock helpers (REVIEW-2026-11-04-11
 * NEW-P0-11): {@code time.Now()} and timer-channel values must carry
 * nanosecond-resolution wall time, and timer values must be the heap
 * FIRE instant, not delivery wall time.
 *
 * <p>These pin the two failure modes at the unit level:
 * <ul>
 *   <li>{@code wallClockNanos()} quantized to milliseconds (the old
 *       {@code currentTimeMillis() * 1e6} shape): every sample's
 *       sub-millisecond residue would be 0.
 *   <li>{@code wallNanosForMonotonicFire} using the heap Long directly
 *       as a unixnano: the heap value is nanoTime-based, so the result
 *       would read as a January-1970 date (order 1e15, not 1e18).
 * </ul>
 *
 * <p>verifies: GG-0017 (time slice)
 */
public final class P3TimeWallClockTest {

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    public void testWallClockHasSubMillisecondResolution() {
        int nonzero = 0;
        long prev = 0;
        boolean distinct = false;
        for (int i = 0; i < 200; i++) {
            long w = P3TimeRuntime.wallClockNanos();
            assertTrue(w >= prev, "wall clock went backwards: prev=" + prev + " now=" + w);
            if (w != prev) {
                distinct = true;
            }
            prev = w;
            if (w % 1_000_000L != 0) {
                nonzero++;
            }
        }
        assertTrue(distinct, "wall clock never advanced over 200 samples");
        // A millisecond-quantized clock scores 0 here; a nanosecond
        // clock scores ~200 (only a 1-in-1e6 sample lands exactly on a
        // millisecond boundary). The half-way bar tolerates coarse
        // platform clocks without admitting the quantized shape.
        assertTrue(nonzero >= 100,
                "wall clock lacks sub-millisecond resolution: nonzero residues=" + nonzero + "/200");
    }

    public void testFireConversionIsPlausibleWallTime() {
        long fireMono = System.nanoTime();
        long converted = P3TimeRuntime.wallNanosForMonotonicFire(fireMono);
        long now = P3TimeRuntime.wallClockNanos();
        // Plausible unixnano: after 2001-09-09 (1e18 ns). A direct
        // nanoTime-as-unixnano misuse lands near 1e12..1e15 (1970).
        assertTrue(converted > 1_000_000_000_000_000_000L,
                "fire conversion is not a wall unixnano: " + converted);
        long skew = Math.abs(converted - now);
        assertTrue(skew < 100_000_000L,
                "fire conversion skew vs wall now too large: " + skew + "ns");
    }

    public void testFireConversionPreservesAge() {
        // A fire 50ms ago (monotonic basis) must convert to a wall time
        // ~50ms in the past — not to delivery wall time (age ~0), which
        // is what stamping delivery time would produce.
        long fireMono = System.nanoTime() - 50_000_000L;
        long converted = P3TimeRuntime.wallNanosForMonotonicFire(fireMono);
        long age = P3TimeRuntime.wallClockNanos() - converted;
        assertTrue(age > 25_000_000L,
                "fire conversion lost the fire age (delivery-stamp shape): age=" + age + "ns");
        assertTrue(age < 100_000_000L,
                "fire conversion age implausibly large: age=" + age + "ns");
    }
}
