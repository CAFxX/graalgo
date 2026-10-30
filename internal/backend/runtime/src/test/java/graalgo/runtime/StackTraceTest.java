package graalgo.runtime;

/**
 * Regression tests for review 2026-10-30-21, batch-mode state leak (P1):
 * FUNC_DEFS was first-write-wins and never reset, so in a multi-program
 * JVM every batch entry defines {@code main.main} and entry 2's
 * {@code runtime.Func.Entry()} / {@code CallersFrames} reported entry 1's
 * file/line. {@link StackTrace#resetForRun} must clear definition sites
 * (and the intern table) on the program boundary.
 *
 * <p>Each test resets at start and end so it neither depends on nor
 * pollutes other tests sharing this JVM.
 */
public final class StackTraceTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    public void testFuncDefResetAcrossRuns() {
        StackTrace.resetForRun();
        try {
            StackTrace.recordFuncDef("main.main", "first.go", 10);
            long pc1 = StackTrace.entryFor("main.main");
            assertTrue(pc1 != 0, "entryFor should resolve after recordFuncDef");
            StackTrace.PcInfo info1 = StackTrace.lookup(pc1);
            assertTrue(info1 != null && "first.go".equals(info1.file()) && info1.line() == 10,
                    "first entry should resolve to first.go:10, got " + info1);
            StackTrace.resetForRun();
            assertTrue(StackTrace.entryFor("main.main") == 0,
                    "entryFor should be unknown after resetForRun");
            StackTrace.recordFuncDef("main.main", "second.go", 20);
            long pc2 = StackTrace.entryFor("main.main");
            StackTrace.PcInfo info2 = StackTrace.lookup(pc2);
            assertTrue(info2 != null && "second.go".equals(info2.file()) && info2.line() == 20,
                    "second entry should resolve to second.go:20, got " + info2);
        } finally {
            StackTrace.resetForRun();
        }
    }

    public void testInternTableResetAcrossRuns() {
        StackTrace.resetForRun();
        try {
            long pc = StackTrace.intern("main.main", "first.go", 10);
            assertTrue(StackTrace.lookup(pc) != null, "interned PC should resolve");
            StackTrace.resetForRun();
            assertTrue(StackTrace.lookup(pc) == null,
                    "interned PC from a previous run must not resolve after resetForRun");
            long pc2 = StackTrace.intern("main.main", "second.go", 20);
            StackTrace.PcInfo info = StackTrace.lookup(pc2);
            assertTrue(info != null && "second.go".equals(info.file()) && info.line() == 20,
                    "re-intern after reset should resolve to second.go:20, got " + info);
        } finally {
            StackTrace.resetForRun();
        }
    }
}
