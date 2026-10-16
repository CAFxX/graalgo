package graalgo.frontend;

import com.oracle.truffle.api.RootCallTarget;
import graalgo.runtime.GoCalls;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoSlice;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import java.lang.reflect.Proxy;
import java.util.List;

/**
 * Regression test for bug-043 (2026-10-15): method values must copy
 * array/struct receivers — at capture time (like defer arguments) and
 * at every invocation (like every other call boundary).
 *
 * <p>The mechanism is {@link Boxing#copyForAssign}: {@code MethodValueNode}
 * wraps receiver boxing with it at formation, {@code GoFunc} records the
 * bound receiver's kind, and {@code GoCalls#callArgs} re-applies the copy
 * at each invocation. Slices/maps/pointers pass through by reference,
 * as Go requires.
 *
 * <p>RED (pre-fix): the bound receiver was the same object at capture
 * and across calls, so {@code testArrayReceiverCopiedPerCall} fails on
 * the identity check and {@code testCallMutationDoesNotLeakAcrossCalls}
 * fails because the second call observes the first call's mutation.
 * GREEN: every call gets a fresh deep copy; reference kinds stay shared.
 */
public final class MethodValueSemanticsTest {

    private static RootCallTarget dummyTarget() {
        return (RootCallTarget) Proxy.newProxyInstance(
                MethodValueSemanticsTest.class.getClassLoader(),
                new Class<?>[] {RootCallTarget.class},
                (proxy, method, args) -> null);
    }

    private static GoFunc methodValue(Object receiver, GoKind kind) {
        return new GoFunc(dummyTarget(), null, null,
                new Object[] {receiver}, new GoKind[] {kind});
    }

    /** Array receiver: two invocations get distinct, equal copies. */
    public void testArrayReceiverCopiedPerCall() {
        GoFunc f = methodValue(new int[] {1, 2, 3, 4}, GoKind.ARRAY);
        Object r1 = GoCalls.callArgs(f, new Object[0])[1];
        Object r2 = GoCalls.callArgs(f, new Object[0])[1];
        if (!(r1 instanceof int[] a1) || !(r2 instanceof int[] a2)) {
            throw new AssertionError("bound array receiver lost its type (bug-043)");
        }
        if (a1 == a2) {
            throw new AssertionError(
                    "callArgs returned the same array for two calls — "
                            + "per-call copy missing (bug-043)");
        }
        for (int i = 0; i < 4; i++) {
            if (a1[i] != i + 1 || a2[i] != i + 1) {
                throw new AssertionError("bound array copy has wrong contents (bug-043)");
            }
        }
    }

    /** Mutation through one call's receiver is invisible to the next call. */
    public void testCallMutationDoesNotLeakAcrossCalls() {
        int[] stored = new int[] {1, 2, 3, 4};
        GoFunc f = methodValue(stored, GoKind.ARRAY);
        int[] first = (int[]) GoCalls.callArgs(f, new Object[0])[1];
        first[0] = 99; // callee mutates its receiver copy
        int[] second = (int[]) GoCalls.callArgs(f, new Object[0])[1];
        if (second[0] != 1) {
            throw new AssertionError(
                    "second call observed first call's mutation: second[0] = "
                            + second[0] + ", expected 1 (bug-043)");
        }
        if (stored[0] != 1) {
            throw new AssertionError(
                    "mutation leaked into the stored bound receiver: stored[0] = "
                            + stored[0] + ", expected 1 (bug-043)");
        }
    }

    /** Nested arrays are deep-copied per call. */
    public void testNestedArrayReceiverDeepCopiedPerCall() {
        GoFunc f = methodValue(new int[][] {{1, 2}, {3, 4}}, GoKind.ARRAY);
        int[][] r1 = (int[][]) GoCalls.callArgs(f, new Object[0])[1];
        int[][] r2 = (int[][]) GoCalls.callArgs(f, new Object[0])[1];
        if (r1[1] == r2[1]) {
            throw new AssertionError(
                    "inner array aliased across calls (bug-043)");
        }
        r1[1][1] = 99;
        if (r2[1][1] != 4) {
            throw new AssertionError(
                    "inner-array mutation leaked across calls (bug-043)");
        }
    }

    /** Struct receiver: per-call deep copy; callee mutation stays local. */
    public void testStructReceiverCopiedPerCall() {
        GoStructDescriptor desc = new GoStructDescriptor("T",
                List.of(new GoStructDescriptor.Field("X", GoKind.INT64, true, 0,
                        -1, null, null)));
        GoStruct stored = new GoStruct(desc, new long[] {7}, new Object[0]);
        GoFunc f = methodValue(stored, GoKind.STRUCT);
        GoStruct r1 = (GoStruct) GoCalls.callArgs(f, new Object[0])[1];
        GoStruct r2 = (GoStruct) GoCalls.callArgs(f, new Object[0])[1];
        if (r1 == r2 || r1 == stored) {
            throw new AssertionError(
                    "struct receiver not copied per call (bug-043)");
        }
        if (r1.getLong(0) != 7 || r2.getLong(0) != 7) {
            throw new AssertionError("struct copy has wrong contents (bug-043)");
        }
        r1.setLong(0, 99);
        if (r2.getLong(0) != 7 || stored.getLong(0) != 7) {
            throw new AssertionError(
                    "struct mutation leaked across calls or into the stored "
                            + "receiver (bug-043)");
        }
    }

    /** Slice receiver: reference semantics — the same object every call. */
    public void testSliceReceiverPassedThrough() {
        GoSlice s = new GoSlice(new int[] {1, 2, 3}, 0, 3, 3);
        GoFunc f = methodValue(s, GoKind.SLICE);
        Object r1 = GoCalls.callArgs(f, new Object[0])[1];
        Object r2 = GoCalls.callArgs(f, new Object[0])[1];
        if (r1 != s || r2 != s) {
            throw new AssertionError(
                    "slice receiver was copied — slices share backing storage "
                            + "in Go (bug-043 over-copy)");
        }
    }

    /** Map receiver: reference semantics — the same object every call. */
    public void testMapReceiverPassedThrough() {
        GoMap m = new GoMap();
        GoFunc f = methodValue(m, GoKind.MAP);
        Object r1 = GoCalls.callArgs(f, new Object[0])[1];
        Object r2 = GoCalls.callArgs(f, new Object[0])[1];
        if (r1 != m || r2 != m) {
            throw new AssertionError(
                    "map receiver was copied — maps are references in Go "
                            + "(bug-043 over-copy)");
        }
    }

    /** Legacy GoFunc without recorded kinds: bound args pass through. */
    public void testUnkindedBoundArgsPassThrough() {
        int[] bound = new int[] {1, 2};
        GoFunc f = new GoFunc(dummyTarget(), null, null, new Object[] {bound});
        Object r = GoCalls.callArgs(f, new Object[0])[1];
        if (r != bound) {
            throw new AssertionError(
                    "GoFunc without boundArgKinds must pass bound args through "
                            + "unchanged (bug-043)");
        }
    }
}
