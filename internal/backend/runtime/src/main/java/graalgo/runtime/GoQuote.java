package graalgo.runtime;

/**
 * Go's {@code strconv} quoting (WS G): verbatim port of
 * {@code strconv.appendQuotedWith} / {@code appendEscapedRune} /
 * {@code CanBackquote}, operating on UTF-8 bytes.
 *
 * <p>Used by the {@code %q} verb (and {@code %#v} on strings). Rune
 * decoding follows {@code utf8.DecodeRuneInString}: invalid encodings
 * decode as {@code RuneError} with width 1 and are emitted as
 * {@code \xNN} per byte.
 *
 * <p>Public: the frontend uses {@link #quote} for Go-syntax struct-tag
 * rendering (P0-fmt-022).
 */
public final class GoQuote {
    private GoQuote() {}

    private static final char[] LOWER_HEX = "0123456789abcdef".toCharArray();
    private static final int RUNE_ERROR = 0xFFFD;
    private static final int RUNE_SELF = 0x80;
    private static final int MAX_RUNE = 0x10FFFF;

    /** Decoded rune + width in bytes. Package-visible for byte-wise rune decoding paths (M8). */
    record Rune(int r, int width) {}

    /** utf8.DecodeRuneInString equivalent. Package-visible for byte-wise rune decoding paths (M8). */
    static Rune decodeRune(byte[] s, int i) {
        int n = s.length - i;
        int c0 = s[i] & 0xFF;
        if (c0 < RUNE_SELF) {
            return new Rune(c0, 1);
        }
        // 2-byte sequence.
        if (c0 < 0xE0) {
            if (n < 2) {
                return new Rune(RUNE_ERROR, 1);
            }
            int c1 = s[i + 1] & 0xFF;
            if (c1 < 0x80 || c1 >= 0xC0) {
                return new Rune(RUNE_ERROR, 1);
            }
            int r = ((c0 & 0x1F) << 6) | (c1 & 0x3F);
            if (r < 0x80) {
                return new Rune(RUNE_ERROR, 1); // overlong
            }
            return new Rune(r, 2);
        }
        // 3-byte sequence.
        if (c0 < 0xF0) {
            if (n < 3) {
                return new Rune(RUNE_ERROR, 1);
            }
            int c1 = s[i + 1] & 0xFF, c2 = s[i + 2] & 0xFF;
            if (c1 < 0x80 || c1 >= 0xC0 || c2 < 0x80 || c2 >= 0xC0) {
                return new Rune(RUNE_ERROR, 1);
            }
            int r = ((c0 & 0x0F) << 12) | ((c1 & 0x3F) << 6) | (c2 & 0x3F);
            if (r < 0x800 || (0xD800 <= r && r <= 0xDFFF)) {
                return new Rune(RUNE_ERROR, 1);
            }
            return new Rune(r, 3);
        }
        // 4-byte sequence.
        if (c0 < 0xF8) {
            if (n < 4) {
                return new Rune(RUNE_ERROR, 1);
            }
            int c1 = s[i + 1] & 0xFF, c2 = s[i + 2] & 0xFF, c3 = s[i + 3] & 0xFF;
            if (c1 < 0x80 || c1 >= 0xC0 || c2 < 0x80 || c2 >= 0xC0 || c3 < 0x80 || c3 >= 0xC0) {
                return new Rune(RUNE_ERROR, 1);
            }
            int r = ((c0 & 0x07) << 18) | ((c1 & 0x3F) << 12) | ((c2 & 0x3F) << 6) | (c3 & 0x3F);
            if (r < 0x10000 || r > MAX_RUNE) {
                return new Rune(RUNE_ERROR, 1);
            }
            return new Rune(r, 4);
        }
        return new Rune(RUNE_ERROR, 1);
    }

    private static boolean validRune(int r) {
        return !(r < 0 || r > MAX_RUNE || (0xD800 <= r && r <= 0xDFFF));
    }

    private static void appendRune(StringBuilder sb, int r) {
        sb.appendCodePoint(r);
    }

    /** Port of {@code strconv.appendEscapedRune}. */
    private static void appendEscapedRune(StringBuilder sb, int r, char quote,
            boolean asciiOnly, boolean graphicOnly) {
        if (r == quote || r == '\\') {
            sb.append('\\').append((char) r);
            return;
        }
        if (asciiOnly) {
            if (r < RUNE_SELF && GoIsPrint.isPrint(r)) {
                sb.append((char) r);
                return;
            }
        } else if (GoIsPrint.isPrint(r) || graphicOnly && GoIsPrint.isGraphic(r)) {
            appendRune(sb, r);
            return;
        }
        switch (r) {
            case '\u0007' -> sb.append("\\a");
            case '\b' -> sb.append("\\b");
            case '\f' -> sb.append("\\f");
            case '\n' -> sb.append("\\n");
            case '\r' -> sb.append("\\r");
            case '\t' -> sb.append("\\t");
            case '\u000B' -> sb.append("\\v");
            default -> {
                if (r < ' ' || r == 0x7f) {
                    sb.append("\\x").append(LOWER_HEX[(r >> 4) & 0xF]).append(LOWER_HEX[r & 0xF]);
                } else if (!validRune(r)) {
                    r = RUNE_ERROR;
                    sb.append("\\u");
                    for (int s = 12; s >= 0; s -= 4) {
                        sb.append(LOWER_HEX[(r >> s) & 0xF]);
                    }
                } else if (r < 0x10000) {
                    sb.append("\\u");
                    for (int s = 12; s >= 0; s -= 4) {
                        sb.append(LOWER_HEX[(r >> s) & 0xF]);
                    }
                } else {
                    sb.append("\\U");
                    for (int s = 28; s >= 0; s -= 4) {
                        sb.append(LOWER_HEX[(r >> s) & 0xF]);
                    }
                }
            }
        }
    }

    /**
     * Port of {@code strconv.appendQuotedWith}: quotes UTF-8 bytes
     * {@code s} with {@code quote} ({@code '"'} or {@code '\''}).
     */
    static String quoteWith(byte[] s, char quote, boolean asciiOnly, boolean graphicOnly) {
        StringBuilder sb = new StringBuilder(s.length + 2);
        sb.append(quote);
        for (int i = 0; i < s.length; ) {
            Rune dr = decodeRune(s, i);
            int r = dr.r(), width = dr.width();
            if (width == 1 && r == RUNE_ERROR) {
                sb.append("\\x")
                        .append(LOWER_HEX[(s[i] >> 4) & 0xF])
                        .append(LOWER_HEX[s[i] & 0xF]);
                i += 1;
                continue;
            }
            appendEscapedRune(sb, r, quote, asciiOnly, graphicOnly);
            i += width;
        }
        sb.append(quote);
        return sb.toString();
    }

    /** {@code strconv.Quote} on UTF-8 bytes. Public for frontend type rendering (P0-fmt-022). */
    public static String quote(byte[] s) {
        return quoteWith(s, '"', false, false);
    }

    /** {@code strconv.QuoteToASCII} on UTF-8 bytes. */
    static String quoteToASCII(byte[] s) {
        return quoteWith(s, '"', true, false);
    }

    /** {@code strconv.QuoteRune}. */
    static String quoteRune(int r) {
        if (!validRune(r)) {
            r = RUNE_ERROR;
        }
        StringBuilder sb = new StringBuilder();
        sb.append('\'');
        appendEscapedRune(sb, r, '\'', false, false);
        sb.append('\'');
        return sb.toString();
    }

    /** {@code strconv.QuoteRuneToASCII}. */
    static String quoteRuneToASCII(int r) {
        if (!validRune(r)) {
            r = RUNE_ERROR;
        }
        StringBuilder sb = new StringBuilder();
        sb.append('\'');
        appendEscapedRune(sb, r, '\'', true, false);
        sb.append('\'');
        return sb.toString();
    }

    /** Port of {@code strconv.CanBackquote}. */
    static boolean canBackquote(byte[] s) {
        for (int i = 0; i < s.length; ) {
            Rune dr = decodeRune(s, i);
            int r = dr.r(), wid = dr.width();
            i += wid;
            if (wid > 1) {
                if (r == 0xFEFF) {
                    return false;
                }
                continue;
            }
            if (r == RUNE_ERROR) {
                return false;
            }
            if ((r < ' ' && r != '\t') || r == '`' || r == 0x7F) {
                return false;
            }
        }
        return true;
    }
}
