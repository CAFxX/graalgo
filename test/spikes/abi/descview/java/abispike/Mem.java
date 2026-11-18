package abispike;

/**
 * The pointer model of the prototype: SPEC §3.4's handle (object identity +
 * byte offset), with the addressed object answering loads and stores in
 * terms of gc byte offsets. A Go {@code unsafe.Pointer}, and any typed
 * pointer obtained from one by conversion, is an {@link Addr}.
 */
public final class Mem {
    private Mem() {}

    /** A Java object that presents (part of) itself as gc-layout memory. */
    public interface Addressable {
        /** Little-endian scalar of {@code size} bytes at {@code off}; traps outside scalar storage. */
        long loadBits(long off, int size);

        void storeBits(long off, int size, long bits);

        /** The pointer-shaped word at {@code off}: an {@link Addr}, another reference value, or null. */
        Object loadRef(long off);

        void storeRef(long off, Object ref);
    }

    /** Arithmetic or an access left the addressed object: SPEC GG-0016 says trap, never silently. */
    public static final class UnsafeTrap extends RuntimeException {
        private static final long serialVersionUID = 1L;

        public UnsafeTrap(String msg) {
            super(msg);
        }
    }

    public record Addr(Addressable base, long off) {
        /** {@code unsafe.Add(p, n)} / {@code unsafe.Pointer(uintptr(p) + n)}. */
        public Addr add(long n) {
            return new Addr(base, off + n);
        }

        public long u8(long field) {
            return base.loadBits(off + field, 1) & 0xff;
        }

        public long u16(long field) {
            return base.loadBits(off + field, 2) & 0xffff;
        }

        public long u32(long field) {
            return base.loadBits(off + field, 4) & 0xffffffffL;
        }

        public int i32(long field) {
            return (int) base.loadBits(off + field, 4);
        }

        public long u64(long field) {
            return base.loadBits(off + field, 8);
        }

        public Addr ptr(long field) {
            return (Addr) base.loadRef(off + field);
        }

        public Object ref(long field) {
            return base.loadRef(off + field);
        }
    }

    /**
     * Byte-addressed storage with a parallel reference array for the
     * pointer-shaped words: the representation of a synthesized descriptor
     * (and of any other byte-addressed allocation, e.g. a name or a []byte).
     */
    public static final class Blob implements Addressable {
        public final String label;
        public final byte[] data;
        public final Object[] refs;

        public Blob(String label, int size) {
            this.label = label;
            this.data = new byte[size];
            this.refs = new Object[(size + 7) / 8];
        }

        private void check(long off, int size) {
            if (off < 0 || off + size > data.length) {
                throw new UnsafeTrap("access [" + off + "," + (off + size) + ") outside " + label + " (" + data.length + " bytes)");
            }
        }

        @Override
        public long loadBits(long off, int size) {
            check(off, size);
            long v = 0;
            for (int i = size - 1; i >= 0; i--) {
                v = (v << 8) | (data[(int) off + i] & 0xff);
            }
            return v;
        }

        @Override
        public void storeBits(long off, int size, long bits) {
            check(off, size);
            for (int i = 0; i < size; i++) {
                data[(int) off + i] = (byte) (bits >>> (8 * i));
            }
        }

        @Override
        public Object loadRef(long off) {
            check(off, 8);
            if (off % 8 != 0) {
                throw new UnsafeTrap("unaligned pointer load at " + off + " in " + label);
            }
            return refs[(int) (off / 8)];
        }

        @Override
        public void storeRef(long off, Object ref) {
            check(off, 8);
            if (off % 8 != 0) {
                throw new UnsafeTrap("unaligned pointer store at " + off + " in " + label);
            }
            refs[(int) (off / 8)] = ref;
        }
    }
}
