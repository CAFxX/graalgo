package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.runtime.GoErrors;
import graalgo.runtime.Intrinsics;
import graalgo.runtime.Panic;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;

// verifies: GG-0020 (WS G builtin)
/**
 * The {@code make} builtin (WS G; verifies GG-0020).
 *
 * <p>Forms:
 * <ul>
 *   <li>{@code make([]T, len)} — slice with len and cap = len;</li>
 *   <li>{@code make([]T, len, cap)} — slice with len and cap;</li>
 *   <li>{@code make(map[K]V)} / {@code make(map[K]V, hint)} — empty map
 *       (hint sizes the table);</li>
 *   <li>{@code make(chan T)} — Phase-2 scope error (no channels in Phase 1).</li>
 * </ul>
 *
 * <p>Slice allocation uses {@link Intrinsics#makeslice}. Out-of-range
 * len/cap panic here with gc's messages (gc parity): negative len/cap or
 * len &gt; cap panic "makeslice: len out of range"; len/cap beyond
 * {@link Integer#MAX_VALUE} — which no Java array can address — panic
 * "makeslice: len/cap out of range" respectively. The range checks precede
 * the {@code (int)} narrowing so a huge len/cap can never silently wrap.
 *
 * <p>Map allocation evaluates the hint expression exactly once (gc
 * semantics) and discards the value: the hint only sizes the table and a
 * GoMap grows dynamically. Negative hints clamp to an empty map, matching
 * gc runtime/makemap — they do not panic.
 */
public final class MakeNode extends GoExprNode {
    @Child private GoExprNode lenChild;
    @Child private GoExprNode capChild; // null when absent
    @Child private GoExprNode hintChild; // map form with a hint node; null otherwise
    private final GoKind elemKind;
    private final boolean isMap;
    private final int mapHint; // legacy constant-hint form; -1 when absent
    /**
     * Element descriptor for struct kinds (P3-M9): makeslice zero-initializes
     * struct elements; null for non-struct kinds.
     */
    private final graalgo.values.GoStructDescriptor elemDesc;

    /** Slice form. */
    public MakeNode(GoKind elemKind, GoExprNode lenChild, GoExprNode capChild,
            graalgo.values.GoStructDescriptor elemDesc) {
        super(GoKind.SLICE);
        this.elemKind = elemKind;
        this.lenChild = lenChild;
        this.capChild = capChild;
        this.elemDesc = elemDesc;
        this.isMap = false;
        this.mapHint = -1;
    }

    /** Map form with a legacy constant hint (frontend v2; hint is advisory). */
    public MakeNode(int mapHint) {
        super(GoKind.MAP);
        this.elemKind = null;
        this.lenChild = null;
        this.capChild = null;
        this.hintChild = null;
        this.isMap = true;
        this.mapHint = mapHint;
        this.elemDesc = null;
    }

    /**
     * Map form with a hint expression node (frontend v1). The node is
     * executed on every evaluation: gc evaluates the hint (its side
     * effects and panics are observable) even though the value is only a
     * sizing hint. The evaluated value is discarded — a GoMap grows
     * dynamically, so the hint is unobservable except through evaluation.
     */
    public MakeNode(GoExprNode hintChild) {
        super(GoKind.MAP);
        this.elemKind = null;
        this.lenChild = null;
        this.capChild = null;
        this.hintChild = hintChild;
        this.isMap = true;
        this.mapHint = -1;
        this.elemDesc = null;
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        if (isMap) {
            if (hintChild != null) {
                // Evaluate the hint: gc evaluates it exactly once, so its
                // side effects and panics must happen even though the
                // value is discarded below. Negative hints do NOT panic:
                // gc runtime/makemap (go1.27.1) clamps hint < 0 to 0, and
                // a constant negative hint is a gc compile error, so the
                // only runtime-reachable negatives are variables, which gc
                // accepts. There is no "makemap: size out of range" panic
                // in go1.27.1.
                hintChild.executeLong(frame);
                return new GoMap();
            }
            return mapHint < 0 ? new GoMap() : new GoMap(mapHint);
        }
        long len = lenChild.executeLong(frame);
        long cap = capChild != null ? capChild.executeLong(frame) : len;
        if (len < 0 || cap < 0 || len > cap) {
            // gc: "makeslice: len out of range" panic (runtime.errorString,
            // so the header renders "panic: runtime error: ..." like gc;
            // a raw String value would degrade to "panic: (?) 0xADDR").
            Panic.throwPanic(GoErrors.makesliceLenOutOfRange(), null, null, this);
        }
        if (len > Integer.MAX_VALUE) {
            // No Java array holds more than Integer.MAX_VALUE elements,
            // so the (int) narrowing below would silently wrap (len in
            // (2^31, 2^32) wraps to a small positive value; len =
            // Integer.MAX_VALUE + 1 wraps to Integer.MIN_VALUE and the
            // allocation throws a host NegativeArraySizeException, not a
            // Go panic). gc panics "makeslice: len out of range" once len
            // exceeds its maxAlloc; below that threshold gc would
            // allocate (or fatal-OOM) where the JVM cannot represent the
            // array at all, so a Go panic is the only faithful outcome.
            Panic.throwPanic(GoErrors.makesliceLenOutOfRange(), null, null, this);
        }
        if (cap > Integer.MAX_VALUE) {
            // gc: "makeslice: cap out of range" panic (len is checked
            // before cap, matching gc's check order).
            Panic.throwPanic(GoErrors.makesliceCapOutOfRange(), null, null, this);
        }
        return Intrinsics.makeslice(elemKind, elemDesc, (int) len, (int) cap);
    }
}
