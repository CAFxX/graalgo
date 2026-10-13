package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoKind;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Assignment through a selector: {@code s.X = v}, {@code p.X = v}, and
 * promoted {@code s.E.Y = v} (M4, WS E). Navigates the selection path to
 * the owning struct (auto-dereferencing pointers) and writes the field;
 * in-place mutation is sound because every struct assignment deep-copies,
 * so the navigated structs are uniquely owned. Struct/array field values
 * go through the value-copy rule.
 */
public final class SelectorWriteNode extends GoStmtNode {
    @Child private GoExprNode obj;
    @Child private GoExprNode value;
    private final int[] selpath;
    private final GoKind fieldKind;

    public SelectorWriteNode(GoExprNode obj, GoExprNode value, int[] selpath, GoKind fieldKind) {
        this.obj = obj;
        this.value = value;
        this.selpath = selpath;
        this.fieldKind = fieldKind;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        Object o = obj.executeObject(frame);
        // Intermediate path elements are (possibly embedded) struct or
        // pointer-to-struct fields — always objects-carried.
        for (int d = 0; d < selpath.length - 1; d++) {
            o = SelectorNode.deref(o, this);
            o = ((GoStruct) o).getObject(selpath[d]);
        }
        o = SelectorNode.deref(o, this);
        GoStruct s = (GoStruct) o;
        int idx = selpath[selpath.length - 1];
        GoStructDescriptor.Field f = s.descriptor().field(idx);
        Object b = Boxing.copyForAssign(fieldKind, Boxing.box(fieldKind, value, frame));
        if (f.inLongs()) {
            s.setLong(idx, Boxing.toLongBits(fieldKind, b));
        } else {
            s.setObject(idx, b);
        }
    }
}
