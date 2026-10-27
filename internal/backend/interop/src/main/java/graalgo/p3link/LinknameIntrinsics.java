package graalgo.p3link;

import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import java.util.Map;

import graalgo.frontend.Json;
import graalgo.frontend.FrontendException;
import graalgo.frontend.NodeFactory;
import graalgo.iface.IfaceMethodTable;
import graalgo.iface.IfaceMethodTables;
import graalgo.p3os.GoOs;
import graalgo.p3reflect.ReflectMem;
import graalgo.p3net.GoHttp;
import graalgo.p3net.GoNet;
import graalgo.p3net.GoNetpoll;
import graalgo.p3tls.TlsRuntime;
import graalgo.p3unsafe.UnsafeTable;
import graalgo.p3x509.GoX509;
import graalgo.runtime.Intrinsics;
import graalgo.runtime.Linknames;
import graalgo.runtime.GoDebug;
import graalgo.runtime.Timers;
import graalgo.runtime.sync.GoSemaphore;
import graalgo.runtime.sync.GoPointerCell32;
import graalgo.runtime.sync.IntCell;
import graalgo.values.GoFunc;
import graalgo.values.GoPointer;
import graalgo.values.GoString;

/**
 * Phase-3 linkname installation (P3-M4, WS C, ADR-0004 D1/D3).
 *
 * <p>Both directions, by symbol name (SPEC §2.8):
 *
 * <ul>
 *   <li><b>Go → intrinsic.</b> The M4 gc-symbol table: gc symbol names
 *       from {@code //go:linkname} directives map to the intrinsic
 *       implementations, so identical Go source links against gc's
 *       runtime under gc and graalgo's intrinsics under graalgo:
 *       <ul>
 *         <li>{@code runtime.fastrand} → {@link Intrinsics#fastrand}
 *         <li>{@code runtime.fastrandn} → {@link Intrinsics#fastrandn}
 *         <li>{@code runtime.nanotime} → {@link Timers#nanotime}
 *       </ul>
 *       Arguments arrive in D5 boxed form; each target adapts explicitly
 *       (documented per target below).
 *   <li><b>Intrinsic → Go helper.</b>
 *       {@code graalgo.runtime.Linknames.callGo} invokes the Go function
 *       bound under a qualified Go name (see {@link #bindGoFuncs}).
 * </ul>
 *
 * <p>Also installs the {@link Intrinsics} itabFor provider (the P3-M3
 * itab machinery) and the interface method-name lists, and binds every
 * program function for {@code callGo}.
 *
 * <p>verifies: GG-0020
 */
public final class LinknameIntrinsics {
    private LinknameIntrinsics() {}

    /**
     * Installs everything linkname-related for one program build. Called
     * by the P3 launcher after {@code factory.buildProgram(json)} (and
     * after {@code IfaceMethodTable.install}, which this does not repeat).
     */
    public static void install(Json.Object doc, NodeFactory.Program program) {
        // P3-M4 remediation (P2-2): the static registries must be cleared
        // on every install — the launcher calls install per program build
        // (even empty), and without this a previous program's symbols,
        // intrinsics, and Go bindings would leak into this run.
        Linknames.clear();
        GoDebug.clear();
        SyncRuntime.clear();
        // Go → intrinsic: the M4 gc-symbol table. D5 argument types:
        // fastrandn takes an int32 (Integer).
        Linknames.register("runtime.fastrand", args -> Intrinsics.fastrand(), 0);
        Linknames.register("runtime.fastrandn",
                args -> Intrinsics.fastrandn(((Number) args[0]).intValue()), 1);
        Linknames.register("runtime.nanotime", args -> Timers.nanotime(), 0);
        // Intrinsic → Go helper by symbol name. D5: name arrives as
        // GoString; the single any-typed argument passes through boxed.
        Linknames.register("graalgo.runtime.Linknames.callGo", args -> {
            String name = new String(((GoString) args[0]).bytes(), StandardCharsets.UTF_8);
            Object[] rest = Arrays.copyOfRange(args, 1, args.length);
            return Linknames.callGo(name, rest);
        }, 2);
        // P3-M8 (wave 2): the synthetic os package's intrinsic table
        // (os.md §2.4; the symbol table is frozen — the synthetic
        // syscall worker binds to it). D5 argument types: strings
        // arrive as GoString, int64 as Long, []byte as GoSlice; each
        // GoOs target adapts explicitly (see GoOs's javadoc for the
        // calling conventions: -errno, n+1, one channel op per call).
        registerOsIntrinsics();
        // P3-M9 (RISK-M9-1/RISK-M9-2): the netpoller (internal/poll.*) and
        // socket-trap (graalgo.net.*) intrinsic tables. D5 argument types:
        // integers arrive as Long (int/uintptr are 64-bit), []byte as
        // GoSlice; each GoNet/GoNetpoll target adapts explicitly (see
        // their javadoc for the calling conventions).
        registerNetIntrinsics();
        // P3-M9 (tier-5): x509 certificate parsing (java.security.cert
        // delegation) and TLS (SSLEngine delegation) intrinsic tables.
        // D5 argument types: []byte arrives as GoSlice, strings as
        // GoString, ints/bools as Long; the X509Certificate passes through
        // as Object; each target adapts explicitly.
        registerX509Intrinsics();
        registerTlsIntrinsics();
        // P3-M9 (tier-5): the HTTP client intrinsic (java.net.http
        // delegation) for the synthetic net/http Transport.RoundTrip.
        // D5 argument types: method and URL arrive as GoString, headers
        // as GoSlice of GoString (flat k/v pairs), body as GoSlice of
        // bytes; the target adapts explicitly (see GoHttp's javadoc for
        // the calling convention).
        registerHttpIntrinsics();
        // itabFor → the P3-M3 itab machinery (ADR-0004 D3).
        Intrinsics.setItabProvider(IfaceMethodTable::itabForDescriptors);
        // Interface method-name lists for itabForIntrinsic.
        IfaceMethodTable.installInterfaces(IfaceMethodTables.resolveInterfaces(doc));
        // Go function bindings for callGo.
        bindGoFuncs(doc, program);
        // fix-linkname (Caddy/godebug lane): the runtime provider
        // intrinsics for internal/godebug's //go:linkname consumers
        // (runtime.write, godebug setUpdate/registerMetric/
        // setNewIncNonDefault).
        registerGodebugIntrinsics();
        // real-sync lane: runtime semaphore intrinsics for the real
        // sync package's runtime_Semacquire/runtime_Semrelease (and
        // variants). These are declared bodyless in sync/runtime.go;
        // gc's runtime provides them via //go:linkname. The frontend
        // emits them as linknames (pkgpath.name); the backend implements
        // them here with scheduler-aware blocking.
        registerSyncIntrinsics();
        // real-sync lane (PKG-012): the remaining bodyless decls of the
        // real sync (and sync/atomic) packages that gc's runtime provides
        // via //go:linkname — pool-cleanup registration, P pinning, the
        // notifyList ticket lock, and sync.throw/fatal. Same mechanism as
        // registerSyncIntrinsics: the gc symbol table, implemented by
        // SyncRuntime with gc-faithful semantics (see its javadoc for the
        // JVM adaptations).
        registerSyncRuntimeIntrinsics();
        // real-sync lane (PKG-012 follow-up): internal/synctest's
        // //go:linkname stubs, provided by gc's runtime
        // (src/runtime/synctest.go). The real sync.WaitGroup calls
        // IsInBubble/Associate/Disassociate/IsAssociated; without these
        // any WaitGroup use fails at link time. graalgo implements no
        // synctest bubbles — see Synctest for the honest no-bubble
        // semantics.
        registerSynctestIntrinsics();
        // real-sync lane (PKG-012 follow-up): internal/sync's
        // //go:linkname stubs, provided by gc's runtime
        // (src/runtime/sema.go, src/runtime/proc.go, src/runtime/panic.go).
        // sync.Mutex embeds isync.Mutex, so any contended Mutex needs
        // these (runtime_canSpin is only reached on the slow path — an
        // uncontended Lock/Unlock never calls it, which is why the gap
        // stayed hidden until now).
        registerInternalSyncIntrinsics();
        // fix-redmaster-r28 (Fix 4): reflect's runtime-provided memory
        // primitives (unsafe_New, typedmemmove) — see
        // registerReflectMemIntrinsics.
        registerReflectMemIntrinsics();
    }

    /**
     * The runtime provider side of {@code internal/godebug}'s
     * {@code //go:linkname} consumers (fix-linkname, Caddy/godebug lane).
     *
     * <p>In gc, package runtime provides these symbols:
     * <ul>
     *   <li>{@code runtime.write(fd, p, n)} — raw fd write used by
     *       godebug's {@code runtimeStderr};</li>
     *   <li>{@code internal/godebug.setUpdate} — provided by
     *       {@code runtime.godebug_setUpdate};</li>
     *   <li>{@code internal/godebug.registerMetric} — provided by
     *       {@code runtime.registerMetric} (metrics.go);</li>
     *   <li>{@code internal/godebug.setNewIncNonDefault} — provided by
     *       {@code runtime.setNewIncNonDefault} (runtime.go).</li>
     * </ul>
     * The Go-callable state lives in {@link GoDebug};
     * {@code runtime.write} is implemented here (it needs the interop
     * {@code UnsafeTable}).
     */
    private static void registerGodebugIntrinsics() {
        Linknames.register("runtime.write", args -> runtimeWrite(args[0], args[1], args[2]), 3);
        Linknames.register("internal/godebug.setUpdate", args -> {
            if (!(args[0] instanceof GoFunc f)) {
                throw new FrontendException("godebug.setUpdate: expected a func argument");
            }
            Object env = GoOs.getenv(GoString.fromUtf8("GODEBUG"));
            String envStr = env instanceof GoString gs
                    ? new String(gs.bytes(), StandardCharsets.UTF_8) : "";
            GoDebug.setUpdate(f, envStr);
            return null;
        }, 1);
        Linknames.register("internal/godebug.registerMetric", args -> {
            if (!(args[0] instanceof GoString gs) || !(args[1] instanceof GoFunc f)) {
                throw new FrontendException(
                        "godebug.registerMetric: expected (string, func) arguments");
            }
            GoDebug.registerMetric(new String(gs.bytes(), StandardCharsets.UTF_8), f);
            return null;
        }, 2);
        Linknames.register("internal/godebug.setNewIncNonDefault", args -> {
            if (!(args[0] instanceof GoFunc f)) {
                throw new FrontendException(
                        "godebug.setNewIncNonDefault: expected a func argument");
            }
            GoDebug.setNewIncNonDefault(f);
            return null;
        }, 1);
    }

    /**
     * Runtime semaphore intrinsics for the real {@code sync} package
     * (real-sync lane).
     *
     * <p>The real {@code sync} package (go-1.27.1, sync/runtime.go)
     * declares bodyless functions like {@code func runtime_Semacquire(s
     * *uint32)}; gc's runtime provides them via {@code //go:linkname}
     * (src/runtime/sema.go). The frontend emits these as linknames
     * with symbol {@code pkgpath.name} (e.g.,
     * {@code "sync.runtime_Semacquire"}); they dispatch here.
     *
     * <p>D5 argument types: the {@code *uint32} arrives as a Long
     * (address) or as a {@link GoPointer} (a field address such as
     * {@code &wg.sema} or {@code &mu.state}); {@link #semCell} accepts
     * both carriers, booleans as Boolean/Long; ints as Long/Integer.
     * Each target adapts explicitly.
     */
    /**
     * The {@link IntCell} view of a semaphore word argument (fix-redmaster-r29).
     * A {@code *uint32} arrives either as a raw address Long (an unsafe-derived
     * address, resolved through the {@link UnsafeTable}) or as a
     * {@link GoPointer} (a field address such as {@code &wg.sema}, whose
     * storage is the pointer's referent cell). Both carriers denote the same
     * word; anything else fails closed instead of casting blindly (the old
     * code assumed Long unconditionally and died with a ClassCastException
     * on every WaitGroup.Wait / RWMutex path that passed a GoPointer).
     */
    private static IntCell semCell(Object arg) {
        if (arg instanceof Number n) {
            return new AddressIntCell(n.longValue());
        }
        if (arg instanceof GoPointer p) {
            return new GoPointerCell32(p);
        }
        throw new FrontendException(
                "sync semaphore: *uint32 argument is neither an address nor a pointer: "
                        + (arg == null ? "null" : arg.getClass().getSimpleName()));
    }

    private static void registerSyncIntrinsics() {
        // sync.runtime_Semacquire(s *uint32): wait until *s > 0, then
        // atomically decrement.
        Linknames.register("sync.runtime_Semacquire", args -> {
            GoSemaphore.acquire(semCell(args[0]));
            return null;
        }, 1);
        // sync.runtime_Semrelease(s *uint32, handoff bool, skipframes int):
        // atomically increment *s, wake one waiter if any. The handoff
        // and skipframes args affect profiling/tracing only; the
        // semaphore semantics are identical.
        Linknames.register("sync.runtime_Semrelease", args -> {
            GoSemaphore.release(semCell(args[0]));
            return null;
        }, 3);
        // sync.runtime_SemacquireRWMutexR(s *uint32, lifo bool, skipframes int)
        // sync.runtime_SemacquireRWMutex(s *uint32, lifo bool, skipframes int)
        // Functionally identical to Semacquire; lifo/skipframes affect
        // queueing order and profiling only.
        Linknames.register("sync.runtime_SemacquireRWMutexR", args -> {
            GoSemaphore.acquire(semCell(args[0]));
            return null;
        }, 3);
        Linknames.register("sync.runtime_SemacquireRWMutex", args -> {
            GoSemaphore.acquire(semCell(args[0]));
            return null;
        }, 3);
        // sync.runtime_SemacquireWaitGroup(s *uint32, synctestDurable bool):
        // like Semacquire, for WaitGroup.Wait.
        Linknames.register("sync.runtime_SemacquireWaitGroup", args -> {
            GoSemaphore.acquireWaitGroup(semCell(args[0]));
            return null;
        }, 2);
    }

    /**
     * Runtime provider surface for the real {@code sync} and
     * {@code sync/atomic} packages (PKG-012, real-sync lane).
     *
     * <p>In gc, the bodyless declarations in {@code sync/runtime.go} and
     * {@code sync/pool.go} (plus {@code sync/atomic/value.go}'s
     * {@code runtime_procPin}/{@code runtime_procUnpin}) are provided by
     * the runtime via {@code //go:linkname} ({@code src/runtime/sema.go},
     * {@code src/runtime/mgc.go}, {@code src/runtime/proc.go},
     * {@code src/runtime/panic.go} in go1.27.1). The frontend emits each
     * as a linkname call with symbol {@code <pkgpath>.<name>} (e.g.
     * {@code "sync.runtime_registerPoolCleanup"}); they dispatch here.
     * Semantics live in {@link SyncRuntime} — this method is only the gc
     * symbol table, following the {@link #registerSyncIntrinsics} pattern.
     *
     * <p>D5 argument types: {@code uint32}/{@code uintptr}/{@code int} as
     * Number; {@code func()} as {@link GoFunc}; {@code string} as
     * {@link GoString}; {@code *notifyList} as {@link GoPointer} (D5:
     * pointers are opaque Java references — SyncRuntime keys by storage
     * identity); {@code unsafe.Pointer} as a Long (address). Each target
     * adapts explicitly.
     */
    private static void registerSyncRuntimeIntrinsics() {
        // sync.runtime_registerPoolCleanup(cleanup func()): called once by
        // sync.init(). gc stores it and runs it at every GC (clearpools);
        // graalgo stores it without auto-invoking (no STW hook on the JVM
        // — see SyncRuntime's javadoc). The Pool contract permits never
        // evicting, so this is behavior-preserving.
        Linknames.register("sync.runtime_registerPoolCleanup", args -> {
            if (!(args[0] instanceof GoFunc f)) {
                throw new FrontendException(
                        "sync.runtime_registerPoolCleanup: expected a func argument");
            }
            SyncRuntime.registerPoolCleanup(f);
            return null;
        }, 1);
        // P pinning. gc's runtime provides both spellings from the same
        // procPin/procUnpin (src/runtime/proc.go).
        Linknames.register("sync.runtime_procPin",
                args -> (long) SyncRuntime.procPin(), 0);
        Linknames.register("sync.runtime_procUnpin", args -> {
            SyncRuntime.procUnpin();
            return null;
        }, 0);
        Linknames.register("sync/atomic.runtime_procPin",
                args -> (long) SyncRuntime.procPin(), 0);
        Linknames.register("sync/atomic.runtime_procUnpin", args -> {
            SyncRuntime.procUnpin();
            return null;
        }, 0);
        // The notifyList ticket lock behind sync.Cond. The *notifyList
        // arrives as a GoPointer (D5: pointers are opaque Java
        // references); SyncRuntime keys state by its storage identity.
        Linknames.register("sync.runtime_notifyListAdd", args ->
                SyncRuntime.notifyListAdd(args[0]), 1);
        Linknames.register("sync.runtime_notifyListWait", args -> {
            SyncRuntime.notifyListWait(args[0], ((Number) args[1]).intValue());
            return null;
        }, 2);
        Linknames.register("sync.runtime_notifyListNotifyOne", args -> {
            SyncRuntime.notifyListNotifyOne(args[0]);
            return null;
        }, 1);
        Linknames.register("sync.runtime_notifyListNotifyAll", args -> {
            SyncRuntime.notifyListNotifyAll(args[0]);
            return null;
        }, 1);
        // Called by sync.init(): the frontend constant-folds the
        // unsafe.Sizeof argument via go/types.
        Linknames.register("sync.runtime_notifyListCheck", args -> {
            SyncRuntime.notifyListCheck(((Number) args[0]).longValue());
            return null;
        }, 1);
        // gc's runtime.throw / runtime.fatal (src/runtime/panic.go).
        Linknames.register("sync.throw", args -> {
            SyncRuntime.syncThrow(args[0]);
            return null;
        }, 1);
        Linknames.register("sync.fatal", args -> {
            SyncRuntime.syncFatal(args[0]);
            return null;
        }, 1);
    }

    /**
     * Runtime provider for {@code internal/sync}'s {@code //go:linkname}
     * stubs (PKG-012 follow-up, real-sync lane).
     *
     * <p>{@code sync.Mutex} embeds {@code isync.Mutex}
     * ({@code src/internal/sync/mutex.go}), whose slow path calls
     * {@code runtime_canSpin}/{@code runtime_doSpin} and
     * {@code runtime_SemacquireMutex}/{@code runtime_Semrelease}; gc's
     * runtime provides all of these plus {@code runtime_nanotime},
     * {@code throw}, and {@code fatal} via {@code //go:linkname}
     * ({@code src/runtime/sema.go}, {@code src/runtime/proc.go},
     * {@code src/runtime/panic.go} in go1.27.1). The frontend emits each
     * as a linkname call with symbol {@code "internal/sync.<name>"}; they
     * dispatch here. Semantics live in {@link SyncRuntime} (semaphores
     * reuse {@link GoSemaphore}; nanotime reuses {@link Timers}).
     *
     * <p>D5 argument types: {@code *uint32} arrives as a Long (address);
     * {@code int} as Number; {@code string} as {@link GoString}.
     */
    private static void registerInternalSyncIntrinsics() {
        // SemacquireMutex/Semrelease: same wakeup and mutual-exclusion
        // semantics as the sync spellings (only profiling/wait-reason differ
        // in gc), but NOT gc's lifo queueing: gc's semacquire(lifo=true) —
        // which sync.Mutex passes via internal_sync_runtime_SemacquireMutex
        // (src/runtime/sema.go) — enqueues the waiter at the queue head so
        // the most recent waiter is served next, while GoSemaphore.acquire
        // is FIFO. Ignoring lifo is correctness-safe (no waiter is ever lost
        // or spuriously granted) but the fairness approximation differs from
        // gc: under contention a mutex waiter can wait longer than it would
        // under gc. Documented, not denied.
        Linknames.register("internal/sync.runtime_SemacquireMutex", args -> {
            GoSemaphore.acquire(semCell(args[0]));
            return null;
        }, 3);
        Linknames.register("internal/sync.runtime_Semrelease", args -> {
            GoSemaphore.release(semCell(args[0]));
            return null;
        }, 3);
        // Cooperative spinning for sync.Mutex's slow path.
        Linknames.register("internal/sync.runtime_canSpin", args ->
                SyncRuntime.canSpin(((Number) args[0]).intValue()), 1);
        Linknames.register("internal/sync.runtime_doSpin", args -> {
            SyncRuntime.doSpin();
            return null;
        }, 0);
        // The internal/sync spelling of the nanotime intrinsic.
        Linknames.register("internal/sync.runtime_nanotime",
                args -> Timers.nanotime(), 0);
        // gc's runtime.throw / runtime.fatal (src/runtime/panic.go).
        Linknames.register("internal/sync.throw", args -> {
            SyncRuntime.syncThrow(args[0]);
            return null;
        }, 1);
        Linknames.register("internal/sync.fatal", args -> {
            SyncRuntime.syncFatal(args[0]);
            return null;
        }, 1);
    }

    /** Adapts a D5-boxed Go pointer/unsafe.Pointer argument to a raw address. */
    private static long addrArg(Object arg, String symbol) {
        if (!(arg instanceof Number n)) {
            throw new FrontendException(symbol + ": expected a pointer argument");
        }
        return n.longValue();
    }

    /**
     * Runtime provider for {@code internal/synctest}'s
     * {@code //go:linkname} stubs (PKG-012 follow-up, real-sync lane).
     *
     * <p>The real {@code sync.WaitGroup} (go1.27.1
     * {@code src/sync/waitgroup.go}) calls {@code synctest.IsInBubble},
     * {@code synctest.Associate}, {@code synctest.Disassociate}, and
     * {@code synctest.IsAssociated}; gc's runtime provides all nine
     * stubs in {@code src/internal/synctest/synctest.go} via
     * {@code //go:linkname} from {@code src/runtime/synctest.go}. The
     * frontend emits each as a linkname call with symbol
     * {@code "internal/synctest.<name>"}; they dispatch here. Semantics
     * live in {@link Synctest} — graalgo implements no synctest bubbles,
     * so every entry is the honest no-bubble answer (see its javadoc).
     *
     * <p>D5 argument types: {@code unsafe.Pointer} arrives as a Long
     * (address); {@code func()} as {@link GoFunc}; {@code any} boxed.
     */
    private static void registerSynctestIntrinsics() {
        Linknames.register("internal/synctest.IsInBubble",
                args -> Synctest.isInBubble(), 0);
        Linknames.register("internal/synctest.associate", args ->
                Synctest.associate(addrArg(args[0], "internal/synctest.associate")), 1);
        Linknames.register("internal/synctest.disassociate", args -> {
            Synctest.disassociate(addrArg(args[0], "internal/synctest.disassociate"));
            return null;
        }, 1);
        Linknames.register("internal/synctest.isAssociated", args ->
                Synctest.isAssociated(addrArg(args[0], "internal/synctest.isAssociated")), 1);
        Linknames.register("internal/synctest.acquire",
                args -> Synctest.acquire(), 0);
        Linknames.register("internal/synctest.release", args -> {
            Synctest.release(args[0]);
            return null;
        }, 1);
        Linknames.register("internal/synctest.inBubble", args -> {
            Synctest.inBubble(args[0], args[1]);
            return null;
        }, 2);
        Linknames.register("internal/synctest.Run", args -> {
            Synctest.run(args[0]);
            return null;
        }, 1);
        Linknames.register("internal/synctest.Wait", args -> {
            Synctest.waitForBubble();
            return null;
        }, 0);
    }

    /**
     * {@code runtime.write(fd uintptr, p unsafe.Pointer, n int32) int32}:
     * writes n bytes from p to a raw file descriptor. Only the stdio fds
     * are wired (1 → stdout, 2 → stderr); anything else returns
     * {@code -EBADF}, and an I/O failure returns {@code -EIO} (gc: the
     * negative errno).
     */
    private static Object runtimeWrite(Object fdArg, Object pArg, Object nArg) {
        long fd = ((Number) fdArg).longValue();
        long p = ((Number) pArg).longValue();
        int n = ((Number) nArg).intValue();
        if (n == 0) {
            return 0;
        }
        if (p == 0 || n < 0) {
            throw new FrontendException("runtime.write: nil pointer or negative length");
        }
        Object ref = UnsafeTable.current().resolve(p);
        if (!(ref instanceof UnsafeTable.ArrayRef ar) || !(ar.array() instanceof byte[] bytes)) {
            throw new FrontendException(
                    "runtime.write: pointer does not address a byte array (got "
                            + (ref == null ? "null" : ref.getClass().getSimpleName()) + ")");
        }
        int off = ar.offset();
        if (off < 0 || off + n > bytes.length) {
            throw new FrontendException("runtime.write: range out of bounds");
        }
        try {
            if (fd == 1) {
                System.out.write(bytes, off, n);
                System.out.flush();
                if (System.out.checkError()) {
                    return -5; // -EIO
                }
            } else if (fd == 2) {
                System.err.write(bytes, off, n);
                System.err.flush();
                if (System.err.checkError()) {
                    return -5; // -EIO
                }
            } else {
                return -9; // -EBADF
            }
        } catch (RuntimeException e) {
            return -5; // -EIO
        }
        return n;
    }

    /**
     * Registers the 46 {@code graalgo.os.*} intrinsics implemented by
     * {@link GoOs} (P3-M8 wave 2, os.md §2.4). Arity is the __-helper
     * argument count from synth_os.go.
     */
    private static void registerOsIntrinsics() {
        // Files: open/close/read/write/readAt/writeAt/seek/truncate/sync.
        Linknames.register("graalgo.os.open", args -> GoOs.open(args[0], args[1], args[2]), 3);
        Linknames.register("graalgo.os.close", args -> GoOs.close(args[0]), 1);
        Linknames.register("graalgo.os.read", args -> GoOs.read(args[0], args[1]), 2);
        Linknames.register("graalgo.os.write", args -> GoOs.write(args[0], args[1]), 2);
        Linknames.register("graalgo.os.readAt", args -> GoOs.readAt(args[0], args[1], args[2]), 3);
        Linknames.register("graalgo.os.writeAt",
                args -> GoOs.writeAt(args[0], args[1], args[2]), 3);
        Linknames.register("graalgo.os.seek", args -> GoOs.seek(args[0], args[1], args[2]), 3);
        Linknames.register("graalgo.os.truncate", args -> GoOs.truncate(args[0], args[1]), 2);
        Linknames.register("graalgo.os.sync", args -> GoOs.sync(args[0]), 1);
        // Pipes + stdio + handle kinds.
        Linknames.register("graalgo.os.pipe", args -> GoOs.pipe(), 0);
        Linknames.register("graalgo.os.stdio", args -> GoOs.stdio(args[0]), 1);
        Linknames.register("graalgo.os.handleKind", args -> GoOs.handleKind(args[0]), 1);
        // Stat.
        Linknames.register("graalgo.os.statMode", args -> GoOs.statMode(args[0], args[1]), 2);
        Linknames.register("graalgo.os.statSize", args -> GoOs.statSize(args[0], args[1]), 2);
        Linknames.register("graalgo.os.statMtimeNs",
                args -> GoOs.statMtimeNs(args[0], args[1]), 2);
        Linknames.register("graalgo.os.statDev", args -> GoOs.statDev(args[0], args[1]), 2);
        Linknames.register("graalgo.os.statIno", args -> GoOs.statIno(args[0], args[1]), 2);
        // Directory iteration.
        Linknames.register("graalgo.os.dirNext", args -> GoOs.dirNext(args[0], args[1]), 2);
        // Path operations.
        Linknames.register("graalgo.os.mkdir", args -> GoOs.mkdir(args[0], args[1]), 2);
        Linknames.register("graalgo.os.remove", args -> GoOs.remove(args[0]), 1);
        Linknames.register("graalgo.os.rename", args -> GoOs.rename(args[0], args[1]), 2);
        Linknames.register("graalgo.os.symlink", args -> GoOs.symlink(args[0], args[1]), 2);
        Linknames.register("graalgo.os.readlink", args -> GoOs.readlink(args[0], args[1]), 2);
        Linknames.register("graalgo.os.link", args -> GoOs.link(args[0], args[1]), 2);
        Linknames.register("graalgo.os.chmod", args -> GoOs.chmod(args[0], args[1]), 2);
        Linknames.register("graalgo.os.chtimes",
                args -> GoOs.chtimes(args[0], args[1], args[2]), 3);
        Linknames.register("graalgo.os.truncatePath",
                args -> GoOs.truncatePath(args[0], args[1]), 2);
        // Virtual cwd.
        Linknames.register("graalgo.os.getcwd", args -> GoOs.getcwd(), 0);
        Linknames.register("graalgo.os.chdir", args -> GoOs.chdir(args[0]), 1);
        // Environment.
        Linknames.register("graalgo.os.getenvFound", args -> GoOs.getenvFound(args[0]), 1);
        Linknames.register("graalgo.os.getenv", args -> GoOs.getenv(args[0]), 1);
        Linknames.register("graalgo.os.setenv", args -> GoOs.setenv(args[0], args[1]), 2);
        Linknames.register("graalgo.os.unsetenv", args -> GoOs.unsetenv(args[0]), 1);
        Linknames.register("graalgo.os.clearenv", args -> GoOs.clearenv(), 0);
        Linknames.register("graalgo.os.environ", args -> GoOs.environ(), 0);
        // Identity.
        Linknames.register("graalgo.os.getpid", args -> GoOs.getpid(), 0);
        Linknames.register("graalgo.os.getppid", args -> GoOs.getppid(), 0);
        Linknames.register("graalgo.os.getuid", args -> GoOs.getuid(), 0);
        Linknames.register("graalgo.os.geteuid", args -> GoOs.geteuid(), 0);
        Linknames.register("graalgo.os.getgid", args -> GoOs.getgid(), 0);
        Linknames.register("graalgo.os.getegid", args -> GoOs.getegid(), 0);
        Linknames.register("graalgo.os.getgroups", args -> GoOs.getgroups(), 0);
        // Hostname, exit, argv.
        Linknames.register("graalgo.os.hostname", args -> GoOs.hostname(args[0]), 1);
        Linknames.register("graalgo.os.exit", args -> GoOs.exit(args[0]), 1);
        Linknames.register("graalgo.os.argc", args -> GoOs.argc(), 0);
        Linknames.register("graalgo.os.argv", args -> GoOs.argv(args[0]), 1);
        // P3-M8 (crypto/rand): entropy source.
        Linknames.register("graalgo.os.getrandom", args -> GoOs.getrandom(args[0]), 1);
    }

    /**
     * Registers the {@code internal/poll.*} netpoller linknames
     * (implemented by {@link GoNetpoll}) and the {@code graalgo.net.*}
     * socket traps (implemented by {@link GoNet}).
     *
     * <p>Arity is the single-result __-helper argument count. The poll
     * linkname Go declarations are single-result by v1-frontend
     * necessity (the exporter rejects multi-result declarations, even
     * for linknames): {@code runtime_pollOpen} returns the pollDesc
     * cookie (0 on failure; errno via
     * {@code internal/poll.runtime_pollOpenErrno}).
     */
    private static void registerNetIntrinsics() {
        // Netpoller (internal/poll.runtime_poll*).
        Linknames.register("internal/poll.runtime_pollServerInit",
                args -> { GoNetpoll.pollServerInit(); return null; }, 0);
        Linknames.register("internal/poll.runtime_pollOpen",
                args -> GoNetpoll.pollOpen(args[0]), 1);
        Linknames.register("internal/poll.runtime_pollOpenErrno",
                args -> GoNetpoll.pollOpenErrno(), 0);
        Linknames.register("internal/poll.runtime_pollClose",
                args -> { GoNetpoll.pollClose(args[0]); return null; }, 1);
        Linknames.register("internal/poll.runtime_pollWait",
                args -> GoNetpoll.pollWait(args[0], args[1]), 2);
        Linknames.register("internal/poll.runtime_pollWaitCanceled",
                args -> { GoNetpoll.pollWaitCanceled(args[0], args[1]); return null; }, 2);
        Linknames.register("internal/poll.runtime_pollReset",
                args -> GoNetpoll.pollReset(args[0], args[1]), 2);
        Linknames.register("internal/poll.runtime_pollSetDeadline",
                args -> { GoNetpoll.pollSetDeadline(args[0], args[1], args[2]); return null; }, 3);
        Linknames.register("internal/poll.runtime_pollUnblock",
                args -> { GoNetpoll.pollUnblock(args[0]); return null; }, 1);
        Linknames.register("internal/poll.runtime_isPollServerDescriptor",
                args -> GoNetpoll.isPollServerDescriptor(args[0]), 1);
        // Socket traps (graalgo.net.*).
        Linknames.register("graalgo.net.socket",
                args -> GoNet.socket(args[0], args[1], args[2]), 3);
        Linknames.register("graalgo.net.bind",
                args -> GoNet.bind(args[0], args[1]), 2);
        Linknames.register("graalgo.net.listen",
                args -> GoNet.listen(args[0], args[1]), 2);
        Linknames.register("graalgo.net.accept",
                args -> GoNet.accept(args[0]), 1);
        Linknames.register("graalgo.net.connect",
                args -> GoNet.connect(args[0], args[1]), 2);
        Linknames.register("graalgo.net.setsockopt",
                args -> GoNet.setsockopt(args[0], args[1], args[2], args[3]), 4);
        Linknames.register("graalgo.net.getsockopt",
                args -> GoNet.getsockopt(args[0], args[1], args[2]), 3);
        Linknames.register("graalgo.net.getsockname",
                args -> GoNet.getsockname(args[0], args[1]), 2);
        Linknames.register("graalgo.net.getpeername",
                args -> GoNet.getpeername(args[0], args[1]), 2);
        Linknames.register("graalgo.net.shutdown",
                args -> GoNet.shutdown(args[0], args[1]), 2);
        Linknames.register("graalgo.net.setnonblock",
                args -> GoNet.setnonblock(args[0], args[1]), 2);
        Linknames.register("graalgo.net.sendto",
                args -> GoNet.sendto(args[0], args[1], args[2]), 3);
        Linknames.register("graalgo.net.recvfrom",
                args -> GoNet.recvfrom(args[0], args[1], args[2]), 3);
        // P3-M9 (module-probe lane): interface enumeration for
        // net.Interface/Interfaces (github.com/google/uuid v1 node IDs).
        Linknames.register("graalgo.net.interfaces",
                args -> GoNet.interfaces(args[0]), 1);
    }

    /**
     * Registers the 26 {@code graalgo.x509.*} intrinsics implemented by
     * {@link GoX509} (P3-M9 tier-5, x509.md §2). All take a single argument
     * (the X509Certificate as Object, or DER bytes as GoSlice) and return
     * the D5-boxed field value ([]byte as GoSlice, strings as GoString,
     * ints as Long, null on absent/error).
     */
    private static void registerX509Intrinsics() {
        Linknames.register("graalgo.x509.parse",
                args -> GoX509.parse(args[0]), 1);
        Linknames.register("graalgo.x509.serialNumber",
                args -> GoX509.serialNumber(args[0]), 1);
        Linknames.register("graalgo.x509.subject",
                args -> GoX509.subject(args[0]), 1);
        Linknames.register("graalgo.x509.issuer",
                args -> GoX509.issuer(args[0]), 1);
        Linknames.register("graalgo.x509.notBefore",
                args -> GoX509.notBefore(args[0]), 1);
        Linknames.register("graalgo.x509.notAfter",
                args -> GoX509.notAfter(args[0]), 1);
        Linknames.register("graalgo.x509.version",
                args -> GoX509.version(args[0]), 1);
        Linknames.register("graalgo.x509.sigAlgOid",
                args -> GoX509.sigAlgOid(args[0]), 1);
        Linknames.register("graalgo.x509.signature",
                args -> GoX509.signature(args[0]), 1);
        Linknames.register("graalgo.x509.tbsCertificate",
                args -> GoX509.tbsCertificate(args[0]), 1);
        Linknames.register("graalgo.x509.keyUsage",
                args -> GoX509.keyUsage(args[0]), 1);
        Linknames.register("graalgo.x509.basicConstraints",
                args -> GoX509.basicConstraints(args[0]), 1);
        Linknames.register("graalgo.x509.subjectKeyId",
                args -> GoX509.subjectKeyId(args[0]), 1);
        Linknames.register("graalgo.x509.authorityKeyId",
                args -> GoX509.authorityKeyId(args[0]), 1);
        Linknames.register("graalgo.x509.publicKeyAlg",
                args -> GoX509.publicKeyAlgOid(args[0]), 1);
        Linknames.register("graalgo.x509.publicKeyEncoded",
                args -> GoX509.publicKeyEncoded(args[0]), 1);
        Linknames.register("graalgo.x509.rawIssuer",
                args -> GoX509.rawIssuer(args[0]), 1);
        Linknames.register("graalgo.x509.rawSubject",
                args -> GoX509.rawSubject(args[0]), 1);
        Linknames.register("graalgo.x509.rsaModulus",
                args -> GoX509.rsaModulus(args[0]), 1);
        Linknames.register("graalgo.x509.rsaExponent",
                args -> GoX509.rsaExponent(args[0]), 1);
        Linknames.register("graalgo.x509.ecX",
                args -> GoX509.ecX(args[0]), 1);
        Linknames.register("graalgo.x509.ecY",
                args -> GoX509.ecY(args[0]), 1);
        Linknames.register("graalgo.x509.ecFieldSize",
                args -> GoX509.ecFieldSize(args[0]), 1);
        Linknames.register("graalgo.x509.ed25519Raw",
                args -> GoX509.ed25519Raw(args[0]), 1);
        Linknames.register("graalgo.x509.parseSpki",
                args -> GoX509.parseSpki(args[0]), 1);
        Linknames.register("graalgo.x509.parsePkcs1",
                args -> GoX509.parsePkcs1(args[0]), 1);
        Linknames.register("graalgo.x509.subjectAltNames",
                args -> GoX509.subjectAltNames(args[0]), 1);
        Linknames.register("graalgo.x509.parsePrivateKey",
                args -> GoX509.parsePrivateKey(args[0]), 1);
        Linknames.register("graalgo.x509.extKeyUsage",
                args -> GoX509.extKeyUsage(args[0]), 1);
    }

    /**
     * Registers the 11 {@code graalgo.tls.*} intrinsics implemented by
     * {@link TlsRuntime} (P3-M9 tier-5, SSLEngine delegation). The Go side
     * (synth_crypto_tls.go) drives the handshake state machine; these traps
     * only implement the cryptographic operations. Engine handles are longs
     * into TlsRuntime's table; []byte arrives as GoSlice; strings as
     * GoString; ints/bools as Long.
     */
    private static void registerTlsIntrinsics() {
        Linknames.register("graalgo.tls.newClientEngine",
                args -> TlsRuntime.newClientEngine(args[0], args[1], args[2],
                        args[3], args[4]), 5);
        Linknames.register("graalgo.tls.newServerEngine",
                args -> TlsRuntime.newServerEngine(args[0], args[1], args[2],
                        args[3], args[4], args[5]), 6);
        Linknames.register("graalgo.tls.beginHandshake",
                args -> TlsRuntime.beginHandshake(args[0]), 1);
        Linknames.register("graalgo.tls.engineStatus",
                args -> TlsRuntime.engineStatus(args[0]), 1);
        Linknames.register("graalgo.tls.wrap",
                args -> TlsRuntime.wrap(args[0], args[1]), 2);
        Linknames.register("graalgo.tls.unwrap",
                args -> TlsRuntime.unwrap(args[0], args[1]), 2);
        Linknames.register("graalgo.tls.hasPending",
                args -> TlsRuntime.hasPending(args[0]), 1);
        Linknames.register("graalgo.tls.runTasks",
                args -> TlsRuntime.runTasks(args[0]), 1);
        Linknames.register("graalgo.tls.peerCertificates",
                args -> TlsRuntime.peerCertificates(args[0]), 1);
        Linknames.register("graalgo.tls.connectionState",
                args -> TlsRuntime.connectionState(args[0]), 1);
        Linknames.register("graalgo.tls.close",
                args -> { TlsRuntime.close(args[0]); return null; }, 1);
    }

    /**
     * Registers the {@code graalgo.http.client.do} intrinsic implemented
     * by {@link GoHttp} (P3-M9 tier-5, java.net.http.HttpClient
     * delegation; the synthetic net/http Transport.RoundTrip drives it).
     * D5 argument types: method and URL arrive as GoString, headers as
     * GoSlice of GoString (flat k/v pairs), body as GoSlice of bytes
     * (see {@link GoHttp#clientDo} for the calling convention); returns
     * the documented GoSlice triple (status Long, header pairs, body
     * bytes) which the Go side reads as []interface{}.
     */
    private static void registerHttpIntrinsics() {
        Linknames.register("graalgo.http.client.do",
                args -> GoHttp.clientDo(args[0], args[1], args[2], args[3]), 4);
    }

    /**
     * Binds every top-level program function under its qualified Go name
     * ({@code <pkgpath>.<name>}, e.g. {@code main.helper}) for
     * intrinsic→Go lookup and for the Go→Go linkname fallback in
     * {@link Linknames#dispatch} (fix-linkname: cross-package symbol
     * resolution — a {@code //go:linkname} stub in one package resolves
     * to the provider function in another by symbol name, exactly like
     * gc's linker).
     *
     * <p>The package path comes from the funcdecl's additive
     * {@code "pkgpath"} field (emitted by the frontend for every
     * funcdecl); documents predating the field fall back to the document
     * package name (the previous behavior). Bodyless
     * {@code //go:linkname} stubs are <em>not</em> bound: a stub is a
     * consumer, not a provider — binding it would let
     * {@link Linknames#dispatch} recurse into the stub itself instead of
     * failing fast on a symbol with no provider (gc: undefined-symbol
     * link error). Method instantiations remain out of scope.
     */
    /**
     * fix-redmaster-r28 (Fix 4): reflect's runtime-provided memory
     * primitives. In gc these are bodyless declarations in reflect
     * ("implemented in package runtime"); graalgo resolves them through
     * the {@code UnimplementedRootNode} fallback, which dispatches to
     * the intrinsic registered here under {@code pkgpath.name}. The
     * implementations in {@link ReflectMem} are general over graalgo
     * carriers (not per-type special cases) and fail closed on shapes
     * they do not understand.
     */
    private static void registerReflectMemIntrinsics() {
        Linknames.register("reflect.unsafe_New", args -> ReflectMem.unsafeNew(args[0]), 1);
        Linknames.register("reflect.typedmemmove",
                args -> ReflectMem.typedmemmove(args[0], args[1], args[2]), 3);
    }

    static void bindGoFuncs(Json.Object doc, NodeFactory.Program program) {
        Json.Object pkg = doc.object("package", "document");
        String docPkgName = pkg.string("name", "document.package");
        Map<Integer, GoFunc> functions = program.functions();
        for (Json.Value f : doc.array("funcs", "document").items()) {
            if (!(f instanceof Json.Object fn)
                    || !"funcdecl".equals(fn.string("kind", "document.funcs"))) {
                continue;
            }
            Json.Object fields = fn.object("fields", "document.funcs");
            // Linkname stubs are consumers, never providers (see javadoc).
            if (fields.opt("linkname") instanceof Json.Str) {
                continue;
            }
            // fix-redmaster-r29: bodyless stubs are consumers, never
            // providers, for the same reason. A bodyless funcdecl builds
            // an UnimplementedRootNode GoFunc; binding it here makes
            // Linknames.hasSymbol true for its own symbol, so the stub's
            // Fix-4 dispatch arm invokes itself forever (16_generics /
            // 30_type_switch %T: StackOverflowError through
            // UnimplementedRootNode -> dispatch -> UnimplementedRootNode)
            // instead of taking the honest "unimplemented" panic. A
            // symbol with a real Java intrinsic still resolves through
            // the TARGETS arm; a symbol with neither keeps panicking.
            if (fields.opt("unimplemented") instanceof Json.Bool ub && ub.value()) {
                continue;
            }
            int objId = fields.intValue("obj", "document.funcs.fields");
            GoFunc gf = functions.get(objId);
            if (gf == null) {
                continue; // main's GoRootNode is not in functions()
            }
            String name = fields.string("name", "document.funcs.fields");
            String pkgPath = docPkgName;
            if (fields.opt("pkgpath") instanceof Json.Str pp) {
                pkgPath = pp.value();
            }
            Linknames.bindGo(pkgPath + "." + name, gf);
        }
    }
}
