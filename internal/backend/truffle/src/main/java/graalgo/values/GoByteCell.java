package graalgo.values;

/**
 * A {@link GoCell} addressing one byte of a Long-carried scalar field
 * (fix-redmaster-r59): the storage behind a sub-word pointer such as
 * {@code *(*ctrl)(unsafe.Add(unsafe.Pointer(g), k))} over a ctrlGroup
 * word, where the addressed byte has no field of its own.
 *
 * <p>{@code get} extracts byte {@code byteIndex} (little-endian) of the
 * wrapped cell's long; {@code set} merges a byte back into the long
 * (read-modify-write through the wrapped cell). The byte value is a
 * {@code Long} in canonical boxed form, so the existing cell-pointer
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
        if (!(v instanceof Number n)) {
            throw new IllegalStateException("GoByteCell: wrapped cell holds "
                    + (v == null ? "null" : v.getClass().getSimpleName()) + ", not a Long");
        }
        long word = n.longValue();
        return Long.valueOf((word >> (8 * byteIndex)) & 0xFF);
    }

    @Override
    public void set(Object value) {
        Object cur = cell.get();
        if (!(cur instanceof Number)) {
            throw new IllegalStateException("GoByteCell: wrapped cell holds "
                    + (cur == null ? "null" : cur.getClass().getSimpleName()) + ", not a Long");
        }
        if (!(value instanceof Number n)) {
            throw new IllegalStateException("GoByteCell: cannot store "
                    + (value == null ? "null" : value.getClass().getSimpleName()) + " as a byte");
        }
        long word = ((Number) cur).longValue();
        long b = n.longValue() & 0xFF;
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
