package graalgo.go.runtime;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.types.GoType;
import graalgo.go.types.GoType.Field;
import graalgo.go.types.Method;
import graalgo.go.values.Flat;
import graalgo.go.values.Ptr;

/**
 * Adapts an interface's data word to the receiver a method-set entry's function declares:
 * the identity for the common shapes, a private copy for struct value receivers, and for
 * promoted methods a walk along the embedded-field path (replacing v1's embedded redispatch
 * loop at the call site). Used by interface calls, method values and method expressions.
 */
public final class Receivers {
    private Receivers() {}

    public static Object adapt(Method m, Object data, Node node) {
        switch (m.mode) {
            case Method.IDENTITY:
                return data;
            case Method.COPY:
                return ((Flat) data).copy();
            default:
                return general(m, data, node);
        }
    }

    @TruffleBoundary
    private static Object general(Method m, Object data, Node node) {
        GoType cur = m.owner;
        Flat obj;
        int p = 0;
        int r = 0;
        int b = 0;
        if (m.ofPointer) {
            if (data == null) {
                throw Panics.nilDeref(node);
            } else if (data instanceof Ptr ptr) {
                obj = ptr.obj();
                p = ptr.prim();
                r = ptr.ref();
                b = ptr.byt();
            } else {
                obj = (Flat) data;
            }
        } else {
            obj = (Flat) data;
        }
        for (int index : m.path) {
            GoType u = cur.u();
            u.nPrim();
            Field f = u.fields[index];
            GoType ft = f.type.u();
            if (ft.kind == GoType.POINTER) {
                Object ptr = obj.refs[r + f.refOff];
                if (ptr == null) {
                    throw Panics.nilDeref(node);
                } else if (ptr instanceof Ptr q) {
                    obj = q.obj();
                    p = q.prim();
                    r = q.ref();
                    b = q.byt();
                } else {
                    obj = (Flat) ptr;
                    p = 0;
                    r = 0;
                    b = 0;
                }
                cur = ft.elem;
            } else {
                p += f.primOff;
                r += f.refOff;
                b += f.byteOff;
                cur = f.type;
            }
        }
        return m.ptrRecv ? Ptr.make(obj, p, r, b) : Values.load(cur, obj, p, r, b);
    }
}
