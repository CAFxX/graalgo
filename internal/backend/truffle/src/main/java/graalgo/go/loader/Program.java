package graalgo.go.loader;

import com.oracle.truffle.api.RootCallTarget;
import graalgo.gir.Gir;
import graalgo.gir.GirFile;
import graalgo.go.intrinsics.IntrinsicTable;
import graalgo.go.runtime.GoProcess;
import graalgo.go.runtime.RuntimeTypes;
import graalgo.go.types.Descriptors;
import graalgo.go.types.GoType;
import graalgo.go.types.TypeTable;
import graalgo.go.values.Closure;
import graalgo.go.values.Flat;
import graalgo.language.GoLanguage;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

/**
 * A loaded bundle (toolexec-design §4.2–4.3; truffle-design §2.2): the context-independent
 * result of {@code GoLanguage.parse} — package units in initialization order and the link's
 * symbol resolution. Running it in a context allocates the globals, initializes every package
 * and calls {@code main.main}.
 *
 * <p>Symbols are resolved by the linker ({@code internal/toolexec/link}, toolexec-design §5.3),
 * not here: the bundle's resolution table binds each function that is not its own Go body to
 * another package's body ({@code //go:linkname}) or to an intrinsic of the registry
 * ({@link IntrinsicTable}), and names the variables that share storage. A function without a
 * body and without a binding was unreachable at link time; calling it is the linker's
 * "undefined" error, at run time.
 *
 * <p>Bundle wire format ({@code internal/toolexec/bundle}): {@code "GGBN" | version | runner |
 * main package | count | x* | modinfo | resolution | count | (import path | GIR)*} with
 * {@code resolution := registry version | registry hash | count | (package | name | intrinsic |
 * target package | target name)* | count | (package | name | target package | target name)*},
 * unsigned varints and length-prefixed bytes.
 */
public final class Program {
    public final GoLanguage language;
    public final RuntimeTypes runtime;
    public final TypeTable types;
    private final Map<String, PackageUnit> byPath = new HashMap<>();
    /** Packages in initialization order. */
    private final List<PackageUnit> initOrder = new ArrayList<>();
    private PackageUnit main;
    /** Function bindings of the link, by {@code package + '\0' + name}. */
    private final Map<String, String[]> funcBindings = new HashMap<>();
    /** Variable aliases of the link, by {@code package + '\0' + name}: target package, name. */
    private final Map<String, String[]> varAliases = new HashMap<>();
    private final Map<String, RootCallTarget> intrinsics = new ConcurrentHashMap<>();

    private Program(GoLanguage language) {
        this.language = language;
        this.runtime = language.runtimeTypes();
        this.types = runtime.table;
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
        if (version != 3) {
            throw new IllegalArgumentException("bundle version " + version + ", this runtime reads 3");
        }
        bytes(bundle, pos);
        String mainPath = new String(bytes(bundle, pos), StandardCharsets.UTF_8);
        // link -X settings and the main module's build info: carried by the bundle, applied
        // by a later work package (toolexec-design §3.6).
        for (long nx = uvarint(bundle, pos); nx > 0; nx--) {
            bytes(bundle, pos);
        }
        bytes(bundle, pos);
        long registryVersion = uvarint(bundle, pos);
        String registryHash = string(bundle, pos);
        if (registryVersion != IntrinsicTable.VERSION || !registryHash.equals(IntrinsicTable.HASH)) {
            throw new IllegalArgumentException("bundle was linked against intrinsic registry version "
                    + registryVersion + " (" + registryHash + "); this runtime implements version "
                    + IntrinsicTable.VERSION + " (" + IntrinsicTable.HASH + ") — rebuild the program");
        }
        for (long n = uvarint(bundle, pos); n > 0; n--) {
            String key = string(bundle, pos) + '\0' + string(bundle, pos);
            p.funcBindings.put(key, new String[] {string(bundle, pos), string(bundle, pos), string(bundle, pos)});
        }
        for (long n = uvarint(bundle, pos); n > 0; n--) {
            String key = string(bundle, pos) + '\0' + string(bundle, pos);
            p.varAliases.put(key, new String[] {string(bundle, pos), string(bundle, pos)});
        }
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
        PackageUnit abi = p.byPath.get("internal/abi");
        if (abi != null) {
            // The program can read type descriptors: its internal/abi defines their layout.
            p.types.descriptors.attach(new Descriptors.Host() {
                @Override
                public GoType abiType(String name) {
                    return abi.namedType(name);
                }

                @Override
                public String packageName(String path) {
                    return p.packageNamed(path).name;
                }

                @Override
                public Object equalFunc(GoType t) {
                    return new Closure(new TypeEqual(language, t).getCallTarget(), null);
                }
            });
        }
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

    private static String string(byte[] b, int[] pos) {
        return new String(bytes(b, pos), StandardCharsets.UTF_8);
    }

    /**
     * gc's order since go1.21 (toolexec-design §3.6): sort by import path, then repeatedly
     * initialize the first package whose imports are all initialized; {@code runtime} first
     * (gc's runtime is running before any initializer; packages reach it by
     * {@code //go:linkname} without importing it). Imports outside the bundle ({@code unsafe})
     * are skipped.
     */
    private void computeInitOrder(List<PackageUnit> units) {
        List<PackageUnit> pending = new ArrayList<>(units);
        pending.sort((a, b) -> a.path.compareTo(b.path));
        PackageUnit rt = byPath.get("runtime");
        if (rt != null) {
            pending.remove(rt);
            pending.add(0, rt);
        }
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
            throw new IllegalStateException("package " + path + " is not in the bundle");
        }
        return u;
    }

    public int packageCount() {
        return byPath.size();
    }

    /**
     * The link's binding of {@code f} (toolexec-design §5.3, D9): the call target of the Go
     * body or of the intrinsic it resolves to, or null when {@code f} is simply its own body.
     * A function with neither body nor binding was unreachable for the linker.
     */
    RootCallTarget bound(FunctionUnit f) {
        String[] b = funcBindings.get(f.pkg.path + '\0' + f.pkg.str(f.func.name()));
        if (b == null) {
            if (f.isBodyless()) {
                throw new IllegalStateException("undefined: " + f.symbol + " (no Go body and no graalgo intrinsic)");
            }
            return null;
        }
        if (b[0].isEmpty()) {
            return packageNamed(b[1]).funcNamed(b[2]).instance(null).get();
        }
        return intrinsics.computeIfAbsent(b[0], symbol -> {
            com.oracle.truffle.api.nodes.RootNode root = IntrinsicTable.root(language, symbol);
            if (root == null) {
                throw new IllegalStateException("intrinsic " + symbol + " is not in this runtime's registry");
            }
            return root.getCallTarget();
        });
    }

    /**
     * The package and variable that hold the storage of variable {@code name} of {@code u}:
     * itself, unless {@code //go:linkname} makes it another package's variable.
     */
    String[] varAlias(PackageUnit u, String name) {
        return varAliases.get(u.path + '\0' + name);
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
