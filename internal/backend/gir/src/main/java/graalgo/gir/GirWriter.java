package graalgo.gir;

import java.util.Arrays;

/** Encodes GIR primitives (minimal LEB128 varints, as the Go encoder writes them). */
final class GirWriter {
    private byte[] buf = new byte[256];
    private int len;

    private void ensure(int n) {
        if (len + n > buf.length) {
            buf = Arrays.copyOf(buf, Math.max(buf.length * 2, len + n));
        }
    }

    void raw(byte[] b, int off, int n) {
        ensure(n);
        System.arraycopy(b, off, buf, len, n);
        len += n;
    }

    void uvarint(long v) {
        ensure(10);
        while (Long.compareUnsigned(v, 0x80) >= 0) {
            buf[len++] = (byte) (v | 0x80);
            v >>>= 7;
        }
        buf[len++] = (byte) v;
    }

    void varint(long v) {
        uvarint((v << 1) ^ (v >> 63));
    }

    void bool(boolean v) {
        ensure(1);
        buf[len++] = (byte) (v ? 1 : 0);
    }

    void bytes(byte[] b) {
        uvarint(b.length);
        raw(b, 0, b.length);
    }

    void optRef(int v) {
        if (v < -1) {
            throw new IllegalArgumentException("optional reference " + v);
        }
        uvarint(v + 1L);
    }

    int size() {
        return len;
    }

    byte[] toByteArray() {
        return Arrays.copyOf(buf, len);
    }
}
