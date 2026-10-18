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

    // ------------------------------------------------------------------
    // R15-P1-3: GoCell.storageHash() contract — a.sameStorageAs(b) implies
    // a.storageHash() == b.storageHash(). The finalizer registry keys on
    // storage identity, so distinct cell objects over the same storage
    // must hash alike.
    // ------------------------------------------------------------------

    private static void assertStorageHashConsistent(GoCell a, GoCell b, String what) {
        if (a.sameStorageAs(b)) {
            assertTrue(a.storageHash() == b.storageHash(),
                    what + ": sameStorageAs implies equal storageHash");
            assertTrue(b.storageHash() == a.storageHash(),
                    what + ": symmetric storageHash");
        }
    }

    public void testStorageHashBox() {
        GoBox b = new GoBox(null);
        assertTrue(b.storageHash() == b.storageHash(), "deterministic");
        // Pin the approved scheme: box storage identity is object identity.
        assertTrue(b.storageHash() == System.identityHashCode(b),
                "box hash must be its identity hash");
        assertStorageHashConsistent(b, b, "&x vs &x");
        GoBox other = new GoBox(null);
        assertFalse(b.sameStorageAs(other), "&x != &y");
        assertStorageHashConsistent(b, other, "&x vs &y (no constraint)");
    }

    public void testStorageHashFieldCell() {
        GoStructDescriptor d = twoPtrDesc();
        GoStruct s = newStruct(d);
        // Two evaluations of &s.a: distinct objects, same storage.
        GoCell c1 = new GoFieldCell(s, 0, GoKind.POINTER);
        GoCell c2 = new GoFieldCell(s, 0, GoKind.POINTER);
        assertTrue(c1 != c2, "fresh cells are distinct objects");
        assertTrue(c1.sameStorageAs(c2), "same storage");
        assertTrue(c1.storageHash() == c2.storageHash(),
                "distinct cells over the same storage hash alike");
        // Pin the approved scheme (UnsafeTable.ArrayRef precedent): the hash
        // covers exactly the components sameStorageAs compares.
        assertTrue(c1.storageHash() == System.identityHashCode(s) * 31 + 0,
                "field-cell hash must be identityHashCode(struct)*31+fieldIndex");
        GoCell otherField = new GoFieldCell(s, 1, GoKind.POINTER);
        assertTrue(otherField.storageHash() == System.identityHashCode(s) * 31 + 1,
                "field index must discriminate the hash");
    }

    public void testStorageHashArrayCell() {
        Object arr = new Object[4];
        GoCell c1 = new GoArrayCell(arr, GoKind.POINTER, 2);
        GoCell c2 = new GoArrayCell(arr, GoKind.POINTER, 2);
        assertTrue(c1.sameStorageAs(c2), "same storage");
        assertTrue(c1.storageHash() == c2.storageHash(),
                "distinct cells over the same storage hash alike");
        // Pin the approved scheme: identityHashCode(array)*31+index.
        assertTrue(c1.storageHash() == System.identityHashCode(arr) * 31 + 2,
                "array-cell hash must be identityHashCode(array)*31+index");
    }

    /** The contract as a property over a battery of cell pairs, cross-kind included. */
    public void testStorageHashContractBattery() {
        GoStructDescriptor d = twoPtrDesc();
        GoStruct s1 = newStruct(d);
        GoStruct s2 = newStruct(d);
        Object arr = new Object[4];
        GoBox box = new GoBox(null);
        GoCell[] cells = {
                box,
                new GoBox(null),
                new GoFieldCell(s1, 0, GoKind.POINTER),
                new GoFieldCell(s1, 0, GoKind.POINTER), // fresh, same storage
                new GoFieldCell(s1, 1, GoKind.POINTER),
                new GoFieldCell(s2, 0, GoKind.POINTER),
                new GoArrayCell(arr, GoKind.POINTER, 2),
                new GoArrayCell(arr, GoKind.POINTER, 2), // fresh, same storage
                new GoArrayCell(arr, GoKind.POINTER, 3),
                new GoArrayCell(new Object[4], GoKind.POINTER, 2),
        };
        for (int i = 0; i < cells.length; i++) {
            for (int j = 0; j < cells.length; j++) {
                assertStorageHashConsistent(cells[i], cells[j], "cells[" + i + "]/cells[" + j + "]");
            }
        }
    }

    // ------------------------------------------------------------------
    // R15-P1-3: GoStorageKey contract — of(a).equals(of(b)) iff
    // a.sameStorageAs(b), with consistent hashCodes, over every row of the
    // pointer-shape table plus the cross-kind partition and nil handling.
    // ------------------------------------------------------------------

    private static void assertKeyMirrors(GoPointer a, GoPointer b, String what) {
        GoStorageKey ka = GoStorageKey.of(a);
        GoStorageKey kb = GoStorageKey.of(b);
        boolean storageEq = a.sameStorageAs(b);
        assertTrue(ka.equals(kb) == storageEq,
                what + ": key equality must mirror sameStorageAs (sameStorageAs=" + storageEq + ")");
        assertTrue(kb.equals(ka) == storageEq, what + ": symmetric");
        if (storageEq) {
            assertTrue(ka.hashCode() == kb.hashCode(),
                    what + ": equal keys must have equal hashes");
        }
    }

    public void testStorageKeyBoxCell() {
        GoBox box = new GoBox(null);
        // Same storage via distinct wrappers (two &x evaluations).
        assertKeyMirrors(new GoPointer(box), new GoPointer(box), "&x vs &x");
        assertKeyMirrors(new GoPointer(box), new GoPointer(new GoBox(null)), "&x vs &y");
    }

    public void testStorageKeyFieldCell() {
        GoStructDescriptor d = twoPtrDesc();
        GoStruct s1 = newStruct(d);
        GoStruct s2 = newStruct(d);
        // Fresh GoFieldCell per evaluation, same (struct, field).
        assertKeyMirrors(
                new GoPointer(new GoFieldCell(s1, 0, GoKind.POINTER)),
                new GoPointer(new GoFieldCell(s1, 0, GoKind.POINTER)),
                "&s.a vs &s.a");
        assertKeyMirrors(
                new GoPointer(new GoFieldCell(s1, 0, GoKind.POINTER)),
                new GoPointer(new GoFieldCell(s1, 1, GoKind.POINTER)),
                "&s.a vs &s.b");
        assertKeyMirrors(
                new GoPointer(new GoFieldCell(s1, 0, GoKind.POINTER)),
                new GoPointer(new GoFieldCell(s2, 0, GoKind.POINTER)),
                "&s1.a vs &s2.a");
    }

    public void testStorageKeyElem() {
        Object arr1 = new Object[4];
        Object arr2 = new Object[4];
        assertKeyMirrors(new GoPointer(arr1, 2), new GoPointer(arr1, 2), "&a[2] vs &a[2]");
        assertKeyMirrors(new GoPointer(arr1, 2), new GoPointer(arr1, 3), "&a[2] vs &a[3]");
        assertKeyMirrors(new GoPointer(arr1, 2), new GoPointer(arr2, 2), "different backing arrays");
    }

    public void testStorageKeyReferent() {
        Object r = new Object();
        assertKeyMirrors(new GoPointer(r), new GoPointer(r), "same referent twice");
        assertKeyMirrors(new GoPointer(r), new GoPointer(new Object()), "distinct referents");
    }

    /** Row 6 of the shape table: a defensive snapshot's fresh box never collides. */
    public void testStorageKeySnapshotNeverCollides() {
        assertKeyMirrors(new GoPointer(new GoBox(null)), new GoPointer(new GoBox(null)),
                "two fresh snapshot boxes");
    }

    /** Cross-kind partition: cell != elem != ref, in both directions. */
    public void testStorageKeyCrossKind() {
        GoStructDescriptor d = twoPtrDesc();
        GoStruct s = newStruct(d);
        Object arr = new Object[4];
        GoPointer cell = new GoPointer(new GoFieldCell(s, 0, GoKind.POINTER));
        GoPointer elem = new GoPointer(arr, 0);
        GoPointer ref = new GoPointer(new Object());
        GoPointer arrayCell = new GoPointer(new GoArrayCell(arr, GoKind.POINTER, 1));
        GoPointer[] ps = { cell, elem, ref, arrayCell };
        for (int i = 0; i < ps.length; i++) {
            for (int j = 0; j < ps.length; j++) {
                if (i != j) {
                    assertFalse(GoStorageKey.of(ps[i]).equals(GoStorageKey.of(ps[j])),
                            "cross-kind keys must differ: " + i + " vs " + j);
                }
            }
        }
        // Array cell vs elem pointer over the SAME array: still different kinds.
        assertFalse(new GoPointer(arr, 1).sameStorageAs(arrayCell), "sanity: &a[1] != cell &a[1]");
    }

    public void testStorageKeyNil() {
        GoPointer nilRef = new GoPointer((Object) null);
        GoPointer nilRef2 = new GoPointer((Object) null);
        GoPointer nilElem = new GoPointer((Object) null, 0);
        // sameStorageAs equates all nils regardless of shape; the key must too.
        assertKeyMirrors(nilRef, nilRef2, "nil vs nil");
        assertKeyMirrors(nilRef, nilElem, "nil referent vs nil element pointer");
        GoStructDescriptor d = twoPtrDesc();
        assertKeyMirrors(nilRef,
                new GoPointer(new GoFieldCell(newStruct(d), 0, GoKind.POINTER)),
                "nil vs &s.a");
        assertKeyMirrors(nilRef, new GoPointer(new Object[4], 1), "nil vs &a[1]");
        assertKeyMirrors(nilRef, new GoPointer(new Object()), "nil vs referent");
    }

    public void testStorageKeyEqualsContract() {
        GoBox box = new GoBox(null);
        GoStorageKey k = GoStorageKey.of(new GoPointer(box));
        assertTrue(k.equals(k), "reflexive");
        assertFalse(k.equals(null), "never equal to null");
        assertFalse(k.equals("not-a-key"), "never equal to another type");
    }

    /** Exact mirroring as a property over a battery of pointer pairs. */
    public void testStorageKeyMirrorsSameStorageAsBattery() {
        GoStructDescriptor d = twoPtrDesc();
        GoStruct s1 = newStruct(d);
        GoStruct s2 = newStruct(d);
        Object arr1 = new Object[4];
        Object arr2 = new Object[4];
        GoBox box1 = new GoBox(null);
        Object ref1 = new Object();
        GoPointer[] ps = {
                new GoPointer((Object) null),          // nil referent
                new GoPointer((Object) null, 0),       // nil element pointer
                new GoPointer(box1),                   // &x
                new GoPointer(box1),                   // &x again (fresh wrapper)
                new GoPointer(new GoBox(null)),        // &y
                new GoPointer(new GoFieldCell(s1, 0, GoKind.POINTER)),
                new GoPointer(new GoFieldCell(s1, 0, GoKind.POINTER)), // fresh cell
                new GoPointer(new GoFieldCell(s1, 1, GoKind.POINTER)),
                new GoPointer(new GoFieldCell(s2, 0, GoKind.POINTER)),
                new GoPointer(new GoArrayCell(arr1, GoKind.POINTER, 2)),
                new GoPointer(new GoArrayCell(arr1, GoKind.POINTER, 2)), // fresh cell
                new GoPointer(arr1, 2),                // &a[2] elem pointer
                new GoPointer(arr1, 2),                // &a[2] again
                new GoPointer(arr1, 3),
                new GoPointer(arr2, 2),
                new GoPointer(ref1),
                new GoPointer(ref1),                   // same referent
                new GoPointer(new Object()),
        };
        for (int i = 0; i < ps.length; i++) {
            for (int j = 0; j < ps.length; j++) {
                assertKeyMirrors(ps[i], ps[j], "ps[" + i + "]/ps[" + j + "]");
            }
        }
    }
}
