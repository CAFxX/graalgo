package graalgo.values;

/**
 * A {@link GoCell} addressing one byte of a scalar field's boxed value
 * (fix-redmaster-r59): the storage behind a sub-word pointer such as
 * {@code *(*ctrl)(unsafe.Add(unsafe.Pointer(g), k))} over a ctrlGroup
 * word, where the addressed byte has no field of its own, or
 * {@code *(*byte)(unsafe.Add(unsafe.Pointer(&x), k))} over a sub-word
 * integer variable (fix-redmaster-r69, C1/F1).
 *
 * <p>The cell addresses byte {@code byteIndex} (little-endian) of the
 * wrapped cell's boxed value, whose storage width is {@code widthBytes}:
 * 8 for a {@link Long}, 4 for an {@link Integer}, 2 for a {@link Short},
 * 1 for a {@link Byte}. The two-argument constructor is the Long-only
 * form (width 8): the wrapped cell must hold a {@code Long}, anything
 * else fails loudly at first use, never silently — a {@code Double}
 * would truncate, and an {@code Integer}'s sign extension would make
 * bytes 4-7 read extension bytes. The three-argument constructor is
 * the width-aware form (r69): the wrapped value must be an integral
 * {@code Number} whose boxed width covers {@code widthBytes}, and
 * {@code byteIndex} must lie inside the width; reads and writes are
 * masked to the width, so no extension byte is ever observable, and
 * out-of-width offsets fail loudly instead of reading past the
 * storage. Writes store the merged value back in the wrapped value's
 * own boxed class (an {@code Integer} stays an {@code Integer}), so
 * the variable's box type — and, for array elements, the
 * {@code Boxing.arraySet} cast — is preserved, and reads/writes
 * through the byte pointer alias the original storage.
 *
 * <p>{@code get} returns the byte as a {@code Long} in canonical boxed
 * form, and {@code set} accepts any integral {@code Number} (a guest
 * uint8 arrives Integer-boxed) while rejecting floating-point values
 * loudly, so the existing cell-pointer machinery ({@link GoPointer},
 * DerefNode, DerefWriteNode) works unchanged — no DerefNode/
 * DerefWriteNode change is needed for sub-word reads or writes.
 *
 * <p>Known limit (r69, follow-up, same family as F2): the width is
 * the width of the BOXED value, and a Go value narrower than its box
 * (an int8/int16/uint8/uint16 local, boxed as {@code Integer}) has
 * box bytes above its Go width that are extension bytes, not the
 * variable's storage: reading them is deterministic but does not
 * match gc, which would read adjacent memory. Bytes within the Go
 * width — every case the C1 oracle pins — are exact.
 */
public final class GoByteCell implements GoCell {
    private final GoCell cell;
    private final int byteIndex;
    private final int widthBytes;

    /** Byte {@code byteIndex} (0-7, little-endian) of the wrapped cell's long. */
    public GoByteCell(GoCell cell, int byteIndex) {
        this(cell, byteIndex, 8);
    }

    /**
     * Byte {@code byteIndex} (little-endian) of the wrapped cell's boxed
     * value, whose storage width is {@code widthBytes} bytes (1-8).
     * The wrapped value must be an integral {@link Number} whose boxed
     * width covers {@code widthBytes} (see {@link #boxedWidth}); the
     * byte index must lie inside the width.
     */
    public GoByteCell(GoCell cell, int byteIndex, int widthBytes) {
        if (cell == null) {
            throw new IllegalArgumentException("GoByteCell: null cell");
        }
        if (widthBytes < 1 || widthBytes > 8) {
            throw new IllegalArgumentException("GoByteCell: width out of range: " + widthBytes);
        }
        if (byteIndex < 0 || byteIndex >= widthBytes) {
            throw new IllegalArgumentException("GoByteCell: byte index out of range: " + byteIndex
                    + " for width " + widthBytes);
        }
        this.cell = cell;
        this.byteIndex = byteIndex;
        this.widthBytes = widthBytes;
    }

    /** The wrapped cell. */
    public GoCell cell() {
        return cell;
    }

    /** The byte index within the wrapped cell's boxed value. */
    public int byteIndex() {
        return byteIndex;
    }

    /** The storage width in bytes of the wrapped cell's boxed value. */
    public int widthBytes() {
        return widthBytes;
    }

    /**
     * Storage width in bytes of a canonically-boxed integral value:
     * 8 for {@link Long}, 4 for {@link Integer}, 2 for {@link Short},
     * 1 for {@link Byte}; -1 for anything else (floating-point, boolean,
     * String, null, ...). The width is the width of the BOXED value:
     * a sub-64-bit Go int boxes as {@code Integer} (width 4) whatever
     * its Go width.
     */
    public static int boxedWidth(Object v) {
        if (v instanceof Long) {
            return 8;
        }
        if (v instanceof Integer) {
            return 4;
        }
        if (v instanceof Short) {
            return 2;
        }
        if (v instanceof Byte) {
            return 1;
        }
        return -1;
    }

    /** Bit mask keeping exactly {@code widthBytes} low bytes. */
    private static long maskFor(int widthBytes) {
        return widthBytes == 8 ? -1L : (1L << (8 * widthBytes)) - 1;
    }

    /** Re-boxes {@code merged} (already masked to the width) in the class of {@code stored}. */
    private static Object boxFor(Object stored, long merged) {
        if (stored instanceof Long) {
            return Long.valueOf(merged);
        }
        if (stored instanceof Integer) {
            return Integer.valueOf((int) merged);
        }
        if (stored instanceof Short) {
            return Short.valueOf((short) merged);
        }
        return Byte.valueOf((byte) merged);
    }

    /** The wrapped value, validated as an integral Number covering this cell's width. */
    private Object checkedStorage() {
        Object v = cell.get();
        if (boxedWidth(v) < widthBytes) {
            throw new IllegalStateException("GoByteCell: wrapped cell holds "
                    + (v == null ? "null" : v.getClass().getSimpleName())
                    + ", not an integral value of width " + widthBytes);
        }
        return v;
    }

    @Override
    public Object get() {
        Object v = checkedStorage();
        long word = ((Number) v).longValue() & maskFor(widthBytes);
        return Long.valueOf((word >> (8 * byteIndex)) & 0xFF);
    }

    @Override
    public void set(Object value) {
        Object cur = checkedStorage();
        // The byte being stored is an integral Number: a guest uint8
        // arrives Integer-boxed (Boxing.java), so Long-only here would
        // break every real byte write. Double/Float would truncate
        // silently, so they fail loudly instead.
        if (!(value instanceof Long || value instanceof Integer
                || value instanceof Byte || value instanceof Short)) {
            throw new IllegalStateException("GoByteCell: cannot store "
                    + (value == null ? "null" : value.getClass().getSimpleName()) + " as a byte");
        }
        long word = ((Number) cur).longValue() & maskFor(widthBytes);
        long b = ((Number) value).longValue() & 0xFF;
        long merged = (word & ~(0xFFL << (8 * byteIndex))) | (b << (8 * byteIndex));
        cell.set(boxFor(cur, merged & maskFor(widthBytes)));
    }

    @Override
    public boolean sameStorageAs(GoCell other) {
        return other instanceof GoByteCell b
                && cell.sameStorageAs(b.cell) && byteIndex == b.byteIndex;
    }

    @Override
    public int storageHash() {
        return 31 * cell.storageHash() + byteIndex;
    }

    @Override
    public Object storageKey() {
        return new GoCell.StorageKey(cell.storageKey(), byteIndex);
    }

    @Override
    public String toString() {
        return "GoByteCell{byte " + byteIndex + " of " + cell + "}";
    }
}
