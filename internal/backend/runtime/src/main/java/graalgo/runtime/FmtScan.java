package graalgo.runtime;

// verifies: GG-0020 (WS G intrinsic surface)

import graalgo.values.GoComplex32;
import graalgo.values.GoComplex64;
import graalgo.values.GoFunc;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
import java.nio.charset.StandardCharsets;

/**
 * The {@code fmt} scan family (P3-M10, fix-fmt-funcs): {@code Scan},
 * {@code Scanf}, {@code Scanln}, {@code Fscan}, {@code Fscanf},
 * {@code Fscanln}, {@code Sscan}, {@code Sscanf}, {@code Sscanln}.
 *
 * <p>A port of gc's {@code fmt/scan.go} (go1.27.1) scanning semantics:
 * space-separated ({@code Scan}), newline-terminated ({@code Scanln}),
 * and format-directed ({@code Scanf}) scanning into pointer destinations
 * (or {@code fmt.Scanner} implementations). Like the D4 {@code Print}
 * family, the scan family is statement-position only in Phase 1 — the
 * {@code (n, err)} result is discarded — but the scan side effects
 * (storing through the destination pointers) are fully realized, so
 * {@code fmt.Sscan("42", &x)} sets {@code x}.
 *
 * <p>Deliberate, documented boundaries of the port:
 * <ul>
 *   <li>destination kinds: pointers to bool, ints, uints, floats,
 *       complexes, strings, and {@code []byte} (named types via their
 *       underlying kind), plus {@code fmt.Scanner}; anything else stops
 *       the scan (gc reports an error);</li>
 *   <li>user-defined {@code fmt.Scanner} implementations: the runtime
 *       dispatch ({@code hasMethod}/{@code scanStateValue}, plus the
 *       Java-installed {@code fmt.ScanState} methods) is in place, but a
 *       {@code Scan} method body calling the multi-result
 *       {@code ScanState} methods ({@code Token}, {@code ReadRune},
 *       {@code Read}) hits the frontend's WS-D fail-fast for multi-result
 *       interface methods (a loud scope error, not silent misbehavior) —
 *       wiring the {@code {"fmt", ...}} pair-desugar entries is
 *       follow-up work;</li>
 *   <li>{@code Scan}/{@code Scanf}/{@code Scanln} read {@code os.Stdin}
 *       (fd 0) one byte at a time — no Java-level prefetching, so mixed
 *       use with {@code os.Stdin.Read} keeps fd semantics;</li>
 *   <li>{@code Fscan} and friends read the {@code io.Reader} like gc's
 *       {@code newScanState}: through {@code ReadRune}/{@code UnreadRune}
 *       when the reader implements {@code io.RuneScanner} (pushback is
 *       then reader-owned, surviving across calls), otherwise lazily
 *       through its {@code Read} method (one byte per call, like gc's
 *       {@code readRune}), so unread input stays available to the
 *       program afterwards.</li>
 * </ul>
 */
public final class FmtScan {
    private FmtScan() {}

    /** Control-flow for scan errors (gc's {@code panic(scanError)}). */
    private static final class ScanError extends RuntimeException {
        ScanError() {}
        ScanError(String msg) {
            super(msg);
        }
    }

    private static final int EOF = -1;
    private static final int HUGE_WID = 1 << 30;

    // ------------------------------------------------------------------
    // Rune input.
    // ------------------------------------------------------------------

    /** One rune of input, or {@link #EOF}. */
    private interface RuneReader {
        int readRune();
        void unreadRune();
    }

    /** Rune reader over a Java string (for the {@code Sscan} family). */
    private static final class StringRuneReader implements RuneReader {
        private final String s;
        private int pos;
        private int prevWidth;
        private boolean canUnread;

        StringRuneReader(String s) {
            this.s = s;
        }

        @Override
        public int readRune() {
            if (pos >= s.length()) {
                return EOF;
            }
            int r = s.codePointAt(pos);
            prevWidth = Character.charCount(r);
            pos += prevWidth;
            canUnread = true;
            return r;
        }

        @Override
        public void unreadRune() {
            if (!canUnread) {
                throw new ScanError("fmt: UnreadRune with no rune available");
            }
            pos -= prevWidth;
            canUnread = false;
        }
    }

    /** One input byte, unsigned, or -1. */
    private interface ByteSource {
        int readByte();
    }

    /**
     * UTF-8 rune decoding over a byte source (gc's {@code readRune}:
     * ASCII fast path, multi-byte accumulation, leftover bytes saved
     * for bad encodings, one-rune pushback for {@code UnreadRune}).
     * Invalid sequences decode to U+FFFD with size 1, like Go's
     * {@code utf8.DecodeRune}.
     */
    private static final class Utf8RuneReader implements RuneReader {
        private final ByteSource src;
        private final byte[] pendBuf = new byte[4];
        private int pending;
        private int peeked = -1;
        private boolean havePeek;
        private boolean canUnread;

        Utf8RuneReader(ByteSource src) {
            this.src = src;
        }

        private int readByte() {
            if (pending > 0) {
                int b = pendBuf[0] & 0xFF;
                System.arraycopy(pendBuf, 1, pendBuf, 0, --pending);
                return b;
            }
            return src.readByte();
        }

        @Override
        public int readRune() {
            if (havePeek) {
                havePeek = false;
                canUnread = true;
                return peeked;
            }
            int b0 = readByte();
            if (b0 < 0) {
                return EOF;
            }
            int r;
            if (b0 < 0x80) {
                r = b0;
            } else {
                byte[] buf = new byte[4];
                buf[0] = (byte) b0;
                int n = 1;
                while (n < 4 && !fullRune(buf, n)) {
                    int b = readByte();
                    if (b < 0) {
                        break; // gc: EOF mid-rune -> DecodeRune on the partial
                    }
                    buf[n++] = (byte) b;
                }
                int[] dr = decodeRune(buf, n);
                r = dr[0];
                int size = dr[1];
                if (size < n) {
                    // Bad encoding: save leftover bytes for the next read.
                    System.arraycopy(buf, size, pendBuf, pending, n - size);
                    pending += n - size;
                }
            }
            peeked = r;
            // NOTE: havePeek stays false here — the rune is consumed. It is
            // set only by unreadRune() for the one-rune pushback. Setting it
            // here (as before) delivered every rune twice.
            canUnread = true;
            return r;
        }

        @Override
        public void unreadRune() {
            if (!canUnread) {
                throw new ScanError("fmt: UnreadRune with no rune available");
            }
            havePeek = true;
            canUnread = false;
        }

        private static boolean fullRune(byte[] buf, int n) {
            int b0 = buf[0] & 0xFF;
            int want;
            if (b0 < 0x80) {
                want = 1;
            } else if ((b0 & 0xE0) == 0xC0) {
                want = 2;
            } else if ((b0 & 0xF0) == 0xE0) {
                want = 3;
            } else if ((b0 & 0xF8) == 0xF0) {
                want = 4;
            } else {
                return true; // invalid lead: DecodeRune handles it
            }
            return n >= want;
        }

        /** Decodes buf[:n]; returns {rune, size}, RuneError on invalid. */
        private static int[] decodeRune(byte[] buf, int n) {
            final int runeError = 0xFFFD;
            if (n == 0) {
                return new int[]{runeError, 0};
            }
            int b0 = buf[0] & 0xFF;
            if (b0 < 0x80) {
                return new int[]{b0, 1};
            }
            int want;
            int r;
            if ((b0 & 0xE0) == 0xC0) {
                want = 2;
                r = b0 & 0x1F;
            } else if ((b0 & 0xF0) == 0xE0) {
                want = 3;
                r = b0 & 0x0F;
            } else if ((b0 & 0xF8) == 0xF0) {
                want = 4;
                r = b0 & 0x07;
            } else {
                return new int[]{runeError, 1};
            }
            if (n < want) {
                return new int[]{runeError, 1};
            }
            for (int i = 1; i < want; i++) {
                int b = buf[i] & 0xFF;
                if ((b & 0xC0) != 0x80) {
                    return new int[]{runeError, 1};
                }
                r = (r << 6) | (b & 0x3F);
            }
            // Reject overlongs, surrogates, out-of-range.
            int min = want == 2 ? 0x80 : want == 3 ? 0x800 : 0x10000;
            if (r < min || (r >= 0xD800 && r <= 0xDFFF) || r > 0x10FFFF) {
                return new int[]{runeError, 1};
            }
            return new int[]{r, want};
        }
    }

    /** Rune reader over a Go {@code io.Reader} (for the {@code Fscan} family). */
    private static final class GoReaderRuneReader implements RuneReader {
        private final Utf8RuneReader rr;

        GoReaderRuneReader(Fmt.GoMethodDispatch d, Fmt.Arg r) {
            // int[]-backed: the D5 representation of []byte.
            final int[] one = new int[1];
            this.rr = new Utf8RuneReader(() -> {
                Object pair = d.call(r, "Read", new Object[]{new GoSlice(one, 0, 1, 1)});
                Object[] f = Fmt.pairFields(pair);
                long n = ((Number) f[0]).longValue();
                Object err = f[1];
                if (n > 0) {
                    return one[0] & 0xFF;
                }
                if (err != null && !(err instanceof GoInterface gi && gi.isNil())) {
                    // Any read error (including io.EOF) ends the rune stream.
                    // (gc's readRune: EOF -> atEOF; other errors -> s.error.)
                    if (!Fmt.isEofError(err)) {
                        throw new ScanError("fmt: reader error");
                    }
                }
                return -1;
            });
        }

        @Override
        public int readRune() {
            return rr.readRune();
        }

        @Override
        public void unreadRune() {
            rr.unreadRune();
        }
    }

    /**
     * Rune reader over a Go {@code io.RuneScanner} (for the {@code Fscan}
     * family). gc's {@code newScanState}: when the reader implements
     * {@code io.RuneScanner} its {@code ReadRune}/{@code UnreadRune} are
     * used directly, so pushback is reader-owned and survives across
     * top-level {@code Fscan} calls (e.g. {@code Fscan} followed by
     * {@code Fscanln} on the same {@code strings.Reader}).
     */
    private static final class GoRuneScannerReader implements RuneReader {
        private final Fmt.GoMethodDispatch d;
        private final Fmt.Arg r;

        GoRuneScannerReader(Fmt.GoMethodDispatch d, Fmt.Arg r) {
            this.d = d;
            this.r = r;
        }

        @Override
        public int readRune() {
            Object[] t = Fmt.tripleFields(d.call(r, "ReadRune", new Object[0]));
            Object err = t[2];
            if (err != null && !(err instanceof GoInterface gi && gi.isNil())) {
                if (Fmt.isEofError(err)) {
                    return EOF;
                }
                throw new ScanError("fmt: ReadRune error");
            }
            return ((Number) t[0]).intValue();
        }

        @Override
        public void unreadRune() {
            Object err = d.call(r, "UnreadRune", new Object[0]);
            if (err != null && !(err instanceof GoInterface gi && gi.isNil())) {
                throw new ScanError("fmt: UnreadRune with no rune available");
            }
        }
    }

    /**
     * Selects the rune reader for a Go {@code io.Reader} (gc's
     * {@code newScanState}): {@code io.RuneScanner} readers are driven
     * directly, others through a per-call UTF-8 adapter over {@code Read}.
     */
    private static RuneReader goRuneReader(Fmt.GoMethodDispatch d, Fmt.Arg r) {
        if (d.isScanStateValue(r.value())) {
            // gc's ss.reset: Fscan/Fscanf/Fscanln on a ScanState value
            // (the state passed to a custom Scanner.Scan) reuses the
            // in-progress scan's rune stream instead of calling the
            // stubbed ScanState.Read.
            ScanState current = currentScan.get();
            if (current == null) {
                throw new ScanError("fmt: ScanState used outside of Scan");
            }
            return current.runeReader();
        }
        if (d.hasMethod(r, "ReadRune") && d.hasMethod(r, "UnreadRune")) {
            return new GoRuneScannerReader(d, r);
        }
        return new GoReaderRuneReader(d, r);
    }

    /** Rune reader over fd 0 (for {@code Scan}/{@code Scanf}/{@code Scanln}). */
    private static final class StdinRuneReader implements RuneReader {
        private final Utf8RuneReader rr;

        StdinRuneReader() {
            this.rr = new Utf8RuneReader(() -> {
                try {
                    return System.in.read();
                } catch (java.io.IOException e) {
                    return -1;
                }
            });
        }

        @Override
        public int readRune() {
            return rr.readRune();
        }

        @Override
        public void unreadRune() {
            rr.unreadRune();
        }
    }

    // ------------------------------------------------------------------
    // The scanner (gc's ss).
    // ------------------------------------------------------------------

    /**
     * The current scan state, for the Java-implemented
     * {@code fmt.ScanState} methods (installed by the interop layer).
     * Set around each top-level scan and around each {@code Scanner.Scan}
     * dispatch, like {@code Fmt.currentPrinter}.
     */
    public static final ThreadLocal<ScanState> currentScan = new ThreadLocal<>();

    /** The scanner state (port of gc's {@code ss}). */
    public static final class ScanState {
        private final RuneReader rr;
        // The underlying rune stream, shared when a ScanState value is
        // passed to Fscan/Fscanf/Fscanln (gc's ss.reset reuses an *ss).
        RuneReader runeReader() {
            return rr;
        }
        private final java.io.ByteArrayOutputStream buf = new java.io.ByteArrayOutputStream();
        private int count;
        private int argLimit = HUGE_WID;
        private final int limit = HUGE_WID;
        private int maxWid = HUGE_WID;
        private boolean nlIsSpace;
        private boolean nlIsEnd;
        private boolean atEOF;

        ScanState(RuneReader rr, boolean nlIsSpace, boolean nlIsEnd) {
            this.rr = rr;
            this.nlIsSpace = nlIsSpace;
            this.nlIsEnd = nlIsEnd;
        }

        // -- input primitives --
        // Public: the interop-installed fmt.ScanState methods (in
        // graalgo.iface, a different package) delegate through these.

        public int readRuneChecked() {
            if (atEOF || count >= argLimit) {
                return EOF;
            }
            int r = rr.readRune();
            if (r == EOF) {
                atEOF = true;
                return EOF;
            }
            count++;
            if (nlIsEnd && r == '\n') {
                atEOF = true;
            }
            return r;
        }

        /** gc's getRune: EOF panics are ScanError here (unobservable). */
        int getRune() {
            return readRuneChecked();
        }

        int mustReadRune() {
            int r = getRune();
            if (r == EOF) {
                throw new ScanError("unexpected EOF");
            }
            return r;
        }

        public void unreadRune() {
            rr.unreadRune();
            atEOF = false;
            count--;
        }

        void error(String msg) {
            throw new ScanError(msg);
        }

        // -- java-side accessors for the ScanState method impls --

        /** gc's Width: (0,false) when no width was set. */
        public long scanWidth() {
            return maxWid == HUGE_WID ? 0 : maxWid;
        }

        public boolean scanWidthOk() {
            return maxWid != HUGE_WID;
        }

        // -- whitespace and tokens --

        public void skipSpace() {
            for (;;) {
                int r = getRune();
                if (r == EOF) {
                    return;
                }
                if (r == '\r' && peek("\n")) {
                    continue;
                }
                if (r == '\n') {
                    if (nlIsSpace) {
                        continue;
                    }
                    error("unexpected newline");
                    return;
                }
                if (!isSpace(r)) {
                    unreadRune();
                    break;
                }
            }
        }

        /** Next space-delimited token (gc's token). */
        public byte[] token(boolean skipSpace, java.util.function.IntPredicate f) {
            if (skipSpace) {
                skipSpace();
            }
            buf.reset();
            for (;;) {
                int r = getRune();
                if (r == EOF) {
                    break;
                }
                if (!f.test(r)) {
                    unreadRune();
                    break;
                }
                writeRune(r);
            }
            return buf.toByteArray();
        }

        boolean consume(String ok, boolean accept) {
            int r = getRune();
            if (r == EOF) {
                return false;
            }
            if (ok.indexOf((char) r) >= 0 && r < 128) {
                if (accept) {
                    writeRune(r);
                }
                return true;
            }
            if (accept) {
                unreadRune();
            }
            return false;
        }

        boolean peek(String ok) {
            int r = getRune();
            if (r != EOF) {
                unreadRune();
            }
            return r < 128 && ok.indexOf((char) r) >= 0;
        }

        void notEOF() {
            if (getRune() == EOF) {
                throw new ScanError("EOF");
            }
            unreadRune();
        }

        boolean accept(String ok) {
            return consume(ok, true);
        }

        boolean okVerb(int verb, String okVerbs, String typ) {
            for (int i = 0; i < okVerbs.length(); i++) {
                if (okVerbs.charAt(i) == verb) {
                    return true;
                }
            }
            error("bad verb '%" + (char) verb + "' for " + typ);
            return false;
        }

        // -- booleans --

        boolean scanBool(int verb) {
            skipSpace();
            notEOF();
            if (!okVerb(verb, "tv", "boolean")) {
                return false;
            }
            switch (getRune()) {
                case '0':
                    return false;
                case '1':
                    return true;
                case 't':
                case 'T':
                    if (accept("rR") && (!accept("uU") || !accept("eE"))) {
                        error("syntax error scanning boolean");
                    }
                    return true;
                case 'f':
                case 'F':
                    if (accept("aA") && (!accept("lL") || !accept("sS") || !accept("eE"))) {
                        error("syntax error scanning boolean");
                    }
                    return false;
                default:
                    return false;
            }
        }

        // -- integers --

        private static final String BINARY_DIGITS = "01";
        private static final String OCTAL_DIGITS = "01234567";
        private static final String DECIMAL_DIGITS = "0123456789";
        private static final String HEX_DIGITS = "0123456789aAbBcCdDeEfF";
        private static final String SIGN = "+-";

        private String baseDigits(int verb) {
            switch (verb) {
                case 'b':
                    return BINARY_DIGITS;
                case 'o':
                    return OCTAL_DIGITS;
                case 'x':
                case 'X':
                case 'U':
                    return HEX_DIGITS;
                default:
                    return DECIMAL_DIGITS;
            }
        }

        String scanNumber(String digits, boolean haveDigits) {
            if (!haveDigits) {
                notEOF();
                if (!accept(digits)) {
                    error("expected integer");
                }
            }
            while (accept(digits)) {
            }
            return bufString();
        }

        long scanRune(int bitSize) {
            notEOF();
            int r = getRune();
            int n = bitSize;
            long x = (long) r << (64 - n) >> (64 - n);
            if (x != r) {
                error("overflow on character value");
            }
            return r;
        }

        /** gc's scanBasePrefix: (base, digits, zeroFound, haveDigits). */
        private Object[] scanBasePrefix() {
            if (!peek("0")) {
                return new Object[]{0, DECIMAL_DIGITS + "_", false, false};
            }
            accept("0");
            if (peek("bB")) {
                consume("bB", true);
                return new Object[]{0, BINARY_DIGITS + "_", true, true};
            }
            if (peek("oO")) {
                consume("oO", true);
                return new Object[]{0, OCTAL_DIGITS + "_", true, true};
            }
            if (peek("xX")) {
                consume("xX", true);
                return new Object[]{0, HEX_DIGITS + "_", true, true};
            }
            return new Object[]{0, OCTAL_DIGITS + "_", true, true};
        }

        long scanInt(int verb, int bitSize) {
            if (verb == 'c') {
                return scanRune(bitSize);
            }
            skipSpace();
            notEOF();
            int base;
            String digits;
            boolean haveDigits = false;
            if (verb == 'U') {
                base = 16;
                digits = HEX_DIGITS;
                if (!consume("U", false) || !consume("+", false)) {
                    error("bad unicode format ");
                }
            } else {
                base = verb == 'b' ? 2 : verb == 'o' ? 8
                        : (verb == 'x' || verb == 'X') ? 16 : 10;
                digits = baseDigits(verb);
                okVerb(verb, "bdoUxXv", "integer");
                accept(SIGN);
                if (verb == 'v') {
                    Object[] bp = scanBasePrefix();
                    base = (Integer) bp[0];
                    digits = (String) bp[1];
                    haveDigits = (Boolean) bp[3];
                }
            }
            String tok = scanNumber(digits, haveDigits);
            long i = parseInt(tok, base);
            int n = bitSize;
            long x = (i << (64 - n)) >> (64 - n);
            if (x != i) {
                error("integer overflow on token " + tok);
            }
            return i;
        }

        long scanUint(int verb, int bitSize) {
            if (verb == 'c') {
                return scanRune(bitSize);
            }
            skipSpace();
            notEOF();
            int base;
            String digits;
            boolean haveDigits = false;
            if (verb == 'U') {
                base = 16;
                digits = HEX_DIGITS;
                if (!consume("U", false) || !consume("+", false)) {
                    error("bad unicode format ");
                }
            } else {
                base = verb == 'b' ? 2 : verb == 'o' ? 8
                        : (verb == 'x' || verb == 'X') ? 16 : 10;
                digits = baseDigits(verb);
                okVerb(verb, "bdoUxXv", "integer");
                if (verb == 'v') {
                    Object[] bp = scanBasePrefix();
                    base = (Integer) bp[0];
                    digits = (String) bp[1];
                    haveDigits = (Boolean) bp[3];
                }
            }
            String tok = scanNumber(digits, haveDigits);
            long i = parseUint(tok, base);
            int n = bitSize;
            long x = (i << (64 - n)) >>> (64 - n);
            if (x != i) {
                error("unsigned integer overflow on token " + tok);
            }
            return i;
        }

        // -- floats --

        String floatToken() {
            buf.reset();
            if (accept("nN") && accept("aA") && accept("nN")) {
                return bufString();
            }
            accept(SIGN);
            if (accept("iI") && accept("nN") && accept("fF")) {
                return bufString();
            }
            String digits = DECIMAL_DIGITS + "_";
            String exp = "eEpP";
            if (accept("0") && accept("xX")) {
                digits = HEX_DIGITS + "_";
                exp = "pP";
            }
            while (accept(digits)) {
            }
            if (accept(".")) {
                while (accept(digits)) {
                }
            }
            if (accept(exp)) {
                accept(SIGN);
                while (accept(DECIMAL_DIGITS + "_")) {
                }
            }
            return bufString();
        }

        double convertFloat(String str, int n) {
            int p = str.indexOf('p');
            if (p < 0) {
                p = str.indexOf('P');
            }
            if (p >= 0 && str.indexOf('x') < 0 && str.indexOf('X') < 0) {
                // Non-standard decimal+binary exponent mix (1.2p4).
                double f = n == 32 ? Float.parseFloat(str.substring(0, p))
                        : Double.parseDouble(str.substring(0, p));
                int m = Integer.parseInt(str.substring(p + 1).replace("_", ""));
                return Math.scalb(f, m);
            }
            String clean = str.replace("_", "");
            // Java's parseDouble rejects Go's "inf"/"nan" spellings.
            String low = clean.toLowerCase();
            String mag = low.startsWith("+") || low.startsWith("-") ? low.substring(1) : low;
            boolean neg = low.startsWith("-");
            if (mag.equals("inf") || mag.equals("infinity")) {
                return neg ? Double.NEGATIVE_INFINITY : Double.POSITIVE_INFINITY;
            }
            if (mag.equals("nan")) {
                return Double.NaN;
            }
            try {
                return n == 32 ? Float.parseFloat(clean) : Double.parseDouble(clean);
            } catch (NumberFormatException e) {
                throw new ScanError("float syntax: " + str);
            }
        }

        // -- complex --

        private String[] complexTokens() {
            boolean parens = accept("(");
            String real = floatToken();
            buf.reset();
            if (!accept("+-")) {
                error("syntax error scanning complex number");
            }
            String imagSign = bufString();
            String imag = floatToken();
            if (!accept("i")) {
                error("syntax error scanning complex number");
            }
            if (parens && !accept(")")) {
                error("syntax error scanning complex number");
            }
            return new String[]{real, imagSign + imag};
        }

        void scanComplexInto(int verb, int n, double[] out) {
            if (!okVerb(verb, "beEfFgGv", "complex")) {
                return;
            }
            skipSpace();
            notEOF();
            String[] toks = complexTokens();
            out[0] = convertFloat(toks[0], n / 2);
            out[1] = convertFloat(toks[1], n / 2);
        }

        // -- strings --

        String convertString(int verb) {
            if (!okVerb(verb, "svqxX", "string")) {
                return "";
            }
            skipSpace();
            notEOF();
            switch (verb) {
                case 'q':
                    return quotedString();
                case 'x':
                case 'X':
                    return hexString();
                default:
                    return new String(token(true, FmtScan::notSpace), StandardCharsets.UTF_8);
            }
        }

        private String quotedString() {
            notEOF();
            int quote = getRune();
            buf.reset();
            switch (quote) {
                case '`':
                    for (;;) {
                        int r = mustReadRune();
                        if (r == quote) {
                            break;
                        }
                        writeRune(r);
                    }
                    return bufString();
                case '"':
                    buf.write('"');
                    for (;;) {
                        int r = mustReadRune();
                        writeRune(r);
                        if (r == '\\') {
                            writeRune(mustReadRune());
                        } else if (r == '"') {
                            break;
                        }
                    }
                    return unquote(bufString());
                default:
                    error("expected quoted string");
                    return "";
            }
        }

        private String hexString() {
            notEOF();
            buf.reset();
            for (;;) {
                int[] hb = hexByte();
                if (hb == null) {
                    break;
                }
                buf.write(hb[0]);
            }
            if (buf.size() == 0) {
                error("no hex data for %x string");
            }
            return new String(buf.toByteArray(), StandardCharsets.ISO_8859_1);
        }

        /** Returns {byte} or null when the next bytes are not hex. */
        private int[] hexByte() {
            int r1 = getRune();
            if (r1 == EOF) {
                return null;
            }
            int[] d1 = hexDigit(r1);
            if (d1 == null) {
                unreadRune();
                return null;
            }
            int r2 = mustReadRune();
            int[] d2 = hexDigit(r2);
            if (d2 == null) {
                error("illegal hex digit");
            }
            return new int[]{(d1[0] << 4) | d2[0]};
        }

        private static int[] hexDigit(int d) {
            if (d >= '0' && d <= '9') {
                return new int[]{d - '0'};
            }
            if (d >= 'a' && d <= 'f') {
                return new int[]{10 + d - 'a'};
            }
            if (d >= 'A' && d <= 'F') {
                return new int[]{10 + d - 'A'};
            }
            return null;
        }

        // -- one value --

        void scanOne(int verb, Fmt.Arg arg) {
            buf.reset();
            Fmt.GoMethodDispatch d = Fmt.goMethodDispatch();
            if (d != null && d.hasMethod(arg, "Scan")) {
                Object stateIface = d.scanStateValue();
                currentScan.set(this);
                try {
                    Object err = d.call(arg, "Scan", new Object[]{stateIface, verb});
                    if (err instanceof GoInterface gi && !gi.isNil()) {
                        throw new ScanError("scanner error");
                    }
                } finally {
                    currentScan.remove();
                }
                return;
            }
            Object v = arg.value();
            if (!(v instanceof GoPointer ptr) || ptr.isNil()) {
                error("type not a pointer");
                return;
            }
            GoKind ek = arg.elemKind();
            if (ek == null) {
                error("type not a pointer");
                return;
            }
            switch (ek) {
                case BOOL:
                    ptr.setReferent(Boolean.valueOf(scanBool(verb)));
                    break;
                case INT8:
                case INT16:
                case INT32:
                    ptr.setReferent(Integer.valueOf((int) scanInt(verb, ek.bits())));
                    break;
                case INT64:
                    ptr.setReferent(Long.valueOf(scanInt(verb, 64)));
                    break;
                case UINT8:
                case UINT16:
                case UINT32:
                    ptr.setReferent(Integer.valueOf((int) scanUint(verb, ek.bits())));
                    break;
                case UINT64:
                    ptr.setReferent(Long.valueOf(scanUint(verb, 64)));
                    break;
                case FLOAT32:
                    if (okVerb(verb, "beEfFgGv", "float32")) {
                        skipSpace();
                        notEOF();
                        ptr.setReferent(Float.valueOf((float) convertFloat(floatToken(), 32)));
                    }
                    break;
                case FLOAT64:
                    if (okVerb(verb, "beEfFgGv", "float64")) {
                        skipSpace();
                        notEOF();
                        ptr.setReferent(Double.valueOf(convertFloat(floatToken(), 64)));
                    }
                    break;
                case COMPLEX64: {
                    double[] c = new double[2];
                    scanComplexInto(verb, 64, c);
                    ptr.setReferent(new GoComplex32((float) c[0], (float) c[1]));
                    break;
                }
                case COMPLEX128: {
                    double[] c = new double[2];
                    scanComplexInto(verb, 128, c);
                    ptr.setReferent(new GoComplex64(c[0], c[1]));
                    break;
                }
                case STRING:
                    ptr.setReferent(new GoString(
                            convertString(verb).getBytes(StandardCharsets.UTF_8)));
                    break;
                case SLICE:
                    // gc: only (renamed) []byte. int[]-backed: the D5
                    // representation of []byte.
                    if (isByteSlice(arg)) {
                        String s = convertString(verb);
                        byte[] bytes = s.getBytes(StandardCharsets.UTF_8);
                        int[] arr = new int[bytes.length];
                        for (int i = 0; i < bytes.length; i++) {
                            arr[i] = bytes[i] & 0xFF;
                        }
                        ptr.setReferent(new GoSlice(arr, 0, arr.length, arr.length));
                    } else {
                        error("can't scan type: " + arg.typeName());
                    }
                    break;
                default:
                    error("can't scan type: " + arg.typeName());
                    break;
            }
        }

        // -- drivers --

        void doScan(Fmt.Arg[] args) {
            try {
                for (Fmt.Arg arg : args) {
                    scanOne('v', arg);
                }
                if (nlIsEnd) {
                    for (;;) {
                        int r = getRune();
                        if (r == '\n' || r == EOF) {
                            break;
                        }
                        if (!isSpace(r)) {
                            error("expected newline");
                            break;
                        }
                    }
                }
            } catch (ScanError e) {
                // (n, err) discarded in statement position; stop like gc.
            }
        }

        void doScanf(String format, Fmt.Arg[] args) {
            try {
                int[] fpcs = format.codePoints().toArray();
                int end = fpcs.length - 1;
                int numProcessed = 0;
                for (int i = 0; i <= end;) {
                    int w = advance(fpcs, i);
                    if (w > 0) {
                        i += w;
                        continue;
                    }
                    if (fpcs[i] != '%') {
                        if (w < 0) {
                            error("input does not match format");
                        }
                        break;
                    }
                    i++; // %
                    // Width.
                    int num = 0;
                    boolean widPresent = false;
                    while (i <= end && fpcs[i] >= '0' && fpcs[i] <= '9') {
                        num = num * 10 + (fpcs[i] - '0');
                        widPresent = true;
                        i++;
                    }
                    maxWid = widPresent ? num : HUGE_WID;
                    int c = fpcs[i];
                    i++;
                    if (c != 'c') {
                        skipSpace();
                    }
                    if (c == '%') {
                        scanPercent();
                        continue;
                    }
                    argLimit = limit;
                    int f = count + maxWid;
                    if (f < argLimit) {
                        argLimit = f;
                    }
                    if (numProcessed >= args.length) {
                        error("too few operands for format '%" + (char) c + "'");
                        break;
                    }
                    scanOne(c, args[numProcessed]);
                    numProcessed++;
                    argLimit = limit;
                }
                if (numProcessed < args.length) {
                    error("too many operands");
                }
            } catch (ScanError e) {
                // (n, err) discarded in statement position; stop like gc.
            }
        }

        /**
         * gc's advance: matches literals/space in the format against the
         * input. Returns code points consumed from the format, 0 when the
         * format starts with a verb, negative on mismatch.
         */
        int advance(int[] fpcs, int start) {
            int i = start;
            while (i < fpcs.length) {
                int fmtc = fpcs[i];
                if (isSpace(fmtc)) {
                    int newlines = 0;
                    boolean trailingSpace = false;
                    while (i < fpcs.length && isSpace(fpcs[i])) {
                        if (fpcs[i] == '\n') {
                            newlines++;
                            trailingSpace = false;
                        } else {
                            trailingSpace = true;
                        }
                        i++;
                    }
                    for (int j = 0; j < newlines; j++) {
                        int inputc = getRune();
                        while (isSpace(inputc) && inputc != '\n') {
                            inputc = getRune();
                        }
                        if (inputc != '\n' && inputc != EOF) {
                            error("newline in format does not match input");
                        }
                    }
                    if (trailingSpace) {
                        int inputc = getRune();
                        if (newlines == 0) {
                            if (!isSpace(inputc) && inputc != EOF) {
                                error("expected space in input to match format");
                            }
                            if (inputc == '\n') {
                                error("newline in input does not match format");
                            }
                        }
                        while (isSpace(inputc) && inputc != '\n') {
                            inputc = getRune();
                        }
                        if (inputc != EOF) {
                            unreadRune();
                        }
                    }
                    continue;
                }
                if (fmtc == '%') {
                    if (i + 1 == fpcs.length) {
                        error("missing verb: % at end of format string");
                    }
                    if (fpcs[i + 1] != '%') {
                        return i - start;
                    }
                    i++; // skip the first %
                }
                int inputc = mustReadRune();
                if (fmtc != inputc) {
                    unreadRune();
                    return -1;
                }
                i++;
            }
            return i - start;
        }

        void scanPercent() {
            skipSpace();
            notEOF();
            if (!accept("%")) {
                error("missing literal %");
            }
        }

        // -- buffer helpers --

        private void writeRune(int r) {
            if (r < 0x80) {
                buf.write(r);
            } else if (r < 0x800) {
                buf.write(0xC0 | (r >> 6));
                buf.write(0x80 | (r & 0x3F));
            } else if (r < 0x10000) {
                buf.write(0xE0 | (r >> 12));
                buf.write(0x80 | ((r >> 6) & 0x3F));
                buf.write(0x80 | (r & 0x3F));
            } else {
                buf.write(0xF0 | (r >> 18));
                buf.write(0x80 | ((r >> 12) & 0x3F));
                buf.write(0x80 | ((r >> 6) & 0x3F));
                buf.write(0x80 | (r & 0x3F));
            }
        }

        private String bufString() {
            return new String(buf.toByteArray(), StandardCharsets.UTF_8);
        }
    }

    // ------------------------------------------------------------------
    // Helpers.
    // ------------------------------------------------------------------

    /** gc's White_Space table (public for the interop ScanState Token impl). */
    public static boolean isSpace(int r) {
        if (r >= 1 << 16) {
            return false;
        }
        // gc's White_Space table copy.
        int[][] space = {
            {0x0009, 0x000D}, {0x0020, 0x0020}, {0x0085, 0x0085},
            {0x00A0, 0x00A0}, {0x1680, 0x1680}, {0x2000, 0x200A},
            {0x2028, 0x2029}, {0x202F, 0x202F}, {0x205F, 0x205F},
            {0x3000, 0x3000},
        };
        for (int[] rng : space) {
            if (r < rng[0]) {
                return false;
            }
            if (r <= rng[1]) {
                return true;
            }
        }
        return false;
    }

    private static boolean notSpace(int r) {
        return !isSpace(r);
    }

    /** True when the arg is a pointer to a (possibly renamed) byte slice. */
    private static boolean isByteSlice(Fmt.Arg arg) {
        // elemKindChain carries the referent's element kind for
        // pointer-to-slice (populated by the factory, P3-M10).
        java.util.List<GoKind> chain = arg.elemKindChain();
        if (chain != null && !chain.isEmpty()) {
            GoKind ek = chain.get(0);
            return ek == GoKind.UINT8;
        }
        // Fallback: the Go-syntax element type name.
        String etn = arg.elemTypeName();
        return etn != null && (etn.equals("[]uint8") || etn.equals("[]byte"));
    }

    /** Go's ParseInt semantics: base 0 infers from prefix; underscores ok. */
    private static long parseInt(String tok, int base) {
        String[] nb = normalizeInt(tok, base);
        try {
            return Long.parseLong(nb[0], Integer.parseInt(nb[1]));
        } catch (NumberFormatException e) {
            throw new ScanError("integer syntax: " + tok);
        }
    }

    private static long parseUint(String tok, int base) {
        String[] nb = normalizeInt(tok, base);
        try {
            return Long.parseUnsignedLong(nb[0], Integer.parseInt(nb[1]));
        } catch (NumberFormatException e) {
            throw new ScanError("integer syntax: " + tok);
        }
    }

    /** Returns {digits, base}: strips underscores, resolves base-0 prefixes. */
    private static String[] normalizeInt(String tok, int base) {
        String t = tok.replace("_", "");
        int b = base;
        if (b == 0) {
            String u = t.startsWith("+") || t.startsWith("-") ? t.substring(1) : t;
            if (u.startsWith("0b") || u.startsWith("0B")) {
                b = 2;
                t = t.substring(0, t.length() - u.length()) + u.substring(2);
            } else if (u.startsWith("0o") || u.startsWith("0O")) {
                b = 8;
                t = t.substring(0, t.length() - u.length()) + u.substring(2);
            } else if (u.startsWith("0x") || u.startsWith("0X")) {
                b = 16;
                t = t.substring(0, t.length() - u.length()) + u.substring(2);
            } else if (u.startsWith("0") && u.length() > 1) {
                b = 8;
            } else {
                b = 10;
            }
        }
        return new String[]{t, Integer.toString(b)};
    }

    /**
     * Minimal {@code strconv.Unquote} for double-quoted strings (gc's
     * quotedString path): handles the common escapes; anything exotic
     * stops the scan (gc would error).
     */
    private static String unquote(String quoted) {
        // quoted includes the surrounding quotes.
        StringBuilder sb = new StringBuilder();
        for (int i = 1; i + 1 < quoted.length();) {
            char c = quoted.charAt(i);
            if (c != '\\') {
                sb.append(c);
                i++;
                continue;
            }
            i++;
            if (i + 1 > quoted.length()) {
                throw new ScanError("bad quoted string escape");
            }
            char e = quoted.charAt(i);
            i++;
            switch (e) {
                case 'n':
                    sb.append('\n');
                    break;
                case 't':
                    sb.append('\t');
                    break;
                case 'r':
                    sb.append('\r');
                    break;
                case '\\':
                    sb.append('\\');
                    break;
                case '"':
                    sb.append('"');
                    break;
                case '\'':
                    sb.append('\'');
                    break;
                case 'x':
                    sb.append((char) hexVal(quoted, i, 2));
                    i += 2;
                    break;
                case 'u':
                    sb.append((char) hexVal(quoted, i, 4));
                    i += 4;
                    break;
                case 'U': {
                    int cp = hexVal(quoted, i, 8);
                    i += 8;
                    sb.append(Character.toChars(cp));
                    break;
                }
                default:
                    throw new ScanError("bad quoted string escape");
            }
        }
        return sb.toString();
    }

    private static int hexVal(String s, int i, int n) {
        if (i + n > s.length()) {
            throw new ScanError("bad quoted string escape");
        }
        try {
            return Integer.parseInt(s.substring(i, i + n), 16);
        } catch (NumberFormatException e) {
            throw new ScanError("bad quoted string escape");
        }
    }

    // ------------------------------------------------------------------
    // Entry points (called from FmtCallNode).
    // ------------------------------------------------------------------

    private static Fmt.GoMethodDispatch requireDispatch(String what) {
        Fmt.GoMethodDispatch d = Fmt.goMethodDispatch();
        if (d == null) {
            throw new graalgo.frontend.FrontendException(
                    "fmt." + what + ": no Go method dispatch installed (needs the phase-3 launcher)");
        }
        return d;
    }

    private static String goString(Fmt.Arg arg, String what) {
        Object v = arg.value();
        if (v instanceof GoString gs) {
            return new String(gs.bytes(), StandardCharsets.UTF_8);
        }
        throw new graalgo.frontend.FrontendException("fmt." + what + ": string argument is not a string");
    }

    /** {@code Sscan}. */
    public static void sscan(Fmt.Arg str, Fmt.Arg[] args) {
        new ScanState(new StringRuneReader(goString(str, "Sscan")), true, false).doScan(args);
    }

    /** {@code Sscanf}. */
    public static void sscanf(Fmt.Arg str, String format, Fmt.Arg[] args) {
        new ScanState(new StringRuneReader(goString(str, "Sscanf")), false, false).doScanf(format, args);
    }

    /** {@code Sscanln}. */
    public static void sscanln(Fmt.Arg str, Fmt.Arg[] args) {
        new ScanState(new StringRuneReader(goString(str, "Sscanln")), false, true).doScan(args);
    }

    /** {@code Fscan}. */
    public static void fscan(Fmt.Arg r, Fmt.Arg[] args) {
        Fmt.GoMethodDispatch d = requireDispatch("Fscan");
        new ScanState(goRuneReader(d, r), true, false).doScan(args);
    }

    /** {@code Fscanf}. */
    public static void fscanf(Fmt.Arg r, String format, Fmt.Arg[] args) {
        Fmt.GoMethodDispatch d = requireDispatch("Fscanf");
        new ScanState(goRuneReader(d, r), false, false).doScanf(format, args);
    }

    /** {@code Fscanln}. */
    public static void fscanln(Fmt.Arg r, Fmt.Arg[] args) {
        Fmt.GoMethodDispatch d = requireDispatch("Fscanln");
        new ScanState(goRuneReader(d, r), false, true).doScan(args);
    }

    /** {@code Scan} (os.Stdin). */
    public static void scanStdin(Fmt.Arg[] args) {
        new ScanState(new StdinRuneReader(), true, false).doScan(args);
    }

    /** {@code Scanf} (os.Stdin). */
    public static void scanfStdin(String format, Fmt.Arg[] args) {
        new ScanState(new StdinRuneReader(), false, false).doScanf(format, args);
    }

    /** {@code Scanln} (os.Stdin). */
    public static void scanlnStdin(Fmt.Arg[] args) {
        new ScanState(new StdinRuneReader(), false, true).doScan(args);
    }
}
