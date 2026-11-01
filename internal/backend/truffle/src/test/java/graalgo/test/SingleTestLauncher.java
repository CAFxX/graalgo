// Single-test launcher: runs one test class's test* methods by name filter.
// Usage: java ... graalgo.test.SingleTestLauncher <fully.qualified.ClassName> [methodPrefix]
package graalgo.test;

import graalgo.launcher.OverlayGate;

public final class SingleTestLauncher {
    public static void main(String[] args) throws Exception {
        // P1-5: same fail-fast as the test runners — a truffle-first
        // classpath would otherwise load the Phase-1 stubs into the
        // singled-out test instead of failing. Before the usage check:
        // even a usage error must not exit while the stub is loaded
        // without the FATAL message having had its chance (the gate
        // throws, so a wrong-order invocation never reaches usage).
        OverlayGate.assertSchedulerOverlay();
        if (args.length < 1) {
            System.err.println("usage: SingleTestLauncher <class> [prefix]");
            System.exit(2);
        }
        String prefix = args.length > 1 ? args[1] : "test";
        Class<?> c = Class.forName(args[0]);
        Object inst = c.getDeclaredConstructor().newInstance();
        int run = 0, failed = 0;
        for (java.lang.reflect.Method m : c.getMethods()) {
            if (!m.getName().startsWith(prefix) || m.getParameterCount() != 0
                    || !java.lang.reflect.Modifier.isPublic(m.getModifiers())) {
                continue;
            }
            run++;
            try {
                m.invoke(inst);
                System.out.println("PASS " + c.getSimpleName() + "." + m.getName());
            } catch (Exception e) {
                failed++;
                Throwable cause = e.getCause() != null ? e.getCause() : e;
                System.out.println("FAIL " + c.getSimpleName() + "." + m.getName() + ": " + cause);
                cause.printStackTrace(System.out);
            }
        }
        System.out.println(run + " tests, " + failed + " failures");
        if (failed > 0) System.exit(1);
    }
}
