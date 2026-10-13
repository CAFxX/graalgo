package graalgo.values;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.IdentityHashMap;
import java.util.List;
import java.util.Map;

/**
 * Call-site polymorphism measurement hooks (M9, WS L).
 *
 * <p>Records, per instrumented call site, the identities of the compiled
 * function targets invoked through the site and (for method calls) the
 * receiver type keys observed. When disabled (the default) the cost is a
 * single volatile read per call; when enabled each site accumulates an
 * identity histogram. Compiled targets ({@code RootCallTarget}) and type
 * descriptors are immutable, so identity is a stable polymorphism key.
 *
 * <p>This is the Phase-1 input to the devirtualization thesis (SPEC §6.5a):
 * monomorphic sites devirtualize, megamorphic sites don't. Interface
 * dispatch has no implementation in Phase 1 (see {@link InterfaceDispatchProbes}
 * for the future hooks); the measurable sites are {@code FuncValueCallNode}
 * invocations — direct calls, closures, func-typed variables, and statically
 * resolved method calls.
 *
 * <p>Sites are keyed by source position ({@code file:line:col} of the call);
 * {@link #snapshotJson} renders every site's histograms for
 * {@code graalgo-run --dump-polymorphism}.
 */
public final class CallSiteProbes {
    private static volatile boolean enabled = false;

    private CallSiteProbes() {}

    /** Enables or disables recording. Disabled by default. */
    public static void setEnabled(boolean e) {
        enabled = e;
    }

    public static boolean isEnabled() {
        return enabled;
    }

    /** One instrumented call site's identity histograms. */
    public static final class Site {
        private final String name;
        private final Map<Object, long[]> targets = new IdentityHashMap<>();
        private final Map<Object, long[]> receivers = new IdentityHashMap<>();

        private Site(String name) {
            this.name = name;
        }

        /** Records one invocation of compiled-target {@code key} (no-op unless enabled). */
        public void recordTarget(Object key) {
            if (!enabled || key == null) {
                return;
            }
            synchronized (targets) {
                targets.computeIfAbsent(key, k -> new long[1])[0]++;
            }
        }

        /** Records one method-call receiver type key (no-op unless enabled). */
        public void recordReceiver(Object key) {
            if (!enabled || key == null) {
                return;
            }
            synchronized (receivers) {
                receivers.computeIfAbsent(key, k -> new long[1])[0]++;
            }
        }

        public String name() {
            return name;
        }

        /** Distinct compiled targets observed at this site. */
        public int targetCardinality() {
            synchronized (targets) {
                return targets.size();
            }
        }

        /** Distinct receiver type keys observed at this site. */
        public int receiverCardinality() {
            synchronized (receivers) {
                return receivers.size();
            }
        }

        public long totalCalls() {
            long t = 0;
            synchronized (targets) {
                for (long[] c : targets.values()) {
                    t += c[0];
                }
            }
            return t;
        }
    }

    private static final Map<String, Site> sites = new HashMap<>();

    /** Registers (or returns the existing) site for {@code name}. */
    public static synchronized Site registerSite(String name) {
        return sites.computeIfAbsent(name, Site::new);
    }

    /**
     * Receiver type key for a method-call receiver value: the struct
     * descriptor's identity. A {@code GoInterface} receiver unwraps to its
     * dynamic descriptor's identity (M9 review #2 P2-1) so that, when WS D
     * lands interface dispatch, distinct dynamic types do not collapse to
     * one key.
     */
    public static Object receiverKey(Object recv) {
        Object v = recv;
        if (v instanceof GoPointer p) {
            v = p.referent();
        }
        if (v instanceof GoInterface i) {
            v = i.data();
            if (v instanceof GoPointer p2) {
                v = p2.referent();
            }
        }
        if (v instanceof GoStruct s) {
            return s.descriptor();
        }
        return v == null ? null : v.getClass();
    }

    /** JSON snapshot of every registered site, for {@code --dump-polymorphism}. */
    public static synchronized String snapshotJson() {
        List<String> names = new ArrayList<>(sites.keySet());
        names.sort(String::compareTo);
        StringBuilder sb = new StringBuilder();
        sb.append("{\"sites\":[");
        boolean first = true;
        for (String n : names) {
            Site s = sites.get(n);
            if (!first) {
                sb.append(',');
            }
            first = false;
            sb.append("{\"site\":").append(quote(n));
            sb.append(",\"calls\":").append(s.totalCalls());
            sb.append(",\"targets\":").append(s.targetCardinality());
            sb.append(",\"receiverTypes\":").append(s.receiverCardinality());
            sb.append('}');
        }
        sb.append("]}");
        return sb.toString();
    }

    /** Clears all registered sites (tests). */
    public static synchronized void reset() {
        sites.clear();
    }

    private static String quote(String s) {
        // M9 review #2 P2-6: escape per RFC 8259 (control characters, not
        // just backslash and quote) so a hostile filename cannot emit
        // invalid JSON.
        StringBuilder sb = new StringBuilder(s.length() + 2);
        sb.append('"');
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            switch (c) {
                case '"' -> sb.append("\\\"");
                case '\\' -> sb.append("\\\\");
                case '\n' -> sb.append("\\n");
                case '\r' -> sb.append("\\r");
                case '\t' -> sb.append("\\t");
                case '\b' -> sb.append("\\b");
                case '\f' -> sb.append("\\f");
                default -> {
                    if (c < 0x20) {
                        sb.append(String.format("\\u%04x", (int) c));
                    } else {
                        sb.append(c);
                    }
                }
            }
        }
        sb.append('"');
        return sb.toString();
    }
}
