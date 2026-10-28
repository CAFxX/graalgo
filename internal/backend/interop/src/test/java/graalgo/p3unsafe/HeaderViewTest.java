package graalgo.p3unsafe;

import graalgo.values.GoBox;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoSlice;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

import java.util.List;

/**
 * fix-redmaster-r40: unit tests for {@link HeaderView} — the header
 * struct materialized when an {@code unsafe.Pointer} over a string or
 * slice value is converted to a header-shaped struct pointer
 * (reflect's {@code Len} path: {@code (*unsafeheader.String)(v.ptr).Len}).
 *
 * <p>Positive coverage: string views (Data is the established
 * string-data handle, Len from the value), slice views (Data is the
 * slice-data ArrayRef at the slice's offset, Len/Cap from the value),
 * the uintptr-carried and pointer-carried Data word encodings, cell
 * unwrapping, the empty string's nil Data handle, and the zero header
 * for a null referent. Negative coverage: non-string/slice referents
 * and non-header descriptor shapes get NO view (the caller keeps the
 * pre-view behavior), so the mechanism can never silently reinterpret
 * an unrelated value as a header.
 *
 * <p>Falsifiability: {@code HeaderView} does not exist on the unfixed
 * base, so this test does not compile there; each positive assertion
 * also pins exact field values a wrong view (e.g. the raw value, or a
 * zero struct) would fail.
 */
public final class HeaderViewTest {

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

    /** A fresh table per test: handles restart from 1, like a new run. */
    private static UnsafeTable freshTable() {
        UnsafeTable.install();
        return UnsafeTable.current();
    }

    /** unsafeheader.String shape: {Data unsafe.Pointer, Len int}. */
    private static GoStructDescriptor stringHeaderDesc() {
        return GoStructDescriptor.layout("test.StringHeader",
                List.of("Data", "Len"),
                List.of(GoKind.UNSAFE_POINTER, GoKind.INT64));
    }

    /** unsafeheader.Slice shape: {Data unsafe.Pointer, Len int, Cap int}. */
    private static GoStructDescriptor sliceHeaderDesc() {
        return GoStructDescriptor.layout("test.SliceHeader",
                List.of("Data", "Len", "Cap"),
                List.of(GoKind.UNSAFE_POINTER, GoKind.INT64, GoKind.INT64));
    }

    public void testStringHeaderView() {
        UnsafeTable t = freshTable();
        GoString gs = GoString.fromUtf8("hello");
        GoStruct view = HeaderView.build(gs, stringHeaderDesc());
        assertTrue(view != null, "a string value must produce a header view");
        assertEquals(5, view.getLong(1), "header Len must be the string length");
        long dataHandle = (Long) view.getObject(0);
        assertEquals(UnsafeOps.stringData(gs), dataHandle,
                "header Data must be the established string-data handle");
        assertTrue(t.resolve(dataHandle) == gs,
                "header Data must resolve back to the same GoString");
    }

    public void testSliceHeaderView() {
        UnsafeTable t = freshTable();
        byte[] backing = {104, 101, 108, 108, 111};
        GoSlice gs = new GoSlice(backing, 1, 3, 4);
        GoStruct view = HeaderView.build(gs, sliceHeaderDesc());
        assertTrue(view != null, "a slice value must produce a header view");
        assertEquals(3, view.getLong(1), "header Len must be the slice length");
        assertEquals(4, view.getLong(2), "header Cap must be the slice capacity");
        long dataHandle = (Long) view.getObject(0);
        assertEquals(UnsafeOps.sliceData(gs), dataHandle,
                "header Data must be the established slice-data handle");
        Object resolved = t.resolve(dataHandle);
        assertTrue(resolved instanceof UnsafeTable.ArrayRef ref
                        && ref.array() == backing && ref.offset() == 1,
                "header Data must resolve to the slice's ArrayRef at its offset");
    }

    public void testTwoWordHeaderOverSliceReadsLen() {
        freshTable();
        GoSlice gs = new GoSlice(new int[6], 2, 4, 4);
        GoStruct view = HeaderView.build(gs, stringHeaderDesc());
        assertTrue(view != null, "a two-word header over a slice must still build");
        assertEquals(4, view.getLong(1), "two-word header Len must be the slice length");
    }

    public void testEmptyStringHasNilDataHandle() {
        freshTable();
        GoStruct view = HeaderView.build(GoString.EMPTY, stringHeaderDesc());
        assertTrue(view != null, "the empty string must produce a header view");
        assertEquals(0, view.getLong(1), "empty string Len must be 0");
        assertEquals(0, (Long) view.getObject(0),
                "empty string Data must be handle 0 (gc: StringData(\"\") == nil)");
    }

    public void testNullReferentIsZeroHeader() {
        freshTable();
        GoStruct view = HeaderView.build(null, sliceHeaderDesc());
        assertTrue(view != null, "a null referent must produce the zero header");
        assertEquals(0, view.getLong(1), "zero header Len must be 0");
        assertEquals(0, view.getLong(2), "zero header Cap must be 0");
        assertNull(view.getObject(0), "zero header Data must be unset (handle 0)");
    }

    public void testCellWrappedStringUnwraps() {
        freshTable();
        GoString gs = GoString.fromUtf8("hi");
        GoStruct view = HeaderView.build(new GoBox(gs), stringHeaderDesc());
        assertTrue(view != null, "a cell holding a string must produce a header view");
        assertEquals(2, view.getLong(1), "cell-wrapped string Len must be 2");
    }

    public void testUintptrDataWordIsLongsCarried() {
        freshTable();
        GoString gs = GoString.fromUtf8("hello");
        GoStructDescriptor desc = GoStructDescriptor.layout("test.UintptrHeader",
                List.of("Data", "Len"),
                List.of(GoKind.UINT64, GoKind.INT64));
        GoStruct view = HeaderView.build(gs, desc);
        assertTrue(view != null, "a uintptr-Data header must build");
        assertEquals(UnsafeOps.stringData(gs), view.getLong(0),
                "uintptr Data word must carry the data handle in longs");
        assertEquals(5, view.getLong(1), "uintptr header Len must be 5");
    }

    public void testPointerTypedDataWord() {
        freshTable();
        GoString gs = GoString.fromUtf8("hello");
        GoStructDescriptor desc = GoStructDescriptor.layout("test.PointerHeader",
                List.of("Data", "Len"),
                List.of(GoKind.POINTER, GoKind.INT64));
        GoStruct view = HeaderView.build(gs, desc);
        assertTrue(view != null, "a pointer-typed Data header must build");
        Object data = view.getObject(0);
        assertTrue(data instanceof GoPointer gp && gp.referent() == gs,
                "pointer-typed Data must be a GoPointer to the string value");
        assertEquals(5, view.getLong(1), "pointer header Len must be 5");
    }

    public void testNonHeaderReferentsGetNoView() {
        freshTable();
        GoStruct structReferent = GoStruct.zero(stringHeaderDesc());
        assertNull(HeaderView.build(structReferent, stringHeaderDesc()),
                "a struct referent must NOT be reinterpreted as a header");
        assertNull(HeaderView.build(Long.valueOf(42), stringHeaderDesc()),
                "a scalar referent must NOT be reinterpreted as a header");
        assertNull(HeaderView.build(new int[4], stringHeaderDesc()),
                "a bare array referent must NOT be reinterpreted as a header");
    }

    public void testWrongShapeDescriptorsGetNoView() {
        freshTable();
        GoString gs = GoString.fromUtf8("hello");
        GoStructDescriptor fourFields = GoStructDescriptor.layout("test.Four",
                List.of("Data", "Len", "Cap", "Extra"),
                List.of(GoKind.UNSAFE_POINTER, GoKind.INT64, GoKind.INT64, GoKind.INT64));
        assertNull(HeaderView.build(gs, fourFields),
                "a four-field descriptor is not a header shape");
        GoStructDescriptor stringLen = GoStructDescriptor.layout("test.StringLen",
                List.of("Data", "Len"),
                List.of(GoKind.UNSAFE_POINTER, GoKind.STRING));
        assertNull(HeaderView.build(gs, stringLen),
                "a non-integer Len field is not a header shape");
    }
}
