package graalgo.go.nodes;

import com.oracle.truffle.api.CompilerDirectives;
import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.dsl.Bind;
import com.oracle.truffle.api.dsl.Cached;
import com.oracle.truffle.api.dsl.GenerateInline;
import com.oracle.truffle.api.dsl.ReportPolymorphism;
import com.oracle.truffle.api.dsl.Specialization;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.MaterializedFrame;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.DirectCallNode;
import com.oracle.truffle.api.nodes.ExplodeLoop;
import com.oracle.truffle.api.nodes.IndirectCallNode;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.nodes.RootNode;
import com.oracle.truffle.api.profiles.BranchProfile;
import graalgo.go.nodes.N.Expr;
import graalgo.go.nodes.N.GoNode;
import graalgo.go.nodes.N.Jump;
import graalgo.go.nodes.N.Ref;
import graalgo.go.nodes.N.Stmt;
import graalgo.go.runtime.G;
import graalgo.go.runtime.GoPanic;
import graalgo.go.runtime.GoProcess;
import graalgo.go.runtime.Panics;
import graalgo.go.runtime.Receivers;
import graalgo.go.types.GoType;
import graalgo.go.types.Itab;
import graalgo.go.types.Method;
import graalgo.go.values.Closure;
import graalgo.go.values.Env;
import graalgo.go.values.Iface;
import graalgo.language.GoLanguage;
import graalgo.runtime.Goexit;
import graalgo.safepoint.Safepoint;
import java.util.function.Supplier;

/**
 * Functions and calls (truffle-design §4.3, §4.5, §4.6).
 *
 * <p>Calling convention: {@code arguments[0]} is the callee's capture environment (a closure's
 * {@code MaterializedFrame}, null otherwise), {@code arguments[1..]} the Go arguments in carrier
 * form, boxed; a method's receiver is the first Go argument. One result is returned boxed,
 * several as an {@code Object[]} tuple. Struct and array arguments are private copies made by
 * the caller.
 *
 * <p>Defers: a function whose {@code defer} statements each run at most once is an
 * {@link OpenDeferRoot} (open-coded, amendment A8, GG-0071); any other deferring function is a
 * {@link DeferRoot} over the explicit per-goroutine chain of SPEC §3.5 (GG-0071). Both unwind a
 * panic the same way. Functions without {@code defer} never touch goroutine state.
 */
public final class Calls {
    private Calls() {}

    /** A Go function body. */
    public static class FuncRoot extends RootNode {
        private final String name;
        @Children private final Stmt[] prologue;
        @Child private Stmt body;
        @Child private Expr result;

        public FuncRoot(GoLanguage language, FrameDescriptor descriptor, String name, Stmt[] prologue, Stmt body,
                Expr result) {
            super(language, descriptor);
            this.name = name;
            this.prologue = prologue;
            this.body = body;
            this.result = result;
        }

        @Override
        public Object execute(VirtualFrame frame) {
            Safepoint.poll(this);
            enter(frame);
            runBody(frame);
            return result(frame);
        }

        @ExplodeLoop
        final void enter(VirtualFrame frame) {
            for (Stmt s : prologue) {
                s.executeVoid(frame);
            }
        }

        final void runBody(VirtualFrame frame) {
            try {
                body.executeVoid(frame);
            } catch (Jump j) {
                if (j != Jump.RETURN) {
                    throw j;
                }
            }
        }

        final Object result(VirtualFrame frame) {
            return result == null ? null : result.executeGeneric(frame);
        }

        @Override
        public String getName() {
            return name;
        }

        @Override
        public String toString() {
            return name;
        }
    }

    /** Shared machinery of the two kinds of deferring function. */
    abstract static class DeferringRoot extends FuncRoot {
        final BranchProfile panicked = BranchProfile.create();

        DeferringRoot(GoLanguage language, FrameDescriptor descriptor, String name, Stmt[] prologue, Stmt body,
                Expr result) {
            super(language, descriptor, name, prologue, body, result);
        }

        /**
         * Runs one deferred call while the function exits; {@code cur} is what is unwinding it:
         * null (normal return), a {@link GoPanic} or a {@link Goexit}. Without either it is a
         * plain call. With a panic the goroutine state names the panic and the call's argument
         * array, which is what makes {@code recover()} effective in exactly the function called
         * here; during a Goexit no panic is named, so {@code recover()} returns nil (GG-0070).
         * The result is what still unwinds the function afterwards: null once a panic is
         * recovered (or the Goexit that panic interrupted, which then resumes), a panic raised
         * by the call (chained to the one it ran for), or a Goexit called by it (gc: it aborts
         * the panic in flight).
         */
        final RuntimeException callDeferred(Dispatch site, Object target, Object[] args, RuntimeException cur) {
            if (cur == null) {
                try {
                    invoke(site, target, args);
                    return null;
                } catch (GoPanic p) {
                    panicked.enter();
                    return p;
                } catch (Goexit e) {
                    panicked.enter();
                    return e;
                }
            }
            G g = G.current(this);
            GoPanic outer = g.panic;
            Object[] outerArgs = g.recoverArgs;
            GoPanic running = cur instanceof GoPanic p ? p : null;
            g.panic = running;
            g.recoverArgs = running == null ? null : args;
            try {
                invoke(site, target, args);
                if (running != null && running.recovered) {
                    return running.resumeExit;
                }
                return cur;
            } catch (GoPanic p) {
                return raisedDuring(p, cur);
            } catch (Goexit e) {
                return e;
            } finally {
                g.panic = outer;
                g.recoverArgs = outerArgs;
            }
        }

        private void invoke(Dispatch site, Object target, Object[] args) {
            if (target == null) {
                throw Panics.nilDeref(this);
            }
            site.execute((RootCallTarget) target, args);
        }

        /**
         * A panic raised by a deferred call that ran for {@code cur}: linked to the panic that
         * call was running for; raised during a Goexit, it remembers the Goexit, which resumes
         * if the panic is recovered (gc).
         */
        @TruffleBoundary
        private static GoPanic raisedDuring(GoPanic raised, RuntimeException cur) {
            if (cur instanceof Goexit e) {
                if (raised.resumeExit == null) {
                    raised.resumeExit = e;
                }
                return raised;
            }
            GoPanic running = (GoPanic) cur;
            if (raised.resumeExit == null) {
                raised.resumeExit = running.resumeExit;
            }
            if (raised != running) {
                GoPanic tail = raised;
                while (tail.link != null && tail.link != running) {
                    tail = tail.link;
                }
                tail.link = running;
            }
            return raised;
        }
    }

    /**
     * A function whose {@code defer} statements can run more than once or are too many to
     * open-code (in a loop, with {@code goto}, over the bit limit): its deferred calls are the
     * records of the goroutine's defer chain above the mark taken at entry (SPEC §3.5).
     */
    public static final class DeferRoot extends DeferringRoot {
        @Child private Dispatch site = CallsFactory.DispatchNodeGen.create();

        public DeferRoot(GoLanguage language, FrameDescriptor descriptor, String name, Stmt[] prologue, Stmt body,
                Expr result) {
            super(language, descriptor, name, prologue, body, result);
        }

        @Override
        public Object execute(VirtualFrame frame) {
            Safepoint.poll(this);
            G g = G.current(this);
            G.Defer mark = g.defers;
            RuntimeException cur = null;
            try {
                enter(frame);
                runBody(frame);
            } catch (GoPanic | Goexit p) {
                panicked.enter();
                cur = p;
            }
            while (g.defers != mark) {
                G.Defer d = g.defers;
                g.defers = d.next;
                if (d instanceof G.RangeDefers r) {
                    // The defers of a range-over-func body (RangeFunc), in their place.
                    while (r.list != null) {
                        G.Defer x = r.list;
                        r.list = x.next;
                        cur = callDeferred(site, x.target, x.args, cur);
                    }
                } else {
                    cur = callDeferred(site, d.target, d.args, cur);
                }
            }
            if (cur != null) {
                throw cur;
            }
            return result(frame);
        }
    }

    /**
     * A function with open-coded defers (truffle-design §4.5, amendment A8; gc's design since
     * Go 1.14): every {@code defer} statement runs at most once, so it stores its function and
     * arguments in frame slots of its own and sets its bit; the exit sequence tests the bits in
     * reverse order and calls through a per-site inline cache, so a deferred call to a known
     * function is a direct, inlinable call. The goroutine state is touched only while a panic
     * unwinds.
     */
    public static final class OpenDeferRoot extends DeferringRoot {
        private final int bitsSlot;
        @CompilerDirectives.CompilationFinal(dimensions = 1) private final int[] targetSlots;
        @CompilerDirectives.CompilationFinal(dimensions = 1) private final int[] argsSlots;
        @Children private final Dispatch[] sites;

        public OpenDeferRoot(GoLanguage language, FrameDescriptor descriptor, String name, Stmt[] prologue, Stmt body,
                Expr result, int bitsSlot, int[] targetSlots, int[] argsSlots) {
            super(language, descriptor, name, prologue, body, result);
            this.bitsSlot = bitsSlot;
            this.targetSlots = targetSlots;
            this.argsSlots = argsSlots;
            this.sites = new Dispatch[targetSlots.length];
            for (int i = 0; i < sites.length; i++) {
                sites[i] = CallsFactory.DispatchNodeGen.create();
            }
        }

        @Override
        public Object execute(VirtualFrame frame) {
            Safepoint.poll(this);
            frame.setInt(bitsSlot, 0);
            RuntimeException cur = null;
            try {
                enter(frame);
                runBody(frame);
            } catch (GoPanic | Goexit p) {
                panicked.enter();
                cur = p;
            }
            cur = runDefers(frame, cur);
            if (cur != null) {
                throw cur;
            }
            return result(frame);
        }

        @ExplodeLoop
        private RuntimeException runDefers(VirtualFrame frame, RuntimeException unwinding) {
            RuntimeException cur = unwinding;
            for (int i = sites.length - 1; i >= 0; i--) {
                int bits = frame.getInt(bitsSlot);
                if ((bits & (1 << i)) != 0) {
                    frame.setInt(bitsSlot, bits & ~(1 << i));
                    Object target = frame.getObject(targetSlots[i]);
                    Object[] args = (Object[]) frame.getObject(argsSlots[i]);
                    cur = callDeferred(sites[i], target, args, cur);
                }
            }
            return cur;
        }
    }

    /**
     * A generated wrapper (method value, method expression, embedded-interface forwarder,
     * deferred builtin): one expression. The forwarding wrappers are gc's {@code WRAPPER}
     * frames: invisible to {@code recover()} eligibility, which their call node passes on to
     * the method they forward to ({@link #passRecover}).
     */
    public static final class ThunkRoot extends RootNode {
        private final String name;
        @Child private Expr body;

        public ThunkRoot(GoLanguage language, FrameDescriptor descriptor, String name, Expr body) {
            super(language, descriptor);
            this.name = name;
            this.body = body;
        }

        @Override
        public Object execute(VirtualFrame frame) {
            return body.executeGeneric(frame);
        }

        @Override
        public String getName() {
            return name;
        }

        @Override
        public boolean isInternal() {
            return true;
        }
    }

    // ---------------------------------------------------------------- calls

    /** A call of a statically known function or method: a direct call, target built lazily. */
    public static final class Call extends Ref {
        private final Supplier<RootCallTarget> target;
        @Children private final Expr[] args;
        @Child private DirectCallNode call;
        private final boolean forward;

        public Call(Supplier<RootCallTarget> target, Expr[] args) {
            this(target, args, false);
        }

        /** {@code forward}: the body of a forwarding wrapper (see {@link ThunkRoot}). */
        public Call(Supplier<RootCallTarget> target, Expr[] args, boolean forward) {
            this.target = target;
            this.args = args;
            this.forward = forward;
        }

        @Override
        @ExplodeLoop
        public Object executeGeneric(VirtualFrame frame) {
            Object[] a = new Object[args.length + 1];
            for (int i = 0; i < args.length; i++) {
                a[i + 1] = args[i].executeGeneric(frame);
            }
            if (call == null) {
                CompilerDirectives.transferToInterpreterAndInvalidate();
                call = insert(DirectCallNode.create(target.get()));
            }
            if (forward) {
                passRecover(this, frame, a);
            }
            return call.call(a);
        }
    }

    /** The func-value inline cache: target identity, limit 3, then an indirect call. */
    @ReportPolymorphism
    @GenerateInline(false)
    public abstract static class Dispatch extends GoNode {
        public abstract Object execute(RootCallTarget target, Object[] args);

        @Specialization(guards = "target == cached", limit = "3")
        static Object direct(RootCallTarget target, Object[] args,
                @Cached("target") RootCallTarget cached,
                @Cached("create(cached)") DirectCallNode call) {
            return call.call(args);
        }

        @Specialization(replaces = "direct")
        static Object indirect(RootCallTarget target, Object[] args, @Cached IndirectCallNode call) {
            return call.call(target, args);
        }
    }

    /** A call of a func value. */
    public static final class CallClosure extends Ref {
        @Child private Expr fn;
        @Children private final Expr[] args;
        @Child private Dispatch dispatch = CallsFactory.DispatchNodeGen.create();
        private final BranchProfile nil = BranchProfile.create();

        public CallClosure(Expr fn, Expr[] args) {
            this.fn = fn;
            this.args = args;
        }

        @Override
        @ExplodeLoop
        public Object executeGeneric(VirtualFrame frame) {
            Closure c = (Closure) fn.executeGeneric(frame);
            Object[] a = new Object[args.length + 1];
            for (int i = 0; i < args.length; i++) {
                a[i + 1] = args[i].executeGeneric(frame);
            }
            if (c == null) {
                nil.enter();
                throw Panics.nilDeref(this);
            }
            a[0] = c.env;
            return dispatch.execute(c.target, a);
        }
    }

    /**
     * Interface method dispatch, the central dynamic site (SPEC §3.3): an inline cache on the
     * dynamic type's identity whose entry holds the itab method (a PE constant: itabs are
     * immutable, no assumption) and a direct call; megamorphic sites load the method from the
     * type's itab by the site's constant method index and call indirectly.
     */
    @ReportPolymorphism
    @GenerateInline(false)
    public abstract static class IfaceDispatch extends GoNode {
        final GoType iface;
        final int index;

        IfaceDispatch(GoType iface, int index) {
            this.iface = iface;
            this.index = index;
        }

        public abstract Object execute(Iface recv, Object[] args);

        @Specialization(guards = "recv.type == cachedType", limit = "3")
        static Object cached(Iface recv, Object[] args,
                @Bind Node node,
                @Cached("recv.type") GoType cachedType,
                @Cached("method(cachedType)") Method method,
                @Cached("create(method.target())") DirectCallNode call) {
            args[1] = Receivers.adapt(method, recv.data, node);
            return call.call(args);
        }

        @Specialization(replaces = "cached")
        Object megamorphic(Iface recv, Object[] args, @Cached IndirectCallNode call) {
            Method m = method(recv.type);
            args[1] = Receivers.adapt(m, recv.data, this);
            return call.call(m.target(), args);
        }

        Method method(GoType dyn) {
            Itab itab = dyn.itab(iface);
            return itab.methods[index];
        }
    }

    /** {@code recv.M(args)} on an interface value. */
    public static final class IfaceCall extends Ref {
        @Child private Expr recv;
        @Children private final Expr[] args;
        @Child private IfaceDispatch dispatch;
        private final BranchProfile nil = BranchProfile.create();
        private final boolean forward;

        public IfaceCall(Expr recv, GoType iface, int index, Expr[] args) {
            this(recv, iface, index, args, false);
        }

        /** {@code forward}: the body of a forwarding wrapper (see {@link ThunkRoot}). */
        public IfaceCall(Expr recv, GoType iface, int index, Expr[] args, boolean forward) {
            this.recv = recv;
            this.args = args;
            this.dispatch = CallsFactory.IfaceDispatchNodeGen.create(iface, index);
            this.forward = forward;
        }

        @Override
        @ExplodeLoop
        public Object executeGeneric(VirtualFrame frame) {
            Iface r = (Iface) recv.executeGeneric(frame);
            Object[] a = new Object[args.length + 2];
            for (int i = 0; i < args.length; i++) {
                a[i + 2] = args[i].executeGeneric(frame);
            }
            if (r == null) {
                nil.enter();
                throw Panics.nilDeref(this);
            }
            if (forward) {
                passRecover(this, frame, a);
            }
            return dispatch.execute(r, a);
        }
    }

    /** The receiver a method-set entry's function expects, from a value of the dynamic type. */
    public static final class Adapt extends Ref {
        private final Method method;
        @Child private Expr recv;

        public Adapt(Method method, Expr recv) {
            this.method = method;
            this.recv = recv;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return Receivers.adapt(method, recv.executeGeneric(frame), this);
        }
    }

    /**
     * A forwarding wrapper's call: when the wrapper's own frame is the one {@code recover()} is
     * effective in (it was called directly by the deferred-call runner), the forwarded call's
     * argument array {@code a} inherits that eligibility, as gc skips {@code WRAPPER} frames.
     * The runner restores the goroutine state when the deferred call returns.
     */
    static void passRecover(Node n, VirtualFrame frame, Object[] a) {
        G g = G.current(n);
        if (g.recoverArgs == frame.getArguments()) {
            g.recoverArgs = a;
        }
    }

    /** Several results as the call boundary's tuple. */
    public static final class Tuple extends Ref {
        @Children private final Expr[] elems;

        public Tuple(Expr[] elems) {
            this.elems = elems;
        }

        @Override
        @ExplodeLoop
        public Object executeGeneric(VirtualFrame frame) {
            Object[] t = new Object[elems.length];
            for (int i = 0; i < elems.length; i++) {
                t[i] = elems[i].executeGeneric(frame);
            }
            return t;
        }
    }

    public static final class TupleGet extends Ref {
        @Child private Expr tuple;
        private final int index;

        public TupleGet(Expr tuple, int index) {
            this.tuple = tuple;
            this.index = index;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return ((Object[]) tuple.executeGeneric(frame))[index];
        }
    }

    // ---------------------------------------------------------------- closures

    /**
     * A function literal: the func value {@code {target, environment frame}} (SPEC §3.4). The
     * environment is a new frame holding the boxes of the captured variables, read from this
     * frame's slots now — so a per-iteration loop variable is captured per iteration.
     */
    public static final class MakeClosure extends Ref {
        private final Supplier<RootCallTarget> target;
        private final FrameDescriptor envDescriptor;
        @CompilerDirectives.CompilationFinal(dimensions = 1) private final int[] slots;
        @CompilerDirectives.CompilationFinal private RootCallTarget resolved;

        public MakeClosure(Supplier<RootCallTarget> target, FrameDescriptor envDescriptor, int[] slots) {
            this.target = target;
            this.envDescriptor = envDescriptor;
            this.slots = slots;
        }

        @Override
        @ExplodeLoop
        public Object executeGeneric(VirtualFrame frame) {
            if (resolved == null) {
                CompilerDirectives.transferToInterpreterAndInvalidate();
                resolved = target.get();
            }
            MaterializedFrame env = Env.create(envDescriptor);
            for (int i = 0; i < slots.length; i++) {
                Env.set(env, i, frame.getObject(slots[i]));
            }
            return new Closure(resolved, env);
        }
    }

    /** A top-level function or instantiation as a func value; resolved on first evaluation. */
    public static final class FuncConst extends Ref {
        private final Supplier<Closure> closure;
        @CompilerDirectives.CompilationFinal private Closure resolved;

        public FuncConst(Supplier<Closure> closure) {
            this.closure = closure;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            if (resolved == null) {
                CompilerDirectives.transferToInterpreterAndInvalidate();
                resolved = closure.get();
            }
            return resolved;
        }
    }

    /** A bound value (method value receiver) as a closure over a one-slot environment. */
    public static final class Bind1 extends Ref {
        private final RootCallTarget target;
        private final FrameDescriptor envDescriptor;
        @Child private Expr value;
        private final boolean nilCheck;

        public Bind1(RootCallTarget target, FrameDescriptor envDescriptor, Expr value, boolean nilCheck) {
            this.target = target;
            this.envDescriptor = envDescriptor;
            this.value = value;
            this.nilCheck = nilCheck;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            Object v = value.executeGeneric(frame);
            if (nilCheck && v == null) {
                throw Panics.nilDeref(this);
            }
            MaterializedFrame env = Env.create(envDescriptor);
            Env.set(env, 0, v);
            return new Closure(target, env);
        }
    }

    /** Slot {@code index} of the running closure's environment. */
    public static final class EnvRead extends Ref {
        private final int index;

        public EnvRead(int index) {
            this.index = index;
        }

        @Override
        public Object executeGeneric(VirtualFrame frame) {
            return Env.get(frame.getArguments()[0], index);
        }
    }

    // ---------------------------------------------------------------- defer, go, panic, recover

    /**
     * Evaluates the function and arguments of a {@code defer} or {@code go} call now: the
     * argument array (calling-convention form) is the result, the call target (null: a nil func
     * value) goes to the frame slot {@link #targetSlot} — for an open-coded defer the site's own
     * slot — so preparing allocates nothing but the argument array.
     */
    public abstract static class Prepared extends GoNode {
        final int targetSlot;

        Prepared(int targetSlot) {
            this.targetSlot = targetSlot;
        }

        public abstract Object[] prepare(VirtualFrame frame);
    }

    /** A statically known callee: its target is resolved once and is a PE constant. */
    public static final class PrepStatic extends Prepared {
        private final Supplier<RootCallTarget> target;
        @Children private final Expr[] args;
        @CompilerDirectives.CompilationFinal private RootCallTarget resolved;

        public PrepStatic(int targetSlot, Supplier<RootCallTarget> target, Expr[] args) {
            super(targetSlot);
            this.target = target;
            this.args = args;
        }

        @Override
        @ExplodeLoop
        public Object[] prepare(VirtualFrame frame) {
            Object[] a = new Object[args.length + 1];
            for (int i = 0; i < args.length; i++) {
                a[i + 1] = args[i].executeGeneric(frame);
            }
            if (resolved == null) {
                CompilerDirectives.transferToInterpreterAndInvalidate();
                resolved = target.get();
            }
            frame.setObject(targetSlot, resolved);
            return a;
        }
    }

    public static final class PrepClosure extends Prepared {
        @Child private Expr fn;
        @Children private final Expr[] args;

        public PrepClosure(int targetSlot, Expr fn, Expr[] args) {
            super(targetSlot);
            this.fn = fn;
            this.args = args;
        }

        @Override
        @ExplodeLoop
        public Object[] prepare(VirtualFrame frame) {
            Closure c = (Closure) fn.executeGeneric(frame);
            Object[] a = new Object[args.length + 1];
            for (int i = 0; i < args.length; i++) {
                a[i + 1] = args[i].executeGeneric(frame);
            }
            if (c == null) {
                frame.setObject(targetSlot, null);
                return a;
            }
            a[0] = c.env;
            frame.setObject(targetSlot, c.target);
            return a;
        }
    }

    /** An interface method call resolved when the statement runs, so the record is direct. */
    public static final class PrepIface extends Prepared {
        @Child private Expr recv;
        @Children private final Expr[] args;
        private final GoType iface;
        private final int index;

        public PrepIface(int targetSlot, Expr recv, GoType iface, int index, Expr[] args) {
            super(targetSlot);
            this.recv = recv;
            this.iface = iface;
            this.index = index;
            this.args = args;
        }

        @Override
        @ExplodeLoop
        public Object[] prepare(VirtualFrame frame) {
            Iface r = (Iface) recv.executeGeneric(frame);
            Object[] a = new Object[args.length + 2];
            for (int i = 0; i < args.length; i++) {
                a[i + 2] = args[i].executeGeneric(frame);
            }
            if (r == null) {
                throw Panics.nilDeref(this);
            }
            Method m = method(r.type);
            a[1] = Receivers.adapt(m, r.data, this);
            frame.setObject(targetSlot, m.target());
            return a;
        }

        @TruffleBoundary
        private Method method(GoType dyn) {
            return dyn.itab(iface).methods[index];
        }
    }

    /** {@code defer} in a function that is not open-coded: a record on the goroutine's defer chain. */
    public static final class Defer extends Stmt {
        @Child private Prepared call;

        public Defer(Prepared call) {
            this.call = call;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            Object[] a = call.prepare(frame);
            G g = G.current(this);
            g.defers = new G.Defer(frame.getObject(call.targetSlot), a, g.defers);
        }
    }

    /**
     * An open-coded {@code defer}: the function into the site's target slot (by the prepared
     * call itself), the arguments into its argument slot, its bit set. No allocation beyond the
     * argument array, no access to the goroutine state (GG-0071).
     */
    public static final class OpenDefer extends Stmt {
        @Child private Prepared call;
        private final int bit;
        private final int bitsSlot;
        private final int argsSlot;

        public OpenDefer(Prepared call, int bit, int bitsSlot, int argsSlot) {
            this.call = call;
            this.bit = bit;
            this.bitsSlot = bitsSlot;
            this.argsSlot = argsSlot;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            frame.setObject(argsSlot, call.prepare(frame));
            frame.setInt(bitsSlot, frame.getInt(bitsSlot) | (1 << bit));
        }
    }

    /** {@code go}: starts a goroutine on the scheduler. */
    public static final class Go extends Stmt {
        @Child private Prepared call;

        public Go(Prepared call) {
            this.call = call;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            Object[] a = call.prepare(frame);
            Object target = frame.getObject(call.targetSlot);
            if (target == null) {
                throw Panics.plain(this, "go of nil func value");
            }
            GoProcess.get(this).go((RootCallTarget) target, a);
        }
    }

    public static final class Panic extends Stmt {
        @Child private Expr value;

        public Panic(Expr value) {
            this.value = value;
        }

        @Override
        public void executeVoid(VirtualFrame frame) {
            throw Panics.user(this, value.executeGeneric(frame));
        }
    }

    /**
     * {@code recover()}: effective only in the function the deferring function called directly
     * for the panic being unwound — the frame whose argument array is the defer record's.
     */
    public static final class Recover extends Ref {
        @Override
        public Object executeGeneric(VirtualFrame frame) {
            G g = G.current(this);
            GoPanic p = g.panic;
            if (p != null && !p.recovered && g.recoverArgs == frame.getArguments()) {
                p.recovered = true;
                return p.value;
            }
            return null;
        }
    }
}
