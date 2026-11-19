package graalgo.go.nodes;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.dsl.Cached;
import com.oracle.truffle.api.dsl.GenerateInline;
import com.oracle.truffle.api.dsl.Specialization;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.profiles.BranchProfile;
import graalgo.go.nodes.N.Bool;
import graalgo.go.nodes.N.Expr;
import graalgo.go.nodes.N.GoNode;
import graalgo.go.nodes.N.Ref;
import graalgo.go.runtime.Panics;
import graalgo.go.types.GoType;
import graalgo.go.values.Iface;

/**
 * Interface conversion, type assertion and the tests a type switch is lowered to
 * (truffle-design §4.6 c). An interface value carries its dynamic type, so interface-to-interface
 * conversion is the identity and assertion to a concrete type is one identity comparison against
 * a constant descriptor; assertion to an interface type caches dynamic type → implements.
 */
public final class Ifaces {
    private Ifaces() {}

    /** Value of a concrete type → interface. Allocates the interface value. */
    public static final class ToIface extends Ref {
        private final GoType dyn;
        @Child private Expr value;

        public ToIface(GoType dyn, Expr value) {
            this.dyn = dyn;
            this.value = value;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return new Iface(dyn, value.executeGeneric(frame));
        }
    }

    /** Dynamic type → does it implement the interface: type-identity cache, limit 4. */
    @GenerateInline(false)
    public abstract static class Implements extends GoNode {
        final GoType iface;
        final int ifaceId;

        Implements(GoType iface) {
            this.iface = iface;
            this.ifaceId = iface.ifaceId();
        }

        public abstract boolean execute(GoType dyn);

        @Specialization(guards = "dyn == cached", limit = "4")
        static boolean cached(GoType dyn, @Cached("dyn") GoType cached, @Cached("lookup(cached)") boolean result) {
            return result;
        }

        @Specialization(replaces = "cached")
        boolean lookup(GoType dyn) {
            return dyn.itabFast(iface, ifaceId) != null;
        }
    }

    /** {@code x.(T)}, T concrete: the data word, boxed in T's carrier. */
    public static final class AssertConcrete extends Ref {
        @Child private Expr x;
        private final GoType from;
        private final GoType target;
        private final BranchProfile fail = BranchProfile.create();

        public AssertConcrete(Expr x, GoType from, GoType target) {
            this.x = x;
            this.from = from;
            this.target = target;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            Iface i = (Iface) x.executeGeneric(frame);
            if (i == null || i.type != target) {
                fail.enter();
                throw Panics.typeAssert(this, from, i == null ? null : i.type, target);
            }
            return i.data;
        }
    }

    /** {@code v, ok := x.(T)}, T concrete, as a tuple. */
    public static final class AssertConcreteOk extends Ref {
        @Child private Expr x;
        private final GoType target;

        public AssertConcreteOk(Expr x, GoType target) {
            this.x = x;
            this.target = target;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            Iface i = (Iface) x.executeGeneric(frame);
            if (i != null && i.type == target) {
                return new Object[] {i.data, true};
            }
            return new Object[] {zero(), false};
        }

        @TruffleBoundary
        private Object zero() {
            return target.zero();
        }
    }

    /** {@code x.(I)}, I an interface type: the same interface value. */
    public static final class AssertIface extends Ref {
        @Child private Expr x;
        @Child private Implements impl;
        private final GoType from;
        private final GoType target;
        private final BranchProfile fail = BranchProfile.create();

        public AssertIface(Expr x, GoType from, GoType target) {
            this.x = x;
            this.from = from;
            this.target = target;
            this.impl = IfacesFactory.ImplementsNodeGen.create(target);
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            Iface i = (Iface) x.executeGeneric(frame);
            if (i == null || !impl.execute(i.type)) {
                fail.enter();
                throw Panics.typeAssert(this, from, i == null ? null : i.type, target);
            }
            return i;
        }
    }

    public static final class AssertIfaceOk extends Ref {
        @Child private Expr x;
        @Child private Implements impl;

        public AssertIfaceOk(Expr x, GoType target) {
            this.x = x;
            this.impl = IfacesFactory.ImplementsNodeGen.create(target);
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            Iface i = (Iface) x.executeGeneric(frame);
            boolean ok = i != null && impl.execute(i.type);
            return new Object[] {ok ? i : null, ok};
        }
    }

    /** Type-switch test: the dynamic type is exactly {@code type}. */
    public static final class TypeIs extends Bool {
        @Child private Expr x;
        private final GoType type;

        public TypeIs(Expr x, GoType type) {
            this.x = x;
            this.type = type;
        }

        @Override
        public boolean executeBoolean(VirtualFrame frame) {
            Iface i = (Iface) x.executeGeneric(frame);
            return i != null && i.type == type;
        }
    }

    /** Type-switch test: the dynamic type implements the interface. */
    public static final class TypeImplements extends Bool {
        @Child private Expr x;
        @Child private Implements impl;

        public TypeImplements(Expr x, GoType iface) {
            this.x = x;
            this.impl = IfacesFactory.ImplementsNodeGen.create(iface);
        }

        @Override
        public boolean executeBoolean(VirtualFrame frame) {
            Iface i = (Iface) x.executeGeneric(frame);
            return i != null && impl.execute(i.type);
        }
    }

    /** The data word of a non-nil interface value (after a successful type test). */
    public static final class Data extends Ref {
        @Child private Expr x;

        public Data(Expr x) {
            this.x = x;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return ((Iface) x.executeGeneric(frame)).data;
        }
    }
}
