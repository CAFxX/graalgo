package graalgo.runtime;

import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.net.URL;
import java.net.URLClassLoader;
import java.nio.charset.StandardCharsets;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.List;
import java.util.TreeSet;

import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoString;
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
 * <p>Signatures alone cannot see behavioral drift: the missing bug-019
 * {@code GoErrorValue} descriptor inference (RV-P0-05) passed the
 * signature check while {@code recover()} of a runtime panic returned the
 * raw error instead of a boxed {@code GoInterface}. The behavioral
 * contract below drives {@code throwPanic}/{@code recover} on <em>both</em>
 * copies over a runtime-panic matrix and asserts identical outcomes, plus
 * the absolute {@code recover()} contract for string-kind runtime errors
 * (fix-redmaster-r43): the interface data IS the message string, so
 * {@code recover()} must unwrap the {@code GoErrorValue} carrier to its
 * message {@link GoString} — boxing the wrapper as the data makes every
 * kind-STRING consumer read a non-string.
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
    // Behavioral drift guard (M-P1-3): the signature check above is blind
    // to behavioral drift — the r43 string-kind unwrap landed in the
    // overlay's recover() only, so the truffle copy returned the raw
    // GoErrorValue wrapper as the interface data while the overlay
    // returned the message GoString. This contract drives
    // throwPanic/recover on BOTH copies over a runtime-panic matrix and
    // asserts identical outcomes, plus the absolute recover() contract
    // for string-kind runtime errors.
    //
    // NOTE (bounds errors): every in-tree bounds throw passes a null
    // descriptor, so throwPanic infers runtime.errorString/STRING and the
    // unwrap applies — the r43 comment's "struct-kind errors keep the
    // wrapper" holds only when a caller supplies a non-STRING
    // descriptor. The bounds-inferred case pins the actual behavior
    // (both copies unwrap); the bounds-explicit-struct case pins the
    // kind guard (both copies keep the wrapper).
    // ------------------------------------------------------------------

    /** One matrix case: the panic value, the descriptor throwPanic gets, and the expected recover() descriptor. */
    private record PanicCase(String name, Object value, Object descriptor,
            String expectedRecoverDescriptor) {
    }

    /** One throwPanic/recover observation on a single Panic copy. */
    private record PanicObservation(PanicException panic, Object recovered, Object input) {
        /** Comparable summary of the observable behavior. */
        String summary() {
            Object v = panic.goValue();
            String vs = (v == null ? "null" : v.getClass().getName())
                    + (v == input ? ",isInput" : "");
            Object d = panic.descriptor();
            String ds;
            if (d == null) {
                ds = "null";
            } else if (d instanceof GoTypeDescriptor td) {
                ds = td.name() + ":" + td.kind();
            } else {
                ds = "unexpected:" + d.getClass().getName();
            }
            String rs;
            if (recovered == null) {
                rs = "null";
            } else if (recovered instanceof GoInterface gi) {
                Object gd = gi.descriptor();
                String gds = gd instanceof GoTypeDescriptor gtd ? gtd.name() : String.valueOf(gd);
                rs = "iface:" + gds + ",data=" + describeData(gi.data(), input);
            } else {
                rs = "raw:" + recovered.getClass().getName()
                        + (recovered == input ? ",isInput" : "");
            }
            return "value=[" + vs + "] descriptor=[" + ds + "] recover=[" + rs + "]";
        }
    }

    /** Data detail for the summary: class plus content, so an unwrap (or its absence) is visible. */
    private static String describeData(Object data, Object input) {
        if (data == null) {
            return "null";
        }
        if (data instanceof GoString gs) {
            return "GoString(" + new String(gs.bytes(), StandardCharsets.UTF_8) + ")";
        }
        if (data instanceof GoErrorValue gev) {
            return "GoErrorValue(" + gev.errorString() + ")"
                    + (data == input ? ",isInput" : "");
        }
        return data.getClass().getName() + (data == input ? ",isInput" : "");
    }

    /** The behavioral contract matrix: string-kind runtime errors plus the struct-kind guard. */
    private static List<PanicCase> stringKindMatrix() {
        List<PanicCase> cases = new ArrayList<>();
        cases.add(new PanicCase("runtime-error-divzero",
                GoErrors.integerDivideByZero(), null, "runtime.errorString"));
        cases.add(new PanicCase("runtime-error-typeassert",
                GoErrors.interfaceConversion("interface {}", "string", "int"),
                null, "*runtime.TypeAssertionError"));
        cases.add(new PanicCase("runtime-error-negshift",
                GoErrors.negativeShift(), null, "runtime.errorString"));
        cases.add(new PanicCase("runtime-error-bounds-inferred",
                GoErrors.indexOutOfRange(5, 2, true), null, "runtime.errorString"));
        cases.add(new PanicCase("runtime-error-bounds-explicit-struct",
                GoErrors.bounds(GoErrors.BoundsCode.INDEX, 5, 2, true),
                new GoTypeDescriptor("runtime.boundsError", GoKind.STRUCT, false),
                "runtime.boundsError"));
        return cases;
    }

    /** A recover() call that may throw checked reflection exceptions. */
    private interface RecoverFn {
        Object get() throws Exception;
    }

    /** Runs fn as a deferred call during panic unwinding: arms recoverAllowed. */
    private static Object recoverArmed(PanicException p, RecoverFn fn) throws Exception {
        GoroutineState prev = GoroutineState.install(new GoroutineState());
        try {
            GoroutineState s = GoroutineState.current();
            s.setPanicking(p);
            s.setRecoverAllowed(true);
            try {
                return fn.get();
            } catch (InvocationTargetException e) {
                Throwable c = e.getCause();
                if (c instanceof Exception ex) {
                    throw ex;
                }
                throw new AssertionError("recover() threw", c);
            }
        } finally {
            GoroutineState.restore(prev);
        }
    }

    /** Throws via the overlay's 4-arg throwPanic; returns the PanicException. */
    private static PanicException catchPanicOverlay(Object value, Object descriptor) {
        try {
            Panic.throwPanic(value, descriptor, null, null);
        } catch (PanicException p) {
            return p;
        }
        throw new AssertionError("overlay throwPanic did not throw PanicException");
    }

    /** Throws via the Phase-1 copy's 4-arg throwPanic (reflective); returns it. */
    private static PanicException catchPanicPhase1(Method throwM, Object value, Object descriptor)
            throws Exception {
        try {
            throwM.invoke(null, value, descriptor, null, null);
        } catch (InvocationTargetException e) {
            if (e.getCause() instanceof PanicException p) {
                return p;
            }
            throw new AssertionError("phase-1 throwPanic threw " + e.getCause(), e.getCause());
        }
        throw new AssertionError("phase-1 throwPanic did not throw PanicException");
    }

    private static PanicObservation observeOverlay(Object value, Object descriptor)
            throws Exception {
        PanicException p = catchPanicOverlay(value, descriptor);
        return new PanicObservation(p, recoverArmed(p, Panic::recover), value);
    }

    private static PanicObservation observePhase1(Method throwM, Method recoverM,
            Object value, Object descriptor) throws Exception {
        PanicException p = catchPanicPhase1(throwM, value, descriptor);
        return new PanicObservation(p, recoverArmed(p, () -> recoverM.invoke(null)), value);
    }

    /**
     * Absolute contract for runtime panics (fix-redmaster-r43): recover()
     * must box the value as a Go {@code any} with the expected descriptor,
     * and the interface data must be the message {@link GoString} when the
     * descriptor kind is STRING, or the untouched {@code GoErrorValue}
     * wrapper when it is not.
     */
    private static void assertRecoverContract(String side, PanicCase c, PanicObservation o) {
        Object r = o.recovered();
        if (!(r instanceof GoInterface gi)) {
            throw new AssertionError(side + " case <" + c.name() + ">: recover() did not box "
                    + "the panic as GoInterface, got "
                    + (r == null ? "null" : r.getClass().getName()));
        }
        Object d = gi.descriptor();
        if (!(d instanceof GoTypeDescriptor td)
                || !c.expectedRecoverDescriptor().equals(td.name())) {
            throw new AssertionError(side + " case <" + c.name() + ">: boxed descriptor <"
                    + (d == null ? "null" : d) + ">, expected <"
                    + c.expectedRecoverDescriptor() + ">");
        }
        Object data = gi.data();
        if (td.kind() == GoKind.STRING) {
            if (!(data instanceof GoString gs)) {
                throw new AssertionError(side + " case <" + c.name()
                        + ">: string-kind recover() data is not a GoString, got "
                        + (data == null ? "null" : data.getClass().getName()));
            }
            String expected = ((GoErrorValue) o.input()).errorString();
            if (!gs.equals(GoString.fromUtf8(expected))) {
                throw new AssertionError(side + " case <" + c.name()
                        + ">: string-kind recover() data <"
                        + new String(gs.bytes(), StandardCharsets.UTF_8)
                        + ">, expected <" + expected + ">");
            }
        } else {
            // Non-string descriptor: the wrapper stays as data (its field
            // values are not recoverable from the carrier).
            if (data != o.input()) {
                throw new AssertionError(side + " case <" + c.name()
                        + ">: non-string-kind recover() data is not the panic value");
            }
        }
    }

    /**
     * Behavioral overlay↔Phase-1 parity for string-kind runtime errors: both
     * copies must produce identical throwPanic/recover outcomes for every
     * matrix case, and each must satisfy the absolute recover() contract. A
     * future one-sided behavior change — the exact failure mode of bug-019
     * and of the r43 unwrap port — fails loudly instead of silently
     * desyncing the copies.
     */
    public void testStringKindRecoverUnwrapsGoErrorValueOnBothCopies() throws Exception {
        Class<?> phase1Panic = loadPhase1PanicClass();
        Method phase1Throw = phase1Panic.getMethod("throwPanic",
                Object.class, Object.class, String.class,
                com.oracle.truffle.api.nodes.Node.class);
        Method phase1Recover = phase1Panic.getMethod("recover");
        for (PanicCase c : stringKindMatrix()) {
            PanicObservation overlay = observeOverlay(c.value(), c.descriptor());
            PanicObservation phase1 = observePhase1(phase1Throw, phase1Recover,
                    c.value(), c.descriptor());
            if (!overlay.summary().equals(phase1.summary())) {
                throw new AssertionError("behavioral drift in case <" + c.name() + ">:\n"
                        + "  overlay: " + overlay.summary() + "\n"
                        + "  phase-1: " + phase1.summary());
            }
            assertRecoverContract("overlay", c, overlay);
            assertRecoverContract("phase-1", c, phase1);
        }
    }
}
