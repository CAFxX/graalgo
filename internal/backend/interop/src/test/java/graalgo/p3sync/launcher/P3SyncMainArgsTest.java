package graalgo.p3sync.launcher;

import java.util.List;

/**
 * Unit tests for P3SyncMain.parseArgs (V12-P1-4): the argv parsing
 * contract, without executing a program. Pins the --tags flag parsing
 * and that the pre-existing flags keep their behavior after the
 * parseArgs extraction from run().
 */
public final class P3SyncMainArgsTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(Object expected, Object actual, String msg) {
        if (!expected.equals(actual)) {
            throw new AssertionError(msg + ": expected <" + expected + "> but was <" + actual + ">");
        }
    }

    private static <T extends Throwable> T assertThrows(Class<T> type, Runnable r, String msg) {
        try {
            r.run();
        } catch (Throwable t) {
            if (type.isInstance(t)) {
                return type.cast(t);
            }
            throw new AssertionError(msg + ": threw " + t + " instead of " + type.getSimpleName());
        }
        throw new AssertionError(msg + ": no exception thrown");
    }

    public void testTagsParsed() {
        P3SyncMain.ParsedArgs p = P3SyncMain.parseArgs(new String[] {"--tags=foo bar", "a.go"});
        assertEquals("foo bar", p.buildTags(), "buildTags");
        assertEquals(List.of("a.go"), p.files(), "files");
    }

    public void testTagsEmptyRejected() {
        P3SyncMain.UsageError e = assertThrows(P3SyncMain.UsageError.class,
                () -> P3SyncMain.parseArgs(new String[] {"--tags="}),
                "empty --tags");
        assertEquals("--tags must not be empty", e.getMessage(), "message");
    }

    public void testTagsAndTestPackageDirCombine() {
        P3SyncMain.ParsedArgs p = P3SyncMain.parseArgs(new String[] {
                "--test-package-dir=/some/pkg", "--tags=foo", "--check", "a.go"});
        assertEquals("foo", p.buildTags(), "buildTags");
        assertEquals("/some/pkg", p.testPackageDir(), "testPackageDir");
        assertTrue(p.check(), "check");
    }

    public void testUnknownFlagRejected() {
        P3SyncMain.UsageError e = assertThrows(P3SyncMain.UsageError.class,
                () -> P3SyncMain.parseArgs(new String[] {"--bogus"}),
                "unknown flag");
        assertEquals("unknown flag: --bogus", e.getMessage(), "message");
    }

    public void testNoInputFile() {
        P3SyncMain.UsageError e = assertThrows(P3SyncMain.UsageError.class,
                () -> P3SyncMain.parseArgs(new String[] {}),
                "no input file");
        assertEquals("no input file", e.getMessage(), "message");
    }

    public void testHelpRequested() {
        assertThrows(P3SyncMain.HelpRequested.class,
                () -> P3SyncMain.parseArgs(new String[] {"--help"}),
                "--help");
        assertThrows(P3SyncMain.HelpRequested.class,
                () -> P3SyncMain.parseArgs(new String[] {"-h"}),
                "-h");
    }

    public void testWireInputCannotCombineWithFiles() {
        P3SyncMain.UsageError e = assertThrows(P3SyncMain.UsageError.class,
                () -> P3SyncMain.parseArgs(new String[] {"--wire-input=x.json", "a.go"}),
                "wire-input + files");
        assertEquals("--wire-input cannot be combined with input files", e.getMessage(), "message");
    }

    public void testProgramArgsAfterDashDash() {
        P3SyncMain.ParsedArgs p = P3SyncMain.parseArgs(new String[] {"a.go", "--", "-x", "y"});
        assertEquals(List.of("a.go"), p.files(), "files");
        assertEquals(List.of("-x", "y"), p.goArgs(), "goArgs");
    }

    public void testGmpValidation() {
        P3SyncMain.UsageError e = assertThrows(P3SyncMain.UsageError.class,
                () -> P3SyncMain.parseArgs(new String[] {"--gmp=0"}),
                "gmp=0");
        assertEquals("--gmp must be >= 1", e.getMessage(), "message");
    }
}
