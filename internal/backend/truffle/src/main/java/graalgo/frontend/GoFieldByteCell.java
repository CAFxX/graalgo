package graalgo.frontend;

import graalgo.values.GoCell;
import graalgo.values.GoStruct;

/**
 * A {@link GoCell} addressing one byte of a Long-carried struct field
 * (fix-rv1107-10-mapaccess, E2): the live byte view behind
 * {@code *(*uint8)(unsafe.Add(unsafe.Pointer(&s.x), i))} and friends.
 * gc allows any struct field's bytes to be read/written through unsafe;
 * a byte strictly inside (or at the start of, for a 1-byte target) a
 * Long-carried scalar field is such a byte. {@code get} extracts byte
 * {@code byteIndex} (little-endian) of the field's Long; {@code set}
 * read-modify-writes that byte back, so writes through the view land
 * in the struct (e.g. {@code ctrls().set(i, ...)} in the maps
 * iterator). The value shape is the raw byte as an {@link Integer}
 * (the canonical boxed form for 8-bit int kinds); {@code set} accepts
 * a {@link Number} or a {@link Boolean} (1/0).
 */
public final class GoFieldByteCell implements GoCell {
    private final GoStruct struct;
    private final int fieldIndex;
    private final int byteIndex;

    public GoFieldByteCell(GoStruct struct, int fieldIndex, int byteIndex) {
        this.struct = struct;
        this.fieldIndex = fieldIndex;
        this.byteIndex = byteIndex;
    }

    /** The struct value that owns the field. */
    public GoStruct ownerStruct() {
        return struct;
    }

    /** The field index within {@link #ownerStruct()}. */
    public int fieldIndex() {
        return fieldIndex;
    }

    /** The byte index within the field's Long (little-endian). */
    public int byteIndex() {
        return byteIndex;
    }

    @Override
    public Object get() {
        long bits = struct.getLong(fieldIndex);
        int b = (int) ((bits >> (8L * byteIndex)) & 0xFFL);
        return Integer.valueOf(b);
    }

    @Override
    public void set(Object value) {
        int b;
        if (value instanceof Boolean bool) {
            b = bool ? 1 : 0;
        } else if (value instanceof Number n) {
            b = n.intValue() & 0xFF;
        } else {
            throw new IllegalArgumentException(
                    "GoFieldByteCell.set: expected a Number or Boolean, got " + value);
        }
        long bits = struct.getLong(fieldIndex);
        long mask = 0xFFL << (8L * byteIndex);
        bits = (bits & ~mask) | ((long) b << (8L * byteIndex));
        struct.setLong(fieldIndex, bits);
    }

    @Override
    public String toString() {
        return "GoFieldByteCell{byte=" + byteIndex + " of field=" + fieldIndex
                + " of " + struct + "}";
    }

    @Override
    public boolean sameStorageAs(GoCell other) {
        return other instanceof GoFieldByteCell c && struct == c.struct
                && fieldIndex == c.fieldIndex && byteIndex == c.byteIndex;
    }

    @Override
    public int storageHash() {
        return 31 * (31 * System.identityHashCode(struct) + fieldIndex) + byteIndex;
    }

    /**
     * Dedicated storage key (never collides with
     * {@link GoCell.StorageKey}, which keys (owner, index) pairs for
     * whole fields/elements — a byte view is a distinct storage from
     * the field itself).
     */
    public record ByteKey(Object owner, int field, int b) {
        @Override
        public boolean equals(Object o) {
            return o instanceof ByteKey k && k.owner == owner && k.field == field && k.b == b;
        }

        @Override
        public int hashCode() {
            return 31 * (31 * System.identityHashCode(owner) + field) + b;
        }
    }

    @Override
    public Object storageKey() {
        return new ByteKey(struct, fieldIndex, byteIndex);
    }

    @Override
    public Object prefixStruct() {
        // A byte cell addresses a byte inside a field, never a struct start.
        return null;
    }
}
