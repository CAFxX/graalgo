package graalgo.values;

/**
 * M9: unit tests for {@link CallSiteProbes}.
 *
 * <p>Verifies the disabled-by-default cost model (no recording), the
 * identity-histogram semantics (distinct keys counted once, repeats
 * aggregated), the receiver-key unwrapping, and the JSON snapshot shape.
 */
public final class CallSiteProbesTest {

    private static void assertEq(long expected, long actual, String what) {
        if (expected != actual) {
            throw new AssertionError(what + ": expected " + expected + " but got " + actual);
        }
    }

    private static void assertTrue(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError(what);
        }
    }

    public void testDisabledByDefault() {
        CallSiteProbes.reset();
        CallSiteProbes.setEnabled(false);
        try {
            CallSiteProbes.Site s = CallSiteProbes.registerSite("disabled-site");
            Object k1 = new Object();
            s.recordTarget(k1);
            s.recordTarget(k1);
            s.recordReceiver(k1);
            assertEq(0, s.targetCardinality(), "disabled: no targets recorded");
            assertEq(0, s.receiverCardinality(), "disabled: no receivers recorded");
            assertEq(0, s.totalCalls(), "disabled: no calls counted");
        } finally {
            CallSiteProbes.reset();
        }
    }

    public void testRecordsTargetCardinality() {
        CallSiteProbes.reset();
        CallSiteProbes.setEnabled(true);
        try {
            CallSiteProbes.Site s = CallSiteProbes.registerSite("card-site");
            Object k1 = new Object();
            Object k2 = new Object();
            s.recordTarget(k1);
            s.recordTarget(k1); // repeat: same key, must not inflate cardinality
            s.recordTarget(k2);
            assertEq(2, s.targetCardinality(), "two distinct targets");
            assertEq(3, s.totalCalls(), "three calls total");
        } finally {
            CallSiteProbes.setEnabled(false);
            CallSiteProbes.reset();
        }
    }

    public void testIdentityNotEquality() {
        CallSiteProbes.reset();
        CallSiteProbes.setEnabled(true);
        try {
            CallSiteProbes.Site s = CallSiteProbes.registerSite("ident-site");
            // Equal-but-not-identical keys must count as distinct (itab identity semantics).
            s.recordTarget(new String("k"));
            s.recordTarget(new String("k"));
            assertEq(2, s.targetCardinality(), "identity, not equality");
        } finally {
            CallSiteProbes.setEnabled(false);
            CallSiteProbes.reset();
        }
    }

    public void testReceiverKey() {
        // Struct → its descriptor identity.
        GoStructDescriptor d = new GoStructDescriptor("P", java.util.List.of());
        GoStruct st = new GoStruct(d, new long[0], new Object[0]);
        assertTrue(CallSiteProbes.receiverKey(st) == d, "struct key is the descriptor");
        // Pointer → unwrapped to the referent struct's descriptor.
        assertTrue(CallSiteProbes.receiverKey(new GoPointer(st)) == d,
                "pointer unwrapped to descriptor");
        // Anything else → its class.
        assertTrue(CallSiteProbes.receiverKey("s") == String.class,
                "fallback key is the runtime class");
        assertTrue(CallSiteProbes.receiverKey(null) == null, "null receiver → null key");
    }

    public void testSnapshotJsonShape() {
        CallSiteProbes.reset();
        CallSiteProbes.setEnabled(true);
        try {
            CallSiteProbes.Site s = CallSiteProbes.registerSite("snap.go:1:2");
            Object k = new Object();
            s.recordTarget(k);
            s.recordReceiver(k);
            String json = CallSiteProbes.snapshotJson();
            assertTrue(json.contains("\"site\":\"snap.go:1:2\""), "site name in snapshot: " + json);
            assertTrue(json.contains("\"calls\":1"), "call count in snapshot: " + json);
            assertTrue(json.contains("\"targets\":1"), "target cardinality in snapshot: " + json);
            assertTrue(json.contains("\"receiverTypes\":1"),
                    "receiver cardinality in snapshot: " + json);
        } finally {
            CallSiteProbes.setEnabled(false);
            CallSiteProbes.reset();
        }
    }
}
