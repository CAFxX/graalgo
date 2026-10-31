package graalgo.p3sync.nodes;

import java.lang.reflect.Constructor;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.frontend.GoExprNode;
import graalgo.runtime.sync.IntCell;
import graalgo.runtime.sync.LongCell;
import graalgo.values.GoKind;

/**
 * Unit tests for {@link AtomicOpNode} (R-03 remediation, 2026-10-31).
 *
 * <p>The {@code p3sync/nodes} package previously had no tests under
 * its own path (only launcher-level tests existed). The atomic nodes
 * are unit-testable because they operate on plain {@link LongCell} /
 * {@link IntCell} values through the real {@code Atomics} primitives:
 * the tests use minimal in-test cell implementations, so the node
 * logic (op dispatch, signed/unsigned extension, CAS semantics) is
 * exercised without a scheduler or a frontend type table.
 *
 * <p>Not covered here, deliberately: {@code SyncOpNode} (Mutex /
 * RWMutex / WaitGroup / Once / Cond) and {@code SyncNewNode} /
 * {@code AtomicPtrOpNode} operate on GoStruct sync shells built by
 * the frontend type table and, for blocking ops, on scheduler
 * parking — they cannot execute meaningfully in this tree. Their
 * behaviour is covered differentially by the sync corpus
 * ({@code test/corpus/sync/sy01_mutex_exclusion.go} and siblings)
 * and the atomic corpus ({@code test/corpus/atomic/at00_ops.go}).
 */
public final class SyncAtomicTest {

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(long e, long a, String msg) {
        if (e != a) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    private static VirtualFrame frame() {
        return Truffle.getRuntime().createVirtualFrame(new Object[0],
                FrameDescriptor.newBuilder().build());
    }

    static final class Cell64 implements LongCell {
        private final AtomicLong v;

        Cell64(long initial) {
            v = new AtomicLong(initial);
        }

        @Override
        public long get() {
            return v.get();
        }

        @Override
        public void set(long newValue) {
            v.set(newValue);
        }

        @Override
        public long getAndSet(long newValue) {
            return v.getAndSet(newValue);
        }

        @Override
        public boolean compareAndSet(long expect, long update) {
            return v.compareAndSet(expect, update);
        }

        @Override
        public long getAndAdd(long delta) {
            return v.getAndAdd(delta);
        }

        @Override
        public long addAndGet(long delta) {
            return v.addAndGet(delta);
        }
    }

    static final class Cell32 implements IntCell {
        private final AtomicInteger v;

        Cell32(int initial) {
            v = new AtomicInteger(initial);
        }

        @Override
        public int get() {
            return v.get();
        }

        @Override
        public void set(int newValue) {
            v.set(newValue);
        }

        @Override
        public int getAndSet(int newValue) {
            return v.getAndSet(newValue);
        }

        @Override
        public boolean compareAndSet(int expect, int update) {
            return v.compareAndSet(expect, update);
        }

        @Override
        public int getAndAdd(int delta) {
            return v.getAndAdd(delta);
        }

        @Override
        public int addAndGet(int delta) {
            return v.addAndGet(delta);
        }
    }

    static final class ConstLong extends GoExprNode {
        private final long v;

        ConstLong(long v) {
            super(GoKind.INT64);
            this.v = v;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            return v;
        }
    }

    static final class ConstInt extends GoExprNode {
        private final int v;

        ConstInt(int v) {
            super(GoKind.INT32);
            this.v = v;
        }

        @Override
        public int executeInt(VirtualFrame frame) {
            return v;
        }
    }

    static final class ConstObj extends GoExprNode {
        private final Object v;

        ConstObj(Object v) {
            super(GoKind.INTERFACE);
            this.v = v;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return v;
        }
    }

    private static GoExprNode op(String op, String atomicType, GoKind resultKind,
            GoExprNode recv, GoExprNode... args) throws Exception {
        Constructor<AtomicOpNode> c = AtomicOpNode.class.getDeclaredConstructor(
                String.class, String.class, GoKind.class, GoExprNode.class, GoExprNode[].class);
        c.setAccessible(true);
        return c.newInstance(op, atomicType, resultKind, recv, args);
    }

    public void testInt64LoadAddSwapAndStore() throws Exception {
        VirtualFrame f = frame();
        Cell64 cell = new Cell64(10);
        GoExprNode recv = new ConstObj(cell);
        assertEq(10, op("Load", "atomic.Int64", GoKind.INT64, recv).executeLong(f),
                "Int64 Load must read 10");
        // gc Add returns the NEW value (like AddAndGet), not the old one.
        assertEq(15, op("Add", "atomic.Int64", GoKind.INT64, recv,
                new ConstLong(5)).executeLong(f), "Int64 Add must return the new value");
        assertEq(15, cell.get(), "cell must hold 15 after Add(5)");
        assertEq(15, op("Swap", "atomic.Int64", GoKind.INT64, recv,
                new ConstLong(7)).executeLong(f), "Int64 Swap must return the old value");
        assertEq(7, cell.get(), "cell must hold 7 after Swap");
        op("Store", "atomic.Int64", GoKind.INTERFACE, recv,
                new ConstLong(42)).executeObject(f);
        assertEq(42, cell.get(), "Int64 Store must write 42");
    }

    public void testInt64AndOrAndCas() throws Exception {
        VirtualFrame f = frame();
        Cell64 cell = new Cell64(0b1100);
        GoExprNode recv = new ConstObj(cell);
        assertEq(0b1100, op("And", "atomic.Int64", GoKind.INT64, recv,
                new ConstLong(0b1010)).executeLong(f), "And must return the old value");
        assertEq(0b1000, cell.get(), "cell must hold 0b1000 after And");
        assertEq(0b1000, op("Or", "atomic.Int64", GoKind.INT64, recv,
                new ConstLong(0b0101)).executeLong(f), "Or must return the old value");
        assertEq(0b1101, cell.get(), "cell must hold 0b1101 after Or");
        assertTrue(op("CompareAndSwap", "atomic.Int64", GoKind.BOOL, recv,
                new ConstLong(0b1101), new ConstLong(9)).executeBoolean(f),
                "CAS with matching expect must succeed");
        assertEq(9, cell.get(), "cell must hold 9 after successful CAS");
        assertTrue(!op("CompareAndSwap", "atomic.Int64", GoKind.BOOL, recv,
                new ConstLong(0b1101), new ConstLong(1)).executeBoolean(f),
                "CAS with stale expect must fail");
        assertEq(9, cell.get(), "failed CAS must not change the cell");
    }

    public void testUint64IsNotSignExtended() throws Exception {
        VirtualFrame f = frame();
        Cell64 cell = new Cell64(-1L);
        GoExprNode recv = new ConstObj(cell);
        // Uint64 Load returns the raw bits; a sign-extending bug would
        // still return -1 here, so the discriminating case is Add on a
        // value whose low bit pattern differs under signed reading:
        // Add(1) to 2^64-1 must wrap to 0 and report the old value.
        assertEq(-1L, op("Load", "atomic.Uint64", GoKind.UINT64, recv).executeLong(f),
                "Uint64 Load of all-ones must be -1 as raw bits");
        // gc Add returns the NEW value: max + 1 wraps to 0.
        assertEq(0, op("Add", "atomic.Uint64", GoKind.UINT64, recv,
                new ConstLong(1)).executeLong(f), "Uint64 Add must return the wrapped new value");
        assertEq(0, cell.get(), "Uint64 Add(1) to max must wrap to 0");
    }

    public void testInt32LoadAddCasAndBool() throws Exception {
        VirtualFrame f = frame();
        Cell32 cell = new Cell32(20);
        GoExprNode recv = new ConstObj(cell);
        assertEq(20, op("Load", "atomic.Int32", GoKind.INT32, recv).executeInt(f),
                "Int32 Load must read 20");
        // gc Add returns the NEW value.
        assertEq(15, op("Add", "atomic.Int32", GoKind.INT32, recv,
                new ConstInt(-5)).executeInt(f), "Int32 Add must return the new value");
        assertEq(15, cell.get(), "cell must hold 15 after Add(-5)");
        assertTrue(op("CompareAndSwap", "atomic.Int32", GoKind.BOOL, recv,
                new ConstInt(15), new ConstInt(16)).executeBoolean(f),
                "Int32 CAS must succeed on match");
        // Bool ops ride on the 0/1 int encoding of the same cell type.
        Cell32 boolCell = new Cell32(0);
        GoExprNode boolRecv = new ConstObj(boolCell);
        assertTrue(!op("Load", "atomic.Bool", GoKind.BOOL, boolRecv).executeBoolean(f),
                "Bool Load of 0 must be false");
        assertTrue(!op("Swap", "atomic.Bool", GoKind.BOOL, boolRecv,
                new ConstBool(true)).executeBoolean(f), "Bool Swap must return the old value");
        assertTrue(op("Load", "atomic.Bool", GoKind.BOOL, boolRecv).executeBoolean(f),
                "Bool Load of 1 must be true");
    }

    static final class ConstBool extends GoExprNode {
        private final boolean v;

        ConstBool(boolean v) {
            super(GoKind.BOOL);
            this.v = v;
        }

        @Override
        public boolean executeBoolean(VirtualFrame frame) {
            return v;
        }
    }

    public void testWrongCellTypeFailsLoud() throws Exception {
        VirtualFrame f = frame();
        GoExprNode recv = new ConstObj(new Cell32(0));
        try {
            op("Load", "atomic.Int64", GoKind.INT64, recv).executeLong(f);
            throw new AssertionError("Int64 op on an IntCell must throw");
        } catch (graalgo.frontend.FrontendException expected) {
            assertTrue(expected.getMessage().contains("non-LongCell"),
                    "message must name the cell mismatch: " + expected.getMessage());
        }
    }
}
