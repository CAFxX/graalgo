package graalgo.go.runtime;

import java.util.ArrayList;
import java.util.List;

/**
 * The uintptr handle table (GG-0062): ranges resolve to their object and offset (the end
 * included, nothing past it), one object keeps one base, and the table stays linear in the live
 * objects — many conversions of short-lived objects (abi.NoEscape's shape) neither grow it
 * without bound nor lose a live range when dead ones are squeezed out.
 */
public final class HandlesTest {
    private static void check(boolean ok, String what) {
        if (!ok) {
            throw new AssertionError(what);
        }
    }

    public void testRangesResolveToTheirObjectAndOffset() {
        Handles h = new Handles();
        List<Object> objs = new ArrayList<>();
        List<Long> bases = new ArrayList<>();
        for (int i = 0; i < 5000; i++) {
            Object o = new Object();
            objs.add(o);
            bases.add(h.base(o, 8 + i % 40));
        }
        for (int i = 0; i < objs.size(); i++) {
            check(h.base(objs.get(i), 8 + i % 40) == bases.get(i), "stable base " + i);
            long ext = 8 + i % 40;
            Object[] r = h.resolve(bases.get(i) + ext);
            check(r != null && r[0] == objs.get(i) && (long) r[1] == ext, "end of range " + i);
            r = h.resolve(bases.get(i) + 3);
            check(r != null && r[0] == objs.get(i) && (long) r[1] == 3, "inside range " + i);
            r = h.resolve(bases.get(i) + ext + 1);
            check(r == null || r[0] != objs.get(i), "past the end " + i);
        }
        check(h.resolve(bases.get(0) - 1) == null, "below every range");
    }

    public void testCompactionKeepsLiveRanges() {
        Handles h = new Handles();
        Object keep = new Object();
        long kb = h.base(keep, 16);
        Object[] young = new Object[64];
        long[] youngBase = new long[64];
        for (int i = 0; i < 300_000; i++) {
            Object o = new Object();
            long b = h.base(o, 16);
            young[i % 64] = o;
            youngBase[i % 64] = b;
            if (i % 100_000 == 0) {
                System.gc();
            }
        }
        for (int i = 0; i < 64; i++) {
            Object[] y = h.resolve(youngBase[i] + 16);
            check(y != null && y[0] == young[i], "a live young object resolves after compactions " + i);
        }
        Object[] r = h.resolve(kb + 4);
        check(r != null && r[0] == keep, "a live object survives compaction");
        check(h.base(keep, 16) == kb, "and keeps its base");
    }
}
