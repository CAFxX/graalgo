package graalgo.runtime;

import graalgo.values.FuncIds;
import graalgo.values.GoFunc;

/**
 * The opaque handle behind a Go {@code *runtime.Func} value: the
 * (qualified) function name and its interned entry PC.
 */
public final class GoFuncInfo {
    private final String funcName;
    private final long entry;

    public GoFuncInfo(String funcName, long entry) {
        this.funcName = funcName;
        this.entry = entry;
    }

    /** The qualified function name, e.g. {@code main.main}. */
    public String funcName() {
        return funcName;
    }

    /** The interned entry PC, or 0 when unknown. */
    public long entry() {
        return entry;
    }

    /**
     * Builds the handle for a PC's function, or null when the PC is unknown.
     *
     * <p>Two disjoint id spaces feed this lookup, because gc has one
     * address space and graalgo has none:
     * <ol>
     *   <li>the dense {@link StackTrace}-interned PCs from
     *       {@code runtime.Callers}/{@code runtime.Caller} (ids from 1);
     *   <li>the opaque entry ids minted by {@link FuncIds} for the
     *       {@code internal/abi} {@code FuncPCABI0}/{@code FuncPCABIInternal}
     *       intrinsics (ids from {@code 0x1000}) — the values
     *       {@code reflect.Value.Pointer()} returns for funcs.
     * </ol>
     * Both are consulted so {@code runtime.FuncForPC} resolves PCs from
     * either producer; without the second lookup a reflect-derived PC
     * yields Go nil and a later {@code (*Func).Name()} nil-derefs.
     * The {@code StackTrace} space wins on the (pathological) numeric
     * collision of the two spaces, preserving the pre-existing behavior
     * for every PC it knows.
     */
    public static GoFuncInfo forPc(long pc) {
        StackTrace.PcInfo info = StackTrace.lookup(pc);
        if (info != null) {
            return new GoFuncInfo(StackTrace.qualifyFuncName(info.funcName()),
                    StackTrace.entryFor(info.funcName()));
        }
        GoFunc fn = FuncIds.funcFor(pc);
        if (fn != null) {
            // Root node names are the wire function names: GoFuncRootNode,
            // LinknameRootNode and UnimplementedRootNode all override
            // getName() accordingly. Anything else (e.g. a synthetic
            // reflect-method wrapper with an anonymous root) falls back to
            // Truffle's default class-based name — still a valid, non-nil
            // answer, which is what matters for FuncForPC's contract.
            String wireName = fn.target().getRootNode().getName();
            if (wireName == null || wireName.isEmpty()) {
                wireName = fn.target().getRootNode().toString();
            }
            return new GoFuncInfo(StackTrace.qualifyFuncName(wireName),
                    StackTrace.entryFor(wireName));
        }
        return null;
    }
}
