package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Panic;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
import graalgo.values.GoStructDescriptor;

/**
 * Index expression {@code x[i]} (M4, WS E): array, slice, string
 * (→ byte), and map (absent key → element zero value). Multi-dimensional
 * indexing nests these nodes ({@code m[i][j]}).
 *
 * <p>Out-of-range indices throw a real Go panic (M5, WS F):
 * {@code runtime error: index out of range [i] with length n}, with the
 * index rendered signed/unsigned per its Go kind (gc-exact).
 */
public final class IndexNode extends GoExprNode {
    @Child private GoExprNode container;
    @Child private GoExprNode key;
    private final GoKind containerKind;
    private final GoKind elemKind;
    private final GoStructDescriptor elemStructDesc; // for struct-kind map/slice elements

    public IndexNode(GoExprNode container, GoExprNode key,
                     GoKind containerKind, GoKind elemKind,
                     GoStructDescriptor elemStructDesc) {
        super(elemKind);
        this.container = container;
        this.key = key;
        this.containerKind = containerKind;
        this.elemKind = elemKind;
        this.elemStructDesc = elemStructDesc;
    }

    public GoExprNode container() {
        return container;
    }

    public GoExprNode key() {
        return key;
    }

    /**
     * Bounds check with gc-exact panic (M5, WS F). The index renders signed
     * or unsigned per its Go kind; a negative signed index selects gc's
     * overriding negative format ({@code [i]}, no length). A nil slice has
     * length 0, so the same check covers it (gc: {@code [i] with length 0}).
     */
    private int checkedIndex(long idx, long len) {
        boolean signed = keyKind().isSigned();
        boolean bad = signed ? (idx < 0 || idx >= len) : Long.compareUnsigned(idx, len) >= 0;
        if (bad) {
            Panic.throwPanic(GoErrors.indexOutOfRange(idx, len, signed), null, null, this);
        }
        return (int) idx;
    }

    private Object read(VirtualFrame frame) {
        return switch (containerKind) {
            case ARRAY -> {
                long idx = key.executeLong(frame);
                Object arr = container.executeObject(frame);
                int i = checkedIndex(idx, java.lang.reflect.Array.getLength(arr));
                yield Boxing.arrayGet(arr, elemKind, i);
            }
            case SLICE -> {
                long idx = key.executeLong(frame);
                GoSlice s = (GoSlice) container.executeObject(frame);
                int i = checkedIndex(idx, s == null ? 0 : s.len());
                yield Boxing.arrayGet(s.array(), elemKind, s.offset() + i);
            }
            case STRING -> {
                long idx = key.executeLong(frame);
                byte[] bytes = ((GoString) container.executeObject(frame)).bytes();
                int i = checkedIndex(idx, bytes.length);
                yield Integer.valueOf(bytes[i] & 0xFF);
            }
            case MAP -> {
                GoMap m = (GoMap) container.executeObject(frame);
                if (m == null) {
                    // Reading a nil map is legal: zero value.
                    yield Boxing.zero(elemKind, elemStructDesc);
                }
                Object k = MapKeys.box(keyKind(), key, frame);
                Object v = m.get(k);
                // A present key may hold a null (nil) value; only an
                // absent key yields the zero value.
                yield (v == null && !m.containsKey(k))
                        ? Boxing.zero(elemKind, elemStructDesc)
                        : v;
            }
            default -> throw new FrontendException("index on kind " + containerKind + " (WS E)");
        };
    }

    /** The key's Go kind (for canonical key boxing). */
    private GoKind keyKind() {
        return key.kind();
    }

    @Override
    public long executeLong(VirtualFrame frame) {
        Object v = read(frame);
        // Numeric nodes implement both int/long readers with correct
        // extension: an Int-slot value (e.g. byte from string indexing)
        // widens here.
        if (v instanceof Integer i) {
            return elemKind.isSigned() ? (long) i.intValue() : Integer.toUnsignedLong(i);
        }
        return ((Long) v).longValue();
    }

    @Override
    public int executeInt(VirtualFrame frame) {
        Object v = read(frame);
        if (v instanceof Long l) {
            return l.intValue();
        }
        return ((Integer) v).intValue();
    }

    @Override
    public float executeFloat(VirtualFrame frame) {
        return ((Float) read(frame)).floatValue();
    }

    @Override
    public double executeDouble(VirtualFrame frame) {
        return ((Double) read(frame)).doubleValue();
    }

    @Override
    public boolean executeBoolean(VirtualFrame frame) {
        return ((Boolean) read(frame)).booleanValue();
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        return read(frame);
    }
}
