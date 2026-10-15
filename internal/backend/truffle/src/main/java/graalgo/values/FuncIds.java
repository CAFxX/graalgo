package graalgo.values;

import com.oracle.truffle.api.RootCallTarget;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicLong;

/**
 * Opaque stable function ids for the {@code internal/abi}
 * {@code FuncPCABI0}/{@code FuncPCABIInternal} compile intrinsics
 * (real-reflect Phase 1, the {@code "abifuncpc"} wire node).
 *
 * <p>gc's intrinsics return the entry PC of a func value; graalgo has no
 * PCs, so the frontend lowers them to an id minted here. The id identifies
 * the <em>function</em> (keyed by {@link RootCallTarget}, so every closure
 * of the same function maps to the same id, matching gc's entry-PC
 * semantics), is stable for the life of the process, and is non-zero for
 * non-nil funcs (0 is reserved for the nil func value).
 *
 * <p>The reverse mapping ({@link #funcFor}) exists for the Phase-4
 * {@code runtime.reflectcall} linkname, which dispatches
 * {@code MakeFunc}/{@code methodValue} stub ids back to executable
 * {@link GoFunc}s.
 */
public final class FuncIds {
    private FuncIds() {}

    /** First minted id; 0 is reserved for nil. */
    private static final long FIRST_ID = 0x1000L;

    private static final ConcurrentHashMap<RootCallTarget, Long> TARGET_TO_ID =
            new ConcurrentHashMap<>();
    private static final ConcurrentHashMap<Long, GoFunc> ID_TO_FUNC =
            new ConcurrentHashMap<>();
    private static final AtomicLong NEXT = new AtomicLong(FIRST_ID);

    /**
     * Returns the stable id for {@code fn}'s function, minting one on
     * first use. A nil func value maps to 0.
     */
    public static long idFor(GoFunc fn) {
        if (fn == null) {
            return 0L;
        }
        RootCallTarget target = fn.target();
        Long existing = TARGET_TO_ID.get(target);
        if (existing != null) {
            return existing;
        }
        long id = NEXT.getAndIncrement();
        Long prev = TARGET_TO_ID.putIfAbsent(target, id);
        if (prev != null) {
            return prev;
        }
        ID_TO_FUNC.put(id, fn);
        return id;
    }

    /**
     * Resolves an id minted by {@link #idFor} back to a {@link GoFunc},
     * or null for 0 / unknown ids.
     */
    public static GoFunc funcFor(long id) {
        if (id == 0L) {
            return null;
        }
        return ID_TO_FUNC.get(id);
    }
}
