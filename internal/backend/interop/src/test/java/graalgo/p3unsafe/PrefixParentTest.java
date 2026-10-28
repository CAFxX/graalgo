package graalgo.p3unsafe;

import graalgo.values.GoKind;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

import java.util.Arrays;
import java.util.List;

/**
 * fix-redmaster-r40: unit tests for {@code GoStruct.prefixParent}
 * first-store-wins semantics.
 *
 * <p>The prefix parent links a field-0 embedded struct value to the
 * widest struct that owns it — for reflect's type shells, the
 * canonical AbiTypes shell minted for the type. Transient wrappers
 * (an rtype view of the same header, built during a conversion) are
 * created AFTER the shell and must not steal the link: with
 * most-recent-store-wins, hello's {@code len(t.Methods)} read the
 * bare header's Hash field as the Methods slice, because a transient
 * rtype wrap had overwritten the header's prefixParent and the
 * sibling-transfer in {@link PrefixView} recovered the wrong owner.
 *
 * <p>Positive coverage: the first field-0 store wins through both
 * registration paths (constructor via {@code GoStruct} copying and
 * {@code setObject}). Negative coverage: a non-struct field-0 and a
 * scalar field-0 never register a prefix parent. Falsifiability: on
 * the most-recent-wins implementation the second store overwrites the
 * parent and the identity assertions fail.
 */
public final class PrefixParentTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static GoStructDescriptor childDesc() {
        return GoStructDescriptor.layout("test.Child",
                List.of("V"), List.of(GoKind.INT64));
    }

    private static GoStructDescriptor wrapperDesc(String name, GoStructDescriptor child) {
        return GoStructDescriptor.layout(name,
                List.of("Head", "Ext"),
                List.of(GoKind.STRUCT, GoKind.INT64),
                List.of(-1, -1),
                Arrays.asList(null, null),
                Arrays.asList(null, null),
                Arrays.asList(child, null));
    }

    public void testFirstStoreWinsAcrossTwoOwners() {
        GoStructDescriptor child = childDesc();
        GoStruct shared = GoStruct.zero(child);
        GoStruct canonical = GoStruct.zero(wrapperDesc("test.Shell", child));
        canonical.setObject(0, shared);
        assertTrue(shared.prefixParent() == canonical,
                "the first owner must be recorded as prefix parent");
        GoStruct transientWrap = GoStruct.zero(wrapperDesc("test.Transient", child));
        transientWrap.setObject(0, shared);
        assertTrue(shared.prefixParent() == canonical,
                "a later wrapper must NOT overwrite the canonical prefix parent");
    }

    public void testConstructorRegistrationIsAlsoFirstWins() {
        GoStructDescriptor child = childDesc();
        GoStruct shared = GoStruct.zero(child);
        GoStruct first = GoStruct.zero(wrapperDesc("test.First", child));
        first.setObject(0, shared);
        // Constructing another owner around the same child (the copy /
        // materialize path) must not steal the link either.
        GoStructDescriptor w2 = wrapperDesc("test.Second", child);
        long[] longs = new long[w2.longsCount()];
        Object[] objects = new Object[w2.objectsCount()];
        objects[0] = shared;
        new GoStruct(w2, longs, objects);
        assertTrue(shared.prefixParent() == first,
                "constructor registration must also be first-store-wins");
    }

    public void testScalarFieldZeroNeverRegisters() {
        GoStructDescriptor flat = GoStructDescriptor.layout("test.Flat",
                List.of("A", "B"), List.of(GoKind.INT64, GoKind.INT64));
        GoStruct s = GoStruct.zero(flat);
        assertTrue(s.prefixParent() == null,
                "a struct with a scalar field 0 has no prefix child to own");
        assertTrue(GoStruct.zero(childDesc()).prefixParent() == null,
                "an unowned struct has no prefix parent");
    }

    public void testNonStructObjectFieldZeroNeverRegisters() {
        GoStructDescriptor withSlice = GoStructDescriptor.layout("test.WithSlice",
                List.of("S", "N"),
                List.of(GoKind.SLICE, GoKind.INT64),
                List.of(-1, -1),
                Arrays.asList(null, null),
                Arrays.asList(null, null),
                Arrays.asList(null, null));
        GoStruct owner = GoStruct.zero(withSlice);
        GoStruct bystander = GoStruct.zero(childDesc());
        owner.setObject(0, new graalgo.values.GoSlice(new Object[0], 0, 0, 0));
        assertTrue(bystander.prefixParent() == null,
                "storing a slice at field 0 must not register a struct prefix parent");
        assertTrue(owner.prefixParent() == null,
                "the owner itself gains no prefix parent from a slice field");
    }
}
