package graalgo.go.nodes;

import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.FrameSlotKind;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;
import graalgo.go.nodes.N.Stmt;
import graalgo.go.values.Closure;

/**
 * Closure environments and frame slots (truffle-audit WP 5, ADR-0023): {@link Calls.MakeClosure}
 * builds the {@link graalgo.go.values.Env} layout of its capture count (none: one constant func
 * value; one: the captured box itself; more: an {@code Object[]} read from the slots at each
 * evaluation), {@link Calls.EnvRead} reads every layout back, {@link Calls.Bind1} binds its
 * receiver as a one-value environment, and {@link N.ClearRefs} clears exactly its reference
 * slots of a static-slot frame. In-process, no engine: milliseconds.
 */
public final class FrameEnvTest {
    private static final RootCallTarget TARGET = new RootNode(null) {
        @Override
        public Object execute(VirtualFrame frame) {
            return null;
        }
    }.getCallTarget();

    private static final FrameDescriptor FD;
    private static final int A;
    private static final int B;
    private static final int C;
    private static final int N0;

    static {
        FrameDescriptor.Builder b = FrameDescriptor.newBuilder();
        A = b.addSlot(FrameSlotKind.Static, "a", null);
        B = b.addSlot(FrameSlotKind.Static, "b", null);
        C = b.addSlot(FrameSlotKind.Static, "c", null);
        N0 = b.addSlot(FrameSlotKind.Static, "n", null);
        FD = b.build();
    }

    private static VirtualFrame frame(Object... args) {
        VirtualFrame f = Truffle.getRuntime().createVirtualFrame(args, FD);
        f.setObjectStatic(A, "a0");
        f.setObjectStatic(B, "b0");
        f.setObjectStatic(C, "c0");
        f.setLongStatic(N0, 42);
        return f;
    }

    private static void check(boolean ok, String what) {
        if (!ok) {
            throw new AssertionError(what);
        }
    }

    private static Closure make(int[] slots, VirtualFrame f) {
        return (Closure) new Calls.MakeClosure(() -> TARGET, slots).executeGeneric(f);
    }

    private static Object read(Closure c, int index, int size) {
        return new Calls.EnvRead(index, size).executeGeneric(Truffle.getRuntime().createVirtualFrame(
                new Object[] {c.env()}, FD));
    }

    public void testNoCaptureIsOneConstant() {
        Calls.MakeClosure node = new Calls.MakeClosure(() -> TARGET, new int[0]);
        VirtualFrame f = frame();
        Closure c1 = (Closure) node.executeGeneric(f);
        Closure c2 = (Closure) node.executeGeneric(f);
        check(c1 == c2, "a capture-free literal is one func value");
        check(c1.env() == null && c1.target() == TARGET, "no environment");
    }

    public void testOneCaptureIsTheValueItself() {
        VirtualFrame f = frame();
        Closure c = make(new int[] {B}, f);
        check(c.env() == "b0", "env is the captured box: " + c.env());
        check(read(c, 0, 1) == "b0", "EnvRead of a one-value env");
    }

    public void testCapturesAreReadPerEvaluation() {
        VirtualFrame f = frame();
        Calls.MakeClosure node = new Calls.MakeClosure(() -> TARGET, new int[] {C, A, B});
        Closure c1 = (Closure) node.executeGeneric(f);
        f.setObjectStatic(A, "a1");
        Closure c2 = (Closure) node.executeGeneric(f);
        check(c1.env() instanceof Object[] e && e.length == 3, "an Object[] of the capture count");
        check(c1.env() != c2.env(), "a fresh environment per evaluation");
        String[] want1 = {"c0", "a0", "b0"};
        String[] want2 = {"c0", "a1", "b0"};
        for (int i = 0; i < 3; i++) {
            check(read(c1, i, 3) == want1[i], "first closure, capture " + i);
            check(read(c2, i, 3) == want2[i], "second closure, capture " + i);
        }
    }

    public void testBindIsAOneValueEnvironment() {
        Closure c = (Closure) new Calls.Bind1(TARGET, new N.ConstRef("recv"), false).executeGeneric(frame());
        check(c.env() == "recv" && c.target() == TARGET, "bound receiver is the env");
        check(read(c, 0, 1) == "recv", "EnvRead of the receiver");
    }

    public void testClearRefsClearsOnlyItsSlots() {
        VirtualFrame f = frame();
        Stmt clear = new N.ClearRefs(new int[] {A, C});
        clear.executeVoid(f);
        check(f.getObjectStatic(A) == null && f.getObjectStatic(C) == null, "cleared slots read null");
        check(f.getObjectStatic(B) == "b0", "other reference slot kept");
        check(f.getLongStatic(N0) == 42, "primitive slot kept");
    }
}
