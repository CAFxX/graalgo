package graalgo.go.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.profiles.BranchProfile;
import graalgo.go.nodes.N.Expr;
import graalgo.go.nodes.N.GoNode;
import graalgo.go.nodes.N.I64;
import graalgo.go.nodes.N.Ref;
import graalgo.go.nodes.N.Stmt;
import graalgo.go.runtime.Panics;
import graalgo.go.runtime.Values;
import graalgo.go.types.GoType;
import graalgo.go.values.Flat;
import graalgo.go.values.GoMap;
import graalgo.go.values.Ptr;
import graalgo.go.values.Slice;

/**
 * Memory locations of the flat value model (SPEC §3.4, GG-0061/0062): a location is an object
 * plus a scalar-slot and a reference-slot offset. A {@link Loc} node evaluates its operand
 * expressions exactly once in {@link Loc#resolve}, returns the object and makes the two offsets
 * available through {@link Loc#prim}/{@link Loc#ref}: constants for a field path into a known
 * object, otherwise two {@code int} scratch slots of the frame (no allocation in the
 * interpreter; virtual after partial evaluation). The load, store and address-of nodes are
 * shared by every location shape, and the field offsets of a path are folded into the innermost
 * location by the factory, so {@code p.a.b[i].c} is one pointer location, one index location
 * and one load.
 *
 * <p>Toolexec work package W13 (the pointer model, milestone M1r, GG-0062) hooks in here: a view
 * pointer converted from {@code unsafe.Pointer} becomes one more {@link Ptr} shape whose offsets
 * come from the type's leaf table.
 */
public final class Mem {
    private Mem() {}

    public abstract static class Loc extends GoNode {
        public abstract Flat resolve(VirtualFrame frame);

        /** Valid after {@link #resolve} and before any other location of the frame resolves. */
        public abstract int prim(VirtualFrame frame);

        public abstract int ref(VirtualFrame frame);
    }

    /** A location whose offsets live in the frame's two scratch slots. */
    abstract static class DynLoc extends Loc {
        final int auxP;
        final int auxR;

        DynLoc(int auxP, int auxR) {
            this.auxP = auxP;
            this.auxR = auxR;
        }

        @Override
        public final int prim(VirtualFrame frame) {
            return frame.getInt(auxP);
        }

        @Override
        public final int ref(VirtualFrame frame) {
            return frame.getInt(auxR);
        }
    }

    /** A field path into the object an expression yields (struct local, variable box, globals). */
    public static final class FlatLoc extends Loc {
        @Child private Ref base;
        private final int p;
        private final int r;

        public FlatLoc(Ref base, int p, int r) {
            this.base = base;
            this.p = p;
            this.r = r;
        }

        @Override
        public Flat resolve(VirtualFrame frame) {
            return (Flat) base.executeGeneric(frame);
        }

        @Override
        public int prim(VirtualFrame frame) {
            return p;
        }

        @Override
        public int ref(VirtualFrame frame) {
            return r;
        }
    }

    /**
     * {@code *ptr} plus a field path. The pointee type is static, the pointer shape is not
     * (whole object, interior pointer, nil): the one dynamic property of a dereference
     * (truffle-design §4.4), tested on exact final classes.
     */
    public static final class PtrLoc extends DynLoc {
        @Child private Ref ptr;
        private final int p;
        private final int r;
        private final BranchProfile interior = BranchProfile.create();
        private final BranchProfile nil = BranchProfile.create();

        public PtrLoc(Ref ptr, int p, int r, int auxP, int auxR) {
            super(auxP, auxR);
            this.ptr = ptr;
            this.p = p;
            this.r = r;
        }

        @Override
        public Flat resolve(VirtualFrame frame) {
            Object o = ptr.executeGeneric(frame);
            if (o instanceof Flat flat) {
                frame.setInt(auxP, p);
                frame.setInt(auxR, r);
                return flat;
            } else if (o instanceof Ptr q) {
                interior.enter();
                frame.setInt(auxP, q.prim + p);
                frame.setInt(auxR, q.ref + r);
                return q.obj;
            }
            nil.enter();
            throw Panics.nilDeref(this);
        }
    }

    /** {@code slice[index]} plus a field path into the element. */
    public static final class SliceLoc extends DynLoc {
        @Child private Ref slice;
        @Child private I64 index;
        private final int np;
        private final int nr;
        private final int p;
        private final int r;
        private final BranchProfile outOfRange = BranchProfile.create();
        private final boolean signed;

        /** {@code signed}: whether the index's type is signed (the panic's text). */
        public SliceLoc(Ref slice, I64 index, int np, int nr, int p, int r, int auxP, int auxR, boolean signed) {
            super(auxP, auxR);
            this.signed = signed;
            this.slice = slice;
            this.index = index;
            this.np = np;
            this.nr = nr;
            this.p = p;
            this.r = r;
        }

        @Override
        public Flat resolve(VirtualFrame frame) {
            Slice s = (Slice) slice.executeGeneric(frame);
            long i = index.executeLong(frame);
            if (Long.compareUnsigned(i, s.len) >= 0) {
                outOfRange.enter();
                throw Panics.index(this, i, s.len, signed);
            }
            frame.setInt(auxP, s.prim + (int) i * np + p);
            frame.setInt(auxR, s.ref + (int) i * nr + r);
            return s.base;
        }
    }

    /** {@code array[index]} inside the parent location, plus a field path into the element. */
    public static final class IndexLoc extends DynLoc {
        @Child private Loc parent;
        @Child private I64 index;
        private final long len;
        private final int np;
        private final int nr;
        private final int p;
        private final int r;
        private final BranchProfile outOfRange = BranchProfile.create();
        private final boolean signed;

        /** {@code signed}: whether the index's type is signed (the panic's text). */
        public IndexLoc(Loc parent, I64 index, long len, int np, int nr, int p, int r, int auxP, int auxR,
                boolean signed) {
            super(auxP, auxR);
            this.signed = signed;
            this.parent = parent;
            this.index = index;
            this.len = len;
            this.np = np;
            this.nr = nr;
            this.p = p;
            this.r = r;
        }

        @Override
        public Flat resolve(VirtualFrame frame) {
            Flat o = parent.resolve(frame);
            int bp = parent.prim(frame);
            int br = parent.ref(frame);
            long i = index.executeLong(frame);
            if (Long.compareUnsigned(i, len) >= 0) {
                outOfRange.enter();
                throw Panics.index(this, i, len, signed);
            }
            frame.setInt(auxP, bp + (int) i * np + p);
            frame.setInt(auxR, br + (int) i * nr + r);
            return o;
        }
    }

    /**
     * {@code m[key]} as an rvalue for integer/boolean keys: the entry's value slots, or a
     * shared zero value (never written) when the key is absent or the map is nil.
     */
    public static final class MapGetI extends DynLoc {
        @Child private Ref map;
        @Child private I64 key;
        private final Flat zero;
        private final int p;
        private final int r;

        public MapGetI(Ref map, I64 key, Flat zero, int p, int r, int auxP, int auxR) {
            super(auxP, auxR);
            this.map = map;
            this.key = key;
            this.zero = zero;
            this.p = p;
            this.r = r;
        }

        @Override
        public Flat resolve(VirtualFrame frame) {
            GoMap m = (GoMap) map.executeGeneric(frame);
            long k = key.executeLong(frame);
            int e = m == null ? -1 : m.find(k);
            if (e < 0) {
                frame.setInt(auxP, p);
                frame.setInt(auxR, r);
                return zero;
            }
            frame.setInt(auxP, e * m.vp + p);
            frame.setInt(auxR, e * m.vr + r);
            return m.vals;
        }
    }

    /** As {@link MapGetI} for every other key kind (hashing and {@code ==} behind a boundary). */
    public static final class MapGetG extends DynLoc {
        @Child private Ref map;
        @Child private Expr key;
        private final Flat zero;
        private final int p;
        private final int r;

        public MapGetG(Ref map, Expr key, Flat zero, int p, int r, int auxP, int auxR) {
            super(auxP, auxR);
            this.map = map;
            this.key = key;
            this.zero = zero;
            this.p = p;
            this.r = r;
        }

        @Override
        public Flat resolve(VirtualFrame frame) {
            GoMap m = (GoMap) map.executeGeneric(frame);
            Object k = key.executeGeneric(frame);
            int e = m == null ? -1 : find(m, k);
            if (e < 0) {
                frame.setInt(auxP, p);
                frame.setInt(auxR, r);
                return zero;
            }
            frame.setInt(auxP, e * m.vp + p);
            frame.setInt(auxR, e * m.vr + r);
            return m.vals;
        }

        private int find(GoMap m, Object k) {
            try {
                return m.find(k);
            } catch (Values.Uncomparable u) {
                throw Panics.unhashable(this, u.type);
            }
        }
    }

    /** {@code m[key]} as an assignment target, integer/boolean keys: inserts the entry. */
    public static final class MapPutI extends DynLoc {
        @Child private Ref map;
        @Child private I64 key;
        private final BranchProfile nil = BranchProfile.create();

        public MapPutI(Ref map, I64 key, int auxP, int auxR) {
            super(auxP, auxR);
            this.map = map;
            this.key = key;
        }

        @Override
        public Flat resolve(VirtualFrame frame) {
            GoMap m = (GoMap) map.executeGeneric(frame);
            long k = key.executeLong(frame);
            if (m == null) {
                nil.enter();
                throw Panics.nilMap(this);
            }
            int e = m.put(k);
            frame.setInt(auxP, e * m.vp);
            frame.setInt(auxR, e * m.vr);
            return m.vals;
        }
    }

    /** As {@link MapPutI} for every other key kind; the key expression yields a private value. */
    public static final class MapPutG extends DynLoc {
        @Child private Ref map;
        @Child private Expr key;
        private final BranchProfile nil = BranchProfile.create();

        public MapPutG(Ref map, Expr key, int auxP, int auxR) {
            super(auxP, auxR);
            this.map = map;
            this.key = key;
        }

        @Override
        public Flat resolve(VirtualFrame frame) {
            GoMap m = (GoMap) map.executeGeneric(frame);
            Object k = key.executeGeneric(frame);
            if (m == null) {
                nil.enter();
                throw Panics.nilMap(this);
            }
            int e;
            try {
                e = m.put(k);
            } catch (Values.Uncomparable u) {
                throw Panics.unhashable(this, u.type);
            }
            frame.setInt(auxP, e * m.vp);
            frame.setInt(auxR, e * m.vr);
            return m.vals;
        }
    }

    /** The value slots of map entry {@code entry} (range loops). */
    public static final class MapEntryLoc extends DynLoc {
        @Child private Ref map;
        @Child private I64 entry;

        public MapEntryLoc(Ref map, I64 entry, int auxP, int auxR) {
            super(auxP, auxR);
            this.map = map;
            this.entry = entry;
        }

        @Override
        public Flat resolve(VirtualFrame frame) {
            GoMap m = (GoMap) map.executeGeneric(frame);
            int e = (int) entry.executeLong(frame);
            frame.setInt(auxP, e * m.vp);
            frame.setInt(auxR, e * m.vr);
            return m.vals;
        }
    }

    // ---------------------------------------------------------------- loads and stores

    /** A scalar slot, raw: an integer as is; float and bool through {@code RawF64}/{@code RawBool}. */
    public static final class LoadI64 extends I64 {
        @Child private Loc loc;

        public LoadI64(Loc loc) {
            this.loc = loc;
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            Flat o = loc.resolve(frame);
            return o.prims[loc.prim(frame)];
        }
    }

    public static final class LoadRef extends Ref {
        @Child private Loc loc;

        public LoadRef(Loc loc) {
            this.loc = loc;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            Flat o = loc.resolve(frame);
            return o.refs[loc.ref(frame)];
        }
    }

    /** A struct or array value copied out of its enclosing object (allocates the copy). */
    public static final class LoadSub extends Ref {
        @Child private Loc loc;
        private final GoType type;
        private final int np;
        private final int nr;

        public LoadSub(Loc loc, GoType type) {
            this.loc = loc;
            this.type = type;
            this.np = type.nPrim();
            this.nr = type.nRef();
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            Flat o = loc.resolve(frame);
            return o.sub(type, loc.prim(frame), np, loc.ref(frame), nr);
        }
    }

    /** Stores a raw scalar. The value is evaluated first: the factory spills impure operands. */
    public static final class StoreI64 extends Stmt {
        @Child private Loc loc;
        @Child private I64 value;

        public StoreI64(Loc loc, I64 value) {
            this.loc = loc;
            this.value = value;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            long v = value.executeLong(frame);
            Flat o = loc.resolve(frame);
            o.prims[loc.prim(frame)] = v;
        }
    }

    public static final class StoreRef extends Stmt {
        @Child private Loc loc;
        @Child private Expr value;

        public StoreRef(Loc loc, Expr value) {
            this.loc = loc;
            this.value = value;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            Object v = value.executeGeneric(frame);
            Flat o = loc.resolve(frame);
            o.refs[loc.ref(frame)] = v;
        }
    }

    /** Copies a struct or array value into its enclosing object. */
    public static final class StoreSub extends Stmt {
        @Child private Loc loc;
        @Child private Expr value;

        public StoreSub(Loc loc, Expr value) {
            this.loc = loc;
            this.value = value;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            Flat v = (Flat) value.executeGeneric(frame);
            Flat o = loc.resolve(frame);
            o.store(loc.prim(frame), loc.ref(frame), v);
        }
    }

    /** {@code &location}: allocates a {@link Ptr} unless the location is an object's start. */
    public static final class AddrOf extends Ref {
        @Child private Loc loc;

        public AddrOf(Loc loc) {
            this.loc = loc;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            Flat o = loc.resolve(frame);
            return Ptr.make(o, loc.prim(frame), loc.ref(frame));
        }
    }

    /** A private copy of a struct or array value (Go value semantics at a store). */
    public static final class Copy extends Ref {
        @Child private Expr value;

        public Copy(Expr value) {
            this.value = value;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return ((Flat) value.executeGeneric(frame)).copy();
        }
    }

    /** A fresh zero value of a struct/array type, or the box of an escaping variable. */
    public static final class Zero extends Ref {
        private final GoType type;
        private final int np;
        @com.oracle.truffle.api.CompilerDirectives.CompilationFinal(dimensions = 1) private final Object[] zeroRefs;

        public Zero(GoType type) {
            this.type = type;
            this.np = type.nPrim();
            this.zeroRefs = type.zeroRefs();
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return new Flat(type, np == 0 ? Flat.NO_PRIMS : new long[np],
                    zeroRefs.length == 0 ? Flat.NO_REFS : zeroRefs.clone());
        }
    }

    /** The globals object of a package in the current context (truffle-design §2.4). */
    public static final class Globals extends Ref {
        private final int pkg;

        public Globals(int pkg) {
            this.pkg = pkg;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return graalgo.language.GoContext.get(this).process().globals[pkg];
        }
    }
}
