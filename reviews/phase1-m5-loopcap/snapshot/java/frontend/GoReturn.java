package graalgo.frontend;

import com.oracle.truffle.api.nodes.ControlFlowException;

/**
 * Go's return control flow (M5, WS F).
 *
 * <p>Carries the returned value up to the owning root node
 * ({@link GoRootNode} or {@link GoFuncRootNode}), skipping the rest of the
 * statement list. A {@link ControlFlowException} (D3): no stack trace, and
 * Truffle treats it as a fast local control transfer. The value is widened
 * to {@link Object} (D5); root nodes with result slots read the slot
 * instead (defer runs before the return completes).
 */
final class GoReturn extends ControlFlowException {
    final Object value;

    GoReturn(Object value) {
        this.value = value;
    }
}
