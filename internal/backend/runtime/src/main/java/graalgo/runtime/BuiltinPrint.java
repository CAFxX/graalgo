package graalgo.runtime;
// verifies: GG-0020 (WS G intrinsic surface)

import graalgo.values.GoComplex32;
import graalgo.values.GoComplex64;
import graalgo.values.GoKind;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
import java.io.ByteArrayOutputStream;
import java.nio.charset.StandardCharsets;

/**
 * Go's {@code print}/{@code println} builtins (WS G, REAL).
 *
 * <p>Semantics ported from {@code runtime/print.go} +
 * {@code cmd/compile/internal/walk/builtin.go walkPrint} (gc):
 * <ul>
 *   <li>output goes to <b>stderr</b> (unlike the M1 hello fixture's
 *       {@link Runtime#println}, which deliberately uses stdout);</li>
 *   <li>{@code println(a, b)} ≡ {@code print(a, " ", b, "\n")} — a single
 *       space between every pair of arguments and a trailing newline;</li>
 *   <li>{@code print(a, b)} ≡ {@code print(a); print(b)} — no separators,
 *       no trailing newline;</li>
 *   <li>per-type formats: bool → {@code true}/{@code false}; ints/uints →
 *       base-10; float32/64 → shortest {@code 'g'}; complex64/128 →
 *       {@code (re+imi)} shortest {@code 'g'}; string → raw bytes;
 *       slice → {@code [len/cap]0x0}; ptr/chan/map/func →
 *       {@code 0x0}; interface → {@code (0x0,0x0)}.</li>
 * </ul>
 *
 * <p>Address erasure: gc prints real addresses for pointers, slices,
 * maps, funcs and interface (type,data) pairs. Addresses are
 * nondeterministic under gc too, so this implementation prints
 * {@code 0x0} — the shape matches, the values are never differential.
 * Struct and array arguments are a gc <em>compile</em> error
 * ({@code badtype} in walkPrint); the factory rejects them at build
 * time, so they never reach this class.
 */
public final class BuiltinPrint {
    private BuiltinPrint() {}

    /** An argument: its Go kind and canonical boxed value. */
    public record Arg(GoKind kind, Object value) {}

    /** {@code print(args...)} → stderr, no separators, no trailing newline. */
    public static void print(Arg[] args) {
        write(formatAll(args, false));
    }

    /** {@code println(args...)} → stderr, spaces between args, trailing newline. */
    public static void println(Arg[] args) {
        write(formatAll(args, true));
    }

    /** Renders the full call to bytes (exposed for testing). */
    static byte[] formatAll(Arg[] args, boolean newline) {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        for (int i = 0; i < args.length; i++) {
            if (newline && i > 0) {
                out.write(' ');
            }
            writeOne(out, args[i]);
        }
        if (newline) {
            out.write('\n');
        }
        return out.toByteArray();
    }

    private static void write(ByteArrayOutputStream out) {
        write(out.toByteArray());
    }

    private static void write(byte[] bytes) {
        try {
            System.err.write(bytes);
            System.err.flush();
        } catch (java.io.IOException e) {
            throw new RuntimeException("print: stderr write failed", e);
        }
    }

    private static void writeOne(ByteArrayOutputStream out, Arg arg) {
        GoKind k = arg.kind();
        Object v = arg.value();
        // Untyped constants take their default type (mirrors gc's
        // typecheck.DefaultLit in walkPrint).
        if (k == GoKind.UNTYPED_INT) {
            k = GoKind.INT64;
        } else if (k == GoKind.UNTYPED_FLOAT) {
            k = GoKind.FLOAT64;
        } else if (k == GoKind.UNTYPED_BOOL) {
            k = GoKind.BOOL;
        } else if (k == GoKind.UNTYPED_STRING) {
            k = GoKind.STRING;
        } else if (k == GoKind.UNTYPED_COMPLEX) {
            k = GoKind.COMPLEX128;
        }
        switch (k) {
            case BOOL, UNTYPED_BOOL -> writeAscii(out, ((Boolean) v) ? "true" : "false");
            case INT8, INT16, INT32 -> writeAscii(out, Integer.toString((Integer) v));
            case INT64 -> writeAscii(out, Long.toString((Long) v));
            case UINT8, UINT16, UINT32 -> writeAscii(out, Integer.toUnsignedString((Integer) v));
            case UINT64 -> writeAscii(out, Long.toUnsignedString((Long) v));
            case FLOAT32 -> writeAscii(out, GoFloatFmt.formatFloat((Float) v, 'g', -1, 32));
            case FLOAT64 -> writeAscii(out, GoFloatFmt.formatFloat((Double) v, 'g', -1, 64));
            case COMPLEX64 -> {
                GoComplex32 c = (GoComplex32) v;
                writeAscii(out, GoFloatFmt.formatComplex(c.re(), c.im(), 'g', -1, 64));
            }
            case COMPLEX128 -> {
                GoComplex64 c = (GoComplex64) v;
                writeAscii(out, GoFloatFmt.formatComplex(c.re(), c.im(), 'g', -1, 128));
            }
            case STRING -> out.write(((GoString) v).bytes(), 0, ((GoString) v).length());
            case SLICE -> {
                GoSlice s = (GoSlice) v;
                if (s == null) {
                    writeAscii(out, "[0/0]0x0");
                } else {
                    writeAscii(out, "[" + s.len() + "/" + s.cap() + "]0x0");
                }
            }
            case POINTER, CHAN, MAP, FUNC, UNSAFE_POINTER -> writeAscii(out, "0x0");
            case INTERFACE -> writeAscii(out, "(0x0,0x0)");
            default -> throw new IllegalArgumentException(
                    "print of kind " + k + " is a compile error in gc (badtype)");
        }
    }

    private static void writeAscii(ByteArrayOutputStream out, String s) {
        out.write(s.getBytes(StandardCharsets.US_ASCII), 0, s.length());
    }
}
