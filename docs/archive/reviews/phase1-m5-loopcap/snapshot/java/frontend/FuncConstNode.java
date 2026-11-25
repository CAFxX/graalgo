package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;
import java.util.Map;

/**
 * Reference to a named top-level function or a monomorphized instantiation
 * (M6, WS H).
 *
 * <p>Evaluates to the shared {@link GoFunc} value for the function object id.
 * Resolution is lazy through the program's function table (filled by
 * {@link NodeFactory#buildProgram} after all bodies are built), so
 * (mutually) recursive functions resolve correctly.
 */
public final class FuncConstNode extends GoExprNode {
    private final Map<Integer, GoFunc> funcTable;
    private final int objId;

    public FuncConstNode(Map<Integer, GoFunc> funcTable, int objId) {
        super(GoKind.FUNC);
        this.funcTable = funcTable;
        this.objId = objId;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        GoFunc f = funcTable.get(objId);
        if (f == null) {
            throw new FrontendException(
                    "unresolved function object " + objId + " (WS H)");
        }
        return f;
    }
}
