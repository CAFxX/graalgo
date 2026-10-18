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
import java.util.Set;
import java.util.TreeMap;

/**
 * Extends the {@code PanicOverlayDriftTest} isolated-classloader drift check
 * to all 8 GG-0020 frozen stub files (the truffle module's {@code STUBS}
 * list in {@code internal/backend/truffle/Makefile}).
 *
 * <p>Background: the runtime overlay classes shadow the frozen truffle
 * stubs at runtime (runtime classes precede truffle classes on every
 * classpath), and the joint javac round excludes the stubs — so a
 * signature change to a truffle stub compiles fine and nothing references
 * it. {@code FreezeSignatureTest}/{@code FreezeGapTest} resolve via
 * {@code Class.forName} on the app classloader, i.e. they check the
 * <em>overlay's</em> signatures, never the frozen stub files. Without this
 * test, the "freeze" guards the overlay, not the freeze.
 *
 * <p>This test loads each truffle module copy in isolation (child-first for
 * exactly the 8 frozen FQNs, app classloader as parent for everything else)
 * and asserts both directions:
 * <ol>
 *   <li>every public method declared in the frozen stub exists in the
 *       overlay with an identical signature (name, parameter type names,
 *       return type name, public/static shape);</li>
 *   <li>the overlay adds no public method beyond the per-class additive
 *       exemption set below (fail closed on widening).</li>
 * </ol>
 * Signatures are compared by type <em>name</em>, not {@code Class}
 * identity: nested graalgo types resolve through different classloaders
 * on the two sides.
 *
 * <p>{@code graalgo.runtime.Panic} is additionally covered by
 * {@code PanicOverlayDriftTest}; it is included here so the mechanism
 * covers the full frozen set uniformly.
 *
 * <p>verifies: GG-0020 (freeze v2: the frozen stubs cannot drift
 * unnoticed)
 */
public final class FrozenStubDriftTest {

    /** The 8 frozen stub FQNs (truffle module STUBS list). */
    private static final List<String> STUBS = List.of(
            "graalgo.runtime.Netpoll",
            "graalgo.runtime.ChanOps",
            "graalgo.runtime.Scheduler",
            "graalgo.runtime.GcInterop",
            "graalgo.runtime.Timers",
            "graalgo.runtime.GoroutineState",
            "graalgo.runtime.Panic",
            "graalgo.safepoint.Safepoint");

    /**
     * Overlay-additive public methods, exempt from the no-widening check.
     * Key format: {@code name(paramBinaryName,...)} with
     * {@code Class.getName()} components. Verified against
     * {@code javap} output on master; any new additive method must be
     * added here deliberately, with its root-cause reason documented.
     */
    private static final Map<String, Set<String>> ADDITIVE = Map.of(
            "graalgo.runtime.Panic", Set.of(
                    "setPanicNilOldBehavior(boolean)",
                    "isPanicNilOldBehavior()"),
            "graalgo.runtime.ChanOps", Set.of(
                    "makeChan(long,int,java.lang.Object)",
                    "recvOk(graalgo.runtime.ChanOps$Chan)",
                    "selectRecvValue()",
                    "selectRecvOk()"),
            "graalgo.runtime.GcInterop", Set.of(
                    "newWeakRef(java.lang.Object)",
                    "weakRefValue(java.lang.ref.WeakReference)",
                    "memStats()",
                    "pauseNsRing()"),
            "graalgo.runtime.Timers", Set.of(
                    "startTimerId(long,java.lang.Runnable)",
                    "after(long)",
                    "newTimer(long)",
                    "newTicker(long)",
                    "tick(long)"),
            "graalgo.runtime.Scheduler", Set.of(
                    "isPhase2()"));

    /** Signature key: name + parameter type names (Class.getName()). */
    private static String sigKey(Method m) {
        StringBuilder sb = new StringBuilder(m.getName()).append('(');
        Class<?>[] ps = m.getParameterTypes();
        for (int i = 0; i < ps.length; i++) {
            if (i > 0) {
                sb.append(',');
            }
            sb.append(ps[i].getName());
        }
        return sb.append(')').toString();
    }

    private static ClassLoader stubLoader() {
        // The frozen stub surface compiles in the truffle module (see its
        // Makefile: one javac round cannot hold the surface and the
        // overlay's same FQN).
        Path p1 = Paths.get("../truffle/out/classes").toAbsolutePath().normalize();
        if (!p1.toFile().isDirectory()) {
            throw new AssertionError("truffle module classes not built: " + p1);
        }
        final URL url;
        try {
            url = p1.toUri().toURL();
        } catch (Exception e) {
            throw new AssertionError(e);
        }
        ClassLoader app = FrozenStubDriftTest.class.getClassLoader();
        return new URLClassLoader(new URL[]{url}, app) {
            @Override
            protected Class<?> loadClass(String name, boolean resolve)
                    throws ClassNotFoundException {
                synchronized (getClassLoadingLock(name)) {
                    Class<?> c = findLoadedClass(name);
                    if (c == null && STUBS.contains(name)) {
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
    }

    private final ClassLoader stubs = stubLoader();

    private void checkDrift(String fqn) throws Exception {
        Class<?> stub;
        try {
            stub = Class.forName(fqn, false, stubs);
        } catch (ClassNotFoundException e) {
            throw new AssertionError("frozen stub not found in truffle classes: " + fqn, e);
        }
        Class<?> overlay = Class.forName(fqn); // app classloader: the overlay
        if (stub == overlay) {
            throw new AssertionError(fqn + ": isolated loader returned the overlay "
                    + "(classpath order changed? runtime classes must not precede "
                    + "the isolated truffle copy here)");
        }
        List<String> problems = new ArrayList<>();
        // Direction 1: every frozen public method exists in the overlay
        // with an identical signature.
        Map<String, Method> overlaySigs = new TreeMap<>();
        for (Method m : overlay.getDeclaredMethods()) {
            if (m.isSynthetic() || !Modifier.isPublic(m.getModifiers())) {
                continue;
            }
            overlaySigs.put(sigKey(m), m);
        }
        for (Method m : stub.getDeclaredMethods()) {
            if (m.isSynthetic() || !Modifier.isPublic(m.getModifiers())) {
                continue;
            }
            String key = sigKey(m);
            Method o = overlaySigs.get(key);
            if (o == null) {
                problems.add(key + " (absent from overlay or signature changed)");
                continue;
            }
            if (!o.getReturnType().getName().equals(m.getReturnType().getName())) {
                problems.add(key + " (return type drift: "
                        + m.getReturnType().getName() + " vs "
                        + o.getReturnType().getName() + ")");
            } else if (Modifier.isStatic(o.getModifiers())
                    != Modifier.isStatic(m.getModifiers())) {
                problems.add(key + " (static-ness drift)");
            }
        }
        // Direction 2: the overlay adds nothing beyond the additive set.
        Set<String> additive = ADDITIVE.getOrDefault(fqn, Set.of());
        Map<String, String> stubKeys = new TreeMap<>();
        for (Method m : stub.getDeclaredMethods()) {
            if (m.isSynthetic() || !Modifier.isPublic(m.getModifiers())) {
                continue;
            }
            stubKeys.put(sigKey(m), m.getReturnType().getName());
        }
        for (Method m : overlay.getDeclaredMethods()) {
            if (m.isSynthetic() || !Modifier.isPublic(m.getModifiers())) {
                continue;
            }
            String key = sigKey(m);
            String stubRet = stubKeys.get(key);
            if (stubRet != null && stubRet.equals(m.getReturnType().getName())) {
                continue; // identical frozen signature
            }
            if (!additive.contains(key)) {
                problems.add(key + " (overlay adds non-exempt public method)");
            }
        }
        if (!problems.isEmpty()) {
            throw new AssertionError("frozen stub drifted: " + fqn + ":\n  "
                    + String.join("\n  ", problems));
        }
    }

    public void testNetpoll() throws Exception {
        checkDrift("graalgo.runtime.Netpoll");
    }

    public void testChanOps() throws Exception {
        checkDrift("graalgo.runtime.ChanOps");
    }

    public void testScheduler() throws Exception {
        checkDrift("graalgo.runtime.Scheduler");
    }

    public void testGcInterop() throws Exception {
        checkDrift("graalgo.runtime.GcInterop");
    }

    public void testTimers() throws Exception {
        checkDrift("graalgo.runtime.Timers");
    }

    public void testGoroutineState() throws Exception {
        checkDrift("graalgo.runtime.GoroutineState");
    }

    public void testPanic() throws Exception {
        checkDrift("graalgo.runtime.Panic");
    }

    public void testSafepoint() throws Exception {
        checkDrift("graalgo.safepoint.Safepoint");
    }
}
