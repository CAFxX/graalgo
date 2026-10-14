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

    private boolean started;
    private Object array;
    private int len;
    private int i;

    public RangeArrayNode(GoExprNode x, GoStmtNode body,
                         int keySlot, GoKind keyKind, int valSlot, GoKind valKind) {
        this(x, body, keySlot, keyKind, false, true, valSlot, valKind, false, true);
    }

    public RangeArrayNode(GoExprNode x, GoStmtNode body,
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
            // Value copy of the array at range-evaluation time.
            array = Boxing.copyForAssign(GoKind.ARRAY, o);
            len = java.lang.reflect.Array.getLength(array);
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
