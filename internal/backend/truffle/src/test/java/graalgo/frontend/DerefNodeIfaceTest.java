package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Tests for {@link DerefNode}'s unsafe interface reinterpretation
 * (P3-M16, fix-redmaster-r16).
 *
 * <p>The canonical Go idiom {@code *(*S)(unsafe.Pointer(&i))} reads an
 * interface value's (type, data) words as a struct. graalgo represents
 * interface values as {@link GoInterface} objects, not word pairs, so
 * dereferencing a {@code *S} (S struct) whose referent is a GoInterface
 * must materialize S from the interface's (descriptor, data), mapped
 * positionally to S's fields. Without this, the raw GoInterface flows into
 * struct field selection and NPEs/CCEs.
 */
public final class DerefNodeIfaceTest {

    /** A GoExprNode returning a fixed value, ignoring the frame. */
    private static final class ConstNode extends GoExprNode {
        private final Object value;

        ConstNode(GoKind kind, Object value) {
            super(kind);
            this.value = value;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return value;
        }
    }

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError(what + ": expected true");
        }
    }

    /** Descriptor shape {typ STRING, data POINTER}: the type word lands as-is, the data word as a GoPointer. */
    private static GoStructDescriptor twoWordDesc() {
        return GoStructDescriptor.layout("S",
                java.util.List.of("typ", "data"),
                java.util.List.of(GoKind.STRING, GoKind.POINTER));
    }

    /**
     * A GoInterface referent behind a struct deref materializes to a
     * GoStruct: field 0 receives the type word, field 1 the data word
     * (as a GoPointer). Without the fix, the raw GoInterface is returned.
     */
    public void testIfaceMaterializesStruct() {
        Object typeWord = new Object();
        Object dataWord = "hello";
        GoInterface gi = new GoInterface(typeWord, dataWord, null);
        DerefNode n = new DerefNode(GoKind.STRUCT, new ConstNode(GoKind.INTERFACE, gi), twoWordDesc());
        Object out = n.executeObject(null);
        assertTrue(out instanceof GoStruct, "deref of GoInterface yields a GoStruct, not " + out);
        GoStruct gs = (GoStruct) out;
        assertTrue(gs.getObject(0) == typeWord, "field 0 is the type word");
        Object dataField = gs.getObject(1);
        assertTrue(dataField instanceof GoPointer, "field 1 is a GoPointer");
        assertTrue(((GoPointer) dataField).referent() == dataWord, "field 1 points at the data word");
    }

    /**
     * The data word mapping works for int, string, and struct carriers
     * (the interface's dynamic value in its D5 carrier).
     */
    public void testIfaceDataWordCarriers() {
        Object[] datas = {Long.valueOf(42), "s", new GoStruct(twoWordDesc(),
                new long[0], new Object[]{null, new GoPointer(null)})};
        for (Object dataWord : datas) {
            GoInterface gi = new GoInterface(new Object(), dataWord, null);
            DerefNode n = new DerefNode(GoKind.STRUCT, new ConstNode(GoKind.INTERFACE, gi), twoWordDesc());
            Object out = n.executeObject(null);
            assertTrue(out instanceof GoStruct, "GoStruct for data " + dataWord);
            Object dataField = ((GoStruct) out).getObject(1);
            assertTrue(dataField instanceof GoPointer, "data field is a GoPointer for " + dataWord);
            assertTrue(((GoPointer) dataField).referent() == dataWord,
                    "data pointer referent is identical for " + dataWord);
        }
    }

    /**
     * A nil interface materializes to the zero struct: null words become
     * null (STRING field) and nil pointers (POINTER field) — never an NPE.
     */
    public void testNilIfaceMaterializesZeroStruct() {
        DerefNode n = new DerefNode(GoKind.STRUCT,
                new ConstNode(GoKind.INTERFACE, GoInterface.NIL), twoWordDesc());
        Object out = n.executeObject(null);
        assertTrue(out instanceof GoStruct, "nil interface yields a GoStruct");
        GoStruct gs = (GoStruct) out;
        assertTrue(gs.getObject(0) == null, "null type word maps to null");
        Object dataField = gs.getObject(1);
        assertTrue(dataField instanceof GoPointer && ((GoPointer) dataField).isNil(),
                "null data word maps to a nil GoPointer");
    }

    /**
     * Negative: a genuine GoStruct referent is NOT materialized — it passes
     * through as-is. The materialization trigger is precise to the unsafe
     * reinterpretation idiom.
     */
    public void testGoStructReferentPassesThrough() {
        GoStruct gs = new GoStruct(twoWordDesc(), new long[0],
                new Object[]{"t", new GoPointer(null)});
        DerefNode n = new DerefNode(GoKind.STRUCT, new ConstNode(GoKind.STRUCT, gs), twoWordDesc());
        assertTrue(n.executeObject(null) == gs, "GoStruct referent passes through unchanged");
    }

    /**
     * Negative: without a struct descriptor (non-struct deref), a
     * GoInterface referent is returned as-is — the guard requires
     * structDesc != null.
     */
    public void testNullStructDescPassesThrough() {
        GoInterface gi = new GoInterface(new Object(), "x", null);
        DerefNode n = new DerefNode(GoKind.INT64, new ConstNode(GoKind.INTERFACE, gi), null);
        assertTrue(n.executeObject(null) == gi, "GoInterface passes through when structDesc is null");
    }
}
