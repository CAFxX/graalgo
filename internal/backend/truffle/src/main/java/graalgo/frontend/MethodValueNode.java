package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;
import java.util.Map;

/**
 * A method value {@code x.M} in value position (P3-M9, WS H).
 *
 * <p>Evaluates the receiver expression once at creation time (Go semantics:
 * the receiver is evaluated and saved when the method value is formed) and
 * produces a {@link GoFunc} with the receiver bound. Every invocation of
 * the resulting function value prepends the bound receiver to the call
 * arguments (see {@link graalgo.runtime.GoCalls#invoke}), so the callee
 * observes {@code [fn, receiver, ...args]} exactly as for a direct
 * {@code methodref} call.
 *
 * <p>Resolution of the method object id is lazy through the program's
 * function table (filled by {@link NodeFactory#buildProgram} after all
 * bodies are built), mirroring {@link FuncConstNode}.
 */
public final class MethodValueNode extends GoExprNode {
    @Child private GoExprNode recv;
    private final Map<Integer, GoFunc> funcTable;
    private final int objId;

    public MethodValueNode(GoExprNode recv, Map<Integer, GoFunc> funcTable, int objId) {
        super(GoKind.FUNC);
        this.recv = recv;
        this.funcTable = funcTable;
        this.objId = objId;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        // Go evaluates the receiver once when the method value is formed,
        // and for a value receiver that means a copy (same rule as defer
        // arguments): later mutation of the source must not leak into the
        // method value, and each call below re-copies the bound copy.
        Object recvVal = Boxing.copyForAssign(recv.kind(),
                Boxing.box(recv.kind(), recv, frame));
        GoFunc f = funcTable.get(objId);
        if (f == null) {
            throw new FrontendException(
                    "unresolved method object " + objId + " (WS H)");
        }
        return new GoFunc(f.target(), null, null, new Object[] {recvVal},
                new GoKind[] {recv.kind()});
    }
}
