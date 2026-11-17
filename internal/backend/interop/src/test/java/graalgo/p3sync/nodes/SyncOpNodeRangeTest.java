package graalgo.p3sync.nodes;

import java.lang.reflect.Constructor;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.atomic.AtomicBoolean;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;

import graalgo.frontend.GoExprNode;
import graalgo.runtime.sync.GoSyncMap;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;
import graalgo.values.GoStructDescriptor;

/**
 * R2316-01-P1-1: {@code SyncOpNode.doRange} must never deliver a nil value
 * to the callback for an absent key.
 *
 * <p>Why this test drives the node directly instead of going through the
 * Go API: the defect is a TOCTOU between two adjacent synchronized calls
 * ({@code containsKey(k)} then {@code get(k)}) in {@code doRange}. The
 * window between them is a few JIT-compiled bytecodes — far too small
 * for the interpreter's deleter goroutine to hit reliably (measured:
 * ~80 map ops/s, one cycle per ~5s even uncontended; the Go-level
 * stress observed {@code bad=0} pre-fix across 20,000+ windows). A
 * regression test through the Go API therefore cannot demonstrate the
 * failure. This test drives the same {@code doRange} code with real
 * Java threads instead: a deleter thread hammering {@code delete} at
 * ~10^6 ops/s interleaves between the two calls constantly, so the
 * test fails pre-fix (nil delivered) and passes post-fix (single atomic
 * {@code load()} per key, absent keys skipped).
 *
 * <p>The node is built via its private constructor (reflection) with
 * constant child nodes, following the {@code IfaceDispatchNodeTest}
 * pattern (constant nodes, capturing callback root, direct
 * {@code executeObject} on a fresh frame).
 */
public final class SyncOpNodeRangeTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    /** A GoExprNode evaluating to a constant (IfaceDispatchNodeTest pattern). */
    static final class ConstNode extends GoExprNode {
        private final Object objVal;

        ConstNode(GoKind kind, Object objVal) {
            super(kind);
            this.objVal = objVal;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return objVal;
        }
    }

    /**
     * A callback root capturing its call arguments and returning true
     * (continue). The captured argument arrays are {@code [fn, k, v]}
     * per the {@code GoCalls} calling convention.
     */
    static final class CaptureRoot extends RootNode {
        final List<Object[]> seen = Collections.synchronizedList(new ArrayList<>());

        CaptureRoot() {
            super(null);
        }

        @Override
        public Object execute(VirtualFrame frame) {
            seen.add(frame.getArguments().clone());
            return Boolean.TRUE;
        }
    }

    private static VirtualFrame frame() {
        return Truffle.getRuntime().createVirtualFrame(new Object[0],
                FrameDescriptor.newBuilder().build());
    }

    private static GoFunc callback(CaptureRoot root) {
        RootCallTarget target = root.getCallTarget();
        return new GoFunc(target, null, null);
    }

    /** Builds the {@code Map.Range} node for the given map and callback. */
    private static GoExprNode rangeNode(GoSyncMap m, GoFunc cb) throws Exception {
        Constructor<SyncOpNode> c = SyncOpNode.class.getDeclaredConstructor(
                String.class, String.class, GoExprNode.class, GoExprNode[].class,
                GoKind.class, GoStructDescriptor.class);
        c.setAccessible(true);
        GoExprNode recv = new ConstNode(GoKind.INTERFACE, m);
        GoExprNode arg = new ConstNode(GoKind.FUNC, cb);
        return c.newInstance("Range", "Map", recv, new GoExprNode[] {arg},
                GoKind.INTERFACE, null);
    }

    /**
     * Harness sanity: single-threaded Range visits every stored entry
     * with its stored value, exactly once. This pins the callback
     * argument layout ({@code [fn, k, v]}) the concurrent test relies on.
     */
    public void testRangeVisitsAllSingleThreaded() throws Exception {
        GoSyncMap m = new GoSyncMap();
        final int n = 200;
        for (int i = 0; i < n; i++) {
            m.store(i, i * 10);
        }
        CaptureRoot root = new CaptureRoot();
        GoExprNode node = rangeNode(m, callback(root));
        node.executeObject(frame());
        assertTrue(root.seen.size() == n,
                "expected " + n + " callback invocations, got " + root.seen.size());
        for (Object[] a : root.seen) {
            assertTrue(a.length == 3, "callback args must be [fn, k, v], got length " + a.length);
            int k = (Integer) a[1];
            assertTrue(a[2] != null, "single-threaded Range delivered null for key " + k);
            assertTrue(a[2].equals(k * 10),
                    "key " + k + ": expected value " + (k * 10) + ", got " + a[2]);
        }
    }

    /**
     * R2316-01-P1-1: concurrent Range+Delete must never deliver a nil
     * value to the callback. A deleter thread cycles delete-all/store-all
     * over every key while the test thread runs many Ranges. Pre-fix the
     * delete landing in the containsKey→get window delivers Java-null
     * (nil) to the callback; post-fix the single atomic load() skips
     * absent keys.
     */
    public void testRangeConcurrentDeleteNeverNil() throws Exception {
        GoSyncMap m = new GoSyncMap();
        final int n = 200;
        List<Object> keys = new ArrayList<>();
        for (int i = 0; i < n; i++) {
            Object k = i;
            keys.add(k);
            m.store(k, i + 1);
        }
        CaptureRoot root = new CaptureRoot();
        GoExprNode node = rangeNode(m, callback(root));
        VirtualFrame f = frame();
        AtomicBoolean stop = new AtomicBoolean(false);
        Thread deleter = new Thread(() -> {
            while (!stop.get()) {
                for (Object k : keys) {
                    m.delete(k);
                }
                for (Object k : keys) {
                    m.store(k, 1);
                }
            }
        });
        deleter.setDaemon(true);
        deleter.start();
        try {
            final int ranges = 200;
            for (int r = 0; r < ranges; r++) {
                node.executeObject(f);
            }
        } finally {
            stop.set(true);
            deleter.join();
        }
        int bad = 0;
        for (Object[] a : root.seen) {
            if (a[2] == null) {
                bad++;
            }
        }
        assertTrue(!root.seen.isEmpty(), "callback never invoked");
        assertTrue(bad == 0, "callback saw nil for " + bad + " absent key(s) of "
                + root.seen.size() + " invocations");
    }
}
