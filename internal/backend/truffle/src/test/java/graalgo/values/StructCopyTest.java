package graalgo.values;

import graalgo.runtime.sync.Copyable;
import graalgo.runtime.sync.GoAtomicInt32;
import graalgo.runtime.sync.GoAtomicInt64;
import graalgo.runtime.sync.GoMutex;
import graalgo.runtime.sync.GoOnce;
import graalgo.runtime.sync.GoPool;
import graalgo.runtime.sync.GoRWMutex;
import graalgo.runtime.sync.GoWaitGroup;

import java.util.List;

/**
 * Pins the structural copy dispatch for Go value semantics (RV18-P1-16).
 *
 * <p>Sync/atomic cells are Go values: struct copies, array copies, and
 * by-value assignments get fresh cells through the {@link Copyable}
 * interface — never shared instances. The dispatch is structural (on the
 * interface), not name-keyed on concrete classes, so a new cell type only
 * has to implement {@code Copyable}.
 *
 * <p>gc rule under test: {@code b := a; b.x.Add(1)} must not observably
 * mutate {@code a.x} (atomic cells copy the integer; copying is a vet
 * error in gc but defined).
 */
public final class StructCopyTest {

    private static void assertEq(long want, long got, String what) {
        if (want != got) {
            throw new AssertionError(what + ": want " + want + " got " + got);
        }
    }

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError(what + ": expected true");
        }
    }

    private static GoStructDescriptor oneCellStruct() {
        return GoStructDescriptor.layout("T",
                List.of("x"), List.of(GoKind.STRUCT));
    }

    public void testDeepCopyClonesAtomicInt64Field() {
        GoStruct s = GoStruct.zero(oneCellStruct());
        GoAtomicInt64 cell = new GoAtomicInt64(41);
        s.setObject(0, cell);
        GoStruct c = s.deepCopy();
        Object got = c.getObject(0);
        assertTrue(got instanceof GoAtomicInt64, "copy keeps the wrapper type");
        assertTrue(got != cell, "copy cell is a fresh instance");
        assertEq(41, ((GoAtomicInt64) got).get(), "copy carries the value");
        // b := a; b.x.Add(1) must not observably mutate a.x (either way).
        cell.addAndGet(1);
        assertEq(41, ((GoAtomicInt64) c.getObject(0)).get(),
                "Add on original invisible to copy");
        ((GoAtomicInt64) c.getObject(0)).addAndGet(10);
        assertEq(42, cell.get(), "Add on copy invisible to original");
    }

    public void testDeepCopyClonesAtomicInt32Field() {
        GoStruct s = GoStruct.zero(oneCellStruct());
        GoAtomicInt32 cell = new GoAtomicInt32(7);
        s.setObject(0, cell);
        GoStruct c = s.deepCopy();
        Object got = c.getObject(0);
        assertTrue(got instanceof GoAtomicInt32, "copy keeps the wrapper type");
        assertTrue(got != cell, "copy cell is a fresh instance");
        assertEq(7, ((GoAtomicInt32) got).get(), "copy carries the value");
        cell.addAndGet(1);
        assertEq(7, ((GoAtomicInt32) c.getObject(0)).get(),
                "Add on original invisible to copy");
    }

    public void testDeepCopyFreshensSyncCellField() {
        GoStruct s = GoStruct.zero(oneCellStruct());
        GoMutex m = new GoMutex();
        s.setObject(0, m);
        GoStruct c = s.deepCopy();
        Object got = c.getObject(0);
        assertTrue(got instanceof GoMutex, "copy keeps the cell type");
        assertTrue(got != m, "sync cell is a fresh instance, not shared");
    }

    public void testDeepCopyArrayCopiesAtomicCellsByValue() {
        GoAtomicInt64 cell = new GoAtomicInt64(9);
        Object[] arr = new Object[]{cell};
        Object[] out = (Object[]) GoStruct.deepCopyArray(arr);
        Object got = out[0];
        assertTrue(got instanceof GoAtomicInt64, "array copy keeps the wrapper type");
        assertTrue(got != cell, "array element is a fresh instance");
        assertEq(9, ((GoAtomicInt64) got).get(), "array copy carries the value");
        cell.addAndGet(1);
        assertEq(9, ((GoAtomicInt64) out[0]).get(),
                "Add on original invisible to array copy");
    }

    public void testDeepCopyArrayFreshensSyncCells() {
        GoMutex m = new GoMutex();
        GoRWMutex rw = new GoRWMutex();
        GoWaitGroup wg = new GoWaitGroup();
        GoOnce once = new GoOnce();
        GoPool pool = new GoPool();
        Object[] out = (Object[]) GoStruct.deepCopyArray(
                new Object[]{m, rw, wg, once, pool});
        Object[] want = new Object[]{m, rw, wg, once, pool};
        for (int i = 0; i < want.length; i++) {
            assertTrue(out[i] != null && out[i].getClass() == want[i].getClass(),
                    "element " + i + " keeps its cell type");
            assertTrue(out[i] != want[i], "element " + i + " is a fresh instance");
        }
    }

    /** Every Copyable cell type honors the fresh-copy contract. */
    public void testFreshCopyContract() {
        checkFresh(new GoMutex(), GoMutex.class);
        checkFresh(new GoRWMutex(), GoRWMutex.class);
        checkFresh(new GoWaitGroup(), GoWaitGroup.class);
        checkFresh(new GoOnce(), GoOnce.class);
        checkFresh(new GoPool(), GoPool.class);
        GoAtomicInt64 i64 = new GoAtomicInt64(123);
        Object c64 = ((Copyable) i64).freshCopy();
        assertTrue(c64 instanceof GoAtomicInt64 && c64 != i64,
                "GoAtomicInt64 freshCopy is a fresh wrapper");
        assertEq(123, ((GoAtomicInt64) c64).get(), "GoAtomicInt64 value carried");
        GoAtomicInt32 i32 = new GoAtomicInt32(45);
        Object c32 = ((Copyable) i32).freshCopy();
        assertTrue(c32 instanceof GoAtomicInt32 && c32 != i32,
                "GoAtomicInt32 freshCopy is a fresh wrapper");
        assertEq(45, ((GoAtomicInt32) c32).get(), "GoAtomicInt32 value carried");
    }

    private static void checkFresh(Object cell, Class<?> type) {
        Object copy = ((Copyable) cell).freshCopy();
        assertTrue(copy != null && copy.getClass() == type,
                type.getSimpleName() + " freshCopy keeps the type");
        assertTrue(copy != cell, type.getSimpleName() + " freshCopy is independent");
    }
}
