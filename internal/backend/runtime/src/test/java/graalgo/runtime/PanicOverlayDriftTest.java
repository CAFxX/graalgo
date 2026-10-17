package graalgo.runtime;

import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.net.URL;
import java.net.URLClassLoader;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.List;
import java.util.TreeSet;

import graalgo.values.GoInterface;
import graalgo.values.GoTypeDescriptor;

/**
 * P2-M5 (WS E): drift guard for the Phase-2 {@code graalgo.runtime.Panic}
 * same-FQN overlay (panicnil emulation).
 *
 * <p>The overlay shadows the truffle module's {@code Panic} surface at runtime
 * (runtime classes precede truffle classes on the classpath). If that
 * surface ever changes, the overlay would silently desync. This test
 * loads the truffle module's copy via an isolated classloader and asserts
 * every public method of it exists here with an identical signature. The
 * overlay's additive methods (setPanicNilOldBehavior,
 * isPanicNilOldBehavior) are exempt.
 *
 * <p>verifies: GG-0055 (panicnil emulation does not desync from the frozen surface)
 */
public final class PanicOverlayDriftTest {

    /** Additive overlay methods, exempt from the drift check. */
    private static final TreeSet<String> ADDITIVE = new TreeSet<>(List.of(
            "setPanicNilOldBehavior", "isPanicNilOldBehavior"));

    /**
     * Loads the Phase-1 (truffle module) {@code graalgo.runtime.Panic} via a
     * child-first loader for exactly that FQN: truffle classes first, the app
     * classloader as parent for everything else (Truffle, GoInterface, ...).
     * Shared by the signature drift check and the behavioral parity checks
     * below, so the two cannot disagree about which class is "Phase-1".
     */
    private static Class<?> loadPhase1PanicClass() throws Exception {
        // The frozen Panic surface compiles in the truffle module (see its Makefile:
        // one javac round cannot hold the surface and the overlay's same FQN).
        Path p1 = Paths.get("../truffle/out/classes").toAbsolutePath().normalize();
        if (!p1.toFile().isDirectory()) {
            throw new AssertionError("truffle module classes not built: " + p1);
        }
        // Child-first for exactly graalgo.runtime.Panic: truffle classes URL
        // first, app classloader as parent for everything else (Truffle,
        // GoInterface, ...). This loads the truffle module's Panic (the frozen
        // surface), not the overlay the app classloader would return parent-first.
        List<URL> urls = new ArrayList<>();
        urls.add(p1.toUri().toURL());
        ClassLoader app = PanicOverlayDriftTest.class.getClassLoader();
        URLClassLoader stubLoader = new URLClassLoader(urls.toArray(new URL[0]), app) {
            @Override
            protected Class<?> loadClass(String name, boolean resolve)
                    throws ClassNotFoundException {
                synchronized (getClassLoadingLock(name)) {
                    Class<?> c = findLoadedClass(name);
                    if (c == null && name.equals("graalgo.runtime.Panic")) {
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
        try {
            return Class.forName("graalgo.runtime.Panic", false, stubLoader);
        } catch (ClassNotFoundException e) {
            throw new AssertionError("frozen Panic surface not found", e);
        }
    }

    public void testOverlayMatchesPhase1Surface() throws Exception {
        Class<?> stubPanic = loadPhase1PanicClass();
        Class<?> overlay = Panic.class; // resolves to the overlay (phase2 first)
        List<String> missing = new ArrayList<>();
        for (Method m : stubPanic.getDeclaredMethods()) {
            if (!Modifier.isPublic(m.getModifiers())) {
                continue;
            }
            try {
                Method o = overlay.getDeclaredMethod(m.getName(), m.getParameterTypes());
                if (!Modifier.isPublic(o.getModifiers())) {
                    missing.add(m.getName() + " (not public in overlay)");
                } else if (!o.getReturnType().equals(m.getReturnType())) {
                    missing.add(m.getName() + " (return type drift: "
                            + m.getReturnType() + " vs " + o.getReturnType() + ")");
                }
            } catch (NoSuchMethodException e) {
                missing.add(m.getName() + " (absent from overlay)");
            }
        }
        if (!missing.isEmpty()) {
            throw new AssertionError("Panic overlay drifted from the frozen surface: " + missing);
        }
        // The overlay must not widen the surface beyond the additive methods.
        for (Method m : overlay.getDeclaredMethods()) {
            if (!Modifier.isPublic(m.getModifiers()) || ADDITIVE.contains(m.getName())) {
                continue;
            }
            boolean found = false;
            for (Method pm : stubPanic.getDeclaredMethods()) {
                if (pm.getName().equals(m.getName())) {
                    found = true;
                    break;
                }
            }
            if (!found) {
                throw new AssertionError(
                        "overlay adds non-exempt public method: " + m.getName());
            }
        }
    }

    // ------------------------------------------------------------------
    // Behavioral drift guard (bug-019): the signature check above cannot
    // see behavioral drift. The overlay once silently dropped the
    // descriptor inference for bare GoErrorValue panics, so recover() of
    // a runtime panic returned the raw GoErrorValue instead of a
    // GoInterface — invisible to the whole suite. These tests pin the
    // inference and the recover() boxing contract on the LIVE overlay
    // (this module's classes come first on the test classpath, exactly
    // like the production launchers), and assert the overlay agrees with
    // the Phase-1 copy behaviorally.
    // ------------------------------------------------------------------

    /** Runs r, expecting Panic.throwPanic to throw; returns the exception. */
    private static PanicException catchPanic(Runnable r) {
        try {
            r.run();
        } catch (PanicException p) {
            return p;
        }
        throw new AssertionError("throwPanic did not throw PanicException");
    }

    /** Asserts the panic carries an inferred GoTypeDescriptor with the gc type name. */
    private static void assertInferredDescriptor(PanicException p, String typeName) {
        Object d = p.descriptor();
        if (!(d instanceof GoTypeDescriptor td)) {
            throw new AssertionError("expected inferred GoTypeDescriptor, got "
                    + (d == null ? "null" : d.getClass().getName()));
        }
        if (!typeName.equals(td.name())) {
            throw new AssertionError("inferred descriptor name <" + td.name()
                    + ">, expected <" + typeName + ">");
        }
    }

    /** bug-019: a bare GoErrorValue (runtime panic) thrown without a
     * descriptor gets "runtime.errorString" inferred. */
    public void testOverlayInfersDescriptorForBareGoErrorValue() {
        PanicException p = catchPanic(() ->
                Panic.throwPanic(GoErrors.integerDivideByZero(), null, null, null));
        assertInferredDescriptor(p, "runtime.errorString");
    }

    /** bug-019: interface-conversion panics infer "*runtime.TypeAssertionError". */
    public void testOverlayInfersTypeAssertionDescriptor() {
        PanicException p = catchPanic(() ->
                Panic.throwPanic(
                        GoErrors.interfaceConversion("interface {}", "string", "int"),
                        null, null, null));
        assertInferredDescriptor(p, "*runtime.TypeAssertionError");
    }

    /** The inference fires only for GoErrorValue: other values keep a null descriptor. */
    public void testOverlayDoesNotInferDescriptorForNonErrorValues() {
        PanicException p = catchPanic(() ->
                Panic.throwPanic(GoStrings.of("boom"), null, null, null));
        if (p.descriptor() != null) {
            throw new AssertionError("unexpected inferred descriptor for a string panic: "
                    + p.descriptor());
        }
    }

    /**
     * recover() of a runtime panic returns the value boxed as a Go
     * {@code any} (GoInterface) — gc parity. Before the bug-019 port the
     * overlay returned the raw GoErrorValue, which then crashed type
     * assertions and type switches with ClassCastException
     * (GoInterface.narrow's blind cast).
     */
    public void testRecoverBoxesRuntimePanicAsInterface() {
        GoroutineState prev = GoroutineState.install(new GoroutineState());
        try {
            GoErrorValue v = GoErrors.integerDivideByZero();
            PanicException p = catchPanic(() -> Panic.throwPanic(v, null, null, null));
            GoroutineState s = GoroutineState.current();
            s.setPanicking(p);
            s.setRecoverAllowed(true);
            Object r = Panic.recover();
            if (!(r instanceof GoInterface gi)) {
                throw new AssertionError("recover() did not box the runtime panic "
                        + "as GoInterface, got "
                        + (r == null ? "null" : r.getClass().getName()));
            }
            if (gi.data() != v) {
                throw new AssertionError("boxed interface data is not the panic value");
            }
            assertInferredDescriptor(p, "runtime.errorString");
            // recover() marks the panic stopped; the DeferChain (not
            // recover() itself) clears the in-flight panic afterwards.
            if (!p.isRecovered()) {
                throw new AssertionError("recover() did not mark the panic recovered");
            }
        } finally {
            GoroutineState.restore(prev);
        }
    }

    /**
     * The panicnil knob still works with the inference in place, and the
     * inference is orthogonal to it: a GoErrorValue panic still gets its
     * descriptor with old behavior on, while panic(nil) stays a nil panic
     * (no inference fires for nil).
     */
    public void testDescriptorInferenceOrthogonalToPanicNilKnob() {
        boolean prev = Panic.isPanicNilOldBehavior();
        try {
            Panic.setPanicNilOldBehavior(true);
            // A GoErrorValue panic still gets its descriptor with old behavior on.
            PanicException p = catchPanic(() ->
                    Panic.throwPanic(GoErrors.integerDivideByZero(), null, null, null));
            assertInferredDescriptor(p, "runtime.errorString");
            // panic(nil) stays a nil panic under old behavior: no inference fires.
            PanicException q = catchPanic(() -> Panic.throwPanic(null, null, null, null));
            if (q.goValue() != null || q.descriptor() != null) {
                throw new AssertionError("panicnil old behavior changed: value="
                        + q.goValue() + " descriptor=" + q.descriptor());
            }
        } finally {
            Panic.setPanicNilOldBehavior(prev);
        }
    }

    /**
     * Default knob (off): panic(nil) still becomes *PanicNilError, and the
     * bug-019 inference must not rewrite its descriptor (the nil wrap sets
     * one, so the inference's d == null guard does not fire).
     */
    public void testPanicNilDefaultStillWraps() {
        PanicException p = catchPanic(() -> Panic.throwPanic(null, null, null, null));
        if (!(p.goValue() instanceof PanicNilError)) {
            throw new AssertionError("default panicnil behavior changed: value="
                    + p.goValue());
        }
        if (p.descriptor() == null) {
            throw new AssertionError("PanicNilError lost its descriptor");
        }
    }

    /**
     * Invokes the Phase-1 copy's 4-arg throwPanic reflectively (via the
     * shared child-first loader), returning the thrown PanicException.
     */
    private static PanicException catchPanicReflective(Class<?> phase1Panic, Object goValue)
            throws Exception {
        Method m = phase1Panic.getMethod("throwPanic",
                Object.class, Object.class, String.class,
                com.oracle.truffle.api.nodes.Node.class);
        try {
            m.invoke(null, goValue, null, null, null);
        } catch (InvocationTargetException e) {
            if (e.getCause() instanceof PanicException p) {
                return p;
            }
            throw new AssertionError("Phase-1 throwPanic threw " + e.getCause(), e.getCause());
        }
        throw new AssertionError("Phase-1 throwPanic did not throw");
    }

    /**
     * Behavioral overlay↔Phase-1 parity: both copies must infer the same
     * descriptor for the same panic values. A future Phase-1 change to the
     * inference that is not ported here fails loudly instead of silently
     * desyncing the overlay (the exact failure mode of bug-019).
     */
    public void testOverlayDescriptorInferenceMatchesPhase1() throws Exception {
        Class<?> phase1Panic = loadPhase1PanicClass();
        Object[][] cases = {
                {GoErrors.integerDivideByZero(), "runtime.errorString"},
                {GoErrors.negativeShift(), "runtime.errorString"},
                {GoErrors.comparingUncomparable("func()"), "runtime.errorString"},
                {GoErrors.interfaceConversion("interface {}", "string", "int"),
                        "*runtime.TypeAssertionError"},
        };
        for (Object[] c : cases) {
            PanicException overlayP = catchPanic(() -> Panic.throwPanic(c[0], null, null, null));
            PanicException phase1P = catchPanicReflective(phase1Panic, c[0]);
            String want = (String) c[1];
            assertInferredDescriptor(overlayP, want);
            assertInferredDescriptor(phase1P, want);
        }
    }
}
