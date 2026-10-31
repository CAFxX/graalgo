package graalgo.p3unsafe;

import graalgo.runtime.PanicException;
import graalgo.values.GoString;
import graalgo.values.UnsafeHandles;

/**
 * review-20261031 R-06: the unproven {@code uintptr → unsafe.Pointer}
 * conversion (the {@code "unsafefromuint"} node) must panic with a Go
 * panic — recoverable by guest code, exit 2 uncaught — never a host
 * internal error. These tests fail on the pre-fix backend, where
 * {@code UnsafeOps.fromUintTrap} threw
 * {@link UnsafeHandles.UnsafeEscapeException}: the launcher's generic
 * {@code RuntimeException} catch turned that into
 * "graalgo-run: execution failed" with exit 1, which no guest
 * {@code recover()} could catch.
 */
public final class UnsafeFromUintTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    public void testFromUintPanicsAsGoPanic() {
        try {
            UnsafeOps.fromUintPanic(null);
            throw new AssertionError("unproven uintptr -> unsafe.Pointer must panic");
        } catch (PanicException pe) {
            assertTrue(pe.goValue() instanceof GoString,
                    "panic value must be a GoString so the uncaught path renders the message "
                            + "(a raw Java string renders as \"(?) 0xADDR\")");
            String msg = ((GoString) pe.goValue()).toString();
            assertTrue(msg.contains("uintptr -> unsafe.Pointer"),
                    "panic message must name the conversion, got: " + msg);
            assertTrue(msg.contains("re-test: byte-addressed value model"),
                    "panic message must carry the re-test trigger, got: " + msg);
            assertTrue(!msg.contains("runtime error:"),
                    "the message must not masquerade as a gc runtime error (gc would not panic here), got: " + msg);
        } catch (UnsafeHandles.UnsafeEscapeException e) {
            throw new AssertionError(
                    "unproven uintptr -> unsafe.Pointer must not trap as a host internal error", e);
        }
    }

    public void testFromUintMessageIsStable() {
        assertTrue(UnsafeOps.FROM_UINT_MESSAGE.contains("D5"),
                "the documented message must keep its D5 tag");
    }
}
