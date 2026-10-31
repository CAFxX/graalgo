package graalgo.rtstack;

import java.util.List;

import graalgo.frontend.FrontendException;
import graalgo.runtime.GoFrames;
import graalgo.runtime.GoFuncInfo;
import graalgo.runtime.PanicException;
import graalgo.runtime.StackTrace;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoString;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Unit tests for {@link RtStack} (R-03 remediation, 2026-10-31).
 *
 * <p>{@code RtStack} is the shared helper behind the {@code runtime}
 * stack-inspection wire nodes ({@code runtimecaller},
 * {@code runtimecallersframes}, {@code runtimefuncforpc}, ...), i.e.
 * the call-site accuracy the R-03 fix direction names. The tests pin
 * the frame construction against {@link StackTrace} interned PCs:
 * a frame built for an interned PC must carry that PC, its call-site
 * file/line, the qualified function name, and the definition-site
 * entry PC; an unknown PC must yield the zero frame, never a
 * plausible-but-wrong frame.
 *
 * <p>Not covered here: {@code structFieldTypeId}, which needs a
 * populated frontend type table ({@code BuildCtx}) that cannot be
 * built in this tree without a full frontend parse. Its caller
 * ({@code RuntimeFramesNextNode}) is covered differentially by the
 * traceback corpus ({@code test/corpus/traceback/tb00_simple.go} and
 * siblings, which exercise {@code CallersFrames} end to end).
 */
public final class RtStackTest {

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(long e, long a, String msg) {
        if (e != a) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    private static void assertEq(String e, String a, String msg) {
        if (!e.equals(a)) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    /** runtime.Frame-shaped descriptor: PC/Line/Entry longs, Func/Function/File objects. */
    private static GoStructDescriptor frameDesc() {
        // NB: List.of rejects null elements, so the element-kind list is
        // built as an ArrayList (the RtMemStatsTest pattern).
        java.util.List<GoKind> elemKinds = new java.util.ArrayList<>();
        for (int i = 0; i < 6; i++) {
            elemKinds.add(null);
        }
        return GoStructDescriptor.layout("runtime.Frame",
                List.of("PC", "Func", "Function", "File", "Line", "Entry"),
                List.of(GoKind.UINT64, GoKind.POINTER, GoKind.STRING,
                        GoKind.STRING, GoKind.UINT64, GoKind.UINT64),
                List.of(-1, -1, -1, -1, -1, -1),
                elemKinds);
    }

    private static String strOf(Object o) {
        return new String(((GoString) o).bytes(), java.nio.charset.StandardCharsets.UTF_8);
    }

    public void testBuildFrameForInternedPcIsAccurate() {
        StackTrace.recordFuncDef("rtstackLaneFunc", "example_test.go", 10);
        long pc = StackTrace.intern("rtstackLaneFunc", "example_test.go", 77);
        long entry = StackTrace.entryFor("rtstackLaneFunc");
        assertTrue(entry != 0, "definition-site entry must be known");
        GoStruct frame = RtStack.buildFrame(frameDesc(), pc);
        assertEq(pc, frame.getLong(0), "PC field must be the interned PC");
        assertEq(77, frame.getLong(4), "Line field must be the call-site line");
        assertEq(entry, frame.getLong(5), "Entry field must be the definition-site PC");
        assertEq("rtstackLaneFunc", strOf(frame.getObject(2)), "Function name");
        assertEq("example_test.go", strOf(frame.getObject(3)), "File");
        Object funcObj = frame.getObject(1);
        assertTrue(funcObj instanceof GoPointer, "Func field must be a GoPointer");
        Object referent = ((GoPointer) funcObj).referent();
        assertTrue(referent instanceof GoFuncInfo, "Func must point at GoFuncInfo");
        assertEq("rtstackLaneFunc", ((GoFuncInfo) referent).funcName(), "Func name");
        assertEq(entry, ((GoFuncInfo) referent).entry(), "Func entry PC");
        // The entry PC itself resolves to the definition site.
        StackTrace.PcInfo def = StackTrace.lookup(entry);
        assertTrue(def != null && def.line() == 10 && def.file().equals("example_test.go"),
                "entry PC must resolve to the definition site");
    }

    public void testBuildFrameQualifiesMain() {
        long pc = StackTrace.intern("main", "main_test.go", 5);
        GoStruct frame = RtStack.buildFrame(frameDesc(), pc);
        assertEq("main.main", strOf(frame.getObject(2)),
                "wire name 'main' must qualify to 'main.main' like gc traces");
        assertEq(5, frame.getLong(4), "Line for main frame");
    }

    public void testBuildFrameForUnknownPcIsZero() {
        GoStruct frame = RtStack.buildFrame(frameDesc(), Long.MAX_VALUE / 2);
        assertEq(0, frame.getLong(0), "unknown PC must yield zero PC");
        assertEq(0, frame.getLong(4), "unknown PC must yield zero Line");
        assertEq(0, frame.getLong(5), "unknown PC must yield zero Entry");
        assertTrue(frame.getObject(1) == null, "unknown PC must yield nil Func");
        assertEq("", strOf(frame.getObject(2)), "unknown PC must yield empty Function");
        assertEq("", strOf(frame.getObject(3)), "unknown PC must yield empty File");
    }

    public void testFieldIndexErrorsFailLoud() {
        GoStructDescriptor desc = frameDesc();
        try {
            RtStack.longIndex(desc, "NoSuchField", "frame");
            throw new AssertionError("missing field must throw");
        } catch (FrontendException expected) {
            assertTrue(expected.getMessage().contains("no field"),
                    "message must name the missing field: " + expected.getMessage());
        }
        try {
            RtStack.longIndex(desc, "Function", "frame");
            throw new AssertionError("objects-carried field used as longs must throw");
        } catch (FrontendException expected) {
            assertTrue(expected.getMessage().contains("not longs-carried"),
                    "message must name the carrier mismatch: " + expected.getMessage());
        }
        try {
            RtStack.objIndex(desc, "PC", "frame");
            throw new AssertionError("longs-carried field used as object must throw");
        } catch (FrontendException expected) {
            assertTrue(expected.getMessage().contains("not objects-carried"),
                    "message must name the carrier mismatch: " + expected.getMessage());
        }
    }

    public void testDerefFuncAndFrames() {
        GoFuncInfo info = new GoFuncInfo("someFunc", 42);
        assertTrue(RtStack.derefFunc(new GoPointer(info), null) == info,
                "derefFunc must return the pointed-at GoFuncInfo");
        GoFrames frames = new GoFrames(new long[]{1, 2, 3});
        assertTrue(RtStack.derefFrames(new GoPointer(frames), null) == frames,
                "derefFrames must return the pointed-at GoFrames");
        try {
            RtStack.derefFunc(new GoPointer(null), null);
            throw new AssertionError("nil *Func must panic");
        } catch (PanicException expected) {
            // gc nil-dereference panic, not a silent null.
        }
        try {
            RtStack.derefFrames(null, null);
            throw new AssertionError("nil *Frames must panic");
        } catch (PanicException expected) {
            // Expected.
        }
        try {
            RtStack.derefFunc(new GoPointer("not a func"), null);
            throw new AssertionError("non-func referent must panic");
        } catch (PanicException expected) {
            // Expected.
        }
    }
}
