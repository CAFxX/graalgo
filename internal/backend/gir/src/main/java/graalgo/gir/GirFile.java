package graalgo.gir;

import java.nio.charset.StandardCharsets;
import java.util.Arrays;

/**
 * One GIR file (docs/arch/toolexec-design.md §3.2): header, string table, the typed sections,
 * and the function bodies, each decoded lazily on first use. {@link #decode} checks the whole
 * container (header, section directory, table sizes, every reference in the typed sections and
 * the body layout); {@link #body} checks one body; {@link #validate} checks all bodies.
 */
public final class GirFile {
    private static final byte[] MAGIC = {'G', 'I', 'R', '1'};

    private final byte[] data;
    private final byte[] goVersion;
    private final byte[] goos;
    private final byte[] goarch;
    private final int[] stringOffsets;
    private final int[] stringLengths;
    private final String[] stringCache;
    private final Gir.Sections sections;
    private final int[] counts;
    private final int bodiesOff;
    private final int bodiesLen;
    private final int dataOff;
    private final int dataLen;
    private final Gir.FuncBody[] bodies;

    private GirFile(byte[] data) {
        this.data = data;
        GirReader r = new GirReader(data, 0, data.length, 0, new int[4]);
        if (data.length < MAGIC.length || !Arrays.equals(data, 0, MAGIC.length, MAGIC, 0, MAGIC.length)) {
            throw r.fail("not a GIR file (bad magic)");
        }
        r = new GirReader(data, MAGIC.length, data.length - MAGIC.length, 0, new int[4]);
        long version = r.uvarint();
        if (version != Gir.SCHEMA_VERSION) {
            throw r.fail("GIR version " + Long.toUnsignedString(version) + ", this decoder reads version " + Gir.SCHEMA_VERSION);
        }
        int h = r.offset();
        if (data.length - h < Gir.SCHEMA_HASH.length
                        || !Arrays.equals(data, h, h + Gir.SCHEMA_HASH.length, Gir.SCHEMA_HASH, 0, Gir.SCHEMA_HASH.length)) {
            throw r.fail("GIR schema hash mismatch (encoder and decoder built from different schema.txt)");
        }
        r = new GirReader(data, h + Gir.SCHEMA_HASH.length, data.length - h - Gir.SCHEMA_HASH.length, 0, new int[4]);
        goVersion = r.bytes();
        goos = r.bytes();
        goarch = r.bytes();
        int n = r.count();
        int[] ids = GirCodec.SECTION_IDS;
        if (n != ids.length) {
            throw r.fail(n + " sections, want " + ids.length);
        }
        long[] lengths = new long[n];
        for (int i = 0; i < n; i++) {
            long id = r.uvarint();
            if (id != ids[i]) {
                throw r.fail("section " + i + " is id " + Long.toUnsignedString(id) + ", want " + ids[i] + " (" + GirCodec.sectionName(ids[i]) + ")");
            }
            lengths[i] = r.uvarint();
        }
        int[] offs = new int[n];
        int[] lens = new int[n];
        long off = r.offset();
        for (int i = 0; i < n; i++) {
            if (Long.compareUnsigned(lengths[i], data.length - off) > 0) {
                throw new GirFormatException(off, "section " + GirCodec.sectionName(ids[i]) + " exceeds the input");
            }
            offs[i] = (int) off;
            lens[i] = (int) lengths[i];
            off += lengths[i];
        }
        if (off != data.length) {
            throw new GirFormatException(off, (data.length - off) + " trailing bytes after the last section");
        }
        counts = new int[4];
        for (int k = 0; k < 4; k++) {
            int i = indexOf(ids, GirCodec.REF_SECTIONS[k]);
            counts[k] = new GirReader(data, offs[i], lens[i], 0, counts).count();
        }
        int si = indexOf(ids, GirCodec.SEC_STRINGS);
        GirReader sr = new GirReader(data, offs[si], lens[si], 0, counts);
        int ns = sr.count();
        stringOffsets = new int[ns];
        stringLengths = new int[ns];
        for (int i = 0; i < ns; i++) {
            stringOffsets[i] = sr.skipBytes();
            stringLengths[i] = sr.lastLength;
        }
        if (sr.remaining() != 0) {
            throw sr.fail(sr.remaining() + " trailing bytes in STRINGS");
        }
        if (ns == 0 || stringLengths[0] != 0) {
            throw sr.fail("string 0 must be the empty string");
        }
        stringCache = new String[ns];
        sections = new Gir.Sections();
        int bo = 0;
        int bl = 0;
        int dof = 0;
        int dl = 0;
        for (int i = 0; i < n; i++) {
            int id = ids[i];
            if (id == GirCodec.SEC_STRINGS) {
                continue;
            } else if (id == GirCodec.SEC_BODIES) {
                bo = offs[i];
                bl = lens[i];
                continue;
            } else if (id == GirCodec.SEC_DATA) {
                dof = offs[i];
                dl = lens[i];
                continue;
            }
            GirReader tr = new GirReader(data, offs[i], lens[i], ns, counts);
            GirCodec.readTypedSection(tr, sections, id);
            if (tr.remaining() != 0) {
                throw tr.fail(tr.remaining() + " trailing bytes in section " + GirCodec.sectionName(id));
            }
        }
        bodiesOff = bo;
        bodiesLen = bl;
        dataOff = dof;
        dataLen = dl;
        long next = 0;
        Gir.Func[] funcs = sections.funcs;
        for (int i = 0; i < funcs.length; i++) {
            Gir.Func f = funcs[i];
            if (f.size() == 0) {
                if (f.body() != 0) {
                    throw new GirFormatException(bodiesOff, "func " + i + ": empty body at offset " + Long.toUnsignedString(f.body()) + ", want 0");
                }
                continue;
            }
            if (f.body() != next) {
                throw new GirFormatException(bodiesOff, "func " + i + ": body at " + Long.toUnsignedString(f.body()) + ", want " + next);
            }
            if (Long.compareUnsigned(f.size(), bodiesLen - next) > 0) {
                throw new GirFormatException(bodiesOff, "func " + i + ": body exceeds BODIES");
            }
            next += f.size();
        }
        if (next != bodiesLen) {
            throw new GirFormatException(bodiesOff, "BODIES has " + bodiesLen + " bytes, FUNCINDEX covers " + next);
        }
        bodies = new Gir.FuncBody[funcs.length];
    }

    private static int indexOf(int[] a, int v) {
        for (int i = 0; i < a.length; i++) {
            if (a[i] == v) {
                return i;
            }
        }
        throw new IllegalStateException("section " + v);
    }

    /** Decodes the container; bodies are decoded on demand. */
    public static GirFile decode(byte[] data) {
        return new GirFile(data);
    }

    public String goVersion() {
        return new String(goVersion, StandardCharsets.UTF_8);
    }

    public String goos() {
        return new String(goos, StandardCharsets.UTF_8);
    }

    public String goarch() {
        return new String(goarch, StandardCharsets.UTF_8);
    }

    public Gir.Sections sections() {
        return sections;
    }

    public int stringCount() {
        return stringOffsets.length;
    }

    /** The raw bytes of string i (Go strings are byte sequences, not necessarily UTF-8). */
    public byte[] stringBytes(int i) {
        return Arrays.copyOfRange(data, stringOffsets[i], stringOffsets[i] + stringLengths[i]);
    }

    /** String i decoded as UTF-8 (identifiers, file names); cached. */
    public String string(int i) {
        String s = stringCache[i];
        if (s == null) {
            s = new String(data, stringOffsets[i], stringLengths[i], StandardCharsets.UTF_8);
            stringCache[i] = s;
        }
        return s;
    }

    public byte[] data() {
        return Arrays.copyOfRange(data, dataOff, dataOff + dataLen);
    }

    /**
     * Bytes {@code [off, off+len)} of the DATA section — a copy of just that range (a Data
     * expression's embedded file), never of the whole section; null when the range is outside
     * it (the decoders do not cross-check Data ranges, schema.txt).
     */
    public byte[] dataRange(long off, long len) {
        if (off < 0 || len < 0 || off > dataLen || len > dataLen - off) {
            return null;
        }
        return Arrays.copyOfRange(data, dataOff + (int) off, dataOff + (int) (off + len));
    }

    /** Decodes (once) the body of function i; null for a bodyless function. */
    public Gir.FuncBody body(int i) {
        Gir.FuncBody b = bodies[i];
        Gir.Func f = sections.funcs[i];
        if (b != null || f.size() == 0) {
            return b;
        }
        GirReader r = new GirReader(data, bodiesOff + (int) f.body(), (int) f.size(), stringOffsets.length, counts);
        b = GirCodec.readFuncBody(r);
        if (r.remaining() != 0) {
            throw r.fail("func " + i + ": " + r.remaining() + " trailing bytes in body");
        }
        bodies[i] = b;
        return b;
    }

    /** The raw blob of function i's body. */
    byte[] bodyBytes(int i) {
        Gir.Func f = sections.funcs[i];
        int o = bodiesOff + (int) f.body();
        return Arrays.copyOfRange(data, o, o + (int) f.size());
    }

    /** Decodes every body and checks the file-level invariants. */
    public void validate() {
        for (int i = 0; i < sections.funcs.length; i++) {
            body(i);
            boolean bodyless = (sections.funcs[i].flags() & 1) != 0;
            if (bodyless != (sections.funcs[i].size() == 0)) {
                throw new GirFormatException(bodiesOff, "func " + i + ": bodyless flag disagrees with body size");
            }
        }
    }

    /** Encodes one body exactly as the Go encoder does. */
    static byte[] encodeBody(Gir.FuncBody b) {
        GirWriter w = new GirWriter();
        GirCodec.writeFuncBody(w, b);
        return w.toByteArray();
    }

    /**
     * Re-encodes the file from its decoded form: header, the string table as decoded, every typed
     * section, and every body (decoding any not yet decoded). For a valid file this reproduces the
     * input byte for byte, which is what the cross-language round-trip tests check.
     */
    public byte[] encode() {
        validate();
        int[] ids = GirCodec.SECTION_IDS;
        byte[][] payload = new byte[ids.length][];
        for (int i = 0; i < ids.length; i++) {
            int id = ids[i];
            GirWriter w = new GirWriter();
            if (id == GirCodec.SEC_STRINGS) {
                w.uvarint(stringOffsets.length);
                for (int s = 0; s < stringOffsets.length; s++) {
                    w.uvarint(stringLengths[s]);
                    w.raw(data, stringOffsets[s], stringLengths[s]);
                }
            } else if (id == GirCodec.SEC_BODIES) {
                for (int f = 0; f < bodies.length; f++) {
                    if (bodies[f] != null) {
                        GirCodec.writeFuncBody(w, bodies[f]);
                    }
                }
            } else if (id == GirCodec.SEC_DATA) {
                w.raw(data, dataOff, dataLen);
            } else {
                GirCodec.writeTypedSection(w, sections, id);
            }
            payload[i] = w.toByteArray();
        }
        GirWriter out = new GirWriter();
        out.raw(MAGIC, 0, MAGIC.length);
        out.uvarint(Gir.SCHEMA_VERSION);
        out.raw(Gir.SCHEMA_HASH, 0, Gir.SCHEMA_HASH.length);
        out.bytes(goVersion);
        out.bytes(goos);
        out.bytes(goarch);
        out.uvarint(ids.length);
        for (int i = 0; i < ids.length; i++) {
            out.uvarint(ids[i]);
            out.uvarint(payload[i].length);
        }
        for (byte[] p : payload) {
            out.raw(p, 0, p.length);
        }
        return out.toByteArray();
    }
}
