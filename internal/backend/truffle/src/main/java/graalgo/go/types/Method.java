package graalgo.go.types;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.RootCallTarget;
import java.util.function.Supplier;

/**
 * One entry of a type's method set (toolexec-design §3.4: the frontend emits the complete sets
 * with embedding paths; the backend never runs a method-set algorithm). Calling it through an
 * interface means adapting the interface's data word to the declared receiver — see
 * {@code graalgo.go.runtime.Receivers} — and calling {@link #target()}.
 */
public final class Method {
    /** The interface data word is the receiver. */
    public static final int IDENTITY = 0;
    /** The data word is a struct/array value; the callee gets its own copy. */
    public static final int COPY = 1;
    /** Embedded-field path and/or dereference; done by the generic adapter. */
    public static final int GENERAL = 2;

    public final String key;
    /** Embedded-field index path from the dynamic type to the declaring type. */
    public final int[] path;
    /** Whether the declared receiver is a pointer. */
    public final boolean ptrRecv;
    /** The type owning the method set (T, for both T's and *T's sets). */
    public final GoType owner;
    /** Whether this entry belongs to the pointer method set (dynamic type *T). */
    public final boolean ofPointer;
    public final int mode;
    private final Supplier<RootCallTarget> targetSupplier;
    private volatile RootCallTarget target;
    private final Supplier<GoType> sigSupplier;
    private volatile GoType sig;

    /**
     * {@code sig}: the receiver-less signature, resolved on first use; null where no
     * interface satisfaction is ever decided on the entry (a generic method's own instance).
     */
    public Method(String key, int[] path, boolean ptrRecv, GoType owner, boolean ofPointer,
            Supplier<RootCallTarget> target, Supplier<GoType> sig) {
        this.sigSupplier = sig;
        this.key = key;
        this.path = path;
        this.ptrRecv = ptrRecv;
        this.owner = owner;
        this.ofPointer = ofPointer;
        this.targetSupplier = target;
        if (path.length == 0 && ofPointer == ptrRecv) {
            this.mode = !ptrRecv && owner.carrier() == GoType.C_FLAT ? COPY : IDENTITY;
        } else {
            this.mode = GENERAL;
        }
    }

    /**
     * Whether this entry satisfies an interface method of signature {@code want}: gc's itab
     * rule (runtime/iface.go) matches name and type, so a same-named method of another
     * signature does not implement it. Type identity is reference equality (D7).
     */
    @TruffleBoundary
    public boolean hasSig(GoType want) {
        if (sigSupplier == null) {
            return true;
        }
        GoType s = sig;
        if (s == null) {
            s = sigSupplier.get();
            sig = s;
        }
        return s == want;
    }

    /** The receiver-less signature, or null for an entry no interface is ever matched against. */
    @TruffleBoundary
    public GoType sig() {
        if (sigSupplier == null) {
            return null;
        }
        GoType s = sig;
        if (s == null) {
            s = sigSupplier.get();
            sig = s;
        }
        return s;
    }

    /** The method's call target (built on first use). */
    public RootCallTarget target() {
        RootCallTarget t = target;
        return t != null ? t : resolve();
    }

    @TruffleBoundary
    private synchronized RootCallTarget resolve() {
        if (target == null) {
            target = targetSupplier.get();
        }
        return target;
    }
}
