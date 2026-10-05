package graalgo.sched.launcher;

import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import graalgo.sched.SchedTrace;

/**
 * P2-M5 (WS E): GODEBUG surface triage.
 *
 * <p>Parses the {@code GODEBUG} environment variable against the versioned
 * knob registry ({@code graalgo/sched/godebug/registry.json}, versioned;
 * see {@code phases/phase2/GODEBUG.md}). Every knob is one of:
 *
 * <ul>
 *   <li>EMULATED — graalgo implements gc-compatible observable behavior;
 *   <li>NO-OP — accepted and ignored, with a documented justification;
 *   <li>UNSUPPORTED — setting it to a non-default value is a hard error,
 *       with a documented justification.
 * </ul>
 *
 * <p>Deliberate divergences from gc (all documented in
 * {@code phases/phase2/DIVERGENCES.md}, DIV-M5-*):
 *
 * <ul>
 *   <li>an <em>unknown</em> knob name is a hard error (gc silently ignores);
 *   <li>a malformed value (not a base-10 int32) is a hard error (gc keeps
 *       the old value silently);
 *   <li>a field without {@code =} is a hard error (gc skips it silently).
 * </ul>
 *
 * <p>Empty fields (from leading/trailing/doubled commas) are skipped like
 * gc. Parsing is left-to-right, later settings overriding earlier ones
 * (gc startup semantics).
 *
 * <p>Call {@link #apply()} once at launcher startup, before the scheduler
 * is installed. On denial it throws {@link GoDebugException}; the launcher
 * must report it and exit non-zero (gc fatals with exit status 2).
 */
public final class GoDebug {
    private GoDebug() {}

    /** Denial or malformed input: message is the stderr diagnostic. */
    public static final class GoDebugException extends Exception {
        GoDebugException(String msg) {
            super(msg);
        }
    }

    /** Disposition of a registry knob. */
    public enum Disposition {
        EMULATED, NOOP, UNSUPPORTED
    }

    /** One registry knob. */
    public static final class Knob {
        public final String name;
        public final Disposition disposition;
        public final int defaultValue;
        public final String summary;
        public final String justification;

        Knob(String name, Disposition disposition, int defaultValue,
             String summary, String justification) {
            this.name = name;
            this.disposition = disposition;
            this.defaultValue = defaultValue;
            this.summary = summary;
            this.justification = justification;
        }
    }

    /** One removed (gc-deleted) knob with its "old value" predicate. */
    static final class RemovedKnob {
        final String name;
        final List<String> oldValues;

        RemovedKnob(String name, List<String> oldValues) {
            this.name = name;
            this.oldValues = oldValues;
        }
    }

    /** Parsed, validated GODEBUG settings: knob name → int32 value. */
    public static final class Settings {
        private final Map<String, Integer> values;

        Settings(Map<String, Integer> values) {
            this.values = Collections.unmodifiableMap(new LinkedHashMap<>(values));
        }

        /** Value of a knob (its default if unset). */
        public int get(String name) {
            Integer v = values.get(name);
            return v != null ? v : registryKnob(name).defaultValue;
        }

        /** Whether the knob was explicitly set (even to its default). */
        public boolean isSet(String name) {
            return values.containsKey(name);
        }

        /** Explicitly-set entries, in parse order. */
        public Map<String, Integer> explicit() {
            return values;
        }
    }

    // ------------------------------------------------------------------
    // Registry loading
    // ------------------------------------------------------------------

    private static final String REGISTRY_RESOURCE =
            "graalgo/sched/godebug/registry.json";

    private static volatile int registryVersion = -1;
    private static volatile Map<String, Knob> registryKnobs;
    private static volatile List<RemovedKnob> registryRemoved;

    /** Registry version (machine-checked by GoDebugRegistryTest). */
    public static int registryVersion() {
        ensureRegistry();
        return registryVersion;
    }

    /** All registry knob names, sorted. */
    public static List<String> knobNames() {
        ensureRegistry();
        List<String> names = new ArrayList<>(registryKnobs.keySet());
        Collections.sort(names);
        return names;
    }

    /** The registry entry for a knob (null if unknown). */
    public static Knob knob(String name) {
        ensureRegistry();
        return registryKnobs.get(name);
    }

    static Knob registryKnob(String name) {
        Knob k = knob(name);
        if (k == null) {
            throw new IllegalArgumentException("unknown knob: " + name);
        }
        return k;
    }

    static List<RemovedKnob> removedKnobs() {
        ensureRegistry();
        return registryRemoved;
    }

    private static void ensureRegistry() {
        if (registryKnobs != null) {
            return;
        }
        synchronized (GoDebug.class) {
            if (registryKnobs != null) {
                return;
            }
            loadRegistry();
        }
    }

    @SuppressWarnings("unchecked")
    private static void loadRegistry() {
        String json;
        try (InputStream in = GoDebug.class.getClassLoader()
                .getResourceAsStream(REGISTRY_RESOURCE)) {
            if (in == null) {
                throw new IllegalStateException(
                        "GODEBUG registry resource missing: " + REGISTRY_RESOURCE);
            }
            json = new String(in.readAllBytes(), StandardCharsets.UTF_8);
        } catch (IOException e) {
            throw new IllegalStateException("cannot read GODEBUG registry", e);
        }
        Map<String, Object> root = (Map<String, Object>) Json.parse(json);
        registryVersion = ((Number) root.get("version")).intValue();
        Map<String, Knob> knobs = new LinkedHashMap<>();
        for (Object o : (List<Object>) root.get("knobs")) {
            Map<String, Object> k = (Map<String, Object>) o;
            String name = (String) k.get("name");
            Disposition d = Disposition.valueOf(
                    ((String) k.get("disposition")).toUpperCase());
            int def = Integer.parseInt(k.get("default").toString());
            knobs.put(name, new Knob(name, d, def,
                    (String) k.get("summary"), (String) k.get("justification")));
        }
        List<RemovedKnob> removed = new ArrayList<>();
        Object ro = root.get("removed");
        if (ro instanceof List) {
            for (Object o : (List<Object>) ro) {
                Map<String, Object> r = (Map<String, Object>) o;
                List<String> olds = new ArrayList<>();
                for (Object v : (List<Object>) r.get("oldValues")) {
                    olds.add(v.toString());
                }
                removed.add(new RemovedKnob((String) r.get("name"), olds));
            }
        }
        // Publication order matters: the fast path in ensureRegistry()
        // checks registryKnobs, so registryRemoved must be assigned first
        // (both volatile); otherwise a reader could see a non-null map
        // with a still-null removed list.
        registryRemoved = Collections.unmodifiableList(removed);
        registryKnobs = Collections.unmodifiableMap(knobs);
    }

    // ------------------------------------------------------------------
    // Parsing
    // ------------------------------------------------------------------

    /**
     * Parses a GODEBUG value string, validating every field against the
     * registry. Throws {@link GoDebugException} on any unknown knob,
     * malformed field/value, unsupported non-default, or removed-knob
     * old value.
     */
    public static Settings parse(String godebug) throws GoDebugException {
        try {
            ensureRegistry();
        } catch (RuntimeException e) {
            // A missing/unreadable registry must deny cleanly (exit 2),
            // not escape as an uncaught IllegalStateException.
            throw new GoDebugException(
                    "cannot load GODEBUG registry: " + e.getMessage());
        }
        Map<String, Integer> values = new LinkedHashMap<>();
        if (godebug == null) {
            return new Settings(values);
        }
        // Split on commas (gc startup semantics: left to right).
        int start = 0;
        for (int i = 0; i <= godebug.length(); i++) {
            if (i < godebug.length() && godebug.charAt(i) != ',') {
                continue;
            }
            String field = godebug.substring(start, i);
            start = i + 1;
            if (field.isEmpty()) {
                continue; // leading/trailing/doubled comma, like gc
            }
            int eq = field.indexOf('=');
            if (eq < 0) {
                throw new GoDebugException("malformed GODEBUG field without '=': \""
                        + field + "\" (did you mean " + field + "=1?)");
            }
            String key = field.substring(0, eq);
            String value = field.substring(eq + 1);
            // Removed knobs: gc fatals only on "old" values.
            for (RemovedKnob r : registryRemoved) {
                if (r.name.equals(key)) {
                    if (r.oldValues.contains(value)) {
                        throw new GoDebugException("GODEBUG \"" + key
                                + "\" was removed; value \"" + value
                                + "\" requests removed behavior"
                                + " (https://go.dev/doc/godebug#removed)");
                    }
                    // Non-old value: accepted and ignored, like gc.
                    key = null;
                    break;
                }
            }
            if (key == null) {
                continue;
            }
            Knob knob = registryKnobs.get(key);
            if (knob == null) {
                throw new GoDebugException("unknown GODEBUG setting \"" + key
                        + "\" (typo? see GODEBUG.md for the supported registry)");
            }
            // ASCII digits only (Go's ParseInt rejects non-ASCII digits;
            // Long.parseLong would accept e.g. Arabic-Indic digits).
            if (!value.matches("[+-]?[0-9]+")) {
                throw new GoDebugException("malformed GODEBUG value for \""
                        + key + "\": \"" + value + "\" is not a base-10 integer");
            }
            long parsed;
            try {
                parsed = Long.parseLong(value, 10);
            } catch (NumberFormatException e) {
                throw new GoDebugException("malformed GODEBUG value for \""
                        + key + "\": \"" + value + "\" is out of range");
            }
            // gc: every knob is int32 except memprofilerate (full int).
            if (!key.equals("memprofilerate")
                    && (parsed < Integer.MIN_VALUE || parsed > Integer.MAX_VALUE)) {
                throw new GoDebugException("malformed GODEBUG value for \""
                        + key + "\": \"" + value + "\" is out of int32 range");
            }
            // gc fatals at startup on cgocheck > 1 (needs GOEXPERIMENT=
            // cgocheck2 at build time). We must deny it too: otherwise the
            // inherited GODEBUG would kill the gc-built gofrontend child
            // with a confusing build failure.
            if (key.equals("cgocheck") && parsed > 1) {
                throw new GoDebugException("GODEBUG cgocheck > 1 mode is not "
                        + "supported at runtime (like gc); use "
                        + "GOEXPERIMENT=cgocheck2 at build time instead");
            }
            int n = (int) parsed;
            if (knob.disposition == Disposition.UNSUPPORTED && n != knob.defaultValue) {
                throw new GoDebugException("unsupported GODEBUG setting \""
                        + key + "=" + value + "\": " + knob.justification);
            }
            values.put(key, n);
        }
        return new Settings(values);
    }

    // ------------------------------------------------------------------
    // Apply at startup
    // ------------------------------------------------------------------

    /**
     * Reads {@code GODEBUG} from the environment, parses and validates it,
     * and installs the EMULATED knobs. Call once at launcher startup.
     *
     * @throws GoDebugException if the value is denied (unknown knob,
     *         malformed field/value, unsupported non-default, removed old
     *         value). The launcher must print the message and exit non-zero.
     */
    public static void apply() throws GoDebugException {
        Settings s = parse(System.getenv("GODEBUG"));
        applySettings(s);
    }

    /** Installs EMULATED knobs from already-parsed settings (test seam). */
    static void applySettings(Settings s) throws GoDebugException {
        int schedtrace = s.get("schedtrace");
        if (schedtrace < 0) {
            throw new GoDebugException(
                    "GODEBUG schedtrace must be >= 0 (milliseconds)");
        }
        if (schedtrace > 0) {
            SchedTrace.start(schedtrace, s.get("scheddetail") != 0);
        }
        if (s.get("gctrace") != 0) {
            GcTrace.start();
        }
        if (s.get("panicnil") != 0) {
            graalgo.runtime.Panic.setPanicNilOldBehavior(true);
        }
    }

    // ------------------------------------------------------------------
    // Minimal JSON parser (restricted to the registry schema: objects,
    // arrays, strings with standard escapes, integers).
    // ------------------------------------------------------------------

    /** Tiny JSON parser; throws IllegalArgumentException on malformed input. */
    static final class Json {
        private final String s;
        private int i;

        private Json(String s) {
            this.s = s;
        }

        static Object parse(String s) {
            Json j = new Json(s);
            Object v = j.value();
            j.ws();
            if (j.i != j.s.length()) {
                throw new IllegalArgumentException("trailing JSON at " + j.i);
            }
            return v;
        }

        private void ws() {
            while (i < s.length() && Character.isWhitespace(s.charAt(i))) {
                i++;
            }
        }

        private Object value() {
            ws();
            if (i >= s.length()) {
                throw new IllegalArgumentException("unexpected end of JSON");
            }
            char c = s.charAt(i);
            if (c == '{') {
                return object();
            }
            if (c == '[') {
                return array();
            }
            if (c == '"') {
                return string();
            }
            if (c == '-' || (c >= '0' && c <= '9')) {
                return number();
            }
            throw new IllegalArgumentException("unexpected JSON char '" + c + "' at " + i);
        }

        private Map<String, Object> object() {
            Map<String, Object> m = new LinkedHashMap<>();
            i++; // {
            ws();
            if (i < s.length() && s.charAt(i) == '}') {
                i++;
                return m;
            }
            while (true) {
                ws();
                String k = string();
                ws();
                if (i >= s.length() || s.charAt(i) != ':') {
                    throw new IllegalArgumentException("expected ':' at " + i);
                }
                i++;
                m.put(k, value());
                ws();
                if (i >= s.length()) {
                    throw new IllegalArgumentException("unterminated object");
                }
                char c = s.charAt(i++);
                if (c == '}') {
                    return m;
                }
                if (c != ',') {
                    throw new IllegalArgumentException("expected ',' or '}' at " + (i - 1));
                }
            }
        }

        private List<Object> array() {
            List<Object> l = new ArrayList<>();
            i++; // [
            ws();
            if (i < s.length() && s.charAt(i) == ']') {
                i++;
                return l;
            }
            while (true) {
                l.add(value());
                ws();
                if (i >= s.length()) {
                    throw new IllegalArgumentException("unterminated array");
                }
                char c = s.charAt(i++);
                if (c == ']') {
                    return l;
                }
                if (c != ',') {
                    throw new IllegalArgumentException("expected ',' or ']' at " + (i - 1));
                }
            }
        }

        private String string() {
            if (s.charAt(i) != '"') {
                throw new IllegalArgumentException("expected string at " + i);
            }
            i++;
            StringBuilder b = new StringBuilder();
            while (true) {
                if (i >= s.length()) {
                    throw new IllegalArgumentException("unterminated string");
                }
                char c = s.charAt(i++);
                if (c == '"') {
                    return b.toString();
                }
                if (c == '\\') {
                    if (i >= s.length()) {
                        throw new IllegalArgumentException("bad escape");
                    }
                    char e = s.charAt(i++);
                    switch (e) {
                        case '"': b.append('"'); break;
                        case '\\': b.append('\\'); break;
                        case '/': b.append('/'); break;
                        case 'b': b.append('\b'); break;
                        case 'f': b.append('\f'); break;
                        case 'n': b.append('\n'); break;
                        case 't': b.append('\t'); break;
                        case 'r': b.append('\r'); break;
                        case 'u':
                            if (i + 4 > s.length()) {
                                throw new IllegalArgumentException("bad \\u escape");
                            }
                            b.append((char) Integer.parseInt(s.substring(i, i + 4), 16));
                            i += 4;
                            break;
                        default: throw new IllegalArgumentException("bad escape \\" + e);
                    }
                } else {
                    b.append(c);
                }
            }
        }

        private Number number() {
            int j = i;
            if (s.charAt(j) == '-') {
                j++;
            }
            while (j < s.length() && Character.isDigit(s.charAt(j))) {
                j++;
            }
            long v = Long.parseLong(s.substring(i, j));
            i = j;
            return v;
        }
    }
}
