package graalgo.go.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.profiles.BranchProfile;
import graalgo.go.nodes.N.Expr;
import graalgo.go.nodes.N.GoNode;
import graalgo.go.nodes.N.I64;
import graalgo.go.nodes.N.Ref;
import graalgo.go.nodes.N.Stmt;
import com.oracle.truffle.api.CompilerDirectives;
import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import graalgo.go.runtime.Panics;
import graalgo.go.runtime.Unsafe;
import graalgo.go.runtime.Values;
import graalgo.go.types.GoType;
import graalgo.go.values.Flat;
import graalgo.go.values.GoMap;
import graalgo.go.values.Ptr;
import graalgo.go.values.Slice;
import graalgo.go.values.View;

/**
 * Memory locations of the flat value model (SPEC §3.4, GG-0072/0062): a location is an object
 * plus one offset per region — scalar slots, reference slots, byte-sized leaves (ADR-0015
 * decision 3). A {@link Loc} node evaluates its operand
 * expressions exactly once in {@link Loc#resolve}, returns the object and makes the offsets
 * available through {@link Loc#prim}/{@link Loc#ref}/{@link Loc#byt}: constants for a field path
 * into a known object, otherwise three {@code int} scratch slots of the frame (no allocation in the
 * interpreter; virtual after partial evaluation). The load, store and address-of nodes are
 * shared by every location shape, and the field offsets of a path are folded into the innermost
 * location by the factory, so {@code p.a.b[i].c} is one pointer location, one index location
 * and one load.
 *
 * <p>The region of a load or store is chosen by the factory from the static type of the leaf
 * ({@link LoadI64}/{@link LoadU8}/{@link LoadI8}/{@link LoadRef}); nothing here tests a shape.
 *
 * <p>The one dynamic property is the pointer of a dereference ({@link PtrLoc}): whole object,
 * interior pointer, or — the third shape, GG-0062 / ADR-0015 decision 1 — a view pointer that
 * a checked conversion from {@code unsafe.Pointer} produced because the pointee's leaves are not
 * the referent's. Then {@link Loc#resolve} returns null and the consumer goes by gc byte offset
 * through the referent's leaf table ({@link Unsafe}), behind a branch typed code never enters.
 */
public final class Mem {
    private Mem() {}

    public abstract static class Loc extends GoNode {
        public abstract Flat resolve(VirtualFrame frame);

        /** Valid after {@link #resolve} and before any other location of the frame resolves. */
        public abstract int prim(VirtualFrame frame);

        public abstract int ref(VirtualFrame frame);

        public abstract int byt(VirtualFrame frame);

        /** Region masks for {@link #need}. */
        public static final int P = 1, R = 2, B = 4, ALL = 7;

        /**
         * Tells the location which offsets its one consumer reads (called by the consumer's
         * constructor, before the first execution): a scalar load reads one. The others are
         * then neither computed nor written to the frame — the scratch slots are real stores
         * wherever the frame is not virtual (interpreter, OSR-compiled loops).
         */
        public void need(int regions) {}

        /**
         * The pointer to the location ({@code &x}), {@code type} being the location's type.
         * Shapes that have the three offsets at hand override this to build the pointer
         * without the scratch slots.
         */
        public Object addr(VirtualFrame frame, GoType type) {
            Flat o = resolve(frame);
            if (o == null) {
                return Unsafe.typed(this, viewObj(frame), viewOff(frame), type, null);
            }
            return Ptr.make(o, prim(frame), ref(frame), byt(frame));
        }

        /**
         * After {@link #resolve} returned null: the location is reached through a view pointer
         * (GG-0062, ADR-0015 decision 1) and is byte {@link #viewOff} of this object. Only
         * locations behind a pointer can be; the consumers take their slow path through
         * {@link Unsafe}.
         */
        public Flat viewObj(VirtualFrame frame) {
            throw CompilerDirectives.shouldNotReachHere("not a view location");
        }

        public long viewOff(VirtualFrame frame) {
            throw CompilerDirectives.shouldNotReachHere("not a view location");
        }

        /** The regions a value of type {@code t} occupies. */
        public static int regionsOf(GoType t) {
            return (t.nPrim() > 0 ? P : 0) | (t.nRef() > 0 ? R : 0) | (t.nByte() > 0 ? B : 0);
        }
    }

    /**
     * The frame's scratch slots of a dynamic location: the three slot offsets, and for a
     * location reached through a view the referent and the gc byte offset.
     */
    public record Aux(int p, int r, int b, int v, int g) {}

    /** A location whose offsets live in the frame's scratch slots. */
    abstract static class DynLoc extends Loc {
        final int auxP;
        final int auxR;
        final int auxB;
        final int auxV;
        final int auxG;
        @com.oracle.truffle.api.CompilerDirectives.CompilationFinal int need = ALL;

        @Override
        public final Flat viewObj(VirtualFrame frame) {
            return (Flat) frame.getObjectStatic(auxV);
        }

        @Override
        public final long viewOff(VirtualFrame frame) {
            return frame.getLongStatic(auxG);
        }

        /** Enters view mode: {@link #resolve} returns the null this returns. */
        final Flat view(VirtualFrame frame, Flat obj, long off) {
            frame.setObjectStatic(auxV, obj);
            frame.setLongStatic(auxG, off);
            return null;
        }

        @Override
        public void need(int regions) {
            need = regions;
        }

        DynLoc(Aux aux) {
            this.auxP = aux.p();
            this.auxR = aux.r();
            this.auxB = aux.b();
            this.auxV = aux.v();
            this.auxG = aux.g();
        }

        @Override
        public final int byt(VirtualFrame frame) {
            return frame.getIntStatic(auxB);
        }

        final void set(VirtualFrame frame, int p, int r, int b) {
            if ((need & P) != 0) {
                frame.setIntStatic(auxP, p);
            }
            if ((need & R) != 0) {
                frame.setIntStatic(auxR, r);
            }
            if ((need & B) != 0) {
                frame.setIntStatic(auxB, b);
            }
        }

        @Override
        public final int prim(VirtualFrame frame) {
            return frame.getIntStatic(auxP);
        }

        @Override
        public final int ref(VirtualFrame frame) {
            return frame.getIntStatic(auxR);
        }
    }

    /** A field path into the object an expression yields (struct local, variable box, globals). */
    public static final class FlatLoc extends Loc {
        @Child private Ref base;
        private final int p;
        private final int r;
        private final int b;

        public FlatLoc(Ref base, int p, int r, int b) {
            this.base = base;
            this.p = p;
            this.r = r;
            this.b = b;
        }

        @Override
        public int byt(VirtualFrame frame) {
            return b;
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
        private final int b;
        /** The static pointee type: its leaf table turns the field path into a byte offset for views. */
        private final GoType pointee;
        private final BranchProfile interior = BranchProfile.create();
        private final BranchProfile viewed = BranchProfile.create();
        private final BranchProfile nil = BranchProfile.create();

        public PtrLoc(Ref ptr, GoType pointee, int p, int r, int b, Aux aux) {
            super(aux);
            this.ptr = ptr;
            this.pointee = pointee;
            this.p = p;
            this.r = r;
            this.b = b;
        }

        /** The third shape: a view pointer, or the fatal error for a word that is no pointer. */
        private Flat resolveView(VirtualFrame frame, Object o) {
            if (o instanceof View v) {
                viewed.enter();
                return view(frame, v.obj(), v.off() + pathOff());
            }
            nil.enter();
            throw o == null ? Panics.nilDeref(this) : Panics.fatal(this, Unsafe.INVALID);
        }

        @TruffleBoundary
        private long pathOff() {
            return pointee.gcOffOfLeaf(p + r + b);
        }

        @Override
        public Flat resolve(VirtualFrame frame) {
            Object o = ptr.executeGeneric(frame);
            if (o instanceof Flat flat) {
                set(frame, p, r, b);
                return flat;
            } else if (o instanceof Ptr q) {
                interior.enter();
                set(frame, q.prim() + p, q.ref() + r, q.byt() + b);
                return q.obj();
            }
            return resolveView(frame, o);
        }

        @Override
        public Object addr(VirtualFrame frame, GoType type) {
            Object o = ptr.executeGeneric(frame);
            if (o instanceof Flat flat) {
                return Ptr.make(flat, p, r, b);
            } else if (o instanceof Ptr q) {
                interior.enter();
                return Ptr.make(q.obj(), q.prim() + p, q.ref() + r, q.byt() + b);
            }
            resolveView(frame, o);
            return Unsafe.typed(this, viewObj(frame), viewOff(frame), type, null);
        }
    }

    /** {@code slice[index]} plus a field path into the element. */
    public static final class SliceLoc extends DynLoc {
        @Child private Ref slice;
        @Child private I64 index;
        private final int np;
        private final int nr;
        private final int nb;
        private final int p;
        private final int r;
        private final int b;
        private final BranchProfile outOfRange = BranchProfile.create();
        private final boolean signed;

        /** {@code signed}: whether the index's type is signed (the panic's text). */
        public SliceLoc(Ref slice, I64 index, GoType elem, int p, int r, int b, Aux aux, boolean signed) {
            super(aux);
            this.signed = signed;
            this.slice = slice;
            this.index = index;
            this.np = elem.nPrim();
            this.nr = elem.nRef();
            this.nb = elem.nByte();
            this.p = p;
            this.r = r;
            this.b = b;
        }

        @Override
        public Flat resolve(VirtualFrame frame) {
            Slice s = (Slice) slice.executeGeneric(frame);
            long i = index.executeLong(frame);
            if (Long.compareUnsigned(i, s.len()) >= 0) {
                outOfRange.enter();
                throw Panics.index(this, i, s.len(), signed);
            }
            set(frame, s.prim() + (int) i * np + p, s.ref() + (int) i * nr + r, s.byt() + (int) i * nb + b);
            return s.base();
        }

        @Override
        public Object addr(VirtualFrame frame, GoType type) {
            Slice s = (Slice) slice.executeGeneric(frame);
            long i = index.executeLong(frame);
            if (Long.compareUnsigned(i, s.len()) >= 0) {
                outOfRange.enter();
                throw Panics.index(this, i, s.len(), signed);
            }
            return Ptr.make(s.base(), s.prim() + (int) i * np + p, s.ref() + (int) i * nr + r, s.byt() + (int) i * nb + b);
        }
    }

    /** {@code array[index]} inside the parent location, plus a field path into the element. */
    public static final class IndexLoc extends DynLoc {
        @Child private Loc parent;
        @Child private I64 index;
        private final long len;
        private final int np;
        private final int nr;
        private final int nb;
        private final int p;
        private final int r;
        private final int b;
        private final GoType elem;
        private final BranchProfile outOfRange = BranchProfile.create();
        private final BranchProfile viewed = BranchProfile.create();
        private final boolean signed;

        @TruffleBoundary
        private long viewOffOf(long i) {
            return i * elem.gcSize() + elem.gcOffOfLeaf(p + r + b);
        }

        /** {@code signed}: whether the index's type is signed (the panic's text). */
        public IndexLoc(Loc parent, I64 index, long len, GoType elem, int p, int r, int b, Aux aux, boolean signed) {
            super(aux);
            this.elem = elem;
            this.signed = signed;
            this.parent = parent;
            this.index = index;
            this.len = len;
            this.np = elem.nPrim();
            this.nr = elem.nRef();
            this.nb = elem.nByte();
            this.p = p;
            this.r = r;
            this.b = b;
        }

        @Override
        public void need(int regions) {
            super.need(regions);
            parent.need(regions);
        }

        @Override
        public Flat resolve(VirtualFrame frame) {
            Flat o = parent.resolve(frame);
            if (o == null) {
                // The array is reached through a view: the element is a byte offset further on.
                viewed.enter();
                Flat vo = parent.viewObj(frame);
                long vg = parent.viewOff(frame);
                long vi = index.executeLong(frame);
                if (Long.compareUnsigned(vi, len) >= 0) {
                    outOfRange.enter();
                    throw Panics.index(this, vi, len, signed);
                }
                return view(frame, vo, vg + viewOffOf(vi));
            }
            int bp = (need & P) != 0 ? parent.prim(frame) : 0;
            int br = (need & R) != 0 ? parent.ref(frame) : 0;
            int bb = (need & B) != 0 ? parent.byt(frame) : 0;
            long i = index.executeLong(frame);
            if (Long.compareUnsigned(i, len) >= 0) {
                outOfRange.enter();
                throw Panics.index(this, i, len, signed);
            }
            set(frame, bp + (int) i * np + p, br + (int) i * nr + r, bb + (int) i * nb + b);
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
        private final int b;

        public MapGetI(Ref map, I64 key, Flat zero, int p, int r, int b, Aux aux) {
            super(aux);
            this.map = map;
            this.key = key;
            this.zero = zero;
            this.p = p;
            this.r = r;
            this.b = b;
        }

        @Override
        public Flat resolve(VirtualFrame frame) {
            GoMap m = (GoMap) map.executeGeneric(frame);
            long k = key.executeLong(frame);
            int e = m == null ? -1 : m.find(k);
            if (e < 0) {
                set(frame, p, r, b);
                return zero;
            }
            set(frame, e * m.vp + p, e * m.vr + r, e * m.vb + b);
            return m.vals;
        }
    }

    /** As {@link MapGetI} for every other key kind: hashing and {@code ==} by the key type's {@link MapKeys.KeyOps}. */
    public static final class MapGetG extends DynLoc {
        @Child private Ref map;
        @Child private Expr key;
        @Child private MapKeys.KeyOps ops;
        private final Flat zero;
        private final int p;
        private final int r;
        private final int b;

        private final GoType keyType;
        private final boolean checkKey;

        public MapGetG(Ref map, Expr key, GoType keyType, Flat zero, int p, int r, int b, Aux aux) {
            super(aux);
            this.keyType = keyType;
            this.checkKey = Values.hashMightPanic(keyType);
            this.ops = MapKeys.create(keyType);
            this.map = map;
            this.key = key;
            this.zero = zero;
            this.p = p;
            this.r = r;
            this.b = b;
        }

        @Override
        public Flat resolve(VirtualFrame frame) {
            GoMap m = (GoMap) map.executeGeneric(frame);
            Object k = key.executeGeneric(frame);
            if (checkKey && (m == null || m.len() == 0)) {
                Panics.checkMapKey(this, keyType, k);
            }
            int e = m == null ? -1 : find(m, k);
            if (e < 0) {
                set(frame, p, r, b);
                return zero;
            }
            set(frame, e * m.vp + p, e * m.vr + r, e * m.vb + b);
            return m.vals;
        }

        private int find(GoMap m, Object k) {
            try {
                return m.findRaw(ops.hash(k), k, ops);
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

        public MapPutI(Ref map, I64 key, Aux aux) {
            super(aux);
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
            set(frame, e * m.vp, e * m.vr, e * m.vb);
            return m.vals;
        }
    }

    /** As {@link MapPutI} for every other key kind; the key expression yields a private value. */
    public static final class MapPutG extends DynLoc {
        @Child private Ref map;
        @Child private Expr key;
        @Child private MapKeys.KeyOps ops;
        private final BranchProfile nil = BranchProfile.create();

        public MapPutG(Ref map, Expr key, GoType keyType, Aux aux) {
            super(aux);
            this.map = map;
            this.key = key;
            this.ops = MapKeys.create(keyType);
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
                e = m.putRaw(ops.hash(k), k, ops);
            } catch (Values.Uncomparable u) {
                throw Panics.unhashable(this, u.type);
            }
            set(frame, e * m.vp, e * m.vr, e * m.vb);
            return m.vals;
        }
    }

    /** The value slots of map entry {@code entry} (range loops). */
    public static final class MapEntryLoc extends DynLoc {
        @Child private Ref map;
        @Child private I64 entry;

        public MapEntryLoc(Ref map, I64 entry, Aux aux) {
            super(aux);
            this.map = map;
            this.entry = entry;
        }

        @Override
        public Flat resolve(VirtualFrame frame) {
            GoMap m = (GoMap) map.executeGeneric(frame);
            int e = (int) entry.executeLong(frame);
            set(frame, e * m.vp, e * m.vr, e * m.vb);
            return m.vals;
        }
    }

    // ---------------------------------------------------------------- loads and stores

    /** A scalar slot, raw: an integer as is; float and bool through {@code RawF64}/{@code RawBool}. */
    public static final class LoadI64 extends I64 {
        @Child private Loc loc;

        private final GoType type;
        private final BranchProfile viewed = BranchProfile.create();

        public LoadI64(Loc loc, GoType type) {
            this.loc = loc;
            this.type = type;
            loc.need(Loc.P);
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            Flat o = loc.resolve(frame);
            if (o == null) {
                viewed.enter();
                return Unsafe.loadScalar(this, loc.viewObj(frame), loc.viewOff(frame), type);
            }
            return o.prims[loc.prim(frame)];
        }
    }

    /** A byte-sized leaf, zero-extended ({@code uint8}; {@code bool} through {@code RawBool}). */
    public static final class LoadU8 extends I64 {
        @Child private Loc loc;

        private final GoType type;
        private final BranchProfile viewed = BranchProfile.create();

        public LoadU8(Loc loc, GoType type) {
            this.loc = loc;
            this.type = type;
            loc.need(Loc.B);
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            Flat o = loc.resolve(frame);
            if (o == null) {
                viewed.enter();
                return Unsafe.loadScalar(this, loc.viewObj(frame), loc.viewOff(frame), type);
            }
            return o.bytes[loc.byt(frame)] & 0xFFL;
        }
    }

    /** A byte-sized leaf, sign-extended ({@code int8}). */
    public static final class LoadI8 extends I64 {
        @Child private Loc loc;

        private final GoType type;
        private final BranchProfile viewed = BranchProfile.create();

        public LoadI8(Loc loc, GoType type) {
            this.loc = loc;
            this.type = type;
            loc.need(Loc.B);
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            Flat o = loc.resolve(frame);
            if (o == null) {
                viewed.enter();
                return Unsafe.loadScalar(this, loc.viewObj(frame), loc.viewOff(frame), type);
            }
            return o.bytes[loc.byt(frame)];
        }
    }

    public static final class LoadRef extends Ref {
        @Child private Loc loc;

        private final GoType type;
        private final BranchProfile viewed = BranchProfile.create();

        public LoadRef(Loc loc, GoType type) {
            this.loc = loc;
            this.type = type;
            loc.need(Loc.R);
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            Flat o = loc.resolve(frame);
            if (o == null) {
                viewed.enter();
                return Unsafe.loadRef(this, loc.viewObj(frame), loc.viewOff(frame), type);
            }
            return o.refs[loc.ref(frame)];
        }
    }

    /** A struct or array value copied out of its enclosing object (allocates the copy). */
    public static final class LoadSub extends Ref {
        @Child private Loc loc;
        private final GoType type;
        private final int np;
        private final int nr;
        private final int nb;

        public LoadSub(Loc loc, GoType type) {
            this.loc = loc;
            this.type = type;
            this.np = type.nPrim();
            this.nr = type.nRef();
            this.nb = type.nByte();
            loc.need(Loc.regionsOf(type));
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            Flat o = loc.resolve(frame);
            if (o == null) {
                return Unsafe.loadFlat(this, loc.viewObj(frame), loc.viewOff(frame), type);
            }
            return o.subFixed(type, np == 0 ? 0 : loc.prim(frame), np, nr == 0 ? 0 : loc.ref(frame), nr,
                    nb == 0 ? 0 : loc.byt(frame), nb);
        }
    }

    /** Stores a raw scalar. The value is evaluated first: the factory spills impure operands. */
    public static final class StoreI64 extends Stmt {
        @Child private Loc loc;
        @Child private I64 value;

        private final GoType type;
        private final BranchProfile viewed = BranchProfile.create();

        public StoreI64(Loc loc, GoType type, I64 value) {
            this.loc = loc;
            this.type = type;
            this.value = value;
            loc.need(Loc.P);
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            long v = value.executeLong(frame);
            Flat o = loc.resolve(frame);
            if (o == null) {
                viewed.enter();
                Unsafe.storeScalar(this, loc.viewObj(frame), loc.viewOff(frame), type, v);
                return;
            }
            o.prims[loc.prim(frame)] = v;
        }
    }

    /** Stores a byte-sized leaf: the low byte of the raw value (canonical forms truncate to it). */
    public static final class StoreByte extends Stmt {
        @Child private Loc loc;
        @Child private I64 value;

        private final GoType type;
        private final BranchProfile viewed = BranchProfile.create();

        public StoreByte(Loc loc, GoType type, I64 value) {
            this.loc = loc;
            this.type = type;
            this.value = value;
            loc.need(Loc.B);
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            long v = value.executeLong(frame);
            Flat o = loc.resolve(frame);
            if (o == null) {
                viewed.enter();
                Unsafe.storeScalar(this, loc.viewObj(frame), loc.viewOff(frame), type, v);
                return;
            }
            o.bytes[loc.byt(frame)] = (byte) v;
        }
    }

    public static final class StoreRef extends Stmt {
        @Child private Loc loc;
        @Child private Expr value;

        private final GoType type;
        private final BranchProfile viewed = BranchProfile.create();

        public StoreRef(Loc loc, GoType type, Expr value) {
            this.loc = loc;
            this.type = type;
            this.value = value;
            loc.need(Loc.R);
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            Object v = value.executeGeneric(frame);
            Flat o = loc.resolve(frame);
            if (o == null) {
                viewed.enter();
                Unsafe.storeRef(this, loc.viewObj(frame), loc.viewOff(frame), type, v);
                return;
            }
            o.refs[loc.ref(frame)] = v;
        }
    }

    /** Copies a struct or array value into its enclosing object. */
    public static final class StoreSub extends Stmt {
        @Child private Loc loc;
        @Child private Expr value;

        private final GoType type;
        private final int np;
        private final int nr;
        private final int nb;

        public StoreSub(Loc loc, GoType type, Expr value) {
            this.loc = loc;
            this.type = type;
            this.value = value;
            this.np = type.nPrim();
            this.nr = type.nRef();
            this.nb = type.nByte();
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            Flat v = (Flat) value.executeGeneric(frame);
            Flat o = loc.resolve(frame);
            if (o == null) {
                Unsafe.storeFlat(this, loc.viewObj(frame), loc.viewOff(frame), type, v);
                return;
            }
            o.storeFixed(loc.prim(frame), loc.ref(frame), loc.byt(frame), v, np, nr, nb);
        }
    }

    /** {@code &location}: allocates a {@link Ptr} unless the location is an object's start. */
    public static final class AddrOf extends Ref {
        @Child private Loc loc;

        private final GoType type;

        /** {@code type}: the type of the location (the pointee of the result). */
        public AddrOf(Loc loc, GoType type) {
            this.loc = loc;
            this.type = type;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return loc.addr(frame, type);
        }
    }

    /** A private copy of a struct or array value (Go value semantics at a store). */
    public static final class Copy extends Ref {
        @Child private Expr value;
        /**
         * The region sizes of the values seen (a static type fixes them), -1 before the first;
         * {@link #GENERIC} once two different shapes were seen.
         */
        @com.oracle.truffle.api.CompilerDirectives.CompilationFinal private int np = -1;
        @com.oracle.truffle.api.CompilerDirectives.CompilationFinal private int nr;
        @com.oracle.truffle.api.CompilerDirectives.CompilationFinal private int nb;
        private static final int GENERIC = -2;

        public Copy(Expr value) {
            this.value = value;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            Flat v = (Flat) value.executeGeneric(frame);
            if (np >= 0 && v.prims.length == np && v.refs.length == nr && v.bytes.length == nb) {
                return v.copyFixed(np, nr, nb);
            }
            if (np != GENERIC) {
                com.oracle.truffle.api.CompilerDirectives.transferToInterpreterAndInvalidate();
                if (np == -1) {
                    np = v.prims.length;
                    nr = v.refs.length;
                    nb = v.bytes.length;
                } else {
                    np = GENERIC;
                }
            }
            return v.copy();
        }
    }

    /** A fresh zero value of a struct/array type, or the box of an escaping variable. */
    public static final class Zero extends Ref {
        private final GoType type;
        private final int np;
        private final int nb;
        @com.oracle.truffle.api.CompilerDirectives.CompilationFinal(dimensions = 1) private final Object[] zeroRefs;

        public Zero(GoType type) {
            this.type = type;
            this.np = type.nPrim();
            this.nb = type.nByte();
            this.zeroRefs = type.zeroRefs();
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return new Flat(type, np == 0 ? Flat.NO_PRIMS : new long[np],
                    zeroRefs.length == 0 ? Flat.NO_REFS : zeroRefs.clone(), nb == 0 ? Flat.NO_BYTES : new byte[nb]);
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
