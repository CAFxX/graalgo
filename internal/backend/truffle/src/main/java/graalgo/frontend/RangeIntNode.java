package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.nodes.RepeatingNode;
import graalgo.safepoint.Safepoint;
import graalgo.values.GoBox;
import graalgo.values.GoKind;

/**
 * {@code range} over an integer (Go 1.22+, P3-M9, WS E).
 *
 * <p>{@code for i := range n} iterates i from 0 to n-1. The integer is
 * evaluated once; if n &lt;= 0, the loop executes zero times. Only the
 * key variable is used (there is no value for integer ranges).
 *
 * <p>Per-iteration capture (loopcap): when the range variable is boxed,
 * each iteration installs a fresh {@link GoBox} cell in its slot
 * ({@code keyDefine}, {@code :=} form) or mutates the existing cell
 * ({@code =} form on an outer boxed variable).
 *
 * <p>Concurrency (M10, F-1): iteration state lives in <em>frame slots</em>,
 * never in node fields — the node object is shared by every goroutine
 * executing the same compiled function (see RangeSliceNode).
 */
public final class RangeIntNode extends Node implements RepeatingNode, LabelTarget {
    @Child private GoExprNode x;
    @Child private GoStmtNode body;
    private final int keySlot;
    private final GoKind keyKind;
    private final boolean keyBoxed;
    private final boolean keyDefine;
    private String label;

    /** Frame slots for the per-invocation iteration state (see class doc). */
    private final int startedSlot; // boolean: range operand evaluated
    private final int boundSlot;    // long: the bound n
    private final int indexSlot;    // long: current index i

    public RangeIntNode(GoExprNode x, GoStmtNode body,
                        int keySlot, GoKind keyKind, boolean keyBoxed, boolean keyDefine,
                        int startedSlot, int boundSlot, int indexSlot) {
        this.x = x;
        this.body = body;
        this.keySlot = keySlot;
        this.keyKind = keyKind;
        this.keyBoxed = keyBoxed;
        this.keyDefine = keyDefine;
        this.startedSlot = startedSlot;
        this.boundSlot = boundSlot;
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
        final long n;
        final long i;
        if (!frame.getBoolean(startedSlot)) {
            frame.setBoolean(startedSlot, true);
            n = x.executeLong(frame);
            frame.setLong(boundSlot, n);
            i = 0;
            frame.setLong(indexSlot, 0);
        } else {
            n = frame.getLong(boundSlot);
            i = frame.getLong(indexSlot);
        }
        if (i >= n) {
            return false;
        }
        // Set the key variable to i.
        if (keySlot >= 0) {
            if (keyBoxed) {
                GoBox box;
                // keyKind is INT64 for integer ranges; box as Long.
                Object boxed = Long.valueOf(i);
                if (keyDefine) {
                    box = new GoBox(boxed);
                    frame.setObject(keySlot, box);
                } else {
                    box = (GoBox) frame.getObject(keySlot);
                    box.value = boxed;
                }
            } else {
                // keyKind is INT64 for integer ranges.
                frame.setLong(keySlot, i);
            }
        }
        Safepoint.poll();
        try {
            body.executeVoid(frame);
        } catch (ContinueSignal s) {
            // continue: proceed to next iteration
        }
        // Advance the per-invocation index (committed before the next
        // executeRepeating call, matching the old i++ ordering).
        frame.setLong(indexSlot, i + 1);
        return true;
    }
}
