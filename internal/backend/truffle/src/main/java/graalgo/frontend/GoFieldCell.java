package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoCell;
import graalgo.values.GoKind;
import graalgo.values.GoStruct;

/**
 * A {@link GoCell} addressing a struct field: the storage behind
 * {@code &s.f} (P3-M9, WS D).
 *
 * <p>The cell holds the {@link GoStruct} object that was the field owner's
 * value when {@code &} was evaluated, plus the field index. If the owner
 * variable is later reassigned, the cell keeps pointing at the old
 * struct's field — matching gc. {@code get} returns the canonical boxed
 * form (raw {@code Long} bits for {@code inLongs} fields, mirroring
 * {@link SelectorNode}); {@code set} applies the struct carrier encoding
 * (mirroring {@link SelectorWriteNode}). The value handed to {@code set}
 * is already canonical-boxed and copyForAssign'd by {@link DerefWriteNode}.
 */
public final class GoFieldCell implements GoCell {
    private final GoStruct struct;
    private final int fieldIndex;
    private final GoKind fieldKind;
    private final boolean inLongs;

    public GoFieldCell(GoStruct struct, int fieldIndex, GoKind fieldKind) {
        this.struct = struct;
        this.fieldIndex = fieldIndex;
        this.fieldKind = fieldKind;
        this.inLongs = struct.descriptor().field(fieldIndex).inLongs();
    }

    @Override
    public Object get() {
        return inLongs
                ? Long.valueOf(struct.getLong(fieldIndex))
                : struct.getObject(fieldIndex);
    }

    @Override
    public void set(Object value) {
        if (inLongs) {
            struct.setLong(fieldIndex, Boxing.toLongBits(fieldKind, value));
        } else {
            struct.setObject(fieldIndex, value);
        }
    }

    /** For debugging: which struct/field this cell addresses. */
    @Override
    public String toString() {
        return "GoFieldCell{field=" + fieldIndex + " of " + struct + "}";
    }

    @Override
    public boolean sameStorageAs(GoCell other) {
        // A GoFieldCell is allocated fresh on every &s.f evaluation, so
        // two cells address the same storage iff they were taken over the
        // same struct value (object identity: a reassigned owner variable
        // holds a new struct, and the old cell keeps pointing at the old
        // one, matching gc) and the same field index.
        return other instanceof GoFieldCell f && struct == f.struct && fieldIndex == f.fieldIndex;
    }

    @Override
    public boolean equals(Object o) {
        return o instanceof GoCell c && sameStorageAs(c);
    }

    @Override
    public int hashCode() {
        return System.identityHashCode(struct) * 31 + fieldIndex;
    }
}
