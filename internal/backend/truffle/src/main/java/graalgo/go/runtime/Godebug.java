package graalgo.go.runtime;

/**
 * The runtime's own {@code GODEBUG} settings (upstream runtime1.go {@code parsedebugvars},
 * {@code reparsedebugvars} and {@code parsegodebug}), for the settings the backend's runtime reads
 * ({@code panicnil}). A setting is {@code key=value} in a comma-separated list; its value is an
 * {@code int32} in decimal, and an unparsable one is ignored. The compile-time default is the
 * link's {@code -X runtime.godebugDefault} (cmd/go: go.mod's go version, {@code godebug} lines,
 * {@code //go:debug} directives).
 */
public final class Godebug {
    private Godebug() {}

    /** At startup: the default, then the environment, each left to right; later settings win. */
    public static int startup(String key, String def, String env) {
        return scanLeft(key, env, scanLeft(key, def, 0));
    }

    /**
     * After the environment variable changed to {@code env}: its rightmost setting of the key
     * decides, else the default's rightmost, else the key returns to 0. A deciding setting whose
     * value does not parse leaves {@code current} unchanged.
     */
    public static int reparse(String key, String def, String env, int current) {
        long v = scanRight(key, env, current);
        if (v == ABSENT) {
            v = scanRight(key, def, current);
        }
        return v == ABSENT ? 0 : (int) v;
    }

    private static final long ABSENT = Long.MIN_VALUE;

    private static int scanLeft(String key, String list, int v) {
        if (list == null) {
            return v;
        }
        for (String field : list.split(",", -1)) {
            int eq = field.indexOf('=');
            if (eq >= 0 && field.substring(0, eq).equals(key)) {
                long n = parseInt32(field.substring(eq + 1));
                if (n != ABSENT) {
                    v = (int) n;
                }
            }
        }
        return v;
    }

    private static long scanRight(String key, String list, int current) {
        if (list == null) {
            return ABSENT;
        }
        String[] fields = list.split(",", -1);
        for (int i = fields.length - 1; i >= 0; i--) {
            int eq = fields[i].indexOf('=');
            if (eq >= 0 && fields[i].substring(0, eq).equals(key)) {
                long n = parseInt32(fields[i].substring(eq + 1));
                return n == ABSENT ? current : n;
            }
        }
        return ABSENT;
    }

    /** {@code strconv.ParseInt(s, 10, 32)}; {@link #ABSENT} when it fails. */
    private static long parseInt32(String s) {
        int i = 0;
        boolean neg = false;
        if (!s.isEmpty() && (s.charAt(0) == '+' || s.charAt(0) == '-')) {
            neg = s.charAt(0) == '-';
            i = 1;
        }
        if (i == s.length()) {
            return ABSENT;
        }
        long n = 0;
        for (; i < s.length(); i++) {
            char c = s.charAt(i);
            if (c < '0' || c > '9') {
                return ABSENT;
            }
            n = n * 10 + (c - '0');
            if (n > (long) Integer.MAX_VALUE + 1) {
                return ABSENT;
            }
        }
        n = neg ? -n : n;
        return n < Integer.MIN_VALUE || n > Integer.MAX_VALUE ? ABSENT : n;
    }
}
