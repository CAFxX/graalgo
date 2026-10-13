package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.nodes.RepeatingNode;
import graalgo.safepoint.Safepoint;
import graalgo.values.GoBox;
import graalgo.values.GoKind;

/**
 * {@code range} over an array (M4, WS E). The array is <em>copied</em> when
 * the range expression is evaluated (gc-verified: writes to the array
 * during iteration are not observed); iteration is over the copy.
 *
 * <p>Concurrency (M10, F-1): iteration state lives in <em>frame slots</em>,
 * never in node fields — the node object is shared by every goroutine
 * executing the same compiled function (see RangeSliceNode).
 */
public final class RangeArrayNode extends Node implements RepeatingNode, LabelTarget {
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
    private final int arraySlot;   // Object: the copied array
    private final int indexSlot;   // int: current index i

    public RangeArrayNode(GoExprNode x, GoStmtNode body,
                         int keySlot, GoKind keyKind, int valSlot, GoKind valKind,
                         int startedSlot, int arraySlot, int indexSlot) {
        this(x, body, keySlot, keyKind, false, true, valSlot, valKind, false, true,
                startedSlot, arraySlot, indexSlot);
    }

    public RangeArrayNode(GoExprNode x, GoStmtNode body,
                         int keySlot, GoKind keyKind, boolean keyBoxed, boolean keyDefine,
                         int valSlot, GoKind valKind, boolean valBoxed, boolean valDefine,
                         int startedSlot, int arraySlot, int indexSlot) {
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
        this.arraySlot = arraySlot;
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
        final Object array;
        final int i;
        if (!frame.getBoolean(startedSlot)) {
            frame.setBoolean(startedSlot, true);
            Object o = x.executeObject(frame);
            // Value copy of the array at range-evaluation time.
            array = Boxing.copyForAssign(GoKind.ARRAY, o);
            frame.setObject(arraySlot, array);
            i = 0;
            frame.setInt(indexSlot, 0);
        } else {
            array = frame.getObject(arraySlot);
            i = frame.getInt(indexSlot);
        }
        final int len = java.lang.reflect.Array.getLength(array);
        if (i >= len) {
            return false;
        }
        if (keySlot >= 0) {
            if (keyBoxed) {
                if (keyDefine) {
                    frame.setObject(keySlot, new GoBox(Long.valueOf(i)));
                } else {
                    // `for k = range ...` into a boxed variable: reassign the
                    // same variable (write into its existing cell).
                    Boxing.setBoxedCell(frame, keySlot, keyKind, Long.valueOf(i));
                }
            } else {
                Boxing.setSlot(frame, keySlot, keyKind, Long.valueOf(i));
            }
        }
        if (valSlot >= 0) {
            Object v = Boxing.arrayGet(array, valKind, i);
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
