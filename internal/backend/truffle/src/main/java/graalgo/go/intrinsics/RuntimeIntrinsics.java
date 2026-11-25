package graalgo.go.intrinsics;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.runtime.GoProcess;
import graalgo.go.runtime.Panics;
import graalgo.go.runtime.Stacks;
import graalgo.go.types.Descriptors;
import graalgo.go.types.GoType;
import graalgo.go.values.Slice;
import graalgo.go.values.Str;
import graalgo.runtime.GoErrors;
import graalgo.sched.SchedulerCore;
import java.nio.charset.StandardCharsets;
import java.util.List;

/**
 * Intrinsics of the facade {@code runtime} package ({@code stdlib/fork/runtime}); registered in
 * {@code internal/toolexec/intrinsics/registry.txt} (toolexec-design §5.3, §5.6).
 */
final class RuntimeIntrinsics {
    private RuntimeIntrinsics() {}

    @TruffleBoundary
    static void runtime_Gosched(Node node) {
        SchedulerCore.current().yieldCurrent();
    }

    @TruffleBoundary
    static void runtime_GC(Node node) {
        // gc clears sync.Pool's caches at the start of a collection, with the world stopped:
        // every pool field is touched only inside pin sections, so holding the pin lock is
        // the stopped world for them.
        graalgo.go.values.Closure cleanup = graalgo.go.runtime.GoProcess.get(node).poolCleanup;
        if (cleanup != null) {
            graalgo.runtime.sync.PinLock.lockForGo(node);
            try {
                cleanup.target().call(node, cleanup.env());
            } finally {
                graalgo.runtime.sync.PinLock.LOCK.unlock();
            }
        }
        GoProcess process = GoProcess.get(node);
        process.numForcedGC.incrementAndGet();
        SchedulerCore sc = SchedulerCore.current();
        // gc's runtime.GC blocks the calling goroutine, not its P: the collection (and the wait
        // for the reference handler) releases the scheduler slot, as a blocking system call.
        sc.ioEnter();
        try {
            process.finalizers.collect();
        } finally {
            sc.ioExit();
        }
        // gc's runtime.GC ends sweeping with Gosched calls, during which the finalizer goroutine
        // it readied runs: let it.
        sc.yieldCurrent();
    }

    // ------------------------------------------------------------------ finalizers, cleanups

    /** {@code runtime.SetFinalizer(obj, finalizer any)} (graalgo.go.runtime.Finalizers). */
    @TruffleBoundary
    static void runtime_SetFinalizer(Node node, Object obj, Object finalizer) {
        GoProcess.get(node).finalizers.setFinalizer(obj, finalizer);
    }

    /**
     * {@code runtime.KeepAlive(x any)}: x is reachable until here. The JIT considers a value dead
     * after its last use; the boundary makes x an argument of a real call at this point (a use
     * compiled code cannot drop), and the fence is the JVM's statement of the same contract.
     */
    @TruffleBoundary
    static void runtime_KeepAlive(Node node, Object x) {
        java.lang.ref.Reference.reachabilityFence(x);
    }

    /** {@code runtime.addCleanup(ptr unsafe.Pointer, fn func(), arg any) int64} (the facade's AddCleanup). */
    @TruffleBoundary
    static long runtime_addCleanup(Node node, Object ptr, Object fn, Object arg) {
        return GoProcess.get(node).finalizers.addCleanup(ptr, fn, arg);
    }

    /** {@code runtime.stopCleanup(id int64)} ({@code Cleanup.Stop}). */
    @TruffleBoundary
    static void runtime_stopCleanup(Node node, long id) {
        GoProcess.get(node).finalizers.stopCleanup(id);
    }

    // ------------------------------------------------------------------ memory statistics

    /**
     * {@code runtime.readMemStats(*[memStatsWords]uint64)}: the JVM's memory and collector
     * figures in the facade's order (stdlib/fork/runtime/mstats.go maps them onto
     * {@code MemStats} and documents the mapping). The JVM heap is the Go heap (ADR-0002):
     * <ol start="0">
     * <li>heap in use: {@code MemoryMXBean} heap used (gc {@code HeapAlloc}, {@code Alloc});
     * <li>bytes allocated, cumulative: the sum over threads of
     * {@code com.sun.management.ThreadMXBean#getTotalThreadAllocatedBytes} (gc
     * {@code TotalAlloc}; it counts the runtime's own allocations as well);
     * <li>heap committed (gc {@code HeapSys});
     * <li>non-heap committed: code cache, metaspace (gc {@code OtherSys});
     * <li>heap maximum, or committed if unbounded (gc {@code NextGC}: the size the heap grows
     * to before the collector must reclaim);
     * <li>end of the last collection, Unix ns (gc {@code LastGC});
     * <li>total pause, ns: the stop-the-world collectors' accumulated time (gc
     * {@code PauseTotalNs}; a concurrent collector's background cycles are not pauses);
     * <li>completed collections, the same collectors' counts (gc {@code NumGC});
     * <li>{@code runtime.GC} calls of this process (gc {@code NumForcedGC});
     * <li>the last collection's duration, ns (gc {@code PauseNs[(NumGC+255)%256]});
     * </ol>
     * The result is the collectors' CPU fraction since start (gc {@code GCCPUFraction}).
     */
    @TruffleBoundary
    static double runtime_readMemStats(Node node, Object out) {
        long[] w;
        int o;
        if (out instanceof graalgo.go.values.Ptr p) {
            w = p.obj().prims;
            o = p.prim();
        } else {
            w = ((graalgo.go.values.Flat) out).prims;
            o = 0;
        }
        java.lang.management.MemoryMXBean mem = java.lang.management.ManagementFactory.getMemoryMXBean();
        java.lang.management.MemoryUsage heap = mem.getHeapMemoryUsage();
        java.lang.management.MemoryUsage nonHeap = mem.getNonHeapMemoryUsage();
        long numGC = 0, pauseMs = 0, lastEnd = -1, lastDur = 0;
        for (java.lang.management.GarbageCollectorMXBean b : java.lang.management.ManagementFactory.getGarbageCollectorMXBeans()) {
            if (b.getName().contains("Concurrent")) {
                continue; // G1/ZGC background cycles: not stop-the-world collections
            }
            numGC += Math.max(0, b.getCollectionCount());
            pauseMs += Math.max(0, b.getCollectionTime());
            if (b instanceof com.sun.management.GarbageCollectorMXBean g && g.getLastGcInfo() != null) {
                com.sun.management.GcInfo info = g.getLastGcInfo();
                if (info.getEndTime() > lastEnd) {
                    lastEnd = info.getEndTime();
                    lastDur = info.getDuration();
                }
            }
        }
        long allocated = 0;
        if (java.lang.management.ManagementFactory.getThreadMXBean() instanceof com.sun.management.ThreadMXBean t
                        && t.isThreadAllocatedMemorySupported()) {
            allocated = Math.max(0, t.getTotalThreadAllocatedBytes());
        }
        java.lang.management.RuntimeMXBean rt = java.lang.management.ManagementFactory.getRuntimeMXBean();
        long uptimeMs = Math.max(1, rt.getUptime());
        w[o + 0] = heap.getUsed();
        w[o + 1] = Math.max(allocated, heap.getUsed());
        w[o + 2] = heap.getCommitted();
        w[o + 3] = nonHeap.getCommitted();
        w[o + 4] = heap.getMax() > 0 ? heap.getMax() : heap.getCommitted();
        w[o + 5] = lastEnd < 0 ? 0 : (rt.getStartTime() + lastEnd) * 1_000_000L;
        w[o + 6] = pauseMs * 1_000_000L;
        w[o + 7] = numGC;
        w[o + 8] = GoProcess.get(node).numForcedGC.get();
        w[o + 9] = lastDur * 1_000_000L;
        return Math.min(1.0, (double) pauseMs / ((double) uptimeMs * Runtime.getRuntime().availableProcessors()));
    }

    // ------------------------------------------------------------------ memory hashing

    /**
     * {@code runtime.memhash(p unsafe.Pointer, h, s uintptr) uintptr}: a hash of the {@code s}
     * bytes at {@code p}, seeded by {@code h} — what {@code hash/maphash} (without
     * {@code purego}) and code outside std reach by {@code //go:linkname}. gc's is AES- or
     * wyhash-based with a per-process random key; only its contract is observable (equal bytes
     * and seed, equal hash; the values differ between processes), and this one keeps it: a
     * wyhash-style mix of the bytes (graalgo.go.runtime.Unsafe reads them in gc byte order).
     */
    @TruffleBoundary
    static long runtime_memhash(Node node, Object p, long h, long s) {
        int n = (int) Math.min(s, Integer.MAX_VALUE);
        byte[] b = new byte[n];
        if (n > 0) {
            graalgo.go.runtime.Unsafe.readBytes(node, graalgo.go.runtime.Unsafe.referent(p), graalgo.go.runtime.Unsafe.offset(p), b, n);
        }
        long acc = h ^ 0xa0761d6478bd642fL ^ (s * 0xe7037ed1a0b428dbL);
        int i = 0;
        for (; i + 8 <= n; i += 8) {
            long w = (b[i] & 0xFFL) | (b[i + 1] & 0xFFL) << 8 | (b[i + 2] & 0xFFL) << 16 | (b[i + 3] & 0xFFL) << 24
                            | (b[i + 4] & 0xFFL) << 32 | (b[i + 5] & 0xFFL) << 40 | (b[i + 6] & 0xFFL) << 48 | (b[i + 7] & 0xFFL) << 56;
            acc = mix(acc ^ w, 0x8ebc6af09c88c6e3L);
        }
        long tail = 0;
        for (int k = 0; i < n; i++, k += 8) {
            tail |= (b[i] & 0xFFL) << k;
        }
        return mix(acc ^ tail, 0x589965cc75374cc3L) ^ mix(acc, 0x1d8e4e27c47d124fL);
    }

    /**
     * {@code runtime.memequal(a, b unsafe.Pointer, size uintptr) bool}: the {@code size} bytes at
     * {@code a} and {@code b} are equal (what code outside std reaches by {@code //go:linkname}).
     */
    @TruffleBoundary
    static boolean runtime_memequal(Node node, Object a, Object b, long size) {
        int n = (int) Math.min(size, Integer.MAX_VALUE);
        if (n == 0 || graalgo.go.values.Ptr.same(a, b)) {
            return true;
        }
        byte[] x = new byte[n], y = new byte[n];
        graalgo.go.runtime.Unsafe.readBytes(node, graalgo.go.runtime.Unsafe.referent(a), graalgo.go.runtime.Unsafe.offset(a), x, n);
        graalgo.go.runtime.Unsafe.readBytes(node, graalgo.go.runtime.Unsafe.referent(b), graalgo.go.runtime.Unsafe.offset(b), y, n);
        return java.util.Arrays.equals(x, y);
    }

    private static long mix(long a, long b) {
        return Math.multiplyHigh(a, b) ^ (a * b);
    }

    // ------------------------------------------------------------------ runtime flags of goroutines

    /** {@code crypto/fips140.setBypass} (gc: {@code getg().fipsOnlyBypass = true}). */
    static void crypto_fips140_setBypass(Node node) {
        graalgo.go.runtime.G.current(node).fipsOnlyBypass = true;
    }

    /** {@code crypto/fips140.unsetBypass}. */
    static void crypto_fips140_unsetBypass(Node node) {
        graalgo.go.runtime.G.current(node).fipsOnlyBypass = false;
    }

    /** {@code crypto/fips140.isBypassed}. */
    static boolean crypto_fips140_isBypassed(Node node) {
        return graalgo.go.runtime.G.current(node).fipsOnlyBypass;
    }

    /** {@code crypto/internal/fips140.getIndicator} (gc: {@code getg().fipsIndicator}). */
    static long crypto_internal_fips140_getIndicator(Node node) {
        return graalgo.go.runtime.G.current(node).fipsIndicator & 0xFF;
    }

    /** {@code crypto/internal/fips140.setIndicator}. */
    static void crypto_internal_fips140_setIndicator(Node node, long indicator) {
        graalgo.go.runtime.G.current(node).fipsIndicator = (byte) indicator;
    }

    /** {@code runtime/debug.setPanicOnFault(bool) bool}: the goroutine's flag, the previous value returned. */
    static boolean runtime_debug_setPanicOnFault(Node node, boolean enabled) {
        graalgo.go.runtime.G g = graalgo.go.runtime.G.current(node);
        boolean old = g.panicOnFault;
        g.panicOnFault = enabled;
        return old;
    }

    /**
     * {@code crypto/subtle.setDITEnabled() bool}: gc enables data-independent timing on arm64
     * only ({@code sys.EnableDIT} is false elsewhere); on amd64 nothing changes.
     */
    static boolean crypto_subtle_setDITEnabled(Node node) {
        return false;
    }

    /** {@code crypto/subtle.setDITDisabled()}. */
    static void crypto_subtle_setDITDisabled(Node node) {
    }

    // ------------------------------------------------------------------ fatal errors

    /** {@code crypto/internal/fips140.fatal}: gc's {@code fatal}. */
    @TruffleBoundary
    static void crypto_internal_fips140_fatal(Node node, Str s) {
        throw GoProcess.get(node).fatal(s.toString());
    }

    /** {@code crypto/internal/sysrand.fatal}: gc's {@code fatal}. */
    @TruffleBoundary
    static void crypto_internal_sysrand_fatal(Node node, Str s) {
        throw GoProcess.get(node).fatal(s.toString());
    }

    /** {@code crypto/rand.fatal}: gc's {@code fatal}. */
    @TruffleBoundary
    static void crypto_rand_fatal(Node node, Str s) {
        throw GoProcess.get(node).fatal(s.toString());
    }

    /**
     * {@code runtime.breakpoint} ({@code runtime.Breakpoint}): gc executes a trap instruction
     * and the program dies of {@code SIGTRAP} (exit status 2).
     */
    @TruffleBoundary
    static void runtime_breakpoint(Node node) {
        throw GoProcess.get(node).signal("SIGTRAP: trace trap", "PC=0x0 m=0 sigcode=128");
    }

    /** Sets the cap and returns the previous setting; n &lt; 1 only queries. */
    @TruffleBoundary
    static long runtime_GOMAXPROCS(Node node, long n) {
        return SchedulerCore.current().setGomaxprocs((int) Math.max(Math.min(n, Integer.MAX_VALUE), 0));
    }

    @TruffleBoundary
    static long runtime_NumCPU(Node node) {
        return Runtime.getRuntime().availableProcessors();
    }

    /** gc's gcount: the goroutines that exist, blocked ones included, the runtime's own not. */
    @TruffleBoundary
    static long runtime_NumGoroutine(Node node) {
        return SchedulerCore.current().numGoroutines();
    }

    /** Terminates the calling goroutine after running its deferred calls (GG-0070). */
    static void runtime_Goexit(Node node) {
        graalgo.runtime.Goexit.goexit();
    }

    /**
     * {@code syscall.Exit}, which upstream's runtime provides by {@code //go:linkname} and
     * {@code os.Exit} ends in: the scheduler's exit request; the calling goroutine never returns.
     */
    @TruffleBoundary
    static void syscall_Exit(Node node, long code) {
        SchedulerCore sc = SchedulerCore.installedOrNull();
        if (sc == null || !sc.onGoroutine()) {
            throw new IllegalStateException("syscall.Exit outside a goroutine");
        }
        sc.requestExit((int) code);
        sc.freezeForExit();
        throw new IllegalStateException("syscall.Exit returned");
    }

    /**
     * {@code runtime.callers}: the return pcs of the calling goroutine's Go frames after
     * {@code skip} of them, into {@code pcbuf} (WP-TR-AUDIT-6, BUG-0047; {@link Stacks}).
     */
    @TruffleBoundary
    static long runtime_callers(Node node, long skip, Object pcbuf) {
        Slice buf = (Slice) pcbuf;
        if (buf.len() == 0) {
            return 0;
        }
        List<Stacks.Frame> frames = GoProcess.get(node).stacks.current((int) Math.max(0, Math.min(skip, Integer.MAX_VALUE)),
                buf.len());
        long[] prims = buf.base().prims;
        for (int i = 0; i < frames.size(); i++) {
            prims[buf.prim() + i] = frames.get(i).pc() + 1;
        }
        return frames.size();
    }

    /** {@code runtime.funcInfo}: the name and entry pc of the function containing {@code pc}. */
    @TruffleBoundary
    static Object runtime_funcInfo(Node node, long pc) {
        Stacks.Fn f = GoProcess.get(node).stacks.fnAt(GoProcess.get(node), pc);
        return f == null ? new Object[] {Str.EMPTY, 0L} : new Object[] {Str.of(f.name()), f.entry()};
    }

    /** {@code runtime.pcFileLine}: the file and line of {@code pc}. */
    @TruffleBoundary
    static Object runtime_pcFileLine(Node node, long pc) {
        GoProcess p = GoProcess.get(node);
        Stacks.Frame f = p.stacks.frameAt(p, pc);
        return f == null ? new Object[] {Str.EMPTY, 0L} : new Object[] {Str.of(f.file()), (long) f.line()};
    }

    /**
     * {@code runtime.Stack}: the calling goroutine's traceback, truncated to {@code buf}. With
     * {@code all}, gc appends every other goroutine's; their stacks are not walkable from here,
     * so only the caller's is reported.
     */
    @TruffleBoundary
    static long runtime_Stack(Node node, Object buf, boolean all) {
        Slice s = (Slice) buf;
        byte[] text = GoProcess.get(node).currentStack().getBytes(StandardCharsets.UTF_8);
        int n = Math.min(text.length, s.len());
        if (n > 0) {
            System.arraycopy(text, 0, s.base().bytes, s.byt(), n);
        }
        return n;
    }

    static long runtime_nanotime(Node node) {
        return System.nanoTime();
    }

    /** {@code runtime.typeString(t)}: upstream's {@code toRType(t).string()}. */
    @TruffleBoundary
    static Str runtime_typeString(Node node, Object t) {
        return Str.of(descriptorType(node, t).str());
    }

    /**
     * {@code runtime.typePkgPath(t)}: upstream's {@code toRType(t).pkgpath()} for the types a
     * failed assertion compares: a defined type's package path, "" for any other type.
     */
    @TruffleBoundary
    static Str runtime_typePkgPath(Node node, Object t) {
        GoType g = descriptorType(node, t);
        return g.isNamed() && g.pkg != null ? Str.of(g.pkg) : Str.EMPTY;
    }

    private static GoType descriptorType(Node node, Object t) {
        GoType g = Descriptors.typeOf(t);
        if (g == null) {
            throw Panics.fatal(node, "type descriptor not built by the runtime");
        }
        return g;
    }

    /**
     * The backend's value for a run-time error a Go package raises itself (the facade's
     * {@code divideError} and {@code overflowError}): the same {@code runtime.Error} a
     * backend-raised panic of that kind carries.
     */
    @TruffleBoundary
    static Object runtime_runtimeError(Node node, long code) {
        switch ((int) code) {
            case 0:
                return Panics.box(node, GoErrors.integerDivideByZero());
            case 1:
                // An errorString like the other; GoErrors names its factory after its first
                // user.
                return Panics.box(node, GoErrors.rangeState("integer overflow"));
            default:
                throw new IllegalStateException("runtime.runtimeError: unknown code " + code);
        }
    }
}
