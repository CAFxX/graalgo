package graalgo.iface;

import java.util.HashMap;
import java.util.List;
import java.util.Map;

import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;

import graalgo.runtime.PanicException;
import graalgo.values.GoFunc;
import graalgo.values.GoString;

/**
 * Regression test for the N4 codebase review (2026-10-15, P1-N6):
 * FmtHandleMethods swallowed itab-lookup failures in an empty
 * {@code catch (Exception e) {}}, leaving a null itab in the
 * {@code fmt.State} value handed to user {@code Format} methods — the
 * failure was silent and formatting fell back to a wrong strategy with
 * no error.
 *
 * <p>The lookup is now fail-loud: a failure raises a Go panic
 * (PanicException) via {@link FmtHandleMethods#stateItab}, and Fmt's
 * method-dispatch lets Go panics through instead of swallowing them.
 * These tests pin the fail-loud contract at the lookup boundary.
 */
public final class FmtStateItabTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    /**
     * N4 P1-N6: an itab lookup failure must panic loudly — never return
     * null, never swallow. A bogus pair forces the failure path
     * deterministically.
     */
    public void testStateItabFailurePanicsLoud() {
        boolean panicked = false;
        try {
            FmtHandleMethods.stateItab("bogus.Type", "bogus.Iface");
        } catch (PanicException pe) {
            panicked = true;
            Object v = pe.goValue();
            assertTrue(v instanceof GoString,
                    "panic value must be a Go string, got " + v);
            String msg = v.toString();
            assertTrue(msg.contains("bogus.Type") && msg.contains("bogus.Iface"),
                    "panic message must name the failing pair: " + msg);
        }
        assertTrue(panicked,
                "stateItab must raise a Go panic on lookup failure, not swallow it");
    }

    /**
     * The happy path still works: with the fmt.State interface entry and
     * the PrinterState method table installed (as the phase-3 launcher
     * does), the itab for the real (PrinterState, fmt.State) pair builds.
     *
     * <p>The tables are installed explicitly here rather than relying on
     * FmtHandleMethods' one-shot static initializer: earlier tests in the
     * runner call {@link IfaceMethodTable#clear()}, which wipes the
     * initializer's registrations.
     */
    public void testStateItabBuildsForRealPair() {
        IfaceMethodTable.clear();
        IfaceMethodTable.installInterfaces(Map.of(
                "fmt.State", new IfaceMethodTable.IfaceWireEntry(77,
                        List.of("Write", "Width", "Precision", "Flag"))));
        Map<String, IfaceMethodTarget> methods = new HashMap<>();
        for (String m : List.of("Write", "Width", "Precision", "Flag")) {
            methods.put(m, new IfaceMethodTarget(goFunc(), new int[0], false, false, -1, null));
        }
        IfaceMethodTable.install(Map.of("internal/graalgo/fmtstate.PrinterState", methods));
        IfaceItab itab = FmtHandleMethods.stateItab(
                "internal/graalgo/fmtstate.PrinterState", "fmt.State");
        assertTrue(itab != null,
                "itab must build for the real (PrinterState, fmt.State) pair");
        assertTrue(itab.ifaceId() == 77,
                "itab must use the installed wire id, got " + itab.ifaceId());
    }

    private static GoFunc goFunc() {
        RootNode root = new RootNode(null) {
            @Override
            public Object execute(VirtualFrame frame) {
                return null;
            }
        };
        RootCallTarget target = root.getCallTarget();
        return new GoFunc(target, null, null);
    }
}
