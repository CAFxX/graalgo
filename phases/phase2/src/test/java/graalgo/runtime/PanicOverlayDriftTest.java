package graalgo.runtime;

import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.net.URL;
import java.net.URLClassLoader;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.List;
import java.util.TreeSet;

/**
 * P2-M5 (WS E): drift guard for the Phase-2 {@code graalgo.runtime.Panic}
 * same-FQN overlay (panicnil emulation).
 *
 * <p>The overlay shadows Phase-1's {@code Panic} at runtime (phase2 classes
 * precede phase1 on the classpath). If Phase 1 ever changes {@code Panic}'s
 * public surface, the overlay would silently desync. This test loads the
 * Phase-1 original via an isolated classloader and asserts every public
 * method of the original exists here with an identical signature. The
 * overlay's additive methods (setPanicNilOldBehavior,
 * isPanicNilOldBehavior) are exempt.
 *
 * <p>verifies: GG-0055 (panicnil emulation does not desync from Phase 1)
 */
public final class PanicOverlayDriftTest {

    /** Additive overlay methods, exempt from the drift check. */
    private static final TreeSet<String> ADDITIVE = new TreeSet<>(List.of(
            "setPanicNilOldBehavior", "isPanicNilOldBehavior"));

    public void testOverlayMatchesPhase1Surface() throws Exception {
        Path p1 = Paths.get("../phase1/out/classes").toAbsolutePath().normalize();
        if (!p1.toFile().isDirectory()) {
            throw new AssertionError("phase1 classes not built: " + p1);
        }
        // Child-first for exactly graalgo.runtime.Panic: phase1 classes URL
        // first, app classloader as parent for everything else (Truffle,
        // GoInterface, ...). This loads the Phase-1 *original*, not the
        // overlay the app classloader would return parent-first.
        List<URL> urls = new ArrayList<>();
        urls.add(p1.toUri().toURL());
        ClassLoader app = PanicOverlayDriftTest.class.getClassLoader();
        URLClassLoader p1Loader = new URLClassLoader(urls.toArray(new URL[0]), app) {
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
        Class<?> p1Panic;
        try {
            p1Panic = Class.forName("graalgo.runtime.Panic", false, p1Loader);
        } catch (ClassNotFoundException e) {
            throw new AssertionError("Phase-1 Panic not found", e);
        }
        Class<?> overlay = Panic.class; // resolves to the overlay (phase2 first)
        List<String> missing = new ArrayList<>();
        for (Method m : p1Panic.getDeclaredMethods()) {
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
            throw new AssertionError("Panic overlay drifted from Phase-1: " + missing);
        }
        // The overlay must not widen the surface beyond the additive methods.
        for (Method m : overlay.getDeclaredMethods()) {
            if (!Modifier.isPublic(m.getModifiers()) || ADDITIVE.contains(m.getName())) {
                continue;
            }
            boolean found = false;
            for (Method pm : p1Panic.getDeclaredMethods()) {
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
}
