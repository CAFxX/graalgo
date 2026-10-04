package graalgo.runtime;

import graalgo.values.GoKind;
import graalgo.values.GoTypeDescriptor;

/**
 * The Go value {@code *runtime.PanicNilError} (M5, WS F; GG-0019).
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
 */
public final class PanicNilError extends GoErrorValue {
    public static final PanicNilError INSTANCE = new PanicNilError();

    /** Canonical dynamic-type descriptor for the recovered value. */
    private static final GoTypeDescriptor DESCRIPTOR =
            new GoTypeDescriptor("*runtime.PanicNilError", GoKind.POINTER, false);

    private PanicNilError() {
        super("runtime error: panic called with nil argument");
    }

    public static GoTypeDescriptor descriptor() {
        return DESCRIPTOR;
    }
}
