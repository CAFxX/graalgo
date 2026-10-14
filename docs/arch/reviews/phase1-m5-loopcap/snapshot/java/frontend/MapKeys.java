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
            case ARRAY, SLICE, MAP -> throw new FrontendException(
                    "map key of kind " + kind + " is not supported in Phase 1 (WS D)");
            default -> {
            }
        }
        return Boxing.copyForAssign(kind, Boxing.box(kind, node, frame));
    }
}
