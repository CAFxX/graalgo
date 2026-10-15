package graalgo.values;

import graalgo.frontend.GoArrayCell;
import graalgo.frontend.GoFieldCell;
import java.util.Arrays;
import java.util.List;

/**
 * PKG-011 regression tests: storage identity of {@link GoCell}s.
 *
 * <p>A {@code GoFieldCell} is allocated fresh on every {@code &s.f}
 * evaluation, so Java object identity is the wrong equivalence for
 * pointer comparison: {@code p != &e.list.root} (container/list's
 * {@code Next}) compared two pointers to the same field via distinct
 * cell objects and wrongly reported them different, corrupting the list
 * iteration and panicking. {@link GoPointer#sameStorageAs} must compare
 * the addressed <em>storage</em> via {@link GoCell#sameStorageAs}.
 */
public final class CellIdentityTest {

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError(what + ": expected true");
        }
    }

    private static void assertFalse(boolean cond, String what) {
        if (cond) {
            throw new AssertionError(what + ": expected false");
        }
    }

    private static GoStructDescriptor twoPtrDesc() {
        return GoStructDescriptor.layout("T",
                List.of("a", "b"),
                List.of(GoKind.POINTER, GoKind.POINTER),
                List.of(-1, -1),
                Arrays.asList(null, null));
    }

    private static GoStruct newStruct(GoStructDescriptor d) {
        return new GoStruct(d, new long[0], new Object[2]);
    }

    public void testFieldCellSameStorage() {
        GoStructDescriptor d = twoPtrDesc();
        GoStruct s = newStruct(d);
        // Two evaluations of &s.a: distinct GoFieldCell objects, same storage.
        GoPointer p = new GoPointer(new GoFieldCell(s, 0, GoKind.POINTER));
        GoPointer q = new GoPointer(new GoFieldCell(s, 0, GoKind.POINTER));
        assertTrue(p != q, "fresh cells are distinct objects");
        assertTrue(p.sameStorageAs(q), "&s.a == &s.a");
        assertTrue(q.sameStorageAs(p), "symmetric");
    }

    public void testFieldCellDifferentField() {
        GoStructDescriptor d = twoPtrDesc();
        GoStruct s = newStruct(d);
        GoPointer p = new GoPointer(new GoFieldCell(s, 0, GoKind.POINTER));
        GoPointer q = new GoPointer(new GoFieldCell(s, 1, GoKind.POINTER));
        assertFalse(p.sameStorageAs(q), "&s.a != &s.b");
    }

    public void testFieldCellDifferentStruct() {
        GoStructDescriptor d = twoPtrDesc();
        GoStruct s1 = newStruct(d);
        GoStruct s2 = newStruct(d);
        GoPointer p = new GoPointer(new GoFieldCell(s1, 0, GoKind.POINTER));
        GoPointer q = new GoPointer(new GoFieldCell(s2, 0, GoKind.POINTER));
        assertFalse(p.sameStorageAs(q), "&s1.a != &s2.a");
    }

    public void testFieldCellVsBox() {
        GoStructDescriptor d = twoPtrDesc();
        GoStruct s = newStruct(d);
        GoPointer p = new GoPointer(new GoFieldCell(s, 0, GoKind.POINTER));
        GoPointer q = new GoPointer(new GoBox(null));
        assertFalse(p.sameStorageAs(q), "field cell != variable cell");
        assertFalse(q.sameStorageAs(p), "symmetric");
    }

    public void testFieldCellVsHeapPointer() {
        GoStructDescriptor d = twoPtrDesc();
        GoStruct s = newStruct(d);
        GoPointer p = new GoPointer(new GoFieldCell(s, 0, GoKind.POINTER));
        GoPointer q = new GoPointer(new Object());
        assertFalse(p.sameStorageAs(q), "field cell != heap referent");
    }

    public void testBoxIdentityPreserved() {
        GoBox b = new GoBox(null);
        GoPointer p = new GoPointer(b);
        GoPointer q = new GoPointer(b);
        assertTrue(p.sameStorageAs(q), "&x == &x (same box)");
        GoPointer r = new GoPointer(new GoBox(null));
        assertFalse(p.sameStorageAs(r), "&x != &y (distinct boxes)");
    }

    public void testArrayCellSameStorage() {
        Object arr = new Object[4];
        GoPointer p = new GoPointer(new GoArrayCell(arr, GoKind.POINTER, 2));
        GoPointer q = new GoPointer(new GoArrayCell(arr, GoKind.POINTER, 2));
        assertTrue(p.sameStorageAs(q), "&a[2] == &a[2]");
        GoPointer r = new GoPointer(new GoArrayCell(arr, GoKind.POINTER, 3));
        assertFalse(p.sameStorageAs(r), "&a[2] != &a[3]");
        GoPointer s = new GoPointer(new GoArrayCell(new Object[4], GoKind.POINTER, 2));
        assertFalse(p.sameStorageAs(s), "different backing arrays");
    }

    public void testNilUnchanged() {
        GoPointer nil1 = new GoPointer((Object) null);
        GoPointer nil2 = new GoPointer((Object) null);
        assertTrue(nil1.sameStorageAs(nil2), "nil == nil");
        GoStructDescriptor d = twoPtrDesc();
        GoPointer p = new GoPointer(new GoFieldCell(newStruct(d), 0, GoKind.POINTER));
        assertFalse(nil1.sameStorageAs(p), "nil != &s.a");
        assertFalse(p.sameStorageAs(nil1), "&s.a != nil");
    }
}
