package graalgo.p3os;

import graalgo.launcher.OsSurfaceInitializer;

/**
 * {@link OsSurfaceInitializer} backed by {@link GoOs}: (re)initializes
 * the Go os surface — argv, env overlay, virtual cwd, stdio cookies —
 * for one program run. Registered via {@code
 * META-INF/services/graalgo.launcher.OsSurfaceInitializer} so the plain
 * launcher discovers it with {@link java.util.ServiceLoader} without a
 * compile-time dependency on this module.
 *
 * <p>Net state ({@code graalgo.p3net.GoNet} / {@code GoNetpoll}) is not
 * reset by this class: that wiring lands in {@link GoOs#init} itself
 * (review B-04); this class only forwards the call, so a per-entry
 * {@code init} in batch mode picks the complete reset up with it.
 */
public final class GoOsSurfaceInitializer implements OsSurfaceInitializer {
    @Override
    public void initialize(String[] argv) {
        GoOs.init(argv);
    }
}
