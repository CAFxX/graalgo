package graalgo.gir;

import java.util.Arrays;

/**
 * Decodes GIR primitives from a byte range and validates every reference against the table
 * sizes it was given. All failures are {@link GirFormatException}s; the generated {@link
 * GirCodec} readers call nothing else, so no input can make decoding throw anything else, loop or
 * allocate more than a constant factor of the input size (every list element occupies at least
 * one byte, and {@link #count()} bounds lists by the remaining input).
 */
final class GirReader {
    static final int REF_TYPE = 0;
    static final int REF_SYM = 1;
    static final int REF_OBJ = 2;
    static final int REF_FN = 3;
    private static final String[] REF_NAMES = {"type", "sym", "obj", "fn"};

    static final int MAX_DEPTH = Gir.MAX_DEPTH;

    private final byte[] data;
    private final int end;
    private int off;
    private final int nstrings;
    private final int[] counts;
    private int depth;
    // scope stack: (positions, locals, labels) per open scope
    private int[] scopes = new int[3 * 8];
    private int nscopes;

    GirReader(byte[] data, int off, int len, int nstrings, int[] counts) {
        this.data = data;
        this.off = off;
        this.end = off + len;
        this.nstrings = nstrings;
        this.counts = counts;
    }

    GirFormatException fail(String message) {
        return new GirFormatException(off, message);
    }

    int offset() {
        return off;
    }

    int remaining() {
        return end - off;
    }

    long uvarint() {
        long v = 0;
        for (int shift = 0, i = 0;; shift += 7, i++) {
            if (off >= end) {
                throw fail("truncated varint");
            }
            int c = data[off++] & 0xff;
            if (i == 9 && c > 1) {
                throw fail("varint overflows 64 bits");
            }
            v |= (long) (c & 0x7f) << shift;
            if (c < 0x80) {
                if (c == 0 && i > 0) {
                    throw fail("non-minimal varint");
                }
                return v;
            }
        }
    }

    long varint() {
        long u = uvarint();
        return (u >>> 1) ^ -(u & 1);
    }

    boolean bool() {
        if (off >= end) {
            throw fail("truncated bool");
        }
        int c = data[off++];
        if (c != 0 && c != 1) {
            throw fail("bad bool " + c);
        }
        return c == 1;
    }

    int count() {
        long n = uvarint();
        if (Long.compareUnsigned(n, remaining()) > 0) {
            throw fail("list of " + Long.toUnsignedString(n) + " elements exceeds the " + remaining() + " remaining bytes");
        }
        return (int) n;
    }

    byte[] bytes() {
        int n = count();
        byte[] b = Arrays.copyOfRange(data, off, off + n);
        off += n;
        return b;
    }

    /** Skips a length-prefixed byte string and returns its start offset; the length is lastLength. */
    int skipBytes() {
        int n = count();
        int start = off;
        off += n;
        lastLength = n;
        return start;
    }

    int lastLength;

    private int index(long v, int bound, String what) {
        if (Long.compareUnsigned(v, bound) >= 0) {
            throw fail(what + " index " + Long.toUnsignedString(v) + " out of range [0," + bound + ")");
        }
        return (int) v;
    }

    int str() {
        return index(uvarint(), nstrings, "string");
    }

    int typeRef() {
        return index(uvarint(), counts[REF_TYPE] + 1, "type");
    }

    int ref(int kind) {
        return index(uvarint(), counts[kind], REF_NAMES[kind]);
    }

    int optRef(int kind) {
        return index(uvarint(), counts[kind] + 1, "optional " + REF_NAMES[kind]) - 1;
    }

    private int scopeSlot(int k) {
        if (nscopes == 0) {
            throw fail("local reference outside a function body");
        }
        return scopes[3 * (nscopes - 1) + k];
    }

    void pushScope() {
        if (3 * (nscopes + 1) > scopes.length) {
            scopes = Arrays.copyOf(scopes, scopes.length * 2);
        }
        Arrays.fill(scopes, 3 * nscopes, 3 * nscopes + 3, 0);
        nscopes++;
    }

    void popScope() {
        nscopes--;
    }

    void scopeSetPositions(int n) {
        scopes[3 * (nscopes - 1)] = n;
    }

    void scopeSetLocals(int n) {
        scopes[3 * (nscopes - 1) + 1] = n;
    }

    void scopeSetLabels(int n) {
        scopes[3 * (nscopes - 1) + 2] = n;
    }

    int posRef() {
        return index(uvarint(), scopeSlot(0) + 1, "position");
    }

    int localRef() {
        return index(uvarint(), scopeSlot(1), "local");
    }

    int optLocalRef() {
        return index(uvarint(), scopeSlot(1) + 1, "optional local") - 1;
    }

    int labelRef() {
        return index(uvarint(), scopeSlot(2), "label");
    }

    int optLabelRef() {
        return index(uvarint(), scopeSlot(2) + 1, "optional label") - 1;
    }

    void enter() {
        if (++depth > MAX_DEPTH) {
            throw fail("nesting deeper than " + MAX_DEPTH);
        }
    }

    void leave() {
        depth--;
    }
}
