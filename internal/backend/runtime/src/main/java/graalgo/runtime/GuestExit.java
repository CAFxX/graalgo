package graalgo.runtime;

import java.util.concurrent.atomic.AtomicInteger;

/**
 * Guest-initiated process exit ({@code os.Exit}, {@code sync.throw},
 * {@code sync.fatal}) as a host control-flow signal.
 *
 * <p>In single-run mode a guest exit is a real process exit: {@link
 * #exit} calls {@link System#exit} exactly as before. In batch mode
 * ({@code graalgo-run --batch}), one entry's exit must not kill the
 * whole JVM — the batch loop traps it, records the entry's exit code,
 * and continues with the next entry. The launcher arms the trap with
 * {@link #beginTrap} around each entry and disarms it with {@link
 * #endTrap} afterwards; while armed, {@link #exit} records the code and
 * throws instead of exiting.
 *
 * <p>Why an {@link Error}, not an exception: the launcher's
 * {@code runOne} catches {@code RuntimeException} for execution
 * failures, so a {@code RuntimeException} trap would be misreported as
 * exit 1 before the batch loop ever saw the guest's code. An
 * {@code Error} propagates through {@code runOne} untouched to the
 * batch loop's catch. It is also deliberately not a Truffle
 * {@code ControlFlowException}: a guest exit is not guest control flow
 * (no defers run, nothing recovers it), it is the end of the entry.
 *
 * <p>The recorded code ({@link #trappedCode}) is visible even when the
 * exit is raised off the entry thread (a lingering goroutine): the
 * throwing thread dies, but the batch loop still observes the code and
 * reports it for the entry in flight. That attribution is best-effort:
 * the batch contract assumes entries do not leak live goroutines past
 * {@code main}'s return — under gc the process would be dead anyway,
 * so an exit racing into the next entry has no meaningful owner.
 */
public final class GuestExit extends Error {
    private final int code;

    /** True while a batch entry is running under a trap. */
    private static volatile boolean trapActive;

    /** Code recorded by the most recent trapped exit, or -1 if none. */
    private static final AtomicInteger trappedCode = new AtomicInteger(-1);

    private GuestExit(int code) {
        super("guest exit(" + code + ")");
        this.code = code;
    }

    /** The guest's exit code. */
    public int code() {
        return code;
    }

    /** Whether a batch trap is currently armed. */
    public static boolean trapActive() {
        return trapActive;
    }

    /** The code recorded by the most recent trapped exit, or -1 if none. */
    public static int trappedCode() {
        return trappedCode.get();
    }

    /**
     * Arms the trap for one batch entry: clears any code recorded by a
     * previous entry, then makes {@link #exit} throw instead of exiting.
     */
    public static void beginTrap() {
        trappedCode.set(-1);
        trapActive = true;
    }

    /** Disarms the trap. The recorded code (if any) is left for the caller to read. */
    public static void endTrap() {
        trapActive = false;
    }

    /**
     * Exits the guest program with the given code: throws this error
     * under a batch trap, otherwise exits the JVM process (the
     * pre-existing single-run behavior).
     */
    public static void exit(int code) {
        if (trapActive) {
            trappedCode.set(code);
            throw new GuestExit(code);
        }
        System.exit(code);
    }
}
