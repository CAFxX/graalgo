package graalgo.go.runtime;

import com.oracle.truffle.api.CompilerDirectives.CompilationFinal;
import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.ExplodeLoop;
import graalgo.go.types.GoType;
import graalgo.go.types.GoType.Field;
import graalgo.go.values.Flat;
import graalgo.go.values.Ptr;
import graalgo.go.values.Str;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

/**
 * Go {@code ==} and the map hash of a struct or array type whose leaves are all scalars,
 * strings, pointers or channels (F-6.3): the non-blank leaves in region order, runs of
 * consecutive integer or byte slots merged, at most {@link #MAX_ENTRIES} entries. A node that
 * holds a plan in a final field compiles {@link #eq}/{@link #hash} to straight-line slot
 * compares (the arrays are compilation-final, the entry loop exploded); the generic
 * {@link Values} code uses the same plan, so the two agree on every hash.
 *
 * <p>Types with an interface, complex, slice, map or func leaf have no plan ({@link #of} is
 * null): their {@code ==} can panic or needs a dynamic type, and stays on the generic path.
 */
public final class KeyPlan {
    static final int PRIMS = 0;
    static final int BYTES = 1;
    static final int F64 = 2;
    static final int STR = 3;
    static final int PTR = 4;
    static final int CHAN = 5;
    /** Entries past which a type keeps the generic path (graph size; [64]string keys exist). */
    static final int MAX_ENTRIES = 8;
    /** Raw leaves past which collection gives up before merging (huge arrays of mixed leaves). */
    private static final int MAX_LEAVES = 4096;
    private static final Object NONE = new Object();

    @CompilationFinal(dimensions = 1) private final int[] kind;
    @CompilationFinal(dimensions = 1) private final int[] off;
    @CompilationFinal(dimensions = 1) private final int[] n;

    private KeyPlan(int[] kind, int[] off, int[] n) {
        this.kind = kind;
        this.off = off;
        this.n = n;
    }

    /** The plan of struct/array type {@code t}, or null if it has none. Cached in the type. */
    public static KeyPlan of(GoType t) {
        GoType u = t.u();
        Object c = u.keyPlan;
        if (c == null) {
            c = build(u);
            u.keyPlan = c == null ? NONE : c;
        }
        return c == NONE ? null : (KeyPlan) c;
    }

    @TruffleBoundary
    private static KeyPlan build(GoType u) {
        if (u.kind != GoType.STRUCT && u.kind != GoType.ARRAY) {
            return null;
        }
        u.nPrim(); // lay out: field offsets are computed lazily
        List<int[]> leaves = new ArrayList<>();
        if (!collect(u, 0, 0, 0, leaves)) {
            return null;
        }
        // Region order: prims, bytes, refs; a run merges slots k, k+1, ... of one region.
        leaves.sort((x, y) -> region(x[0]) != region(y[0]) ? Integer.compare(region(x[0]), region(y[0]))
                : Integer.compare(x[1], y[1]));
        List<int[]> merged = new ArrayList<>();
        for (int[] l : leaves) {
            int[] last = merged.isEmpty() ? null : merged.get(merged.size() - 1);
            if (last != null && last[0] == l[0] && (l[0] == PRIMS || l[0] == BYTES) && last[1] + last[2] == l[1]) {
                last[2]++;
            } else {
                merged.add(new int[] {l[0], l[1], 1});
            }
            if (merged.size() > MAX_ENTRIES) {
                return null;
            }
        }
        int m = merged.size();
        int[] kind = new int[m];
        int[] off = new int[m];
        int[] n = new int[m];
        for (int i = 0; i < m; i++) {
            kind[i] = merged.get(i)[0];
            off[i] = merged.get(i)[1];
            n[i] = merged.get(i)[2];
        }
        return new KeyPlan(kind, off, n);
    }

    private static int region(int kind) {
        return switch (kind) {
            case PRIMS, F64 -> 0;
            case BYTES -> 1;
            default -> 2;
        };
    }

    private static boolean collect(GoType t, int p, int r, int b, List<int[]> out) {
        if (out.size() > MAX_LEAVES) {
            return false;
        }
        GoType u = t.u();
        switch (u.kind) {
            case GoType.STRUCT:
                u.nPrim();
                for (Field f : u.fields) {
                    if (!f.name.equals("_") && !collect(f.type, p + f.primOff, r + f.refOff, b + f.byteOff, out)) {
                        return false;
                    }
                }
                return true;
            case GoType.ARRAY: {
                GoType e = u.elem;
                int np = e.nPrim();
                int nr = e.nRef();
                int nb = e.nByte();
                if (u.len > MAX_LEAVES) {
                    return false;
                }
                for (int i = 0; i < u.len; i++) {
                    if (!collect(e, p + i * np, r + i * nr, b + i * nb, out)) {
                        return false;
                    }
                }
                return true;
            }
            default:
                break;
        }
        switch (u.carrier()) {
            case GoType.C_I64, GoType.C_BOOL:
                out.add(u.isByteLeaf() ? new int[] {BYTES, b} : new int[] {PRIMS, p});
                return true;
            case GoType.C_F64:
                out.add(new int[] {F64, p});
                return true;
            default:
                return switch (u.kind) {
                    case GoType.STRING -> out.add(new int[] {STR, r});
                    case GoType.POINTER, GoType.UNSAFE_POINTER -> out.add(new int[] {PTR, r});
                    case GoType.CHAN -> out.add(new int[] {CHAN, r});
                    default -> false;
                };
        }
    }

    /** The hash of the value at ({@code p}, {@code r}, {@code b}) of {@code a}. */
    @ExplodeLoop
    public int hash(Flat a, int p, int r, int b) {
        int h = 17;
        for (int i = 0; i < kind.length; i++) {
            int o = off[i];
            int c = n[i];
            switch (kind[i]) {
                case PRIMS:
                    for (int j = 0; j < c; j++) {
                        h = h * 31 + Long.hashCode(a.prims[p + o + j]);
                    }
                    break;
                case BYTES:
                    for (int j = 0; j < c; j++) {
                        h = h * 31 + a.bytes[b + o + j];
                    }
                    break;
                case F64: {
                    double d = Double.longBitsToDouble(a.prims[p + o]);
                    h = h * 31 + (d == 0 ? 0 : Double.hashCode(d));
                    break;
                }
                case STR:
                    h = h * 31 + ((Str) a.refs[r + o]).hash();
                    break;
                default: // PTR, CHAN
                    h = h * 31 + Unsafe.hashPointer(a.refs[r + o]);
                    break;
            }
        }
        return h;
    }

    /** Go {@code ==} of the values at the two locations. */
    @ExplodeLoop
    public boolean eq(Flat a, int pa, int ra, int ba, Flat b, int pb, int rb, int bb) {
        for (int i = 0; i < kind.length; i++) {
            int o = off[i];
            int c = n[i];
            switch (kind[i]) {
                case PRIMS:
                    if (c == 1 ? a.prims[pa + o] != b.prims[pb + o]
                            : !Arrays.equals(a.prims, pa + o, pa + o + c, b.prims, pb + o, pb + o + c)) {
                        return false;
                    }
                    break;
                case BYTES:
                    if (c == 1 ? a.bytes[ba + o] != b.bytes[bb + o]
                            : !Arrays.equals(a.bytes, ba + o, ba + o + c, b.bytes, bb + o, bb + o + c)) {
                        return false;
                    }
                    break;
                case F64:
                    if (Double.longBitsToDouble(a.prims[pa + o]) != Double.longBitsToDouble(b.prims[pb + o])) {
                        return false;
                    }
                    break;
                case STR:
                    if (!Str.eq((Str) a.refs[ra + o], (Str) b.refs[rb + o])) {
                        return false;
                    }
                    break;
                case PTR:
                    if (!Ptr.same(a.refs[ra + o], b.refs[rb + o])) {
                        return false;
                    }
                    break;
                default: // CHAN
                    if (a.refs[ra + o] != b.refs[rb + o]) {
                        return false;
                    }
                    break;
            }
        }
        return true;
    }

    /** The number of entries (tests). */
    int entries() {
        return kind.length;
    }
}
