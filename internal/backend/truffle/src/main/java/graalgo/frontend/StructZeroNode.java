package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoKind;
import graalgo.values.GoStruct;

/**
 * A struct type's zero value as a fresh object per evaluation (M4, WS E).
 * Nested struct/array fields are recursively zeroed; the template is built
 * once per type at build time and never shared between evaluations
 * (sharing it would alias nested values across executions).
 */
public final class StructZeroNode extends GoExprNode {
    private final GoStruct template;

    public StructZeroNode(GoStruct template) {
        super(GoKind.STRUCT);
        this.template = template;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        // NEW-P0-3: freshZeroCopy, not deepCopy — nested sync-primitive
        // cells (e.g. atomic.Pointer fields) must be freshened per
        // evaluation; deepCopy shares them with the template.
        return Boxing.freshZeroCopy(template);
    }
}
