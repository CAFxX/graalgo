package graalgo.go.nodes;

import com.oracle.truffle.api.dsl.Introspection;
import graalgo.go.runtime.Values;
import graalgo.go.types.GoType;
import graalgo.go.types.GoType.Field;
import graalgo.go.types.TypeTable;
import graalgo.go.values.Complex;
import graalgo.go.values.Flat;
import graalgo.go.values.GoMap;
import graalgo.go.values.Iface;
import graalgo.go.values.Slice;
import graalgo.go.values.Str;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Random;
import java.util.Set;

/**
 * The PE-visible key nodes of {@link MapKeys} (truffle-audit WP 4): every {@link MapKeys.KeyOps}
 * computes exactly the generic {@link Values#hash}/{@link Values#equal} of its type (NaN, ±0,
 * blank fields, shared string storage, nested and merged leaves, interfaces of every kind), so
 * a map probed by site nodes and by reflection agrees; the interface caches go monomorphic →
 * polymorphic at the limit → megamorphic with the same answers and the same panics; and the
 * map keeps gc's key semantics (NaN keys, NeedKeyUpdate) and a random range start. In-process,
 * no engine: well under a second.
 */
public final class KeyOpsTest {
    private final TypeTable tt = new TypeTable();
    private final GoType tInt = tt.basic(GoType.INT);
    private final GoType tI8 = tt.basic(GoType.INT8);
    private final GoType tU8 = tt.basic(GoType.UINT8);
    private final GoType tBool = tt.basic(GoType.BOOL);
    private final GoType tF64 = tt.basic(GoType.FLOAT64);
    private final GoType tStr = tt.basic(GoType.STRING);
    private final GoType tC128 = tt.basic(GoType.COMPLEX128);
    private final GoType tAny = tt.emptyInterface;
    private final GoType tPtr = tt.pointer(tInt);
    private final GoType tChan = tt.chan(1, tInt);
    private final GoType tSlice = tt.slice(tInt);
    private final Flat[] targets = {new Flat(null, new long[1], Flat.NO_REFS, Flat.NO_BYTES),
        new Flat(null, new long[1], Flat.NO_REFS, Flat.NO_BYTES)};
    private final Object[] chans = {new Object(), new Object()};
    /** The content of generated values; {@link #rep} picks representations (string storage). */
    private Random rnd = new Random(42);
    private final Random rep = new Random(7);
    private final Random seeds = new Random(1);

    private static void check(boolean ok, String what) {
        if (!ok) {
            throw new AssertionError(what);
        }
    }

    private static Field f(String name, GoType t) {
        return new Field(name, "", t, false, "");
    }

    private GoType struct(GoType... ts) {
        Field[] fs = new Field[ts.length];
        for (int i = 0; i < ts.length; i++) {
            fs[i] = f("F" + i, ts[i]);
        }
        return tt.struct(fs);
    }

    /** {a int; _ int; s string; x float64; b uint8; c bool; p *int; ch chan int; i int8}. */
    private GoType mixed() {
        return tt.struct(new Field[] {f("a", tInt), f("_", tInt), f("s", tStr), f("x", tF64), f("b", tU8),
            f("c", tBool), f("p", tPtr), f("ch", tChan), f("i", tI8)});
    }

    private static final String[] WORDS = {"", "a", "ab", "abcdefgh", "abcdefghi", "key-12345678"};

    /** A small-domain random value of {@code t} in carrier form (collisions are the point). */
    private Object value(GoType t, int depth) {
        GoType u = t.u();
        switch (u.kind) {
            case GoType.STRING: {
                // The same bytes at different offsets of different arrays.
                String w = WORDS[rnd.nextInt(WORDS.length)];
                byte[] pad = ("##" + w + "#").getBytes(StandardCharsets.UTF_8);
                return rep.nextBoolean() ? Str.of(w) : new Str(pad, 2, w.length());
            }
            case GoType.POINTER:
                return rnd.nextInt(3) == 0 ? null : targets[rnd.nextInt(2)];
            case GoType.CHAN:
                return rnd.nextInt(3) == 0 ? null : chans[rnd.nextInt(2)];
            case GoType.COMPLEX128:
                return new Complex(rnd.nextBoolean() ? 0.0 : -0.0, rnd.nextInt(3) == 0 ? Double.NaN : 1);
            case GoType.SLICE:
                return Slice.NIL;
            case GoType.INTERFACE: {
                if (rnd.nextInt(5) == 0) {
                    return null;
                }
                GoType[] dyn = depth > 1 ? new GoType[] {tInt, tStr, tF64}
                    : new GoType[] {tInt, tStr, tF64, tBool, tPtr, tC128, mixed(), struct(tInt, tAny), tt.array(2, tStr)};
                GoType d = dyn[rnd.nextInt(dyn.length)];
                return new Iface(d, value(d, depth + 1));
            }
            case GoType.STRUCT, GoType.ARRAY: {
                Flat o = u.zeroFlat();
                if (u.kind == GoType.STRUCT) {
                    for (Field fl : u.fields) {
                        Values.store(fl.type, o, fl.primOff, fl.refOff, fl.byteOff, value(fl.type, depth + 1));
                    }
                } else {
                    GoType e = u.elem;
                    for (int i = 0; i < u.len; i++) {
                        Values.store(e, o, i * e.nPrim(), i * e.nRef(), i * e.nByte(), value(e, depth + 1));
                    }
                }
                return o;
            }
            default:
                break;
        }
        return switch (u.carrier()) {
            case GoType.C_I64 -> (long) (rnd.nextInt(3) - 1);
            case GoType.C_F64 -> new double[] {0.0, -0.0, Double.NaN, 1.5}[rnd.nextInt(4)];
            case GoType.C_BOOL -> rnd.nextBoolean();
            default -> throw new AssertionError("no generator for " + t.str());
        };
    }

    /** Node vs generic over every pair of a sample: same ==, same hash, and equal values hash alike. */
    private void agree(GoType t, int n) {
        MapKeys.KeyOps ops = MapKeys.create(t);
        List<Object> vs = new ArrayList<>();
        for (int i = 0; i < n; i++) {
            // Each value twice: the same content, independently built (equal, not identical).
            long seed = seeds.nextLong();
            rnd = new Random(seed);
            vs.add(value(t, 0));
            rnd = new Random(seed);
            vs.add(value(t, 0));
        }
        int equalPairs = 0;
        for (Object a : vs) {
            check(ops.hash(a) == Values.hash(t, a), t.str() + ": node hash = generic hash");
            for (Object b : vs) {
                boolean want = Values.equal(t, a, b);
                check(ops.eq(a, b) == want, t.str() + ": node == generic ==");
                if (want) {
                    equalPairs++;
                    check(ops.hash(a) == ops.hash(b), t.str() + ": equal values hash alike");
                }
            }
        }
        check(equalPairs > vs.size(), t.str() + ": the sample has equal pairs beyond the diagonal: " + equalPairs);
    }

    public void testNodesAgreeWithTheGenericCode() {
        GoType[] types = {tStr, tF64, tPtr, tChan, mixed(), tt.array(4, tU8), tt.array(3, tInt),
            tt.array(2, struct(tI8, tStr)), struct(struct(tInt, tF64), tt.array(2, tBool)), struct(tAny, tInt),
            tC128, tt.array(20, tStr), struct(), tAny};
        for (GoType t : types) {
            agree(t, 40);
        }
        check(MapKeys.create(mixed()) instanceof MapKeys.PlanOps, "a struct of scalars/strings/pointers has a plan");
        check(MapKeys.create(tt.array(64, tU8)) instanceof MapKeys.PlanOps, "a byte array is one merged run");
        check(MapKeys.create(struct(tAny, tInt)) instanceof MapKeys.GenericOps, "an interface leaf: generic");
        check(MapKeys.create(tt.array(20, tStr)) instanceof MapKeys.GenericOps, "past MAX_ENTRIES: generic");
        check(MapKeys.create(tStr) instanceof MapKeys.StrOps, "string keys");
    }

    public void testFloatKeysNaNAndSignedZero() {
        MapKeys.KeyOps f = MapKeys.create(tF64);
        check(!f.eq(Double.NaN, Double.NaN), "NaN != NaN");
        check(f.eq(0.0, -0.0) && f.hash(0.0) == f.hash(-0.0), "+0 == -0, same hash");
        GoType s = struct(tF64, tStr);
        MapKeys.KeyOps p = MapKeys.create(s);
        Flat a = s.zeroFlat();
        Flat b = s.zeroFlat();
        Values.store(tF64, a, 0, 0, 0, -0.0);
        check(p.eq(a, b) && p.hash(a) == p.hash(b), "struct{-0} == struct{+0}");
        Values.store(tF64, a, 0, 0, 0, Double.NaN);
        check(!p.eq(a, a), "struct{NaN} != itself");
    }

    public void testStringHashIsCachedAndContentBased() {
        byte[] big = "xxhello, worldxx".getBytes(StandardCharsets.UTF_8);
        Str a = new Str(big, 2, 12);
        Str b = Str.of("hello, world");
        check(a.hash() == b.hash() && a.hash() == a.hashCode(), "same bytes, same hash");
        Set<Integer> hs = new HashSet<>();
        for (int i = 0; i < 20000; i++) {
            hs.add(Str.of("key-" + i * 31).hash());
        }
        check(hs.size() > 19990, "distinct keys spread: " + hs.size());
    }

    /** Four dynamic types through an interface cache of limit 3, with nil. */
    private Object[] ifaces() {
        GoType pl = struct(tInt, tStr);
        Flat pv = pl.zeroFlat();
        Values.store(tStr, pv, 0, 0, 0, Str.of("x"));
        return new Object[] {new Iface(tInt, 7L), new Iface(tStr, Str.of("s")), new Iface(pl, pv),
            new Iface(tF64, -0.0), null};
    }

    public void testIfaceEqMonoPolyMega() {
        MapKeys.IfaceEq eq = MapKeysFactory.IfaceEqNodeGen.create();
        Object[] v = ifaces();
        Object[] same = ifaces();
        check(eq.execute(v[0], same[0]) && !eq.execute(v[0], v[1]), "mono: int");
        check(Introspection.getSpecialization(eq, "cached").getInstances() == 1, "one cache entry");
        check(eq.execute(v[1], same[1]) && eq.execute(v[2], same[2]), "poly: string, struct");
        check(Introspection.getSpecialization(eq, "cached").getInstances() == 3, "three entries at the limit");
        check(!Introspection.getSpecialization(eq, "generic").isActive(), "not megamorphic yet");
        check(eq.execute(v[3], new Iface(tF64, 0.0)), "mega: -0 == +0 through float64");
        check(Introspection.getSpecialization(eq, "generic").isActive()
            && !Introspection.getSpecialization(eq, "cached").isActive(), "megamorphic replaces the cache");
        for (int i = 0; i < v.length; i++) {
            for (int j = 0; j < v.length; j++) {
                check(eq.execute(v[i], same[j]) == Values.ifaceEqual(v[i], same[j]), "mega == generic " + i + "," + j);
            }
        }
        check(eq.execute(null, null) && !eq.execute(null, v[0]) && !eq.execute(v[0], null), "nil");
        check(Introspection.getSpecialization(eq, "nil").isActive(), "nil specialization");
    }

    public void testIfaceHashMonoPolyMega() {
        MapKeys.IfaceHash h = MapKeysFactory.IfaceHashNodeGen.create();
        Object[] v = ifaces();
        for (int i = 0; i < 3; i++) {
            check(h.execute(v[i]) == Values.ifaceHash(v[i]), "cached hash = generic " + i);
        }
        check(Introspection.getSpecialization(h, "cached").getInstances() == 3, "three entries at the limit");
        check(h.execute(v[3]) == Values.ifaceHash(v[3]), "mega hash = generic");
        check(Introspection.getSpecialization(h, "generic").isActive()
            && !Introspection.getSpecialization(h, "cached").isActive(), "megamorphic replaces the cache");
        check(h.execute(null) == 0, "nil hashes 0");
    }

    /** The panic's type and kind are the generic path's, cached or megamorphic, direct or nested. */
    public void testUnhashableDynamicTypesPanicAsTheGenericCode() {
        GoType holder = struct(tAny);
        Flat inner = holder.zeroFlat();
        Values.store(tAny, inner, 0, 0, 0, new Iface(tSlice, Slice.NIL));
        Object[] bad = {new Iface(tSlice, Slice.NIL), new Iface(holder, inner)};
        for (Object x : bad) {
            Values.Uncomparable wantEq = null;
            Values.Uncomparable wantHash = null;
            try {
                Values.ifaceEqual(x, x);
            } catch (Values.Uncomparable u) {
                wantEq = u;
            }
            try {
                Values.ifaceHash(x);
            } catch (Values.Uncomparable u) {
                wantHash = u;
            }
            check(wantEq != null && wantHash != null && wantHash.hashing && !wantEq.hashing, "generic panics");
            check(wantEq.type == tSlice, "the slice type is reported");
            MapKeys.IfaceEq eq = MapKeysFactory.IfaceEqNodeGen.create();
            MapKeys.IfaceHash h = MapKeysFactory.IfaceHashNodeGen.create();
            for (int round = 0; round < 2; round++) {
                try {
                    eq.execute(x, x);
                    check(false, "== panics");
                } catch (Values.Uncomparable u) {
                    check(u.type == wantEq.type && !u.hashing, "== panic type");
                }
                try {
                    h.execute(x);
                    check(false, "hash panics");
                } catch (Values.Uncomparable u) {
                    check(u.type == wantHash.type && u.hashing, "hash panic type");
                }
                // Megamorphic on the second round.
                for (Object o : ifaces()) {
                    eq.execute(o, o);
                    h.execute(o);
                }
            }
            check(!eq.execute(x, new Iface(tInt, 1L)), "a different dynamic type is unequal without hashing");
        }
    }

    /** Site nodes and the generic (reflection) path share one table. */
    public void testMapNodeAndGenericPathsInteroperate() {
        GoType[] kts = {tStr, mixed(), tAny, tF64};
        for (GoType kt : kts) {
            GoMap m = new GoMap(kt, tInt, 0);
            MapKeys.KeyOps ops = MapKeys.create(kt);
            List<Object> keys = new ArrayList<>();
            for (int i = 0; i < 400; i++) {
                keys.add(value(kt, 0));
            }
            for (int i = 0; i < keys.size(); i++) {
                Object k = keys.get(i);
                int e = i % 2 == 0 ? m.putRaw(ops.hash(k), k, ops) : m.put(k);
                m.vprims[e] = i;
            }
            for (Object k : keys) {
                int a = m.findRaw(ops.hash(k), k, ops);
                int b = m.find(k);
                check(a == b, kt.str() + ": node and generic find the same entry");
                check(a >= 0 || !Values.equal(kt, k, k), kt.str() + ": a key equal to itself is found");
            }
            Object first = keys.get(0);
            if (Values.equal(kt, first, first)) {
                m.delete(m.findRaw(ops.hash(first), first, ops));
                check(m.find(first) < 0, kt.str() + ": deleted by the node path");
            }
        }
    }

    public void testNaNKeysAndKeyUpdate() {
        GoMap m = new GoMap(tF64, tInt, 0);
        MapKeys.KeyOps ops = MapKeys.create(tF64);
        for (int i = 0; i < 3; i++) {
            m.putRaw(ops.hash(Double.NaN), Double.NaN, ops);
        }
        check(m.len() == 3, "each NaN insert is a new entry");
        check(m.findRaw(ops.hash(Double.NaN), Double.NaN, ops) < 0, "NaN is never found");
        int e = m.putRaw(ops.hash(0.0), 0.0, ops);
        check(m.putRaw(ops.hash(-0.0), -0.0, ops) == e && m.len() == 4, "-0 assigns the +0 entry");
        check(Double.doubleToRawLongBits((Double) m.keyAt(e)) == Double.doubleToRawLongBits(-0.0),
            "the entry's key is the latest (gc NeedKeyUpdate)");
    }

    /** Each range visits every entry once, across growth, from a random start (gc randomizes). */
    public void testRangeStartIsRandomAndComplete() {
        GoMap m = new GoMap(tStr, tInt, 0);
        MapKeys.KeyOps ops = MapKeys.create(tStr);
        for (int i = 0; i < 1000; i++) {
            Str k = Str.of("k" + i);
            m.putRaw(ops.hash(k), k, ops);
        }
        Set<Integer> starts = new HashSet<>();
        for (int r = 0; r < 50; r++) {
            GoMap.Iter it = new GoMap.Iter(m);
            Set<Integer> seen = new HashSet<>();
            int first = -2;
            for (int e = it.next(); e >= 0; e = it.next()) {
                if (first == -2) {
                    first = e;
                }
                check(seen.add(e), "an entry at most once");
            }
            check(seen.size() == 1000, "every entry");
            starts.add(first);
        }
        check(starts.size() > 25, "range starts vary: " + starts.size());
    }
}
