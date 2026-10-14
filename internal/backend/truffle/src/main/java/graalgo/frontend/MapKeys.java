package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoKind;

/**
 * Canonical boxing for Go map keys (M4, WS E): int/uint kinds box as
 * {@link Long} (never {@link Integer}), floats as {@link Double},
 * strings as {@link GoString} — so literal-constructed keys and
 * lookup keys always agree under {@link graalgo.values.GoEquality}.
 * Keys are value-copied at insertion (struct keys deep-copy), matching
 * Go's copy-at-insert semantics.
 */
final class MapKeys {
    private MapKeys() {}

    static Object box(GoKind kind, GoExprNode node, VirtualFrame frame) {
        switch (kind) {
            // ARRAY keys are comparable in Go (value equality); GoEquality
            // provides value-based equals/hashCode. SLICE and MAP are not
            // comparable and cannot be map keys.
            case SLICE, MAP -> throw new FrontendException(
                    "map key of kind " + kind + " is not supported in Phase 1 (WS D)");
            default -> {
            }
        }
        Object boxed = Boxing.box(kind, node, frame);
        // Canonicalize int/uint keys to Long: Boxing.box may return Integer
        // for sub-64-bit kinds, but map lookups must agree on the boxing.
        // Using Long for all int/uint kinds ensures Integer(-1) (uint32
        // 0xFFFFFFFF) and Long(4294967295L) are the same key.
        if (kind.isIntKind() || kind.isUintKind()) {
            long v = Boxing.toLong(boxed, kind);
            boxed = Long.valueOf(v);
        }
        return Boxing.copyForAssign(kind, boxed);
    }
}
