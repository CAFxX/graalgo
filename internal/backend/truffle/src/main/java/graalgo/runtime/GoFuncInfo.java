package graalgo.runtime;

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

    /** Builds the handle for an interned PC's function, or null when the PC is unknown. */
    public static GoFuncInfo forPc(long pc) {
        StackTrace.PcInfo info = StackTrace.lookup(pc);
        if (info == null) {
            return null;
        }
        return new GoFuncInfo(StackTrace.qualifyFuncName(info.funcName()),
                StackTrace.entryFor(info.funcName()));
    }
}
