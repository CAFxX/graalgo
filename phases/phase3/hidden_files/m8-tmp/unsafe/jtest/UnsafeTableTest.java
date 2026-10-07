import graalgo.p3unsafe.UnsafeTable;
import graalgo.p3unsafe.UnsafeOps;
import graalgo.values.*;

public class UnsafeTableTest {
    static int checks = 0;
    static void check(boolean b, String msg) {
        checks++;
        if (!b) throw new AssertionError("FAIL: " + msg);
    }
    public static void main(String[] a) {
        UnsafeTable.install();
        UnsafeTable t = UnsafeTable.current();

        // interning: same referent -> same handle (== parity)
        Object r = new Object();
        long h1 = t.intern(r), h2 = t.intern(r);
        check(h1 == h2 && h1 != 0, "intern identity");
        check(t.intern(null) == 0, "intern null -> 0");
        check(t.resolve(h1) == r, "resolve");

        // scalar normalization: Integer box -> int[1]
        long hs = t.intern(Integer.valueOf(42));
        Object rs = t.resolve(hs);
        check(rs instanceof int[] && ((int[]) rs)[0] == 42, "normalizeScalar int");

        // internEqual: value equality
        UnsafeTable.ArrayRef k1 = new UnsafeTable.ArrayRef(new int[4], 0);
        UnsafeTable.ArrayRef k2 = new UnsafeTable.ArrayRef(k1.array(), 0);
        check(t.internEqual(k1) == t.internEqual(k2), "internEqual value eq");
        UnsafeTable.ArrayRef k3 = new UnsafeTable.ArrayRef(k1.array(), 1);
        check(t.internEqual(k1) != t.internEqual(k3), "internEqual offset differs");

        // free: never reused
        t.free(h1);
        long h1b = t.intern(r);
        check(h1b != h1, "free never reuses");

        // slice shapes
        int[] arr = {1, 2, 3, 4};
        GoSlice s = UnsafeOps.slice(arr, 3, GoKind.INT32, null);
        check(s.len() == 3 && s.array() == arr, "slice whole array");
        GoSlice nilS = UnsafeOps.slice(null, 0, GoKind.INT32, null);
        check(nilS == null, "slice nil len0 -> nil");
        boolean panicked = false;
        try { UnsafeOps.slice(null, 5, GoKind.INT32, null); }
        catch (graalgo.runtime.PanicException pe) {
            panicked = String.valueOf(pe.goValue()).equals(
                "runtime error: unsafe.Slice: ptr is nil and len is not zero");
        }
        check(panicked, "slice nil len5 gc-exact panic");
        panicked = false;
        try { UnsafeOps.slice(arr, 5, GoKind.INT32, null); }
        catch (graalgo.runtime.PanicException pe) {
            panicked = String.valueOf(pe.goValue()).equals("unsafe.Slice: len out of range");
        }
        check(panicked, "slice over-long fail-fast");
        panicked = false;
        try { UnsafeOps.slice(arr, 2, GoKind.INT64, null); }
        catch (graalgo.runtime.PanicException pe) { panicked = true; }
        check(panicked, "slice carrier mismatch fail-fast");

        // ArrayRef view with offset
        long hd = UnsafeOps.sliceData(new GoSlice(arr, 1, 2, 3));
        Object ref = t.resolve(hd);
        check(ref instanceof UnsafeTable.ArrayRef ar && ar.offset() == 1, "sliceData ArrayRef");
        GoSlice s2 = UnsafeOps.slice(ref, 2, GoKind.INT32, null);
        check(s2.offset() == 1 && s2.len() == 2, "slice over ArrayRef");

        // string shapes (DIV-US-1: copies)
        GoString g = UnsafeOps.stringOf(arr, 2, null);
        check(g.length() == 2, "stringOf int[] len");
        arr[0] = 99;
        check(g.bytes()[0] == 1, "stringOf copies (DIV-US-1)");
        GoString empty = UnsafeOps.stringOf(null, 0, null);
        check(empty.length() == 0, "stringOf nil len0");

        // stringData: empty -> 0, else interned
        check(UnsafeOps.stringData(GoString.EMPTY) == 0, "stringData empty -> 0");
        GoString hello = GoString.fromUtf8("hello");
        check(UnsafeOps.stringData(hello) == UnsafeOps.stringData(hello), "stringData interned");

        // traps
        boolean trapped = false;
        try { UnsafeOps.addTrap(); }
        catch (UnsafeHandles.UnsafeEscapeException e) { trapped = true; }
        check(trapped, "addTrap");
        trapped = false;
        try { UnsafeOps.fromUintTrap(); }
        catch (UnsafeHandles.UnsafeEscapeException e) { trapped = true; }
        check(trapped, "fromUintTrap");

        System.out.println("ALL " + checks + " CHECKS PASSED");
    }
}
