package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;

/**
 * Assignment through an index: {@code m[k] = v}, {@code s[i] = v},
 * {@code a[i] = v} (M4, WS E). The container/key subtrees are the factory's
 * already-evaluated temporaries (single evaluation, Go's two-phase order).
 * Struct/array values go through the value-copy rule.
 */
public final class IndexWriteNode extends GoStmtNode {
    @Child private GoExprNode container;
    @Child private GoExprNode key;
    @Child private GoExprNode value;
    private final GoKind containerKind;
    private final GoKind elemKind;

    public IndexWriteNode(GoExprNode container, GoExprNode key, GoExprNode value,
                          GoKind containerKind, GoKind elemKind) {
        this.container = container;
        this.key = key;
        this.value = value;
        this.containerKind = containerKind;
        this.elemKind = elemKind;
    }

    /** Bounds check with gc-exact panic (M5, WS F). */
    private boolean badIndex(long idx, long len) {
        return key.kind().isSigned() ? (idx < 0 || idx >= len) : Long.compareUnsigned(idx, len) >= 0;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        Object boxed = Boxing.copyForAssign(elemKind, Boxing.box(elemKind, value, frame));
        switch (containerKind) {
            case MAP -> {
                GoMap m = (GoMap) container.executeObject(frame);
                if (m == null) {
                    Panic.throwPanic(GoErrors.assignmentToNilMap(), null, null, this);
                }
                m.put(MapKeys.box(key.kind(), key, frame), boxed);
            }
            case SLICE -> {
                long idx = key.executeLong(frame);
                GoSlice s = (GoSlice) container.executeObject(frame);
                long n = s == null ? 0 : s.len();
                if (badIndex(idx, n)) {
                    Panic.throwPanic(GoErrors.indexOutOfRange(idx, n, key.kind().isSigned()), null, null, this);
                }
                Boxing.arraySet(s.array(), elemKind, s.offset() + (int) idx, boxed);
            }
            case ARRAY -> {
                long idx = key.executeLong(frame);
                Object arr = container.executeObject(frame);
                int n = java.lang.reflect.Array.getLength(arr);
                if (badIndex(idx, n)) {
                    Panic.throwPanic(GoErrors.indexOutOfRange(idx, n, key.kind().isSigned()), null, null, this);
                }
                Boxing.arraySet(arr, elemKind, (int) idx, boxed);
            }
            case POINTER -> {
                // Go: p[i] = v where p is *[N]T — dereference to the array.
                long idx = key.executeLong(frame);
                GoPointer p = (GoPointer) container.executeObject(frame);
                if (p == null || p.isNil()) {
                    Panic.throwPanic(GoErrors.nilDereference(), null, null, this);
                }
                Object arr = p.referent();
                int n = java.lang.reflect.Array.getLength(arr);
                if (badIndex(idx, n)) {
                    Panic.throwPanic(GoErrors.indexOutOfRange(idx, n, key.kind().isSigned()), null, null, this);
                }
                Boxing.arraySet(arr, elemKind, (int) idx, boxed);
            }
            default -> throw new FrontendException("index assignment on kind " + containerKind + " (WS E)");
        }
    }
}
