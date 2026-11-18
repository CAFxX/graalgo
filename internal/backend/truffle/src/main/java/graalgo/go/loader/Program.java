package graalgo.go.loader;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.RootCallTarget;
import graalgo.gir.Gir;
import graalgo.gir.GirFile;
import graalgo.go.runtime.GoProcess;
import graalgo.go.runtime.RuntimeTypes;
import graalgo.go.types.TypeTable;
import graalgo.go.values.Flat;
import graalgo.language.GoLanguage;
import graalgo.sched.SchedulerCore;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * A loaded bundle (toolexec-design §4.2–4.3; truffle-design §2.2): the context-independent
 * result of {@code GoLanguage.parse} — package units in initialization order, the symbol
 * resolution between them and the intrinsic table. Running it in a context allocates the
 * globals, initializes every package and calls {@code main.main}.
 *
 * <p>Bundle wire format ({@code internal/toolexec/bundle}): {@code "GGBN" | version | runner |
 * main package | count | x* | modinfo | count | (import path | GIR)*}, unsigned varints and length-prefixed bytes.
 */
public final class Program {
    public final GoLanguage language;
    public final RuntimeTypes runtime;
    public final TypeTable types;
    private final Map<String, PackageUnit> byPath = new HashMap<>();
    /** Packages in initialization order. */
    private final List<PackageUnit> initOrder = new ArrayList<>();
    private PackageUnit main;
    private final Map<String, RootCallTarget> intrinsics = new HashMap<>();

    private Program(GoLanguage language) {
        this.language = language;
        this.runtime = language.runtimeTypes();
        this.types = runtime.table;
        // The intrinsic registry until the facade runtime (toolexec W5) provides the package
        // surface: keyed by gc symbol, reached by a body-less function's linkname or, for a
        // package the bundle does not carry, by a direct reference ({@link #externalIntrinsic}).
        intrinsics.put("runtime.nanotime", runtime.nativeTarget("runtime.nanotime", args -> System.nanoTime()));
        intrinsics.put("runtime.Goexit", runtime.nativeTarget("runtime.Goexit", args -> {
            graalgo.runtime.Goexit.goexit();
            return null;
        }));
        intrinsics.put("os.Exit", runtime.nativeTarget("os.Exit", args -> exit((long) args[1])));
    }

    /** {@code os.Exit}: the scheduler's exit request; the calling goroutine never returns. */
    @TruffleBoundary
    private static Object exit(long code) {
        SchedulerCore sc = SchedulerCore.installedOrNull();
        if (sc == null || !sc.onGoroutine()) {
            throw new IllegalStateException("os.Exit outside a goroutine");
        }
        sc.requestExit((int) code);
        sc.freezeForExit();
        throw new IllegalStateException("os.Exit returned");
    }

    /**
     * The intrinsic for function {@code symbol} ({@code path.Name}) of a package the bundle does
     * not carry (the unforked standard library), or null: the facade runtime of toolexec W5
     * replaces this lookup by real package units.
     */
    public RootCallTarget externalIntrinsic(String path, String symbol) {
        return byPath.containsKey(path) ? null : intrinsics.get(symbol);
    }

    /** Decodes a bundle; function bodies stay undecoded. */
    public static Program load(GoLanguage language, byte[] bundle) {
        Program p = new Program(language);
        int[] pos = {0};
        if (bundle.length < 4 || bundle[0] != 'G' || bundle[1] != 'G' || bundle[2] != 'B' || bundle[3] != 'N') {
            throw new IllegalArgumentException("not a graalgo bundle (bad magic)");
        }
        pos[0] = 4;
        long version = uvarint(bundle, pos);
        if (version != 2) {
            throw new IllegalArgumentException("bundle version " + version + ", this runtime reads 2");
        }
        bytes(bundle, pos);
        String mainPath = new String(bytes(bundle, pos), StandardCharsets.UTF_8);
        // link -X settings and the main module's build info: carried by the bundle, applied
        // by a later work package (toolexec-design §3.6).
        for (long nx = uvarint(bundle, pos); nx > 0; nx--) {
            bytes(bundle, pos);
        }
        bytes(bundle, pos);
        long count = uvarint(bundle, pos);
        List<PackageUnit> units = new ArrayList<>();
        for (long i = 0; i < count; i++) {
            String importPath = new String(bytes(bundle, pos), StandardCharsets.UTF_8);
            GirFile gir = GirFile.decode(bytes(bundle, pos));
            // Symbols name a package by its -p path ("main" for the main package), which is
            // what the GIR records; the bundle's key is the import path of the build.
            String path = gir.string(gir.sections().pkg.path());
            PackageUnit u = new PackageUnit(p, units.size(), path, gir);
            units.add(u);
            p.byPath.put(path, u);
            if (importPath.equals(mainPath)) {
                p.main = u;
            }
        }
        if (pos[0] != bundle.length) {
            throw new IllegalArgumentException("bundle: trailing bytes");
        }
        if (p.main == null) {
            throw new IllegalArgumentException("bundle has no main package " + mainPath);
        }
        p.computeInitOrder(units);
        return p;
    }

    private static long uvarint(byte[] b, int[] pos) {
        long v = 0;
        for (int shift = 0; shift < 64; shift += 7) {
            if (pos[0] >= b.length) {
                break;
            }
            int x = b[pos[0]++] & 0xFF;
            v |= (long) (x & 0x7F) << shift;
            if (x < 0x80) {
                return v;
            }
        }
        throw new IllegalArgumentException("bundle: bad varint");
    }

    private static byte[] bytes(byte[] b, int[] pos) {
        long n = uvarint(b, pos);
        if (n > b.length - pos[0]) {
            throw new IllegalArgumentException("bundle: truncated");
        }
        byte[] out = Arrays.copyOfRange(b, pos[0], pos[0] + (int) n);
        pos[0] += (int) n;
        return out;
    }

    /**
     * gc's order since go1.21 (toolexec-design §3.6): sort by import path, then repeatedly
     * initialize the first package whose imports are all initialized. Imports outside the
     * bundle (the unforked standard library, toolexec W5) are skipped.
     */
    private void computeInitOrder(List<PackageUnit> units) {
        List<PackageUnit> pending = new ArrayList<>(units);
        pending.sort((a, b) -> a.path.compareTo(b.path));
        Map<String, Boolean> done = new HashMap<>();
        while (!pending.isEmpty()) {
            PackageUnit next = null;
            for (PackageUnit u : pending) {
                boolean ready = true;
                for (int imp : u.sections().pkg.imports()) {
                    String path = u.str(imp);
                    if (byPath.containsKey(path) && !done.containsKey(path)) {
                        ready = false;
                        break;
                    }
                }
                if (ready) {
                    next = u;
                    break;
                }
            }
            if (next == null) {
                throw new IllegalArgumentException("bundle: import cycle among " + pending.size() + " packages");
            }
            pending.remove(next);
            done.put(next.path, true);
            initOrder.add(next);
        }
    }

    public PackageUnit packageNamed(String path) {
        PackageUnit u = byPath.get(path);
        if (u == null) {
            throw new IllegalStateException("package " + path + " is not in the bundle (standard-library "
                    + "packages need the toolexec fork, work package W5)");
        }
        return u;
    }

    public int packageCount() {
        return byPath.size();
    }

    /** The Java implementation of a body-less function (toolexec-design §5.3, D9). */
    RootCallTarget intrinsic(FunctionUnit f) {
        String linkname = f.pkg.str(f.func.linkname());
        RootCallTarget t = intrinsics.get(linkname.isEmpty() ? f.symbol : linkname);
        if (t == null) {
            for (Gir.Linkname l : f.pkg.sections().pkg.linknames()) {
                if (f.pkg.str(l.local()).equals(f.pkg.str(f.func.name()))) {
                    t = intrinsics.get(f.pkg.str(l.remote()));
                }
            }
        }
        if (t == null) {
            throw new IllegalStateException("undefined: " + f.symbol + " (no Go body and no graalgo intrinsic)");
        }
        return t;
    }

    /** Fresh, zeroed package-variable storage for one context. */
    public Flat[] newGlobals() {
        Flat[] globals = new Flat[byPath.size()];
        for (PackageUnit u : initOrder) {
            globals[u.index] = u.globalsType().zeroFlat();
        }
        return globals;
    }

    /** Package initialization in order, then {@code main.main}; runs on the main goroutine. */
    public void run(GoProcess process) {
        for (PackageUnit u : initOrder) {
            Gir.Init init = u.sections().init;
            if (init.varInit() >= 0) {
                u.func(init.varInit()).instance(null).get().call(new Object[] {null});
            }
            for (int fn : init.inits()) {
                u.func(fn).instance(null).get().call(new Object[] {null});
            }
        }
        main.funcNamed("main").instance(null).get().call(new Object[] {null});
    }
}
