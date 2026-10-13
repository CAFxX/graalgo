package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.nodes.RepeatingNode;
import graalgo.safepoint.Safepoint;
import graalgo.values.GoBox;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;

/**
 * {@code range} over a slice (M4, WS E). The slice header is evaluated
 * once; its length is fixed for the iteration, but the backing array is
 * shared, so element writes during iteration are visible (gc-verified).
 * A nil slice iterates zero times.
 *
 * <p>Per-iteration capture (loopcap): when a range variable is boxed, each
 * iteration installs a fresh {@link GoBox} cell in its slot
 * ({@code keyDefine}/{@code valDefine}, {@code :=} form) or mutates the
 * existing cell ({@code =} form on an outer boxed variable) instead of a
 * raw value, so closures created in the body capture that iteration's
 * variable.
 *
 * <p>Concurrency (M10, F-1): iteration state lives in <em>frame slots</em>
 * ({@code startedSlot}/{@code sliceSlot}/{@code indexSlot}), never in
 * node fields. The Truffle AST node object is shared by every goroutine
 * executing the same compiled function; per-node mutable iteration state
 * raced across goroutines (one worker's slice/length/index clobbering
 * another's), losing or duplicating iterations. Each invocation's frame
 * is private, so frame slots give each goroutine its own iteration state.
 */
public final class RangeSliceNode extends Node implements RepeatingNode, LabelTarget {
    @Child private GoExprNode x;
    @Child private GoStmtNode body;
    private final int keySlot;
    private final GoKind keyKind;
    private final boolean keyBoxed;
    private final boolean keyDefine;
    private final int valSlot;
    private final GoKind valKind;
    private final boolean valBoxed;
    private final boolean valDefine;
    private String label;

    /** Frame slots for the per-invocation iteration state (see class doc). */
    private final int startedSlot; // boolean: range operand evaluated
    private final int sliceSlot;   // Object: the GoSlice header (null = nil slice)
    private final int indexSlot;   // int: current index i

    public RangeSliceNode(GoExprNode x, GoStmtNode body,
                          int keySlot, GoKind keyKind, int valSlot, GoKind valKind,
                          int startedSlot, int sliceSlot, int indexSlot) {
        this(x, body, keySlot, keyKind, false, true, valSlot, valKind, false, true,
                startedSlot, sliceSlot, indexSlot);
    }

    public RangeSliceNode(GoExprNode x, GoStmtNode body,
                          int keySlot, GoKind keyKind, boolean keyBoxed, boolean keyDefine,
                          int valSlot, GoKind valKind, boolean valBoxed, boolean valDefine,
                          int startedSlot, int sliceSlot, int indexSlot) {
        this.x = x;
        this.body = body;
        this.keySlot = keySlot;
        this.keyKind = keyKind;
        this.keyBoxed = keyBoxed;
        this.keyDefine = keyDefine;
        this.valSlot = valSlot;
        this.valKind = valKind;
        this.valBoxed = valBoxed;
        this.valDefine = valDefine;
        this.startedSlot = startedSlot;
        this.sliceSlot = sliceSlot;
        this.indexSlot = indexSlot;
    }

    @Override
    public void setLabel(String label) {
        this.label = label;
    }

    /**
     * Resets iteration state (called before each range statement
     * execution, with that execution's frame).
     */
    public void reset(VirtualFrame frame) {
        frame.setBoolean(startedSlot, false);
    }

    @Override
    public boolean executeRepeating(VirtualFrame frame) {
        final GoSlice slice;
        final int i;
        if (!frame.getBoolean(startedSlot)) {
            frame.setBoolean(startedSlot, true);
            Object o = x.executeObject(frame);
            slice = (GoSlice) o;
            frame.setObject(sliceSlot, slice);
            i = 0;
            frame.setInt(indexSlot, 0);
        } else {
            slice = (GoSlice) frame.getObject(sliceSlot);
            i = frame.getInt(indexSlot);
        }
        final int len = slice == null ? 0 : slice.len();
        if (i >= len) {
            return false;
        }
        if (keySlot >= 0) {
            if (keyBoxed) {
                if (keyDefine) {
                    frame.setObject(keySlot, new GoBox(Long.valueOf(i)));
                } else {
                    // `for k = range ...` into a boxed variable: the same
                    // variable is reassigned, so write into its existing
                    // cell (with outer write-through) instead of a fresh one.
                    Boxing.setBoxedCell(frame, keySlot, keyKind, Long.valueOf(i));
                }
            } else {
                Boxing.setSlot(frame, keySlot, keyKind, Long.valueOf(i));
            }
        }
        if (valSlot >= 0) {
            // Read per iteration from the shared backing array: writes
            // during iteration are visible (gc-verified). Range values are
            // copies — struct/array elements go through the value-copy rule.
            Object v = Boxing.arrayGet(slice.array(), valKind, slice.offset() + i);
            v = Boxing.copyForAssign(valKind, v);
            if (valBoxed) {
                if (valDefine) {
                    frame.setObject(valSlot, new GoBox(v));
                } else {
                    Boxing.setBoxedCell(frame, valSlot, valKind, v);
                }
            } else {
                Boxing.setSlot(frame, valSlot, valKind, v);
            }
        }
        // Advance the per-invocation index before running the body (the
        // body may break/continue/throw; the increment is already committed,
        // matching the old i++ ordering).
        frame.setInt(indexSlot, i + 1);
        try {
            body.executeVoid(frame);
        } catch (BreakSignal b) {
            if (b.matches(label)) {
                return false;
            }
            throw b;
        } catch (ContinueSignal c) {
            if (!c.matches(label)) {
                throw c;
            }
        }
        Safepoint.poll(); // D8 seam.
        return true;
    }
}
