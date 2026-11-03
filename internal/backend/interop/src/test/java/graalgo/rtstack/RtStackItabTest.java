package graalgo.rtstack;

import java.nio.charset.StandardCharsets;
import java.util.List;

import graalgo.iface.IfaceItab;
import graalgo.iface.IfaceMethodTable;
import graalgo.iface.IfaceMethodTarget;
import graalgo.runtime.GoCalls;
import graalgo.runtime.GoFuncInfo;
import graalgo.runtime.PanicException;
import graalgo.values.GoFunc;
import graalgo.values.GoPointer;
import graalgo.values.GoString;
import graalgo.values.GoStruct;

/**
 * Tests for {@link RtStackItab} (REVIEW-2026-11-03-10 P1-2): the
 * {@code *runtime.Func} interface method-table entries. The end-to-end
 * shape (interface dispatch from guest code) is covered by
 * test/regression/funcname under graalgo; these tests pin the table
 * installation and the synthetic targets' semantics directly.
 */
public final class RtStackItabTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(Object expected, Object actual, String msg) {
        if (!expected.equals(actual)) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    private static GoPointer funcPointer(String name, long entry) {
        return new GoPointer(new GoFuncInfo(name, entry));
    }

    private static String goString(GoString s) {
        return new String(s.bytes(), StandardCharsets.UTF_8);
    }

    /**
     * install() registers Name/Entry/FileLine under "*runtime.Func" and
     * the itab builds for an interface listing those methods.
     */
    public void testInstallBuildsItab() {
        try {
            RtStackItab.install();
            IfaceMethodTarget name = IfaceMethodTable.lookup("*runtime.Func", "Name");
            assertTrue(name != null && name.func() != null, "Name target installed");
            assertTrue(name.isDirect(), "Name target is direct (no receiver adaptation)");
            IfaceItab itab = IfaceMethodTable.itabFor("*runtime.Func", 7,
                    List.of("Name", "Entry", "FileLine"));
            assertEquals(3, itab.methods().size(), "itab carries all three methods");
        } finally {
            IfaceMethodTable.clear();
        }
    }

    /** Name/Entry targets return the handle's name and entry PC. */
    public void testNameAndEntryTargets() {
        try {
            RtStackItab.install();
            GoFunc nameFunc = IfaceMethodTable.lookup("*runtime.Func", "Name").func();
            GoFunc entryFunc = IfaceMethodTable.lookup("*runtime.Func", "Entry").func();
            GoPointer recv = funcPointer("main.main", 12345L);
            Object name = GoCalls.invoke(nameFunc, new Object[]{recv});
            assertTrue(name instanceof GoString, "Name returns a GoString, got " + name);
            assertEquals("main.main", goString((GoString) name), "Name value");
            Object entry = GoCalls.invoke(entryFunc, new Object[]{recv});
            assertEquals(12345L, ((Number) entry).longValue(), "Entry value");
        } finally {
            IfaceMethodTable.clear();
        }
    }

    /**
     * FileLine target returns a pair struct with the (string, int) layout
     * the interface desugar extracts by index: string in objects[0],
     * int in longs[0]. An unknown PC yields ("", 0), like the direct
     * wire node.
     */
    public void testFileLineTargetPairLayout() {
        try {
            RtStackItab.install();
            GoFunc fileLineFunc = IfaceMethodTable.lookup("*runtime.Func", "FileLine").func();
            GoPointer recv = funcPointer("main.main", 12345L);
            Object pair = GoCalls.invoke(fileLineFunc, new Object[]{recv, Long.valueOf(0L)});
            assertTrue(pair instanceof GoStruct, "FileLine returns a GoStruct pair, got " + pair);
            GoStruct s = (GoStruct) pair;
            assertEquals("", goString((GoString) s.getObject(0)), "unknown PC file");
            assertEquals(0L, s.getLong(1), "unknown PC line");
        } finally {
            IfaceMethodTable.clear();
        }
    }

    /** A nil receiver raises the Go nil-dereference panic, like a direct call. */
    public void testNilReceiverPanics() {
        try {
            RtStackItab.install();
            GoFunc nameFunc = IfaceMethodTable.lookup("*runtime.Func", "Name").func();
            try {
                GoCalls.invoke(nameFunc, new Object[]{null});
                throw new AssertionError("nil receiver must panic");
            } catch (PanicException expected) {
                // The Go nil-dereference panic, not a Java failure.
            }
        } finally {
            IfaceMethodTable.clear();
        }
    }
}
