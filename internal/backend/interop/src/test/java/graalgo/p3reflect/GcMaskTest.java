package graalgo.p3reflect;

import graalgo.values.GoKind;

import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

/**
 * fix-redmaster-r42: unit tests for {@code AbiTypes.gcMaskWords} — the
 * GC pointer mask behind {@code reflect.Type.GcSlice}, used by
 * {@code emitGCMask} during {@code ArrayOf}/{@code StructOf}
 * construction. A wrong mask makes the runtime scan a non-pointer word
 * as a pointer (or miss a live pointer); a nil mask on a pointer type
 * made {@code GcSlice} panic (the r42 {@code fo} failure).
 *
 * <p>Tests build {@code TypeInfo} values directly and invoke the
 * private helper reflectively, asserting the exact mask word by word.
 * Falsifiability: dropping the STRING/SLICE data-word-only rule (e.g.
 * marking all words) fails the string/slice tests; dropping the
 * INTERFACE second word fails the interface test; removing the
 * visiting guard on the recursive test hangs instead of returning.
 */
public final class GcMaskTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static boolean[] mask(ReflectTypes.TypeInfo info) throws Exception {
        Method m = AbiTypes.class.getDeclaredMethod("gcMaskWords",
                ReflectTypes.TypeInfo.class, Set.class);
        m.setAccessible(true);
        return (boolean[]) m.invoke(null, info, new HashSet<Long>());
    }

    private static ReflectTypes.TypeInfo basic(GoKind kind, long size) {
        return new ReflectTypes.TypeInfo(-1, kind, "", "", size, 8,
                null, null, 0, null, null, null, null, null, false, null, "", -1);
    }

    private static ReflectTypes.TypeInfo struct(long size, ReflectTypes.FieldInfo... fields) {
        return new ReflectTypes.TypeInfo(-1, GoKind.STRUCT, "", "", size, 8,
                null, null, 0, null, List.of(fields), null, null, null, false, null, "", -1);
    }

    private static ReflectTypes.FieldInfo field(String name, ReflectTypes.TypeInfo type, long offset) {
        return new ReflectTypes.FieldInfo(name, "", type, "", offset, new int[]{0}, false);
    }

    private static void assertMask(boolean[] expected, boolean[] actual, String msg) {
        assertTrue(expected.length == actual.length,
                msg + ": length expected=" + expected.length + " actual=" + actual.length);
        for (int i = 0; i < expected.length; i++) {
            assertTrue(expected[i] == actual[i],
                    msg + ": word " + i + " expected=" + expected[i] + " actual=" + actual[i]);
        }
    }

    public void testPointerHasOnePointerWord() throws Exception {
        assertMask(new boolean[]{true}, mask(basic(GoKind.POINTER, 8)), "pointer");
    }

    public void testIntHasNoPointerWords() throws Exception {
        assertMask(new boolean[]{false}, mask(basic(GoKind.INT64, 8)), "int");
    }

    public void testStringMarksOnlyDataWord() throws Exception {
        assertMask(new boolean[]{true, false}, mask(basic(GoKind.STRING, 16)),
                "string (len word is an int)");
    }

    public void testSliceMarksOnlyDataWord() throws Exception {
        assertMask(new boolean[]{true, false, false}, mask(basic(GoKind.SLICE, 24)),
                "slice (len/cap words are ints)");
    }

    public void testInterfaceMarksBothWords() throws Exception {
        assertMask(new boolean[]{true, true}, mask(basic(GoKind.INTERFACE, 16)),
                "interface (type word + data word)");
    }

    public void testStructOrsFieldMasksByOffset() throws Exception {
        ReflectTypes.TypeInfo s = struct(16,
                field("A", basic(GoKind.INT64, 8), 0),
                field("B", basic(GoKind.POINTER, 8), 8));
        assertMask(new boolean[]{false, true}, mask(s), "struct{int,*int}");
    }

    public void testArrayRepeatsElementMask() throws Exception {
        ReflectTypes.TypeInfo elem = basic(GoKind.POINTER, 8);
        ReflectTypes.TypeInfo arr = new ReflectTypes.TypeInfo(-1, GoKind.ARRAY, "", "", 24, 8,
                elem, null, 3, null, null, null, null, null, false, null, "", -1);
        assertMask(new boolean[]{true, true, true}, mask(arr), "[3]*int");
    }

    public void testArrayOfPointerlessStructHasNoPointerWords() throws Exception {
        ReflectTypes.TypeInfo elem = struct(8, field("A", basic(GoKind.INT64, 8), 0));
        ReflectTypes.TypeInfo arr = new ReflectTypes.TypeInfo(-1, GoKind.ARRAY, "", "", 16, 8,
                elem, null, 2, null, null, null, null, null, false, null, "", -1);
        assertMask(new boolean[]{false, false}, mask(arr), "[2]struct{int}");
    }

    public void testRecursiveStructTerminates() throws Exception {
        // A struct naming itself as a field type: the visiting guard must
        // cut the recursion instead of looping forever.
        List<ReflectTypes.FieldInfo> fields = new ArrayList<>();
        ReflectTypes.TypeInfo self = new ReflectTypes.TypeInfo(-7, GoKind.STRUCT, "", "", 8, 8,
                null, null, 0, null, fields, null, null, null, false, null, "", -1);
        fields.add(field("Next", self, 0));
        assertMask(new boolean[]{false}, mask(self), "recursive struct terminates with empty mask");
    }
}
