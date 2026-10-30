package graalgo.p3link;

import graalgo.runtime.PanicException;

/**
 * Panic-contract pins for the unimplemented synctest bubbles
 * (codebase review 2026-10-30, finding R1-8; lane L08).
 *
 * <p>Today {@link Synctest} implements no bubbles, and that gap is
 * fail-loud, not silent: {@code synctest.Run} and
 * {@code synctest.inBubble} raise a Go panic instead of running the
 * function without a bubble (which would silently drop the fake-time
 * and deadlock-detection semantics), and {@code synctest.Wait}
 * outside a bubble panics exactly as gc does. These tests pin that
 * contract at the Java level, so a future change that accidentally
 * runs a bubble function without bubble semantics — or that
 * changes a panic message a guest program's {@code recover()} may
 * observe — fails here instead of silently changing behaviour.
 *
 * <p>The no-bubble answers that are already gc-faithful outside a
 * bubble are pinned alongside the panics: no goroutine is ever in a
 * bubble, nothing is ever associated, and {@code acquire} yields
 * nil, so {@code release} of that nil is a no-op.
 *
 * <p>Tripwire: when real bubbles land (see
 * {@code docs/arch/synctest-bubble-design.md}), {@code testRunPanics}
 * and {@code testInBubblePanics} MUST start failing — a bubble
 * implementation that still panics on {@code Run} is not an
 * implementation — and this class must be replaced by bubble
 * semantics tests, not quietly edited to accept the old panics
 * alongside the new behaviour. The two tests are the red side of
 * that transition by design.
 */
public final class SynctestContractTest {

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(String expected, String actual, String what) {
        if (!expected.equals(actual)) {
            throw new AssertionError(
                    what + ": expected <" + expected + "> but got <" + actual + ">");
        }
    }

    /** Runs {@code body} and returns the Go panic message, or fails if it does not panic. */
    private static String panicMessage(Runnable body, String what) {
        try {
            body.run();
        } catch (PanicException pe) {
            return pe.goValue().toString();
        }
        throw new AssertionError(what + ": expected a Go panic, but the call returned normally");
    }

    public void testRunPanics() {
        assertEq("synctest.Run: not implemented by graalgo",
                panicMessage(() -> Synctest.run(new Object()), "Synctest.run"),
                "Synctest.run panic message");
    }

    public void testInBubblePanics() {
        assertEq("synctest.inBubble: not implemented by graalgo",
                panicMessage(() -> Synctest.inBubble(null, new Object()), "Synctest.inBubble"),
                "Synctest.inBubble panic message");
    }

    public void testWaitOutsideBubblePanicsLikeGc() {
        // gc's exact unbubbled behaviour; under graalgo every call is
        // unbubbled, so this is faithful, not a placeholder.
        assertEq("goroutine is not in a bubble",
                panicMessage(Synctest::waitForBubble, "Synctest.waitForBubble"),
                "Synctest.waitForBubble panic message");
    }

    public void testNoBubbleAnswers() {
        assertTrue(!Synctest.isInBubble(), "isInBubble must be false: no bubbles exist");
        assertTrue(Synctest.associate(0x1000L) == 0L,
                "associate must return Unbubbled (0) outside any bubble");
        assertTrue(!Synctest.isAssociated(0x1000L),
                "isAssociated must be false: nothing is associated");
        assertTrue(Synctest.acquire() == null,
                "acquire must yield nil: there is no bubble to acquire");
        // The only reachable release argument is the nil acquire yields;
        // it must not throw (gc would panic on a nil release, but gc can
        // only reach release with a real bubble; see Synctest javadoc).
        Synctest.release(null);
        Synctest.disassociate(0x1000L);
    }

    public void testRunPanicsWithoutRunningTheFunction() {
        // The panic must precede any execution of f: a fake bubble that
        // ran f would be the silent semantic drop this contract forbids.
        // Synctest.run takes f as an opaque Object at this layer, so the
        // observable half of the property is the panic itself (asserted
        // in testRunPanics); this test additionally pins that the panic
        // is raised even when f is null, i.e. no dispatch on f happens
        // before the fail-loud check.
        assertEq("synctest.Run: not implemented by graalgo",
                panicMessage(() -> Synctest.run(null), "Synctest.run(null)"),
                "Synctest.run(null) panic message");
    }
}
