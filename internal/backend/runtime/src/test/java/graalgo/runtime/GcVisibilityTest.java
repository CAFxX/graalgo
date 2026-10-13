package graalgo.runtime;

import java.lang.ref.WeakReference;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

import graalgo.values.GoPointer;

/**
 * P2-M4 F3 semantic backstop (WS D; ADR-0002 F3).
 *
 * <p>GC-visibility differential per value kind: allocate a payload, retain it
 * <em>only</em> through the carrier under test, force GC, and weakref-verify
 * survival. If a carrier stored references without JVM GC barriers (off-heap,
 * raw memory, misclassified primitive array), the payload could be collected
 * while still reachable — this test fails loudly in that case.
 *
 * <p>Carriers tested: Go struct field (Object[]), Go slice backing array,
 * Java ArrayList (Go slice model), Java HashMap (Go map model), GoPointer
 * referent, interface-converted value.
 *
 * <p>verifies: WS D (ADR-0002 F3 backstop)
 */
public final class GcVisibilityTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    /** Force GC until the weak ref clears or we give up (returns true if cleared). */
    private static boolean awaitClear(WeakReference<?> ref) {
        for (int i = 0; i < 20 && ref.get() != null; i++) {
            GcInterop.gc();
        }
        return ref.get() == null;
    }

    /** Assert the referent survives multiple forced GCs. */
    private static void assertSurvives(WeakReference<?> ref, String carrier) {
        for (int i = 0; i < 5; i++) {
            GcInterop.gc();
            assertTrue(ref.get() != null,
                    carrier + ": payload collected while still reachable (barrier miss!)");
        }
    }

    /** Go struct field carrier: payload held only in a struct's Object[] field. */
    public void testStructFieldVisibility() {
        byte[] payload = new byte[1 << 20];
        WeakReference<byte[]> weak = new WeakReference<>(payload);
        // Simulate a Go struct: Object[] fields, payload in field 0.
        Object[] structFields = new Object[1];
        structFields[0] = payload;
        payload = null; // drop local; only the struct field retains it
        assertSurvives(weak, "struct field");
        structFields[0] = null; // release
        assertTrue(awaitClear(weak), "struct field: payload not collected after release");
    }

    /** Go slice backing array carrier. */
    public void testSliceBackingArrayVisibility() {
        byte[] payload = new byte[1 << 20];
        WeakReference<byte[]> weak = new WeakReference<>(payload);
        // Simulate a Go slice: backing array with payload at index 0.
        Object[] backing = new Object[4];
        backing[0] = payload;
        payload = null;
        assertSurvives(weak, "slice backing array");
        backing[0] = null;
        assertTrue(awaitClear(weak), "slice backing array: payload not collected after release");
    }

    /** Java ArrayList carrier (Go slice model). */
    public void testArrayListVisibility() {
        byte[] payload = new byte[1 << 20];
        WeakReference<byte[]> weak = new WeakReference<>(payload);
        List<Object> list = new ArrayList<>();
        list.add(payload);
        payload = null;
        assertSurvives(weak, "ArrayList");
        list.clear();
        assertTrue(awaitClear(weak), "ArrayList: payload not collected after release");
    }

    /** Java HashMap carrier (Go map model). */
    public void testHashMapVisibility() {
        byte[] payload = new byte[1 << 20];
        WeakReference<byte[]> weak = new WeakReference<>(payload);
        Map<String, Object> map = new HashMap<>();
        map.put("key", payload);
        payload = null;
        assertSurvives(weak, "HashMap");
        map.clear();
        assertTrue(awaitClear(weak), "HashMap: payload not collected after release");
    }

    /** GoPointer referent carrier. */
    public void testGoPointerVisibility() {
        byte[] payload = new byte[1 << 20];
        WeakReference<byte[]> weak = new WeakReference<>(payload);
        GoPointer ptr = new GoPointer(payload);
        payload = null;
        assertSurvives(weak, "GoPointer referent");
        // GoPointer has no setter; drop the pointer itself.
        ptr = null;
        assertTrue(awaitClear(weak), "GoPointer: payload not collected after release");
    }

    /** Interface-converted value carrier (boxing). */
    public void testInterfaceBoxVisibility() {
        byte[] payload = new byte[1 << 20];
        WeakReference<byte[]> weak = new WeakReference<>(payload);
        Object iface = (Object) payload; // box into interface
        payload = null;
        assertSurvives(weak, "interface box");
        iface = null;
        assertTrue(awaitClear(weak), "interface box: payload not collected after release");
    }
}
