package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;
import java.util.Map;

/**
 * A method expression {@code T.M} in value position (REVIEW-2026-11-05-05).
 *
 * <p>Unlike a method value {@code x.M} (see {@link MethodValueNode}), a
 * method expression binds no receiver: the resulting function value's
 * first parameter IS the receiver (Go spec). It therefore produces a
 * {@link GoFunc} with no bound arguments, so the call convention
 * ({@link graalgo.runtime.GoCalls#callArgs}) passes the call's first
 * argument as the method's receiver parameter, exactly as for a direct
 * {@code methodref} call. Promoted methods are excluded on the frontend
 * side (a scope error): their callee expects the embedded field, not
 * the outer value, as its receiver argument.
 *
 * <p>Resolution of the method object id is lazy through the program's
 * function table (filled by {@link NodeFactory#buildProgram} after all
 * bodies are built), mirroring {@link FuncConstNode} and
 * {@link MethodValueNode}.
 */
public final class MethodExprNode extends GoExprNode {
    private final Map<Integer, GoFunc> funcTable;
    private final int objId;

    public MethodExprNode(Map<Integer, GoFunc> funcTable, int objId) {
        super(GoKind.FUNC);
        this.funcTable = funcTable;
        this.objId = objId;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        GoFunc f = funcTable.get(objId);
        if (f == null) {
            throw new FrontendException(
                    "unresolved method object " + objId + " (methodexpr)");
        }
        return new GoFunc(f.target(), null, null, null);
    }
}
