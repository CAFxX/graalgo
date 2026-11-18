package graalgo.runtime;

/**
 * The Go value {@code *runtime.PanicNilError} (M5, WS F; GG-0070).
 *
 * <p>Since Go 1.21, {@code panic(nil)} — and panicking with a nil interface
 * value — produces a {@code *PanicNilError} instead of a nil panic value
 * (runtime/panic.go: {@code gopanic} converts {@code e == nil};
 * {@code (*PanicNilError).Error()} returns
 * {@code "runtime error: panic called with nil argument"}). {@code recover()}
 * therefore returns a non-nil value, and the crash prints
 * {@code panic: runtime error: panic called with nil argument}.
 *
 * <p>Parity stance (GG-0045, freeze-stub.md): Phase 1 replicates the gc
 * default (≥1.21) unconditionally. The {@code GODEBUG=panicnil=1} legacy mode
 * is out of scope, as are per-file {@code -lang} gates (those arrive with
 * WS G, GG-0013).
 *
 * <p>The value carries no v1 type descriptor (layering gate,
 * check_layering.py): the v1 path's canonical descriptor lives in
 * {@link RuntimeErrorTypes#panicNilDescriptor()}.
 */
public final class PanicNilError extends GoErrorValue {
    public static final PanicNilError INSTANCE = new PanicNilError();

    private PanicNilError() {
        // P0-fmt-016: gc's %T is *runtime.PanicNilError; %#v is
        // &runtime.PanicNilError{_:[0]*runtime.PanicNilError{}}; %p is the
        // (erased) address.
        super("runtime error: panic called with nil argument", false,
                "*runtime.PanicNilError",
                "&runtime.PanicNilError{_:[0]*runtime.PanicNilError{}}", null);
    }
}
