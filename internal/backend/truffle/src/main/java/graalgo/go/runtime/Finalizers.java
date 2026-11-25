package graalgo.go.runtime;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import graalgo.go.types.GoType;
import graalgo.go.values.Closure;
import graalgo.go.values.Flat;
import graalgo.go.values.Iface;
import graalgo.go.values.Ptr;
import graalgo.go.values.View;
import graalgo.sched.SchedulerCore;
import java.lang.ref.Reference;
import java.lang.ref.ReferenceQueue;
import java.lang.ref.WeakReference;
import java.util.ArrayDeque;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Map;
import java.util.Set;
import java.util.WeakHashMap;

/**
 * Finalizers and cleanups of one process (gc's {@code mfinal.go}, {@code mcleanup.go}; G3 of
 * docs/arch/m3-plan.md): {@code runtime.SetFinalizer}, {@code runtime.AddCleanup},
 * {@code Cleanup.Stop} and what {@code runtime.GC} owes them.
 *
 * <p><b>Detection.</b> Go objects are JVM objects and the JVM collector is the Go collector
 * (ADR-0002); a special (gc's word for a finalizer or cleanup record) is a
 * {@link WeakReference} to the object a pointer names — the {@link Flat} itself, or the
 * referent of an interior pointer — which the collector clears when the object becomes
 * unreachable (nothing holds Go objects softly, so weakly reachable is unreachable for Go).
 *
 * <p><b>Resurrection.</b> gc passes the finalizer the object itself, which makes it reachable
 * again. A cleared reference cannot return its referent, but a {@link Flat}'s identity and its
 * storage are separate: the special holds the object's storage arrays (the type and the three
 * slot regions), and the finalizer receives a new {@code Flat} over the same storage. Nothing
 * else can observe the difference — no reference to the old identity survives, which is why
 * the reference was cleared — and the contents are the object's, so the finalizer reads (and
 * may resurrect) exactly what gc's would. Holding the storage also keeps everything the object
 * references alive until the finalizer has run, as gc's marking from a special does; an object
 * that reaches itself through its own fields is therefore never finalized, which gc's
 * documentation allows for cycles. A cleanup gets no object, so its special holds nothing of
 * it (gc: the cleanup must not reach its pointer, or it never runs).
 *
 * <p><b>Running.</b> As in gc, finalizers run one at a time on a finalizer goroutine, and
 * cleanups on a cleanup goroutine (gc may run several; one is a valid schedule): each is a
 * runtime goroutine ({@link SchedulerCore#spawnSystem}: not counted by deadlock detection),
 * started when its queue becomes non-empty and ending when it drains. A panic in a finalizer
 * or cleanup is fatal to the process, as in gc.
 *
 * <p><b>When.</b> {@code runtime.GC} collects ({@link System#gc}) and dispatches the specials
 * whose references that collection cleared ({@link #collect}); its caller then yields once: as
 * in gc, every finalizer the collection found is queued when {@code GC} returns, and the
 * finalizer goroutine gets to run promptly. After a collection the program did not ask for,
 * the reference handler enqueues the cleared references and a watcher thread dispatches them
 * within {@link #POLL_MILLIS}.
 */
public final class Finalizers {
    /** How often the watcher looks for specials after collections nobody asked for. */
    static final long POLL_MILLIS = 100;

    private final GoProcess process;
    private final SchedulerCore sched;
    /** Guards every field below and the queues' state. */
    private final Object lock = new Object();
    private final ReferenceQueue<Object> queue = new ReferenceQueue<>();
    /** Finalizer specials by object; weak keys (identity: {@link Flat} has no equals). */
    private final WeakHashMap<Flat, Special> finalizers = new WeakHashMap<>();
    /**
     * Every registered, not yet dispatched special, strongly: a reference object that is itself
     * unreachable is never enqueued.
     */
    private final Set<Special> live = new HashSet<>();
    private final Map<Long, Special> cleanups = new HashMap<>();
    private long nextCleanupId = 1;
    private final Runner finq = new Runner();
    private final Runner cleanupq = new Runner();
    private Thread watcher;

    public Finalizers(GoProcess process, SchedulerCore sched) {
        this.process = process;
        this.sched = sched;
    }

    /** A finalizer's hold on its object's storage, rebuilt into the argument. */
    private record Storage(GoType type, long[] prims, Object[] refs, byte[] bytes) {
    }

    /**
     * gc's special record: a finalizer (with the object's storage) or a cleanup. What it holds
     * ({@link #fn}, {@link #storage}) is released when it has run or is cancelled: the reference
     * object itself may linger (in the reference queue, in the map's stale entries) and must
     * not keep the object's referents alive — gc frees them in the next cycle.
     */
    private static final class Special extends WeakReference<Object> {
        final boolean finalizer;
        Closure fn;
        /** Finalizer: the object's storage; null for a cleanup and once released. */
        Storage storage;
        /** Finalizer: the slot offsets of an interior pointer (zero for the object's start). */
        final int prim;
        final int ref;
        final int byt;
        /** Finalizer: the argument's interface type (pass {@code obj} boxed), or null (pass the pointer). */
        final GoType boxAs;
        final long id;
        boolean cancelled;

        Special(Flat referent, ReferenceQueue<Object> q, Closure fn, boolean finalizer, int prim, int ref, int byt,
                        GoType boxAs, long id) {
            super(referent, q);
            this.finalizer = finalizer;
            this.fn = fn;
            this.storage = finalizer ? new Storage(referent.type, referent.prims, referent.refs, referent.bytes) : null;
            this.prim = prim;
            this.ref = ref;
            this.byt = byt;
            this.boxAs = boxAs;
            this.id = id;
        }

        /** The finalizer's argument: the resurrected object (or interior pointer into it). */
        Object argument(Storage st) {
            Object p = Ptr.make(new Flat(st.type(), st.prims(), st.refs(), st.bytes()), prim, ref, byt);
            return boxAs == null ? p : new Iface(boxAs, p);
        }

        /** Drops what the special holds (lock held). */
        void release() {
            fn = null;
            storage = null;
        }
    }

    /** One runtime goroutine running a queue's functions in order (gc's fing / cleanup goroutine). */
    private final class Runner {
        private final ArrayDeque<Special> q = new ArrayDeque<>();
        private boolean running;

        /** Queues {@code s}; starts the goroutine if none runs. Lock held. */
        void add(Special s) {
            q.add(s);
            if (!running && !sched.isExiting()) {
                running = true;
                try {
                    sched.spawnSystem(() -> process.run(this::drain));
                } catch (RuntimeException teardown) {
                    // The scheduler is being torn down (the context closes): gc runs no
                    // finalizer once the program exits either.
                    running = false;
                }
            }
        }

        private void drain() {
            for (;;) {
                Closure fn;
                Storage st;
                Special s;
                synchronized (lock) {
                    s = q.poll();
                    if (s == null) {
                        running = false;
                        return;
                    }
                    fn = s.fn;
                    st = s.storage;
                    s.release();
                }
                if (s.finalizer) {
                    fn.target().call(GoProcess.NO_CALLER, fn.env(), s.argument(st));
                } else {
                    fn.target().call(GoProcess.NO_CALLER, fn.env());
                }
            }
        }
    }

    // ------------------------------------------------------------------ SetFinalizer

    /** The referent and slot offsets of a pointer; null when it is not a Go heap pointer. */
    private static Object[] locate(Object p) {
        if (p instanceof Flat f) {
            return new Object[] {f, 0, 0, 0};
        } else if (p instanceof Ptr q) {
            return new Object[] {q.obj(), q.prim(), q.ref(), q.byt()};
        } else if (p instanceof View v) {
            return new Object[] {v.obj(), -1, -1, -1};
        }
        return null;
    }

    /**
     * gc's {@code isGoPointerWithoutSpan}: the location is not in an allocated block — a package
     * variable (in the package's globals object), a zero-size object (gc: {@code zerobase}),
     * or a representation that is no heap object at all.
     */
    private boolean noSpan(Object[] at) {
        if (at == null) {
            return true;
        }
        Flat f = (Flat) at[0];
        if (f.type != null && f.type.gcSize() == 0 || f.prims.length + f.refs.length + f.bytes.length == 0) {
            return true;
        }
        for (Flat g : process.globals) {
            if (g == f) {
                return true;
            }
        }
        return false;
    }

    /** {@code runtime.SetFinalizer(obj, finalizer)}: upstream's checks, then the special. */
    @TruffleBoundary
    public void setFinalizer(Object obj, Object finalizer) {
        if (obj == null) {
            throw process.fatal("runtime.SetFinalizer: first argument is nil");
        }
        Iface e = (Iface) obj;
        GoType etyp = e.type;
        if (etyp.u().kind != GoType.POINTER) {
            throw process.fatal("runtime.SetFinalizer: first argument is " + etyp.str() + ", not pointer");
        }
        GoType elem = etyp.u().elem;
        if (e.data == null) {
            throw process.fatal("runtime.SetFinalizer: pointer not in allocated block");
        }
        Object[] at = locate(e.data);
        if (noSpan(at)) {
            // gc: a pointer to a global (or to zerobase, every zero-size allocation's address)
            // has no span, and SetFinalizer returns without doing anything.
            return;
        }
        Flat base = (Flat) at[0];
        int prim = (int) at[1], ref = (int) at[2], byt = (int) at[3];
        if ((prim | ref | byt) != 0 && (prim < 0 || elem.hasPointers() || elem.gcSize() >= 16)) {
            throw process.fatal("runtime.SetFinalizer: pointer not at beginning of allocated block");
        }
        if (finalizer == null) {
            synchronized (lock) {
                Special old = finalizers.remove(base);
                if (old != null) {
                    old.cancelled = true;
                    old.release();
                    live.remove(old);
                }
            }
            return;
        }
        Iface f = (Iface) finalizer;
        GoType ftyp = f.type;
        if (ftyp.u().kind != GoType.FUNC) {
            throw process.fatal("runtime.SetFinalizer: second argument is " + ftyp.str() + ", not a function");
        }
        GoType ft = ftyp.u();
        if (ft.variadic) {
            throw process.fatal("runtime.SetFinalizer: cannot pass " + etyp.str() + " to finalizer " + ftyp.str()
                            + " because dotdotdot");
        }
        if (ft.params.length != 1) {
            throw process.fatal("runtime.SetFinalizer: cannot pass " + etyp.str() + " to finalizer " + ftyp.str());
        }
        GoType fint = ft.params[0];
        GoType boxAs = null;
        boolean ok;
        if (fint.key.equals(etyp.key)) {
            ok = true;
        } else if (fint.u().kind == GoType.POINTER) {
            ok = (!fint.isNamed() || !etyp.isNamed()) && fint.u().elem.key.equals(elem.key);
        } else if (fint.u().kind == GoType.INTERFACE) {
            ok = fint.u().imethods.length == 0 || etyp.itab(fint) != null;
            boxAs = etyp;
        } else {
            ok = false;
        }
        if (!ok) {
            throw process.fatal("runtime.SetFinalizer: cannot pass " + etyp.str() + " to finalizer " + ftyp.str());
        }
        synchronized (lock) {
            if (finalizers.containsKey(base)) {
                throw process.fatal("runtime.SetFinalizer: finalizer already set");
            }
            Special s = new Special(base, queue, (Closure) f.data, true, prim, ref, byt, boxAs, 0);
            finalizers.put(base, s);
            live.add(s);
            startWatcher();
        }
        Reference.reachabilityFence(base);
    }

    // ------------------------------------------------------------------ AddCleanup

    /** {@link #addCleanup}'s answer: {@code arg} is {@code ptr} (the facade panics as gc does). */
    public static final long CLEANUP_ARG_IS_PTR = -1;

    /**
     * {@code runtime.addCleanup(ptr, fn, arg)}: registers {@code fn} to run once the object
     * {@code ptr} points into is unreachable (an interior pointer names its whole object, as in
     * gc); returns the cleanup's id, 0 when the location has no span (a global: gc registers
     * nothing and returns a zero {@code Cleanup}), or {@link #CLEANUP_ARG_IS_PTR}. {@code arg}
     * is the cleanup's argument, only checked: gc panics when it is {@code ptr} itself (the
     * cleanup would keep its object alive and never run).
     */
    @TruffleBoundary
    public long addCleanup(Object ptr, Object fn, Object arg) {
        if (arg instanceof Iface a && a.data != null && Ptr.same(a.data, ptr)) {
            return CLEANUP_ARG_IS_PTR;
        }
        Object[] at = locate(ptr);
        if (noSpan(at)) {
            return 0;
        }
        synchronized (lock) {
            long id = nextCleanupId++;
            Special s = new Special((Flat) at[0], queue, (Closure) fn, false, 0, 0, 0, null, id);
            live.add(s);
            cleanups.put(id, s);
            startWatcher();
            return id;
        }
    }

    /** {@code Cleanup.Stop}: the cleanup will not run (a no-op once it ran or was stopped). */
    @TruffleBoundary
    public void stopCleanup(long id) {
        synchronized (lock) {
            Special s = cleanups.remove(id);
            if (s != null) {
                s.cancelled = true;
                s.release();
                live.remove(s);
            }
        }
    }

    // ------------------------------------------------------------------ dispatch

    /** Dispatches every enqueued special not dispatched yet. Lock held. */
    private void drainLocked() {
        Reference<?> r;
        while ((r = queue.poll()) != null) {
            Special s = (Special) r;
            if (live.remove(s)) {
                dispatchLocked(s);
            }
        }
    }

    /** Queues a special whose object is unreachable (unless it was cancelled). Lock held. */
    private void dispatchLocked(Special s) {
        if (s.cancelled) {
            return;
        }
        if (s.finalizer) {
            finq.add(s);
            // The map's entry for the object is stale now (its weak key cleared with the
            // special); expunge it, or its value would hold the special until the next
            // registration.
            finalizers.size();
        } else {
            cleanups.remove(s.id);
            cleanupq.add(s);
        }
    }

    /** Starts the watcher thread on the first registration. Lock held. */
    private void startWatcher() {
        if (watcher != null) {
            return;
        }
        watcher = Thread.ofPlatform().daemon(true).name("graalgo-finalizers").unstarted(this::watch);
        watcher.start();
    }

    /** After collections nobody asked for: dispatch what they found, until the process exits. */
    private void watch() {
        synchronized (lock) {
            try {
                while (!sched.isExiting()) {
                    drainLocked();
                    lock.wait(POLL_MILLIS);
                }
            } catch (InterruptedException e) {
                // fall through: the process is going away
            } finally {
                releaseAllLocked();
            }
        }
    }

    /**
     * The process exits (gc runs no finalizer then): every special is cleared and drops what it
     * holds. A special the collector still finds (cleared but not yet enqueued) sits on the
     * JVM's pending-reference list, a strong root; holding its closure there would keep the
     * closed context's whole program alive (test/m1r/leak.sh). Lock held.
     */
    private void releaseAllLocked() {
        for (Special s : live) {
            s.clear();
            s.release();
        }
        live.clear();
        finalizers.clear();
        cleanups.clear();
        finq.q.clear();
        cleanupq.q.clear();
    }

    /**
     * {@code runtime.GC}'s part: collect, then dispatch the specials of every object the
     * collection found unreachable. The collector clears the weak references of the objects it
     * found dead before {@link System#gc} returns (the reference handler only enqueues them,
     * later), so the scan finds exactly this collection's — as gc queues finalizers during the
     * collection that finds them.
     */
    @TruffleBoundary
    public void collect() {
        System.gc();
        synchronized (lock) {
            for (java.util.Iterator<Special> it = live.iterator(); it.hasNext();) {
                Special s = it.next();
                if (s.refersTo(null)) {
                    it.remove();
                    dispatchLocked(s);
                }
            }
            drainLocked();
        }
    }
}
