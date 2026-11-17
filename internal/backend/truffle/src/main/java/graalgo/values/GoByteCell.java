package graalgo.values;

/**
 * A {@link GoCell} addressing one byte of a Long-carried scalar field
 * (fix-redmaster-r59): the storage behind a sub-word pointer such as
 * {@code *(*ctrl)(unsafe.Add(unsafe.Pointer(g), k))} over a ctrlGroup
 * word, where the addressed byte has no field of its own.
 *
 * <p>{@code get} extracts byte {@code byteIndex} (little-endian) of the
 * wrapped cell's long; {@code set} merges a byte back into the long
 * (read-modify-write through the wrapped cell). {@code get} returns the
 * byte as a {@code Long} in canonical boxed form, and {@code set}
 * accepts any integral {@code Number} (a guest uint8 arrives
 * Integer-boxed) while rejecting floating-point values loudly, so the
 * existing cell-pointer
 * machinery ({@link GoPointer}, DerefNode, DerefWriteNode) works
 * unchanged — no DerefNode/DerefWriteNode change is needed for
 * sub-word reads or writes. The wrapped cell must hold a {@code Long}
 * (a longs-carried scalar); anything else fails loudly at first use,
 * never silently.
 */
public final class GoByteCell implements GoCell {
    private final GoCell cell;
    private final int byteIndex;

    /** Byte {@code byteIndex} (0-7, little-endian) of the wrapped cell's long. */
    public GoByteCell(GoCell cell, int byteIndex) {
        if (cell == null) {
            throw new IllegalArgumentException("GoByteCell: null cell");
        }
        if (byteIndex < 0 || byteIndex > 7) {
            throw new IllegalArgumentException("GoByteCell: byte index out of range: " + byteIndex);
        }
        this.cell = cell;
        this.byteIndex = byteIndex;
    }

    /** The wrapped longs-carrying cell. */
    public GoCell cell() {
        return cell;
    }

    /** The byte index within the wrapped cell's long. */
    public int byteIndex() {
        return byteIndex;
    }

    @Override
    public Object get() {
        Object v = cell.get();
        // r64 (C1): the wrapped word MUST be a Long. Any other Number
        // fails loudly: Double.longValue() truncates (1.5 -> 1) and
        // Integer.longValue() sign-extends, so bytes 4-7 would read
        // extension bytes — silently wrong instead of loudly wrong.
        if (!(v instanceof Long)) {
            throw new IllegalStateException("GoByteCell: wrapped cell holds "
                    + (v == null ? "null" : v.getClass().getSimpleName()) + ", not a Long");
        }
        long word = ((Long) v).longValue();
        return Long.valueOf((word >> (8 * byteIndex)) & 0xFF);
    }

    @Override
    public void set(Object value) {
        Object cur = cell.get();
        // r64 (C1): same Long-only storage invariant as get().
        if (!(cur instanceof Long)) {
            throw new IllegalStateException("GoByteCell: wrapped cell holds "
                    + (cur == null ? "null" : cur.getClass().getSimpleName()) + ", not a Long");
        }
        // The byte being stored is an integral Number: a guest uint8
        // arrives Integer-boxed (Boxing.java), so Long-only here would
        // break every real byte write. Double/Float would truncate
        // silently, so they fail loudly instead.
        if (!(value instanceof Long || value instanceof Integer
                || value instanceof Byte || value instanceof Short)) {
            throw new IllegalStateException("GoByteCell: cannot store "
                    + (value == null ? "null" : value.getClass().getSimpleName()) + " as a byte");
        }
        long word = ((Long) cur).longValue();
        long b = ((Number) value).longValue() & 0xFF;
        long merged = (word & ~(0xFFL << (8 * byteIndex))) | (b << (8 * byteIndex));
        cell.set(Long.valueOf(merged));
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
