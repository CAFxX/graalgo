package graalgo.frontend;

import java.util.Arrays;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoComplex32;
import graalgo.values.GoComplex64;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;

// verifies: GG-0021 (WS G builtin)
/**
 * The {@code clear} builtin (WS G; verifies GG-0021).
 *
 * <p>{@code clear(m)} deletes all entries from map {@code m}.
 * {@code clear(s)} zeroes all elements of slice {@code s} (up to its length).
 * It is a statement-only builtin (no result): {@link #executeLong} is
 * implemented so {@link ExprStmtNode} can execute it.
 *
 * <p>Nil handling (gc parity): {@code clear} of a nil map or a nil slice is
 * a no-op. Nil slices are a Java {@code null} {@link GoSlice} reference
 * ({@link GoSlice} forbids a null backing array), so the null check must
 * precede any dereference — the old code dereferenced first and threw a
 * Java {@link NullPointerException} on a nil slice (R10-02).
 *
 * <p>Slice zeroing covers every backing-array type the D5 mapping can
 * produce (see {@code Boxing.newArray}): the primitive arrays, the complex
 * carriers (filled with their {@code ZERO} value, never {@code null} — a
 * null complex slot would surface as nil instead of 0+0i), and
 * {@code Object[]} (filled with {@code null}, the zero for every nil-able
 * element type). An unrecognised backing array or operand kind fails fast
 * with {@link FrontendException} instead of silently doing nothing
 * (R10-02). Residual gap, documented not hidden: an {@code Object[]}
 * holding struct values allocated with a descriptor holds zero-valued
 * {@code GoStruct}s, not nulls, but the element kind is not carried by
 * {@link GoSlice}, so this node cannot distinguish struct slots from
 * string/pointer slots — clearing a struct slice nulls its slots. Fixing
 * that needs element-kind plumbing in both clear implementations
 * (R10-P2-03 follow-up).
 *
 * <p>Unification note (R10-P2-03): the live path's
 * {@code graalgo.p3builtin.nodes.ClearNode} implements the same semantics
 * for the {@code "clear"} wire node. This node cannot reuse that class:
 * the runtime module's joint round compiles these sources without the
 * interop classes on the classpath, and interop depends on this module —
 * importing it would invert the dependency. The two implementations are
 * kept in agreement by the shared cases in {@link ClearNodeTest}.
 */
public final class ClearNode extends GoExprNode {
    @Child private GoExprNode targetChild;

    public ClearNode(GoExprNode targetChild) {
        // No result; INT64 is a placeholder kind (only executeLong is
        // ever called in statement position, via ExprStmtNode; executeObject
        // exists so a value-position evaluation clears instead of throwing).
        super(GoKind.INT64);
        this.targetChild = targetChild;
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        Object target = targetChild.executeObject(frame);
        // clear on a nil map or nil slice is a no-op (gc parity). Nil
        // slices are Java null: check before any dereference.
        if (target == null) {
            return 0L;
        }
        GoKind k = targetChild.kind();
        if (k == GoKind.MAP) {
            if (!(target instanceof GoMap m)) {
                throw new FrontendException(
                        "clear: MAP operand is not a GoMap: " + target.getClass().getName());
            }
            m.clear();
        } else if (k == GoKind.SLICE) {
            if (!(target instanceof GoSlice gs)) {
                throw new FrontendException(
                        "clear: SLICE operand is not a GoSlice: " + target.getClass().getName());
            }
            clearSlice(gs);
        } else {
            throw new FrontendException("clear: unsupported operand kind " + k);
        }
        return 0L;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        // clear is statement-only (see the class doc), but a value-position
        // evaluation must clear, not throw UnsupportedOperationException:
        // mirrors the live p3builtin ClearNode.
        executeLong(frame);
        return null;
    }

    /** Zeroes the slice's {@code len} elements, honouring offset/len. */
    private static void clearSlice(GoSlice gs) {
        Object arr = gs.array();
        int off = gs.offset();
        int len = gs.len();
        if (len == 0) {
            return;
        }
        // Backing arrays are typed per D5 (see Boxing.newArray). The
        // complex branches must precede Object[]: GoComplex32[] and
        // GoComplex64[] ARE Object[], but their zero is the ZERO value,
        // not null (Boxing.newArray pre-initialises them to 0+0i).
        if (arr instanceof byte[] ba) {
            Arrays.fill(ba, off, off + len, (byte) 0);
        } else if (arr instanceof short[] sa) {
            Arrays.fill(sa, off, off + len, (short) 0);
        } else if (arr instanceof int[] ia) {
            Arrays.fill(ia, off, off + len, 0);
        } else if (arr instanceof long[] la) {
            Arrays.fill(la, off, off + len, 0L);
        } else if (arr instanceof float[] fa) {
            Arrays.fill(fa, off, off + len, 0.0f);
        } else if (arr instanceof double[] da) {
            Arrays.fill(da, off, off + len, 0.0);
        } else if (arr instanceof boolean[] bo) {
            Arrays.fill(bo, off, off + len, false);
        } else if (arr instanceof GoComplex32[] ca) {
            Arrays.fill(ca, off, off + len, GoComplex32.ZERO);
        } else if (arr instanceof GoComplex64[] ca) {
            Arrays.fill(ca, off, off + len, GoComplex64.ZERO);
        } else if (arr instanceof Object[] oa) {
            Arrays.fill(oa, off, off + len, null);
        } else {
            throw new FrontendException(
                    "clear: unsupported slice backing array " + arr.getClass().getName());
        }
    }
}
