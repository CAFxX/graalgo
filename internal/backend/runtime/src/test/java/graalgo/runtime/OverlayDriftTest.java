package graalgo.runtime;

import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.net.URL;
import java.net.URLClassLoader;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.TreeSet;

/**
 * P2-M5 (WS E), generalized by RV22-P1-1: drift guard for the Phase-2
 * same-FQN classpath overlay (bridge B-8; design
 * {@code docs/arch/p2-d8-same-fqn-overlay.md}).
 *
 * <p>Eight classes exist twice under identical fully-qualified names; the
 * runtime module's copy shadows the truffle module's copy at load time
 * (runtime classes precede truffle classes on the classpath). If the
 * truffle surface ever changes, the shadowing copy would silently desync.
 * This test loads each truffle copy via an isolated child-first
 * classloader and asserts every public method of it exists in the
 * shadowing copy with an identical signature (name, parameter types,
 * return type, static-ness). Type comparison is by type <em>name</em>,
 * not {@code Class} identity, so inner types that legitimately load in
 * both loaders (e.g. {@code ChanOps$Chan}) compare equal.
 *
 * <p>The reverse check pins the shadowing copy's public surface: every
 * public method of it must either exist in the truffle copy or be listed
 * in that pair's additive exemptions. Exemptions are deliberate Phase-2
 * surface additions, documented per pair below; a new public method that
 * is neither mirrored nor exempted fails the suite instead of silently
 * widening the overlay.
 *
 * <p>verifies: GG-0055 (panicnil emulation does not desync from the frozen surface),
 * B-8 (the overlay inventory's drift guard for all 8 same-FQN pairs)
 */
public final class OverlayDriftTest {

    /** One overlay pair: truffle FQN + the overlay's documented additive methods. */
    private record Pair(String fqn, TreeSet<String> additive) {
        Pair(String fqn, String... additive) {
            this(fqn, new TreeSet<>(List.of(additive)));
        }
    }

    // Signature key for an exemption: name + "(" + param type names + ")".
    private static String ex(String name, String... params) {
        return name + "(" + String.join(",", params) + ")";
    }

    private static final List<Pair> PAIRS = List.of(
            // Panic: additive panicnil emulation (GG-0055).
            new Pair("graalgo.runtime.Panic",
                    ex("setPanicNilOldBehavior", "boolean"),
                    ex("isPanicNilOldBehavior")),
            // Scheduler: isPhase2() is the phase-2-only diagnostic used by
            // OverlayGuard; intentionally not part of the frozen contract.
            new Pair("graalgo.runtime.Scheduler",
                    ex("isPhase2")),
            // GoroutineState: instance API byte-identical by design (P2-D8).
            new Pair("graalgo.runtime.GoroutineState"),
            // Safepoint: single-method surface, identical both sides.
            new Pair("graalgo.safepoint.Safepoint"),
            // Netpoll: GG-0018 deletion; both sides fail-fast with the same
            // 5 signatures, exact match pinned.
            new Pair("graalgo.runtime.Netpoll"),
            // ChanOps: Phase-2 channel surface additions over the frozen stub.
            new Pair("graalgo.runtime.ChanOps",
                    ex("makeChan", "long", "int", "java.lang.Object"),
                    ex("recvOk", "graalgo.runtime.ChanOps$Chan"),
                    ex("selectRecvValue"),
                    ex("selectRecvOk")),
            // GcInterop: Phase-2 GC surface additions over the frozen stub.
            new Pair("graalgo.runtime.GcInterop",
                    ex("memStats"),
                    ex("weakRefValue", "java.lang.ref.WeakReference"),
                    ex("newWeakRef", "java.lang.Object"),
                    ex("pauseNsRing")),
            // Timers: Phase-2 timer-heap surface additions over the frozen stub.
            new Pair("graalgo.runtime.Timers",
                    ex("after", "long"),
                    ex("tick", "long"),
                    ex("newTicker", "long"),
                    ex("newTimer", "long"),
                    ex("startTimerId", "long", "java.lang.Runnable")));

    /** Full signature key: name + params + return + static-ness. */
    private static String sig(Method m) {
        StringBuilder b = new StringBuilder(m.getName()).append("(");
        for (Class<?> p : m.getParameterTypes()) {
            b.append(p.getName()).append(",");
        }
        b.append("):").append(m.getReturnType().getName());
        b.append(Modifier.isStatic(m.getModifiers()) ? "[static]" : "[instance]");
        return b.toString();
    }

    private static String exKey(Method m) {
        StringBuilder b = new StringBuilder(m.getName()).append("(");
        String sep = "";
        for (Class<?> p : m.getParameterTypes()) {
            b.append(sep).append(p.getName());
            sep = ",";
        }
        return b.append(")").toString();
    }

    public void testOverlayMatchesPhase1Surface() throws Exception {
        // The frozen surfaces compile in the truffle module (see its Makefile:
        // one javac round cannot hold the surface and the overlay's same FQN).
        Path p1 = Paths.get("../truffle/out/classes").toAbsolutePath().normalize();
        if (!p1.toFile().isDirectory()) {
            throw new AssertionError("truffle module classes not built: " + p1);
        }
        // Child-first for the overlay packages: truffle classes URL first,
        // app classloader as parent for everything else (Truffle,
        // GoInterface, ...). This loads the truffle module's copies (the
        // frozen surfaces), not the overlays the app classloader would
        // return parent-first.
        List<URL> urls = new ArrayList<>();
        urls.add(p1.toUri().toURL());
        ClassLoader app = OverlayDriftTest.class.getClassLoader();
        URLClassLoader stubLoader = new URLClassLoader(urls.toArray(new URL[0]), app) {
            @Override
            protected Class<?> loadClass(String name, boolean resolve)
                    throws ClassNotFoundException {
                synchronized (getClassLoadingLock(name)) {
                    Class<?> c = findLoadedClass(name);
                    if (c == null
                            && (name.startsWith("graalgo.runtime.")
                                || name.startsWith("graalgo.safepoint."))) {
                        try {
                            c = findClass(name);
                        } catch (ClassNotFoundException e) {
                            // fall through to parent
                        }
                    }
                    if (c == null) {
                        c = super.loadClass(name, false);
                    }
                    if (resolve) {
                        resolveClass(c);
                    }
                    return c;
                }
            }
        };

        List<String> problems = new ArrayList<>();
        for (Pair pair : PAIRS) {
            Class<?> frozen;
            try {
                frozen = Class.forName(pair.fqn(), false, stubLoader);
            } catch (ClassNotFoundException e) {
                problems.add(pair.fqn() + ": frozen surface not found in truffle classes");
                continue;
            }
            // Resolves to the overlay: the test classpath puts the runtime
            // module's classes before the truffle module's. Guard against a
            // vacuous pass: the overlay must come from a different location
            // than the truffle classes dir (a misordered test classpath
            // would otherwise compare a class against itself).
            Class<?> overlay = Class.forName(pair.fqn());
            if (!p1.toUri().getPath().equals(
                    frozen.getProtectionDomain().getCodeSource().getLocation().getPath())) {
                problems.add(pair.fqn() + ": frozen copy did not load from " + p1);
                continue;
            }
            if (p1.toUri().getPath().equals(
                    overlay.getProtectionDomain().getCodeSource().getLocation().getPath())) {
                problems.add(pair.fqn()
                        + ": overlay resolved to the truffle copy — test classpath order is wrong");
                continue;
            }
            Map<String, Method> overlaySigs = new java.util.HashMap<>();
            for (Method m : overlay.getDeclaredMethods()) {
                if (Modifier.isPublic(m.getModifiers())) {
                    overlaySigs.put(sig(m), m);
                }
            }
            // Forward: every public method of the frozen surface exists in
            // the overlay with an identical signature.
            for (Method m : frozen.getDeclaredMethods()) {
                if (!Modifier.isPublic(m.getModifiers())) {
                    continue;
                }
                if (!overlaySigs.containsKey(sig(m))) {
                    problems.add(pair.fqn() + ": overlay drifted — missing/changed " + sig(m));
                }
            }
            // Reverse: the overlay must not widen the surface beyond the
            // documented additive methods.
            TreeSet<String> frozenNames = new TreeSet<>();
            for (Method m : frozen.getDeclaredMethods()) {
                if (Modifier.isPublic(m.getModifiers())) {
                    frozenNames.add(m.getName());
                }
            }
            for (Method m : overlay.getDeclaredMethods()) {
                if (!Modifier.isPublic(m.getModifiers())) {
                    continue;
                }
                if (!frozenNames.contains(m.getName())
                        && !pair.additive().contains(exKey(m))) {
                    problems.add(pair.fqn()
                            + ": overlay adds non-exempt public method " + sig(m));
                }
            }
            // Public inner types (e.g. ChanOps$Chan, ChanOps$SelectCase)
            // must match; private implementation details are exempt.
            TreeSet<String> frozenInner = new TreeSet<>();
            for (Class<?> ic : frozen.getDeclaredClasses()) {
                if (Modifier.isPublic(ic.getModifiers())) {
                    frozenInner.add(ic.getSimpleName());
                }
            }
            TreeSet<String> overlayInner = new TreeSet<>();
            for (Class<?> ic : overlay.getDeclaredClasses()) {
                if (Modifier.isPublic(ic.getModifiers())) {
                    overlayInner.add(ic.getSimpleName());
                }
            }
            if (!frozenInner.equals(overlayInner)) {
                problems.add(pair.fqn() + ": inner types differ: truffle=" + frozenInner
                        + " overlay=" + overlayInner);
            }
        }
        if (!problems.isEmpty()) {
            throw new AssertionError("overlay drifted from the frozen surfaces: " + problems);
        }
    }
}
