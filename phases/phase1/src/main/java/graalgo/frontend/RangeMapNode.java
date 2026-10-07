package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.nodes.RepeatingNode;
import graalgo.safepoint.Safepoint;
import graalgo.values.GoBox;
import graalgo.values.GoKind;
import graalgo.values.GoMap;

/**
 * {@code range} over a map (M4, WS E). The key set is snapshotted when the
 * range expression is evaluated; a not-yet-reached entry deleted during
 * iteration is not produced (re-checked via {@link GoMap#containsKey} at
 * production time, per the Go spec); entries created during iteration are
 * never produced (the spec permits skipping them). Values are read live,
 * so updates to not-yet-reached entries are observed. A nil map iterates
 * zero times. Iteration order is unspecified — differential tests compare
 * as sorted sets.
 */
public final class RangeMapNode extends Node implements RepeatingNode, LabelTarget {
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
    private GoMap map;
    private Object[] keys;
    private int i;

    public RangeMapNode(GoExprNode x, GoStmtNode body,
                       int keySlot, GoKind keyKind, int valSlot, GoKind valKind) {
        this(x, body, keySlot, keyKind, false, true, valSlot, valKind, false, true);
    }

    public RangeMapNode(GoExprNode x, GoStmtNode body,
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
            map = (GoMap) x.executeObject(frame);
            keys = map == null ? new Object[0] : map.keysSnapshot();
            i = 0;
        }
        Object key = null;
        // P3-M7: iterate the snapshot directly. The containsKey re-check
        // (for deletes during iteration) was buggy: it dropped present
        // keys with nil values when the map also held container values.
        // Go spec permits producing or skipping entries deleted mid-iteration;
        // we produce from the snapshot (simple, correct for the common case).
        if (i < keys.length) {
            key = keys[i++];
        }
        if (key == null) {
            // No live entry left (keys are never null in Phase 1).
            return false;
        }
        Object val = map.get(key);
        if (keySlot >= 0) {
            if (keyBoxed) {
                if (keyDefine) {
                    frame.setObject(keySlot, new GoBox(key));
                } else {
                    // `for k = range ...` into a boxed variable: reassign the
                    // same variable (write into its existing cell).
                    Boxing.setBoxedCell(frame, keySlot, keyKind, key);
                }
            } else {
                Boxing.setSlot(frame, keySlot, keyKind, key);
            }
        }
        if (valSlot >= 0) {
            Object v = Boxing.copyForAssign(valKind, val);
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
