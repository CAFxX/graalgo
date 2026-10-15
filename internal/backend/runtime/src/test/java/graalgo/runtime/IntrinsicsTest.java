package graalgo.runtime;

/**
 * Unit tests for {@link Intrinsics} memory primitives (P2-M2).
 *
 * <p>Red-green discipline: memequal must validate ranges like memmove
 * (P1-R4-3, review 2026-10-15) — an out-of-range call must throw
 * {@link IndexOutOfBoundsException} (which callers translate to a Go
 * panic), never a raw {@link ArrayIndexOutOfBoundsException} escaping
 * as a scheduler "internal error".
 */
public final class IntrinsicsTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    /** Happy path: equal and unequal ranges across element types. */
    public void testMemequalHappyPath() {
        byte[] a = {1, 2, 3, 4};
        byte[] b = {0, 1, 2, 3, 4, 0};
        assertTrue(Intrinsics.memequal(a, 0, b, 1, 4), "equal byte ranges");
        assertTrue(!Intrinsics.memequal(a, 0, b, 0, 4), "unequal byte ranges");
        int[] ia = {7, 8, 9};
        int[] ib = {7, 8, 0};
        assertTrue(Intrinsics.memequal(ia, 0, ia, 0, 3), "self equal");
        assertTrue(!Intrinsics.memequal(ia, 0, ib, 0, 3), "unequal int ranges");
        assertTrue(Intrinsics.memequal(ia, 0, ib, 0, 0), "zero count equal");
    }

    /**
     * Out-of-range reads throw IndexOutOfBoundsException (Go panic at
     * the caller), not a raw ArrayIndexOutOfBoundsException from deep
     * in the loop. P1-R4-3.
     */
    public void testMemequalBoundsChecked() {
        byte[] a = {1, 2, 3};
        byte[] b = {1, 2, 3};
        expectIobe(() -> Intrinsics.memequal(a, 0, b, 0, 4), "count past end");
        expectIobe(() -> Intrinsics.memequal(a, 2, b, 0, 2), "aOff+n past end");
        expectIobe(() -> Intrinsics.memequal(a, 0, b, 2, 2), "bOff+n past end");
        expectIobe(() -> Intrinsics.memequal(a, -1, b, 0, 1), "negative aOff");
        expectIobe(() -> Intrinsics.memequal(a, 0, b, -1, 1), "negative bOff");
        expectIobe(() -> Intrinsics.memequal(a, 0, b, 0, -1), "negative count");
    }

    /**
     * Parity with memmove: both reject the same out-of-range shapes.
     */
    public void testMemequalMatchesMemmoveDiscipline() {
        byte[] a = {1, 2, 3};
        byte[] b = {1, 2, 3};
        byte[] dst = new byte[3];
        expectIobe(() -> Intrinsics.memmove(dst, 0, a, 0, 4), "memmove count past end");
        expectIobe(() -> Intrinsics.memequal(a, 0, b, 0, 4), "memequal count past end");
    }

    private interface ThrowingRunnable {
        void run();
    }

    private static void expectIobe(ThrowingRunnable r, String what) {
        try {
            r.run();
        } catch (IndexOutOfBoundsException e) {
            return; // expected: callers translate to a Go panic
        }
        throw new AssertionError("expected IndexOutOfBoundsException: " + what);
    }
}
