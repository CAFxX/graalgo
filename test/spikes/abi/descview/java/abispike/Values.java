package abispike;

import abispike.Mem.Addr;
import abispike.Mem.Addressable;
import abispike.Mem.Blob;
import abispike.Mem.UnsafeTrap;
import java.util.ArrayList;
import java.util.List;

/**
 * Go values in the SPEC §3.4 shape — a flat {@code long[]} for scalars and
 * an {@code Object[]} for references per value — made addressable by gc
 * byte offset through a per-type leaf table derived from the type table's
 * layout. Nothing is stored as bytes: the offset is translated to a slot.
 */
public final class Values {
    private Values() {}

    enum LeafKind { SCALAR, REF, STRING, SLICE, IFACE }

    record Leaf(long off, int size, LeafKind kind, int slot) {}

    /** Per-type translation table: gc byte offset → slot. Built once per type. */
    public static final class Layout {
        final List<Leaf> leaves = new ArrayList<>();
        int prims;
        int refs;
        final long size;

        /** Byte offset → leaf, or null for padding: one array load per access. */
        private final Leaf[] byOffset;

        Layout(TypeTab tab, TypeTab.Type t) {
            size = t.size;
            add(tab, t, 0);
            byOffset = new Leaf[(int) size];
            for (Leaf l : leaves) {
                for (int i = 0; i < l.size; i++) {
                    byOffset[(int) l.off + i] = l;
                }
            }
        }

        private void add(TypeTab tab, TypeTab.Type t, long off) {
            switch (t.kind) {
                case Images.K_STRUCT -> {
                    for (TypeTab.Field f : t.fields) {
                        add(tab, tab.types.get(f.type), off + f.offset);
                    }
                }
                case Images.K_ARRAY -> {
                    TypeTab.Type e = tab.types.get(t.elem);
                    for (long i = 0; i < t.len; i++) {
                        add(tab, e, off + i * e.size);
                    }
                }
                case Images.K_STRING -> leaves.add(new Leaf(off, 16, LeafKind.STRING, refs++));
                case Images.K_SLICE -> leaves.add(new Leaf(off, 24, LeafKind.SLICE, refs++));
                case Images.K_INTERFACE -> leaves.add(new Leaf(off, 16, LeafKind.IFACE, refs++));
                case Images.K_CHAN, Images.K_FUNC, Images.K_MAP, Images.K_POINTER, 26 ->
                        leaves.add(new Leaf(off, 8, LeafKind.REF, refs++));
                case 15, 16 -> { // complex64, complex128: two floats
                    int half = (int) t.size / 2;
                    leaves.add(new Leaf(off, half, LeafKind.SCALAR, prims++));
                    leaves.add(new Leaf(off + half, half, LeafKind.SCALAR, prims++));
                }
                default -> leaves.add(new Leaf(off, (int) t.size, LeafKind.SCALAR, prims++));
            }
        }

        Leaf at(long off) {
            if (off >= 0 && off < size && byOffset[(int) off] != null) {
                return byOffset[(int) off];
            }
            throw new UnsafeTrap("offset " + off + " is outside the value or in padding (size " + size + ")");
        }
    }

    /** A Go string value: immutable bytes. */
    public record Str(Blob bytes) {
        public static Str of(String s) {
            byte[] b = s.getBytes(java.nio.charset.StandardCharsets.UTF_8);
            Blob blob = new Blob("string", b.length);
            System.arraycopy(b, 0, blob.data, 0, b.length);
            return new Str(blob);
        }
    }

    /** A Go slice value. */
    public record Slice(Addr data, long len, long cap) {}

    /** A Go interface value: dynamic type descriptor + the value (a pointer for direct types, else its box). */
    public record Iface(Addr type, Object data) {}

    /** A value of any type in flat form; also the box of a non-pointer value held by an interface. */
    public static final class Flat implements Addressable {
        final Layout layout;
        final long[] prims;
        final Object[] refs;

        public Flat(Layout layout) {
            this.layout = layout;
            this.prims = new long[layout.prims];
            this.refs = new Object[layout.refs];
        }

        private static long mask(int size) {
            return size == 8 ? -1L : (1L << (8 * size)) - 1;
        }

        @Override
        public long loadBits(long off, int size) {
            Leaf l = layout.at(off);
            switch (l.kind) {
                case SCALAR:
                    if (off == l.off && size == l.size) {
                        return prims[l.slot] & mask(size);
                    }
                    break;
                case STRING:
                    if (off == l.off + 8 && size == 8) { // unsafeheader.String.Len
                        Str s = (Str) refs[l.slot];
                        return s == null ? 0 : s.bytes().data.length;
                    }
                    break;
                case SLICE:
                    if (size == 8 && (off == l.off + 8 || off == l.off + 16)) { // unsafeheader.Slice.Len/Cap
                        Slice s = (Slice) refs[l.slot];
                        return s == null ? 0 : (off == l.off + 8 ? s.len() : s.cap());
                    }
                    break;
                default:
                    break;
            }
            throw new UnsafeTrap("scalar load of " + size + " bytes at " + off + " does not match the " + l.kind + " stored there");
        }

        @Override
        public void storeBits(long off, int size, long bits) {
            Leaf l = layout.at(off);
            if (l.kind != LeafKind.SCALAR || off != l.off || size != l.size) {
                throw new UnsafeTrap("scalar store of " + size + " bytes at " + off + " does not match the " + l.kind + " stored there");
            }
            prims[l.slot] = bits & mask(size);
        }

        @Override
        public Object loadRef(long off) {
            Leaf l = layout.at(off);
            if (l.kind == LeafKind.SCALAR) {
                throw new UnsafeTrap("pointer load at " + off + " from scalar storage");
            }
            Object v = refs[l.slot];
            switch (l.kind) {
                case REF:
                    return v;
                case STRING:
                    if (off == l.off) { // unsafeheader.String.Data
                        return v == null ? null : new Addr(((Str) v).bytes(), 0);
                    }
                    break;
                case SLICE:
                    if (off == l.off) { // unsafeheader.Slice.Data
                        return v == null ? null : ((Slice) v).data();
                    }
                    break;
                case IFACE: // abi.EmptyInterface{Type, Data}
                    if (v == null) {
                        return null;
                    }
                    return off == l.off ? ((Iface) v).type() : dataWord((Iface) v);
                default:
                    break;
            }
            throw new UnsafeTrap("pointer load at " + off + " does not match the " + l.kind + " stored there");
        }

        @Override
        public void storeRef(long off, Object ref) {
            Leaf l = layout.at(off);
            if (l.kind != LeafKind.REF) {
                throw new UnsafeTrap("pointer store into a " + l.kind + " header word is not implemented in the prototype");
            }
            refs[l.slot] = ref;
        }

        /** Whole-value store of a string (what compiled non-reflect code does for {@code x.f = "s"}). */
        public void setString(long off, String s) {
            refs[layout.at(off).slot] = Str.of(s);
        }
    }

    /** The data word of an interface as gc defines it: the pointer itself, or a pointer to the boxed value. */
    static Object dataWord(Iface i) {
        return i.data() instanceof Addressable box ? new Addr(box, 0) : i.data();
    }

    /** An addressable variable of interface type: what {@code &i} denotes in {@code unpackEface(i any)}. */
    public static final class IfaceCell implements Addressable {
        Iface value;

        public IfaceCell(Iface value) {
            this.value = value;
        }

        @Override
        public long loadBits(long off, int size) {
            throw new UnsafeTrap("scalar load from an interface header");
        }

        @Override
        public void storeBits(long off, int size, long bits) {
            throw new UnsafeTrap("scalar store into an interface header");
        }

        @Override
        public Object loadRef(long off) {
            if (value == null) {
                return null;
            }
            if (off == 0) {
                return value.type();
            }
            if (off == 8) {
                return dataWord(value);
            }
            throw new UnsafeTrap("interface header has two words; offset " + off);
        }

        @Override
        public void storeRef(long off, Object ref) {
            throw new UnsafeTrap("packEface (store into an interface header) is not implemented in the prototype");
        }
    }
}
