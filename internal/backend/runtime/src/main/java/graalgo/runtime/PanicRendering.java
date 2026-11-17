package graalgo.runtime;

import graalgo.values.GoKind;
import graalgo.values.GoString;
import graalgo.values.GoTypeDescriptor;

/**
 * Renders a panic value for the {@code panic: } header (M5, WS F).
 *
 * <p>Ports {@code printpanicval}, {@code printanycustomtype}, and
 * {@code printindented} from go1.27.1's runtime/error.go. Callers apply
 * {@code preprintpanics} first: any Go error/Stringer value arrives as a
 * {@link GoErrorValue} carrying its {@code Error()}/{@code String()} text
 * (in v1 only runtime panics produce these; user error values need methods,
 * which are WS D).
 *
 * <p>Rendering rules (all gc-verified):
 * <ul>
 *   <li>predeclared bool/int/uint/float/complex kinds → bare value
 *       (uint64 full range; floats via Go's {@code 'g'} shortest format);
 *   <li>predeclared string → raw bytes, newlines indented ({@code \n\t});
 *   <li>named basic types → {@code Name(value)} (named strings as
 *       {@code Name("quoted")});
 *   <li>everything else (slices, maps, structs, arrays, pointers, chans,
 *       funcs) → {@code (gotype) 0xADDR};
 *   <li>error values → the {@code Error()} text (which already contains any
 *       {@code "runtime error: "} prefix, as gc's implementations do).
 * </ul>
 */
public final class PanicRendering {
    private PanicRendering() {}

    /**
     * Renders the full {@code panic: <value>} header line for an uncaught
     * panic (P3-M4, WS C): the same header the phase-1 launcher prints,
     * shared with the phase-2/3 scheduler panic path so both render
     * gc-identical headers (previously the scheduler printed the raw
     * {@code toString}, diverging on error values).
     */
    /**
     * Resolves a panic value's {@code Error()} (else {@code String()})
     * text for gc's {@code preprintpanics}: {@code (descriptor, value)}
     * &rarr; the method's result, or null when the dynamic type has no such
     * method. Installed by the interop launcher (the method tables live
     * there); unset, values render structurally.
     */
    private static volatile java.util.function.BiFunction<Object, Object, String> methodText;

    /** Installs the {@link #methodText} resolver (launcher-wide). */
    public static void setMethodTextResolver(java.util.function.BiFunction<Object, Object, String> r) {
        methodText = r;
    }

    /**
     * Renders the crash header for an uncaught panic chain, exactly like
     * gc's {@code preprintpanics} + {@code printpanics}
     * (runtime/panic.go): the oldest panic first, each later one on its
     * own line indented by a tab; recovered panics marked
     * {@code [recovered]}; a panic re-raised with the identical value
     * printed once, as {@code [recovered, repanicked]}. Error and Stringer
     * values print their {@code Error()}/{@code String()} text. Must run
     * on the panicking goroutine (the methods are Go code). Ends with a
     * newline.
     */
    public static String renderPanicChain(PanicException top) {
        java.util.List<PanicException> chain = new java.util.ArrayList<>();
        for (PanicException p = top; p != null; p = p.link()) {
            chain.add(p); // newest first
        }
        int n = chain.size();
        boolean[] repanicked = new boolean[n];
        String[] text = new String[n];
        for (int i = 0; i < n; i++) {
            if (i + 1 < n && sameValue(chain.get(i + 1), chain.get(i))) {
                repanicked[i + 1] = true;
                continue;
            }
            text[i] = renderPanicValue(chain.get(i));
        }
        StringBuilder sb = new StringBuilder();
        // printpanics recursion, unrolled from the oldest panic.
        for (int i = n - 1; i >= 0; i--) {
            if (i < n - 1) {
                if (repanicked[i + 1]) {
                    break; // gc: the re-raise of a repanicked value is not printed
                }
                sb.append('\t');
            }
            PanicException p = chain.get(i);
            sb.append("panic: ").append(text[i]);
            if (p.isRecovered() && repanicked[i]) {
                sb.append(" [recovered, repanicked]");
            } else if (p.isRecovered()) {
                sb.append(" [recovered]");
            }
            sb.append('\n');
        }
        return sb.toString();
    }

    /**
     * gc compares the two panics' interface words ({@code eface ==}):
     * same dynamic type, same data pointer. A recovered string-kind
     * runtime error re-boxes its message (see {@link Panic#recover}), so
     * that pair compares by message.
     */
    private static boolean sameValue(PanicException older, PanicException newer) {
        if (older.descriptor() != newer.descriptor()) {
            return false;
        }
        Object a = older.goValue();
        Object b = newer.goValue();
        if (a == b) {
            return true;
        }
        if (a instanceof GoErrorValue ea && b instanceof GoString sb) {
            return ea.errorString().equals(utf8(sb));
        }
        return false;
    }

    /** One chain entry's value text (preprintpanics + printpanicval). */
    private static String renderPanicValue(PanicException p) {
        Object v = p.goValue();
        if (v == null && Panic.isPanicNilOldBehavior()) {
            return "nil";
        }
        if (!(v instanceof GoErrorValue)) {
            java.util.function.BiFunction<Object, Object, String> r = methodText;
            if (r != null && p.descriptor() != null) {
                String t = r.apply(p.descriptor(), v);
                if (t != null) {
                    return printIndented(t);
                }
            }
        }
        RenderInfo info = infoFor(p.descriptor(), p.goTypeName());
        GoKind kind = info != null ? info.kind() : null;
        String typeName = info != null ? info.typeName() : p.goTypeName();
        return renderValue(v, kind, typeName);
    }

    public static String renderPanicLine(PanicException p) {
        Object v = p.goValue();
        RenderInfo info = infoFor(p.descriptor(), p.goTypeName());
        GoKind kind = info != null ? info.kind() : null;
        String typeName = info != null ? info.typeName() : p.goTypeName();
        return "panic: " + renderValue(v, kind, typeName);
    }

    /**
     * Renders the panic header value.
     *
     * @param value    the Go value (D5 mapping; a {@link GoErrorValue} for
     *                 errors).
     * @param kind     the value's kind with untyped kinds normalized to
     *                 their default (may be null on the degraded path).
     * @param typeName Go-syntax type name; non-null exactly for named basic
     *                 types (e.g. {@code main.MyInt}) and composite types
     *                 (e.g. {@code []int}), null for predeclared basics.
     */
    public static String renderValue(Object value, GoKind kind, String typeName) {
        if (value instanceof GoErrorValue e) {
            return printIndented(e.errorString());
        }
        // P3-M4: a GoString value renders as a string even when the kind
        // is unknown (no descriptor — e.g. panics thrown from runtime
        // helpers). Without this, string panics degrade to "(?) 0xADDR".
        if (value instanceof GoString gs && kind == null) {
            return printIndented(utf8(gs));
        }
        if (kind == null) {
            return "(" + (typeName != null ? typeName : "?") + ") 0xADDR";
        }
        boolean named = typeName != null;
        return switch (kind) {
            case BOOL -> named ? typeName + "(" + value + ")" : value.toString();
            case INT8, INT16, INT32, INT64 -> {
                String s = Long.toString(((Number) value).longValue());
                yield named ? typeName + "(" + s + ")" : s;
            }
            case UINT8, UINT16, UINT32, UINT64 -> {
                String s = Long.toUnsignedString(((Number) value).longValue());
                yield named ? typeName + "(" + s + ")" : s;
            }
            case FLOAT32 -> {
                String s = GoFloatFmt.formatFloat(((Number) value).floatValue(), 'g', -1, 32);
                yield named ? typeName + "(" + s + ")" : s;
            }
            case FLOAT64 -> {
                String s = GoFloatFmt.formatFloat(((Number) value).doubleValue(), 'g', -1, 64);
                yield named ? typeName + "(" + s + ")" : s;
            }
            case STRING -> {
                String s = utf8((GoString) value);
                yield named
                        // gc printanycustomtype: raw bytes between the
                        // quotes (printindented, no escaping).
                        ? typeName + "(\"" + printIndented(s) + "\")"
                        : printIndented(s);
            }
            default -> "(" + (typeName != null ? typeName : kind.name().toLowerCase()) + ") 0xADDR";
        };
    }

    /**
     * Derives the rendering inputs from a panic descriptor. Returns null
     * when the descriptor is absent (degraded path).
     */
    public static RenderInfo infoFor(Object descriptor, String goTypeName) {
        if (!(descriptor instanceof GoTypeDescriptor d)) {
            return null;
        }
        GoKind kind = d.kind();
        // Untyped kinds take their default type when the value materializes
        // (mirrors go/types: an untyped constant passed to panic becomes its
        // default type).
        kind = switch (kind) {
            case UNTYPED_INT -> GoKind.INT64;
            case UNTYPED_FLOAT -> GoKind.FLOAT64;
            case UNTYPED_STRING -> GoKind.STRING;
            case UNTYPED_BOOL -> GoKind.BOOL;
            default -> kind;
        };
        return new RenderInfo(kind, goTypeName);
    }

    /** Rendering inputs: normalized kind + optional Go-syntax type name. */
    public record RenderInfo(GoKind kind, String typeName) {}

    /**
     * Replaces {@code \n} with {@code \n\t} so the traceback stays
     * unambiguously parseable (runtime/error.go {@code printindented}).
     */
    public static String printIndented(String s) {
        int i = s.indexOf('\n');
        if (i < 0) {
            return s;
        }
        StringBuilder sb = new StringBuilder(s.length() + 8);
        int start = 0;
        while (i >= 0) {
            sb.append(s, start, i + 1).append('\t');
            start = i + 1;
            i = s.indexOf('\n', start);
        }
        sb.append(s, start, s.length());
        return sb.toString();
    }

    private static String utf8(GoString gs) {
        return new String(gs.bytes(), java.nio.charset.StandardCharsets.UTF_8);
    }
}
