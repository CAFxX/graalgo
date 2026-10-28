package graalgo.p3unsafe;

import graalgo.frontend.SelectorNode;
import graalgo.values.GoBox;
import graalgo.values.GoKind;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

import java.util.List;

/**
 * fix-redmaster-r40: unit tests for {@link PrefixView} — the embedding
 * prefix reinterpretation applied when an {@code unsafe.Pointer} over
 * a struct value is converted to a pointer to a struct type that
 * embeds it (or vice versa). This is the shape reflect relies on:
 * {@code sliceType} embeds {@code abi.SliceType}, so
 * {@code (*sliceType)(unsafe.Pointer(v.typ())).Elem} walks field path
 * {@code [0, 1]} and must land on the abi shell's {@code Elem}, not on
 * a field of the shell's nested header.
 *
 * <p>Positive coverage: wrap (embedded referent → wrapper whose field
 * 0 IS the referent; the promoted path {@code [0, 1]} then reads the
 * referent's own field 1 — the exact read that failed before the fix),
 * unwrap (wrapper referent → its nested target struct, by identity),
 * and cell unwrapping. Negative coverage: a referent already of the
 * target type, an unrelated struct, a scalar referent, and a field-0
 * name mismatch all get NO view.
 *
 * <p>Falsifiability: {@code PrefixView} does not exist on the unfixed
 * base, so this test does not compile there; the wrap test also pins
 * the promoted-path read result, which the pre-fix bare conversion
 * gets wrong (it reads the nested header's field 1 instead).
 */
public final class PrefixViewTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(long expected, long actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    private static void assertNull(Object o, String msg) {
        if (o != null) {
            throw new AssertionError(msg + " (got " + o + ")");
        }
    }

    /** The "abi shell" shape: {Header struct, Elem int-as-long-field stand-in}. */
    private static GoStructDescriptor embeddedDesc() {
        return GoStructDescriptor.layout("test.Embedded",
                List.of("Head", "Elem"),
                List.of(GoKind.STRUCT, GoKind.INT64),
                List.of(-1, -1),
                java.util.Arrays.asList(null, null),
                java.util.Arrays.asList(null, null),
                java.util.Arrays.asList(headerDesc(), null));
    }

    private static GoStructDescriptor headerDesc() {
        return GoStructDescriptor.layout("test.Header",
                List.of("Size", "PtrBytes"),
                List.of(GoKind.INT64, GoKind.INT64));
    }

    /** The "reflect wrapper" shape: {Embedded}. */
    private static GoStructDescriptor wrapperDesc() {
        return GoStructDescriptor.layout("test.Wrapper",
                List.of("Embedded"),
                List.of(GoKind.STRUCT),
                List.of(-1),
                java.util.Collections.singletonList(null),
                java.util.Collections.singletonList(null),
                List.of(embeddedDesc()));
    }

    /** An embedded value whose Head.PtrBytes (99) differs from Elem (22). */
    private static GoStruct embeddedValue() {
        GoStruct head = GoStruct.zero(headerDesc());
        head.setLong(0, 1000);
        head.setLong(1, 99);
        GoStruct emb = GoStruct.zero(embeddedDesc());
        emb.setObject(0, head);
        emb.setLong(1, 22);
        return emb;
    }

    public void testWrapPromotedPathReadsEmbeddedField() {
        GoStruct emb = embeddedValue();
        GoStruct view = PrefixView.viewFor(emb, wrapperDesc(), "test.Embedded");
        assertTrue(view != null, "an embedded referent must produce a wrapper view");
        assertTrue(view.getObject(0) == emb,
                "the wrapper's field 0 must BE the referent (no copy)");
        // The exact read reflect performs: tt.Elem via path [0, 1].
        // Pre-fix (bare pointer to emb), path [0, 1] reads Head.PtrBytes
        // (99); the wrapped view must read Elem (22).
        Object elem = SelectorNode.readFieldPath(view, new int[]{0, 1}, false);
        assertEquals(22, (Long) elem, "promoted path [0,1] must read the embedded Elem field");
    }

    public void testUnwrapReturnsNestedStructByIdentity() {
        GoStruct emb = embeddedValue();
        GoStruct wrapper = GoStruct.zero(wrapperDesc());
        wrapper.setObject(0, emb);
        GoStruct view = PrefixView.viewFor(wrapper, embeddedDesc(), "test.Header");
        assertTrue(view == emb, "unwrap must return the nested struct itself");
    }

    public void testCellWrappedEmbeddedUnwraps() {
        GoStruct emb = embeddedValue();
        GoStruct view = PrefixView.viewFor(new GoBox(emb), wrapperDesc(), "test.Embedded");
        assertTrue(view != null && view.getObject(0) == emb,
                "a cell holding the embedded value must produce the wrapper view");
    }

    public void testFieldCellPrefixIdentityYieldsOwner() {
        // The rtype.common() shape: a field cell over field 0 of the
        // "shell" (here test.Embedded, fields [Head, Elem]) converted
        // to the shell's own type must yield the shell itself, with
        // its Elem (22) intact — viewing the field value (the Head
        // header) instead would wrap it and zero the Elem.
        GoStruct shell = embeddedValue();
        graalgo.frontend.GoFieldCell cell =
                new graalgo.frontend.GoFieldCell(shell, 0, GoKind.STRUCT);
        GoStruct view = PrefixView.viewFor(cell, embeddedDesc(), "test.Header");
        assertTrue(view == shell,
                "a field-0 cell converted to the owner's type must yield the owner");
        assertEquals(22, view.getLong(1), "the owner's Elem field must survive the conversion");
    }

    public void testWrapRecoversUnwrappedOwner() {
        // The round trip that corrupted the abi shells at reflect
        // init: unwrap the shell to its Head header, then convert the
        // header back to the shell type. The wrap must recover the
        // ORIGINAL shell (Elem 22 intact), not fabricate a wrapper
        // with Elem zeroed.
        GoStruct shell = embeddedValue();
        Object head = PrefixView.viewFor(shell, headerDesc(), null);
        assertTrue(head instanceof GoStruct, "unwrap must yield the Head struct");
        GoStruct back = PrefixView.viewFor((GoStruct) head, embeddedDesc(), "test.Header");
        assertTrue(back == shell,
                "wrap of an unwrapped header must recover its owner shell");
        assertEquals(22, back.getLong(1), "the recovered shell's Elem must be intact");
    }

    public void testAlreadyTargetShapeGetsNoView() {
        GoStruct wrapper = GoStruct.zero(wrapperDesc());
        wrapper.setObject(0, embeddedValue());
        assertNull(PrefixView.viewFor(wrapper, wrapperDesc(), "test.Embedded"),
                "a referent already of the target type needs no view");
    }

    public void testUnrelatedReferentsGetNoView() {
        GoStruct other = GoStruct.zero(headerDesc());
        assertNull(PrefixView.viewFor(other, wrapperDesc(), "test.Embedded"),
                "an unrelated struct must NOT be wrapped");
        assertNull(PrefixView.viewFor(Long.valueOf(7), wrapperDesc(), "test.Embedded"),
                "a scalar referent must NOT be wrapped");
        GoStruct emb = embeddedValue();
        assertNull(PrefixView.viewFor(emb, wrapperDesc(), "test.SomethingElse"),
                "a field-0 name mismatch must NOT wrap");
        GoStructDescriptor otherDesc = GoStructDescriptor.layout("test.Other",
                List.of("X"), List.of(GoKind.INT64));
        assertNull(PrefixView.viewFor(emb, otherDesc, null),
                "a target whose name matches no prefix relation gets no view");
    }
}
