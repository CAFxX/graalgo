package graalgo.runtime;

import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

import graalgo.values.GoFunc;
import graalgo.values.GoString;

/**
 * The runtime side of {@code internal/godebug}'s {@code //go:linkname}
 * providers (fix-linkname, Caddy/godebug lane).
 *
 * <p>In gc, package runtime provides these symbols and package
 * internal/godebug consumes them:
 * <ul>
 *   <li>{@code internal/godebug.setUpdate} — {@code runtime.godebug_setUpdate}:
 *       registers the callback the runtime invokes as
 *       {@code update(def, env)} whenever the {@code GODEBUG} setting
 *       changes (and once at registration);</li>
 *   <li>{@code internal/godebug.registerMetric} — forwards metric
 *       registrations to runtime/metrics;</li>
 *   <li>{@code internal/godebug.setNewIncNonDefault} — lets the runtime
 *       construct non-default-behavior counters without importing
 *       godebug.</li>
 * </ul>
 *
 * <p>graalgo's runtime is synthetic (no Go bodies), so the linkname
 * table registers these symbols as intrinsics delegating here.
 * {@code setUpdate} invokes the callback immediately with
 * {@code ("", current GODEBUG)}, matching gc's registration-time call;
 * later {@code os.Setenv}/{@code os.Unsetenv}/{@code os.Clearenv} of
 * {@code GODEBUG} re-invoke it via {@link #onGodebugChange} (wired by the
 * interop {@code GoOs} layer, which owns the environment overlay).
 * The metric registry is retained for a future runtime/metrics surface;
 * nothing reads it yet.
 *
 * <p>All state is per-run: {@link #clear()} resets it (called by the
 * launcher's linkname install, alongside {@code Linknames.clear()}).
 */
public final class GoDebug {
    private GoDebug() {}

    private static volatile GoFunc updateFunc;
    private static final Map<String, GoFunc> METRICS = new ConcurrentHashMap<>();
    private static volatile GoFunc newIncNonDefaultFunc;

    /** Removes all registrations (launcher, per run). */
    public static void clear() {
        updateFunc = null;
        METRICS.clear();
        newIncNonDefaultFunc = null;
    }

    /**
     * Registers the godebug update callback and invokes it once with
     * {@code ("", env)}, where env is the current {@code $GODEBUG} value
     * (gc's registration-time {@code update(def, env)} call).
     */
    public static void setUpdate(GoFunc f, String env) {
        if (f == null) {
            throw new IllegalArgumentException("GoDebug.setUpdate: null func");
        }
        updateFunc = f;
        invokeUpdate(env);
    }

    /**
     * Re-invokes the registered update callback after a
     * {@code GODEBUG} environment change (gc: the runtime calls
     * {@code update(def, env)} on every change). No-op when no callback
     * is registered.
     */
    public static void onGodebugChange(String env) {
        GoFunc f = updateFunc;
        if (f != null) {
            invokeUpdate(env);
        }
    }

    private static void invokeUpdate(String env) {
        GoFunc f = updateFunc;
        if (f == null) {
            return;
        }
        GoCalls.invoke(f, new Object[]{
                GoString.fromUtf8(""), GoString.fromUtf8(env == null ? "" : env)});
    }

    /** Registers a named metric reader (runtime/metrics feed; retained). */
    public static void registerMetric(String name, GoFunc read) {
        if (name == null || read == null) {
            throw new IllegalArgumentException("GoDebug.registerMetric: null name or func");
        }
        METRICS.put(name, read);
    }

    /** Registers the runtime's non-default-counter constructor. */
    public static void setNewIncNonDefault(GoFunc f) {
        if (f == null) {
            throw new IllegalArgumentException("GoDebug.setNewIncNonDefault: null func");
        }
        newIncNonDefaultFunc = f;
    }
}
