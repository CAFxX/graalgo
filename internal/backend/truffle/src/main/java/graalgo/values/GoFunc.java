package graalgo.values;

import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.frame.MaterializedFrame;

/**
 * A Go function value: the compiled body plus its closure environment (D5).
 *
 * <p>{@code target} is the Truffle call target for the (possibly
 * monomorphized — D2) function body; {@code closureFrame} is the
 * materialized frame captured at closure creation, or null for plain
 * function values with no free variables. Calling a {@code GoFunc}
 * invokes the target with the closure frame as the lexical parent (WS D).
 *
 * <p>Per-iteration capture (M6): {@code boxCells} holds the {@link GoBox}
 * cells snapshotted at closure creation for captured boxed (loop)
 * variables, in a fixed order agreed with the {@code FuncLitNode} that
 * created this value; the body root installs them into its snapshot
 * slots on entry. Null when the literal captures no boxed variables.
 * A {@code for}-header variable's per-iteration cell carries its outer
 * cell on {@link GoBox#outer}, so no separate outer snapshot is needed.
 */
public final class GoFunc {
    private final RootCallTarget target;
    private final MaterializedFrame closureFrame;
    private final GoBox[] boxCells;
    /**
     * Prefix arguments prepended to the call arguments at invocation
     * (P3-M9, WS H): for a method value, the bound receiver. Null for
     * ordinary function values. See {@link graalgo.runtime.GoCalls#invoke}.
     */
    private final Object[] boundArgs;
    /**
     * Kind of each bound argument (parallel to {@code boundArgs}); null
     * when no bound args. Used at invocation time to apply Go's
     * value-copy rule per call (array/struct bound receivers are
     * re-copied for every call, like any other value argument).
     */
    private final GoKind[] boundArgKinds;

    public GoFunc(RootCallTarget target, MaterializedFrame closureFrame) {
        this(target, closureFrame, null, null, null);
    }

    public GoFunc(RootCallTarget target, MaterializedFrame closureFrame, GoBox[] boxCells) {
        this(target, closureFrame, boxCells, null, null);
    }

    public GoFunc(RootCallTarget target, MaterializedFrame closureFrame, GoBox[] boxCells,
            Object[] boundArgs) {
        this(target, closureFrame, boxCells, boundArgs, null);
    }

    public GoFunc(RootCallTarget target, MaterializedFrame closureFrame, GoBox[] boxCells,
            Object[] boundArgs, GoKind[] boundArgKinds) {
        if (target == null) {
            throw new IllegalArgumentException("call target must not be null (nil funcs are null references)");
        }
        if (boundArgs != null && boundArgKinds != null && boundArgs.length != boundArgKinds.length) {
            throw new IllegalArgumentException(
                    "boundArgs and boundArgKinds must be parallel (lengths "
                            + boundArgs.length + " vs " + boundArgKinds.length + ")");
        }
        this.target = target;
        this.closureFrame = closureFrame;
        this.boxCells = boxCells;
        this.boundArgs = boundArgs;
        this.boundArgKinds = boundArgKinds;
    }

    public RootCallTarget target() {
        return target;
    }

    /** Null when the function closes over nothing. */
    public MaterializedFrame closureFrame() {
        return closureFrame;
    }

    /** Snapshot cells for captured boxed variables; null when none. */
    public GoBox[] boxCells() {
        return boxCells;
    }

    /** Prefix arguments prepended at invocation; null for ordinary function values. */
    public Object[] boundArgs() {
        return boundArgs;
    }

    /**
     * Kind of each bound argument (parallel to {@link #boundArgs});
     * null when the producer did not record kinds (no per-call copy).
     */
    public GoKind[] boundArgKinds() {
        return boundArgKinds;
    }

    @Override
    public String toString() {
        return "GoFunc{" + target + (closureFrame == null ? "" : ", closure") + "}";
    }
}
