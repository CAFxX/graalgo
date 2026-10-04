package graalgo.values;

import java.util.HashMap;
import java.util.IdentityHashMap;
import java.util.Map;

/**
 * Call-site polymorphism measurement hooks for interface dispatch (WS D,
 * consumed by M9).
 *
 * <p>Hooks only: there is no interface dispatch implementation in Phase 1.
 * When enabled, {@link #record} logs the itab <em>identity</em> observed at
 * a dispatch site into a per-site histogram; when disabled (the default)
 * the cost is a single volatile read. Itabs are immutable (D6), so identity
 * is a stable polymorphism key.
 *
 * <p>Sites register with {@link #registerSite} and record through the
 * returned {@link Site}; {@link #snapshot} renders every site's histogram
 * for {@code --dump-polymorphism} (WS L).
 */
public final class InterfaceDispatchProbes {
    /** One instrumented dispatch site's itab-identity histogram. */
    public static final class Site {
        private final String name;
        private final Map<Object, long[]> counts = new IdentityHashMap<>();

        private Site(String name) {
            this.name = name;
        }

        /** Records one dispatch through {@code itab} (no-op unless enabled). */
        public void record(Object itab) {
            if (!enabled) {
                return;
            }
            synchronized (counts) {
                counts.computeIfAbsent(itab, k -> new long[1])[0]++;
            }
        }

        public String name() {
            return name;
        }

        /** Itab identity hash → dispatch count (a copy). */
        public synchronized Map<Integer, Long> histogram() {
            Map<Integer, Long> out = new HashMap<>();
            synchronized (counts) {
                for (Map.Entry<Object, long[]> e : counts.entrySet()) {
                    out.put(System.identityHashCode(e.getKey()), e.getValue()[0]);
                }
            }
            return out;
        }

        public long total() {
            long t = 0;
            synchronized (counts) {
                for (long[] c : counts.values()) {
                    t += c[0];
                }
            }
            return t;
        }
    }

    private static volatile boolean enabled = false;
    private static final Map<String, Site> sites = new HashMap<>();

    private InterfaceDispatchProbes() {}

    public static void setEnabled(boolean e) {
        enabled = e;
    }

    public static boolean isEnabled() {
        return enabled;
    }

    /** Registers (or returns) the named dispatch site. Idempotent. */
    public static synchronized Site registerSite(String name) {
        return sites.computeIfAbsent(name, Site::new);
    }

    /** Clears all histograms (between measurement runs). */
    public static synchronized void reset() {
        sites.clear();
    }

    public static synchronized Map<String, Site> sites() {
        return Map.copyOf(sites);
    }
}
