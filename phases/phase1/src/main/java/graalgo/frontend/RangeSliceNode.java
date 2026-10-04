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

    private boolean started;
    private GoSlice slice;
    private int len;
    private int i;

    public RangeSliceNode(GoExprNode x, GoStmtNode body,
                          int keySlot, GoKind keyKind, int valSlot, GoKind valKind) {
        this(x, body, keySlot, keyKind, false, true, valSlot, valKind, false, true);
    }

    public RangeSliceNode(GoExprNode x, GoStmtNode body,
                          int keySlot, GoKind keyKind, boolean keyBoxed, boolean keyDefine,
                          int valSlot, GoKind valKind, boolean valBoxed, boolean valDefine) {
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
    }

    @Override
    public void setLabel(String label) {
        this.label = label;
    }

    /** Resets iteration state (called before each range statement execution). */
    public void reset() {
        started = false;
    }

    @Override
    public boolean executeRepeating(VirtualFrame frame) {
        if (!started) {
            started = true;
            Object o = x.executeObject(frame);
            slice = (GoSlice) o;
            len = slice == null ? 0 : slice.len();
            i = 0;
        }
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
        i++;
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
