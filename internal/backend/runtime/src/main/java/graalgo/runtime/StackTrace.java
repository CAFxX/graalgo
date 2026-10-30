package graalgo.runtime;

import com.oracle.truffle.api.source.SourceSection;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicLong;

/**
 * Guest stack-trace support for the synthetic {@code runtime} package's
 * stack-inspection family ({@code Callers}, {@code Caller},
 * {@code CallersFrames}, {@code FuncForPC}).
 *
 * <p>graalgo has no machine code, so there are no real program counters.
 * Each (function, file, line) site is interned to a dense {@code long} id;
 * the ids are opaque — exactly gc's contract ("Iterating over the returned
 * slice of PCs directly is discouraged", and {@code CallersFrames}
 * accounts for inlining/adjustment which do not exist here). The ids are
 * only meaningful to {@code CallersFrames} and {@code FuncForPC}, which
 * resolve them through {@link #lookup}.
 *
 * <p>Known divergences from gc, documented: frame function names are the
 * unqualified wire names (the wire carries no package path; {@code main}
 * is qualified to {@code main.main} like the panic traceback in
 * {@link Intrinsics#renderGoStack}), and there are no pc offsets or
 * inlined frames.
 */
public final class StackTrace {
    private StackTrace() {}

    /** Symbolic info for one interned PC. */
    public record PcInfo(String funcName, String file, int line) {}

    /** One captured Go frame: call-site attribution plus definition site. */
    public record FrameData(String funcName, String file, int line,
                            String defFile, int defLine) {}

    private record Key(String funcName, String file, int line) {}

    private record DefInfo(String file, int line) {}

    private static final ConcurrentHashMap<Key, Long> INTERN = new ConcurrentHashMap<>();
    private static final List<PcInfo> TABLE = new ArrayList<>();
    private static final AtomicLong NEXT_ID = new AtomicLong(1);
    private static final Object INTERN_LOCK = new Object();
    private static final ConcurrentHashMap<String, DefInfo> FUNC_DEFS = new ConcurrentHashMap<>();

    static {
        TABLE.add(null); // id 0 is never valid
    }

    /**
     * Interns a (function, file, line) site, returning its dense PC id
     * (stable within the process).
     */
    public static long intern(String funcName, String file, int line) {
        Key k = new Key(funcName, file, line);
        Long id = INTERN.get(k);
        if (id != null) {
            return id;
        }
        synchronized (INTERN_LOCK) {
            id = INTERN.get(k);
            if (id != null) {
                return id;
            }
            long nid = NEXT_ID.getAndIncrement();
            INTERN.put(k, nid);
            while (TABLE.size() <= nid) {
                TABLE.add(null);
            }
            TABLE.set((int) nid, new PcInfo(funcName, file, line));
            return nid;
        }
    }

    /** Resolves an interned PC id, or null when unknown. */
    public static PcInfo lookup(long pc) {
        if (pc <= 0) {
            return null;
        }
        synchronized (INTERN_LOCK) {
            if (pc >= TABLE.size()) {
                return null;
            }
            return TABLE.get((int) pc);
        }
    }

    /** Records a function's definition site (first write wins). */
    public static void recordFuncDef(String funcName, String file, int line) {
        FUNC_DEFS.putIfAbsent(funcName, new DefInfo(file, line));
    }

    /**
     * Clears all per-run state: the intern table, the PC table, and the
     * function-definition sites. Called on the program boundary (from
     * {@code ReflectTypes.install}, which every launcher runs per build)
     * so a later program in the same JVM never observes an earlier
     * program's interning. Without this, tokens/PCs restart per program
     * while this state does not: every batch entry defines
     * {@code main.main}, so first-write-wins {@code FUNC_DEFS} made
     * entry 2's {@code Func.Entry()} report entry 1's file/line, and
     * stale interned PCs could resolve to another program's sites.
     * PCs are only meaningful within the run that interned them, so
     * clearing them at the boundary invalidates nothing live: no
     * entry's execution has started yet when its install runs.
     */
    public static void resetForRun() {
        synchronized (INTERN_LOCK) {
            INTERN.clear();
            TABLE.clear();
            TABLE.add(null); // id 0 is never valid
            NEXT_ID.set(1);
        }
        FUNC_DEFS.clear();
    }

    /**
     * The entry PC for the named function (its definition site), or 0 when
     * unknown. gc: "Entry ... may be zero if not known".
     */
    public static long entryFor(String funcName) {
        DefInfo d = FUNC_DEFS.get(funcName);
        if (d == null) {
            return 0;
        }
        return intern(funcName, d.file(), d.line());
    }

    /**
     * Qualifies a wire function name the way gc names appear in traces:
     * {@code main} becomes {@code main.main} (same rule as
     * {@link Intrinsics#renderGoStack}); every other name is already the
     * bare declared name and stays unqualified (the wire carries no
     * package path — documented divergence).
     */
    public static String qualifyFuncName(String name) {
        return "main".equals(name) ? "main.main" : name;
    }

    private static String sectionFile(SourceSection sec) {
        if (sec == null || sec.getSource() == null) {
            return "";
        }
        String name = sec.getSource().getName();
        return name != null ? name : "";
    }

    private static int sectionLine(SourceSection sec) {
        return sec != null ? sec.getStartLine() : 0;
    }

    /**
     * Captures the current goroutine's Go frames, innermost first. Each
     * frame's (file, line) is its call-site attribution: the location
     * within that frame's function where it invoked the next-inner frame
     * (maintained by {@link GoroutineState#updateTopFrameSection}; the
     * intrinsic wire nodes update the innermost frame to their own call
     * site before capturing, mirroring {@code FuncValueCallNode}).
     * Function definition sites are recorded for {@link #entryFor}.
     */
    public static List<FrameData> capture() {
        List<FrameData> out = new ArrayList<>();
        GoroutineState state = GoroutineState.current();
        if (state == null) {
            return out;
        }
        GoStackFrame f = state.goStack();
        while (f != null && !f.isEmpty()) {
            SourceSection sec = f.section();
            SourceSection def = f.defSection();
            String defFile = sectionFile(def);
            int defLine = sectionLine(def);
            if (!defFile.isEmpty() || defLine != 0) {
                recordFuncDef(f.funcName(), defFile, defLine);
            }
            out.add(new FrameData(f.funcName(), sectionFile(sec), sectionLine(sec),
                    defFile, defLine));
            f = f.parent();
        }
        return out;
    }

    /**
     * Updates the innermost frame's section to the given call site, then
     * captures. Every stack-inspection intrinsic calls this (instead of
     * capturing directly) so the innermost recorded PC attributes the
     * intrinsic's own call site — the analogue of what
     * {@code FuncValueCallNode} does for real calls.
     */
    public static List<FrameData> captureAt(SourceSection callSite) {
        GoroutineState state = GoroutineState.current();
        if (state != null) {
            state.updateTopFrameSection(callSite);
        }
        return capture();
    }
}
