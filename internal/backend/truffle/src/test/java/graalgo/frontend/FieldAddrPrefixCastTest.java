package graalgo.frontend;

import java.util.List;

import com.oracle.truffle.api.frame.VirtualFrame;

import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Pin for review finding F1 (2026-11-01 codebase review): the write path
 * ({@link FieldAddrNode} for {@code &s.f}, {@link SelectorWriteNode} for
 * {@code s.f = v}) has none of the embedding-prefix descent the read path
 * ({@link SelectorNode}, fix-redmaster-r53) has, so a direct
 * {@code *Type}-compiled field write/address against a wider abi kind
 * shell addresses the shell's colliding field instead of the embedded
 * header's field.
 *
 * <p>VERDICT (F1 lane, 2026-11-01): the defect is REAL at node level
 * (see {@link #testDirectTypeFieldWriteOnMapShellKnownLimitation}), but
 * UNREACHABLE through the frontend in the current tree, so no fix is
 * landed here — this test pins the non-reachability contract instead of
 * adding speculative code. The proof, in full:
 *
 * <ol>
 *   <li>Direct {@code Type}-field access requires code in package
 *       {@code internal/abi}: every {@code Type} field ({@code Kind_},
 *       {@code Size_}, ...) is unexported, and the frontend's selpaths
 *       come from go/types, which enforces that boundary. A
 *       {@code *Type}-compiled write/address can therefore only be
 *       emitted for code inside {@code internal/abi} itself.</li>
 *   <li>{@code internal/abi} (go1.27.1, all non-test {@code *.go})
 *       contains zero field-selection assignments, zero
 *       address-of-field expressions, and zero inc/dec operations on
 *       any field: its methods are pure readers (plus
 *       {@code RegArgs}/{@code IntArgRegBitmap} helpers on unrelated
 *       types). There is no writer to descend for.</li>
 *   <li>The packages that hold {@code *Type}-typed shell pointers
 *       ({@code internal/reflectlite}, real {@code reflect} via
 *       {@code toRType}, {@code hash/maphash},
 *       {@code internal/sync/hashtriemap}) only READ through
 *       {@code *Type}; their writes are to their own structs, to
 *       freshly allocated kind structs via PROMOTED paths
 *       ({@code [0, idx]} — the nested header is reached exactly, see
 *       {@link #testPromotedWriteOnMapShellLandsOnHeader}), or via
 *       Java linkname paths (name-based, not hops-based).</li>
 *   <li>User code cannot import {@code internal/abi}: graalgo's import
 *       allow-list blocks {@code internal/*} for user imports, and
 *       Go's own internal-package rule independently blocks it.</li>
 *   <li>User-code unsafe reinterpretations are normalized at the
 *       conversion site by {@code PrefixView} (wrap/unwrap/sibling/
 *       field-cell identity), so no user value reaches a frontend
 *       write node as a wider struct under a narrower static type.</li>
 *   <li>Writer enumeration (this pass): the only hops-walking writers
 *       are {@link FieldAddrNode} (sole emitter:
 *       {@code NodeFactory.buildFieldAddr}) and
 *       {@link SelectorWriteNode} (emitters:
 *       {@code NodeFactory.writeLvalue} — including op-assign and
 *       inc/dec write halves — and {@code NodeFactoryV2} single-field
 *       selector LHS). {@code DerefWriteNode} writes through cells
 *       minted by {@link FieldAddrNode} (downstream, not independent).
 *       The other cell minters ({@code UnsafeToPtrNode},
 *       {@code ReflectOpNode}/{@code AbiTypes}) validate by field NAME
 *       against the runtime descriptor, so they cannot mismatch.</li>
 * </ol>
 *
 * <p>WHAT THIS TEST PINS (all green on current code):
 * <ul>
 *   <li>The safe shapes: promoted {@code [0, idx]} writes and field-0
 *       addresses on a kind shell land on the nested header.</li>
 *   <li>The r53 read side at node level: a {@code *Type}-compiled
 *       {@code Kind_} read on a {@code MapType} shell returns the map
 *       kind (21), not {@code KeysOff}.</li>
 *   <li>The write-side tripwire: a direct {@code *Type}-compiled
 *       {@code Kind_} address/write on a {@code MapType} shell
 *       CURRENTLY addresses the shell's {@code KeysOff}. That
 *       assertion is NOT a statement that this behavior is correct —
 *       it is the known F1 node defect, locked in so that any future
 *       change (a reachable emitter appearing, or the shared-descent
 *       fix landing) must flip this test deliberately, in the same
 *       change, with review. If this tripwire ever needs to change
 *       because a reachable writer appears, land the shared descent
 *       (one helper in {@code SelectorNode}, used by both write nodes)
 *       and flip the tripwire to descended addressing; do not
 *       silently delete it.</li>
 * </ul>
 *
 * <p>Field orders below are GOROOT go1.27.1
 * ({@code internal/abi/type.go} for {@code Type}, 11 fields;
 * {@code internal/abi/map.go} for {@code MapType}, 12 fields — note
 * the r53 doc comment in {@code SelectorNode} says 13; the collision
 * at index 6 is unaffected by that count discrepancy).
 */
public final class FieldAddrPrefixCastTest {

    /** abi.Kind ordinal for Map (Invalid=0 … Interface=20, Map=21). */
    private static final long MAP_KIND = 21;

    /** Distinctive nonzero value for the shell's KeysOff slot. */
    private static final long KEYS_OFF = 4096;

    /** A write value that is a plausible Kind ordinal (Uint=7). */
    private static final int WRITE_KIND = 7;

    private static GoStructDescriptor typeDesc() {
        return GoStructDescriptor.layout("internal/abi.Type",
                List.of("Size_", "PtrBytes", "Hash", "TFlag", "Align_", "FieldAlign_",
                        "Kind_", "Equal", "GCData", "Str", "PtrToThis"),
                List.of(GoKind.UINT64, GoKind.UINT64, GoKind.UINT32, GoKind.UINT8,
                        GoKind.UINT8, GoKind.UINT8, GoKind.UINT8, GoKind.FUNC,
                        GoKind.POINTER, GoKind.INT32, GoKind.INT32));
    }

    private static GoStructDescriptor mapDesc() {
        return GoStructDescriptor.layout("internal/abi.MapType",
                List.of("Type", "Key", "Elem", "Group", "Hasher", "GroupSize",
                        "KeysOff", "KeyStride", "ElemsOff", "ElemStride", "ElemOff", "Flags"),
                List.of(GoKind.STRUCT, GoKind.POINTER, GoKind.POINTER, GoKind.POINTER,
                        GoKind.FUNC, GoKind.UINT64, GoKind.UINT64, GoKind.UINT64,
                        GoKind.UINT64, GoKind.UINT64, GoKind.UINT64, GoKind.UINT32));
    }

    /** A MapType shell whose nested Type header carries Kind_=Map. */
    private static GoStruct mapShell() {
        GoStruct header = GoStruct.zero(typeDesc());
        header.setLong(6, MAP_KIND);
        GoStruct shell = GoStruct.zero(mapDesc());
        shell.setObject(0, header);
        shell.setLong(6, KEYS_OFF);
        return shell;
    }

    private static GoStruct headerOf(GoStruct shell) {
        return (GoStruct) shell.getObject(0);
    }

    private static GoExprNode constBase(Object value, GoKind kind) {
        return new GoExprNode(kind) {
            @Override
            public Object executeObject(VirtualFrame frame) {
                return value;
            }
        };
    }

    private static GoExprNode constInt(int value, GoKind kind) {
        return new GoExprNode(kind) {
            @Override
            public Object executeObject(VirtualFrame frame) {
                return Integer.valueOf(value);
            }

            @Override
            public int executeInt(VirtualFrame frame) {
                return value;
            }
        };
    }

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(long want, long got, String msg) {
        if (want != got) {
            throw new AssertionError(msg + " — want " + want + ", got " + got);
        }
    }

    private static void assertSame(Object want, Object got, String msg) {
        if (want != got) {
            throw new AssertionError(msg + " — want identical object, got "
                    + (got == null ? "null" : got.getClass().getName()));
        }
    }

    /**
     * Promoted writes are safe without any descent: {@code [0, 6]} on a
     * MapType shell (the shape real reflect's MapOf/StructOf emit for
     * promoted {@code Type} fields on fresh kind structs) walks field 0
     * to the nested header and writes {@code Kind_} there; KeysOff is
     * untouched.
     */
    public void testPromotedWriteOnMapShellLandsOnHeader() {
        GoStruct shell = mapShell();
        SelectorWriteNode w = new SelectorWriteNode(
                constBase(new GoPointer(shell), GoKind.POINTER),
                constInt(WRITE_KIND, GoKind.UINT8),
                new int[]{0, 6}, GoKind.UINT8);
        w.executeVoid(null);
        assertEq(WRITE_KIND, headerOf(shell).getLong(6),
                "promoted write must land on header Kind_");
        assertEq(KEYS_OFF, shell.getLong(6),
                "promoted write must not touch shell KeysOff");

        // Same shape through &: &shell.Type.Kind_ addresses the header.
        GoStruct shell2 = mapShell();
        FieldAddrNode a = new FieldAddrNode(
                constBase(new GoPointer(shell2), GoKind.POINTER),
                new int[]{0, 6}, GoKind.UINT8);
        Object p = a.executeObject(null);
        assertTrue(p instanceof GoPointer gp && !gp.isNil(),
                "promoted & must yield a non-nil GoPointer");
        GoFieldCell cell = (GoFieldCell) ((GoPointer) p).cell();
        assertSame(headerOf(shell2), cell.ownerStruct(),
                "promoted & must address the nested header");
        assertEq(6, cell.fieldIndex(), "promoted & must address field 6");
    }

    /**
     * Field-0 address identity (the {@code rtype.common()} /
     * {@code toRType} shape): {@code &shell.Type} addresses the header
     * itself — the cell over shell field 0 reads back the header.
     */
    public void testFieldZeroAddressOnMapShellIsHeader() {
        GoStruct shell = mapShell();
        FieldAddrNode a = new FieldAddrNode(
                constBase(new GoPointer(shell), GoKind.POINTER),
                new int[]{0}, GoKind.STRUCT);
        Object p = a.executeObject(null);
        assertTrue(p instanceof GoPointer gp && !gp.isNil(),
                "&shell.Type must yield a non-nil GoPointer");
        GoFieldCell cell = (GoFieldCell) ((GoPointer) p).cell();
        assertSame(shell, cell.ownerStruct(), "&shell.Type cell owner must be the shell");
        assertEq(0, cell.fieldIndex(), "&shell.Type cell must address field 0");
        assertSame(headerOf(shell), cell.get(),
                "&shell.Type cell must read back the nested header");
    }

    /**
     * The r53 read side at node level (mirrors the Value.Clear map
     * case's root cause): a {@code *Type}-compiled {@code Kind_} read
     * (selpath [6], UINT8) on a MapType shell returns the map kind 21
     * from the nested header, not the shell's KeysOff (4096).
     */
    public void testSelectorReadOfKindOnMapShellDescends() {
        GoStruct shell = mapShell();
        SelectorNode r = new SelectorNode(
                constBase(new GoPointer(shell), GoKind.POINTER),
                new int[]{6}, GoKind.UINT8);
        assertEq(MAP_KIND, r.executeInt(null),
                "*Type-compiled Kind_ read on MapType shell must return the map kind");
    }

    /**
     * KNOWN-LIMITATION TRIPWIRE — read the class Javadoc before touching
     * this test. The write path has no kind-aware descent, so a direct
     * {@code *Type}-compiled {@code Kind_} address/write (selpath [6],
     * UINT8) on a MapType shell CURRENTLY addresses the shell's
     * {@code KeysOff} (field 6), not the header's {@code Kind_}: a
     * write through it corrupts KeysOff and leaves Kind_ unchanged.
     * This is the F1 node defect. It is unreachable through the
     * frontend today (see class Javadoc for the proof), which is why
     * this test locks in the current behavior instead of asserting a
     * fix that no reachable program can exercise. If a reachable
     * emitter ever appears, land the shared descent and flip this
     * test to descended addressing in the same change — do not delete
     * it silently.
     */
    public void testDirectTypeFieldWriteOnMapShellKnownLimitation() {
        // &t.Kind_ shape: the minted cell is over the SHELL at field 6.
        GoStruct shell = mapShell();
        FieldAddrNode a = new FieldAddrNode(
                constBase(new GoPointer(shell), GoKind.POINTER),
                new int[]{6}, GoKind.UINT8);
        Object p = a.executeObject(null);
        assertTrue(p instanceof GoPointer gp && !gp.isNil(),
                "&t.Kind_ must yield a non-nil GoPointer");
        GoFieldCell cell = (GoFieldCell) ((GoPointer) p).cell();
        assertSame(shell, cell.ownerStruct(),
                "TRIPWIRE: direct &t.Kind_ cell owner is currently the shell, not the header");
        assertEq(6, cell.fieldIndex(),
                "TRIPWIRE: direct &t.Kind_ cell field is currently 6 (KeysOff on the shell)");
        cell.set(Integer.valueOf(WRITE_KIND));
        assertEq(WRITE_KIND, shell.getLong(6),
                "TRIPWIRE: write through direct &t.Kind_ currently lands on KeysOff");
        assertEq(MAP_KIND, headerOf(shell).getLong(6),
                "TRIPWIRE: write through direct &t.Kind_ currently leaves header Kind_ unchanged");

        // t.Kind_ = v shape: same mis-addressing via SelectorWriteNode.
        GoStruct shell2 = mapShell();
        SelectorWriteNode w = new SelectorWriteNode(
                constBase(new GoPointer(shell2), GoKind.POINTER),
                constInt(WRITE_KIND, GoKind.UINT8),
                new int[]{6}, GoKind.UINT8);
        w.executeVoid(null);
        assertEq(WRITE_KIND, shell2.getLong(6),
                "TRIPWIRE: direct t.Kind_ = v currently lands on KeysOff");
        assertEq(MAP_KIND, headerOf(shell2).getLong(6),
                "TRIPWIRE: direct t.Kind_ = v currently leaves header Kind_ unchanged");
    }
}
