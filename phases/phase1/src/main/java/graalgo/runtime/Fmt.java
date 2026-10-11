package graalgo.runtime;
// verifies: GG-0020 (WS G intrinsic surface)

import graalgo.values.GoComplex32;
import graalgo.values.GoComplex64;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import graalgo.values.GoTypeDescriptor;
import java.io.ByteArrayOutputStream;
import java.nio.charset.StandardCharsets;
import java.util.List;

/**
 * Minimal intrinsic-backed {@code fmt} stub (WS G, PLAN.md D4, REAL).
 *
 * <p>Covers exactly the D4 symbol set — {@code Print}, {@code Printf},
 * {@code Println}, {@code Sprint}, {@code Sprintf}, {@code Sprintln},
 * {@code Errorf} — resolved only through the frontend's Phase-1 import
 * overlay (any other {@code fmt.X} is a scope error naming Phase 3).
 * Every symbol is marked {@code replaceable-in-Phase-3} in
 * docs/freeze-stub.md; the stub must never be mistaken for the real
 * package downstream.
 *
 * <p>Formatting semantics are ported from {@code fmt/print.go} +
 * {@code fmt/format.go} (gc) and held to the verb-conformance matrix
 * ({@code corpus/fmt/matrix.tsv}, differential vs gc at test time):
 * verbs {@code v T t d b o O x X c U s q f F e E g G p} and {@code %}
 * with flags {@code - + # ' ' 0}, widths, precisions ({@code *}
 * included), and {@code %[n]} explicit argument indexes. A stub
 * divergence is a stub bug, never masked.
 *
 * <p>Deliberate, documented boundaries of the stub:
 * <ul>
 *   <li>addresses are erased: pointers, maps, funcs, chans and interface
 *       (type,data) pairs print as {@code 0x0} / {@code (0x0,0x0)} — gc
 *       prints real addresses, which are nondeterministic anyway;</li>
 *   <li>map key sorting covers the orderable key kinds (ints, uints,
 *       floats, strings, bools); other key kinds print in snapshot
 *       order (gc's order there is randomized, so never differential);</li>
 *   <li>{@code Errorf} returns an interface value whose dynamic type
 *       reports {@code *errors.errorString} (matching gc's
 *       {@code %T}); calling its {@code Error} method is Phase-3 scope
 *       (no methods exist in Phase 1 at all).</li>
 * </ul>
 */
public final class Fmt {
    private Fmt() {}

    /**
     * The current Printer (P3-M9, RC-6): set around each top-level
     * Printf/Sprintf/... call so fmt.State methods (invoked via
     * handleMethods during Formatter dispatch) can delegate to it.
     */
    public static final ThreadLocal<Printer> currentPrinter = new ThreadLocal<>();

    /**
     * Handler for Go's fmt.handleMethods (P3-M9, RC-6): dispatches to
     * Format/String/Error/GoString methods. Installed by the phase-3
     * launcher (which has the method tables); null in phase-1-only mode.
     */
    public interface FmtMethodHandler {
        boolean handle(Printer printer, Arg arg, int verb);
    }

    private static volatile FmtMethodHandler methodHandler;

    /** Installs the handleMethods handler (phase-3 launcher). */
    public static void setFmtMethodHandler(FmtMethodHandler handler) {
        methodHandler = handler;
    }

    /**
     * One formatted argument: the canonical boxed Go value, its kind, its
     * static Go-syntax type name (used for {@code %T} on non-interface
     * values and inside {@code %!verb(...)} markers), and — for
     * composite kinds — the key/element kinds and their Go-syntax type
     * names:
     * <ul>
     *   <li>slice/array: {@code elemKind}/{@code elemTypeName} = element;</li>
     *   <li>map: {@code keyKind}/{@code keyTypeName} = key,
     *       {@code elemKind}/{@code elemTypeName} = value;</li>
     *   <li>pointer: {@code elemKind}/{@code elemTypeName} = referent;</li>
     *   <li>other kinds: composite fields null.</li>
     * </ul>
     * For nested slices/arrays, {@code elemKindChain}/{@code elemTypeNameChain}
     * hold the element kinds/names below {@code elemKind} (innermost last),
     * so recursive printing works at arbitrary depth (R2 P0-2); empty or
     * null when the element is not itself a composite.
     * For nested maps, {@code keyKindChain}/{@code keyTypeNameChain} carry the
     * key kinds/names per nesting level, parallel to the elem chains
     * (M8 closeout: nested-map printing; e.g. map[string]map[string]int has
     * keyKindChain=[STRING], elemKindChain=[INT]).
     * For {@link GoKind#INTERFACE} the value is a {@link GoInterface}
     * (or {@code null} for a nil interface, like Go's {@code arg == nil}).
     */
    public record Arg(Object value, GoKind kind, String typeName,
                      GoKind keyKind, String keyTypeName,
                      GoKind elemKind, String elemTypeName,
                      List<GoKind> elemKindChain,
                      List<String> elemTypeNameChain,
                      List<GoKind> keyKindChain,
                      List<String> keyTypeNameChain) {
        /** Convenience for non-composite args. */
        public Arg(Object value, GoKind kind, String typeName) {
            this(value, kind, typeName, null, null, null, null, null, null, null, null);
        }
        /** Convenience for composite args without nested composites. */
        public Arg(Object value, GoKind kind, String typeName,
                   GoKind keyKind, String keyTypeName,
                   GoKind elemKind, String elemTypeName) {
            this(value, kind, typeName, keyKind, keyTypeName, elemKind, elemTypeName,
                    null, null, null, null);
        }
    }

    /** {@code Sprintf}: formats and returns the string. */
    public static GoString sprintf(String format, Arg[] args) {
        Printer p = new Printer();
        currentPrinter.set(p);
        try {
            return new GoString(p.doPrintf(format, args));
        } finally {
            currentPrinter.remove();
        }
    }

    /** {@code Sprint}: {@code %v} of each arg, spaces between non-strings. */
    public static GoString sprint(Arg[] args) {
        Printer p = new Printer();
        currentPrinter.set(p);
        try {
            return new GoString(p.doPrint(args));
        } finally {
            currentPrinter.remove();
        }
    }

    /** {@code Sprintln}: {@code %v} of each arg, spaces always, trailing newline. */
    public static GoString sprintln(Arg[] args) {
        Printer p = new Printer();
        currentPrinter.set(p);
        try {
            return new GoString(p.doPrintln(args));
        } finally {
            currentPrinter.remove();
        }
    }

    /**
     * {@code Print}: {@code %v} of each arg to stdout (os.Stdout).
     * Returns bytes written, like gc.
     */
    public static int print(Arg[] args) {
        Printer p = new Printer();
        currentPrinter.set(p);
        try {
            byte[] b = p.doPrint(args);
            System.out.write(b, 0, b.length);
            System.out.flush();
            return b.length;
        } finally {
            currentPrinter.remove();
        }
    }

    /** {@code Printf}: formats to stdout. Returns bytes written. */
    public static int printf(String format, Arg[] args) {
        Printer p = new Printer();
        currentPrinter.set(p);
        try {
            byte[] b = p.doPrintf(format, args);
            System.out.write(b, 0, b.length);
            System.out.flush();
            return b.length;
        } finally {
            currentPrinter.remove();
        }
    }

    /** {@code Println}: {@code %v} of each arg to stdout, spaces + newline. */
    public static int println(Arg[] args) {
        Printer p = new Printer();
        currentPrinter.set(p);
        try {
            byte[] b = p.doPrintln(args);
            System.out.write(b, 0, b.length);
            System.out.flush();
            return b.length;
        } finally {
            currentPrinter.remove();
        }
    }

    /**
     * {@code Errorf}: formats like {@code Sprintf} and boxes the message
     * in an interface value whose dynamic type name is
     * {@code *errors.errorString} (gc's concrete error type), so
     * {@code %v}/{@code %s}/{@code %q}/{@code %T} all match gc.
     *
     * <p>The representation is canonical and unique: a {@link GoPointer} to
     * a {@link GoStruct} with a single {@code s} field holding the message
     * {@link GoString} — byte-identical in shape to what the M7 synthetic
     * {@code errors} package produces for {@code errors.New}. There is
     * exactly one {@code *errors.errorString} representation in the
     * runtime; both the {@code Error()} method dispatch and the printer
     * rely on it.
     */
    public static GoInterface errorf(String format, Arg[] args) {
        return errorStringInterface(sprintf(format, args));
    }

    /**
     * Canonical {@code *errors.errorString} interface value wrapping the
     * given message. This is the single construction point for the unique
     * representation (see {@link #errorf}): a {@link GoPointer} to a
     * {@link GoStruct} with an {@code s} field. Every producer — the
     * {@code Errorf} intrinsic, tests, anything else — must go through
     * here so the representation cannot drift again.
     */
    static GoInterface errorStringInterface(GoString msg) {
        GoStructDescriptor desc = GoStructDescriptor.layout(
                "errors.errorString", List.of("s"), List.of(GoKind.STRING));
        GoStruct st = new GoStruct(desc, new long[0], new Object[] {msg});
        return new GoInterface(
                new GoTypeDescriptor("*errors.errorString", GoKind.POINTER, false),
                new GoPointer(st),
                null);
    }

    /** True when this interface value is the Errorf errorString stand-in. */
    static boolean isErrorString(GoInterface gi) {
        Object d = gi.descriptor();
        return d instanceof GoTypeDescriptor td && "*errors.errorString".equals(td.name());
    }

    /**
     * Message bytes of an {@code *errors.errorString} interface value.
     * The representation is canonical (see {@link #errorf}): a
     * {@link GoPointer} to a struct with an {@code s} field holding the
     * message {@link GoString}. Exactly one shape exists; anything else
     * is a bug.
     */
    private static byte[] errorStringBytes(GoInterface gi) {
        Object data = gi.data();
        if (data instanceof GoPointer gp && gp.referent() instanceof GoStruct st) {
            GoStructDescriptor desc = st.descriptor();
            for (int i = 0; i < desc.fields().size(); i++) {
                GoStructDescriptor.Field f = desc.field(i);
                if ("s".equals(f.name()) && !f.inLongs()
                        && st.getObject(f.index()) instanceof GoString gs) {
                    return gs.bytes();
                }
            }
        }
        throw new IllegalStateException(
                "errorString interface data has unexpected shape: " + data);
    }

    // ------------------------------------------------------------------
    // The printer: a port of fmt's pp + fmt structs.
    // ------------------------------------------------------------------

    /** The fmt printer (public for FmtHandleMethods in phase3). */
    public static final class Printer {
        final ByteArrayOutputStream buf = new ByteArrayOutputStream();

        /** Writes a String as UTF-8 bytes (ByteArrayOutputStream.write(byte[]) throws). */
        public void writeUtf8(String s) {
            byte[] b = s.getBytes(StandardCharsets.UTF_8);
            buf.write(b, 0, b.length);
        }

        /** Writes raw bytes (for invalid-UTF8 Go strings: Go prints bytes, not U+FFFD). */
        public void writeBytes(byte[] b) {
            buf.write(b, 0, b.length);
        }

        // flags (fmtFlags) — public for FmtHandleMethods.
        public boolean minus, plus, sharp, space, zero;
        public boolean sharpV, plusV; // repurposed #/+ for %v (Go syntax / field names)
        public int wid;
        public boolean widPresent;
        public int prec;
        public boolean precPresent;

        boolean reordered; // an explicit %[n] was used

        void clearFlags() {
            minus = plus = sharp = space = zero = false;
            sharpV = plusV = false;
            wid = 0;
            widPresent = false;
            prec = 0;
            precPresent = false;
        }

        // -- padding ----------------------------------------------------

        private void writePadding(int n) {
            if (n <= 0) {
                return;
            }
            char padByte = (zero && !minus) ? '0' : ' ';
            for (int i = 0; i < n; i++) {
                buf.write(padByte);
            }
        }

        /** pad: width counts runes (Go: utf8.RuneCount). */
        void pad(String s) {
            if (!widPresent || wid == 0) {
                writeUtf8(s);
                return;
            }
            int width = wid - s.codePointCount(0, s.length());
            if (!minus) {
                writePadding(width);
                writeUtf8(s);
            } else {
                writeUtf8(s);
                writePadding(width);
            }
        }

        // -- format loop (doPrintf) --------------------------------------

        byte[] doPrintf(String format, Arg[] args) {
            byte[] f = format.getBytes(StandardCharsets.UTF_8);
            int end = f.length;
            int argNum = 0;
            boolean afterIndex = false;
            reordered = false;
            formatLoop:
            for (int i = 0; i < end; ) {
                boolean goodArgNum = true;
                int lasti = i;
                while (i < end && f[i] != '%') {
                    i++;
                }
                if (i > lasti) {
                    // R1#1 (P0): i/lasti are BYTE indices into f; decode the
                    // byte range (format.substring takes CHAR indices).
                    writeBytes(java.util.Arrays.copyOfRange(f, lasti, i));
                }
                if (i >= end) {
                    break;
                }
                i++; // consume '%'

                clearFlags();
                // flags
                boolean simple = true;
                for (; i < end; i++) {
                    byte c = f[i];
                    switch (c) {
                        case '#' -> sharp = true;
                        case '0' -> zero = true;
                        case '+' -> plus = true;
                        case '-' -> minus = true;
                        case ' ' -> space = true;
                        default -> {
                            simple = false;
                        }
                    }
                    if (!simple) {
                        break;
                    }
                }
                if (!simple) {
                    // i points at the first non-flag byte; re-examine below.
                }

                // explicit argument index?
                int[] an = argNumber(argNum, f, i, args.length);
                argNum = an[0];
                i = an[1];
                afterIndex = an[2] == 1;
                if (an[3] == 0) {
                    goodArgNum = false;
                }

                // width
                if (i < end && f[i] == '*') {
                    i++;
                    int[] wr = intFromArg(args, argNum);
                    wid = wr[0];
                    widPresent = wr[1] == 1;
                    argNum = wr[2];
                    if (!widPresent) {
                        writeUtf8(("%!(BADWIDTH)"));
                    }
                    if (wid < 0) {
                        wid = -wid;
                        minus = true;
                        zero = false;
                    }
                    afterIndex = false;
                } else {
                    int[] pr = parseNum(f, i, end);
                    wid = pr[0];
                    widPresent = pr[1] == 1;
                    i = pr[2];
                    if (afterIndex && widPresent) {
                        goodArgNum = false;
                    }
                }

                // precision
                if (i + 1 < end && f[i] == '.') {
                    i++;
                    if (afterIndex) {
                        goodArgNum = false;
                    }
                    an = argNumber(argNum, f, i, args.length);
                    argNum = an[0];
                    i = an[1];
                    afterIndex = an[2] == 1;
                    if (an[3] == 0) {
                        goodArgNum = false;
                    }
                    if (i < end && f[i] == '*') {
                        i++;
                        int[] prr = intFromArg(args, argNum);
                        prec = prr[0];
                        precPresent = prr[1] == 1;
                        argNum = prr[2];
                        if (prec < 0) {
                            prec = 0;
                            precPresent = false;
                        }
                        if (!precPresent) {
                            writeUtf8(("%!(BADPREC)"));
                        }
                        afterIndex = false;
                    } else {
                        int[] pr = parseNum(f, i, end);
                        prec = pr[0];
                        precPresent = pr[1] == 1;
                        i = pr[2];
                        if (!precPresent) {
                            prec = 0;
                            precPresent = true;
                        }
                    }
                }

                if (!afterIndex) {
                    an = argNumber(argNum, f, i, args.length);
                    argNum = an[0];
                    i = an[1];
                    afterIndex = an[2] == 1;
                    if (an[3] == 0) {
                        goodArgNum = false;
                    }
                }

                if (i >= end) {
                    writeUtf8(("%!(NOVERB)"));
                    break;
                }

                // verb: one rune (verbs are ASCII in practice)
                int verb = f[i] & 0xFF;
                i++;
                if (verb >= 0x80) {
                    // Non-ASCII verb: consume the full rune like Go's
                    // DecodeRuneInString, then badVerb it.
                    int rlen = utf8Len(f, i - 1);
                    i = i - 1 + rlen;
                    verb = 0xFFFD;
                }

                if (verb == '%') {
                    buf.write('%');
                    continue;
                }
                if (!goodArgNum) {
                    badArgNum(verb);
                    continue;
                }
                if (argNum >= args.length) {
                    missingArg(verb);
                    continue;
                }
                if (verb == 'v') {
                    // Go syntax / struct-field syntax repurposing.
                    sharpV = sharp;
                    sharp = false;
                    plusV = plus;
                    plus = false;
                }
                printArg(args[argNum], verb);
                argNum++;
            }

            // extra arguments
            if (!reordered && argNum < args.length) {
                clearFlags();
                writeUtf8(("%!(EXTRA "));
                for (int k = argNum; k < args.length; k++) {
                    if (k > argNum) {
                        writeUtf8((", "));
                    }
                    Arg a = args[k];
                    if (a.value() == null) {
                        writeUtf8(("<nil>"));
                    } else {
                        writeUtf8(dynamicTypeName(a));
                        buf.write('=');
                        printArg(a, 'v');
                    }
                }
                buf.write(')');
            }
            return buf.toByteArray();
        }

        // -- number/index parsing -----------------------------------------

        /** parsenum: returns {num, isnum(1/0), newi}. */
        private int[] parseNum(byte[] f, int start, int end) {
            if (start >= end) {
                return new int[]{0, 0, end};
            }
            int num = 0, newi = start;
            boolean isnum = false;
            while (newi < end && '0' <= f[newi] && f[newi] <= '9') {
                if (tooLarge(num)) {
                    return new int[]{0, 0, end};
                }
                num = num * 10 + (f[newi] - '0');
                isnum = true;
                newi++;
            }
            return new int[]{num, isnum ? 1 : 0, newi};
        }

        private static boolean tooLarge(int x) {
            return x > 1_000_000 || x < -1_000_000;
        }

        /**
         * argNumber: returns {newArgNum, newi, afterIndex(1/0), ok(1/0)}.
         * Parses a "[n]" explicit argument index.
         */
        private int[] argNumber(int argNum, byte[] f, int i, int numArgs) {
            if (i >= f.length || f[i] != '[') {
                return new int[]{argNum, i, 0, 1};
            }
            reordered = true;
            // parseArgNumber: "[n]" — at least 3 bytes.
            if (f.length - i < 3) {
                return new int[]{argNum, i + 1, 0, 0};
            }
            int j = i + 1;
            while (j < f.length && f[j] != ']') {
                j++;
            }
            if (j >= f.length) {
                return new int[]{argNum, i + 1, 0, 0};
            }
            int[] pr = parseNum(f, i + 1, j);
            if (pr[1] == 0 || pr[2] != j) {
                return new int[]{argNum, j + 1, 0, 0};
            }
            int index = pr[0] - 1; // one-indexed
            if (0 <= index && index < numArgs) {
                return new int[]{index, j + 1, 1, 1};
            }
            return new int[]{argNum, j + 1, 0, 0};
        }

        /** intFromArg: returns {num, isInt(1/0), newArgNum}. */
        private int[] intFromArg(Arg[] a, int argNum) {
            int newArgNum = argNum;
            int num = 0;
            boolean isInt = false;
            if (argNum < a.length) {
                Arg arg = a[argNum];
                long v = 0;
                boolean ok = false, fits = false;
                switch (arg.kind()) {
                    case INT8, INT16, INT32 -> { v = (Integer) arg.value(); ok = true; }
                    case INT64 -> { v = (Long) arg.value(); ok = true; }
                    case UINT8, UINT16, UINT32 -> { v = Integer.toUnsignedLong((Integer) arg.value()); ok = true; }
                    case UINT64 -> {
                        long uv = (Long) arg.value();
                        if (uv >= 0) { v = uv; ok = true; }
                    }
                    case UNTYPED_INT -> { v = (Long) arg.value(); ok = true; }
                    default -> {}
                }
                if (ok) {
                    long iv = v;
                    if (iv == (int) iv && !tooLarge((int) iv)) {
                        num = (int) iv;
                        isInt = true;
                    }
                }
                newArgNum = argNum + 1;
            }
            return new int[]{num, isInt ? 1 : 0, newArgNum};
        }

        private static int utf8Len(byte[] f, int i) {
            int c0 = f[i] & 0xFF;
            if (c0 < 0x80) return 1;
            if (c0 < 0xE0) return 2;
            if (c0 < 0xF0) return 3;
            return 4;
        }

        // -- Sprint/Println bodies -----------------------------------------

        byte[] doPrint(Arg[] a) {
            boolean prevString = false;
            for (int n = 0; n < a.length; n++) {
                Arg arg = a[n];
                boolean isString = arg.value() != null
                        && (arg.kind() == GoKind.STRING || arg.kind() == GoKind.UNTYPED_STRING);
                if (n > 0 && !isString && !prevString) {
                    buf.write(' ');
                }
                clearFlags();
                printArg(arg, 'v');
                prevString = isString;
            }
            return buf.toByteArray();
        }

        byte[] doPrintln(Arg[] a) {
            for (int n = 0; n < a.length; n++) {
                if (n > 0) {
                    buf.write(' ');
                }
                clearFlags();
                printArg(a[n], 'v');
            }
            buf.write('\n');
            return buf.toByteArray();
        }

        // -- errors ----------------------------------------------------------

        private void badArgNum(int verb) {
            writeUtf8(("%!"));
            writeUtf8(new String(Character.toChars(verb)));
            writeUtf8(("(BADINDEX)"));
        }

        private void missingArg(int verb) {
            writeUtf8(("%!"));
            writeUtf8(new String(Character.toChars(verb)));
            writeUtf8(("(MISSING)"));
        }

        /** badVerb: "%!verb(type=value)" with the value in %v form. */
        private void badVerb(Arg arg, int verb) {
            writeUtf8(("%!"));
            writeUtf8(new String(Character.toChars(verb)));
            buf.write('(');
            if (arg.value() == null) {
                writeUtf8(("<nil>"));
            } else {
                writeUtf8(dynamicTypeName(arg));
                buf.write('=');
                // The inner value prints with %v under the *current* flags
                // (gc: "%05d" of "hi" -> "%!d(string=000hi)"). Method
                // dispatch is disabled inside (Go's p.erroring), so the
                // errorString stand-in prints in its &{...} struct form.
                boolean wasErroring = erroring;
                erroring = true;
                printArg(arg, 'v');
                erroring = wasErroring;
            }
            buf.write(')');
        }

        /**
         * The type name Go would report for this arg: the dynamic type for
         * interface values (descriptor name), the static name otherwise.
         */
        private String dynamicTypeName(Arg arg) {
            if (arg.kind() == GoKind.INTERFACE && arg.value() instanceof GoInterface gi) {
                Object d = gi.descriptor();
                if (d instanceof GoTypeDescriptor td) {
                    return td.name();
                }
            }
            return arg.typeName();
        }

        // -- printArg / printValue --------------------------------------------

        private Arg curArg; // for badVerb (mirrors Go passing arg through)

        /** Top-level arg print (handles %T and %p first, like Go's printArg). */
        void printArg(Arg arg, int verb) {
            curArg = arg;
            if (arg.value() == null) {
                // Go's printArg: a nil slice prints [], a nil map prints
                // map[] (not <nil>); typed nils print their type for %T.
                // (M8 closeout, triage class C.)
                if (verb == 'T') {
                    if (arg.kind() == GoKind.INTERFACE) {
                        pad("<nil>");
                    } else {
                        fmtS(arg.typeName());
                    }
                    return;
                }
                if (arg.kind() == GoKind.SLICE) {
                    pad("[]");
                    return;
                }
                if (arg.kind() == GoKind.MAP) {
                    pad("map[]");
                    return;
                }
                // nil interface/pointer/chan/func, like Go's arg == nil.
                if (verb == 'v') {
                    pad("<nil>");
                } else {
                    badVerb(arg, verb);
                }
                return;
            }
            if (verb == 'T') {
                fmtS(dynamicTypeName(arg));
                return;
            }
            if (verb == 'p') {
                fmtPointer(arg, verb);
                return;
            }
            printValue(arg, verb, 0);
        }

        /**
         * Value print (Go's printValue): no %T/%p interception. The
         * errorString stand-in takes the error/Stringer method path
         * (handleMethods equivalent) for the stringable verbs.
         */
        void printValue(Arg arg, int verb, int depth) {
            curArg = arg;
            Object v = arg.value();
            GoKind k = normalize(arg.kind());
            // P3-M9 (RC-6): Go's handleMethods — dispatch to Format/String/
            // Error/GoString methods before structural printing. The
            // errorString special-case below is subsumed when the dynamic
            // type has the methods, but kept for the synthetic stand-in.
            // The handler is installed by the phase-3 launcher (which has
            // the method tables); null in phase-1-only mode.
            if (depth == 0 && !erroring) {
                FmtMethodHandler handler = methodHandler;
                if (handler != null) {
                    try {
                        if (handler.handle(this, arg, verb)) {
                            return;
                        }
                    } catch (Exception e) {
                        // Method dispatch failed; fall through to structural.
                    }
                }
            }
            if (k == GoKind.INTERFACE && v instanceof GoInterface gi
                    && !gi.isNil() && isErrorString(gi)) {
                // The errorString value models *errors.errorString (a
                // pointer to struct{ s string }); see errorf for the
                // canonical representation. Verb dispatch mirrors Go:
                // method dispatch (Error()) only for v/s/x/X/q (and w under
                // wrapErrs); %p is the (erased) address; %#v is Go syntax;
                // every other verb — and every verb while erroring (method
                // dispatch disabled, Go's p.erroring) — takes the
                // pointer-to-struct path.
                byte[] msg = errorStringBytes(gi);
                if (sharpV) {
                    writeUtf8(("&errors.errorString{s:"));
                    writeUtf8((GoQuote.quote(msg)));
                    buf.write('}');
                    return;
                }
                if (!erroring) {
                    switch (verb) {
                        case 'v', 's', 'x', 'X', 'q' -> {
                            fmtString(msg, verb);
                            return;
                        }
                        case 'w' -> {
                            if (wrapErrs) {
                                fmtString(msg, 'v');
                            } else {
                                badVerb(arg, verb);
                            }
                            return;
                        }
                        case 'p' -> {
                            fmt0x64(0, !sharp);
                            return;
                        }
                        default -> {}
                    }
                }
                buf.write('&');
                buf.write('{');
                printValue(new Arg(new GoString(msg), GoKind.STRING, "string"),
                        verb, depth + 1);
                buf.write('}');
                return;
            }
            switch (k) {
                case BOOL -> fmtBool((Boolean) v, verb);
                // Small int kinds are boxed as Integer everywhere (Boxing.box,
                // IncDecNode); mask to width for unsigned printing.
                case INT8 -> fmtInteger(((Integer) v).byteValue(), true, verb);
                case INT16 -> fmtInteger(((Integer) v).shortValue(), true, verb);
                case INT32 -> fmtInteger(((Integer) v).longValue(), true, verb);
                case INT64 -> fmtInteger((Long) v, true, verb);
                case UINT8 -> fmtInteger(Integer.toUnsignedLong(((Integer) v).intValue() & 0xFF), false, verb);
                case UINT16 -> fmtInteger(Integer.toUnsignedLong(((Integer) v).intValue() & 0xFFFF), false, verb);
                case UINT32 -> fmtInteger(Integer.toUnsignedLong((Integer) v), false, verb);
                case UINT64 -> fmtInteger((Long) v, false, verb);
                case FLOAT32 -> fmtFloat((Float) v, 32, verb);
                case FLOAT64 -> fmtFloat((Double) v, 64, verb);
                case COMPLEX64 -> {
                    GoComplex32 c = (GoComplex32) v;
                    fmtComplex(c.re(), c.im(), 64, verb);
                }
                case COMPLEX128 -> {
                    GoComplex64 c = (GoComplex64) v;
                    fmtComplex(c.re(), c.im(), 128, verb);
                }
                case STRING -> fmtString(((GoString) v).bytes(), verb);
                case SLICE, ARRAY -> printSlice(arg, v, verb, depth);
                case MAP -> printMap(arg, (GoMap) v, verb, depth);
                case STRUCT -> printStruct(arg, (GoStruct) v, verb, depth);
                case POINTER -> fmtPointer(arg, verb);
                case INTERFACE -> {
                    // P3-M7: a nil interface arg may flow as Java null — narrow to NIL.
                    GoInterface gi = GoInterface.narrow(v);
                    if (gi.isNil() || gi.data() == null) {
                        if (verb == 'v') {
                            writeUtf8(("<nil>"));
                        } else {
                            badVerb(arg, verb);
                        }
                        return;
                    }
                    GoTypeDescriptor td = (GoTypeDescriptor) gi.descriptor();
                    printValue(new Arg(gi.data(), td.kind(), td.name(), null, null, null, null), verb, depth + 1);
                }
                case CHAN, FUNC, UNSAFE_POINTER -> fmtPointer(arg, verb);
                default -> badVerb(arg, verb);
            }
        }

        private boolean erroring;
        private boolean wrapErrs;

        private static GoKind normalize(GoKind k) {
            return switch (k) {
                case UNTYPED_INT -> GoKind.INT64;
                case UNTYPED_FLOAT -> GoKind.FLOAT64;
                case UNTYPED_COMPLEX -> GoKind.COMPLEX128;
                case UNTYPED_STRING -> GoKind.STRING;
                case UNTYPED_BOOL -> GoKind.BOOL;
                default -> k;
            };
        }

        // -- verbs ------------------------------------------------------------

        private void fmtBool(boolean v, int verb) {
            switch (verb) {
                case 't', 'v' -> pad(v ? "true" : "false");
                default -> badVerb(curArg, verb);
            }
        }

        private static final char[] LDIGITS = "0123456789abcdefx".toCharArray();
        private static final char[] UDIGITS = "0123456789ABCDEFX".toCharArray();

        private void fmtInteger(long u, boolean isSigned, int verb) {
            switch (verb) {
                case 'v' -> {
                    if (sharpV && !isSigned) {
                        fmt0x64(u, true);
                    } else {
                        fmtIntegerRaw(u, 10, isSigned, verb, LDIGITS);
                    }
                }
                case 'd' -> fmtIntegerRaw(u, 10, isSigned, verb, LDIGITS);
                case 'b' -> fmtIntegerRaw(u, 2, isSigned, verb, LDIGITS);
                case 'o', 'O' -> fmtIntegerRaw(u, 8, isSigned, verb, LDIGITS);
                case 'x' -> fmtIntegerRaw(u, 16, isSigned, verb, LDIGITS);
                case 'X' -> fmtIntegerRaw(u, 16, isSigned, verb, UDIGITS);
                case 'c' -> fmtC(u);
                case 'q' -> fmtQc(u);
                case 'U' -> fmtUnicode(u);
                default -> badVerb(curArg, verb);
            }
        }

        /** fmt0x64: hex with optional 0x, via the sharp flag dance. */
        private void fmt0x64(long v, boolean leading0x) {
            boolean s = sharp;
            sharp = leading0x;
            fmtIntegerRaw(v, 16, false, 'v', LDIGITS);
            sharp = s;
        }

        /** Port of fmt.fmtInteger. */
        private void fmtIntegerRaw(long u, int base, boolean isSigned, int verb, char[] digits) {
            boolean negative = isSigned && u < 0;
            if (negative) {
                u = -u; // wraps like Go for Long.MIN_VALUE
            }
            int prec = 0;
            if (precPresent) {
                prec = this.prec;
                if (prec == 0 && u == 0) {
                    // Precision 0 and value 0: "print nothing" but padding.
                    boolean oz = zero;
                    zero = false;
                    writePadding(wid);
                    zero = oz;
                    return;
                }
            } else if (zero && !minus && widPresent) {
                prec = wid;
                if (negative || plus || space) {
                    prec--; // leave room for sign
                }
            }
            // digits, right to left
            StringBuilder nb = new StringBuilder();
            long uu = u;
            switch (base) {
                case 10 -> { while (Long.compareUnsigned(uu, 10) >= 0) { nb.append((char) ('0' + Long.remainderUnsigned(uu, 10))); uu = Long.divideUnsigned(uu, 10); } }
                case 16 -> { while (Long.compareUnsigned(uu, 16) >= 0) { nb.append(digits[(int) (uu & 15)]); uu >>>= 4; } }
                case 8 -> { while (Long.compareUnsigned(uu, 8) >= 0) { nb.append((char) ('0' + (uu & 7))); uu >>>= 3; } }
                case 2 -> { while (Long.compareUnsigned(uu, 2) >= 0) { nb.append((char) ('0' + (uu & 1))); uu >>>= 1; } }
                default -> throw new IllegalArgumentException("bad base " + base);
            }
            nb.append(digits[(int) uu]);
            while (nb.length() < prec) {
                nb.append('0');
            }
            nb.reverse();
            StringBuilder sb = new StringBuilder();
            if (sharp) {
                switch (base) {
                    case 2 -> sb.append("0b");
                    case 8 -> { if (nb.charAt(0) != '0') sb.append('0'); }
                    case 16 -> sb.append('0').append(digits[16]);
                }
            }
            if (verb == 'O') {
                sb.append("0o");
            }
            if (negative) {
                sb.append('-');
            } else if (plus) {
                sb.append('+');
            } else if (space) {
                sb.append(' ');
            }
            sb.append(nb);
            boolean oz = zero;
            zero = false;
            pad(sb.toString());
            zero = oz;
        }

        /** Port of fmt.fmtFloat. */
        private void fmtFloat(double v, int size, int verb) {
            int prec;
            switch (verb) {
                case 'v' -> { prec = -1; verb = 'g'; }
                case 'b', 'g', 'G', 'x', 'X' -> prec = -1;
                case 'f', 'e', 'E', 'F' -> prec = 6;
                default -> { badVerb(curArg, verb); return; }
            }
            if (verb == 'F') {
                verb = 'f';
            }
            if (precPresent) {
                prec = this.prec;
            }
            fmtFloatRaw(v, size, (char) verb, prec);
        }

        private void fmtFloatRaw(double v, int size, char verb, int prec) {
            String digits;
            if (verb == 'b') {
                digits = GoFloatFmt.formatBinary(v, size);
            } else if (verb == 'x' || verb == 'X') {
                digits = GoFloatFmt.formatHex(v, verb, prec, size);
            } else {
                digits = GoFloatFmt.formatFloat(v, verb, prec, size);
            }
            // Reserve num[0] for the sign, like Go's intbuf[:1].
            String num = (digits.startsWith("-") || digits.startsWith("+")) ? digits : "+" + digits;
            if (space && num.charAt(0) == '+' && !plus) {
                num = " " + num.substring(1);
            }
            // Inf/NaN: no zero padding; drop the sign on NaN unless asked.
            if (num.charAt(1) == 'I' || num.charAt(1) == 'N') {
                boolean oz = zero;
                zero = false;
                if (num.charAt(1) == 'N' && !space && !plus) {
                    num = num.substring(1);
                }
                pad(num);
                zero = oz;
                return;
            }
            // The sharp flag forces a decimal point / retains trailing zeros.
            if (sharp && verb != 'b') {
                num = sharpFloat(num, verb, prec);
            }
            if (plus || num.charAt(0) != '+') {
                if (zero && !minus && widPresent && wid > num.length()) {
                    buf.write(num.charAt(0));
                    writePadding(wid - num.length());
                    writeUtf8(num.substring(1, num.length()));
                    return;
                }
                pad(num);
                return;
            }
            pad(num.substring(1));
        }

        /** Port of the sharp-flag tail of fmt.fmtFloat. */
        private String sharpFloat(String num, char verb, int prec) {
            int digits = 0;
            switch (verb) {
                case 'g', 'G', 'x' -> {
                    digits = prec;
                    if (digits == -1) {
                        digits = 6;
                    }
                }
                default -> {}
            }
            StringBuilder n = new StringBuilder(num);
            StringBuilder tail = new StringBuilder();
            boolean hasDecimalPoint = false;
            boolean sawNonzero = false;
            int i = 1; // skip sign
            for (; i < n.length(); i++) {
                char c = n.charAt(i);
                if (c == '.') {
                    hasDecimalPoint = true;
                } else if (c == 'p' || c == 'P') {
                    tail.append(n.substring(i));
                    n.setLength(i);
                    break;
                } else if (c == 'e' || c == 'E') {
                    if (verb != 'x' && verb != 'X') {
                        tail.append(n.substring(i));
                        n.setLength(i);
                        break;
                    }
                    if (c != '0') {
                        sawNonzero = true;
                    }
                    if (sawNonzero) {
                        digits--;
                    }
                } else {
                    if (c != '0') {
                        sawNonzero = true;
                    }
                    if (sawNonzero) {
                        digits--;
                    }
                }
            }
            if (!hasDecimalPoint) {
                if (n.length() == 2 && n.charAt(1) == '0') {
                    digits--;
                }
                n.append('.');
            }
            for (; digits > 0; digits--) {
                n.append('0');
            }
            n.append(tail);
            return n.toString();
        }

        /** Port of fmt's fmtComplex. */
        private void fmtComplex(double re, double im, int size, int verb) {
            switch (verb) {
                case 'v', 'b', 'g', 'G', 'x', 'X', 'f', 'F', 'e', 'E' -> {}
                default -> { badVerb(curArg, verb); return; }
            }
            boolean oldPlus = plus;
            buf.write('(');
            fmtFloat(re, size / 2, verb);
            // Imaginary part always has a sign.
            plus = true;
            fmtFloat(im, size / 2, verb);
            writeUtf8(("i)"));
            plus = oldPlus;
        }

        private void fmtString(byte[] sBytes, int verb) {
            if (isValidUtf8(sBytes)) {
                String s = new String(sBytes, StandardCharsets.UTF_8);
                switch (verb) {
                    case 'v' -> { if (sharpV) fmtQ(s); else fmtS(s); }
                    case 's' -> fmtS(s);
                    case 'x' -> fmtSx(s, LDIGITS);
                    case 'X' -> fmtSx(s, UDIGITS);
                    case 'q' -> fmtQ(s);
                    default -> badVerb(curArg, verb);
                }
                return;
            }
            // M8 (fuzzer find): invalid UTF-8. Go strings are bytes; %s/%v
            // print raw bytes, never U+FFFD. The old code decoded via
            // new String(bytes, UTF_8), which sanitizes malformed input.
            switch (verb) {
                case 'v' -> { if (sharpV) fmtQBytes(sBytes); else fmtSBytes(sBytes); }
                case 's' -> fmtSBytes(sBytes);
                case 'x' -> fmtBx(sBytes, LDIGITS);
                case 'X' -> fmtBx(sBytes, UDIGITS);
                case 'q' -> fmtQBytes(sBytes);
                default -> badVerb(curArg, verb);
            }
        }

        /** UTF-8 validity: a byte is invalid iff DecodeRune gives (RuneError, 1). */
        private static boolean isValidUtf8(byte[] b) {
            for (int i = 0; i < b.length; ) {
                GoQuote.Rune dr = GoQuote.decodeRune(b, i);
                if (dr.width() == 1 && dr.r() == 0xFFFD) {
                    return false;
                }
                i += dr.width();
            }
            return true;
        }

        /** Rune count with each invalid byte counting as 1 (Go utf8.RuneCount). */
        private static int runeCountBytes(byte[] b) {
            int n = 0;
            for (int i = 0; i < b.length; ) {
                n++;
                i += GoQuote.decodeRune(b, i).width();
            }
            return n;
        }

        /** Truncate to prec runes (Go fmt precision on strings). */
        private static byte[] truncateBytes(byte[] b, int prec) {
            int i = 0, n = 0;
            while (i < b.length && n < prec) {
                i += GoQuote.decodeRune(b, i).width();
                n++;
            }
            return java.util.Arrays.copyOf(b, i);
        }

        /** pad for raw bytes: width counts runes (invalid bytes count as 1). */
        private void padBytes(byte[] b) {
            if (precPresent) {
                b = truncateBytes(b, prec);
            }
            if (!widPresent || wid == 0) {
                writeBytes(b);
                return;
            }
            int width = wid - runeCountBytes(b);
            if (!minus) {
                writePadding(width);
                writeBytes(b);
            } else {
                writeBytes(b);
                writePadding(width);
            }
        }

        /** %s/%v of invalid-UTF-8 bytes: raw bytes, rune-wise precision. */
        private void fmtSBytes(byte[] b) {
            padBytes(b);
        }

        /** %q of invalid-UTF-8 bytes: GoQuote handles raw bytes (\\xNN escapes). */
        private void fmtQBytes(byte[] b) {
            byte[] t = b;
            if (precPresent) {
                t = truncateBytes(b, prec);
            }
            // GoQuote.quote emits pure ASCII; safe to pad as a String.
            // (canBackquote is always false for invalid UTF-8.)
            pad(plus ? GoQuote.quoteToASCII(t) : GoQuote.quote(t));
        }

        /** truncateString: rune-wise truncation to prec. */
        private String truncateString(String s) {
            if (!precPresent) {
                return s;
            }
            int n = prec;
            int i = 0;
            while (i < s.length()) {
                if (n <= 0) {
                    return s.substring(0, i);
                }
                n--;
                i += Character.charCount(s.codePointAt(i));
            }
            return s;
        }

        private void fmtS(String s) {
            pad(truncateString(s));
        }

        /** Port of fmt.fmtSbx (hex of string/byte-slice). */
        private void fmtSbx(String s, byte[] b, char[] digits) {
            byte[] src = b != null ? b : s.getBytes(StandardCharsets.UTF_8);
            int length = src.length;
            if (precPresent && prec < length) {
                length = prec;
            }
            int width = 2 * length;
            if (width > 0) {
                if (space) {
                    if (sharp) {
                        width *= 2;
                    }
                    width += length - 1;
                } else if (sharp) {
                    width += 2;
                }
            } else {
                if (widPresent) {
                    writePadding(wid);
                }
                return;
            }
            StringBuilder out = new StringBuilder();
            if (widPresent && wid > width && !minus) {
                int n = wid - width;
                char pb = (zero && !minus) ? '0' : ' ';
                for (int k = 0; k < n; k++) out.append(pb);
            }
            if (sharp) {
                out.append('0').append(digits[16]);
            }
            for (int k = 0; k < length; k++) {
                if (space && k > 0) {
                    out.append(' ');
                    if (sharp) {
                        out.append('0').append(digits[16]);
                    }
                }
                int c = src[k] & 0xFF;
                out.append(digits[c >> 4]).append(digits[c & 0xF]);
            }
            writeUtf8(out.toString());
            if (widPresent && wid > width && minus) {
                writePadding(wid - width);
            }
        }

        private void fmtSx(String s, char[] digits) {
            fmtSbx(s, null, digits);
        }

        private void fmtBx(byte[] b, char[] digits) {
            fmtSbx("", b, digits);
        }

        private void fmtBs(byte[] b) {
            // M8: byte-faithful; invalid UTF-8 prints raw (never U+FFFD).
            padBytes(b);
        }

        /** Port of fmt.fmtQ. */
        private void fmtQ(String s) {
            s = truncateString(s);
            if (sharp && GoQuote.canBackquote(s.getBytes(StandardCharsets.UTF_8))) {
                pad("`" + s + "`");
                return;
            }
            if (plus) {
                pad(GoQuote.quoteToASCII(s.getBytes(StandardCharsets.UTF_8)));
            } else {
                pad(GoQuote.quote(s.getBytes(StandardCharsets.UTF_8)));
            }
        }

        /** Port of fmt.fmtC. */
        private void fmtC(long c) {
            int r = (int) c;
            if (Long.compareUnsigned(c, 0x10FFFFL) > 0) {
                r = 0xFFFD;
            }
            pad(new String(Character.toChars(r)));
        }

        /** Port of fmt.fmtQc. */
        private void fmtQc(long c) {
            int r = (int) c;
            if (Long.compareUnsigned(c, 0x10FFFFL) > 0) {
                r = 0xFFFD;
            }
            if (plus) {
                pad(GoQuote.quoteRuneToASCII(r));
            } else {
                pad(GoQuote.quoteRune(r));
            }
        }

        /** Port of fmt.fmtUnicode. */
        private void fmtUnicode(long u) {
            int prec = 4;
            if (precPresent && this.prec > 4) {
                prec = this.prec;
            }
            StringBuilder sb = new StringBuilder();
            boolean addChar = sharp && Long.compareUnsigned(u, 0x10FFFFL) <= 0
                    && GoIsPrint.isPrint((int) u);
            if (addChar) {
                sb.append('\'').appendCodePoint((int) u).append('\'').append(' ');
            }
            String hex = Long.toHexString(u).toUpperCase();
            while (hex.length() < prec) {
                hex = "0" + hex;
            }
            // Build right-to-left like Go: "U+" + hex [+ " 'c'"].
            StringBuilder out = new StringBuilder("U+").append(hex);
            if (addChar) {
                out.append(' ').append('\'').appendCodePoint((int) u).append('\'');
            }
            boolean oz = zero;
            zero = false;
            pad(out.toString());
            zero = oz;
        }

        // -- bytes / pointer / composites --------------------------------------

        /** Port of fmt's fmtBytes (the []byte fast path). */
        private void fmtBytes(byte[] v, int verb, String typeString) {
            switch (verb) {
                case 'v', 'd' -> {
                    if (sharpV) {
                        writeUtf8((typeString));
                        if (v == null) {
                            writeUtf8(("(nil)"));
                            return;
                        }
                        buf.write('{');
                        for (int i = 0; i < v.length; i++) {
                            if (i > 0) {
                                writeUtf8((", "));
                            }
                            fmt0x64(v[i] & 0xFFL, true);
                        }
                        buf.write('}');
                    } else {
                        buf.write('[');
                        for (int i = 0; i < v.length; i++) {
                            if (i > 0) {
                                buf.write(' ');
                            }
                            fmtIntegerRaw(v[i] & 0xFFL, 10, false, verb, LDIGITS);
                        }
                        buf.write(']');
                    }
                }
                case 's' -> fmtBs(v);
                case 'x' -> fmtBx(v, LDIGITS);
                case 'X' -> fmtBx(v, UDIGITS);
                case 'q' -> fmtQBytes(v);
                default -> {
                    // Like Go's printValue fallthrough: per-element verb.
                    buf.write('[');
                    for (int i = 0; i < v.length; i++) {
                        if (i > 0) {
                            buf.write(' ');
                        }
                        curArg = new Arg(null, GoKind.UINT8, "uint8", null, null, null, null);
                        fmtInteger(v[i] & 0xFFL, false, verb);
                    }
                    buf.write(']');
                }
            }
        }

        /** Extracts the bytes of a []byte/[]uint8 slice or array value. */
        private byte[] sliceBytes(Object v, GoKind kind) {
            if (kind == GoKind.SLICE) {
                GoSlice s = (GoSlice) v;
                if (s == null) {
                    return new byte[0];
                }
                byte[] out = new byte[s.len()];
                Object arr = s.array();
                for (int i = 0; i < s.len(); i++) {
                    out[i] = (byte) ((int[]) arr)[s.offset() + i];
                }
                return out;
            }
            int n = java.lang.reflect.Array.getLength(v);
            byte[] out = new byte[n];
            for (int i = 0; i < n; i++) {
                out[i] = (byte) ((int[]) v)[i];
            }
            return out;
        }

        /**
         * Port of fmt's fmtPointer. Addresses are erased (always zero in
         * Phase 1 — gc's are nondeterministic anyway); the shape of the
         * output matches gc.
         */
        private void fmtPointer(Arg arg, int verb) {
            switch (verb) {
                case 'v' -> {
                    if (sharpV) {
                        buf.write('(');
                        writeUtf8(dynamicTypeName(arg));
                        writeUtf8(")(");
                        writeUtf8(("nil")); // u == 0 always in Phase 1
                        buf.write(')');
                        return;
                    }
                    GoPointer gp = arg.value() instanceof GoPointer p ? p : null;
                    if (gp == null || gp.isNil()) {
                        pad("<nil>");
                        return;
                    }
                    // Pointer to array/slice/struct/map at depth 0 prints
                    // as &value (Go's printValue Pointer case); other
                    // pointers print their (erased) address.
                    if (arg.elemKind() != null) {
                        switch (arg.elemKind()) {
                            case ARRAY, SLICE, STRUCT, MAP -> {
                                buf.write('&');
                                Arg ref = new Arg(gp.referent(), arg.elemKind(),
                                        arg.elemTypeName(), null, null, null, null);
                                printValue(ref, verb, 1);
                                return;
                            }
                            default -> {}
                        }
                    }
                    fmt0x64(0, !sharp);
                }
                case 'p' -> {
                    // The errorString value is conceptually *errors.errorString.
                    if (arg.kind() == GoKind.INTERFACE && arg.value() instanceof GoInterface gi
                            && !gi.isNil() && isErrorString(gi)) {
                        fmt0x64(0, !sharp);
                        return;
                    }
                    switch (arg.kind()) {
                        case CHAN, FUNC, MAP, POINTER, SLICE, UNSAFE_POINTER ->
                            fmt0x64(0, !sharp);
                        default -> badVerb(arg, verb);
                    }
                }
                case 'b', 'o', 'd', 'x', 'X' -> fmtInteger(0, false, verb);
                default -> badVerb(arg, verb);
            }
        }

        /** Number of elements of a slice/array value. */
        private int compositeLen(Object v, GoKind kind) {
            if (kind == GoKind.SLICE) {
                GoSlice s = (GoSlice) v;
                return s == null ? 0 : s.len();
            }
            return java.lang.reflect.Array.getLength(v);
        }

        /** Boxed element i of a slice/array value (canonical form). */
        private Object compositeGet(Object v, GoKind kind, GoKind elemKind, int i) {
            if (kind == GoKind.SLICE) {
                GoSlice s = (GoSlice) v;
                return boxArrayElement(s.array(), elemKind, s.offset() + i);
            }
            return boxArrayElement(v, elemKind, i);
        }

        private static Object boxArrayElement(Object array, GoKind elemKind, int i) {
            // M7: canonical boxing is Integer for all ≤32-bit int kinds
            // (Boxing.box); Byte/Short would break the printValue switch.
            return switch (elemKind) {
                case INT8, UINT8, INT16, UINT16, INT32, UINT32 -> Integer.valueOf(((int[]) array)[i]);
                case INT64, UINT64 -> Long.valueOf(((long[]) array)[i]);
                case FLOAT32 -> Float.valueOf(((float[]) array)[i]);
                case FLOAT64 -> Double.valueOf(((double[]) array)[i]);
                case BOOL -> Boolean.valueOf(((boolean[]) array)[i]);
                case COMPLEX64 -> ((GoComplex32[]) array)[i];
                case COMPLEX128 -> ((GoComplex64[]) array)[i];
                default -> ((Object[]) array)[i];
            };
        }

        /** Port of fmt's printValue Array/Slice case. */
        private void printSlice(Arg arg, Object v, int verb, int depth) {
            GoKind elemKind = arg.elemKind() != null ? arg.elemKind() : GoKind.UINT8;
            boolean isBytes = elemKind == GoKind.UINT8;
            if ((verb == 's' || verb == 'q' || verb == 'x' || verb == 'X') && isBytes) {
                fmtBytes(sliceBytes(v, arg.kind()), verb, arg.typeName());
                return;
            }
            if (sharpV) {
                writeUtf8((arg.typeName()));
                if (arg.kind() == GoKind.SLICE && v == null) {
                    writeUtf8(("(nil)"));
                    return;
                }
                buf.write('{');
                int n = compositeLen(v, arg.kind());
                for (int i = 0; i < n; i++) {
                    if (i > 0) {
                        writeUtf8((", "));
                    }
                    // Remediation (R2 P0-2): depth-general nested kinds. The
                    // old single elemElemKind field structurally capped
                    // nesting at depth 2; pop one level off the kind/name
                    // chains per recursion instead.
                    printValue(nestedArg(arg, compositeGet(v, arg.kind(), elemKind, i), elemKind),
                            verb, depth + 1);
                }
                buf.write('}');
                return;
            }
            buf.write('[');
            int n = compositeLen(v, arg.kind());
            for (int i = 0; i < n; i++) {
                if (i > 0) {
                    buf.write(' ');
                }
                // Remediation (R2 P0-2): depth-general nested kinds (see above).
                printValue(nestedArg(arg, compositeGet(v, arg.kind(), elemKind, i), elemKind),
                        verb, depth + 1);
            }
            buf.write(']');
        }

        /**
         * Builds the {@link Arg} for one element of a composite value,
         * popping one level off the element kind/name chains (remediation
         * for R2 P0-2: nested array/slice printing at arbitrary depth).
         */
        private Arg nestedArg(Arg arg, Object elemValue, GoKind elemKind) {
            List<GoKind> chain = arg.elemKindChain() != null ? arg.elemKindChain() : List.of();
            List<String> nameChain =
                    arg.elemTypeNameChain() != null ? arg.elemTypeNameChain() : List.of();
            return new Arg(elemValue, elemKind, arg.elemTypeName(), null, null,
                    chain.isEmpty() ? null : chain.get(0),
                    nameChain.isEmpty() ? null : nameChain.get(0),
                    chain.size() <= 1 ? List.of() : chain.subList(1, chain.size()),
                    nameChain.size() <= 1 ? List.of() : nameChain.subList(1, nameChain.size()),
                    null, null);
        }

        /** Port of fmt's printValue Map case (keys sorted for orderable kinds). */
        private void printMap(Arg arg, GoMap m, int verb, int depth) {
            GoKind keyKind = arg.keyKind() != null ? arg.keyKind() : GoKind.STRING;
            GoKind elemKind = arg.elemKind() != null ? arg.elemKind() : GoKind.INTERFACE;
            String kt = arg.keyTypeName() != null ? arg.keyTypeName() : "?";
            String et = arg.elemTypeName() != null ? arg.elemTypeName() : "?";
            Object[] keys = m == null ? new Object[0] : m.keysSnapshot();
            // Sort for the orderable key kinds (Go's fmtsort).
            if (isOrderableKey(keyKind) && keys.length > 1) {
                java.util.Arrays.sort(keys, (a, b) -> compareKeys(a, b, keyKind));
            }
            if (sharpV) {
                writeUtf8((arg.typeName()));
                if (m == null) {
                    writeUtf8(("(nil)"));
                    return;
                }
                buf.write('{');
                for (int i = 0; i < keys.length; i++) {
                    if (i > 0) {
                        writeUtf8((", "));
                    }
                    printValue(new Arg(keys[i], keyKind, kt), verb, depth + 1);
                    buf.write(':');
                    printValue(nestedMapArg(arg, m.get(keys[i])), verb, depth + 1);
                }
                buf.write('}');
            } else {
                writeUtf8(("map["));
                for (int i = 0; i < keys.length; i++) {
                    if (i > 0) {
                        buf.write(' ');
                    }
                    printValue(new Arg(keys[i], keyKind, kt), verb, depth + 1);
                    buf.write(':');
                    printValue(nestedMapArg(arg, m.get(keys[i])), verb, depth + 1);
                }
                buf.write(']');
            }
        }

        /**
         * Builds the {@link Arg} for one value of a map, popping one level
         * off the key/elem kind chains (M8 closeout: nested maps at arbitrary
         * depth; e.g. map[string]map[string]int threads keyKindChain=[STRING],
         * elemKindChain=[INT] so the inner map prints with the right kinds).
         */
        private Arg nestedMapArg(Arg arg, Object elemValue) {
            List<GoKind> kc = arg.keyKindChain() != null ? arg.keyKindChain() : List.of();
            List<String> knc = arg.keyTypeNameChain() != null ? arg.keyTypeNameChain() : List.of();
            List<GoKind> ec = arg.elemKindChain() != null ? arg.elemKindChain() : List.of();
            List<String> enc = arg.elemTypeNameChain() != null ? arg.elemTypeNameChain() : List.of();
            return new Arg(elemValue, arg.elemKind(), arg.elemTypeName(),
                    kc.isEmpty() ? null : kc.get(0),
                    knc.isEmpty() ? null : knc.get(0),
                    ec.isEmpty() ? null : ec.get(0),
                    enc.isEmpty() ? null : enc.get(0),
                    ec.size() <= 1 ? List.of() : ec.subList(1, ec.size()),
                    enc.size() <= 1 ? List.of() : enc.subList(1, enc.size()),
                    kc.size() <= 1 ? List.of() : kc.subList(1, kc.size()),
                    knc.size() <= 1 ? List.of() : knc.subList(1, knc.size()));
        }

        /** Port of fmt's printValue Struct case. */
        private void printStruct(Arg arg, GoStruct s, int verb, int depth) {
            GoStructDescriptor d = s.descriptor();
            if (sharpV) {
                writeUtf8((arg.typeName()));
            }
            buf.write('{');
            var fields = d.fields();
            for (int i = 0; i < fields.size(); i++) {
                if (i > 0) {
                    writeUtf8((sharpV ? ", " : " "));
                }
                var f = fields.get(i);
                if (plusV || sharpV) {
                    writeUtf8(f.name());
                    buf.write(':');
                }
                printValue(new Arg(structFieldBoxed(s, i), f.kind(), kindName(f.kind())),
                        verb, depth + 1);
            }
            buf.write('}');
        }

        /**
         * Canonical boxed value of one struct field.
         *
         * @param fieldIndex index into the descriptor's field list (NOT the
         *                   carrier index — {@link GoStructDescriptor.Field#index}
         *                   is the position within longs[]/objects[]).
         */
        private static Object structFieldBoxed(GoStruct s, int fieldIndex) {
            GoStructDescriptor.Field f = s.descriptor().field(fieldIndex);
            if (f.inLongs()) {
                long v = s.getLong(fieldIndex);
                return switch (f.kind()) {
                    case FLOAT32 -> Float.valueOf(Float.intBitsToFloat((int) v));
                    case FLOAT64 -> Double.valueOf(Double.longBitsToDouble(v));
                    case BOOL -> Boolean.valueOf(v != 0);
                    case INT8, INT16, INT32, UINT8, UINT16, UINT32 -> Integer.valueOf((int) v);
                    default -> Long.valueOf(v);
                };
            }
            return s.getObject(fieldIndex);
        }

        /** Go-syntax name for a kind (nested %T inside composites). */
        private static String kindName(GoKind k) {
            return switch (k) {
                case INT8 -> "int8";
                case INT16 -> "int16";
                case INT32 -> "int32";
                case INT64 -> "int";
                case UINT8 -> "uint8";
                case UINT16 -> "uint16";
                case UINT32 -> "uint32";
                case UINT64 -> "uint";
                case FLOAT32 -> "float32";
                case FLOAT64 -> "float64";
                case COMPLEX64 -> "complex64";
                case COMPLEX128 -> "complex128";
                case BOOL -> "bool";
                case STRING -> "string";
                default -> k.name().toLowerCase();
            };
        }

        private static boolean isOrderableKey(GoKind k) {
            return switch (k) {
                case INT8, INT16, INT32, INT64, UINT8, UINT16, UINT32, UINT64,
                        FLOAT32, FLOAT64, STRING, BOOL -> true;
                default -> false;
            };
        }

        private static int compareKeys(Object a, Object b, GoKind k) {
            return switch (k) {
                case INT8, INT16, INT32, UINT8, UINT16, UINT32 -> Integer.compare((Integer) a, (Integer) b);
                case INT64, UINT64 -> Long.compare((Long) a, (Long) b);
                case FLOAT32 -> Float.compare((Float) a, (Float) b);
                case FLOAT64 -> Double.compare((Double) a, (Double) b);
                case STRING -> ((GoString) a).compareTo((GoString) b);
                case BOOL -> Boolean.compare((Boolean) a, (Boolean) b);
                default -> 0;
            };
        }
    }
}
