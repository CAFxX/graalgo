package graalgo.frontend;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.FrameSlotKind;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.RootCallTarget;
import graalgo.runtime.sync.GoAtomicInt64;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;

/**
 * Pins the by-value STRUCT parameter cloning in {@link GoFuncRootNode}
 * (RV18-P1-16): an atomic cell passed by value gets its own cell through
 * the structural {@code Copyable} dispatch, so the callee cannot observe
 * or affect the caller's cell — like gc. Pointer-kind params carry the
 * same cell object and are untouched by this path.
 */
public final class GoFuncRootNodeParamCopyTest {

    private static void assertEq(long want, long got, String what) {
        if (want != got) {
            throw new AssertionError(what + ": want " + want + " got " + got);
        }
    }

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError(what + ": expected true");
        }
    }

    private static final class NoopBody extends GoStmtNode {
        @Override
        public void executeVoid(VirtualFrame frame) {
        }
    }

    /**
     * Runs one STRUCT-kind by-value parameter through the root node and
     * returns what landed in the parameter slot.
     */
    private static Object runStructParam(Object arg) {
        FrameDescriptor fd = new FrameDescriptor();
        int closureSlot = fd.addSlot(FrameSlotKind.Object, "$closure", null);
        int paramSlot = fd.addSlot(FrameSlotKind.Object, "p0", null);
        GoFuncRootNode root = new GoFuncRootNode(fd, new NoopBody(), closureSlot,
                new int[]{paramSlot}, new GoKind[]{GoKind.STRUCT},
                -1, GoKind.STRUCT, "testfn");
        RootCallTarget target = root.getCallTarget();
        GoFunc fn = new GoFunc(target, null);
        VirtualFrame frame = Truffle.getRuntime()
                .createVirtualFrame(new Object[]{fn, arg}, fd);
        root.execute(frame);
        return frame.getObject(paramSlot);
    }

    public void testStructParamClonesAtomicInt64() {
        GoAtomicInt64 cell = new GoAtomicInt64(41);
        Object got = runStructParam(cell);
        assertTrue(got instanceof GoAtomicInt64, "param keeps the wrapper type");
        assertTrue(got != cell, "param is a fresh cell, not the caller's");
        assertEq(41, ((GoAtomicInt64) got).get(), "param carries the value");
    }

    public void testStructParamAddIsIndependent() {
        GoAtomicInt64 cell = new GoAtomicInt64(41);
        GoAtomicInt64 param = (GoAtomicInt64) runStructParam(cell);
        cell.addAndGet(1);
        assertEq(41, param.get(), "caller Add invisible to callee param");
        param.addAndGet(10);
        assertEq(42, cell.get(), "callee Add invisible to caller cell");
    }

    public void testCopyForAssignClonesAtomicCell() {
        GoAtomicInt64 cell = new GoAtomicInt64(7);
        Object got = Boxing.copyForAssign(GoKind.STRUCT, cell);
        assertTrue(got instanceof GoAtomicInt64, "assign keeps the wrapper type");
        assertTrue(got != cell, "assign clones the cell");
        assertEq(7, ((GoAtomicInt64) got).get(), "assign carries the value");
        cell.addAndGet(1);
        assertEq(7, ((GoAtomicInt64) got).get(),
                "Add on original invisible to assigned copy");
    }
}
