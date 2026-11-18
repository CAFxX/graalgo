package abispike;

import abispike.Mem.Addr;
import abispike.Mem.Blob;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HexFormat;
import java.util.IdentityHashMap;
import java.util.List;
import java.util.Map;

/**
 * Synthesizes, for every type of a {@link TypeTab}, a descriptor with gc's
 * exact {@code internal/abi} memory layout: the kind-specific struct, the
 * trailing {@code UncommonType}, parameter pointers and method array, plus
 * the separately allocated names, field arrays and interface method arrays.
 *
 * <p>Offsets come from the table's layout records (go/types' view of
 * internal/abi), so nothing here is tied to one Go release. NameOff/TypeOff
 * values are IDs in a process-wide table with gc's "reflectOffs" numbering
 * (negative, from -1): the same scheme the gc runtime uses for descriptors
 * that live outside a module's type section.
 */
public final class Images {
    /** Marker stored in the Equal word of comparable types (stands in for the equality intrinsic). */
    public static final Object EQUAL = new Object() {
        @Override
        public String toString() {
            return "<equal>";
        }
    };

    static final int TFLAG_UNCOMMON = 1;
    static final int TFLAG_EXTRA_STAR = 2;
    static final int TFLAG_NAMED = 4;
    static final int TFLAG_REGULAR_MEMORY = 8;
    static final int TFLAG_DIRECT_IFACE = 32;

    static final int K_ARRAY = 17, K_CHAN = 18, K_FUNC = 19, K_INTERFACE = 20, K_MAP = 21, K_POINTER = 22, K_SLICE = 23,
            K_STRING = 24, K_STRUCT = 25;

    private record OffReloc(Blob blob, long off, Blob target) {}

    public final TypeTab tab;
    public final Addr[] desc;
    private final List<Blob> blobs = new ArrayList<>();
    private final List<Addr> offTable = new ArrayList<>();
    private final Map<Blob, Integer> offIds = new IdentityHashMap<>();
    private final Map<String, Blob> names = new HashMap<>();
    private final List<OffReloc> offRelocs = new ArrayList<>();
    /** Test hook: corrupt one field offset so the differential check must fail. */
    private final boolean mutate;

    public Images(TypeTab tab, boolean mutate) {
        this.tab = tab;
        this.mutate = mutate;
        this.desc = new Addr[tab.types.size()];
        for (TypeTab.Type t : tab.types) {
            desc[t.id] = new Addr(newBlob("type:" + t.str, mainSize(t)), 0);
        }
        for (TypeTab.Type t : tab.types) {
            fill(t);
        }
    }

    /** The {@code resolveNameOff}/{@code resolveTypeOff} intrinsic: ID → pointer. 0 is nil. */
    public Addr resolveOff(int id) {
        if (id == 0) {
            return null;
        }
        if (id > 0 || -id > offTable.size()) {
            throw new Mem.UnsafeTrap("unknown name/type offset " + id);
        }
        return offTable.get(-id - 1);
    }

    private Blob newBlob(String label, int size) {
        Blob b = new Blob(label, size);
        blobs.add(b);
        return b;
    }

    private int offId(Blob target) {
        Integer id = offIds.get(target);
        if (id == null) {
            offTable.add(new Addr(target, 0));
            id = -offTable.size();
            offIds.put(target, id);
        }
        return id;
    }

    private void putOff(Blob b, long off, Blob target) {
        if (target == null) {
            return;
        }
        b.storeBits(off, 4, offId(target));
        offRelocs.add(new OffReloc(b, off, target));
    }

    String kindStruct(int kind) {
        return switch (kind) {
            case K_ARRAY -> "ArrayType";
            case K_CHAN -> "ChanType";
            case K_FUNC -> "FuncType";
            case K_INTERFACE -> "InterfaceType";
            case K_MAP -> "MapType";
            case K_POINTER -> "PtrType";
            case K_SLICE -> "SliceType";
            case K_STRUCT -> "StructType";
            default -> "Type";
        };
    }

    private static boolean uncommon(TypeTab.Type t) {
        return t.named || !t.methods.isEmpty();
    }

    private int mainSize(TypeTab.Type t) {
        int n = tab.sizeof(kindStruct(t.kind));
        if (uncommon(t)) {
            n += tab.sizeof("UncommonType");
        }
        if (t.kind == K_FUNC) {
            n += 8 * (t.in.length + t.out.length);
        }
        return n + tab.sizeof("Method") * t.methods.size();
    }

    /** gc's reflect name encoding: flag byte, varint length, bytes, optional varint+tag. */
    private Blob name(String name, String tag, boolean exported, boolean embedded) {
        String key = name + "\u0000" + tag + "\u0000" + exported + embedded;
        Blob cached = names.get(key);
        if (cached != null) {
            return cached;
        }
        byte[] n = name.getBytes(StandardCharsets.UTF_8);
        byte[] g = tag.getBytes(StandardCharsets.UTF_8);
        byte[] buf = new byte[1 + 10 + n.length + 10 + g.length];
        int p = 0;
        buf[p++] = (byte) ((exported ? 1 : 0) | (g.length > 0 ? 2 : 0) | (embedded ? 8 : 0));
        p = varint(buf, p, n.length);
        System.arraycopy(n, 0, buf, p, n.length);
        p += n.length;
        if (g.length > 0) {
            p = varint(buf, p, g.length);
            System.arraycopy(g, 0, buf, p, g.length);
            p += g.length;
        }
        Blob b = newBlob("name:" + name, p);
        System.arraycopy(buf, 0, b.data, 0, p);
        names.put(key, b);
        return b;
    }

    private static int varint(byte[] buf, int p, int n) {
        while (true) {
            int b = n & 0x7f;
            n >>>= 7;
            if (n == 0) {
                buf[p++] = (byte) b;
                return p;
            }
            buf[p++] = (byte) (b | 0x80);
        }
    }

    private static boolean isExported(String name) {
        return !name.isEmpty() && Character.isUpperCase(name.codePointAt(0));
    }

    /** cmd/compile's TypeHash: SHA-256 of the link string with the first byte flipped, low 4 bytes LE. */
    static int typeHash(String link) {
        try {
            byte[] h = MessageDigest.getInstance("SHA-256").digest(link.getBytes(StandardCharsets.UTF_8));
            h[0] ^= (byte) 0xff;
            return (h[0] & 0xff) | (h[1] & 0xff) << 8 | (h[2] & 0xff) << 16 | (h[3] & 0xff) << 24;
        } catch (java.security.NoSuchAlgorithmException e) {
            throw new IllegalStateException(e);
        }
    }

    private void fill(TypeTab.Type t) {
        Blob b = (Blob) desc[t.id].base();
        boolean unc = uncommon(t);
        boolean star = !t.str.startsWith("*");
        int tflag = (unc ? TFLAG_UNCOMMON : 0) | (star ? TFLAG_EXTRA_STAR : 0) | (t.named ? TFLAG_NAMED : 0)
                | (t.regular ? TFLAG_REGULAR_MEMORY : 0) | (t.direct ? TFLAG_DIRECT_IFACE : 0);
        b.storeBits(tab.off("Type.Size_"), 8, t.size);
        b.storeBits(tab.off("Type.PtrBytes"), 8, t.ptrBytes);
        b.storeBits(tab.off("Type.Hash"), 4, typeHash(t.link));
        b.storeBits(tab.off("Type.TFlag"), 1, tflag);
        b.storeBits(tab.off("Type.Align_"), 1, t.align);
        b.storeBits(tab.off("Type.FieldAlign_"), 1, t.fieldAlign);
        b.storeBits(tab.off("Type.Kind_"), 1, t.kind);
        if (t.comparable) {
            b.storeRef(tab.off("Type.Equal"), EQUAL);
        }
        Blob mask = newBlob("gcmask:" + t.str, t.gcMask.length);
        System.arraycopy(t.gcMask, 0, mask.data, 0, t.gcMask.length);
        b.storeRef(tab.off("Type.GCData"), new Addr(mask, 0));
        boolean strExported = star ? (t.named && isExported(t.name))
                : (t.elem >= 0 && tab.types.get(t.elem).named && isExported(tab.types.get(t.elem).name));
        putOff(b, tab.off("Type.Str"), name(star ? "*" + t.str : t.str, "", strExported, false));
        if (t.ptrTo >= 0) {
            putOff(b, tab.off("Type.PtrToThis"), (Blob) desc[t.ptrTo].base());
        }
        String ks = kindStruct(t.kind);
        switch (t.kind) {
            case K_POINTER, K_SLICE -> b.storeRef(tab.off(ks + ".Elem"), desc[t.elem]);
            case K_ARRAY -> {
                b.storeRef(tab.off("ArrayType.Elem"), desc[t.elem]);
                b.storeRef(tab.off("ArrayType.Slice"), desc[t.slice]);
                b.storeBits(tab.off("ArrayType.Len"), 8, t.len);
            }
            case K_CHAN -> {
                b.storeRef(tab.off("ChanType.Elem"), desc[t.elem]);
                b.storeBits(tab.off("ChanType.Dir"), 8, t.dir);
            }
            case K_MAP -> {
                b.storeRef(tab.off("MapType.Key"), desc[t.key]);
                b.storeRef(tab.off("MapType.Elem"), desc[t.elem]);
            }
            case K_FUNC -> {
                b.storeBits(tab.off("FuncType.InCount"), 2, t.in.length);
                b.storeBits(tab.off("FuncType.OutCount"), 2, t.out.length | (t.variadic ? 1 << 15 : 0));
                long p = tab.sizeof("FuncType") + (unc ? tab.sizeof("UncommonType") : 0);
                for (int id : t.in) {
                    b.storeRef(p, desc[id]);
                    p += 8;
                }
                for (int id : t.out) {
                    b.storeRef(p, desc[id]);
                    p += 8;
                }
            }
            case K_INTERFACE -> {
                if (!t.ifacePkg.isEmpty()) {
                    b.storeRef(tab.off("InterfaceType.PkgPath"), new Addr(name(t.ifacePkg, "", false, false), 0));
                }
                int isz = tab.sizeof("Imethod");
                Blob ms = newBlob("imethods:" + t.str, isz * t.imethods.size());
                for (int i = 0; i < t.imethods.size(); i++) {
                    TypeTab.Method m = t.imethods.get(i);
                    putOff(ms, (long) i * isz + tab.off("Imethod.Name"), name(m.name, "", m.exported, false));
                    putOff(ms, (long) i * isz + tab.off("Imethod.Typ"), (Blob) desc[m.type].base());
                }
                sliceHeader(b, tab.off("InterfaceType.Methods"), ms, t.imethods.size());
            }
            case K_STRUCT -> {
                if (!t.structPkg.isEmpty()) {
                    b.storeRef(tab.off("StructType.PkgPath"), new Addr(name(t.structPkg, "", false, false), 0));
                }
                int fsz = tab.sizeof("StructField");
                Blob fs = newBlob("fields:" + t.str, fsz * t.fields.size());
                for (int i = 0; i < t.fields.size(); i++) {
                    TypeTab.Field f = t.fields.get(i);
                    long base = (long) i * fsz;
                    fs.storeRef(base + tab.off("StructField.Name"), new Addr(name(f.name, f.tag, f.exported, f.embedded), 0));
                    fs.storeRef(base + tab.off("StructField.Typ"), desc[f.type]);
                    long off = f.offset;
                    if (mutate && t.fields.size() > 1 && i == 1) {
                        off += 8;
                    }
                    fs.storeBits(base + tab.off("StructField.Offset"), 8, off);
                }
                sliceHeader(b, tab.off("StructType.Fields"), fs, t.fields.size());
            }
            default -> { }
        }
        if (unc) {
            long u = tab.sizeof(ks);
            long moff = tab.sizeof("UncommonType") + (t.kind == K_FUNC ? 8L * (t.in.length + t.out.length) : 0);
            if (!t.uncommonPkg.isEmpty()) {
                putOff(b, u + tab.off("UncommonType.PkgPath"), name(t.uncommonPkg, "", false, false));
            }
            int exported = 0;
            for (TypeTab.Method m : t.methods) {
                exported += m.exported ? 1 : 0;
            }
            b.storeBits(u + tab.off("UncommonType.Mcount"), 2, t.methods.size());
            b.storeBits(u + tab.off("UncommonType.Xcount"), 2, exported);
            b.storeBits(u + tab.off("UncommonType.Moff"), 4, moff);
            int msz = tab.sizeof("Method");
            for (int i = 0; i < t.methods.size(); i++) {
                TypeTab.Method m = t.methods.get(i);
                long base = u + moff + (long) i * msz;
                putOff(b, base + tab.off("Method.Name"), name(m.name, "", m.exported, false));
                putOff(b, base + tab.off("Method.Mtyp"), (Blob) desc[m.type].base());
                // No code pointers in this prototype: -1 is gc's "unreachable method" sentinel.
                b.storeBits(base + tab.off("Method.Ifn"), 4, -1);
                b.storeBits(base + tab.off("Method.Tfn"), 4, -1);
            }
        }
    }

    private static void sliceHeader(Blob b, long off, Blob array, int len) {
        if (len > 0) {
            b.storeRef(off, new Addr(array, 0));
        }
        b.storeBits(off + 8, 8, len);
        b.storeBits(off + 16, 8, len);
    }

    /** Relocatable dump for imgcheck, which materializes it as real memory under gc. */
    public String dump() {
        Map<Blob, Integer> ids = new IdentityHashMap<>();
        for (Blob b : blobs) {
            ids.put(b, ids.size());
        }
        StringBuilder sb = new StringBuilder();
        for (Blob b : blobs) {
            sb.append("blob id=").append(ids.get(b)).append(" size=").append(b.data.length).append(" data=x")
                    .append(HexFormat.of().formatHex(b.data)).append('\n');
        }
        for (Blob b : blobs) {
            for (int w = 0; w < b.refs.length; w++) {
                Object r = b.refs[w];
                if (r == EQUAL) {
                    sb.append("equal blob=").append(ids.get(b)).append(" off=").append(w * 8).append('\n');
                } else if (r instanceof Addr a) {
                    sb.append("ptr blob=").append(ids.get(b)).append(" off=").append(w * 8).append(" tblob=")
                            .append(ids.get((Blob) a.base())).append(" toff=").append(a.off()).append('\n');
                } else if (r != null) {
                    throw new IllegalStateException("undumpable reference " + r);
                }
            }
        }
        for (OffReloc r : offRelocs) {
            sb.append("offreloc blob=").append(ids.get(r.blob())).append(" off=").append(r.off()).append(" tblob=")
                    .append(ids.get(r.target())).append('\n');
        }
        for (int i = 0; i < tab.roots.size(); i++) {
            sb.append("root index=").append(i).append(" blob=").append(ids.get((Blob) desc[tab.roots.get(i)].base())).append('\n');
        }
        return sb.toString();
    }
}
