package graalgo.go.nodes;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.dsl.Cached;
import com.oracle.truffle.api.dsl.GenerateInline;
import com.oracle.truffle.api.dsl.ImportStatic;
import com.oracle.truffle.api.dsl.Introspectable;
import com.oracle.truffle.api.dsl.NeverDefault;
import com.oracle.truffle.api.dsl.ReportPolymorphism;
import com.oracle.truffle.api.dsl.Specialization;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.runtime.KeyPlan;
import graalgo.go.runtime.Unsafe;
import graalgo.go.runtime.Values;
import graalgo.go.types.GoType;
import graalgo.go.values.Flat;
import graalgo.go.values.GoMap;
import graalgo.go.values.Iface;
import graalgo.go.values.Ptr;
import graalgo.go.values.Str;

/**
 * Go {@code ==} and the map hash as PE-visible nodes (truffle-audit F-6.3/F-7.1, WP 4): one
 * {@link KeyOps} per static type, chosen by kind when the site is built — strings (the hash
 * cached in the {@link Str}), boxed scalars, pointers, channels, struct/array types with a
 * {@link KeyPlan}, interfaces (an inline cache on the dynamic type, limit 3, each entry
 * holding the key node of that type) — and the generic {@link Values} code behind a boundary
 * for everything else. Every node computes exactly {@link Values#hash} and {@link Values#equal}
 * of its type (KeyOpsTest checks it), so a map can be probed by a site node and by reflection
 * alike, and every panic (an unhashable dynamic type) is the generic path's.
 */
public final class MapKeys {
    private MapKeys() {}

    /** The hash and {@code ==} of values of one static type, in carrier form. */
    public abstract static class KeyOps extends Node implements GoMap.KeyEq {
        /** {@code Values.hash(type, k)}; throws {@link Values.Uncomparable} as it does. */
        public abstract int hash(Object k);

        /** {@code Values.equal(type, a, b)}; throws {@link Values.Uncomparable} as it does. */
        @Override
        public abstract boolean eq(Object a, Object b);
    }

    /** The key node of static type {@code t}. */
    @NeverDefault
    public static KeyOps create(GoType t) {
        GoType u = t.u();
        switch (u.kind) {
            case GoType.STRING:
                return new StrOps();
            case GoType.INTERFACE:
                return new IfaceOps();
            case GoType.POINTER, GoType.UNSAFE_POINTER:
                return new PtrOps();
            case GoType.CHAN:
                return new ChanOps();
            case GoType.STRUCT, GoType.ARRAY: {
                KeyPlan plan = KeyPlan.of(u);
                return plan != null ? new PlanOps(plan) : new GenericOps(t);
            }
            default:
                break;
        }
        return switch (u.carrier()) {
            case GoType.C_I64 -> new LongOps();
            case GoType.C_BOOL -> new BoolOps();
            case GoType.C_F64 -> new F64Ops();
            default -> new GenericOps(t);
        };
    }

    static final class StrOps extends KeyOps {
        @Override
        public int hash(Object k) {
            return ((Str) k).hash();
        }

        @Override
        public boolean eq(Object a, Object b) {
            return Str.eq((Str) a, (Str) b);
        }
    }

    /** Integer kinds boxed (an interface's data; integer map keys are stored unboxed). */
    static final class LongOps extends KeyOps {
        @Override
        public int hash(Object k) {
            return Long.hashCode((Long) k);
        }

        @Override
        public boolean eq(Object a, Object b) {
            return (long) (Long) a == (long) (Long) b;
        }
    }

    static final class BoolOps extends KeyOps {
        @Override
        public int hash(Object k) {
            return Boolean.hashCode((Boolean) k);
        }

        @Override
        public boolean eq(Object a, Object b) {
            return (boolean) (Boolean) a == (boolean) (Boolean) b;
        }
    }

    /** Floats: NaN != NaN, +0 == -0 (both hash 0). */
    static final class F64Ops extends KeyOps {
        @Override
        public int hash(Object k) {
            double d = (Double) k;
            return d == 0 ? 0 : Double.hashCode(d);
        }

        @Override
        public boolean eq(Object a, Object b) {
            return (double) (Double) a == (double) (Double) b;
        }
    }

    static final class PtrOps extends KeyOps {
        @Override
        public int hash(Object k) {
            return Unsafe.hashPointer(k);
        }

        @Override
        public boolean eq(Object a, Object b) {
            return Ptr.same(a, b);
        }
    }

    static final class ChanOps extends KeyOps {
        @Override
        public int hash(Object k) {
            return Unsafe.hashPointer(k);
        }

        @Override
        public boolean eq(Object a, Object b) {
            return a == b;
        }
    }

    /** A struct/array type with a {@link KeyPlan}: the leaves compared and hashed straight-line. */
    static final class PlanOps extends KeyOps {
        private final KeyPlan plan;

        PlanOps(KeyPlan plan) {
            this.plan = plan;
        }

        @Override
        public int hash(Object k) {
            return plan.hash((Flat) k, 0, 0, 0);
        }

        @Override
        public boolean eq(Object a, Object b) {
            return plan.eq((Flat) a, 0, 0, 0, (Flat) b, 0, 0, 0);
        }
    }

    /** Interface values: the dynamic-type caches of {@link IfaceHash} and {@link IfaceEq}. */
    static final class IfaceOps extends KeyOps {
        @Child private IfaceHash hash = MapKeysFactory.IfaceHashNodeGen.create();
        @Child private IfaceEq eq = MapKeysFactory.IfaceEqNodeGen.create();

        @Override
        public int hash(Object k) {
            return hash.execute(k);
        }

        @Override
        public boolean eq(Object a, Object b) {
            return eq.execute(a, b);
        }
    }

    /** Every other type (complex, structs with interface or complex leaves): the generic code. */
    static final class GenericOps extends KeyOps {
        private final GoType type;

        GenericOps(GoType type) {
            this.type = type;
        }

        @Override
        @TruffleBoundary
        public int hash(Object k) {
            return Values.hash(type, k);
        }

        @Override
        @TruffleBoundary
        public boolean eq(Object a, Object b) {
            return Values.equal(type, a, b);
        }
    }

    /**
     * The hash of an interface value ({@code Values.ifaceHash}): an inline cache on the dynamic
     * type (limit 3) whose entry folds the type's key hash and holds that type's key node; a
     * megamorphic site takes the generic code.
     */
    @Introspectable
    @ReportPolymorphism
    @GenerateInline(false)
    @ImportStatic(MapKeys.class)
    public abstract static class IfaceHash extends Node {
        public abstract int execute(Object k);

        @Specialization(guards = "k == null")
        static int nil(@SuppressWarnings("unused") Object k) {
            return 0;
        }

        @Specialization(guards = "k.type == cachedType", limit = "3")
        static int cached(Iface k,
                @Cached("k.type") GoType cachedType,
                @Cached("typeHash(cachedType)") int typeHash,
                @Cached("create(cachedType)") KeyOps ops) {
            try {
                return typeHash * 31 + ops.hash(k.data);
            } catch (Values.Uncomparable u) {
                throw Values.atInterface(u, cachedType);
            }
        }

        @Specialization(replaces = "cached")
        static int generic(Iface k) {
            return Values.ifaceHash(k);
        }

        static int typeHash(GoType t) {
            return t.key.hashCode();
        }
    }

    /**
     * Go {@code ==} on interface values ({@code Values.ifaceEqual}): an inline cache on the
     * dynamic type of the left operand (limit 3) holding that type's key node; a different
     * right type is unequal without touching the data; a megamorphic site takes the generic code.
     */
    @Introspectable
    @ReportPolymorphism
    @GenerateInline(false)
    @ImportStatic(MapKeys.class)
    public abstract static class IfaceEq extends Node {
        public abstract boolean execute(Object a, Object b);

        @Specialization(guards = "a == null")
        static boolean nil(@SuppressWarnings("unused") Object a, Object b) {
            return b == null;
        }

        @Specialization(guards = "a.type == cachedType", limit = "3")
        static boolean cached(Iface a, Object b,
                @Cached("a.type") GoType cachedType,
                @Cached("create(cachedType)") KeyOps ops) {
            if (!(b instanceof Iface y) || y.type != cachedType) {
                return false;
            }
            try {
                return ops.eq(a.data, y.data);
            } catch (Values.Uncomparable u) {
                throw Values.atInterface(u, cachedType);
            }
        }

        @Specialization(replaces = "cached")
        static boolean generic(Iface a, Object b) {
            return Values.ifaceEqual(a, b);
        }
    }
}
