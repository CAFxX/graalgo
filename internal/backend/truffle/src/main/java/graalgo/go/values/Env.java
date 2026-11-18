package graalgo.go.values;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.FrameSlotKind;
import com.oracle.truffle.api.frame.MaterializedFrame;

/**
 * The capture environment of a closure — the one place that knows its representation. Today it
 * is SPEC §3.4's {@code MaterializedFrame}: one object slot per captured variable, holding the
 * variable's {@link Flat} box. The closure nodes ({@code MakeClosure}, {@code Bind1},
 * {@code EnvRead}) go through these four methods only, so a typed-cell array can be swapped in
 * and benchmarked without touching the factory (truffle-design amendment A9, undecided).
 */
public final class Env {
    private Env() {}

    private static final Object[] NO_ARGS = new Object[0];

    /** The layout of an environment with {@code n} captured variables. */
    public static FrameDescriptor layout(int n) {
        FrameDescriptor.Builder b = FrameDescriptor.newBuilder();
        b.addSlots(n, FrameSlotKind.Object);
        return b.build();
    }

    public static MaterializedFrame create(FrameDescriptor layout) {
        return Truffle.getRuntime().createMaterializedFrame(NO_ARGS, layout);
    }

    public static void set(MaterializedFrame env, int index, Object box) {
        env.setObject(index, box);
    }

    /** Capture {@code index} of the environment passed as a call's first argument. */
    public static Object get(Object env, int index) {
        return ((MaterializedFrame) env).getObject(index);
    }
}
