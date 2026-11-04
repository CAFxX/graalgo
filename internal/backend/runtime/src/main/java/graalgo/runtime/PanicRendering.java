package graalgo.runtime;

import graalgo.values.GoKind;
import graalgo.values.GoString;
import graalgo.values.GoTypeDescriptor;

/**
 * Renders a panic value for the {@code panic: } header (M5, WS F).
 *
 * <p>Ports {@code printpanicval}, {@code printanycustomtype}, and
 * {@code printindented} from go1.27.1's runtime/error.go, plus the
 * {@code preprintpanics} step from runtime/panic.go: a panic value that
 * implements {@code error} (or, failing that, {@code Stringer}) prints
 * its {@code Error()} (resp. {@code String()}) text, not its type and
 * address. Runtime panics arrive as a {@link GoErrorValue} carrying that
 * text already; user error/Stringer values are resolved at render time
 * by the installed preprint provider (below), because calling the method
 * is guest-code execution and must happen at fatal-print time — gc runs
 * {@code preprintpanics} only once the panic is uncaught, so a recovered
 * panic still yields the original value from {@code recover()}. This
 * class never converts the panic value itself.
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
     * Preprint provider (gc {@code preprintpanics}, NEW-P1-10): given an
     * uncaught panic, returns the {@code Error()}/{@code String()} text
     * for a value whose dynamic type implements {@code error} or
     * {@code Stringer}, or null when the value implements neither (or
     * the text cannot be produced). The provider lives outside this
     * module — this module compiles before the interop module that owns
     * the interface method tables, so it cannot resolve methods itself —
     * and is installed by the product launcher alongside the itab
     * provider (same pattern as {@link Intrinsics#setItabProvider}).
     * Absent (phase-1 launcher, phase-2 scheduler: no method tables
     * there at all), rendering falls back to the kind-based rules below.
     * A provider failure (including a panic inside {@code Error()} /
     * {@code String()} itself) likewise falls back instead of breaking
     * the crash report; gc's fatal {@code "panic while printing panic
     * value"} shape for that case is a documented divergence.
     */
    private static volatile java.util.function.Function<PanicException, String> preprintProvider;

    /** Installs (or, with null, clears) the preprint provider. */
    public static void setPreprintProvider(
            java.util.function.Function<PanicException, String> provider) {
        preprintProvider = provider;
    }

    /**
     * Renders the full {@code panic: <value>} header line for an uncaught
     * panic (P3-M4, WS C): the same header the phase-1 launcher prints,
     * shared with the phase-2/3 scheduler panic path so both render
     * gc-identical headers (previously the scheduler printed the raw
     * {@code toString}, diverging on error values).
     */
    public static String renderPanicLine(PanicException p) {
        Object v = p.goValue();
        if (v instanceof GoErrorValue e) {
            return "panic: " + printIndented(e.errorString());
        }
        java.util.function.Function<PanicException, String> provider = preprintProvider;
        if (provider != null) {
            String preprint = null;
            try {
                preprint = provider.apply(p);
            } catch (Throwable ignored) {
                // Fall through to kind-based rendering (see field doc).
            }
            if (preprint != null) {
                return "panic: " + printIndented(preprint);
            }
        }
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
                        ? typeName + "(\"" + printIndented(GoQuote.quote(((GoString) value).bytes())) + "\")"
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
