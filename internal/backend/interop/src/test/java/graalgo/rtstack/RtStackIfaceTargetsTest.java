package graalgo.rtstack;

import java.nio.charset.StandardCharsets;

import graalgo.iface.IfaceMethodTable;
import graalgo.iface.IfaceMethodTarget;
import graalgo.runtime.GoCalls;
import graalgo.runtime.GoFuncInfo;
import graalgo.values.GoFunc;
import graalgo.values.GoPointer;
import graalgo.values.GoString;

/**
 * Unit tests for {@link RtStackIfaceTargets} (review-20261103-funcname):
 * the interface-dispatch targets for (*runtime.Func).Name()/Entry() must
 * return exactly what the direct wire nodes return for the same handle.
 * Pre-fix there was no installed table for *runtime.Func at all, so an
 * interface call died at dispatch with "no method table installed".
 */
public final class RtStackIfaceTargetsTest {

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(Object e, Object a, String msg) {
        if (!e.equals(a)) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    private static GoPointer ptr(String name, long entry) {
        return new GoPointer(new GoFuncInfo(name, entry));
    }

    public void testNameTargetReturnsHandleName() {
        IfaceMethodTable.clear();
        RtStackIfaceTargets.install();
        IfaceMethodTarget target = IfaceMethodTable.lookup("*runtime.Func", "Name");
        assertTrue(target != null, "Name target must be installed for *runtime.Func");
        assertTrue(target.pointerReceiver(), "Name has a pointer receiver");
        assertTrue(target.recvPath().length == 0, "Name is a direct method (empty recvPath)");
        GoFunc fn = target.func();
        Object result = GoCalls.invoke(fn, new Object[]{ptr("main.main", 4242)});
        assertTrue(result instanceof GoString,
                "Name() must return a GoString, got: " + result);
        assertEq("main.main",
                new String(((GoString) result).bytes(), StandardCharsets.UTF_8),
                "Name() must return the handle's function name");
    }

    public void testEntryTargetReturnsHandleEntry() {
        IfaceMethodTable.clear();
        RtStackIfaceTargets.install();
        IfaceMethodTarget target = IfaceMethodTable.lookup("*runtime.Func", "Entry");
        assertTrue(target != null, "Entry target must be installed for *runtime.Func");
        Object result = GoCalls.invoke(target.func(), new Object[]{ptr("main.main", 4242)});
        assertEq(4242L, result, "Entry() must return the handle's entry PC");
    }

    public void testNameTargetReturnsEmptyForNil() {
        IfaceMethodTable.clear();
        RtStackIfaceTargets.install();
        IfaceMethodTarget target = IfaceMethodTable.lookup("*runtime.Func", "Name");
        assertTrue(target != null, "Name target must be installed for *runtime.Func");
        // gc: (*Func).Name() returns "" for a nil receiver (symtab.go).
        Object result = GoCalls.invoke(target.func(), new Object[]{new GoPointer(null)});
        assertTrue(result instanceof GoString, "nil Name() must return a GoString");
        assertEq("", new String(((GoString) result).bytes(), StandardCharsets.UTF_8),
                "nil Name() must return empty string, like gc");
    }

    public void testInstallIsIdempotent() {
        IfaceMethodTable.clear();
        RtStackIfaceTargets.install();
        RtStackIfaceTargets.install();
        IfaceMethodTarget target = IfaceMethodTable.lookup("*runtime.Func", "Name");
        assertTrue(target != null, "Name target must survive a second install");
        Object result = GoCalls.invoke(target.func(), new Object[]{ptr("main.helper", 7)});
        assertTrue(result instanceof GoString, "re-installed target must still dispatch");
        assertEq("main.helper",
                new String(((GoString) result).bytes(), StandardCharsets.UTF_8),
                "re-installed Name() must return the handle's name");
    }
}
