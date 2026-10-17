package graalgo.p3reflect;

import graalgo.frontend.FrontendException;
import graalgo.p3unsafe.UnsafeTable;
import graalgo.values.GoBox;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoString;
import graalgo.values.GoStruct;

import java.util.Map;

/**
 * Tests for the RV-P1-02 F4 allocator intrinsics
 * ({@link AllocIntrinsics}): {@code unsafe_New} / {@code unsafe_NewArray}
 * must allocate genuine zeroed, GC-managed objects with correct pointer
 * identity — never nulls, constants, or shared boxes.
 *
 * <p>Registered in {@code graalgo.iface.IfaceTestRunner}.
 */
public final class AllocIntrinsicsTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static graalgo.frontend.Json.Object parseDoc(String text) {
        graalgo.frontend.Json.Value v = graalgo.frontend.Json.parse(text);
        if (!(v instanceof graalgo.frontend.Json.Object doc)) {
            throw new AssertionError("test doc must be a JSON object");
        }
        return doc;
    }

    /** Minimal table: int, string, struct{x int; s string}, *int, []int. */
    private static void installTable() {
        String doc = """
                {"types": [
                  {"kind": "basic", "detail": {"name": "int", "class": "int", "untyped": false}},
                  {"kind": "basic", "detail": {"name": "string", "class": "string", "untyped": false}},
                  {"kind": "struct", "detail": {"fields": [
                    {"name": "x", "type": 0, "tag": "", "offset": 0},
                    {"name": "s", "type": 1, "tag": "", "offset": 8}]}},
                  {"kind": "named", "detail": {"name": "main.T", "pkgpath": "main",
                    "underlying": 2, "methods": []}},
                  {"kind": "pointer", "detail": {"elem": 0}},
                  {"kind": "slice", "detail": {"elem": 0}}
                ]}
                """;
        ReflectTypes.install(parseDoc(doc), Map.of());
        UnsafeTable.install();
    }

    private static long tokForKind(GoKind kind) {
        for (ReflectTypes.TypeInfo ti : ReflectTypes.table().byTok) {
            if (ti != null && ti.kind == kind && !ti.isNamed()) {
                return ti.tok;
            }
        }
        throw new AssertionError("no token for kind " + kind);
    }

    private static long tokForNamed(String name) {
        for (ReflectTypes.TypeInfo ti : ReflectTypes.table().byTok) {
            if (ti != null && ti.isNamed() && name.equals(ti.name)) {
                return ti.tok;
            }
        }
        throw new AssertionError("no token for named type " + name);
    }

    private static Object resolveCell(long handle) {
        Object ref = UnsafeTable.current().resolve(handle);
        assertTrue(ref instanceof GoBox,
                "unsafe_New handle should resolve to a GoBox cell, got "
                        + (ref == null ? "null" : ref.getClass().getSimpleName()));
        return ((GoBox) ref).get();
    }

    /**
     * Scalar allocation: zeroed Long, fresh identity per call (two
     * allocations mint distinct handles even though the zero box value
     * is JVM-shared).
     */
    public void testUnsafeNewScalarZeroAndIdentity() {
        installTable();
        long tok = tokForKind(GoKind.INT64);
        long h1 = AllocIntrinsics.unsafeNewForToken(tok);
        long h2 = AllocIntrinsics.unsafeNewForToken(tok);
        assertTrue(h1 != 0 && h2 != 0, "handles must be non-zero");
        assertTrue(h1 != h2, "distinct allocations must mint distinct handles");
        assertTrue(Long.valueOf(0L).equals(resolveCell(h1)),
                "int allocation must be zeroed");
        assertTrue(Long.valueOf(0L).equals(resolveCell(h2)),
                "int allocation must be zeroed");
    }

    /** Struct allocation: recursively zeroed fields (int + string). */
    public void testUnsafeNewStructZeroed() {
        installTable();
        long tok = tokForNamed("main.T");
        long h = AllocIntrinsics.unsafeNewForToken(tok);
        Object zero = resolveCell(h);
        assertTrue(zero instanceof GoStruct,
                "struct allocation should be a GoStruct, got "
                        + zero.getClass().getSimpleName());
        GoStruct s = (GoStruct) zero;
        assertTrue(s.getLong(0) == 0L, "field x must be zero");
        assertTrue(GoString.EMPTY.equals(s.getObject(1)),
                "field s must be the empty string");
    }

    /** Nil-able kinds allocate a cell holding null (the nil value). */
    public void testUnsafeNewNilableKinds() {
        installTable();
        assertTrue(resolveCell(AllocIntrinsics.unsafeNewForToken(tokForKind(GoKind.SLICE))) == null,
                "slice allocation must hold nil");
        assertTrue(resolveCell(AllocIntrinsics.unsafeNewForToken(tokForKind(GoKind.POINTER))) == null,
                "pointer allocation must hold nil");
    }

    /** String allocation: the empty string, not null. */
    public void testUnsafeNewString() {
        installTable();
        assertTrue(GoString.EMPTY.equals(
                resolveCell(AllocIntrinsics.unsafeNewForToken(tokForKind(GoKind.STRING)))),
                "string allocation must be the empty string");
    }

    /**
     * Array allocation: n zeroed elements; the handle addresses element
     * 0 via the ArrayRef element-pointer model.
     */
    public void testUnsafeNewArray() {
        installTable();
        long tok = tokForKind(GoKind.INT64);
        long h = AllocIntrinsics.unsafeNewArrayForToken(tok, 3);
        Object ref = UnsafeTable.current().resolve(h);
        assertTrue(ref instanceof UnsafeTable.ArrayRef,
                "array handle should resolve to ArrayRef, got "
                        + ref.getClass().getSimpleName());
        UnsafeTable.ArrayRef ar = (UnsafeTable.ArrayRef) ref;
        assertTrue(ar.offset() == 0, "array handle must address element 0");
        assertTrue(ar.array() instanceof long[],
                "int array backing should be long[], got "
                        + ar.array().getClass().getSimpleName());
        assertTrue(((long[]) ar.array()).length == 3, "array length must be 3");
        assertTrue(((long[]) ar.array())[2] == 0L, "array elements must be zeroed");
        // Element-pointer round trip: (*T)(handle) addresses element 0.
        GoPointer p = UnsafeTable.pointerFor(ref);
        assertTrue(p.referent() instanceof long[], "element pointer referent");
    }

    /** Negative length fails loud (gc panics). */
    public void testUnsafeNewArrayNegativeFailsLoud() {
        installTable();
        long tok = tokForKind(GoKind.INT64);
        try {
            AllocIntrinsics.unsafeNewArrayForToken(tok, -1);
        } catch (FrontendException e) {
            return;
        }
        throw new AssertionError("negative length should fail loud");
    }

    /** Unknown token fails loud — never a mistyped allocation. */
    public void testUnsafeNewUnknownTokenFailsLoud() {
        installTable();
        try {
            AllocIntrinsics.unsafeNewForToken(999999L);
        } catch (IllegalArgumentException e) {
            return;
        }
        throw new AssertionError("unknown token should fail loud");
    }

    /** Test carrier implementing the *abi.Type contract. */
    private static final class TestCarrier implements AllocIntrinsics.AbiTypeCarrier {
        private final long tok;

        TestCarrier(long tok) {
            this.tok = tok;
        }

        @Override
        public long tok() {
            return tok;
        }
    }

    /**
     * The intrinsic entry point accepts a GoPointer over a carrier
     * (the *abi.Type D5 shape) and returns the unsafe.Pointer handle.
     */
    public void testUnsafeNewEntryPointViaCarrier() {
        installTable();
        long tok = tokForKind(GoKind.INT64);
        Object result = AllocIntrinsics.unsafeNew(new GoPointer(new TestCarrier(tok)));
        assertTrue(result instanceof Long,
                "unsafe_New must return a Long handle, got "
                        + result.getClass().getSimpleName());
        assertTrue(Long.valueOf(0L).equals(resolveCell((Long) result)),
                "entry-point allocation must be zeroed");
    }

    /**
     * A non-carrier *abi.Type argument fails loud with the prerequisite
     * message — never a silent wrong allocation.
     */
    public void testUnsafeNewNonCarrierFailsLoud() {
        installTable();
        try {
            AllocIntrinsics.unsafeNew(new GoPointer(new Object()));
        } catch (FrontendException e) {
            assertTrue(e.getMessage().contains("unsafe_New"),
                    "message should name unsafe_New: " + e.getMessage());
            return;
        }
        throw new AssertionError("non-carrier argument should fail loud");
    }
}
